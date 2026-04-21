package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The following small compatibility surface keeps pre-migration users of the
// original infra batch writer working while the active gateway uses the
// observability package.
type UsageEvent struct {
	RequestID          string
	KeyID              string
	UserID             string
	DeptID             *string
	StudioID           *string
	Model              string
	ProviderType       string
	ProviderModelID    *string
	IsStream           bool
	SessionID          *string
	StatDate           time.Time
	PromptTokens       int
	CompletionTokens   int
	ReasoningTokens    int
	CachedTokens       int
	TTFTMs             *int
	DurationMs         *int
	ProviderDurationMs *int
	Status             string
	UsageMissing       bool
}

type ModelPricing struct {
	PromptPrice     float64
	CompletionPrice float64
	ReasoningPrice  *float64
	CachedPrice     *float64
	PricingMissing  bool
}

type TokenBuckets struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
}

type CostBuckets struct {
	PromptCost        float64
	CompletionCost    float64
	ReasoningCost     float64
	CachedCost        float64
	TotalCost         float64
	FallbackReasoning bool
}

func EstimateCost(tokens TokenBuckets, pricing ModelPricing) CostBuckets {
	if pricing.PromptPrice <= 0 || pricing.CompletionPrice <= 0 {
		return CostBuckets{}
	}
	const million = 1_000_000.0
	result := CostBuckets{
		PromptCost:     float64(tokens.PromptTokens) * pricing.PromptPrice / million,
		CompletionCost: float64(tokens.CompletionTokens) * pricing.CompletionPrice / million,
	}
	reasoningPrice := pricing.CompletionPrice
	if pricing.ReasoningPrice != nil {
		reasoningPrice = *pricing.ReasoningPrice
	} else {
		result.FallbackReasoning = true
	}
	result.ReasoningCost = float64(tokens.ReasoningTokens) * reasoningPrice / million
	if pricing.CachedPrice != nil {
		result.CachedCost = float64(tokens.CachedTokens) * *pricing.CachedPrice / million
	}
	result.TotalCost = result.PromptCost + result.CompletionCost + result.ReasoningCost + result.CachedCost
	return result
}

type legacyCost struct {
	prompt, completion, reasoning, cached, total float64
}

type metricCounter struct{ value atomic.Int64 }

func (c *metricCounter) Inc()         { c.value.Add(1) }
func (c *metricCounter) Add(v int64)  { c.value.Add(v) }
func (c *metricCounter) Value() int64 { return c.value.Load() }

type legacyUsageMetrics struct {
	BatchWriteErrors        metricCounter
	Written                 metricCounter
	PricingCacheRefreshErrs metricCounter
}

func (m *legacyUsageMetrics) Reset() {
	m.BatchWriteErrors = metricCounter{}
	m.Written = metricCounter{}
	m.PricingCacheRefreshErrs = metricCounter{}
}

var UsageMetricsInstance legacyUsageMetrics

type MemoryPricingCache struct {
	cache  sync.Map
	url    string
	token  string
	client *http.Client
	cancel context.CancelFunc
}

func NewMemoryPricingCache(url, token string) *MemoryPricingCache {
	return &MemoryPricingCache{url: url, token: token, client: &http.Client{Timeout: 30 * time.Second}}
}

func (c *MemoryPricingCache) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.refresh(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.refresh(ctx)
			}
		}
	}()
}

func (c *MemoryPricingCache) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *MemoryPricingCache) Get(model string) (ModelPricing, bool) {
	value, ok := c.cache.Load(model)
	if !ok {
		return ModelPricing{}, false
	}
	return value.(ModelPricing), true
}

func (c *MemoryPricingCache) refresh(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/internal/v1/pricing", nil)
	if err != nil {
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}
	var payload struct {
		Models map[string]ModelPricing `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		UsageMetricsInstance.PricingCacheRefreshErrs.Inc()
		return
	}
	for model, pricing := range payload.Models {
		c.cache.Store(model, pricing)
	}
}

type BatchWriter struct {
	ch       <-chan UsageEvent
	db       *gorm.DB
	pricing  sync.Map
	stop     context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
}

func NewBatchWriter(ch <-chan UsageEvent, db *gorm.DB) *BatchWriter {
	return &BatchWriter{ch: ch, db: db, done: make(chan struct{})}
}

func (w *BatchWriter) SetPricing(model string, pricing ModelPricing) {
	w.pricing.Store(model, pricing)
}

func (w *BatchWriter) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	w.stop = cancel
	go w.run(ctx)
}

func (w *BatchWriter) Stop() {
	w.stopOnce.Do(func() {
		if w.stop != nil {
			w.stop()
		}
		<-w.done
	})
}

func (w *BatchWriter) run(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	batch := make([]UsageEvent, 0, 200)
	flush := func() {
		if len(batch) > 0 {
			w.flush(batch)
			batch = batch[:0]
		}
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case event, ok := <-w.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, event)
			if len(batch) >= 200 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (w *BatchWriter) flush(events []UsageEvent) {
	for _, event := range events {
		cost, pricingMissing := w.cost(event)
		row := map[string]any{
			"request_id": event.RequestID, "key_id": event.KeyID, "user_id": event.UserID,
			"dept_id": event.DeptID, "studio_id": event.StudioID, "model": event.Model,
			"stat_date": event.StatDate, "prompt_tokens": event.PromptTokens,
			"completion_tokens": event.CompletionTokens, "reasoning_tokens": event.ReasoningTokens,
			"cached_tokens": event.CachedTokens, "ttft_ms": event.TTFTMs,
			"duration_ms": event.DurationMs, "provider_duration_ms": event.ProviderDurationMs,
			"provider_type": event.ProviderType, "provider_model_id": event.ProviderModelID,
			"is_stream": event.IsStream, "session_id": event.SessionID, "status": event.Status,
			"usage_missing": event.UsageMissing, "prompt_est_cost": cost.prompt,
			"completion_est_cost": cost.completion, "reasoning_est_cost": cost.reasoning,
			"cached_est_cost": cost.cached, "total_est_cost": cost.total,
			"pricing_missing": pricingMissing,
		}
		err := w.db.Table("call_logs").Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
		if err != nil {
			UsageMetricsInstance.BatchWriteErrors.Inc()
			continue
		}
		UsageMetricsInstance.Written.Inc()
	}
}

func (w *BatchWriter) cost(event UsageEvent) (legacyCost, bool) {
	raw, ok := w.pricing.Load(event.Model)
	if !ok {
		return legacyCost{}, true
	}
	pricing := raw.(ModelPricing)
	const million = 1_000_000.0
	cost := legacyCost{
		prompt:     float64(event.PromptTokens) * pricing.PromptPrice / million,
		completion: float64(event.CompletionTokens) * pricing.CompletionPrice / million,
	}
	reasoningPrice := pricing.CompletionPrice
	if pricing.ReasoningPrice != nil {
		reasoningPrice = *pricing.ReasoningPrice
	}
	cost.reasoning = float64(event.ReasoningTokens) * reasoningPrice / million
	if pricing.CachedPrice != nil {
		cost.cached = float64(event.CachedTokens) * *pricing.CachedPrice / million
	}
	cost.total = cost.prompt + cost.completion + cost.reasoning + cost.cached
	return cost, pricing.PricingMissing
}
