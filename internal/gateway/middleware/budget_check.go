package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// 预算 Redis 键前缀
const (
	budgetUsedKeyPrefix   = "budget:used:"   // budget:used:{scope_type}:{scope_id}:{period}
	budgetAmountKeyPrefix = "budget:amount:"  // budget:amount:{scope_type}:{scope_id}
	budgetUsedKeyTTLDays  = 37               // 周期末缓冲 TTL（天）
)

// budgetReserveScript 原子预扣 Lua：多层一次性校验 + 占用。
// KEYS：各层 used 键；ARGV[1]：est_cost；ARGV[2..2n]：[amount_i, label_i]...
// 返回：{1, ""} = 通过；{0, label} = 超额层
var budgetReserveScript = redis.NewScript(`
local est = tonumber(ARGV[1])
local n = #KEYS
-- 第一遍：纯校验，任一层超额立即返回
for i = 1, n do
  local used = tonumber(redis.call('GET', KEYS[i]) or '0')
  local amount = tonumber(ARGV[i * 2])
  local label = ARGV[i * 2 + 1]
  if amount >= 0 and (used + est) > amount then
    return {0, label}
  end
end
-- 第二遍：全部通过才预占
for i = 1, n do
  redis.call('INCRBYFLOAT', KEYS[i], est)
end
return {1, ''}
`)

// budgetSettleScript 核销 Lua：对各层 used 键回写差额（real_cost - est_cost）。
// KEYS：各层 used 键；ARGV[1]：delta
var budgetSettleScript = redis.NewScript(`
local delta = tonumber(ARGV[1])
for i = 1, #KEYS do
  redis.call('INCRBYFLOAT', KEYS[i], delta)
end
return 1
`)

// BudgetCheckMiddleware 预算并集校验中间件（网关第④关）。
// 挂在授权和限流之后、调度之前（post-body，需要 BudgetChain 快照）。
// 失败模式：fail_open——Redis 不可用时放行并标记 budget_skipped=true，事后由对账补算。
func BudgetCheckMiddleware(rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rdb == nil {
			c.Next()
			return
		}

		identityVal, exists := c.Get("identity")
		if !exists {
			c.Next()
			return
		}
		id, ok := identityVal.(*gwmodel.UserIdentity)
		if !ok || len(id.BudgetChain) == 0 {
			// 无预算链（旧版快照兼容）：放行，不计费拦截
			c.Next()
			return
		}

		// 计算预估成本（从 context 中取，由 handler 层 preflight 写入）
		estCost, _ := c.Get("budget_est_cost")
		est, _ := estCost.(float64)
		if est <= 0 {
			// 预估成本为 0 或未设置：放行（不做无谓校验）
			c.Next()
			return
		}

		period := time.Now().Format("2006-01")
		keys, args := buildLuaArgs(id.BudgetChain, est, period)

		result, err := budgetReserveScript.Run(c.Request.Context(), rdb, keys, args...).Slice()
		if err != nil {
			// Redis 不可用：fail-open
			slog.Warn("budget_check: redis error, fail-open",
				"key_id", id.KeyID, "est_cost", est, "error", err)
			c.Set("budget_skipped", true)
			c.Next()
			return
		}

		ok2, _ := result[0].(int64)
		if ok2 == 0 {
			// 超额：返回 402
			label, _ := result[1].(string)
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error": gin.H{
					"message": fmt.Sprintf("预算已用尽：%s，请联系负责人扩容", label),
					"type":    "budget_exceeded",
					"code":    "budget_exceeded",
					"param":   label,
					"gate":    "budget",
				},
			})
			return
		}

		// 预占成功：注册核销回调（由 usage_collector 在 post-call 阶段执行）
		c.Set("budget_keys", keys)
		c.Set("budget_est_cost", est)
		c.Next()
	}
}

// BudgetSettle 核销预算（usage_collector post-call 阶段调用）。
// realCost：真实成本（来自 usage）；estCost：预扣成本。
// 差额 delta = realCost - estCost（可正可负）。
func BudgetSettle(ctx context.Context, rdb *redis.Client, keys []string, estCost, realCost float64) {
	if rdb == nil || len(keys) == 0 {
		return
	}
	delta := realCost - estCost
	if err := budgetSettleScript.Run(ctx, rdb, keys, delta).Err(); err != nil {
		slog.Warn("budget_check: settle failed", "delta", delta, "error", err)
	}
}

// BudgetCancelReserve 冲销预扣（请求失败/取消时调用）。
func BudgetCancelReserve(ctx context.Context, rdb *redis.Client, keys []string, estCost float64) {
	BudgetSettle(ctx, rdb, keys, estCost, 0) // delta = 0 - estCost = -estCost
}

// buildLuaArgs 构造 Lua 脚本的 KEYS 和 ARGV。
func buildLuaArgs(chain []gwmodel.BudgetScope, estCost float64, period string) ([]string, []interface{}) {
	keys := make([]string, len(chain))
	args := make([]interface{}, 0, 1+len(chain)*2)
	args = append(args, estCost)
	for i, scope := range chain {
		keys[i] = fmt.Sprintf("%s%s:%s:%s", budgetUsedKeyPrefix, scope.ScopeType, scope.ScopeID, period)
		args = append(args, scope.AmountUSD, scope.Label)
	}
	return keys, args
}

// BudgetUsedKey 暴露用键名供外部使用（Redis 金额快照键）。
func BudgetAmountKey(scopeType, scopeID string) string {
	return fmt.Sprintf("%s%s:%s", budgetAmountKeyPrefix, scopeType, scopeID)
}
