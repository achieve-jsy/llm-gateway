# AI 应用聚合页同步子系统实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 提供全量 / 单部门 / 单工作室三种同步，通过一个 HTTP 接口触发，并复用于定时任务。

**Architecture:** 三层单向依赖——`ehrclient`（HTTP 拉取）→ `AIAppHubSyncService`（按 Scope 过滤、落库、写状态）→ 触发器（HTTP handler / cron）。所有触发方式共用 `Sync(ctx, scope)`，并发用进程内锁，状态写 `system_configs`。

**Tech Stack:** Go + Gin + GORM(Postgres) + testify + sqlite（仓储/服务单测）+ httptest（EHR 客户端）。

**Spec:** `docs/superpowers/specs/2026-10-28-ai-app-hub-sync-design.md`

## Global Constraints

- **不改 schema**：只用已提交的 `ai_app_studios` / `ai_app_departments` / `ai_apps` / `ai_app_hub_login_logs`。
- **只发一次请求**：EHR 没有单节点接口，三种范围都调 `departments?orgId=1428195512246472704`，差别只在写库范围。
- **字段级覆盖**：`name / leader_*` 每次以 EHR 为准；`visible / sort_order` 只在新建时写；`source='manual'` 的行整行跳过。
- 规则常量：隐藏 `0B3CF753-9D6D-46E8-89C2-8E752D2A460F`（公共关系事业部）、`08C6856B-D0D5-4FC4-830E-7D13441EA89F`（元气工作室）；改名 `53E83585-6CE3-48EF-8255-520BBAFCB156` → **技术研究工作室**。
- 单次同步超时 30s；全量/单工作室/单部门同一把进程内锁。
- 同步状态键：`ai_app_hub.last_sync_at` / `last_sync_status` / `last_sync_scope` / `last_sync_error`（仅失败时）。
- 代码与注释用中文；提交信息用 Conventional Commits。
- 本计划**替代** `docs/superpowers/plans/2026-10-28-ai-app-hub-backend.md` 的 Task 1（工作室/部门仓储）、Task 3（EHR 客户端）、Task 4（同步服务）、Task 10（同步触发），其余任务不变。

## Review Focus

- **type2 当部门同步**：`scope=department` 传的是事业部节点时，必须同时更新 `ai_app_studios` 那一行，否则负责人改了却不生效。
- **手工工作室**：`scope=studio` 指向 `department_id` 为 NULL 的手工工作室时返回 404，不能拿 NULL 去请求 EHR。
- **空 items**：EHR 返回 `{"items":[]}` 视为上游异常（500 变体），**不得**当成"组织被清空"处理。
- **重复点击**：两个请求同时到达，第二个返回 409，且不能写坏数据。
- **visible 不被覆盖**：运营把某工作室设为不展示后，再次同步仍然是 false。

---

### Task 1: 工作室与部门仓储

**Files:**
- Create: `internal/controlplane/repository/ai_app_hub_repo.go`
- Test: `tests/controlplane/repository/ai_app_hub_repo_test.go`

**Interfaces:**
- Consumes: `model.AIAppStudio` / `model.AIAppDepartment`（已提交）
- Produces:
  - `func NewAIAppHubRepo(db *gorm.DB) *AIAppHubRepo`
  - `func (r *AIAppHubRepo) InsertStudioIfAbsent(ctx context.Context, s *model.AIAppStudio) (inserted bool, err error)`
  - `func (r *AIAppHubRepo) UpdateStudioEHRFields(ctx context.Context, id uuid.UUID, name, leaderName, leaderPassport, leaderEmployeeID string) error`
  - `func (r *AIAppHubRepo) GetStudioByID(ctx context.Context, id uuid.UUID) (*model.AIAppStudio, error)`
  - `func (r *AIAppHubRepo) GetStudioByDepartmentID(ctx context.Context, deptID string) (*model.AIAppStudio, error)`
  - `func (r *AIAppHubRepo) ListStudios(ctx context.Context, onlyVisible bool) ([]model.AIAppStudio, error)`
  - `func (r *AIAppHubRepo) UpsertDepartment(ctx context.Context, d *model.AIAppDepartment) error`
  - `func (r *AIAppHubRepo) GetDepartment(ctx context.Context, deptID string) (*model.AIAppDepartment, error)`
  - `func (r *AIAppHubRepo) ListDepartments(ctx context.Context) ([]model.AIAppDepartment, error)`
  - `func (r *AIAppHubRepo) SetDepartmentStudio(ctx context.Context, deptID string, studioID *uuid.UUID) error`

- [ ] **Step 1: 写失败测试**

```go
func TestInsertStudioIfAbsentDoesNotOverwrite(t *testing.T) {
    db := newAIHubTestDB(t)
    repo := repository.NewAIAppHubRepo(db)
    dept := "0B3CF753-9D6D-46E8-89C2-8E752D2A460F"
    first := &model.AIAppStudio{Name: "公共关系事业部", DepartmentID: &dept, Visible: false}
    inserted, err := repo.InsertStudioIfAbsent(context.Background(), first)
    require.NoError(t, err)
    require.True(t, inserted)

    second := &model.AIAppStudio{Name: "改名了", DepartmentID: &dept, Visible: true}
    inserted, err = repo.InsertStudioIfAbsent(context.Background(), second)
    require.NoError(t, err)
    require.False(t, inserted)

    got, err := repo.GetStudioByDepartmentID(context.Background(), dept)
    require.NoError(t, err)
    require.Equal(t, "公共关系事业部", got.Name)
    require.False(t, got.Visible)
}

func TestUpdateStudioEHRFieldsLeavesVisibility(t *testing.T) {
    // 建一条 visible=false 的行，调用 UpdateStudioEHRFields 改 name/leader
    // 断言 name 变了、visible 仍是 false
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/repository/ -run 'TestInsertStudioIfAbsent|TestUpdateStudioEHRFields' -v`
Expected: FAIL（`NewAIAppHubRepo` 未定义）

- [ ] **Step 3: 实现**

`InsertStudioIfAbsent` 用 `clause.OnConflict{Columns: []clause.Column{{Name: "department_id"}}, DoNothing: true}`，通过 `RowsAffected > 0` 判断是否真的插入。`UpdateStudioEHRFields` 用 `Updates(map[string]any{...})` 只带 4 个 EHR 字段。`SetDepartmentStudio` 只更新 `studio_id`。sqlite 建表按 model 字段手写 DDL（参考 `tests/controlplane/repository/budget_tier_repo_test.go`），UUID 用固定字符串。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/repository/ -run 'TestInsertStudioIfAbsent|TestUpdateStudioEHRFields' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/ai_app_hub_repo.go tests/controlplane/repository/ai_app_hub_repo_test.go
git commit -m "feat(hub): 工作室与部门仓储"
```

---

### Task 2: EHR 部门客户端

**Files:**
- Create: `internal/aiapphub/ehrclient/client.go`
- Test: `tests/aiapphub/ehrclient/client_test.go`

**Interfaces:**
- Produces:
  - `type Config struct { BaseURL, Prefix, Token, OrgID string; Timeout time.Duration }`
  - `func New(cfg Config) *Client`
  - `func (c *Client) FetchDepartments(ctx context.Context) ([]Department, error)`
  - `type Department struct { ID, Name, ParentID, Path, LeaderName, LeaderPassport, LeaderEmployeeID string; Type int }`
  - `var ErrUnauthorized = errors.New("ehr unauthorized")`
  - `var ErrUpstream = errors.New("ehr upstream")`
  - `var ErrEmptyResult = errors.New("ehr empty result")`

- [ ] **Step 1: 写失败测试**

```go
func TestFetchDepartmentsParsesItems(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.Equal(t, "/open-apis/ehr-apic/open-apis/resource/v1/departments", r.URL.Path)
        require.Equal(t, "1428195512246472704", r.URL.Query().Get("orgId"))
        require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
        io.WriteString(w, `{"items":[{"departmentId":"d1","departmentName":"AI-Lab","parentId":"00000000-0000-0000-0000-000000000000","departmentType":2,"leaderName":"何治国","leaderPassport":"hezhiguo","leaderEmployeeId":"e1"}]}`)
    }))
    defer srv.Close()
    c := ehrclient.New(ehrclient.Config{BaseURL: srv.URL, Prefix: "/open-apis/ehr-apic/open-apis/resource/v1", Token: "tok", OrgID: "1428195512246472704"})
    got, err := c.FetchDepartments(context.Background())
    require.NoError(t, err)
    require.Len(t, got, 1)
    require.Equal(t, "AI-Lab", got[0].Name)
    require.Equal(t, 2, got[0].Type)
    require.Equal(t, "hezhiguo", got[0].LeaderPassport)
}

func TestFetchDepartmentsErrors(t *testing.T) {
    // 403 → errors.Is(err, ehrclient.ErrUnauthorized)
    // 500 → errors.Is(err, ehrclient.ErrUpstream)
    // {"items":[]} → errors.Is(err, ehrclient.ErrEmptyResult)
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/aiapphub/ehrclient/ -v`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现**

URL 为 `BaseURL + Prefix + "/departments?orgId=" + url.QueryEscape(OrgID)`；`http.Client` 超时取 `Config.Timeout`，为 0 时默认 30s；响应体读上限 4MB；状态码 401/403 → `ErrUnauthorized`，其余非 200 → `ErrUpstream`，JSON 解析失败 → `ErrUpstream`；`len(items) == 0` → `ErrEmptyResult`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/aiapphub/ehrclient/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/aiapphub/ehrclient/ tests/aiapphub/ehrclient/
git commit -m "feat(hub): EHR 部门接口客户端"
```

---

### Task 3: 同步核心（all 范围 + 规则 + 状态）

**Files:**
- Create: `internal/controlplane/service/ai_app_hub_sync.go`
- Test: `tests/controlplane/service/ai_app_hub_sync_test.go`

**Interfaces:**
- Consumes: Task 1 的仓储、Task 2 的 `DepartmentFetcher` 形状、`repository.SettingsRepo`
- Produces:
  - `type ScopeKind string`，常量 `ScopeAll ScopeKind = "all"`、`ScopeDepartment = "department"`、`ScopeStudio = "studio"`
  - `type SyncScope struct { Kind ScopeKind; ID string }`
  - `type SyncResult struct { Scope string; Departments, Studios, Hidden int; DurationMs int64 }`
  - `type SyncRules struct { HiddenDepartmentIDs map[string]bool; NameOverrides map[string]string }`
  - `func DefaultSyncRules() SyncRules`
  - `type DepartmentFetcher interface { FetchDepartments(ctx context.Context) ([]ehrclient.Department, error) }`
  - `func NewAIAppHubSyncService(f DepartmentFetcher, repo *repository.AIAppHubRepo, settings *repository.SettingsRepo, rules SyncRules) *AIAppHubSyncService`
  - `func (s *AIAppHubSyncService) Sync(ctx context.Context, scope SyncScope) (SyncResult, error)`

- [ ] **Step 1: 写失败测试**

```go
type fakeFetcher struct{ items []ehrclient.Department; err error }
func (f fakeFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
    return f.items, f.err
}

func TestSyncAllDerivesStudiosAndBackfills(t *testing.T) {
    db, settings := newAIHubTestDBWithSettings(t)
    repo := repository.NewAIAppHubRepo(db)
    fetcher := fakeFetcher{items: []ehrclient.Department{
        {ID: "pub", Name: "公共关系事业部", Type: 2, LeaderPassport: "fandongbo"},
        {ID: "xlab", Name: "战略发展中心", Type: 2, LeaderPassport: "hezhiguo"},
        {ID: "tlab", Name: "T-Lab", ParentID: "xlab", Type: 4},
    }}
    svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

    got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
    require.NoError(t, err)
    require.Equal(t, 3, got.Departments)
    require.Equal(t, 2, got.Studios)
    require.Equal(t, 1, got.Hidden)

    studios, _ := repo.ListStudios(context.Background(), false)
    byDept := map[string]model.AIAppStudio{}
    for _, s := range studios { byDept[*s.DepartmentID] = s }
    require.False(t, byDept["pub"].Visible)
    require.Equal(t, "技术研究工作室", byDept["xlab"].Name)

    d, _ := repo.GetDepartment(context.Background(), "tlab")
    require.NotNil(t, d.StudioID)
    require.Equal(t, byDept["xlab"].ID, *d.StudioID)

    status, _ := settings.GetValue("ai_app_hub.last_sync_status")
    require.Equal(t, "success", status)
}

func TestSyncAllKeepsManualVisibility(t *testing.T) {
    // 先跑一次 → 把 pub 的 visible 改成 true → 再跑一次
    // 断言 visible 仍是 true（同步不覆盖运营字段）
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncAll' -v`
Expected: FAIL（服务未定义）

- [ ] **Step 3: 实现**

`Sync` 流程：①`FetchDepartments` 失败直接返回，不写库；②逐条 `UpsertDepartment`；③`Type==2` 的节点构造 `AIAppStudio`：`GetStudioByDepartmentID` 查不到则 `InsertStudioIfAbsent`（`Name` 取 `NameOverrides`、`Visible = !HiddenDepartmentIDs[ID]`），查到则 `UpdateStudioEHRFields`；④内存建 `id -> node` 映射，对每个节点向上找最近的 type=2 祖先，`SetDepartmentStudio`（找不到传 nil，上溯上限 10 层）；⑤`SettingsRepo.SetMultiple` 写 `last_sync_at`（RFC3339）、`last_sync_status=success`、`last_sync_scope=all`。`SyncResult.Studios` 计数 type2 节点数，`Hidden` 计数规则命中的节点数。 测试辅助 `newAIHubTestDBWithSettings(t)` 放在 `tests/controlplane/service/helper_test.go`：用 sqlite 内存库 AutoMigrate 四张表（含 `model.SystemConfig`），返回 `(*gorm.DB, *repository.SettingsRepo)`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncAll' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_sync.go tests/controlplane/service/ai_app_hub_sync_test.go
git commit -m "feat(hub): 同步核心与 all 范围"
```


---

### Task 4: 范围过滤（department / studio）

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_sync.go`
- Test: `tests/controlplane/service/ai_app_hub_sync_scope_test.go`

**Interfaces:**
- Produces:
  - `var ErrScopeNotFound = errors.New("sync scope not found")`
  - `Sync` 支持 `ScopeDepartment`（只写该节点）与 `ScopeStudio`（工作室行 + 子树）
  - 内部函数 `func (s *AIAppHubSyncService) resolveTargets(scope SyncScope, nodes map[string]ehrclient.Department) (map[string]bool, error)` 返回本次要写的 `department_id` 集合

- [ ] **Step 1: 写失败测试**

```go
func TestSyncDepartmentWritesOnlyThatNode(t *testing.T) {
    // 预置一条 name="旧名字" 的部门 d2；本次 EHR 里 d2 改名、d3 也改名
    // Sync(scope=department, id=d2) 之后：d2 已更新，d3 仍是旧值
}

func TestSyncDepartmentOnStudioNodeUpdatesStudioRow(t *testing.T) {
    // scope=department, id=xlab（type2，EHR 里 leader 变了）
    // 断言 ai_app_studios 对应行的 leader_passport 已更新
}

func TestSyncStudioWritesSubtree(t *testing.T) {
    // xlab 下有 d1、d1 下有 g1；Sync(scope=studio, id=<studio uuid>)
    // 断言 d1/g1 的 studio_id 都被回填，且 count == 3（xlab+d1+g1）
}

func TestSyncStudioWithManualStudioReturnsNotFound(t *testing.T) {
    // 手工建一个 department_id 为 NULL 的工作室 → errors.Is(err, service.ErrScopeNotFound)
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncDepartment|TestSyncStudio' -v`
Expected: FAIL

- [ ] **Step 3: 实现**

`resolveTargets`：`ScopeAll` 返回全部 ID；`ScopeDepartment` 校验 ID 在 `nodes` 中（否则 `ErrScopeNotFound`）只返回它；`ScopeStudio` 用 `GetStudioByID` 取 `department_id`（为 NULL → `ErrScopeNotFound`），在 `nodes` 中定位后 BFS 收集所有后代。`Sync` 把写入循环限定在 targets 内；工作室行的新建/更新只在 `ScopeAll` 或 targets 内存在 type2 节点时执行。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncDepartment|TestSyncStudio' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_sync.go tests/controlplane/service/ai_app_hub_sync_scope_test.go
git commit -m "feat(hub): 单部门与单工作室同步范围"
```

---

### Task 5: 并发控制与失败语义

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_sync.go`
- Test: `tests/controlplane/service/ai_app_hub_sync_guard_test.go`

**Interfaces:**
- Produces:
  - `var ErrSyncInProgress = errors.New("sync in progress")`
  - `Sync` 在同一时刻只允许一个执行；抢不到立即返回 `ErrSyncInProgress`

- [ ] **Step 1: 写失败测试**

```go
func TestSyncRejectsConcurrentRun(t *testing.T) {
    // 用一个阻塞的 fake fetcher（收到信号才返回），启动 goroutine A 调 Sync
    // 等 A 进入 Sync 后，主 goroutine 调 Sync → errors.Is(err, service.ErrSyncInProgress)
}

func TestSyncFailureKeepsLocalData(t *testing.T) {
    // 先成功同步一次；换成 err=fakeFetcher{err: ehrclient.ErrUpstream}
    // 再 Sync → 返回 error，ListStudios 数量不变，last_sync_status=failed 且 last_sync_error 非空
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncRejectsConcurrent|TestSyncFailureKeeps' -v`
Expected: FAIL

- [ ] **Step 3: 实现**

在 `AIAppHubSyncService` 里加 `mu sync.Mutex`，`Sync` 开头 `if !s.mu.TryLock() { return SyncResult{}, ErrSyncInProgress }` + `defer s.mu.Unlock()`；随后 `ctx, cancel := context.WithTimeout(ctx, 30*time.Second)`。fetch 失败分支写 `last_sync_status=failed`、`last_sync_scope`、`last_sync_error`（截断 500 字）后返回 error，不写业务表。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncRejectsConcurrent|TestSyncFailureKeeps' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_sync.go tests/controlplane/service/ai_app_hub_sync_guard_test.go
git commit -m "feat(hub): 同步互斥与失败语义"
```


---

### Task 6: HTTP 触发与权限

**Files:**
- Create: `internal/controlplane/handler/ai_app_hub_sync_handler.go`
- Modify: `internal/controlplane/repository/ai_app_hub_repo.go`（新增 HasAnyRole）
- Modify: `internal/controlplane/service/ai_app_hub_sync.go`（新增 Authorize）
- Modify: `internal/controlplane/router/router.go`
- Test: `tests/controlplane/handler/ai_app_hub_sync_handler_test.go`

**Interfaces:**
- Produces:
  - `func (r *AIAppHubRepo) HasAnyRole(ctx context.Context, passport string, roles []string) (bool, error)`
  - `func (s *AIAppHubSyncService) Authorize(ctx context.Context, passport string, scope SyncScope) (bool, error)`
  - `func NewAIAppHubSyncHandler(svc *AIAppHubSyncService) *AIAppHubSyncHandler`
  - `func (h *AIAppHubSyncHandler) Sync(c *gin.Context)`
  - 路由 `POST /api/v1/admin/ai-app-hub/sync`（挂在 `adminGroup` 内，路径写 `/admin/ai-app-hub/sync`）
  - 请求体 JSON：`{"scope":"all"}`、`{"scope":"studio","id":"<uuid>"}`、`{"scope":"department","id":"<departmentId>"}`

- [ ] **Step 1: 写失败测试**

```go
func TestSyncHandlerMapsErrors(t *testing.T) {
    cases := []struct{
        name string
        svcErr error
        want int
    }{
        {"in progress", service.ErrSyncInProgress, 409},
        {"scope missing", service.ErrScopeNotFound, 404},
        {"upstream", ehrclient.ErrUpstream, 502},
        {"unauthorized upstream", ehrclient.ErrUnauthorized, 502},
        {"empty", ehrclient.ErrEmptyResult, 502},
    }
    // 每个 case 构造一个返回对应 error 的假服务，断言状态码
}

func TestSyncHandlerForbiddenForNonLeader(t *testing.T) {
    // passport=zhangsan，scope=studio 指向别人负责的工作室 → 403
}

func TestSyncHandlerRejectsUnknownScope(t *testing.T) {
    // body {"scope":"whatever"} → 400
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/handler/ -run TestSyncHandler -v`
Expected: FAIL

- [ ] **Step 3: 实现**

`HasAnyRole` 查 legacy 表 `user_roles`（`passport IN (?) AND role IN (?)`）。`Authorize`：`ScopeAll` 只认 `admin/ai_app_admin`；`ScopeStudio` 追加"该工作室 `leader_passport == passport`"；`ScopeDepartment` 先 `GetDepartment` 取 `studio_id` 再比对负责人。handler 解析 body → 校验 scope 枚举（否则 400）→ `Authorize`（false 则 403）→ `Sync` → 按 `errors.Is` 映射状态码；成功返回 `c.JSON(200, result)`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/handler/ -run TestSyncHandler -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/handler/ai_app_hub_sync_handler.go internal/controlplane/repository/ai_app_hub_repo.go internal/controlplane/service/ai_app_hub_sync.go internal/controlplane/router/router.go tests/controlplane/handler/ai_app_hub_sync_handler_test.go
git commit -m "feat(hub): 同步触发接口与权限"
```

---

### Task 7: 定时同步

**Files:**
- Create: `internal/controlplane/service/ai_app_hub_cron.go`
- Modify: `cmd/control-plane/main.go`
- Test: `tests/controlplane/service/ai_app_hub_cron_test.go`

**Interfaces:**
- Produces:
  - `type SyncRunner interface { Sync(ctx context.Context, scope SyncScope) (SyncResult, error) }`
  - `func StartAIAppHubSyncCron(ctx context.Context, runner SyncRunner, interval time.Duration)`
  - `cmd/control-plane/main.go` 用 `EHR_BASE_URL` / `EHR_API_TOKEN` / `EHR_ORG_ID` 构造 client；三者任一为空则跳过启动并 `slog.Warn`

- [ ] **Step 1: 写失败测试**

```go
func TestSyncCronRunsImmediatelyAndStopsOnCancel(t *testing.T) {
    // 假 runner 记录调用次数；interval=10ms，启动后等 50ms 取消 ctx
    // 断言调用次数 >= 1，且取消后不再增长
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run TestSyncCron -v`
Expected: FAIL

- [ ] **Step 3: 实现**

仿 `internal/controlplane/keyactivity/cron.go`：先在 goroutine 里立即执行一次，再用 `time.After(interval)` 循环；每次调用包 `recover`，错误只 `slog.Error` 不退出。`main.go` 里在 control-plane 启动处调用，interval 取 `6*time.Hour`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run TestSyncCron -v && go build ./internal/... ./cmd/...`
Expected: PASS + 构建通过

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_cron.go cmd/control-plane/main.go tests/controlplane/service/ai_app_hub_cron_test.go
git commit -m "feat(hub): 每 6 小时定时同步"
```

---

## 完成后验收

```bash
go build ./internal/... ./cmd/... && go test ./tests/controlplane/... ./tests/aiapphub/... -count=1
```

手工验证（本地 control-plane + 已 apply 迁移 + 配好 `EHR_API_TOKEN`）：

```bash
# 全量
curl -s -X POST localhost:8081/api/v1/admin/ai-app-hub/sync   -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json'   -d '{"scope":"all"}'
# 期望 {"scope":"all","departments":99,"studios":15,"hidden":2,...}

# 单工作室（先查一个 studio id）
curl -s localhost:8081/api/v1/admin/ai-apps/meta -H "Authorization: Bearer $JWT"
curl -s -X POST localhost:8081/api/v1/admin/ai-app-hub/sync   -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json'   -d '{"scope":"studio","id":"<uuid>"}'

# 重复触发应得 409
```

## 不在本计划范围

- H5 聚合查询接口（backend 计划 Task 5-9）
- 人员/员工同步（`employees` 路由未授权）
- 同步历史表、异步任务、进度查询（spec §2 非目标）

