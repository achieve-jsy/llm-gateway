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
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

// TestOpenAICompatVideoPassthrough 验收：网关把 video_url 内容片段原样透传给
// OpenAI 兼容上游，字段落位与数值不被改写（fps 必须留在 video_url 对象内）。
func TestOpenAICompatVideoPassthrough(t *testing.T) {
	var captured []byte
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		captured = body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-video","object":"chat.completion","model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer fakeServer.Close()

	provider := openai.New(providers.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: fakeServer.URL,
	}, providers.ProviderOptions{})

	req := &core.ChatRequest{
		Model: "gpt-4o",
		Messages: []core.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "what happens in this video?"},
				map[string]any{"type": "video_url", "video_url": map[string]any{
					"url": "https://a/b.mp4",
					"fps": 2,
				}},
			},
		}},
	}

	_, err := provider.ChatCompletion(context.Background(), req)
	require.NoError(t, err)
	require.NotEmpty(t, captured, "upstream should receive a body")

	// 逐字节比对关键片段，防止 ExtraFields 或 fps 落位漂移
	var payload struct {
		Messages []struct {
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(captured, &payload))
	require.Len(t, payload.Messages, 1)
	require.Len(t, payload.Messages[0].Content, 2)

	assertJSONEqual(t, `{"type":"text","text":"what happens in this video?"}`, payload.Messages[0].Content[0])
	assertJSONEqual(t, `{"type":"video_url","video_url":{"url":"https://a/b.mp4","fps":2}}`, payload.Messages[0].Content[1])
}

// assertJSONEqual 比较两段 JSON 的语义等价性（键顺序无关，缺失/多余键会失败）。
func assertJSONEqual(t *testing.T, want string, got json.RawMessage) {
	t.Helper()
	var wantAny, gotAny any
	require.NoError(t, json.Unmarshal([]byte(want), &wantAny))
	require.NoError(t, json.Unmarshal(got, &gotAny))
	require.Equal(t, wantAny, gotAny, "mismatch: want %s, got %s", want, string(got))
}
