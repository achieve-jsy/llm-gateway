package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedAccounts 一个工作室两个团队 + 四种角色的人：
//
//	张三 admin（无部门）、李四 studio_admin(S1)+dept_admin(dept-a)、
//	王五 dept_admin(dept-a)、赵六 无任何授权（dept-b）
func seedAccounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO studios (id, name, source) VALUES ('s1', '战龙工作室', 'ehr'), ('s2', '元气工作室', 'ehr')`,
		`INSERT INTO departments (id, dept_id, dept_name, studio_id, status) VALUES
			('dp-1', 'dept-a', '技术部', 's1', 'active'),
			('dp-2', 'dept-b', '数据组', 's1', 'active')`,
		`INSERT INTO users (id, name, email, dept_id, employee_id, job_title, created_at) VALUES
			('u-admin', '张三', 'zhang@example.com', NULL, 'emp-001', '平台工程师', '2026-01-10 00:00:00'),
			('u-studio', '李四', 'li@example.com', 'dp-1', 'emp-002', '工作室负责人', '2026-02-10 00:00:00'),
			('u-dept', '王五', 'wang@example.com', 'dp-1', 'emp-003', '团队负责人', '2026-03-10 00:00:00'),
			('u-plain', '赵六', 'zhao@example.com', 'dp-2', 'emp-004', '后端工程师', '2026-04-10 00:00:00')`,
		`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by, created_at) VALUES
			('u-admin', 'admin', 'company', '', 'tester', '2026-01-10 00:00:00'),
			('u-studio', 'studio_admin', 'studio', 's1', 'tester', '2026-02-10 00:00:00'),
			('u-studio', 'dept_admin', 'dept', 'dept-a', 'tester', '2026-02-11 00:00:00'),
			('u-dept', 'dept_admin', 'dept', 'dept-a', 'tester', '2026-03-10 00:00:00')`,
	}
	for _, stmt := range stmts {
		require.NoError(t, db.Exec(stmt).Error)
	}
}

func listAccounts(t *testing.T, db *gorm.DB, f repository.AccountListFilter) ([]repository.AccountRow, int64, repository.AccountRoleCounts) {
	t.Helper()
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	rows, total, counts, err := repository.NewAccountRepo(db).ListAccounts(context.Background(), f)
	require.NoError(t, err)
	return rows, total, counts
}

// 一个人多角色 → 仍然一行，主角色取最高权限，roles[] 带全部明细与 scope_name。
func TestListAccountsOneRowPerPerson(t *testing.T) {
	db := openWorkspaceTestDB(t)
	seedAccounts(t, db)

	rows, total, counts := listAccounts(t, db, repository.AccountListFilter{})
	require.Equal(t, int64(4), total, "一人一行，不按授权条数算")
	require.Len(t, rows, 4)
	require.Equal(t, repository.AccountRoleCounts{Admin: 1, StudioAdmin: 1, DeptAdmin: 1, User: 1}, counts)

	byName := map[string]repository.AccountRow{}
	for _, row := range rows {
		byName[row.Name] = row
	}
	require.Equal(t, repository.RoleKeyAdmin, byName["张三"].PrimaryRole)
	require.Equal(t, repository.RoleKeyStudioAdmin, byName["李四"].PrimaryRole, "studio_admin 高于 dept_admin")
	require.Equal(t, repository.RoleKeyDeptAdmin, byName["王五"].PrimaryRole)
	require.Equal(t, repository.RoleKeyUser, byName["赵六"].PrimaryRole, "无授权 = 员工")
	require.Empty(t, byName["赵六"].Roles)
	require.NotNil(t, byName["赵六"].Roles, "空 roles 序列化成 []，不是 null")

	// 工作室/团队名 join 出来
	require.Equal(t, "s1", byName["李四"].StudioID)
	require.Equal(t, "战龙工作室", byName["李四"].StudioName)
	require.Equal(t, "dept-a", byName["李四"].DeptID)
	require.Equal(t, "技术部", byName["李四"].DeptName)
	require.Greater(t, len(byName["李四"].CreatedAt), 0)

	// 多角色明细 + scope_name
	require.Len(t, byName["李四"].Roles, 2)
	scopeNames := map[string]string{}
	for _, item := range byName["李四"].Roles {
		scopeNames[item.Role] = item.ScopeName
	}
	require.Equal(t, "战龙工作室", scopeNames[repository.RoleKeyStudioAdmin])
	require.Equal(t, "技术部", scopeNames[repository.RoleKeyDeptAdmin])
	require.Equal(t, "全公司", byName["张三"].Roles[0].ScopeName)
}

// 角色筛选按"人"算：role=user 只返回无授权者，role=studio_admin 不返回同一个人第二次。
func TestListAccountsRoleFilter(t *testing.T) {
	db := openWorkspaceTestDB(t)
	seedAccounts(t, db)

	rows, total, counts := listAccounts(t, db, repository.AccountListFilter{Role: repository.RoleKeyUser})
	require.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	require.Equal(t, "赵六", rows[0].Name)
	require.Equal(t, int64(4), counts.Admin+counts.StudioAdmin+counts.DeptAdmin+counts.User, "统计不受角色筛选影响")

	rows, _, _ = listAccounts(t, db, repository.AccountListFilter{Role: repository.RoleKeyStudioAdmin})
	require.Len(t, rows, 1)
	require.Equal(t, "李四", rows[0].Name, "李四既是 studio_admin 也是 dept_admin，只能出现一次")

	rows, _, _ = listAccounts(t, db, repository.AccountListFilter{Role: repository.RoleKeyDeptAdmin})
	require.Len(t, rows, 1)
	require.Equal(t, "王五", rows[0].Name, "李四的高角色是 studio_admin，不算 dept_admin 人数")
}

// 关键词打姓名/工号/岗位/邮箱，组织过滤按工作室与团队，创建时间区间含当日。
func TestListAccountsFilters(t *testing.T) {
	db := openWorkspaceTestDB(t)
	seedAccounts(t, db)

	for _, kw := range []string{"王五", "emp-003", "团队负责人", "wang@example.com", "WANG@EXAMPLE.COM"} {
		rows, total, _ := listAccounts(t, db, repository.AccountListFilter{Keyword: kw})
		require.Equal(t, int64(1), total, "keyword=%s", kw)
		require.Equal(t, "王五", rows[0].Name, "keyword=%s", kw)
	}

	rows, total, _ := listAccounts(t, db, repository.AccountListFilter{StudioID: "s1"})
	require.Equal(t, int64(3), total, "工作室过滤命中其下两个团队的人（张三没有部门，不算）")
	require.Len(t, rows, 3)

	rows, _, _ = listAccounts(t, db, repository.AccountListFilter{DeptID: "dept-a"})
	require.Len(t, rows, 2)
	rows, _, _ = listAccounts(t, db, repository.AccountListFilter{DeptID: "dp-1"})
	require.Len(t, rows, 2, "dept_id 也接受 departments.id（uuid）")

	from := mustDate(t, "2026-03-01")
	rows, total, counts := listAccounts(t, db, repository.AccountListFilter{CreatedFrom: &from})
	require.Equal(t, int64(2), total, "王五、赵六")
	require.Len(t, rows, 2)
	require.Equal(t, int64(2), counts.DeptAdmin+counts.User)

	to := mustDate(t, "2026-02-10").AddDate(0, 0, 1) // 含当日
	rows, total, _ = listAccounts(t, db, repository.AccountListFilter{CreatedTo: &to})
	require.Equal(t, int64(2), total, "张三、李四（李四当天创建要算进来）")
	require.Len(t, rows, 2)

	rows, total, _ = listAccounts(t, db, repository.AccountListFilter{CreatedFrom: &from, CreatedTo: &to})
	require.Equal(t, int64(0), total, "区间互斥时 role_counts 全 0 也不报错")
	require.Empty(t, rows)
}

// 分页稳定：page_size=2 两页各 2 行，total 不随分页变。
func TestListAccountsPagination(t *testing.T) {
	db := openWorkspaceTestDB(t)
	seedAccounts(t, db)

	first, total, _ := listAccounts(t, db, repository.AccountListFilter{Page: 1, PageSize: 2})
	second, _, _ := listAccounts(t, db, repository.AccountListFilter{Page: 2, PageSize: 2})
	require.Equal(t, int64(4), total)
	require.Len(t, first, 2)
	require.Len(t, second, 2)
	require.NotEqual(t, first[0].UserID, second[0].UserID)
}

// 非平台管理员只看自己管辖范围；无管辖范围时一条都不返回。
func TestListAccountsScope(t *testing.T) {
	db := openWorkspaceTestDB(t)
	seedAccounts(t, db)

	rows, total, _ := listAccounts(t, db, repository.AccountListFilter{Restricted: true, ScopeDeptIDs: []string{"dept-b"}})
	require.Equal(t, int64(1), total)
	require.Equal(t, "赵六", rows[0].Name)

	_, total, _ = listAccounts(t, db, repository.AccountListFilter{Restricted: true})
	require.Equal(t, int64(0), total, "无管辖范围 = 不看任何数据")
}

func mustDate(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	require.NoError(t, err)
	return parsed
}
