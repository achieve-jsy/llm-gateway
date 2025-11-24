package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeModelsCache 模拟 ModelsCacheInterface，用于测试 ModelsHandler 的缓存行为
type fakeModelsCache struct {
	models  []core.Model
	version int64
	getErr  error
	setErr  error
}

func (f *fakeModelsCache) GetModelList(_ context.Context) ([]core.Model, error) {
	return f.models, f.getErr
}

func (f *fakeModelsCache) SetModelList(_ context.Context, models []core.Model) error {
	f.models = models
	return f.setErr
}

func (f *fakeModelsCache) GetModelListWithVersion(_ context.Context) ([]core.Model, int64, error) {
	return f.models, f.version, f.getErr
}

func (f *fakeModelsCache) SetModelListWithVersion(_ context.Context, models []core.Model, version int64) error {
	f.models = models
	f.version = version
	return f.setErr
}

// fakeModelsRouter 模拟 ModelsRouterInterface，用于测试 ModelsHandler 从 Router 获取模型列表
type fakeModelsRouter struct {
	resp *core.ModelsResponse
	err  error
}

func (f *fakeModelsRouter) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	return f.resp, f.err
}

func TestModelsHandler_Handle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		router      *fakeModelsRouter
		wantCode    int
		wantCount   int // 期望返回的模型数量
	}{
		{
			name:        "正常返回模型列表",
			description: "Router 正常返回包含 2 个模型的列表，期望 200 + data 数组长度为 2",
			router: &fakeModelsRouter{
				resp: &core.ModelsResponse{
					Object: "list",
					Data: []core.Model{
						{ID: "gpt-4", Object: "model", OwnedBy: "openai"},
						{ID: "claude-3", Object: "model", OwnedBy: "anthropic"},
					},
				},
			},
			wantCode:  http.StatusOK,
			wantCount: 2,
		},
		{
			name:        "Router返回错误时返回空列表",
			description: "Router.ListModels 返回错误，handler 降级返回空 data 数组 + 200",
			router: &fakeModelsRouter{
				err: errors.New("connection failed"),
			},
			wantCode:  http.StatusOK,
			wantCount: 0,
		},
		{
			name:        "Router为nil时返回空列表",
			description: "构造 ModelsHandler 时 router 为 nil，handler 安全返回空 data 数组",
			router:      nil,
			wantCode:    http.StatusOK,
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &fakeModelsCache{}

			var routerIface handler.ModelsRouterInterface
			if tt.router != nil {
				routerIface = tt.router
			}

			h := handler.NewModelsHandler(cache, routerIface, nil)

			router := gin.New()
			router.GET("/v1/models", h.Handle)

			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)

			var resp core.ModelsResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, "list", resp.Object)
			assert.Len(t, resp.Data, tt.wantCount)
		})
	}
}

func TestModelsHandler_HandleLegacy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		cache       *fakeModelsCache
		router      *fakeModelsRouter
		wantCode    int
		wantCount   int
	}{
		{
			name:        "缓存命中直接返回",
			description: "cache.GetModelListWithVersion 返回模型列表且版本匹配（version=0 时无校验），直接返回缓存数据",
			cache: &fakeModelsCache{
				models: []core.Model{
					{ID: "gpt-4", Object: "model", OwnedBy: "openai"},
				},
				version: 0,
			},
			router:    &fakeModelsRouter{},
			wantCode:  http.StatusOK,
			wantCount: 1,
		},
		{
			name:        "缓存未命中从Router回退",
			description: "cache 返回 nil models，handler 从 Router 获取并写入缓存，返回 Router 数据",
			cache: &fakeModelsCache{
				models: nil,
			},
			router: &fakeModelsRouter{
				resp: &core.ModelsResponse{
					Object: "list",
					Data: []core.Model{
						{ID: "claude-3", Object: "model", OwnedBy: "anthropic"},
						{ID: "deepseek", Object: "model", OwnedBy: "deepseek"},
					},
				},
			},
			wantCode:  http.StatusOK,
			wantCount: 2,
		},
		{
			name:        "缓存和Router都为空",
			description: "cache 无数据，Router 也返回空列表，handler 返回空 data 数组",
			cache:       &fakeModelsCache{},
			router: &fakeModelsRouter{
				resp: &core.ModelsResponse{
					Object: "list",
					Data:   []core.Model{},
				},
			},
			wantCode:  http.StatusOK,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewModelsHandler(tt.cache, tt.router, nil)

			router := gin.New()
			router.GET("/v1/health/models", h.HandleLegacy)

			req := httptest.NewRequest(http.MethodGet, "/v1/health/models", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)

			var resp core.ModelsResponse
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)
			assert.Equal(t, "list", resp.Object)
			assert.Len(t, resp.Data, tt.wantCount)
		})
	}
}
