package core

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────────────────────────
// sanitizeToolUseIDs tests
// ──────────────────────────────────────────────

func TestSanitizeToolUseIDs(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
	}{
		{
			name: "replaces invalid chars in ToolCallID",
			input: []Message{
				{Role: "tool", ToolCallID: "tool_123/abc:def", Content: "result"},
			},
			expected: []Message{
				{Role: "tool", ToolCallID: "tool_123_abc_def", Content: "result"},
			},
		},
		{
			name: "replaces invalid chars in ToolCalls IDs",
			input: []Message{
				{Role: "assistant", Content: "ok", ToolCalls: []ToolCall{
					{ID: "call_abc.def", Type: "function", Function: FunctionCall{Name: "test", Arguments: "{}"}},
				}},
			},
			expected: []Message{
				{Role: "assistant", Content: "ok", ToolCalls: []ToolCall{
					{ID: "call_abc_def", Type: "function", Function: FunctionCall{Name: "test", Arguments: "{}"}},
				}},
			},
		},
		{
			name: "valid IDs unchanged",
			input: []Message{
				{Role: "tool", ToolCallID: "tool_123-abc", Content: "result"},
			},
			expected: []Message{
				{Role: "tool", ToolCallID: "tool_123-abc", Content: "result"},
			},
		},
		{
			name: "empty IDs not modified",
			input: []Message{
				{Role: "user", Content: "hello"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeToolUseIDs(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.expected))
			}
			for i := range got {
				if got[i].ToolCallID != tt.expected[i].ToolCallID {
					t.Errorf("msg[%d].ToolCallID=%q, want %q", i, got[i].ToolCallID, tt.expected[i].ToolCallID)
				}
				for j := range got[i].ToolCalls {
					if got[i].ToolCalls[j].ID != tt.expected[i].ToolCalls[j].ID {
						t.Errorf("msg[%d].ToolCalls[%d].ID=%q, want %q", i, j, got[i].ToolCalls[j].ID, tt.expected[i].ToolCalls[j].ID)
					}
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// dropThinkingBlocks tests
// ──────────────────────────────────────────────

func TestDropThinkingBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
	}{
		{
			name: "drops thinking and redacted_thinking blocks",
			input: []Message{
				{Role: "assistant", Content: []ContentPart{
					{Type: "thinking", Text: "thinking content"},
					{Type: "text", Text: "hello"},
					{Type: "redacted_thinking", Text: "redacted"},
					{Type: "text", Text: "world"},
				}},
			},
			expected: []Message{
				{Role: "assistant", Content: []ContentPart{
					{Type: "text", Text: "hello"},
					{Type: "text", Text: "world"},
				}},
			},
		},
		{
			name: "all thinking blocks yields empty string",
			input: []Message{
				{Role: "assistant", Content: []ContentPart{
					{Type: "thinking", Text: "only thinking"},
				}},
			},
			expected: []Message{
				{Role: "assistant", Content: ""},
			},
		},
		{
			name: "single text block after filtering becomes plain string",
			input: []Message{
				{Role: "assistant", Content: []ContentPart{
					{Type: "thinking", Text: "to drop"},
					{Type: "text", Text: "only text"},
				}},
			},
			expected: []Message{
				{Role: "assistant", Content: "only text"},
			},
		},
		{
			name: "no thinking blocks, unchanged",
			input: []Message{
				{Role: "user", Content: "hello"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dropThinkingBlocks(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.expected))
			}
			for i := range got {
				switch exp := tt.expected[i].Content.(type) {
				case string:
					if s, ok := got[i].Content.(string); !ok || s != exp {
						t.Errorf("msg[%d].Content=%v (%T), want string %q", i, got[i].Content, got[i].Content, exp)
					}
				case []ContentPart:
					gotParts, ok := got[i].Content.([]ContentPart)
					if !ok {
						t.Errorf("msg[%d].Content=%T, want []ContentPart", i, got[i].Content)
						continue
					}
					if len(gotParts) != len(exp) {
						t.Errorf("msg[%d].Content len=%d, want %d", i, len(gotParts), len(exp))
						continue
					}
					for j := range gotParts {
						if gotParts[j].Type != exp[j].Type || gotParts[j].Text != exp[j].Text {
							t.Errorf("msg[%d].Content[%d] = {%s,%s}, want {%s,%s}", i, j, gotParts[j].Type, gotParts[j].Text, exp[j].Type, exp[j].Text)
						}
					}
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// flattenToolMessageContent tests
// ──────────────────────────────────────────────

func TestFlattenToolMessageContent(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
	}{
		{
			name: "non-tool messages unchanged",
			input: []Message{
				{Role: "user", Content: "hello"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
			},
		},
		{
			name: "tool with string content unchanged",
			input: []Message{
				{Role: "tool", ToolCallID: "call_1", Content: "result"},
			},
			expected: []Message{
				{Role: "tool", ToolCallID: "call_1", Content: "result"},
			},
		},
		{
			name: "tool with array content flattened",
			input: []Message{
				{Role: "tool", ToolCallID: "call_1", Content: []ContentPart{{Type: "text", Text: "result text"}}},
			},
			expected: []Message{
				{Role: "tool", ToolCallID: "call_1", Content: `[{"type":"text","text":"result text"}]`},
			},
		},
		{
			name: "tool with nil content becomes empty string",
			input: []Message{
				{Role: "tool", ToolCallID: "call_1", Content: nil},
			},
			expected: []Message{
				{Role: "tool", ToolCallID: "call_1", Content: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := flattenToolMessageContent(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.expected))
			}
			for i := range got {
				gotStr, ok := got[i].Content.(string)
				if !ok {
					t.Errorf("msg[%d].Content is %T, want string", i, got[i].Content)
					continue
				}
				expStr, ok := tt.expected[i].Content.(string)
				if !ok {
					t.Errorf("expected[%d].Content is %T, want string", i, tt.expected[i].Content)
					continue
				}
				if gotStr != expStr {
					t.Errorf("msg[%d].Content=%q, want %q", i, gotStr, expStr)
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// dropOrphanedToolResults tests
// ──────────────────────────────────────────────

func TestDropOrphanedToolResults(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
	}{
		{
			name: "drops orphaned tool result",
			input: []Message{
				{Role: "user", Content: "hello"},
				{Role: "tool", ToolCallID: "orphan", Content: "result"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
			},
		},
		{
			name: "keeps tool result with matching tool_call",
			input: []Message{
				{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "test", Arguments: "{}"}}}},
				{Role: "tool", ToolCallID: "call_1", Content: "result"},
			},
			expected: []Message{
				{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "test", Arguments: "{}"}}}},
				{Role: "tool", ToolCallID: "call_1", Content: "result"},
			},
		},
		{
			name: "no tool calls means all tool results are orphaned",
			input: []Message{
				{Role: "tool", ToolCallID: "some_id", Content: "data"},
			},
			expected: []Message{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dropOrphanedToolResults(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("len(got)=%d, want %d\ngot: %+v\nexpected: %+v", len(got), len(tt.expected), got, tt.expected)
			}
		})
	}
}

// ──────────────────────────────────────────────
// addDummyToolResults tests
// ──────────────────────────────────────────────

func TestAddDummyToolResults(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		checkLen int // minimum expected length after adding
	}{
		{
			name: "adds dummy for orphaned tool_call",
			input: []Message{
				{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "test", Arguments: "{}"}}}},
			},
			checkLen: 2,
		},
		{
			name: "no tool_calls, no dummy added",
			input: []Message{
				{Role: "user", Content: "hello"},
			},
			checkLen: 1,
		},
		{
			name: "existing tool result does not add extra dummy",
			input: []Message{
				{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "test", Arguments: "{}"}}}},
				{Role: "tool", ToolCallID: "call_1", Content: "result"},
			},
			checkLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addDummyToolResults(tt.input)
			if len(got) < tt.checkLen {
				t.Errorf("len(got)=%d, want >= %d", len(got), tt.checkLen)
			}
			// Verify all tool_calls have matching tool results
			pendingIDs := make(map[string]struct{})
			for _, m := range got {
				for _, tc := range m.ToolCalls {
					pendingIDs[tc.ID] = struct{}{}
				}
			}
			for _, m := range got {
				if m.Role == "tool" && m.ToolCallID != "" {
					delete(pendingIDs, m.ToolCallID)
				}
			}
			if len(pendingIDs) > 0 {
				t.Errorf("unmatched tool_call IDs remain: %v", pendingIDs)
			}
			// Verify dummy results have content "."
			for _, m := range got {
				if m.Role == "tool" && m.Content == "." {
					// This is a dummy - check it has a valid ToolCallID
					if m.ToolCallID == "" {
						t.Error("dummy tool result has empty ToolCallID")
					}
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// mergeConsecutiveSameRole tests
// ──────────────────────────────────────────────

func TestMergeConsecutiveSameRole(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
	}{
		{
			name: "merges consecutive user messages",
			input: []Message{
				{Role: "user", Content: "hello"},
				{Role: "user", Content: "world"},
			},
			expected: []Message{
				{Role: "user", Content: "hello\nworld"},
			},
		},
		{
			name: "does not merge different roles",
			input: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
		},
		{
			name: "does not merge tool messages",
			input: []Message{
				{Role: "tool", ToolCallID: "c1", Content: "r1"},
				{Role: "tool", ToolCallID: "c2", Content: "r2"},
			},
			expected: []Message{
				{Role: "tool", ToolCallID: "c1", Content: "r1"},
				{Role: "tool", ToolCallID: "c2", Content: "r2"},
			},
		},
		{
			name: "single message unchanged",
			input: []Message{
				{Role: "user", Content: "hello"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeConsecutiveSameRole(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("len(got)=%d, want %d\ngot: %+v\nexpected: %+v", len(got), len(tt.expected), got, tt.expected)
			}
			for i := range got {
				if got[i].Role != tt.expected[i].Role {
					t.Errorf("msg[%d].Role=%q, want %q", i, got[i].Role, tt.expected[i].Role)
				}
				gotStr, _ := got[i].Content.(string)
				expStr, _ := tt.expected[i].Content.(string)
				if gotStr != expStr {
					t.Errorf("msg[%d].Content=%q, want %q", i, gotStr, expStr)
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// ensureFirstMessageIsUser tests
// ──────────────────────────────────────────────

func TestEnsureFirstMessageIsUser(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
		expLen   int
	}{
		{
			name: "adds user for empty list",
			input: []Message{},
			expected: []Message{
				{Role: "user", Content: "."},
			},
			expLen: 1,
		},
		{
			name: "inserts user before assistant",
			input: []Message{
				{Role: "assistant", Content: "hello"},
			},
			expected: []Message{
				{Role: "user", Content: "."},
				{Role: "assistant", Content: "hello"},
			},
			expLen: 2,
		},
		{
			name: "preserves existing user first",
			input: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			expLen: 2,
		},
		{
			name: "inserts user after system",
			input: []Message{
				{Role: "system", Content: "be helpful"},
				{Role: "assistant", Content: "ok"},
			},
			expected: []Message{
				{Role: "system", Content: "be helpful"},
				{Role: "user", Content: "."},
				{Role: "assistant", Content: "ok"},
			},
			expLen: 3,
		},
		{
			name: "only system messages get trailing user",
			input: []Message{
				{Role: "system", Content: "be helpful"},
			},
			expected: []Message{
				{Role: "system", Content: "be helpful"},
				{Role: "user", Content: "."},
			},
			expLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ensureFirstMessageIsUser(tt.input)
			if len(got) != tt.expLen {
				t.Fatalf("len(got)=%d, want %d\ngot: %+v\nexpected: %+v", len(got), tt.expLen, got, tt.expected)
			}
			for i := range got {
				if got[i].Role != tt.expected[i].Role {
					t.Errorf("msg[%d].Role=%q, want %q", i, got[i].Role, tt.expected[i].Role)
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// sanitizeEmptyContent tests
// ──────────────────────────────────────────────

func TestSanitizeEmptyContent(t *testing.T) {
	tests := []struct {
		name     string
		input    []Message
		expected []Message
	}{
		{
			name: "replaces empty string with dot",
			input: []Message{
				{Role: "user", Content: ""},
			},
			expected: []Message{
				{Role: "user", Content: "."},
			},
		},
		{
			name: "replaces nil with dot",
			input: []Message{
				{Role: "user", Content: nil},
			},
			expected: []Message{
				{Role: "user", Content: "."},
			},
		},
		{
			name: "non-empty string unchanged",
			input: []Message{
				{Role: "user", Content: "hello"},
			},
			expected: []Message{
				{Role: "user", Content: "hello"},
			},
		},
		{
			name: "structured content unchanged",
			input: []Message{
				{Role: "assistant", Content: []ContentPart{{Type: "text", Text: "hello"}}},
			},
			expected: []Message{
				{Role: "assistant", Content: []ContentPart{{Type: "text", Text: "hello"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeEmptyContent(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("len(got)=%d, want %d", len(got), len(tt.expected))
			}
			for i := range got {
				switch exp := tt.expected[i].Content.(type) {
				case string:
					if s, ok := got[i].Content.(string); !ok || s != exp {
						t.Errorf("msg[%d].Content=%v (%T), want string %q", i, got[i].Content, got[i].Content, exp)
					}
				case []ContentPart:
					_, ok := got[i].Content.([]ContentPart)
					if !ok {
						t.Errorf("msg[%d].Content=%T, want []ContentPart", i, got[i].Content)
					}
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// sanitizeMessagesForProviders end-to-end tests
// See spec section "Verification for P1"
// ──────────────────────────────────────────────

func TestSanitizeMessagesForProviders_EndToEnd(t *testing.T) {
	// Helper: convert Anthropic messages to ChatRequest messages, then sanitize
	buildMessages := func(anthMsgs []AnthropicMessage) []Message {
		return sanitizeMessagesForProviders(convertAnthropicMessages(anthMsgs))
	}

	t.Run("TC1 basic text messages pass through", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		})
		if len(msgs) != 2 {
			t.Fatalf("len=%d, want 2", len(msgs))
		}
		if s, _ := msgs[0].Content.(string); s != "hello" {
			t.Errorf("msg[0].Content=%q, want %q", s, "hello")
		}
		if s, _ := msgs[1].Content.(string); s != "hi" {
			t.Errorf("msg[1].Content=%q, want %q", s, "hi")
		}
	})

	t.Run("TC2 tool_use + tool_result pairing preserved", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: "What's the weather?"},
			{Role: "assistant", Content: []AnthropicContentBlock{
				{Type: "text", Text: "Let me check"},
				{Type: "tool_use", ID: "tu_123", Name: "get_weather", Input: json.RawMessage(`{"loc":"NYC"}`)},
			}},
			{Role: "user", Content: []AnthropicContentBlock{
				{Type: "tool_result", ToolUseID: "tu_123", Content: "72°F"},
			}},
		})
		// Should have: user, assistant (with tool_calls), tool
		if len(msgs) != 3 {
			t.Fatalf("len=%d, want 3", len(msgs))
		}
		if msgs[0].Role != "user" {
			t.Errorf("msg[0].Role=%q, want user", msgs[0].Role)
		}
		if msgs[1].Role != "assistant" {
			t.Errorf("msg[1].Role=%q, want assistant", msgs[1].Role)
		}
		if len(msgs[1].ToolCalls) != 1 {
			t.Fatalf("msg[1].ToolCalls len=%d, want 1", len(msgs[1].ToolCalls))
		}
		if msgs[1].ToolCalls[0].ID != "tu_123" {
			t.Errorf("ToolCall ID=%q, want tu_123", msgs[1].ToolCalls[0].ID)
		}
		if msgs[2].Role != "tool" {
			t.Errorf("msg[2].Role=%q, want tool", msgs[2].Role)
		}
		if msgs[2].ToolCallID != "tu_123" {
			t.Errorf("msg[2].ToolCallID=%q, want tu_123", msgs[2].ToolCallID)
		}
	})

	t.Run("TC3 thinking blocks stripped", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: "Think step by step"},
			{Role: "assistant", Content: []AnthropicContentBlock{
				{Type: "thinking", Thinking: "I should think..."},
				{Type: "text", Text: "The answer is 42"},
			}},
		})
		if len(msgs) != 2 {
			t.Fatalf("len=%d, want 2", len(msgs))
		}
		if s, _ := msgs[1].Content.(string); s != "The answer is 42" {
			t.Errorf("msg[1].Content=%q, want 'The answer is 42'", s)
		}
	})

	t.Run("TC4 invalid tool ID chars replaced", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: "Use tool"},
			{Role: "assistant", Content: []AnthropicContentBlock{
				{Type: "tool_use", ID: "call/abc:def", Name: "test", Input: json.RawMessage(`{}`)},
			}},
			{Role: "user", Content: []AnthropicContentBlock{
				{Type: "tool_result", ToolUseID: "call/abc:def", Content: "done"},
			}},
		})
		if len(msgs) != 3 {
			t.Fatalf("len=%d, want 3", len(msgs))
		}
		// Both the tool_use ID and tool_result ToolCallID should be sanitized
		if msgs[1].ToolCalls[0].ID != "call_abc_def" {
			t.Errorf("ToolCall ID=%q, want call_abc_def", msgs[1].ToolCalls[0].ID)
		}
		if msgs[2].ToolCallID != "call_abc_def" {
			t.Errorf("ToolCallID=%q, want call_abc_def", msgs[2].ToolCallID)
		}
	})

	t.Run("TC5 orphaned tool result dropped", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: "Just a tool result"},
			{Role: "user", Content: []AnthropicContentBlock{
				{Type: "tool_result", ToolUseID: "nonexistent", Content: "orphan"},
			}},
		})
		// After sanitizeMessagesForProviders: dropOrphanedToolResults removes the tool message
		// with nonexistent ID. Then mergeConsecutiveSameRole merges the two user messages.
		if len(msgs) != 1 {
			t.Fatalf("len=%d, want 1 (merged user messages after dropping orphaned tool result)", len(msgs))
		}
		if msgs[0].Role != "user" {
			t.Errorf("msg[0].Role=%q, want user", msgs[0].Role)
		}
	})

	t.Run("TC6 orphaned tool_call gets dummy result", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: "Use tool please"},
			{Role: "assistant", Content: []AnthropicContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "test", Input: json.RawMessage(`{}`)},
			}},
		})
		// Should have: user, assistant (with tool_call), and a dummy tool result
		if len(msgs) < 3 {
			t.Fatalf("len=%d, want >= 3 (with dummy tool result)", len(msgs))
		}
		lastMsg := msgs[len(msgs)-1]
		if lastMsg.Role != "tool" {
			t.Errorf("last msg role=%q, want tool", lastMsg.Role)
		}
		if lastMsg.ToolCallID != "call_1" {
			t.Errorf("last msg ToolCallID=%q, want call_1", lastMsg.ToolCallID)
		}
		if s, _ := lastMsg.Content.(string); s != "." {
			t.Errorf("last msg Content=%q, want '.'", s)
		}
	})

	t.Run("TC7 consecutive same-role messages merged", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: "first"},
			{Role: "user", Content: "second"},
			{Role: "assistant", Content: "reply"},
			{Role: "user", Content: "third"},
		})
		// Two user messages merged, then assistant, then user: 3 total
		if len(msgs) != 3 {
			t.Fatalf("len=%d, want 3 (merged first two user messages)", len(msgs))
		}
		if s, _ := msgs[0].Content.(string); s != "first\nsecond" {
			t.Errorf("msg[0].Content=%q, want 'first\\nsecond'", s)
		}
		if msgs[1].Role != "assistant" {
			t.Errorf("msg[1].Role=%q, want assistant", msgs[1].Role)
		}
		if msgs[2].Role != "user" {
			t.Errorf("msg[2].Role=%q, want user", msgs[2].Role)
		}
	})

	t.Run("TC8 first non-system message ensured user", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "assistant", Content: "hello"},
		})
		if len(msgs) != 2 {
			t.Fatalf("len=%d, want 2 (inserted placeholder user)", len(msgs))
		}
		if msgs[0].Role != "user" {
			t.Errorf("msg[0].Role=%q, want user", msgs[0].Role)
		}
		if msgs[1].Role != "assistant" {
			t.Errorf("msg[1].Role=%q, want assistant", msgs[1].Role)
		}
	})

	t.Run("TC9 empty content replaced with dot", func(t *testing.T) {
		msgs := buildMessages([]AnthropicMessage{
			{Role: "user", Content: ""},
			{Role: "assistant", Content: ""},
		})
		if len(msgs) != 2 {
			t.Fatalf("len=%d, want 2", len(msgs))
		}
		if s, _ := msgs[0].Content.(string); s != "." {
			t.Errorf("msg[0].Content=%q, want '.'", s)
		}
		if s, _ := msgs[1].Content.(string); s != "." {
			t.Errorf("msg[1].Content=%q, want '.'", s)
		}
	})
}

// ──────────────────────────────────────────────
// decodeContentBlocks thinking block skip test
// ──────────────────────────────────────────────

func TestDecodeContentBlocks_SkipsThinking(t *testing.T) {
	input := []any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "thinking", "thinking": "thinking text"},
		map[string]any{"type": "redacted_thinking", "signature": "sig123"},
		map[string]any{"type": "text", "text": "world"},
	}
	blocks := decodeContentBlocks(input)
	if len(blocks) != 2 {
		t.Fatalf("len(blocks)=%d, want 2 (only text blocks)", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "hello" {
		t.Errorf("block[0] = %+v, want {text hello}", blocks[0])
	}
	if blocks[1].Type != "text" || blocks[1].Text != "world" {
		t.Errorf("block[1] = %+v, want {text world}", blocks[1])
	}
}

// ──────────────────────────────────────────────
// AnthropicToChatRequest end-to-end with sanitization
// ──────────────────────────────────────────────

func TestAnthropicToChatRequest_Sanitization(t *testing.T) {
	anthReq := &AnthropicMessagesRequest{
		Model: "claude-3-sonnet",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "hello"},
		},
	}
	req := AnthropicToChatRequest(anthReq)
	if req == nil {
		t.Fatal("req is nil")
	}
	if req.Model != "claude-3-sonnet" {
		t.Errorf("Model=%q, want claude-3-sonnet", req.Model)
	}
	if len(req.Messages) == 0 {
		t.Fatal("no messages")
	}
	if s, _ := req.Messages[0].Content.(string); s != "hello" {
		t.Errorf("msg[0].Content=%q, want 'hello'", s)
	}
}

func TestAnthropicToChatRequest_WithToolUseAndResult(t *testing.T) {
	anthReq := &AnthropicMessagesRequest{
		Model: "claude-3-sonnet",
		Messages: []AnthropicMessage{
			{Role: "user", Content: "What's the weather?"},
			{Role: "assistant", Content: []AnthropicContentBlock{
				{Type: "text", Text: "Let me check"},
				{Type: "tool_use", ID: "tu_123", Name: "get_weather", Input: json.RawMessage(`{"loc":"NYC"}`)},
			}},
			{Role: "user", Content: []AnthropicContentBlock{
				{Type: "tool_result", ToolUseID: "tu_123", Content: "72°F"},
			}},
		},
	}
	req := AnthropicToChatRequest(anthReq)
	if req == nil {
		t.Fatal("req is nil")
	}
	// Expected: user("What's the weather?"), assistant(text + tool_use), tool(result)
	if len(req.Messages) != 3 {
		t.Fatalf("len(messages)=%d, want 3", len(req.Messages))
	}
	if req.Messages[1].Role != "assistant" {
		t.Errorf("msg[1].Role=%q, want assistant", req.Messages[1].Role)
	}
	if len(req.Messages[1].ToolCalls) != 1 {
		t.Fatalf("msg[1].ToolCalls len=%d, want 1", len(req.Messages[1].ToolCalls))
	}
	if req.Messages[1].ToolCalls[0].ID != "tu_123" {
		t.Errorf("ToolCall ID=%q, want tu_123", req.Messages[1].ToolCalls[0].ID)
	}
	if req.Messages[2].Role != "tool" {
		t.Errorf("msg[2].Role=%q, want tool", req.Messages[2].Role)
	}
	if req.Messages[2].ToolCallID != "tu_123" {
		t.Errorf("msg[2].ToolCallID=%q, want tu_123", req.Messages[2].ToolCallID)
	}
	if s, _ := req.Messages[2].Content.(string); s != "72°F" {
		t.Errorf("msg[2].Content=%q, want '72°F'", s)
	}
}

// ──────────────────────────────────────────────
// asContentParts tests
// ──────────────────────────────────────────────

func TestAsContentParts(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int // expected length
	}{
		{name: "string", input: "hello", expected: 1},
		{name: "empty string", input: "", expected: 0},
		{name: "nil", input: nil, expected: 0},
		{name: "[]ContentPart", input: []ContentPart{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}, expected: 2},
		{name: "[]any text maps", input: []any{map[string]any{"type": "text", "text": "a"}}, expected: 1},
		{name: "int", input: 42, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := asContentParts(tt.input)
			if len(got) != tt.expected {
				t.Errorf("len=%d, want %d, got=%+v", len(got), tt.expected, got)
			}
		})
	}
}

// ──────────────────────────────────────────────
// mergeContent tests
// ──────────────────────────────────────────────

func TestMergeContent(t *testing.T) {
	tests := []struct {
		name string
		a, b any
	}{
		{name: "both strings", a: "hello", b: "world"},
		{name: "first empty", a: "", b: "world"},
		{name: "second empty", a: "hello", b: ""},
		{name: "both nil", a: nil, b: nil},
		{name: "first nil", a: nil, b: "world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeContent(tt.a, tt.b)
			if got == nil {
				return
			}
			_ = got
		})
	}
}
