package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEmbeddingsOrchestrator 模拟 EmbeddingsOrchestratorInterface，用于测试 EmbeddingsHandler 的各种场景
type fakeEmbeddingsOrchestrator struct {
	resp    *core.EmbeddingResponse
	err     error
	calls   int
	lastReq *core.EmbeddingRequest
}

func (f *fakeEmbeddingsOrchestrator) Embeddings(_ context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	f.calls++
	if req != nil {
		cp := *req
		f.lastReq = &cp
	}
	return f.resp, f.err
}

type fakeEmbeddingScheduler struct {
	decision *scheduler.RouteDecision
	err      error
}

func (f *fakeEmbeddingScheduler) Route(_ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return f.decision, f.err
}

func (f *fakeEmbeddingScheduler) Failover(_, _ string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeEmbeddingScheduler) FailoverExcluding(_ string, _ []string, _ *core.ChatRequest) (*scheduler.RouteDecision, error) {
	return nil, model.ErrNoOtherModelAvailable
}

func (f *fakeEmbeddingScheduler) GetGroupConfig(_ string) *scheduler.GroupRoutingConfig {
	return nil
}

func (f *fakeEmbeddingScheduler) NotifyFailure(_ string) {}

func (f *fakeEmbeddingScheduler) SetCooldown(_ string, _ time.Duration) {}

func newEmbeddingGroupDispatcher(groupName, modelName, modelType string) *orchestrator.GroupAwareDispatcher {
	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{groupName})
	sch := &fakeEmbeddingScheduler{
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

func TestEmbeddingsHandler_Handle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		body        string
		orch        *fakeEmbeddingsOrchestrator
		wantCode    int
		wantErr     bool
		wantErrCode string
	}{
		{
			name:        "正常Embeddings请求",
			description: "发送有效 model + input 字段，orchestrator 正常返回 EmbeddingResponse，期望 200",
			body:        `{"model":"text-embedding-ada-002","input":"hello world"}`,
			orch: &fakeEmbeddingsOrchestrator{
				resp: &core.EmbeddingResponse{
					Object: "list",
					Model:  "text-embedding-ada-002",
					Data: []core.EmbeddingData{
						{
							Object:    "embedding",
							Embedding: json.RawMessage(`[0.1, 0.2, 0.3]`),
							Index:     0,
						},
					},
					Usage: core.EmbeddingUsage{
						PromptTokens: 2,
						TotalTokens:  2,
					},
				},
			},
			wantCode: http.StatusOK,
			wantErr:  false,
		},
		{
			name:        "无效JSON body",
			description: "发送畸形 JSON body，ShouldBindJSON 解析失败，期望 400 + invalid_request_body",
			body:        `not valid json`,
			orch:        &fakeEmbeddingsOrchestrator{},
			wantCode:    http.StatusBadRequest,
			wantErr:     true,
			wantErrCode: "request_body_invalid",
		},
		{
			name:        "空model字符串",
			description: "发送空 model 字符串，handler 校验 model 非空，期望 400 + request_missing_model",
			body:        `{"model":"","input":"test"}`,
			orch:        &fakeEmbeddingsOrchestrator{},
			wantCode:    http.StatusBadRequest,
			wantErr:     true,
			wantErrCode: "request_missing_model",
		},
		{
			name:        "空input字符串",
			description: "发送空字符串 input，handler 校验 input 不能为空字符串，期望 400",
			body:        `{"model":"test","input":""}`,
			orch:        &fakeEmbeddingsOrchestrator{},
			wantCode:    http.StatusBadRequest,
			wantErr:     true,
			wantErrCode: "request_body_invalid",
		},
		{
			name:        "nil input",
			description: "发送不含 input 字段的请求，handler 校验 input 非空，期望 400",
			body:        `{"model":"test"}`,
			orch:        &fakeEmbeddingsOrchestrator{},
			wantCode:    http.StatusBadRequest,
			wantErr:     true,
			wantErrCode: "request_body_invalid",
		},
		{
			name:        "空input数组",
			description: "发送空数组 input，handler 校验数组不能为空，期望 400",
			body:        `{"model":"test","input":[]}`,
			orch:        &fakeEmbeddingsOrchestrator{},
			wantCode:    http.StatusBadRequest,
			wantErr:     true,
			wantErrCode: "request_body_invalid",
		},
		{
			name:        "无效input类型",
			description: "发送数字类型 input，handler 校验 input 必须是 string 或 []string，期望 400",
			body:        `{"model":"test","input":123}`,
			orch:        &fakeEmbeddingsOrchestrator{},
			wantCode:    http.StatusBadRequest,
			wantErr:     true,
			wantErrCode: "request_body_invalid",
		},
		{
			name:        "Orchestrator返回UnifiedError",
			description: "orchestrator 返回 model.UnifiedError（模型不存在），handler 映射为对应 HTTP 状态码",
			body:        `{"model":"nonexistent","input":"test"}`,
			orch: &fakeEmbeddingsOrchestrator{
				err: model.ErrModelNotFound,
			},
			wantCode:    http.StatusNotFound,
			wantErr:     true,
			wantErrCode: "unknown_model",
		},
		{
			name:        "Orchestrator返回通用error",
			description: "orchestrator 返回非 UnifiedError 的普通 error，handler 返回 500 internal",
			body:        `{"model":"text-embedding-ada-002","input":"test"}`,
			orch: &fakeEmbeddingsOrchestrator{
				err: errors.New("upstream timeout"),
			},
			wantCode:    http.StatusInternalServerError,
			wantErr:     true,
			wantErrCode: "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			h := handler.NewEmbeddingsHandler(tt.orch)
			router.POST("/v1/embeddings", h.Handle)

			req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)

			var resp map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err, "响应体应为合法 JSON")

			if tt.wantErr {
				errObj, ok := resp["error"].(map[string]any)
				require.True(t, ok, "响应体应包含 error 对象")
				if tt.wantErrCode != "" {
					assert.Equal(t, tt.wantErrCode, errObj["code"])
				}
			} else {
				_, hasErr := resp["error"]
				assert.False(t, hasErr, "成功响应不应包含 error 字段")
				assert.Equal(t, "list", resp["object"])
				assert.Equal(t, "text-embedding-ada-002", resp["model"])
			}
		})
	}
}

func TestEmbeddingsHandler_MultimodalInputRequiresEmbeddingVL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &fakeEmbeddingsOrchestrator{
		resp: &core.EmbeddingResponse{
			Object: "list",
			Model:  "text-embedding-v4",
			Data: []core.EmbeddingData{{
				Object:    "embedding",
				Embedding: json.RawMessage(`[0.1,0.2,0.3]`),
				Index:     0,
			}},
			Usage: core.EmbeddingUsage{PromptTokens: 1, TotalTokens: 1},
		},
	}
	gd := newEmbeddingGroupDispatcher("embedding-group", "text-embedding-v4", "embedding")

	router := gin.New()
	h := handler.NewEmbeddingsHandlerWithGroup(orch, gd)
	router.POST("/v1/embeddings", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"embedding-group","input":[{"image":"https://example.com/cat.png"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, orch.calls, "能力不匹配时不应调用上游 orchestrator")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "响应体应包含 error 对象")
	assert.Equal(t, "model_capability_mismatch", errObj["code"])
	assert.Contains(t, errObj["message"], "embedding_vl")
}

func TestEmbeddingsHandler_DirectMultimodalInputRequiresEmbeddingVL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &fakeEmbeddingsOrchestrator{
		resp: &core.EmbeddingResponse{
			Object: "list",
			Model:  "text-embedding-v4",
			Data: []core.EmbeddingData{{
				Object:    "embedding",
				Embedding: json.RawMessage(`[0.1,0.2,0.3]`),
				Index:     0,
			}},
			Usage: core.EmbeddingUsage{PromptTokens: 1, TotalTokens: 1},
		},
	}

	router := gin.New()
	h := handler.NewEmbeddingsHandler(orch)
	router.POST("/v1/embeddings", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"text-embedding-v4","input":[{"audio":"https://example.com/cat.wav"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, orch.calls, "direct model 能力不明时不应调用上游 orchestrator")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "响应体应包含 error 对象")
	assert.Equal(t, "model_capability_mismatch", errObj["code"])
}

func TestEmbeddingsHandler_MultimodalInputAllowsEmbeddingVL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &fakeEmbeddingsOrchestrator{
		resp: &core.EmbeddingResponse{
			Object: "list",
			Model:  "qwen3-vl-embedding",
			Data: []core.EmbeddingData{{
				Object:    "embedding",
				Embedding: json.RawMessage(`[0.1,0.2,0.3]`),
				Index:     0,
			}},
			Usage: core.EmbeddingUsage{PromptTokens: 1, TotalTokens: 1},
		},
	}
	gd := newEmbeddingGroupDispatcher("embedding-vl-group", "qwen3-vl-embedding", "embedding_vl")

	router := gin.New()
	h := handler.NewEmbeddingsHandlerWithGroup(orch, gd)
	router.POST("/v1/embeddings", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"embedding-vl-group","input":[{"image":"https://example.com/cat.png"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, orch.calls)
	require.NotNil(t, orch.lastReq)
	assert.Equal(t, "embedding_vl", orch.lastReq.ModelType)
	assert.Equal(t, "qwen3-vl-embedding", orch.lastReq.Model)
}

func TestEmbeddingsHandler_TextInputAllowsEmbeddingType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &fakeEmbeddingsOrchestrator{
		resp: &core.EmbeddingResponse{
			Object: "list",
			Model:  "text-embedding-v4",
			Data: []core.EmbeddingData{{
				Object:    "embedding",
				Embedding: json.RawMessage(`[0.1,0.2,0.3]`),
				Index:     0,
			}},
			Usage: core.EmbeddingUsage{PromptTokens: 1, TotalTokens: 1},
		},
	}
	gd := newEmbeddingGroupDispatcher("embedding-group", "text-embedding-v4", "embedding")

	router := gin.New()
	h := handler.NewEmbeddingsHandlerWithGroup(orch, gd)
	router.POST("/v1/embeddings", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"embedding-group","input":"hello world"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, orch.calls)
	require.NotNil(t, orch.lastReq)
	assert.Equal(t, "embedding", orch.lastReq.ModelType)
	assert.Equal(t, "text-embedding-v4", orch.lastReq.Model)
}
