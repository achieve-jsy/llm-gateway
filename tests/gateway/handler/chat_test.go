package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// fakeChatOrchestrator 模拟 ChatOrchestratorInterface，用于测试 ChatHandler 的各种场景
type fakeChatOrchestrator struct {
	chatResp   *core.ChatResponse
	chatErr    error
	streamBody io.ReadCloser
	streamErr  error
}

func (f *fakeChatOrchestrator) Chat(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	return f.chatResp, f.chatErr
}

func (f *fakeChatOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return f.streamBody, f.streamErr
}

func TestChatHandler_Handle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		body        string
		orch        *fakeChatOrchestrator
		wantCode    int
		wantErr     bool   // 响应体含 error 字段
		wantErrCode string // 期望的 error.code
	}{
		{
			name:        "正常同步请求",
			description: "发送有效 JSON body（model + messages），orchestrator 正常返回 ChatResponse，期望 200 + 正确 JSON",
			body:        `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`,
			orch: &fakeChatOrchestrator{
				chatResp: &core.ChatResponse{
					ID:     "chatcmpl-123",
					Object: "chat.completion",
					Model:  "gpt-4",
					Choices: []core.Choice{
						{
							Index:        0,
							FinishReason: "stop",
							Message: core.ResponseMessage{
								Role:    "assistant",
								Content: "Hi there!",
							},
						},
					},
					Usage: core.Usage{
						PromptTokens:     10,
						CompletionTokens: 5,
						TotalTokens:      15,
					},
				},
			},
			wantCode: http.StatusOK,
			wantErr:  false,
		},
		{
			name:        "无效JSON body",
			description: "发送畸形 JSON body，ShouldBindJSON 解析失败，期望 400 + invalid_request_body",
			body:        `{invalid json`,
			orch:        &fakeChatOrchestrator{},
			wantCode:    http.StatusBadRequest,
			wantErr:     true,
			wantErrCode: "invalid_request_body",
		},
		{
			name:        "Orchestrator返回UnifiedError",
			description: "orchestrator 返回 model.UnifiedError（如模型不存在），handler 映射为对应 HTTP 状态码 + 错误格式",
			body:        `{"model":"nonexistent","messages":[{"role":"user","content":"hi"}]}`,
			orch: &fakeChatOrchestrator{
				chatErr: model.ErrModelNotFound,
			},
			wantCode:    http.StatusNotFound,
			wantErr:     true,
			wantErrCode: "unknown_model",
		},
		{
			name:        "Orchestrator返回通用error",
			description: "orchestrator 返回非 UnifiedError 的普通 error，handler 返回 500 internal",
			body:        `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`,
			orch: &fakeChatOrchestrator{
				chatErr: errors.New("unexpected failure"),
			},
			wantCode:    http.StatusInternalServerError,
			wantErr:     true,
			wantErrCode: "internal",
		},
		{
			name:        "请求体model字段缺失",
			description: "model 为空字符串时 ShouldBindJSON 仍能解析，但 orchestrator 可能报错；此处验证空 model 不触发 bind 错误",
			body:        `{"messages":[{"role":"user","content":"hi"}]}`,
			orch: &fakeChatOrchestrator{
				chatErr: model.ErrModelNotFound,
			},
			wantCode:    http.StatusNotFound,
			wantErr:     true,
			wantErrCode: "unknown_model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			h := handler.NewChatHandler(tt.orch)
			router.POST("/v1/chat/completions", h.Handle)

			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tt.body))
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
				assert.Equal(t, "chatcmpl-123", resp["id"])
			}
		})
	}
}

func TestChatHandler_Handle_StreamRequest(t *testing.T) {
	// 测试目的：验证 stream=true 时 handler 走 SSE 流式路径，正确设置响应头并转发流内容
	gin.SetMode(gin.TestMode)

	sseData := "data: {\"id\":\"chatcmpl-1\"}\n\ndata: [DONE]\n\n"
	orch := &fakeChatOrchestrator{
		streamBody: io.NopCloser(strings.NewReader(sseData)),
	}

	router := gin.New()
	h := handler.NewChatHandler(orch)
	router.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/event-stream")
	assert.Contains(t, w.Body.String(), "data: {\"id\":\"chatcmpl-1\"}")
}

func TestChatHandler_Handle_StreamError(t *testing.T) {
	// 测试目的：验证 stream=true 且 orchestrator 返回错误时，handler 返回错误 JSON
	gin.SetMode(gin.TestMode)

	orch := &fakeChatOrchestrator{
		streamErr: model.ErrModelNotFound,
	}

	router := gin.New()
	h := handler.NewChatHandler(orch)
	router.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"bad-model","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "unknown_model", errObj["code"])
}
