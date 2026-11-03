package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 工作室概览：人数/管理员(RBAC)/负责人(EHR 多值)三类数据分开，软删团队不出现。
func seedOverview(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`INSERT INTO studios (id, name, source) VALUES ('s1', '战龙工作室', 'ehr')`,
		`INSERT INTO departments (id, dept_id, dept_name, studio_id, status) VALUES
			('dp-1', 'dept-a', '技术部', 's1', 'active'),
			('dp-2', 'dept-b', '数据组', 's1', 'active'),
			('dp-3', 'dept-c', '已撤并团队', 's1', 'deleted'),
			('dp-4', 'dept-d', '待归集部门', NULL, 'pending_studio')`,
		`INSERT INTO users (id, name, email, dept_id, employee_id) VALUES
			('u-1', 'Alice', 'alice@example.com', 'dp-1', 'emp-1'),
			('u-2', 'Bob', 'bob@example.com', 'dp-1', 'emp-2'),
			('u-3', 'Carol', 'carol@example.com', 'dp-2', 'emp-3'),
			('u-4', '离职的人', 'gone@example.com', 'dp-1', 'emp-4')`,
		`UPDATE users SET deleted_at = '2026-05-01 00:00:00' WHERE id = 'u-4'`,
		`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES
			('u-1', 'studio_admin', 'studio', 's1', 'tester'),
			('u-1', 'dept_admin', 'dept', 'dept-a', 'tester'),
			('u-2', 'dept_admin', 'dept', 'dept-a', 'tester'),
			('u-3', 'dept_admin', 'dept', 'dept-b', 'tester')`,
		`INSERT INTO org_node_leaders (id, node_type, node_id, name, source, created_at) VALUES
			('l-1', 'studio', 's1', '傅彭彭', 'ehr', '2026-05-01 00:00:00'),
			('l-2', 'studio', 's1', '刘黎', 'ehr', '2026-05-02 00:00:00'),
			('l-3', 'dept', 'dp-1', '甲方负责人', 'ehr', '2026-05-01 00:00:00'),
			('l-4', 'dept', 'dp-1', '乙方负责人', 'manual', '2026-05-02 00:00:00')`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
}

func TestStudioOverview(t *testing.T) {
	db := openWorkspaceTestDB(t)
	seedOverview(t, db)

	ov, err := repository.NewOrgOverviewRepo(db).StudioOverview(context.Background(), "s1")
	require.NoError(t, err)

	require.Equal(t, "s1", ov.StudioID)
	require.Equal(t, "战龙工作室", ov.StudioName)
	require.Equal(t, 3, ov.MemberCount, "离职的人不算")
	require.Equal(t, 1, ov.PendingDeptCount)

	// 管理员：只有 studio_admin（Alice 的 dept_admin 不算工作室管理员）
	require.Len(t, ov.Admins, 1)
	require.Equal(t, "Alice", ov.Admins[0].Name)
	require.Equal(t, "emp-1", ov.Admins[0].EmployeeID)

	// 负责人：多值 + source 分开（EHR / 手动）
	require.Len(t, ov.Leaders, 2)
	require.Equal(t, "傅彭彭", ov.Leaders[0].Name)
	require.Equal(t, "ehr", ov.Leaders[0].Source)

	// 团队卡：软删的 team 不出现
	require.Len(t, ov.Teams, 2)
	teams := map[string]repository.TeamCard{}
	for _, team := range ov.Teams {
		teams[team.DeptID] = team
	}
	require.NotContains(t, teams, "dept-c", "status=deleted 的团队不进左树")

	tech := teams["dept-a"]
	require.Equal(t, "dp-1", tech.DeptUUID)
	require.Equal(t, "技术部", tech.DeptName)
	require.Equal(t, 2, tech.MemberCount)
	require.Len(t, tech.Admins, 2, "一个团队可以有多名管理员")
	require.Equal(t, "Alice", tech.Admins[0].Name)
	require.Len(t, tech.Leaders, 2)
	require.Equal(t, "ehr", tech.Leaders[0].Source)
	require.Equal(t, "manual", tech.Leaders[1].Source)

	data := teams["dept-b"]
	require.Equal(t, 1, data.MemberCount)
	require.Len(t, data.Admins, 1)
	require.Equal(t, "Carol", data.Admins[0].Name)
	require.Empty(t, data.Leaders)
	require.NotNil(t, data.Leaders, "空负责人序列化成 []，不是 null")
}

// 没有团队的 / 没有管理员的 / 没有负责人的工作室，也必须返回空数组而不是 null。
func TestStudioOverviewEmptyCollections(t *testing.T) {
	db := openWorkspaceTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO studios (id, name, source) VALUES ('s9', '空工作室', 'ehr')`).Error)

	ov, err := repository.NewOrgOverviewRepo(db).StudioOverview(context.Background(), "s9")
	require.NoError(t, err)
	require.Equal(t, 0, ov.MemberCount)
	require.Equal(t, 0, ov.PendingDeptCount)
	require.NotNil(t, ov.Admins)
	require.NotNil(t, ov.Leaders)
	require.NotNil(t, ov.Teams)
	require.Len(t, ov.Teams, 0)
}

// 不存在的工作室 → gorm.ErrRecordNotFound（handler 映射成 404 studio_not_found）
func TestStudioOverviewNotFound(t *testing.T) {
	db := openWorkspaceTestDB(t)
	_, err := repository.NewOrgOverviewRepo(db).StudioOverview(context.Background(), "nope")
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}
