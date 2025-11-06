package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockOrchestrator mocks ChatOrchestrator
type MockOrchestrator struct {
	mock.Mock
}

func (m *MockOrchestrator) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.ChatCompletionResponse), args.Error(1)
}

func (m *MockOrchestrator) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan model.StreamChunk), args.Error(1)
}

// TestHandle_NonStream_Success tests ChatHandler.Handle with non-stream response
func TestHandle_NonStream_Success(t *testing.T) {
	tests := []struct {
		name        string
		request     model.ChatCompletionRequest
		mockResp    *model.ChatCompletionResponse
		mockErr     error
		expectCode  int
		expectModel string
	}{
		{
			name: "valid request returns 200 with response",
			request: model.ChatCompletionRequest{
				Model: "gpt-3.5",
				Messages: []model.Message{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
				Stream: false,
			},
			mockResp: &model.ChatCompletionResponse{
				ID:      "resp-123",
				Object:  "chat.completion",
				Created: 1234567890,
				Model:   "gpt-3.5",
				Choices: []model.Choice{
					{
						Index: 0,
						Message: &model.Message{
							Role:    "assistant",
							Content: "Hi there!",
						},
						FinishReason: strPtr("stop"),
					},
				},
			},
			mockErr:     nil,
			expectCode:  http.StatusOK,
			expectModel: "gpt-3.5",
		},
		{
			name: "request with temperature and max_tokens",
			request: model.ChatCompletionRequest{
				Model: "claude-3",
				Messages: []model.Message{
					{
						Role:    "user",
						Content: "Test",
					},
				},
				Stream:      false,
				Temperature: float64Ptr(0.7),
				MaxTokens:   intPtr(100),
			},
			mockResp: &model.ChatCompletionResponse{
				ID:     "resp-456",
				Object: "chat.completion",
				Model:  "claude-3",
				Choices: []model.Choice{
					{
						Index:        0,
						FinishReason: strPtr("stop"),
					},
				},
			},
			mockErr:     nil,
			expectCode:  http.StatusOK,
			expectModel: "claude-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOrch := new(MockOrchestrator)
			mockOrch.On("Chat", mock.Anything, mock.MatchedBy(func(r *model.ChatCompletionRequest) bool {
				return r.Model == tt.request.Model
			})).Return(tt.mockResp, tt.mockErr)

			handler := NewChatHandler(mockOrch)

			c, w := createTestContext()
			body, _ := json.Marshal(tt.request)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			handler.Handle(c)

			assert.Equal(t, tt.expectCode, w.Code)
			if tt.expectCode == http.StatusOK {
				var respBody map[string]interface{}
				json.Unmarshal(w.Body.Bytes(), &respBody)
				assert.Equal(t, tt.mockResp.ID, respBody["id"])
				assert.Equal(t, tt.mockResp.Object, respBody["object"])
			}
		})
	}
}

// TestHandle_Stream_Success tests ChatHandler.Handle with stream=true
func TestHandle_Stream_Success(t *testing.T) {
	mockOrch := new(MockOrchestrator)

	// Create a channel for stream chunks
	chunkChan := make(chan model.StreamChunk, 2)
	chunkChan <- model.StreamChunk{
		Data: &model.ChatCompletionChunk{
			ID:      "chunk-1",
			Object:  "chat.completion.chunk",
			Created: 1234567890,
			Model:   "gpt-3.5",
			Choices: []model.Choice{
				{
					Index: 0,
					Delta: &model.Delta{
						Content: "Hello",
					},
				},
			},
		},
	}
	chunkChan <- model.StreamChunk{Done: true}
	close(chunkChan)

	mockOrch.On("ChatStream", mock.Anything, mock.MatchedBy(func(r *model.ChatCompletionRequest) bool {
		return r.Model == "gpt-3.5" && r.Stream
	})).Return((<-chan model.StreamChunk)(chunkChan), nil)

	handler := NewChatHandler(mockOrch)

	c, w := createTestContext()
	req := model.ChatCompletionRequest{
		Model: "gpt-3.5",
		Messages: []model.Message{
			{Role: "user", Content: "Hi"},
		},
		Stream: true,
	}
	body, _ := json.Marshal(req)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	body_str := w.Body.String()
	assert.Contains(t, body_str, "data:")
	assert.Contains(t, body_str, "[DONE]")
}

// TestHandle_InvalidBody tests ChatHandler.Handle with malformed JSON
func TestHandle_InvalidBody(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		expectCode     int
		expectErrorMsg string
	}{
		{
			name:       "malformed JSON returns 400",
			body:       `{invalid json}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "missing required model field",
			body:       `{"messages":[{"role":"user","content":"hi"}]}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "missing required messages field",
			body:       `{"model":"gpt-3.5"}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "empty messages array",
			body:       `{"model":"gpt-3.5","messages":[]}`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "invalid message role",
			body:       `{"model":"gpt-3.5","messages":[{"role":"invalid","content":"hi"}]}`,
			expectCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOrch := new(MockOrchestrator)
			// 即使 binding 通过，调用 orchestrator 也应被识别为参数错误；
			// 这里给出默认返回，避免 mock 在意外调用时 panic。
			mockOrch.On("Chat", mock.Anything, mock.Anything).Return(nil, model.ErrInternal).Maybe()
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
		request    model.ChatCompletionRequest
		mockErr    *model.UnifiedError
		expectCode int
		expectType string
	}{
		{
			name: "model not found returns 404",
			request: model.ChatCompletionRequest{
				Model: "unknown-model",
				Messages: []model.Message{
					{Role: "user", Content: "test"},
				},
			},
			mockErr:    model.ErrModelNotFound,
			expectCode: http.StatusNotFound,
			expectType: "model_not_found",
		},
		{
			name: "model unavailable returns 503",
			request: model.ChatCompletionRequest{
				Model: "gpt-3.5",
				Messages: []model.Message{
					{Role: "user", Content: "test"},
				},
			},
			mockErr:    model.ErrModelUnavailable,
			expectCode: http.StatusServiceUnavailable,
			expectType: "model_unavailable",
		},
		{
			name: "provider timeout returns 504",
			request: model.ChatCompletionRequest{
				Model: "gpt-3.5",
				Messages: []model.Message{
					{Role: "user", Content: "test"},
				},
			},
			mockErr:    model.ErrProviderTimeout,
			expectCode: http.StatusGatewayTimeout,
			expectType: "temporary_failure",
		},
		{
			name: "provider error returns 502",
			request: model.ChatCompletionRequest{
				Model: "gpt-3.5",
				Messages: []model.Message{
					{Role: "user", Content: "test"},
				},
			},
			mockErr:    model.ErrProviderError,
			expectCode: http.StatusBadGateway,
			expectType: "temporary_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOrch := new(MockOrchestrator)
			mockOrch.On("Chat", mock.Anything, mock.Anything).Return(nil, tt.mockErr)

			handler := NewChatHandler(mockOrch)

			c, w := createTestContext()
			body, _ := json.Marshal(tt.request)
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

func strPtr(s string) *string {
	return &s
}

func float64Ptr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}
