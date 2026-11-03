package observability

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	cppricing "github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/observability/model"
	"github.com/example/llm-gateway/internal/gateway/observability/repository"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type PostgresConnector func(context.Context) (*gorm.DB, error)

type mutableCallLogSink struct {
	mu   sync.RWMutex
	repo *repository.CallLogRepository
}

func (s *mutableCallLogSink) Set(repo *repository.CallLogRepository) {
	s.mu.Lock()
	s.repo = repo
	s.mu.Unlock()
}

func (s *mutableCallLogSink) Clear() {
	s.Set(nil)
}

func (s *mutableCallLogSink) Get() *repository.CallLogRepository {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repo
}

func (s *mutableCallLogSink) Available() bool {
	return s.Get() != nil
}

func (s *mutableCallLogSink) BatchInsert(ctx context.Context, logs []model.CallLog) error {
	repo := s.Get()
	if repo == nil {
		return errors.New("postgres call-log sink is not connected")
	}
	return repo.BatchInsert(ctx, logs)
}

type livePricingResolver struct {
	mu       sync.RWMutex
	primary  PricingResolver
	fallback PricingResolver
}

func (r *livePricingResolver) SetPrimary(primary PricingResolver) {
	r.mu.Lock()
	r.primary = primary
	r.mu.Unlock()
}

func (r *livePricingResolver) Resolve(modelKey string) (core.ModelPricing, bool) {
	return r.resolveForProvider(modelKey, "")
}

func (r *livePricingResolver) ResolveForProvider(modelKey, provider string) (core.ModelPricing, bool) {
	return r.resolveForProvider(modelKey, provider)
}

func (r *livePricingResolver) resolveForProvider(modelKey, provider string) (core.ModelPricing, bool) {
	r.mu.RLock()
	primary, fallback := r.primary, r.fallback
	r.mu.RUnlock()
	for _, resolver := range []PricingResolver{primary, fallback} {
		if resolver == nil {
			continue
		}
		if providerResolver, ok := resolver.(ProviderPricingResolver); ok {
			if pricing, found := providerResolver.ResolveForProvider(modelKey, provider); found {
				return pricing, true
			}
			continue
		}
		if pricing, found := resolver.Resolve(modelKey); found {
			return pricing, true
		}
	}
	return core.ModelPricing{}, false
}

// StartProductionCollector starts the production stream-first pipeline.
// Every event is durably appended to Redis before PostgreSQL is consulted.
// PostgreSQL is connected in the background and may be absent at process start.
func StartProductionCollector(
	ctx context.Context,
	cfg CollectorConfig,
	rdb *redis.Client,
	fallbackPricing PricingResolver,
	connect PostgresConnector,
	onConnect func(*gorm.DB),
) *Collector {
	pricing := &livePricingResolver{fallback: fallbackPricing}
	collector := NewCollector(cfg, pricing)
	collector.streamFirst = true
	collector.spool = NewCallLogSpoolWithConfig(rdb, CallLogSpoolConfig{
		MaxRetries:   cfg.SpoolMaxRetries,
		ClaimMinIdle: cfg.SpoolClaimMinIdle,
		Capacity:     cfg.SpoolCapacity,
	})
	collector.streamSink = &mutableCallLogSink{}
	collector.writerDone = make(chan struct{})
	go func() {
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			collector.runSpoolHealthMonitor(ctx, rdb)
		}()
		go func() {
			defer workers.Done()
			collector.runProductionPipeline(ctx, pricing, connect, onConnect)
		}()
		workers.Wait()
		defer close(collector.writerDone)
	}()
	return collector
}

func (c *Collector) runSpoolHealthMonitor(ctx context.Context, rdb *redis.Client) {
	if rdb == nil {
		return
	}
	interval := c.cfg.SpoolHealthCheckInterval
	if interval <= 0 {
		interval = time.Second
	}
	c.refreshSpoolHealth(ctx, rdb)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshSpoolHealth(ctx, rdb)
		}
	}
}

func (c *Collector) refreshSpoolHealth(ctx context.Context, rdb *redis.Client) {
	if c == nil || rdb == nil {
		return
	}
	highWatermark := c.cfg.SpoolHighWatermark
	if highWatermark <= 0 {
		highWatermark = 80_000
	}
	pipe := rdb.Pipeline()
	lengthCmd := pipe.XLen(ctx, CallLogSpoolStream)
	inflightCmd := pipe.ZCard(ctx, budget.BillableInflightKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}
	length := lengthCmd.Val()
	inflight := inflightCmd.Val()
	effectiveUsage := length + inflight
	high := effectiveUsage >= highWatermark
	changed := c.spoolHighWatermark.Swap(high) != high
	if high && changed {
		if c.spoolHighWaterCounter != nil {
			c.spoolHighWaterCounter.Add(ctx, 1)
		}
		slog.ErrorContext(ctx, "call log spool reached high watermark; rejecting new billable requests",
			"stream_length", length,
			"inflight_reservations", inflight,
			"effective_usage", effectiveUsage,
			"high_watermark", highWatermark,
			"capacity", c.cfg.SpoolCapacity,
		)
	}
}

func (c *Collector) runProductionPipeline(
	ctx context.Context,
	pricing *livePricingResolver,
	connect PostgresConnector,
	onConnect func(*gorm.DB),
) {
	retry := c.cfg.DBConnectRetryInterval
	if retry <= 0 {
		retry = 5 * time.Second
	}
	for ctx.Err() == nil {
		db, err := connect(ctx)
		if err != nil {
			slog.WarnContext(ctx, "call log postgres unavailable; Redis spool remains active", "error", err)
			if !waitFor(ctx, retry) {
				return
			}
			continue
		}
		if !callLogSchemaReady(db) {
			slog.WarnContext(ctx, "call log schema is not ready; Redis spool remains active")
			if !waitFor(ctx, retry) {
				return
			}
			continue
		}
		repo := repository.NewCallLogRepository(db)
		c.streamSink.Set(repo)
		table := cppricing.NewPricingTable()
		if err := table.Refresh(ctx, db); err != nil {
			slog.WarnContext(ctx, "pricing table refresh after postgres reconnect failed", "error", err)
		} else {
			pricing.SetPrimary(table)
		}
		if onConnect != nil {
			onConnect(db)
		}
		slog.InfoContext(ctx, "call log postgres connected; starting spool replay")

		replayInterval := c.cfg.SpoolReplayInterval
		if replayInterval <= 0 {
			replayInterval = time.Second
		}
		replayTicker := time.NewTicker(replayInterval)
		pingTicker := time.NewTicker(retry)
		priceRefreshInterval := c.cfg.PriceRefreshInterval
		if priceRefreshInterval <= 0 {
			priceRefreshInterval = 5 * time.Minute
		}
		priceTicker := time.NewTicker(priceRefreshInterval)
		replay := func() {
			if c.spool != nil {
				if err := c.spool.ReplayOnce(ctx, c.streamSink); err != nil && ctx.Err() == nil {
					slog.ErrorContext(ctx, "call log spool replay failed", "error", err)
				}
			}
		}
		replay()
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				replayTicker.Stop()
				pingTicker.Stop()
				priceTicker.Stop()
				c.finalProductionReplay()
				return
			case <-replayTicker.C:
				replay()
			case <-pingTicker.C:
				sqlDB, dbErr := db.DB()
				if dbErr != nil || sqlDB.PingContext(ctx) != nil {
					replayTicker.Stop()
					pingTicker.Stop()
					priceTicker.Stop()
					c.streamSink.Clear()
					pricing.SetPrimary(nil)
					slog.WarnContext(ctx, "call log postgres connection lost; returning to Redis-only mode")
					goto reconnect
				}
			case <-priceTicker.C:
				if err := table.Refresh(ctx, db); err != nil {
					slog.WarnContext(ctx, "pricing table refresh failed", "error", err)
				}
			}
		}
	reconnect:
	}
}

func callLogSchemaReady(db *gorm.DB) bool {
	if db == nil || !db.Migrator().HasTable(&model.CallLog{}) {
		return false
	}
	// 每加一个 gateway 会写的列，这里都要补一条：探针的意义就是「列没上就退回 Redis-only」，
	// 漏一条会让 gateway 先于迁移上线时 INSERT 未知列失败，全量 call log 掉 spool。
	return db.Migrator().HasColumn(&model.CallLog{}, "KeyType") &&
		db.Migrator().HasColumn(&model.CallLog{}, "BudgetTierConstraintsSnapshot") &&
		db.Migrator().HasColumn(&model.CallLog{}, "UpstreamProtocol")
}

func (c *Collector) finalProductionReplay() {
	if c.spool == nil || c.streamSink == nil || !c.streamSink.Available() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.spool.ReplayOnce(ctx, c.streamSink); err != nil {
		slog.ErrorContext(ctx, "call log final spool replay failed", "error", err)
	}
}

func waitFor(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
