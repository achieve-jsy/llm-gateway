# 模型等级子预算 P0：数据模型、Atlas、领域契约与 Control Plane 基础能力实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 涉及数据库结构的任务还必须先使用仓库的 `atlas-db-migration` skill。

**Goal:** 在不改变现有总预算与扩容 JSON 兼容契约的前提下，交付模型等级数据基础、完整 Control Plane 管理/批量/用量 API、版本化等级快照发布与对账能力，为 P1 Gateway Redis v2 consumer 提供可直接集成的稳定生产者。

**Architecture:** GORM model 是 schema 唯一事实源，全部新结构只通过 `atlas-migrations/` 的 Atlas diff 生成，遗留 `migrations/` 不得新增文件。Control Plane 按 model → repository（只返回 model/repository DTO）→ service → handler/router 分层；新 API 金额使用 6 位 Decimal 字符串，既有 expansion 金额继续使用 JSON number。`BudgetTierSnapshotPublisher` 从数据库构建完整版本化快照，通过现有 ChangeEvent JSON 发布并由 30 秒 reconcile 修复；`BudgetLifecycleExecutor` 以逐记录独立事务统一执行 schedule 和 override 生命周期。

**Tech Stack:** Go 1.26.1、Gin 1.12、GORM 1.31、PostgreSQL 16、Redis 9、Atlas + GORM Provider、`github.com/shopspring/decimal`、Testify、miniredis、SQLite 内存 repository/handler 测试、现有 RBAC、AuditMiddleware、`config:change`。

**Specs:**
- `docs/superpowers/specs/2026-07-10-model-tier-budget-design.md`
- `docs/superpowers/specs/2026-07-11-model-tier-budget-technical-design.md`

**Worktree:** `/Users/jsy/Public/self_code/llm-gateway/.claude/worktrees/model-tier-quota`

---

## 阶段边界与不变量

- P0 交付模型等级 CRUD、排序、启停、归档/安全删除、模型组单个/批量分级、等级额度查询与立即/下周期配置、批量 preview/apply、组织 tier usage、`my budget tiers`、完整快照发布和 reconcile。
- P0 同时扩展 `KeyService.buildBudgetChain`：verify-key 原有总预算字段名、Go `float64` 和 JSON number 保持不变，仅新增 Decimal 字符串 `tier_limits`；总预算和等级预算都合并 active temporary override 后下发 effective limit。
- P0 用单一 `BudgetLifecycleExecutor` 执行 total/tier schedule 与 temporary override 到期；P0 负责逐记录事务、状态、KeyCache 失效和快照发布，P2 依赖统一 observer 接入站内信/邮件通知。
- P0 扩展 `budget_alert_records`、`expansion_requests`；`call_logs` 只交付 schema/DTO/contracts。完整审批状态机、风险事件 API、Gateway writer/consumer、Redis v2 计数/拦截、影子记账和前端页面属于后续计划。
- 现有 `/api/v1/budgets`、总预算 Redis key、扩容总预算请求必须保持兼容。
- `group_models.complexity_tier` 不得改名或复用；预算分类只使用 `model_groups.budget_tier_id`。
- 新增 DTO 的金额字段都是固定 6 位小数字符串，例如 `"100.000000"`；既有 `expansion_requests.current_amount` / `requested_amount` 的 Go 字段和 JSON number 契约保持不变，只把数据库列无损扩大到 `numeric(18,6)`。
- 周期计算统一使用 `internal/budgetperiod`，配置固定为 `budget.timezone=Asia/Shanghai`、`week_start=monday`；禁止依赖容器本地时区。
- 快照只使用 canonical flags：`master_enabled`、`accounting_enabled`、`positive_enforcement_enabled`、`zero_block_enabled`、`shadow_enabled`、`v2_total_enforcement_enabled`、`legacy_total_write_enabled`。
- Gateway 公开 402 错误码契约固定为小写 `budget_exceeded` 与 `tier_budget_exceeded`；Control Plane 管理 API 继续使用大写业务错误码。
- `NULL` 等级表示未分级；未分级模型组仅受总预算约束。
- 模型等级被模型组、等级额度、临时覆盖、计划变更、告警、扩容申请或调用日志引用时禁止物理删除；有历史但无活跃配置时使用 `archived_at` 归档。
- 新增 DDL 只允许进入 `atlas-migrations/`；不得调用 `AutoMigrate`，不得修改遗留 `migrations/`，不得手工修改 `atlas.sum`。

## 文件地图

### 新建

- `internal/controlplane/budgetdomain/types.go`：金额、周期、维度、持续时间、计划/覆盖状态的类型与校验。
- `internal/budgetperiod/period.go`：共享时区、ISO 周一周期键与 reset boundary 计算。
- `internal/configsync/budget_tier_snapshot.go`：快照 schema、canonical flags、Redis key 和原子 load/save。
- `internal/controlplane/model/model_tier.go`：`ModelTier`。
- `internal/controlplane/model/budget_tier.go`：`BudgetTierLimit`、`BudgetTemporaryOverride`、`BudgetLimitSchedule`。
- `internal/controlplane/repository/model_tier_repo.go`：等级 CRUD、引用统计、排序事务、模型组绑定。
- `internal/controlplane/repository/budget_tier_repo.go`：等级额度、临时覆盖、计划变更持久化。
- `internal/controlplane/service/model_tier_service.go`：等级业务规则。
- `internal/controlplane/service/budget_tier_service.go`：等级额度与计划生效规则。
- `internal/controlplane/service/budget_tier_snapshot_publisher.go`：完整快照、revision、事件、KeyCache 失效和 reconcile。
- `internal/controlplane/service/budget_lifecycle_executor.go`：统一执行 total/tier schedule 与 temporary override 到期，发布并暴露 P2 observer。
- `internal/controlplane/handler/model_tier_handler.go`：模型等级与组绑定 HTTP API。
- `internal/controlplane/handler/budget_tier_handler.go`：等级额度 HTTP API 与作用域裁剪。
- `tests/controlplane/budgetdomain/types_test.go`
- `tests/budgetperiod/period_test.go`
- `tests/configsync/budget_tier_snapshot_test.go`
- `tests/controlplane/repository/model_tier_repo_test.go`
- `tests/controlplane/repository/budget_tier_repo_test.go`
- `tests/controlplane/service/model_tier_service_test.go`
- `tests/controlplane/service/budget_tier_service_test.go`
- `tests/controlplane/service/budget_tier_snapshot_publisher_test.go`
- `tests/controlplane/service/budget_lifecycle_executor_test.go`
- `tests/controlplane/handler/model_tier_handler_test.go`
- `tests/controlplane/handler/budget_tier_handler_test.go`
- `tests/controlplane/handler/budget_tier_batch_handler_test.go`
- `tests/controlplane/handler/budget_tier_usage_handler_test.go`
- `tests/controlplane/middleware/audit_test.go`
- `tests/controlplane/repository/budget_alert_repo_dimension_test.go`
- `tests/controlplane/repository/expansion_repo_dimension_test.go`
- `atlas-migrations/20260711160000_add_model_tier_budget_foundation.sql`：计划中的预期文件名；实际执行时必须由 `atlas migrate diff add_model_tier_budget_foundation --env local` 生成。若 Atlas 生成不同的 14 位时间戳，以 Atlas 原始文件名为准，禁止手工改名。

### 修改

- `go.mod`、`go.sum`：加入 decimal 依赖。
- `internal/controlplane/model/group.go`：增加可空 `BudgetTierID`。
- `internal/controlplane/model/budget_alert.go`：增加预算维度与确定性维度键。
- `internal/controlplane/model/expansion_request.go`：增加等级/持续时间/快照/预检/生效/同步字段和状态。
- `internal/observability/model/call_log.go`：增加请求开始时等级快照、`budget_tier_accounting_eligible`、实际每层 scope constraint 的 JSONB 快照与预算 402 首要阻断字段。
- `internal/controlplane/service/key_service.go`：`buildBudgetChain` 合并 total/tier active override，保持既有总预算 number 契约。
- `internal/controlplane/event/change_event.go`：增加 `budget_tier_snapshot_updated`，沿用现有 ChangeEvent JSON。
- `tools/atlas-loader/main.go`：注册三个新 GORM model，去除重复 `GroupEntitlement` 注册。
- `internal/controlplane/repository/group_repo.go`：读取等级、单个/批量绑定、完整快照查询。
- `internal/controlplane/repository/call_log_repo.go`：按 scope/tier/period 聚合符合 accounting eligibility 的用量。
- `internal/controlplane/repository/budget_alert_repo.go`：修复遗漏 `period_type`，按维度去重。
- `internal/controlplane/repository/expansion_repo.go`：等级维度待审批唯一查询与锁定读取契约。
- `tests/controlplane/service/key_service_test.go`：verify-key 总预算兼容、tier limits 与 override effective limit。
- `internal/controlplane/service/group_service.go`：模型组等级关联入口。
- `internal/controlplane/service/budget_service.go`：暴露按 allocation ID 查询及作用域辅助方法。
- `internal/controlplane/service/alert_expansion_service.go`：现有总预算告警显式使用 `total` 维度；旧扩容入口填充兼容默认值。
- `internal/controlplane/handler/group_handler.go`：列表/详情返回等级摘要。
- `internal/controlplane/handler/budget_handler.go`：保留旧接口，复用新的 allocation 查询。
- `internal/controlplane/handler/expansion_handler.go`：保留既有 JSON number，新增快照 Decimal 字符串 DTO。
- `internal/controlplane/handler/errcodes.go`：新增稳定错误码。
- `internal/controlplane/router/router.go`：依赖注入与路由。
- `internal/controlplane/audit/types.go`、`internal/controlplane/middleware/audit.go`：等级操作审计；新增测试只写入 `tests/controlplane/middleware/audit_test.go`。
- `internal/orgservice/rbac/permission.go`、`internal/orgservice/rbac/registry.go`：`model_tier:view/manage`。
- `tests/controlplane/service/budget_service_test.go`：fake 接口随 repository 契约调整。
- `tests/controlplane/service/budget_alert_service_test.go`：维度化告警调用。
- `tests/controlplane/repository/expansion_repo_test.go`：新字段和新签名。
- `tests/contract/contracts.json`：模型等级、组分级、等级额度、扩容维度和调用日志字段契约。

---

### Task 1: 建立 6 位小数金额与预算枚举领域契约

**Files:**
- Create: `internal/controlplane/budgetdomain/types.go`
- Create: `tests/controlplane/budgetdomain/types_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 写失败测试**

```go
package budgetdomain_test

import (
	"encoding/json"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/stretchr/testify/require"
)

func TestAmountJSONUsesSixDecimalString(t *testing.T) {
	var a budgetdomain.Amount
	require.NoError(t, json.Unmarshal([]byte(`"12.3456784"`), &a))
	require.Equal(t, "12.345678", a.String())
	b, err := json.Marshal(a)
	require.NoError(t, err)
	require.JSONEq(t, `"12.345678"`, string(b))
}

func TestAmountRejectsNumberAndNegativeValue(t *testing.T) {
	var a budgetdomain.Amount
	require.ErrorIs(t, json.Unmarshal([]byte(`12.5`), &a), budgetdomain.ErrAmountMustBeString)
	require.ErrorIs(t, json.Unmarshal([]byte(`"-0.000001"`), &a), budgetdomain.ErrAmountNegative)
}

func TestDimensionValidation(t *testing.T) {
	require.NoError(t, budgetdomain.ValidateDimension(budgetdomain.DimensionTotal, nil))
	require.ErrorIs(t, budgetdomain.ValidateDimension(budgetdomain.DimensionTier, nil), budgetdomain.ErrTierIDRequired)
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
cd /Users/jsy/Public/self_code/llm-gateway/.claude/worktrees/model-tier-quota
go test ./tests/controlplane/budgetdomain -count=1
```

Expected: FAIL，编译器报告 `internal/controlplane/budgetdomain` 包不存在。

- [ ] **Step 3: 添加依赖并写最小实现**

Run:

```bash
go get github.com/shopspring/decimal
```

核心实现：

```go
package budgetdomain

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrAmountMustBeString = errors.New("amount must be a decimal string")
	ErrAmountNegative     = errors.New("amount must be greater than or equal to zero")
	ErrTierIDRequired     = errors.New("model_tier_id is required for tier dimension")
	ErrTierIDForbidden    = errors.New("model_tier_id must be null for total dimension")
)

type Amount struct{ decimal.Decimal }

func ParseAmount(s string) (Amount, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Amount{}, ErrAmountMustBeString
	}
	if d.IsNegative() {
		return Amount{}, ErrAmountNegative
	}
	d = d.Round(6)
	return Amount{Decimal: d}, nil
}

func (a Amount) String() string { return a.Decimal.StringFixed(6) }
func (a Amount) Value() (driver.Value, error) { return a.String(), nil }
func (a *Amount) Scan(value any) error {
	var raw string
	switch v := value.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		raw = fmt.Sprint(v)
	}
	parsed, err := ParseAmount(raw)
	if err != nil { return err }
	*a = parsed
	return nil
}
func (a Amount) MarshalJSON() ([]byte, error) { return json.Marshal(a.String()) }
func (a *Amount) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || data[0] != '"' || bytes.Equal(data, []byte("null")) {
		return ErrAmountMustBeString
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil { return ErrAmountMustBeString }
	parsed, err := ParseAmount(raw)
	if err != nil { return err }
	*a = parsed
	return nil
}

type PeriodType string
const (
	PeriodDaily PeriodType = "daily"
	PeriodWeekly PeriodType = "weekly"
	PeriodMonthly PeriodType = "monthly"
)

type DimensionType string
const (
	DimensionTotal DimensionType = "total"
	DimensionTier DimensionType = "tier"
)

func ValidateDimension(d DimensionType, tierID *uuid.UUID) error {
	switch d {
	case DimensionTotal:
		if tierID != nil { return ErrTierIDForbidden }
	case DimensionTier:
		if tierID == nil || *tierID == uuid.Nil { return ErrTierIDRequired }
	default:
		return fmt.Errorf("invalid dimension_type %q", d)
	}
	return nil
}

func DimensionKey(d DimensionType, tierID *uuid.UUID) (string, error) {
	if err := ValidateDimension(d, tierID); err != nil { return "", err }
	if d == DimensionTotal { return "total", nil }
	return "tier:" + tierID.String(), nil
}
```

同时完整定义 `DurationType`（`permanent|current_period`）、`OverrideStatus`（`active|expired|cancelled|failed`）、`ScheduleStatus`（`pending|applied|replaced|cancelled|failed`）及各自穷举式 `Valid()`；未知字符串必须返回 false。`failed` 供统一生命周期执行器在独立补偿事务记录到期失败。测试额外调用 `Scan("-1.000000")` 并断言 `ErrAmountNegative`，保证数据库扫描和 JSON 输入都拒绝负数。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/budgetdomain/types.go tests/controlplane/budgetdomain/types_test.go
go test ./tests/controlplane/budgetdomain -count=1
```

Expected: PASS，所有金额均以 6 位小数字符串序列化，JSON number 和负数被拒绝。

- [ ] **Step 5: 提交**

```bash
git add go.mod go.sum internal/controlplane/budgetdomain/types.go tests/controlplane/budgetdomain/types_test.go
git commit -m "feat(budget): 建立等级预算金额与枚举契约"
```

---

### Task 2: 固定共享周期、快照 flags 与错误码契约

**Files:**
- Create: `internal/budgetperiod/period.go`
- Create: `internal/configsync/budget_tier_snapshot.go`
- Create: `tests/budgetperiod/period_test.go`
- Create: `tests/configsync/budget_tier_snapshot_test.go`
- Modify: `tests/contract/contracts.json`

- [ ] **Step 1: 写失败测试**

```go
package budgetperiod_test

func TestWeeklyPeriodUsesShanghaiISOWeek(t *testing.T) {
	cfg := budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"}
	at := time.Date(2026, 7, 12, 16, 30, 0, 0, time.UTC) // 上海周一 00:30
	p, err := budgetperiod.Resolve(at, budgetdomain.PeriodWeekly, cfg)
	require.NoError(t, err)
	require.Equal(t, "2026-W29", p.Key)
	require.Equal(t, time.Date(2026, 7, 20, 0, 0, 0, 0, mustShanghai()), p.ResetAt)
}

func TestBudgetTierSnapshotUsesCanonicalFlags(t *testing.T) {
	b, err := json.Marshal(configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 42,
		MasterEnabled: true, AccountingEnabled: true, ShadowEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
	  "schema_version":1,"revision":42,"updated_at":"0001-01-01T00:00:00Z",
	  "master_enabled":true,"accounting_enabled":true,
	  "positive_enforcement_enabled":false,"zero_block_enabled":false,
	  "shadow_enabled":true,"v2_total_enforcement_enabled":false,
	  "legacy_total_write_enabled":false,"timezone":"Asia/Shanghai",
	  "week_start":"monday","group_tiers":{}
	}`, string(b))
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/budgetperiod ./tests/configsync -count=1
```

Expected: FAIL，`internal/budgetperiod` 与 `BudgetTierSnapshot` 尚不存在。

- [ ] **Step 3: 写最小实现**

```go
package budgetperiod

const (
	DefaultTimezone = "Asia/Shanghai"
	CanonicalWeekStart = "monday"
)

type Config struct {
	Timezone string `json:"timezone"`
	WeekStart string `json:"week_start"`
}

type Period struct {
	Key string
	StartAt time.Time
	ResetAt time.Time
}

func ValidateConfig(cfg Config) error
func Resolve(now time.Time, period budgetdomain.PeriodType, cfg Config) (Period, error)
```

`Resolve` 先 `time.LoadLocation(cfg.Timezone)`，只接受 `week_start=monday`；daily、weekly、monthly 的 key 分别为 `YYYY-MM-DD`、ISO `YYYY-Www`、`YYYY-MM`。`ResetAt` 返回对应时区下一周期边界，不转成容器 local。该公开 API 是跨 P0/P1/P2 的交付契约，必须维持 `Resolve(now, periodType, config) -> (Period, error)`；不得拆成按日/周/月三个入口，也不得把 timezone 隐藏为进程全局变量。

快照契约：

```go
const BudgetTierSnapshotKey = "budget:v2:tier:snapshot"

type BudgetTierSnapshot struct {
	SchemaVersion int `json:"schema_version"`
	Revision int64 `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
	MasterEnabled bool `json:"master_enabled"`
	AccountingEnabled bool `json:"accounting_enabled"`
	PositiveEnforcementEnabled bool `json:"positive_enforcement_enabled"`
	ZeroBlockEnabled bool `json:"zero_block_enabled"`
	ShadowEnabled bool `json:"shadow_enabled"`
	V2TotalEnforcementEnabled bool `json:"v2_total_enforcement_enabled"`
	LegacyTotalWriteEnabled bool `json:"legacy_total_write_enabled"`
	Timezone string `json:"timezone"`
	WeekStart string `json:"week_start"`
	GroupTiers map[string]BudgetTierGroupSnapshot `json:"group_tiers"`
}

type BudgetTierGroupSnapshot struct {
	TierID uuid.UUID `json:"tier_id"`
	TierName string `json:"tier_name"`
	Enabled bool `json:"enabled"`
}
```

`tests/contract/contracts.json` 同时固定 Gateway 公开错误码：

```json
{
  "gateway_402_error_codes":["budget_exceeded","tier_budget_exceeded"],
  "period_resolver_signature":"Resolve(now, periodType, config) -> (Period, error)"
}
```

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/budgetperiod/period.go internal/configsync/budget_tier_snapshot.go tests/budgetperiod/period_test.go tests/configsync/budget_tier_snapshot_test.go
go test ./tests/budgetperiod ./tests/configsync -count=1
```

Expected: PASS；上海跨日、跨 ISO 周、跨月边界均稳定，快照 JSON 不出现旧名称 `positive_enforcement_enabled` 之外的别名。

- [ ] **Step 5: 提交**

```bash
git add internal/budgetperiod/period.go internal/configsync/budget_tier_snapshot.go tests/budgetperiod/period_test.go tests/configsync/budget_tier_snapshot_test.go tests/contract/contracts.json
git commit -m "feat(budget): 固定周期与等级快照契约"
```

---

### Task 3: 声明模型等级、额度、覆盖、计划及历史扩展 GORM model

**Files:**
- Create: `internal/controlplane/model/model_tier.go`
- Create: `internal/controlplane/model/budget_tier.go`
- Modify: `internal/controlplane/model/group.go`
- Modify: `internal/controlplane/model/budget_alert.go`
- Modify: `internal/controlplane/model/expansion_request.go`
- Modify: `internal/observability/model/call_log.go`
- Modify: `tools/atlas-loader/main.go`
- Create: `internal/controlplane/repository/model_tier_repo.go`（先实现 Create/FindByID，Task 5 扩展）
- Create: `tests/controlplane/repository/model_tier_repo_test.go`（持久化行为，Task 5 继续扩展）

- [ ] **Step 1: 写失败 repository 行为测试**

```go
func TestModelTierRepositoryCreatesAndReadsTier(t *testing.T) {
	db := openRepositoryTestDB(t)
	repo := repository.NewModelTierRepo(db)
	tier := &model.ModelTier{
		ID: uuid.New(), Name: "高级", Color: "#7C3AED", Enabled: true,
	}
	require.NoError(t, repo.Create(context.Background(), tier))
	got, err := repo.FindByID(context.Background(), tier.ID)
	require.NoError(t, err)
	require.Equal(t, "高级", got.Name)
	require.True(t, got.Enabled)
}
```

`openRepositoryTestDB` 使用仓库已有 repository 测试连接，并在 test setup 执行固定 fixture：

```sql
CREATE TABLE model_tiers (
  id text PRIMARY KEY,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  color text NOT NULL,
  sort_order integer NOT NULL DEFAULT 0,
  enabled boolean NOT NULL DEFAULT true,
  archived_at datetime NULL,
  created_at datetime NOT NULL,
  updated_at datetime NOT NULL
);
```

不得调用 `AutoMigrate`。测试只验证可观察的创建/读取行为，不读取 Go 字段元数据、不解析 GORM tag、不断言结构体类型。

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/repository -run TestModelTierRepositoryCreatesAndReadsTier -count=1
```

Expected: FAIL，`model.ModelTier` 与 `repository.NewModelTierRepo` 尚不存在。

- [ ] **Step 3: 写最小 GORM model**

`model_tier.go`：

```go
type ModelTier struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name        string     `gorm:"type:varchar(64);not null;uniqueIndex:uniq_model_tiers_active_name,where:archived_at IS NULL" json:"name"`
	Description string     `gorm:"type:text;not null;default:''" json:"description"`
	Color       string     `gorm:"type:varchar(7);not null;default:'#64748B'" json:"color"`
	SortOrder   int        `gorm:"not null;default:0;index:idx_model_tiers_sort" json:"sort_order"`
	Enabled     bool       `gorm:"not null;default:true" json:"enabled"`
	ArchivedAt  *time.Time `gorm:"index:idx_model_tiers_archived_at" json:"archived_at,omitempty"`
	CreatedAt   time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}
func (ModelTier) TableName() string { return "model_tiers" }
```

`budget_tier.go` 的核心字段：

```go
type BudgetTierLimit struct {
	ID                 uuid.UUID           `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	BudgetAllocationID uuid.UUID           `gorm:"type:uuid;not null;uniqueIndex:uniq_budget_tier_limit" json:"budget_allocation_id"`
	ModelTierID        uuid.UUID           `gorm:"type:uuid;not null;uniqueIndex:uniq_budget_tier_limit" json:"model_tier_id"`
	PeriodType         budgetdomain.PeriodType `gorm:"type:varchar(8);not null;uniqueIndex:uniq_budget_tier_limit" json:"period_type"`
	LimitUSD           budgetdomain.Amount `gorm:"type:numeric(18,6);not null" json:"limit_usd"`
	EffectiveAt        time.Time           `gorm:"not null" json:"effective_at"`
	Enabled            bool                `gorm:"not null;default:true" json:"enabled"`
	BudgetAllocation   BudgetAllocation    `gorm:"foreignKey:BudgetAllocationID;constraint:OnDelete:RESTRICT" json:"-"`
	ModelTier          ModelTier           `gorm:"foreignKey:ModelTierID;constraint:OnDelete:RESTRICT" json:"-"`
	CreatedAt          time.Time           `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time           `gorm:"not null;autoUpdateTime" json:"updated_at"`
}
```

`BudgetTemporaryOverride` 和 `BudgetLimitSchedule` 都包含 `BudgetAllocationID`、`DimensionType`、`DimensionKey`、可空 `ModelTierID`、`PeriodType`、`TargetLimitUSD numeric(18,6)`；覆盖额外包含 `StartsAt`、`ExpiresAt`、可空唯一 `ExpansionRequestID`、`Status(active|expired|cancelled|failed)`、`FailureReason`，计划额外包含 `EffectiveAt`、`Reason`、`CreatedBy`、`Status`、`FailureReason`。两者使用确定性 `dimension_key` 解决 PostgreSQL `NULL` 唯一键问题。

对现有 model 做以下精确扩展：

```go
// model.Group
BudgetTierID *uuid.UUID `gorm:"type:uuid;index:idx_model_groups_budget_tier,where:budget_tier_id IS NOT NULL AND deleted_at IS NULL" json:"budget_tier_id,omitempty"`
BudgetTier   *ModelTier `gorm:"foreignKey:BudgetTierID;constraint:OnDelete:RESTRICT" json:"budget_tier,omitempty"`

// model.BudgetAlertRecord：复合唯一索引的每个字段必须使用完全相同的
// uniqueIndex 名称与 priority，禁止混用旧 idx_budget_alert 或省略任一字段。
ScopeType     string `gorm:"type:varchar(32);not null;uniqueIndex:uniq_budget_alert_dimension,priority:1" json:"scope_type"`
ScopeID       string `gorm:"type:varchar(255);not null;uniqueIndex:uniq_budget_alert_dimension,priority:2" json:"scope_id"`
PeriodType    string `gorm:"type:varchar(16);not null;uniqueIndex:uniq_budget_alert_dimension,priority:3" json:"period_type"`
Period        string `gorm:"type:varchar(16);not null;uniqueIndex:uniq_budget_alert_dimension,priority:4" json:"period"`
Threshold     int    `gorm:"not null;uniqueIndex:uniq_budget_alert_dimension,priority:5" json:"threshold"`
DimensionKey  string `gorm:"type:varchar(48);not null;default:'total';uniqueIndex:uniq_budget_alert_dimension,priority:6" json:"dimension_key"`
DimensionType budgetdomain.DimensionType `gorm:"type:varchar(8);not null;default:'total';index" json:"dimension_type"`
ModelTierID   *uuid.UUID `gorm:"type:uuid;index" json:"model_tier_id,omitempty"`

// observability/model.CallLog
BudgetTierID           *uuid.UUID `gorm:"type:uuid;index:idx_call_logs_budget_tier_ts,composite:budget_tier_ts" json:"budget_tier_id,omitempty"`
BudgetTierNameSnapshot string     `gorm:"type:varchar(64)" json:"budget_tier_name_snapshot,omitempty"`
BudgetTierAccountingEligible bool `gorm:"not null;default:false" json:"budget_tier_accounting_eligible"`
BudgetTierConstraintsSnapshot []BudgetTierConstraintSnapshot `gorm:"type:jsonb;not null;default:'[]';serializer:json" json:"budget_tier_constraints_snapshot"`
BudgetBlockCode *string `gorm:"type:varchar(64);index" json:"budget_block_code,omitempty"`
BudgetBlockDimension *string `gorm:"type:varchar(8)" json:"budget_block_dimension,omitempty"`
BudgetBlockScopeType *string `gorm:"type:varchar(32)" json:"budget_block_scope_type,omitempty"`
BudgetBlockScopeID *string `gorm:"type:varchar(128)" json:"budget_block_scope_id,omitempty"`
BudgetBlockPeriodType *string `gorm:"type:varchar(8)" json:"budget_block_period_type,omitempty"`
```

五个 `budget_block_*` Go 字段全部是 `*string`，数据库长度依次为 `64/8/32/128/8`，严格对齐 technical design。P0 只交付数据库列、Go DTO 字段和 contracts；不引入 P1 的 TierContext、Gate 或 BlockContext，也不要求现有 UsageCollector 构造或写入这些值。P1 在 Gateway 拦截链实现实际写入。

`ExpansionRequest` 新增 `DimensionType`、`DimensionKey`、`ModelTierID`、`DurationType`、`Priority`、`CurrentAmountSnapshot`、`UsedAmountSnapshot`、`PrecheckVersion`、`PrecheckedAt`、`ExpiresAt`、`AppliedAt`、`AppliedTargetType`、`AppliedTargetID`、`SyncStatus`，并新增 `precheck_required|expired` 状态常量。`CurrentAmountSnapshot` 与 `UsedAmountSnapshot` 使用 `budgetdomain.Amount` / `numeric(18,6)` 和 JSON Decimal 字符串。既有 `CurrentAmount`、`RequestedAmount` 的 Go 类型及 `json:"current_amount"` / `json:"requested_amount"` number 序列化保持原样，只把对应数据库列 tag 扩为 `type:numeric(18,6)`。

`tools/atlas-loader/main.go` 注册：

```go
&cpmmodel.ModelTier{},
&cpmmodel.BudgetTierLimit{},
&cpmmodel.BudgetTemporaryOverride{},
&cpmmodel.BudgetLimitSchedule{},
```

并删除当前重复的第二个 `&cpmmodel.GroupEntitlement{}`。

同一步补上 `ModelTierRepo.Create/FindByID` 的最小实现，使 Step 1 的持久化行为测试通过；Task 5 在该文件上扩展完整 CRUD、引用统计与排序事务。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/model/model_tier.go internal/controlplane/model/budget_tier.go internal/controlplane/model/group.go internal/controlplane/model/budget_alert.go internal/controlplane/model/expansion_request.go internal/observability/model/call_log.go internal/controlplane/repository/model_tier_repo.go tools/atlas-loader/main.go tests/controlplane/repository/model_tier_repo_test.go
go test ./tests/controlplane/repository -run TestModelTierRepositoryCreatesAndReadsTier -count=1
(cd tools/atlas-loader && go build ./...)
```

Expected: PASS；atlas-loader 编译成功。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/model internal/observability/model/call_log.go internal/controlplane/repository/model_tier_repo.go tools/atlas-loader/main.go tests/controlplane/repository/model_tier_repo_test.go
git commit -m "feat(db): 声明模型等级预算数据结构"
```

---

### Task 4: 只使用 Atlas 生成并验证 Expand 迁移

**Files:**
- Create: `atlas-migrations/20260711160000_add_model_tier_budget_foundation.sql`（Atlas 生成时间戳可能不同，后缀必须精确为 `add_model_tier_budget_foundation.sql`）
- Modify: `atlas-migrations/atlas.sum`（只能由 Atlas 命令生成）

- [ ] **Step 1: 先验证当前迁移目录，再确认目标分支时间戳**

Run:

```bash
git fetch origin
for branch in test main; do
  latest=$(git ls-tree -r "origin/$branch" atlas-migrations/ 2>/dev/null | awk '{print $4}' | rg '\.sql$' | sort | tail -1)
  echo "$branch: $latest"
done
atlas migrate validate --env local
```

Expected:

```text
test: atlas-migrations/20260710110000_embedding_rerank_provider_definitions.sql
main: atlas-migrations/20260710110000_embedding_rerank_provider_definitions.sql
Migration directory is valid
```

以上文件名是本计划编写时 worktree 的最新迁移；若命令显示更新版本，必须以两条命令输出中的较大时间戳为基线并先 rebase 目标分支，不得在陈旧目录上生成 diff。

- [ ] **Step 2: 运行 schema diff 并确认产生待审查 SQL**

Run:

```bash
make atlas-diff name=add_model_tier_budget_foundation
```

Expected: Atlas 输出 `Migration file created`，并在 `atlas-migrations/` 新增一个 `*_add_model_tier_budget_foundation.sql`；若输出 `The migration directory is synced with the desired state, no changes to be made`，说明 Task 3 model 未被 loader 注册，必须修正 loader 后重跑。

- [ ] **Step 3: 审查生成 SQL**

生成 SQL 必须等价包含以下结构，且不得出现 `DROP TABLE`、`DROP COLUMN` 或列重命名：

```sql
CREATE TABLE "model_tiers" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "name" character varying(64) NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "color" character varying(7) NOT NULL DEFAULT '#64748B',
  "sort_order" bigint NOT NULL DEFAULT 0,
  "enabled" boolean NOT NULL DEFAULT true,
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "uniq_model_tiers_active_name"
  ON "model_tiers" ("name") WHERE "archived_at" IS NULL;

ALTER TABLE "model_groups" ADD COLUMN "budget_tier_id" uuid NULL;
ALTER TABLE "model_groups" ADD CONSTRAINT "fk_model_groups_budget_tier"
  FOREIGN KEY ("budget_tier_id") REFERENCES "model_tiers" ("id") ON DELETE RESTRICT;

CREATE TABLE "budget_tier_limits" (
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  "budget_allocation_id" uuid NOT NULL,
  "model_tier_id" uuid NOT NULL,
  "period_type" character varying(8) NOT NULL,
  "limit_usd" numeric(18,6) NOT NULL,
  "effective_at" timestamptz NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "chk_budget_tier_limits_period" CHECK (period_type IN ('daily','weekly','monthly')),
  CONSTRAINT "chk_budget_tier_limits_amount" CHECK (limit_usd >= 0)
);
CREATE UNIQUE INDEX "uniq_budget_tier_limit"
  ON "budget_tier_limits" ("budget_allocation_id","model_tier_id","period_type");
```

同一迁移还必须创建 `budget_temporary_overrides`、`budget_limit_schedules`，两表都包含可空 `failure_reason text`；override status check 包含 `active|expired|cancelled|failed`，schedule status check 包含 `pending|applied|replaced|cancelled|failed`。迁移同时增加 alert/expansion/call_logs 字段，给存量告警与扩容记录设置 `dimension_type='total'`、`dimension_key='total'`、`duration_type='permanent'`、`priority='normal'`，并建立：

```sql
CREATE UNIQUE INDEX "uniq_budget_override_active"
  ON "budget_temporary_overrides" ("budget_allocation_id","dimension_key","period_type")
  WHERE "status" = 'active';
CREATE UNIQUE INDEX "uniq_budget_schedule_pending"
  ON "budget_limit_schedules" ("budget_allocation_id","dimension_key","period_type")
  WHERE "status" = 'pending';
CREATE UNIQUE INDEX "uniq_budget_alert_dimension"
  ON "budget_alert_records"
  ("scope_type","scope_id","period_type","period","threshold","dimension_key");
```

历史兼容与日志字段还必须等价包含：

```sql
ALTER TABLE "call_logs"
  ADD COLUMN "budget_tier_id" uuid NULL,
  ADD COLUMN "budget_tier_name_snapshot" character varying(64) NULL,
  ADD COLUMN "budget_tier_accounting_eligible" boolean NOT NULL DEFAULT false,
  ADD COLUMN "budget_tier_constraints_snapshot" jsonb NOT NULL DEFAULT '[]',
  ADD COLUMN "budget_block_code" character varying(64) NULL,
  ADD COLUMN "budget_block_dimension" character varying(8) NULL,
  ADD COLUMN "budget_block_scope_type" character varying(32) NULL,
  ADD COLUMN "budget_block_scope_id" character varying(128) NULL,
  ADD COLUMN "budget_block_period_type" character varying(8) NULL;
ALTER TABLE "expansion_requests"
  ALTER COLUMN "current_amount" TYPE numeric(18,6),
  ALTER COLUMN "requested_amount" TYPE numeric(18,6);
```

该精度扩展不得伴随 handler DTO 或 Go 字段类型变化；存量 call log 不回填等级，eligible 保持 false，scope constraint snapshot 保持空数组。该 JSONB 列是正确重建的基础事实，必须与其他 call_logs 字段一起归属 P0 foundation migration，P1 不得新增独立迁移。

P0 不为 block 字段增加依赖 P1 内部类型的 check constraint；contracts 只限制公开 code 为 `budget_exceeded|tier_budget_exceeded`、dimension 为 `total|tier`。P1 负责在实际拦截写入时保证五个字段组合一致。

- [ ] **Step 4: 重新计算 hash 并验证**

Run:

```bash
atlas migrate hash --env local
atlas migrate validate --env local
rg -n 'DROP TABLE|DROP COLUMN|RENAME COLUMN' atlas-migrations/*_add_model_tier_budget_foundation.sql
```

Expected: validate 成功；最后一条 `rg` 无输出并以状态 1 结束。`atlas.sum` 必须变化，但不得手工编辑。

- [ ] **Step 5: 提交**

```bash
git add atlas-migrations/*_add_model_tier_budget_foundation.sql atlas-migrations/atlas.sum
git commit -m "feat(db): 生成模型等级预算基础迁移"
```

---

### Task 5: 实现模型等级 repository 与 service

**Files:**
- Modify: `internal/controlplane/repository/model_tier_repo.go`
- Create: `internal/controlplane/service/model_tier_service.go`
- Modify: `tests/controlplane/repository/model_tier_repo_test.go`
- Create: `tests/controlplane/service/model_tier_service_test.go`

- [ ] **Step 1: 写失败测试**

覆盖名称 trim/唯一、颜色校验、停用后禁止新绑定、排序事务、安全删除和归档：

```go
func TestModelTierServiceDeleteReferencedTierReturnsConflict(t *testing.T) {
	repo := newFakeModelTierRepo()
	id := uuid.New()
	repo.tiers[id] = &model.ModelTier{ID: id, Name: "高级", Enabled: true}
	repo.references[id] = repository.ModelTierReferenceCounts{Groups: 1}
	svc := service.NewModelTierService(repo)

	err := svc.Delete(context.Background(), id)
	require.ErrorIs(t, err, service.ErrModelTierReferenced)
}

func TestModelTierServiceReorderRequiresExactActiveSet(t *testing.T) {
	svc := service.NewModelTierService(newFakeModelTierRepoWithThreeActive())
	err := svc.Reorder(context.Background(), []uuid.UUID{tierA, tierA, tierC})
	require.ErrorIs(t, err, service.ErrInvalidTierOrder)
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/repository ./tests/controlplane/service -run ModelTier -count=1
```

Expected: FAIL，`NewModelTierService`、`ErrModelTierReferenced` 和 repository 类型不存在。

- [ ] **Step 3: 写最小实现**

Repository 精确接口：

```go
type ModelTierRepo interface {
	Create(ctx context.Context, tier *model.ModelTier) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.ModelTier, error)
	List(ctx context.Context, includeArchived bool) ([]model.ModelTier, error)
	Update(ctx context.Context, tier *model.ModelTier) error
	Reorder(ctx context.Context, ids []uuid.UUID) error
	ReferenceCounts(ctx context.Context, id uuid.UUID) (repository.ModelTierReferenceCounts, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
```

真实 repository 的 `ReferenceCounts` 必须分别查询 `model_groups`、`budget_tier_limits`、`budget_temporary_overrides`、`budget_limit_schedules`、`budget_alert_records`、`expansion_requests`、`call_logs`，返回：

```go
// 位于 internal/controlplane/repository/model_tier_repo.go。
// repository 层不得 import service，也不得返回 service.* 类型。
type ModelTierReferenceCounts struct {
	Groups, Limits, Overrides, Schedules, Alerts, Expansions, CallLogs int64
}
func (r ModelTierReferenceCounts) Total() int64 {
	return r.Groups + r.Limits + r.Overrides + r.Schedules + r.Alerts + r.Expansions + r.CallLogs
}
```

依赖方向必须通过编译检查固定为 `service -> repository -> model/budgetdomain`。任何 repository 接口参数或返回值只能使用标准库、`model.*`、`budgetdomain.*` 或定义在 `repository` 包内的 DTO；禁止 `internal/controlplane/repository` import `internal/controlplane/service`。

Service 签名：

```go
func (s *ModelTierService) Create(ctx context.Context, in CreateModelTierInput) (*model.ModelTier, error)
func (s *ModelTierService) Update(ctx context.Context, id uuid.UUID, in UpdateModelTierInput) (*model.ModelTier, error)
func (s *ModelTierService) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*model.ModelTier, error)
func (s *ModelTierService) Archive(ctx context.Context, id uuid.UUID) (*model.ModelTier, error)
func (s *ModelTierService) Delete(ctx context.Context, id uuid.UUID) error
func (s *ModelTierService) Reorder(ctx context.Context, ids []uuid.UUID) error
```

规则：名称 trim 后 1..64；颜色必须匹配 `^#[0-9A-Fa-f]{6}$`；归档强制 `enabled=false`；引用总数大于 0 时 `Delete` 返回 `ErrModelTierReferenced`；排序必须恰好包含所有未归档 ID 且不重复。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/repository/model_tier_repo.go internal/controlplane/service/model_tier_service.go tests/controlplane/repository/model_tier_repo_test.go tests/controlplane/service/model_tier_service_test.go
go test ./tests/controlplane/repository ./tests/controlplane/service -run ModelTier -count=1
```

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/model_tier_repo.go internal/controlplane/service/model_tier_service.go tests/controlplane/repository/model_tier_repo_test.go tests/controlplane/service/model_tier_service_test.go
git commit -m "feat(controlplane): 实现模型等级领域服务"
```

---

### Task 6: 暴露模型等级 API、RBAC 和审计

**Files:**
- Create: `internal/controlplane/handler/model_tier_handler.go`
- Create: `tests/controlplane/handler/model_tier_handler_test.go`
- Modify: `internal/controlplane/handler/errcodes.go`
- Modify: `internal/orgservice/rbac/permission.go`
- Modify: `internal/orgservice/rbac/registry.go`
- Modify: `internal/controlplane/audit/types.go`
- Modify: `internal/controlplane/middleware/audit.go`
- Create: `tests/controlplane/middleware/audit_test.go`
- Modify: `internal/controlplane/router/router.go`

- [ ] **Step 1: 写失败 handler、RBAC 与审计测试**

```go
func TestDeleteReferencedModelTierReturns409(t *testing.T) {
	r := setupModelTierHandler(t, fakeTierService{deleteErr: service.ErrModelTierReferenced})
	w := doJSON(r, http.MethodDelete, "/api/v1/model-tiers/"+tierID.String(), `{"reason":"配置仍被引用"}`)
	require.Equal(t, http.StatusConflict, w.Code)
	require.JSONEq(t, `{"error":{"type":"conflict","code":"MODEL_TIER_REFERENCED","message":"model tier is referenced"}}`, w.Body.String())
}

func TestCreateModelTierRequiresGlobalManagePermission(t *testing.T) {
	r := setupModelTierHandlerWithActor(t, actorWithout(rbac.PermModelTierManage))
	w := doJSON(r, http.MethodPost, "/api/v1/model-tiers", validCreateTierBody)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Zero(t, fakeService(t).createCalls)
}
```

审计测试对 create/update/status/delete/reorder 发起真实 handler 请求，再断言 audit sink 收到对应 action、resource ID、reason 与 before/after；不读取 router 注册表或 handler 类型元数据。

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/handler -run ModelTier -count=1
go test ./tests/controlplane/middleware -run Audit -count=1
```

Expected: FAIL，handler、权限和审计动作不存在。

- [ ] **Step 3: 写最小 handler 与路由**

路由：

```go
adminGroup.GET("/model-tiers", guard(rbac.PermModelTierView), modelTierHandler.List)
adminGroup.GET("/model-tiers/:id", guard(rbac.PermModelTierView), modelTierHandler.Get)
adminGroup.POST("/model-tiers", guard(rbac.PermModelTierManage), modelTierHandler.Create)
adminGroup.PUT("/model-tiers/:id", guard(rbac.PermModelTierManage), modelTierHandler.Update)
adminGroup.PATCH("/model-tiers/:id/status", guard(rbac.PermModelTierManage), modelTierHandler.SetStatus)
adminGroup.DELETE("/model-tiers/:id", guard(rbac.PermModelTierManage), modelTierHandler.Delete)
adminGroup.PUT("/model-tiers/reorder", guard(rbac.PermModelTierManage), modelTierHandler.Reorder)
```

请求/响应：

```http
POST /api/v1/model-tiers
Content-Type: application/json

{"name":"高级","description":"高成本推理模型","color":"#7C3AED","sort_order":10,"reason":"建立预算分层"}
```

```json
{
  "data": {
    "id": "2f63d65a-a0f7-4abe-b9d0-3ace39a44f30",
    "name": "高级",
    "description": "高成本推理模型",
    "color": "#7C3AED",
    "sort_order": 10,
    "enabled": true,
    "created_at": "2026-07-11T08:00:00Z",
    "updated_at": "2026-07-11T08:00:00Z"
  }
}
```

错误码与 HTTP：

- `MODEL_TIER_NOT_FOUND` → 404
- `MODEL_TIER_NAME_CONFLICT` → 409
- `MODEL_TIER_REFERENCED` → 409
- `MODEL_TIER_DISABLED` → 422
- `INVALID_MODEL_TIER_ORDER` → 422
- `bad_request` → 400
- RBAC 拒绝沿用 `org.forbidden` → 403

新增审计动作 `model_tier.create|update|status|reorder|archive|delete` 与资源 `model_tier`；handler 调用 `audit.SetResourceName`，AuditMiddleware 记录请求 ID、reason、before/after 摘要。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/handler/model_tier_handler.go internal/controlplane/handler/errcodes.go internal/orgservice/rbac/permission.go internal/orgservice/rbac/registry.go internal/controlplane/audit/types.go internal/controlplane/middleware/audit.go internal/controlplane/router/router.go tests/controlplane/middleware/audit_test.go tests/controlplane/handler/model_tier_handler_test.go
go test ./tests/controlplane/handler -run ModelTier -count=1
go test ./tests/controlplane/middleware -run Audit -count=1
```

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/handler/model_tier_handler.go internal/controlplane/handler/errcodes.go internal/orgservice/rbac internal/controlplane/audit/types.go internal/controlplane/middleware/audit.go internal/controlplane/router/router.go tests/controlplane/middleware/audit_test.go tests/controlplane/handler/model_tier_handler_test.go
git commit -m "feat(api): 提供模型等级管理接口"
```

---

### Task 7: 实现模型组单个与批量等级关联

**Files:**
- Modify: `internal/controlplane/repository/group_repo.go`
- Modify: `internal/controlplane/service/group_service.go`
- Modify: `internal/controlplane/handler/group_handler.go`
- Modify: `internal/controlplane/handler/model_tier_handler.go`
- Modify: `tests/controlplane/service/group_service_test.go`
- Modify: `tests/controlplane/handler/model_tier_handler_test.go`
- Modify: `internal/controlplane/router/router.go`
- Modify: `internal/controlplane/middleware/audit.go`
- Modify: `tests/controlplane/middleware/audit_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestAssignBudgetTierRejectsDisabledTier(t *testing.T) {
	svc := newGroupServiceWithTierRepo(disabledTier)
	err := svc.SetBudgetTier(context.Background(), 42, &disabledTier.ID)
	require.ErrorIs(t, err, service.ErrModelTierDisabled)
}

func TestBatchAssignBudgetTierIsTransactional(t *testing.T) {
	err := repo.SetBudgetTierBatch(ctx, []uint{1, 999}, &tierID)
	require.Error(t, err)
	require.Nil(t, reloadGroup(t, db, 1).BudgetTierID)
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/service -run BudgetTier -count=1
go test ./tests/controlplane/handler -run GroupBudgetTier -count=1
```

Expected: FAIL，`SetBudgetTier` 与批量 API 不存在。

- [ ] **Step 3: 写最小实现**

Repository：

```go
func (r *GroupRepo) SetBudgetTier(ctx context.Context, groupID uint, tierID *uuid.UUID) error
func (r *GroupRepo) SetBudgetTierBatch(ctx context.Context, groupIDs []uint, tierID *uuid.UUID) error
```

批量方法在事务内先 `SELECT COUNT(*) FROM model_groups WHERE id IN ? AND deleted_at IS NULL`，数量不等时返回 `ErrGroupNotFound`，然后一次 `UPDATE`。`FindByID`、`List`、`ListEnabledGroupsWithModels` 预加载 `BudgetTier`；列表响应增加：

```json
{
  "budget_tier": {
    "id": "2f63d65a-a0f7-4abe-b9d0-3ace39a44f30",
    "name": "高级",
    "color": "#7C3AED",
    "enabled": true
  }
}
```

API：

```http
PUT /api/v1/groups/42/budget-tier
{"model_tier_id":"2f63d65a-a0f7-4abe-b9d0-3ace39a44f30","reason":"统一高级模型预算"}
```

清除关联必须显式传 `null`：

```http
PUT /api/v1/groups/budget-tiers
{"group_ids":[42,43],"model_tier_id":null,"reason":"恢复未分级"}
```

两条写路由都使用 `PermModelTierManage`；成功返回 200：

```json
{"data":{"updated_group_ids":[42,43],"model_tier_id":null}}
```

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/repository/group_repo.go internal/controlplane/service/group_service.go internal/controlplane/handler/group_handler.go internal/controlplane/handler/model_tier_handler.go internal/controlplane/router/router.go internal/controlplane/middleware/audit.go tests/controlplane/middleware/audit_test.go tests/controlplane/service/group_service_test.go tests/controlplane/handler/model_tier_handler_test.go
go test ./tests/controlplane/service -run BudgetTier -count=1
go test ./tests/controlplane/handler -run GroupBudgetTier -count=1
```

Expected: PASS；不存在、已归档或停用等级均不能新增绑定，`null` 可清除绑定。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/group_repo.go internal/controlplane/service/group_service.go internal/controlplane/handler/group_handler.go internal/controlplane/handler/model_tier_handler.go internal/controlplane/router/router.go internal/controlplane/middleware/audit.go tests/controlplane/middleware/audit_test.go tests/controlplane/service/group_service_test.go tests/controlplane/handler/model_tier_handler_test.go
git commit -m "feat(groups): 支持模型组预算等级关联"
```

---

### Task 8: 实现等级额度、通用临时覆盖与计划变更 repository

**Files:**
- Create: `internal/controlplane/repository/budget_tier_repo.go`
- Create: `tests/controlplane/repository/budget_tier_repo_test.go`
- Modify: `internal/controlplane/repository/budget_repo.go`

- [ ] **Step 1: 写失败 repository 测试**

```go
func TestUpsertTierLimitKeepsIdentityAndEffectiveAt(t *testing.T) {
	first := seedTierLimit(t, repo, "80.000000", effectiveAt)
	got, err := repo.UpsertTierLimit(ctx, repository.UpsertTierLimitParams{
		BudgetAllocationID: allocationID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount("100.000000"), EffectiveAt: effectiveAt, Enabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, got.ID)
	require.Equal(t, effectiveAt, got.EffectiveAt)
}

func TestReplacePendingSchedulePreservesOldAuditRow(t *testing.T) {
	old := seedPendingSchedule(t, repo)
	current, err := repo.ReplacePendingSchedule(ctx, newSchedule)
	require.NoError(t, err)
	require.Equal(t, model.ScheduleStatusReplaced, reloadSchedule(t, db, old.ID).Status)
	require.Equal(t, model.ScheduleStatusPending, current.Status)
}

func TestApplyTotalScheduledLimitUpdatesRequestedPeriodOnly(t *testing.T) {
	alloc := seedAllocation(t, repo, daily("10.0000"), weekly("50.0000"), monthly("200.0000"))
	require.NoError(t, repo.ApplyTotalScheduledLimit(ctx, db, alloc.ID, budgetdomain.PeriodWeekly, mustAmount("75.000000")))
	got := reloadAllocation(t, db, alloc.ID)
	require.Equal(t, 10.0, got.DailyAmountUSD)
	require.Equal(t, 75.0, got.WeeklyAmountUSD)
	require.Equal(t, 200.0, got.AmountUSD)
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/repository -run 'TierLimit|TemporaryOverride|Schedule' -count=1
```

Expected: FAIL，`BudgetTierRepo` 不存在。

- [ ] **Step 3: 写最小 repository**

精确签名：

```go
func (r *BudgetRepo) GetAllocationByID(ctx context.Context, id uuid.UUID) (*model.BudgetAllocation, error)

func (r *BudgetTierRepo) ListTierLimits(ctx context.Context, allocationID uuid.UUID) ([]model.BudgetTierLimit, error)
func (r *BudgetTierRepo) FindTierLimit(ctx context.Context, allocationID, tierID uuid.UUID, period budgetdomain.PeriodType) (*model.BudgetTierLimit, error)
func (r *BudgetTierRepo) UpsertTierLimit(ctx context.Context, p UpsertTierLimitParams) (*model.BudgetTierLimit, error)
func (r *BudgetTierRepo) ListActiveOverrides(ctx context.Context, allocationID uuid.UUID, now time.Time) ([]model.BudgetTemporaryOverride, error)
func (r *BudgetTierRepo) CreateOverride(ctx context.Context, override *model.BudgetTemporaryOverride) error
func (r *BudgetTierRepo) ListDueOverrideIDs(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
func (r *BudgetTierRepo) LockOverrideForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.BudgetTemporaryOverride, error)
func (r *BudgetTierRepo) MarkOverrideExpired(ctx context.Context, tx *gorm.DB, id uuid.UUID, expiredAt time.Time) error
func (r *BudgetTierRepo) MarkOverrideFailed(ctx context.Context, tx *gorm.DB, id uuid.UUID, reason string) error
func (r *BudgetTierRepo) ReplacePendingSchedule(ctx context.Context, schedule *model.BudgetLimitSchedule) (*model.BudgetLimitSchedule, error)
func (r *BudgetTierRepo) ListDueScheduleIDs(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
func (r *BudgetTierRepo) LockScheduleForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.BudgetLimitSchedule, error)
func (r *BudgetTierRepo) MarkSchedule(ctx context.Context, tx *gorm.DB, id uuid.UUID, status budgetdomain.ScheduleStatus, reason string) error
func (r *BudgetTierRepo) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error
func (r *BudgetRepo) ApplyTotalScheduledLimit(ctx context.Context, tx *gorm.DB, allocationID uuid.UUID, period budgetdomain.PeriodType, target budgetdomain.Amount) error
```

`UpsertTierLimit` 使用 `(allocation_id,tier_id,period_type)` 冲突键；修改已有额度不重置 `effective_at`，首次创建才使用输入生效时间。计划替换在一个事务中把旧 `pending` 更新为 `replaced` 后插入新行。due ID 查询只找候选；Task 17 必须为每个 ID 开独立事务，再用 `Lock*ForUpdate` 的 `FOR UPDATE SKIP LOCKED` 复核状态与时间。`ApplyTotalScheduledLimit` 将 daily/weekly/monthly 分别映射到现有 `daily_amount_usd`/`weekly_amount_usd`/`amount_usd`，保留既有字段类型和 JSON 语义。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/repository/budget_tier_repo.go internal/controlplane/repository/budget_repo.go tests/controlplane/repository/budget_tier_repo_test.go
go test ./tests/controlplane/repository -run 'TierLimit|TemporaryOverride|Schedule' -count=1
```

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/budget_tier_repo.go internal/controlplane/repository/budget_repo.go tests/controlplane/repository/budget_tier_repo_test.go
git commit -m "feat(repository): 持久化等级额度与计划变更"
```

---

### Task 9: 实现等级额度 service 与计划创建

**Files:**
- Create: `internal/controlplane/service/budget_tier_service.go`
- Create: `tests/controlplane/service/budget_tier_service_test.go`
- Modify: `internal/controlplane/service/budget_service.go`
- Modify: `tests/controlplane/service/budget_service_test.go`
- Modify: `internal/controlplane/router/router.go`

- [ ] **Step 1: 写失败 service 测试**

```go
func TestSetTierLimitImmediatePreservesOriginalEffectiveAt(t *testing.T) {
	repo := fakeBudgetTierRepo{existing: limitAt("80.000000", cycleStart)}
	svc := service.NewBudgetTierService(&repo, fakeAllocationReader, fakeTierReader, fixedClock(now))
	got, err := svc.SetTierLimit(ctx, service.SetTierLimitInput{
		AllocationID: allocationID, TierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustAmount("100.000000"), Enabled: true,
		EffectiveMode: service.EffectiveImmediate, Reason: "扩容",
	})
	require.NoError(t, err)
	require.Equal(t, cycleStart, got.EffectiveAt)
}

func TestSetTierLimitNextPeriodReplacesPendingSchedule(t *testing.T) {
	_, err := svc.SetTierLimit(ctx, inputWithMode(service.EffectiveNextPeriod))
	require.NoError(t, err)
	require.Equal(t, nextMonthBoundaryUTC, repo.createdSchedule.EffectiveAt)
	require.Equal(t, "tier:"+tierID.String(), repo.createdSchedule.DimensionKey)
}

```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/service -run BudgetTierService -count=1
```

Expected: FAIL，`BudgetTierService` 不存在。

- [ ] **Step 3: 写最小实现**

Service 输入与输出：

```go
type EffectiveMode string
const (
	EffectiveImmediate EffectiveMode = "immediate"
	EffectiveNextPeriod EffectiveMode = "next_period"
)

type SetTierLimitInput struct {
	AllocationID uuid.UUID
	TierID uuid.UUID
	PeriodType budgetdomain.PeriodType
	LimitUSD budgetdomain.Amount
	Enabled bool
	EffectiveMode EffectiveMode
	Reason string
	ActorID uuid.UUID
	ConfirmReplaceSchedule bool
}

type TierLimitView struct {
	ID uuid.UUID `json:"id"`
	ModelTierID uuid.UUID `json:"model_tier_id"`
	ModelTierName string `json:"model_tier_name"`
	PeriodType budgetdomain.PeriodType `json:"period_type"`
	BaseLimitUSD budgetdomain.Amount `json:"base_limit_usd"`
	TemporaryTargetUSD *budgetdomain.Amount `json:"temporary_target_usd,omitempty"`
	EffectiveLimitUSD budgetdomain.Amount `json:"effective_limit_usd"`
	SourceSummary LimitSourceSummary `json:"source_summary"`
	Enabled bool `json:"enabled"`
	EffectiveAt time.Time `json:"effective_at"`
	OverrideExpiresAt *time.Time `json:"override_expires_at,omitempty"`
	PendingSchedule *ScheduleView `json:"pending_schedule,omitempty"`
}

type LimitSourceSummary struct {
	SourceType string `json:"source_type"` // base|temporary_override
	TemporaryActive bool `json:"temporary_active"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
```

`ListTierLimits` 只返回已配置记录，不制造“无限”金额。有效额度取基础值和 active override 的最大值；存在 active override 时 `temporary_target_usd` 返回目标值，`source_summary.source_type=temporary_override`，否则该字段省略且 source 为 base。立即更新保留已有 `effective_at`；首次创建使用当前时间。下周期模式按共享 `budgetperiod.Resolve` 计算边界并要求 reason 非空；已有 pending 计划且 `ConfirmReplaceSchedule=false` 时返回 `ErrPendingScheduleExists`。schedule 与 override 的实际生命周期执行全部集中到 Task 17 `BudgetLifecycleExecutor`。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/service/budget_tier_service.go internal/controlplane/service/budget_service.go internal/controlplane/router/router.go tests/controlplane/service/budget_tier_service_test.go tests/controlplane/service/budget_service_test.go
go test ./tests/controlplane/service -run 'BudgetTierService|BudgetService' -count=1
```

Expected: PASS；等级额度立即/下周期配置和 source summary 正确，现有总预算测试仍通过。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/budget_tier_service.go internal/controlplane/service/budget_service.go internal/controlplane/router/router.go tests/controlplane/service/budget_tier_service_test.go tests/controlplane/service/budget_service_test.go
git commit -m "feat(controlplane): 实现等级额度与计划创建"
```

---

### Task 10: 暴露等级额度 API 并按 allocation 实施 RBAC

**Files:**
- Create: `internal/controlplane/handler/budget_tier_handler.go`
- Create: `tests/controlplane/handler/budget_tier_handler_test.go`
- Modify: `internal/controlplane/handler/errcodes.go`
- Modify: `internal/controlplane/router/router.go`
- Modify: `internal/controlplane/audit/types.go`
- Modify: `internal/controlplane/middleware/audit.go`
- Modify: `tests/controlplane/middleware/audit_test.go`

- [ ] **Step 1: 写失败 handler/RBAC 测试**

```go
func TestStudioAdminCannotEditOwnStudioAllocation(t *testing.T) {
	r := setupBudgetTierHandler(t, actorForStudio("studio-a"), allocation("studio", "studio-a"))
	w := doJSON(r, http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", validLimitBody)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeptAdminCanEditManagedMemberTierLimit(t *testing.T) {
	r := setupBudgetTierHandler(t, actorForDept("dept-a"), memberAllocationInDept("member-a", "dept-a"))
	w := doJSON(r, http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", validLimitBody)
	require.Equal(t, http.StatusOK, w.Code)
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/handler -run BudgetTier -count=1
```

Expected: FAIL，`BudgetTierHandler` 不存在。

- [ ] **Step 3: 写最小 handler、范围规则和路由**

路由：

```go
adminGroup.GET("/budgets/:allocation_id/tier-limits", guard(rbac.PermBudgetViewScoped), budgetTierHandler.List)
adminGroup.PUT("/budgets/:allocation_id/tier-limits", guard(rbac.PermBudgetSet), budgetTierHandler.Set)
```

写权限遵循“上级配置下级”：

- global admin 可配置 company 和任意下级；
- studio admin 的自身 studio allocation 只读，可配置其下 dept_daily/dept_service；
- dept admin 的自身 dept allocation 只读，可配置本部门 member；
- 普通用户不能访问管理 API。

请求：

```json
{
  "items": [
    {
      "model_tier_id": "2f63d65a-a0f7-4abe-b9d0-3ace39a44f30",
      "period_type": "monthly",
      "limit_usd": "100.000000",
      "enabled": true,
      "effective_mode": "immediate"
    },
    {
      "model_tier_id": "2f63d65a-a0f7-4abe-b9d0-3ace39a44f30",
      "period_type": "weekly",
      "limit_usd": "25.000000",
      "enabled": true,
      "effective_mode": "next_period"
    }
  ],
  "reason": "限制高级模型消费",
  "confirm_replace_schedule": false
}
```

响应：

```json
{
  "data": {
    "allocation_id": "0e71ad3e-f75a-4f50-b791-593df2c35b27",
    "tier_limits": [
      {
        "model_tier_id": "2f63d65a-a0f7-4abe-b9d0-3ace39a44f30",
        "model_tier_name": "高级",
        "period_type": "monthly",
        "base_limit_usd": "80.000000",
        "temporary_target_usd": "100.000000",
        "effective_limit_usd": "100.000000",
        "source_summary": {
          "source_type": "temporary_override",
          "temporary_active": true,
          "expires_at": "2026-08-01T00:00:00+08:00"
        },
        "enabled": true,
        "effective_at": "2026-07-11T08:00:00Z"
      }
    ]
  }
}
```

错误码：

- `BUDGET_ALLOCATION_NOT_FOUND` → 404
- `MODEL_TIER_NOT_FOUND` → 404
- `MODEL_TIER_DISABLED` → 422
- `INVALID_BUDGET_PERIOD` → 422
- `INVALID_MONEY_AMOUNT` → 422
- `PENDING_BUDGET_SCHEDULE_EXISTS` → 409
- `BUDGET_LIMIT_BELOW_USED` 预留为 409，本阶段 service 未注入实时用量时不触发
- `BUDGET_USAGE_UNAVAILABLE` 预留为 503，供后续高风险调额预检使用

写操作审计动作 `budget_tier_limit.set|schedule|replace_schedule`，details 保存 before/after、reason、effective_mode、allocation scope，不保存上级敏感金额到普通用户响应。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/handler/budget_tier_handler.go internal/controlplane/handler/errcodes.go internal/controlplane/router/router.go internal/controlplane/audit/types.go internal/controlplane/middleware/audit.go tests/controlplane/middleware/audit_test.go tests/controlplane/handler/budget_tier_handler_test.go
go test ./tests/controlplane/handler -run BudgetTier -count=1
go test ./tests/controlplane/middleware -run Audit -count=1
```

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/handler/budget_tier_handler.go internal/controlplane/handler/errcodes.go internal/controlplane/router/router.go internal/controlplane/audit/types.go internal/controlplane/middleware/audit.go tests/controlplane/middleware/audit_test.go tests/controlplane/handler/budget_tier_handler_test.go
git commit -m "feat(api): 提供等级子预算配置接口"
```

---

### Task 11: 实现等级额度批量 preview/apply

**Files:**
- Modify: `internal/controlplane/service/budget_tier_service.go`
- Modify: `internal/controlplane/handler/budget_tier_handler.go`
- Modify: `internal/controlplane/router/router.go`
- Modify: `internal/controlplane/audit/types.go`
- Modify: `internal/controlplane/middleware/audit.go`
- Modify: `tests/controlplane/middleware/audit_test.go`
- Create: `tests/controlplane/service/budget_tier_batch_service_test.go`
- Create: `tests/controlplane/handler/budget_tier_batch_handler_test.go`

- [ ] **Step 1: 写失败 service 与 handler 测试**

```go
func TestPreviewBatchReturnsPerItemResultWithoutWriting(t *testing.T) {
	svc := newBatchService(t)
	got, err := svc.PreviewBatch(ctx, actor, service.BatchTierLimitInput{
		Operation: service.BatchSetFixed,
		Items: []service.BatchTierLimitItem{
			{AllocationID: editableID, TierID: tierID, PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount("100.000000")},
			{AllocationID: forbiddenID, TierID: tierID, PeriodType: budgetdomain.PeriodMonthly, Value: mustAmount("100.000000")},
		},
		Reason: "统一高级等级额度",
	})
	require.NoError(t, err)
	require.Equal(t, "valid", got.Items[0].Status)
	require.Equal(t, "forbidden", got.Items[1].Status)
	require.Equal(t, 0, svc.repo.writeCount)
	require.NotEmpty(t, got.PreviewToken)
}

func TestApplyBatchWritesOnlyPreviewedValidItems(t *testing.T) {
	preview := mustPreview(t, svc, inputWithOneValidOneForbidden())
	got, err := svc.ApplyBatch(ctx, actor, service.ApplyBatchTierLimitInput{
		PreviewToken: preview.PreviewToken,
		ConfirmItemIDs: []string{preview.Items[0].ItemID},
	})
	require.NoError(t, err)
	require.Equal(t, "applied", got.Items[0].Status)
	require.Equal(t, "skipped", got.Items[1].Status)
	require.Equal(t, 1, svc.repo.writeCount)
}
```

Handler 测试额外覆盖百分比减少后变负、preview 过期/输入漂移、重复 item、无权限 allocation 和共享池 member 固定额度提示。

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/service ./tests/controlplane/handler -run BatchTierLimit -count=1
```

Expected: FAIL，batch service 方法与两条路由不存在。

- [ ] **Step 3: 写最小实现**

```go
type BatchOperation string
const (
	BatchSetFixed BatchOperation = "set_fixed"
	BatchAdjustPercent BatchOperation = "adjust_percent"
)

type BatchTierLimitItem struct {
	ItemID string `json:"item_id"`
	AllocationID uuid.UUID `json:"allocation_id"`
	TierID uuid.UUID `json:"model_tier_id"`
	PeriodType budgetdomain.PeriodType `json:"period_type"`
	Direction string `json:"direction,omitempty"` // increase|decrease，仅 percent 使用
	Value budgetdomain.Amount `json:"value"`
}

type BatchPreviewItem struct {
	ItemID string `json:"item_id"`
	OldLimitUSD *budgetdomain.Amount `json:"old_limit_usd"`
	NewLimitUSD budgetdomain.Amount `json:"new_limit_usd"`
	DeltaUSD string `json:"delta_usd"` // 6 位有符号 Decimal 字符串，降额可为负
	Status string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
	OverAllocationRatio string `json:"over_allocation_ratio,omitempty"`
	ConvertsSharedMemberToIndependent bool `json:"converts_shared_member_to_independent"`
}

func (s *BudgetTierService) PreviewBatch(ctx context.Context, actor rbac.Actor, in BatchTierLimitInput) (*BatchTierLimitPreview, error)
func (s *BudgetTierService) ApplyBatch(ctx context.Context, actor rbac.Actor, in ApplyBatchTierLimitInput) (*BatchTierLimitApplyResult, error)
```

API：

```http
POST /api/v1/budgets/tier-limits/batch/preview
{"operation":"adjust_percent","items":[{"item_id":"row-1","allocation_id":"0e71ad3e-f75a-4f50-b791-593df2c35b27","model_tier_id":"2f63d65a-a0f7-4abe-b9d0-3ace39a44f30","period_type":"monthly","direction":"increase","value":"10.000000"}],"reason":"高级等级统一增加 10%"}
```

```json
{"data":{"preview_token":"sha256:4b9...","expires_at":"2026-07-11T08:05:00Z","items":[{"item_id":"row-1","old_limit_usd":"80.000000","new_limit_usd":"88.000000","delta_usd":"8.000000","status":"valid","over_allocation_ratio":"0.176000","converts_shared_member_to_independent":false}]}}
```

```http
POST /api/v1/budgets/tier-limits/batch/apply
{"preview_token":"sha256:4b9...","confirm_item_ids":["row-1"]}
```

preview token 绑定 actor、规范化输入、每项当前版本和 5 分钟过期时间；apply 重新检查 scope、当前版本和 token，仅逐项提交 `valid && confirmed` 项，失败项返回 `failed` 而不回滚已成功项。响应逐项返回 `applied|failed|skipped`，满足“标记失败项，只提交校验通过对象”。写操作使用 `PermBudgetSet` 并记录每个 allocation 的 before/after/reason 审计。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/service/budget_tier_service.go internal/controlplane/handler/budget_tier_handler.go internal/controlplane/router/router.go internal/controlplane/audit/types.go internal/controlplane/middleware/audit.go tests/controlplane/middleware/audit_test.go tests/controlplane/service/budget_tier_batch_service_test.go tests/controlplane/handler/budget_tier_batch_handler_test.go
go test ./tests/controlplane/service ./tests/controlplane/handler -run BatchTierLimit -count=1
go test ./tests/controlplane/middleware -run Audit -count=1
```

Expected: PASS；preview 零写入，apply 不接受过期/篡改 preview，部分失败结果稳定可重试。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/budget_tier_service.go internal/controlplane/handler/budget_tier_handler.go internal/controlplane/router/router.go internal/controlplane/audit/types.go internal/controlplane/middleware/audit.go tests/controlplane/middleware/audit_test.go tests/controlplane/service/budget_tier_batch_service_test.go tests/controlplane/handler/budget_tier_batch_handler_test.go
git commit -m "feat(budget): 支持等级额度批量预览与应用"
```

---

### Task 12: 实现 tier usage 与 my budget tiers 聚合 API

**Files:**
- Modify: `internal/controlplane/repository/call_log_repo.go`
- Modify: `internal/controlplane/repository/budget_repo.go`
- Modify: `internal/controlplane/repository/budget_tier_repo.go`
- Modify: `internal/controlplane/service/budget_tier_service.go`
- Modify: `internal/controlplane/handler/budget_tier_handler.go`
- Modify: `internal/controlplane/router/router.go`
- Create: `tests/controlplane/repository/call_log_tier_usage_repo_test.go`
- Create: `tests/controlplane/service/budget_tier_usage_service_test.go`
- Create: `tests/controlplane/handler/budget_tier_usage_handler_test.go`

- [ ] **Step 1: 写失败聚合与脱敏测试**

```go
func TestTierUsageCountsOnlyEligibleRowsForAccountingUsage(t *testing.T) {
	seedCallLog(t, db, tierID, "1.250000", true)
	seedCallLog(t, db, tierID, "9.000000", false)
	got, err := repo.SumTierUsage(ctx, repository.TierUsageQuery{
		TierID: &tierID, StartAt: cycleStart, EndAt: cycleEnd,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "10.250000", got[0].ReportedCostUSD.String())
	require.Equal(t, "1.250000", got[0].AccountingCostUSD.String())
}

func TestMyBudgetTiersHidesUpstreamExactAmount(t *testing.T) {
	got, err := svc.GetMyBudgetTiers(ctx, memberActor)
	require.NoError(t, err)
	require.Equal(t, "restricted", got.Data.TierLimits[0].Periods[0].Status)
	require.Equal(t, "studio", got.Data.TierLimits[0].Periods[0].LimitingSource.ScopeType)
	require.Nil(t, got.Data.TierLimits[0].Periods[0].LimitingSource.RemainingUSD)
	require.Equal(t, service.SourceModeMixed, got.Data.SourceSummary.Mode)
	require.NotEmpty(t, got.Data.EffectiveTotal)
	require.Len(t, got.Data.BudgetChain, 4)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"source_mode"`)
	require.NotContains(t, string(raw), `"tiers"`)
	require.NotContains(t, string(raw), `"data_as_of"`)
	var envelope struct{ Data map[string]json.RawMessage `json:"data"` }
	require.NoError(t, json.Unmarshal(raw, &envelope))
	for _, key := range []string{
		"source_summary", "effective_total", "budget_chain",
		"tier_limits", "data_updated_at", "stale",
	} {
		require.Contains(t, envelope.Data, key)
	}
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/repository ./tests/controlplane/service ./tests/controlplane/handler -run 'TierUsage|MyBudgetTiers' -count=1
```

Expected: FAIL，tier usage repository、聚合 service 与路由尚不存在。

- [ ] **Step 3: 写最小实现**

Repository DTO 必须定义在 `repository` 包，禁止返回 `service.TierUsage*`：

```go
type TierUsageQuery struct {
	ScopeType string
	ScopeID string
	TierID *uuid.UUID
	GroupID *uint
	StartAt time.Time
	EndAt time.Time
}

type TierUsageAggregate struct {
	ScopeType string
	ScopeID string
	TierID *uuid.UUID
	GroupID *uint
	ReportedCostUSD budgetdomain.Amount
	AccountingCostUSD budgetdomain.Amount
	RequestCount int64
}

func (r *CallLogRepo) SumTierUsage(ctx context.Context, q TierUsageQuery) ([]TierUsageAggregate, error)
func (s *BudgetTierService) QueryTierUsage(ctx context.Context, actor rbac.Actor, q TierUsageQueryInput) (*TierUsageResponse, error)
func (s *BudgetTierService) GetMyBudgetTiers(ctx context.Context, actor rbac.Actor) (*MyBudgetTiersResponse, error)

type MyBudgetTiersResponse struct {
	Data MyBudgetTiersData `json:"data"`
}

type MyBudgetTiersData struct {
	SourceSummary MyBudgetSourceSummary `json:"source_summary"`
	EffectiveTotal []EffectiveTotalPeriod `json:"effective_total"`
	BudgetChain []MyBudgetChainScope `json:"budget_chain"`
	TierLimits []MyBudgetTierView `json:"tier_limits"`
	DataUpdatedAt time.Time `json:"data_updated_at"`
	Stale bool `json:"stale"`
}

type BudgetSourceMode string
const (
	SourceModePersonal BudgetSourceMode = "personal"
	SourceModeShared BudgetSourceMode = "shared"
	SourceModeMixed BudgetSourceMode = "mixed"
)

type MyBudgetSourceSummary struct {
	Mode BudgetSourceMode `json:"mode"`
	PrimaryScopeType string `json:"primary_scope_type"`
	HasSharedPool bool `json:"has_shared_pool"`
	HasTemporaryOverride bool `json:"has_temporary_override"`
}

type EffectiveTotalPeriod struct {
	PeriodType budgetdomain.PeriodType `json:"period_type"`
	Configured bool `json:"configured"`
	BaseLimitUSD *budgetdomain.Amount `json:"base_limit_usd,omitempty"`
	TemporaryTargetUSD *budgetdomain.Amount `json:"temporary_target_usd,omitempty"`
	EffectiveLimitUSD *budgetdomain.Amount `json:"effective_limit_usd,omitempty"`
	UsedUSD *budgetdomain.Amount `json:"used_usd,omitempty"`
	RemainingUSD *budgetdomain.Amount `json:"remaining_usd,omitempty"`
	ResetAt time.Time `json:"reset_at"`
}

type MyBudgetChainScope struct {
	ScopeType string `json:"scope_type"`
	Label string `json:"label"`
	VisibleAmounts bool `json:"visible_amounts"`
}

type MyBudgetTierView struct {
	ModelTierID uuid.UUID `json:"model_tier_id"`
	ModelTierName string `json:"model_tier_name"`
	Periods []MyBudgetTierPeriod `json:"periods"`
}

type MyBudgetTierPeriod struct {
	PeriodType budgetdomain.PeriodType `json:"period_type"`
	Configured bool `json:"configured"`
	BaseLimitUSD *budgetdomain.Amount `json:"base_limit_usd,omitempty"`
	TemporaryTargetUSD *budgetdomain.Amount `json:"temporary_target_usd,omitempty"`
	EffectiveLimitUSD *budgetdomain.Amount `json:"effective_limit_usd,omitempty"`
	UsedUSD *budgetdomain.Amount `json:"used_usd,omitempty"`
	RemainingUSD *budgetdomain.Amount `json:"remaining_usd,omitempty"`
	ResetAt time.Time `json:"reset_at"`
	Status string `json:"status"`
	LimitingSource LimitingSourceView `json:"limiting_source"`
}

type LimitingSourceView struct {
	ScopeType string `json:"scope_type"`
	RemainingUSD *budgetdomain.Amount `json:"remaining_usd,omitempty"`
}
```

`ReportedCostUSD` 汇总所有有 tier snapshot 的成功调用，`AccountingCostUSD` 只汇总 `budget_tier_accounting_eligible=true`；额度已用/剩余使用 accounting 值，报表同时返回两者，避免停用或 feature-off 区间被误算。周期范围通过 `budgetperiod.Resolve(now, period, Config{Timezone:"Asia/Shanghai",WeekStart:"monday"})` 取得。

管理 API：

```http
GET /api/v1/budgets/tier-usage?scope_type=dept_daily&scope_id=dept-a&period_type=monthly&model_tier_id=2f63d65a-a0f7-4abe-b9d0-3ace39a44f30&group_id=42
```

```json
{"data":{"data_as_of":"2026-07-11T08:00:00Z","stale":false,"items":[{"scope_type":"dept_daily","scope_id":"dept-a","model_tier_id":"2f63d65a-a0f7-4abe-b9d0-3ace39a44f30","group_id":42,"reported_cost_usd":"12.500000","accounting_cost_usd":"10.000000","request_count":37}]}}
```

用户 API：

```http
GET /api/v1/my/budget/tiers
```

```json
{
  "data": {
    "source_summary": {
      "mode": "mixed",
      "primary_scope_type": "member",
      "has_shared_pool": true,
      "has_temporary_override": true
    },
    "effective_total": [
      {
        "period_type": "monthly",
        "configured": true,
        "base_limit_usd": "300.000000",
        "temporary_target_usd": "350.000000",
        "effective_limit_usd": "350.000000",
        "used_usd": "20.000000",
        "remaining_usd": "330.000000",
        "reset_at": "2026-08-01T00:00:00+08:00"
      }
    ],
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
        "limiting_source": {"scope_type":"member","remaining_usd":"80.000000"}
      }]
    }],
    "data_updated_at": "2026-07-11T08:00:00Z",
    "stale": false
  }
}
```

未配置时返回 `configured:false` 且省略 limit/remaining，不伪造无限值。`source_summary.mode` 只允许 `personal|shared|mixed`；顶层 `data` 固定包含 `source_summary/effective_total/budget_chain/tier_limits/data_updated_at/stale`，禁止输出 `source_mode` 或 `tiers/data_as_of` 别名。`temporary_target_usd` 只在 active override 存在时返回；`effective_total` 给 P2 顶部总预算卡片，`source_summary` 给来源文案，`budget_chain` 给预算链展示。有效 remaining 是预算链中所有生效约束的最小值；普通用户对 company/studio/dept 上级限制只收到 scope type、label 和状态，不返回精确 limit/used/remaining 或主体 ID。管理接口按 actor 范围过滤，普通用户只能调用 `/my`。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/repository/call_log_repo.go internal/controlplane/repository/budget_repo.go internal/controlplane/repository/budget_tier_repo.go internal/controlplane/service/budget_tier_service.go internal/controlplane/handler/budget_tier_handler.go internal/controlplane/router/router.go tests/controlplane/repository/call_log_tier_usage_repo_test.go tests/controlplane/service/budget_tier_usage_service_test.go tests/controlplane/handler/budget_tier_usage_handler_test.go
go test ./tests/controlplane/repository ./tests/controlplane/service ./tests/controlplane/handler -run 'TierUsage|MyBudgetTiers' -count=1
```

Expected: PASS；停用/feature-off 行只进入 reported，不进入 accounting；普通用户看不到上级精确金额。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/call_log_repo.go internal/controlplane/repository/budget_repo.go internal/controlplane/repository/budget_tier_repo.go internal/controlplane/service/budget_tier_service.go internal/controlplane/handler/budget_tier_handler.go internal/controlplane/router/router.go tests/controlplane/repository/call_log_tier_usage_repo_test.go tests/controlplane/service/budget_tier_usage_service_test.go tests/controlplane/handler/budget_tier_usage_handler_test.go
git commit -m "feat(api): 提供等级用量与我的预算聚合"
```

---

### Task 13: 实现 Control Plane BudgetTierSnapshotPublisher

**Files:**
- Modify: `internal/configsync/budget_tier_snapshot.go`
- Modify: `internal/controlplane/repository/group_repo.go`
- Modify: `internal/controlplane/service/model_tier_service.go`
- Modify: `internal/controlplane/service/group_service.go`
- Modify: `internal/controlplane/service/budget_tier_service.go`
- Create: `internal/controlplane/service/budget_tier_snapshot_publisher.go`
- Modify: `internal/controlplane/event/change_event.go`
- Modify: `internal/controlplane/event/publisher.go`
- Modify: `internal/controlplane/router/router.go`
- Create: `tests/controlplane/service/budget_tier_snapshot_publisher_test.go`
- Modify: `tests/configsync/budget_tier_snapshot_test.go`

- [ ] **Step 1: 写失败发布、失效与 reconcile 测试**

```go
func TestBudgetTierSnapshotPublisherWritesCompleteMonotonicSnapshot(t *testing.T) {
	p := newSnapshotPublisher(t, groups("claude-opus", "gpt-5"), revisionAt(41))
	first, err := p.Publish(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(42), first.Revision)
	require.Len(t, first.GroupTiers, 2)

	second, err := p.Publish(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(43), second.Revision)

	var got infra.ChangeEvent
	require.NoError(t, json.Unmarshal(publishedPayloads(t)[0], &got))
	require.Equal(t, "budget_tier_snapshot_updated", got.Type)
	require.Equal(t, "budget_tier_snapshot", got.ResourceType)
	require.Equal(t, uuid.Nil.String(), got.ResourceID)
	require.Equal(t, int64(42), got.SnapshotVersion)
	require.False(t, got.Timestamp.IsZero())
}

func TestBudgetTierSnapshotReconcileRepairsFailedPublication(t *testing.T) {
	p := newSnapshotPublisherWithFirstSaveFailure(t)
	require.Error(t, p.MarkDirtyAndPublish(ctx))
	require.True(t, p.Dirty())
	require.NoError(t, p.Reconcile(ctx))
	require.False(t, p.Dirty())
	require.NotNil(t, mustLoadSnapshot(t))
}
```

还要覆盖删除最后一个 group 关联后快照不残留旧 map entry、Redis 保存失败不发布事件、事件失败保留 dirty、并发 publish revision 不重复、publisher 重启后 revision 继续递增。

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/service ./tests/configsync -run BudgetTierSnapshot -count=1
```

Expected: FAIL，publisher、revision key 和 reconcile 尚不存在。

- [ ] **Step 3: 写最小实现并接入所有写路径**

```go
const (
	BudgetTierSnapshotKey = "budget:v2:tier:snapshot"
	BudgetTierSnapshotRevisionKey = "budget:v2:tier:snapshot:revision"
)

// internal/controlplane/event/change_event.go
const BudgetTierSnapshotUpdated ChangeEventType = "budget_tier_snapshot_updated"

type BudgetTierSnapshotSource interface {
	ListBudgetTierGroupSnapshots(ctx context.Context) ([]repository.BudgetTierGroupSnapshotRow, error)
}

type SnapshotEventPublisher interface {
	Publish(ctx context.Context, change *event.ChangeEvent) error
}

type BudgetTierSnapshotPublisher struct {
	source BudgetTierSnapshotSource
	rdb *redis.Client
	events SnapshotEventPublisher
	settings BudgetTierSnapshotSettings
	clock func() time.Time
	dirty atomic.Bool
}

func (p *BudgetTierSnapshotPublisher) Publish(ctx context.Context) (*configsync.BudgetTierSnapshot, error)
func (p *BudgetTierSnapshotPublisher) MarkDirtyAndPublish(ctx context.Context) error
func (p *BudgetTierSnapshotPublisher) Reconcile(ctx context.Context) error
func (p *BudgetTierSnapshotPublisher) StartReconcile(ctx context.Context, interval time.Duration)
```

`repository.BudgetTierGroupSnapshotRow` 定义在 repository 包，只含 `GroupName`、`TierID`、`TierName`、`TierEnabled`；repository 不得 import service。每次发布必须查询所有未删除模型组与其当前等级，构建完整 `group_tiers`，禁止增量 patch。flags 只能来自 Task 2 的 canonical 字段，周期设置固定从 `budget.timezone` 与 `week_start` 读取并验证。

Redis Lua 在一次原子操作中 `INCR budget:v2:tier:snapshot:revision`、把 revision 注入完整 JSON 后 `SET budget:v2:tier:snapshot`；若序列化或 SET 失败，不发布事件。成功保存后依次：

1. 通过现有 `event.Publisher.Publish` 向 `config:change` 发布 `event.NewChangeEvent(event.BudgetTierSnapshotUpdated, "budget_tier_snapshot", uuid.Nil, revision)`；线上 JSON 必须与 `infra.ChangeEvent` 完全一致：`type/resource_type/resource_id/snapshot_version/timestamp`，禁止发布裸字符串；
2. 再通过同一 publisher 发布 `event.NewChangeEvent(event.AuthConfigChanged, "auth_config", uuid.Nil, revision)`，让每个 Gateway `KeyCache.FlushAll` 清除本地与 `gateway:keycache:*` / `key:auth:verify:*`，确保 verify-key 中 `tier_limits` 重新拉取；
3. 两步都成功才清除 dirty。

所有 model tier create/update/status/archive/delete/reorder、group 单个/批量关联、tier limit immediate/schedule apply、temporary override create 写路径都在数据库提交后调用 `MarkDirtyAndPublish`；temporary override expire 由 Task 17 worker 在事务提交后调用同一 publisher。发布失败不回滚数据库事务，返回/记录 `BUDGET_CONFIG_SYNC_PENDING` 并保留 dirty。Control Plane 启动 `StartReconcile(ctx, 30*time.Second)`；reconcile 在 dirty 或数据库投影 hash 与 Redis 快照不一致时重建完整快照，加载失败继续保留 Redis last-known-good。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/configsync/budget_tier_snapshot.go internal/controlplane/repository/group_repo.go internal/controlplane/service/model_tier_service.go internal/controlplane/service/group_service.go internal/controlplane/service/budget_tier_service.go internal/controlplane/service/budget_tier_snapshot_publisher.go internal/controlplane/event/change_event.go internal/controlplane/event/publisher.go internal/controlplane/router/router.go tests/controlplane/service/budget_tier_snapshot_publisher_test.go tests/configsync/budget_tier_snapshot_test.go
go test ./tests/controlplane/service ./tests/configsync -run BudgetTierSnapshot -count=1
go test ./tests/controlplane/service -run 'ModelTier|BudgetTierService' -count=1
```

Expected: PASS；revision 单调递增，快照始终完整，KeyCache 失效事件可观察，30 秒 reconcile 能修复失败且不清空 last-known-good。

- [ ] **Step 5: 提交**

```bash
git add internal/configsync/budget_tier_snapshot.go internal/controlplane/repository/group_repo.go internal/controlplane/service/model_tier_service.go internal/controlplane/service/group_service.go internal/controlplane/service/budget_tier_service.go internal/controlplane/service/budget_tier_snapshot_publisher.go internal/controlplane/event/change_event.go internal/controlplane/event/publisher.go internal/controlplane/router/router.go tests/controlplane/service/budget_tier_snapshot_publisher_test.go tests/configsync/budget_tier_snapshot_test.go
git commit -m "feat(config): 发布并对账等级预算快照"
```

---

### Task 14: 扩展告警维度并修复 period_type 去重遗漏

**Files:**
- Modify: `internal/controlplane/repository/budget_alert_repo.go`
- Modify: `internal/controlplane/service/budget_service.go`
- Modify: `internal/controlplane/service/alert_expansion_service.go`
- Modify: `internal/controlplane/handler/budget_handler.go`
- Create: `tests/controlplane/repository/budget_alert_repo_dimension_test.go`
- Modify: `tests/controlplane/service/budget_service_test.go`
- Modify: `tests/controlplane/service/budget_alert_service_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestAlertDedupSeparatesPeriodTypeAndTier(t *testing.T) {
	require.NoError(t, repo.Create(totalMonthly))
	require.False(t, repo.ExistsForPeriod(ctx, repository.AlertDimension{
		ScopeType: "member", ScopeID: memberID, PeriodType: "daily",
		Period: "2026-07-11", Threshold: 80, DimensionType: "total",
	}))
	require.False(t, repo.ExistsForPeriod(ctx, repository.AlertDimension{
		ScopeType: "member", ScopeID: memberID, PeriodType: "monthly",
		Period: "2026-07", Threshold: 80, DimensionType: "tier", ModelTierID: &tierID,
	}))
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/repository -run AlertDedup -count=1
go test ./tests/controlplane/service -run EvaluateAndAlert -count=1
```

Expected: FAIL；现有 `ExistsForPeriod` 不接收 `period_type` 与等级维度。

- [ ] **Step 3: 写最小实现**

统一参数：

```go
type AlertDimension struct {
	ScopeType string
	ScopeID string
	PeriodType budgetdomain.PeriodType
	Period string
	Threshold int
	DimensionType budgetdomain.DimensionType
	ModelTierID *uuid.UUID
}

func (r *BudgetAlertRepo) ExistsForPeriod(ctx context.Context, d AlertDimension) (bool, error)
func (r *BudgetAlertRepo) DeleteByDimension(ctx context.Context, d AlertDimension) error
```

方法进入 DB 前调用 `budgetdomain.DimensionKey`，查询必须包含 `scope_type, scope_id, period_type, period, threshold, dimension_key`。现有总预算 `EvaluateAndAlert` 明确构造 `DimensionTotal`，内部 API 请求增加可选：

```json
{
  "scope_type": "dept_daily",
  "scope_id": "dept-a",
  "period_type": "monthly",
  "dimension_type": "total",
  "model_tier_id": null
}
```

旧 Gateway 只发送 scope 字段时默认 `monthly + total`，保持兼容；tier 告警评估本阶段只建立契约，不读取 Redis v2。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/repository/budget_alert_repo.go internal/controlplane/service/budget_service.go internal/controlplane/service/alert_expansion_service.go internal/controlplane/handler/budget_handler.go tests/controlplane/repository/budget_alert_repo_dimension_test.go tests/controlplane/service/budget_service_test.go tests/controlplane/service/budget_alert_service_test.go
go test ./tests/controlplane/repository -run AlertDedup -count=1
go test ./tests/controlplane/service -run 'EvaluateAndAlert|BudgetService' -count=1
```

Expected: PASS；同 scope/周期/阈值的 total 与多个 tier 可分别去重。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/budget_alert_repo.go internal/controlplane/service/budget_service.go internal/controlplane/service/alert_expansion_service.go internal/controlplane/handler/budget_handler.go tests/controlplane/repository/budget_alert_repo_dimension_test.go tests/controlplane/service/budget_service_test.go tests/controlplane/service/budget_alert_service_test.go
git commit -m "fix(alerts): 按周期与预算维度去重"
```

---

### Task 15: 扩展 expansion 兼容与 call_logs P0 契约

**Files:**
- Modify: `internal/controlplane/repository/expansion_repo.go`
- Modify: `internal/controlplane/service/alert_expansion_service.go`
- Modify: `internal/controlplane/handler/expansion_handler.go`
- Modify: `tests/controlplane/repository/expansion_repo_test.go`
- Create: `tests/controlplane/repository/expansion_repo_dimension_test.go`
- Modify: `tests/contract/contracts.json`（先加入 call log 字段；Task 18 完成全部契约）

- [ ] **Step 1: 写失败测试**

```go
func TestPendingExpansionUniquenessIncludesDimensionAndDuration(t *testing.T) {
	base := pendingExpansion("total", nil, "permanent")
	require.NoError(t, repo.Create(base))
	exists, err := repo.ExistsPending(ctx, repository.PendingExpansionKey{
		RequesterID: base.RequesterID, ScopeType: base.ScopeType, ScopeID: base.ScopeID,
		PeriodType: budgetdomain.PeriodMonthly, DimensionType: budgetdomain.DimensionTotal,
		DurationType: budgetdomain.DurationPermanent,
	})
	require.NoError(t, err)
	require.True(t, exists)

	tierID := uuid.New()
	exists, err = repo.ExistsPending(ctx, repository.PendingExpansionKey{
		RequesterID: base.RequesterID, ScopeType: base.ScopeType, ScopeID: base.ScopeID,
		PeriodType: budgetdomain.PeriodMonthly, DimensionType: budgetdomain.DimensionTier,
		ModelTierID: &tierID, DurationType: budgetdomain.DurationCurrentPeriod,
	})
	require.NoError(t, err)
	require.False(t, exists)
}
```

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/repository -run Expansion -count=1
```

Expected: FAIL，新的 expansion 查询与兼容维度尚不存在。

- [ ] **Step 3: 写最小兼容实现**

Repository 增加：

```go
func (r *ExpansionRepo) ExistsPending(ctx context.Context, key PendingExpansionKey) (bool, error)
func (r *ExpansionRepo) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*model.ExpansionRequest, error)
```

`ExistsPending` 只匹配 `pending|precheck_required`，并包含 requester、scope、period、dimension_key、duration_type。`FindByIDForUpdate` 使用 `clause.Locking{Strength:"UPDATE"}`，供后续完整批准事务使用。

旧 `POST /expansion-requests` 未传新字段时默认：

```json
{
  "dimension_type": "total",
  "model_tier_id": null,
  "duration_type": "permanent",
  "priority": "normal"
}
```

新请求金额：

```json
{
  "scope_type":"member",
  "scope_id":"7c5e6af2-aac6-465f-a2fb-4273a444c2b4",
  "period_type":"monthly",
  "dimension_type":"tier",
  "model_tier_id":"2f63d65a-a0f7-4abe-b9d0-3ace39a44f30",
  "duration_type":"current_period",
  "priority":"urgent",
  "current_amount_snapshot":"80.000000",
  "used_amount_snapshot":"79.500000",
  "requested_amount":120.000000,
  "reason":"生产发布需要高级模型"
}
```

非法 total+tier_id 或 tier+null 返回 422 `INVALID_EXPANSION_DIMENSION`；重复 pending 返回 409 `DUPLICATE_PENDING_EXPANSION`。本任务不改变批准事务语义，不创建 temporary override；完整幂等批准留给审批阶段计划。

兼容边界必须由 handler 测试锁定：旧字段 `current_amount`、`requested_amount` 输入与输出仍为 JSON number，禁止改成字符串；新增 `current_amount_snapshot`、`used_amount_snapshot` 以及其他新增 DTO 金额字段才使用 6 位 Decimal 字符串。数据库把旧两列扩大为 `numeric(18,6)` 不改变 Go 字段类型。

CallLog 在 P0 的边界仅为 Task 3 model、Task 4 Atlas migration 与 Task 18 contracts：等级字段为 `budget_tier_id`、`budget_tier_name_snapshot`、`budget_tier_accounting_eligible`，并提供 `budget_tier_constraints_snapshot jsonb not null default '[]'` 以承载 P1 写入的逐层 scope/tier/period/key/reset_at 事实；阻断字段严格为 `budget_block_code`、`budget_block_dimension`、`budget_block_scope_type`、`budget_block_scope_id`、`budget_block_period_type`。P0 不定义或引用 TierContext、Gate、BlockContext，不修改 UsageCollector，也不测试 Gateway 写入；实际分类、eligible、constraint snapshot 和预算 402 写入全部属于 P1。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/repository/expansion_repo.go internal/controlplane/service/alert_expansion_service.go internal/controlplane/handler/expansion_handler.go tests/controlplane/repository/expansion_repo_test.go tests/controlplane/repository/expansion_repo_dimension_test.go
go test ./tests/controlplane/repository -run Expansion -count=1
```

Expected: PASS；旧 total/permanent 请求及 JSON number 金额契约保持兼容，新等级维度请求被正确持久化；P0 没有新增 Gateway 写入依赖。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/expansion_repo.go internal/controlplane/service/alert_expansion_service.go internal/controlplane/handler/expansion_handler.go tests/controlplane/repository/expansion_repo_test.go tests/controlplane/repository/expansion_repo_dimension_test.go tests/contract/contracts.json
git commit -m "feat(expansion): 扩展等级额度申请领域字段"
```

---

### Task 16: 扩展 verify-key budget chain 与 effective overrides

**Files:**
- Modify: `internal/controlplane/service/key_service.go`
- Modify: `internal/controlplane/repository/budget_tier_repo.go`
- Modify: `internal/controlplane/repository/budget_repo.go`
- Modify: `tests/controlplane/service/key_service_test.go`
- Modify: `tests/contract/contracts.json`

- [ ] **Step 1: 写失败兼容与 effective limit 测试**

```go
func TestVerifyKeyBudgetChainKeepsTotalNumbersAndAddsTierDecimalStrings(t *testing.T) {
	svc := newKeyServiceWithBudgetReaders(t,
		monthlyAllocation(100),
		activeTotalOverride("120.000000"),
		tierLimit("80.000000"),
		activeTierOverride("100.000000"),
	)
	got := verifyActiveKey(t, svc)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, `{
	  "key_id":"00000000-0000-0000-0000-000000000001",
	  "status":"active","access_all_groups":false,"bypass_org_check":false,
	  "log_enabled":false,"user":{"user_id":"00000000-0000-0000-0000-000000000002","name":""},
	  "key_type":"personal",
	  "budget_chain":[{
	    "scope_type":"member","scope_id":"00000000-0000-0000-0000-000000000002",
	    "amount_usd":120,"daily_amount_usd":-1,"weekly_amount_usd":-1,"label":"我的额度",
	    "tier_limits":[{
	      "tier_id":"2f63d65a-a0f7-4abe-b9d0-3ace39a44f30","tier_name":"高级",
	      "period_type":"monthly","base_limit_usd":"80.000000",
	      "effective_limit_usd":"100.000000","enabled":true,
	      "effective_at":"2026-07-01T00:00:00+08:00",
	      "expires_at":"2026-08-01T00:00:00+08:00"
	    }]
	  }]
	}`, string(raw))
}

func TestBuildBudgetChainMergesOverridesByDimensionAndPeriod(t *testing.T) {
	got := verifyBudgetChain(t, totalOverridesByPeriod(), tierOverridesByPeriod())
	require.Equal(t, 12.0, got[0].DailyAmountUSD)
	require.Equal(t, 55.0, got[0].WeeklyAmountUSD)
	require.Equal(t, 120.0, got[0].AmountUSD)
	require.Equal(t, "100.000000", got[0].TierLimits[0].EffectiveLimitUSD.String())
}
```

另测 expired/cancelled override 被忽略、override 小于 base 时仍取 base、member 未配置总预算时仍跳过 member scope、service key 仍走 `dept_service → studio → company`。

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/service -run 'VerifyKeyBudgetChain|BuildBudgetChain' -count=1
```

Expected: FAIL，`BudgetScope.TierLimits`、tier reader 和 override 合并尚不存在。

- [ ] **Step 3: 写最小实现**

既有总预算字段严禁改名或改类型：

```go
type BudgetScope struct {
	ScopeType string `json:"scope_type"`
	ScopeID string `json:"scope_id"`
	AmountUSD float64 `json:"amount_usd"`
	DailyAmountUSD float64 `json:"daily_amount_usd"`
	WeeklyAmountUSD float64 `json:"weekly_amount_usd"`
	Label string `json:"label"`
	TierLimits []BudgetScopeTierLimit `json:"tier_limits,omitempty"`
}

type BudgetScopeTierLimit struct {
	TierID uuid.UUID `json:"tier_id"`
	TierName string `json:"tier_name"`
	PeriodType budgetdomain.PeriodType `json:"period_type"`
	BaseLimitUSD budgetdomain.Amount `json:"base_limit_usd"`
	EffectiveLimitUSD budgetdomain.Amount `json:"effective_limit_usd"`
	Enabled bool `json:"enabled"`
	EffectiveAt time.Time `json:"effective_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *KeyService) WithBudgetTierRepo(repo *repository.BudgetTierRepo) *KeyService
func (s *KeyService) buildBudgetChain(ctx context.Context, k *model.ApiKey, now time.Time) []BudgetScope
```

`VerifyKey` 改为调用 `buildBudgetChain(ctx, k, time.Now())`。每个 allocation 一次读取长期 tier limits 和 `starts_at <= now < expires_at AND status=active` 的通用 overrides：

- total override 按 period 合并到现有 `AmountUSD/DailyAmountUSD/WeeklyAmountUSD`，有效值为 `max(base, target_limit)`，仍序列化为 JSON number；`-1` 未配置语义保持不变。
- tier override 只匹配相同 allocation、tier、period，有效值为 `max(base_limit_usd, target_limit_usd)`，新增 `base_limit_usd/effective_limit_usd` 均为 6 位 Decimal 字符串。
- 无 override 时 effective 等于 base；disabled tier limit 仍下发 `enabled:false` 供 Gateway 跳过累计；只下发已配置 tier limits。

Repository 返回 model/repository DTO，不返回 service 类型。查询错误记录日志并保持最近兼容行为：总预算链仍可返回，tier limits 暂为空；不得把 tier 查询失败变成 key 鉴权失败。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/service/key_service.go internal/controlplane/repository/budget_tier_repo.go internal/controlplane/repository/budget_repo.go tests/controlplane/service/key_service_test.go
go test ./tests/controlplane/service -run 'VerifyKeyBudgetChain|BuildBudgetChain|KeyService' -count=1
```

Expected: PASS；旧总预算三个字段仍是 JSON number，tier limits 仅使用 Decimal 字符串，total/tier active override 均正确形成 effective limit。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/key_service.go internal/controlplane/repository/budget_tier_repo.go internal/controlplane/repository/budget_repo.go tests/controlplane/service/key_service_test.go tests/contract/contracts.json
git commit -m "feat(auth): 下发等级预算与有效临时额度"
```

---

### Task 17: 实现统一 BudgetLifecycleExecutor

**Files:**
- Modify: `internal/controlplane/repository/budget_tier_repo.go`
- Modify: `internal/controlplane/repository/budget_repo.go`
- Create: `internal/controlplane/service/budget_lifecycle_executor.go`
- Modify: `internal/controlplane/router/router.go`
- Create: `tests/controlplane/service/budget_lifecycle_executor_test.go`
- Modify: `tests/controlplane/repository/budget_tier_repo_test.go`

- [ ] **Step 1: 写失败的逐记录事务、状态与 observer 行为测试**

```go
type fakeLifecycleObserver struct {
	expiredOverrides []service.ExpiredBudgetOverride
	appliedSchedules []service.AppliedBudgetSchedule
	failedSchedules []service.FailedBudgetSchedule
}

func (f *fakeLifecycleObserver) OnOverrideExpired(_ context.Context, item service.ExpiredBudgetOverride) error {
	f.expiredOverrides = append(f.expiredOverrides, item)
	return nil
}
func (f *fakeLifecycleObserver) OnScheduleApplied(_ context.Context, item service.AppliedBudgetSchedule) error {
	f.appliedSchedules = append(f.appliedSchedules, item)
	return nil
}
func (f *fakeLifecycleObserver) OnScheduleFailed(_ context.Context, item service.FailedBudgetSchedule) error {
	f.failedSchedules = append(f.failedSchedules, item)
	return nil
}
var _ service.BudgetLifecycleObserver = (*fakeLifecycleObserver)(nil)

func TestBudgetLifecycleExecutorProcessesSchedulesAndOverrides(t *testing.T) {
	repo := newLifecycleRepo(
		dueTotalSchedule(), dueTierSchedule(),
		dueTotalOverride(), dueTierOverride(),
	)
	observer := &fakeLifecycleObserver{}
	executor := service.NewBudgetLifecycleExecutor(repo, fakeTotalWriter, fakeTierWriter, fakePublisher, observer)

	got, err := executor.RunOnce(ctx, now, 100)
	require.NoError(t, err)
	require.Equal(t, service.BudgetLifecycleResult{
		SchedulesApplied: 2, OverridesExpired: 2,
	}, got)
	require.Len(t, observer.appliedSchedules, 2)
	require.Len(t, observer.expiredOverrides, 2)
}

func TestBudgetLifecycleExecutorFailureUsesFreshFailedTransaction(t *testing.T) {
	repo := newLifecycleRepo(dueTierScheduleThatFails(), dueTotalSchedule())
	observer := &fakeLifecycleObserver{}
	got, err := executorWithRepoAndObserver(repo, observer).RunOnce(ctx, now, 100)
	require.Error(t, err)
	require.Equal(t, 1, got.SchedulesApplied)
	require.Equal(t, 1, got.SchedulesFailed)
	require.True(t, repo.applyTxRolledBack)
	require.NotEqual(t, repo.applyTxID, repo.markFailedTxID)
	require.False(t, repo.anyOperationUsedAbortedTx)
	require.Equal(t, budgetdomain.ScheduleFailed, repo.failedSchedule.Status)
	require.Len(t, observer.failedSchedules, 1)
}
```

同组测试覆盖 override 到期失败后在新事务标 `failed`、一个记录失败不阻断后续记录、publish 失败不回滚已提交状态；observer 只能在对应最终状态事务提交后回调，schedule 新事务成功标 failed 后调用 `OnScheduleFailed`，已回滚事务不得触发 applied/expired 回调。Repository 行为测试覆盖候选 ID 查询、逐 ID `FOR UPDATE SKIP LOCKED` 和并发不重复领取。

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/repository ./tests/controlplane/service -run BudgetLifecycle -count=1
```

Expected: FAIL，统一 executor、结果 DTO 和 observer 尚不存在。

- [ ] **Step 3: 写最小统一 executor、明确方法和 observer**

```go
type BudgetLifecycleResult struct {
	SchedulesApplied int
	SchedulesFailed int
	OverridesExpired int
	OverridesFailed int
}

type AppliedBudgetSchedule struct {
	ID uuid.UUID
	BudgetAllocationID uuid.UUID
	DimensionType budgetdomain.DimensionType
	ModelTierID *uuid.UUID
	PeriodType budgetdomain.PeriodType
	TargetLimitUSD budgetdomain.Amount
	AppliedAt time.Time
}

type ExpiredBudgetOverride struct {
	ID uuid.UUID
	BudgetAllocationID uuid.UUID
	DimensionType budgetdomain.DimensionType
	ModelTierID *uuid.UUID
	PeriodType budgetdomain.PeriodType
	ExpiredAt time.Time
	ExpansionRequestID *uuid.UUID
}

type FailedBudgetSchedule struct {
	ID uuid.UUID
	BudgetAllocationID uuid.UUID
	DimensionType budgetdomain.DimensionType
	ModelTierID *uuid.UUID
	PeriodType budgetdomain.PeriodType
	FailureReason string
	FailedAt time.Time
}

type BudgetLifecycleObserver interface {
	OnOverrideExpired(ctx context.Context, item ExpiredBudgetOverride) error
	OnScheduleApplied(ctx context.Context, item AppliedBudgetSchedule) error
	OnScheduleFailed(ctx context.Context, item FailedBudgetSchedule) error
}

func (e *BudgetLifecycleExecutor) RunOnce(ctx context.Context, now time.Time, batchSize int) (BudgetLifecycleResult, error)
func (e *BudgetLifecycleExecutor) Start(ctx context.Context, interval time.Duration)
func (e *BudgetLifecycleExecutor) executeSchedule(ctx context.Context, id uuid.UUID, now time.Time) (AppliedBudgetSchedule, error)
func (e *BudgetLifecycleExecutor) executeOverride(ctx context.Context, id uuid.UUID, now time.Time) (ExpiredBudgetOverride, error)
func (e *BudgetLifecycleExecutor) markScheduleFailed(ctx context.Context, id uuid.UUID, cause error) error
func (e *BudgetLifecycleExecutor) markOverrideFailed(ctx context.Context, id uuid.UUID, cause error) error
```

`RunOnce` 先取 due schedule/override ID；每个 ID 独立调用一次 `repo.Transaction`，事务内重新 `FOR UPDATE SKIP LOCKED`、复核 due 状态，再执行：

- schedule total：更新 allocation 对应日/周/月总预算；schedule tier：upsert tier limit；成功标 `applied`。
- override total/tier：到期都标 `expired`，effective limit 自然恢复当前 base。

任一记录执行失败时，该记录原事务必须返回 error 并完成 rollback；随后 `markScheduleFailed` 或 `markOverrideFailed` 必须开启**全新的 repository transaction** 写 `status=failed` 与截断后的 `failure_reason`。禁止在 aborted transaction 上继续 `UPDATE`，禁止一个记录失败回滚同批其他记录。所有记录处理完后，若至少一条成功提交，只调用一次 snapshot publisher；publisher 失败留 dirty，不回滚生命周期状态。

P0 提供上述固定三方法 observer 和 no-op 实现。P2 只能通过 `OnOverrideExpired` 接入站内信/邮件与“恢复基础额度”通知；审批/审计分别复用 `OnScheduleApplied`、`OnScheduleFailed`，不得改名、改为批量回调或创建第二个 schedule/override worker。Router 以可取消 context 每分钟运行，启动时先执行一次。

- [ ] **Step 4: 运行并确认通过**

Run:

```bash
gofmt -w internal/controlplane/repository/budget_tier_repo.go internal/controlplane/repository/budget_repo.go internal/controlplane/service/budget_lifecycle_executor.go internal/controlplane/router/router.go tests/controlplane/repository/budget_tier_repo_test.go tests/controlplane/service/budget_lifecycle_executor_test.go
go test ./tests/controlplane/repository ./tests/controlplane/service -run BudgetLifecycle -count=1
```

Expected: PASS；total/tier schedule 与 override 由同一 executor 处理，每条记录独立事务，失败只在新事务标 failed，observer 三方法只接收对应已提交的状态事件。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/budget_tier_repo.go internal/controlplane/repository/budget_repo.go internal/controlplane/service/budget_lifecycle_executor.go internal/controlplane/router/router.go tests/controlplane/repository/budget_tier_repo_test.go tests/controlplane/service/budget_lifecycle_executor_test.go
git commit -m "feat(budget): 统一预算生命周期执行器"
```

---

### Task 18: 固化 FE↔BE JSON 契约并做全量自检

**Files:**
- Modify: `tests/contract/contracts.json`
- Modify: `internal/controlplane/handler/errcodes.go`
- Modify: `tests/controlplane/handler/model_tier_handler_test.go`
- Modify: `tests/controlplane/handler/budget_tier_handler_test.go`
- Modify: `tests/controlplane/service/key_service_test.go`
- Modify: `tests/controlplane/service/budget_lifecycle_executor_test.go`

- [ ] **Step 1: 写失败 handler/service 行为回归测试**

通过真实 Gin router 发请求，验证成功响应、RBAC 和错误映射，不读取路由表或 handler 类型：

```go
func TestModelTierBudgetHandlersExposeFinalResponseContract(t *testing.T) {
	r := setupAuthorizedBudgetRouter(t)
	for _, tc := range []struct {
		method, path, body string
		wantStatus int
	}{
		{http.MethodGet, "/api/v1/model-tiers", "", http.StatusOK},
		{http.MethodGet, "/api/v1/budgets/" + allocationID.String() + "/tier-limits", "", http.StatusOK},
		{http.MethodPost, "/api/v1/budgets/tier-limits/batch/preview", validPreviewBody, http.StatusOK},
		{http.MethodPost, "/api/v1/budgets/tier-limits/batch/apply", validApplyBody, http.StatusOK},
		{http.MethodGet, "/api/v1/budgets/tier-usage?period_type=monthly", "", http.StatusOK},
		{http.MethodGet, "/api/v1/my/budget/tiers", "", http.StatusOK},
	} {
		w := doJSON(r, tc.method, tc.path, tc.body)
		require.Equal(t, tc.wantStatus, w.Code)
		require.JSONEq(t, expectedFixtureFor(tc.path), w.Body.String())
	}
}
```

handler fixture 必须覆盖 `temporary_target_usd` 以及 data 下的 `source_summary/effective_total/budget_chain/tier_limits/data_updated_at/stale`，并断言 JSON 不含 `source_mode/tiers/data_as_of`。`key_service_test.go` 继续验证 verify-key 总预算 number 与 tier Decimal string；`budget_lifecycle_executor_test.go` 验证统一 executor 的逐记录事务和固定三方法 observer。Call log 五字段由 model/Atlas/contracts 交付，不新增字段元数据或 Gateway writer 测试。

- [ ] **Step 2: 运行并确认失败**

Run:

```bash
go test ./tests/controlplane/handler ./tests/controlplane/service -run 'FinalResponseContract|VerifyKeyBudgetChain|BudgetLifecycle' -count=1
```

Expected: FAIL，最终 handler response、verify-key effective override 或统一 lifecycle 行为尚未满足。

- [ ] **Step 3: 写完整 JSON 契约**

新增顶层 `model_tier_budget`，至少包含：

```json
{
  "base_prefix": "/api/v1",
  "money_format": "decimal string (scale=6)",
  "legacy_expansion_money_format": {
    "current_amount": "JSON number (legacy)",
    "requested_amount": "JSON number (legacy)"
  },
  "period_type_enum": ["daily", "weekly", "monthly"],
  "dimension_type_enum": ["total", "tier"],
  "duration_type_enum": ["permanent", "current_period"],
  "gateway_402_error_codes": ["budget_exceeded", "tier_budget_exceeded"],
  "budget_period": {
    "timezone":"Asia/Shanghai",
    "week_start":"monday",
    "resolver_signature":"Resolve(now, periodType, config) -> (Period, error)"
  },
  "verify_key_budget_scope": {
    "legacy_total_number_fields":["amount_usd","daily_amount_usd","weekly_amount_usd"],
    "tier_limit_decimal_string_fields":["base_limit_usd","effective_limit_usd"],
    "effective_limit_rule":"max(base_limit, active_temporary_override_target)"
  },
  "lifecycle_executor": {
    "name":"BudgetLifecycleExecutor",
    "dimensions":["total","tier"],
    "records":["schedule","temporary_override"],
    "transaction_rule":"one record per transaction; mark failed in a fresh transaction",
    "observer":{
      "name":"BudgetLifecycleObserver",
      "methods":["OnOverrideExpired","OnScheduleApplied","OnScheduleFailed"]
    }
  },
  "snapshot_event": {
    "type":"budget_tier_snapshot_updated",
    "resource_type":"budget_tier_snapshot",
    "json_fields":["type","resource_type","resource_id","snapshot_version","timestamp"]
  },
  "call_log_block_fields": {
    "budget_block_code":{"go_type":"*string","max_length":64},
    "budget_block_dimension":{"go_type":"*string","max_length":8},
    "budget_block_scope_type":{"go_type":"*string","max_length":32},
    "budget_block_scope_id":{"go_type":"*string","max_length":128},
    "budget_block_period_type":{"go_type":"*string","max_length":8}
  },
  "snapshot_flags": [
    "master_enabled", "accounting_enabled", "positive_enforcement_enabled",
    "zero_block_enabled", "shadow_enabled", "v2_total_enforcement_enabled",
    "legacy_total_write_enabled"
  ],
  "model_tier_fields": [
    "id", "name", "description", "color", "sort_order",
    "enabled", "archived_at", "created_at", "updated_at"
  ],
  "tier_limit_fields": [
    "id", "budget_allocation_id", "model_tier_id", "model_tier_name",
    "period_type", "base_limit_usd", "temporary_target_usd",
    "effective_limit_usd", "source_summary", "enabled",
    "effective_at", "override_expires_at", "pending_schedule"
  ],
  "my_budget_tiers_fields": [
    "source_summary","effective_total","budget_chain","tier_limits","data_updated_at","stale"
  ],
  "my_budget_source_modes":["personal","shared","mixed"],
  "my_budget_forbidden_aliases":["source_mode","tiers","data_as_of"],
  "error_codes": {
    "MODEL_TIER_NOT_FOUND": 404,
    "MODEL_TIER_NAME_CONFLICT": 409,
    "MODEL_TIER_REFERENCED": 409,
    "MODEL_TIER_DISABLED": 422,
    "INVALID_MODEL_TIER_ORDER": 422,
    "BUDGET_ALLOCATION_NOT_FOUND": 404,
    "INVALID_BUDGET_PERIOD": 422,
    "INVALID_MONEY_AMOUNT": 422,
    "PENDING_BUDGET_SCHEDULE_EXISTS": 409,
    "INVALID_EXPANSION_DIMENSION": 422,
    "DUPLICATE_PENDING_EXPANSION": 409,
    "BUDGET_LIMIT_BELOW_USED": 409,
    "BUDGET_USAGE_UNAVAILABLE": 503,
    "BUDGET_CONFIG_SYNC_PENDING": 202
  }
}
```

每条 route 写出 auth permission、request body、成功状态、错误状态和 response fields。`GET tier-limits` 的空状态必须是 `"tier_limits":[]`，不得返回伪造的无限金额。batch 契约必须包含逐项 `valid|forbidden|invalid` preview 与 `applied|failed|skipped` apply 状态；tier usage 必须区分 `reported_cost_usd`/`accounting_cost_usd`；my budget 顶层 data 必须精确包含 `source_summary/effective_total/budget_chain/tier_limits/data_updated_at/stale`，mode 只允许 `personal|shared|mixed`，不得提供列出的旧别名。verify-key 契约明确旧 total number 不变、tier Decimal string 新增；统一 lifecycle executor 同时处理 schedule/override 和 total/tier。

- [ ] **Step 4: 运行全量验证和规格自检**

Run:

```bash
python3 -m json.tool tests/contract/contracts.json >/dev/null
go test ./tests/controlplane/... -count=1
go build ./...
atlas migrate validate --env local
rg -n '\x54\x42\x44|\x54\x4f\x44\x4f|\x{7c7b}\x{4f3c}\x{4e0a}\x{9762}|\x{672a}\x{5b9a}\x{5360}\x{4f4d}|\x{7167}\x{524d}\x{6587}\x{5904}\x{7406}' docs/superpowers/plans/2026-07-11-model-tier-budget-p0-data-controlplane.md
rg -n 'AutoMigrate|migrations/' docs/superpowers/plans/2026-07-11-model-tier-budget-p0-data-controlplane.md
```

Expected:

- JSON 校验成功；
- Go 测试和 build 全部通过；
- Atlas 输出迁移目录有效；
- 第一条占位词搜索无输出并以状态 1 结束；第二条只允许本文的否定性约束句出现 `AutoMigrate` 和 `migrations/`；
- `git diff --name-only` 只包含本计划文件地图列出的实施文件；
- `git diff --name-only -- migrations/` 无输出。

逐项人工确认：

- [ ] `ModelTier.ID`、所有 tier 外键、handler 参数均为 `uuid.UUID`；`Group.ID`/`group_id` 仍为 `uint`/`bigint`。
- [ ] 所有新增非负额度/用量字段统一为 `budgetdomain.Amount`、JSON 字符串和 `numeric(18,6)`；批量降额的有符号 `delta_usd` 仍是 6 位 Decimal 字符串。
- [ ] 既有 expansion `current_amount/requested_amount` Go 类型与 JSON number 不变，仅 DB 列扩大为 `numeric(18,6)`；新增 snapshot DTO 才使用 Decimal 字符串。
- [ ] `PeriodType` 只使用 `daily|weekly|monthly`，数据库、service、handler、contracts 拼写一致。
- [ ] 共享周期 API 精确保持 `Resolve(now, periodType, config) -> (Period, error)`，Control Plane 与后续 Gateway/P2 不另建周期算法。
- [ ] `DimensionType` 与 `DimensionKey` 在 limit/override/schedule/alert/expansion 中语义一致，total 不带 tier ID，tier 必带 tier ID。
- [ ] `model_groups.budget_tier_id` 与 `group_models.complexity_tier` 完全独立。
- [ ] 新增等级与额度写 API 都有 RBAC、作用域校验、稳定错误码和审计映射。
- [ ] model tier 引用统计覆盖 group、limit、override、schedule、alert、expansion、call_logs。
- [ ] 存量 group、budget、alert、expansion、call_logs 无需回填等级即可继续工作。
- [ ] 临时覆盖有效值使用 `max(base, override)`；计划替换保留旧行为 `replaced`。
- [ ] verify-key 的 `amount_usd/daily_amount_usd/weekly_amount_usd` 仍是 JSON number；新增 `tier_limits` 金额为 Decimal string；total/tier active override 都已合并。
- [ ] `BudgetLifecycleExecutor` 同时处理 total/tier schedule 与 override；每条记录独立事务，失败 rollback 后用新事务标 failed，未复用 aborted tx。
- [ ] lifecycle 成功提交后发布快照并失效 KeyCache；observer 方法固定为 `OnOverrideExpired/OnScheduleApplied/OnScheduleFailed`，P2 通知只通过 `OnOverrideExpired` 接入。
- [ ] 告警去重键包含 `period_type` 和确定性 `dimension_key`。
- [ ] `BudgetAlertRecord` 复合唯一索引的六个字段使用同一 `uniqueIndex:uniq_budget_alert_dimension` 与连续 priority。
- [ ] expansion 重复 pending 键包含 requester、scope、period、dimension、tier 和 duration。
- [ ] 只有 `atlas-migrations/` 有新迁移，`atlas.sum` 由 Atlas 生成，遗留 `migrations/` 未变。
- [ ] 所有 Go 测试文件都在 `tests/` 镜像目录；实施 diff 不新增或修改 `internal/**/*_test.go`。
- [ ] 快照只包含七个 canonical flags、`timezone=Asia/Shanghai`、`week_start=monday`，revision 单调，完整 map 发布后触发 `config:change` 和 KeyCache 失效。
- [ ] Control Plane 已提供可测试的 tier usage、my budget tiers、batch preview/apply handler/service，不只存在 contracts 声明。
- [ ] repository 包没有 import service，repository 接口不返回 `service.*` 类型。
- [ ] Gateway 公开 402 code 只允许小写 `budget_exceeded|tier_budget_exceeded`。
- [ ] call log 阻断字段严格只有 `budget_block_code/dimension/scope_type/scope_id/period_type`；P0 仅交付 schema/DTO/contracts，不修改 UsageCollector 或依赖 P1 上下文。
- [ ] snapshot publisher 通过现有 ChangeEvent JSON 发布 `budget_tier_snapshot_updated`，五个字段与 `infra.ChangeEvent` 一致，不发布裸字符串。
- [ ] `TierLimitView` 包含 `temporary_target_usd/source_summary`；`/my/budget/tiers` 顶层 data 精确包含 `source_summary/effective_total/budget_chain/tier_limits/data_updated_at/stale`，mode 枚举固定且无旧别名。
- [ ] P0 未开启 Gateway 等级拦截，旧总预算流程仍为唯一线上拦截路径。

- [ ] **Step 5: 提交**

```bash
git add tests/contract/contracts.json internal/controlplane/handler/errcodes.go tests/controlplane/handler/model_tier_handler_test.go tests/controlplane/handler/budget_tier_handler_test.go tests/controlplane/service/key_service_test.go tests/controlplane/service/budget_lifecycle_executor_test.go
git commit -m "test(contract): 固化模型等级预算控制面契约"
```

---

## 任务依赖与可并行性

- Task 1（Decimal/枚举）与 Task 2（周期/快照契约）可并行；二者完成后执行 Task 3（GORM models），再执行 Task 4（Atlas diff）。
- Task 5（等级 repository/service）与 Task 8（额度 repository）依赖 Task 3、4，可并行。
- Task 6（等级 API）依赖 Task 5；Task 7（组关联）依赖 Task 5、6。
- Task 9（额度 service/计划创建）依赖 Task 2、8；Task 10（单项额度 API）依赖 Task 6、9。
- Task 11（batch preview/apply）依赖 Task 10；Task 12（tier usage/my tiers）依赖 Task 2、3、8、9。
- Task 13（snapshot publisher）依赖 Task 2、5、7、9、10，并作为 P1 Gateway consumer 的硬前置。
- Task 14（alert）与 Task 15（expansion/call_logs P0 schema/DTO/contracts）依赖 Task 1、3、4，可在 Task 5–13 期间并行。
- Task 16（verify-key budget chain）依赖 Task 8、9，必须在 Task 13 KeyCache 失效链路验收时联测。
- Task 17（统一 lifecycle executor）依赖 Task 8、9、13、16；P2 通知明确依赖 `BudgetLifecycleObserver`，不得另建 schedule/override worker。
- Task 18 必须最后执行，统一验证 handler 行为、verify-key、lifecycle、兼容性、快照事件、Atlas、contracts 和全量 build。

## 规格覆盖索引

- Decimal 负数拒绝、新 DTO 字符串与旧 expansion number 兼容：Task 1、3、15、18。
- canonical flags、`budget.timezone`、ISO 周一、稳定 `Resolve` API 与 Gateway 小写错误码：Task 2、13、18。
- `model_tiers`、动态 CRUD、排序、启停、归档、安全删除：Task 3、5、6。
- `model_groups.budget_tier_id`、单个/批量关联、未分级：Task 3、4、7。
- `budget_tier_limits`、日/周/月、`$0`、正数、停用、保留 `effective_at`：Task 3、4、8、9、10。
- 通用 `budget_temporary_overrides` 与 `max(base, override)`：Task 3、4、8、9。
- 通用 `budget_limit_schedules`、替换 pending 保留审计、统一生命周期执行：Task 3、4、8、9、17。
- batch preview/apply 的逐项预览、部分应用、RBAC、审计：Task 11、18。
- 管理 tier usage 与用户 my budget tiers、来源脱敏：Task 12、18。
- 完整快照、revision、ChangeEvent JSON、`config:change`、KeyCache 失效、30 秒 reconcile：Task 2、13、17、18。
- `budget_alert_records` total/tier 维度、统一复合 tag 和 `period_type` 修复：Task 3、4、14、18。
- `expansion_requests` 新字段与旧 amount number 兼容：Task 3、4、15、18。
- `call_logs` 等级快照、accounting eligibility 与五个 `budget_block_*` P0 字段：Task 3、4、12、15、18。
- verify-key 旧 total number、tier Decimal string、total/tier effective override：Task 8、9、16、18。
- total/tier schedule 与 override 的统一逐记录事务 executor：Task 8、9、13、17、18。
- `temporary_target_usd`、source mode 及 `source_summary/effective_total/budget_chain/tier_limits/data_updated_at/stale` P2 契约：Task 9、10、12、18。
- repository 不返回 service 类型、分层无 import cycle：Task 5、8、12、13、16、17、18。
- `model_tier:view/manage`、预算作用域裁剪、403：Task 6、10、11、12。
- 审计动作、reason、before/after：Task 6、7、10、11、13。
- Atlas-only、Expand、hash/validate、禁止遗留迁移目录：Task 4、18。
- 所有 Go 测试位于 `tests/` 镜像目录：Task 1–18。

## P0 完成定义

全部 18 个任务的 checkbox 完成；目标测试、`go build ./...` 和 `atlas migrate validate --env local` 有当次运行的成功输出；contracts JSON 可解析；迁移无破坏性 DDL；旧 expansion 与 verify-key 总预算 JSON number 契约回归通过；新增 tier DTO 使用 Decimal 字符串；`BudgetLifecycleExecutor` 以逐记录事务执行 total/tier schedule 与 override；Control Plane 发布标准 ChangeEvent JSON 和完整快照，并提供 P2 所需预算响应。P0 不实现 Gateway 等级拦截、Redis v2 计数或 call log 实际写入，P1 负责 writer，P2 复用 lifecycle observer 实现通知。
