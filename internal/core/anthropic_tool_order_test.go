package core

import (
	"encoding/json"
	"testing"
)

// TestFlattenAnthropicBlocks_ToolResultBeforeText verifies that a user message
// containing [tool_result, text] produces OpenAI messages in the order:
//
//	role:"tool" (matching the prior assistant tool_call)
//	role:"user" (the accompanying text)
//
// This prevents the Azure/OpenAI error:
//
//	"An assistant message with 'tool_calls' must be followed by tool messages..."
//
// which occurs when the user text message is emitted before the tool result.
func TestFlattenAnthropicBlocks_ToolResultBeforeText(t *testing.T) {
	blocks := []AnthropicContentBlock{
		{
			Type:      "tool_result",
			ToolUseID: "call_sB80VJ5yd4ytNXYHXUD8U5vf",
			Content:   "42",
		},
		{
			Type: "text",
			Text: "Here is the answer.",
		},
	}

	msgs, err := flattenAnthropicBlocksToMessages("user", blocks)
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), roles(msgs))
	}
	if msgs[0].Role != "tool" {
		t.Errorf("first message must be role 'tool', got %q", msgs[0].Role)
	}
	if msgs[0].ToolCallID != "call_sB80VJ5yd4ytNXYHXUD8U5vf" {
		t.Errorf("tool_call_id mismatch: %q", msgs[0].ToolCallID)
	}
	if msgs[1].Role != "user" {
		t.Errorf("second message must be role 'user', got %q", msgs[1].Role)
	}
	content, _ := msgs[1].Content.(string)
	if content != "Here is the answer." {
		t.Errorf("user text mismatch: %q", content)
	}
}

// TestFlattenAnthropicBlocks_ToolResultOnly verifies that a user message
// containing only a tool_result block produces a single role:"tool" message.
func TestFlattenAnthropicBlocks_ToolResultOnly(t *testing.T) {
	blocks := []AnthropicContentBlock{
		{
			Type:      "tool_result",
			ToolUseID: "call_abc",
			Content:   "result data",
		},
	}

	msgs, err := flattenAnthropicBlocksToMessages("user", blocks)
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "tool" || msgs[0].ToolCallID != "call_abc" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}
}

// TestFlattenAnthropicBlocks_MultipleToolResultsThenText verifies parallel tool
// results followed by a text block are ordered: all tool messages first, then user.
func TestFlattenAnthropicBlocks_MultipleToolResultsThenText(t *testing.T) {
	blocks := []AnthropicContentBlock{
		{Type: "tool_result", ToolUseID: "call_1", Content: "res1"},
		{Type: "tool_result", ToolUseID: "call_2", Content: "res2"},
		{Type: "text", Text: "Done."},
	}

	msgs, err := flattenAnthropicBlocksToMessages("user", blocks)
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %v", len(msgs), roles(msgs))
	}
	if msgs[0].Role != "tool" || msgs[0].ToolCallID != "call_1" {
		t.Errorf("first message wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "call_2" {
		t.Errorf("second message wrong: %+v", msgs[1])
	}
	if msgs[2].Role != "user" {
		t.Errorf("third message must be user, got %q", msgs[2].Role)
	}
}

// TestAnthropicToChatRequest_ToolResultTextOrdering is an end-to-end test that
// feeds a full AnthropicMessagesRequest (JSON-decoded) through AnthropicToChatRequest
// and checks that the final []Message slice has the correct tool-before-text order.
// This mirrors the production payload for request d5f69282-c314-425f-b746-f9d9869a8d3b.
func TestAnthropicToChatRequest_ToolResultTextOrdering(t *testing.T) {
	raw := []byte(`{
		"model": "gpt-5.5",
		"max_tokens": 1024,
		"messages": [
			{
				"role": "user",
				"content": "Can you help?"
			},
			{
				"role": "assistant",
				"content": [
					{
						"type": "tool_use",
						"id": "call_sB80VJ5yd4ytNXYHXUD8U5vf",
						"name": "some_tool",
						"input": {"q": "test"}
					}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "call_sB80VJ5yd4ytNXYHXUD8U5vf",
						"content": "tool output"
					},
					{
						"type": "text",
						"text": "Thanks, now continue."
					}
				]
			}
		]
	}`)

	var anthReq AnthropicMessagesRequest
	if err := json.Unmarshal(raw, &anthReq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	chatReq, err := AnthropicToChatRequest(&anthReq)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	msgs := chatReq.Messages

	// Expected sequence (no system):
	//   [0] user "Can you help?"
	//   [1] assistant {tool_calls: [call_sB80VJ5yd4ytNXYHXUD8U5vf]}
	//   [2] tool  tool_call_id=call_sB80VJ5yd4ytNXYHXUD8U5vf
	//   [3] user  "Thanks, now continue."
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(msgs), roles(msgs))
	}

	// Validate order
	if msgs[0].Role != "user" {
		t.Errorf("[0] want user, got %q", msgs[0].Role)
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) == 0 {
		t.Errorf("[1] want assistant with tool_calls, got %+v", msgs[1])
	}
	if msgs[2].Role != "tool" {
		t.Errorf("[2] want tool, got %q — Azure would reject this ordering", msgs[2].Role)
	}
	if msgs[2].ToolCallID != "call_sB80VJ5yd4ytNXYHXUD8U5vf" {
		t.Errorf("[2] tool_call_id mismatch: %q", msgs[2].ToolCallID)
	}
	if msgs[3].Role != "user" {
		t.Errorf("[3] want user, got %q", msgs[3].Role)
	}
	userText, _ := msgs[3].Content.(string)
	if userText != "Thanks, now continue." {
		t.Errorf("[3] user text mismatch: %q", userText)
	}
}
