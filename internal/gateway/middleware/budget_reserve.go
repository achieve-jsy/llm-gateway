package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// BudgetDeps 预算校验的依赖（router setup 时一次构造，传给所有 handler）。
// v0.9：去掉估算相关字段（Pricing、FallbackOutputTokens、MaxSingleRequestUSD），
// 改为事后记账模式，不再需要在请求前估算成本。
type BudgetDeps struct {
	RDB *redis.Client
}

// ReserveResult 检查通过后的上下文信息，供 post-hook 累加真实成本使用。
type ReserveResult struct {
	Keys      []string
	ScopeArgs []interface{} // 传给 BudgetAdd 的 scope 额度参数
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

const (
	CtxKeyBudgetKeys      = "budget_keys"
	CtxKeyBudgetScopeArgs = "budget_scope_args" // []interface{}，传给 BudgetAdd
	CtxKeyBudgetSkipped   = "budget_skipped"

	budgetRedisUsedKeyPrefix = "budget:used:"
	budgetPeriodMonthLayout  = "2006-01"
	budgetPeriodDayLayout    = "2006-01-02"
)

// Reserve 检查当前累计用量是否已超限（v0.9 事后记账模式）。
//
// 设计变更（v0.9）：
//   - 不再估算本次请求成本，不再预扣 Redis 计数器
//   - 只读取各层各维度已累计用量，与配额比较
//   - 通过后将 keys 和 scopeArgs 写入 ctx，供 post-hook 调用 BudgetAdd 累加真实成本
//
// 返回值：
//   - result != nil：检查通过，post-hook 会用 result 做真实成本累加
//   - err == *BudgetExceededError：已超限，调用方直接 err.WriteHTTPResponse(c) 终止
//   - result == nil && err == nil：跳过（无 BudgetChain / rdb 不可用），调用方正常放行
func Reserve(c *gin.Context, deps *BudgetDeps) (*ReserveResult, error) {
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

	now := time.Now()
	period := now.Format(budgetPeriodMonthLayout)
	weekPeriod := isoWeekString(now)
	dayPeriod := now.Format(budgetPeriodDayLayout)

	keys := make([]string, 0, len(id.BudgetChain)*3)
	// scopeArgs: [monthly_amount_1, weekly_amount_1, daily_amount_1, label_1, ...]
	scopeArgs := make([]interface{}, 0, len(id.BudgetChain)*4)
	for _, scope := range id.BudgetChain {
		keys = append(keys,
			fmt.Sprintf("%s%s:%s:%s", budgetRedisUsedKeyPrefix, scope.ScopeType, scope.ScopeID, period),
			fmt.Sprintf("%s%s:%s:%s", budgetRedisUsedKeyPrefix, scope.ScopeType, scope.ScopeID, weekPeriod),
			fmt.Sprintf("%s%s:%s:%s", budgetRedisUsedKeyPrefix, scope.ScopeType, scope.ScopeID, dayPeriod),
		)
		scopeArgs = append(scopeArgs, scope.AmountUSD, scope.WeeklyAmountUSD, scope.DailyAmountUSD, scope.Label)
	}

	result, err := budgetCheckScript.Run(c.Request.Context(), deps.RDB, keys, scopeArgs...).Slice()
	if err != nil {
		slog.Warn("budget_check: redis error, fail-open",
			"key_id", id.KeyID, "error", err)
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

	c.Set(CtxKeyBudgetKeys, keys)
	c.Set(CtxKeyBudgetScopeArgs, scopeArgs)
	return &ReserveResult{Keys: keys, ScopeArgs: scopeArgs}, nil
}

// isoWeekString 返回 ISO 8601 周标识，格式 "YYYY-Www"，如 "2026-W24"。
func isoWeekString(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}
