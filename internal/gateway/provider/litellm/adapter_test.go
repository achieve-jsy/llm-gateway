package litellm

// NOTE: BUG FOUND — double path in Chat() and ChatStream():
//
//   Chat() builds:  endpoint = cfg.Endpoint + "/v1/chat/completions"
//   Then calls:     provider.OpenAICompatChat(client, endpoint, ...)
//   Which appends:  BuildRequestURL(endpoint, "/chat/completions", ...) => endpoint + "/chat/completions"
//
//   Result:  cfg.Endpoint + "/v1/chat/completions/chat/completions"  (WRONG)
//   Expected: cfg.Endpoint + "/v1/chat/completions"
//
// The fix is to pass cfg.Endpoint (or cfg.Endpoint+"/v1") to OpenAICompatChat
// and let it construct the full path.
// Tests use catch-all handlers to work with current (buggy) behavior.

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

func newTestAdapter(endpoint string) *Adapter {
	return New(Config{
		Endpoint: endpoint,
		APIKey:   "test-key",
		Models:   []string{"gpt-4", "gpt-3.5-turbo"},
	}).(*Adapter)
}

func basicChatReq() *model.ChatCompletionRequest {
	return &model.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
	}
}

// ─── Name ─────────────────────────────────────────────────────────────────

func TestLiteLLM_Name(t *testing.T) {
	a := New(Config{Endpoint: "http://localhost:4000"})
	assert.Equal(t, "litellm", a.Name())
}

// ─── New / URL trimming ────────────────────────────────────────────────────

func TestLiteLLM_New_TrimsTrailingSlash(t *testing.T) {
	a := New(Config{Endpoint: "http://localhost:4000/"}).(*Adapter)
	assert.Equal(t, "http://localhost:4000", a.config.Endpoint)
}

func TestLiteLLM_New_PreservesEndpoint(t *testing.T) {
	a := New(Config{Endpoint: "http://localhost:4000"}).(*Adapter)
	assert.Equal(t, "http://localhost:4000", a.config.Endpoint)
}

// ─── ListModels ────────────────────────────────────────────────────────────

func TestLiteLLM_ListModels_ReturnsConfiguredModels(t *testing.T) {
	a := New(Config{Endpoint: "http://localhost:4000", Models: []string{"gpt-4", "gpt-3.5-turbo"}})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "gpt-4", models[0].ID)
	assert.Equal(t, "litellm", models[0].OwnedBy)
}

func TestLiteLLM_ListModels_Empty(t *testing.T) {
	a := New(Config{Endpoint: "http://localhost:4000"})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

// ─── ToLiteLLMError ────────────────────────────────────────────────────────

func TestLiteLLM_ToLiteLLMError_ParsesStructuredError(t *testing.T) {
	body := []byte(`{"error":{"code":"invalid_request_error","message":"Bad request","type":"invalid_request_error"}}`)
	err := ToLiteLLMError(http.StatusBadRequest, body)
	require.Error(t, err)

	le, ok := err.(*LiteLLMError)
	require.True(t, ok, "error should be *LiteLLMError")
	assert.Equal(t, http.StatusBadRequest, le.HTTPStatus)
	assert.Equal(t, "invalid_request_error", le.Code)
	assert.Equal(t, "Bad request", le.Message)
	assert.Contains(t, le.Error(), "400")
}

func TestLiteLLM_ToLiteLLMError_FallsBackToRawBody(t *testing.T) {
	body := []byte(`plain error text`)
	err := ToLiteLLMError(http.StatusServiceUnavailable, body)
	le := err.(*LiteLLMError)
	assert.Equal(t, 503, le.HTTPStatus)
	assert.Equal(t, "plain error text", le.Message)
}

func TestLiteLLM_ToLiteLLMError_401Unauthorized(t *testing.T) {
	body := []byte(`{"error":{"code":"unauthorized","message":"Invalid key","type":"auth_error"}}`)
	err := ToLiteLLMError(http.StatusUnauthorized, body)
	le := err.(*LiteLLMError)
	assert.Equal(t, 401, le.HTTPStatus)
	assert.Contains(t, le.Error(), "401")
}

func TestLiteLLM_ToLiteLLMError_429RateLimit(t *testing.T) {
	body := []byte(`{"error":{"code":"rate_limit","message":"Too many requests","type":"rate_limit_error"}}`)
	err := ToLiteLLMError(http.StatusTooManyRequests, body)
	le := err.(*LiteLLMError)
	assert.Equal(t, 429, le.HTTPStatus)
}

// ─── Chat – happy path via httptest ────────────────────────────────────────

func TestLiteLLM_Chat_Success(t *testing.T) {
	finishReason := "stop"
	response := model.ChatCompletionResponse{
		ID:     "chatcmpl-001",
		Object: "chat.completion",
		Model:  "gpt-4",
		Choices: []model.Choice{
			{
				Index:        0,
				Message:      &model.Message{Role: "assistant", Content: "Hello from LiteLLM"},
				FinishReason: &finishReason,
			},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 6, TotalTokens: 11},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Catch-all: accepts any POST regardless of path (handles double-path bug).
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
	assert.Equal(t, "Hello from LiteLLM", result.Choices[0].Message.Content)
	assert.Equal(t, 11, result.Usage.TotalTokens)
}

func TestLiteLLM_Chat_401Unauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"code":"unauthorized","message":"Invalid key","type":"auth_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	le, ok := err.(*LiteLLMError)
	require.True(t, ok)
	assert.Equal(t, 401, le.HTTPStatus)
}

func TestLiteLLM_Chat_429RateLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"code":"rate_limit","message":"Too many requests","type":"rate_limit_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	le, ok := err.(*LiteLLMError)
	require.True(t, ok)
	assert.Equal(t, 429, le.HTTPStatus)
}

func TestLiteLLM_Chat_500InternalError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"code":"server_error","message":"Internal error","type":"server_error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	le, ok := err.(*LiteLLMError)
	require.True(t, ok)
	assert.Equal(t, 500, le.HTTPStatus)
}

// ─── ChatStream – SSE via httptest ────────────────────────────────────────

func TestLiteLLM_ChatStream_Success(t *testing.T) {
	finishReason := "stop"
	chunk := model.ChatCompletionChunk{
		ID:    "chatcmpl-001",
		Model: "gpt-4",
		Choices: []model.Choice{
			{Index: 0, Delta: &model.Delta{Content: "Hi there"}, FinishReason: nil},
		},
	}
	doneChunk := model.ChatCompletionChunk{
		ID:    "chatcmpl-001",
		Model: "gpt-4",
		Choices: []model.Choice{
			{Index: 0, Delta: &model.Delta{}, FinishReason: &finishReason},
		},
	}
	c1, _ := json.Marshal(chunk)
	c2, _ := json.Marshal(doneChunk)
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
	assert.Contains(t, contents, "Hi there")
}

// ─── HealthCheck ────────────────────────────────────────────────────────────

func TestLiteLLM_HealthCheck_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	err := a.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestLiteLLM_HealthCheck_5xxFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	err := a.HealthCheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}
