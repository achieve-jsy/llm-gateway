# EHR 组织树每日定时同步 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `feat/ai-app-hub` 的 EHR 同步按"每天 03:00（Asia/Shanghai）整棵树全量"执行，多实例只跑一个，失败可查，并隐藏 EHR 已撤销的工作室。

**Architecture:** 在 `internal/controlplane/service` 里把「触发」与「同步」解耦：同步服务只负责一次 `Sync(scope)`；新的 `ai_app_hub_cron.go` 负责墙上时钟判定（`robfig/cron` 解析 + 1 分钟 tick）、Redis leader 锁、`last_success` 跨重启去重、保护式重试。删除对账是 `scope=all` 收尾的一条批量 UPDATE，落不到其他 scope。

**Tech Stack:** Go 1.x、gin、GORM（tests 用 sqlite in-memory）、`github.com/redis/go-redis/v9`、`github.com/robfig/cron/v3`（已在 go.mod）、`github.com/alicebob/miniredis/v2`（已在 go.mod）、testify。

**Spec:** `docs/superpowers/specs/2026-11-02-ehr-org-tree-daily-sync-design.md`

## Global Constraints

- **不改表结构、不新增迁移**；`ai_app_departments` 本轮不动。
- **不新增任何依赖**；cron 用 `github.com/robfig/cron/v3`，Redis 用 `go-redis/v9`，测试 Redis 用 `miniredis/v2`。
- 所有路径以 worktree `/Users/jsy/Public/self_code/llm-gateway-ai-apps`（分支 `feat/ai-app-hub`）为根；每个 `go test` 命令都在这里跑。
- Go 代码注释用中文；提交信息用 Conventional Commits。
- 删除语义只碰 `source='ehr' AND department_id 非空` 的行；**永不物理删除** `ai_app_studios`/`ai_apps`。
- `visible` 是运营决策：任何同步路径都不得把它改回 `true`。
- 时区默认 `Asia/Shanghai`，`time.LoadLocation` 失败回退 `time.Local`。
- 每个 Task 结束时 `go build ./...` 必须绿；不要跨 Task 留半截编译错误。

## Review Focus

- **空/部分结果** → EHR 返回空列表时绝不能把全站工作室隐藏（一次上游异常 = 全站消失）。
- **时区** → 容器没被 `TZ` 覆盖时 03:00 会漂成 UTC 03:00；判定与补跑必须都走 `loc`。
- **`last_success` 只在成功后更新** → 失败后重启不得认为"今天已跑过"，否则当天静默漏同步。
- **多实例/蓝绿** → 锁必须比较持有者再释放，不能删别人的锁；两个实例同一分钟 tick 只能跑一个。
- **复活 bug** → 同步必须保留运营手工设的 `visible=false`，不能因为部门仍在 EHR 就被改回 `true`。

---

### Task 1: 仓储新增"隐藏已撤销工作室"

**Files:**
- Modify: `internal/controlplane/repository/ai_app_hub_repo.go`
- Test: `tests/controlplane/repository/ai_app_hub_repo_test.go`

**Interfaces:**
- Consumes: 现有 `AIAppHubRepo`（`r.db *gorm.DB`）与测试助手 `newAIHubRepoTestDB(t)`。
- Produces: `func (r *AIAppHubRepo) HideStudiosNotInDepartments(ctx context.Context, keep []string) (int64, error)` — 返回受影响行数；`keep` 为空时表示"没有任何部门存活"（遮蔽全部 `source='ehr'` 行，服务层负责保证不会传空）。

- [ ] **Step 1: 写失败测试**

```go
func TestHideStudiosNotInDepartmentsHidesOnlyVanishedEHRRows(t *testing.T) {
	db := newAIHubRepoTestDB(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	keepDept, goneDept, manualDept := "keep-1", "gone-1", "gone-manual"
	for _, s := range []model.AIAppStudio{
		{DepartmentID: &keepDept, Name: "保留", Source: "ehr", Visible: true},
		{DepartmentID: &goneDept, Name: "撤销", Source: "ehr", Visible: true},
		{DepartmentID: &manualDept, Name: "手工", Source: "manual", Visible: true},
	} {
		_, err := repo.InsertStudioIfAbsent(ctx, &s)
		require.NoError(t, err)
	}

	n, err := repo.HideStudiosNotInDepartments(ctx, []string{keepDept})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "只隐藏 source=ehr 且不在 keep 里的行")

	kept, err := repo.GetStudioByDepartmentID(ctx, keepDept)
	require.NoError(t, err)
	require.True(t, kept.Visible)

	gone, err := repo.GetStudioByDepartmentID(ctx, goneDept)
	require.NoError(t, err)
	require.False(t, gone.Visible)

	manual, err := repo.GetStudioByDepartmentID(ctx, manualDept)
	require.NoError(t, err)
	require.True(t, manual.Visible, "manual 行永不被同步改动")
}

func TestHideStudiosNotInDepartmentsIgnoresEmptyDepartmentID(t *testing.T) {
	db := newAIHubRepoTestDB(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	_, err := repo.InsertStudioIfAbsent(ctx, &model.AIAppStudio{Name: "手工工作室", Source: "ehr", Visible: true})
	require.NoError(t, err)

	n, err := repo.HideStudiosNotInDepartments(ctx, []string{"any"})
	require.NoError(t, err)
	require.Equal(t, int64(0), n, "department_id IS NULL 的行不参与对账")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/controlplane/repository/ -run 'TestHideStudiosNotInDepartments' -count=1`
Expected: FAIL，编译错误 `repo.HideStudiosNotInDepartments undefined`。

- [ ] **Step 3: 实现**

在 `ai_app_hub_repo.go` 的 `SetDepartmentStudio` 之后新增。一条 UPDATE，条件里带上 `source = 'ehr'`、`department_id IS NOT NULL`、`department_id <> ''`、`visible = true`，用 `clause.Not`/`NOT IN` 表达 `keep`；`keep` 为空时用 `gorm.Expr("true")` 避免 `NOT IN (NULL)` 静默失效。`updated_at` 写 `time.Now()`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./tests/controlplane/repository/ -run 'TestHideStudiosNotInDepartments' -count=1`
Expected: PASS（2 个用例）。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/repository/ai_app_hub_repo.go tests/controlplane/repository/ai_app_hub_repo_test.go
git commit -m "feat(hub): 仓储支持隐藏已撤销工作室"
```

---

### Task 2: `scope=all` 收尾对账 + 结果/状态字段

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_sync.go`
- Test: `tests/controlplane/service/ai_app_hub_sync_test.go`

**Interfaces:**
- Consumes: Task 1 的 `HideStudiosNotInDepartments`；现有 `syncStudios`、`markStatus`、测试助手 `newAIHubTestDBWithSettings(t)`、`fakeFetcher`、`threeNodes()`、`pubDeptID`、`xlabDeptID`。
- Produces:
  - `HubSyncResult.DeletedStudios int`（json `deleted_studios`）
  - `markStatus(scope SyncScope, status, errMsg string, deleted int)`（内部函数，成功时写 `ai_app_hub.last_sync_deleted_studios`）

- [ ] **Step 1: 写失败测试**

```go
func TestSyncAllHidesVanishedStudios(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()

	first := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())
	_, err := first.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	// 第二轮：公共关系事业部从 EHR 消失
	second := service.NewAIAppHubSyncService(fakeFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2, LeaderPassport: "hezhiguo"},
	}}, repo, settings, service.DefaultSyncRules())
	got, err := second.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, got.DeletedStudios)

	gone, err := repo.GetStudioByDepartmentID(ctx, pubDeptID)
	require.NoError(t, err)
	require.False(t, gone.Visible, "EHR 撤销的事业部要隐藏")

	status, err := settings.GetValue("ai_app_hub.last_sync_deleted_studios")
	require.NoError(t, err)
	require.Equal(t, "1", status)

	// 幂等：同样的输入再跑一次，没有新行可隐藏
	again, err := second.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Zero(t, again.DeletedStudios)
}

func TestSyncAllEmptyResultHidesNothing(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	empty := service.NewAIAppHubSyncService(fakeFetcher{items: nil}, repo, settings, service.DefaultSyncRules())
	got, err := empty.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Zero(t, got.DeletedStudios, "空结果不得触发隐藏")

	for _, id := range []string{pubDeptID, xlabDeptID} {
		s, err := repo.GetStudioByDepartmentID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, id != pubDeptID, s.Visible, "只有规则隐藏的公共关系事业部是 false")
	}
}

func TestSyncNonAllScopeDoesNotHide(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	// 只同步 xlab 单部门：公共关系事业部不在 targets，但也绝不能因此被隐藏
	got, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeDepartment, ID: xlabDeptID})
	require.NoError(t, err)
	require.Zero(t, got.DeletedStudios)
}

func TestSyncDoesNotReviveManuallyHiddenStudio(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	xlab, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE ai_app_studios SET visible = 0 WHERE department_id = ?", xlabDeptID).Error)

	_, err = svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	still, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
	require.NoError(t, err)
	require.False(t, still.Visible, "同步不得把运营关闭的工作室改回可见")
	_ = xlab
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncAllHidesVanishedStudios|TestSyncAllEmptyResultHidesNothing|TestSyncNonAllScopeDoesNotHide|TestSyncDoesNotReviveManuallyHiddenStudio' -count=1`
Expected: FAIL（`got.DeletedStudios undefined` / 断言失败）。

- [ ] **Step 3: 实现**

1. `HubSyncResult` 加 `DeletedStudios int `json:"deleted_studios"``。
2. `Sync` 在回填 `studio_id` 循环之后、`markStatus` 之前：仅当 `scope.Kind == ScopeAll` 且 `len(items) > 0` 时，构造 `keep`（全部 `it.ID`）并调用 `s.repo.HideStudiosNotInDepartments(ctx, keep)`，行数写入结果与 `markStatus` 的第 4 个参数；失败按现有风格 `return HubSyncResult{}, err`。
3. `markStatus` 增加 `deleted int` 参数：`status=="success"` 时额外写 `ai_app_hub.last_sync_deleted_studios = strconv.Itoa(deleted)`；失败路径传 0 且不写该键。

- [ ] **Step 4: 跑测试确认通过（含回归）**

Run: `go test ./tests/controlplane/service/ -count=1`
Expected: PASS（新 4 个 + 原有 sync/scope/guard 用例全绿）。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_sync.go tests/controlplane/service/ai_app_hub_sync_test.go
git commit -m "feat(hub): 全量同步隐藏 EHR 已撤销工作室"
```

---

### Task 3: 调度配置与到点判定（纯函数）

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_cron.go`
- Test: `tests/controlplane/service/ai_app_hub_cron_test.go`（本 Task 只放这两组用例，Task 5 再补循环/重试用例）

**Interfaces:**
- Consumes: `github.com/robfig/cron/v3`。
- Produces:
  - `type SyncSchedule struct`（字段私有：`expr string`、`loc *time.Location`、`sched cron.Schedule`）
  - `func LoadSyncSchedule(getenv func(string) string) (*SyncSchedule, error)` — `AI_APP_HUB_SYNC_CRON` 为空串 → `(nil, nil)`；表达式非法 → error；`AI_APP_HUB_SYNC_TZ` 解析失败回退 `time.Local` 并 `slog.Warn`
  - `func (s *SyncSchedule) Next(lastSuccess time.Time) time.Time`
  - `func (s *SyncSchedule) IsDue(lastSuccess, now time.Time) bool`

- [ ] **Step 1: 写失败测试**

```go
func TestLoadSyncScheduleDefaults(t *testing.T) {
	env := map[string]string{}
	s, err := service.LoadSyncSchedule(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.NotNil(t, s)

	// 2026-11-02 03:00 +08 已成功 → 下一次是 11-03 03:00 +08
	last := time.Date(2026, 11, 2, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	require.Equal(t, time.Date(2026, 11, 3, 3, 0, 0, 0, time.FixedZone("CST", 8*3600)), s.Next(last))
}

func TestLoadSyncScheduleDisabledWhenCronEmpty(t *testing.T) {
	env := map[string]string{"AI_APP_HUB_SYNC_CRON": ""}
	s, err := service.LoadSyncSchedule(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Nil(t, s, "空表达式是运维开关，返回 nil 表示不启动")
}

func TestLoadSyncScheduleRejectsBadCron(t *testing.T) {
	env := map[string]string{"AI_APP_HUB_SYNC_CRON": "not-a-cron"}
	_, err := service.LoadSyncSchedule(func(k string) string { return env[k] })
	require.Error(t, err)
}

func TestIsDueUsesConfiguredTimezone(t *testing.T) {
	env := map[string]string{}
	s, err := service.LoadSyncSchedule(func(k string) string { return env[k] })
	require.NoError(t, err)

	// 昨天 03:00 成功；现在 10:20（重启补跑）→ 该跑
	last := time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.FixedZone("CST", 8*3600))
	require.True(t, s.IsDue(last, now))

	// 今天 03:00 已成功，03:00:30 再 tick → 不跑
	last = time.Date(2026, 11, 2, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	require.False(t, s.IsDue(last, time.Date(2026, 11, 2, 3, 0, 30, 0, time.FixedZone("CST", 8*3600))))

	// UTC 容器视角：2026-11-02T19:00Z 就是上海 11-03 03:00
	require.True(t, s.IsDue(last, time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)))
}

func TestLoadSyncScheduleFallsBackOnBadTimezone(t *testing.T) {
	env := map[string]string{"AI_APP_HUB_SYNC_TZ": "Mars/Base"}
	s, err := service.LoadSyncSchedule(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.NotNil(t, s, "时区坏掉不能阻止调度启动，回退 time.Local")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/controlplane/service/ -run 'TestLoadSyncSchedule|TestIsDue' -count=1`
Expected: FAIL（`LoadSyncSchedule undefined`）。

- [ ] **Step 3: 实现**

`ai_app_hub_cron.go` 顶部保留 `SyncRunner` 接口，新增 `SyncSchedule`。解析用 `cron.NewParser(cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)`（5 字段，与 `scheduled_report_service.go` 一致）。`Next` 返回 `s.sched.Next(t.In(s.loc))`；`IsDue` 返回 `!now.In(s.loc).Before(s.Next(lastSuccess))`。`LoadSyncSchedule` 里默认值与回退按 Interfaces 描述实现，`slog.Warn` 各打一行。

**此时文件里旧的 `StartAIAppHubSyncCron(ctx, runner, interval)` 先原样保留**（Task 5 才替换），保证编译不断。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./tests/controlplane/service/ -run 'TestLoadSyncSchedule|TestIsDue' -count=1`
Expected: PASS（5 个用例）。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_cron.go tests/controlplane/service/ai_app_hub_cron_test.go
git commit -m "feat(hub): 同步调度表达式与到点判定"
```

---

### Task 4: Redis 状态（last_success + leader 锁）

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_cron.go`
- Test: `tests/controlplane/service/ai_app_hub_cron_test.go`

**Interfaces:**
- Consumes: `github.com/redis/go-redis/v9`、`github.com/alicebob/miniredis/v2`。
- Produces:
  - `func NewSyncStateStore(rdb *redis.Client) *SyncStateStore`
  - `func (s *SyncStateStore) LastSuccess(ctx context.Context) (time.Time, bool, error)` — 键 `ai_app_hub:sync:last_success`，RFC3339，TTL 30 天
  - `func (s *SyncStateStore) MarkSuccess(ctx context.Context, t time.Time) error`
  - `func (s *SyncStateStore) TryLock(ctx context.Context) (release func(), ok bool)` — 键 `ai_app_hub:sync:leader`，value `hostname:pid`，TTL 10 分钟，Lua 比较后删除

- [ ] **Step 1: 写失败测试**

```go
func newSyncStateStoreForTest(t *testing.T) (*service.SyncStateStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return service.NewSyncStateStore(redis.NewClient(&redis.Options{Addr: mr.Addr()})), mr
}

func TestSyncStateStoreLastSuccessRoundTrip(t *testing.T) {
	store, _ := newSyncStateStoreForTest(t)
	ctx := context.Background()

	_, ok, err := store.LastSuccess(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	want := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	require.NoError(t, store.MarkSuccess(ctx, want))

	got, ok, err := store.LastSuccess(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.WithinDuration(t, want, got, time.Second)
}

func TestSyncStateStoreLockIsExclusiveAndOwnerScoped(t *testing.T) {
	store, _ := newSyncStateStoreForTest(t)
	ctx := context.Background()

	release, ok := store.TryLock(ctx)
	require.True(t, ok)
	_, second := store.TryLock(ctx)
	require.False(t, second, "同一时刻只允许一个持有者")

	release()
	_, third := store.TryLock(ctx)
	require.True(t, third, "释放后可以重新获取")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncStateStore' -count=1`
Expected: FAIL（`NewSyncStateStore undefined`）。

- [ ] **Step 3: 实现**

键名用常量；`TryLock` 用 `SetNX`，`release` 用 `redis.NewScript` 做 `GET==value` 才 `DEL`（照抄 `scheduled_report_service.go` 的写法）；`LastSuccess` 用 `Get` 并把 `redis.Nil` 转成 `(zero,false,nil)`；`MarkSuccess` 用 `Set` + 30 天 TTL。value 来源 `os.Hostname()` + `os.Getpid()`，构造函数里算一次。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./tests/controlplane/service/ -run 'TestSyncStateStore' -count=1`
Expected: PASS（2 个用例）。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_cron.go tests/controlplane/service/ai_app_hub_cron_test.go
git commit -m "feat(hub): 同步 last_success 与 Redis leader 锁"
```

---

### Task 5: 单次周期编排（判定 → 抢锁 → 重试 → 记账）+ tick 循环

**Files:**
- Modify: `internal/controlplane/service/ai_app_hub_cron.go`（替换旧 `StartAIAppHubSyncCron` 与 `runAIAppHubSyncOnce`）
- Test: `tests/controlplane/service/ai_app_hub_cron_test.go`（重写旧的 `TestSyncCronRunsImmediatelyAndStopsOnCancel`）

**Interfaces:**
- Consumes: Task 3 `SyncSchedule.IsDue`、Task 4 `SyncStateStore`、现有 `SyncRunner`、`ErrSyncInProgress`。
- Produces:
  - `type SyncCronOptions struct { Schedule *SyncSchedule; State *SyncStateStore; Runner SyncRunner; Tick time.Duration; Attempts int; Backoff []time.Duration; Now func() time.Time; Sleep func(context.Context, time.Duration) error }`
  - `func RunSyncCycle(ctx context.Context, opts SyncCronOptions) (ran bool, attempts int, err error)`
  - `func StartAIAppHubSyncCron(ctx context.Context, opts SyncCronOptions)`（**签名变更**，唯一调用方在 Task 6 改）
  - 默认值：`Tick=1*time.Minute`、`Attempts=3`、`Backoff={30s,2m}`、`Now=time.Now`、`Sleep=`ctx-aware timer。

- [ ] **Step 1: 写失败测试**

```go
type flakyRunner struct {
	calls int
	errs  []error // 按调用序号返回；耗尽后返回 nil
}

func (r *flakyRunner) Sync(context.Context, service.SyncScope) (service.HubSyncResult, error) {
	r.calls++
	if r.calls-1 < len(r.errs) && r.errs[r.calls-1] != nil {
		return service.HubSyncResult{}, r.errs[r.calls-1]
	}
	return service.HubSyncResult{Scope: "all"}, nil
}

func cronOptsForTest(t *testing.T, runner service.SyncRunner, now time.Time) (service.SyncCronOptions, *service.SyncStateStore) {
	t.Helper()
	sched, err := service.LoadSyncSchedule(func(k string) string {
		if k == "AI_APP_HUB_SYNC_CRON" {
			return "0 3 * * *"
		}
		return ""
	})
	require.NoError(t, err)
	require.NotNil(t, sched)
	store, _ := newSyncStateStoreForTest(t)
	return service.SyncCronOptions{
		Schedule: sched,
		State:    store,
		Runner:   runner,
		Attempts: 3,
		Now:      func() time.Time { return now },
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}, store
}

func TestRunSyncCycleRunsOnFirstStart(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, attempts, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, runner.calls, "首次没有 last_success，立即跑一次")

	_, ok, err := store.LastSuccess(context.Background())
	require.NoError(t, err)
	require.True(t, ok, "成功后才记账")

	// 同一个周期内再 tick：不重复
	ran, _, err = service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.False(t, ran)
	require.Equal(t, 1, runner.calls)
}

func TestRunSyncCycleCatchesUpAfterRestart(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)
	require.NoError(t, store.MarkSuccess(context.Background(), time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)))

	ran, _, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, ran, "昨天成功后错过今天 03:00，起来要补跑")
}

func TestRunSyncCycleSkipsWhenLockHeld(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)
	_, ok := store.TryLock(context.Background())
	require.True(t, ok)

	ran, _, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.False(t, ran)
	require.Zero(t, runner.calls)
}

func TestRunSyncCycleRetriesThenSucceeds(t *testing.T) {
	runner := &flakyRunner{errs: []error{errors.New("boom"), errors.New("boom")}}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, attempts, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, 3, attempts)
	require.Equal(t, 3, runner.calls)
	_, ok, err := store.LastSuccess(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRunSyncCycleGivesUpAndDoesNotMarkSuccess(t *testing.T) {
	runner := &flakyRunner{errs: []error{errors.New("a"), errors.New("b"), errors.New("c")}}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, attempts, err := service.RunSyncCycle(context.Background(), opts)
	require.Error(t, err)
	require.True(t, ran)
	require.Equal(t, 3, attempts)
	_, ok, err := store.LastSuccess(context.Background())
	require.NoError(t, err)
	require.False(t, ok, "失败不得记账，否则当天会静默漏跑")
}

func TestRunSyncCycleSkipsRetryWhenSyncInProgress(t *testing.T) {
	runner := &flakyRunner{errs: []error{service.ErrSyncInProgress}}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, _, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err, "手动触发正在进行不算失败")
	require.False(t, ran)
	require.Equal(t, 1, runner.calls, "不重试")
	_, ok, _ := store.LastSuccess(context.Background())
	require.False(t, ok)
}

func TestStartSyncCronTicksAndStopsOnCancel(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	sched, err := service.LoadSyncSchedule(func(k string) string {
		if k == "AI_APP_HUB_SYNC_CRON" {
			return "0 3 * * *"
		}
		return ""
	})
	require.NoError(t, err)
	require.NotNil(t, sched)
	store, _ := newSyncStateStoreForTest(t)
	ctx, cancel := context.WithCancel(context.Background())

	service.StartAIAppHubSyncCron(ctx, service.SyncCronOptions{
		Schedule: sched, State: store, Runner: runner,
		Tick: 5 * time.Millisecond, Attempts: 1,
		Now:   func() time.Time { return now },
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	require.Eventually(t, func() bool { r := runner; return r.calls >= 1 }, time.Second, 5*time.Millisecond)

	cancel()
	time.Sleep(30 * time.Millisecond)
	stopped := runner.calls
	time.Sleep(60 * time.Millisecond)
	require.Equal(t, stopped, runner.calls)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/controlplane/service/ -run 'TestRunSyncCycle|TestStartSyncCron' -count=1`
Expected: FAIL（`RunSyncCycle undefined`）。

- [ ] **Step 3: 实现**

`RunSyncCycle` 顺序：`State==nil → return (false,0,error)`；读 `LastSuccess`（读失败 → `slog.Warn` + 不跑）；`ok && !Schedule.IsDue(last, Now())` → 不跑；`TryLock` 失败 → 不跑；`defer release()`；尝试 `Attempts` 次，每次 `Runner.Sync(ctx, SyncScope{Kind: ScopeAll})`，`errors.Is(err, ErrSyncInProgress)` 立即返回 `(false,1,nil)`，成功则 `State.MarkSuccess(ctx, Now())` 并返回 `(true,n,nil)`，失败且还有下次则 `Sleep(ctx, Backoff[i])`；全部失败返回 `(true,Attempts,lastErr)`。

`StartAIAppHubSyncCron` 填默认值后起 goroutine：`ticker := time.NewTicker(opts.Tick)`，先立即 `RunSyncCycle` 一次，然后 `for { select { <-ctx.Done() → return; <-ticker.C → RunSyncCycle } }`，每次 `recover()` 包一层（照抄现有写法），日志 `ai_app_hub_sync_cycle` 带 ran/attempts/error。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./tests/controlplane/service/ -run 'TestRunSyncCycle|TestStartSyncCron' -count=1`
Expected: PASS（7 个用例）。

- [ ] **Step 5: 提交**

```bash
git add internal/controlplane/service/ai_app_hub_cron.go tests/controlplane/service/ai_app_hub_cron_test.go
git commit -m "feat(hub): 每日到点同步周期（补跑/锁/重试）"
```

---

### Task 6: 接线、契约与清理

**Files:**
- Modify: `internal/controlplane/router/router.go`（`Setup` 内 514-518 行附近；删除 `aiAppHubSyncInterval`）
- Modify: `tests/contract/contracts.json`
- Test: `tests/controlplane/router/ai_app_hub_route_test.go`

**Interfaces:**
- Consumes: Task 3 `LoadSyncSchedule`、Task 4 `NewSyncStateStore`、Task 5 `StartAIAppHubSyncCron(ctx, SyncCronOptions)`。
- Produces: 无新导出符号；`EHR_SYNC_INTERVAL` 与 `aiAppHubSyncInterval()` 从代码中消失。

- [ ] **Step 1: 写失败测试**

在 `ai_app_hub_route_test.go` 追加：

```go
// 关掉调度的开关必须真的关掉：EHR 配好了也不能有请求打出去。
func TestAIAppHubSyncCronCanBeDisabledByEnv(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	t.Setenv("EHR_BASE_URL", srv.URL)
	t.Setenv("EHR_API_TOKEN", "t")
	t.Setenv("EHR_ORG_ID", "o")
	t.Setenv("AI_APP_HUB_SYNC_CRON", "")

	db := assemblyDB(t)
	_ = assemblyEngine(t, db) // 只关心 Setup 期间不触发同步

	time.Sleep(150 * time.Millisecond)
	require.Zero(t, atomic.LoadInt32(&hits), "cron 关闭时不得访问 EHR")
}
```

（需在 import 里加 `sync/atomic`、`time`、`net/http` 已有。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./tests/controlplane/router/ -run 'TestAIAppHubSyncCronCanBeDisabledByEnv' -count=1`
Expected: FAIL（`AI_APP_HUB_SYNC_CRON` 目前没人读，旧的 `EHR_SYNC_INTERVAL` 逻辑会启动 cron 并打到 fake server）。

- [ ] **Step 3: 实现接线**

`Setup` 内把 `StartAIAppHubSyncCron(lifecycleCtx, hubSyncSvc, aiAppHubSyncInterval())` 换成：

```go
if aiAppHubEHRConfigured() {
    sched, err := service.LoadSyncSchedule(os.Getenv)
    switch {
    case err != nil:
        slog.Warn("ai_app_hub_sync_schedule_invalid", "error", err)
    case sched == nil:
        slog.Info("ai_app_hub_sync_cron_disabled")
    default:
        service.StartAIAppHubSyncCron(lifecycleCtx, service.SyncCronOptions{
            Schedule: sched,
            State:    service.NewSyncStateStore(rdb),
            Runner:   hubSyncSvc,
        })
    }
}
```

删除 `aiAppHubSyncInterval()`（及其 `EHR_SYNC_INTERVAL` 读取）；确认 `rdb` 是 `Setup` 的入参、`slog`/`os` 已 import。

- [ ] **Step 4: 更新契约**

`tests/contract/contracts.json` 的 `ai_app_hub` 节点：`success_fields` 加 `"deleted_studios"`；`status_keys` 加 `"ai_app_hub.last_sync_deleted_studios"`；在 `routes` 下补一行说明定时行为（`"schedule": "默认每天 03:00 Asia/Shanghai（AI_APP_HUB_SYNC_CRON），空串关闭"`）。

- [ ] **Step 5: 全量验证**

Run: `go build ./... && go test ./tests/controlplane/service/ ./tests/controlplane/repository/ ./tests/controlplane/handler/ ./tests/controlplane/router/ -count=1`
Expected: 全部 PASS，无 `EHR_SYNC_INTERVAL` 残留（`grep -rn EHR_SYNC_INTERVAL . --include=*.go` 为空）。

- [ ] **Step 6: 提交**

```bash
git add internal/controlplane/router/router.go tests/contract/contracts.json tests/controlplane/router/ai_app_hub_route_test.go
git commit -m "feat(hub): 控制面接入每日 03:00 同步调度"
```

---

## 与 spec 的单点偏差（已在 spec 同步）

spec §5.3 原写"Redis 未配置时降级为不加锁单实例运行"。实现上 `State` 为 `nil` 直接不启动（控制面始终有 rdb；测试用 miniredis），比维护一条无法验证的降级路径更省。spec 已改为该表述。
