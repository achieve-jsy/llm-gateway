package middleware

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/redis/go-redis/v9"
)

// 本文件包含预算拦截的 Redis 原子脚本和 helper 函数。
//
// v0.9 架构调整（事后记账，对齐 LiteLLM / Cursor 方式）：
//   - 废弃"预扣→核销"模式，改为"请求前检查已累计用量，请求后直接加真实成本"
//   - 消除成本估算误差导致的误杀问题
//   - 并发窗口（多请求同时通过检查后超限）对内部场景可接受
//
// budgetCheckScript：纯读检查，任一维度已达上限则拒绝（不写 Redis）
// budgetAddScript：请求成功后，将真实成本原子累加到各层各维度计数器

// budgetCheckScript 纯检查 Lua：读取各层各维度已用量，超限立即返回，不修改任何 key。
// KEYS：[scope1_monthly, scope1_weekly, scope1_daily, scope2_monthly, ...]
// ARGV：[monthly_amount_1, weekly_amount_1, daily_amount_1, label_1, ...]
// 返回：{1, ""} = 通过；{0, label} = 超额层+维度
var budgetCheckScript = redis.NewScript(`
local n = #KEYS / 3
for i = 0, n - 1 do
  local mk = KEYS[i * 3 + 1]
  local wk = KEYS[i * 3 + 2]
  local dk = KEYS[i * 3 + 3]
  local base = i * 4 + 1
  local m_amount = tonumber(ARGV[base])
  local w_amount = tonumber(ARGV[base + 1])
  local d_amount = tonumber(ARGV[base + 2])
  local label = ARGV[base + 3]
  if m_amount >= 0 then
    local used = tonumber(redis.call('GET', mk) or '0')
    if used >= m_amount then
      return {0, label .. ' [月限额]'}
    end
  end
  if w_amount >= 0 then
    local used = tonumber(redis.call('GET', wk) or '0')
    if used >= w_amount then
      return {0, label .. ' [周限额]'}
    end
  end
  if d_amount >= 0 then
    local used = tonumber(redis.call('GET', dk) or '0')
    if used >= d_amount then
      return {0, label .. ' [日限额]'}
    end
  end
end
return {1, ''}
`)

// budgetAddScript 累加真实成本 Lua：请求成功后将 real_cost 加到各层各维度计数器。
// KEYS：[scope1_monthly, scope1_weekly, scope1_daily, ...]（与 budgetCheckScript 相同顺序）
// ARGV[1]：real_cost
// ARGV[2..]：[monthly_amount_1, weekly_amount_1, daily_amount_1, label_1, ...]（用于判断哪些维度配额已启用）
// TTL：月 key 70 天，周 key 14 天，日 key 3 天
var budgetAddScript = redis.NewScript(`
local real = tonumber(ARGV[1])
local n = #KEYS / 3
local ttls = {6048000, 1209600, 259200}
for i = 0, n - 1 do
  local mk = KEYS[i * 3 + 1]
  local wk = KEYS[i * 3 + 2]
  local dk = KEYS[i * 3 + 3]
  local base = i * 4 + 2
  local m_amount = tonumber(ARGV[base])
  local w_amount = tonumber(ARGV[base + 1])
  local d_amount = tonumber(ARGV[base + 2])
  if m_amount >= 0 then
    redis.call('INCRBYFLOAT', mk, real)
    if redis.call('TTL', mk) < 0 then redis.call('EXPIRE', mk, ttls[1]) end
  end
  if w_amount >= 0 then
    redis.call('INCRBYFLOAT', wk, real)
    if redis.call('TTL', wk) < 0 then redis.call('EXPIRE', wk, ttls[2]) end
  end
  if d_amount >= 0 then
    redis.call('INCRBYFLOAT', dk, real)
    if redis.call('TTL', dk) < 0 then redis.call('EXPIRE', dk, ttls[3]) end
  end
end
return 1
`)

// BudgetAdd 请求成功后将真实成本累加到各层各维度计数器。
// keys：各层各维度 Redis key（月+周+日 × N scopes）
// scopeArgs：scope 额度信息，用于判断哪些维度已启用（来自 ctx 中的 budget_scope_args）
// realCost：provider 返回的真实成本（USD）
func BudgetAdd(ctx context.Context, rdb *redis.Client, keys []string, scopeArgs []interface{}, realCost float64) error {
	if rdb == nil || len(keys) == 0 || realCost <= 0 {
		return nil
	}
	args := make([]interface{}, 0, 1+len(scopeArgs))
	args = append(args, realCost)
	args = append(args, scopeArgs...)
	if err := budgetAddScript.Run(ctx, rdb, keys, args...).Err(); err != nil {
		slog.Warn("budget_add: redis error", "real_cost", realCost, "error", err)
		return err
	}
	return nil
}

var legacyPlanCheckScript = redis.NewScript(`
for i = 1, #KEYS do
  local used = tonumber(redis.call('GET', KEYS[i]) or '0')
  local limit = tonumber(ARGV[i])
  if used >= limit then return i end
end
return 0
`)

func CheckLegacyBudget(
	ctx context.Context,
	rdb *redis.Client,
	plan *budget.AccountingPlan,
) (*budget.Constraint, error) {
	if rdb == nil || plan == nil {
		return nil, nil
	}
	keys := make([]string, 0, len(plan.Constraints))
	args := make([]any, 0, len(plan.Constraints))
	constraints := make([]budget.Constraint, 0, len(plan.Constraints))
	for _, constraint := range plan.Constraints {
		if constraint.Dimension != budget.DimensionTotal {
			continue
		}
		keys = append(keys, BudgetUsedKey(constraint.ScopeType, constraint.ScopeID, constraint.Period))
		args = append(args, strconv.FormatFloat(float64(constraint.LimitMicroUSD)/1_000_000, 'f', 6, 64))
		constraints = append(constraints, constraint)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	index, err := legacyPlanCheckScript.Run(ctx, rdb, keys, args...).Int()
	if err != nil {
		return nil, err
	}
	if index == 0 {
		return nil, nil
	}
	blocked := constraints[index-1]
	return &blocked, nil
}

// BudgetUsedKey 构造 Redis 预算用键名（外部如需读取实时已用额用）。
func BudgetUsedKey(scopeType, scopeID, period string) string {
	return "budget:used:" + scopeType + ":" + scopeID + ":" + period
}

// BudgetAmountKey 构造 Redis 预算快照键名（控制面改额时写入，网关并集校验直接读）。
func BudgetAmountKey(scopeType, scopeID string) string {
	return "budget:amount:" + scopeType + ":" + scopeID
}
