package repository_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 成员配置三块摘要：继承 / 单独设置两种情形都要对。
func seedMemberConfig(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`INSERT INTO studios (id, name, source) VALUES ('s1', '战龙工作室', 'ehr')`,
		`INSERT INTO departments (id, dept_id, dept_name, studio_id, status) VALUES
			('dp-1', 'dept-a', '技术部', 's1', 'active')`,
		// u-1 单独设了 RPM（并发仍继承）；u-2 全继承
		`INSERT INTO users (id, name, email, dept_id, employee_id, quota_rpm) VALUES
			('u-1', 'Alice', 'alice@example.com', 'dp-1', 'emp-1', 100),
			('u-2', 'Bob', 'bob@example.com', 'dp-1', 'emp-2', NULL)`,
		`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES
			('u-1', 'dept_admin', 'dept', 'dept-a', 'tester')`,
		`INSERT INTO system_configs (key, value) VALUES
			('quota_default_user_rpm', '50'),
			('quota_default_user_max_concurrency', '5')`,
		// 团队池（dept_daily）+ 成员单设（member）
		`INSERT INTO budget_allocations (id, scope_type, scope_id, amount_usd, daily_amount_usd, weekly_amount_usd, enabled) VALUES
			('b-team', 'dept_daily', 'dept-a', 1000, 100, 500, 1),
			('b-mem', 'member', 'u-1', 300, 20, 80, 1)`,
		// 模型授权：工作室 2 个组 + 团队 1 个（与工作室重复）+ 1 个 disabled
		`INSERT INTO group_entitlements (id, group_id, scope_type, scope_id, enabled) VALUES
			('e-1', 1, 'studio', 's1', 1),
			('e-2', 2, 'studio', 's1', 1),
			('e-3', 1, 'dept', 'dept-a', 1),
			('e-4', 3, 'dept', 'dept-a', 0)`,
	} {
		require.NoError(t, db.Exec(stmt).Error)
	}
}

func TestListMembersConfigSummary(t *testing.T) {
	db := openWorkspaceTestDB(t)
	seedMemberConfig(t, db)

	members, total, err := repository.NewUserOrgRepo(db).ListMembersByDeptID("dept-a", 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, members, 2)

	byName := map[string]repository.DeptMemberInfo{}
	for _, m := range members {
		byName[m.Name] = m
	}

	alice := byName["Alice"]
	require.True(t, alice.IsDeptAdmin)
	require.NotNil(t, alice.Config)
	// 配额：只设了 RPM，并发回落到全局默认 → 整体算 override
	require.Equal(t, repository.MemberQuotaConfig{RPM: 100, MaxConcurrency: 5, Source: "override"}, alice.Config.Quota)
	// 预算：成员单设优先于团队池
	require.Equal(t, repository.MemberBudgetConfig{Monthly: 300, Daily: 20, Weekly: 80, Source: "member"}, alice.Config.Budget)
	// 模型授权：studio+dept 去重 2 个组
	require.Equal(t, repository.MemberEntitlementConfig{GroupCount: 2, Source: "studio_dept"}, alice.Config.Entitlement)

	bob := byName["Bob"]
	require.False(t, bob.IsDeptAdmin)
	require.Equal(t, repository.MemberQuotaConfig{RPM: 50, MaxConcurrency: 5, Source: "inherit"}, bob.Config.Quota)
	require.Equal(t, repository.MemberBudgetConfig{Monthly: 1000, Daily: 100, Weekly: 500, Source: "team"}, bob.Config.Budget)
	require.Equal(t, 2, bob.Config.Entitlement.GroupCount)
}

// 什么预算/授权都没配：source=none、数值 0（前端据此显示"未设置"而不是假数据）。
func TestListMembersConfigEmpty(t *testing.T) {
	db := openWorkspaceTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO departments (id, dept_id, dept_name, status) VALUES ('dp-9', 'dept-z', '无配置团队', 'active')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users (id, name, email, dept_id) VALUES ('u-9', 'Zoe', 'zoe@example.com', 'dp-9')`).Error)

	members, _, err := repository.NewUserOrgRepo(db).ListMembersByDeptID("dept-z", 1, 20)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.NotNil(t, members[0].Config)
	require.Equal(t, repository.MemberQuotaConfig{RPM: 60, MaxConcurrency: 10, Source: "inherit"}, members[0].Config.Quota,
		"没有 system_configs 行时用兜底默认（与 settings_service 同值）")
	require.Equal(t, repository.MemberBudgetConfig{Source: "none"}, members[0].Config.Budget)
	require.Equal(t, repository.MemberEntitlementConfig{GroupCount: 0, Source: "none"}, members[0].Config.Entitlement)
	require.False(t, members[0].IsDeptAdmin)
}
