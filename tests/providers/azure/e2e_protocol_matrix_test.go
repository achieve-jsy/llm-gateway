package azure_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/azure"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type azurePathCounter struct {
	chat, responses atomic.Int32
	lastPath        atomic.Value
	lastBody        atomic.Value
}

func newAzureMatrixServer(t *testing.T) (*httptest.Server, *azurePathCounter) {
	t.Helper()
	c := &azurePathCounter{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.lastPath.Store(r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		c.lastBody.Store(body)
		assert.Equal(t, "test-key", r.Header.Get("api-key"))

		switch r.URL.Path {
		case "/openai/v1/chat/completions":
			c.chat.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-az", "object": "chat.completion", "model": "gpt-5.5",
				"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			})
		case "/openai/v1/responses":
			c.responses.Add(1)
			if body["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"az\"}\n\n")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_az\",\"model\":\"gpt-5.5\",\"output\":[{\"type\":\"message\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "resp_az", "object": "response", "model": "gpt-5.5", "status": "completed",
				"output": []map[string]any{
					{"type": "reasoning", "summary": []map[string]any{{"type": "summary_text", "text": "az-reason"}}},
					{"type": "function_call", "call_id": "call_az", "name": "get_weather", "arguments": `{}`},
				},
				"usage": map[string]any{"input_tokens": 5, "output_tokens": 3, "total_tokens": 8},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func newAzureProvider(t *testing.T, base string) core.Provider {
	t.Helper()
	return azure.New(providers.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: base,
	}, providers.ProviderOptions{})
}

func TestE2E_AzureProtocolMatrix_AllPathways(t *testing.T) {
	srv, ctr := newAzureMatrixServer(t)
	p := newAzureProvider(t, srv.URL)

	t.Run("plain_chat_openai_v1", func(t *testing.T) {
		ctr.chat.Store(0)
		ctr.responses.Store(0)
		maxTok := 16
		_, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
			Model:     "gpt-5.5",
			Messages:  []core.Message{{Role: "user", Content: "hi"}},
			MaxTokens: &maxTok,
		})
		require.NoError(t, err)
		assert.Equal(t, int32(1), ctr.chat.Load())
		assert.Equal(t, "/openai/v1/chat/completions", ctr.lastPath.Load())
	})

	t.Run("aliases_firewall_on_chat", func(t *testing.T) {
		ctr.chat.Store(0)
		ctr.responses.Store(0)
		// 无 summary 时不桥接；Chat 路径清洗 camelCase effort
		raw := []byte(`{
			"model":"gpt-5.5",
			"messages":[{"role":"user","content":"hi"}],
			"reasoningEffort":"low"
		}`)
		var req core.ChatRequest
		require.NoError(t, json.Unmarshal(raw, &req))
		_, err := p.ChatCompletion(context.Background(), &req)
		require.NoError(t, err)
		body, _ := ctr.lastBody.Load().(map[string]any)
		_, hasCamel := body["reasoningEffort"]
		assert.False(t, hasCamel)
		assert.Equal(t, "low", body["reasoning_effort"])
		assert.Equal(t, int32(1), ctr.chat.Load())
		assert.Equal(t, int32(0), ctr.responses.Load())
	})

	t.Run("summary_plus_effort_bridges_without_tools", func(t *testing.T) {
		ctr.chat.Store(0)
		ctr.responses.Store(0)
		raw := []byte(`{
			"model":"gpt-5.5",
			"messages":[{"role":"user","content":"hi"}],
			"reasoningSummary":"auto",
			"reasoningEffort":"low"
		}`)
		var req core.ChatRequest
		require.NoError(t, json.Unmarshal(raw, &req))
		require.True(t, openai.ShouldBridgeChatToResponses(&req))
		_, err := p.ChatCompletion(context.Background(), &req)
		require.NoError(t, err)
		assert.Equal(t, int32(0), ctr.chat.Load())
		assert.Equal(t, int32(1), ctr.responses.Load())
		assert.Equal(t, "/openai/v1/responses", ctr.lastPath.Load())
		body, _ := ctr.lastBody.Load().(map[string]any)
		_, hasSummary := body["reasoningSummary"]
		assert.False(t, hasSummary)
		r, _ := body["reasoning"].(map[string]any)
		require.NotNil(t, r)
		assert.Equal(t, "low", r["effort"])
		assert.Equal(t, "auto", r["summary"])
	})

	t.Run("tools_plus_reasoning_bridges_openai_v1_responses", func(t *testing.T) {
		ctr.chat.Store(0)
		ctr.responses.Store(0)
		req := &core.ChatRequest{
			Model: "gpt-5.5",
			Messages: []core.Message{
				{Role: "user", Content: "weather?"},
			},
			Tools: []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name":       "get_weather",
					"parameters": map[string]any{"type": "object"},
				},
			}},
			Reasoning: &core.Reasoning{Effort: "low", Summary: "auto"},
		}
		require.True(t, openai.ShouldBridgeChatToResponses(req))
		resp, err := p.ChatCompletion(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, int32(0), ctr.chat.Load())
		assert.Equal(t, int32(1), ctr.responses.Load())
		assert.Equal(t, "/openai/v1/responses", ctr.lastPath.Load())
		assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
		raw := resp.Choices[0].Message.ExtraFields.Lookup("reasoning_content")
		require.NotEmpty(t, raw)
	})

	t.Run("stream_bridge_no_stream_options", func(t *testing.T) {
		ctr.chat.Store(0)
		ctr.responses.Store(0)
		req := &core.ChatRequest{
			Model: "gpt-5.5",
			Messages: []core.Message{
				{Role: "user", Content: "hi"},
			},
			Tools: []map[string]any{{
				"type": "function", "function": map[string]any{"name": "get_weather"},
			}},
			Reasoning:     &core.Reasoning{Effort: "low"},
			Stream:        true,
			StreamOptions: &core.StreamOptions{IncludeUsage: true},
		}
		stream, err := p.StreamChatCompletion(context.Background(), req)
		require.NoError(t, err)
		defer stream.Close()
		body, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, int32(1), ctr.responses.Load())
		assert.Equal(t, "/openai/v1/responses", ctr.lastPath.Load())
		assert.Contains(t, string(body), "chat.completion.chunk")
		reqBody, _ := ctr.lastBody.Load().(map[string]any)
		_, hasSO := reqBody["stream_options"]
		assert.False(t, hasSO, "Azure Responses rejects stream_options")
	})
}
