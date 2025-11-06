package bailian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ──────────────────────────────────────────────────────────────

func newTestAdapter(baseURL string) *Adapter {
	return New(Config{
		APIKey:  "test-key",
		BaseURL: baseURL,
		Models:  []string{"qwen-turbo", "qwen-plus"},
	}).(*Adapter)
}

func basicChatReq() *model.ChatCompletionRequest {
	return &model.ChatCompletionRequest{
		Model: "qwen-turbo",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
	}
}

// ─── Name ─────────────────────────────────────────────────────────────────

func TestBailian_Name(t *testing.T) {
	a := New(Config{APIKey: "k"})
	assert.Equal(t, "bailian", a.Name())
}

// ─── New / defaults ────────────────────────────────────────────────────────

func TestBailian_New_SetsDefaultBaseURL(t *testing.T) {
	a := New(Config{APIKey: "k"}).(*Adapter)
	assert.Equal(t, defaultBaseURL, a.config.BaseURL)
}

func TestBailian_New_PreservesCustomBaseURL(t *testing.T) {
	a := New(Config{APIKey: "k", BaseURL: "https://custom.example.com"}).(*Adapter)
	assert.Equal(t, "https://custom.example.com", a.config.BaseURL)
}

// ─── ListModels ────────────────────────────────────────────────────────────

func TestBailian_ListModels_ReturnsConfiguredModels(t *testing.T) {
	a := New(Config{APIKey: "k", Models: []string{"qwen-turbo", "qwen-plus"}})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "qwen-turbo", models[0].ID)
	assert.Equal(t, "bailian", models[0].OwnedBy)
	assert.Equal(t, "model", models[0].Object)
}

func TestBailian_ListModels_Empty(t *testing.T) {
	a := New(Config{APIKey: "k"})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

// ─── ToBailianError ────────────────────────────────────────────────────────

func TestBailian_ToBailianError_ParsesStructuredError(t *testing.T) {
	body := []byte(`{"error":{"code":"invalid_request_error","message":"Bad input","type":"invalid_request_error"}}`)
	err := ToBailianError(http.StatusBadRequest, body)
	require.Error(t, err)

	be, ok := err.(*BailianError)
	require.True(t, ok, "error should be *BailianError")
	assert.Equal(t, http.StatusBadRequest, be.HTTPStatus)
	assert.Equal(t, "invalid_request_error", be.Code)
	assert.Equal(t, "Bad input", be.Message)
	assert.Contains(t, be.Error(), "400")
}

func TestBailian_ToBailianError_FallsBackToRawBody(t *testing.T) {
	body := []byte(`not valid json`)
	err := ToBailianError(http.StatusInternalServerError, body)
	require.Error(t, err)

	be, ok := err.(*BailianError)
	require.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, be.HTTPStatus)
	assert.Equal(t, "not valid json", be.Message)
}

func TestBailian_ToBailianError_401Unauthorized(t *testing.T) {
	body := []byte(`{"error":{"code":"unauthorized","message":"Invalid API key","type":"auth_error"}}`)
	err := ToBailianError(http.StatusUnauthorized, body)
	be := err.(*BailianError)
	assert.Equal(t, 401, be.HTTPStatus)
	assert.Contains(t, be.Error(), "401")
}

func TestBailian_ToBailianError_429RateLimit(t *testing.T) {
	body := []byte(`{"error":{"code":"rate_limit_exceeded","message":"Too many requests","type":"rate_limit_error"}}`)
	err := ToBailianError(http.StatusTooManyRequests, body)
	be := err.(*BailianError)
	assert.Equal(t, 429, be.HTTPStatus)
	assert.Equal(t, "rate_limit_exceeded", be.Code)
}

// ─── Chat – happy path via httptest ────────────────────────────────────────

func TestBailian_Chat_Success(t *testing.T) {
	// Bailian returns OpenAI-compatible format.
	finishReason := "stop"
	response := model.ChatCompletionResponse{
		ID:     "chatcmpl-001",
		Object: "chat.completion",
		Model:  "qwen-turbo",
		Choices: []model.Choice{
			{
				Index:        0,
				Message:      &model.Message{Role: "assistant", Content: "Hello from Qwen"},
				FinishReason: &finishReason,
			},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		// Bailian passes BaseURL directly; OpenAICompatChat appends /chat/completions.
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer test-key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	result, err := a.Chat(context.Background(), basicChatReq())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Hello from Qwen", result.Choices[0].Message.Content)
	assert.Equal(t, 9, result.Usage.TotalTokens)
}

func TestBailian_Chat_401Unauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"Invalid API key","type":"auth_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	be, ok := err.(*BailianError)
	require.True(t, ok)
	assert.Equal(t, 401, be.HTTPStatus)
}

func TestBailian_Chat_429RateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"code":"rate_limit_exceeded","message":"Too many requests","type":"rate_limit_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	be, ok := err.(*BailianError)
	require.True(t, ok)
	assert.Equal(t, 429, be.HTTPStatus)
}

func TestBailian_Chat_500InternalError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"code":"server_error","message":"Internal error","type":"server_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	be, ok := err.(*BailianError)
	require.True(t, ok)
	assert.Equal(t, 500, be.HTTPStatus)
}

// ─── ChatStream – SSE parsing ──────────────────────────────────────────────

func TestBailian_ChatStream_Success(t *testing.T) {
	finishReason := "stop"
	chunk1 := model.ChatCompletionChunk{
		ID:    "chatcmpl-001",
		Model: "qwen-turbo",
		Choices: []model.Choice{
			{Index: 0, Delta: &model.Delta{Content: "Hello"}, FinishReason: nil},
		},
	}
	chunk2 := model.ChatCompletionChunk{
		ID:    "chatcmpl-001",
		Model: "qwen-turbo",
		Choices: []model.Choice{
			{Index: 0, Delta: &model.Delta{Content: ""}, FinishReason: &finishReason},
		},
	}
	c1, _ := json.Marshal(chunk1)
	c2, _ := json.Marshal(chunk2)

	sse := fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", c1, c2)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	ch, err := a.ChatStream(context.Background(), basicChatReq())
	require.NoError(t, err)

	var contents []string
	var doneReceived bool
	for sc := range ch {
		if sc.Error != nil {
			t.Fatalf("unexpected stream error: %v", sc.Error)
		}
		if sc.Done {
			doneReceived = true
			break
		}
		if sc.Data != nil {
			for _, choice := range sc.Data.Choices {
				if choice.Delta != nil && choice.Delta.Content != "" {
					contents = append(contents, choice.Delta.Content)
				}
			}
		}
	}
	assert.True(t, doneReceived)
	assert.Contains(t, contents, "Hello")
}
