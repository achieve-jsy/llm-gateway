package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/require"
)

func TestCompatibilityGolden_OpenAIReasoningChatPayload(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-golden",
			"object":  "chat.completion",
			"model":   "gpt-5.1",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
		})
	}))
	defer fakeServer.Close()

	raw := []byte(`{
		"model":"gpt-5.1",
		"messages":[{"role":"user","content":"hi"}],
		"max_tokens":16,
		"temperature":0.2,
		"reasoningSummary":"auto",
		"reasoningEffort":{"effort":"medium","summary":"detailed"},
		"reasoning":{"effort":"high"},
		"custom_passthrough":"keep"
	}`)
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal(raw, &req))

	p := newFakeProvider(t, fakeServer.URL)
	_, err := p.ChatCompletion(context.Background(), &req)
	require.NoError(t, err)

	require.Equal(t, "/responses", gotPath)
	assertGoldenMap(t, map[string]any{
		"input": []any{
			map[string]any{
				"content": []any{map[string]any{"text": "hi", "type": "input_text"}},
				"role":    "user",
				"type":    "message",
			},
		},
		"max_output_tokens": float64(16),
		"model":             "gpt-5.1",
		"reasoning": map[string]any{
			"effort":  "high",
			"summary": "detailed",
		},
		"temperature": float64(0.2),
	}, gotBody)
}

func TestCompatibilityGolden_OpenAIReasoningChatNoSummaryPayload(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-golden-chat",
			"object":  "chat.completion",
			"model":   "gpt-5.1",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
		})
	}))
	defer fakeServer.Close()

	raw := []byte(`{
		"model":"gpt-5.1",
		"messages":[{"role":"user","content":"hi"}],
		"max_tokens":16,
		"temperature":0.2,
		"reasoningEffort":"medium",
		"reasoning":{"effort":"high"},
		"custom_passthrough":"keep"
	}`)
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal(raw, &req))

	p := newFakeProvider(t, fakeServer.URL)
	_, err := p.ChatCompletion(context.Background(), &req)
	require.NoError(t, err)

	require.Equal(t, "/chat/completions", gotPath)
	assertGoldenMap(t, map[string]any{
		"custom_passthrough":    "keep",
		"max_completion_tokens": float64(16),
		"messages": []any{
			map[string]any{"content": "hi", "role": "user"},
		},
		"model":            "gpt-5.1",
		"reasoning_effort": "medium",
	}, gotBody)
}

func assertGoldenMap(t *testing.T, want, got map[string]any) {
	t.Helper()
	wantJSON, err := json.Marshal(want)
	require.NoError(t, err)
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(wantJSON), string(gotJSON))
}
