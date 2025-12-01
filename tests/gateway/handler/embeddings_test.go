package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEmbeddingsOrchestrator 模拟 EmbeddingsOrchestratorInterface，用于测试 EmbeddingsHandler 的各种场景
type fakeEmbeddingsOrchestrator struct {
	resp *core.EmbeddingResponse
	err  error
}

func (f *fakeEmbeddingsOrchestrator) Embeddings(_ context.Context, _ *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return f.resp, f.err
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
			wantErrCode: "invalid_request_body",
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
			wantErrCode: "internal",
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
