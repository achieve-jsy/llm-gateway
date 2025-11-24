package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/core"
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
