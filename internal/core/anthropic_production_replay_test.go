package core

import (
	"encoding/json"
	"testing"
)

// productionFixture holds the exact two messages extracted from production
// request d5f69282-c314-425f-b746-f9d9869a8d3b (2026-07-16).
//
// Before the fix, the user message [tool_result, text] was converted to:
//
//	assistant (tool_calls: call_sB80VJ5yd4ytNXYHXUD8U5vf)
//	user (text with cache_control)          ← WRONG ORDER
//	tool (tool_call_id=call_sB80VJ5yd4ytNXYHXUD8U5vf)
//
// Azure rejected this with:
//
//	"An assistant message with 'tool_calls' must be followed by tool messages..."
//
// After the fix the order must be:
//
//	assistant (tool_calls)
//	tool                                    ← immediately after assistant
//	user (text with cache_control)          ← trailing user turn
const productionFixture = `[
  {
    "role": "assistant",
    "content": [
      {
        "id": "call_sB80VJ5yd4ytNXYHXUD8U5vf",
        "name": "Skill",
        "type": "tool_use",
        "input": {
          "args": "用户问：你是什么模型？",
          "skill": "claude-api"
        }
      }
    ]
  },
  {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "content": "Launching skill: claude-api",
        "tool_use_id": "call_sB80VJ5yd4ytNXYHXUD8U5vf"
      },
      {
        "text": "Base directory for this skill: /tmp/claude/bundled-skills/claude-api\n\n# Building LLM-Powered Applications with Claude\nThis skill helps you build LLM-powered applications.",
        "type": "text",
        "cache_control": {
          "type": "ephemeral"
        }
      }
    ]
  }
]`

// TestProductionReplay_ToolResultOrderingFixed replays the exact production
// message sequence and asserts the converted OpenAI messages are valid:
// the tool message must immediately follow the assistant message, and the
// user text message must come last.
func TestProductionReplay_ToolResultOrderingFixed(t *testing.T) {
	var anthMsgs []AnthropicMessage
	if err := json.Unmarshal([]byte(productionFixture), &anthMsgs); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	msgs, err := convertAnthropicMessages(anthMsgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	// Expected:
	//   [0] assistant  {ToolCalls: [{ID: call_sB80VJ5yd4ytNXYHXUD8U5vf}]}
	//   [1] tool       {ToolCallID: call_sB80VJ5yd4ytNXYHXUD8U5vf}
	//   [2] user       {Content: "Base directory..."}
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d: %v", len(msgs), roles(msgs))
	}

	// [0] assistant with tool_calls
	if msgs[0].Role != "assistant" {
		t.Errorf("[0] want assistant, got %q", msgs[0].Role)
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Errorf("[0] want 1 ToolCall, got %d", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].ID != "call_sB80VJ5yd4ytNXYHXUD8U5vf" {
		t.Errorf("[0] ToolCall ID mismatch: %q", msgs[0].ToolCalls[0].ID)
	}

	// [1] tool — must be IMMEDIATELY after assistant, not after user text
	if msgs[1].Role != "tool" {
		t.Errorf("[1] want tool (must follow assistant directly), got %q — Azure would reject this", msgs[1].Role)
	}
	if msgs[1].ToolCallID != "call_sB80VJ5yd4ytNXYHXUD8U5vf" {
		t.Errorf("[1] ToolCallID mismatch: %q", msgs[1].ToolCallID)
	}
	content, _ := msgs[1].Content.(string)
	if content != "Launching skill: claude-api" {
		t.Errorf("[1] tool content mismatch: %q", content)
	}

	// [2] user text — comes AFTER tool result
	if msgs[2].Role != "user" {
		t.Errorf("[2] want user, got %q", msgs[2].Role)
	}

	// Verify the user text block preserved its cache_control in ExtraFields
	parts, ok := msgs[2].Content.([]ContentPart)
	if !ok || len(parts) == 0 {
		t.Errorf("[2] want ContentPart slice (for cache_control), got %T: %v", msgs[2].Content, msgs[2].Content)
	} else {
		cc := parts[0].ExtraFields.Lookup("cache_control")
		if len(cc) == 0 {
			t.Errorf("[2] cache_control lost from user text block")
		}
	}

	t.Logf("Production replay OK: %v", roles(msgs))
}

// TestProductionReplay_SanitizeStillPasses verifies that running SanitizeToolMessages
// on the already-correct output does not break anything (idempotency check).
func TestProductionReplay_SanitizeStillPasses(t *testing.T) {
	var anthMsgs []AnthropicMessage
	if err := json.Unmarshal([]byte(productionFixture), &anthMsgs); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	converted, err := convertAnthropicMessages(anthMsgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	sanitized := SanitizeToolMessages(converted)

	if len(sanitized) != len(converted) {
		t.Errorf("SanitizeToolMessages changed message count: %d -> %d", len(converted), len(sanitized))
	}
	for i, m := range sanitized {
		if m.Role != converted[i].Role {
			t.Errorf("[%d] role changed by sanitizer: %q -> %q", i, converted[i].Role, m.Role)
		}
	}
	if sanitized[1].Role != "tool" || sanitized[1].ToolCallID != "call_sB80VJ5yd4ytNXYHXUD8U5vf" {
		t.Errorf("tool message corrupted by sanitizer: %+v", sanitized[1])
	}
}
