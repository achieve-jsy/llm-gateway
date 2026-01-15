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
		{"PATCH", "/api/v1/providers/" + id + "/status", "provider.status.update"},
		{"DELETE", "/api/v1/providers/" + id, "provider.delete"},

		{"POST", "/api/v1/models", "model.create"},
		{"PUT", "/api/v1/models/" + id, "model.update"},
		{"DELETE", "/api/v1/models/" + id, "model.delete"},

		{"POST", "/api/v1/service-keys", "service-key.create"},
		{"PATCH", "/api/v1/service-keys/" + id + "/status", "service-key.status.update"},
		{"PATCH", "/api/v1/service-keys/" + id + "/ip-allowlist", "service-key.ip-allowlist.update"},
		{"DELETE", "/api/v1/service-keys/" + id, "service-key.delete"},

		{"PUT", "/api/v1/budgets", "budget.update"},
		{"DELETE", "/api/v1/budgets", "budget.delete"},

		{"POST", "/api/v1/keys/" + id + "/groups", "key.group.create"},
		{"PATCH", "/api/v1/keys/" + id + "/log-enabled", "key.log-enabled.update"},

		{"POST", "/api/v1/expansion-requests", "expansion-request.create"},

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
