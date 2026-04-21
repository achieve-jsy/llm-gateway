# Gateway、Redis v2、等级快照、记账与影子模式 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不破坏现有总预算的前提下，让 Gateway 对已授权模型组执行总预算与模型等级子预算检查，以整数微美元完成 Redis v2 检查/记账，并支持独立等级快照、双写、影子对账、重建、告警和完整观测。

**Architecture:** P0 的 Control Plane `BudgetTierSnapshotPublisher` 负责从数据库构建、原子保存并发布 `budget:v2:tier:snapshot`；P1 只实现 Gateway consumer，不在 Gateway 或 P1 Control Plane 代码中再次构建/发布快照。P0 的 `verify-key` 下发预算链和已配置等级额度，Gateway 在模型组授权成功后由 `TierResolver` 固定请求级 `TierContext`；disabled/archived 仍保留 ID/name 供报表，只关闭 accounting/enforcement eligibility，未分级才没有 context。随后构造不可变 `AccountingPlan`，用整数 Lua 执行总预算与等级预算检查及成功后累计；旧浮点总预算 key 与 v2 key 在灰度期双写。流式请求只在建流前检查，流结束后按最终 usage 记账；P1 把 tier/eligible 和真实 402 block 快照写入 `call_logs`，作为重建、影子对账和 P2 风险聚合事实源。所有周期键和重置边界只通过 P0 共享的 `internal/budgetperiod` 计算，固定 `Asia/Shanghai` 与 ISO Monday，禁止读取 `time.Local`。

**Tech Stack:** Go 1.x、Gin、go-redis/v9、Redis Lua、`sync/atomic`、OpenTelemetry metrics/trace、GORM/PostgreSQL、miniredis、testify。

---

## 范围、前置条件与不可变规则

本计划是技术设计第 16 节的第 2 个独立交付，严格依赖 P0 已完成并通过测试的以下产物：

1. Atlas/GORM：`model_tiers`、`model_groups.budget_tier_id`、`budget_tier_limits`、`budget_temporary_overrides`、`budget_limit_schedules`、扩展后的 `budget_alert_records`，以及 `call_logs.budget_tier_id`、`call_logs.budget_tier_name_snapshot`、`call_logs.budget_tier_accounting_eligible boolean not null default false`、`call_logs.budget_tier_constraints_snapshot jsonb not null default '[]'`。JSONB 保存请求实际参与等级记账的逐层 `redis_key/scope_type/scope_id/tier_id/period_type/period/reset_at`，归属 P0 foundation migration；P1 只负责运行时写入。五个可空阻断列由 P0 提供：`budget_block_code varchar(64)`、`budget_block_dimension varchar(8)`、`budget_block_scope_type varchar(32)`、`budget_block_scope_id varchar(128)`、`budget_block_period_type varchar(8)`；对应 Go 字段统一为 `*string`。
2. verify-key：`internal/controlplane/service/key_service.go` 保留现有总预算 JSON number 字段 `amount_usd`、`daily_amount_usd`、`weekly_amount_usd`，仅新增只包含已配置项且金额为 6 位小数字符串的 `tier_limits`，并在额度变更时失效相关 KeyCache；不得增加任何同 tag 的 v2 总预算字段。
3. 基础 API：模型等级 CRUD/排序/启停/归档、安全删除、单个/批量 group tier 绑定，以及 `GET/PUT /api/v1/budgets/:allocation_id/tier-limits`、batch preview/apply、tier usage、my budget tier aggregation 都已有可测试 handler/service。
4. snapshot publisher：P0 的 `BudgetTierSnapshotPublisher` 已按单调 revision 构建完整快照、原子写 `budget:v2:tier:snapshot`、发布 `infra.ChangeEvent` JSON（`type=budget_tier_snapshot_updated`）、记录同步状态并运行 reconcile；disabled/archived tier 的 group map entry 仍保留 ID/name，但 `enabled=false`。P1 只按 P0 的 `configsync.BudgetTierSnapshot`/key/event 契约执行 Redis GET 和 JSON 解码。
5. 共享周期包：`internal/budgetperiod` 已提供 `ValidateConfig`/`Resolve`，校验 timezone=`Asia/Shanghai`、week_start=`monday`，日/周/月窗口由 Control Plane 与 Gateway 共用。

P1 不创建迁移、不实现或复制 snapshot publisher、不重新实现上述 P0 API。若任一前置产物缺失，先回到 P0 完成，不能在 P1 用 `AutoMigrate`、遗留 `migrations/`、Gateway HTTP 回源或第二套 DTO 绕过。

`budget_tier_accounting_eligible` 已是 Spec v0.6/technical design 的正式前置列。P1 负责在请求开始时写入其值：它表示“本请求业务上应进入等级计数”，不表示当时 Redis 写入成功；Redis fail-open 的成功调用在重建时仍应补回真实消费。其值为 `tier != nil && tier.AccountingEligible && master_enabled && accounting_enabled && 至少存在一个已启用且已生效的该等级 constraint`；功能关闭、accounting 关闭、等级/额度停用、等级归档或未分级时均为 false，影子模式下仍为 true。

P0 对五个 `budget_block_*` 字段的责任仅限 Atlas/GORM schema。P1 独占运行时写入责任：Gate 固定真实 402 首要阻断项，Collector 使用 `stringPtr` 把值复制为 `*string`，其中 `Dimension` 与 `PeriodType` 必须先显式转为 string；UsageEvent 到调用日志完整落库，供 P2 风险事件聚合。只有真实返回 402 时写入，其他情况保持 `nil`；403、shadow would-block、Redis fail-open、成功请求均为空。`budget_block_code` 只允许 `budget_exceeded|tier_budget_exceeded`，dimension 只允许 `total|tier`；scope ID 仅落内部调用日志，不进入公开响应。

固定规则：

1. `/v1/p/:provider/*` 不参与等级预算。
2. 模型组授权永远先于预算检查；未授权只返回 403，不执行 Redis GET/Lua，不泄露 402。
3. 未分级请求只检查/累计总预算。
4. `$0` 从内存预算链判断，Redis 故障时仍 fail-closed；正数额度 Redis 故障 fail-open，且本请求不写任何 v2 counter。
5. 开关只允许 `master_enabled`、`accounting_enabled`、`positive_enforcement_enabled`、`zero_block_enabled`、`shadow_enabled`、`v2_total_enforcement_enabled`、`legacy_total_write_enabled`；不得增加 `v2_write_enabled`、`shadow_mode` 等别名。
6. 单次实际成本统一 `ROUND_HALF_UP(USD × 1_000_000)`；Lua 不接收浮点美元。
7. Chat、Responses、Messages、Embeddings、文本 Rerank、VL Rerank 共用同一个 `PrepareAuthorizedBudget` helper。
8. 只有最终 HTTP 状态 `< 400` 的请求记账；价格或 usage 缺失得到 0 微美元，不写 counter、不告警但仍写调用日志。
9. `master_enabled=false` 是关闭：仍写 tier ID/name 快照，但不检查、不累计 tier，eligible=false；影子是 `master_enabled=true && accounting_enabled=true && shadow_enabled=true && positive_enforcement_enabled=false`：累计 tier 并计算 would-block，但不执行正数 tier 402。
10. Gateway 公开 402 `error.code` 只能是小写 `budget_exceeded` 或 `tier_budget_exceeded`；大写错误码仅属于 Control Plane 管理 API。

## 文件结构

**新建：**

- `internal/gateway/budget/money.go`：十进制 USD 与微美元的唯一转换边界。
- `internal/gateway/budget/context.go`：`TierContext`、`Constraint`、`AccountingPlan` 与 Gin context helper。
- `internal/gateway/budget/snapshot_holder.go`：独立等级快照的原子 last-known-good holder。
- `internal/gateway/budget/config_loader.go`：Redis 初始加载、`config:change` 订阅和 30 秒 reconcile。
- `internal/gateway/budget/tier_resolver.go`：只按原始模型组名解析并固定等级快照。
- `internal/gateway/budget/redis_store.go`：v2 key、总+等级检查 Lua、累计 Lua、fail-open 结果。
- `internal/gateway/budget/gate.go`：静态 `$0`、正数检查、阻断排序、shadow 与双写决策。
- `internal/gateway/budget/metrics.go`：预算维度 OTel counters。
- `internal/controlplane/service/budget_counter_rebuild_service.go`：从 `call_logs` 重建 v2 总/等级 counter 与影子差异。
- `tests/gateway/budget/period_contract_test.go`：验证 Gateway 消费 P0 周期包时在上海时区和 ISO Monday 边界一致。
- `tests/gateway/budget/money_test.go`
- `tests/gateway/budget/snapshot_loader_test.go`
- `tests/gateway/budget/tier_resolver_test.go`
- `tests/gateway/budget/redis_store_test.go`
- `tests/gateway/budget/gate_test.go`
- `tests/gateway/handler/model_tier_budget_test.go`
- `tests/gateway/observability/tier_snapshot_test.go`
- `tests/controlplane/service/budget_counter_rebuild_service_test.go`
- `tests/integration/model_tier_budget_flow_test.go`

**修改：**

- `internal/configsync/budget_tier_snapshot.go`（P0 已创建）：P1 只消费其类型、key 和 event 常量，不修改保存/发布逻辑。
- `internal/gateway/model/identity.go`：verify-key 镜像保持 total JSON number，仅增加金额为字符串的 `TierLimits`。
- `internal/gateway/infra/auth_client.go`：解码 tier limits。
- `internal/gateway/orchestrator/group_dispatcher.go`：授权后调用 `TierResolver`，固定 tier；不得把 provider/model failover 当成等级来源。
- `internal/gateway/handler/chat.go`
- `internal/gateway/handler/responses.go`
- `internal/gateway/handler/messages.go`
- `internal/gateway/handler/embeddings.go`
- `internal/gateway/handler/rerank.go`：统一改为授权/解析等级后检查预算。
- `internal/gateway/middleware/budget_check.go`：保留旧 key 双写函数，但让它返回错误并接收已完成的 legacy plan。
- `internal/gateway/middleware/budget_reserve.go`：旧总预算检查收敛为 legacy adapter，不再由 handler 直接调用。
- `internal/gateway/middleware/usage_collector.go`：先完成同步 Redis 记账结果，再投递包含等级快照的 usage event。
- `internal/gateway/observability/event.go`
- `internal/gateway/observability/collector.go`
- `internal/observability/model/call_log.go`（P0 只提供 tier 三列与 block 五列 schema）：P1 在 Collector/event 转换中负责写入 tier ID/name/accounting eligible 和真实 402 首要阻断快照。
- `internal/gateway/server/router.go`：组装 holder/loader/gate/post-hook、双写和告警请求。
- `internal/gateway/middleware/tracing.go`：写预算 trace 属性。
- `cmd/gateway/main.go`：启动独立等级 loader，并把 holder 注入 router。
- `internal/controlplane/handler/budget_handler.go`：告警内部请求增加 dimension/tier；重建返回 v2 与 shadow 差异。
- `internal/controlplane/router/router.go`：注入重建 service。
- `tests/gateway/infra/authclient/auth_client_test.go`
- `tests/gateway/orchestrator/group_auth_test.go`
- `tests/gateway/middleware/budget_reserve_test.go`
- `tests/gateway/middleware/trace_pipeline_test.go`
- `tests/contract/contracts.json`：P0 已固定 verify-key/snapshot/公开 402；P1 只追加内部 alert request 契约，不改 P0 字段。

## JSON 与 Redis 契约

P1 直接消费 P0 已交付的 verify-key 契约。总预算只保留现有 JSON number，Gateway 在 `BuildAccountingPlan` 内转换为微美元；只有 `tier_limits` 金额使用 6 位小数字符串：

```json
{
  "scope_type": "member",
  "scope_id": "8c76ec36-b265-4a10-8db5-51d51965f4ca",
  "amount_usd": 300,
  "daily_amount_usd": -1,
  "weekly_amount_usd": -1,
  "tier_limits": [
    {
      "tier_id": "00bb2e65-9c67-45f7-b061-d297a3944876",
      "tier_name": "高级",
      "period_type": "monthly",
      "base_limit_usd": "80.000000",
      "effective_limit_usd": "100.000000",
      "enabled": true,
      "effective_at": "2026-07-11T08:00:00Z",
      "expires_at": "2026-08-01T00:00:00Z"
    }
  ]
}
```

独立等级快照：

```json
{
  "schema_version": 1,
  "revision": 42,
  "updated_at": "2026-07-11T08:00:00Z",
  "master_enabled": true,
  "accounting_enabled": true,
  "positive_enforcement_enabled": false,
  "zero_block_enabled": true,
  "shadow_enabled": true,
  "v2_total_enforcement_enabled": false,
  "legacy_total_write_enabled": true,
  "timezone": "Asia/Shanghai",
  "week_start": "monday",
  "group_tiers": {
    "claude-opus": {
      "tier_id": "00bb2e65-9c67-45f7-b061-d297a3944876",
      "tier_name": "高级",
      "enabled": true
    }
  }
}
```

v2 keys：

```text
budget:v2:used:{scope_type}:{scope_id}:{period}
budget:v2:tier:used:{scope_type}:{scope_id}:{tier_id}:{period}
budget:v2:tier:snapshot
```

所有 used value 都是十进制 `int64` 微美元字符串。月/周/日 TTL 分别为 70 天、14 天、3 天。

## P0 → P1 依赖矩阵

| P0 产物 | P1 消费点 | P1 禁止事项 | 验证 |
|---|---|---|---|
| `BudgetTierSnapshotPublisher` + reconcile + `infra.ChangeEvent` JSON | Task 2 loader | 不构建、不保存、不 publish 快照；不消费裸字符串事件 | loader 仅 GET，按 event.Type reload，坏 revision 保留 LKG |
| `configsync.BudgetTierSnapshot`/key/event 常量 | Task 2、3、7 | 不定义第二份 snapshot struct | Redis GET + JSON fixture |
| verify-key 原 total JSON number + `BudgetScope.tier_limits` | Task 1、4 | 不增加重复 tag/v2 total DTO，不改 Control Plane `KeyService` | auth client fixture + JSON key uniqueness |
| tier CRUD/group binding/tier-limit/batch/usage/my-budget APIs | 集成 fixture 与运维依赖 | 不在 P1 补 API | P0 contract tests 先全绿 |
| `call_logs` tier 三列 + scope constraint JSONB + block 五列 `*string` schema（64/8/32/128/8） | Task 8、10 | P0 不负责 runtime 写入；P1 不直接把 string 赋给指针、不猜测 eligible、不把当前配置当历史事实、不把 shadow 当实际 block | event、402 和 rebuild tests |
| `budgetperiod.Resolve(now, periodType, config)` | Task 4、10、12 | 不定义 type/function alias，不用 `time.Local`、不自写 ISO week helper | 上海跨日/跨周/跨年测试 |

## P1 固定交付契约

实现开始后不得自行改名、增加平行 constructor 或在 `middleware` 下暴露第二套等级预算 API。P1 对其他包开放的 constructor/方法固定如下：

```go
// internal/budgetperiod（P0 提供；P1 直接调用，禁止 alias/wrapper）
func Resolve(
	now time.Time,
	periodType budgetdomain.PeriodType,
	config Config,
) (Period, error)

// internal/gateway/budget
func ParseMicroUSD(raw string) (int64, error)
func MicroUSDFromFloat64(value float64) (int64, error)

func NewSnapshotHolder() *SnapshotHolder
func (h *SnapshotHolder) Get() *configsync.BudgetTierSnapshot
func (h *SnapshotHolder) Set(next *configsync.BudgetTierSnapshot) bool

func NewConfigLoader(rdb *redis.Client, holder *SnapshotHolder) *ConfigLoader
func (l *ConfigLoader) Load(ctx context.Context) error
func (l *ConfigLoader) Start(ctx context.Context, reconcileEvery time.Duration)
func (l *ConfigLoader) HandleChangeEvent(event infra.ChangeEvent)

type TierResolver interface {
	Resolve(groupName string) (TierContext, bool)
}
func NewTierResolver(holder *SnapshotHolder) *SnapshotTierResolver

func SetTierContext(ctx *gin.Context, tier TierContext)
func GetTierContext(ctx *gin.Context) (TierContext, bool)
func SetBlockContext(ctx *gin.Context, block BlockContext)
func GetBlockContext(ctx *gin.Context) (BlockContext, bool)
func SetAccountingPlan(ctx *gin.Context, plan *AccountingPlan)
func GetAccountingPlan(ctx *gin.Context) (*AccountingPlan, bool)

type BlockContext struct {
	Code string
	Dimension Dimension
	ScopeType string
	ScopeID string
	PeriodType budgetdomain.PeriodType
}

func BuildAccountingPlan(
	identity *gwmodel.UserIdentity,
	tier TierContext,
	now time.Time,
	snapshot *configsync.BudgetTierSnapshot,
) (*AccountingPlan, error)
func TotalUsedKey(scopeType, scopeID, period string) string
func TierUsedKey(scopeType, scopeID, tierID, period string) string

func NewRedisStore(rdb *redis.Client) *RedisStore
func (s *RedisStore) Check(ctx context.Context, plan *AccountingPlan) CheckResult
func (s *RedisStore) Add(ctx context.Context, plan *AccountingPlan, costMicroUSD int64) error

type LegacyTotalAdapter interface {
	Check(ctx context.Context, plan *AccountingPlan) (*Constraint, error)
	Add(ctx context.Context, keys []string, scopeArgs []any, costUSD float64) error
}
type LegacyTotalFuncs struct {
	CheckFunc func(context.Context, *AccountingPlan) (*Constraint, error)
	AddFunc func(context.Context, []string, []any, float64) error
}
func (f LegacyTotalFuncs) Check(
	ctx context.Context,
	plan *AccountingPlan,
) (*Constraint, error) {
	return f.CheckFunc(ctx, plan)
}
func (f LegacyTotalFuncs) Add(
	ctx context.Context,
	keys []string,
	scopeArgs []any,
	costUSD float64,
) error {
	return f.AddFunc(ctx, keys, scopeArgs, costUSD)
}
type CheckOutcome struct {
	Plan *AccountingPlan
	WouldBlock []Constraint
	FailOpen bool
}
type AccountResult struct {
	V2Error error
	LegacyError error
}
func NewGate(
	holder *SnapshotHolder,
	store *RedisStore,
	legacyTotal LegacyTotalAdapter,
	now func() time.Time,
) *Gate
func (g *Gate) Check(ctx *gin.Context) (*CheckOutcome, error)
func (g *Gate) Account(
	ctx context.Context,
	plan *AccountingPlan,
	costMicroUSD int64,
) AccountResult

type ExceededError struct {
	Code string
	Primary Constraint
	Additional []PublicConstraint
}
type PublicConstraint struct {
	Dimension Dimension
	PeriodType budgetdomain.PeriodType
	SourceLevel string
}
func (e *ExceededError) Error() string
func (e *ExceededError) WriteHTTPResponse(ctx *gin.Context)

// internal/gateway/orchestrator
func NewGroupAwareDispatcherWithBindingAndTier(
	scheduler Scheduler,
	nameCache *cache.GroupNameCache,
	bindingClient *binding.Client,
	tierResolver budget.TierResolver,
) *GroupAwareDispatcher

// internal/gateway/handler
func PrepareAuthorizedBudget(
	ctx *gin.Context,
	originalGroup string,
	dispatch func() error,
	gate *budget.Gate,
) error

// internal/gateway/observability
func (c *Collector) NewUsageEvent(ctx *gin.Context) UsageEvent
func (event UsageEvent) ToCallLog() (observabilitymodel.CallLog, error)

// internal/controlplane/service
func NewBudgetCounterRebuildService(
	db *gorm.DB,
	rdb *redis.Client,
	periodConfig budgetperiod.Config,
) *BudgetCounterRebuildService
func (s *BudgetCounterRebuildService) RebuildCurrentPeriods(
	ctx context.Context,
	now time.Time,
) (*RebuildV2Result, error)
```

这些都是进程内 Go 契约，不新增 HTTP API。

---

### Task 1: 固定金额与 verify-key 领域契约

**Files:**
- Create: `internal/gateway/budget/money.go`
- Create: `tests/gateway/budget/money_test.go`
- Modify: `internal/gateway/model/identity.go`
- Modify: `internal/gateway/infra/auth_client.go`
- Modify: `tests/gateway/infra/authclient/auth_client_test.go`

- [ ] **Step 1: Write the failing tests**

在 `tests/gateway/budget/money_test.go` 写表驱动测试，明确半入、半下、负数拒绝和溢出；`gate_test.go` 另断言 total `-1` 在转换前按“不限额”跳过：

```go
package budget_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/stretchr/testify/require"
)

func TestParseMicroUSD_ROUND_HALF_UP(t *testing.T) {
	tests := []struct {
		name, raw string
		want      int64
		wantErr   bool
	}{
		{"六位原值", "1.234567", 1234567, false},
		{"第七位五进一", "1.2345675", 1234568, false},
		{"小于半微美元舍去", "0.00000049", 0, false},
		{"正半微美元进一", "0.00000050", 1, false},
		{"负数额度拒绝", "-0.000001", 0, true},
		{"非数字拒绝", "12.3x", 0, true},
		{"int64溢出拒绝", "9223372036854.775808", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := budget.ParseMicroUSD(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
```

在 auth client 测试加入 P0 verify-key 真实 payload，断言 `amount_usd`/`daily_amount_usd`/`weekly_amount_usd` 解码为现有 `float64`，`tier_limits[0].effective_limit_usd` 原样保留为字符串。再把解码后的 `BudgetScope` marshal 回 JSON，使用 `strings.Count` 断言三个 total key 各出现一次且序列化结果不存在 `"_v2"`；这是 wire behavior 测试。同时运行 P0 contract tests，但不修改 P0 handler/service/contract。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/budget ./tests/gateway/infra/authclient -run 'Test(ParseMicroUSD|AuthClient.*TierLimit)' -count=1`

Expected: FAIL，分别出现 `no required module provides package github.com/example/llm-gateway/internal/gateway/budget` 和 `BudgetScope.TierLimits undefined`。

- [ ] **Step 3: Write minimal implementation**

`money.go` 使用 `math/big`，不能通过 `float64 * 1e6`：

```go
package budget

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const microsPerUSD int64 = 1_000_000

func ParseMicroUSD(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty USD amount")
	}
	r, ok := new(big.Rat).SetString(raw)
	if !ok || r.Sign() < 0 {
		return 0, fmt.Errorf("invalid non-negative USD amount %q", raw)
	}
	scaled := new(big.Rat).Mul(r, big.NewRat(microsPerUSD, 1))
	q := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	rem := new(big.Int).Rem(scaled.Num(), scaled.Denom())
	if new(big.Int).Lsh(rem, 1).Cmp(scaled.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, fmt.Errorf("USD amount overflows int64 micro-USD")
	}
	return q.Int64(), nil
}

func MicroUSDFromFloat64(v float64) (int64, error) {
	return ParseMicroUSD(strconv.FormatFloat(v, 'f', -1, 64))
}
```

Gateway 镜像以下 P0 契约；现有 total 字段和 JSON number 类型原样保留，只新增 `TierLimits`：

```go
type TierLimit struct {
	TierID            string     `json:"tier_id"`
	TierName          string     `json:"tier_name"`
	PeriodType        string     `json:"period_type"`
	BaseLimitUSD      string     `json:"base_limit_usd"`
	EffectiveLimitUSD string     `json:"effective_limit_usd"`
	Enabled           bool       `json:"enabled"`
	EffectiveAt       time.Time  `json:"effective_at"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
}

type BudgetScope struct {
	ScopeType       string      `json:"scope_type"`
	ScopeID         string      `json:"scope_id"`
	AmountUSD       float64     `json:"amount_usd"`
	DailyAmountUSD  float64     `json:"daily_amount_usd"`
	WeeklyAmountUSD float64     `json:"weekly_amount_usd"`
	Label           string      `json:"label"`
	TierLimits      []TierLimit `json:"tier_limits,omitempty"`
}
```

P0 的 `KeyService.buildBudgetChain` 已负责批量读取已配置 tier limits、输出 6 位小数字符串且不为缺失周期生成“无限”项。P1 仅保证 AuthClient 解码和 Gateway mirror 与该 JSON 一致。total 的微美元转换延迟到 `BuildAccountingPlan`：

```go
func totalLimitMicroUSD(raw float64) (int64, bool, error) {
	if raw < 0 {
		return 0, false, nil
	}
	value, err := MicroUSDFromFloat64(raw)
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}
```

`raw < 0` 保持现有“不限额”语义，不生成 constraint；`raw == 0` 生成 total `$0` constraint；禁止在 AuthClient DTO 增加字符串 total 镜像。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/budget ./tests/gateway/infra/authclient -run 'Test(ParseMicroUSD|AuthClient.*TierLimit)' -count=1`

Expected: PASS；再运行 `go test ./tests/contract/... -count=1`，Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/budget/money.go internal/gateway/model/identity.go internal/gateway/infra/auth_client.go tests/gateway/budget/money_test.go tests/gateway/infra/authclient/auth_client_test.go
git commit -m "feat(budget): add micro-usd tier limit contracts"
```

### Task 2: 实现独立等级快照 holder、loader 与 last-known-good

**Files:**
- Create: `internal/gateway/budget/snapshot_holder.go`
- Create: `internal/gateway/budget/config_loader.go`
- Create: `tests/gateway/budget/snapshot_loader_test.go`
- Modify: `cmd/gateway/main.go`

- [ ] **Step 1: Write the failing tests**

测试必须覆盖初次加载、revision 倒退拒绝、坏 JSON 保留旧值、Redis 断线保留旧值、30 秒 reconcile，以及现有 Subscriber 解码后的 `infra.ChangeEvent`；事件 payload 必须是 JSON 且 `type=budget_tier_snapshot_updated`，不能用裸字符串：

```go
func TestConfigLoader_InvalidSnapshotKeepsLastKnownGood(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	loader := budget.NewConfigLoader(rdb, holder)

	valid := `{"schema_version":1,"revision":7,"updated_at":"2026-07-11T08:00:00Z","master_enabled":true,"accounting_enabled":true,"positive_enforcement_enabled":false,"zero_block_enabled":true,"shadow_enabled":true,"v2_total_enforcement_enabled":false,"legacy_total_write_enabled":true,"timezone":"Asia/Shanghai","week_start":"monday","group_tiers":{}}`
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, valid))
	require.NoError(t, loader.Load(context.Background()))
	require.Equal(t, int64(7), holder.Get().Revision)

	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, `{"revision":`))
	require.Error(t, loader.Load(context.Background()))
	require.Equal(t, int64(7), holder.Get().Revision)
}

func TestConfigLoader_HandlesStructuredChangeEvent(t *testing.T) {
	eventJSON := `{
		"type":"budget_tier_snapshot_updated",
		"resource_type":"budget_tier_snapshot",
		"resource_id":"global",
		"snapshot_version":8,
		"timestamp":"2026-07-11T08:01:00Z"
	}`
	var event infra.ChangeEvent
	require.NoError(t, json.Unmarshal([]byte(eventJSON), &event))
	require.Equal(t, configsync.BudgetTierSnapshotUpdated, event.Type)

	next := `{"schema_version":1,"revision":8,"updated_at":"2026-07-11T08:01:00Z","master_enabled":true,"accounting_enabled":true,"positive_enforcement_enabled":false,"zero_block_enabled":true,"shadow_enabled":true,"v2_total_enforcement_enabled":false,"legacy_total_write_enabled":true,"timezone":"Asia/Shanghai","week_start":"monday","group_tiers":{}}`
	require.NoError(t, mr.Set(configsync.BudgetTierSnapshotKey, next))
	loader.HandleChangeEvent(event)
	require.Equal(t, int64(8), holder.Get().Revision)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/budget -run 'Test(ConfigLoader|SnapshotHolder)' -count=1`

Expected: FAIL with `undefined: budget.NewSnapshotHolder`。

- [ ] **Step 3: Write minimal implementation**

快照类型、Redis key、Save 和 publisher 均由 P0 的 `internal/configsync/budget_tier_snapshot.go` 与 Control Plane `BudgetTierSnapshotPublisher` 提供。P1 loader 只能 GET、解码和校验：

```go
raw, err := l.rdb.Get(ctx, configsync.BudgetTierSnapshotKey).Bytes()
if err != nil {
	return fmt.Errorf("load budget tier snapshot: %w", err)
}
var snap configsync.BudgetTierSnapshot
if err := json.Unmarshal(raw, &snap); err != nil {
	return fmt.Errorf("decode budget tier snapshot: %w", err)
}
if err := validateSnapshot(&snap); err != nil {
	return err
}
if !l.holder.Set(&snap) {
	return fmt.Errorf("budget tier snapshot revision regressed: %d", snap.Revision)
}
```

`validateSnapshot` 要求 schema=1、timezone=`Asia/Shanghai`、week_start=`monday`，并拒绝 `shadow_enabled && positive_enforcement_enabled` 的矛盾快照，使 Gateway 保留 last-known-good。

holder 只接受 schema 1 且 revision 不倒退：

```go
type SnapshotHolder struct {
	current atomic.Pointer[configsync.BudgetTierSnapshot]
}

func NewSnapshotHolder() *SnapshotHolder { return &SnapshotHolder{} }
func (h *SnapshotHolder) Get() *configsync.BudgetTierSnapshot { return h.current.Load() }
func (h *SnapshotHolder) Set(next *configsync.BudgetTierSnapshot) bool {
	if next == nil || next.SchemaVersion != 1 {
		return false
	}
	for {
		old := h.current.Load()
		if old != nil && next.Revision < old.Revision {
			return false
		}
		if h.current.CompareAndSwap(old, next) {
			return true
		}
	}
}
```

loader 签名：

```go
func NewConfigLoader(rdb *redis.Client, holder *SnapshotHolder) *ConfigLoader
func (l *ConfigLoader) Load(ctx context.Context) error
func (l *ConfigLoader) Start(ctx context.Context, reconcileEvery time.Duration)
func (l *ConfigLoader) HandleChangeEvent(ev infra.ChangeEvent)
```

`HandleChangeEvent` 只读取已由 `infra.Subscriber.handleMessage` JSON 解码的 `Type`：

```go
func (l *ConfigLoader) HandleChangeEvent(event infra.ChangeEvent) {
	if event.Type != configsync.BudgetTierSnapshotUpdated {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.Load(ctx); err != nil {
		slog.Error("reload budget tier snapshot failed",
			"snapshot_version", event.SnapshotVersion,
			"error", err,
		)
	}
}
```

`Start` 立即 Load，一条 goroutine 每 30 秒 reconcile；复用主进程 `infra.Subscriber.AddCallback(loader.HandleChangeEvent)`，不再创建第二个 Pub/Sub 连接，也不自行订阅或解析 Redis message payload。`Load` 只有完整校验成功后才 `holder.Set`。`cmd/gateway/main.go` 创建独立 holder/loader、注册 callback 并启动 reconcile。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/budget -run 'Test(ConfigLoader|SnapshotHolder)' -count=1 && go test -race ./tests/gateway/budget -run TestSnapshotHolder -count=1`

Expected: 两条命令 PASS，`-race` 无 data race。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/budget/snapshot_holder.go internal/gateway/budget/config_loader.go tests/gateway/budget/snapshot_loader_test.go cmd/gateway/main.go
git commit -m "feat(budget): load tier snapshot atomically"
```

### Task 3: 实现 TierResolver 并固定请求等级

**Files:**
- Create: `internal/gateway/budget/context.go`
- Create: `internal/gateway/budget/tier_resolver.go`
- Create: `tests/gateway/budget/tier_resolver_test.go`
- Modify: `internal/gateway/orchestrator/group_dispatcher.go`
- Modify: `tests/gateway/orchestrator/group_auth_test.go`

- [ ] **Step 1: Write the failing tests**

覆盖未分级、active、disabled、archived、快照后续热更新不改变已固定 context，以及“403 前不调用 resolver”。disabled/archived 仍必须返回 ID/name context，只把两个 eligibility 设 false：

```go
func TestTierResolver_FixesRequestSnapshot(t *testing.T) {
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1,
		Revision: 1,
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
			"claude-opus": {
				TierID: uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876"),
				TierName: "高级", Enabled: true,
			},
		},
	})
	resolver := budget.NewTierResolver(holder)
	got, ok := resolver.Resolve("claude-opus")
	require.True(t, ok)
	require.Equal(t, budget.TierContext{
		TierID: "00bb2e65-9c67-45f7-b061-d297a3944876",
		TierName: "高级", GroupName: "claude-opus", SnapshotRevision: 1,
		AccountingEligible: true, EnforcementEligible: true,
	}, got)

	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 2,
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{},
	})
	require.Equal(t, "00bb2e65-9c67-45f7-b061-d297a3944876", got.TierID)
}

func TestTierResolver_DisabledOrArchivedTierKeepsReportingSnapshot(t *testing.T) {
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 3,
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
			"disabled-group": {
				TierID: uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876"),
				TierName: "高级", Enabled: false,
			},
			"archived-group": {
				TierID: uuid.MustParse("7534a819-ceb7-4ce8-a62f-bf78627eabb7"),
				TierName: "归档等级", Enabled: false,
			},
		},
	})
	resolver := budget.NewTierResolver(holder)
	for _, group := range []string{"disabled-group", "archived-group"} {
		got, ok := resolver.Resolve(group)
		require.True(t, ok)
		require.NotEmpty(t, got.TierID)
		require.NotEmpty(t, got.TierName)
		require.False(t, got.AccountingEligible)
		require.False(t, got.EnforcementEligible)
	}
	_, ok := resolver.Resolve("unclassified-group")
	require.False(t, ok)
}
```

在 group auth 测试注入 counting resolver；不可访问组断言 `calls == 0` 且返回 403 对应错误。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/budget ./tests/gateway/orchestrator -run 'Test(TierResolver|Dispatch_AuthorizationBeforeTier)' -count=1`

Expected: FAIL with `undefined: budget.NewTierResolver` 或 constructor 参数不匹配。

- [ ] **Step 3: Write minimal implementation**

```go
const CtxKeyTierContext = "budget_tier_context"

type TierContext struct {
	TierID          string
	TierName        string
	GroupName       string
	SnapshotRevision int64
	AccountingEligible bool
	EnforcementEligible bool
}

type TierResolver interface {
	Resolve(groupName string) (TierContext, bool)
}

type SnapshotTierResolver struct{ holder *SnapshotHolder }

func NewTierResolver(holder *SnapshotHolder) *SnapshotTierResolver {
	return &SnapshotTierResolver{holder: holder}
}

func (r *SnapshotTierResolver) Resolve(groupName string) (TierContext, bool) {
	snap := r.holder.Get()
	if snap == nil {
		return TierContext{}, false
	}
	entry, ok := snap.GroupTiers[groupName]
	if !ok || entry.TierID == uuid.Nil {
		return TierContext{}, false
	}
	return TierContext{
		TierID: entry.TierID.String(), TierName: entry.TierName,
		GroupName: groupName, SnapshotRevision: snap.Revision,
		AccountingEligible: entry.Enabled,
		EnforcementEligible: entry.Enabled,
	}, true
}
```

P0 publisher 对 archived tier 也保留 group mapping，并和 disabled 一样下发 `enabled=false`；P1 不需要新增 archive 字段。`ok=false` 只表示 group 未分级（无 map entry 或 TierID 为空），绝不能表示“有等级但不可计费”。

`GroupAwareDispatcher` 增加 `tierResolver budget.TierResolver` 和精确 constructor：

```go
func NewGroupAwareDispatcherWithBindingAndTier(
	sch Scheduler,
	nc *cache.GroupNameCache,
	bindClient *binding.Client,
	tierResolver budget.TierResolver,
) *GroupAwareDispatcher
```

`Dispatch` 的顺序必须是：

```go
if err := authz.AuthorizeGroupAccess(c.Request.Context(), id, d.bindingCheck, modelName); err != nil {
	return nil, err
}
if d.tierResolver != nil {
	if tier, ok := d.tierResolver.Resolve(modelName); ok {
		budget.SetTierContext(c, tier)
	}
}
decision, err := d.scheduler.Route(modelName, req)
```

`HandleFailover` 只能更新 routed model/provider，不能再次解析或覆盖 tier context。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/budget ./tests/gateway/orchestrator -run 'Test(TierResolver|Dispatch_)' -count=1`

Expected: PASS，并确认 unauthorized case 的 resolver call count 为 0。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/budget/context.go internal/gateway/budget/tier_resolver.go internal/gateway/orchestrator/group_dispatcher.go tests/gateway/budget/tier_resolver_test.go tests/gateway/orchestrator/group_auth_test.go
git commit -m "feat(budget): resolve tier after group authorization"
```

### Task 4: 构造不可变 AccountingPlan 与静态 `$0` 判断

**Files:**
- Modify: `internal/gateway/budget/context.go`
- Create: `internal/gateway/budget/gate.go`
- Create: `tests/gateway/budget/gate_test.go`
- Create: `tests/gateway/budget/period_contract_test.go`

- [ ] **Step 1: Write the failing tests**

测试链上总预算和等级预算的日/周/月独立展开、未配置跳过、disabled 跳过、`effective_at` 未到跳过、`$0` 按“静态零优先，再按 reset 最晚”排序：

```go
func TestBuildAccountingPlan_ZeroTierIsStaticBlock(t *testing.T) {
	zero := "0.000000"
	id := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 500.0,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", TierName: "高级", PeriodType: "monthly",
			EffectiveLimitUSD: zero, Enabled: true,
			EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		}},
	}}}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1,
		MasterEnabled: true,
		AccountingEnabled: true,
		Timezone: "Asia/Shanghai",
		WeekStart: "monday",
	}
	plan, err := budget.BuildAccountingPlan(id, budget.TierContext{
		TierID: "tier-1", TierName: "高级", GroupName: "claude-opus",
	}, time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC), snapshot)
	require.NoError(t, err)
	require.Len(t, plan.Constraints, 2)
	require.True(t, plan.Constraints[1].StaticZero)
	require.Equal(t, "budget:v2:tier:used:member:u-1:tier-1:2026-07", plan.Constraints[1].Key)
}

func TestBuildAccountingPlan_DisabledTierReportsWithoutAccounting(t *testing.T) {
	zero := "0.000000"
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 500.0,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "00bb2e65-9c67-45f7-b061-d297a3944876",
			TierName: "高级", PeriodType: "monthly",
			EffectiveLimitUSD: zero, Enabled: true,
			EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		}},
	}}}
	tier := budget.TierContext{
		TierID: "00bb2e65-9c67-45f7-b061-d297a3944876",
		TierName: "高级",
		GroupName: "claude-opus",
		AccountingEligible: false,
		EnforcementEligible: false,
	}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		ZeroBlockEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	plan, err := budget.BuildAccountingPlan(identity, tier, now, snapshot)
	require.NoError(t, err)
	require.NotNil(t, plan.Tier)
	require.Equal(t, tier.TierID, plan.Tier.TierID)
	require.False(t, plan.TierAccountingEligible)
	require.False(t, plan.TierEnforcementEligible)
	require.NotEmpty(t, plan.Constraints)
	for _, constraint := range plan.Constraints {
		require.Equal(t, budget.DimensionTotal, constraint.Dimension)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/budget -run 'TestBuildAccountingPlan' -count=1`

Expected: FAIL with `undefined: budget.BuildAccountingPlan`。

- [ ] **Step 3: Write minimal implementation**

```go
type Dimension string
const (
	DimensionTotal Dimension = "total"
	DimensionTier  Dimension = "tier"
)

type Constraint struct {
	Dimension Dimension
	ScopeType string
	ScopeID string
	Label string
	TierID string
	TierName string
	PeriodType string
	Period string
	Key string
	LimitMicroUSD int64
	ResetAt time.Time
	StaticZero bool
}

type AccountingPlan struct {
	Tier *TierContext
	Constraints []Constraint
	LegacyKeys []string
	LegacyScopeArgs []any
	TierAccountingEligible bool
	TierEnforcementEligible bool
	RedisCheckSucceeded bool
}

type BlockContext struct {
	Code string
	Dimension Dimension
	ScopeType string
	ScopeID string
	PeriodType budgetdomain.PeriodType
}

func BuildAccountingPlan(id *gwmodel.UserIdentity, tier TierContext, now time.Time, snapshot *configsync.BudgetTierSnapshot) (*AccountingPlan, error)
func TotalUsedKey(scopeType, scopeID, period string) string
func TierUsedKey(scopeType, scopeID, tierID, period string) string
```

周期只允许调用 P0 共享包：

```go
periodConfig := budgetperiod.Config{
	Timezone: snapshot.Timezone,
	WeekStart: snapshot.WeekStart,
}
periodType := budgetdomain.PeriodType(limit.PeriodType)
window, err := budgetperiod.Resolve(now, periodType, periodConfig)
if err != nil {
	return nil, fmt.Errorf("resolve %s budget period: %w", limit.PeriodType, err)
}
constraint.Period = window.Key
constraint.ResetAt = window.ResetAt
```

`tests/gateway/budget/period_contract_test.go` 固定验证上海时间周日 23:59 到周一 00:00、UTC 跨日、ISO 跨年周，且临时设置 `time.Local = time.FixedZone("wrong", -7*3600)` 后结果不变。禁止 `time.Now().Format`、`time.Now().ISOWeek()` 或 `time.Local` 出现在 Gateway/重建实现。

P1 只能直接调用 P0 的 `budgetperiod.Resolve(now time.Time, periodType budgetdomain.PeriodType, config budgetperiod.Config) (budgetperiod.Period, error)`；不得声明 `type PeriodType = ...`、`ResolvePeriod`、`CurrentPeriod` 或任何 wrapper/alias。

`Gate.Check` 在一次 holder `Get()` 后把该 snapshot 同时传给 `BuildAccountingPlan` 和开关决策，确保 plan、period 和 eligible 来自同一 revision；不得在同一请求内二次读取 holder。无 snapshot 时保持 legacy 总预算路径，跳过全部 v2/tier 行为并记录 loader-not-ready 指标。

总预算仍为每层硬约束。tier ID/name 无论 enabled 与否都保留在 `AccountingPlan.Tier` 供 call log；只有 `tier.AccountingEligible && tier.EnforcementEligible` 且该层存在同 tier、enabled、已生效的 limit 时才生成等级 constraint。`TierAccountingEligible` 还必须满足 snapshot 的 `master_enabled && accounting_enabled`，`TierEnforcementEligible` 还必须满足 `master_enabled`；影子不改变 accounting eligible，只改变是否实际阻断。disabled/archived tier 两者均为 false，因此不生成 tier Redis key、不执行 `$0` 或正数 tier 检查，但报表快照仍写 ID/name。

`Gate.checkStatic(plan, snapshot)` 是包内 helper，只看内存，不扩大公开 API：

```go
func (g *Gate) checkStatic(plan *AccountingPlan, snap *configsync.BudgetTierSnapshot) *ExceededError {
	if snap == nil || !snap.ZeroBlockEnabled {
		return nil
	}
	for _, c := range plan.Constraints {
		if c.Dimension == DimensionTier && c.StaticZero {
			return newExceededError("tier_budget_exceeded", c, nil)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/budget -run 'Test(BuildAccountingPlan|Gate_Static)' -count=1`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/budget/context.go internal/gateway/budget/gate.go tests/gateway/budget/gate_test.go tests/gateway/budget/period_contract_test.go
git commit -m "feat(budget): build total and tier accounting plans"
```

### Task 5: 实现 Redis v2 总+等级原子检查与累计

**Files:**
- Create: `internal/gateway/budget/redis_store.go`
- Create: `tests/gateway/budget/redis_store_test.go`

- [ ] **Step 1: Write the failing tests**

用 miniredis 覆盖：总预算命中、等级预算命中、同次返回多个 blockers、整数累计无浮点漂移、只累计 plan 中启用项、TTL、Redis down：

```go
func TestRedisStore_AddUsesIntegerMicroUSD(t *testing.T) {
	mr := miniredis.RunT(t)
	store := budget.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{
		{Key: "budget:v2:used:member:u-1:2026-07", LimitMicroUSD: 10_000_000, PeriodType: "monthly"},
		{Key: "budget:v2:tier:used:member:u-1:t-1:2026-07", LimitMicroUSD: 2_000_000, PeriodType: "monthly"},
	}}
	require.NoError(t, store.Add(context.Background(), plan, 100_001))
	require.NoError(t, store.Add(context.Background(), plan, 200_002))
	require.Equal(t, "300003", mr.MustGet(plan.Constraints[0].Key))
	require.Equal(t, "300003", mr.MustGet(plan.Constraints[1].Key))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/budget -run 'TestRedisStore' -count=1`

Expected: FAIL with `undefined: budget.NewRedisStore`。

- [ ] **Step 3: Write minimal implementation**

检查 Lua 只做 GET、整数比较，不在 Lua 中 `tonumber` used/limit 后参与加法；为了规避 Lua 5.1 double 对超过 2^53 整数的精度问题，比较使用十进制字符串长度+字典序：

```lua
local function ge_decimal(a, b)
  a = string.gsub(a or '0', '^0+', '')
  b = string.gsub(b or '0', '^0+', '')
  if a == '' then a = '0' end
  if b == '' then b = '0' end
  if string.len(a) ~= string.len(b) then
    return string.len(a) > string.len(b)
  end
  return a >= b
end

local blocked = {}
for i = 1, #KEYS do
  local used = redis.call('GET', KEYS[i]) or '0'
  if ge_decimal(used, ARGV[i]) then
    table.insert(blocked, tostring(i))
  end
end
return blocked
```

累计 Lua：

```lua
for i = 1, #KEYS do
  redis.call('INCRBY', KEYS[i], ARGV[1])
  if redis.call('TTL', KEYS[i]) < 0 then
    redis.call('EXPIRE', KEYS[i], ARGV[i + 1])
  end
end
return 1
```

公开 API：

```go
type CheckResult struct {
	Blockers []Constraint
	RedisError error
}

func NewRedisStore(rdb *redis.Client) *RedisStore
func (s *RedisStore) Check(ctx context.Context, plan *AccountingPlan) CheckResult
func (s *RedisStore) Add(ctx context.Context, plan *AccountingPlan, costMicroUSD int64) error
```

`Check` 只传正数 constraints，`$0` 已由内存处理；Redis error 原样返回给 Gate 决策。`Add` 使用一次 Lua 同时累计所有 total+tier keys，不能循环逐 key 写。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/budget -run 'TestRedisStore' -count=1`

Expected: PASS；整数断言必须精确相等，禁止 `InDelta`。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/budget/redis_store.go tests/gateway/budget/redis_store_test.go
git commit -m "feat(budget): add atomic redis v2 accounting"
```

### Task 6: 落地权限先于预算及六类协议一致性

**Files:**
- Modify: `internal/gateway/handler/chat.go`
- Modify: `internal/gateway/handler/responses.go`
- Modify: `internal/gateway/handler/messages.go`
- Modify: `internal/gateway/handler/embeddings.go`
- Modify: `internal/gateway/handler/rerank.go`
- Modify: `internal/gateway/budget/gate.go`
- Create: `tests/gateway/handler/model_tier_budget_test.go`
- Modify: `internal/gateway/server/router.go`

- [ ] **Step 1: Write the failing tests**

构建一个表驱动组件测试，六个 case 为 Chat、Responses、Messages、Embeddings、Rerank text、Rerank VL。每个 case 都验证调用顺序 `authorize -> tier -> budget -> quota -> upstream`；另加 unauthorized + exhausted，期望 403 且 Redis key 不被读取：

```go
tests := []struct {
	name, path, body string
}{
	{"Chat", "/v1/chat/completions", `{"model":"premium","messages":[{"role":"user","content":"hi"}]}`},
	{"Responses", "/v1/responses", `{"model":"premium","input":"hi"}`},
	{"Messages", "/v1/messages", `{"model":"premium","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`},
	{"Embeddings", "/v1/embeddings", `{"model":"premium","input":"hi"}`},
	{"Rerank", "/v1/rerank", `{"model":"premium","query":"q","documents":["d"]}`},
	{"VL Rerank", "/v1/rerank", `{"model":"premium-vl","query":{"image":"https://x/q.png"},"documents":[{"text":"d"}]}`},
}
```

每个测试用 recording fakes 断言顺序；禁止通过复制六份预算实现来让测试通过。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/handler -run 'TestModelTierBudget_' -count=1`

Expected: FAIL；当前 handler 在 `Dispatch`（授权）之前调用 `middleware.Reserve`，记录顺序实际为 `budget -> authorize`。

- [ ] **Step 3: Write minimal implementation**

增加一个所有 handler 共用的 helper：

```go
func PrepareAuthorizedBudget(
	c *gin.Context,
	originalGroup string,
	dispatch func() error,
	gate *budget.Gate,
) error {
	if err := dispatch(); err != nil {
		return err
	}
	if gate == nil {
		return nil
	}
	_, err := gate.Check(c)
	return err
}
```

每个 handler 的固定顺序：

1. 完整解析并校验 body，保存原始 `model`。
2. `groupDispatch.Dispatch` 执行授权、固定 tier、路由。
3. `budgetGate.Check(c)`；402 由统一 `budget.ExceededError.WriteHTTPResponse` 写入。
4. 现有 model quota gate。
5. 调用 upstream。

删除五个 handler 对 `middleware.Reserve` 的直接调用。文本/VL Rerank 仍走同一个 `RerankHandler.Handle` 和同一个 budget gate，差异仅保留在 capability check；tier 始终来自原始组名，不来自 `decision.ModelType` 或 routed model。

`server.Setup` 接收 `tierHolder *budget.SnapshotHolder`，创建 resolver、store、gate 后注入所有 handler；passthrough 不注入 gate。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/handler ./tests/gateway/orchestrator -run 'Test(ModelTierBudget_|Dispatch_)' -count=1`

Expected: PASS；unauthorized case 必须为 403，Redis command count 为 0。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/handler/chat.go internal/gateway/handler/responses.go internal/gateway/handler/messages.go internal/gateway/handler/embeddings.go internal/gateway/handler/rerank.go internal/gateway/budget/gate.go internal/gateway/server/router.go tests/gateway/handler/model_tier_budget_test.go
git commit -m "fix(budget): authorize groups before budget checks"
```

### Task 7: 完成 `$0`/正数 fail policy、shadow 与双写

**Files:**
- Modify: `internal/gateway/budget/gate.go`
- Modify: `internal/gateway/middleware/budget_check.go`
- Modify: `internal/gateway/middleware/budget_reserve.go`
- Modify: `internal/gateway/middleware/usage_collector.go`
- Modify: `internal/gateway/server/router.go`
- Modify: `tests/gateway/budget/gate_test.go`
- Modify: `tests/gateway/middleware/budget_reserve_test.go`

- [ ] **Step 1: Write the failing tests**

覆盖以下矩阵：

```text
master_enabled=false                                      => tier 快照仍写日志；不检查、不累计 tier；eligible=false
master_enabled=true + zero_block_enabled=true + $0        => Redis down 仍返回 402 tier_budget_exceeded
master_enabled=true + accounting_enabled=false            => 不写 tier v2；非 $0 tier 不检查；eligible=false
disabled/archived tier + 已配置 $0/正数 limit             => 保留 ID/name 日志；不检查、不阻断、不写 tier v2
master_enabled=true + accounting_enabled=true
  + shadow_enabled=true
  + positive_enforcement_enabled=false + positive exhausted
                                                        => pass，写 tier v2，记录 would-block，eligible=true
master_enabled=true + shadow_enabled=false
  + positive_enforcement_enabled=true + positive exhausted
                                                        => 402 tier_budget_exceeded
positive constraint + Redis down                         => pass，RedisCheckSucceeded=false，本请求不写 v2
v2_total_enforcement_enabled=false + total exhausted     => 由旧总预算结果决定
v2_total_enforcement_enabled=true + total exhausted      => 402 budget_exceeded
check success                                             => 总预算总写 v2；tier 仅 accounting_enabled=true 时写 v2
legacy_total_write_enabled=true                           => 同时写旧 budget:used:* float key
legacy_total_write_enabled=false                          => 不再写旧 key
```

disabled 与 archived 各做一个完整行为 case：verify-key 中即使存在 `$0`/正数 tier limit，请求也放行到 upstream；miniredis 断言没有对应 tier key 的 GET/INCRBY，`GetBlockContext` 返回 false，成功后仅 total counter 增加。随后断言 UsageEvent/CallLog 仍保留原 tier ID/name 且 `budget_tier_accounting_eligible=false`。

`ExceededError` 响应断言 code 精确区分 `budget_exceeded` 与 `tier_budget_exceeded`，公开 body 不包含 scope ID/limit/used。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/budget ./tests/gateway/middleware -run 'Test(Gate_|BudgetDualWrite_)' -count=1`

Expected: FAIL，当前 Redis down 无法区分 `$0`，且只支持旧浮点总预算。

- [ ] **Step 3: Write minimal implementation**

Gate API：

```go
type CheckOutcome struct {
	Plan *AccountingPlan
	WouldBlock []Constraint
	FailOpen bool
}

func (g *Gate) Check(c *gin.Context) (*CheckOutcome, error)
func (g *Gate) Account(ctx context.Context, plan *AccountingPlan, costMicroUSD int64) AccountResult
```

按“P1 固定交付契约”实现 `NewGate`；`server.Setup` 使用 adapter，不让 canonical `budget` 包反向依赖 middleware：

```go
gate := budget.NewGate(
	holder,
	budget.NewRedisStore(rdb),
	budget.LegacyTotalFuncs{
		CheckFunc: func(ctx context.Context, plan *budget.AccountingPlan) (*budget.Constraint, error) {
			return middleware.CheckLegacyBudget(ctx, rdb, plan)
		},
		AddFunc: func(ctx context.Context, keys []string, scopeArgs []any, costUSD float64) error {
			return middleware.BudgetAdd(ctx, rdb, keys, scopeArgs, costUSD)
		},
	},
	time.Now,
)
```

legacy adapter 固定签名：

```go
func CheckLegacyBudget(
	ctx context.Context,
	rdb *redis.Client,
	plan *budget.AccountingPlan,
) (*budget.Constraint, error)
func BudgetAdd(
	ctx context.Context,
	rdb *redis.Client,
	keys []string,
	scopeArgs []any,
	realCost float64,
) error
```

旧总预算真实阻断必须返回首要 total constraint，Gate 据此写 `budget_block_code=budget_exceeded` 与其 scope/period；不能只返回无结构字符串错误。

`Check` 始终处理总预算，并在进入 Redis 前通过 `SetAccountingPlan` 固定 plan，返回同一指针的 `CheckOutcome`；tier 分支先应用规范开关状态机：

```go
func tierPolicy(s *configsync.BudgetTierSnapshot) TierPolicy {
	if s == nil || !s.MasterEnabled {
		return TierPolicy{}
	}
	return TierPolicy{
		Account:         s.AccountingEnabled,
		BlockZero:       s.ZeroBlockEnabled,
		ComputeWouldBlock: s.ShadowEnabled,
		BlockPositive:   s.PositiveEnforcementEnabled && !s.ShadowEnabled,
	}
}
```

loader 校验并拒绝 `shadow_enabled && positive_enforcement_enabled`、`positive_enforcement_enabled && !accounting_enabled`。关闭不是影子：`master_enabled=false` 不执行 tier Redis GET/INCRBY，也不产生 would-block；影子必须累计 tier 并计算 would-block。

Redis error 时设置 `plan.RedisCheckSucceeded=false`、`budget.fail_open=true` 并放行所有正数约束。成功时为 true；shadow 把正数 tier blockers 写入 context `budget_shadow_would_block`，但不阻断。非 shadow 时：

- total blocker 仅在 `V2TotalEnforcementEnabled` 时阻断；
- tier 正数 blocker 仅在 `PositiveEnforcementEnabled` 时阻断；
- 多 blocker 首要原因按 static zero、`ResetAt` 最晚、dimension/scope 稳定排序；其余只返回脱敏的 `additional_constraints`（dimension、period、source_level，不含金额和 ID）。

旧 `BudgetAdd` 改为返回 error：

```go
func BudgetAdd(ctx context.Context, rdb *redis.Client, keys []string, scopeArgs []any, realCost float64) error
```

`Account` 只有 `plan.RedisCheckSucceeded` 才写 v2；total v2 没有额外 write 开关，成功请求始终写 total v2，tier v2 仅当请求开始时 `master_enabled && accounting_enabled` 且存在适用 tier constraint 时写。v2 一次 Lua 失败不回退逐 key。legacy 总预算双写只由 `legacy_total_write_enabled` 控制；legacy 失败与 v2 失败分别记录，不让一个失败阻止另一个。Usage collector 对 `<400` 且 cost > 0 调用 `Gate.Account`，对 `>=400` 完全不记账。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/budget ./tests/gateway/middleware -run 'Test(Gate_|BudgetDualWrite_)' -count=1`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/budget/gate.go internal/gateway/middleware/budget_check.go internal/gateway/middleware/budget_reserve.go internal/gateway/middleware/usage_collector.go internal/gateway/server/router.go tests/gateway/budget/gate_test.go tests/gateway/middleware/budget_reserve_test.go
git commit -m "feat(budget): enforce tier policy with shadow dual-write"
```

### Task 8: 写入 call_logs 等级与真实阻断快照并保证流式完成后记账

**Files:**
- Modify: `internal/gateway/observability/event.go`
- Modify: `internal/gateway/observability/collector.go`
- Modify: `internal/gateway/middleware/usage_collector.go`
- Create: `tests/gateway/observability/tier_snapshot_test.go`
- Modify: `tests/gateway/orchestrator/stream_usage_test.go`

- [ ] **Step 1: Write the failing tests**

测试请求固定 `tier-1/高级` 后热更新为 `tier-2/旗舰`，最终 call log 仍为 tier-1；未分级两个字段为空；等级功能关闭但有 tier 时 `BudgetTierAccountingEligible=false`；真实 tier 402 写完整五个 block 字段，total 402 写 total 维度，403/shadow/fail-open/成功请求五字段全空；流式在最后 usage 到达前不记账、不被中途截断，Close 后仅记一次：

```go
func TestCollector_PersistsFixedTierSnapshot(t *testing.T) {
	c := newUsageGinContext()
	budget.SetTierContext(c, budget.TierContext{
		TierID: "00bb2e65-9c67-45f7-b061-d297a3944876",
		TierName: "高级", GroupName: "claude-opus", SnapshotRevision: 9,
	})
	c.Set(budget.CtxKeyTierAccountingEligible, true)
	budget.SetBlockContext(c, budget.BlockContext{
		Code: "tier_budget_exceeded",
		Dimension: budget.DimensionTier,
		ScopeType: "member",
		ScopeID: "u-1",
		PeriodType: budgetdomain.PeriodMonthly,
	})
	event := collector.NewUsageEvent(c)
	got, err := event.ToCallLog()
	require.NoError(t, err)
	require.NotNil(t, got.BudgetTierID)
	require.Equal(t, "00bb2e65-9c67-45f7-b061-d297a3944876", got.BudgetTierID.String())
	require.Equal(t, "高级", got.BudgetTierNameSnapshot)
	require.True(t, got.BudgetTierAccountingEligible)
	require.NotNil(t, got.BudgetBlockCode)
	require.NotNil(t, got.BudgetBlockDimension)
	require.NotNil(t, got.BudgetBlockScopeType)
	require.NotNil(t, got.BudgetBlockScopeID)
	require.NotNil(t, got.BudgetBlockPeriodType)
	require.Equal(t, "tier_budget_exceeded", *got.BudgetBlockCode)
	require.Equal(t, "tier", *got.BudgetBlockDimension)
	require.Equal(t, "member", *got.BudgetBlockScopeType)
	require.Equal(t, "u-1", *got.BudgetBlockScopeID)
	require.Equal(t, "monthly", *got.BudgetBlockPeriodType)
}

func TestCollector_DisabledTierKeepsReportSnapshotOnly(t *testing.T) {
	c := newUsageGinContext()
	budget.SetTierContext(c, budget.TierContext{
		TierID: "00bb2e65-9c67-45f7-b061-d297a3944876",
		TierName: "高级",
		GroupName: "claude-opus",
		SnapshotRevision: 10,
		AccountingEligible: false,
		EnforcementEligible: false,
	})
	c.Set(budget.CtxKeyTierAccountingEligible, false)
	event := collector.NewUsageEvent(c)
	got, err := event.ToCallLog()
	require.NoError(t, err)
	require.NotNil(t, got.BudgetTierID)
	require.Equal(t, "高级", got.BudgetTierNameSnapshot)
	require.False(t, got.BudgetTierAccountingEligible)
	require.Nil(t, got.BudgetBlockCode)
	require.Nil(t, got.BudgetBlockDimension)
	require.Nil(t, got.BudgetBlockScopeType)
	require.Nil(t, got.BudgetBlockScopeID)
	require.Nil(t, got.BudgetBlockPeriodType)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/observability ./tests/gateway/orchestrator -run 'Test(Collector_(PersistsFixedTier|PersistsBudgetBlock|DisabledTierKeepsReportSnapshotOnly)|Stream.*Budget)' -count=1`

Expected: FAIL with missing fields。

- [ ] **Step 3: Write minimal implementation**

P0 已在 `internal/observability/model.CallLog` 增加数据库字段：

```go
BudgetTierID                 *uuid.UUID `gorm:"type:uuid" json:"budget_tier_id,omitempty"`
BudgetTierNameSnapshot       string     `gorm:"type:varchar(64)" json:"budget_tier_name_snapshot,omitempty"`
BudgetTierAccountingEligible bool       `gorm:"not null;default:false" json:"budget_tier_accounting_eligible"`
BudgetTierConstraintsSnapshot []BudgetTierConstraintSnapshot `gorm:"type:jsonb;not null;default:'[]';serializer:json" json:"budget_tier_constraints_snapshot"`
BudgetBlockCode              *string    `gorm:"type:varchar(64)" json:"budget_block_code,omitempty"`
BudgetBlockDimension         *string    `gorm:"type:varchar(8)" json:"budget_block_dimension,omitempty"`
BudgetBlockScopeType         *string    `gorm:"type:varchar(32)" json:"budget_block_scope_type,omitempty"`
BudgetBlockScopeID           *string    `gorm:"type:varchar(128)" json:"budget_block_scope_id,omitempty"`
BudgetBlockPeriodType        *string    `gorm:"type:varchar(8)" json:"budget_block_period_type,omitempty"`
```

P1 在 `UsageEvent` 增加可空字符串指针，并提供可从 `tests/` 调用的转换方法：

```go
BudgetTierID                 string
BudgetTierNameSnapshot       string
BudgetTierAccountingEligible bool
BudgetTierSnapshotRevision   int64
BudgetBlockCode              *string
BudgetBlockDimension         *string
BudgetBlockScopeType         *string
BudgetBlockScopeID           *string
BudgetBlockPeriodType        *string

func (e UsageEvent) ToCallLog() (observabilitymodel.CallLog, error) {
	var tierID *uuid.UUID
	if e.BudgetTierID != "" {
		parsed, err := uuid.Parse(e.BudgetTierID)
		if err != nil {
			return observabilitymodel.CallLog{}, fmt.Errorf("parse budget tier id: %w", err)
		}
		tierID = &parsed
	}
	return observabilitymodel.CallLog{
		RequestID: e.RequestID,
		BudgetTierID: tierID,
		BudgetTierNameSnapshot: e.BudgetTierNameSnapshot,
		BudgetTierAccountingEligible: e.BudgetTierAccountingEligible,
		BudgetBlockCode: e.BudgetBlockCode,
		BudgetBlockDimension: e.BudgetBlockDimension,
		BudgetBlockScopeType: e.BudgetBlockScopeType,
		BudgetBlockScopeID: e.BudgetBlockScopeID,
		BudgetBlockPeriodType: e.BudgetBlockPeriodType,
	}, nil
}
```

Collector 使用唯一 helper，空字符串返回 nil，非空时复制局部值后取地址，禁止直接把 string 赋给 `*string`：

```go
func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return &copied
}

if block, ok := budget.GetBlockContext(ctx); ok {
	event.BudgetBlockCode = stringPtr(block.Code)
	event.BudgetBlockDimension = stringPtr(string(block.Dimension))
	event.BudgetBlockScopeType = stringPtr(block.ScopeType)
	event.BudgetBlockScopeID = stringPtr(block.ScopeID)
	event.BudgetBlockPeriodType = stringPtr(string(block.PeriodType))
}
```

`Dimension` 与 `PeriodType` 是领域枚举，必须显式 `string(...)` 后再调用 `stringPtr`。真实实现的 `ToCallLog` 必须保留现有全部 call-log 字段映射，上述片段展示新增字段的可编译签名和错误处理；指针可直接从 UsageEvent 赋给 CallLog，因为 Collector 已完成复制和可空处理。`Collector.NewUsageEvent` 只从 `budget.GetTierContext(ctx)` 与 `budget.GetBlockContext(ctx)` 复制，不能在写库时重新查 holder。

`Gate.Check` 只在决定真实阻断后、返回 `ExceededError` 前调用 `SetBlockContext`，内容来自排序后的首要 `Constraint`；shadow would-block 只写 `CtxKeyShadowWouldBlock`，绝不能调用 `SetBlockContext`。UsageCollector 对 `>=400` 不记 Redis 账，但仍投递 usage event/call log，因此 402 block 字段必须落库。这保证 P2 聚合的是实际 402，而不是潜在阻断。

流式规则保持现有 observer：请求开始前 Gate.Check；SSE chunk 继续发送，不做预算检查；流正常结束或 client close 后，observer 把最终 `core.Usage` 放入 Gin context，外层 UsageCollector 在 `c.Next()` 返回后计算一次 cost 并记账。若无最终 usage，cost=0。用 `sync.Once` 保护 post-hook，避免 stream close 与 EOF 双触发。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/observability ./tests/gateway/orchestrator -run 'Test(Collector_(PersistsFixedTier|PersistsBudgetBlock|DisabledTierKeepsReportSnapshotOnly)|Stream.*Budget)' -count=1`

Expected: PASS；流式 counter 精确增加一次。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/observability/event.go internal/gateway/observability/collector.go internal/gateway/middleware/usage_collector.go tests/gateway/observability/tier_snapshot_test.go tests/gateway/orchestrator/stream_usage_test.go
git commit -m "feat(observability): persist request tier snapshots"
```

### Task 9: 扩展总预算与等级预算告警请求

**Files:**
- Modify: `internal/gateway/server/router.go`
- Modify: `internal/controlplane/handler/budget_handler.go`
- Modify: `tests/gateway/middleware/budget_reserve_test.go`
- Modify: `tests/controlplane/service/budget_alert_service_test.go`
- Modify: `tests/contract/contracts.json`

- [ ] **Step 1: Write the failing tests**

对一个两层 budget chain、其中一层配置 tier 的成功请求，HTTP fake 必须收到三条唯一请求：两条 total + 一条 tier；payload 精确如下：

```json
{
  "scope_type": "member",
  "scope_id": "u-1",
  "period_type": "monthly",
  "dimension_type": "tier",
  "model_tier_id": "tier-1"
}
```

失败请求、0 成本请求、Redis check fail-open 请求不得触发告警；同一 scope/tier 的日周月应分别请求。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/middleware ./tests/controlplane/service -run 'Test(BudgetAlertRequest|EvaluateTierAlert)' -count=1`

Expected: FAIL；当前请求只有 scope type/id，无法区分 period 和 tier。

- [ ] **Step 3: Write minimal implementation**

```go
type BudgetAlertRequest struct {
	ScopeType    string `json:"scope_type"`
	ScopeID      string `json:"scope_id"`
	PeriodType   string `json:"period_type"`
	DimensionType string `json:"dimension_type"`
	ModelTierID  string `json:"model_tier_id,omitempty"`
}

func triggerBudgetAlerts(ctx context.Context, requests []BudgetAlertRequest, controlPlaneURL, internalToken string)
```

请求列表直接由成功写入的 `AccountingPlan.Constraints` 去重生成；不能重新从 identity 推导，否则会对未配置/disabled tier 告警。使用项目标准 `httpclient.NewHTTPClient`，每请求 3 秒 timeout；异步 goroutine 使用复制后的纯值 slice 和 `context.WithTimeout(context.Background(), 10*time.Second)`。

Control Plane handler 校验 `dimension_type in total|tier`、tier 时 ID 非空、total 时 ID 为空，并调用：

```go
EvaluateAndAlert(ctx context.Context, scopeType, scopeID, periodType, dimensionType, modelTierID string)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/middleware ./tests/controlplane/service -run 'Test(BudgetAlertRequest|EvaluateTierAlert)' -count=1 && go test ./tests/contract/... -count=1`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/server/router.go internal/controlplane/handler/budget_handler.go tests/gateway/middleware/budget_reserve_test.go tests/controlplane/service/budget_alert_service_test.go tests/contract/contracts.json
git commit -m "feat(budget): evaluate total and tier alerts"
```

### Task 10: 从 call_logs 重建 v2 counter 并生成影子对账

**Files:**
- Create: `internal/controlplane/service/budget_counter_rebuild_service.go`
- Create: `tests/controlplane/service/budget_counter_rebuild_service_test.go`
- Modify: `internal/controlplane/handler/budget_handler.go`
- Modify: `internal/controlplane/router/router.go`
- Modify: `internal/controlplane/service/budget_service.go`

- [ ] **Step 1: Write the failing tests**

使用 SQLite/GORM fixture 或 repository fake 写入：成功/失败、分级/未分级、eligible true/false、personal/service、不同周期边界。断言：

- v2 total 汇总所有 `<400` 且成本正数成功调用；
- v2 tier 只汇总 tier ID 非空且 eligible=true，并逐项消费 `budget_tier_constraints_snapshot` 中实际参与记账的 Redis key/period/reset_at；不得查询当前配置反推历史 scope；
- service key 进入 `dept_service`，不进入 member；
- 重建先获取带续租的全局 Redis lease；Gateway 在 lease 存在时以 `503 budget_rebuild_in_progress` 拒绝新可计费请求，并等待已登记的 inflight ZSET 归零；
- inflight reservation 按 TTL/3 heartbeat 续租；归零后可靠 drain call-log Stream 到 PostgreSQL，确认 stream length/pending/lag 归零，再以 keyset 分页聚合到 run 临时 Redis hash，分批构建新 generation namespace，最后单 Lua 切换 `budget:v2:active_generation`；
- lease 丢失立即中止；切换前校验所有正式值，查询、drain、聚合或校验失败均不覆盖正式 key；
- 对账输出 legacy-v2、v2-call_logs 的绝对差和比例；
- 阈值 `max(0.1%, 1_000_000 microUSD/day)`。

```go
require.Equal(t, int64(1_250_001), got.ExpectedMicroUSD)
require.Equal(t, int64(1_250_001), mustRedisInt(t, rdb,
	"budget:v2:tier:used:member:u-1:tier-1:2026-07"))
require.LessOrEqual(t, got.AbsDiffMicroUSD, got.AllowedDiffMicroUSD)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/controlplane/service -run 'TestBudgetCounterRebuildV2' -count=1`

Expected: FAIL with `undefined: service.NewBudgetCounterRebuildService`。

- [ ] **Step 3: Write minimal implementation**

```go
type CounterDiff struct {
	Key string `json:"key"`
	ExpectedMicroUSD int64 `json:"expected_micro_usd"`
	ActualMicroUSD int64 `json:"actual_micro_usd"`
	AbsDiffMicroUSD int64 `json:"abs_diff_micro_usd"`
	DiffRatio float64 `json:"diff_ratio"`
	AllowedDiffMicroUSD int64 `json:"allowed_diff_micro_usd"`
	WithinThreshold bool `json:"within_threshold"`
}

type RebuildV2Result struct {
	KeysWritten int `json:"keys_written"`
	Diffs []CounterDiff `json:"diffs"`
	CompletedAt time.Time `json:"completed_at"`
}

func NewBudgetCounterRebuildService(db *gorm.DB, rdb *redis.Client, periodConfig budgetperiod.Config) *BudgetCounterRebuildService
func (s *BudgetCounterRebuildService) RebuildCurrentPeriods(ctx context.Context, now time.Time) (*RebuildV2Result, error)
```

SQL 的事实过滤必须是 `status < 400 AND est_cost_usd > 0`，不能沿用旧实现的 `status = 200`。每行金额从 PostgreSQL 以 `SUM(ROUND(est_cost_usd * 1000000))::bigint` 聚合，符合“每请求先 ROUND_HALF_UP 再求和”；SQLite 测试用 Go 聚合 adapter 验证相同结果。

日/周/月查询起点和 key 全部来自 `budgetperiod.Resolve(now, periodType, s.periodConfig)`，TTL 由该结果的 `ResetAt` 加保留窗口计算；不得在重建 service 内调用 `time.Local`、`ISOWeek` 或手写零点。测试把进程 Local 改为错误时区后，上海周期结果必须不变。

tier SQL 必须额外：

```sql
budget_tier_id IS NOT NULL
AND budget_tier_accounting_eligible = TRUE
```

写入流程采用“短时独占重建 + generation 单指针”：`SET budget:v2:rebuild:lock <owner> NX PX <lease>` 并持续 compare-and-renew；Gateway 准入 Lua 在同一原子操作中检查 lease、清理过期 inflight reservation、固定 `budget:v2:active_generation`（缺失时为 `legacy`），并校验 `XLEN(call-log stream) + ZCARD(valid inflight) < capacity` 后登记 request_id。reservation 按 TTL/3 heartbeat 续租，UsageCollector 在 counter 与 Stream/同步 PG fallback 全部完成后停止 heartbeat 并释放；正常完成使用单个 Lua `XADD + ZREM`。重建持锁后等待 inflight 归零，drain Stream 并确认 pending/lag 为零，再按 keyset 分页读取 allocations、tier limits 和 call_logs，聚合结果写入 run 临时 Redis hashes并按最多 100 keys 构建 `budget:v2:gen:{new_generation}:...`。全部构建成功后仅用一个 Lua 校验 lease token 和旧 generation，再一次切换 active pointer；失败时指针不变并清理新 namespace，旧 generation 保留用于回滚和 TTL 自然过期。

准入结果必须区分业务背压与基础设施失败：rebuild lock、容量满分别返回明确 503；正数预算遇到 Redis 连接/Lua 基础设施错误则 fail-open，不创建 reservation、不累计 v2 counter，并优先同步写 PostgreSQL call_log。heartbeat 的错误和 member 缺失不得忽略；迟到完成若跨越 rebuild，必须等待 lease 释放后读取新 active generation，并通过 request_id 幂等 marker 将新 generation account 与 Stream fact 原子提交。无法安全重绑时禁止写旧 generation，只可靠保存 call_log 并增加 reconciliation-required 指标，后续 rebuild 从事实源补齐。

handler 的重建响应包含 `diffs`；若任一 `WithinThreshold=false` 返回 200 和 `"cutover_ready": false`，不得自动切开关。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/controlplane/service -run 'TestBudgetCounterRebuildV2' -count=1`

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/controlplane/service/budget_counter_rebuild_service.go internal/controlplane/service/budget_service.go internal/controlplane/handler/budget_handler.go internal/controlplane/router/router.go tests/controlplane/service/budget_counter_rebuild_service_test.go
git commit -m "feat(budget): rebuild and reconcile v2 counters"
```

### Task 11: 增加指标、日志与 trace 属性

**Files:**
- Create: `internal/gateway/budget/metrics.go`
- Modify: `internal/gateway/budget/gate.go`
- Modify: `internal/gateway/middleware/tracing.go`
- Modify: `tests/gateway/middleware/trace_pipeline_test.go`
- Modify: `tests/gateway/budget/gate_test.go`

- [ ] **Step 1: Write the failing tests**

用 OTel manual reader/in-memory span exporter 断言以下指标和属性：

```text
gateway_budget_check_total{dimension,result,period,scope_type}
gateway_budget_tier_reject_total{tier_id,scope_type,period}
gateway_budget_fail_open_total{dimension}
gateway_budget_shadow_would_block_total{dimension,tier_id}

llm.budget_tier_id
budget.check_result
budget.block_reason
budget.shadow_mode
```

高基数约束：通用 check metric 不含 scope ID、user ID、group name；只有专用 tier reject/shadow metric 带 tier ID。日志包含 request_id、group_name、tier ID、dimension、scope type、period、used/limit 微美元、fail_open、shadow_mode，但绝不记录 API key 或上级精确金额到客户端响应。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/gateway/budget ./tests/gateway/middleware -run 'Test(BudgetMetrics|TracePipeline_Budget)' -count=1`

Expected: FAIL，找不到新 counters/attributes。

- [ ] **Step 3: Write minimal implementation**

```go
func recordCheck(ctx context.Context, c Constraint, result string)
func recordTierReject(ctx context.Context, c Constraint)
func recordFailOpen(ctx context.Context, dimension Dimension)
func recordShadowWouldBlock(ctx context.Context, c Constraint)
```

meter name 固定 `llm-gateway/budget`，按 `quota/metrics.go` 的 `sync.Once` 模式创建 counters。Gate 在每个 constraint 的 pass/block/error 路径只记录一次。

Tracing middleware 在 `c.Next()` 后读取 context：

```go
if tier, ok := budget.GetTierContext(c); ok {
	span.SetAttributes(attribute.String("llm.budget_tier_id", tier.TierID))
}
span.SetAttributes(
	attribute.String("budget.check_result", c.GetString(budget.CtxKeyCheckResult)),
	attribute.String("budget.block_reason", c.GetString(budget.CtxKeyBlockReason)),
	attribute.Bool("budget.shadow_mode", c.GetBool(budget.CtxKeyShadowMode)),
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./tests/gateway/budget ./tests/gateway/middleware -run 'Test(BudgetMetrics|TracePipeline_Budget)' -count=1`

Expected: PASS，且 metric labels 不包含 `scope_id`。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/budget/metrics.go internal/gateway/budget/gate.go internal/gateway/middleware/tracing.go tests/gateway/budget/gate_test.go tests/gateway/middleware/trace_pipeline_test.go
git commit -m "feat(budget): observe tier checks and shadow decisions"
```

### Task 12: 全链路、竞态、性能与灰度验收

**Files:**
- Create: `tests/integration/model_tier_budget_flow_test.go`
- Modify: `tests/gateway/handler/model_tier_budget_test.go`
- Modify: `tests/gateway/budget/redis_store_test.go`
- Modify: `docs/deployment.md`

- [ ] **Step 1: Write the failing tests**

集成测试用真实 Redis/PostgreSQL fixture 和 fake upstream 覆盖：

1. unauthorized + `$0` => 403，upstream 0 次，counter 0；
2. authorized + `$0` => 402，Redis down 仍 402；
3. authorized + positive exhausted => shadow 期 200 且 would-block，开启后 402；
4. 未分级 => 只增加 v2 total；
5. 已分级 => 所有预算链层级的 total + 已配置 tier 同时增加；
6. 六协议成本按相同微美元规则累计，VL Rerank 走 search_units；
7. 流式开始后变更 tier/limit 不截断，结束后按旧 tier 快照记账；
8. Redis down 正数 fail-open，call log 仍落库，v2 counter 不写；重建后补回；
9. legacy + v2 双写对账；
10. disabled/archived tier 调用成功：call log 保留 tier ID/name，eligible=false，无 tier check/add、无 402；
11. 100 goroutine 并发 check/add 无 data race，接受事后记账并发软超额。

性能 benchmark：

```go
func BenchmarkRedisStore_CheckTotalAndTier(b *testing.B) {
	plan := benchmarkPlan(4, 3)
	for i := 0; i < b.N; i++ {
		result := store.Check(context.Background(), plan)
		if result.RedisError != nil || len(result.Blockers) != 0 {
			b.Fatal(result)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./tests/integration -tags=integration -run TestModelTierBudgetFlow -count=1`

Expected: integration 在尚未完成完整 wiring 或数据库前置迁移时 FAIL；不得用 skip 伪造通过。

- [ ] **Step 3: Complete wiring and rollout documentation**

补齐测试暴露的 wiring；`docs/deployment.md` 写入不可跳步的开关顺序：

```text
1. 部署 P0 publisher/API 与全部支持 v2 的 Gateway：master_enabled=false、accounting_enabled=false、positive_enforcement_enabled=false、zero_block_enabled=false、shadow_enabled=false、v2_total_enforcement_enabled=false、legacy_total_write_enabled=true。
2. 开启影子：master_enabled=true、accounting_enabled=true、positive_enforcement_enabled=false、zero_block_enabled=false、shadow_enabled=true、v2_total_enforcement_enabled=false、legacy_total_write_enabled=true。
3. 从 call_logs 重建当前日/周/月；连续一个完整最长预算周期满足 max(0.1%, $1/day)。
4. 开启静态零：master_enabled=true、accounting_enabled=true、positive_enforcement_enabled=false、zero_block_enabled=true、shadow_enabled=true。
5. 退出影子并启用正数：master_enabled=true、accounting_enabled=true、positive_enforcement_enabled=true、zero_block_enabled=true、shadow_enabled=false。
6. 切 v2 总预算：v2_total_enforcement_enabled=true，继续 legacy_total_write_enabled=true 一个完整周期。
7. 稳定后 legacy_total_write_enabled=false；旧 key 按 TTL 自然过期。
```

回滚：先设置 `positive_enforcement_enabled=false, shadow_enabled=true` 恢复影子，再设置 `v2_total_enforcement_enabled=false`；普通故障不关闭 `zero_block_enabled`。只有确认静态配置本身错误时才关闭 `zero_block_enabled`；需要完全关闭等级功能时设置 `master_enabled=false`，此时 tier eligible=false 且暂停 tier 累计，绝不能把它描述为影子。任何回滚都不删 v2 keys、不改 call logs。

- [ ] **Step 4: Run the complete verification suite**

Run:

```bash
gofmt -w internal/gateway/budget internal/gateway/model/identity.go internal/gateway/infra/auth_client.go internal/gateway/orchestrator/group_dispatcher.go internal/gateway/handler internal/gateway/middleware internal/gateway/observability internal/gateway/server/router.go internal/controlplane/service/budget_counter_rebuild_service.go internal/controlplane/handler/budget_handler.go cmd/gateway/main.go tests/gateway/budget tests/gateway/handler/model_tier_budget_test.go tests/gateway/observability/tier_snapshot_test.go tests/controlplane/service/budget_counter_rebuild_service_test.go tests/integration/model_tier_budget_flow_test.go
go test ./tests/gateway/... ./tests/controlplane/... -count=1
go test -race ./tests/gateway/budget ./tests/gateway/handler ./tests/gateway/observability -count=1
go test ./tests/integration -tags=integration -run TestModelTierBudgetFlow -count=1
go test ./tests/gateway/budget -run '^$' -bench BenchmarkRedisStore_CheckTotalAndTier -benchmem -count=5
go build ./...
```

Expected: 所有 test/build PASS、race 无报告；benchmark 报告分配与耗时基线。使用 Redis latency histogram 验证预算 Lua p99 < 5ms，并用端到端压测验证预算检查新增 p99 < 2ms、Gateway CPU 增幅 < 5%。若未达到目标，保持 `positive_enforcement_enabled=false`、`shadow_enabled=true`、`v2_total_enforcement_enabled=false`，不得带性能豁免启用拦截。

- [ ] **Step 5: Commit**

```bash
git add tests/integration/model_tier_budget_flow_test.go tests/gateway/handler/model_tier_budget_test.go tests/gateway/budget/redis_store_test.go docs/deployment.md
git commit -m "test(budget): verify tier accounting rollout"
```

---

## 实施完成自检

- [ ] P0 的 Atlas/GORM、verify-key tier limits、基础 APIs、`BudgetTierSnapshotPublisher`、共享 snapshot DTO 和 `internal/budgetperiod` 已先验收；P1 没有任何 publisher/save 实现。
- [ ] P0 对 `budget_block_*` 只提供 `*string` schema（64/8/32/128/8）；P1 Gate → Collector `stringPtr` → UsageEvent → CallLog 独立完成五字段 runtime 写入。
- [ ] 每个入口都先完成模型组授权，再固定 tier，再预算检查；unauthorized 测试证明没有 Redis 访问。
- [ ] `TierResolver` 只使用原始组名和独立快照；disabled/archived 仍返回 ID/name 且两个 eligibility=false，只有未分级无 TierContext；provider 切换、模型路由、failover 不改变 tier。
- [ ] verify-key total 仍是唯一的 `amount_usd`/`daily_amount_usd`/`weekly_amount_usd` JSON number，不存在同 tag 或 `*_v2` 字段；只有 tier limit 金额是 6 位小数字符串。
- [ ] holder/loader 与 provider/quota loader 相互独立；坏快照、旧 revision、Redis 故障不覆盖 last-known-good。
- [ ] hot reload 复用 `infra.Subscriber` 解码的 `infra.ChangeEvent` JSON，只在 `event.Type == budget_tier_snapshot_updated` 时加载，不消费裸字符串。
- [ ] 周期仅调用签名 `budgetperiod.Resolve(now, periodType, config) (Period, error)`，不定义类型/函数别名；快照 timezone=`Asia/Shanghai`、week_start=`monday`，修改容器 `time.Local` 不改变结果。
- [ ] 快照只包含七个规范开关；关闭不累计 tier，影子累计并记录 would-block，二者测试明确区分。
- [ ] 所有 v2 used key 都是整数微美元，转换测试覆盖 `ROUND_HALF_UP` 边界；Lua 不用 `INCRBYFLOAT`。
- [ ] 一次检查/累计覆盖全部 total+tier constraints；未分级只触碰 total。
- [ ] `$0` 在内存中 fail-closed；正数 Redis error fail-open 且该请求不写 v2 counter。
- [ ] Chat、Responses、Messages、Embeddings、Rerank、VL Rerank 共用预算 helper，并覆盖成功、402、403、0 成本。
- [ ] 流式只在开始前检查，不中断；结束后依据最终 usage 记一次账。
- [ ] call log 固定 tier ID/name 并持久化 `budget_tier_accounting_eligible`；disabled/archived 期间保留报表快照但不计数/阻断；真实 402 的五个 `budget_block_*` 指针均非 nil 且值正确，403/shadow/fail-open/success 五指针均为 nil。
- [ ] 告警请求包含 total/tier、period、tier ID，且只从成功 accounting plan 生成。
- [ ] 旧 float total 与 v2 双写可独立开关；shadow would-block 可观测但不拦截。
- [ ] 重建使用每请求半入后的微美元事实、`status < 400`、tier eligible，并以临时 key 原子替换。
- [ ] 指标和 trace 名称与技术设计一致，没有 scope ID/user ID 等高基数通用标签。
- [ ] `/v1/p/:provider/*` 保持等级预算豁免。
- [ ] constructor/public API 与“P1 固定交付契约”逐项一致，没有平行 API；测试只验证 wire/runtime behavior。
- [ ] `go test`、`go test -race`、integration、benchmark、`go build ./...` 全部执行并记录结果。
- [ ] 搜索本文，确认没有占位语句，也没有缺少真实代码的“写测试/实现”步骤。

## 验收映射

- 权限先于预算：Task 3、6、12。
- P0 API/snapshot publisher/shared period 依赖：前置条件、依赖矩阵、Task 1、2、4。
- TierResolver/独立快照：Task 2、3。
- disabled/archived 保留报表且不计数/阻断：Task 3、4、7、8、12。
- `infra.ChangeEvent` JSON hot reload：Task 2。
- verify-key 原 total number + tier limit strings：Task 1、4。
- 整数微美元、v2 key、总+等级检查/累计：Task 1、4、5。
- `$0` fail-closed/正数 fail-open：Task 4、7。
- 六协议和流式一致性：Task 6、8、12。
- call_logs tier/eligible 与真实 402 block 快照：Task 8、12。
- 告警请求：Task 9。
- 双写、影子、重建：Task 7、10、12。
- 七个规范开关与关闭/影子差异：Task 2、4、7、12。
- 指标与 trace：Task 11。
- 固定 constructor/public API 交付契约：交付契约章节、Task 2—12。

计划共 **12 个任务**。
