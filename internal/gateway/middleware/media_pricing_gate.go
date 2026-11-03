package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/example/llm-gateway/internal/core"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// 本文件是「媒体准入计价闸门」（A2）：媒体请求（图像 / 视频）在**调用上游之前**
// 必须确认「这个模型到底有没有价」，避免出现设计文档 §8.2 说的「静默 0 元」。
//
// 设计依据：
//   - docs/media-channels-integration-design.md §8.3（无价策略 D3）
//   - docs/media-channels-implementation-plan.md Batch 0 · A2 行
//   - tests/contract/contracts.json（媒体对外错误码约定：402 media_pricing_missing）
//
// 三种结果：
//
//	有价            → 放行（什么都不做）
//	无价 + 开关开   → 402 media_pricing_missing + 计数（默认；MEDIA_REQUIRE_PRICING=true）
//	无价 + 开关关   → 放行，但 gin context 打 CtxKeyMediaPricingMissing 标记
//	                  （采集侧据此把 usage_missing 置 true）+ Error 级强告警 + 计数
//
// 调用方（B 队图像链路 / C 队视频链路）在**组调度之后、调用上游之前**调用。
// 推荐用一步到位的 EnsureMediaPricing（检查 + 写响应，不会忘记处理错误）：
//
//	routedModel, providerType, err := dispatch(...)
//	if err != nil { ... }
//	if !middleware.EnsureMediaPricing(c, routedModel, providerType) {
//		return // 已写 402 media_pricing_missing
//	}
//
// 需要自定义处理（换错误体、加审计字段）时用 RequireMediaPricing 拿错误：
//
//	if err := middleware.RequireMediaPricing(c, routedModel, providerType); err != nil {
//		var missingErr *middleware.MediaPricingMissingError
//		if errors.As(err, &missingErr) {
//			missingErr.WriteHTTPResponse(c)
//		}
//		return
//	}
//
// 为什么不是 gin 中间件：中间件阶段拿不到「组名 → 上游模型」的调度结果（模型名在
// request body 里，multipart/form-data 的图像编辑链路更是要 handler 自己解析），
// 此时计价必然算错对象。

// CtxKeyMediaPricingMissing 是「媒体请求命中无价」的 gin context 标记。
//
// 只要本次请求被判定为无价（无论最终是拒绝还是放行），闸门都会写这个键；
// usage_collector.go 读到后强制 event.UsageMissing=true，保证「有字段但无价」
// 这件事实一定落到 call_logs（usage_missing 列），可查、可统计、可告警。
const CtxKeyMediaPricingMissing = "media_pricing_missing"

// MediaPricingMissingInfo 记录无价判定的上下文，供埋点/日志使用。
type MediaPricingMissingInfo struct {
	Model    string // 上游模型（调度后的 routed model）
	Provider string // 渠道类型，如 openai / kling / vidu
	Endpoint string // 入站端点，如 /v1/images/generations
	Rejected bool   // true=已被拒绝（402）；false=开关关闭，放行
}

// MediaPricingMissingError 媒体请求命中「无价」且开关打开时的结构化错误。
// 调用方直接 err.WriteHTTPResponse(c) 即可返回 402（对齐 budget.BudgetExceededError 范式）。
type MediaPricingMissingError struct {
	Model    string
	Provider string
}

func (e *MediaPricingMissingError) Error() string {
	return "media_pricing_missing: model " + e.Model + " has no usable media pricing"
}

// ToJSON 转成 OpenAI 兼容错误体。code 与 tests/contract/contracts.json 的
// 「402: media_pricing_missing」约定一致（E 队/前端按 code 分支，不要改）。
func (e *MediaPricingMissingError) ToJSON() gin.H {
	return gin.H{
		"error": gin.H{
			"message": "该模型未配置媒体单价（图像/视频），为避免静默 0 元计费已拒绝本次调用；" +
				"请管理员在模型目录补齐该模型的媒体单价后重试",
			"type":  "media_pricing_missing",
			"code":  "media_pricing_missing",
			"param": e.Model,
			"gate":  "media_pricing",
		},
	}
}

// WriteHTTPResponse 写 402 Payment Required。
//
// 状态码选择理由（见实现报告）：
//   - 402 是本仓库「准入闸门因计费/配额原因拒绝」的既有约定（budget.ExceededError、
//     BudgetExceededError 都是 402），且 tests/contract/contracts.json 已经把
//     「402 → media_pricing_missing」写进对外契约；
//   - 503 在本仓库已被占用为「服务侧暂时不可用/背压」（billing_fact_store_backpressure、
//     media_pricing_unavailable），语义是"稍后重试可能成功"，而缺价重试多少次都不会成功。
func (e *MediaPricingMissingError) WriteHTTPResponse(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusPaymentRequired, e.ToJSON())
}

// MediaPricingGateConfig 闸门配置（由 cmd/gateway 在启动期注入）。
type MediaPricingGateConfig struct {
	// RequirePricing 命中「媒体请求 + 无价」时是否拒绝准入。
	// 来自 MEDIA_REQUIRE_PRICING，默认 true（保守：宁可拒绝也不静默记 0 元）。
	RequirePricing bool
	// Resolver 价表解析器（按上游模型名 + 渠道查价）。
	// 生产环境复用采集器的同一价表（provider registry 的实时价），保证
	// 「准入判有价 / 记账算得出钱」同源。
	// 为 nil 表示价表不可用 → 按"基础设施不可用"处理（fail-open + 计数 + Error 日志），
	// 不按"无价"处理，避免把价表故障放大成媒体全链路 402。
	Resolver gwobs.PricingResolver
}

// MediaPricingGate 媒体准入计价闸门。零值不可用，请用 NewMediaPricingGate 构造。
type MediaPricingGate struct {
	requirePricing bool
	resolver       gwobs.PricingResolver
}

func NewMediaPricingGate(cfg MediaPricingGateConfig) *MediaPricingGate {
	return &MediaPricingGate{requirePricing: cfg.RequirePricing, resolver: cfg.Resolver}
}

// RequirePricing 返回生效的开关值（供启动日志/自检使用）。
func (g *MediaPricingGate) RequirePricing() bool {
	if g == nil {
		return false
	}
	return g.requirePricing
}

// Require 执行准入判定：
//
//	nil          → 有价，正常放行
//	*MediaPricingMissingError → 无价且开关打开，调用方写 402 并终止
//
// 关键不变量：**无价一定留痕**。拒绝路径写 402 + 计数；放行路径（开关关闭）写
// CtxKeyMediaPricingMissing + 计数 + Error 级日志，绝不允许"悄悄按 0 元放过去"。
func (g *MediaPricingGate) Require(c *gin.Context, model, provider string) error {
	endpoint := ""
	if c != nil && c.Request != nil && c.Request.URL != nil {
		endpoint = c.Request.URL.Path
	}
	attrs := []attribute.KeyValue{
		attribute.String("provider", provider),
		attribute.String("endpoint", endpoint),
	}
	info := MediaPricingMissingInfo{Model: model, Provider: provider, Endpoint: endpoint}

	if g == nil || g.resolver == nil {
		// 价表不可用（闸门未接线 / resolver 缺失）：按基础设施不可用处理。
		// 与 budget 的 AdmissionInfrastructureFailOpen 同范式：不能把"查不到价"和
		// "价表查不了"混为一谈 —— 后者 fail-closed 会把价表故障放大成全链路 402。
		recordMediaPricingDecision(c, "resolver_unavailable", attrs...)
		slog.Error("媒体计价闸门未接线或价表不可用，已放行（不做无价检查）",
			"model", model, "provider", provider, "endpoint", endpoint,
			"hint", "检查 cmd/gateway 是否调用了 middleware.ConfigureMediaPricingGate")
		// 仍然打标：这次请求没有经过无价校验，埋点侧必须按 usage_missing 处理。
		info.Rejected = false
		setMediaPricingMissing(c, info)
		return nil
	}

	if g.isPriced(model, provider) {
		recordMediaPricingDecision(c, "priced", attrs...)
		return nil
	}

	info.Rejected = g.requirePricing
	setMediaPricingMissing(c, info)

	if g.requirePricing {
		// 拒绝：Warn 级（客户端已经拿到 402，事实可见；E 队按计数告警）。
		recordMediaPricingDecision(c, "rejected", attrs...)
		slog.Warn("媒体请求被拒绝：模型无媒体单价（MEDIA_REQUIRE_PRICING=true）",
			"model", model, "provider", provider, "endpoint", endpoint,
			"request_id", ctxString(c, "request_id"),
			"key_id", ctxString(c, "key_id"),
			"code", "media_pricing_missing",
		)
		return &MediaPricingMissingError{Model: model, Provider: provider}
	}

	// 放行但无价：Error 级强告警（这是**账目静默 0 元**的场景，必须人介入；
	// 开关关闭是应急措施，不允许长期停留在这个状态）。
	recordMediaPricingDecision(c, "admitted_unpriced", attrs...)
	slog.Error("媒体请求无价放行，本次将不产生成本（MEDIA_REQUIRE_PRICING=false）",
		"model", model, "provider", provider, "endpoint", endpoint,
		"request_id", ctxString(c, "request_id"),
		"key_id", ctxString(c, "key_id"),
		"usage_missing", true,
		"hint", "请补齐该模型媒体单价，或把 MEDIA_REQUIRE_PRICING 改回 true",
	)
	return nil
}

// isPriced 解析价表并判定「有价」。判定逻辑与采集器一致（同一 ResolveForProvider
// 优先级），差别只在"有价"的粒度：准入是维度无关的模型级判据
// （observability.MediaPriced），记账是逐桶判据（observability.EstimateMediaCost）。
func (g *MediaPricingGate) isPriced(model, provider string) bool {
	var p core.ModelPricing
	var ok bool
	if providerResolver, aware := g.resolver.(gwobs.ProviderPricingResolver); aware {
		p, ok = providerResolver.ResolveForProvider(model, provider)
	} else {
		p, ok = g.resolver.Resolve(model)
	}
	if !ok {
		return false
	}
	return gwobs.MediaPriced(&p)
}

// ── 进程内默认闸门 ────────────────────────────────────────────────────────────
//
// 与 server/routes_media.go 的 mediaAdapterRegistry 同一范式：B/C 两条链路注册在
// server.MediaDeps 里（本轮不改该结构），为了让 handler 能拿到「开关 + 价表」，
// 由 cmd/gateway 在启动期注入一个进程级默认闸门。

var defaultMediaPricingGate atomic.Pointer[MediaPricingGate]

// ConfigureMediaPricingGate 注入进程级默认闸门（cmd/gateway 启动期调用一次）。
func ConfigureMediaPricingGate(cfg MediaPricingGateConfig) *MediaPricingGate {
	gate := NewMediaPricingGate(cfg)
	defaultMediaPricingGate.Store(gate)
	return gate
}

// DefaultMediaPricingGate 返回进程级默认闸门；未接线时为 nil。
func DefaultMediaPricingGate() *MediaPricingGate {
	return defaultMediaPricingGate.Load()
}

// RequireMediaPricing 是 B/C 两条媒体链路的准入入口：用进程级默认闸门判定。
//
// 未接线（nil）时**放行并记 Error 日志**，而不是拒绝：价表不可用属于基础设施故障，
// 按 budget 的 fail-open 范式处理（见 MediaPricingGateConfig.Resolver 注释）。
func RequireMediaPricing(c *gin.Context, model, provider string) error {
	gate := defaultMediaPricingGate.Load()
	if gate == nil {
		return (&MediaPricingGate{}).Require(c, model, provider)
	}
	return gate.Require(c, model, provider)
}

// EnsureMediaPricing 检查并**直接写响应**：返回 false 表示本次已被拒绝（402），
// 调用方必须立刻 return。这是 B/C 两条链路推荐的调用方式。
func EnsureMediaPricing(c *gin.Context, model, provider string) bool {
	err := RequireMediaPricing(c, model, provider)
	if err == nil {
		return true
	}
	var missingErr *MediaPricingMissingError
	if errors.As(err, &missingErr) {
		missingErr.WriteHTTPResponse(c)
		return false
	}
	// Require 目前只可能返回 MediaPricingMissingError；兜底也不静默吞掉。
	c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
		"error": gin.H{
			"message": "媒体计价准入失败",
			"type":    "media_pricing_missing",
			"code":    "media_pricing_missing",
			"param":   model,
			"gate":    "media_pricing",
			"detail":  err.Error(),
		},
	})
	return false
}

func setMediaPricingMissing(c *gin.Context, info MediaPricingMissingInfo) {
	if c == nil {
		return
	}
	c.Set(CtxKeyMediaPricingMissing, info)
}

// MediaPricingMissingInfoFrom 读回无价标记（采集/测试用）。
func MediaPricingMissingInfoFrom(c *gin.Context) (MediaPricingMissingInfo, bool) {
	if c == nil {
		return MediaPricingMissingInfo{}, false
	}
	value, ok := c.Get(CtxKeyMediaPricingMissing)
	if !ok {
		return MediaPricingMissingInfo{}, false
	}
	info, ok := value.(MediaPricingMissingInfo)
	return info, ok
}

func ctxString(c *gin.Context, key string) string {
	if c == nil {
		return ""
	}
	value, _ := c.Get(key)
	s, _ := value.(string)
	return s
}

// ── 计数（E 队告警口径）──────────────────────────────────────────────────────
//
// 指标名与取值是给 E 队的告警接口约定：
//
//	gateway_media_pricing_decision_total{decision,provider,endpoint}
//	  decision = priced            有价，正常放行
//	           | rejected          无价 + 开关开 → 402（**无价拒绝计数**）
//	           | admitted_unpriced 无价 + 开关关 → 放行且 usage_missing=true（**静默 0 元风险**）
//	           | resolver_unavailable 价表不可用 → fail-open（价表故障，非无价）
//
// 刻意**不带 model 标签**：media 路由的 model 直接来自客户端请求体，属用户可控输入，
// 打标签会让指标基数被打爆；具体是哪个模型缺价看日志（Warn/Error 里都带 model）。
var (
	mediaPricingMetricsOnce    sync.Once
	mediaPricingDecisionMeter  metric.Int64Counter
	billableCostMissingMeter   metric.Int64Counter
	mediaPricingMetricsInitErr error
)

func ensureMediaPricingMetrics() {
	mediaPricingMetricsOnce.Do(func() {
		meter := otel.Meter("llm-gateway/media")
		mediaPricingDecisionMeter, mediaPricingMetricsInitErr = meter.Int64Counter(
			"gateway_media_pricing_decision_total",
			metric.WithDescription("媒体准入计价闸门决策计数（无价拒绝/无价放行/有价/价表不可用）"),
		)
		billableCostMissingMeter, _ = meter.Int64Counter(
			"gateway_billable_cost_missing_total",
			metric.WithDescription("有计价事实但金额为 0 的调用计数（无价或用量缺失，绝不静默记 0 元）"),
		)
		if mediaPricingMetricsInitErr != nil {
			slog.Warn("媒体计价指标初始化失败，仅影响告警计数（业务判定不受影响）",
				"error", mediaPricingMetricsInitErr)
		}
	})
}

func recordMediaPricingDecision(c *gin.Context, decision string, attrs ...attribute.KeyValue) {
	ensureMediaPricingMetrics()
	if mediaPricingDecisionMeter == nil {
		return
	}
	all := append([]attribute.KeyValue{attribute.String("decision", decision)}, attrs...)
	mediaPricingDecisionMeter.Add(contextOf(c), 1, metric.WithAttributes(all...))
}

// recordBillableCostMissing 记录「有计价事实但金额为 0」——即被金额门槛吞掉就会
// 变成静默 0 元的那批调用。
func recordBillableCostMissing(c *gin.Context, reason string) {
	ensureMediaPricingMetrics()
	if billableCostMissingMeter == nil {
		return
	}
	billableCostMissingMeter.Add(contextOf(c), 1, metric.WithAttributes(
		attribute.String("reason", reason),
		attribute.String("endpoint", endpointOf(c)),
	))
}

func contextOf(c *gin.Context) context.Context {
	if c != nil && c.Request != nil {
		return c.Request.Context()
	}
	return context.Background()
}

func endpointOf(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}
