package observability

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/core"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
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
		db:       repo.DB(),
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
		KeyName:         getString(ctx, "key_name"),
		KeyType:         normalizeKeyType(getString(ctx, "key_type")),
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
		RoutedViaGroup: func() bool {
			v, ok := ctx.Get("routed_via_group")
			if !ok {
				return false
			}
			b, _ := v.(bool)
			return b
		}(),
		InboundEndpoint: ctx.Request.URL.Path,
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

	// failover 标记
	if b, ok := ctx.Get("failover_used"); ok {
		event.FailoverUsed, _ = b.(bool)
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
	p, ok := pricing.Resolve(modelKey)
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
	db       *gorm.DB // 用于落库前补查缺失的用户/部门字段
	flushN   int
	interval time.Duration
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
		if w.db != nil {
			w.fillMissingIdentity(ctx, b)
		}
		if err := w.repo.BatchInsert(ctx, b); err != nil {
			log.Printf("[observability] batch insert error: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			if len(batch) > 0 {
				flush(batch)
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
	cl := model.CallLog{
		RequestID:           e.RequestID,
		TS:                  e.Timestamp,
		KeyID:               e.KeyID,
		KeyName:             e.KeyName,
		KeyType:             normalizeKeyType(e.KeyType),
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
		StageTTFBMs:         e.StageTTFBMs,
		StageAuthMs:         e.StageAuthMs,
		StageRouteMs:        e.StageRouteMs,
		StageUpstreamMs:     e.StageUpstreamMs,
		Stream:              e.Stream,
		FailoverUsed:        e.FailoverUsed,
		RoutedViaGroup:      e.RoutedViaGroup,
		UsageMissing:        e.UsageMissing,
		ReasoningEffort:     e.ReasoningEffort,
		UserAgent:           e.UserAgent,
		IPAddress:           e.IPAddress,
			InboundEndpoint:     e.InboundEndpoint,
		InputPriceSnapshot:  &e.InputPriceSnapshot,
		OutputPriceSnapshot: &e.OutputPriceSnapshot,
		InputCostUSD:        e.InputCostUSD,
		OutputCostUSD:       e.OutputCostUSD,
		CacheReadCostUSD:    e.CacheReadCostUSD,
		EstCostUSD:          e.EstCostUSD,
	}
	SanitizeCallLogForWrite(&cl)
	return cl
}