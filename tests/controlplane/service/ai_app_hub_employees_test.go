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

// employeeFetcher 同时实现 DepartmentFetcher 与 EmployeeFetcher，
// 记录服务层到底把哪些 id 交给了上游。
type employeeFetcher struct {
	items     []ehrclient.Department
	employees []ehrclient.Employee
	empErr    error
	askedIDs  []string
}

func (f *employeeFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	return f.items, nil
}

func (f *employeeFetcher) FetchEmployees(_ context.Context, ids []string) ([]ehrclient.Employee, error) {
	f.askedIDs = ids
	return f.employees, f.empErr
}

func deptWithPeople() []ehrclient.Department {
	return []ehrclient.Department{{
		ID:                xlabDeptID,
		Name:              "战略发展中心",
		Type:              2,
		LeaderPassport:    "hezhiguo",
		LeaderEmployeeID:  "emp-he",
		ManagerEmployeeID: "emp-zhou",
		HrBpEmployeeID:    "emp-he", // 重复 id 只能查一次
	}}
}

func TestSyncWritesEmployeeProfiles(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	fetcher := &employeeFetcher{
		items: deptWithPeople(),
		employees: []ehrclient.Employee{{
			OrgID:        "1428195512246472704",
			EmployeeID:   "emp-he",
			PersonName:   "何治国",
			GroupENumber: "BF00001",
			Passport:     "hezhiguo",
			Location:     "浙数文化科技园",
			PersonStatus: 1,
			JoinDate:     995126400000,
			ManagerName:  "郑法其",
		}},
	}
	svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, got.Employees)
	require.ElementsMatch(t, []string{"emp-he", "emp-zhou"}, fetcher.askedIDs, "节点上 9 类岗位的 id 都要交给上游，且去重")

	var rows []model.EHREmployee
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "BF00001", rows[0].GroupENumber)
	require.Equal(t, "浙数文化科技园", rows[0].Location, "城市靠 location 字段区分")
	require.Equal(t, "995126400000", rows[0].JoinDate, "毫秒时间戳按字符串落库")
	require.Equal(t, int32(1), rows[0].PersonStatus)

	// 第二轮：同一人换部门，必须更新而不是插新行
	fetcher.items[0].LeaderEmployeeID = "emp-he"
	fetcher.employees[0].DepartmentName = "技术研究工作室"
	_, err = svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1, "按 (org_id, employee_id) 幂等")
	require.Equal(t, "技术研究工作室", rows[0].DepartmentName)
}

func TestSyncKeepsDepartmentsWhenEmployeeFetchFails(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	fetcher := &employeeFetcher{
		items:  deptWithPeople(),
		empErr: errors.New("ehr unauthorized"),
	}
	svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err, "人员是辅助数据，拉不到不该让组织树同步失败")
	require.Zero(t, got.Employees)
	require.Equal(t, 1, got.Departments)

	rows, err := repo.ListDepartments(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestSyncWithoutEmployeeFetcherStillWorks(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	svc := service.NewAIAppHubSyncService(fakeFetcher{items: threeNodes()}, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Zero(t, got.Employees, "没实现 EmployeeFetcher 的 fetcher 直接跳过人员同步")
}
