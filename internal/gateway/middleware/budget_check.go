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
//   - v0.8：扩展为日/周/月三维度并行校验，每个 scope 产生 3 个 Redis key
//
// 本文件保留的：
//   - Redis Lua 脚本（预扣 + 核销）
//   - BudgetSettle / BudgetCancelReserve helpers（供 post-hook 调用）

// budgetReserveScript 原子预扣 Lua：多层多维度一次性校验 + 占用。
// 每个 scope 有 3 个 key（月/周/日），逐一校验。
// KEYS：[scope1_monthly, scope1_weekly, scope1_daily, scope2_monthly, scope2_weekly, scope2_daily, ...]
// ARGV[1]：est_cost
// ARGV[2..]: 每 scope 4 个值 [monthly_amount, weekly_amount, daily_amount, label] 循环
// 返回：{1, ""} = 通过；{0, label} = 超额层+维度
var budgetReserveScript = redis.NewScript(`
local est = tonumber(ARGV[1])
local n = #KEYS / 3
-- 第一遍：纯校验，任一维度超额立即返回
for i = 0, n - 1 do
  local mk = KEYS[i * 3 + 1]
  local wk = KEYS[i * 3 + 2]
  local dk = KEYS[i * 3 + 3]
  local base = i * 4 + 2
  local m_amount = tonumber(ARGV[base])
  local w_amount = tonumber(ARGV[base + 1])
  local d_amount = tonumber(ARGV[base + 2])
  local label = ARGV[base + 3]
  -- 月度校验
  if m_amount >= 0 then
    local used = tonumber(redis.call('GET', mk) or '0')
    if (used + est) > m_amount then
      return {0, label .. ' [月限额]'}
    end
  end
  -- 周度校验
  if w_amount >= 0 then
    local used = tonumber(redis.call('GET', wk) or '0')
    if (used + est) > w_amount then
      return {0, label .. ' [周限额]'}
    end
  end
  -- 日度校验
  if d_amount >= 0 then
    local used = tonumber(redis.call('GET', dk) or '0')
    if (used + est) > d_amount then
      return {0, label .. ' [日限额]'}
    end
  end
end
-- 第二遍：全部通过才预占（对所有 key 做 INCRBYFLOAT）
for i = 0, n - 1 do
  local mk = KEYS[i * 3 + 1]
  local wk = KEYS[i * 3 + 2]
  local dk = KEYS[i * 3 + 3]
  local base = i * 4 + 2
  local m_amount = tonumber(ARGV[base])
  local w_amount = tonumber(ARGV[base + 1])
  local d_amount = tonumber(ARGV[base + 2])
  if m_amount >= 0 then
    redis.call('INCRBYFLOAT', mk, est)
  end
  if w_amount >= 0 then
    redis.call('INCRBYFLOAT', wk, est)
  end
  if d_amount >= 0 then
    redis.call('INCRBYFLOAT', dk, est)
  end
end
return {1, ''}
`)

// budgetSettleScript 核销 Lua：对各层各维度 used 键回写差额（real_cost - est_cost）。
// KEYS：所有 used 键（月+周+日 × N 个 scope）；ARGV[1]：delta
// 只对有值的 key 做 INCRBYFLOAT（key 不存在说明该维度未配额，跳过）。
var budgetSettleScript = redis.NewScript(`
local delta = tonumber(ARGV[1])
for i = 1, #KEYS do
  if redis.call('EXISTS', KEYS[i]) == 1 then
    redis.call('INCRBYFLOAT', KEYS[i], delta)
  end
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
