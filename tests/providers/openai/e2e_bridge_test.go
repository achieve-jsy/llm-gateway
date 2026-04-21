package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// E2E（假上游）：覆盖 Chat→Responses 桥接路径选择、请求形态、响应回转与流式。
func TestE2E_ChatViaResponses_PathAndRoundTrip(t *testing.T) {
	var chatHits, responsesHits atomic.Int32
	var lastResponsesBody map[string]any

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			chatHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-direct", "object": "chat.completion", "model": "gpt-5.5",
				"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "direct"}, "finish_reason": "stop"}},
			})
		case "/responses":
			responsesHits.Add(1)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&lastResponsesBody))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "resp_e2e", "object": "response", "model": "gpt-5.5", "status": "completed", "created_at": 1700000001,
				"output": []map[string]any{
					{"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
						"content": []map[string]any{{"type": "output_text", "text": "bridged-ok"}}},
					{"id": "fc_1", "type": "function_call", "status": "completed",
						"call_id": "call_e2e", "name": "get_weather", "arguments": `{"city":"SF"}`},
				},
				"usage": map[string]any{"input_tokens": 11, "output_tokens": 7, "total_tokens": 18},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)

	t.Run("bridge_when_tools_and_reasoning", func(t *testing.T) {
		chatHits.Store(0)
		responsesHits.Store(0)
		raw := []byte(`{
			"model":"gpt-5.5",
			"messages":[{"role":"system","content":"sys"},{"role":"user","content":"weather?"}],
			"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],
			"reasoning_effort":"medium",
			"reasoningSummary":"auto",
			"max_tokens":32
		}`)
		var req core.ChatRequest
		require.NoError(t, json.Unmarshal(raw, &req))
		resp, err := p.ChatCompletion(context.Background(), &req)
		require.NoError(t, err)
		assert.Equal(t, int32(0), chatHits.Load())
		assert.Equal(t, int32(1), responsesHits.Load())
		assert.Equal(t, "sys", lastResponsesBody["instructions"])
		assert.Equal(t, float64(32), lastResponsesBody["max_output_tokens"])
		reasoning, _ := lastResponsesBody["reasoning"].(map[string]any)
		require.NotNil(t, reasoning)
		assert.Equal(t, "medium", reasoning["effort"])
		assert.Equal(t, "auto", reasoning["summary"])
		assert.Equal(t, "chat.completion", resp.Object)
		assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
		require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
		assert.Equal(t, "get_weather", resp.Choices[0].Message.ToolCalls[0].Function.Name)
		assert.Contains(t, core.ExtractTextContent(resp.Choices[0].Message.Content), "bridged-ok")
		assert.Equal(t, 11, resp.Usage.PromptTokens)
	})

	t.Run("no_bridge_without_reasoning", func(t *testing.T) {
		chatHits.Store(0)
		responsesHits.Store(0)
		req := &core.ChatRequest{
			Model:    "gpt-5.5",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
			Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "ping"}}},
		}
		resp, err := p.ChatCompletion(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, int32(1), chatHits.Load())
		assert.Equal(t, int32(0), responsesHits.Load())
		assert.Equal(t, "direct", resp.Choices[0].Message.Content)
	})
}

func TestE2E_StreamChatViaResponses_FakeUpstream(t *testing.T) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/responses", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		events := []string{
			`data: {"type":"response.output_text.delta","delta":"Hi"}`,
			`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"ping"}}`,
			`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{}"}`,
			`data: {"type":"response.completed","response":{"id":"resp_s","model":"gpt-5.5","output":[{"type":"function_call"}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		}
		for _, ev := range events {
			_, _ = io.WriteString(w, ev+"\n\n")
			flusher.Flush()
		}
	}))
	defer fakeServer.Close()

	p := newFakeProvider(t, fakeServer.URL)
	req := &core.ChatRequest{
		Model: "gpt-5.5",
		Messages: []core.Message{
			{Role: "user", Content: "hi"},
		},
		Tools: []map[string]any{{
			"type": "function", "function": map[string]any{"name": "ping"},
		}},
		Reasoning:     &core.Reasoning{Effort: "low"},
		Stream:        true,
		StreamOptions: &core.StreamOptions{IncludeUsage: true},
	}
	require.True(t, openai.ShouldBridgeChatToResponses(req))

	stream, err := p.StreamChatCompletion(context.Background(), req)
	require.NoError(t, err)
	defer stream.Close()
	body, err := io.ReadAll(stream)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, `"object":"chat.completion.chunk"`)
	assert.Contains(t, text, `"content":"Hi"`)
	assert.Contains(t, text, "tool_calls")
	assert.Contains(t, text, `"finish_reason":"tool_calls"`)
	assert.Contains(t, text, `"prompt_tokens":2`)
	assert.True(t, strings.Contains(text, "data: [DONE]"))
}
