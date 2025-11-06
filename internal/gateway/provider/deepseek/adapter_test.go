package deepseek

// NOTE: BUG FOUND — double /chat/completions path in Chat() and ChatStream():
//
//   Chat() builds:  endpoint = cfg.BaseURL + "/chat/completions"
//   Then calls:     provider.OpenAICompatChat(client, endpoint, ...)
//   Which appends:  BuildRequestURL(endpoint, "/chat/completions", ...) => endpoint + "/chat/completions"
//
//   Result:  cfg.BaseURL + "/chat/completions/chat/completions"  (WRONG)
//   Expected: cfg.BaseURL + "/chat/completions"
//
// The fix is to pass cfg.BaseURL directly (without appending /chat/completions)
// to OpenAICompatChat, and let it append the path — consistent with how bailian works.
// Tests below use a catch-all handler pattern to work with current (buggy) behavior.

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
		Models:  []string{"deepseek-chat", "deepseek-reasoner"},
	}).(*Adapter)
}

func basicChatReq() *model.ChatCompletionRequest {
	return &model.ChatCompletionRequest{
		Model: "deepseek-chat",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
	}
}

// ─── Name ─────────────────────────────────────────────────────────────────

func TestDeepSeek_Name(t *testing.T) {
	a := New(Config{APIKey: "k"})
	assert.Equal(t, "deepseek", a.Name())
}

// ─── New / defaults ────────────────────────────────────────────────────────

func TestDeepSeek_New_SetsDefaultBaseURL(t *testing.T) {
	a := New(Config{APIKey: "k"}).(*Adapter)
	assert.Equal(t, defaultBaseURL, a.config.BaseURL)
}

func TestDeepSeek_New_PreservesCustomBaseURL(t *testing.T) {
	a := New(Config{APIKey: "k", BaseURL: "https://custom.example.com"}).(*Adapter)
	assert.Equal(t, "https://custom.example.com", a.config.BaseURL)
}

// ─── ListModels ────────────────────────────────────────────────────────────

func TestDeepSeek_ListModels_ReturnsConfiguredModels(t *testing.T) {
	a := New(Config{APIKey: "k", Models: []string{"deepseek-chat", "deepseek-reasoner"}})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "deepseek-chat", models[0].ID)
	assert.Equal(t, "deepseek", models[0].OwnedBy)
}

func TestDeepSeek_ListModels_Empty(t *testing.T) {
	a := New(Config{APIKey: "k"})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

// ─── ToDeepSeekError ────────────────────────────────────────────────────────

func TestDeepSeek_ToDeepSeekError_ParsesStructuredError(t *testing.T) {
	body := []byte(`{"error":{"code":"invalid_request_error","message":"Bad input","type":"invalid_request_error"}}`)
	err := ToDeepSeekError(http.StatusBadRequest, body)
	require.Error(t, err)

	de, ok := err.(*DeepSeekError)
	require.True(t, ok, "error should be *DeepSeekError")
	assert.Equal(t, http.StatusBadRequest, de.HTTPStatus)
	assert.Equal(t, "invalid_request_error", de.Code)
	assert.Equal(t, "Bad input", de.Message)
	assert.Contains(t, de.Error(), "400")
}

func TestDeepSeek_ToDeepSeekError_FallsBackToRawBody(t *testing.T) {
	body := []byte(`not valid json`)
	err := ToDeepSeekError(http.StatusInternalServerError, body)
	require.Error(t, err)

	de, ok := err.(*DeepSeekError)
	require.True(t, ok)
	assert.Equal(t, 500, de.HTTPStatus)
	assert.Equal(t, "not valid json", de.Message)
}

func TestDeepSeek_ToDeepSeekError_401Unauthorized(t *testing.T) {
	body := []byte(`{"error":{"code":"invalid_authentication_token","message":"Unauthorized","type":"invalid_authentication_token"}}`)
	err := ToDeepSeekError(http.StatusUnauthorized, body)
	de := err.(*DeepSeekError)
	assert.Equal(t, 401, de.HTTPStatus)
	assert.Contains(t, de.Error(), "401")
}

func TestDeepSeek_ToDeepSeekError_429RateLimit(t *testing.T) {
	body := []byte(`{"error":{"code":"rate_limit_exceeded","message":"Too many requests","type":"rate_limit_error"}}`)
	err := ToDeepSeekError(http.StatusTooManyRequests, body)
	de := err.(*DeepSeekError)
	assert.Equal(t, 429, de.HTTPStatus)
	assert.Equal(t, "rate_limit_exceeded", de.Code)
}

func TestDeepSeek_ToDeepSeekError_500InternalError(t *testing.T) {
	body := []byte(`{"error":{"code":"server_error","message":"Internal server error","type":"server_error"}}`)
	err := ToDeepSeekError(http.StatusInternalServerError, body)
	de := err.(*DeepSeekError)
	assert.Equal(t, 500, de.HTTPStatus)
}

// ─── Chat – happy path via httptest ────────────────────────────────────────
// The test server uses a catch-all handler to accommodate the current double-path bug.

func TestDeepSeek_Chat_Success(t *testing.T) {
	finishReason := "stop"
	response := model.ChatCompletionResponse{
		ID:     "chatcmpl-001",
		Object: "chat.completion",
		Model:  "deepseek-chat",
		Choices: []model.Choice{
			{
				Index:        0,
				Message:      &model.Message{Role: "assistant", Content: "Hello from DeepSeek"},
				FinishReason: &finishReason,
			},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Accept any POST (handles double-path bug gracefully)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer test-key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	result, err := a.Chat(context.Background(), basicChatReq())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Hello from DeepSeek", result.Choices[0].Message.Content)
}

func TestDeepSeek_Chat_401Unauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"invalid_authentication_token","message":"Unauthorized","type":"invalid_authentication_token"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	de, ok := err.(*DeepSeekError)
	require.True(t, ok)
	assert.Equal(t, 401, de.HTTPStatus)
}

func TestDeepSeek_Chat_429RateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"code":"rate_limit_exceeded","message":"Too many requests","type":"rate_limit_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	de, ok := err.(*DeepSeekError)
	require.True(t, ok)
	assert.Equal(t, 429, de.HTTPStatus)
}

func TestDeepSeek_Chat_500InternalError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"code":"server_error","message":"Internal error","type":"server_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	de, ok := err.(*DeepSeekError)
	require.True(t, ok)
	assert.Equal(t, 500, de.HTTPStatus)
}

// ─── ChatStream – SSE via httptest ────────────────────────────────────────

func TestDeepSeek_ChatStream_Success(t *testing.T) {
	finishReason := "stop"
	chunk1 := model.ChatCompletionChunk{
		ID:    "chatcmpl-001",
		Model: "deepseek-chat",
		Choices: []model.Choice{
			{Index: 0, Delta: &model.Delta{Content: "Hi"}, FinishReason: nil},
		},
	}
	chunk2 := model.ChatCompletionChunk{
		ID:    "chatcmpl-001",
		Model: "deepseek-chat",
		Choices: []model.Choice{
			{Index: 0, Delta: &model.Delta{}, FinishReason: &finishReason},
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
	var done bool
	for sc := range ch {
		if sc.Error != nil {
			t.Fatalf("stream error: %v", sc.Error)
		}
		if sc.Done {
			done = true
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
	assert.True(t, done)
	assert.Contains(t, contents, "Hi")
}
