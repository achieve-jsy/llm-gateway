package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/require"
)

// 可变的 fetcher：同一个 repo 上连续跑两次同步，模拟 EHR 数据变化。
type mutableFetcher struct {
	items []ehrclient.Department
}

func (f *mutableFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	return f.items, nil
}

func TestSyncDepartmentWritesOnlyThatNode(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: "d2", Name: "d2-旧名字", ParentID: xlabDeptID, Type: 4},
		{ID: "d3", Name: "d3-旧名字", ParentID: xlabDeptID, Type: 4},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	f.items = []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: "d2", Name: "d2-新名字", ParentID: xlabDeptID, Type: 4},
		{ID: "d3", Name: "d3-新名字", ParentID: xlabDeptID, Type: 4},
	}
	got, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeDepartment, ID: "d2"})
	require.NoError(t, err)
	require.Equal(t, 1, got.Departments)

	d2, err := repo.GetDepartment(ctx, "d2")
	require.NoError(t, err)
	require.Equal(t, "d2-新名字", d2.Name)

	d3, err := repo.GetDepartment(ctx, "d3")
	require.NoError(t, err)
	require.Equal(t, "d3-旧名字", d3.Name, "单部门同步不碰兄弟节点")
}

func TestSyncDepartmentOnStudioNodeUpdatesStudioRow(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2, LeaderPassport: "hezhiguo"},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	f.items = []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2, LeaderName: "谭萌", LeaderPassport: "tanmeng"},
	}
	_, err = svc.Sync(ctx, service.SyncScope{Kind: service.ScopeDepartment, ID: xlabDeptID})
	require.NoError(t, err)

	studio, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
	require.NoError(t, err)
	require.Equal(t, "tanmeng", studio.LeaderPassport, "事业部节点的同步要落到工作室行")
}

func TestSyncStudioWritesSubtree(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: "d1", Name: "d1-旧", ParentID: xlabDeptID, Type: 4},
		{ID: "g1", Name: "g1-旧", ParentID: "d1", Type: 5},
		{ID: pubDeptID, Name: "公共关系事业部", Type: 2},
		{ID: "other", Name: "other-旧", ParentID: pubDeptID, Type: 4},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	studio, err := repo.GetStudioByDepartmentID(ctx, xlabDeptID)
	require.NoError(t, err)

	f.items = []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: "d1", Name: "d1-新", ParentID: xlabDeptID, Type: 4},
		{ID: "g1", Name: "g1-新", ParentID: "d1", Type: 5},
		{ID: pubDeptID, Name: "公共关系事业部", Type: 2},
		{ID: "other", Name: "other-新", ParentID: pubDeptID, Type: 4},
	}
	got, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeStudio, ID: studio.ID.String()})
	require.NoError(t, err)
	require.Equal(t, 3, got.Departments, "工作室 + 两级后代")

	for _, id := range []string{"d1", "g1"} {
		d, err := repo.GetDepartment(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, d.StudioID)
		require.Equal(t, studio.ID, *d.StudioID)
	}
	other, err := repo.GetDepartment(ctx, "other")
	require.NoError(t, err)
	require.Equal(t, "other-旧", other.Name, "别的工作室子树不受影响")
}

func TestSyncStudioWithManualStudioReturnsNotFound(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()
	manual := &model.AIAppStudio{Name: "手工工作室", Source: "manual", Visible: true}
	inserted, err := repo.InsertStudioIfAbsent(ctx, manual)
	require.NoError(t, err)
	require.True(t, inserted)
	created, err := repo.GetStudioByID(ctx, manual.ID)
	require.NoError(t, err)

	f := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
	}}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())
	_, err = svc.Sync(ctx, service.SyncScope{Kind: service.ScopeStudio, ID: created.ID.String()})
	require.Error(t, err)
	require.True(t, errors.Is(err, service.ErrScopeNotFound))
}
