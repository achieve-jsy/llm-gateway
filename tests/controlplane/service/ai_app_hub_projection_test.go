package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// sliceFetcher 每次调用取最新 items，用来模拟"EHR 改名 / 撤并"这种跨轮变化。
type sliceFetcher struct {
	items func() []ehrclient.Department
}

func (f sliceFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	return f.items(), nil
}

func newProjectionSvc(t *testing.T, items []ehrclient.Department) (*service.AIAppHubSyncService, *gorm.DB, func([]ehrclient.Department)) {
	t.Helper()
	db, settings := newAIHubTestDBWithSettings(t)
	current := items
	svc := service.NewAIAppHubSyncService(
		sliceFetcher{items: func() []ehrclient.Department { return current }},
		repository.NewAIAppHubRepo(db), settings, service.DefaultSyncRules(),
	).WithProjection(orgrepo.NewEHRProjectionRepo(db))
	return svc, db, func(next []ehrclient.Department) { current = next }
}

func loadLegacyStudios(t *testing.T, db *gorm.DB) map[string]orgmodel.Studio {
	t.Helper()
	var rows []orgmodel.Studio
	require.NoError(t, db.Find(&rows).Error)
	out := make(map[string]orgmodel.Studio, len(rows))
	for _, s := range rows {
		require.NotNil(t, s.EHRDepartmentID)
		out[*s.EHRDepartmentID] = s
	}
	return out
}

// 投影把 EHR 事业部写进老表；第二次同输入差量为 0；改名只改属性。
func TestSyncProjectsStudiosToLegacyTables(t *testing.T) {
	svc, db, setItems := newProjectionSvc(t, threeNodes())
	ctx := context.Background()

	got, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 2, got.Diff.Studios.Inserted, "两个事业部首次投影都是新增")
	require.Equal(t, 0, got.Diff.Studios.Updated)
	require.Equal(t, 0, got.Diff.Studios.Deleted)

	byEHR := loadLegacyStudios(t, db)
	require.Len(t, byEHR, 2)
	require.Equal(t, "技术研究工作室", byEHR[xlabDeptID].Name, "NameOverrides 要生效，与老表既有命名一致")
	require.Equal(t, "ehr", byEHR[xlabDeptID].Source)
	require.False(t, byEHR[pubDeptID].Visible, "HiddenDepartmentIDs 里的工作室首次建档就不可见")
	xlabID := byEHR[xlabDeptID].ID

	// 幂等：第二次差量全 0
	got, err = svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Zero(t, got.Diff.Studios.Inserted)
	require.Zero(t, got.Diff.Studios.Updated)
	require.Zero(t, got.Diff.Studios.Deleted)
	require.Zero(t, got.Diff.Departments.Inserted+got.Diff.Departments.Updated+got.Diff.Departments.Deleted)
	require.Zero(t, got.Diff.Mappings.Inserted+got.Diff.Mappings.Updated)

	// 改名只改属性，id 不变（Key/角色/账单快照全靠它）。
	// 注意用没有 NameOverrides 的事业部：被规则改名的那个改 EHR 名字本来就不该动老表。
	pubID := byEHR[pubDeptID].ID
	renamed := threeNodes()
	renamed[0].Name = "公共关系事业部改名"
	setItems(renamed)
	got, err = svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, got.Diff.Studios.Updated)
	byEHR = loadLegacyStudios(t, db)
	require.Equal(t, "公共关系事业部改名", byEHR[pubDeptID].Name)
	require.Equal(t, pubID, byEHR[pubDeptID].ID, "改名不能换 id")
	require.False(t, byEHR[pubDeptID].Visible, "改名顺手把隐藏的工作室复活了")
	require.Equal(t, xlabID, byEHR[xlabDeptID].ID)
}

// 部门投影：能沿父链解析到事业部的落 active + 写映射；解析不到的落 pending_studio。
func TestSyncProjectsDepartmentsAndMappings(t *testing.T) {
	svc, db, _ := newProjectionSvc(t, []ehrclient.Department{
		{ID: "ehr-a", Name: "A 工作室", Type: 2},
		{ID: "dept-1", Name: "A 部门", ParentID: "ehr-a", Type: 4},
		{ID: "orphan-1", Name: "孤儿部门", Type: 4},
	})
	ctx := context.Background()
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	var dept orgmodel.OrgDepartment
	require.NoError(t, db.Where("dept_id = ?", "dept-1").Take(&dept).Error)
	require.NotNil(t, dept.StudioID)
	require.Equal(t, string(orgmodel.DeptStatusActive), string(dept.Status))

	var mapping orgmodel.DeptStudioMap
	require.NoError(t, db.Where("dept_id = ?", "dept-1").Take(&mapping).Error)
	require.Equal(t, "ehr", mapping.Source)

	var orphan orgmodel.OrgDepartment
	require.NoError(t, db.Where("dept_id = ?", "orphan-1").Take(&orphan).Error)
	require.Nil(t, orphan.StudioID)
	require.Equal(t, string(orgmodel.DeptStatusPendingStudio), string(orphan.Status))
}

// 撤并：EHR 里消失的事业部/部门 → 软删（visible=false / status=deleted），行保留。
func TestSyncSoftDeletesVanishedOrg(t *testing.T) {
	svc, db, setItems := newProjectionSvc(t, []ehrclient.Department{
		{ID: "ehr-a", Name: "A 工作室", Type: 2},
		{ID: "ehr-b", Name: "B 工作室", Type: 2},
		{ID: "dept-1", Name: "A 部门", ParentID: "ehr-a", Type: 4},
		{ID: "dept-2", Name: "B 部门", ParentID: "ehr-b", Type: 4},
	})
	ctx := context.Background()
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	setItems([]ehrclient.Department{
		{ID: "ehr-a", Name: "A 工作室", Type: 2},
		{ID: "dept-1", Name: "A 部门", ParentID: "ehr-a", Type: 4},
	})
	got, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, got.Diff.Studios.Deleted)
	require.Equal(t, 1, got.Diff.Departments.Deleted)

	byEHR := loadLegacyStudios(t, db)
	require.Len(t, byEHR, 2, "软删不删行")
	require.False(t, byEHR["ehr-b"].Visible)

	var vanished orgmodel.OrgDepartment
	require.NoError(t, db.Where("dept_id = ?", "dept-2").Take(&vanished).Error)
	require.Equal(t, string(orgmodel.DeptStatusDeleted), string(vanished.Status))

	// 差量写进状态键，管理页要能看到
	var diffValue string
	require.NoError(t, db.Raw("SELECT value FROM system_configs WHERE key = 'ai_app_hub.last_diff'").Scan(&diffValue).Error)
	require.Contains(t, diffValue, "\"deleted\":1")
}

// 同步进行中 Running()=true，结束后 false（页面按钮靠它兜住刷新）。
func TestRunningReflectsInFlightSync(t *testing.T) {
	svc, _, _ := newProjectionSvc(t, threeNodes())
	require.False(t, svc.Running())

	started := make(chan struct{})
	release := make(chan struct{})
	probe := service.NewAIAppHubSyncService(
		runningProbeFetcher{started: started, release: release}, nil, nil, service.DefaultSyncRules())
	go func() { _, _ = probe.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll}) }()

	<-started
	require.True(t, probe.Running())
	close(release)
	require.Eventually(t, func() bool { return !probe.Running() }, 2*time.Second, 10*time.Millisecond)
}

type runningProbeFetcher struct {
	started chan struct{}
	release chan struct{}
}

func (f runningProbeFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	close(f.started)
	<-f.release
	return nil, errors.New("stop")
}
