package middleware

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// 本文件包含预算拦截的 Redis 原子脚本（预扣 + 核销）和 helper 函数。
//
// 设计历史：
//   - v0.7 初期：以 Gin middleware（BudgetCheckMiddleware）实现，挂在 handler 之前
//   - 问题：middleware 拿不到请求 body 里的 model / max_tokens，无法估算成本
//   - 结果：BudgetCheckMiddleware 始终按 est_cost = 0 处理，预算拦截形同虚设
//   - v0.7 修订：改为函数 Reserve，由每个 handler 在解析 body 后主动调用（见 budget_reserve.go）
//
// 本文件保留的：
//   - Redis Lua 脚本（预扣 + 核销）
//   - BudgetSettle / BudgetCancelReserve helpers（供 post-hook 调用）

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

// BudgetSettle 核销预算（usage_collector post-call 阶段调用）。
// realCost：真实成本（来自 usage）；estCost：预扣成本。
// 差额 delta = realCost - estCost（可正可负）。
func BudgetSettle(ctx context.Context, rdb *redis.Client, keys []string, estCost, realCost float64) {
	if rdb == nil || len(keys) == 0 {
		return
	}
	delta := realCost - estCost
	if err := budgetSettleScript.Run(ctx, rdb, keys, delta).Err(); err != nil {
		slog.Warn("budget_settle: redis error", "delta", delta, "error", err)
	}
}

// BudgetCancelReserve 冲销预扣（请求失败/取消时调用）。
// 等价于 BudgetSettle(..., estCost, 0)，delta = -estCost。
func BudgetCancelReserve(ctx context.Context, rdb *redis.Client, keys []string, estCost float64) {
	BudgetSettle(ctx, rdb, keys, estCost, 0)
}

// BudgetUsedKey 构造 Redis 预算用键名（外部如需读取实时已用额用）。
func BudgetUsedKey(scopeType, scopeID, period string) string {
	return "budget:used:" + scopeType + ":" + scopeID + ":" + period
}

// BudgetAmountKey 构造 Redis 预算快照键名（控制面改额时写入，网关并集校验直接读）。
func BudgetAmountKey(scopeType, scopeID string) string {
	return "budget:amount:" + scopeType + ":" + scopeID
}
