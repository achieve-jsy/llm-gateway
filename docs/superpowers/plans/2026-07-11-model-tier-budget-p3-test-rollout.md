# 模型等级子预算 P3：端到端测试、观测、迁移验证、灰度与回滚 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为模型等级子预算建立可重复执行的自动化验证、生产观测、Atlas 迁移门禁、完整周期影子对账，以及可演练的灰度启用和蓝绿回滚流程，使正数等级额度与 `$0` 静态禁用能够在量化证据支持下安全上线。

**Architecture:** 测试采用仓库既有的 Go `testing`、`httptest`、手写 fake、`miniredis`、PostgreSQL/Redis 集成环境和 CLI 冒烟；正式 E2E 不引入 Playwright/Cypress。Gateway 等级预算实现统一复用 `internal/gateway/budget`，周期统一复用 `internal/budgetperiod`；`call_logs` 是重建事实源，Control Plane 的 `BudgetCounterRebuildService` 是唯一重建/对账公开 API，Redis v2 整数微美元计数是实时拦截源。CI 在构建前增加强制 `test` stage，并把 Atlas 校验、单元/组件/契约、集成和性能门槛分层执行。

**Tech Stack:** Go 1.26、Gin/httptest、testify、miniredis、PostgreSQL 16、Redis 7、Atlas + GORM Provider、OpenTelemetry、GitLab CI、Docker Compose、k6、curl/jq/psql/redis-cli、现有蓝绿部署脚本。

**Task Count:** 16

---

## 依赖矩阵与文件边界

执行顺序固定为 `P0 → P1 → P2 → P3`。P3 不补写前序阶段业务实现；下列任一硬门禁缺失时停止 P3：

- **P0 数据与 Control Plane 基础：** Atlas Expand migration 必须包含 tier ID/name snapshot、`budget_tier_accounting_eligible boolean not null default false`，以及且仅有 `budget_block_code`、`budget_block_dimension`、`budget_block_scope_type`、`budget_block_scope_id`、`budget_block_period_type` 五个阻断列；不得新增其他 `budget_block_*`。P0 必须实现 `BudgetTierSnapshotPublisher`，从数据库生成单调 revision 快照、原子写 `budget:v2:tier:snapshot`，并向 `config:change` 发布可被 Gateway `infra.ChangeEvent` 解码的 JSON。
- **P0 API：** 除 CRUD、组关联和单条 tier limits 外，以下 handler/service 必须可测试后 P3 才开始：`POST /api/v1/budgets/tier-limits/batch/preview`、`POST /api/v1/budgets/tier-limits/batch/apply`、`GET /api/v1/budgets/tier-usage`、`GET /api/v1/my/budget/tiers`；`/my` 必须返回 P0 定义的完整 DTO：`source_summary` 含 mode/primary_scope_type/has_shared_pool/has_temporary_override，`effective_total` 为 `EffectiveTotalPeriod` 数组，并包含 budget_chain/tier_limits/data_updated_at/stale。P3 只验证业务契约，不补基础实现。
- **共享周期：** P0/P1 必须共同使用 `internal/budgetperiod`；配置固定 `timezone=Asia/Shanghai`、`week_start=monday`，禁止依赖容器 `TZ` 或在 Gateway/Control Plane 各写一套周期函数。
- **P0 生命周期：** P0 交付唯一 `BudgetLifecycleExecutor`，同一执行器处理 total/tier 的到期 schedule 与 temporary override；唯一 `BudgetLifecycleObserver` 固定实现 `OnOverrideExpired(ctx, ExpiredBudgetOverride)`、`OnScheduleApplied(ctx, AppliedBudgetSchedule)`、`OnScheduleFailed(ctx, FailedBudgetSchedule)`。P2 只实现通知 observer，不创建 worker、ticker、schedule executor 或第二条扫描路径。
- **P1 Gateway：** `internal/gateway/budget/money.go`、`context.go`、`snapshot_holder.go`、`config_loader.go`、`tier_resolver.go`、`redis_store.go`、`gate.go`、`metrics.go` 已实现；所有 Gateway 预算测试只导入 `internal/gateway/budget`。
- **P1 重建：** 唯一公开重建 API 是 `service.NewBudgetCounterRebuildService(db, rdb, periodConfig)` 与 `(*BudgetCounterRebuildService).RebuildCurrentPeriods(ctx, now)`；`periodConfig` 固定为 `budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"}`。HTTP 入口沿用 `POST /api/v1/budget/rebuild-counters`。不得新建 Gateway reconciler 或第二套对账 CLI。
- **P2：** 审批、风险和 Web 页面已接入 P0 API 与 P1 v2 usage；temporary override 到期与 schedule 结果通知只挂接 `BudgetLifecycleObserver`。前端自动门禁只使用 `vue-tsc` 与 Vite build，正式交互 E2E 只使用 `specs/model-tier-budget/test/*.md` 手工用例。

P1 还必须提供以下公开边界，测试不得访问私有方法：

- `internal/gateway/budget/redis_store.go`：Redis v2 检查、累计、legacy 总预算双写和 fail-open。
- `internal/gateway/budget/gate.go`：总预算后、上游前执行等级预算判断。
- `internal/gateway/budget/snapshot_holder.go`：版本化等级快照与 last-known-good holder。
- `internal/gateway/model/identity.go`：verify-key 下发的 `TierLimits`。
- `internal/observability/model/call_log.go`：`BudgetTierID`、`BudgetTierNameSnapshot` 与 `BudgetTierAccountingEligible`。
- `internal/controlplane/service/model_tier_service.go`、`budget_tier_service.go`：等级与等级额度业务服务。
- `internal/controlplane/handler/model_tier_handler.go`、`budget_tier_handler.go`：管理 API。

本阶段按职责新增或修改以下文件：

- 测试：`tests/gateway/budget/*.go`、`tests/gateway/handler/model_tier_budget_test.go`、`tests/controlplane/service/model_tier_observability_test.go`、`tests/controlplane/handler/model_tier_contract_test.go`、`tests/integration/model_tier_budget_flow_test.go`、`tests/integration/model_tier_budget_fault_test.go`。
- 契约：`tests/contract/contracts.json`、`tests/contract/model_tier_contract_test.go`。
- 观测：`internal/gateway/budget/metrics.go`、`internal/gateway/budget/telemetry.go`、`internal/controlplane/service/budget_tier_metrics.go`、`internal/controlplane/middleware/audit.go`、`internal/gateway/middleware/tracing.go`。
- 性能：`tests/performance/model_tier_budget.js`、`tests/gateway/budget/benchmark_test.go`。
- 对账与运维：`tests/controlplane/service/budget_counter_rebuild_service_test.go`、`docs/runbooks/model-tier-budget-rollout.md`、`docs/runbooks/model-tier-budget-rollback.md`。
- CI/部署：`.gitlab-ci.yml`、`deploy/blue-green/deploy-blue-green.sh`、`deploy/blue-green/docker-compose.blue.yml`、`deploy/blue-green/docker-compose.green.yml`。

所有 Go 测试必须放在 `tests/` 下的外部测试包，表驱动用例包含中文 `description`；集成测试使用 `//go:build integration`。正式 E2E 只由 Go 集成测试、curl/jq、psql、redis-cli 和 `specs/model-tier-budget/test/*.md` 手工用例完成。

### Task 1: 验证 P0/P1/P2 业务硬依赖

**Files:**
- Verify: `tests/controlplane/service/budget_tier_snapshot_publisher_test.go`
- Verify: `tests/controlplane/service/budget_lifecycle_executor_test.go`
- Verify: `tests/controlplane/service/budget_lifecycle_notification_test.go`
- Verify: `tests/configsync/budget_tier_snapshot_test.go`
- Verify: `tests/controlplane/handler/budget_tier_usage_handler_test.go`

- [ ] **Step 1: 运行 P0 snapshot 与 lifecycle 业务测试**

Run:

```bash
go test ./tests/controlplane/service ./tests/configsync \
  -run 'Test(BudgetTierSnapshotPublisher|BudgetLifecycleExecutor|BudgetLifecycleObserver_OnScheduleFailed|BudgetTierSnapshot)' \
  -count=1 -v
```

Expected: PASS；publisher 发送结构化 change event；唯一 lifecycle executor 同时处理 schedule/override，并分别调用三个 observer 方法。失败 schedule 先回滚业务事务，再以新事务标记 failed；`OnScheduleFailed` 收到包含 `ID/BudgetAllocationID/DimensionType/ModelTierID/PeriodType/FailureReason/FailedAt` 的 `FailedBudgetSchedule`。

- [ ] **Step 2: 运行 P2 通知 observer 业务测试**

Run: `go test ./tests/controlplane/service -run 'TestBudgetLifecycleNotification_(OverrideExpired|ScheduleApplied|OnScheduleFailed)' -count=1 -v`

Expected: PASS；P2 observer 发送幂等通知，特别断言 `OnScheduleFailed(ctx, item FailedBudgetSchedule)` 使用 `item.FailureReason` 通知对应负责人；测试不启动第二个 worker/ticker。

- [ ] **Step 3: 以 `infra.ChangeEvent` 解码 snapshot 事件**

```go
var event infra.ChangeEvent
require.NoError(t, json.Unmarshal(publishedPayload, &event))
require.Equal(t, "budget_tier_snapshot_updated", event.Type)
require.Equal(t, "budget_tier_snapshot", event.ResourceType)
require.Equal(t, int64(42), event.SnapshotVersion)
require.NotEmpty(t, event.ResourceID)
require.False(t, event.Timestamp.IsZero())
```

Expected: publisher 不发送裸字符串；revision 进入 `snapshot_version`，Gateway 可直接按既有 `infra.ChangeEvent` JSON 结构消费。另发的 KeyCache 失效事件同样使用该结构。

- [ ] **Step 4: Commit**

本任务只验证前序交付，不产生 P3 commit；任一检查失败，回到对应 P0/P1/P2 计划修复后重新执行。

### Task 2: 建立共享周期边界测试

**Files:**
- Verify: `internal/budgetperiod/period.go`
- Modify: `tests/gateway/budget/period_contract_test.go`

- [ ] **Step 1: 按 P0/P1 已公开签名写边界契约测试**

```go
package budget_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedPeriod_边界与时区(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	cfg := budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"}
	tests := []struct {
		name        string
		description string
		now         time.Time
		period      budgetdomain.PeriodType
		wantKey     string
		wantReset   string
	}{
		{
			name:        "上海日周期",
			description: "UTC 时间必须映射到上海日边界",
			now:         time.Date(2026, 7, 11, 16, 30, 0, 0, time.UTC),
			period:      budgetdomain.PeriodDaily,
			wantKey:     "2026-07-12",
			wantReset:   "2026-07-13T00:00:00+08:00",
		},
		{
			name:        "ISO周一周期",
			description: "周周期固定使用上海时区与 ISO Monday",
			now:         time.Date(2026, 7, 11, 16, 30, 0, 0, time.UTC),
			period:      budgetdomain.PeriodWeekly,
			wantKey:     "2026-W28",
			wantReset:   "2026-07-13T00:00:00+08:00",
		},
		{
			name:        "上海月周期",
			description: "月周期在上海下月一日零点重置",
			now:         time.Date(2026, 7, 11, 16, 30, 0, 0, time.UTC),
			period:      budgetdomain.PeriodMonthly,
			wantKey:     "2026-07",
			wantReset:   "2026-08-01T00:00:00+08:00",
		},
		{
			name:        "ISO跨年周",
			description: "年末日期属于下一 ISO 周年时，周 key 必须使用 ISO week-year 而不是自然年",
			now:         time.Date(2025, 12, 29, 1, 0, 0, 0, loc),
			period:      budgetdomain.PeriodWeekly,
			wantKey:     "2026-W01",
			wantReset:   "2026-01-05T00:00:00+08:00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := budgetperiod.Resolve(tt.now, tt.period, cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.wantKey, got.Key, tt.description)
			assert.Equal(t, tt.wantReset, got.ResetAt.Format(time.RFC3339), tt.description)
		})
	}
}
```

- [ ] **Step 2: 运行共享包与 Gateway 契约测试**

Run: `go test ./tests/budgetperiod ./tests/gateway/budget -run 'Test(WeeklyPeriodUsesShanghaiISOWeek|SharedPeriod)' -count=1 -v`

Expected: PASS；若签名或结果不一致，停止 P3 并修复 P0/P1，不在 P3 重新定义 shared period API。

- [ ] **Step 3: 用错误进程时区验证 Gateway 与重建周期行为**

测试保存 `time.Local`，临时替换为 `time.FixedZone("wrong", -7*3600)` 并在 `t.Cleanup` 恢复；分别通过 Gateway `BuildAccountingPlan` 和三参数 `BudgetCounterRebuildService` 生成同一 fixed now 的日/周/月 key。

Expected: 两条业务路径都得到 `Asia/Shanghai`、ISO Monday 的相同 key/reset，不受进程时区影响。

- [ ] **Step 4: 补充夏令时、月末、闰日和边界前后 1ns 用例**

在同一测试表中加入 `2028-02-29`、上海月末 `23:59:59.999999999`、ISO Monday 前后 1ns 与 ISO 跨年周；断言 key 只在 `Asia/Shanghai` 边界变化，`ResetAt` 始终晚于 `now`。不得加入可配置周起始日或其他业务时区分支。

- [ ] **Step 5: 运行测试与竞态检查**

Run: `go test ./tests/budgetperiod ./tests/gateway/budget -run 'Period' -race -count=20`

Expected: PASS；20 次结果一致，无数据竞争、无依赖墙钟的偶发失败。

- [ ] **Step 6: Commit**

```bash
git add tests/gateway/budget/period_contract_test.go
git commit -m "test(budget-tier): cover clock and period boundaries"
```

### Task 3: 覆盖 Redis v2 整数记账、双写与并发窗口

**Files:**
- Modify: `internal/gateway/budget/redis_store.go`
- Modify: `tests/gateway/budget/redis_store_test.go`
- Create: `tests/gateway/budget/concurrency_test.go`

- [ ] **Step 1: 写微美元换算、原子累计和双写测试**

```go
func TestRedisStore_AddUsesIntegerMicroUSD(t *testing.T) {
	// 测试目的：P1 RedisStore.Add 应把整数微美元原子写入 plan 的总预算和等级 key。
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStore(rdb)
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{
		{Key: "budget:v2:used:member:u-1:2026-07", LimitMicroUSD: 10_000_000, PeriodType: "monthly"},
		{Key: "budget:v2:tier:used:member:u-1:tier-high:2026-07", LimitMicroUSD: 2_000_000, PeriodType: "monthly"},
	}}
	require.NoError(t, store.Add(context.Background(), plan, 2))
	assert.Equal(t, "2", mr.MustGet("budget:v2:used:member:u-1:2026-07"))
	assert.Equal(t, "2", mr.MustGet("budget:v2:tier:used:member:u-1:tier-high:2026-07"))
}
```

同文件另测 post-success pipeline：当快照 `legacy_total_write_enabled=true` 时旧总预算 key 与 v2 total/tier key 都增加；关闭该 flag 后只停止旧 key 写入。旧 key 双写不属于 `RedisStore.Add` 的公开签名。

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/gateway/budget -run 'TestRedisStore_AddUsesIntegerMicroUSD|TestPostSuccess_LegacyTotalWriteFlag' -count=1 -v`

Expected: PASS；v2 keys 使用整数 `INCRBY`，legacy 双写只受 canonical flag 控制。

- [ ] **Step 3: 实现 Lua 多 key 原子写与可控旧 key 双写**

Lua 必须先验证所有参数为非负十进制整数，再对 v2 keys 执行 `INCRBY`；旧 key 双写在独立 best-effort 分支执行，旧 key 失败不撤销 v2 原子累计，但必须记录 `legacy_write_failed` 指标和结构化日志。

- [ ] **Step 4: 写 200 goroutine 并发累计测试**

```go
func TestStore_ConcurrentAdd_NoLostUpdate(t *testing.T) {
	// 测试目的：200 个成功请求并发累计同一 plan，每次 7 微美元，最终值必须精确为 1400。
	mr := miniredis.RunT(t)
	store := budget.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{{
		Key: "budget:v2:tier:used:member:u-1:tier-high:2026-07",
		LimitMicroUSD: 100_000_000,
		PeriodType: "monthly",
	}}}
	var wg sync.WaitGroup
	errCh := make(chan error, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- store.Add(context.Background(), plan, 7)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	assert.Equal(t, "1400", mr.MustGet("budget:v2:tier:used:member:u-1:tier-high:2026-07"))
}
```

- [ ] **Step 5: 写并发软超额语义测试**

固定已用 `99_000_000`、上限 `100_000_000`，用 barrier 同时放行 20 个成本 `100_000` 的请求；断言开始前检查可以全部通过、最终最大软超额为在途请求总成本，下一请求必定被拒绝。测试名称明确为 `TestGate_ConcurrentWindow_BoundedOvershoot`，不能把“绝不超额”写成验收条件。

- [ ] **Step 6: 运行单元、并发和竞态测试**

Run: `go test ./tests/gateway/budget -run 'TestStore|TestGate_ConcurrentWindow' -race -count=10`

Expected: PASS；整数累计无丢失，已知并发窗口有上界且后续请求被拦截。

- [ ] **Step 7: Commit**

```bash
git add internal/gateway/budget/redis_store.go tests/gateway/budget/redis_store_test.go tests/gateway/budget/concurrency_test.go
git commit -m "test(budget-tier): verify redis v2 dual-write concurrency"
```

### Task 4: 覆盖 Gateway 六类协议、授权顺序和流式语义

**Files:**
- Create: `tests/gateway/handler/model_tier_budget_test.go`
- Modify: `internal/gateway/handler/chat.go`
- Modify: `internal/gateway/handler/responses.go`
- Modify: `internal/gateway/handler/messages.go`
- Modify: `internal/gateway/handler/embeddings.go`
- Modify: `internal/gateway/handler/rerank.go`

- [ ] **Step 1: 建立 handler 组件测试表**

```go
tests := []struct {
	name        string
	description string
	path        string
	body        string
}{
	{"ChatCompletions", "已分级 Chat 请求在上游调用前执行等级检查", "/v1/chat/completions", `{"model":"tiered-group","messages":[{"role":"user","content":"hi"}]}`},
	{"Responses", "Responses 与 Chat 使用同一等级预算语义", "/v1/responses", `{"model":"tiered-group","input":"hi"}`},
	{"Messages", "Anthropic Messages 使用同一等级预算语义", "/v1/messages", `{"model":"tiered-group","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`},
	{"Embeddings", "Embedding 成本进入等级子预算", "/v1/embeddings", `{"model":"tiered-embedding","input":"hi"}`},
	{"Rerank", "Rerank 按 search_units 成本进入等级子预算", "/v1/rerank", `{"model":"tiered-rerank","query":"q","documents":["d"]}`},
	{"VLRerank", "VL Rerank 与文本 Rerank 共用等级门禁", "/v1/rerank", `{"model":"tiered-vl-rerank","query":{"text":"q"},"documents":[{"image":"data:image/png;base64,AA=="}]}`},
}
```

每个子测试注入记录调用次数的 fake provider 与 fake tier gate，断言：未授权时 `403` 且 gate/provider 均未调用；总预算耗尽时 `402 budget_exceeded`；授权且 `$0` 或等级正数额度耗尽时 `402 tier_budget_exceeded` 且 provider 未调用；正数额度未耗尽时 provider 调用一次；未分级时 tier gate 不读等级 Redis key。Gateway 公开错误码只允许这两个小写值。

- [ ] **Step 2: 运行组件测试并确认失败**

Run: `go test ./tests/gateway/handler -run TestModelTierBudget_AllProtocols -count=1 -v`

Expected: FAIL，至少暴露尚未接入等级门禁的协议或授权/预算顺序错误。

- [ ] **Step 3: 统一 handler 调用顺序**

所有纳入范围的 handler 调用 P1 已公开 helper；`dispatch` closure 内完成模型组授权、固定 tier 与路由：

```go
if err := PrepareAuthorizedBudget(c, originalGroup, dispatch, h.budgetGate); err != nil {
	// 复用 handler 现有错误写回分支；ExceededError 调用 WriteHTTPResponse。
	return
}
```

`PrepareAuthorizedBudget(c *gin.Context, originalGroup string, dispatch func() error, gate *budget.Gate) error` 的签名不得在 P3 另造；请求开始后固定的 tier 存入 Gin context，调度期间模型组改等级不得替换本请求快照。`/v1/p/:provider/*` 不调用该 helper。

- [ ] **Step 4: 写流式完成后记账测试**

模拟 SSE 已发送首块后把计数推过上限，断言当前流完整结束、成功成本只记一次、下一请求返回 `402`；上游中途失败且最终 HTTP 状态 `>=400` 时不累计。

- [ ] **Step 5: 运行 handler 全量回归**

Run: `go test ./tests/gateway/handler/... ./tests/gateway/orchestrator/... -race -count=1`

Expected: PASS；六类协议一致，passthrough 仍豁免，流式请求不中断且只记一次。

- [ ] **Step 6: Commit**

```bash
git add tests/gateway/handler/model_tier_budget_test.go internal/gateway/handler
git commit -m "test(gateway): cover tier budgets across protocols"
```

### Task 5: 覆盖七开关、停用恢复和故障注入

**Files:**
- Create: `tests/gateway/budget/fault_test.go`
- Modify: `internal/gateway/budget/gate.go`
- Modify: `internal/gateway/budget/snapshot_holder.go`

- [ ] **Step 1: 写故障矩阵测试**

```go
tests := []struct {
	name        string
	description string
	master      bool
	accounting  bool
	positive    bool
	zeroBlock   bool
	shadow      bool
	v2Total     bool
	legacyWrite bool
	limitMicro  int64
	redisDown   bool
	wantBlock   bool
	wantCount   bool
	wantShadow  bool
}{
	{"主开关关闭", "仍写调用日志等级快照，但不检查、不累计、不计算 would-block", false, true, true, true, true, false, true, 100, false, false, false, false},
	{"完整影子", "累计等级计数并计算 would-block，但正数额度不拦截", true, true, false, false, true, false, true, 100, false, false, true, true},
	{"影子关闭仍累计", "shadow 只控制 would-block，accounting 才控制累计", true, true, false, false, false, false, true, 100, false, false, true, false},
	{"记账关闭", "功能开启但 accounting 关闭时不累计；shadow 可计算 would-block", true, false, false, false, true, false, true, 100, false, false, false, true},
	{"零额度静态阻断", "只由 master 与 zero_block 控制，不依赖 positive/shadow/Redis", true, true, false, true, true, false, true, 0, true, true, false, false},
	{"Redis故障正数放行", "正数额度检查失败时 fail-open 且不执行本请求任何 v2 累计", true, true, true, true, false, true, true, 100, true, false, false, false},
	{"v2总预算切换", "v2 total enforcement 与等级正数开关相互独立", true, true, false, false, true, true, true, 100, false, false, true, true},
	{"停止legacy写", "legacy_total_write 关闭只停止旧总预算 key 写入", true, true, false, false, true, false, false, 100, false, false, true, true},
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/gateway/budget -run TestGate_FaultMatrix -count=1 -v`

Expected: FAIL，直到 `master_enabled`、`accounting_enabled`、`positive_enforcement_enabled`、`zero_block_enabled`、`shadow_enabled`、`v2_total_enforcement_enabled`、`legacy_total_write_enabled` 七个字段及 tier enabled 语义完整实现。

- [ ] **Step 3: 实现 last-known-good 快照失败测试**

先载入 revision 42，再注入损坏 JSON、低 revision 41、未知 `schema_version=99`；断言 holder 始终保留 revision 42，加载错误被观测但请求链不中断。

- [ ] **Step 4: 实现停用后同周期恢复测试**

固定同一周期，停用前累计 `30_000_000`，停用期间制造 `$20` 报表消费，恢复后断言实时额度累计仍为 `$30` 而不是 `$0` 或 `$50`；报表查询为 `$50`。

- [ ] **Step 5: 运行故障测试**

Run: `go test ./tests/gateway/budget -run 'TestGate_FaultMatrix|TestSnapshot_LastKnownGood|TestGate_DisableResume' -race -count=10`

Expected: PASS；正数 fail-open、`$0` fail-closed、快照 LKG 和停用恢复语义稳定。

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/budget/gate.go internal/gateway/budget/snapshot_holder.go tests/gateway/budget/fault_test.go
git commit -m "test(budget-tier): inject redis and snapshot failures"
```

### Task 6: 覆盖 Control Plane 服务、HTTP 组件与审计

**Files:**
- Create: `tests/controlplane/service/model_tier_observability_test.go`
- Create: `tests/controlplane/handler/model_tier_contract_test.go`
- Modify: `internal/controlplane/middleware/audit.go`
- Modify: `internal/controlplane/audit/types.go`

- [ ] **Step 1: 写服务层状态变化测试**

测试表覆盖：首次创建以 `effective_at` 为计数起点；立即降额低于已用需二次确认；下周期计划替换旧计划并保留 `replaced`；`$0`、未配置、停用三态不可混淆；被模型组/额度/历史引用的等级只能归档；Redis 发布失败时 DB 成功且 `sync_status=failed`，reconcile 后转 `synced`。

- [ ] **Step 2: 写 HTTP 组件契约测试**

```go
func TestBudgetTierHandler_ZeroLimitIsConfigured(t *testing.T) {
	// 测试目的：字符串 "0.000000" 是显式禁用，不得被 omitempty 或浮点零值解释为未配置。
	tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	zero, err := budgetdomain.ParseAmount("0.000000")
	require.NoError(t, err)
	router := newBudgetTierRouter(t, fakeBudgetTierService{
		items: []service.TierLimitView{{
			ID: uuid.MustParse("10bb2e65-9c67-45f7-b061-d297a3944876"),
			ModelTierID: tierID,
			ModelTierName: "高级",
			PeriodType: budgetdomain.PeriodMonthly,
			BaseLimitUSD: zero,
			EffectiveLimitUSD: zero,
			Enabled: true,
			EffectiveAt: time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC),
		}},
	})
	w := performJSON(router, http.MethodGet,
		"/api/v1/budgets/20bb2e65-9c67-45f7-b061-d297a3944876/tier-limits", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			TierLimits []service.TierLimitView `json:"tier_limits"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data.TierLimits, 1)
	assert.Equal(t, tierID, body.Data.TierLimits[0].ModelTierID)
	assert.Equal(t, "0.000000", body.Data.TierLimits[0].BaseLimitUSD.String())
	assert.Equal(t, "0.000000", body.Data.TierLimits[0].EffectiveLimitUSD.String())
}
```

- [ ] **Step 3: 验证普通用户脱敏与 RBAC**

普通用户读取混合预算来源时只能看到限制层级、状态、reset time，不出现上级 `scope_id`、主体名称和精确金额；平台、工作室、部门管理员只能操作各自管辖范围。对同一未授权模型组，先返回 `403`，不能返回等级预算 `402`。

- [ ] **Step 4: 通过真实写请求验证审计**

逐一发送等级创建、修改、排序、停用、归档、删除、组关联、直接调额、批量调额、重建和功能开关变更 HTTP 请求，再从审计查询接口读取结果。断言包含 `request_id`、reason、before/after、effective_at、actor、resource ID；请求/响应中的 token、secret 和上级敏感金额被脱敏。验收只依据请求结果与审计记录。

- [ ] **Step 5: 运行服务与组件测试**

Run: `go test ./tests/controlplane/service/... ./tests/controlplane/handler/... -run 'ModelTier|BudgetTier' -race -count=1`

Expected: PASS；服务状态机、RBAC、脱敏和审计均通过。

- [ ] **Step 6: Commit**

```bash
git add tests/controlplane/service/model_tier_observability_test.go tests/controlplane/handler/model_tier_contract_test.go internal/controlplane/middleware/audit.go internal/controlplane/audit/types.go
git commit -m "test(controlplane): cover tier budget audit and scope"
```

### Task 7: 固化 FE↔BE 与 verify-key 契约

**Files:**
- Modify: `tests/contract/contracts.json`
- Create: `tests/contract/model_tier_contract_test.go`
- Modify: `internal/gateway/infra/auth_client.go`

- [ ] **Step 1: 扩展 JSON 契约**

增加 model tier CRUD、组关联、tier limits、batch preview/apply、tier usage、my budget tiers、risk events、expansion precheck/decision，以及 verify-key `tier_limits` 字段；`/my/budget/tiers` 固定 `data.source_summary` 四字段、`personal|shared|mixed` mode、`EffectiveTotalPeriod[]` 及 `budget_chain/tier_limits/data_updated_at/stale`。`observability.log_row_fields` 同时固定 eligible 与五个 nullable `budget_block_*` 字段。金额字段统一声明为 6 位小数字符串。Gateway 契约只包含小写 `budget_exceeded`、`tier_budget_exceeded`；Control Plane 管理 API 保留技术设计列出的大写业务错误码，两类错误码不得混用。

- [ ] **Step 2: 写契约结构测试**

```go
func TestVerifyKeyContract_TierLimitsOnlyConfigured(t *testing.T) {
	// 测试目的：verify-key 只下发已配置等级额度，未配置周期不伪造无限值，金额保持字符串精度。
	payload := loadFixture(t, "testdata/verify_key_tier_limits.json")
	var got gatewaymodel.UserIdentity
	require.NoError(t, json.Unmarshal(payload, &got))
	require.Len(t, got.BudgetChain[0].TierLimits, 1)
	assert.Equal(t, "80.000000", got.BudgetChain[0].TierLimits[0].BaseLimitUSD)
	assert.Equal(t, "100.000000", got.BudgetChain[0].TierLimits[0].EffectiveLimitUSD)
}
```

- [ ] **Step 3: 添加快照向前兼容测试**

旧 Gateway 读取新增 verify-key 字段必须忽略未知字段并保持总预算可用；新 Gateway 读取无 `tier_limits` 的旧 payload 必须视为“未配置等级限制”，不能视为 `$0`。

- [ ] **Step 4: 运行契约测试**

Run: `go test ./tests/contract -count=1 -v`

Expected: PASS；JSON 文档可解析、金额类型一致、新旧 payload 双向兼容。

- [ ] **Step 5: Commit**

```bash
git add tests/contract/contracts.json tests/contract/model_tier_contract_test.go internal/gateway/infra/auth_client.go
git commit -m "test(contract): lock model tier budget payloads"
```

### Task 8: 实现并验证指标、日志、Trace 与审计关联

**Files:**
- Modify: `internal/gateway/budget/metrics.go`
- Create: `internal/gateway/budget/telemetry.go`
- Create: `internal/controlplane/service/budget_tier_metrics.go`
- Modify: `internal/gateway/middleware/tracing.go`
- Create: `tests/gateway/budget/telemetry_test.go`
- Create: `tests/controlplane/service/model_tier_metrics_test.go`

- [ ] **Step 1: 用 OTel manual reader 写指标失败测试**

断言以下指标名称和低基数标签：

```text
gateway_budget_check_total{dimension,result,period,scope_type}
gateway_budget_tier_reject_total{tier_id,scope_type,period}
gateway_budget_fail_open_total{dimension}
gateway_budget_shadow_would_block_total{dimension,tier_id}
controlplane_budget_config_sync_total{result}
controlplane_expansion_apply_total{dimension,duration,result}
```

禁止把 `scope_id`、`user_id`、`key_id`、`request_id` 放入指标标签；这些高基数字段只进入日志/Trace。

- [ ] **Step 2: 实现指标记录器并运行测试**

Run: `go test ./tests/gateway/budget ./tests/controlplane/service -run 'Telemetry|Metrics' -count=1 -v`

Expected: 第一次 FAIL；实现后 PASS，manual reader 中每个场景恰好增加 1。

- [ ] **Step 3: 写结构化日志捕获测试**

用 `slog.NewJSONHandler(bytes.Buffer)` 注入 logger，制造 allow、reject、fail-open、shadow-would-block；断言日志包含：

```text
request_id group_name budget_tier_id dimension_type scope_type period_type
used_micro_usd limit_micro_usd fail_open shadow_enabled
```

并断言不包含 API Key 明文、Authorization header、上级主体名称和普通用户不可见金额。

- [ ] **Step 4: 写 Trace 属性测试**

使用 OTel `tracetest.SpanRecorder` 发起一次放行和一次拒绝请求，断言入口 span 含：

```text
llm.budget_tier_id
budget.check_result
budget.block_reason
budget.shadow_enabled
```

拒绝 span 设置 error 状态，`request_id` 能关联同次结构化日志和审计记录。

- [ ] **Step 5: 运行观测回归**

Run: `go test ./tests/gateway/budget ./tests/gateway/middleware ./tests/controlplane/service -run 'Telemetry|Metrics|Tracing|Audit' -race -count=1`

Expected: PASS；指标低基数、日志可关联、Trace 属性齐全、审计包含变更前后值。

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/budget/metrics.go internal/gateway/budget/telemetry.go internal/controlplane/service/budget_tier_metrics.go internal/gateway/middleware/tracing.go tests/gateway/budget/telemetry_test.go tests/controlplane/service/model_tier_metrics_test.go
git commit -m "feat(observability): instrument model tier budgets"
```

### Task 9: 建立真实 PostgreSQL + Redis 端到端集成测试

**Files:**
- Create: `tests/integration/model_tier_budget_flow_test.go`
- Create: `tests/integration/model_tier_budget_test_helpers.go`

- [ ] **Step 1: 编写完全自建数据的集成测试 helper**

helper 仅从环境变量读取 `DATABASE_URL`、`REDIS_ADDR`、`REDIS_PASSWORD`、`CP_URL`、`GW_URL`、`JWT_SECRET`、`INTERNAL_TOKEN`；每次测试使用 UUID 创建公司/工作室/部门/成员/Key/provider/model/group/tier/预算，`t.Cleanup` 删除自身数据，禁止 `FlushDB` 和硬编码共享 Key。

- [ ] **Step 2: 写金线路径测试**

```go
func TestModelTierBudget_FullFlow(t *testing.T) {
	// 测试目的：从管理 API 创建等级与额度，经 verify-key 和快照到 Gateway，请求成功后同时累计总预算及等级预算并落 call_logs 快照。
	env := newTierBudgetEnv(t)
	env.CreateTier("高级")
	env.AssignGroup("tiered-chat")
	env.SetTotalMonthly("100.000000")
	env.SetTierMonthly("10.000000")
	env.PublishSnapshotWithFlags(configsync.BudgetTierSnapshot{
		SchemaVersion: 1,
		MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: false, ZeroBlockEnabled: false,
		ShadowEnabled: true, V2TotalEnforcementEnabled: false,
		LegacyTotalWriteEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	resp := env.CallChat("tiered-chat")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	env.RequireV2TotalUsedPositive()
	env.RequireV2TierUsedPositive()
	env.RequireCallLogTierSnapshot("高级")
}
```

- [ ] **Step 3: 扩展核心 E2E 场景**

同文件增加：未分级只累计总预算；首次创建不回填 `effective_at` 之前消费；模型组换等级后历史快照不变；等级重命名不改历史；日/周/月只检查已配置周期；总预算先耗尽与等级预算先耗尽都在上游前 `402`；价格/用量缺失按 `$0` 成功；失败请求不记账；Service Key 使用 `dept_service`；个人 Key 共享 member 额度。

- [ ] **Step 4: 运行真实依赖集成测试**

Run:

```bash
go test ./tests/integration -tags=integration \
  -run 'TestModelTierBudget_FullFlow|TestModelTierBudget_' \
  -count=1 -v -timeout=15m
```

Expected: PASS；无 skip、无共享数据依赖、清理完成；Redis v2 和 `call_logs` 金额差异为 0 微美元。

- [ ] **Step 5: Commit**

```bash
git add tests/integration/model_tier_budget_flow_test.go tests/integration/model_tier_budget_test_helpers.go
git commit -m "test(integration): add tier budget full flow"
```

### Task 10: 验证额度下发、临时覆盖与阻断风险来源

**Files:**
- Modify: `tests/integration/model_tier_budget_flow_test.go`
- Create: `tests/integration/model_tier_budget_risk_source_test.go`

- [ ] **Step 1: 验证 verify-key 只下发已配置 `tier_limits`**

用管理 API 为 member allocation 配置月基础额度 `"80.000000"`，日/周不配置；调用 `/internal/v1/auth/verify-key` 并解析预算链，断言该 scope 只有一个 monthly item，字段精确为：

```json
{
  "tier_id": "00bb2e65-9c67-45f7-b061-d297a3944876",
  "tier_name": "高级",
  "period_type": "monthly",
  "base_limit_usd": "80.000000",
  "effective_limit_usd": "80.000000",
  "enabled": true,
  "effective_at": "2026-07-11T08:00:00Z"
}
```

Expected: 未配置日/周周期不出现，不伪造无限或 `$0`；新旧总预算字段仍兼容。

- [ ] **Step 2: 验证 disabled tier 保留快照但不计数**

创建已分级模型组和已配置额度，先成功调用一次；随后停用 tier 并等待 snapshot revision 收敛，再调用同一模型组。断言请求成功且 `call_logs.budget_tier_id`、`budget_tier_name_snapshot` 仍保存请求开始时 ID/name，`budget_tier_accounting_eligible=false`，报表 `reported_cost_usd` 增加但 `accounting_cost_usd` 与 Redis tier counter 不增加。恢复 tier 后同周期继续使用停用前 counter，不补算停用区间。

- [ ] **Step 3: 验证 current-period temporary override 全链路**

通过 P2 扩容 API 创建并批准 `dimension_type=tier`、`duration_type=current_period`、target `"120.000000"` 的申请，等待 P0 publisher revision 增加和 KeyCache 失效，再次调用 verify-key。测试先执行 `period, err := budgetperiod.Resolve(fixedNow, budgetdomain.PeriodMonthly, periodConfig)` 并断言无错误，再断言 `base_limit_usd="80.000000"`、`effective_limit_usd="120.000000"`、`expires_at=period.ResetAt`；Gateway 在 `$80` 至 `$120` 区间继续放行。把时钟推进到 reset boundary，只调用 P0 `BudgetLifecycleExecutor.RunOnce`；断言 override 到期、verify-key 恢复届时基础额度、历史 counter 不删除，P2 `BudgetLifecycleObserver` 恰好发送一次恢复通知且没有第二个 worker。

- [ ] **Step 4: 验证 `/my/budget/tiers` 完整 P0 DTO**

调用 `GET /api/v1/my/budget/tiers`，断言 envelope 与 P0 完整 DTO：

```json
{
  "data": {
    "source_summary": {
      "mode": "mixed",
      "primary_scope_type": "member",
      "has_shared_pool": true,
      "has_temporary_override": true
    },
    "effective_total": [{
      "period_type": "monthly",
      "configured": true,
      "base_limit_usd": "300.000000",
      "temporary_target_usd": "350.000000",
      "effective_limit_usd": "350.000000",
      "used_usd": "20.000000",
      "remaining_usd": "330.000000",
      "reset_at": "2026-08-01T00:00:00+08:00"
    }],
    "budget_chain": [
      {"scope_type":"member","label":"我的额度","visible_amounts":true},
      {"scope_type":"dept_daily","label":"部门成员日常预算","visible_amounts":false},
      {"scope_type":"studio","label":"工作室预算","visible_amounts":false},
      {"scope_type":"company","label":"公司预算","visible_amounts":false}
    ],
    "tier_limits": [{
      "model_tier_id": "2f63d65a-a0f7-4abe-b9d0-3ace39a44f30",
      "model_tier_name": "高级",
      "periods": [{
        "period_type": "monthly",
        "configured": true,
        "base_limit_usd": "80.000000",
        "temporary_target_usd": "100.000000",
        "effective_limit_usd": "100.000000",
        "used_usd": "20.000000",
        "remaining_usd": "80.000000",
        "reset_at": "2026-08-01T00:00:00+08:00",
        "status": "available",
        "limiting_source": {
          "scope_type": "member",
          "remaining_usd": "80.000000"
        }
      }]
    }],
    "data_updated_at": "2026-07-11T08:00:00Z",
    "stale": false
  }
}
```

分别构造个人额度、共享池、混合来源三种业务数据。每个测试完整断言 `source_summary.mode/primary_scope_type/has_shared_pool/has_temporary_override`，其中 mode 只取 `personal|shared|mixed` 且 source summary 不含 `label`；完整断言 `effective_total` 是 `EffectiveTotalPeriod` 数组，并逐项校验 period/configured/base/temporary/effective/used/remaining/reset；同时完整断言 `budget_chain`、`tier_limits`、`data_updated_at`、`stale`。共享和混合场景的上级链只返回允许展示的摘要，不泄露上级精确金额；last-known 场景断言 `stale=true` 且保留最后成功的 `data_updated_at`。

- [ ] **Step 5: 验证仅五个 `budget_block_*` 是 request-blocked 风险事实源**

分别制造 total 与 tier 超限请求，断言 Gateway 返回 `budget_exceeded` / `tier_budget_exceeded`，对应 `call_logs` 精确保存首要原因：

```text
budget_block_code
budget_block_dimension
budget_block_scope_type
budget_block_scope_id
budget_block_period_type
```

集成测试必须按 nullable string 读取，不能把数据库 NULL 扫描进普通 `string`：

```go
var code, dimension, scopeType, scopeID, periodType sql.NullString
err := sqlDB.QueryRowContext(ctx, `
	SELECT budget_block_code, budget_block_dimension,
	       budget_block_scope_type, budget_block_scope_id,
	       budget_block_period_type
	FROM call_logs
	WHERE request_id = $1
`, requestID).Scan(&code, &dimension, &scopeType, &scopeID, &periodType)
require.NoError(t, err)
require.True(t, code.Valid)
require.True(t, dimension.Valid)
require.True(t, scopeType.Valid)
require.True(t, scopeID.Valid)
require.True(t, periodType.Valid)
```

tier 场景断言 `dimension.String == "tier"` 且 scope/period 指向真正阻断约束；total 场景断言 `dimension.String == "total"`。成功调用的同五列用相同 `sql.NullString` 读取并全部 `Valid == false`。随后调用 `GET /api/v1/budget/risk-events?risk_type=request_blocked`，断言事件及阻断次数由这些列聚合，未使用结构化日志猜测，普通用户响应不暴露 `budget_block_scope_id`。

tier ID/name/eligible 是独立快照/计数字段，不属于阻断字段；端到端持久化与查询只断言上述五个阻断值，其他阻断原因只进结构化日志，不扩展 call log 阻断契约。

- [ ] **Step 6: 运行端到端测试**

Run:

```bash
go test ./tests/integration -tags=integration \
  -run 'TestModelTierBudget_(VerifyKeyTierLimits|DisabledTierSnapshot|TemporaryOverride|MyBudgetTiers|BlockedRiskSource)' \
  -count=1 -v -timeout=20m
```

Expected: PASS；verify-key、disabled snapshot、唯一 lifecycle executor/observer、完整 my-budget DTO、Gateway 402、五个 call log block columns 与风险事件形成可追溯闭环。

- [ ] **Step 7: Commit**

```bash
git add tests/integration/model_tier_budget_flow_test.go tests/integration/model_tier_budget_risk_source_test.go
git commit -m "test(integration): verify tier budget propagation and risk source"
```

### Task 11: 建立集成级故障注入与重建验证

**Files:**
- Create: `tests/integration/model_tier_budget_fault_test.go`
- Modify: `tests/controlplane/service/budget_counter_rebuild_service_test.go`

- [ ] **Step 1: 写 Redis 中断场景**

通过专用测试 Redis 容器执行 `CLIENT PAUSE` 或停止容器，不能影响共享 Redis。断言正数额度请求放行、`gateway_budget_fail_open_total{dimension="tier"}` 增加、对应 v2 tier key 不累计、`call_logs` 仍写入；恢复 Redis 后 reconcile 从日志补齐。

- [ ] **Step 2: 写 `$0` 故障场景**

在 Redis 停止时调用 `$0` 等级，断言 `402 tier_budget_exceeded`、上游请求计数为 0、响应不暴露上级金额；关闭正数 enforcement 不影响该结果，只有显式关闭 `zero_block_enabled` 或 `master_enabled` 才放行。

- [ ] **Step 3: 写快照发布失败与 LKG 场景**

让 Control Plane DB 事务成功、Redis snapshot 发布失败，断言 API 返回 `sync_status=failed`、审计落库；Gateway 保留旧 revision。恢复 Redis 并执行 reconcile 后 revision 递增、KeyCache 失效、状态转 `synced`。

- [ ] **Step 4: 写计数丢失重建场景**

删除单个测试 scope 的 v2 keys，以 `periodConfig := budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"}` 构造 P1 已公开的 `service.NewBudgetCounterRebuildService(db, rdb, periodConfig)`，再调用 `RebuildCurrentPeriods(ctx, fixedNow)`；断言 total 汇总所有成功日志，tier 只汇总 `budget_tier_id IS NOT NULL AND budget_tier_accounting_eligible = TRUE`。重建须取得全局 lease、等待 heartbeat inflight 归零、drain call-log Stream，再从 PostgreSQL keyset 分页聚合到 run 临时 Redis hash，分批构建新 generation 并单 Lua 切换 active pointer；重建值与事实源一致。不得引用不存在的 `internal/gateway/budget/reconciler.go`。

- [ ] **Step 5: 运行故障集成测试**

Run: `go test ./tests/integration -tags=integration -run TestModelTierBudget_Fault -count=1 -v -timeout=20m`

Expected: PASS；故障期间行为符合 fail-open/fail-closed 契约，恢复后数据可收敛。

- [ ] **Step 6: Commit**

```bash
git add tests/integration/model_tier_budget_fault_test.go tests/controlplane/service/budget_counter_rebuild_service_test.go
git commit -m "test(integration): verify tier budget recovery"
```

### Task 12: 增加基准测试和 1k RPS 性能门禁

**Files:**
- Create: `tests/gateway/budget/benchmark_test.go`
- Create: `tests/performance/model_tier_budget.js`

- [ ] **Step 1: 写 Go micro-benchmark**

```go
func BenchmarkGateCheck_TwoScopesThreePeriods(b *testing.B) {
	gate := newBenchmarkGate(b, 2, 3)
	c := newBenchmarkGinContext(b, 2, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := gate.Check(c); err != nil {
			b.Fatal(err)
		}
	}
}
```

基准同时提供 tier enforcement 关闭、shadow、enforcement 开启三组结果，输出 `ns/op`、`B/op`、`allocs/op`。

- [ ] **Step 2: 写 k6 负载脚本**

脚本使用环境变量 `BASE_URL`、`API_KEY`、`MODEL`，不保存密钥；分 2 分钟预热、5 分钟 1k RPS 稳态、2 分钟降载。threshold：

```javascript
export const options = {
  scenarios: {
    tier_budget: {
      executor: "constant-arrival-rate",
      rate: 1000,
      timeUnit: "1s",
      duration: "5m",
      preAllocatedVUs: 300,
      maxVUs: 1500,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(99)<2000"],
    budget_check_ms: ["p(99)<2"],
  },
}
```

- [ ] **Step 3: 采集基线和功能开启结果**

Run:

```bash
go test ./tests/gateway/budget -run '^$' -bench BenchmarkGateCheck -benchmem -count=5
k6 run -e BASE_URL="$BASE_URL" -e API_KEY="$API_KEY" -e MODEL="$MODEL" tests/performance/model_tier_budget.js
```

Expected: Redis Lua p99 `<5ms`，预算检查额外 p99 `<2ms`，1k RPS 下 Gateway CPU 相对关闭基线增幅 `<5%`，错误率 `<1%`。若任一门槛失败，不进入正数额度灰度。

- [ ] **Step 4: Commit**

```bash
git add tests/gateway/budget/benchmark_test.go tests/performance/model_tier_budget.js
git commit -m "perf(budget-tier): add enforcement load gates"
```

### Task 13: 验证 Atlas Expand 迁移和 schema 一致性

**Files:**
- Modify: `.gitlab-ci.yml`
- Verify: `atlas-migrations/*.sql`
- Verify: `atlas-migrations/atlas.sum`

- [ ] **Step 1: 在最新目标分支基础上确认迁移线性**

Run:

```bash
git fetch origin
for branch in test main; do
  git ls-tree -r "origin/$branch" atlas-migrations/ |
    awk '{print $4}' | sort | tail -1
done
```

Expected: 本功能迁移时间戳严格晚于 test/main 的最新迁移；若尚未共享，可 rebase 后按 Atlas skill 重生成，禁止手工编辑 `atlas.sum`。

- [ ] **Step 2: 执行目录校验**

Run:

```bash
atlas migrate hash --env local
git diff --exit-code -- atlas-migrations/atlas.sum
atlas migrate validate --env local
```

Expected: `atlas.sum` 无未提交漂移；validate 返回成功，无 checksum mismatch、missing migration 或 non-linear history。

- [ ] **Step 3: 在空临时库执行全量迁移**

Run:

```bash
PG_CID="$(docker run -d -P \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=tier_atlas postgres:16)"
trap 'docker rm -f "$PG_CID" >/dev/null 2>&1 || true' EXIT INT TERM
PG_PORT="$(docker port "$PG_CID" 5432/tcp | awk -F: 'END {print $NF}')"
for i in $(seq 1 60); do
  docker exec "$PG_CID" pg_isready -U postgres -d tier_atlas >/dev/null && break
  test "$i" -lt 60 || exit 1
  sleep 1
done
export DATABASE_URL="postgres://postgres:postgres@127.0.0.1:${PG_PORT}/tier_atlas?sslmode=disable"
atlas migrate apply --env local
atlas migrate status --env local
```

Expected: Docker 自动分配宿主机随机端口，脚本退出或中断时 trap 删除容器；空库不使用 baseline；status 显示 `Migration Status: OK` 且无 pending migration。

- [ ] **Step 4: 验证旧 schema 到 Expand schema**

在第二个临时库先 apply 到本功能前一版本，再 apply 当前目录；同时运行旧版 Gateway/Control Plane 二进制做 `/health`、verify-key、总预算读写冒烟。Expected: 迁移只增不减，无 `DROP`/`RENAME`/无默认值 `NOT NULL`/收窄类型，旧应用仍可读写。

- [ ] **Step 5: 在 CI 增加 `atlas-validate` job**

job 位于 `test` stage，执行 hash dry check、`atlas migrate validate --env local` 和空 PostgreSQL apply/status；只读 `atlas-migrations`，不连接共享开发库。

- [ ] **Step 6: Commit**

```bash
git add .gitlab-ci.yml
git commit -m "ci(atlas): validate tier budget expand migration"
```

### Task 14: 增加 CI test stage 与分层门禁

**Files:**
- Modify: `.gitlab-ci.yml`

- [ ] **Step 1: 在 build 前新增 test stage**

```yaml
stages:
  - test
  - build
  - migrate
  - deploy
  - verify
```

- [ ] **Step 2: 增加 Go 单元/组件/契约 job**

```yaml
test-go:
  stage: test
  image: golang:1.26
  tags: [docker]
  script:
    - go test ./tests/budgetperiod/... ./tests/configsync/... ./tests/gateway/... ./tests/controlplane/service/... ./tests/controlplane/handler/... ./tests/contract/... -count=1 -race -coverprofile=coverage.out
    - go tool cover -func=coverage.out
    - go build ./...
  artifacts:
    when: always
    paths: [coverage.out]
  rules:
    - changes:
        - internal/**/*
        - cmd/**/*
        - tests/**/*
        - go.mod
        - go.sum
```

Expected: 任一单元、组件、契约失败或编译失败即阻止 build；`//go:build integration` 测试不在此 job 运行。

- [ ] **Step 3: 增加 vue-tsc/Vite 前端门禁**

```yaml
test-web:
  stage: test
  image: node:22
  tags: [docker]
  script:
    - cd web
    - npm ci
    - npx vue-tsc --noEmit
    - npx vite build
```

Expected: TypeScript 类型检查与 Vite build 任一失败即阻止 build；不运行额外前端测试运行器。

- [ ] **Step 4: 增加隔离依赖的集成 job**

使用 GitLab services 启动 PostgreSQL 16 和 Redis 7，先在空库执行 Atlas apply，再启动 Control Plane/Gateway/fake upstream，运行：

```bash
go test ./tests/integration -tags=integration \
  -run ModelTierBudget -count=1 -v -timeout=20m
```

Expected: 无硬编码内网地址、无 skip；失败时保留服务日志、Atlas status 和仅测试前缀的 Redis key 清单作为 artifact。

- [ ] **Step 5: 设置 job 依赖**

所有 test branch 的 build jobs `needs` 包含 `test-go`、`test-web`、`atlas-validate`、`test-integration-tier-budget`；`migrate-test` 只在上述 job 全部成功后执行。生产 tag 复用同样门禁，不允许仅运行镜像构建。

- [ ] **Step 6: 验证 CI 配置**

Run:

```bash
gitlab-ci-lint .gitlab-ci.yml
go test ./tests/budgetperiod/... ./tests/configsync/... ./tests/gateway/... ./tests/controlplane/service/... ./tests/controlplane/handler/... ./tests/contract/... -count=1
go build ./...
(cd web && npm ci && npx vue-tsc --noEmit && npx vite build)
```

Expected: CI lint valid；Go 业务单元/组件/契约、TypeScript 和 Vite build 全绿。

- [ ] **Step 7: Commit**

```bash
git add .gitlab-ci.yml
git commit -m "ci: enforce tests before tier budget deploy"
```

### Task 15: 验证 P1 重建 API 与完整周期影子验收

**Files:**
- Modify: `tests/controlplane/service/budget_counter_rebuild_service_test.go`
- Create: `tests/integration/model_tier_budget_reconcile_test.go`
- Create: `docs/runbooks/model-tier-budget-rollout.md`

- [ ] **Step 1: 按 P1 公开 API 写重建/对账集成测试**

输入固定 `now`，准备当前日/周/月的 total 日志、tier eligible 日志、tier ineligible 日志与 Redis v2 值；只通过 P1 三参数构造器和公开方法执行，断言结果字段：

```json
{
  "key": "budget:v2:tier:used:member:u-1:tier-high:2026-07",
  "expected_micro_usd": 10000000,
  "actual_micro_usd": 9990000,
  "abs_diff_micro_usd": 10000,
  "diff_ratio": 0.001,
  "allowed_diff_micro_usd": 1000000,
  "within_threshold": true
}
```

- [ ] **Step 2: 验证服务与 HTTP 公开边界**

```go
periodConfig := budgetperiod.Config{
	Timezone: "Asia/Shanghai",
	WeekStart: "monday",
}
svc := service.NewBudgetCounterRebuildService(db, rdb, periodConfig)
result, err := svc.RebuildCurrentPeriods(ctx, fixedNow)
require.NoError(t, err)
require.NotEmpty(t, result.Diffs)
```

服务按 P1 定义使用全局 lease、heartbeat inflight ZSET、可靠 Stream drain 和 run 临时 Redis hashes；新 generation 全部构建后才以一个 Lua 切换 active pointer。lease 丢失或任一查询失败时指针不变，新 namespace 清理，旧 generation 可回滚。HTTP `POST /api/v1/budget/rebuild-counters` 返回同一 `diffs` 与 `cutover_ready`，任何越界项都令 `cutover_ready=false`，但不得自动修改七个开关。

- [ ] **Step 3: 运行对账测试**

Run: `go test ./tests/controlplane/service ./tests/integration -tags=integration -run 'TestBudgetCounterRebuildV2|TestBudgetTierReconcile' -count=1 -v`

Expected: PASS；ineligible 日志不进入 tier counter，total/tier 重建值正确，临时 key 原子切换，失败不覆盖线上 counter。

- [ ] **Step 4: 编写完整周期影子操作步骤**

runbook 明确：

1. 全量 Gateway 已升级到支持 snapshot schema 1、v2 双写和 shadow mode 的同一版本。
2. 七开关固定为：`master_enabled=true`、`accounting_enabled=true`、`positive_enforcement_enabled=false`、`zero_block_enabled=false`、`shadow_enabled=true`、`v2_total_enforcement_enabled=false`、`legacy_total_write_enabled=true`。
3. 调用日志等级快照始终写入；eligible=true 的请求写 v2 等级计数，v2 总预算和旧总预算 key 同时写入。
4. 影子期覆盖已计划启用的最长周期：启用月额度时至少一个完整自然月；只启用周额度时至少一个完整预算周。
5. 每日运行 reconcile 并归档 JSONL、指标截图、异常说明；周期结束执行全量对账。
6. 所有 scope/tier 偏差小于 `max(0.1%, $1/day)`，无连续 24 小时增长趋势，才允许进入 `$0` 启用。

- [ ] **Step 5: 运行重建 HTTP smoke**

Run:

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer $ADMIN_JWT" \
  -H 'Content-Type: application/json' \
  "$CP_URL/api/v1/budget/rebuild-counters" \
  -d '{}' | tee reconcile.json | jq -e \
  '.cutover_ready == true and ([.diffs[] | select(.within_threshold == false)] | length == 0)'
```

Expected: curl 与 jq 均退出 0；`reconcile.json` 可归档，响应不改变任何开关。

- [ ] **Step 6: Commit**

```bash
git add tests/controlplane/service/budget_counter_rebuild_service_test.go tests/integration/model_tier_budget_reconcile_test.go docs/runbooks/model-tier-budget-rollout.md
git commit -m "test(budget-tier): verify shadow reconciliation workflow"
```

### Task 16: 执行 Gateway 全量升级、`$0` 后正数灰度与回滚演练

**Files:**
- Modify: `deploy/blue-green/deploy-blue-green.sh`
- Modify: `deploy/blue-green/docker-compose.blue.yml`
- Modify: `deploy/blue-green/docker-compose.green.yml`
- Create: `docs/runbooks/model-tier-budget-rollback.md`

- [ ] **Step 1: 透传七个 canonical 非密钥运行时开关**

compose 明确传入：

```yaml
BUDGET_TIER_MASTER_ENABLED: ${BUDGET_TIER_MASTER_ENABLED:-false}
BUDGET_TIER_ACCOUNTING_ENABLED: ${BUDGET_TIER_ACCOUNTING_ENABLED:-false}
BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED: ${BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED:-false}
BUDGET_TIER_ZERO_BLOCK_ENABLED: ${BUDGET_TIER_ZERO_BLOCK_ENABLED:-false}
BUDGET_TIER_SHADOW_ENABLED: ${BUDGET_TIER_SHADOW_ENABLED:-false}
BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED: ${BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED:-false}
BUDGET_LEGACY_TOTAL_WRITE_ENABLED: ${BUDGET_LEGACY_TOTAL_WRITE_ENABLED:-true}
```

测试环境通过 CI SSH export 注入；生产由受控部署变量注入。变量不含密钥，可设置安全默认值，但消费方必须完成 Viper/env binding。禁止 `SHADOW_MODE`、`V2_WRITE_ENABLED`、`LEGACY_DUAL_WRITE_ENABLED` 等别名。

- [ ] **Step 2: 在部署脚本加入版本与模式 preflight**

切流前请求新旧颜色内部状态端点，断言新颜色支持 `tier_snapshot_schema=1`、`redis_v2=true` 并回显七个 canonical 字段；只要任一旧 Gateway 不支持该能力，强制安全关闭态：`master=false`、`accounting=false`、`positive=false`、`zero=false`、`shadow=false`、`v2_total=false`、`legacy_total_write=true`。脚本日志输出模式，不输出密钥或完整配置快照。

- [ ] **Step 3: 测试环境执行全量升级**

Run:

```bash
git push origin test
# CI 顺序必须为 test → build → migrate → deploy → verify
curl -fsS "$TEST_GATEWAY_URL/ready" | jq -e \
  '.capabilities.tier_snapshot_schema == 1 and .capabilities.redis_v2 == true'
```

Expected: 所有 Gateway 实例均为支持版本；进入影子态前先设置 `master=true, accounting=true, positive=false, zero=false, shadow=true, v2_total=false, legacy_total_write=true`；关闭态不累计 tier counter，影子态累计并计算 would-block 但不执行正数或 `$0` 阻断；旧总预算行为无回归。

- [ ] **Step 4: 完成一个完整周期影子对账**

按 Task 15 runbook 连续运行；期间要求：

- `gateway_budget_shadow_would_block_total` 可解释且与离线重放一致。
- `gateway_budget_fail_open_total{dimension="tier"}` 无持续增长。
- `controlplane_budget_config_sync_total{result="failed"}` 24 小时窗口为 0。
- snapshot revision 在所有 Gateway 一致。
- v2 与日志差异小于 `max(0.1%, $1/day)`。
- p99、CPU 和 Redis Lua 达到 Task 12 门槛。

Expected: 形成完整周期证据包；任一条件不满足则延长影子期，不启用阻断。

- [ ] **Step 5: 先启用 `$0` 静态禁用**

仅选内部测试等级和内部 Key，设置 `$0` 并切换为 `master=true, accounting=true, positive=false, zero=true, shadow=true, v2_total=false, legacy_total_write=true`。10 分钟后扩大到一个部门，观察 24 小时，再全量启用。`$0` 阻断只由 `master && zero` 决定，shadow 不得绕过它。每阶段手工 E2E：

```bash
curl -sS -o zero.json -w '%{http_code}\n' \
  -H "Authorization: Bearer $E2E_API_KEY" \
  -H 'Content-Type: application/json' \
  "$BASE_URL/v1/chat/completions" \
  -d '{"model":"tier-zero-group","messages":[{"role":"user","content":"probe"}]}'
jq -e '.error.code == "tier_budget_exceeded"' zero.json
```

Expected: HTTP 402、上游调用计数不变、无上级金额泄露；Redis 停止演练时结果仍为 402。

- [ ] **Step 6: 再启用正数额度**

先只为内部 Key 对应 scope 保留正数 tier limits，其余 scope 暂不配置正数 tier limit；把开关切换为 `master=true, accounting=true, positive=true, zero=true, shadow=false, v2_total=false, legacy_total_write=true`。随后按内部 Key 1% → 单部门 5% → 单工作室 25% → 50% → 100% 扩大已配置正数 tier limit 的 scope；每阶段至少观察 24 小时且跨一次上海日边界。七个开关是全局模式，不用百分比直接改变开关；灰度范围由 Control Plane 配置的 scope/tier limits 控制。

每阶段通过条件：

- 非预期 402 比率 `<0.1%`，且每条可由额度/用量解释。
- fail-open 在 15 分钟窗口为 0 或有已确认 Redis 事件。
- p99 增量 `<2ms`、CPU 增幅 `<5%`。
- 对账偏差仍在阈值内。
- 客诉和审计异常为 0。

- [ ] **Step 7: 定义自动停止与回滚条件**

任一条件触发立即停止扩大并关闭正数 enforcement：

- 非预期等级 402 `>=0.1%` 持续 5 分钟。
- 任一 scope/tier 对账偏差 `>max(0.1%, $1/day)`。
- tier fail-open 连续 5 分钟增长或 Redis Lua p99 `>=5ms` 持续 10 分钟。
- Gateway p99 额外延迟 `>=2ms` 或 CPU 增幅 `>=5%` 持续 15 分钟。
- snapshot revision 分裂超过 2 个 reconcile 周期（60 秒）。
- 调用日志等级快照缺失率 `>0.01%`。
- `$0` 在 Redis 故障时未阻断，或 403/402 顺序泄露权限信息。
- migration/schema drift、数据损坏或无法解释的重复记账。

- [ ] **Step 8: 演练普通回滚**

```bash
# 1. 先关闭正数等级拦截，恢复完整影子态
export BUDGET_TIER_MASTER_ENABLED=true
export BUDGET_TIER_ACCOUNTING_ENABLED=true
export BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED=false
export BUDGET_TIER_ZERO_BLOCK_ENABLED=true
export BUDGET_TIER_SHADOW_ENABLED=true
export BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED=false
export BUDGET_LEGACY_TOTAL_WRITE_ENABLED=true

# 2. 若应用版本异常，切回上一蓝绿颜色
cd /opt/llm-gateway
bash deploy-blue-green.sh --rollback

# 3. 验证总预算仍工作、等级正数不阻断、v2 数据仍保留
curl -fsS http://127.0.0.1:8080/ready
```

Expected: 5 分钟内停止正数等级 402；旧总预算继续生效；Expand schema、`call_logs` 和 Redis v2 keys 不删除；不执行破坏性 DDL 回滚。

- [ ] **Step 9: 演练严重故障回滚**

只有 `$0` 误杀、权限泄露或静态配置损坏时，经双人确认关闭 `zero_block_enabled`；若快照整体损坏才关闭 `master_enabled`，并明确这会停止 tier 检查和 tier counter 累计但继续写调用日志等级快照。记录操作者、原因、request/change ID、开始/结束时间和受影响范围；恢复前重新执行 `$0` Redis 故障 E2E。

- [ ] **Step 10: 演练恢复与旧 key 停写**

修复后从完整影子态重新开始；正数稳定一个完整周期且 `v2_total_enforcement_enabled=true` 验证通过后，才能把 `legacy_total_write_enabled=false`。关闭前导出旧/v2/日志三方对账；旧 key 仅依 TTL 自然过期，不批量删除。回滚窗口内若重新开启旧 key 写入，必须先确认旧 key 尚未过期或从日志重建。

- [ ] **Step 11: Commit**

```bash
git add deploy/blue-green/deploy-blue-green.sh deploy/blue-green/docker-compose.blue.yml deploy/blue-green/docker-compose.green.yml docs/runbooks/model-tier-budget-rollback.md
git commit -m "ops(budget-tier): gate rollout and rollback drills"
```

## 手工 E2E 验收清单

- [ ] 使用管理员 API 创建等级、关联模型组、设置月正数额度，确认审计含原因和前后值。
- [ ] 使用普通用户 Key 调用六类协议，确认授权、总预算、等级预算顺序一致。
- [ ] 未分级组只增加总预算 key；已分级组同时增加总预算和等级 key。
- [ ] `$0` 返回 402 且不调用上游；Redis 故障时仍为 402。
- [ ] 总预算耗尽返回 `402 budget_exceeded`；等级 `$0`/正数额度耗尽返回 `402 tier_budget_exceeded`；Redis 故障时正数额度放行、记录 fail-open、等级 key 不累计。
- [ ] 流式请求开始后降额不截断当前流，结束后记账，下一请求被阻断。
- [ ] 跨预算时区日边界、周边界、月边界后，新 period key 从 0 开始，旧 key 保留至 TTL。
- [ ] 模型组换等级和等级重命名后，历史 `call_logs` 快照不变。
- [ ] verify-key 只下发已配置 `tier_limits`；current-period temporary override 同时展示基础值、有效值与到期时间，过期后恢复届时基础值。
- [ ] 总预算与等级预算 402 均写入五个 `budget_block_*` 首要阻断字段，`request_blocked` 风险事件可由调用日志稳定重建。
- [ ] 普通用户“我的预算”不泄露上级精确金额；管理员按管辖范围可见。
- [ ] `RebuildCurrentPeriods` 临时 key 原子替换、失败不覆盖、配置同步失败恢复和蓝绿回滚均按 runbook 成功演练。

## 最终验证

- [ ] Run: `go test ./tests/budgetperiod/... ./tests/configsync/... ./tests/gateway/... ./tests/controlplane/service/... ./tests/controlplane/handler/... ./tests/contract/... -count=1 -race`

  Expected: 单元、组件和契约测试全绿；无 ignored 或新 skip。

- [ ] Run: `go test ./tests/... -tags=integration -count=1 -v -timeout=30m`

  Expected: 集成、故障注入、重建和 E2E 全绿；测试数据完整清理。

- [ ] Run: `go build ./...`

  Expected: 所有 Go 二进制编译成功。

- [ ] Run: `atlas migrate validate --env local && atlas migrate status --env local`

  Expected: migration directory valid；临时验证库无 pending migration。

- [ ] Run: `cd web && npm ci && npx vue-tsc --noEmit && npx vite build`

  Expected: TypeScript 与 Vite 构建成功；前端交互只以构建和 Markdown 手工 E2E 验收。

- [ ] Run: `git diff --check`

  Expected: 无空白错误。

- [ ] Run:

```bash
rg -n 'T[B]D|T[O]DO|implement[[:space:]]+later|fill[[:space:]]+in[[:space:]]+details|similar[[:space:]]+to[[:space:]]+Task' \
  docs/superpowers/plans/2026-07-11-model-tier-budget-p3-test-rollout.md
```

  Expected: 无输出。

- [ ] 对照业务规格 44 条验收标准和技术设计 TD-01 至 TD-16，确认本计划至少覆盖其中与测试、观测、迁移、灰度、回滚相关的全部条目；特别确认六类协议、并发软超额、`internal/budgetperiod` 的上海时区/ISO Monday、Atlas、CI test stage、四类观测、性能、全量 Gateway、Redis v2 双写、七个 canonical 开关、完整周期影子对账、先 `$0` 后正数和回滚演练均有可执行任务与量化预期。

- [ ] 确认恰好 16 个 Task；P0 是唯一 `BudgetLifecycleExecutor/BudgetLifecycleObserver` 所有者，生命周期测试显式覆盖 `OnScheduleFailed(ctx, item FailedBudgetSchedule)` 及 `FailureReason/FailedAt`，P2 只接通知；snapshot 事件以 `infra.ChangeEvent` JSON 验证；disabled tier 保留 ID/name 且 eligible=false。

- [ ] 确认 `/my/budget/tiers` 每个测试完整断言 `source_summary.mode/primary_scope_type/has_shared_pool/has_temporary_override`，source summary 不含 label，`effective_total` 是 `EffectiveTotalPeriod` 数组，并完整断言 `budget_chain/tier_limits/data_updated_at/stale`；call log 阻断契约恰好五个 nullable string `budget_block_*`，集成测试用 `sql.NullString` 读取 NULL 与非 NULL。

- [ ] 确认所有 period 示例只调用 `budgetperiod.Resolve(now, periodType, config) (Period, error)`，所有重建示例只调用三参数 `NewBudgetCounterRebuildService(db, rdb, periodConfig)`，tier limit handler 示例只使用 P0 `TierLimitView` 和 `data.tier_limits` envelope；自动前端门禁只有 `vue-tsc` 与 Vite build，交互验收只使用 Markdown 手工 E2E。

- [ ] 确认实现提交不包含 `.env`、真实 API Key、数据库连接串、Redis 密码、编译产物或测试生成数据；测试/生产密钥只通过 GitLab CI/CD 变量和受控运行环境注入。
