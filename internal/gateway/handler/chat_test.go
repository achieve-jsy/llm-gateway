package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockOrchestrator mocks ChatOrchestratorInterface + EmbeddingsOrchestratorInterface
type MockOrchestrator struct {
	mock.Mock
}

func (m *MockOrchestrator) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*core.ChatResponse), args.Error(1)
}

func (m *MockOrchestrator) ChatStream(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockOrchestrator) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*core.EmbeddingResponse), args.Error(1)
}

// TestHandle_NonStream_Success tests ChatHandler.Handle with non-stream response
func TestHandle_NonStream_Success(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		mockResp    *core.ChatResponse
		mockErr     error
		expectCode  int
		expectModel string
	}{
		{
			name:  "valid request returns 200 with response",
			model: "gpt-3.5",
			mockResp: &core.ChatResponse{
				ID:     "resp-123",
				Object: "chat.completion",
				Model:  "gpt-3.5",
				Choices: []core.Choice{
					{
						Index: 0,
						Message: core.ResponseMessage{
							Role:    "assistant",
							Content: "Hi there!",
						},
						FinishReason: "stop",
					},
				},
			},
			mockErr:    nil,
			expectCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOrch := new(MockOrchestrator)
			mockOrch.On("Chat", mock.Anything, mock.MatchedBy(func(r *core.ChatRequest) bool {
				return r.Model == tt.model
			})).Return(tt.mockResp, tt.mockErr)

			handler := NewChatHandler(mockOrch)

			c, w := createTestContext()
			reqBody := map[string]interface{}{
				"model":    tt.model,
				"messages": []map[string]string{{"role": "user", "content": "Hello"}},
				"stream":   false,
			}
			body, _ := json.Marshal(reqBody)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			handler.Handle(c)

			assert.Equal(t, tt.expectCode, w.Code)
			if tt.expectCode == http.StatusOK {
				var respBody map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &respBody)
				assert.Equal(t, tt.mockResp.ID, respBody["id"])
			}
		})
	}
}

// TestHandle_Stream_Success tests ChatHandler.Handle with stream=true
func TestHandle_Stream_Success(t *testing.T) {
	mockOrch := new(MockOrchestrator)

	// Return a simple SSE stream
	sseBody := "data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\"}\n\ndata: [DONE]\n\n"
	stream := io.NopCloser(strings.NewReader(sseBody))

	mockOrch.On("ChatStream", mock.Anything, mock.MatchedBy(func(r *core.ChatRequest) bool {
		return r.Model == "gpt-3.5" && r.Stream
	})).Return(stream, nil)

	handler := NewChatHandler(mockOrch)

	c, w := createTestContext()
	reqBody := map[string]interface{}{
		"model":    "gpt-3.5",
		"messages": []map[string]string{{"role": "user", "content": "Hi"}},
		"stream":   true,
	}
	body, _ := json.Marshal(reqBody)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	bodyStr := w.Body.String()
	assert.Contains(t, bodyStr, "data:")
	assert.Contains(t, bodyStr, "[DONE]")
}

// TestHandle_InvalidBody tests ChatHandler.Handle with malformed JSON
func TestHandle_InvalidBody(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		expectCode int
	}{
		{
			name:       "malformed JSON returns 400",
			body:       `{invalid json}`,
			expectCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOrch := new(MockOrchestrator)
			handler := NewChatHandler(mockOrch)
			c, w := createTestContext()
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(tt.body)))
			c.Request.Header.Set("Content-Type", "application/json")

			handler.Handle(c)

			assert.Equal(t, tt.expectCode, w.Code)
			var errResp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &errResp)
			errObj, ok := errResp["error"].(map[string]interface{})
			assert.True(t, ok)
			assert.Equal(t, "invalid_request", errObj["type"])
		})
	}
}

// TestHandle_ProviderError tests ChatHandler.Handle when orchestrator returns UnifiedError
func TestHandle_ProviderError(t *testing.T) {
	tests := []struct {
		name       string
		mockErr    *model.UnifiedError
		expectCode int
		expectType string
	}{
		{
			name:       "model not found returns 404",
			mockErr:    model.ErrModelNotFound,
			expectCode: http.StatusNotFound,
			expectType: "model_not_found",
		},
		{
			name:       "model unavailable returns 503",
			mockErr:    model.ErrModelUnavailable,
			expectCode: http.StatusServiceUnavailable,
			expectType: "model_unavailable",
		},
		{
			name:       "provider timeout returns 504",
			mockErr:    model.ErrProviderTimeout,
			expectCode: http.StatusGatewayTimeout,
			expectType: "temporary_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOrch := new(MockOrchestrator)
			mockOrch.On("Chat", mock.Anything, mock.Anything).Return(nil, tt.mockErr)

			handler := NewChatHandler(mockOrch)

			c, w := createTestContext()
			reqBody := map[string]interface{}{
				"model":    "gpt-3.5",
				"messages": []map[string]string{{"role": "user", "content": "test"}},
			}
			body, _ := json.Marshal(reqBody)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			handler.Handle(c)

			assert.Equal(t, tt.expectCode, w.Code)
			var errResp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &errResp)
			errObj := errResp["error"].(map[string]interface{})
			assert.Equal(t, tt.expectType, errObj["type"])
		})
	}
}

// Helper functions
func createTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}
