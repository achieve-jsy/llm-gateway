package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

// buildTestRouter returns a real gin.Engine via Setup with minimal in-memory stubs.
func buildTestRouter() *gin.Engine {
	registry := provider.NewRegistry()
	cache := infra.NewCache(nil) // nil redis; router setup doesn't exercise it
	return Setup(registry, cache)
}

// ─── Route Registration ────────────────────────────────────────────────────

func TestSetup_HealthRoute_Registered(t *testing.T) {
	r := buildTestRouter()
	assertRouteExists(t, r.Routes(), "GET", "/health")
}

func TestSetup_ReadyRoute_Registered(t *testing.T) {
	r := buildTestRouter()
	assertRouteExists(t, r.Routes(), "GET", "/ready")
}

func TestSetup_ChatCompletionsRoute_Registered(t *testing.T) {
	r := buildTestRouter()
	assertRouteExists(t, r.Routes(), "POST", "/v1/chat/completions")
}

func TestSetup_ModelsRoute_Registered(t *testing.T) {
	r := buildTestRouter()
	assertRouteExists(t, r.Routes(), "GET", "/v1/models")
}

func TestSetup_AllExpectedRoutes_Registered(t *testing.T) {
	r := buildTestRouter()
	routes := r.Routes()

	want := []struct{ method, path string }{
		{"GET", "/health"},
		{"GET", "/ready"},
		{"POST", "/v1/chat/completions"},
		{"GET", "/v1/models"},
	}
	for _, w := range want {
		t.Run(w.method+"_"+w.path, func(t *testing.T) {
			assertRouteExists(t, routes, w.method, w.path)
		})
	}
}

// ─── Functional Smoke Tests ────────────────────────────────────────────────

func TestSetup_HealthEndpoint_Returns200(t *testing.T) {
	srv := httptest.NewServer(buildTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSetup_ReadyEndpoint_Returns200(t *testing.T) {
	srv := httptest.NewServer(buildTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ready")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSetup_ChatCompletions_WithoutAuth_Returns401(t *testing.T) {
	srv := httptest.NewServer(buildTestRouter())
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSetup_Models_WithoutAuth_Returns401(t *testing.T) {
	srv := httptest.NewServer(buildTestRouter())
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/v1/models", nil)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSetup_UnknownPath_Returns404(t *testing.T) {
	srv := httptest.NewServer(buildTestRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/no-such-path")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func assertRouteExists(t *testing.T, routes gin.RoutesInfo, method, path string) {
	t.Helper()
	for _, r := range routes {
		if r.Method == method && r.Path == path {
			return
		}
	}
	t.Errorf("route %s %s not registered; got: %v", method, path, routeKeys(routes))
}

func routeKeys(routes gin.RoutesInfo) []string {
	keys := make([]string, 0, len(routes))
	for _, r := range routes {
		keys = append(keys, r.Method+" "+r.Path)
	}
	return keys
}
