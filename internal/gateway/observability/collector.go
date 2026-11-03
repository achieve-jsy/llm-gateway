package observability

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/observability/model"
	"github.com/example/llm-gateway/internal/gateway/observability/repository"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

// Collector 埋点采集器
// 在 Logger 中间件 c.Next() 后调用，构造 UsageEvent 并异步投递
type Collector struct {
	ch                     chan UsageEvent
	writer                 *BatchWriter
	pricing                PricingResolver
	cfg                    CollectorConfig
	syncFallbackCounter    metric.Int64Counter
	deliveryFailureCounter metric.Int64Counter
	writerDone             chan struct{}
	streamFirst            bool
	spool                  *CallLogSpool
	streamSink             *mutableCallLogSink
	spoolHighWatermark     atomic.Bool
	spoolHighWaterCounter  metric.Int64Counter
	reconciliationRequired metric.Int64Counter
}

// PricingResolver 定价查询接口
type PricingResolver interface {
	Resolve(modelKey string) (core.ModelPricing, bool)
}

type PricingResolverFunc func(modelKey string) (core.ModelPricing, bool)

func (f PricingResolverFunc) Resolve(modelKey string) (core.ModelPricing, bool) {
	return f(modelKey)
}

type ProviderPricingResolver interface {
	PricingResolver
	ResolveForProvider(modelKey, provider string) (core.ModelPricing, bool)
}

type ProviderPricingResolverFunc func(modelKey, provider string) (core.ModelPricing, bool)

const largePromptCacheMissThreshold = 100_000

func (f ProviderPricingResolverFunc) Resolve(modelKey string) (core.ModelPricing, bool) {
	return f(modelKey, "")
}

func (f ProviderPricingResolverFunc) ResolveForProvider(modelKey, provider string) (core.ModelPricing, bool) {
	return f(modelKey, provider)
}

type ChainedPricingResolver []PricingResolver

func (r ChainedPricingResolver) Resolve(modelKey string) (core.ModelPricing, bool) {
	for _, resolver := range r {
		if resolver == nil {
			continue
		}
		if pricing, ok := resolver.Resolve(modelKey); ok {
			return pricing, true
		}
	}
	return core.ModelPricing{}, false
}

func (r ChainedPricingResolver) ResolveForProvider(modelKey, provider string) (core.ModelPricing, bool) {
	for _, resolver := range r {
		if resolver == nil {
			continue
		}
		if providerResolver, ok := resolver.(ProviderPricingResolver); ok {
			if pricing, found := providerResolver.ResolveForProvider(modelKey, provider); found {
				return pricing, true
			}
			continue
		}
		if pricing, ok := resolver.Resolve(modelKey); ok {
			return pricing, true
		}
	}
	return core.ModelPricing{}, false
}

// NewCollector 创建 Collector
func NewCollector(cfg CollectorConfig, pricing PricingResolver) *Collector {
	if cfg.EnqueueTimeout <= 0 {
		cfg.EnqueueTimeout = 25 * time.Millisecond
	}
	if cfg.SyncFallbackTimeout <= 0 {
		cfg.SyncFallbackTimeout = 2 * time.Second
	}
	meter := otel.Meter("llm-gateway/observability")
	syncFallbackCounter, _ := meter.Int64Counter("gateway_call_log_sync_fallback_total")
	deliveryFailureCounter, _ := meter.Int64Counter("gateway_call_log_delivery_failure_total")
	spoolHighWaterCounter, _ := meter.Int64Counter("gateway_call_log_spool_high_watermark_total")
	reconciliationRequired, _ := meter.Int64Counter("gateway_budget_counter_reconciliation_required_total")
	return &Collector{
		ch:                     make(chan UsageEvent, cfg.BufferSize),
		pricing:                pricing,
		cfg:                    cfg,
		syncFallbackCounter:    syncFallbackCounter,
		deliveryFailureCounter: deliveryFailureCounter,
		spoolHighWaterCounter:  spoolHighWaterCounter,
		reconciliationRequired: reconciliationRequired,
	}
}

func (c *Collector) CanAcceptBillableRequest() bool {
	return c == nil || !c.spoolHighWatermark.Load()
}

func (c *Collector) BudgetAdmissionConfig() budget.AdmissionConfig {
	if c == nil {
		return budget.AdmissionConfig{}
	}
	return budget.AdmissionConfig{
		SpoolCapacity:  c.cfg.SpoolCapacity,
		ReservationTTL: c.cfg.BillableReservationTTL,
	}
}

func (c *Collector) HandlesBillableReservation() bool {
	return c != nil && c.streamFirst && c.spool != nil
}

func (c *Collector) HasLivePostgresSink() bool {
	return c != nil && c.streamSink != nil && c.streamSink.Available()
}

// StartWriter 启动批量写入 goroutine
func (c *Collector) StartWriter(ctx context.Context, repo *repository.CallLogRepository) {
	c.StartWriterWithSpool(ctx, repo, nil)
}

// StartWriterWithSpool starts reliable DB persistence with Redis Stream
// failover. request_id's unique constraint makes replay idempotent.
func (c *Collector) StartWriterWithSpool(
	ctx context.Context,
	repo *repository.CallLogRepository,
	rdb *redis.Client,
) {
	meter := otel.Meter("llm-gateway/observability")
	retryCounter, _ := meter.Int64Counter("gateway_call_log_db_retry_total")
	unrecoverableCounter, _ := meter.Int64Counter("gateway_call_log_unrecoverable_total")
	c.writer = &BatchWriter{
		repo:                 repo,
		db:                   repo.DB(),
		flushN:               c.cfg.FlushN,
		interval:             c.cfg.FlushInterval,
		retryCount:           c.cfg.DBRetryCount,
		retryBackoff:         c.cfg.DBRetryBackoff,
		spool:                NewCallLogSpool(rdb),
		retryCounter:         retryCounter,
		unrecoverableCounter: unrecoverableCounter,
	}
	c.writerDone = make(chan struct{})
	if c.writer.spool != nil {
		go c.writer.spool.RunReplay(ctx, repo, c.cfg.SpoolReplayInterval)
	}
	go func() {
		defer close(c.writerDone)
		c.writer.Run(ctx, c.ch)
	}()
}

func (c *Collector) Wait(ctx context.Context) error {
	if c == nil || c.writerDone == nil {
		return nil
	}
	select {
	case <-c.writerDone:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// NewUsageEvent 从 gin.Context 提取字段构造 UsageEvent
func (c *Collector) NewUsageEvent(ctx *gin.Context) UsageEvent {
	now := time.Now()
	// 入站端点与其归一化操作类型。call_type 在采集侧唯一算一次（DeriveCallType）：方法参与判定
	// ——POST /v1/contents/generations/tasks 是创建、GET 同前缀 + task_id 是查询。未登记端点
	// DeriveCallType 返回空串（不猜测、不写 "unknown" 之类的兜底值），读侧按 inbound_endpoint 兜底。
	inboundPath := ctx.Request.URL.Path
	event := UsageEvent{
		RequestID: getString(ctx, "request_id"),
		Timestamp: now,
		BillableReserved: func() bool {
			_, reserved := ctx.Get(budget.CtxKeyBillableReservation)
			return reserved
		}(),
		BillableReservation:     budget.GetBillableReservation(ctx),
		PendingBudgetAccount:    budget.GetPendingAccount(ctx),
		BudgetAdmissionFailOpen: budget.GetAdmissionOutcome(ctx) == budget.AdmissionInfrastructureFailOpen,
		KeyID:                   getString(ctx, "key_id"),
		KeyName:                 getString(ctx, "key_name"),
		KeyType:                 normalizeKeyType(getString(ctx, "key_type")),
		UserID:                  getString(ctx, "user_id"),
		UserName:                getString(ctx, "user_name"),
		DeptID:                  getString(ctx, "dept_id"),
		DeptName:                getString(ctx, "dept_name"),
		StudioID:                getString(ctx, "studio_id"),
		StudioName:              getString(ctx, "studio_name"),
		Model:                   getString(ctx, "model"),
		RoutedModel:             getString(ctx, "routed_model"),
		GroupName:               getString(ctx, "group_name"),
		Provider:                getString(ctx, "provider"),
		ReasoningEffort:         getString(ctx, "reasoning_effort"),
		RoutedViaGroup: func() bool {
			v, ok := ctx.Get("routed_via_group")
			if !ok {
				return false
			}
			b, _ := v.(bool)
			return b
		}(),
		InboundEndpoint: inboundPath,
		CallType:        DeriveCallType(ctx.Request.Method, inboundPath),
		UserAgent:       ctx.Request.UserAgent(),
		IPAddress:       ctx.ClientIP(),
		Status:          getInt(ctx, "status"),
		UpstreamStatus:  getInt(ctx, "upstream_status"),
		// 配额拒绝事实：仅当本请求确实被配额闸门拒绝时（quota_gate 非空）才记录限额，
		// 避免把"未发生配额判定"的行写成 limit=0 而误导排查。
		QuotaGate:  getString(ctx, "quota_gate"),
		QuotaLayer: getString(ctx, "quota_layer"),
		QuotaLimit: func() *int {
			if _, ok := ctx.Get("quota_limit"); !ok {
				return nil
			}
			limit := getInt(ctx, "quota_limit")
			return &limit
		}(),
		DurationMs: func() int64 {
			if startTime, ok := ctx.Get("start_time"); ok {
				if t, ok := startTime.(time.Time); ok {
					return now.Sub(t).Milliseconds()
				}
			}
			return 0
		}(),
	}

	// 首 token 延迟
	if ftms, ok := ctx.Get("first_token_ms"); ok {
		if v, ok := ftms.(int); ok && v > 0 {
			event.FirstTokenMs = &v
		}
	}

	// TTFB（仅流式）：上游请求发出到收到响应头
	if ttfb, ok := ctx.Get("t_ttfb_ms"); ok {
		if v, ok := ttfb.(int); ok && v > 0 {
			event.StageTTFBMs = &v
		}
	}

	// 各阶段耗时（始终采集，不受 log_enabled 影响）
	if v, ok := ctx.Get("t_auth_ms"); ok {
		if ms, ok := v.(int16); ok {
			event.StageAuthMs = &ms
		}
	}
	if v, ok := ctx.Get("t_route_ms"); ok {
		if ms, ok := v.(int16); ok {
			event.StageRouteMs = &ms
		}
	}
	if v, ok := ctx.Get("t_upstream_ms"); ok {
		if ms, ok := v.(int16); ok {
			event.StageUpstreamMs = &ms
		}
	}

	// 优先从 usage 对象读取（流式回填后写入）
	if usage, ok := ctx.Get("usage"); ok {
		if u, ok := usage.(core.Usage); ok {
			ci := ExtractCostInput(u)
			event.PromptTokens = ci.PromptTokens
			event.CachedTokens = ci.CachedTokens
			event.CacheWriteTokens = ci.CacheWriteTokens
			event.CompletionTokens = ci.CompletionTokens
			if u.CompletionTokensDetails != nil {
				event.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
			}
			event.TotalTokens = u.TotalTokens
			event.UsageMissing = false
			event.SearchUnits = func() float64 {
				if v, ok := ctx.Get("search_units"); ok {
					if su, ok := v.(float64); ok {
						return su
					}
				}
				return 1.0 // 默认 1 search_unit
			}()
		}
	}

	// 回退：从分开的 context key 读取（非流式调用 orchestrator 写入）
	if event.PromptTokens == 0 && event.CompletionTokens == 0 {
		event.PromptTokens = getInt(ctx, "prompt_tokens")
		event.CompletionTokens = getInt(ctx, "completion_tokens")
		event.TotalTokens = event.PromptTokens + event.CompletionTokens
		if event.PromptTokens > 0 || event.CompletionTokens > 0 {
			event.UsageMissing = false
		} else {
			event.UsageMissing = true
		}
	}

	// stream 标记
	if b, ok := ctx.Get("stream"); ok {
		event.Stream, _ = b.(bool)
	}

	// 媒体用量（图像/视频链路在终态写入；未写则保持零值 → 走 token 口径）
	if v, ok := ctx.Get(CtxKeyMediaUsage); ok {
		if media, ok := v.(core.MediaCostInput); ok {
			event.Media = media
		}
	}

	// failover 标记
	if b, ok := ctx.Get("failover_used"); ok {
		event.FailoverUsed, _ = b.(bool)
	}

	logLargePromptCacheMiss(event)

	// 成本计算
	event.EstimateCost(c.pricing)
	if tier, ok := budget.GetTierContext(ctx); ok {
		event.BudgetTierID = tier.TierID
		event.BudgetTierNameSnapshot = tier.TierName
		event.BudgetTierSnapshotRevision = tier.SnapshotRevision
		event.BudgetTierAccountingEligible = ctx.GetBool(budget.CtxKeyTierAccountingEligible)
	}
	if plan, ok := budget.GetAccountingPlan(ctx); ok && plan != nil {
		for _, constraint := range plan.Constraints {
			if constraint.Dimension != budget.DimensionTier {
				continue
			}
			event.BudgetTierConstraintsSnapshot = append(
				event.BudgetTierConstraintsSnapshot,
				obsmodel.BudgetTierConstraintSnapshot{
					RedisKey: constraint.Key, ScopeType: constraint.ScopeType, ScopeID: constraint.ScopeID,
					TierID: constraint.TierID, PeriodType: string(constraint.PeriodType),
					Period: constraint.Period, ResetAt: constraint.ResetAt,
				},
			)
		}
	}
	if block, ok := budget.GetBlockContext(ctx); ok {
		event.BudgetBlockCode = stringPtr(block.Code)
		event.BudgetBlockDimension = stringPtr(string(block.Dimension))
		event.BudgetBlockScopeType = stringPtr(block.ScopeType)
		event.BudgetBlockScopeID = stringPtr(block.ScopeID)
		event.BudgetBlockPeriodType = stringPtr(string(block.PeriodType))
	}

	return event
}

func logLargePromptCacheMiss(event UsageEvent) {
	if event.UsageMissing || event.Status >= 400 {
		return
	}
	if event.PromptTokens < largePromptCacheMissThreshold || event.CachedTokens > 0 || event.CacheWriteTokens > 0 {
		return
	}
	slog.Warn("large prompt cache miss observed",
		"request_id", event.RequestID,
		"model", event.Model,
		"routed_model", event.RoutedModel,
		"group_name", event.GroupName,
		"provider", event.Provider,
		"prompt_tokens", event.PromptTokens,
		"cached_tokens", event.CachedTokens,
		"cache_write_tokens", event.CacheWriteTokens,
		"completion_tokens", event.CompletionTokens,
		"total_tokens", event.TotalTokens,
		"stream", event.Stream,
		"status", event.Status,
		"failover_used", event.FailoverUsed,
		"inbound_endpoint", event.InboundEndpoint,
	)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}

// Emit 在队列满时先有界等待，再同步落库，避免静默丢失审计事实。
func (c *Collector) Emit(e UsageEvent) {
	if c == nil {
		return
	}
	if c.streamFirst {
		c.emitStreamFirst(e)
		return
	}
	if c.writer == nil {
		ctx := context.Background()
		if c.deliveryFailureCounter != nil {
			c.deliveryFailureCounter.Add(ctx, 1)
		}
		slog.Error("call log collector has no writer",
			"request_id", e.RequestID,
			"stream_first", false,
		)
		return
	}
	timer := time.NewTimer(c.cfg.EnqueueTimeout)
	defer timer.Stop()
	select {
	case c.ch <- e:
		return
	case <-timer.C:
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.SyncFallbackTimeout)
	defer cancel()
	if c.syncFallbackCounter != nil {
		c.syncFallbackCounter.Add(ctx, 1)
	}
	callLog, err := e.ToCallLog()
	if err == nil {
		SanitizeCallLogForWrite(&callLog)
		err = c.writer.persist(ctx, []obsmodel.CallLog{callLog})
	}
	if err != nil {
		if c.deliveryFailureCounter != nil {
			c.deliveryFailureCounter.Add(ctx, 1)
		}
		slog.ErrorContext(ctx, "call log synchronous fallback failed",
			"request_id", e.RequestID,
			"queue_capacity", c.cfg.BufferSize,
			"error", err,
		)
		return
	}
	slog.WarnContext(ctx, "call log queue saturated; synchronous fallback persisted event",
		"request_id", e.RequestID,
		"queue_capacity", c.cfg.BufferSize,
	)
}

func (c *Collector) emitStreamFirst(e UsageEvent) {
	if e.BillableReservation != nil {
		defer func() {
			if err := e.BillableReservation.Finish(context.Background()); err != nil {
				if c.deliveryFailureCounter != nil {
					c.deliveryFailureCounter.Add(context.Background(), 1)
				}
				slog.Error("release billable reservation after delivery",
					"request_id", e.RequestID, "error", err)
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.SyncFallbackTimeout)
	defer cancel()
	callLog, err := e.ToCallLog()
	validCallLog := err == nil
	if err == nil {
		SanitizeCallLogForWrite(&callLog)
		if e.BudgetAdmissionFailOpen && c.streamSink != nil && c.streamSink.Available() {
			pgCtx, pgCancel := context.WithTimeout(context.Background(), c.cfg.SyncFallbackTimeout)
			pgErr := retryCallLogInsert(
				pgCtx, c.streamSink, []obsmodel.CallLog{callLog}, c.cfg.DBRetryCount, c.cfg.DBRetryBackoff,
			)
			pgCancel()
			if pgErr == nil {
				return
			}
			slog.Error("admission fail-open synchronous postgres write failed; trying Redis spool",
				"request_id", e.RequestID, "error", pgErr)
		}
		if c.spool == nil {
			err = errors.New("Redis call-log spool is not configured")
		} else {
			if e.BillableReserved {
				if e.PendingBudgetAccount != nil {
					var newlyAccounted bool
					newlyAccounted, err = c.spool.StoreReservedWithAccounting(
						ctx, callLog, e.PendingBudgetAccount, e.BillableReservation,
					)
					if err == nil && newlyAccounted && e.PendingBudgetAccount.OnAccounted != nil {
						e.PendingBudgetAccount.OnAccounted(ctx)
					}
					if err != nil && c.reconciliationRequired != nil {
						c.reconciliationRequired.Add(ctx, 1)
					}
				} else {
					err = c.spool.StoreReserved(ctx, callLog)
				}
			} else {
				err = c.spool.Store(ctx, []obsmodel.CallLog{callLog})
			}
		}
	}
	if err == nil {
		if c.spool != nil {
			c.refreshSpoolHealth(ctx, c.spool.rdb)
		}
		return
	}
	slog.ErrorContext(ctx, "call log stream-first append failed",
		"request_id", e.RequestID,
		"stream", CallLogSpoolStream,
		"error", err,
	)
	if validCallLog && c.streamSink != nil && c.streamSink.Available() {
		fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), c.cfg.SyncFallbackTimeout)
		defer fallbackCancel()
		if fallbackErr := retryCallLogInsert(
			fallbackCtx, c.streamSink, []obsmodel.CallLog{callLog}, c.cfg.DBRetryCount, c.cfg.DBRetryBackoff,
		); fallbackErr == nil {
			return
		} else {
			err = fmt.Errorf("redis append failed (%v), synchronous postgres fallback failed: %w", err, fallbackErr)
		}
	}
	if validCallLog && e.PendingBudgetAccount != nil && c.spool != nil {
		logOnlyCtx, logOnlyCancel := context.WithTimeout(context.Background(), c.cfg.SyncFallbackTimeout)
		logOnlyErr := c.spool.StoreReserved(logOnlyCtx, callLog)
		logOnlyCancel()
		if logOnlyErr == nil {
			slog.Warn("budget reconciliation required; persisted call log without counter update",
				"request_id", e.RequestID)
			return
		}
		err = fmt.Errorf("%v; reconciliation call-log spool failed: %w", err, logOnlyErr)
	}
	if c.deliveryFailureCounter != nil {
		c.deliveryFailureCounter.Add(ctx, 1)
	}
	slog.ErrorContext(ctx, "call log event could not be durably persisted",
		"request_id", e.RequestID,
		"postgres_connected", c.streamSink != nil && c.streamSink.Available(),
		"error", err,
	)
}

func retryCallLogInsert(
	ctx context.Context,
	sink callLogBatchSink,
	batch []obsmodel.CallLog,
	retries int,
	backoff time.Duration,
) error {
	if retries < 0 {
		retries = 0
	}
	if backoff <= 0 {
		backoff = 50 * time.Millisecond
	}
	var err error
	for attempt := 0; attempt <= retries; attempt++ {
		if err = sink.BatchInsert(ctx, batch); err == nil {
			return nil
		}
		if attempt == retries {
			break
		}
		timer := time.NewTimer(backoff << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			return context.Cause(ctx)
		case <-timer.C:
		}
	}
	return err
}

func getString(ctx *gin.Context, key string) string {
	if v, ok := ctx.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func normalizeKeyType(keyType string) string {
	if keyType == obsmodel.KeyTypeService {
		return obsmodel.KeyTypeService
	}
	return obsmodel.KeyTypePersonal
}

// SanitizeCallLogForWrite 写入前规范化：Service Key 调用不归因到具体用户。
func SanitizeCallLogForWrite(cl *obsmodel.CallLog) {
	if normalizeKeyType(cl.KeyType) == obsmodel.KeyTypeService {
		cl.UserID = ""
		cl.UserName = ""
	}
}

func getInt(ctx *gin.Context, key string) int {
	if v, ok := ctx.Get(key); ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

// EstimateCost 为 UsageEvent 计算成本（锁价）
func (e *UsageEvent) EstimateCost(pricing PricingResolver) {
	if pricing == nil {
		return
	}
	modelKey := e.RoutedModel
	if modelKey == "" {
		modelKey = e.Model
	}
	var p core.ModelPricing
	var ok bool
	if providerResolver, providerAware := pricing.(ProviderPricingResolver); providerAware {
		p, ok = providerResolver.ResolveForProvider(modelKey, e.Provider)
	} else {
		p, ok = pricing.Resolve(modelKey)
	}
	if !ok {
		e.UsageMissing = true
		return
	}

	// Rerank 成本计算路径：按 search_units 计费
	if e.SearchUnits > 0 && p.RerankPerSearchUnit != nil {
		unitPrice := *p.RerankPerSearchUnit
		e.EstCostUSD = e.SearchUnits * unitPrice
		e.InputPriceSnapshot = unitPrice
		e.OutputPriceSnapshot = 0
		e.InputCostUSD = e.EstCostUSD
		e.OutputCostUSD = 0
		return
	}

	// 媒体成本计算路径：按张 / 秒 / 次计费，与 token 口径互斥优先。
	if e.Media.HasUsage() {
		if inputCost, outputCost, missing := estimateMediaCostBuckets(e.Media, &p); !missing {
			e.EstCostUSD = inputCost + outputCost
			e.InputCostUSD = inputCost
			e.OutputCostUSD = outputCost
			// 媒体请求本来就没有 token 用量，NewUsageEvent 的 token 兜底会把它标成
			// usage_missing；这里已经有真实媒体用量与单价，必须纠正回来。
			e.UsageMissing = false
			return
		}
		// 媒体口径无价（含单价为 0/NULL）时**绝不静默记 0 元**：
		// 只有「token 价确实为正 + 本次确实有 token 用量」才允许回退 token 口径，
		// 否则一律标记 usage_missing，交给准入层的无价策略处理。
		if !hasPositiveTokenPrice(&p) || !hasTokenUsage(e) {
			e.UsageMissing = true
			return
		}
	}

	in := core.CostInput{
		PromptTokens:     e.PromptTokens,
		CachedTokens:     e.CachedTokens,
		CacheWriteTokens: e.CacheWriteTokens,
		CompletionTokens: e.CompletionTokens,
	}
	cost, missing := EstimateCost(in, &p)
	if missing {
		e.UsageMissing = true
		return
	}
	e.EstCostUSD = cost

	// 锁价快照
	if p.InputPerMtok != nil {
		e.InputPriceSnapshot = *p.InputPerMtok
	}
	if p.OutputPerMtok != nil {
		e.OutputPriceSnapshot = *p.OutputPerMtok
	}

	// 分项成本（供前端成本分析展示）
	if p.InputPerMtok != nil && p.OutputPerMtok != nil {
		uncached := e.PromptTokens - e.CachedTokens - e.CacheWriteTokens
		if uncached < 0 {
			uncached = 0
		}
		e.InputCostUSD = perM(uncached, *p.InputPerMtok)
		e.OutputCostUSD = perM(e.CompletionTokens, *p.OutputPerMtok)
		if e.CachedTokens > 0 && p.CachedInputPerMtok != nil {
			e.CacheReadCostUSD = perM(e.CachedTokens, *p.CachedInputPerMtok)
		} else if e.CachedTokens > 0 {
			e.CacheReadCostUSD = perM(e.CachedTokens, *p.InputPerMtok)
		}
	}
}

// hasPositiveTokenPrice 判定模型是否配了正 token 价。
// 只看指针非 nil 是不够的：catalog 里 token 价列是 0 占位（如 dall-e-3 的
// input_price_per_million = 0），当成有价会重新引入静默 0 元。
func hasPositiveTokenPrice(p *core.ModelPricing) bool {
	if p == nil {
		return false
	}
	if p.InputPerMtok != nil && *p.InputPerMtok > 0 {
		return true
	}
	if p.OutputPerMtok != nil && *p.OutputPerMtok > 0 {
		return true
	}
	return false
}

// hasTokenUsage 本次请求是否真的带有 token 用量。
func hasTokenUsage(e *UsageEvent) bool {
	return e.PromptTokens > 0 || e.CachedTokens > 0 || e.CacheWriteTokens > 0 || e.CompletionTokens > 0
}

// BatchWriter 批量写入器
type BatchWriter struct {
	repo                 *repository.CallLogRepository
	db                   *gorm.DB // 用于落库前补查缺失的用户/部门字段
	flushN               int
	interval             time.Duration
	retryCount           int
	retryBackoff         time.Duration
	spool                *CallLogSpool
	retryCounter         metric.Int64Counter
	unrecoverableCounter metric.Int64Counter
}

// Run 消费 channel，按条件批量写入
func (w *BatchWriter) Run(ctx context.Context, ch <-chan UsageEvent) {
	batch := make([]model.CallLog, 0, w.flushN)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	flush := func(b []model.CallLog) {
		// 过滤系统探针记录（__system_ 前缀为内部保留命名空间）
		filtered := b[:0]
		for i := range b {
			if len(b[i].KeyName) >= 9 && b[i].KeyName[:9] == "__system_" {
				continue
			}
			filtered = append(filtered, b[i])
		}
		b = filtered
		if len(b) == 0 {
			return
		}
		if err := w.persist(ctx, b); err != nil {
			log.Printf("[observability] call log persistence exhausted: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
		drain:
			for {
				select {
				case e := <-ch:
					batch = append(batch, eventToCallLog(e))
				default:
					break drain
				}
			}
			if len(batch) > 0 {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				if err := w.persist(shutdownCtx, batch); err != nil {
					slog.ErrorContext(shutdownCtx, "call log shutdown drain failed",
						"batch_size", len(batch),
						"error", err,
					)
				}
				cancel()
			}
			if w.spool != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				if err := w.spool.ReplayOnce(shutdownCtx, w.repo); err != nil {
					slog.ErrorContext(shutdownCtx, "call log spool shutdown replay failed", "error", err)
				}
				cancel()
			}
			return
		case e := <-ch:
			batch = append(batch, eventToCallLog(e))
			if len(batch) >= w.flushN {
				flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *BatchWriter) persist(ctx context.Context, batch []model.CallLog) error {
	if len(batch) == 0 {
		return nil
	}
	if w.db != nil {
		w.fillMissingIdentity(ctx, batch)
	}
	retries := w.retryCount
	if retries < 0 {
		retries = 0
	}
	backoff := w.retryBackoff
	if backoff <= 0 {
		backoff = 50 * time.Millisecond
	}
	var dbErr error
	for attempt := 0; attempt <= retries; attempt++ {
		dbErr = w.repo.BatchInsert(ctx, batch)
		if dbErr == nil {
			return nil
		}
		if attempt == retries {
			break
		}
		if w.retryCounter != nil {
			w.retryCounter.Add(ctx, 1)
		}
		timer := time.NewTimer(backoff << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			dbErr = context.Cause(ctx)
			attempt = retries
		case <-timer.C:
		}
	}
	if w.spool != nil {
		if err := w.spool.Store(ctx, batch); err == nil {
			slog.WarnContext(ctx, "call log DB write failed; persisted to Redis Stream",
				"stream", CallLogSpoolStream,
				"batch_size", len(batch),
				"error", dbErr,
			)
			return nil
		} else {
			dbErr = fmt.Errorf("db write failed (%v), spool write failed: %w", dbErr, err)
		}
	}
	if w.unrecoverableCounter != nil {
		w.unrecoverableCounter.Add(ctx, int64(len(batch)))
	}
	slog.ErrorContext(ctx, "call log persistence failed after synchronous retries",
		"batch_size", len(batch),
		"redis_spool_configured", w.spool != nil,
		"error", dbErr,
	)
	return dbErr
}

// fillMissingIdentity 对 batch 中 org 字段缺失的记录，批量查询 api_keys 补全。
// 每批最多额外一条 IN 查询，不影响正常调用链路。
func (w *BatchWriter) fillMissingIdentity(ctx context.Context, batch []model.CallLog) {
	// 收集需要补查的 key_id（去重）：任意 org 字段为空均触发
	needed := make(map[string]struct{})
	for i := range batch {
		if (batch[i].UserName == "" || batch[i].DeptID == "" || batch[i].DeptName == "" || batch[i].StudioID == "" || batch[i].KeyType == "") && batch[i].KeyID != "" {
			needed[batch[i].KeyID] = struct{}{}
		}
	}
	if len(needed) == 0 {
		return
	}

	keyIDs := make([]string, 0, len(needed))
	for id := range needed {
		keyIDs = append(keyIDs, id)
	}

	type keyInfo struct {
		ID         string `gorm:"column:id"`
		KeyName    string `gorm:"column:key_name"`
		KeyType    string `gorm:"column:key_type"`
		UserName   string `gorm:"column:user_name"`
		DeptID     string `gorm:"column:dept_id"`
		DeptName   string `gorm:"column:dept_name"`
		StudioID   string `gorm:"column:studio_id"`
		StudioName string `gorm:"column:studio_name"`
	}
	var rows []keyInfo
	// 从 api_keys 直接读冗余字段，若仍为空则 JOIN users/departments/dept_studio_map 补一次；
	// OIDC users 找不到时再从 dev_users（本地账号）兜底取 username。
	if err := w.db.WithContext(ctx).Raw(`
		SELECT ak.id::text, ak.name AS key_name,
		       COALESCE(NULLIF(ak.key_type, ''), 'personal') AS key_type,
		       COALESCE(
		           NULLIF(ak.user_name,''),
		           u.name,
		           du.username,
		           ''
		       ) AS user_name,
		       COALESCE(NULLIF(ak.dept_id,''), d.dept_id, '') AS dept_id,
		       COALESCE(NULLIF(ak.dept_name,''), d.dept_name, '') AS dept_name,
		       COALESCE(NULLIF(ak.studio_id,''), dsm.studio_id::text, '') AS studio_id,
		       COALESCE(NULLIF(ak.studio_name,''), s.name, '') AS studio_name
		FROM api_keys ak
		LEFT JOIN users u ON u.id = ak.user_id
		LEFT JOIN departments d ON d.id = u.dept_id
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = dsm.studio_id
		LEFT JOIN dev_users du ON du.id = ak.user_id
		WHERE ak.id::text IN ?`, keyIDs).Scan(&rows).Error; err != nil {
		log.Printf("[observability] fillMissingIdentity query error: %v", err)
		return
	}

	infoMap := make(map[string]keyInfo, len(rows))
	for _, r := range rows {
		infoMap[r.ID] = r
	}

	for i := range batch {
		if info, ok := infoMap[batch[i].KeyID]; ok {
			if batch[i].KeyType == "" {
				batch[i].KeyType = normalizeKeyType(info.KeyType)
			}
			if normalizeKeyType(batch[i].KeyType) == obsmodel.KeyTypeService {
				batch[i].UserID = ""
				batch[i].UserName = ""
			} else if batch[i].UserName == "" {
				batch[i].UserName = info.UserName
			}
			if batch[i].DeptID == "" {
				batch[i].DeptID = info.DeptID
			}
			if batch[i].DeptName == "" {
				batch[i].DeptName = info.DeptName
			}
			if batch[i].StudioID == "" {
				batch[i].StudioID = info.StudioID
			}
			if batch[i].StudioName == "" {
				batch[i].StudioName = info.StudioName
			}
			if batch[i].KeyName == "" {
				batch[i].KeyName = info.KeyName
			}
		}
		SanitizeCallLogForWrite(&batch[i])
	}
}

func eventToCallLog(e UsageEvent) model.CallLog {
	cl, err := e.ToCallLog()
	if err != nil {
		log.Printf("[observability] invalid usage event: %v", err)
		return model.CallLog{RequestID: e.RequestID, TS: e.Timestamp}
	}
	SanitizeCallLogForWrite(&cl)
	return cl
}
