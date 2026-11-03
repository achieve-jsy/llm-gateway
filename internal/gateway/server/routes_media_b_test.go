package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Batch 1 · B2 的**路由接线**：assert registerMediaImageRoutes 真的把
// POST /v1/images/generations 与 POST /v1/images/edits 挂到了 deps.V1 上。
//
// 它放在 internal/gateway/server 包内，因为被测对象（registerMediaImageRoutes /
// MediaDeps）是该包的包内符号；除此之外本文件只做接线断言，不含业务逻辑。
//
// 为什么必须有这条测试：route 接线错误（少写一行、挂错组）在 handler 单测里
// 完全看不出来——handler 直测永远通过。

// routeCheckLookup 是 core.ModelLookup 的最小实现：只有一个 openai 类型 provider。
type routeCheckLookup struct {
	provider core.Provider
}

func (l *routeCheckLookup) Supports(model string) bool       { return l.provider != nil }
func (l *routeCheckLookup) GetProvider(string) core.Provider { return l.provider }
func (l *routeCheckLookup) GetProviderType(string) string    { return "openai" }
func (l *routeCheckLookup) ListModels() []core.Model         { return nil }
func (l *routeCheckLookup) ModelCount() int                  { return 0 }
func (l *routeCheckLookup) GetProviderName(string) string    { return "openai-test" }
func (l *routeCheckLookup) GetProviderNameForType(string) string {
	return "openai-test"
}
func (l *routeCheckLookup) GetProviderTypeForName(string) string { return "openai" }
func (l *routeCheckLookup) ProviderByType(string) core.Provider  { return l.provider }

// routeCheckScheduler 是 orchestrator.Scheduler 的最小实现（组内单成员）。
type routeCheckScheduler struct{}

func (routeCheckScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return &scheduler.RouteDecision{
		GroupName:    "image-group",
		ModelID:      "m-1",
		ModelName:    "gpt-image-1",
		ModelType:    "image",
		ChannelType:  "openai",
		ProviderName: "openai-test",
	}, nil
}
func (routeCheckScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, core.NewNotFoundError("no failover")
}
func (routeCheckScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, core.NewNotFoundError("no failover")
}
func (routeCheckScheduler) GetGroupConfig(string) *scheduler.GroupRoutingConfig { return nil }
func (routeCheckScheduler) NotifyFailure(string)                                {}
func (routeCheckScheduler) SetCooldown(string, time.Duration)                   {}
func (routeCheckScheduler) PersistSticky(context.Context, string, string, *scheduler.RouteDecision) {
}
func (routeCheckScheduler) UpdateSticky(context.Context, string, string, string, string) bool {
	return false
}

// noImageCapabilityProvider 只实现 core.Provider：让 /v1/images/* 的请求走到
// 「类型断言失败 → 501」这条路径。501（而不是 404）即证明路由已注册。
type noImageCapabilityProvider struct{}

func (noImageCapabilityProvider) ChatCompletion(context.Context, *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, nil
}
func (noImageCapabilityProvider) StreamChatCompletion(context.Context, *core.ChatRequest) (io.ReadCloser, error) {
	return nil, nil
}
func (noImageCapabilityProvider) ListModels(context.Context) (*core.ModelsResponse, error) {
	return nil, nil
}
func (noImageCapabilityProvider) Responses(context.Context, *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return nil, nil
}
func (noImageCapabilityProvider) StreamResponses(context.Context, *core.ResponsesRequest) (io.ReadCloser, error) {
	return nil, nil
}
func (noImageCapabilityProvider) Embeddings(context.Context, *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return nil, nil
}
func (noImageCapabilityProvider) Rerank(context.Context, *core.RerankRequest) (*core.RerankResponse, error) {
	return nil, nil
}

func newMediaRouteTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	provider := noImageCapabilityProvider{}
	lookup := &routeCheckLookup{provider: provider}
	router, err := providers.NewRouter(lookup)
	require.NoError(t, err)

	nameCache := cache.NewGroupNameCache("")
	nameCache.SetNames([]string{"image-group"})
	dispatcher := orchestrator.NewGroupAwareDispatcher(routeCheckScheduler{}, nameCache)

	engine := gin.New()
	v1 := engine.Group("/v1")
	deps := MediaDeps{
		V1:            v1,
		RouterGetter:  func() *providers.Router { return router },
		GroupDispatch: dispatcher,
	}
	registerMediaImageRoutes(deps)
	return engine
}

func TestRegisterMediaImageRoutes_GenerationsIsWired(t *testing.T) {
	engine := newMediaRouteTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		strings.NewReader(`{"model":"image-group","prompt":"a cat"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusNotFound, w.Code, "路由必须已注册（404 表示 registerMediaImageRoutes 没有挂上）")
	assert.Equal(t, http.StatusNotImplemented, w.Code,
		"无图像能力的 provider 必须映射为 501 not_implemented")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, "not_implemented", errObj["code"])
}

func TestRegisterMediaImageRoutes_EditsIsWired(t *testing.T) {
	engine := newMediaRouteTestRouter(t)

	// 给一个合法 multipart（缺 image 会 400）——重点断言「不是 404」。
	body := "--B\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nimage-group\r\n" +
		"--B\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\nedit\r\n--B--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusNotFound, w.Code, "路由必须已注册")
	// 没有 image part → 400（handler 的参数校验），同样证明请求进了 handler。
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRegisterMediaImageRoutes_OnlyPOSTAndOnlyImagePaths(t *testing.T) {
	engine := newMediaRouteTestRouter(t)

	t.Run("GET 未注册", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/images/generations", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("任务链路路由不得被 B 队注册", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code,
			"/v1/videos 属 C 队（registerMediaTaskRoutes），B 队不得注册")
	})
}

func TestRegisterMediaImageRoutes_NilV1IsSafe(t *testing.T) {
	// 路由未就绪时必须直接返回（不得 panic）——这是 MediaDeps 的既有约定。
	require.NotPanics(t, func() {
		registerMediaImageRoutes(MediaDeps{})
	})
}
