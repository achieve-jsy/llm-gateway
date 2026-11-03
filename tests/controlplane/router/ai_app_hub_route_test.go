package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/router"
	"github.com/redis/go-redis/v9"
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
// engineWithWorkingRedis 用真实 Redis（miniredis）起一次 router.Setup：
// 只有 Redis 可用时同步周期才会真的走到"读 last_success → 抢锁 → 打 EHR"，
// 这两条断言才能区分"调度被关掉"和"调度起来了但被 Redis 挡掉"。
func engineWithWorkingRedis(t *testing.T) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	t.Setenv("JWT_SECRET", assemblyJWTSecret)
	t.Setenv("INTERNAL_TOKEN", assemblyInternalToken)
	// 不建 ai_app_* 表：这两条测试只钉"调度有没有打 EHR"，不关心落库结果
	// （assemblyDB 的 sqlite 建不了 hub 表的 Postgres 默认值）。
	_ = router.Setup(context.Background(), assemblyDB(t), rdb)
}

func ehrServerCountingHits(hits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[{"departmentId":"d-1","departmentName":"某事业部","departmentType":2}]}`))
	}))
}

// AI_APP_HUB_SYNC_CRON 设为空串 = 关掉调度，一个请求都不能打出去。
func TestAIAppHubSyncCronDisabledByEnvDoesNotCallEHR(t *testing.T) {
	var hits int32
	srv := ehrServerCountingHits(&hits)
	defer srv.Close()

	t.Setenv("EHR_BASE_URL", srv.URL)
	t.Setenv("EHR_API_TOKEN", "t")
	t.Setenv("EHR_ORG_ID", "o")
	t.Setenv("AI_APP_HUB_SYNC_CRON", "")

	engineWithWorkingRedis(t)
	time.Sleep(150 * time.Millisecond)
	require.Zero(t, atomic.LoadInt32(&hits), "cron 关闭时不得访问 EHR")
}

// 只有 last_success 一条记录时，冷启动要立刻补一次（同一条断言的阳性对照，
// 证明上一条不是恒真）。
func TestAIAppHubSyncCronColdStartRunsImmediately(t *testing.T) {
	var hits int32
	srv := ehrServerCountingHits(&hits)
	defer srv.Close()

	t.Setenv("EHR_BASE_URL", srv.URL)
	t.Setenv("EHR_API_TOKEN", "t")
	t.Setenv("EHR_ORG_ID", "o")
	t.Setenv("AI_APP_HUB_SYNC_CRON", "0 3 * * *")

	engineWithWorkingRedis(t)
	require.Eventually(t, func() bool { return atomic.LoadInt32(&hits) >= 1 }, 2*time.Second, 10*time.Millisecond)
}