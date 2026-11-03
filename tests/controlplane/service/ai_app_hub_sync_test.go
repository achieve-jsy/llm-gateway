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
