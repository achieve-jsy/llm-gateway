# EHR 组织数据单一来源 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 EHR 每日同步成为组织数据的唯一来源——同步以差量方式写入老表 `studios/departments/dept_studio_map`，删掉 7 个手工写入口，并在管理页提供带防重复的手动触发表单。

**Architecture:** 同步仍拉 EHR 全量（100 部门），但按业务键（`studios.ehr_department_id`、`departments.dept_id`）与库里比对后只写变化的行；改名只改属性，ID 终身不变，因此 `api_keys`/`user_roles`/`call_logs`/`usage_daily` 里的组织快照全部继续有效。删除用软删除（`departments.status='deleted'`、`studios.visible=false`），组织视图过滤、用量 join 不过滤。

**Tech Stack:** Go 1.2x + Gin + GORM(Postgres，测试用 sqlite) + Atlas 迁移 + Vue3/Element Plus + Vitest。

**Spec:** `docs/superpowers/specs/2026-11-03-ehr-org-single-source-design.md`

## Global Constraints

- 迁移只能"加可空列 / 加索引"，阶段 B 才允许 DROP；`atlas migrate hash` 必须重算，`atlas migrate validate` 由 CI `migrate-test` 执行
- 同步对老表**只 upsert，永不 delete + insert**；`studios.id` / `departments.id` 终身不变
- `studios.visible` 单向：只能置 `false`，永不自动置 `true`
- `departments` / `studios` **禁止**使用 `gorm.DeletedAt`（会污染用量 join）
- 事业部（EHR `type=2`）只写 `studios`，不进 `departments`
- 测试用 sqlite 建表必须逐字对齐真实列名（`tests/README.md` 约定），不得用 `AutoMigrate(&User{})` 之类带 `gen_random_uuid()` 默认值的模型
- 中文注释与提交信息（Conventional Commits，如 `feat(org): ...`）
- 每次改动后：`gofmt -l .` 无输出、`go build ./...` 通过、`go test ./tests/...` 通过；前端改动后 `cd web && npx vitest run`

## Review Focus

1. **改名不换 ID**：把"战龙工作室"改名为"战龙科技"，同步后 `studios.id` 必须不变，17 条 `api_keys.studio_id` 仍 join 得到。
2. **软删后用量仍可读**：部门被标 `deleted` 后，`usage_daily`/`call_logs` 的历史 join 仍返回部门名（若误用 `gorm.DeletedAt` 这里会整片变空）。
3. **重复触发**：连点、双标签页、cron 正在跑 —— 三种情况都必须只跑一次，返回 409 且按钮回到"同步中"。
4. **部分失败**：EHR 返回空/超时，`last_sync_status=failed` 且**不得**写入差量统计，也不得把部门全标 deleted。
5. **manual 工作室不复活**：`source='manual'` 或 `visible=false` 的行，同步不得把它们改回可见。

---

### Task 1: `studios` 承载 EHR 身份列

**Files:**
- Modify: `internal/orgservice/model/models.go`（`Studio` struct）
- Create: `atlas-migrations/20261104090000_studios_ehr_identity.sql`
- Modify: `atlas-migrations/atlas.sum`（`atlas migrate hash` 生成）

**Interfaces:**
- Produces: `model.Studio` 新字段 `EHRDepartmentID *string`、`Visible bool`、`Source string`、`LeaderName/LeaderPassport/LeaderEmployeeID string`

- [ ] **Step 1: 写迁移 SQL**

```sql
ALTER TABLE "studios"
  ADD COLUMN "ehr_department_id"  character varying(64) NULL,
  ADD COLUMN "visible"            boolean NOT NULL DEFAULT true,
  ADD COLUMN "source"             character varying(16) NOT NULL DEFAULT 'manual',
  ADD COLUMN "leader_name"        character varying(128) NOT NULL DEFAULT '',
  ADD COLUMN "leader_passport"    character varying(64) NOT NULL DEFAULT '',
  ADD COLUMN "leader_employee_id" character varying(64) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX "uniq_studios_ehr_dept" ON "studios" ("ehr_department_id")
  WHERE "ehr_department_id" IS NOT NULL;
```

- [ ] **Step 2: 模型同步加字段**（`Studio` struct，gorm tag 与上面列名/类型逐字一致；`EHRDepartmentID *string` 用指针以表达 NULL）

- [ ] **Step 3: 重算迁移哈希**

Run: `atlas migrate hash --env local`
Expected: `atlas.sum` 中该文件行被更新，无报错（本机无 docker 时 `validate` 会失败，属预期，交给 CI）

- [ ] **Step 4: 验证编译**

Run: `go build ./...`
Expected: 通过（无引用方受影响）

- [ ] **Step 5: Commit**

```bash
git add internal/orgservice/model/models.go atlas-migrations/
git commit -m "feat(org): studios 增加 EHR 身份列与可见性标记"
```

---

### Task 2: 一次性对账与回填脚本

**Files:**
- Create: `deploy/oneoff/2026-11-03-ehr-studio-dryrun.sql`
- Create: `deploy/oneoff/2026-11-03-ehr-studio-backfill.sql`

**Interfaces:**
- Consumes: 表 `ai_app_studios(department_id, name, visible)`、`studios(id, name, ehr_department_id, visible, source)`
- Produces: 给人看的四分类报告；人工确认后执行的回填语句

- [ ] **Step 1: 写 dry-run SQL**（四段 `SELECT`，每段带标题注释）

```sql
-- ① 精确同名（11 条预期）
SELECT s.id, s.name, a.department_id
  FROM studios s JOIN ai_app_studios a ON a.name = s.name
 WHERE s.ehr_department_id IS NULL;
-- ② 老表独有（7 条预期，其中 XLab 疑似 ↔ AI-Lab）
SELECT s.id, s.name FROM studios s
 WHERE s.ehr_department_id IS NULL
   AND NOT EXISTS (SELECT 1 FROM ai_app_studios a WHERE a.name = s.name);
-- ③ EHR 独有（4 条预期：AI-Lab / 广州公共美术中心 / 杭州集星社 / 棋牌技术中心）
SELECT a.department_id, a.name FROM ai_app_studios a
 WHERE NOT EXISTS (SELECT 1 FROM studios s WHERE s.ehr_department_id = a.department_id);
-- ④ 引用检查：将被隐藏的行是否被角色/Key 引用
SELECT s.name, count(DISTINCT ur.id) AS roles, count(DISTINCT k.id) AS keys
  FROM studios s
  LEFT JOIN user_roles ur ON ur.scope_type = 'studio' AND ur.scope_id = s.id::text
  LEFT JOIN api_keys  k  ON k.studio_id = s.id::text
 GROUP BY s.name HAVING count(DISTINCT ur.id) + count(DISTINCT k.id) > 0;
```

- [ ] **Step 2: 写回填 SQL**（三段：改名认领模板 / 人工隐藏 / 手工残留隐藏，注释保留"执行前须经人工确认"）

```sql
-- 改名认领（逐条确认后取消注释；示例：XLab 是 AI-Lab 的旧名）
-- UPDATE studios SET ehr_department_id = '<EHR事业部ID>' WHERE id = '<老工作室uuid>';

-- EHR 里存在但人工隐藏：仍是 source='ehr'，同步照常更新其 name
UPDATE studios SET visible = false WHERE name IN ('元气工作室','公共关系事业部');

-- 老表独有的手工残留：不再属于任何同步来源
UPDATE studios SET visible = false, source = 'manual'
 WHERE name IN ('test','测试工作室AI','互娱事业部','创新事业部','战略发展中心','数字内容创新工作室');
```

- [ ] **Step 3: 在 test 库跑 dry-run，核对四段条数**

Run: `psql "$DATABASE_URL" -f deploy/oneoff/2026-11-03-ehr-studio-dryrun.sql`
Expected: ①11 条、②7 条、③4 条、④列出被引用的隐藏候选（当前预期为空或仅测试残留）

- [ ] **Step 4: Commit**（只提交脚本，不执行回填）

```bash
git add deploy/oneoff/
git commit -m "chore(org): EHR 工作室对账 dry-run 与回填脚本"
```

---

### Task 3: EHR 投影仓储（差量写老表）

**Files:**
- Create: `internal/orgservice/repository/ehr_projection_repo.go`
- Test: `tests/orgservice/repository/ehr_projection_repo_test.go`

**Interfaces:**
- Consumes: `model.Studio`、`model.OrgDepartment`、`model.DeptStudioMap`
- Produces:
  - `type EHRStudioInput struct { EHRDepartmentID, Name, LeaderName, LeaderPassport, LeaderEmployeeID string }`
  - `type EHRDepartmentInput struct { DeptID, DeptName string; StudioID *uuid.UUID; Status string }`
  - `func NewEHRProjectionRepo(db *gorm.DB) *EHRProjectionRepo`
  - `func (r *EHRProjectionRepo) UpsertStudio(ctx context.Context, in EHRStudioInput) (string, error)` — 返回 `"inserted" | "updated" | "unchanged"`
  - `func (r *EHRProjectionRepo) UpsertDepartment(ctx context.Context, in EHRDepartmentInput) (string, error)`
  - `func (r *EHRProjectionRepo) UpsertMapping(ctx context.Context, deptID string, studioID uuid.UUID, updatedBy string) (string, error)`
  - `func (r *EHRProjectionRepo) HideStudiosNotIn(ctx context.Context, keepEHRDeptIDs []string) (int, error)`
  - `func (r *EHRProjectionRepo) ArchiveDepartmentsNotIn(ctx context.Context, keepDeptIDs []string) (int, error)`

- [ ] **Step 1: 写失败测试**（sqlite；建表语句逐字对齐真实列，含 Task 1 新列）

断言清单（`TestEHRProjection` 内 5 个子测试）：
```go
// 1) 首次投影 → inserted，且 visible=true, source='ehr'
// 2) 同输入再跑一次 → unchanged（差量为 0 的保证）
// 3) 改 Name 后 → updated，且 studios.id 不变（记下第一次的 id 比对）
// 4) source='manual' 的行 → 同步不覆盖，返回 unchanged
// 5) 手工置 visible=false 后，UpsertStudio 带新 Name → 只改 name，visible 仍为 false
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/orgservice/repository/ -run TestEHRProjection -v`
Expected: FAIL（`undefined: NewEHRProjectionRepo`）

- [ ] **Step 3: 实现 `UpsertStudio`**

按 `ehr_department_id` 查行：
- 未找到 → INSERT（`source='ehr'`, `visible=true`）→ `"inserted"`
- 找到且 `source='manual'` → 直接 `"unchanged"`（不覆盖）
- 找到且 `source='ehr'` → 只写出与入参不同的列（`name / leader_*`，**不碰 visible**）→ 有变化 `"updated"`，否则 `"unchanged"`

- [ ] **Step 4: 实现 `UpsertDepartment` / `UpsertMapping`**

按 `dept_id` 查 `departments`：不存在 → INSERT（`status=active`，`studio_id=in.StudioID`）；存在 → 只更新 `dept_name / studio_id / status` 中变化的字段（`status='deleted'` 的行被同步重新看到时改回 `active`）。
`dept_studio_map`：按 `dept_id` upsert，`source='ehr'`、`updated_by` 用入参，`updated_at` 自动。

- [ ] **Step 5: 实现两个对账方法**

`HideStudiosNotIn`：`UPDATE studios SET visible=false WHERE source='ehr' AND ehr_department_id NOT IN (...) AND visible=true`，返回影响行数。
`ArchiveDepartmentsNotIn`：`UPDATE departments SET status='deleted' WHERE status <> 'deleted' AND dept_id NOT IN (...)`，返回影响行数。
（keep 列表为空时直接返回 0 —— 防止 EHR 异常返回空把整库标删）

- [ ] **Step 6: 跑测试确认通过 + Commit**

Run: `go test ./tests/orgservice/repository/ -run TestEHRProjection -v` → PASS
```bash
git add internal/orgservice/repository/ehr_projection_repo.go tests/orgservice/repository/ehr_projection_repo_test.go
git commit -m "feat(org): 新增 EHR 投影仓储（差量 upsert 老表）"
```

---

### Task 4: 同步服务接入投影并输出差量

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_sync.go`（`HubSyncResult`、`Sync`、`markStatus`；新增 `projectOrg`）
- Modify: `internal/controlplane/router/router.go`（构造 `NewAIAppHubSyncService` 处注入 projection repo）
- Test: `tests/controlplane/service/ai_app_hub_projection_test.go`

**Interfaces:**
- Consumes: Task 3 的 `EHRProjectionRepo` 全部方法
- Produces: `HubSyncResult.Diff` 字段（`type SyncDiff struct { Studios, Departments ProjectionDiff }`，`ProjectionDiff struct{ Inserted, Updated, Deleted int }`）；`AIAppHubSyncService.Running() bool`

- [ ] **Step 1: 写失败测试**

```go
// 第一次 Sync(scope=all)：Diff.Studios.Inserted > 0，老表 studios 出现 3 行（fake fetcher 给 3 个事业部）
// 第二次 Sync：Diff.Studios == {0,0,0} 且 Diff.Departments == {0,0,0}
// EHR 少返回一个事业部：Diff.Studios.Deleted == 1，且该行仍在表里（visible=false）
// EHR 返回空列表：Sync 返回 ErrEmptyResult，且 departments 中无任何行被标 deleted
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/controlplane/service/ -run TestSyncProjectsToLegacyTables -v`
Expected: FAIL（`HubSyncResult.Diff` 不存在 / 老表无数据）

- [ ] **Step 3: 实现 `projectOrg`**

在 `Sync()` 的 `syncStudios` 之后调用（scope=all 才做对账；`department`/`studio` 只 upsert 目标子树）：

```go
func (s *AIAppHubSyncService) projectOrg(ctx context.Context, items []ehrclient.Department,
    studioIDs map[string]uuid.UUID, byID map[string]ehrclient.Department,
    targets map[string]bool, fullScope bool) (SyncDiff, error)
```

遍历 `items`：`Type==2` → `UpsertStudio`；否则 → 用 `nearestStudioID` 算 `StudioID`，`Status` 取 `active`（有归属）或 `pending_studio`（无归属），再 `UpsertDepartment` + 有归属时 `UpsertMapping`。`fullScope` 时再调两个对账方法，keep 列表用本轮所有 `items` 的 id。

- [ ] **Step 4: 差量写进结果与状态**

`HubSyncResult` 加 `Diff SyncDiff \`json:"diff"\``；`markStatus` 成功分支额外写一个键 `ai_app_hub.last_diff`（JSON 字符串，便于状态接口原样返回）。

- [ ] **Step 5: 加 `Running()`**

```go
func (s *AIAppHubSyncService) Running() bool {
    if s.mu.TryLock() { s.mu.Unlock(); return false }
    return true
}
```

- [ ] **Step 6: 跑测试 + 全量回归 + Commit**

Run: `go test ./tests/controlplane/service/ -count=1` → PASS
```bash
git add internal/controlplane/service/ai_app_hub_sync.go internal/controlplane/router/router.go tests/controlplane/service/ai_app_hub_projection_test.go
git commit -m "feat(org): 同步阶段投影到老表并输出差量统计"
```

---

### Task 5: 同步状态查询接口

**Files:**
- Create: `internal/controlplane/handler/ai_app_hub_status_handler.go`
- Modify: `internal/controlplane/router/router.go:543`（加一行路由）
- Test: `tests/controlplane/handler/ai_app_hub_status_test.go`

**Interfaces:**
- Consumes: `AIAppHubSyncService.Authorize` / `Running()`、`repository.SettingsRepo` 读取 `ai_app_hub.*`
- Produces: `GET /api/v1/admin/ai-app-hub/sync-status` → `{"last_sync_at","status","scope","error","departments","employees","unmapped_departments","resigned_accounts_disabled","diff","running"}`

- [ ] **Step 1: 写失败测试**

```go
// admin 调用 → 200，body 含 running=false 与上次同步字段
// 权限不足（Authorize 返回 false）→ 403
// repo 中 mu 被占（并发 Sync 中）→ running=true
// settings 里没有任何 ai_app_hub.* 键 → 200 且 status=""（页面显示"尚未同步"）
```

- [ ] **Step 2: 跑测试确认失败** → `go test ./tests/controlplane/handler/ -run TestSyncStatus` → FAIL(404)

- [ ] **Step 3: 实现 handler**（admin 校验复用 `Authorize(ctx, userID, passport, SyncScope{Kind: ScopeAll})`，不新增 RBAC perm）

- [ ] **Step 4: 注册路由**

```go
adminGroup.GET("/admin/ai-app-hub/sync-status", hubSyncHandler.Status)
```

- [ ] **Step 5: 跑测试 + Commit**

```bash
git add internal/controlplane/handler/ai_app_hub_status_handler.go internal/controlplane/router/router.go tests/controlplane/handler/ai_app_hub_status_test.go
git commit -m "feat(org): 同步状态查询接口"
```

---

### Task 6: 手动触发的前端（状态卡 + 三层防重复）

**Files:**
- Modify: `web/src/stores/org.ts`（新增 `syncStatus`、`fetchSyncStatus()`、`triggerSync()`）
- Modify: `web/src/views/admin/org/OrgTreePage.vue`（`<template>` 顶部插入同步卡）
- Test: `web/src/views/admin/org/__tests__/SyncCard.spec.ts`

**Interfaces:**
- Consumes: Task 5 的状态接口；已有 `POST /api/v1/admin/ai-app-hub/sync`（`{scope:'all'}`）
- Produces: `useOrgStore().syncStatus`、`fetchSyncStatus()`、`triggerSync()`

- [ ] **Step 1: 写失败测试**（Vitest + `@vue/test-utils`，mock `http`）

```ts
// 1) 状态 running=true → 按钮文案「同步中…」且 disabled
// 2) 点击「立即同步」后请求未返回 → 按钮 disabled（连续点击只发一次请求）
// 3) 请求返回 409 → 按钮文案回到「同步中…」，提示语含「同步进行中」
// 4) 成功后 → 显示"上次同步"与服务端返回的部门/人员数
```

- [ ] **Step 2: 跑测试确认失败** → `cd web && npx vitest run src/views/admin/org/__tests__/SyncCard.spec.ts` → FAIL

- [ ] **Step 3: store 增加两方法**

```ts
const syncStatus = ref<SyncStatus | null>(null)
async function fetchSyncStatus() // GET /api/v1/admin/ai-app-hub/sync-status
async function triggerSync()     // POST /api/v1/admin/ai-app-hub/sync { scope: 'all' }，timeout 60000
```

- [ ] **Step 4: 页面卡片 + 防重复**

`onMounted` 调 `fetchSyncStatus()`；`syncing = computed(() => syncStatus.value?.running || localSyncing.value)`；按钮 ` :disabled="syncing"`、文案 `syncing ? '同步中…' : '立即同步'`；点击后 `localSyncing=true`，`finally` 里复位并重新拉状态；409 走 `catch` 分支设 `localSyncing=true` 直到状态接口返回 `running=false`（`setTimeout` 5s 轮查两次，不做长轮询）。

- [ ] **Step 5: 跑测试 + 构建 + Commit**

Run: `npx vitest run src/views/admin/org/` 与 `npx vite build`
```bash
git add web/src/stores/org.ts web/src/views/admin/org/OrgTreePage.vue web/src/views/admin/org/__tests__/SyncCard.spec.ts
git commit -m "feat(web): 组织页同步状态卡与手动触发按钮"
```

---

### Task 7: 审计留痕确认

**Files:**
- Test: `tests/controlplane/handler/ai_app_hub_sync_audit_test.go`（仅当中间件未覆盖时改 `internal/controlplane/middleware/audit*.go`）

**Interfaces:**
- Consumes: `middleware.AuditMiddleware`（`router.go:539` 已挂在 `adminGroup`）

- [ ] **Step 1: 写测试断言手动同步留下审计行**

```go
// POST /api/v1/admin/ai-app-hub/sync (admin) 后，audit_logs 中存在 action 含该路径、actor=调用者的行
```

- [ ] **Step 2: 跑测试**

Run: `go test ./tests/controlplane/handler/ -run TestManualSyncAudited -v`
Expected: PASS（中间件已覆盖）→ 只提交测试；若 FAIL 则在同一中间件注册处补 action 名映射后重跑。

- [ ] **Step 3: Commit**

```bash
git add tests/controlplane/handler/ai_app_hub_sync_audit_test.go
git commit -m "test(org): 手动同步审计留痕回归"
```

---

### Task 8: 拆除后端 7 个手工写入口

**Files:**
- Modify: `internal/controlplane/router/router.go:1054-1086`（删 7 条路由）
- Delete: `internal/orgservice` 下对应方法（`org_handler.DeleteStudio`、`mapping_handler.SetDeptStudio/ImportMappings`、`employee_import_handler.Import`、`dept_assign_handler.ChangeDepartment/FollowSSO` 及只被它们调用的 service 方法）
- Modify: `internal/controlplane/router/router.go`（不再构造这些 handler 的引用；保留 `ListStudios/ListDepartments` 等只读接口）
- Test: `tests/controlplane/handler/org_manual_endpoints_gone_test.go`

**Interfaces:**
- Consumes: 无
- Produces: 路由表不再包含 `POST /org/studios`、`DELETE /org/studios/:id`、`PUT /org/departments/:id/studio`、`POST /org/dept-studio-map/import`、`POST /org/employees/import`、`PUT /org/users/:id/department`、`PUT /org/users/:id/department/follow-sso`

- [ ] **Step 1: 写失败测试**

```go
// 对每个已删路由发请求（带 admin JWT）→ 404 或 405，且响应体不是业务成功
// GET /org/studios、GET /org/departments 仍 200
```

- [ ] **Step 2: 跑测试确认失败**（当前应返回 200/400 而非 404）

- [ ] **Step 3: 删路由与 handler 方法**（`PermStudioCreate/PermStudioDelete/PermMappingEdit/PermEmployeeImport/PermDeptChange/PermSSORevert` 的定义**保留**，只是不再被 guard 引用）

- [ ] **Step 4: 清理死代码**

Run: `go build ./... && go vet ./...`
Expected: 通过；若出现未使用告警，删除对应 service 方法（不要用 `_ = x` 绕过）

- [ ] **Step 5: 跑测试 + Commit**

```bash
git add -A
git commit -m "refactor(org): 移除组织数据的手工写入口，只保留 EHR 同步"
```

---

### Task 9: 拆除前端手工入口

**Files:**
- Modify: `web/src/views/admin/org/OrgTreePage.vue`（删导入员工按钮/弹窗、新建/删除工作室、编辑映射）
- Modify: `web/src/stores/org.ts`（删 `importMappings`、`setDeptStudio`、`createStudio`、`deleteStudio`、导入员工相关方法）
- Test: `web/src/views/admin/org/__tests__/OrgTreePage.noManualWrite.spec.ts`

**Interfaces:**
- Consumes: Task 8 已删的路由
- Produces: 组织页只读 + 同步卡

- [ ] **Step 1: 写失败测试**

```ts
// 页面渲染后：不存在「导入员工」「新建工作室」「删除」「编辑映射」按钮（按 data-test 选择器断言 undefined）
```

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 删模板区块、事件处理、store 方法**

- [ ] **Step 4: 跑测试 + `npx vite build` + Commit**

```bash
git add web/
git commit -m "refactor(web): 移除组织手工维护入口"
```

---

### Task 10（阶段 B）: 拆掉 `ai_app_studios` / `ai_app_departments`

**前置**：Task 1–9 已上线并观察 ≥2 天，差量统计正常，页面数据与 EHR 一致。

**Files:**
- Create: `atlas-migrations/20261112090000_drop_ai_app_org_tables.sql`
- Modify: `internal/controlplane/model/ai_app_hub.go`（删 `AIAppStudio`、`AIAppDepartment`）
- Modify: `internal/controlplane/repository/ai_app_hub_repo.go`（删 `InsertStudioIfAbsent/UpdateStudioEHRFields/GetStudioByID/GetStudioByDepartmentID/ListStudios/UpsertDepartment/GetDepartment/ListDepartments/SetDepartmentStudio/HideStudiosNotInDepartments`）
- Modify: `internal/controlplane/service/ai_app_hub_sync.go`（删 `syncStudios` 里写新表的部分，改为只算 `studioIDs` 供投影使用）
- Test: `tests/controlplane/service/ai_app_hub_sync_test.go`（改断言到老表）

**Interfaces:**
- Produces: 组织数据只剩一套表；`ehr_employees` 保留

- [ ] **Step 1: 迁移 SQL**

```sql
DROP TABLE IF EXISTS "ai_app_departments";
DROP TABLE IF EXISTS "ai_app_studios";
```

- [ ] **Step 2: 改测试断言到老表 → 跑测试确认失败**

Run: `go test ./tests/controlplane/service/ -count=1` → FAIL（仍引用新表模型）

- [ ] **Step 3: 删模型/仓储/同步分支**，`studioIDs` 仍由 `ai_app_studios` 逻辑推导 → 改为在投影阶段生成 `map[deptID]uuid.UUID`（首次 INSERT 后取回 id）

- [ ] **Step 4: 跑全量测试 + Commit**

Run: `go test ./tests/... -count=1` → PASS
```bash
git add -A
git commit -m "refactor(org): 下线 ai_app_studios/ai_app_departments，组织数据单一来源"
```

---

## 验收清单（test 环境）

- [ ] 组织树：100 个部门（85 显示 + 15 事业部以工作室形式展示）、工作室 15 个（含 2 隐藏）、未归属 7 个
- [ ] 连续两次同步，第二次 `diff` 全 0
- [ ] 改一个工作室名后同步：`studios.id` 不变，用量页统计不分裂
- [ ] EHR 删一个部门后同步：该部门 `status='deleted'`，行仍在，历史 `call_logs` 仍显示部门名
- [ ] 连点「立即同步」10 次：只产生 1 次同步，其余 409
- [ ] 刷新页面：`running=true` 时按钮显示「同步中…」且不可点
- [ ] 7 个手工接口全部 404，只读接口正常
