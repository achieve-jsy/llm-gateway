package middleware

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/audit"
)

func TestInferAction(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	cases := []struct {
		method string
		path   string
		want   audit.Action
	}{
		{"POST", "/api/v1/providers", "provider.create"},
		{"PUT", "/api/v1/providers/" + id, "provider.update"},
		{"DELETE", "/api/v1/providers/" + id, "provider.delete"},

		{"POST", "/api/v1/models", "model.create"},
		{"PUT", "/api/v1/models/" + id, "model.update"},
		{"DELETE", "/api/v1/models/" + id, "model.delete"},

		{"GET", "/api/v1/providers", ""},
		{"POST", "/api/v1", ""},
		{"POST", "/healthz", ""},
	}

	for _, tc := range cases {
		got := inferAction(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("inferAction(%q, %q) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestLookupRouteAction(t *testing.T) {
	cases := []struct {
		method   string
		fullPath string
		wantAct  audit.Action
		wantRes  audit.Resource
		wantOK   bool
	}{
		{"POST", "/api/v1/providers", audit.ActionProviderCreate, audit.ResourceProvider, true},
		{"PUT", "/api/v1/providers/:id", audit.ActionProviderUpdate, audit.ResourceProvider, true},
		{"PATCH", "/api/v1/providers/:id/status", audit.ActionProviderToggle, audit.ResourceProvider, true},
		{"DELETE", "/api/v1/providers/:id", audit.ActionProviderDelete, audit.ResourceProvider, true},

		{"POST", "/api/v1/models", audit.ActionModelCreate, audit.ResourceModel, true},
		{"DELETE", "/api/v1/models/batch", audit.ActionModelDelete, audit.ResourceModel, true},
		{"PATCH", "/api/v1/models/batch-status", audit.ActionModelBatchUpdateStatus, audit.ResourceModel, true},

		{"POST", "/api/v1/groups", audit.ActionGroupCreate, audit.ResourceGroup, true},
		{"POST", "/api/v1/groups/:id/models", audit.ActionGroupAddModels, audit.ResourceGroup, true},
		{"DELETE", "/api/v1/groups/:id/models/:model_id", audit.ActionGroupRemoveModel, audit.ResourceGroup, true},
		{"POST", "/api/v1/groups/:id/entitlements", audit.ActionGroupEntitlementGrant, audit.ResourceGroup, true},

		{"PUT", "/api/v1/budgets", audit.ActionBudgetSet, audit.ResourceBudget, true},
		{"DELETE", "/api/v1/budgets", audit.ActionBudgetDelete, audit.ResourceBudget, true},

		{"POST", "/api/v1/expansion-requests", audit.ActionExpansionCreate, audit.ResourceExpansion, true},
		{"POST", "/api/v1/expansion-requests/:id/decision", audit.ActionExpansionDecision, audit.ResourceExpansion, true},

		{"PUT", "/api/v1/settings", audit.ActionSettingsUpdate, audit.ResourceSettings, true},

		{"POST", "/api/v1/keys", audit.ActionKeyCreate, audit.ResourceKey, true},
		{"PATCH", "/api/v1/keys/:id/disable", audit.ActionKeyDisable, audit.ResourceKey, true},

		{"POST", "/api/v1/org/studios", audit.Action("studio.create"), audit.Resource("studio"), true},
		{"POST", "/api/v1/org/roles/grant", audit.Action("role.grant"), audit.Resource("role"), true},
		{"POST", "/api/v1/org/employees/import", audit.ActionEmployeeImport, audit.ResourceEmployee, true},

		// 未注册路由应返回 false
		{"GET", "/api/v1/providers", "", "", false},
		{"POST", "/api/v1/nonexistent", "", "", false},
		{"POST", "", "", "", false},
	}

	for _, tc := range cases {
		a, r, ok := lookupRouteAction(tc.fullPath, tc.method)
		if ok != tc.wantOK {
			t.Errorf("lookupRouteAction(%q, %q) ok = %v, want %v", tc.method, tc.fullPath, ok, tc.wantOK)
			continue
		}
		if ok && (a != tc.wantAct || r != tc.wantRes) {
			t.Errorf("lookupRouteAction(%q, %q) = (%q, %q), want (%q, %q)",
				tc.method, tc.fullPath, a, r, tc.wantAct, tc.wantRes)
		}
	}
}

func TestInferResourceType(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/providers", "providers"},
		{"/api/v1/providers/abc", "providers"},
		{"/api/v1/models", "models"},
		{"/api/v1/service-keys/abc/ip-allowlist", "service-keys"},
		{"/internal/v1/auth/verify-key", "auth"},
	}

	for _, tc := range cases {
		got := inferResourceType(tc.path)
		if got != tc.want {
			t.Errorf("inferResourceType(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
