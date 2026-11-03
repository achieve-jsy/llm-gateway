package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	controlmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// fakeSnapshotModelRepo 实现 service.ModelRepoInterface，本测试只关心 ListAvailable。
type fakeSnapshotModelRepo struct {
	listAvailableFunc func() ([]controlmodel.Model, error)
}

func (f *fakeSnapshotModelRepo) Create(*controlmodel.Model) error { return nil }
func (f *fakeSnapshotModelRepo) GetByID(uuid.UUID) (*controlmodel.Model, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSnapshotModelRepo) GetByIDs([]uuid.UUID) (map[uuid.UUID]*controlmodel.Model, error) {
	return nil, nil
}
func (f *fakeSnapshotModelRepo) List(repository.ModelListFilter, int, int) ([]controlmodel.Model, int64, error) {
	return nil, 0, nil
}
func (f *fakeSnapshotModelRepo) Update(*controlmodel.Model) error { return nil }
func (f *fakeSnapshotModelRepo) Delete(uuid.UUID) error           { return nil }
func (f *fakeSnapshotModelRepo) ExistsByName(string, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *fakeSnapshotModelRepo) ExistsByNameExcluding(string, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *fakeSnapshotModelRepo) CountByProviderID(uuid.UUID) (int64, error) { return 0, nil }
func (f *fakeSnapshotModelRepo) BatchCountByProviderID([]uuid.UUID) (map[uuid.UUID]int64, error) {
	return nil, nil
}
func (f *fakeSnapshotModelRepo) ListAvailable() ([]controlmodel.Model, error) {
	if f.listAvailableFunc != nil {
		return f.listAvailableFunc()
	}
	return nil, nil
}
func (f *fakeSnapshotModelRepo) UpdateEnabledByProviderID(uuid.UUID, bool) error { return nil }
func (f *fakeSnapshotModelRepo) BatchDelete([]uuid.UUID) (int64, error)          { return 0, nil }
func (f *fakeSnapshotModelRepo) BatchUpdateEnabled([]uuid.UUID, bool) (int64, error) {
	return 0, nil
}
func (f *fakeSnapshotModelRepo) DeleteModelTagRelations(uuid.UUID) error { return nil }
func (f *fakeSnapshotModelRepo) GetHealthByNames([]string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}
func (f *fakeSnapshotModelRepo) UpdateEnabled(uuid.UUID, *bool) error { return nil }
func (f *fakeSnapshotModelRepo) UpdateStatus(uuid.UUID, bool, int) error {
	return nil
}
func (f *fakeSnapshotModelRepo) AutoDisableModel(uuid.UUID) error { return nil }
func (f *fakeSnapshotModelRepo) AutoEnableModel(uuid.UUID) error  { return nil }
func (f *fakeSnapshotModelRepo) IncrementModelFailures(uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeSnapshotModelRepo) ResetModelFailures(uuid.UUID) error { return nil }
func (f *fakeSnapshotModelRepo) UpdateModelCheckResult(uuid.UUID, int, string) error {
	return nil
}
func (f *fakeSnapshotModelRepo) IncrementRecoverySuccesses(uuid.UUID) error { return nil }
func (f *fakeSnapshotModelRepo) ResetRecoverySuccesses(uuid.UUID) error     { return nil }

// fakeGroupNameLister 实现 service.GroupNameLister。
type fakeGroupNameLister struct {
	names []string
	err   error
	calls int
}

func (f *fakeGroupNameLister) ListEnabledGroupNames() ([]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.names, nil
}

// newSnapshotTestRedis 启动 miniredis 并返回客户端。
func newSnapshotTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

// newSnapshotTestModel 构造一个带可用加密凭据的可用模型，使快照 entries 非空。
func newSnapshotTestModel(t *testing.T) controlmodel.Model {
	t.Helper()
	crypto.InitForTesting()
	encrypted, err := crypto.Encrypt(`{"api_key":"sk-test"}`)
	require.NoError(t, err)

	providerID := uuid.New()
	return controlmodel.Model{
		ID:         uuid.New(),
		Name:       "test-provider/gpt-test",
		ProviderID: providerID,
		ModelType:  "chat",
		Provider: controlmodel.Provider{
			ID:                   providerID,
			Name:                 "test-provider",
			Type:                 "openai",
			EncryptedCredentials: encrypted,
		},
	}
}

func loadSnapshot(t *testing.T, rdb *redis.Client) *configsync.ProviderSnapshot {
	t.Helper()
	snapshot, err := configsync.LoadProviderSnapshot(context.Background(), rdb)
	require.NoError(t, err, "快照应已写入 Redis")
	return snapshot
}

// 注入 GroupNameLister 后，provider 快照应携带启用组名（供网关冷启动预热）。
func TestRefreshAvailableSnapshotIncludesGroupNames(t *testing.T) {
	_, rdb := newSnapshotTestRedis(t)
	lister := &fakeGroupNameLister{names: []string{"gpt-5.6-luna", "gpt-5.6-terra"}}

	refresher := service.NewProviderSnapshotRefresher(&fakeSnapshotModelRepo{
		listAvailableFunc: func() ([]controlmodel.Model, error) {
			return []controlmodel.Model{newSnapshotTestModel(t)}, nil
		},
	}, rdb)
	refresher.SetGroupNameLister(lister)

	version, err := refresher.RefreshAvailableSnapshot(context.Background())
	require.NoError(t, err)
	require.NotZero(t, version)
	assert.Equal(t, 1, lister.calls, "注入 lister 后应恰好读取一次组名")

	snapshot := loadSnapshot(t, rdb)
	assert.Equal(t, version, snapshot.Version)
	assert.Len(t, snapshot.Entries, 1, "provider entries 不应受组名读取影响")
	assert.Equal(t, []string{"gpt-5.6-luna", "gpt-5.6-terra"}, snapshot.GroupNames)
}

// 组名读取失败只能告警：provider 快照必须照常写出，函数仍返回 nil error、组名为空。
func TestRefreshAvailableSnapshotGroupNameErrorIsNotFatal(t *testing.T) {
	_, rdb := newSnapshotTestRedis(t)
	lister := &fakeGroupNameLister{err: errors.New("list enabled group names: db down")}

	refresher := service.NewProviderSnapshotRefresher(&fakeSnapshotModelRepo{
		listAvailableFunc: func() ([]controlmodel.Model, error) {
			return []controlmodel.Model{newSnapshotTestModel(t)}, nil
		},
	}, rdb)
	refresher.SetGroupNameLister(lister)

	version, err := refresher.RefreshAvailableSnapshot(context.Background())
	require.NoError(t, err, "组名读取失败不得改变 RefreshAvailableSnapshot 的错误语义")
	require.NotZero(t, version)
	assert.Equal(t, 1, lister.calls)

	snapshot := loadSnapshot(t, rdb)
	assert.Equal(t, version, snapshot.Version)
	assert.Len(t, snapshot.Entries, 1, "组名读取失败不得影响 provider 快照写入")
	assert.Empty(t, snapshot.GroupNames, "组名读取失败时快照不应携带组名")
}

// 未注入 GroupNameLister（旧装配/测试 fake）时不得 panic，组名为空。
func TestRefreshAvailableSnapshotWithoutGroupNameLister(t *testing.T) {
	_, rdb := newSnapshotTestRedis(t)

	refresher := service.NewProviderSnapshotRefresher(&fakeSnapshotModelRepo{
		listAvailableFunc: func() ([]controlmodel.Model, error) {
			return []controlmodel.Model{newSnapshotTestModel(t)}, nil
		},
	}, rdb)

	version, err := refresher.RefreshAvailableSnapshot(context.Background())
	require.NoError(t, err)
	require.NotZero(t, version)

	snapshot := loadSnapshot(t, rdb)
	assert.Len(t, snapshot.Entries, 1)
	assert.Empty(t, snapshot.GroupNames)
}

// nil 接收者 / nil 依赖必须 nil 安全。
func TestProviderSnapshotRefresherNilSafety(t *testing.T) {
	var nilRefresher *service.ProviderSnapshotRefresher
	require.NotPanics(t, func() { nilRefresher.SetGroupNameLister(&fakeGroupNameLister{}) })
	version, err := nilRefresher.RefreshAvailableSnapshot(context.Background())
	require.NoError(t, err)
	assert.Zero(t, version)

	_, rdb := newSnapshotTestRedis(t)
	refresher := service.NewProviderSnapshotRefresher(&fakeSnapshotModelRepo{}, rdb)
	require.NotPanics(t, func() { refresher.SetGroupNameLister(nil) })
	version, err = refresher.RefreshAvailableSnapshot(context.Background())
	require.NoError(t, err)
	require.NotZero(t, version)
}

// newGroupTestDB 建一张与生产 schema 等价的 model_groups 表（sqlite 内存库）。
func newGroupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.Exec(`CREATE TABLE model_groups (
		id integer PRIMARY KEY AUTOINCREMENT,
		created_at datetime,
		updated_at datetime,
		deleted_at datetime,
		name text NOT NULL,
		description text,
		model_type text DEFAULT 'chat',
		strategy text DEFAULT 'round_robin',
		enabled numeric NOT NULL DEFAULT 1,
		max_switches integer DEFAULT 2,
		same_model_retries integer DEFAULT 1,
		failover_cooldown_seconds integer DEFAULT 60,
		timeout_seconds integer DEFAULT 0,
		open_mode text DEFAULT 'public',
		is_capability_override numeric DEFAULT 0,
		override_supports_vision numeric DEFAULT 0,
		override_supports_function_calling numeric DEFAULT 0,
		override_supports_tool_choice numeric DEFAULT 0,
		override_mode text,
		override_context_length integer,
		override_max_output_tokens integer,
		quota_rpm integer,
		quota_max_concurrency integer,
		budget_tier_id text,
		sticky_enabled numeric,
		sticky_ttl_seconds integer
	)`).Error)

	// FindByID 会 Preload("Models")；组模型表只需能被查询即可（本测试不关心成员）。
	require.NoError(t, db.Exec(`CREATE TABLE group_models (
		id integer PRIMARY KEY AUTOINCREMENT,
		group_id integer,
		model_id text,
		model_name text,
		provider_name text,
		priority integer,
		weight integer,
		complexity_tier text DEFAULT 'MEDIUM',
		created_at datetime,
		updated_at datetime
	)`).Error)

	// FindByID 会 Preload("BudgetTier")；budget_tier_id 为 NULL，仅需表存在。
	require.NoError(t, db.Exec(`CREATE TABLE model_tiers (id text PRIMARY KEY)`).Error)

	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}

func assertSnapshotGroupNames(t *testing.T, rdb *redis.Client, expected []string) {
	t.Helper()
	snapshot := loadSnapshot(t, rdb)
	assert.Equal(t, expected, snapshot.GroupNames)
}

// waitForSnapshotGroupNames 等待（最多 3s，每 10ms 轮询）快照里的组名收敛到期望值。
// 组变更后的 provider 快照刷新是异步的（R4：不阻塞管理面请求），因此断言必须等待收敛，
// 而不是紧跟调用立即断言；这里用有界轮询而不是固定 sleep，既稳定又不会拖慢用例。
func waitForSnapshotGroupNames(t *testing.T, rdb *redis.Client, expected []string) {
	t.Helper()
	// 组变更走 ProviderSnapshotRefresher.ScheduleRefresh 的合并去抖调度：
	// 相邻两次重建之间默认有 2s 最小间隔（defaultSnapshotRefreshMinInterval），
	// 因此等待窗口必须显著大于该节流间隔，避免在慢机器上误报。
	deadline := time.Now().Add(8 * time.Second)
	var last []string
	for time.Now().Before(deadline) {
		if snapshot, err := configsync.LoadProviderSnapshot(context.Background(), rdb); err == nil {
			last = snapshot.GroupNames
			if len(last) == len(expected) {
				matched := true
				for i := range last {
					if last[i] != expected[i] {
						matched = false
						break
					}
				}
				if matched {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("快照组名未在超时内收敛：期望 %v，实际 %v", expected, last)
}

// ListEnabledGroupNames 只能返回 enabled=true 且未软删的组名，并按 name 升序（快照内容需确定）。
func TestGroupRepoListEnabledGroupNames(t *testing.T) {
	db := newGroupTestDB(t)
	groupRepo := repository.NewGroupRepo(db)

	newGroup := func(name string, enabled bool) uint {
		t.Helper()
		group := &controlmodel.Group{
			Name:      name,
			ModelType: "chat",
			Strategy:  "round_robin",
			Enabled:   enabled,
		}
		require.NoError(t, groupRepo.Create(group))
		return group.ID
	}

	// 插入顺序刻意与字典序相反，确保断言能识破「未排序」的实现。
	newGroup("zeta", true)
	newGroup("alpha", true)
	// 注意：Create 时 Enabled 带 gorm default:true，GORM 会忽略零值（false）而落库默认值 true，
	// 因此这里直接改 DB 状态，构造真实的「已停用」行。
	disabledID := newGroup("mid-disabled", true)
	require.NoError(t, db.Exec("UPDATE model_groups SET enabled = 0 WHERE id = ?", disabledID).Error)
	deletedID := newGroup("deleted-group", true)
	require.NoError(t, groupRepo.SoftDelete(deletedID))

	names, err := groupRepo.ListEnabledGroupNames()
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "zeta"}, names)
}

// 组的新增/更新/删除（含启停切换）成功后，必须触发一次快照刷新，让新组名落进 Redis。
func TestGroupMutationTriggersSnapshotRefresh(t *testing.T) {
	_, rdb := newSnapshotTestRedis(t)
	db := newGroupTestDB(t)

	groupRepo := repository.NewGroupRepo(db)
	refresher := service.NewProviderSnapshotRefresher(&fakeSnapshotModelRepo{}, rdb)
	refresher.SetGroupNameLister(groupRepo)

	groupSvc := service.NewGroupService(groupRepo, nil)
	groupSvc.SetSnapshotRefresher(refresher)

	// 创建 → 组名进入快照
	group, err := groupSvc.Create(service.CreateGroupRequest{
		Name:     "gpt-5.6-terra",
		Strategy: "round_robin",
	})
	require.NoError(t, err)
	require.NotZero(t, group.ID)
	waitForSnapshotGroupNames(t, rdb, []string{"gpt-5.6-terra"})

	// 停用 → 组名从快照移除
	disabled := false
	_, err = groupSvc.Update(group.ID, service.UpdateGroupRequest{Enabled: &disabled})
	require.NoError(t, err)
	waitForSnapshotGroupNames(t, rdb, nil)

	// 重新启用 + 改名 → 快照携带新组名
	enabled := true
	renamed := "gpt-5.6-luna"
	_, err = groupSvc.Update(group.ID, service.UpdateGroupRequest{Enabled: &enabled, Name: &renamed})
	require.NoError(t, err)
	waitForSnapshotGroupNames(t, rdb, []string{"gpt-5.6-luna"})

	// 删除 → 组名从快照移除
	require.NoError(t, groupSvc.Delete(group.ID))
	waitForSnapshotGroupNames(t, rdb, nil)
}

// 快照刷新失败不得改变组服务的返回值/错误语义（此处快照写入目标 Redis 不可用）。
func TestGroupMutationSnapshotFailureDoesNotChangeResult(t *testing.T) {
	mr := miniredis.RunT(t)
	// MaxRetries=-1：关掉 Redis 后立刻失败，避免默认重试拖慢用例。
	rdb := redis.NewClient(&redis.Options{
		Addr:        mr.Addr(),
		MaxRetries:  -1,
		DialTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	db := newGroupTestDB(t)

	groupRepo := repository.NewGroupRepo(db)
	refresher := service.NewProviderSnapshotRefresher(&fakeSnapshotModelRepo{}, rdb)
	refresher.SetGroupNameLister(groupRepo)

	groupSvc := service.NewGroupService(groupRepo, nil)
	groupSvc.SetSnapshotRefresher(refresher)

	// 关掉 Redis：快照写入必然失败
	mr.Close()

	group, err := groupSvc.Create(service.CreateGroupRequest{
		Name:     "gpt-5.6-terra",
		Strategy: "round_robin",
	})
	require.NoError(t, err, "快照刷新失败不得让组创建失败")
	require.NotZero(t, group.ID)

	disabled := false
	_, err = groupSvc.Update(group.ID, service.UpdateGroupRequest{Enabled: &disabled})
	require.NoError(t, err, "快照刷新失败不得让组更新失败")

	require.NoError(t, groupSvc.Delete(group.ID), "快照刷新失败不得让组删除失败")
}

// slowSnapshotModelRepo 的 ListAvailable 会阻塞到 release 被关闭，用于证明组变更请求
// 不等待快照刷新完成（R4：刷新必须异步 + 带超时）。
type slowSnapshotModelRepo struct {
	available []controlmodel.Model
	started   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (f *slowSnapshotModelRepo) Create(*controlmodel.Model) error { return nil }
func (f *slowSnapshotModelRepo) GetByID(uuid.UUID) (*controlmodel.Model, error) {
	return nil, errors.New("not implemented")
}
func (f *slowSnapshotModelRepo) GetByIDs([]uuid.UUID) (map[uuid.UUID]*controlmodel.Model, error) {
	return nil, nil
}
func (f *slowSnapshotModelRepo) List(repository.ModelListFilter, int, int) ([]controlmodel.Model, int64, error) {
	return nil, 0, nil
}
func (f *slowSnapshotModelRepo) Update(*controlmodel.Model) error { return nil }
func (f *slowSnapshotModelRepo) Delete(uuid.UUID) error           { return nil }
func (f *slowSnapshotModelRepo) ExistsByName(string, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *slowSnapshotModelRepo) ExistsByNameExcluding(string, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *slowSnapshotModelRepo) CountByProviderID(uuid.UUID) (int64, error) { return 0, nil }
func (f *slowSnapshotModelRepo) BatchCountByProviderID([]uuid.UUID) (map[uuid.UUID]int64, error) {
	return nil, nil
}
func (f *slowSnapshotModelRepo) ListAvailable() ([]controlmodel.Model, error) {
	f.once.Do(func() { close(f.started) })
	<-f.release
	return f.available, nil
}
func (f *slowSnapshotModelRepo) UpdateEnabledByProviderID(uuid.UUID, bool) error { return nil }
func (f *slowSnapshotModelRepo) BatchDelete([]uuid.UUID) (int64, error)          { return 0, nil }
func (f *slowSnapshotModelRepo) BatchUpdateEnabled([]uuid.UUID, bool) (int64, error) {
	return 0, nil
}
func (f *slowSnapshotModelRepo) DeleteModelTagRelations(uuid.UUID) error { return nil }
func (f *slowSnapshotModelRepo) GetHealthByNames([]string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}
func (f *slowSnapshotModelRepo) UpdateEnabled(uuid.UUID, *bool) error { return nil }
func (f *slowSnapshotModelRepo) UpdateStatus(uuid.UUID, bool, int) error {
	return nil
}
func (f *slowSnapshotModelRepo) AutoDisableModel(uuid.UUID) error                    { return nil }
func (f *slowSnapshotModelRepo) AutoEnableModel(uuid.UUID) error                     { return nil }
func (f *slowSnapshotModelRepo) IncrementModelFailures(uuid.UUID) (int, error)       { return 0, nil }
func (f *slowSnapshotModelRepo) ResetModelFailures(uuid.UUID) error                  { return nil }
func (f *slowSnapshotModelRepo) UpdateModelCheckResult(uuid.UUID, int, string) error { return nil }
func (f *slowSnapshotModelRepo) IncrementRecoverySuccesses(uuid.UUID) error          { return nil }
func (f *slowSnapshotModelRepo) ResetRecoverySuccesses(uuid.UUID) error              { return nil }

// TestGroupMutationDoesNotBlockOnSlowSnapshotRefresh：组变更后的 provider 快照刷新
// 不得卡住管理面请求（Create/Update/Delete 必须立刻返回，刷新在后台进行）。
func TestGroupMutationDoesNotBlockOnSlowSnapshotRefresh(t *testing.T) {
	_, rdb := newSnapshotTestRedis(t)
	db := newGroupTestDB(t)

	repo := &slowSnapshotModelRepo{
		available: []controlmodel.Model{newSnapshotTestModel(t)},
		started:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	refresher := service.NewProviderSnapshotRefresher(repo, rdb)

	groupRepo := repository.NewGroupRepo(db)
	groupSvc := service.NewGroupService(groupRepo, nil)
	groupSvc.SetSnapshotRefresher(refresher)

	created := make(chan *controlmodel.Group, 1)
	createErr := make(chan error, 1)
	go func() {
		group, err := groupSvc.Create(service.CreateGroupRequest{
			Name:     "gpt-5.6-terra",
			Strategy: "round_robin",
		})
		if err != nil {
			createErr <- err
			return
		}
		created <- group
	}()

	select {
	case group := <-created:
		require.NotZero(t, group.ID, "组创建应成功返回")
	case err := <-createErr:
		close(repo.release)
		t.Fatalf("组创建失败: %v", err)
	case <-time.After(2 * time.Second):
		close(repo.release) // 解锁后台刷新，避免 goroutine 泄漏
		t.Fatal("组创建被同步的 provider 快照刷新阻塞：刷新必须异步执行")
	}

	// 刷新确实在后台启动了（而不是根本没触发）
	select {
	case <-repo.started:
	case <-time.After(2 * time.Second):
		close(repo.release)
		t.Fatal("组变更后应在后台触发一次 provider 快照刷新")
	}

	close(repo.release)
}

// 未注入 snapshotter 时，组服务行为与现状完全一致（不含任何刷新副作用）。
func TestGroupMutationWithoutSnapshotRefresher(t *testing.T) {
	db := newGroupTestDB(t)
	groupRepo := repository.NewGroupRepo(db)
	groupSvc := service.NewGroupService(groupRepo, nil)

	group, err := groupSvc.Create(service.CreateGroupRequest{
		Name:     "gpt-5.6-terra",
		Strategy: "round_robin",
	})
	require.NoError(t, err)
	require.NotZero(t, group.ID)
	require.NoError(t, groupSvc.Delete(group.ID))
}
