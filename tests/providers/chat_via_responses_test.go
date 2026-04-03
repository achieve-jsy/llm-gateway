package providers_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertChatRequestToResponses_MapsToolsAndReasoning(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.5",
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"user","content":"hi"}
		],
		"tools":[{"type":"function","function":{"name":"ping","description":"d","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"ping"}},
		"reasoning_effort":{"effort":"high","summary":"detailed"},
		"max_completion_tokens":20
	}`)
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal(raw, &req))

	respReq, err := providers.ConvertChatRequestToResponses(&req)
	require.NoError(t, err)
	assert.Equal(t, "sys", respReq.Instructions)
	require.NotNil(t, respReq.MaxOutputTokens)
	assert.Equal(t, 20, *respReq.MaxOutputTokens)
	require.NotNil(t, respReq.Reasoning)
	assert.Equal(t, "high", respReq.Reasoning.Effort)
	assert.Equal(t, "detailed", respReq.Reasoning.Summary)
	require.Len(t, respReq.Tools, 1)
	assert.Equal(t, "ping", respReq.Tools[0]["name"])
	choice, ok := respReq.ToolChoice.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ping", choice["name"])
	_, hasFn := choice["function"]
	assert.False(t, hasFn)
}

func TestConvertResponsesResponseToChat_ToolCalls(t *testing.T) {
	resp := &core.ResponsesResponse{
		ID:        "resp_1",
		CreatedAt: 100,
		Model:     "gpt-5.5",
		Output: []core.ResponsesOutputItem{
			{Type: "function_call", CallID: "call_1", Name: "ping", Arguments: `{}`},
		},
		Usage: &core.ResponsesUsage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
	}
	chat := providers.ConvertResponsesResponseToChat(resp)
	assert.Equal(t, "chat.completion", chat.Object)
	assert.Equal(t, "tool_calls", chat.Choices[0].FinishReason)
	require.Len(t, chat.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "ping", chat.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Equal(t, 1, chat.Usage.PromptTokens)
}

func TestConvertResponsesResponseToChat_ReasoningContent(t *testing.T) {
	resp := &core.ResponsesResponse{
		ID:    "resp_r",
		Model: "gpt-5.5",
		Output: []core.ResponsesOutputItem{
			{
				Type: "reasoning",
				Summary: []core.ResponsesContentItem{
					{Type: "summary_text", Text: "thinking hard"},
				},
			},
			{
				Type: "message",
				Role: "assistant",
				Content: []core.ResponsesContentItem{
					{Type: "output_text", Text: "done"},
				},
			},
		},
	}
	chat := providers.ConvertResponsesResponseToChat(resp)
	assert.Equal(t, "done", chat.Choices[0].Message.Content)
	raw := chat.Choices[0].Message.ExtraFields.Lookup("reasoning_content")
	require.NotEmpty(t, raw)
	var reasoning string
	require.NoError(t, json.Unmarshal(raw, &reasoning))
	assert.Equal(t, "thinking hard", reasoning)
}

func TestResponsesToChatStreamConverter_TextAndDone(t *testing.T) {
	upstream := io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"Hello"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_x","model":"gpt-5.5","output":[{"type":"message"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		``,
	}, "\n")))

	conv := providers.NewResponsesToChatStreamConverter(upstream, "gpt-5.5", true)
	defer conv.Close()

	body, err := io.ReadAll(conv)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, `"object":"chat.completion.chunk"`)
	assert.Contains(t, text, `"content":"Hello"`)
	assert.Contains(t, text, `"finish_reason":"stop"`)
	assert.Contains(t, text, `"prompt_tokens":1`)
	assert.Contains(t, text, "data: [DONE]")
}
