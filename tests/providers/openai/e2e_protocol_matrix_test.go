package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pathCounter struct {
	chat, responses atomic.Int32
	lastPath        atomic.Value // string
	lastBody        atomic.Value // map[string]any
}

func newMatrixServer(t *testing.T, withReasoningContent bool) (*httptest.Server, *pathCounter) {
	t.Helper()
	c := &pathCounter{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.lastPath.Store(r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		c.lastBody.Store(body)

		switch r.URL.Path {
		case "/chat/completions":
			c.chat.Add(1)
			if body["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-s\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
				_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-s\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-m", "object": "chat.completion", "model": body["model"],
				"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "chat-ok"}, "finish_reason": "stop"}},
			})
		case "/responses":
			c.responses.Add(1)
			if body["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				events := []string{
					`data: {"type":"response.reasoning_summary_text.delta","delta":"think"}`,
					`data: {"type":"response.output_text.delta","delta":"hi"}`,
					`data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"get_weather"}}`,
					`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"city\":\"SF\"}"}`,
					`data: {"type":"response.completed","response":{"id":"resp_m","model":"gpt-5.5","output":[{"type":"function_call"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}}`,
				}
				for _, ev := range events {
					_, _ = io.WriteString(w, ev+"\n\n")
				}
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			output := []map[string]any{
				{"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
					"content": []map[string]any{{"type": "output_text", "text": "bridged-ok"}}},
				{"id": "fc_1", "type": "function_call", "status": "completed",
					"call_id": "call_m", "name": "get_weather", "arguments": `{"city":"SF"}`},
			}
			if withReasoningContent {
				output = append([]map[string]any{{
					"id": "rs_1", "type": "reasoning",
					"summary": []map[string]any{{"type": "summary_text", "text": "reasoned"}},
				}}, output...)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "resp_m", "object": "response", "model": body["model"], "status": "completed", "created_at": 1,
				"output": output,
				"usage":  map[string]any{"input_tokens": 9, "output_tokens": 6, "total_tokens": 15},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func weatherTool() []map[string]any {
	return []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":       "get_weather",
			"parameters": map[string]any{"type": "object"},
		},
	}}
}

func mustChatReq(t *testing.T, raw string) *core.ChatRequest {
	t.Helper()
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal([]byte(raw), &req))
	return &req
}

// TestE2E_ProtocolMatrix_AllPathways 覆盖 OpenAI Chat 协议全部关键通路。
func TestE2E_ProtocolMatrix_AllPathways(t *testing.T) {
	srv, ctr := newMatrixServer(t, true)
	p := newFakeProvider(t, srv.URL)

	cases := []struct {
		name          string
		req           *core.ChatRequest
		wantChat      int32
		wantResponses int32
		wantBridge    bool
		assertBody    func(t *testing.T, body map[string]any)
		assertResp    func(t *testing.T, resp *core.ChatResponse)
	}{
		{
			name:          "plain_chat",
			req:           mustChatReq(t, `{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`),
			wantChat:      1,
			wantResponses: 0,
			wantBridge:    false,
			assertBody: func(t *testing.T, body map[string]any) {
				assert.Equal(t, float64(16), body["max_completion_tokens"])
				_, hasMax := body["max_tokens"]
				assert.False(t, hasMax)
			},
		},
		{
			name: "aliases_no_tools_stay_chat",
			req: mustChatReq(t, `{
				"model":"gpt-5.5",
				"messages":[{"role":"user","content":"hi"}],
				"reasoningSummary":"auto",
				"reasoningEffort":"low",
				"temperature":0.2
			}`),
			wantChat:      1,
			wantResponses: 0,
			assertBody: func(t *testing.T, body map[string]any) {
				_, hasSummary := body["reasoningSummary"]
				_, hasCamel := body["reasoningEffort"]
				_, hasTemp := body["temperature"]
				assert.False(t, hasSummary)
				assert.False(t, hasCamel)
				assert.False(t, hasTemp)
				assert.Equal(t, "low", body["reasoning_effort"])
			},
		},
		{
			name: "tools_only_stay_chat",
			req: &core.ChatRequest{
				Model:    "gpt-5.5",
				Messages: []core.Message{{Role: "user", Content: "hi"}},
				Tools:    weatherTool(),
			},
			wantChat:      1,
			wantResponses: 0,
			wantBridge:    false,
		},
		{
			name: "summary_only_with_tools_no_bridge",
			req: mustChatReq(t, `{
				"model":"gpt-5.5",
				"messages":[{"role":"user","content":"hi"}],
				"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],
				"reasoningSummary":"auto"
			}`),
			wantChat:      1,
			wantResponses: 0,
			wantBridge:    false,
		},
		{
			name: "tools_plus_reasoning_effort_bridges",
			req: mustChatReq(t, `{
				"model":"gpt-5.5",
				"messages":[{"role":"system","content":"sys"},{"role":"user","content":"weather?"}],
				"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],
				"reasoning_effort":"medium",
				"reasoningSummary":"auto",
				"max_tokens":32
			}`),
			wantChat:      0,
			wantResponses: 1,
			wantBridge:    true,
			assertBody: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "sys", body["instructions"])
				assert.Equal(t, float64(32), body["max_output_tokens"])
				r, _ := body["reasoning"].(map[string]any)
				require.NotNil(t, r)
				assert.Equal(t, "medium", r["effort"])
				assert.Equal(t, "auto", r["summary"])
			},
			assertResp: func(t *testing.T, resp *core.ChatResponse) {
				assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
				raw := resp.Choices[0].Message.ExtraFields.Lookup("reasoning_content")
				require.NotEmpty(t, raw)
				var rc string
				require.NoError(t, json.Unmarshal(raw, &rc))
				assert.Equal(t, "reasoned", rc)
			},
		},
		{
			name: "tools_plus_reasoningEffort_camel_bridges",
			req: mustChatReq(t, `{
				"model":"gpt-5.5",
				"messages":[{"role":"user","content":"hi"}],
				"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],
				"reasoningEffort":"high"
			}`),
			wantChat:      0,
			wantResponses: 1,
			wantBridge:    true,
		},
		{
			name: "tools_plus_nested_reasoning_bridges",
			req: mustChatReq(t, `{
				"model":"gpt-5.5",
				"messages":[{"role":"user","content":"hi"}],
				"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],
				"reasoning":{"effort":"low","summary":"detailed"}
			}`),
			wantChat:      0,
			wantResponses: 1,
			wantBridge:    true,
			assertBody: func(t *testing.T, body map[string]any) {
				r, _ := body["reasoning"].(map[string]any)
				require.NotNil(t, r)
				assert.Equal(t, "low", r["effort"])
				assert.Equal(t, "detailed", r["summary"])
			},
		},
		{
			name: "gpt51_tools_reasoning_no_bridge",
			req: &core.ChatRequest{
				Model:     "gpt-5.1",
				Messages:  []core.Message{{Role: "user", Content: "hi"}},
				Tools:     weatherTool(),
				Reasoning: &core.Reasoning{Effort: "medium"},
			},
			wantChat:      1,
			wantResponses: 0,
			wantBridge:    false,
		},
		{
			name: "gpt5_chat_keeps_temperature",
			req: func() *core.ChatRequest {
				temp := 0.4
				return &core.ChatRequest{
					Model:       "gpt-5-chat-latest",
					Messages:    []core.Message{{Role: "user", Content: "hi"}},
					Temperature: &temp,
				}
			}(),
			wantChat:      1,
			wantResponses: 0,
			assertBody: func(t *testing.T, body map[string]any) {
				assert.Equal(t, 0.4, body["temperature"])
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctr.chat.Store(0)
			ctr.responses.Store(0)
			assert.Equal(t, tc.wantBridge, openai.ShouldBridgeChatToResponses(tc.req), "bridge gate")

			resp, err := p.ChatCompletion(context.Background(), tc.req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tc.wantChat, ctr.chat.Load(), "chat hits")
			assert.Equal(t, tc.wantResponses, ctr.responses.Load(), "responses hits")

			body, _ := ctr.lastBody.Load().(map[string]any)
			if tc.assertBody != nil {
				tc.assertBody(t, body)
			}
			if tc.assertResp != nil {
				tc.assertResp(t, resp)
			}
		})
	}
}

func TestE2E_ProtocolMatrix_StreamPathways(t *testing.T) {
	srv, ctr := newMatrixServer(t, false)
	p := newFakeProvider(t, srv.URL)

	t.Run("stream_tools_only_stays_chat", func(t *testing.T) {
		ctr.chat.Store(0)
		ctr.responses.Store(0)
		req := &core.ChatRequest{
			Model:    "gpt-5.5",
			Messages: []core.Message{{Role: "user", Content: "hi"}},
			Tools:    weatherTool(),
			Stream:   true,
		}
		stream, err := p.StreamChatCompletion(context.Background(), req)
		require.NoError(t, err)
		defer stream.Close()
		body, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, int32(1), ctr.chat.Load())
		assert.Equal(t, int32(0), ctr.responses.Load())
		assert.Contains(t, string(body), "chat.completion.chunk")
		assert.Contains(t, string(body), "[DONE]")
	})

	t.Run("stream_tools_reasoning_bridges", func(t *testing.T) {
		ctr.chat.Store(0)
		ctr.responses.Store(0)
		req := &core.ChatRequest{
			Model:         "gpt-5.5",
			Messages:      []core.Message{{Role: "user", Content: "hi"}},
			Tools:         weatherTool(),
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
		assert.Equal(t, int32(0), ctr.chat.Load())
		assert.Equal(t, int32(1), ctr.responses.Load())
		assert.Contains(t, text, "reasoning_content")
		assert.Contains(t, text, "tool_calls")
		assert.Contains(t, text, "[DONE]")
		// bridge must not forward stream_options to Responses
		bodyMap, _ := ctr.lastBody.Load().(map[string]any)
		_, hasSO := bodyMap["stream_options"]
		assert.False(t, hasSO)
	})
}

func TestE2E_ProtocolMatrix_AnthropicThinkingDoesNotAutoBridge(t *testing.T) {
	// Claude /v1/messages → AnthropicToChatRequest 当前不映射 thinking → reasoning_effort。
	anth := &core.AnthropicMessagesRequest{
		Model:     "gpt-5.5",
		MaxTokens: 64,
		Messages:  []core.AnthropicMessage{{Role: "user", Content: "hi"}},
		Tools: []core.AnthropicTool{{
			Name:        "get_weather",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		Thinking: &core.AnthropicThinking{Type: "enabled", BudgetTokens: 1024},
	}
	chatReq := core.AnthropicToChatRequest(anth)
	require.NotNil(t, chatReq)
	assert.NotEmpty(t, chatReq.Tools)
	assert.Nil(t, chatReq.Reasoning)
	assert.False(t, openai.ShouldBridgeChatToResponses(chatReq),
		"Anthropic thinking 未映射时不应误触发 GPT Responses 桥")
}
