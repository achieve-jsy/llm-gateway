package anthropic

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
		Models:  []string{"claude-3-opus-20240229", "claude-3-sonnet-20240229"},
	}).(*Adapter)
}

func basicChatReq() *model.ChatCompletionRequest {
	return &model.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
	}
}

// ─── Name ─────────────────────────────────────────────────────────────────

func TestAnthropic_Name(t *testing.T) {
	a := New(Config{APIKey: "k"})
	assert.Equal(t, "anthropic", a.Name())
}

// ─── New / defaults ────────────────────────────────────────────────────────

func TestAnthropic_New_SetsDefaultBaseURL(t *testing.T) {
	a := New(Config{APIKey: "k"}).(*Adapter)
	assert.Equal(t, defaultBaseURL, a.config.BaseURL)
}

func TestAnthropic_New_PreservesCustomBaseURL(t *testing.T) {
	a := New(Config{APIKey: "k", BaseURL: "https://custom.example.com"}).(*Adapter)
	assert.Equal(t, "https://custom.example.com", a.config.BaseURL)
}

// ─── convertRequest ────────────────────────────────────────────────────────

func TestAnthropic_ConvertRequest_BasicMapping(t *testing.T) {
	maxTok := 512
	req := &model.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []model.Message{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: &maxTok,
	}
	cr := convertRequest(req, false)
	assert.Equal(t, "claude-3-opus-20240229", cr.Model)
	assert.Equal(t, 512, cr.MaxTokens)
	assert.False(t, cr.Stream)
	require.Len(t, cr.Messages, 1)
	assert.Equal(t, "user", cr.Messages[0].Role)
	assert.Equal(t, "Hi", cr.Messages[0].Content)
}

func TestAnthropic_ConvertRequest_ExtractsSystemMessage(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "claude-3-opus-20240229",
		Messages: []model.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	}
	cr := convertRequest(req, false)
	assert.Equal(t, "You are helpful.", cr.System)
	require.Len(t, cr.Messages, 1)
	assert.Equal(t, "user", cr.Messages[0].Role)
}

func TestAnthropic_ConvertRequest_DefaultMaxTokens_WhenNil(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model:    "claude-3",
		Messages: []model.Message{{Role: "user", Content: "x"}},
	}
	cr := convertRequest(req, false)
	assert.Equal(t, defaultMaxTokens, cr.MaxTokens)
}

func TestAnthropic_ConvertRequest_StreamFlag(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model:    "claude-3",
		Messages: []model.Message{{Role: "user", Content: "x"}},
	}
	cr := convertRequest(req, true)
	assert.True(t, cr.Stream)
}

// ─── convertResponse ────────────────────────────────────────────────────────

func TestAnthropic_ConvertResponse_TextBlock(t *testing.T) {
	stopReason := "end_turn"
	resp := &claudeResponse{
		ID:         "msg_01XFDUDYJgAACzvnptvVoYEL",
		Type:       "message",
		Role:       "assistant",
		Content:    []claudeBlock{{Type: "text", Text: "Hello world"}},
		Model:      "claude-3-opus-20240229",
		StopReason: &stopReason,
		Usage:      &claudeUsage{InputTokens: 10, OutputTokens: 5},
	}
	result := convertResponse(resp, "claude-3-opus-20240229")

	assert.Equal(t, "chat.completion", result.Object)
	assert.Contains(t, result.ID, "chatcmpl-")
	require.Len(t, result.Choices, 1)
	assert.Equal(t, "Hello world", result.Choices[0].Message.Content)
	assert.Equal(t, "assistant", result.Choices[0].Message.Role)
	assert.NotNil(t, result.Choices[0].FinishReason)
	assert.Equal(t, "stop", *result.Choices[0].FinishReason)
	require.NotNil(t, result.Usage)
	assert.Equal(t, 10, result.Usage.PromptTokens)
	assert.Equal(t, 5, result.Usage.CompletionTokens)
	assert.Equal(t, 15, result.Usage.TotalTokens)
}

func TestAnthropic_ConvertResponse_ThinkingBlock(t *testing.T) {
	thinking := "let me think..."
	resp := &claudeResponse{
		ID:   "msg_123",
		Type: "message",
		Content: []claudeBlock{
			{Type: "thinking", Thinking: &thinking},
			{Type: "text", Text: "Answer"},
		},
	}
	result := convertResponse(resp, "claude-3-opus")
	require.Len(t, result.Choices, 1)
	assert.Equal(t, "Answer", result.Choices[0].Message.Content)
	assert.Equal(t, "let me think...", result.Choices[0].Message.ReasoningContent)
}

// ─── stopReasonToOpenAI ────────────────────────────────────────────────────

func TestAnthropic_StopReasonToOpenAI(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"unknown_reason", "unknown_reason"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.out, stopReasonToOpenAI(c.in))
		})
	}
}

// ─── ListModels ────────────────────────────────────────────────────────────

func TestAnthropic_ListModels_ReturnsConfiguredModels(t *testing.T) {
	a := New(Config{
		APIKey:  "k",
		Models:  []string{"claude-3-opus-20240229", "claude-3-sonnet"},
	})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "claude-3-opus-20240229", models[0].ID)
	assert.Equal(t, "anthropic", models[0].OwnedBy)
	assert.Equal(t, "model", models[0].Object)
}

func TestAnthropic_ListModels_EmptyConfig(t *testing.T) {
	a := New(Config{APIKey: "k"})
	models, err := a.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

// ─── Chat – happy path via httptest ────────────────────────────────────────

func TestAnthropic_Chat_Success(t *testing.T) {
	stopReason := "end_turn"
	response := claudeResponse{
		ID:         "msg_01",
		Type:       "message",
		Role:       "assistant",
		Content:    []claudeBlock{{Type: "text", Text: "Hello from Claude"}},
		Model:      "claude-3-opus-20240229",
		StopReason: &stopReason,
		Usage:      &claudeUsage{InputTokens: 8, OutputTokens: 5},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/messages", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, anthropicVersion, r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	result, err := a.Chat(context.Background(), basicChatReq())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Hello from Claude", result.Choices[0].Message.Content)
	assert.Equal(t, "claude-3-opus-20240229", result.Model)
	assert.Equal(t, 13, result.Usage.TotalTokens)
}

// ─── Chat – error paths ────────────────────────────────────────────────────

func TestAnthropic_Chat_401Unauthorized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":{"type":"authentication_error","message":"Invalid API key"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	result, err := a.Chat(context.Background(), basicChatReq())
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestAnthropic_Chat_429RateLimited(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, `{"error":{"type":"rate_limit_error","message":"Too many requests"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestAnthropic_Chat_500InternalServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":{"type":"api_error","message":"Internal server error"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	_, err := a.Chat(context.Background(), basicChatReq())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// ─── ChatStream – happy path via httptest ──────────────────────────────────

func TestAnthropic_ChatStream_Success(t *testing.T) {
	// Minimal SSE sequence: message_start → content_block_start →
	// content_block_delta (text) → content_block_stop → message_delta →
	// message_stop
	sse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-3-opus-20240229","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	ch, err := a.ChatStream(context.Background(), basicChatReq())
	require.NoError(t, err)

	var contentChunks []string
	var doneReceived bool
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}
		if chunk.Done {
			doneReceived = true
			break
		}
		if chunk.Data != nil {
			for _, choice := range chunk.Data.Choices {
				if choice.Delta != nil && choice.Delta.Content != "" {
					contentChunks = append(contentChunks, choice.Delta.Content)
				}
			}
		}
	}
	assert.True(t, doneReceived, "should receive Done signal")
	assert.Contains(t, contentChunks, "Hello")
}

func TestAnthropic_ChatStream_ErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"type":"authentication_error","message":"bad key"}}`)
	}))
	defer ts.Close()

	a := newTestAdapter(ts.URL)
	ch, err := a.ChatStream(context.Background(), basicChatReq())
	// For non-2xx, err is returned immediately (no channel).
	if err != nil {
		assert.Contains(t, err.Error(), "401")
		return
	}
	// If channel returned, it should send an error chunk.
	for chunk := range ch {
		if chunk.Error != nil {
			assert.Contains(t, chunk.Error.Error(), "401")
			return
		}
	}
}

// ─── setHeaders ────────────────────────────────────────────────────────────

func TestAnthropic_SetHeaders_ContainsRequiredFields(t *testing.T) {
	a := newTestAdapter("https://api.anthropic.com/v1")
	req, _ := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	a.setHeaders(req)

	assert.Equal(t, "test-key", req.Header.Get("x-api-key"))
	assert.Equal(t, anthropicVersion, req.Header.Get("anthropic-version"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}
