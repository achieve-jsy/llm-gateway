# 模型等级预算 P2：扩容审批、风险事件与完整 Vue3 前端交互 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 P0 数据模型、Control Plane 基础 API 与快照发布器以及 P1 Gateway 消费执行链之上，交付事务幂等的扩容审批、实时风险事件 API，以及覆盖模型等级、预算同页下钻、我的预算和移动端审批的完整 Vue3 交互。

**Architecture:** Control Plane 以数据库事务和 `FOR UPDATE` 作为审批唯一写入边界，预检版本固定审批人看到的实时预算状态，提交后再发布 Redis 配置并记录同步结果；风险事件是预算配置、Redis v2 用量、告警记录和 P1 `call_logs.budget_block_*` 的近期 402 实时投影，不新增人工关闭状态。Web 以服务端返回的 `capabilities` 与 `visible_scopes` 做四角色对象裁剪，URL query 保存预算下钻位置；旧扩容金额 number 契约保持兼容，新增 snapshot/target 金额使用 6 位 Decimal 字符串。

**Tech Stack:** Go、Gin、GORM/PostgreSQL、Redis、Vue 3 Composition API、TypeScript、Pinia、Vue Router、Element Plus、`vue-tsc`、Vite、Go contract tests、Markdown 手工 E2E。

---

## 前置条件与边界

- 本计划从 `docs/superpowers/specs/2026-07-10-model-tier-budget-design.md` 与 `docs/superpowers/specs/2026-07-11-model-tier-budget-technical-design.md` 的 P2 范围开始。
- P0 已提供 `model_tiers`、`budget_tier_limits`、`budget_temporary_overrides`、`budget_limit_schedules`、扩展后的 `expansion_requests` GORM model 与 Atlas 迁移，以及 `BudgetTierSnapshotPublisher`。P1 只消费 `budget:v2:tier:snapshot` 与 verify-key 的 `tier_limits`，负责 Gateway holder/loader、Redis v2 用量和 reconcile；P2 只在审批事务提交后调用 P0 publisher，不创建第二套快照发布逻辑。
- P0 已提供且 P2 Web 直接消费下方“P0 依赖 API 清单”中的基础接口。P2 不为这些接口新建 handler/service，只补审批、风险和既有直接调额契约所需的确认字段。
- Gateway 对外 402 `error.code` 只能是小写 `budget_exceeded` 或 `tier_budget_exceeded`；大写错误码仅用于 Control Plane 管理 API。
- 保留侧边栏一级菜单名“扩容申请”；`预算管理` 仅有“预算总览”和“风险事件”两个页签；平台区新增“模型等级”。
- `BudgetManagePage.vue` 不再继续堆积业务逻辑：页面只编排页签和导航，具体功能拆进 `web/src/components/budget/`。
- 普通用户永远不能提交无法解除上级瓶颈的个人申请；`notify-owner` 是明确的替代动作。
- 兼容字段 `ExpansionRequest.current_amount`、`ExpansionRequest.requested_amount` 的 TypeScript/JSON 类型继续是 `number`，不得静默改成字符串；新增的 `*_snapshot`、`target_amount_usd`、额度、用量和剩余金额才使用 6 位 Decimal 字符串，例如 `"120.000000"`。前端只把旧 number 用于兼容展示，所有新增预检与额度合法性判断使用 Decimal 字符串。
- 数据库/领域枚举固定为：`expansion_requests.status=pending|precheck_required|approved|rejected|cancelled|expired`，`budget_temporary_overrides.status=active|expired|cancelled|failed`，`budget_limit_schedules.status=pending|applied|replaced|cancelled|failed`，`expansion_requests.sync_status=pending|synced|failed`。

### P0 依赖 API 清单

```text
GET    /api/v1/model-tiers
POST   /api/v1/model-tiers
GET    /api/v1/model-tiers/:id
PUT    /api/v1/model-tiers/:id
PATCH  /api/v1/model-tiers/:id/status
DELETE /api/v1/model-tiers/:id
PUT    /api/v1/model-tiers/reorder
PUT    /api/v1/groups/:id/budget-tier
PUT    /api/v1/groups/budget-tiers
GET    /api/v1/budgets/:allocation_id/tier-limits
PUT    /api/v1/budgets/:allocation_id/tier-limits
POST   /api/v1/budgets/tier-limits/batch/preview
POST   /api/v1/budgets/tier-limits/batch/apply
GET    /api/v1/budgets/tier-usage
GET    /api/v1/my/budget/tiers
```

## 文件结构

**Control Plane 修改**

- `internal/controlplane/model/expansion_request.go`：审批状态、动作和申请字段的领域常量。
- `internal/controlplane/repository/expansion_repo.go`：行锁、事务内状态/流水/生效标记和动态待审查询。
- `internal/controlplane/repository/budget_risk_repo.go`：从 P1 `call_logs.budget_block_*` 聚合真实 402。
- `internal/controlplane/service/alert_expansion_service.go`：预检、重新预检、动态审批池、事务批准、临时/长期生效和通知。
- `internal/controlplane/service/budget_lifecycle_notification_observer.go`：实现 P0 三方法 observer，发送 override 恢复、schedule 生效和失败通知。
- `internal/controlplane/service/budget_owner_resolver.go`：按 allocation ID 读取真实 scope，再解析生命周期通知负责人。
- `internal/controlplane/service/budget_risk_service.go`：风险实时投影与摘要。
- `internal/controlplane/handler/expansion_handler.go`：扩容 precheck/detail/update/reprecheck/decision/notify-owner HTTP 契约。
- `internal/controlplane/handler/budget_risk_handler.go`：风险列表与摘要 HTTP 契约。
- `internal/controlplane/handler/budget_handler.go`：在既有总预算写接口补充原因、生效时机、高风险确认和影响信息。
- `internal/controlplane/router/router.go`：依赖注入和路由注册。
- `internal/controlplane/audit/types.go`：审批、调额、等级和风险导出的审计动作。
- `internal/controlplane/service/notifier.go`：审批通知载荷的站内信/邮件复用。

**Control Plane 新增测试**

- `tests/controlplane/service/expansion_service_test.go`
- `tests/controlplane/service/budget_risk_service_test.go`
- `tests/controlplane/repository/expansion_repo_test.go`
- `tests/controlplane/repository/budget_risk_repo_test.go`
- `tests/controlplane/handler/expansion_handler_test.go`
- `tests/controlplane/handler/budget_risk_handler_test.go`
- `tests/controlplane/handler/budget_handler_test.go`
- `tests/controlplane/service/budget_lifecycle_notification_observer_test.go`

**Web 新增**

- `web/src/types/model-tier.ts`
- `web/src/types/budget-risk.ts`
- `web/src/stores/model-tier.ts`
- `web/src/stores/budget-nav.ts`
- `web/src/views/admin/ModelTierListPage.vue`
- `web/src/components/budget/BudgetOverviewSection.vue`
- `web/src/components/budget/BudgetRiskEventsSection.vue`
- `web/src/components/budget/BudgetTierLimitsPanel.vue`
- `web/src/components/budget/BudgetAdjustDrawer.vue`
- `web/src/components/budget/BudgetBatchAdjustWizard.vue`
- `web/src/components/expansion/ExpansionCreateForm.vue`
- `web/src/components/expansion/ExpansionApprovalDetail.vue`
- `web/src/components/expansion/ExpansionPrecheckPanel.vue`
- `web/src/components/expansion/ExpansionMobileListItem.vue`
- `web/src/components/common/DataFreshnessNotice.vue`
- `specs/model-tier-budget/test/01-model-tier-and-budget-management.md`
- `specs/model-tier-budget/test/02-expansion-approval.md`
- `specs/model-tier-budget/test/03-risk-and-my-budget.md`
- `specs/model-tier-budget/test/04-mobile-and-stale-states.md`

**Web 修改**

- `web/src/types/expansion.ts`
- `web/src/types/budget.ts`
- `web/src/services/control-plane.ts`
- `web/src/router/index.ts`
- `web/src/App.vue`
- `web/src/views/admin/ExpansionRequestPage.vue`
- `web/src/views/admin/BudgetManagePage.vue`
- `web/src/views/MyBudgetPage.vue`

**契约修改**

- `tests/contract/contracts.json`

---

## 任务依赖

```text
P0 基础表/API/BudgetTierSnapshotPublisher/BudgetLifecycleExecutor/BudgetLifecycleObserver
  ├─ P1 Gateway consumer + Redis v2 + call_logs budget_block_*
  ├─ Task 5 lifecycle 三方法通知 observer
  └─ Task 8 调额确认扩展
P0 + P1 → Task 1 → Task 2 → Task 3 → Task 4
Task 4 + Task 5 → Task 6
P1 call_logs budget_block_* → Task 7
Task 1、6、7、8 → Task 9
Task 9 → Task 10、11、12、13、14、15
Task 10–15 → Task 16 → Task 17
```

Task 4 的事务后同步依赖 P0 publisher；Task 5 只实现并注册 P0 `BudgetLifecycleObserver`；Task 7 的真实阻断来源依赖 P1 写入 `call_logs.budget_block_*`；Task 8 只扩展 P0 写 API 的确认字段。所有 total/tier `next_period` 都由 P0 唯一 `BudgetLifecycleExecutor` 执行，P2 不创建第二个 executor。计划共 **17 个任务**。

### Task 1: 固化扩容状态机与 API 领域类型

**Files:**
- Modify: `internal/controlplane/model/expansion_request.go`
- Modify: `web/src/types/expansion.ts`
- Test: `tests/controlplane/service/expansion_service_test.go`

- [ ] **Step 1: 写失败的 Go 状态转换测试**

```go
func TestExpansionTransitionRequiresFreshPrecheck(t *testing.T) {
	tests := []struct {
		from, action, want string
		ok                 bool
	}{
		{model.ExpansionStatusPending, model.ExpansionActionApprove, model.ExpansionStatusApproved, true},
		{model.ExpansionStatusPending, model.ExpansionActionReprecheckRequired, model.ExpansionStatusPrecheckRequired, true},
		{model.ExpansionStatusPrecheckRequired, model.ExpansionActionConfirmPrecheck, model.ExpansionStatusPending, true},
		{model.ExpansionStatusApproved, model.ExpansionActionCancel, "", false},
		{model.ExpansionStatusExpired, model.ExpansionActionApprove, "", false},
	}
	for _, tt := range tests {
		got, err := model.NextExpansionStatus(tt.from, tt.action)
		if tt.ok {
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		} else {
			require.Error(t, err)
		}
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/controlplane/service -run TestExpansionTransitionRequiresFreshPrecheck -count=1`

Expected: FAIL，提示 `undefined: model.NextExpansionStatus`。

- [ ] **Step 3: 实现完整状态、维度、持续时间和转换函数**

```go
const (
	ExpansionStatusPending          = "pending"
	ExpansionStatusPrecheckRequired = "precheck_required"
	ExpansionStatusApproved         = "approved"
	ExpansionStatusRejected         = "rejected"
	ExpansionStatusCancelled        = "cancelled"
	ExpansionStatusExpired          = "expired"

	ExpansionDimensionTotal = "total"
	ExpansionDimensionTier  = "tier"

	ExpansionDurationPermanent   = "permanent"
	ExpansionDurationCurrentTerm = "current_period"

	ExpansionPriorityNormal = "normal"
	ExpansionPriorityUrgent = "urgent"

	ExpansionActionApprove            = "approve"
	ExpansionActionReject             = "reject"
	ExpansionActionEscalate           = "escalate"
	ExpansionActionCancel             = "cancel"
	ExpansionActionReprecheckRequired = "require_reprecheck"
	ExpansionActionConfirmPrecheck    = "confirm_precheck"
)

func NextExpansionStatus(current, action string) (string, error) {
	transitions := map[string]map[string]string{
		ExpansionStatusPending: {
			ExpansionActionApprove:            ExpansionStatusApproved,
			ExpansionActionReject:             ExpansionStatusRejected,
			ExpansionActionCancel:             ExpansionStatusCancelled,
			ExpansionActionEscalate:           ExpansionStatusPending,
			ExpansionActionReprecheckRequired: ExpansionStatusPrecheckRequired,
		},
		ExpansionStatusPrecheckRequired: {
			ExpansionActionConfirmPrecheck: ExpansionStatusPending,
			ExpansionActionCancel:          ExpansionStatusCancelled,
		},
	}
	next, ok := transitions[current][action]
	if !ok {
		return "", fmt.Errorf("invalid expansion transition: %s -> %s", current, action)
	}
	return next, nil
}
```

同步 TypeScript 联合类型，防止页面遗漏状态：

```ts
export type MoneyUSD = string
export type ExpansionStatus =
  | 'pending' | 'precheck_required' | 'approved'
  | 'rejected' | 'cancelled' | 'expired'
export type ExpansionDimension = 'total' | 'tier'
export type ExpansionDuration = 'permanent' | 'current_period'
export type ExpansionPriority = 'normal' | 'urgent'
export type ExpansionAction = 'approve' | 'reject' | 'escalate' | 'cancel'

export interface ExpansionRequest {
  id: string
  scope_type: BudgetScopeType
  scope_id: string
  scope_name: string
  requester_id: string
  requester_name: string
  period_type: ExpansionPeriod
  dimension_type: ExpansionDimension
  model_tier_id: string | null
  model_tier_name: string | null
  duration_type: ExpansionDuration
  priority: ExpansionPriority
  current_amount: number
  requested_amount: number
  current_amount_snapshot: MoneyUSD
  used_amount_snapshot: MoneyUSD
  target_amount_usd: MoneyUSD
  reason: string
  urgent_reason: string
  status: ExpansionStatus
  current_level: ExpansionLevel
  precheck_version: string
  prechecked_at: string
  expires_at: string
  applied_at: string | null
  sync_status: 'pending' | 'synced' | 'failed' | null
  created_at: string
  updated_at: string
}

export type ExpansionTargetInput =
  | { requested_amount: number; target_amount_usd?: never }
  | { requested_amount?: number; target_amount_usd: MoneyUSD }
```

Handler 同时接受旧 `requested_amount` number 和新 `target_amount_usd` Decimal 字符串；两者同时出现时必须表示同一 6 位金额，否则返回 `INVALID_EXPANSION_INPUT`。响应继续原样返回旧 number 字段，并额外返回 `target_amount_usd` 供新页面比较，不在 P2 删除或改型旧字段。

- [ ] **Step 4: 运行状态测试和类型检查**

Run: `go test ./tests/controlplane/service -run TestExpansionTransitionRequiresFreshPrecheck -count=1 && cd web && npx vue-tsc --noEmit`

Expected: PASS，TypeScript 无错误。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/model/expansion_request.go tests/controlplane/service/expansion_service_test.go web/src/types/expansion.ts
git commit -m "feat(budget): define expansion approval state machine"
```

### Task 2: 实现 precheck/reprecheck 与四角色对象裁剪

**Files:**
- Modify: `internal/controlplane/service/alert_expansion_service.go`
- Modify: `internal/controlplane/handler/expansion_handler.go`
- Test: `tests/controlplane/service/expansion_service_test.go`
- Test: `tests/controlplane/handler/expansion_handler_test.go`

- [ ] **Step 1: 写失败的角色裁剪与瓶颈测试**

```go
func TestPrecheckRejectsMemberExpansionWhenParentIsBlocking(t *testing.T) {
	svc := newExpansionServiceFixture(t)
	svc.usage.Set("member", "u-1", "monthly", "40.000000")
	svc.limits.Set("member", "u-1", "monthly", "50.000000")
	svc.limits.Set("dept_daily", "d-1", "monthly", "100.000000")
	svc.usage.Set("dept_daily", "d-1", "monthly", "100.000000")

	got, err := svc.Precheck(context.Background(), ActorScope{
		UserID: "u-1", Role: "user", DeptIDs: []string{"d-1"},
	}, PrecheckInput{
		ScopeType: "member", ScopeID: "u-1", PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent",
		TargetAmountUSD: "80.000000",
	})
	require.NoError(t, err)
	require.False(t, got.CanSubmit)
	require.True(t, got.CanNotifyOwner)
	require.Equal(t, "PARENT_LIMIT_BLOCKING", got.BlockingReason.Code)
	require.Equal(t, "dept", got.BlockingReason.SourceLevel)
	require.Empty(t, got.BlockingReason.LimitUSD)
}

func TestVisibleScopesAreTrimmedForAllRoles(t *testing.T) {
	fixture := newExpansionServiceFixture(t)
	cases := []struct {
		role string
		want []string
	}{
		{"admin", []string{"company", "studio", "dept_daily", "dept_service", "member"}},
		{"studio_admin", []string{"studio", "dept_daily", "dept_service", "member"}},
		{"dept_admin", []string{"dept_daily", "dept_service", "member"}},
		{"user", []string{"member"}},
	}
	for _, tc := range cases {
		got := fixture.VisibleScopeTypes(tc.role)
		require.Equal(t, tc.want, got)
	}
}

func TestCreateExpansionPreservesLegacyNumberAndAddsDecimalTarget(t *testing.T) {
	r := newExpansionRouterFixture(t)
	res := performJSON(r, "POST", "/api/v1/expansion-requests", map[string]any{
		"scope_type": "member", "scope_id": "u-1", "period_type": "monthly",
		"dimension_type": "total", "duration_type": "permanent",
		"requested_amount": 120.5, "reason": "提高个人月额度",
	}, userToken)
	require.Equal(t, http.StatusCreated, res.Code)
	require.JSONEq(t, `{
	  "current_amount":80,
	  "requested_amount":120.5,
	  "current_amount_snapshot":"80.000000",
	  "used_amount_snapshot":"60.000000",
	  "target_amount_usd":"120.500000"
	}`, selectAmountFields(res.Body.Bytes()))
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/controlplane/service ./tests/controlplane/handler -run 'TestPrecheckRejects|TestVisibleScopes|TestCreateExpansionPreservesLegacyNumber' -count=1`

Expected: FAIL，提示 `Precheck`、`VisibleScopeTypes` 未定义。

- [ ] **Step 3: 实现预检结果、版本和权限裁剪**

```go
type PrecheckInput struct {
	ScopeType      string  `json:"scope_type"`
	ScopeID        string  `json:"scope_id"`
	PeriodType     string  `json:"period_type"`
	DimensionType  string  `json:"dimension_type"`
	ModelTierID    *string `json:"model_tier_id,omitempty"`
	DurationType   string  `json:"duration_type"`
	Priority       string  `json:"priority"`
	TargetAmountUSD string `json:"target_amount_usd"`
}

type PrecheckResult struct {
	CanSubmit          bool              `json:"can_submit"`
	CanNotifyOwner     bool              `json:"can_notify_owner"`
	CurrentLimitUSD    string            `json:"current_limit_usd"`
	CurrentUsedUSD     string            `json:"current_used_usd"`
	TargetAmountUSD    string            `json:"target_amount_usd"`
	IncreaseUSD        string            `json:"increase_usd"`
	EffectiveAfterUSD  string            `json:"effective_after_usd"`
	PrecheckVersion    string            `json:"precheck_version"`
	DataAsOf           time.Time         `json:"data_as_of"`
	Stale              bool              `json:"stale"`
	BlockingReason     *BlockingReason   `json:"blocking_reason,omitempty"`
	ParentConstraints  []ConstraintView  `json:"parent_constraints"`
	AffectedGroups     []AffectedGroup   `json:"affected_groups"`
	VisibleScopes      []ScopeOption     `json:"visible_scopes"`
}

func makePrecheckVersion(input PrecheckInput, current LimitSnapshot) string {
	raw := strings.Join([]string{
		input.ScopeType, input.ScopeID, input.PeriodType, input.DimensionType,
		nullString(input.ModelTierID), input.DurationType,
		current.BaseLimitUSD, current.EffectiveLimitUSD, current.UsedUSD,
		current.DataAsOf.UTC().Format(time.RFC3339Nano),
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
```

服务必须依次执行：Actor 管辖校验 → 目标等级/周期校验 → 当前额度和用量读取 → 上级约束分析 → 最大目标/频率/重复待审校验 → 返回版本。`stale=true` 或 `target_amount_usd <= current_limit_usd` 时 `can_submit=false`。普通用户的父级约束只返回 `source_level`、`status`、`reset_at`，`limit_usd` 和主体 ID 为空。

- [ ] **Step 4: 增加 precheck HTTP 契约**

```go
func (h *ExpansionHandler) Precheck(c *gin.Context) {
	actor := h.actor(c)
	var req service.PrecheckInput
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, "INVALID_EXPANSION_INPUT", err.Error())
		return
	}
	result, err := h.svc.Precheck(c.Request.Context(), actor, req)
	if err != nil {
		writeExpansionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
```

创建/编辑 DTO 保留旧 number 并允许新 Decimal target：

```go
type ExpansionAmountInput struct {
	RequestedAmount *float64 `json:"requested_amount,omitempty"`
	TargetAmountUSD *string  `json:"target_amount_usd,omitempty"`
}

func resolveExpansionTarget(in ExpansionAmountInput) (budgetdomain.Amount, error) {
	var legacy *budgetdomain.Amount
	if in.RequestedAmount != nil {
		v, err := budgetdomain.ParseAmount(strconv.FormatFloat(*in.RequestedAmount, 'f', 6, 64))
		if err != nil {
			return budgetdomain.Amount{}, err
		}
		legacy = &v
	}
	var target *budgetdomain.Amount
	if in.TargetAmountUSD != nil {
		v, err := budgetdomain.ParseAmount(*in.TargetAmountUSD)
		if err != nil {
			return budgetdomain.Amount{}, err
		}
		target = &v
	}
	if legacy == nil && target == nil {
		return budgetdomain.Amount{}, ErrExpansionTargetRequired
	}
	if legacy != nil && target != nil && legacy.String() != target.String() {
		return budgetdomain.Amount{}, ErrExpansionTargetMismatch
	}
	if target != nil {
		return *target, nil
	}
	return *legacy, nil
}
```

写库时继续填充旧 `RequestedAmount`，新响应 DTO 另填 `target_amount_usd=target.String()`；`CurrentAmount` 与 `RequestedAmount` 的旧 JSON number tag 和 TypeScript number 不变。

请求示例固定为：

```json
{
  "scope_type": "member",
  "scope_id": "d99a15fb-4aae-45a7-a1ef-a3491b4df6e6",
  "period_type": "monthly",
  "dimension_type": "tier",
  "model_tier_id": "d753eb15-37e5-4180-9450-3eab10486ce7",
  "duration_type": "current_period",
  "priority": "urgent",
  "target_amount_usd": "120.000000"
}
```

- [ ] **Step 5: 运行服务和 handler 测试**

Run: `go test ./tests/controlplane/service ./tests/controlplane/handler -run 'Precheck|VisibleScopes|LegacyNumber' -count=1`

Expected: PASS；普通用户响应不含上级金额。

- [ ] **Step 6: 提交**

```bash
git add internal/controlplane/service/alert_expansion_service.go internal/controlplane/handler/expansion_handler.go tests/controlplane/service/expansion_service_test.go tests/controlplane/handler/expansion_handler_test.go
git commit -m "feat(budget): add scoped expansion precheck"
```

### Task 3: 实现动态审批池与申请编辑规则

**Files:**
- Modify: `internal/controlplane/repository/expansion_repo.go`
- Modify: `internal/controlplane/service/alert_expansion_service.go`
- Test: `tests/controlplane/repository/expansion_repo_test.go`
- Test: `tests/controlplane/service/expansion_service_test.go`

- [ ] **Step 1: 写失败的动态审批池、重复申请和编辑回退测试**

```go
func TestApproverLosingRoleCannotDecide(t *testing.T) {
	svc := newExpansionServiceFixture(t)
	req := svc.seedPending("member", "u-1", "dept", "100.000000")
	svc.roles.SetDeptApprovers("d-1", []string{"approver-a"})
	require.NoError(t, svc.AssertCanDecide(context.Background(), req.ID.String(), "approver-a"))

	svc.roles.SetDeptApprovers("d-1", nil)
	err := svc.AssertCanDecide(context.Background(), req.ID.String(), "approver-a")
	require.ErrorIs(t, err, ErrNotInApprovalPool)
	require.Equal(t, 1, svc.notifier.Count("approval_pool_empty"))
}

func TestIncreasingTargetRestartsApprovalLevel(t *testing.T) {
	svc := newExpansionServiceFixture(t)
	req := svc.seedEscalated("studio", "s-1", "platform", "1000.000000")
	got, err := svc.UpdateRequest(context.Background(), req.ID.String(), "requester-1", UpdateExpansionInput{
		TargetAmountUSD: "1200.000000",
		Reason: "新增两个生产项目",
	})
	require.NoError(t, err)
	require.Equal(t, "studio", got.CurrentLevel)
	require.Equal(t, "precheck_required", got.Status)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/controlplane/service ./tests/controlplane/repository -run 'ApproverLosingRole|IncreasingTarget|DuplicatePending' -count=1`

Expected: FAIL，动态角色解析和唯一维度查询尚未实现。

- [ ] **Step 3: 实现审批池接口与实时解析**

```go
type ApprovalPoolResolver interface {
	Resolve(ctx context.Context, req *model.ExpansionRequest) ([]uuid.UUID, error)
	ResolveScopeOwnerRecipients(ctx context.Context, scopeType, scopeID string) ([]uuid.UUID, error)
	ResolveParentAdmins(ctx context.Context, req *model.ExpansionRequest) ([]uuid.UUID, error)
}

func (s *ExpansionService) AssertCanDecide(ctx context.Context, requestID, actorID string) error {
	req, err := s.repo.FindByID(requestID)
	if err != nil {
		return err
	}
	pool, err := s.approvalPool.Resolve(ctx, req)
	if err != nil {
		return err
	}
	if len(pool) == 0 {
		s.notifyEmptyPool(ctx, req)
		return ErrApprovalPoolEmpty
	}
	actorUUID, err := uuid.Parse(actorID)
	if err != nil || !slices.Contains(pool, actorUUID) {
		return ErrNotInApprovalPool
	}
	return nil
}
```

动态池规则固定为：成员额度→成员当前部门所有有效 `dept_admin`；部门日常/服务池→部门当前工作室所有有效 `studio_admin`；工作室→所有有效平台 `admin`。不在申请行存审批人列表；SLA job 只通知当前池。

- [ ] **Step 4: 实现重复待审查询和编辑语义**

```go
func (r *ExpansionRepo) FindActiveDuplicate(
	requesterID uuid.UUID,
	scopeType, scopeID, periodType, dimensionType string,
	tierID *uuid.UUID,
	durationType string,
) (*model.ExpansionRequest, error) {
	var item model.ExpansionRequest
	q := r.db.Where(
		`requester_id = ? AND scope_type = ? AND scope_id = ? AND period_type = ?
		 AND dimension_type = ? AND duration_type = ?
		 AND status IN ?`,
		requesterID, scopeType, scopeID, periodType, dimensionType, durationType,
		[]string{model.ExpansionStatusPending, model.ExpansionStatusPrecheckRequired},
	)
	if tierID == nil {
		q = q.Where("model_tier_id IS NULL")
	} else {
		q = q.Where("model_tier_id = ?", *tierID)
	}
	err := q.First(&item).Error
	return &item, err
}
```

提高目标：状态变 `precheck_required`，审批层级回到初始层；降低目标或只改原因：重新预检但保留已完成流水；紧急申请必须有 `urgent_reason`；永久申请默认 30 天过期，当前周期申请的 `expires_at` 是本周期重置边界。

- [ ] **Step 5: 运行审批池和 repository 测试**

Run: `go test ./tests/controlplane/service ./tests/controlplane/repository -run 'Approval|Duplicate|UpdateRequest|Target' -count=1`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/controlplane/repository/expansion_repo.go internal/controlplane/service/alert_expansion_service.go tests/controlplane/service/expansion_service_test.go tests/controlplane/repository/expansion_repo_test.go
git commit -m "feat(budget): resolve dynamic expansion approval pools"
```

### Task 4: 将批准改为事务幂等并支持临时/长期生效

**Files:**
- Modify: `internal/controlplane/repository/expansion_repo.go`
- Modify: `internal/controlplane/service/alert_expansion_service.go`
- Test: `tests/controlplane/service/expansion_service_test.go`
- Test: `tests/controlplane/repository/expansion_repo_test.go`

- [ ] **Step 1: 写失败的并发批准、陈旧预检和临时覆盖测试**

```go
func TestApproveIsIdempotentAcrossConcurrentCalls(t *testing.T) {
	svc := newPostgresExpansionFixture(t)
	req := svc.seedFreshPending("tier", "current_period", "120.000000")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Decide(context.Background(), req.ID.String(), "approver-a", DecisionInput{
				Action: "approve", PrecheckVersion: req.PrecheckVersion,
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), svc.countTemporaryOverrides(req.ID))
	require.Equal(t, int64(1), svc.countSteps(req.ID, "approve"))
}

func TestApproveMovesChangedBudgetToPrecheckRequired(t *testing.T) {
	svc := newPostgresExpansionFixture(t)
	req := svc.seedFreshPending("total", "permanent", "100.000000")
	svc.usage.Set(req.ScopeType, req.ScopeID, req.PeriodType, "110.000000")
	_, err := svc.Decide(context.Background(), req.ID.String(), "approver-a", DecisionInput{
		Action: "approve", PrecheckVersion: req.PrecheckVersion,
	})
	require.ErrorIs(t, err, ErrPrecheckStale)
	require.Equal(t, "precheck_required", svc.reload(req.ID).Status)
	require.Zero(t, svc.countBaseLimitUpdates(req.ID))
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/controlplane/service ./tests/controlplane/repository -run 'ApproveIsIdempotent|ApproveMovesChanged' -count=1`

Expected: FAIL；现有实现会先批准状态，再非事务调用 `SetBudget`。

- [ ] **Step 3: 增加事务 repository 边界**

```go
func (r *ExpansionRepo) WithLockedRequest(
	ctx context.Context,
	id uuid.UUID,
	fn func(tx *gorm.DB, req *model.ExpansionRequest) error,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var req model.ExpansionRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&req, "id = ?", id).Error; err != nil {
			return err
		}
		return fn(tx, &req)
	})
}
```

- [ ] **Step 4: 实现批准事务和事务后同步**

```go
func (s *ExpansionService) approve(
	ctx context.Context,
	requestID uuid.UUID,
	approverID uuid.UUID,
	in DecisionInput,
) (*ApplyResult, error) {
	var result ApplyResult
	err := s.repo.WithLockedRequest(ctx, requestID, func(tx *gorm.DB, req *model.ExpansionRequest) error {
		if req.Status == model.ExpansionStatusApproved && req.AppliedAt != nil {
			result = ApplyResultFromRequest(req)
			return nil
		}
		if req.Status != model.ExpansionStatusPending || time.Now().After(req.ExpiresAt) {
			return ErrRequestExpired
		}
		latest, err := s.precheckInTx(ctx, tx, req)
		if err != nil {
			return err
		}
		if latest.PrecheckVersion != in.PrecheckVersion || decimalCmp(latest.CurrentUsedUSD, latest.TargetAmountUSD) > 0 {
			return s.repo.MarkPrecheckRequired(tx, req.ID, latest)
		}
		if req.DurationType == model.ExpansionDurationCurrentTerm {
			result, err = s.applyTemporaryOverride(tx, req, latest)
		} else {
			result, err = s.applyPermanentLimit(tx, req, latest)
		}
		if err != nil {
			return err
		}
		return s.repo.MarkAppliedAndAppendStep(tx, req, approverID, in.Comment, result)
	})
	if errors.Is(err, errMarkedPrecheckRequired) {
		return nil, ErrPrecheckStale
	}
	if err != nil {
		return nil, err
	}
	if result.AlreadyApplied {
		return &result, nil
	}
	if err := s.snapshotPublisher.Publish(ctx); err != nil {
		_ = s.repo.UpdateSyncStatus(requestID, "failed")
		return &result, ErrConfigSyncPending
	}
	_ = s.repo.UpdateSyncStatus(requestID, "synced")
	s.notifyApplied(ctx, result)
	return &result, nil
}
```

`snapshotPublisher` 必须注入 P0 已交付的 `BudgetTierSnapshotPublisher`；P2 不定义 publisher、Redis key、revision 分配或 `config:change` 发布实现。永久等级申请 upsert `budget_tier_limits` 并保留 `effective_at`；永久总预算更新对应周期基础字段；当前周期申请写 `budget_temporary_overrides`，有效值是 `max(base, target)`，到期后恢复到期时的基础值。数据库成功而 P0 publisher 发布失败返回 `BUDGET_CONFIG_SYNC_PENDING`，不回滚数据库，由 P0 reconcile 恢复；P1 仅消费新 revision。

- [ ] **Step 5: 运行事务测试与 race detector**

Run: `go test -race ./tests/controlplane/service ./tests/controlplane/repository -run 'Approve|Temporary|Permanent|Sync' -count=1`

Expected: PASS；同一申请只有一条生效记录和一条批准流水。

- [ ] **Step 6: 提交**

```bash
git add internal/controlplane/repository/expansion_repo.go internal/controlplane/service/alert_expansion_service.go tests/controlplane/service/expansion_service_test.go tests/controlplane/repository/expansion_repo_test.go
git commit -m "fix(budget): make expansion approval transactional"
```

### Task 5: 实现 P0 生命周期通知 observer

**Files:**
- Create: `internal/controlplane/service/budget_lifecycle_notification_observer.go`
- Create: `internal/controlplane/service/budget_owner_resolver.go`
- Modify: `internal/controlplane/service/notifier.go`
- Modify: `internal/controlplane/router/router.go`
- Create: `tests/controlplane/service/budget_lifecycle_notification_observer_test.go`

- [ ] **Step 1: 写失败的 override、schedule 生效/失败通知与幂等测试**

```go
func TestLifecycleObserverNotifiesRequesterAndCurrentOwnerOnce(t *testing.T) {
	f := newLifecycleObserverFixture(t)
	f.allocations.Seed(allocationID, "dept_daily", "d-1")
	f.scopeOwners.Set("dept_daily", "d-1", []uuid.UUID{ownerID})
	item := service.ExpiredBudgetOverride{
		ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ExpansionRequestID: ptr(requestID),
		BudgetAllocationID: allocationID,
		DimensionType: budgetdomain.DimensionTier,
		ModelTierID: ptr(tierHigh),
		PeriodType: budgetdomain.PeriodMonthly,
		ExpiredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}

	require.NoError(t, f.observer.OnOverrideExpired(context.Background(), item))
	require.NoError(t, f.observer.OnOverrideExpired(context.Background(), item))

	require.Equal(t, 1, f.notifier.Count(requesterID, "expansion_restored"))
	require.Equal(t, 1, f.notifier.Count(ownerID, "expansion_restored"))
	require.NotEmpty(t, f.scopeOwners.Calls())
	for _, call := range f.scopeOwners.Calls() {
		require.Equal(t, scopeKey{ScopeType: "dept_daily", ScopeID: "d-1"}, call)
	}
}

func TestLifecycleObserverWithoutExpansionOnlyNotifiesOwner(t *testing.T) {
	f := newLifecycleObserverFixture(t)
	item := directExpiredBudgetOverride()
	require.NoError(t, f.observer.OnOverrideExpired(context.Background(), item))
	require.Equal(t, 0, f.notifier.CountAllRequesters("expansion_restored"))
	require.Equal(t, 1, f.notifier.Count(ownerID, "expansion_restored"))
}

func TestLifecycleObserverNotifiesScheduleAppliedAndFailedOnce(t *testing.T) {
	f := newLifecycleObserverFixture(t)
	f.allocations.Seed(allocationID, "studio", "studio-7")
	f.scopeOwners.Set("studio", "studio-7", []uuid.UUID{ownerID})
	f.schedules.Seed(scheduleID, createdByID, allocationID)

	applied := service.AppliedBudgetSchedule{ID: scheduleID}
	require.NoError(t, f.observer.OnScheduleApplied(context.Background(), applied))
	require.NoError(t, f.observer.OnScheduleApplied(context.Background(), applied))
	require.Equal(t, 1, f.notifier.Count(createdByID, "budget_schedule_applied"))
	require.Equal(t, 1, f.notifier.Count(ownerID, "budget_schedule_applied"))

	failed := service.FailedBudgetSchedule{
		ID: scheduleID,
		FailureReason: "parent limit changed before effective_at",
		FailedAt: now,
	}
	require.NoError(t, f.observer.OnScheduleFailed(context.Background(), failed))
	require.NoError(t, f.observer.OnScheduleFailed(context.Background(), failed))
	require.Equal(t, 1, f.notifier.Count(createdByID, "budget_schedule_failed"))
	require.Equal(t, 1, f.notifier.Count(ownerID, "budget_schedule_failed"))
}

func TestLifecycleOwnerResolverUsesAllocationScopeInsteadOfDimension(t *testing.T) {
	f := newLifecycleObserverFixture(t)
	f.allocations.Seed(allocationID, "company", "GLOBAL")
	f.scopeOwners.Set("company", "GLOBAL", []uuid.UUID{ownerID})

	got, err := f.owners.ResolveOwnerRecipients(context.Background(), allocationID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{ownerID}, got)
	require.Equal(t, []scopeKey{{ScopeType: "company", ScopeID: "GLOBAL"}}, f.scopeOwners.Calls())
	require.NotContains(t, f.scopeOwners.RawArguments(), allocationID.String())
	require.NotContains(t, f.scopeOwners.RawArguments(), "tier")
}
```

- [ ] **Step 2: 运行并确认失败**

Run: `go test ./tests/controlplane/service -run 'LifecycleObserver' -count=1`

Expected: FAIL，P2 通知 observer 和按 allocation 查真实 scope 的 owner resolver 尚不存在。

- [ ] **Step 3: 实现 P0 `BudgetLifecycleObserver`，不实现 executor 方法**

```go
type LifecycleOwnerResolver interface {
	ResolveOwnerRecipients(ctx context.Context, budgetAllocationID uuid.UUID) ([]uuid.UUID, error)
}

type DatabaseBudgetOwnerResolver struct {
	allocations BudgetAllocationReader
	scopeOwners ScopeOwnerReader
}

func (r *DatabaseBudgetOwnerResolver) ResolveOwnerRecipients(
	ctx context.Context,
	budgetAllocationID uuid.UUID,
) ([]uuid.UUID, error) {
	allocation, err := r.allocations.FindByID(ctx, budgetAllocationID)
	if err != nil {
		return nil, err
	}
	return r.scopeOwners.ResolveByScope(ctx, allocation.ScopeType, allocation.ScopeID)
}

type BudgetLifecycleNotificationObserver struct {
	expansions   ExpansionRequestReader
	schedules    BudgetScheduleReader
	owners       LifecycleOwnerResolver
	notifier     IdempotentNotifier
}

var _ BudgetLifecycleObserver = (*BudgetLifecycleNotificationObserver)(nil)

func (o *BudgetLifecycleNotificationObserver) OnScheduleApplied(
	ctx context.Context,
	item AppliedBudgetSchedule,
) error {
	return o.notifySchedule(ctx, item.ID, "budget_schedule_applied", "", item.AppliedAt)
}

func (o *BudgetLifecycleNotificationObserver) OnScheduleFailed(
	ctx context.Context,
	item FailedBudgetSchedule,
) error {
	return o.notifySchedule(
		ctx, item.ID, "budget_schedule_failed", item.FailureReason, item.FailedAt,
	)
}

func (o *BudgetLifecycleNotificationObserver) OnOverrideExpired(
	ctx context.Context,
	item ExpiredBudgetOverride,
) error {
	recipients := make([]uuid.UUID, 0, 4)
	if item.ExpansionRequestID != nil {
		req, err := o.expansions.FindByID(ctx, *item.ExpansionRequestID)
		if err != nil {
			return err
		}
		recipients = append(recipients, req.RequesterID)
	}
	owners, err := o.owners.ResolveOwnerRecipients(ctx, item.BudgetAllocationID)
	if err != nil {
		return err
	}
	recipients = append(recipients, owners...)
	for _, recipientID := range uniqueUUIDs(recipients) {
		key := "override-expired:" + item.ID.String() + ":" + recipientID.String()
		if err := o.notifier.SendOnce(ctx, key, recipientID, Notification{
			Type: "expansion_restored",
			Data: map[string]any{
				"dimension_type": item.DimensionType,
				"period_type": item.PeriodType,
				"expired_at": item.ExpiredAt,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}
```

`notifySchedule` 用 `BudgetScheduleReader.FindByID` 读取 `created_by` 与 `budget_allocation_id`，再调用固定签名 `ResolveOwnerRecipients(ctx, budgetAllocationID)`；override 直接使用事件中的 `BudgetAllocationID` 调同一 resolver。Resolver 必须先查 `budget_allocations.scope_type/scope_id`，再按真实 scope 解析负责人。禁止把 allocation UUID 或 `dimension_type` 直接作为 `scope_type/scope_id`。收件人是创建者和当前 owner；幂等键分别为 `schedule-applied:<schedule-id>:<recipient-id>` 与 `schedule-failed:<schedule-id>:<recipient-id>`。失败通知必须包含 `failure_reason`、失败时间、维度和周期，并提供重新配置入口；不得把失败伪装成普通生效通知。

P0 是 `BudgetLifecycleExecutor` 的唯一实现和唯一 worker owner：它负责定时扫描、`FOR UPDATE SKIP LOCKED`、状态迁移、事务提交和快照发布。P2 严格实现 P0 `BudgetLifecycleObserver` 的三个既有方法 `OnOverrideExpired`、`OnScheduleApplied`、`OnScheduleFailed`；其中 `OnScheduleFailed` 的签名固定为 `(context.Context, FailedBudgetSchedule) error`，除 `ctx` 外只接收一个对象，不得改成切片或拆散参数。P2 不调用或声明任何 executor 内部方法，不创建 ticker/goroutine，不修改 P0 repository 的锁查询。

- [ ] **Step 4: 在现有 P0 executor 注入 observer**

```go
budgetOwnerResolver := service.NewDatabaseBudgetOwnerResolver(
	budgetAllocationRepo,
	scopeOwnerReader,
)
lifecycleObserver := service.NewBudgetLifecycleNotificationObserver(
	expansionRepo,
	budgetScheduleRepo,
	budgetOwnerResolver,
	sharedNotifier,
)
lifecycleExecutor := service.NewBudgetLifecycleExecutor(
	lifecycleRepo,
	totalWriter,
	tierWriter,
	budgetTierSnapshotPublisher,
	lifecycleObserver,
)
```

Router 只把原来传给 P0 唯一 executor 的 no-op observer 替换为通知 observer，不新增 executor 或启动逻辑。P0 按状态结果调用对应 observer 方法；P0 重试或重复回调时，`SendOnce` 保证 override 恢复、schedule 生效和 schedule 失败通知均不重复。

- [ ] **Step 5: 运行 observer 业务测试**

Run: `go test ./tests/controlplane/service -run 'LifecycleObserver' -count=1`

Expected: PASS；重复事件每个收件人只通知一次，owner 查询只使用 allocation ID 并落到 allocation 的真实 scope，observer 不触碰生命周期状态或快照。

- [ ] **Step 6: 提交**

```bash
git add internal/controlplane/service/budget_lifecycle_notification_observer.go internal/controlplane/service/budget_owner_resolver.go internal/controlplane/service/notifier.go internal/controlplane/router/router.go tests/controlplane/service/budget_lifecycle_notification_observer_test.go
git commit -m "feat(budget): notify lifecycle outcomes"
```

### Task 6: 接通审批详情、reprecheck、notify-owner、审计与通知

**Files:**
- Modify: `internal/controlplane/handler/expansion_handler.go`
- Modify: `internal/controlplane/router/router.go`
- Modify: `internal/controlplane/audit/types.go`
- Modify: `internal/controlplane/service/notifier.go`
- Test: `tests/controlplane/handler/expansion_handler_test.go`

- [ ] **Step 1: 写失败的 HTTP 行为测试**

```go
func TestStaleApprovalReturnsConflictAndReprecheckLink(t *testing.T) {
	r := newExpansionRouterFixture(t)
	res := performJSON(r, "POST", "/api/v1/expansion-requests/req-1/decision", map[string]any{
		"action": "approve", "precheck_version": "old", "comment": "",
	}, approverToken)
	require.Equal(t, http.StatusConflict, res.Code)
	require.JSONEq(t, `{
	  "code":"EXPANSION_PRECHECK_STALE",
	  "message":"预算或用量已变化，请重新预检后确认",
	  "next_action":"reprecheck"
	}`, res.Body.String())
}

func TestNotifyOwnerCreatesNoExpansionRequest(t *testing.T) {
	r := newExpansionRouterFixture(t)
	before := fixtureRequestCount(t)
	res := performJSON(r, "POST", "/api/v1/expansion-requests/notify-owner", map[string]any{
		"scope_type":"dept_daily",
		"scope_id":"d-1",
		"period_type":"monthly",
		"dimension_type":"tier",
		"model_tier_id":"d753eb15-37e5-4180-9450-3eab10486ce7",
		"message":"高级等级已由部门共享池阻断"
	}, userToken)
	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, before, fixtureRequestCount(t))
	require.Equal(t, 1, fixtureNotificationCount(t, "budget_owner_attention"))
}
```

- [ ] **Step 2: 运行 handler 测试并确认失败**

Run: `go test ./tests/controlplane/handler -run 'StaleApproval|NotifyOwner|ExpansionDetail' -count=1`

Expected: FAIL，路由不存在。

- [ ] **Step 3: 注册完整扩容 API，静态路由必须在 `:id` 前**

```go
adminGroup.POST("/expansion-requests/precheck", expansionHandler.Precheck)
adminGroup.POST("/expansion-requests/notify-owner", expansionHandler.NotifyOwner)
adminGroup.GET("/expansion-requests", expansionHandler.ListMine)
adminGroup.GET("/expansion-requests/pending", guard(rbac.PermExpansionApprove), expansionHandler.ListPending)
adminGroup.POST("/expansion-requests", expansionHandler.Create)
adminGroup.GET("/expansion-requests/:id", expansionHandler.Get)
adminGroup.PATCH("/expansion-requests/:id", expansionHandler.Update)
adminGroup.POST("/expansion-requests/:id/reprecheck", expansionHandler.Reprecheck)
adminGroup.POST("/expansion-requests/:id/decision", expansionHandler.Decide)
adminGroup.GET("/expansion-requests/:id/steps", expansionHandler.ListSteps)
```

详情响应包含 `submitted_snapshot`、`live_snapshot`、`parent_constraints`、`approval_pool`、`affected_groups`、`steps`、`capabilities` 和 `data_as_of`。`capabilities.can_decide=false` 时不返回可执行动作。

- [ ] **Step 4: 明确意见校验与审计字段**

```go
func validateDecision(in DecisionInput) error {
	if (in.Action == "reject" || in.Action == "escalate") && strings.TrimSpace(in.Comment) == "" {
		return NewAPIError("DECISION_COMMENT_REQUIRED", "驳回和上报必须填写审批意见")
	}
	return nil
}
```

增加 `ActionExpansionPrecheck`、`ActionExpansionReprecheck`、`ActionExpansionNotifyOwner`、`ActionExpansionApplyFailed`、`ActionBudgetAdjustPreview`、`ActionBudgetAdjustApply`、`ActionModelTierArchive`；审批审计 `Details` 保存 request ID、旧/新状态、维度、等级、周期、目标、持续类型、precheck version、同步结果，不写邮件地址或上级脱敏金额。

- [ ] **Step 5: 使用现有 `MultiNotifier` 发送事件**

通知类型固定为：

```go
const (
	NotificationTypeExpansionSubmitted   = "expansion_submitted"
	NotificationTypeExpansionDecision    = "expansion_decision"
	NotificationTypeExpansionApplyFailed = "expansion_apply_failed"
	NotificationTypeExpansionExpiring    = "expansion_expiring"
	NotificationTypeExpansionRestored    = "expansion_restored"
	NotificationTypeBudgetOwnerAttention = "budget_owner_attention"
)
```

提交通知动态审批池；批准/驳回/取消/上报通知申请人；临时额度到期前与恢复后通知申请人和负责人；生效失败通知审批池与平台管理员。站内信和邮件默认走 `sharedNotifier`，Webhook/飞书仅在设置开启时发送。

- [ ] **Step 6: 运行 handler、审计和通知测试**

Run: `go test ./tests/controlplane/handler ./tests/controlplane/service -run 'Expansion|NotifyOwner|Audit|Notification' -count=1`

Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/controlplane/handler/expansion_handler.go internal/controlplane/router/router.go internal/controlplane/audit/types.go internal/controlplane/service/notifier.go tests/controlplane/handler/expansion_handler_test.go
git commit -m "feat(budget): expose expansion approval workflow"
```

### Task 7: 实现风险事件实时投影 API

**Files:**
- Create: `internal/controlplane/repository/budget_risk_repo.go`
- Create: `internal/controlplane/service/budget_risk_service.go`
- Create: `internal/controlplane/handler/budget_risk_handler.go`
- Create: `tests/controlplane/repository/budget_risk_repo_test.go`
- Create: `tests/controlplane/service/budget_risk_service_test.go`
- Create: `tests/controlplane/handler/budget_risk_handler_test.go`
- Modify: `internal/controlplane/router/router.go`

- [ ] **Step 1: 写失败的 call_logs 聚合与风险合并测试**

```go
func TestBudgetRiskRepoAggregatesP1BudgetBlockFields(t *testing.T) {
	db := newRiskRepoFixture(t)
	db.insertCallLog(callLog{
		Status: 402, BudgetTierID: &tierHigh,
		BudgetBlockCode: stringPtr("tier_budget_exceeded"),
		BudgetBlockDimension: stringPtr("tier"),
		BudgetBlockScopeType: stringPtr("dept_daily"),
		BudgetBlockScopeID: stringPtr("d-1"),
		BudgetBlockPeriodType: stringPtr("monthly"),
		CreatedAt: time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC),
	})
	db.insertCallLog(callLog{
		Status: 402, BudgetTierID: &tierHigh,
		BudgetBlockCode: stringPtr("tier_budget_exceeded"),
		BudgetBlockDimension: stringPtr("tier"),
		BudgetBlockScopeType: stringPtr("dept_daily"),
		BudgetBlockScopeID: stringPtr("d-1"),
		BudgetBlockPeriodType: stringPtr("monthly"),
		CreatedAt: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
	})
	db.insertCallLog(callLog{
		Status: 200,
		BudgetBlockCode: nil,
		BudgetBlockDimension: nil,
		BudgetBlockScopeType: nil,
		BudgetBlockScopeID: nil,
		BudgetBlockPeriodType: nil,
	})

	got, err := repository.NewBudgetRiskRepo(db.DB).AggregateBlockedRequests(
		context.Background(),
		repository.BlockedRequestQuery{
			From: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			To: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			ScopeTypes: []string{"dept_daily"},
			ScopeIDs: []string{"d-1"},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []repository.BlockedRequestAggregate{{
		Code: "tier_budget_exceeded", DimensionType: "tier",
		ScopeType: "dept_daily", ScopeID: "d-1", PeriodType: "monthly",
		ModelTierID: &tierHigh, Count: 2,
		FirstBlockedAt: time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC),
		LastBlockedAt: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
	}}, got)
}

func TestRiskProjectionCombinesThresholdAndBlockedRequests(t *testing.T) {
	blockRepo := &fakeBudgetRiskRepo{rows: []repository.BlockedRequestAggregate{{
		Code: "tier_budget_exceeded", DimensionType: "tier",
		ScopeType: "dept_daily", ScopeID: "d-1", PeriodType: "monthly",
		ModelTierID: &tierHigh, Count: 3, FirstBlockedAt: now.Add(-time.Hour),
	}}}
	svc := newRiskFixture(t, blockRepo)
	svc.usage.SetTier("dept_daily", "d-1", tierHigh, "monthly", 95_000_000)
	svc.limits.SetTier("dept_daily", "d-1", tierHigh, "monthly", 100_000_000)
	got, err := svc.List(context.Background(), managerActor("d-1"), RiskFilter{
		PeriodType: "monthly", DimensionType: "tier",
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
	require.Equal(t, []string{"request_blocked", "threshold_reached"},
		[]string{got.Items[0].RiskType, got.Items[1].RiskType})
	require.Equal(t, "95.000000", got.Items[0].UsedUSD)
}

func TestRiskDisappearsAfterUsageDropsBelowThreshold(t *testing.T) {
	svc := newRiskFixture(t)
	svc.usage.SetTotal("member", "u-1", "monthly", 81_000_000)
	svc.limits.SetTotal("member", "u-1", "monthly", 100_000_000)
	require.Len(t, mustListRisks(t, svc), 1)
	svc.usage.SetTotal("member", "u-1", "monthly", 79_000_000)
	require.Empty(t, mustListRisks(t, svc))
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/controlplane/repository ./tests/controlplane/service -run 'BudgetRiskRepo|RiskProjection|RiskDisappears|RiskStale' -count=1`

Expected: FAIL，`BudgetRiskRepo` 和真实阻断聚合接口不存在。

- [ ] **Step 3: 实现只读取 P1 block 字段的 repository 查询**

```go
type BlockedRequestAggregate struct {
	Code           string
	DimensionType  string
	ScopeType      string
	ScopeID        string
	PeriodType     string
	ModelTierID    *uuid.UUID
	Count          int64
	FirstBlockedAt time.Time
	LastBlockedAt  time.Time
}

func (r *BudgetRiskRepo) AggregateBlockedRequests(
	ctx context.Context,
	q BlockedRequestQuery,
) ([]BlockedRequestAggregate, error) {
	var rows []BlockedRequestAggregate
	err := r.db.WithContext(ctx).Table("call_logs").
		Select(`budget_block_code AS code,
			budget_block_dimension AS dimension_type,
			budget_block_scope_type AS scope_type,
			budget_block_scope_id AS scope_id,
			budget_block_period_type AS period_type,
			CASE WHEN budget_block_dimension = 'tier' THEN budget_tier_id ELSE NULL END AS model_tier_id,
			COUNT(*) AS count, MIN(created_at) AS first_blocked_at,
			MAX(created_at) AS last_blocked_at`).
		Where("status = ?", 402).
		Where("created_at >= ? AND created_at < ?", q.From, q.To).
		Where("budget_block_code IN ?", []string{"budget_exceeded", "tier_budget_exceeded"}).
		Where("budget_block_dimension IN ?", []string{"total", "tier"}).
		Where("(budget_block_dimension <> 'tier' OR budget_tier_id IS NOT NULL)").
		Where("budget_block_scope_type IN ? AND budget_block_scope_id IN ?", q.ScopeTypes, q.ScopeIDs).
		Where("budget_block_period_type IN ?", []string{"daily", "weekly", "monthly"}).
		Group(`budget_block_code, budget_block_dimension,
			budget_block_scope_type, budget_block_scope_id,
			budget_block_period_type,
			CASE WHEN budget_block_dimension = 'tier' THEN budget_tier_id ELSE NULL END`).
		Scan(&rows).Error
	return rows, err
}
```

查询时间窗使用 `internal/budgetperiod.Resolve` 得到当前日/周/月 `[start, reset)`；Actor 管辖范围先解析为允许的 scope type/ID 后传 repository。不得从 access log、结构化应用日志、Redis 计数差值或未定义内存 `blocks` fake 推断真实 402。`tier` 行要求 `budget_tier_id IS NOT NULL`，`total` 行归一化为 nil。

- [ ] **Step 4: 实现稳定事件 ID 和投影模型**

```go
type BudgetRiskEvent struct {
	ID              string    `json:"id"`
	RiskType        string    `json:"risk_type"`
	Severity        string    `json:"severity"`
	ScopeType       string    `json:"scope_type"`
	ScopeID         string    `json:"scope_id"`
	ScopeName       string    `json:"scope_name"`
	PeriodType      string    `json:"period_type"`
	DimensionType   string    `json:"dimension_type"`
	ModelTierID     *string   `json:"model_tier_id"`
	ModelTierName   string    `json:"model_tier_name"`
	EffectiveLimit  string    `json:"effective_limit_usd"`
	UsedUSD         string    `json:"used_usd"`
	UsageRatio      string    `json:"usage_ratio"`
	LimitSource     string    `json:"limit_source"`
	BlockedRequests int64     `json:"blocked_requests"`
	DetectedAt      time.Time `json:"detected_at"`
	DataAsOf        time.Time `json:"data_as_of"`
	Stale           bool      `json:"stale"`
}

func riskEventID(scopeType, scopeID, periodType, dimension, tierID, riskType string) string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{scopeType, scopeID, periodType, dimension, tierID, riskType}, "|",
	)))
	return hex.EncodeToString(sum[:16])
}
```

风险类型固定为 `threshold_reached`、`budget_exhausted`、`request_blocked`；严重度 `warning|critical`。`request_blocked` 只由 repository 的 `BlockedRequestAggregate` 生成，`DetectedAt=FirstBlockedAt`、`BlockedRequests=Count`。价格或用量缺失产生的 `$0` 不生成阈值事件。Redis 不可用时阈值事件读取 last-known 快照并统一 `stale=true`；真实 402 聚合仍可返回，但整体响应保持 `stale=true`，没有 last-known 时返回 `BUDGET_USAGE_UNAVAILABLE`。

- [ ] **Step 5: 实现 GET 列表和摘要**

```go
func (h *BudgetRiskHandler) List(c *gin.Context) {
	actor := h.actor(c)
	filter := service.RiskFilter{
		ScopeType: c.Query("scope_type"), ScopeID: c.Query("scope_id"),
		PeriodType: c.Query("period_type"), DimensionType: c.Query("dimension_type"),
		ModelTierID: c.Query("model_tier_id"), RiskType: c.Query("risk_type"),
		Severity: c.Query("severity"),
	}
	result, err := h.svc.List(c.Request.Context(), actor, filter)
	if err != nil {
		writeBudgetRiskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
```

注册：

```go
adminGroup.GET("/budget/risk-events", guard(rbac.PermBudgetViewScoped), riskHandler.List)
adminGroup.GET("/budget/risk-events/summary", guard(rbac.PermBudgetViewScoped), riskHandler.Summary)
```

摘要响应包含 `total`、`warning`、`critical`、`by_type`、`data_as_of`、`stale`。

- [ ] **Step 6: 运行 repository、service 与 API 测试**

Run: `go test ./tests/controlplane/repository ./tests/controlplane/service ./tests/controlplane/handler -run 'BudgetRiskRepo|Risk' -count=1`

Expected: PASS；非全局管理员只能看管辖范围，request-blocked 数量来自 `call_logs` 聚合。

- [ ] **Step 7: 提交**

```bash
git add internal/controlplane/repository/budget_risk_repo.go internal/controlplane/service/budget_risk_service.go internal/controlplane/handler/budget_risk_handler.go tests/controlplane/repository/budget_risk_repo_test.go tests/controlplane/service/budget_risk_service_test.go tests/controlplane/handler/budget_risk_handler_test.go internal/controlplane/router/router.go
git commit -m "feat(budget): add scoped risk event projection"
```

### Task 8: 在 P0 调额 API 上补齐高风险确认语义

**Files:**
- Modify: `internal/controlplane/handler/budget_handler.go`
- Modify: `internal/controlplane/service/budget_service.go`
- Test: `tests/controlplane/service/budget_service_test.go`
- Test: `tests/controlplane/handler/budget_handler_test.go`

- [ ] **Step 1: 写失败的确认预览与陈旧版本测试**

```go
func TestBudgetAdjustmentRequiresRiskConfirmations(t *testing.T) {
	svc := newBudgetFixture(t)
	got, err := svc.SetBudget(ctx, actor, SetBudgetInput{
		ScopeType: "dept_daily", ScopeID: "d-1", PeriodType: "monthly",
		DimensionType: "tier", ModelTierID: &tierHigh,
		TargetLimitUSD: "0.000000", EffectiveMode: "immediate",
		Reason: "暂停高级模型用于成本控制",
	})
	require.ErrorIs(t, err, ErrZeroBlockConfirmationRequired)
	require.Nil(t, got)
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `go test ./tests/controlplane/service ./tests/controlplane/handler -run 'BudgetAdjustment|BudgetTier' -count=1`

Expected: FAIL，P0 的写接口尚未强制高风险确认字段。

- [ ] **Step 3: 扩展 P0 写入 DTO，不新增平行路由**

```go
type BudgetRiskConfirmation struct {
	ConfirmBelowUsed       bool `json:"confirm_below_used"`
	ConfirmZeroBlock       bool `json:"confirm_zero_block"`
	ConfirmReplaceSchedule bool `json:"confirm_replace_schedule"`
}
```

等级额度继续使用 P0 `PUT /api/v1/budgets/:allocation_id/tier-limits`；等级批量继续使用 P0 的 batch preview/apply；总预算继续扩展现有 `PUT /api/v1/budgets` 和 `PUT /api/v1/budgets/batch`，不得增加平行调额路由。`effective_mode=immediate` 更新基础额度且保留累计；`next_period` 请求只调用 P0 已有的 schedule 创建能力，之后由 P0 唯一 `BudgetLifecycleExecutor` 同时执行 `dimension_type=total|tier`。P2 不直接写或扫描 `budget_limit_schedules`，不调用 executor 内部方法。schedule 状态只使用 `pending|applied|replaced|cancelled|failed`。降到已用以下要求 `confirm_below_used=true`，设 `$0` 要求 `confirm_zero_block=true`，替换 pending schedule 要求 `confirm_replace_schedule=true`，原因必填。

- [ ] **Step 4: 返回确认页所需影响信息**

```go
type BudgetAdjustmentImpact struct {
	BeforeLimitUSD  string          `json:"before_limit_usd"`
	AfterLimitUSD   string          `json:"after_limit_usd"`
	CurrentUsedUSD  string          `json:"current_used_usd"`
	AffectedMembers int             `json:"affected_members"`
	AffectedGroups  []AffectedGroup `json:"affected_groups"`
	WillBlock       bool            `json:"will_block"`
	StreamPolicy    string          `json:"stream_policy"`
	DataAsOf        time.Time       `json:"data_as_of"`
	Stale           bool            `json:"stale"`
}
```

P0 batch preview 返回逐项 `before/after/delta/parent_validation/valid/error_code`，P2 只补 `affected_members`、`affected_groups`、`stream_policy` 与陈旧标记供确认 UI 使用；`stale=true` 时写接口返回 `BUDGET_USAGE_UNAVAILABLE`，不允许高风险调额。

- [ ] **Step 5: 运行预算测试**

Run: `go test ./tests/controlplane/service ./tests/controlplane/handler -run 'BudgetAdjustment|BudgetTier|Schedule|ZeroBlock' -count=1`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/controlplane/handler/budget_handler.go internal/controlplane/service/budget_service.go tests/controlplane/service/budget_service_test.go tests/controlplane/handler/budget_handler_test.go
git commit -m "feat(budget): add confirmed direct adjustments"
```

### Task 9: 建立前端类型、服务、store 与接口契约

**Files:**
- Create: `web/src/types/model-tier.ts`
- Create: `web/src/types/budget-risk.ts`
- Create: `web/src/stores/model-tier.ts`
- Create: `web/src/stores/budget-nav.ts`
- Modify: `web/src/types/budget.ts`
- Modify: `web/src/types/expansion.ts`
- Modify: `web/src/services/control-plane.ts`
- Modify: `tests/contract/contracts.json`

- [ ] **Step 1: 先接入缺失类型，形成失败的 TypeScript 检查**

```ts
import type {
  BudgetRiskFilter,
  BudgetRiskPage,
} from '@/types/budget-risk'
import type {
  ModelTier,
  MyBudgetTiersResponse,
} from '@/types/model-tier'
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，`@/types/model-tier`、`@/types/budget-risk` 或新增 DTO 尚未定义。

- [ ] **Step 3: 定义精确预算/等级/风险类型**

```ts
export interface ModelTier {
  id: string
  name: string
  description: string
  color: string
  sort_order: number
  enabled: boolean
  archived_at: string | null
  group_count: number
  budget_count: number
  history_count: number
  created_at: string
  updated_at: string
}

export type TierLimitState = 'unconfigured' | 'enabled' | 'zero_blocked' | 'disabled'

export interface TierPeriodUsage {
  period_type: ExpansionPeriod
  configured: boolean
  base_limit_usd?: MoneyUSD
  temporary_target_usd?: MoneyUSD
  effective_limit_usd?: MoneyUSD
  used_usd?: MoneyUSD
  remaining_usd?: MoneyUSD
  reset_at: string
  status: string
  limiting_source: {
    scope_type: BudgetScopeType
    remaining_usd?: MoneyUSD
  }
}

export interface MyBudgetSourceSummary {
  mode: 'personal' | 'shared' | 'mixed'
  primary_scope_type: BudgetScopeType
  has_shared_pool: boolean
  has_temporary_override: boolean
}

export interface EffectiveTotalPeriod {
  period_type: ExpansionPeriod
  configured: boolean
  base_limit_usd?: MoneyUSD
  temporary_target_usd?: MoneyUSD
  effective_limit_usd?: MoneyUSD
  used_usd?: MoneyUSD
  remaining_usd?: MoneyUSD
  reset_at: string
}

export interface BudgetChainNode {
  scope_type: BudgetScopeType
  label: string
  visible_amounts: boolean
}

export interface MyBudgetTierLimit {
  model_tier_id: string
  model_tier_name: string
  periods: TierPeriodUsage[]
}

export interface MyBudgetTiersResponse {
  source_summary: MyBudgetSourceSummary
  effective_total: EffectiveTotalPeriod[]
  budget_chain: BudgetChainNode[]
  tier_limits: MyBudgetTierLimit[]
  data_updated_at: string
  stale: boolean
}
```

`GET /api/v1/my/budget/tiers` 必须直接使用 P0 DTO：`source_summary.mode` 只允许 `personal|shared|mixed`，`effective_total` 是按周期排列的数组，`budget_chain` 使用 `label/visible_amounts` 做上级脱敏，`tier_limits` 使用 `model_tier_id/model_tier_name/periods`，更新时间只叫 `data_updated_at`。临时额度字段只叫 `temporary_target_usd`；P2 不引入旧 mode 或平行顶层字段别名。

```ts
export interface BudgetRiskEvent {
  id: string
  risk_type: 'threshold_reached' | 'budget_exhausted' | 'request_blocked'
  severity: 'warning' | 'critical'
  scope_type: BudgetScopeType
  scope_id: string
  scope_name: string
  period_type: ExpansionPeriod
  dimension_type: ExpansionDimension
  model_tier_id: string | null
  model_tier_name: string
  effective_limit_usd: MoneyUSD
  used_usd: MoneyUSD
  usage_ratio: string
  limit_source: string
  blocked_requests: number
  detected_at: string
  data_as_of: string
  stale: boolean
}
```

- [ ] **Step 4: 增加类型安全服务函数**

```ts
export async function precheckExpansion(input: ExpansionPrecheckInput): Promise<ExpansionPrecheck> {
  const { data } = await http.post<{ data: ExpansionPrecheck }>(
    '/api/v1/expansion-requests/precheck', input,
  )
  return data.data
}

export async function reprecheckExpansion(id: string): Promise<ExpansionDetail> {
  const { data } = await http.post<{ data: ExpansionDetail }>(
    `/api/v1/expansion-requests/${id}/reprecheck`,
  )
  return data.data
}

export async function decideExpansion(
  id: string,
  input: { action: ExpansionAction; comment: string; precheck_version: string },
): Promise<ExpansionDetail> {
  const { data } = await http.post<{ data: ExpansionDetail }>(
    `/api/v1/expansion-requests/${id}/decision`, input,
  )
  return data.data
}

export async function notifyBudgetOwner(input: NotifyOwnerInput): Promise<void> {
  await http.post('/api/v1/expansion-requests/notify-owner', input)
}

export async function listBudgetRisks(filter: BudgetRiskFilter): Promise<BudgetRiskPage> {
  const { data } = await http.get<{ data: BudgetRiskPage }>(
    '/api/v1/budget/risk-events', { params: filter },
  )
  return data.data
}
```

同文件增加 model tier CRUD/reorder/status/delete、单个/批量 group 关联，以及对 P0 `tier-limits`、`tier-limits/batch/preview`、`tier-limits/batch/apply`、`tier-usage`、`my/budget/tiers` 的消费函数；P2 新增扩容 precheck/reprecheck/decision/notify-owner 与风险 summary/list。不得创建 P0 基础 API 的假 handler。

- [ ] **Step 5: 更新 contracts.json 为可机器检查的契约**

新增顶层 `model_tier_budget_p2`：

```json
{
  "model_tier_budget_p2": {
    "money_format": "^-?[0-9]+\\.[0-9]{6}$",
    "legacy_number_fields": ["current_amount", "requested_amount"],
    "decimal_string_fields": [
      "current_amount_snapshot", "used_amount_snapshot", "target_amount_usd"
    ],
    "risk_block_source_fields": [
      "budget_block_code", "budget_block_dimension",
      "budget_block_scope_type", "budget_block_scope_id",
      "budget_block_period_type"
    ],
    "expansion_status_enum": [
      "pending", "precheck_required", "approved",
      "rejected", "cancelled", "expired"
    ],
    "routes": [
      {
        "method": "GET",
        "path": "/api/v1/expansion-requests",
        "response_status": [200, 401]
      },
      {
        "method": "GET",
        "path": "/api/v1/expansion-requests/pending",
        "response_status": [200, 401, 403]
      },
      {
        "method": "GET",
        "path": "/api/v1/expansion-requests/:id/steps",
        "response_status": [200, 401, 403, 404]
      },
      {
        "method": "POST",
        "path": "/api/v1/expansion-requests/precheck",
        "response_status": [200, 400, 403, 409, 503],
        "response_fields": [
          "can_submit", "can_notify_owner", "current_limit_usd",
          "current_used_usd", "target_amount_usd", "increase_usd",
          "precheck_version", "data_as_of", "stale",
          "parent_constraints", "affected_groups", "visible_scopes"
        ]
      },
      {
        "method": "GET",
        "path": "/api/v1/budget/risk-events",
        "response_status": [200, 400, 403, 503],
        "response_fields": ["items", "total", "data_as_of", "stale"]
      },
      {
        "method": "GET",
        "path": "/api/v1/my/budget/tiers",
        "response_status": [200, 401, 503],
        "response_fields": [
          "source_summary", "effective_total", "budget_chain",
          "tier_limits", "data_updated_at", "stale"
        ]
      }
    ]
  }
}
```

契约更新采用追加/扩展语义：保留 P0 已锁定的全部路由，并保留现有扩容列表、待审批列表和审批流水路由。不得把 contract 的 route 集合与本功能清单做全等比较；Go handler/contract 测试只断言下列既有与新增路由存在且 method 正确：

```json
[
  ["POST", "/api/v1/expansion-requests"],
  ["GET", "/api/v1/expansion-requests"],
  ["GET", "/api/v1/expansion-requests/pending"],
  ["GET", "/api/v1/expansion-requests/:id"],
  ["PATCH", "/api/v1/expansion-requests/:id"],
  ["GET", "/api/v1/expansion-requests/:id/steps"],
  ["POST", "/api/v1/expansion-requests/precheck"],
  ["POST", "/api/v1/expansion-requests/:id/reprecheck"],
  ["POST", "/api/v1/expansion-requests/:id/decision"],
  ["POST", "/api/v1/expansion-requests/notify-owner"],
  ["GET", "/api/v1/budget/risk-events"],
  ["GET", "/api/v1/budget/risk-events/summary"]
]
```

- [ ] **Step 6: 实现 URL 驱动的 budget nav store**

```ts
export const useBudgetNavStore = defineStore('budgetNav', {
  state: () => ({
    tab: 'overview' as 'overview' | 'risks',
    current: { type: 'company', id: 'GLOBAL' } as BudgetNavNode,
    breadcrumbs: [] as BudgetNavNode[],
  }),
  actions: {
    hydrate(query: LocationQuery) {
      this.tab = query.tab === 'risks' ? 'risks' : 'overview'
      const types = arrayQuery(query.parent_scope_type)
      const ids = arrayQuery(query.parent_scope_id)
      const parents = ids.map((id, index) => ({ type: types[index] as BudgetNodeType, id }))
      this.current = {
        type: (query.scope_type as BudgetNodeType) || 'company',
        id: String(query.scope_id || 'GLOBAL'),
      }
      this.breadcrumbs = [...parents, this.current]
    },
    toQuery(): LocationQueryRaw {
      return {
        tab: this.tab === 'risks' ? 'risks' : undefined,
        scope_type: this.current.type,
        scope_id: this.current.id,
        parent_scope_type: this.breadcrumbs.slice(0, -1).map(v => v.type),
        parent_scope_id: this.breadcrumbs.slice(0, -1).map(v => v.id),
      }
    },
  },
})
```

- [ ] **Step 7: 运行 TypeScript 与构建验证**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: TypeScript 和 Vite build 全部 PASS。

- [ ] **Step 8: 提交**

```bash
git add web/src/types/model-tier.ts web/src/types/budget-risk.ts web/src/types/budget.ts web/src/types/expansion.ts web/src/services/control-plane.ts web/src/stores/model-tier.ts web/src/stores/budget-nav.ts tests/contract/contracts.json
git commit -m "feat(web): add model tier budget contracts"
```

### Task 10: 实现模型等级 CRUD、归档与模型组批量关联

**Files:**
- Create: `web/src/views/admin/ModelTierListPage.vue`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/App.vue`

- [ ] **Step 1: 先注册页面路由，形成失败的 TypeScript 检查**

```ts
{
  path: '/admin/model-tiers',
  name: 'ModelTiers',
  component: () => import('@/views/admin/ModelTierListPage.vue'),
  meta: { title: '模型等级', requiresAdmin: true },
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，`ModelTierListPage.vue` 不存在。

- [ ] **Step 3: 注册平台专属入口且保留“扩容申请”原菜单**

```ts
{
  path: '/admin/model-tiers',
  name: 'ModelTiers',
  component: () => import('@/views/admin/ModelTierListPage.vue'),
  meta: { title: '模型等级', requiresAdmin: true },
}
```

```vue
<el-menu-item v-if="authStore.isAdmin" index="/admin/model-tiers">
  <el-icon><Flag /></el-icon>
  <template #title>模型等级</template>
</el-menu-item>
<el-menu-item v-if="authStore.isManager" index="/admin/expansion-requests">
  <el-icon><TrendCharts /></el-icon>
  <template #title>扩容申请</template>
</el-menu-item>
```

- [ ] **Step 4: 实现列表、编辑、排序、启停、归档/删除与批量关联**

页面包含：名称、颜色、说明、排序、启用状态、模型组数、预算引用数、历史数。拖拽排序提交完整 `{id, sort_order}` 数组；停用确认文案明确“暂停等级计数，历史消费继续进入报表”；引用或历史大于零只显示“归档”，全部为零才显示“删除”。归档和删除均要求原因。

模型组关联抽屉提供等级过滤、关键字、未分级筛选、多选；提交：

```ts
await batchAssignGroupBudgetTier({
  group_ids: selectedGroupIds.value,
  model_tier_id: assignmentMode.value === 'clear' ? null : selectedTierId.value,
  reason: assignmentReason.value.trim(),
})
selectedGroupIds.value = []
await Promise.all([tierStore.refresh(), loadGroups()])
```

未关联行显示“未分级，仅受总预算控制”，同时展示组内实际模型参考价格。

- [ ] **Step 5: 运行 TypeScript 与 Vite 构建验证**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add web/src/views/admin/ModelTierListPage.vue web/src/router/index.ts web/src/App.vue
git commit -m "feat(web): add model tier administration"
```

### Task 11: 重构预算管理为两页签和 URL 同页下钻

**Files:**
- Modify: `web/src/views/admin/BudgetManagePage.vue`
- Create: `web/src/components/budget/BudgetOverviewSection.vue`
- Create: `web/src/components/common/DataFreshnessNotice.vue`

- [ ] **Step 1: 先接入拆分组件，形成失败的 TypeScript 检查**

```ts
import BudgetOverviewSection from '@/components/budget/BudgetOverviewSection.vue'
import DataFreshnessNotice from '@/components/common/DataFreshnessNotice.vue'
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，拆分组件尚不存在。

- [ ] **Step 3: 将页面收敛成页签编排器**

```vue
<template>
  <main class="budget-manage">
    <header class="page-header">
      <div>
        <h1>预算管理</h1>
        <BudgetBreadcrumbs
          v-if="nav.tab === 'overview'"
          :items="nav.breadcrumbs"
          @navigate="navigateTo"
        />
      </div>
      <DataFreshnessNotice :data-as-of="dataAsOf" :stale="stale" @refresh="refreshKey++" />
    </header>
    <el-tabs v-model="nav.tab" @tab-change="syncRoute">
      <el-tab-pane label="预算总览" name="overview">
        <BudgetOverviewSection
          :node="nav.current"
          :refresh-key="refreshKey"
          @drilldown="drilldown"
          @data-state="setDataState"
        />
      </el-tab-pane>
      <el-tab-pane name="risks">
        <template #label>
          风险事件 <el-badge v-if="riskCount" :value="riskCount" />
        </template>
        <BudgetRiskEventsSection @locate-budget="locateBudget" />
      </el-tab-pane>
    </el-tabs>
  </main>
</template>
```

- [ ] **Step 4: 实现明确按钮的同页切换**

公司表仅“查看部门”；工作室表仅“查看预算”；部门表展示日常池、服务池和成员，成员行仅“预算详情”。点击后 `router.push({query: nav.toQuery()})`；名称不触发下钻。浏览器 back/forward 监听 `route.query` 再 `hydrate`。任何层级最多打开一个编辑抽屉，不允许下级抽屉套在当前抽屉内。

- [ ] **Step 5: 实现空、失败和陈旧状态**

`DataFreshnessNotice` 在 `stale=true` 时显示：

```vue
<el-alert type="warning" :closable="false" show-icon>
  数据更新于 {{ formatDateTime(dataAsOf) }}，当前显示最后一次成功结果，不能替代网关实时判断。
  <el-button link type="warning" @click="$emit('refresh')">重新加载</el-button>
</el-alert>
```

公司无工作室预算时引导设置公司预算；工作室无部门预算时引导创建或等待组织同步；部门无独立成员额度时说明成员继续使用共享池。

- [ ] **Step 6: 运行页面测试**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add web/src/views/admin/BudgetManagePage.vue web/src/components/budget/BudgetOverviewSection.vue web/src/components/common/DataFreshnessNotice.vue
git commit -m "refactor(web): add same-page budget drilldown"
```

### Task 12: 实现等级额度配置、批量预览和直接调额确认

**Files:**
- Create: `web/src/components/budget/BudgetTierLimitsPanel.vue`
- Create: `web/src/components/budget/BudgetAdjustDrawer.vue`
- Create: `web/src/components/budget/BudgetBatchAdjustWizard.vue`
- Modify: `web/src/components/budget/BudgetOverviewSection.vue`

- [ ] **Step 1: 先接入额度组件，形成失败的 TypeScript 检查**

```ts
import BudgetTierLimitsPanel from './BudgetTierLimitsPanel.vue'
import BudgetAdjustDrawer from './BudgetAdjustDrawer.vue'
import BudgetBatchAdjustWizard from './BudgetBatchAdjustWizard.vue'
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，组件不存在。

- [ ] **Step 3: 实现单对象配置抽屉**

抽屉按对象显示：

- 公司：总预算 + 等级子预算。
- 工作室/部门：自身总预算只读或可编辑、日常池、服务池 + 等级子预算。
- 成员：独立总额度 + 等级子预算，不展示下级池。

数字输入是最终值，滑块只在父级可分配范围内辅助更新数字。提交前强制原因、生效模式 `immediate|next_period`，调用 preview 后展示原值、新值、已用、影响人数、模型组、流式策略、`$0` 风险、将替换的计划变更。预览版本变化时冻结保存并提示重新预览。

- [ ] **Step 4: 实现批量向导**

三种模式：

```ts
type BatchMode =
  | { kind: 'fixed'; target_limit_usd: MoneyUSD }
  | { kind: 'percentage'; percentage: string }
  | { kind: 'tier'; model_tier_id: string; target_limit_usd: MoneyUSD }
```

只允许选择直接下级；共享池成员设置固定额度时逐项标记“将转为个人独立额度”。预览表显示对象、原值、新值、差额、父级校验、失败原因；apply 只发送 `valid=true` 的项目，响应逐项展示成功/失败，不把部分失败伪装成整体成功。

- [ ] **Step 5: 运行前端结构、类型与构建验证**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: PASS，Vite build 成功。

- [ ] **Step 6: 提交**

```bash
git add web/src/components/budget/BudgetTierLimitsPanel.vue web/src/components/budget/BudgetAdjustDrawer.vue web/src/components/budget/BudgetBatchAdjustWizard.vue web/src/components/budget/BudgetOverviewSection.vue
git commit -m "feat(web): add tier budget adjustment workflows"
```

### Task 13: 实现风险事件页签与单层处置抽屉

**Files:**
- Create: `web/src/components/budget/BudgetRiskEventsSection.vue`
- Modify: `web/src/views/admin/BudgetManagePage.vue`

- [ ] **Step 1: 先接入风险组件，形成失败的 TypeScript 检查**

```ts
import BudgetRiskEventsSection from '@/components/budget/BudgetRiskEventsSection.vue'
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，组件不存在。

- [ ] **Step 3: 实现列表和过滤**

筛选包含组织范围、周期、总预算/等级、模型等级、风险类型、严重程度。列表展示类型、严重度、影响对象、额度、已用、使用率、真正限制来源、阻断次数、发现时间。页签 badge 使用 summary 的未处理实时数量；风险自然消失后列表刷新移除，不提供“忽略/关闭”。

- [ ] **Step 4: 实现单层处置抽屉**

抽屉展示完整预算链、对应等级三周期状态、近期阻断和相关模型组。动作：

- “查看预算”：关闭抽屉，切回 overview，并把 scope 写入 URL。
- “调整额度”：只有有直接配置权且数据不陈旧时打开同一抽屉内的确认步骤，不再打开第二层抽屉。
- “发起扩容”：没有直接配置权时把 scope/period/dimension/tier 带到扩容申请页。
- “通知负责人”：调用 `notify-owner`。
- “查看相关用量”：跳转用量看板并带 scope/tier query。

- [ ] **Step 5: 运行 TypeScript 与 Vite 构建验证**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add web/src/components/budget/BudgetRiskEventsSection.vue web/src/views/admin/BudgetManagePage.vue
git commit -m "feat(web): add budget risk event handling"
```

### Task 14: 重建扩容申请页的创建、审批详情和移动端摘要

**Files:**
- Modify: `web/src/views/admin/ExpansionRequestPage.vue`
- Create: `web/src/components/expansion/ExpansionCreateForm.vue`
- Create: `web/src/components/expansion/ExpansionApprovalDetail.vue`
- Create: `web/src/components/expansion/ExpansionPrecheckPanel.vue`
- Create: `web/src/components/expansion/ExpansionMobileListItem.vue`

- [ ] **Step 1: 先接入扩容子组件，形成失败的 TypeScript 检查**

```ts
import ExpansionCreateForm from '@/components/expansion/ExpansionCreateForm.vue'
import ExpansionApprovalDetail from '@/components/expansion/ExpansionApprovalDetail.vue'
import ExpansionPrecheckPanel from '@/components/expansion/ExpansionPrecheckPanel.vue'
import ExpansionMobileListItem from '@/components/expansion/ExpansionMobileListItem.vue'
```

- [ ] **Step 2: 运行 TypeScript 检查并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，组件不存在。

- [ ] **Step 3: 实现创建表单**

表单字段由入口预填：对象、总预算/等级、等级、日/周/月、长期/仅当前周期、普通/紧急、当前上限、已用、目标和增加额。紧急必须填写紧急原因；普通用户不显示组织选择。每次关键字段变化防抖调用 precheck，只有最新 `precheck_version` 可提交。

```ts
const primaryAction = computed(() =>
  precheck.value?.can_submit
    ? { key: 'submit', label: '提交申请' }
    : precheck.value?.can_notify_owner
      ? { key: 'notify', label: '通知负责人' }
      : { key: 'disabled', label: '当前不可申请' },
)
```

临时申请明确展示“基础额度、临时目标、有效额度、到期时间、到期后恢复届时基础额度”；只显示当前选择周期的数据。

- [ ] **Step 4: 实现审批详情**

左右对照提交快照与实时值；展示上级限制、批准后是否解除、等级与模型组、审批池和流水。若当前已用高于目标、申请过期、重复申请、超过最大目标或 precheck stale，给出错误码对应文案和下一步动作。批准意见选填；驳回/上报必填。

- [ ] **Step 5: 重构列表和移动端**

桌面使用列表 + 单详情抽屉；移动端 `<720px` 使用 `ExpansionMobileListItem`，纵向展示对象、维度、周期、目标、优先级、状态、更新时间，整行点击详情，主要动作不依赖横向滚动。页面标题按角色：

```ts
const titleByRole = {
  admin: '平台扩容审批',
  studio_admin: '工作室扩容审批',
  dept_admin: '部门额度审批',
  user: '我的扩容申请',
} as const
```

页面统一包含正常、空、加载失败、陈旧、提交后变化状态和数据更新时间。

- [ ] **Step 6: 运行扩容 TypeScript 与构建验证**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add web/src/views/admin/ExpansionRequestPage.vue web/src/components/expansion
git commit -m "feat(web): rebuild expansion approval experience"
```

### Task 15: 完成“我的预算”混合来源、等级和临时额度交互

**Files:**
- Modify: `web/src/views/MyBudgetPage.vue`

- [ ] **Step 1: 用 P0 DTO 收紧页面状态，形成失败的 TypeScript 检查**

```ts
const budget = ref<MyBudgetTiersResponse | null>(null)

async function loadBudget() {
  budget.value = await controlPlane.getMyBudgetTiers()
}

const myBudgetMock: MyBudgetTiersResponse = {
  source_summary: {
    mode: 'mixed',
    primary_scope_type: 'member',
    has_shared_pool: true,
    has_temporary_override: true,
  },
  effective_total: [{
    period_type: 'monthly',
    configured: true,
    base_limit_usd: '300.000000',
    temporary_target_usd: '350.000000',
    effective_limit_usd: '350.000000',
    used_usd: '20.000000',
    remaining_usd: '330.000000',
    reset_at: '2026-08-01T00:00:00+08:00',
  }],
  budget_chain: [
    { scope_type: 'member', label: '我的额度', visible_amounts: true },
    { scope_type: 'dept_daily', label: '部门成员日常预算', visible_amounts: false },
  ],
  tier_limits: [{
    model_tier_id: '2f63d65a-a0f7-4abe-b9d0-3ace39a44f30',
    model_tier_name: '高级',
    periods: [{
      period_type: 'monthly',
      configured: true,
      base_limit_usd: '80.000000',
      temporary_target_usd: '100.000000',
      effective_limit_usd: '100.000000',
      used_usd: '20.000000',
      remaining_usd: '80.000000',
      reset_at: '2026-08-01T00:00:00+08:00',
      status: 'available',
      limiting_source: { scope_type: 'member', remaining_usd: '80.000000' },
    }],
  }],
  data_updated_at: '2026-07-11T08:00:00Z',
  stale: false,
}
```

本地 mock、手工 E2E fixture 与接口类型共用上述顶层字段，不保留旧 shape 适配层。

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，页面仍按旧响应结构读取字段，不能赋给 P0 `MyBudgetTiersResponse`。

- [ ] **Step 3: 改为有效预算摘要**

页面只从 P0 响应的 `source_summary`、`effective_total`、`budget_chain`、`tier_limits`、`data_updated_at`、`stale` 读取数据；等级临时目标只读 `temporary_target_usd`。顶部从 `effective_total.find(period_type === 'monthly')` 展示有效可用余额、本月已用、有效月额度，并从 `source_summary.mode` 展示来源。来源枚举文案：

- `personal`：个人独立额度。
- `shared`：部门共享预算池。
- `mixed`：混合预算来源。

共享池说明“先到先得”；无个人独立总预算时，等级扩容按钮禁用并提示先申请个人总预算。

- [ ] **Step 4: 展示等级日/周/月与临时覆盖**

每个等级支持周期切换，展示上限、已用、剩余、重置、限制来源。未配置显示“仅受总预算控制”，不展示“无限”；`$0` 显示“不可使用”；等级停用显示“暂停等级限制与计数，历史仍在报表”。临时额度区：

```vue
<time :datetime="budget.data_updated_at">
  数据更新于 {{ formatDateTime(budget.data_updated_at) }}
</time>
<section v-for="tier in budget.tier_limits" :key="tier.model_tier_id">
  <dl v-for="period in tier.periods" :key="period.period_type">
    <template v-if="period.temporary_target_usd">
      <div><dt>长期基础额度</dt><dd>{{ money(period.base_limit_usd) }}</dd></div>
      <div><dt>临时目标额度</dt><dd>{{ money(period.temporary_target_usd) }}</dd></div>
      <div><dt>当前有效额度</dt><dd>{{ money(period.effective_limit_usd) }}</dd></div>
      <p>到期后恢复届时有效的长期基础额度。</p>
    </template>
  </dl>
</section>
```

模板不读取 P0 DTO 未定义的字段；预算链展示状态只依据 `budget_chain[].visible_amounts` 做脱敏分支。

- [ ] **Step 5: 接入扩容入口和异常状态**

总预算卡和每个等级都可打开 `ExpansionCreateForm`；402 引导 query 自动选择真实阻断维度。父级瓶颈时显示“个人扩容无法解除当前限制”并提供通知负责人。加载失败显示重试；陈旧显示最后成功值和警告；无任何等级配置显示“当前仅受总预算控制”。

- [ ] **Step 6: 运行 TypeScript 与 Vite 构建验证**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: PASS；390px、768px、1440px 可用性由 Task 17 Markdown 手工 E2E 验收。

- [ ] **Step 7: 提交**

```bash
git add web/src/views/MyBudgetPage.vue
git commit -m "feat(web): show effective tier budgets"
```

### Task 16: 补齐结构化错误映射并完成前端构建验证

**Files:**
- Modify: `web/src/services/http.ts`
- Modify: `web/src/views/admin/ExpansionRequestPage.vue`
- Modify: `web/src/views/MyBudgetPage.vue`

- [ ] **Step 1: 先在页面消费缺失错误工具，形成失败的 TypeScript 检查**

```ts
import {
  asAPIProblem,
  budgetErrorMessage,
  type GatewayBudgetErrorCode,
} from '@/services/http'
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd web && npx vue-tsc --noEmit`

Expected: FAIL，错误工具尚未导出。

- [ ] **Step 3: 实现结构化 API 错误**

```ts
export interface APIProblem {
  code: string
  message: string
  next_action?: 'reprecheck' | 'raise_target' | 'retry' | 'notify_owner'
  field_errors?: Record<string, string>
}

export function asAPIProblem(error: unknown): APIProblem {
  const response = (error as AxiosError<APIProblem>)?.response
  return response?.data ?? {
    code: 'NETWORK_ERROR',
    message: '网络请求失败，请检查连接后重试',
    next_action: 'retry',
  }
}
```

- [ ] **Step 4: 在页面处理管理错误与 Gateway 402**

管理 API 使用 `EXPANSION_PRECHECK_STALE`、`EXPANSION_USED_EXCEEDS_TARGET`、`EXPANSION_REQUEST_EXPIRED`、`DUPLICATE_PENDING_EXPANSION`、`BUDGET_USAGE_UNAVAILABLE`、`BUDGET_CONFIG_SYNC_PENDING`；Gateway 402 的联合类型只允许小写 `budget_exceeded | tier_budget_exceeded`。页面按 `next_action` 显示重新预检、提高目标、重试或通知负责人，不通过扫描源码字符串验证。

- [ ] **Step 5: 运行正式前端自动门禁**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: TypeScript 无错误，Vite build 成功；正式前端自动验证仅包含这两条命令。

- [ ] **Step 6: 提交**

```bash
git add web/src/services/http.ts web/src/views/admin/ExpansionRequestPage.vue web/src/views/MyBudgetPage.vue
git commit -m "feat(web): map budget workflow errors"
```

### Task 17: 编写 Markdown 手工 E2E、契约回归与最终验证

**Files:**
- Create: `specs/model-tier-budget/test/01-model-tier-and-budget-management.md`
- Create: `specs/model-tier-budget/test/02-expansion-approval.md`
- Create: `specs/model-tier-budget/test/03-risk-and-my-budget.md`
- Create: `specs/model-tier-budget/test/04-mobile-and-stale-states.md`
- Modify: `tests/contract/contracts.json`

- [ ] **Step 1: 编写统一手工用例模板与核心路径**

```markdown
# TC-MTB-01 平台管理员创建等级并配置 `$0`

## 前置条件
- 使用 `admin` 账号登录；`claude-opus` 模型组存在且未分级。
- P0/P1/P2 服务健康，页面显示的数据不是陈旧状态。

## 步骤
1. 打开 `/admin/model-tiers`，新建“高级”，颜色填 `#7C3AED`。
2. 批量关联 `claude-opus`，填写原因“纳入高级成本控制”并确认。
3. 打开 `/admin/budget?tab=overview&scope_type=company&scope_id=GLOBAL`。
4. 在公司配置抽屉把“高级/月”设为 `0.000000`，原因填“验证静态禁用”。
5. 确认影响人数、模型组和流式请求说明，勾选 `$0` 阻断确认后保存。
6. 用已授权 Key 请求该模型组。

## 预期结果
- 模型等级页显示“高级”；模型组不再显示“未分级”。
- 预算页显示“$0：禁止该作用域使用此等级”。
- Gateway 返回 HTTP 402，公开 `error.code` 精确为 `tier_budget_exceeded`。
- 审计包含 before/after、reason、actor、allocation 和 model tier。
```

`specs/model-tier-budget/test/02-expansion-approval.md`：

```markdown
# TC-MTB-02 陈旧扩容申请重新预检并幂等批准

## 前置条件
- 成员月度高级等级长期额度为 `80.000000`，已用 `60.000000`。
- 成员提交长期目标 `120.000000` 后，管理员把基础额度改为 `90.000000`。

## 步骤
1. 部门管理员打开 `/admin/expansion-requests` 的该申请详情。
2. 验证批准、驳回、上报均冻结，点击“重新预检”。
3. 核对实时额度 `90.000000`、实时已用、上级限制与批准后效果，勾选“基于最新数据确认”。
4. 批准一次后刷新页面，再重复发送同一 request ID 的批准请求。
5. 另建 `current_period` 申请并批准，跨过当前月重置边界后刷新“我的预算”。

## 预期结果
- 重新预检前状态为 `precheck_required`，接口冲突码为 `EXPANSION_PRECHECK_STALE`。
- 第一次批准后状态为 `approved`、`sync_status=synced|failed`；重复批准不新增额度记录或批准流水。
- 长期申请更新 `budget_tier_limits`；当前周期申请只新增一条 active `budget_temporary_overrides`。
- 临时额度到期后恢复届时长期基础额度，并发送到期前/恢复后通知。
```

`specs/model-tier-budget/test/03-risk-and-my-budget.md`：

```markdown
# TC-MTB-03 风险自然消失与我的预算混合来源

## 前置条件
- 用户总预算来自个人额度，高级等级月额度真正受部门共享池限制。
- 制造高级等级使用率 81%、100% 和一次实际 402 阻断。

## 步骤
1. 部门管理员打开预算管理“风险事件”，分别筛选 `threshold_reached`、`budget_exhausted`、`request_blocked`。
2. 从风险行点击“查看预算”，确认切回总览并定位原 scope；再打开处理抽屉通知负责人。
3. 扩容使使用率降到 79%，刷新风险列表。
4. 普通用户打开“我的预算”，切换高级等级日/周/月，查看总预算来源、限制来源和临时额度。

## 预期结果
- 风险列表展示真正限制来源、`data_as_of` 和阻断次数，不提供忽略/关闭动作。
- 使用率降到阈值以下后对应风险自然消失。
- 页面顶部显示“混合预算来源”；上级限制只显示层级和状态，不显示 scope ID、主体名称或精确金额。
- 未配置周期显示“仅受总预算控制”，`$0` 显示“不可使用”，停用显示暂停计数且历史仍在报表。
```

`specs/model-tier-budget/test/04-mobile-and-stale-states.md`：

```markdown
# TC-MTB-04 移动端、异常状态与历史导航

## 前置条件
- 准备 admin、studio_admin、dept_admin、user 四个账号。
- 浏览器可切换 390px、768px、1440px；可临时阻断 Redis 用量读取。

## 步骤
1. 四角色分别打开预算管理或我的预算，记录根节点、面包屑、筛选器和操作按钮。
2. admin 从公司依次进入工作室、部门、成员，使用浏览器后退和前进恢复每层 URL query。
3. 在 390px 打开扩容列表并完成查看详情、重新预检、批准/驳回；在 768px 和 1440px 重复主路径。
4. 分别制造空列表、Control Plane 请求失败、Redis last-known 陈旧数据。

## 预期结果
- studio_admin 不见公司层，dept_admin 不见工作室列表，user 不见管理菜单；服务端越权请求仍为 403。
- 后退/前进恢复 `scope_type`、`scope_id`、父级 query 和当前页签。
- 390px 使用纵向摘要且主要动作无需横向滚动。
- 空状态给出业务引导；失败状态可重试；陈旧状态显示最后更新时间并冻结批准和高风险调额。
```

- [ ] **Step 2: 运行后端测试**

Run: `go test ./tests/controlplane/... -count=1`

Expected: PASS。

- [ ] **Step 3: 运行全仓 Go 构建**

Run: `go build ./...`

Expected: exit 0。

- [ ] **Step 4: 运行前端正式自动验证**

Run: `cd web && npx vue-tsc --noEmit && npx vite build`

Expected: exit 0。

- [ ] **Step 5: 启动本地依赖并逐条执行 Markdown 手工 E2E**

Run: `make dev-all`

Expected: gateway `:8080`、control-plane `:8081`、Vite `:5173` 健康。

按 `specs/model-tier-budget/test/01-*.md` 到 `04-*.md` 顺序执行，每一步记录操作者、时间、实际结果和证据链接；任一步失败就在对应用例的“实际结果”中记录可复现输入与响应，不得把未执行项标为 PASS。

- [ ] **Step 6: 手工验收四角色和异常路径**

逐项确认：

- admin：公司根节点、模型等级 CRUD/排序/启停/归档/删除、工作室下钻、直接调额、平台审批。
- studio_admin：所属工作室根节点，无公司金额/面包屑，可配置部门，可审批部门池申请。
- dept_admin：所属部门根节点，可配置日常池/服务池/成员，可审批成员申请。
- user：无管理菜单；我的预算显示共享/个人/混合来源、等级三周期、临时额度；上级瓶颈只能通知负责人。
- 风险：80%、100%、402 三类来源；扩容后降阈值自然消失；Redis 故障显示陈旧且冻结高风险动作。
- 扩容：同限制项永久与临时可各有一张待审；同类型重复被拒绝；提高目标重启审批；降低目标保留已完成层级；紧急不跳审。
- 移动端：390px 下审批列表为纵向摘要、整行可点、批准/驳回/reprecheck 无横向滚动依赖。
- 文案：未配置、`$0`、等级停用完全不同；数据更新时间在我的预算、风险事件、扩容申请均可见。
- 错误码：Gateway 402 只出现 `budget_exceeded|tier_budget_exceeded`；Control Plane 管理错误保持技术设计中的大写枚举。

- [ ] **Step 7: 用 Go handler 测试校验新增路由并保护既有扩容路由**

Run: `go test ./tests/controlplane/handler -run 'ExpansionRoutes|BudgetRiskRoutes' -count=1`

Expected: PASS；新增 precheck/reprecheck/decision/notify-owner/risk 路由存在，既有 `GET /expansion-requests`、`GET /expansion-requests/pending`、`GET /expansion-requests/:id/steps` 仍可访问。测试只验证必需路由存在，不对整个 router 或 contract route 集合做全等断言。

- [ ] **Step 8: 最终自检**

确认：

- 没有把“扩容申请”改名或并入预算管理。
- 预算管理只有两个页签，没有“变更记录”页签。
- 没有用前端隐藏代替服务端范围校验。
- 旧 `current_amount/requested_amount` 仍为 TypeScript/JSON number；新增 snapshot/target/limit/usage 字段才是 6 位 Decimal 字符串，额度判断不使用旧 number。
- 批准事务锁定申请、重新校验、写额度/override、写流水和 applied 标记后才提交。
- 重复批准不重复应用；Redis 发布失败不回滚数据库。
- 风险事件没有人工关闭状态。
- 普通用户响应不包含上级精确金额或主体 ID。
- 临时额度到期恢复届时基础值，页面只展示当前周期的临时额度。
- 到期扫描、`FOR UPDATE SKIP LOCKED`、状态迁移和快照发布仅由 P0 唯一 `BudgetLifecycleExecutor` 负责；P2 observer 用 `OnOverrideExpired/OnScheduleApplied/OnScheduleFailed` 分别发送恢复、生效和失败通知，重复回调不重复投递。
- `request_blocked` 风险只查询 P1 `call_logs` 的 `budget_block_code|budget_block_dimension|budget_block_scope_type|budget_block_scope_id|budget_block_period_type` 五个阻断字段，tier 事件另读 `budget_tier_id`，不存在内存 blocks 来源。
- total/tier 的 next-period 计划都由 P0 唯一 `BudgetLifecycleExecutor` 执行，P2 不含第二套 executor 或 schedule 扫描。
- 直接调额包含原因、生效模式、影响确认和审计。
- 所有新增页面均有正常、空、失败、陈旧和移动端状态。
- P0 拥有 snapshot publisher 及 model tier/tier limit/batch/tier usage/my budget 基础 API；P1 只消费快照与 verify-key；P2 没有重复实现。
- 表名只使用 `model_tiers`、`budget_tier_limits`、`budget_temporary_overrides`、`budget_limit_schedules`、`budget_alert_records`、`expansion_requests`、`call_logs`。
- 审批状态只使用 `pending|precheck_required|approved|rejected|cancelled|expired`；schedule、override、sync 状态与技术设计枚举完全一致。
- 开关只使用 `master_enabled|accounting_enabled|positive_enforcement_enabled|zero_block_enabled|shadow_enabled|v2_total_enforcement_enabled|legacy_total_write_enabled`。
- 前端正式门禁只有 `vue-tsc` 和 Vite build；手工 E2E 只位于 `specs/model-tier-budget/test/*.md`。

- [ ] **Step 9: 提交**

```bash
git add specs/model-tier-budget/test tests/contract/contracts.json
git commit -m "test(budget): add model tier budget e2e coverage"
```

---

## 完成定义

- 17 个任务全部完成并逐项打勾。
- `go test ./tests/controlplane/... -count=1`、`go build ./...`、`cd web && npx vue-tsc --noEmit && npx vite build` 全部通过，四份 Markdown 手工 E2E 均记录实际结果。
- 四角色的服务端对象裁剪、前端动作隐藏、URL 同页下钻和移动端摘要均完成手工验收。
- contracts.json 覆盖模型等级、等级预算、风险、扩容、我的预算和直接调额的全部新增接口及错误码。
- 审批批准可在并发重试、同步失败和陈旧预检下保持数据库额度只生效一次。
- P0 executor 在多实例下幂等完成 lifecycle 状态与快照；P2 observer 按记录 ID、结果类型与收件人组成幂等键，override 恢复、schedule 生效和失败通知各投递一次。

计划共 **17 个任务**。
