package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

func TestConvertToAnthropicRequest_SanitizesOrphanedToolCall(t *testing.T) {
	req := &core.ChatRequest{
		Model: "claude-sonnet-4-6",
		Messages: []core.Message{
			{Role: "user", Content: "Search"},
			{
				Role: "assistant",
				ToolCalls: []core.ToolCall{
					{ID: "call_1", Type: "function", Function: core.FunctionCall{Name: "web_search", Arguments: "{}"}},
				},
			},
			{Role: "user", Content: "What about JS?"},
		},
		Tools: []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":       "web_search",
					"parameters": map[string]any{"type": "object"},
				},
			},
		},
	}

	out, err := convertToAnthropicRequest(req)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	raw, _ := json.Marshal(out.Messages)
	s := string(raw)
	if !strings.Contains(s, "tool_result") || !strings.Contains(s, "call_1") {
		t.Fatalf("expected injected tool_result in messages: %s", s)
	}
	if !strings.Contains(s, "skipped/interrupted") {
		t.Fatalf("expected dummy content: %s", s)
	}
}
