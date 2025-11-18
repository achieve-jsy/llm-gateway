package observability

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/observability/model"
	"github.com/example/llm-gateway/internal/gateway/observability/repository"
)

// Collector 埋点采集器
// 在 Logger 中间件 c.Next() 后调用，构造 UsageEvent 并异步投递
type Collector struct {
	ch      chan UsageEvent
	writer  *BatchWriter
	pricing PricingResolver
	cfg     CollectorConfig
}

// PricingResolver 定价查询接口
type PricingResolver interface {
	Resolve(modelKey string) (core.ModelPricing, bool)
}

// NewCollector 创建 Collector
func NewCollector(cfg CollectorConfig, pricing PricingResolver) *Collector {
	return &Collector{
		ch:      make(chan UsageEvent, cfg.BufferSize),
		pricing: pricing,
		cfg:     cfg,
	}
}

// StartWriter 启动批量写入 goroutine
func (c *Collector) StartWriter(ctx context.Context, repo *repository.CallLogRepository) {
	c.writer = &BatchWriter{
		repo:     repo,
		flushN:   c.cfg.FlushN,
		interval: c.cfg.FlushInterval,
	}
	go c.writer.Run(ctx, c.ch)
}

// NewUsageEvent 从 gin.Context 提取字段构造 UsageEvent
func (c *Collector) NewUsageEvent(ctx *gin.Context) UsageEvent {
	now := time.Now()
	event := UsageEvent{
		RequestID:       getString(ctx, "request_id"),
		Timestamp:       now,
		KeyID:           getString(ctx, "key_id"),
		UserID:          getString(ctx, "user_id"),
		UserName:        getString(ctx, "user_name"),
		DeptID:          getString(ctx, "dept_id"),
		DeptName:        getString(ctx, "dept_name"),
		StudioID:        getString(ctx, "studio_id"),
		StudioName:      getString(ctx, "studio_name"),
		Model:           getString(ctx, "model"),
		RoutedModel:     getString(ctx, "routed_model"),
		GroupName:       getString(ctx, "group_name"),
		Provider:        getString(ctx, "provider"),
		ReasoningEffort: getString(ctx, "reasoning_effort"),
		UserAgent:       ctx.Request.UserAgent(),
		IPAddress:       ctx.ClientIP(),
		Status:          getInt(ctx, "status"),
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

	// 成本计算
	event.EstimateCost(c.pricing)

	return event
}

// Emit 非阻塞投递事件到 channel
func (c *Collector) Emit(e UsageEvent) {
	select {
	case c.ch <- e:
	default:
		// 队列满则丢弃并计数
		log.Printf("[observability] usage event dropped, queue full (capacity=%d)", c.cfg.BufferSize)
	}
}

func getString(ctx *gin.Context, key string) string {
	if v, ok := ctx.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
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
	p, ok := pricing.Resolve(modelKey)
	if !ok {
		e.UsageMissing = true
		return
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

// BatchWriter 批量写入器
type BatchWriter struct {
	repo     *repository.CallLogRepository
	flushN   int
	interval time.Duration
}

// Run 消费 channel，按条件批量写入
func (w *BatchWriter) Run(ctx context.Context, ch <-chan UsageEvent) {
	batch := make([]model.CallLog, 0, w.flushN)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 退出前写入剩余批次
			if len(batch) > 0 {
				_ = w.repo.BatchInsert(context.Background(), batch)
			}
			return
		case e := <-ch:
			batch = append(batch, eventToCallLog(e))
			if len(batch) >= w.flushN {
				if err := w.repo.BatchInsert(ctx, batch); err != nil {
					log.Printf("[observability] batch insert error: %v", err)
				}
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				if err := w.repo.BatchInsert(ctx, batch); err != nil {
					log.Printf("[observability] batch insert error: %v", err)
				}
				batch = batch[:0]
			}
		}
	}
}

func eventToCallLog(e UsageEvent) model.CallLog {
	cl := model.CallLog{
		RequestID:           e.RequestID,
		TS:                  e.Timestamp,
		KeyID:               e.KeyID,
		UserID:              e.UserID,
		UserName:            e.UserName,
		DeptID:              e.DeptID,
		DeptName:            e.DeptName,
		StudioID:            e.StudioID,
		StudioName:          e.StudioName,
		Model:               e.Model,
		RoutedModel:         e.RoutedModel,
		GroupName:           e.GroupName,
		Provider:            e.Provider,
		PromptTokens:        e.PromptTokens,
		CachedTokens:        e.CachedTokens,
		CacheWriteTokens:    e.CacheWriteTokens,
		CompletionTokens:    e.CompletionTokens,
		ReasoningTokens:     e.ReasoningTokens,
		TotalTokens:         e.TotalTokens,
		Status:              e.Status,
		UpstreamStatus:      e.UpstreamStatus,
		DurationMs:          int(e.DurationMs),
		FirstTokenMs:        e.FirstTokenMs,
		Stream:              e.Stream,
		FailoverUsed:        e.FailoverUsed,
		UsageMissing:        e.UsageMissing,
		ReasoningEffort:     e.ReasoningEffort,
		UserAgent:           e.UserAgent,
		IPAddress:           e.IPAddress,
		InputPriceSnapshot:  &e.InputPriceSnapshot,
		OutputPriceSnapshot: &e.OutputPriceSnapshot,
		InputCostUSD:        e.InputCostUSD,
		OutputCostUSD:       e.OutputCostUSD,
		CacheReadCostUSD:    e.CacheReadCostUSD,
		EstCostUSD:          e.EstCostUSD,
	}
	return cl
}