package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/litellm"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chatRequestFromJSON(t *testing.T, raw string) *core.ChatRequest {
	t.Helper()
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal([]byte(raw), &req))
	return &req
}

func chatResponseBody() map[string]any {
	return map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"model":   "deepseek-chat",
		"created": 1700000000,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": `{"ok":true}`,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
}

func captureChatServer(t *testing.T, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(captured))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		require.NoError(t, json.NewEncoder(w).Encode(chatResponseBody()))
	}))
}

func responseFormatType(t *testing.T, captured map[string]any) string {
	t.Helper()
	raw, ok := captured["response_format"]
	if !ok {
		return ""
	}
	rf, ok := raw.(map[string]any)
	require.True(t, ok, "response_format 应为对象")
	return rf["type"].(string)
}

func TestLiteLLMProvider_DeepSeekModel降级(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		model      string
		wantFormat string // 空表示原样保留 json_schema
	}{
		{
			name:       "litellm + deepseek-chat 降级",
			provider:   "litellm",
			model:      "deepseek-chat",
			wantFormat: "json_object",
		},
		{
			name:       "litellm + openrouter/deepseek-reasoner 降级",
			provider:   "litellm",
			model:      "openrouter/deepseek-reasoner",
			wantFormat: "json_object",
		},
		{
			name:       "litellm + gpt-4 保留作用域",
			provider:   "litellm",
			model:      "gpt-4",
			wantFormat: "json_schema",
		},
		{
			name:       "openai provider + deepseek-chat 保留作用域",
			provider:   "openai",
			model:      "deepseek-chat",
			wantFormat: "json_schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]any
			fakeServer := captureChatServer(t, &captured)
			defer fakeServer.Close()

			var p core.Provider
			cfg := providers.ProviderConfig{APIKey: "test-key", BaseURL: fakeServer.URL}
			if tt.provider == "litellm" {
				p = litellm.New(cfg, providers.ProviderOptions{})
			} else {
				p = openai.New(cfg, providers.ProviderOptions{})
			}

			resp, err := p.ChatCompletion(context.Background(), chatRequestFromJSON(t,
				`{"model":"`+tt.model+`","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","strict":true,"json_schema":{"name":"review","schema":{}}}}`))
			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, tt.wantFormat, responseFormatType(t, captured))
			if tt.wantFormat == "json_object" {
				rf := captured["response_format"].(map[string]any)
				assert.NotContains(t, rf, "json_schema")
				assert.NotContains(t, rf, "strict")
			}
		})
	}
}

func TestLiteLLMProvider_StreamDeepSeek模型降级(t *testing.T) {
	var captured map[string]any
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-chat\",\"choices\":[]}\n\n")
	}))
	defer fakeServer.Close()

	p := litellm.New(providers.ProviderConfig{APIKey: "test-key", BaseURL: fakeServer.URL}, providers.ProviderOptions{})
	stream, err := p.StreamChatCompletion(context.Background(), chatRequestFromJSON(t,
		`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","strict":true,"json_schema":{}}}`))
	require.NoError(t, err)
	require.NotNil(t, stream)
	defer stream.Close()
	_, err = io.ReadAll(stream)
	require.NoError(t, err)

	assert.Equal(t, "json_object", responseFormatType(t, captured))
	rf := captured["response_format"].(map[string]any)
	assert.NotContains(t, rf, "json_schema")
}
