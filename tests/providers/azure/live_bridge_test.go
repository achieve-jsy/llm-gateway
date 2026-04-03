//go:build live

package azure_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/azure"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 用法：
//   source /tmp/azure_e2e.env  # 或手动 export AZURE_API_KEY / AZURE_ENDPOINT
//   go test ./tests/providers/azure/ -tags=live -count=1 -v -timeout 180s -run Live

func liveAzureEnv(t *testing.T) (key, endpoint, model string) {
	t.Helper()
	key = os.Getenv("AZURE_API_KEY")
	endpoint = os.Getenv("AZURE_ENDPOINT")
	model = os.Getenv("AZURE_MODEL")
	if model == "" {
		model = "gpt-5.5"
	}
	if key == "" || endpoint == "" {
		t.Skip("set AZURE_API_KEY and AZURE_ENDPOINT")
	}
	return key, endpoint, model
}

func newLiveAzure(t *testing.T) (core.Provider, string) {
	t.Helper()
	key, endpoint, model := liveAzureEnv(t)
	p := azure.New(providers.ProviderConfig{
		APIKey:  key,
		BaseURL: endpoint,
	}, providers.ProviderOptions{})
	return p, model
}

func TestLiveAzureV1_ChatParamNormalize_NoTools(t *testing.T) {
	// Phase A：无 tools 时不桥接，reasoningSummary 被清洗，Chat 应成功。
	p, model := newLiveAzure(t)
	maxTok := 24
	raw := []byte(`{
		"model":"` + model + `",
		"messages":[{"role":"user","content":"Reply with exactly: OK"}],
		"max_tokens":24,
		"temperature":0.2,
		"reasoningSummary":"auto",
		"reasoningEffort":"low"
	}`)
	var req core.ChatRequest
	require.NoError(t, json.Unmarshal(raw, &req))
	_ = maxTok

	resp, err := p.ChatCompletion(context.Background(), &req)
	require.NoError(t, err, "Phase A chat path should succeed after dropping reasoningSummary")
	require.NotEmpty(t, resp.Choices)
	t.Logf("phaseA model=%s finish=%s content_len=%d", resp.Model, resp.Choices[0].FinishReason, len(core.ExtractTextContent(resp.Choices[0].Message.Content)))
}

func TestLiveAzureV1_ChatViaResponses_ToolsAndReasoning(t *testing.T) {
	// Phase B：gpt-5.4+ + tools + reasoning_effort → 自动桥 /responses。
	p, model := newLiveAzure(t)
	require.True(t, openai.ShouldBridgeChatToResponses(&core.ChatRequest{
		Model:     model,
		Tools:     []map[string]any{{"type": "function", "function": map[string]any{"name": "get_weather"}}},
		Reasoning: &core.Reasoning{Effort: "low"},
	}), "model %s should trigger Chat→Responses bridge", model)

	maxOut := 64
	req := &core.ChatRequest{
		Model: model,
		Messages: []core.Message{
			{Role: "system", Content: "You are a weather assistant. Always call get_weather for city questions."},
			{Role: "user", Content: "What is the weather in San Francisco?"},
		},
		Tools: []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get weather by city",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		}},
		ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "get_weather"},
		},
		MaxTokens: &maxOut,
		Reasoning: &core.Reasoning{Effort: "low", Summary: "auto"},
	}

	resp, err := p.ChatCompletion(context.Background(), req)
	require.NoError(t, err, "bridged ChatViaResponses should succeed against Azure /openai/v1/responses")
	require.Equal(t, "chat.completion", resp.Object)
	require.NotEmpty(t, resp.Choices)
	choice := resp.Choices[0]
	t.Logf("phaseB finish=%s tool_calls=%d usage=%+v", choice.FinishReason, len(choice.Message.ToolCalls), resp.Usage)

	// 强制 tool_choice 时通常返回 tool_calls；若模型直接回答也算协议通。
	if choice.FinishReason == "tool_calls" || len(choice.Message.ToolCalls) > 0 {
		require.NotEmpty(t, choice.Message.ToolCalls)
		assert.Equal(t, "get_weather", choice.Message.ToolCalls[0].Function.Name)
		assert.NotEmpty(t, choice.Message.ToolCalls[0].Function.Arguments)
	} else {
		assert.NotEmpty(t, core.ExtractTextContent(choice.Message.Content))
	}
}

func TestLiveAzureV1_StreamChatViaResponses_ToolsAndReasoning(t *testing.T) {
	p, model := newLiveAzure(t)
	maxOut := 64
	req := &core.ChatRequest{
		Model: model,
		Messages: []core.Message{
			{Role: "user", Content: "Call get_weather for Tokyo."},
		},
		Tools: []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get weather by city",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		}},
		ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "get_weather"},
		},
		MaxTokens:     &maxOut,
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
	t.Logf("stream_bytes=%d", len(body))
	require.Contains(t, text, "chat.completion.chunk")
	require.Contains(t, text, "data: [DONE]")
	assert.True(t,
		strings.Contains(text, `"finish_reason"`) || strings.Contains(text, "tool_calls") || strings.Contains(text, "content"),
		"stream should contain chat deltas or finish",
	)
}

func TestLiveAzureV1_ToolsWithoutReasoning_StaysChat(t *testing.T) {
	p, model := newLiveAzure(t)
	maxOut := 48
	req := &core.ChatRequest{
		Model: model,
		Messages: []core.Message{
			{Role: "user", Content: "Call get_weather for Paris."},
		},
		Tools: []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name": "get_weather",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
				},
			},
		}},
		ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "get_weather"},
		},
		MaxTokens: &maxOut,
	}
	require.False(t, openai.ShouldBridgeChatToResponses(req))

	resp, err := p.ChatCompletion(context.Background(), req)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Choices)
	t.Logf("noBridge finish=%s tools=%d", resp.Choices[0].FinishReason, len(resp.Choices[0].Message.ToolCalls))
}
