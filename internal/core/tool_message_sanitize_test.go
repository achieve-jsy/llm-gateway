package core

import (
	"strings"
	"testing"
)

func TestSanitizeToolMessages_AddsDummyForOrphanedToolCall(t *testing.T) {
	in := []Message{
		{Role: "user", Content: "Search"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_1", Type: "function", Function: FunctionCall{Name: "web_search", Arguments: "{}"}},
			},
		},
		{Role: "user", Content: "What about JS?"},
	}

	out := SanitizeToolMessages(in)
	if len(out) != 4 {
		t.Fatalf("len=%d want 4: %+v", len(out), roles(out))
	}
	if out[2].Role != "tool" || out[2].ToolCallID != "call_1" {
		t.Fatalf("expected dummy tool after assistant, got %+v", out[2])
	}
	content, _ := out[2].Content.(string)
	if !strings.Contains(content, "web_search") || !strings.Contains(content, "skipped/interrupted") {
		t.Fatalf("unexpected dummy content: %q", content)
	}
	// Input must not be mutated
	if len(in) != 3 {
		t.Fatalf("input mutated: len=%d", len(in))
	}
}

func TestSanitizeToolMessages_DropsOrphanedToolResult(t *testing.T) {
	in := []Message{
		{Role: "user", Content: "Hi"},
		{Role: "assistant", Content: "Hello"},
		{Role: "tool", ToolCallID: "call_missing", Content: "orphan"},
		{Role: "user", Content: "Continue"},
	}

	out := SanitizeToolMessages(in)
	if len(out) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(out), roles(out))
	}
	for _, m := range out {
		if m.Role == "tool" {
			t.Fatalf("orphaned tool result should be dropped: %+v", m)
		}
	}
}

func TestSanitizeToolMessages_KeepsValidToolPair(t *testing.T) {
	in := []Message{
		{Role: "user", Content: "Search"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_1", Type: "function", Function: FunctionCall{Name: "web_search", Arguments: "{}"}},
			},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "results"},
		{Role: "user", Content: "Thanks"},
	}

	out := SanitizeToolMessages(in)
	if len(out) != 4 {
		t.Fatalf("len=%d want 4: %+v", len(out), roles(out))
	}
	if out[2].Role != "tool" || out[2].Content != "results" {
		t.Fatalf("valid tool result mutated: %+v", out[2])
	}
}

func TestSanitizeToolMessages_FillsEmptyUserAssistantContent(t *testing.T) {
	in := []Message{
		{Role: "user", Content: ""},
		{Role: "assistant", Content: "   "},
		{Role: "user", Content: "ok"},
	}

	out := SanitizeToolMessages(in)
	u, _ := out[0].Content.(string)
	a, _ := out[1].Content.(string)
	if !strings.Contains(u, "Empty message content") {
		t.Fatalf("user empty not sanitized: %q", u)
	}
	if !strings.Contains(a, "Empty message content") {
		t.Fatalf("assistant empty not sanitized: %q", a)
	}
}

func TestSanitizeToolMessages_ParallelToolCallsPartialResults(t *testing.T) {
	in := []Message{
		{Role: "user", Content: "do both"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "a", Type: "function", Function: FunctionCall{Name: "t1", Arguments: "{}"}},
				{ID: "b", Type: "function", Function: FunctionCall{Name: "t2", Arguments: "{}"}},
			},
		},
		{Role: "tool", ToolCallID: "a", Content: "ok-a"},
		// missing tool result for "b"
		{Role: "user", Content: "go on"},
	}

	out := SanitizeToolMessages(in)
	toolIDs := []string{}
	for _, m := range out {
		if m.Role == "tool" {
			toolIDs = append(toolIDs, m.ToolCallID)
		}
	}
	if len(toolIDs) != 2 {
		t.Fatalf("tool ids=%v want [a b]", toolIDs)
	}
	if toolIDs[0] != "a" || toolIDs[1] != "b" {
		t.Fatalf("tool ids=%v", toolIDs)
	}
}

func roles(msgs []Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return out
}
