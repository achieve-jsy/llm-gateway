package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

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

	// 全量同步的第二段：按工号/passport 反查
	roster         []ehrclient.Employee
	rosterErr      error
	askedNumbers   []string
	askedPassports []string
	passportEmps   []ehrclient.Employee
}

func (f *employeeFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	return f.items, nil
}

func (f *employeeFetcher) FetchEmployees(_ context.Context, ids []string) ([]ehrclient.Employee, error) {
	f.askedIDs = ids
	return f.employees, f.empErr
}

func (f *employeeFetcher) FetchEmployeesByGroupENumber(_ context.Context, numbers []string) ([]ehrclient.Employee, error) {
	f.askedNumbers = numbers
	return f.roster, f.rosterErr
}

func (f *employeeFetcher) FetchEmployeesByPassport(_ context.Context, passports []string) ([]ehrclient.Employee, error) {
	f.askedPassports = passports
	return f.passportEmps, nil
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

// 全量同步必须按本地账号的工号反查——部门节点上点不到的普通员工只能这样补齐。
func TestSyncAllBackfillsRosterByIdentity(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	require.NoError(t, db.Exec("INSERT INTO users (sub, employee_id, name) VALUES ('zhoutian','BF00014','周甜'), ('olduser','','legacy')").Error)

	fetcher := &employeeFetcher{
		items:        deptWithPeople(),
		roster:       []ehrclient.Employee{{OrgID: "org", EmployeeID: "emp-zhoutian", PersonName: "周甜", GroupENumber: "BF00014", Location: "广州", PersonStatus: 1}},
		passportEmps: []ehrclient.Employee{{OrgID: "org", EmployeeID: "emp-legacy", PersonName: "老账号", Passport: "olduser"}},
	}
	svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, []string{"BF00014"}, fetcher.askedNumbers, "有工号的账号按工号查")
	require.Equal(t, []string{"olduser"}, fetcher.askedPassports, "没工号的才用 passport 查")
	require.Equal(t, 2, got.Employees)

	var rows []model.EHREmployee
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 2)
	byName := map[string]model.EHREmployee{}
	for _, r := range rows {
		byName[r.PersonName] = r
	}
	require.Equal(t, "广州", byName["周甜"].Location, "工号反查到的人要落库")
	require.Equal(t, "BF00014", byName["周甜"].GroupENumber)
	require.Equal(t, "olduser", byName["老账号"].Passport, "没工号的走 passport 反查")
}

// 离职（person_status=3）的账号要关停：Key 关掉（网关 15 秒内 401）+ login_status=disabled。
// 最近 30 天还在用 Key 的人放过 —— EHR 说离职但人还在跑流量，先交人工核对。
func TestSyncDisablesResignedAccounts(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	require.NoError(t, db.Exec("INSERT INTO users (id, sub, employee_id, name, login_status) VALUES ('u-left','left','BF00100','走了的人','active'), ('u-live','live','BF00200','还在跑的人','active')").Error)
	require.NoError(t, db.Exec("INSERT INTO api_keys (user_id, name, enabled) VALUES ('u-left','k1',1), ('u-live','k2',1)").Error)
	// u-live 最近在用 Key
	require.NoError(t, db.Exec("UPDATE api_keys SET last_used_at = ? WHERE user_id = 'u-live'", time.Now().Add(-24*time.Hour)).Error)

	fetcher := &employeeFetcher{
		items: deptWithPeople(),
		employees: []ehrclient.Employee{
			{OrgID: "org", EmployeeID: "emp-left", PersonName: "走了的人", GroupENumber: "BF00100", PersonStatus: 3},
			{OrgID: "org", EmployeeID: "emp-live", PersonName: "还在跑的人", GroupENumber: "BF00200", PersonStatus: 3},
			{OrgID: "org", EmployeeID: "emp-ok", PersonName: "在职的人", GroupENumber: "BF00300", PersonStatus: 1},
		},
	}
	svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, got.ResignedAccounts, "只封真正封得动的那一个")

	var leftKey, liveKey struct {
		Enabled bool
		UserID  string
	}
	require.NoError(t, db.Raw("SELECT enabled, user_id FROM api_keys WHERE user_id = 'u-left'").Scan(&leftKey).Error)
	require.False(t, leftKey.Enabled, "离职账号的 Key 要关掉")
	require.NoError(t, db.Raw("SELECT enabled, user_id FROM api_keys WHERE user_id = 'u-live'").Scan(&liveKey).Error)
	require.True(t, liveKey.Enabled, "最近还在用的先不动")

	var statuses []struct {
		Sub         string
		LoginStatus string
		DeletedAt   *time.Time
	}
	require.NoError(t, db.Raw("SELECT sub, login_status, deleted_at FROM users ORDER BY sub").Scan(&statuses).Error)
	require.Equal(t, []string{"left", "live"}, []string{statuses[0].Sub, statuses[1].Sub})
	require.Equal(t, "disabled", statuses[0].LoginStatus)
	require.NotNil(t, statuses[0].DeletedAt, "离职人员要软删除（deleted_at 有值）")
	require.Nil(t, statuses[1].DeletedAt, "有近 30 天流量的不动")
	require.Equal(t, "active", statuses[1].LoginStatus)

	// GORM 查询看不到软删的人，但行还在
	var visible int64
	require.NoError(t, db.Table("users").Where("deleted_at IS NULL").Count(&visible).Error)
	require.Equal(t, int64(1), visible, "软删的人从普通查询里消失")
}

// 单部门/单工作室同步不该顺手把全公司的人都拉一遍。
func TestSyncDepartmentScopeSkipsRoster(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	require.NoError(t, db.Exec("INSERT INTO users (sub, employee_id, name) VALUES ('zhoutian','BF00014','周甜')").Error)

	fetcher := &employeeFetcher{items: deptWithPeople()}
	svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

	_, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeDepartment, ID: xlabDeptID})
	require.NoError(t, err)
	require.Empty(t, fetcher.askedNumbers, "非全量同步不查花名册")
	require.Empty(t, fetcher.askedPassports)
}

// 一人多岗：只能留 isDefault 那条，否则 upsert 后部门/岗位字段会随机漂。
func TestSyncPrefersDefaultAppointment(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)

	fetcher := &employeeFetcher{
		items: deptWithPeople(),
		employees: []ehrclient.Employee{
			{OrgID: "org", EmployeeID: "emp-he", PersonName: "何治国", DepartmentName: "兼职部门"},
			{OrgID: "org", EmployeeID: "emp-he", PersonName: "何治国", DepartmentName: "经管办", IsDefault: true},
		},
	}
	svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

	_, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	var rows []model.EHREmployee
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "经管办", rows[0].DepartmentName, "保留 isDefault 的主岗")
}

// 线上 ehr_employees 有 UNIQUE(passport)：同 passport 多个 employeeId 时只能留一条主档，
// 否则整批 upsert 因唯一约束失败，一个人都写不进去。
func TestSyncCollapsesSamePassportDifferentEmployeeIDs(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)

	fetcher := &employeeFetcher{
		items: deptWithPeople(),
		employees: []ehrclient.Employee{
			{OrgID: "org", EmployeeID: "old-id", PersonName: "冯静", Passport: "fengjing", GroupENumber: "BF00057", DepartmentName: "旧档案"},
			{OrgID: "org", EmployeeID: "new-id", PersonName: "冯静", Passport: "fengjing", GroupENumber: "BF00057", DepartmentName: "飞天", IsDefault: true},
		},
	}
	svc := service.NewAIAppHubSyncService(fetcher, repo, settings, service.DefaultSyncRules())

	got, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)
	require.Equal(t, 1, got.Employees)

	var rows []model.EHREmployee
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "飞天", rows[0].DepartmentName, "保留主档")
	require.Equal(t, "new-id", rows[0].EmployeeID)
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
