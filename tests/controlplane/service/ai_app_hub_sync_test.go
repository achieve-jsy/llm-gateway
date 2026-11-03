package service_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/require"
)

// 规则表按真实 departmentId 配置，测试 fixture 也用真实 ID，否则规则命中不了。
const (
	pubDeptID  = "0B3CF753-9D6D-46E8-89C2-8E752D2A460F" // 公共关系事业部
	xlabDeptID = "53E83585-6CE3-48EF-8255-520BBAFCB156" // 战略发展中心 -> 技术研究工作室
)

type fakeFetcher struct {
	items []ehrclient.Department
	err   error
}

func (f fakeFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	return f.items, f.err
}

func threeNodes() []ehrclient.Department {
	return []ehrclient.Department{
		{ID: pubDeptID, Name: "公共关系事业部", Type: 2, LeaderPassport: "fandongbo"},
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2, LeaderPassport: "hezhiguo"},
		{ID: "tlab", Name: "T-Lab", ParentID: xlabDeptID, Type: 4},
	}
}

func TestSyncAllDerivesStudiosAndBackfills(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 3, got.Departments)
	require.Equal(t, 2, got.Studios)
	require.Equal(t, 1, got.Hidden)
	require.Equal(t, "all", got.Scope)

	studios, err := repo.ListStudios(context.Background(), false)
	require.NoError(t, err)
	byDept := map[string]model.AIAppStudio{}
	for _, s := range studios {
		byDept[*s.DepartmentID] = s
	}
	require.False(t, byDept[pubDeptID].Visible, "公共关系事业部默认不展示")
	require.Equal(t, "技术研究工作室", byDept[xlabDeptID].Name, "战略发展中心改名")

	d, err := repo.GetDepartment(context.Background(), "tlab")
	require.NoError(t, err)
	require.NotNil(t, d.StudioID)
	require.Equal(t, byDept[xlabDeptID].ID, *d.StudioID)

	status, err := settings.GetValue("ai_app_hub.last_sync_status")
	require.NoError(t, err)
	require.Equal(t, "success", status)
}

func TestSyncAllKeepsManualVisibility(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())

	_, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE ai_app_studios SET visible = 1 WHERE department_id = ?", pubDeptID).Error)

	_, err = svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	got, err := repo.GetStudioByDepartmentID(context.Background(), pubDeptID)
	require.NoError(t, err)
	require.True(t, got.Visible, "运营改过的可见性不能被同步打回去")
}

func TestSyncAllHidesVanishedStudios(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()

	first := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())
	_, err := first.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	// 第二轮：战略发展中心从 EHR 消失（公共关系事业部本来就按规则隐藏，不参与计数）
	second := service.NewAIAppHubSyncService(fakeFetcher{items: []ehrclient.Department{
		{ID: pubDeptID, Name: "公共关系事业部", Type: 2, LeaderPassport: "fandongbo"},
	}}, repo, settings, service.DefaultSyncRules())
	got, err := second.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, got.DeletedStudios)

	gone, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
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

	// 公共关系事业部这条本来按规则隐藏，先手动点亮：这样"错把 scope 内的对账套到全员"
	// 才会真的命中它（否则 UPDATE 打不到 visible=false 的行，测试恒真）。
	require.NoError(t, db.Exec("UPDATE ai_app_studios SET visible = 1 WHERE department_id = ?", pubDeptID).Error)

	// 只同步 xlab 单部门：公共关系事业部不在 targets，但也绝不能因此被隐藏
	got, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeDepartment, ID: xlabDeptID})
	require.NoError(t, err)
	require.Zero(t, got.DeletedStudios)

	kept, err := repo.GetStudioByDepartmentID(ctx, pubDeptID)
	require.NoError(t, err)
	require.True(t, kept.Visible, "scope=department 不得做全量对账")
}

func TestSyncDoesNotReviveManuallyHiddenStudio(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	require.NoError(t, db.Exec("UPDATE ai_app_studios SET visible = 0 WHERE department_id = ?", xlabDeptID).Error)

	_, err = svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	still, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
	require.NoError(t, err)
	require.False(t, still.Visible, "同步不得把运营关闭的工作室改回可见")
}

func TestSyncAllSkipsReconcileWhenDepartmentCountDropsSharply(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	// 上次成功记到 40 个部门，这次上游只回 1 个（战略发展中心消失）：按"部分树"处理
	require.NoError(t, settings.SetMultiple(map[string]string{"ai_app_hub.last_sync_departments": "40"}))
	partial := service.NewAIAppHubSyncService(fakeFetcher{items: []ehrclient.Department{
		{ID: pubDeptID, Name: "公共关系事业部", Type: 2, LeaderPassport: "fandongbo"},
	}}, repo, settings, service.DefaultSyncRules())

	got, err := partial.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Zero(t, got.DeletedStudios, "部门数从 40 掉到 1 属于上游异常，不得批量隐藏")

	still, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
	require.NoError(t, err)
	require.True(t, still.Visible, "工作室必须都还在")
}
