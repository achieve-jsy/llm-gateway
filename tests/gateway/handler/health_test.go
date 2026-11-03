package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHealthRouter 模拟 ModelsRouterInterface，用于 HealthHandler 的就绪探针测试
type fakeHealthRouter struct {
	resp *core.ModelsResponse
	err  error
}

func TestHealthHandler_ReadyAdvertisesBudgetDeploymentCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	state := &provider.RuntimeState{}
	state.MarkLoadSuccess("test", 1, 1)
	modelsRouter := &fakeHealthRouter{resp: &core.ModelsResponse{
		Object: "list", Data: []core.Model{{ID: "tiered-model", Object: "model"}},
	}}
	holder := budget.NewSnapshotHolder()
	require.True(t, holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 42, MasterEnabled: true, AccountingEnabled: true,
		ShadowEnabled: true, LegacyTotalWriteEnabled: true,
	}))
	h := handler.NewHealthHandler(rdb, modelsRouter, state, nil)
	h.SetBudgetSnapshotHolder(holder)
	router := gin.New()
	router.GET("/ready", h.Ready)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, float64(1), response.Capabilities["tier_snapshot_schema"])
	assert.Equal(t, true, response.Capabilities["redis_v2"])
	assert.Equal(t, true, response.Capabilities["shadow_enabled"])
	assert.Equal(t, float64(42), response.Capabilities["snapshot_revision"])
}

func (f *fakeHealthRouter) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	return f.resp, f.err
}

func TestHealthHandler_Health(t *testing.T) {
	// 测试目的：验证 GET /health 存活探针总是返回 200 + {"status":"ok"}
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	h := handler.NewHealthHandler(rdb, nil, nil, nil)

	router := gin.New()
	router.GET("/health", h.Health)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp["status"])
}

func TestHealthHandler_Ready(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		redisUp     bool
		routerResp  *core.ModelsResponse
		stateLoaded bool
		wantCode    int
		wantStatus  string
	}{
		{
			name:        "Redis连通且有模型时返回就绪",
			description: "Redis ping 成功，Router 返回非空模型列表，RuntimeState 标记为已加载，期望 200 + ready",
			redisUp:     true,
			routerResp: &core.ModelsResponse{
				Object: "list",
				Data: []core.Model{
					{ID: "gpt-4", Object: "model"},
				},
			},
			stateLoaded: true,
			wantCode:    http.StatusOK,
			wantStatus:  "ready",
		},
		{
			name:        "Redis不可用时返回503",
			description: "Redis 实例已关闭，ping 失败，期望 503 + not_ready",
			redisUp:     false,
			routerResp:  nil,
			stateLoaded: false,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
		},
		{
			name:        "Redis正常但无模型时返回503",
			description: "Redis 正常但 Router 返回空模型列表或 state 未加载，期望 503 + not_ready",
			redisUp:     true,
			routerResp: &core.ModelsResponse{
				Object: "list",
				Data:   []core.Model{},
			},
			stateLoaded: false,
			wantCode:    http.StatusServiceUnavailable,
			wantStatus:  "not_ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr, err := miniredis.Run()
			require.NoError(t, err)

			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

			if !tt.redisUp {
				mr.Close()
			} else {
				defer mr.Close()
			}
			defer rdb.Close()

			var modelsRouter handler.ModelsRouterInterface
			if tt.routerResp != nil {
				modelsRouter = &fakeHealthRouter{resp: tt.routerResp}
			}

			var state *provider.RuntimeState
			if tt.stateLoaded {
				state = &provider.RuntimeState{}
				state.MarkLoadSuccess("test", 1, 1)
			}

			h := handler.NewHealthHandler(rdb, modelsRouter, state, nil)

			router := gin.New()
			router.GET("/ready", h.Ready)

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)

			var resp map[string]any
			err = json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp["status"])
		})
	}
}

// TestHealthHandler_ReadyGroupCacheCheck 覆盖 /ready 的 group_cache 检查项语义：
// 未注入 → unknown 且不改变既有判定；注入未就绪 → 503 + not_initialized；注入已就绪 → 200 + ok；
// 同时确保 redis 失败 / registry 为空 / 正常三种响应分支都带 checks.group_cache。
func TestHealthHandler_ReadyGroupCacheCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		redisUp        bool
		modelCount     int
		stateLoaded    bool
		injectCache    bool
		injectNil      bool
		cacheReady     bool
		wantCode       int
		wantStatus     string
		wantGroupCache string
		wantRedis      string
		wantRegistry   string
	}{
		{
			name:           "Redis不可用分支也带 group_cache（unknown）",
			redisUp:        false,
			wantCode:       http.StatusServiceUnavailable,
			wantStatus:     "not_ready",
			wantRedis:      "unavailable",
			wantGroupCache: "unknown",
			wantRegistry:   "unknown",
		},
		{
			name:           "registry为空分支也带 group_cache（unknown）",
			redisUp:        true,
			wantCode:       http.StatusServiceUnavailable,
			wantStatus:     "not_ready",
			wantRedis:      "ok",
			wantGroupCache: "unknown",
			wantRegistry:   "empty",
		},
		{
			name:           "未注入组名缓存时既有就绪判定不变，group_cache 为 unknown",
			redisUp:        true,
			modelCount:     1,
			stateLoaded:    true,
			wantCode:       http.StatusOK,
			wantStatus:     "ready",
			wantRedis:      "ok",
			wantGroupCache: "unknown",
			wantRegistry:   "ok",
		},
		{
			name:           "显式注入 nil 接口时仍报 unknown 且不 panic",
			redisUp:        true,
			modelCount:     1,
			stateLoaded:    true,
			injectNil:      true,
			wantCode:       http.StatusOK,
			wantStatus:     "ready",
			wantRedis:      "ok",
			wantGroupCache: "unknown",
			wantRegistry:   "ok",
		},
		{
			name:           "组名缓存未就绪时返回503且 group_cache 为 not_initialized",
			redisUp:        true,
			modelCount:     1,
			stateLoaded:    true,
			injectCache:    true,
			cacheReady:     false,
			wantCode:       http.StatusServiceUnavailable,
			wantStatus:     "not_ready",
			wantRedis:      "ok",
			wantGroupCache: "not_initialized",
			// 模型已加载（modelCount>0 且 state.loaded）时 registry 必须是 ok：
			// 503 的真实原因是组名缓存未就绪，把 registry 硬编码为 empty 会自相矛盾。
			wantRegistry: "ok",
		},
		{
			name:           "组名缓存已就绪时返回200且 group_cache 为 ok",
			redisUp:        true,
			modelCount:     1,
			stateLoaded:    true,
			injectCache:    true,
			cacheReady:     true,
			wantCode:       http.StatusOK,
			wantStatus:     "ready",
			wantRedis:      "ok",
			wantGroupCache: "ok",
			wantRegistry:   "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr, err := miniredis.Run()
			require.NoError(t, err)

			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			if !tt.redisUp {
				mr.Close()
			} else {
				defer mr.Close()
			}
			defer rdb.Close()

			var modelsRouter handler.ModelsRouterInterface
			if tt.modelCount > 0 {
				data := make([]core.Model, 0, tt.modelCount)
				for i := 0; i < tt.modelCount; i++ {
					data = append(data, core.Model{ID: "gpt-5.6-terra", Object: "model"})
				}
				modelsRouter = &fakeHealthRouter{resp: &core.ModelsResponse{Object: "list", Data: data}}
			}

			var state *provider.RuntimeState
			if tt.stateLoaded {
				state = &provider.RuntimeState{}
				state.MarkLoadSuccess("test", 1, 1)
			}

			h := handler.NewHealthHandler(rdb, modelsRouter, state, nil)
			if tt.injectCache {
				// 使用真实 GroupNameCache，确保 *cache.GroupNameCache 满足注入接口
				gc := cache.NewGroupNameCache("test-token")
				if tt.cacheReady {
					gc.SetNames([]string{"gpt-5.6-terra"})
				}
				require.Equal(t, tt.cacheReady, gc.IsInitialized())
				h.SetGroupNameCache(gc)
			}
			if tt.injectNil {
				h.SetGroupNameCache(nil)
			}

			router := gin.New()
			router.GET("/ready", h.Ready)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))

			require.Equal(t, tt.wantCode, w.Code)

			var resp struct {
				Status  string            `json:"status"`
				Checks  map[string]string `json:"checks"`
				Details map[string]any    `json:"details"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tt.wantStatus, resp.Status)
			assert.Equal(t, tt.wantRedis, resp.Checks["redis"])
			assert.Equal(t, tt.wantGroupCache, resp.Checks["group_cache"])
			assert.Equal(t, tt.wantRegistry, resp.Checks["registry"], "registry 必须按实际加载情况取值")

			// 正常分支的既有字段不得回归
			if tt.wantCode == http.StatusOK {
				assert.Equal(t, "ok", resp.Checks["scheduler"])
				assert.Equal(t, "test", resp.Details["source"])
				assert.NotEmpty(t, resp.Details["loaded_at"])
				assert.Equal(t, float64(tt.modelCount), resp.Details["model_count"])
			}
		})
	}
}

// TestHealthHandler_ReadyGroupCacheObservability 覆盖 R3：/ready 的 details 必须输出
// group_cache_source（redis-snapshot / control-plane）与 group_cache_count，
// 让「预热就绪」与「控制面已确认」两个语义在运维侧可观测（不改变就绪判定本身）。
func TestHealthHandler_ReadyGroupCacheObservability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	state := &provider.RuntimeState{}
	state.MarkLoadSuccess("test", 1, 1)
	modelsRouter := &fakeHealthRouter{resp: &core.ModelsResponse{
		Object: "list", Data: []core.Model{{ID: "gpt-5.6-terra", Object: "model"}},
	}}

	cp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"name":"gpt-5.6-terra"},{"name":"gpt-5.6-luna"}]}`))
	}))
	defer cp.Close()

	gc := cache.NewGroupNameCache("test-token")

	serve := func() (int, map[string]any) {
		h := handler.NewHealthHandler(rdb, modelsRouter, state, nil)
		h.SetGroupNameCache(gc)
		router := gin.New()
		router.GET("/ready", h.Ready)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ready", nil))
		var resp struct {
			Details map[string]any `json:"details"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return w.Code, resp.Details
	}

	// ① 从未写入过组名（未就绪）→ 503 分支的 details 也要带来源与数量，便于门禁排查。
	status, details := serve()
	require.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "unknown", details["group_cache_source"], "从未写入时来源应为 unknown")
	assert.Equal(t, float64(0), details["group_cache_count"])

	// ② Redis 快照预热（SetNames）→ source=redis-snapshot。
	gc.SetNames([]string{"gpt-5.6-terra"})
	status, details = serve()
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "redis-snapshot", details["group_cache_source"])
	assert.Equal(t, float64(1), details["group_cache_count"])

	// ③ 控制面刷新 → source=control-plane，数量随控制面内容变化。
	require.NoError(t, gc.Refresh(context.Background(), cp.URL))
	status, details = serve()
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "control-plane", details["group_cache_source"])
	assert.Equal(t, float64(2), details["group_cache_count"])
}
