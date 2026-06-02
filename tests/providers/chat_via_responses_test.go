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

func TestConvertChatRequestToResponses_RemapsVerbosityToText(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.5",
		"messages":[{"role":"user","content":"hi"}],
		"verbosity":"low",
		"tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object"}}}],
		"reasoning_effort":"high"
	}`)
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal(raw, &req))

	respReq, err := providers.ConvertChatRequestToResponses(&req)
	require.NoError(t, err)
	assert.Empty(t, respReq.ExtraFields.Lookup("verbosity"))
	textRaw := respReq.ExtraFields.Lookup("text")
	require.NotEmpty(t, textRaw)
	var textObj map[string]any
	require.NoError(t, json.Unmarshal(textRaw, &textObj))
	assert.Equal(t, "low", textObj["verbosity"])
}

func TestConvertChatRequestToResponses_RemapsResponseFormatAndDropsChatNoise(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.5",
		"messages":[{"role":"user","content":"hi"}],
		"response_format":{"type":"json_object"},
		"verbosity":"high",
		"user":"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_EXTRA",
		"store":true,
		"reasoning_effort":"medium",
		"max_tokens":10,
		"stream_options":{"include_usage":true,"include_obfuscation":false}
	}`)
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal(raw, &req))

	respReq, err := providers.ConvertChatRequestToResponses(&req)
	require.NoError(t, err)

	assert.Empty(t, respReq.ExtraFields.Lookup("verbosity"))
	assert.Empty(t, respReq.ExtraFields.Lookup("response_format"))
	assert.Empty(t, respReq.ExtraFields.Lookup("reasoning_effort"))
	assert.Nil(t, respReq.StreamOptions)

	textRaw := respReq.ExtraFields.Lookup("text")
	require.NotEmpty(t, textRaw)
	var textObj map[string]any
	require.NoError(t, json.Unmarshal(textRaw, &textObj))
	assert.Equal(t, "high", textObj["verbosity"])
	format, ok := textObj["format"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "json_object", format["type"])

	userRaw := respReq.ExtraFields.Lookup("user")
	require.NotEmpty(t, userRaw)
	var user string
	require.NoError(t, json.Unmarshal(userRaw, &user))
	assert.Len(t, user, 64)

	// Chat 的 stream_options 是 typed（仅 include_usage）；include_usage 不得出现在 Responses。
	assert.Empty(t, respReq.ExtraFields.Lookup("stream_options"))
	assert.Nil(t, respReq.StreamOptions)

	// 显式带 include_obfuscation 时应透传（LiteLLM 行为）
	req2 := &core.ChatRequest{
		Model:    "gpt-5.5",
		Messages: []core.Message{{Role: "user", Content: "hi"}},
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"stream_options": json.RawMessage(`{"include_usage":true,"include_obfuscation":false}`),
		}),
	}
	resp2, err := providers.ConvertChatRequestToResponses(req2)
	require.NoError(t, err)
	soRaw := resp2.ExtraFields.Lookup("stream_options")
	require.NotEmpty(t, soRaw)
	var so map[string]any
	require.NoError(t, json.Unmarshal(soRaw, &so))
	_, hasUsage := so["include_usage"]
	assert.False(t, hasUsage)
	assert.Equal(t, false, so["include_obfuscation"])
}

func TestSanitizeResponsesRequest_RemapsTopLevelVerbosity(t *testing.T) {
	req := &core.ResponsesRequest{
		Model: "gpt-5.5",
		Input: "hi",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"verbosity": json.RawMessage(`"medium"`),
			"text":      json.RawMessage(`{"format":{"type":"text"}}`),
		}),
		StreamOptions: &core.StreamOptions{IncludeUsage: true},
	}
	providers.SanitizeResponsesRequest(req)
	assert.Nil(t, req.StreamOptions)
	assert.Empty(t, req.ExtraFields.Lookup("verbosity"))
	textRaw := req.ExtraFields.Lookup("text")
	require.NotEmpty(t, textRaw)
	var textObj map[string]any
	require.NoError(t, json.Unmarshal(textRaw, &textObj))
	assert.Equal(t, "medium", textObj["verbosity"])
	format, ok := textObj["format"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "text", format["type"])
}

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

func TestResponsesToChatStreamConverter_PreservesCachedTokens(t *testing.T) {
	upstream := io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_cache","model":"gpt-5.5","output":[{"type":"message"}],"usage":{"input_tokens":2000,"output_tokens":10,"total_tokens":2010,"input_tokens_details":{"cached_tokens":1800},"output_tokens_details":{"reasoning_tokens":4}}}}`,
		``,
	}, "\n")))

	conv := providers.NewResponsesToChatStreamConverter(upstream, "gpt-5.5", true)
	defer conv.Close()

	body, err := io.ReadAll(conv)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, `"prompt_tokens":2000`)
	assert.Contains(t, text, `"prompt_tokens_details"`)
	assert.Contains(t, text, `"cached_tokens":1800`)
	assert.Contains(t, text, `"completion_tokens_details"`)
	assert.Contains(t, text, `"reasoning_tokens":4`)
	assert.NotContains(t, text, `"input_tokens_details"`)
}

func TestResponsesToChatStreamConverter_FailedEmitsUsageAndError(t *testing.T) {
	upstream := io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		``,
		`data: {"type":"response.failed","response":{"id":"resp_f","model":"gpt-5.5","status":"failed","error":{"message":"boom","code":"server_error"},"usage":{"input_tokens":100,"output_tokens":5,"total_tokens":105}}}`,
		``,
	}, "\n")))

	conv := providers.NewResponsesToChatStreamConverter(upstream, "gpt-5.5", true)
	defer conv.Close()

	body, err := io.ReadAll(conv)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, `"content":"partial"`)
	assert.Contains(t, text, `"prompt_tokens":100`)
	assert.Contains(t, text, `"error"`)
	assert.Contains(t, text, `"message":"boom"`)
	assert.Contains(t, text, "data: [DONE]")
}

func TestResponsesToChatStreamConverter_FailedNoIncludeUsageSkipsUsage(t *testing.T) {
	upstream := io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.failed","response":{"id":"resp_f2","model":"gpt-5.5","status":"failed","error":{"message":"boom"},"usage":{"input_tokens":100,"output_tokens":5,"total_tokens":105}}}`,
		``,
	}, "\n")))

	conv := providers.NewResponsesToChatStreamConverter(upstream, "gpt-5.5", false)
	defer conv.Close()

	body, err := io.ReadAll(conv)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, `"error"`)
	assert.NotContains(t, text, `"prompt_tokens"`)
	assert.Contains(t, text, "data: [DONE]")
}

func TestResponsesToChatStreamConverter_IncompleteEmitsError(t *testing.T) {
	upstream := io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.incomplete","response":{"id":"resp_i","model":"gpt-5.5","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
		``,
	}, "\n")))

	conv := providers.NewResponsesToChatStreamConverter(upstream, "gpt-5.5", true)
	defer conv.Close()

	body, err := io.ReadAll(conv)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, `"error"`)
	assert.Contains(t, text, "max_output_tokens")
	assert.Contains(t, text, "data: [DONE]")
	assert.NotContains(t, text, `"prompt_tokens"`)
}

func TestSanitizeResponsesRequest_StreamDoesNotInjectUsage(t *testing.T) {
	// Azure/OpenAI 的 Responses include 白名单不含 "usage"（合法值见
	// ResponseIncludable），网关不得强加 include=["usage"]；流式 usage 从
	// response.completed 事件的 response.usage 提取，无需该参数。
	req := &core.ResponsesRequest{
		Model:  "gpt-5.5",
		Input:  "hi",
		Stream: true,
	}
	providers.SanitizeResponsesRequest(req)
	assert.Empty(t, req.ExtraFields.Lookup("include"))
}

func TestSanitizeResponsesRequest_StreamKeepsClientIncludeUntouched(t *testing.T) {
	req := &core.ResponsesRequest{
		Model:  "gpt-5.5",
		Input:  "hi",
		Stream: true,
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"include": json.RawMessage(`["file_search"]`),
		}),
	}
	providers.SanitizeResponsesRequest(req)
	includeRaw := req.ExtraFields.Lookup("include")
	require.NotEmpty(t, includeRaw)
	var include []string
	require.NoError(t, json.Unmarshal(includeRaw, &include))
	assert.ElementsMatch(t, []string{"file_search"}, include)
}

func TestSanitizeResponsesRequest_NonStreamSkipsInclude(t *testing.T) {
	req := &core.ResponsesRequest{
		Model:  "gpt-5.5",
		Input:  "hi",
		Stream: false,
	}
	providers.SanitizeResponsesRequest(req)
	assert.Empty(t, req.ExtraFields.Lookup("include"))
}
