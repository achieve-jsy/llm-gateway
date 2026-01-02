package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// PricingResolver 按模型名返回单价（USD/1K tokens）。
// 实现者：providers.ModelRegistry（真实 catalog 定价）+ fallback 实现（无定价时使用保守估计）。
type PricingResolver interface {
	GetModelPricing(model string) *core.ModelPricing
}

// FallbackPricingResolver 当真实 pricing 查不到时的保守估计：
// 输入输出统一按 $0.05/1K tokens（≈ $50/1M tokens），避免预算校验被"无定价"绕过。
//
// 设计说明：
//   - 单价偏高会放大 est_cost，让预算提前耗尽（误判）→ 用户可申请扩容
//   - 单价偏低则让预算被绕过（漏判）→ 设计文档 §J 强调"防单次击穿"
//   - 保守偏高是更安全选择；后续接入真实 catalog pricing 后本 fallback 退居备用
type FallbackPricingResolver struct{}

func (FallbackPricingResolver) GetModelPricing(_ string) *core.ModelPricing {
	price := 0.05 // USD per 1K tokens
	return &core.ModelPricing{
		InputPerMtok:  &price,
		OutputPerMtok: &price,
	}
}

// BudgetDeps 预算校验的依赖（router setup 时一次构造，传给所有 handler）。
type BudgetDeps struct {
	RDB     *redis.Client
	Pricing PricingResolver
	// FallbackOutputTokens：请求未传 max_tokens 时的保守估计
	FallbackOutputTokens int
	// MaxSingleRequestUSD：单请求成本上限（0 = 不限）
	MaxSingleRequestUSD float64
}

// ReserveResult 预算预扣成功后的上下文信息，供后续核销使用。
type ReserveResult struct {
	Keys    []string
	EstCost float64
}

// BudgetExceededError 预算超额时返回的结构化错误。调用方据此构造 402 响应。
type BudgetExceededError struct {
	Message string
	Label   string
}

func (e *BudgetExceededError) Error() string { return e.Message }

// ToJSON 转成 OpenAI 兼容的错误响应体。
func (e *BudgetExceededError) ToJSON() gin.H {
	return gin.H{
		"error": gin.H{
			"message": e.Message,
			"type":    "budget_exceeded",
			"code":    "budget_exceeded",
			"param":   e.Label,
			"gate":    "budget",
		},
	}
}

// WriteHTTPResponse 直接写 402 JSON 响应。
func (e *BudgetExceededError) WriteHTTPResponse(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusPaymentRequired, e.ToJSON())
}

// ─────────────────────────────────────────────────────────────────────────────
// Reserve：handler 在解析完请求体后调用，做预算预扣
// ─────────────────────────────────────────────────────────────────────────────

const (
	CtxKeyBudgetKeys    = "budget_keys"
	CtxKeyBudgetEstCost = "budget_est_cost"
	CtxKeyBudgetSkipped = "budget_skipped"
)

const (
	budgetRedisUsedKeyPrefix = "budget:used:"
	budgetPeriodMonthLayout  = "2006-01"
	budgetPeriodDayLayout    = "2006-01-02"
)

// estimateTokensFromChars 粗略估计 prompt tokens（英文 ~4 chars/token 兜底）。
func estimateTokensFromChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	t := chars / 4
	if t < 1 {
		return 1
	}
	return t
}

// estimateCost 根据请求参数估算预扣成本（USD）。
//
// 策略：
//   - prompt_tokens × input_price / 1K
//   - max_tokens × output_price / 1K
//   - 单价来自 PricingResolver；查不到走 FallbackPricingResolver
//   - max_tokens 未传时使用 deps.FallbackOutputTokens（默认 2048）
func estimateCost(deps *BudgetDeps, model string, maxTokens, promptTokens int) float64 {
	var resolver PricingResolver = deps.Pricing
	if resolver == nil {
		resolver = FallbackPricingResolver{}
	}
	p := resolver.GetModelPricing(model)
	if p == nil {
		p = FallbackPricingResolver{}.GetModelPricing(model)
	}
	if p.InputPerMtok == nil || p.OutputPerMtok == nil {
		p = FallbackPricingResolver{}.GetModelPricing(model)
	}

	output := maxTokens
	if output <= 0 {
		output = deps.FallbackOutputTokens
	}
	if output <= 0 {
		output = 2048
	}

	inputCost := float64(promptTokens) * (*p.InputPerMtok) / 1000.0
	outputCost := float64(output) * (*p.OutputPerMtok) / 1000.0
	return inputCost + outputCost
}

// Reserve 执行预算预扣。
//
// 调用时机：handler 解析请求 body 后（拿到 model / max_tokens / prompt_tokens 估算值）。
//
// 返回值：
//   - result != nil：预扣成功，把 result 记到 ctx，post-hook 会用 result.Keys 做核销
//   - err == *BudgetExceededError：调用方直接 err.WriteHTTPResponse(c) 终止
//   - err != nil && 非 BudgetExceededError：内部错误（fail-open 或调用方决定）
//   - result == nil && err == nil：跳过（无 BudgetChain / rdb 不可用），调用方正常放行
func Reserve(c *gin.Context, deps *BudgetDeps, model string, maxTokens, promptChars int) (*ReserveResult, error) {
	if deps == nil || deps.RDB == nil {
		return nil, nil
	}
	identityVal, exists := c.Get("identity")
	if !exists {
		return nil, nil
	}
	id, ok := identityVal.(*gwmodel.UserIdentity)
	if !ok || len(id.BudgetChain) == 0 {
		return nil, nil
	}

	promptTokens := estimateTokensFromChars(promptChars)
	est := estimateCost(deps, model, maxTokens, promptTokens)
	if est <= 0 {
		return nil, nil
	}

	// 单请求成本上限
	if deps.MaxSingleRequestUSD > 0 && est > deps.MaxSingleRequestUSD {
		return nil, &BudgetExceededError{
			Message: fmt.Sprintf("单请求预估成本 $%.4f 超过单请求上限 $%.4f", est, deps.MaxSingleRequestUSD),
			Label:   "single_request_limit",
		}
	}

	period := time.Now().Format(budgetPeriodMonthLayout)
	weekPeriod := isoWeekString(time.Now())
	dayPeriod := time.Now().Format(budgetPeriodDayLayout)

	// 每个 scope 产生 3 个 key（月/周/日）
	keys := make([]string, 0, len(id.BudgetChain)*3)
	// ARGV: [est_cost, monthly_amount_1, weekly_amount_1, daily_amount_1, label_1, ...]
	args := make([]interface{}, 0, 1+len(id.BudgetChain)*4)
	args = append(args, est)
	for _, scope := range id.BudgetChain {
		// 月 key
		keys = append(keys, fmt.Sprintf("%s%s:%s:%s", budgetRedisUsedKeyPrefix, scope.ScopeType, scope.ScopeID, period))
		// 周 key
		keys = append(keys, fmt.Sprintf("%s%s:%s:%s", budgetRedisUsedKeyPrefix, scope.ScopeType, scope.ScopeID, weekPeriod))
		// 日 key
		keys = append(keys, fmt.Sprintf("%s%s:%s:%s", budgetRedisUsedKeyPrefix, scope.ScopeType, scope.ScopeID, dayPeriod))
		// 各维度额度 + label
		args = append(args, scope.AmountUSD, scope.WeeklyAmountUSD, scope.DailyAmountUSD, scope.Label)
	}

	result, err := budgetReserveScript.Run(c.Request.Context(), deps.RDB, keys, args...).Slice()
	if err != nil {
		// Redis 不可用 / 命令错误：fail-open，标记 budget_skipped
		slog.Warn("budget_reserve: redis error, fail-open",
			"key_id", id.KeyID, "model", model, "est_cost", est, "error", err)
		c.Set(CtxKeyBudgetSkipped, true)
		return nil, nil
	}

	ok2, _ := result[0].(int64)
	if ok2 == 0 {
		label, _ := result[1].(string)
		return nil, &BudgetExceededError{
			Message: fmt.Sprintf("预算已用尽：%s，请联系负责人扩容", label),
			Label:   label,
		}
	}

	res := &ReserveResult{Keys: keys, EstCost: est}
	c.Set(CtxKeyBudgetKeys, keys)
	c.Set(CtxKeyBudgetEstCost, est)
	return res, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 成本估算：与 observability.EstimateCost 复用同一桶算法（锁价 + 阶梯 + 缓存）
// ─────────────────────────────────────────────────────────────────────────────

// RealCost 根据真实 usage 计算成本，供 post-hook 核销。
// 若 pricing 查不到则用 FallbackPricingResolver 保持一致。
func RealCost(deps *BudgetDeps, model string, usage core.Usage) float64 {
	if deps == nil || deps.Pricing == nil {
		return observability.FallbackEstimateCost(usage)
	}
	p := deps.Pricing.GetModelPricing(model)
	if p == nil || p.InputPerMtok == nil || p.OutputPerMtok == nil {
		return observability.FallbackEstimateCost(usage)
	}
	cost, missing := observability.EstimateCost(observability.ExtractCostInput(usage), p)
	if missing {
		return observability.FallbackEstimateCost(usage)
	}
	return cost
}

// Settle 核销预算：post-call 阶段由 hook 调用。
// 失败请求（usage 缺失或错误）时 realCost 应传 0，从而冲销全部预扣。
func (d *BudgetDeps) Settle(ctx context.Context, keys []string, estCost, realCost float64) {
	if d == nil || d.RDB == nil || len(keys) == 0 {
		return
	}
	BudgetSettle(ctx, d.RDB, keys, estCost, realCost)
}

// Cancel 冲销预扣：请求失败 / 4xx / 上游错误时调用。
func (d *BudgetDeps) Cancel(ctx context.Context, keys []string, estCost float64) {
	if d == nil || d.RDB == nil || len(keys) == 0 {
		return
	}
	BudgetCancelReserve(ctx, d.RDB, keys, estCost)
}

// isoWeekString 返回 ISO 8601 周标识，格式 "YYYY-Www"，如 "2026-W24"。
func isoWeekString(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}
