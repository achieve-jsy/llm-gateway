package rbac_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
)

func TestRegistryAllowsDepartmentAdminBudgetApprovalReadPaths(t *testing.T) {
	for _, perm := range []rbac.Permission{
		rbac.PermExpansionApprove,
		rbac.PermModelTierView,
		rbac.PermBudgetViewScoped,
	} {
		entry, ok := rbac.Registry[perm]
		if !ok {
			t.Fatalf("permission %s is not registered", perm)
		}
		if entry.Constraint != rbac.ScopeManage {
			t.Fatalf("permission %s constraint = %v, want ScopeManage", perm, entry.Constraint)
		}
	}
}

// 用量统计口径：跨用户视角（用量看板 / 使用分析报告 / 调用明细）仅 admin，
// 管理角色一律不得持有 PermUsageViewAll。
func TestRegistryUsageStatsAreAdminOnly(t *testing.T) {
	entry, ok := rbac.Registry[rbac.PermUsageViewAll]
	if !ok {
		t.Fatal("usage:view:all is not registered")
	}
	if entry.Constraint != rbac.ScopeGlobal {
		t.Fatalf("usage:view:all constraint = %v, want ScopeGlobal", entry.Constraint)
	}

	// 例外：/usage/dashboard 保留管辖范围，供预算管理页取成员聚合用量。
	scoped, ok := rbac.Registry[rbac.PermUsageViewScoped]
	if !ok {
		t.Fatal("usage:view:scoped is not registered")
	}
	if scoped.Constraint != rbac.ScopeManage {
		t.Fatalf("usage:view:scoped constraint = %v, want ScopeManage", scoped.Constraint)
	}
}
