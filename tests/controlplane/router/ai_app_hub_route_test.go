package router_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 这条测试钉的是"接线"，不是业务逻辑：E2E 里是手工注册路由，
// 这里走真实 router.Setup，确认路由挂在 /api/v1/admin/ai-app-hub/sync
// 且 JWT 中间件真的在链上。
func TestAIAppHubSyncRouteWiredThroughRealRouter(t *testing.T) {
	// EHR 未配置：不启动 cron，调用直接 502（证明接线通到了 unconfigured fetcher）。
	t.Setenv("EHR_BASE_URL", "")
	t.Setenv("EHR_API_TOKEN", "")
	t.Setenv("EHR_ORG_ID", "")

	db := assemblyDB(t)
	engine := assemblyEngine(t, db)

	var found bool
	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/v1/admin/ai-app-hub/sync" {
			found = true
		}
	}
	require.True(t, found, "同步路由必须在真实路由表里")

	require.NoError(t, db.Exec(
		"INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by, created_at) VALUES (?, 'admin', 'company', '', 'test', datetime('now'))",
		assemblyAdminUserID,
	).Error)

	post := func(t *testing.T, token, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai-app-hub/sync", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	// 没 token → 401，说明 JWTAuth 在链上。
	require.Equal(t, http.StatusUnauthorized, post(t, "", `{"scope":"all"}`).Code)

	// 有 token 但 EHR 没配 → 502，说明 handler → service → fetcher 都通。
	rec := post(t, assemblyJWT(t, assemblyAdminUserID), `{"scope":"all"}`)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "ehr upstream")
}
