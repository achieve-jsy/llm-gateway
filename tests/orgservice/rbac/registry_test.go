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
