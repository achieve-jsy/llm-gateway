package deepseek_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/deepseek"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeProvider 创建一个指向 fakeServer 的 DeepSeek Provider，用于单元测试
func newFakeProvider(t *testing.T, serverURL string) core.Provider {
	t.Helper()
	return deepseek.New(providers.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: serverURL,
	}, providers.ProviderOptions{})
}

// chatCompletionResponseBody 返回合法的 ChatCompletion 响应
func chatCompletionResponseBody() map[string]any {
	return map[string]any{
		"id":      "chatcmpl-deepseek-test",
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

func chatRequestFromJSON(t *testing.T, raw string) *core.ChatRequest {
	t.Helper()
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal([]byte(raw), &req))
	return &req
}

func TestChatCompletion_JSONSchema降级为JSONObject(t *testing.T) {
	tests := []struct {
		name       string
		reqJSON    string
		wantFormat string // 期望 response_format.type;空表示不应出现 response_format
	}{
		{
			name:       "json_schema strict 降级",
			reqJSON:    `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","strict":true,"json_schema":{"name":"review","schema":{}}}}`,
			wantFormat: "json_object",
		},
		{
			name:       "json_object 原样透传",
			reqJSON:    `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"}}`,
			wantFormat: "json_object",
		},
		{
			name:       "text 原样透传",
			reqJSON:    `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"text"}}`,
			wantFormat: "text",
		},
		{
			name:       "无 response_format 不添加",
			reqJSON:    `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`,
			wantFormat: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]any
			fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				require.NoError(t, json.NewEncoder(w).Encode(chatCompletionResponseBody()))
			}))
			defer fakeServer.Close()

			p := newFakeProvider(t, fakeServer.URL)
			resp, err := p.ChatCompletion(context.Background(), chatRequestFromJSON(t, tt.reqJSON))
			require.NoError(t, err)
			require.NotNil(t, resp)

			if tt.wantFormat == "" {
				assert.NotContains(t, captured, "response_format")
				return
			}
			rf, ok := captured["response_format"].(map[string]any)
			require.True(t, ok, "response_format 应出现在上游 body 中")
			assert.Equal(t, tt.wantFormat, rf["type"])
			assert.NotContains(t, rf, "json_schema")
			assert.NotContains(t, rf, "strict")
		})
	}
}

func TestChatCompletion_Reasoning扁平化与JSONSchema降级(t *testing.T) {
	var captured map[string]any
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		require.NoError(t, json.NewEncoder(w).Encode(chatCompletionResponseBody()))
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	resp, err := p.ChatCompletion(context.Background(), chatRequestFromJSON(t,
		`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"reasoning":{"effort":"high"},"response_format":{"type":"json_schema","strict":true,"json_schema":{}}}`))
	require.NoError(t, err)
	require.NotNil(t, resp)

	// reasoning 仍按既有逻辑扁平化为顶层 reasoning_effort
	assert.NotContains(t, captured, "reasoning")
	assert.Equal(t, "high", captured["reasoning_effort"])
	// response_format 同步降级
	rf, ok := captured["response_format"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "json_object", rf["type"])
	assert.NotContains(t, rf, "json_schema")
}

func TestStreamChatCompletion_JSONSchema降级(t *testing.T) {
	var captured map[string]any
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-chat\",\"choices\":[]}\n\n")
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	stream, err := p.StreamChatCompletion(context.Background(), chatRequestFromJSON(t,
		`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","strict":true,"json_schema":{}}}`))
	require.NoError(t, err)
	require.NotNil(t, stream)
	defer stream.Close()
	_, err = io.ReadAll(stream)
	require.NoError(t, err)

	rf, ok := captured["response_format"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "json_object", rf["type"])
	assert.NotContains(t, rf, "json_schema")
}
