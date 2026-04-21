package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRerankOrchestrator struct {
	resp    *core.RerankResponse
	err     error
	calls   int
	lastReq *core.RerankRequest
}

func (f *fakeRerankOrchestrator) Rerank(_ context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	f.calls++
	if req != nil {
		cp := *req
		f.lastReq = &cp
	}
	return f.resp, f.err
}

type fakeRerankScheduler struct {
	decision *scheduler.RouteDecision
	err      error
}

func (f *fakeRerankScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, f.err
}

func (f *fakeRerankScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeRerankScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeRerankScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig {
	return nil
}

func (f *fakeRerankScheduler) NotifyFailure(_ string) {}

func (f *fakeRerankScheduler) SetCooldown(_ string, _ time.Duration) {}

func (f *fakeRerankScheduler) PersistSticky(_ context.Context, _, _ string, _ *scheduler.RouteDecision) {
}

func (f *fakeRerankScheduler) UpdateSticky(_ context.Context, _, _, _ string) {}

func newRerankGroupDispatcher(groupName, modelName, modelType string) *orchestrator.GroupAwareDispatcher {
	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{groupName})
	sch := &fakeRerankScheduler{
		decision: &scheduler.RouteDecision{
			GroupName:    groupName,
			ModelID:      modelName + "-id",
			ModelName:    modelName,
			ModelType:    modelType,
			ProviderName: "bailian",
		},
	}
	return orchestrator.NewGroupAwareDispatcher(sch, nc)
}

func TestRerankHandler_MultimodalRejectedForTextRerank(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &fakeRerankOrchestrator{
		resp: &core.RerankResponse{
			Model:   "gte-rerank-v2",
			Results: []core.RerankResult{{Index: 0, RelevanceScore: 0.9}},
		},
	}
	gd := newRerankGroupDispatcher("rerank-group", "gte-rerank-v2", "rerank")

	router := gin.New()
	h := handler.NewRerankHandlerWithGroup(orch, gd)
	router.POST("/v1/rerank", h.Handle)

	body := `{"model":"rerank-group","query":{"image":"https://example.com/q.png"},"documents":["doc1"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, orch.calls)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "model_capability_mismatch", errObj["code"])
	assert.Contains(t, errObj["message"], "rerank_vl")
}

func TestRerankHandler_MultimodalAllowedForRerankVL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &fakeRerankOrchestrator{
		resp: &core.RerankResponse{
			Model:   "qwen3-vl-rerank",
			Results: []core.RerankResult{{Index: 0, RelevanceScore: 0.9}},
		},
	}
	gd := newRerankGroupDispatcher("rerank-vl-group", "qwen3-vl-rerank", "rerank_vl")

	router := gin.New()
	h := handler.NewRerankHandlerWithGroup(orch, gd)
	router.POST("/v1/rerank", h.Handle)

	body := `{"model":"rerank-vl-group","query":{"image":"https://example.com/q.png"},"documents":[{"text":"a cat"},{"video":"https://example.com/d.mp4"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, orch.calls)
	require.NotNil(t, orch.lastReq)
	assert.Equal(t, "rerank_vl", orch.lastReq.ModelType)
	assert.Equal(t, "qwen3-vl-rerank", orch.lastReq.Model)
}

// TestRerankHandler_GroupDispatchNilChatRequestNoPanic
// 与 embeddings 同因：RerankHandler 调 Dispatch(c, model, nil)，不得 panic→500。
func TestRerankHandler_GroupDispatchNilChatRequestNoPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &fakeRerankOrchestrator{
		resp: &core.RerankResponse{
			Model:   "gte-rerank-v2",
			Results: []core.RerankResult{{Index: 0, RelevanceScore: 0.8}, {Index: 1, RelevanceScore: 0.2}},
		},
	}
	gd := newRerankGroupDispatcher("gte-rerank-v2", "gte-rerank-v2", "rerank")

	router := gin.New()
	router.Use(middleware.ErrorHandler())
	h := handler.NewRerankHandlerWithGroup(orch, gd)
	router.POST("/v1/rerank", h.Handle)

	body := `{"model":"gte-rerank-v2","query":"健康检查查询","documents":["文档1","文档2"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	require.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	})
	assert.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "internal_error")
	require.Equal(t, 1, orch.calls)
}
