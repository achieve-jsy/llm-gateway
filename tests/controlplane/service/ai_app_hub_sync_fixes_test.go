package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/require"
)

// C2：单部门同步不能把已有的 studio_id 清空。
func TestSyncDepartmentKeepsExistingStudioID(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()

	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: "d2", Name: "d2", ParentID: xlabDeptID, Type: 4},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	before, err := repo.GetDepartment(ctx, "d2")
	require.NoError(t, err)
	require.NotNil(t, before.StudioID)

	_, err = svc.Sync(ctx, service.SyncScope{Kind: service.ScopeDepartment, ID: "d2"})
	require.NoError(t, err)

	after, err := repo.GetDepartment(ctx, "d2")
	require.NoError(t, err)
	require.NotNil(t, after.StudioID, "单部门同步不得清空 studio_id")
	require.Equal(t, *before.StudioID, *after.StudioID)
}

// EHR 里有 parentId 为空的孤儿部门（杭州舰宇/杭州边翼（新）/暂存部门）：
// 归属推不出来 → 不能静默丢掉，要计数（管理页能看到有 7 个部门不可见）。
func TestSyncCountsOrphanDepartments(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()

	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: "orphan", Name: "杭州舰宇", Type: 4}, // 没有 parentId
		{ID: "orphan-child", Name: "至极", ParentID: "orphan", Type: 5},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 2, got.UnmappedDepartments, "孤儿部门自身 + 它的下级都算不出来归属")

	orphan, err := repo.GetDepartment(ctx, "orphan-child")
	require.NoError(t, err)
	require.Nil(t, orphan.StudioID, "推不出归属时保持原值（NULL），不能瞎挂")
}

// I1：写库中途失败也要把状态记为 failed。
func TestSyncMarksFailedWhenWriteFails(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()

	require.NoError(t, db.Exec("CREATE TRIGGER boom BEFORE INSERT ON ai_app_departments WHEN NEW.department_id = 'BOOM' BEGIN SELECT RAISE(ABORT, 'boom'); END").Error)

	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: "BOOM", Name: "会炸的部门", ParentID: xlabDeptID, Type: 4},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.Error(t, err)

	status, err := settings.GetValue("ai_app_hub.last_sync_status")
	require.NoError(t, err)
	require.Equal(t, "failed", status)
}

// I5：空 scope 不能被当成全量同步执行。
func TestSyncRejectsEmptyScopeKind(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	f := &mutableFetcher{items: []ehrclient.Department{{ID: xlabDeptID, Name: "战略发展中心", Type: 2}}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())

	_, err := svc.Sync(context.Background(), service.SyncScope{})
	require.Error(t, err)
	studios, err := repo.ListStudios(context.Background(), false)
	require.NoError(t, err)
	require.Empty(t, studios, "空 scope 不得写任何数据")
}

// C1/I3：管理员判定必须用 user_roles.user_id（真实列名）。
func TestAuthorizeAdminUsesUserIDColumn(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	require.NoError(t, db.Exec("INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES (?, 'admin', 'company', '', 'seed')", "user-uuid-1").Error)

	svc := service.NewAIAppHubSyncService(&mutableFetcher{}, repo, settings, service.DefaultSyncRules())
	ok, err := svc.Authorize(ctx, "user-uuid-1", "any-passport", service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.True(t, ok, "管理员（user_roles.user_id）应能触发全量")
}

// C1：负责人只能触发自己工作室的同步；其他人不行。
func TestAuthorizeStudioLeaderByPassport(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()

	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2, LeaderPassport: "hezhiguo"},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	studio, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
	require.NoError(t, err)
	scope := service.SyncScope{Kind: service.ScopeStudio, ID: studio.ID.String()}

	ok, err := svc.Authorize(ctx, "user-uuid-2", "hezhiguo", scope)
	require.NoError(t, err)
	require.True(t, ok, "工作室负责人可触发自己工作室的同步")

	ok, err = svc.Authorize(ctx, "user-uuid-3", "zhangsan", scope)
	require.NoError(t, err)
	require.False(t, ok, "其他人不行")

	ok, err = svc.Authorize(ctx, "user-uuid-4", "zhangsan", service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.False(t, ok, "非管理员不能触发全量")
}

// I2：本地还没有的部门，应报"范围不存在"而不是当成没权限。
func TestAuthorizeUnknownDepartmentReturnsNotFound(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	svc := service.NewAIAppHubSyncService(&mutableFetcher{}, repo, settings, service.DefaultSyncRules())

	ok, err := svc.Authorize(context.Background(), "user-uuid-5", "zhangsan", service.SyncScope{Kind: service.ScopeDepartment, ID: "not-synced-yet"})
	require.False(t, ok)
	require.True(t, errors.Is(err, service.ErrScopeNotFound), "got %v", err)
}
