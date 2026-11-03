# AI 应用聚合页后端实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `GET /api/v1/ai-apps/hub` 从本地表返回「按工作室分组、已排序」的 AI 应用列表，数据由 EHR 同步任务填充。

**Architecture:** 分两层——`ai_app_departments` 镜像 EHR 组织树，`ai_app_studios` 是由 `departmentType=2` 派生出的工作室主数据（可人工覆盖）；查询时用登录态里的 `dept_id` 沿 `parent_id` 上溯到工作室，再按可见性拼装响应。同步与查询都在 control-plane 进程内，gateway 不参与。

**Tech Stack:** Go + Gin + GORM(Postgres) + Atlas 迁移 + testify + sqlite（仓储单测）+ httptest（EHR 客户端）。

**Spec:** `docs/ai-app-hub-design.md`（随本计划一起提交）

## Global Constraints

- 表结构已提交（迁移 `atlas-migrations/20261028072408_create_ai_app_hub.sql`），本计划**不再改 schema**；要加列另起迁移并走 atlas-db-migration skill。
- 工作室口径：EHR `departmentType=2`（事业部），共 15 条；`departmentType=3` 只有 1 条虚拟组，不参与。
- 排除/改名规则写死在 `DefaultSyncRules()`：`0B3CF753-9D6D-46E8-89C2-8E752D2A460F`（公共关系事业部）不展示；`08C6856B-D0D5-4FC4-830E-7D13441EA89F`（元气工作室）先不展示；`53E83585-6CE3-48EF-8255-520BBAFCB156`（战略发展中心）名称覆盖为 **技术研究工作室**。
- 排序由后端完成：工作室 `app_count DESC, id DESC`；应用 `is_new DESC, heat DESC, sort_order DESC, id DESC`。
- 只返回 `status='published'` 且所属工作室 `visible=true` 的应用。
- 代码与注释用中文；提交信息用 Conventional Commits；新增接口同步 `tests/contract/contracts.json`。
- gateway 不直连数据库，持久化逻辑全部在 control-plane。

## Review Focus

- **空部门**：JWT 里没有 `dept_id` 时，`/hub` 返回 `studios: []` 而不是 500。
- **孤儿部门**：`杭州舰宇` / `杭州边翼（新）` / `暂存部门` 没有事业部祖先，解析结果是"无工作室"，不能死循环或 panic。
- **EHR 异常**：接口 5xx 或 items 为空时，同步任务不得清空本地数据。
- **一人多工作室**：何治国同时是 4 个工作室的 leader，`is_leader=true` 时 `scope` 必须是 `all`。
- **排序稳定性**：工作室 `app_count` 相同时按 `id` 降序，应用同理，避免刷新抖动。

---

### Task 1: 工作室与部门仓储

**Files:**
- Create: `internal/controlplane/repository/ai_app_hub_repo.go`
- Test: `tests/controlplane/repository/ai_app_hub_repo_test.go`

**Interfaces:**
- Consumes: `model.AIAppStudio` / `model.AIAppDepartment`（已存在）
- Produces:
  - `func NewAIAppHubRepo(db *gorm.DB) *AIAppHubRepo`
  - `func (r *AIAppHubRepo) UpsertStudioByDepartment(ctx context.Context, s *model.AIAppStudio) error`
  - `func (r *AIAppHubRepo) ListStudios(ctx context.Context, onlyVisible bool) ([]model.AIAppStudio, error)`
  - `func (r *AIAppHubRepo) GetStudioByDepartmentID(ctx context.Context, deptID string) (*model.AIAppStudio, error)`
  - `func (r *AIAppHubRepo) UpsertDepartment(ctx context.Context, d *model.AIAppDepartment) error`
  - `func (r *AIAppHubRepo) GetDepartment(ctx context.Context, deptID string) (*model.AIAppDepartment, error)`
  - `func (r *AIAppHubRepo) ListDepartments(ctx context.Context) ([]model.AIAppDepartment, error)`
  - `func (r *AIAppHubRepo) SetDepartmentStudio(ctx context.Context, deptID string, studioID *uuid.UUID) error`

- [ ] **Step 1: 写失败测试**

```go
func TestUpsertStudioByDepartmentIsIdempotent(t *testing.T) {
    db := newAIHubTestDB(t)
    repo := repository.NewAIAppHubRepo(db)
    dept := "0B3CF753-9D6D-46E8-89C2-8E752D2A460F"
    s := &model.AIAppStudio{Name: "旧名", DepartmentID: &dept, LeaderPassport: "fandongbo", Source: "ehr", Visible: true}
    require.NoError(t, repo.UpsertStudioByDepartment(context.Background(), s))
    s.Name = "新名"
    require.NoError(t, repo.UpsertStudioByDepartment(context.Background(), s))
    list, err := repo.ListStudios(context.Background(), false)
    require.NoError(t, err)
    require.Len(t, list, 1)
    require.Equal(t, "新名", list[0].Name)
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/repository/ -run TestUpsertStudioByDepartment -v`
Expected: FAIL（`NewAIAppHubRepo` 未定义）

- [ ] **Step 3: 实现仓储**

`UpsertStudioByDepartment` 用 `clause.OnConflict{Columns: []clause.Column{{Name: "department_id"}}, DoUpdates: clause.AssignmentColumns([]string{"name","leader_name","leader_passport","leader_employee_id","source","visible","updated_at"})}`；`SetDepartmentStudio` 只更新 `studio_id`。sqlite 测试按 model 字段手写建表 DDL（参考 `budget_tier_repo_test.go`），UUID 用固定字符串。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/repository/ -run TestUpsertStudioByDepartment -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/ai_app_hub_repo.go tests/controlplane/repository/ai_app_hub_repo_test.go
git commit -m "feat(hub): 工作室与部门仓储"
```

---

### Task 2: 应用、日志与角色仓储

**Files:**
- Modify: `internal/controlplane/repository/ai_app_hub_repo.go`
- Test: `tests/controlplane/repository/ai_app_hub_apps_repo_test.go`

**Interfaces:**
- Produces:
  - `func (r *AIAppHubRepo) ListPublishedApps(ctx context.Context, studioIDs []uuid.UUID) ([]model.AIApp, error)`
  - `func (r *AIAppHubRepo) ListAppsByStudio(ctx context.Context, studioID uuid.UUID) ([]model.AIApp, error)`
  - `func (r *AIAppHubRepo) GetApp(ctx context.Context, id uuid.UUID) (*model.AIApp, error)`
  - `func (r *AIAppHubRepo) CreateApp(ctx context.Context, a *model.AIApp) error`
  - `func (r *AIAppHubRepo) UpdateApp(ctx context.Context, a *model.AIApp) error`
  - `func (r *AIAppHubRepo) DeleteApp(ctx context.Context, id uuid.UUID) error`
  - `func (r *AIAppHubRepo) InsertLoginLog(ctx context.Context, l *model.AIAppHubLoginLog) error`
  - `func (r *AIAppHubRepo) ListLoginLogs(ctx context.Context, f LoginLogFilter) ([]model.AIAppHubLoginLog, int64, error)`
  - `func (r *AIAppHubRepo) HasAnyRole(ctx context.Context, passport string, roles []string) (bool, error)`
  - `type LoginLogFilter struct { From, To *time.Time; Page, PageSize int }`

- [ ] **Step 1: 写失败测试**

```go
func TestListPublishedAppsExcludesDraftAndOffline(t *testing.T) {
    db := newAIHubTestDB(t)
    repo := repository.NewAIAppHubRepo(db)
    studio := uuid.New()
    for _, st := range []string{"published", "draft", "offline"} {
        require.NoError(t, repo.CreateApp(context.Background(), &model.AIApp{
            StudioID: studio, Name: st, Status: st, URL: "https://x",
        }))
    }
    apps, err := repo.ListPublishedApps(context.Background(), []uuid.UUID{studio})
    require.NoError(t, err)
    require.Len(t, apps, 1)
    require.Equal(t, "published", apps[0].Status)
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/repository/ -run TestListPublishedApps -v`
Expected: FAIL（方法未定义）

- [ ] **Step 3: 实现**

`ListPublishedApps` 用 `Where("studio_id IN ? AND status = ?", studioIDs, "published")`，`studioIDs` 为空直接返回空切片。`ListLoginLogs` 返回 `(rows, total, error)`，`Page/PageSize` 缺省 1/20、上限 100。`HasAnyRole` 查 legacy 的 `user_roles` 表（列 `passport/role/scope_type/scope_id`），测试里手工建表。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/repository/ -run 'TestListPublishedApps|TestListLoginLogs' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/ai_app_hub_repo.go tests/controlplane/repository/ai_app_hub_apps_repo_test.go
git commit -m "feat(hub): 应用/日志/角色仓储"
```

---

### Task 3: EHR 部门客户端

**Files:**
- Create: `internal/aiapphub/ehrclient/client.go`
- Test: `tests/aiapphub/ehrclient/client_test.go`

**Interfaces:**
- Produces:
  - `type Config struct { BaseURL, Prefix, Token, OrgID string; Timeout time.Duration }`
  - `func New(cfg Config) *Client`
  - `func (c *Client) FetchDepartments(ctx context.Context) ([]Department, error)`
  - `type Department struct { ID, Name, ParentID, Path, LeaderName, LeaderPassport, LeaderEmployeeID string; Type int }`
  - 错误哨兵：`ErrUnauthorized`（401/403）、`ErrUpstream`（其余非 200）

- [ ] **Step 1: 写失败测试**

```go
func TestFetchDepartmentsParsesItems(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.Equal(t, "/open-apis/ehr-apic/open-apis/resource/v1/departments", r.URL.Path)
        require.Equal(t, "1428195512246472704", r.URL.Query().Get("orgId"))
        require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
        io.WriteString(w, `{"items":[{"departmentId":"d1","departmentName":"AI-Lab","parentId":"00000000-0000-0000-0000-000000000000","departmentType":2,"leaderName":"何治国","leaderPassport":"hezhiguo"}]}`)
    }))
    defer srv.Close()
    c := ehrclient.New(ehrclient.Config{BaseURL: srv.URL, Prefix: "/open-apis/ehr-apic/open-apis/resource/v1", Token: "tok", OrgID: "1428195512246472704"})
    got, err := c.FetchDepartments(context.Background())
    require.NoError(t, err)
    require.Len(t, got, 1)
    require.Equal(t, "AI-Lab", got[0].Name)
    require.Equal(t, 2, got[0].Type)
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/aiapphub/ehrclient/ -v`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现**

URL 为 `BaseURL + Prefix + "/departments?orgId=" + url.QueryEscape(OrgID)`；`http.Client` 超时默认 30s；响应体读上限 4MB；响应结构 `struct{ Items []Department }`，json tag 与 EHR 一致（`departmentId/departmentName/parentId/departmentPath/departmentType/leaderName/leaderPassport/leaderEmployeeId`）。403 必须映射为 `ErrUnauthorized`，日志提示"接口中心未开通该路由"。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/aiapphub/ehrclient/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/aiapphub/ehrclient/ tests/aiapphub/ehrclient/
git commit -m "feat(hub): EHR 部门接口客户端"
```

---

### Task 4: 同步服务

**Files:**
- Create: `internal/controlplane/service/ai_app_hub_sync.go`
- Test: `tests/controlplane/service/ai_app_hub_sync_test.go`

**Interfaces:**
- Consumes: Task 1/2 仓储；Task 3 的 `Department`
- Produces:
  - `type DepartmentFetcher interface { FetchDepartments(ctx context.Context) ([]ehrclient.Department, error) }`
  - `func NewAIAppHubSyncService(f DepartmentFetcher, repo *repository.AIAppHubRepo, rules SyncRules) *AIAppHubSyncService`
  - `func (s *AIAppHubSyncService) Sync(ctx context.Context) (SyncResult, error)`
  - `type SyncResult struct { Departments, Studios, Hidden int }`
  - `type SyncRules struct { HiddenDepartmentIDs map[string]bool; NameOverrides map[string]string }`
  - `func DefaultSyncRules() SyncRules`

- [ ] **Step 1: 写失败测试**

```go
type fakeFetcher struct{ items []ehrclient.Department; err error }
func (f fakeFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) { return f.items, f.err }

func TestSyncCreatesStudiosFromDepartmentType2(t *testing.T) {
    db := newAIHubTestDB(t)
    repo := repository.NewAIAppHubRepo(db)
    fetcher := fakeFetcher{items: []ehrclient.Department{
        {ID: "pub", Name: "公共关系事业部", Type: 2, LeaderPassport: "fandongbo"},
        {ID: "xlab", Name: "战略发展中心", Type: 2, LeaderPassport: "hezhiguo"},
        {ID: "tlab", Name: "T-Lab", ParentID: "xlab", Type: 4},
    }}
    svc := service.NewAIAppHubSyncService(fetcher, repo, service.DefaultSyncRules())
    got, err := svc.Sync(context.Background())
    require.NoError(t, err)
    require.Equal(t, 2, got.Studios)
    studios, _ := repo.ListStudios(context.Background(), false)
    byDept := map[string]model.AIAppStudio{}
    for _, s := range studios { byDept[*s.DepartmentID] = s }
    require.False(t, byDept["pub"].Visible)
    require.Equal(t, "技术研究工作室", byDept["xlab"].Name)
    d, _ := repo.GetDepartment(context.Background(), "tlab")
    require.NotNil(t, d.StudioID)
    require.Equal(t, byDept["xlab"].ID, *d.StudioID)
}
```
```go
func TestSyncKeepsLocalDataWhenFetcherFails(t *testing.T) {
    // 先成功同步一次，再把 fetcher 换成返回 error 的版本；
    // 断言 ListStudios 仍有数据（失败不得清空本地）
}
```


- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run TestSyncCreatesStudios -v`
Expected: FAIL

- [ ] **Step 3: 实现**

顺序：①`FetchDepartments` 失败直接返回 error，不写库；②逐条 `UpsertDepartment`；③`Type==2` 的节点构造 `AIAppStudio`（名称走 `NameOverrides`，`Visible = !HiddenDepartmentIDs[id]`）并 `UpsertStudioByDepartment`；④内存建 `parent->node` 映射，对每个节点向上找最近的 type=2 祖先得到 `studioID`，再 `SetDepartmentStudio`（找不到传 nil）。上溯上限 10 层防环。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run TestSync -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_sync.go tests/controlplane/service/ai_app_hub_sync_test.go
git commit -m "feat(hub): EHR 部门同步与工作室派生"
```

---

### Task 5: 部门到工作室的解析

**Files:**
- Create: `internal/controlplane/service/ai_app_hub_service.go`
- Test: `tests/controlplane/service/ai_app_hub_service_test.go`

**Interfaces:**
- Produces:
  - `func NewAIAppHubService(repo *repository.AIAppHubRepo) *AIAppHubService`
  - `func (s *AIAppHubService) StudioOfDepartment(ctx context.Context, deptID string) (*model.AIAppStudio, error)`（空 `deptID` 或找不到返回 `(nil, nil)`）

- [ ] **Step 1: 写失败测试**

```go
func TestStudioOfDepartmentWalksToAncestorStudio(t *testing.T) {
    db := newAIHubTestDB(t); repo := repository.NewAIAppHubRepo(db)
    ctx := context.Background()
    studioID := uuid.New(); studioDept := "s1"
    require.NoError(t, repo.UpsertStudioByDepartment(ctx, &model.AIAppStudio{
        ID: studioID, Name: "社交工作室", DepartmentID: &studioDept, Visible: true}))
    require.NoError(t, repo.UpsertDepartment(ctx, &model.AIAppDepartment{DepartmentID: "s1", DepartmentType: 2, ParentID: "root"}))
    require.NoError(t, repo.UpsertDepartment(ctx, &model.AIAppDepartment{DepartmentID: "d2", DepartmentType: 4, ParentID: "s1"}))
    require.NoError(t, repo.UpsertDepartment(ctx, &model.AIAppDepartment{DepartmentID: "g3", DepartmentType: 5, ParentID: "d2"}))
    svc := service.NewAIAppHubService(repo)
    got, err := svc.StudioOfDepartment(ctx, "g3")
    require.NoError(t, err)
    require.NotNil(t, got)
    require.Equal(t, studioID, got.ID)
}
```
```go
func TestStudioOfDepartmentOrphanReturnsNil(t *testing.T) {
    // 根级部门（parent 为全零 UUID）→ 返回 (nil, nil)，不报错不死循环
}
```


- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run TestStudioOfDepartment -v`
Expected: FAIL

- [ ] **Step 3: 实现**

从 `GetDepartment` 起步循环：命中 `StudioID != nil` 直接 `GetStudioByDepartmentID` 返回；父节点为全零 UUID 或空串返回 `(nil, nil)`；循环上限 10 次，超限 `slog.Warn` 后返回 nil。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run TestStudioOfDepartment -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_service.go tests/controlplane/service/ai_app_hub_service_test.go
git commit -m "feat(hub): 部门到工作室的解析"
```

---

### Task 6: 查看者与权限

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_service.go`
- Test: `tests/controlplane/service/ai_app_hub_viewer_test.go`

**Interfaces:**
- Produces:
  - `func (s *AIAppHubService) ResolveViewer(ctx context.Context, passport, name, deptID string) (Viewer, error)`
  - `type Viewer struct { Passport, Name, Scope string; IsAdmin, IsLeader bool; StudioIDs []uuid.UUID }`
  - 语义：`IsAdmin` 命中 `user_roles.role IN ('admin','ai_app_admin')`；`IsLeader` 命中 `ai_app_studios.leader_passport = passport`；任一为真 → `Scope="all"` 且 `StudioIDs` 为全部可见工作室；否则 `Scope="studio"` 且只含自己那一个

- [ ] **Step 1: 写失败测试**

```go
func TestResolveViewerLeaderOfAnyStudioSeesAll(t *testing.T) {
    // 3 个工作室，s1.leader_passport = "hezhiguo"
    v, err := svc.ResolveViewer(ctx, "hezhiguo", "何治国", "d2")
    require.NoError(t, err)
    require.True(t, v.IsLeader)
    require.Equal(t, "all", v.Scope)
    require.Len(t, v.StudioIDs, 3)
}

func TestResolveViewerPlainEmployeeSeesOnlyOwnStudio(t *testing.T) {
    v, _ := svc.ResolveViewer(ctx, "zhangsan", "张三", "g3")
    require.False(t, v.IsLeader)
    require.Equal(t, "studio", v.Scope)
    require.Equal(t, []uuid.UUID{s1}, v.StudioIDs)
}
```
```go
func TestResolveViewerWithoutDepartmentGetsEmptyScope(t *testing.T) {
    // deptID 为空 → Scope="studio"，StudioIDs 长度为 0，/hub 返回 studios: []
}
```


- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run TestResolveViewer -v`
Expected: FAIL

- [ ] **Step 3: 实现**

先 `HasAnyRole(passport, []string{"admin","ai_app_admin"})`，再 `ListStudios(ctx,true)` 比对 `LeaderPassport`；`Scope="all"` 时收藏全部可见 ID，否则只放 `StudioOfDepartment(deptID)` 的结果。`passport` 为空直接返回空 `StudioIDs`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run TestResolveViewer -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_service.go tests/controlplane/service/ai_app_hub_viewer_test.go
git commit -m "feat(hub): 查看者身份与可见范围"
```

---

### Task 7: Hub 聚合与排序

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_service.go`
- Test: `tests/controlplane/service/ai_app_hub_build_test.go`

**Interfaces:**
- Produces:
  - `func (s *AIAppHubService) BuildHub(ctx context.Context, v Viewer) (HubResponse, error)`
  - `type HubResponse struct { Viewer HubViewer; Studios []HubStudio }`
  - `type HubViewer struct { Name, Passport, Scope string; IsLeader bool }`
  - `type HubStudio struct { ID uuid.UUID; Name, LeaderName string; AppCount int; Apps []HubApp }`
  - `type HubApp struct { ID uuid.UUID; Name, Description, IconURL, URL string; Heat int16; IsNew bool }`
  - json tag 全部 snake_case（`app_count` / `is_new` / `icon_url` / `leader_name`）

- [ ] **Step 1: 写失败测试**

```go
func TestBuildHubSortsStudiosByAppCountThenIDDesc(t *testing.T) {
    // A:2 个应用，B:2 个，C:0 个，D:1 个
    got, err := svc.BuildHub(ctx, service.Viewer{Scope: "all", StudioIDs: []uuid.UUID{a, b, c, d}})
    require.NoError(t, err)
    want := []uuid.UUID{maxUUID(a, b), minUUID(a, b), d, c}
    require.Equal(t, want, studioIDs(got.Studios))
}

func TestBuildHubSortsAppsNewThenHeat(t *testing.T) {
    // 同工作室：old/heat3、new/heat1、new/heat2 → new/heat2、new/heat1、old/heat3
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/service/ -run TestBuildHub -v`
Expected: FAIL

- [ ] **Step 3: 实现**

`ListStudios(ctx,true)` 过滤到 `v.StudioIDs`；`ListPublishedApps`；按 `studio_id` 分组套应用排序；工作室用 `sort.SliceStable` 先比 `AppCount` 再比 `ID.String()` 降序。没有应用的工作室保留（`AppCount=0`），前端显示占位。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/service/ -run TestBuildHub -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_service.go tests/controlplane/service/ai_app_hub_build_test.go
git commit -m "feat(hub): 聚合响应与排序"
```

---

### Task 8: `/hub` 接口与访问日志

**Files:**
- Create: `internal/controlplane/handler/ai_app_hub_handler.go`
- Modify: `internal/controlplane/router/router.go`
- Modify: `tests/contract/contracts.json`
- Test: `tests/controlplane/handler/ai_app_hub_handler_test.go`

**Interfaces:**
- Produces:
  - `func NewAIAppHubHandler(svc *service.AIAppHubService) *AIAppHubHandler`
  - `func (h *AIAppHubHandler) GetHub(c *gin.Context)`
  - 路由 `GET /api/v1/ai-apps/hub`，挂 `middleware.JWTAuth()`，**不进 adminGroup**

- [ ] **Step 1: 写失败测试**

```go
func TestGetHubReturnsViewerAndStudios(t *testing.T) {
    r := gin.New()
    h := handler.NewAIAppHubHandler(svc)
    r.GET("/hub", func(c *gin.Context) {
        c.Set("user_id", "hezhiguo"); c.Set("user_name", "何治国"); c.Set("user_dept_id", "s1")
        h.GetHub(c)
    })
    w := httptest.NewRecorder()
    r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/hub", nil))
    require.Equal(t, 200, w.Code)
    var body map[string]any
    require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
    require.Equal(t, "all", body["viewer"].(map[string]any)["scope"])
}
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/handler/ -run TestGetHub -v`
Expected: FAIL

- [ ] **Step 3: 实现**

从 `user_id`（passport）、`user_name`、`user_dept_id` 取身份 → `ResolveViewer` → `BuildHub` → 200；随后 `go` 异步写日志（`defer recover` 兜住）。异常返回 500 并 `slog.Error`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/handler/ -run TestGetHub -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/handler/ai_app_hub_handler.go internal/controlplane/router/router.go tests/contract/contracts.json tests/controlplane/handler/ai_app_hub_handler_test.go
git commit -m "feat(hub): /api/v1/ai-apps/hub 接口"
```

---

### Task 9: 管理端 CRUD

**Files:**
- Modify: `internal/controlplane/handler/ai_app_hub_handler.go`
- Modify: `internal/controlplane/router/router.go`
- Modify: `tests/contract/contracts.json`
- Test: `tests/controlplane/handler/ai_app_hub_admin_test.go`

**Interfaces:**
- Produces:
  - `ListApps` / `CreateApp` / `UpdateApp` / `DeleteApp`（`/api/v1/admin/ai-apps`）
  - `GetMeta`（`/api/v1/admin/ai-apps/meta` → `{studios, heat}`）
  - 请求体 `type appRequest struct { StudioID string; Name, Description, IconURL, URL string; Heat int16; IsNew bool; Status string; SortOrder int32 }`
  - 校验：名称/URL 非空、描述 `utf8.RuneCountInString <= 200`、`Heat` 0–3、`Status` ∈ draft|published|offline，违规 400

- [ ] **Step 1: 写失败测试**

```go
func TestCreateAppRejectsOverlongDescription(t *testing.T) {
    // description = strings.Repeat("字", 201) → 400
}
func TestCreateAppRequiresAdminRole(t *testing.T) { /* 非 admin → 403 */ }
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/handler/ -run TestCreateApp -v`
Expected: FAIL

- [ ] **Step 3: 实现**

统一 `h.requireAdmin(c) bool`（内部用 `ResolveViewer` 判 `IsAdmin`）；`CreatedBy/UpdatedBy` 取 `user_id`；删除为物理删除。注册在 `adminGroup` 内，路径写 `/admin/ai-apps`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/handler/ -run 'TestCreateApp|TestListApps' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/handler/ai_app_hub_handler.go internal/controlplane/router/router.go tests/contract/contracts.json tests/controlplane/handler/ai_app_hub_admin_test.go
git commit -m "feat(hub): 管理端 AI 应用 CRUD"
```

---

### Task 10: 同步触发、定时任务与日志查询

**Files:**
- Modify: `internal/controlplane/handler/ai_app_hub_handler.go`
- Create: `internal/controlplane/service/ai_app_hub_cron.go`
- Modify: `cmd/control-plane/main.go`
- Modify: `internal/controlplane/router/router.go`
- Test: `tests/controlplane/handler/ai_app_hub_sync_handler_test.go`

**Interfaces:**
- Produces:
  - `func (h *AIAppHubHandler) SyncEHR(c *gin.Context)` — `POST /api/v1/admin/studios/sync-ehr`，返回 `{"departments":99,"studios":15,"hidden":2}`
  - `func (h *AIAppHubHandler) ListLogs(c *gin.Context)` — `GET /api/v1/admin/ai-app-hub/logs?from=&to=&page=&page_size=`
  - `func StartAIAppHubSyncCron(ctx context.Context, svc *service.AIAppHubSyncService, interval time.Duration)`，默认 6h，启动即跑一次

- [ ] **Step 1: 写失败测试**

```go
func TestSyncEHREndpointReturnsCounts(t *testing.T) {
    // 注入 fake fetcher（3 条 fixture），断言 200 且 body["studios"] == 2
}
func TestSyncEHREndpointRequiresAdmin(t *testing.T) { /* 非 admin → 403 */ }
```

- [ ] **Step 2: 运行，确认失败**

Run: `go test ./tests/controlplane/handler/ -run TestSyncEHR -v`
Expected: FAIL

- [ ] **Step 3: 实现**

`SyncEHR` 调 `syncSvc.Sync`，失败 502 并带原因（便于前端提示"接口中心未授权"）。`ListLogs` 时间用 RFC3339，非法 400。Cron 仿 `internal/controlplane/keyactivity/cron.go`（ticker + ctx.Done + recover），单次超时 2 分钟。`main.go` 用 `EHR_BASE_URL/EHR_API_TOKEN/EHR_ORG_ID` 构造 client，缺任意一个就跳过启动并 `slog.Warn`。

- [ ] **Step 4: 运行，确认通过**

Run: `go test ./tests/controlplane/handler/ -run TestSyncEHR -v && go build ./internal/... ./cmd/...`
Expected: PASS + 构建通过

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/handler/ai_app_hub_handler.go internal/controlplane/service/ai_app_hub_cron.go cmd/control-plane/main.go internal/controlplane/router/router.go tests/controlplane/handler/ai_app_hub_sync_handler_test.go
git commit -m "feat(hub): 同步触发、定时任务与日志查询"
```

---

## 完成后验收

```bash
go build ./internal/... ./cmd/... && go test ./tests/controlplane/... ./tests/aiapphub/... -count=1
```

手工验证（本地起 control-plane，先 apply 迁移）：

```bash
curl -s -H "Authorization: Bearer $JWT" localhost:8081/api/v1/ai-apps/hub | jq '.studios[] | {name, app_count}'
```

期望：可见工作室 13 个（公共关系事业部、元气工作室不可见），按应用数降序。

## 不在本计划范围

- H5 页面（`/ai-apps` 移动端）——单独一份前端计划；
- 大群工作台菜单改造——配置项，随上线单独处理；
- `employees` 接口权限与人员名册同步——依赖接口中心授权，不阻塞本计划。
