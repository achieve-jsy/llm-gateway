package core

import (
	"encoding/json"
	"testing"
)

func TestAnthropicToChatRequest_PreservesCacheControl(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 1024,
		"stream": true,
		"system": [
			{"type": "text", "text": "billing header"},
			{"type": "text", "text": "You are Claude Code.", "cache_control": {"type": "ephemeral"}}
		],
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "hello"}
				]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": {"command": "ls"}}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": "ok",
						"cache_control": {"type": "ephemeral"}
					}
				]
			}
		],
		"tools": [
			{
				"name": "Bash",
				"description": "run shell",
				"input_schema": {"type": "object", "properties": {}},
				"cache_control": {"type": "ephemeral"}
			}
		]
	}`)

	var anthReq AnthropicMessagesRequest
	if err := json.Unmarshal(raw, &anthReq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	req := AnthropicToChatRequest(&anthReq)
	if req == nil {
		t.Fatal("nil ChatRequest")
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		t.Fatalf("expected leading system message, got %+v", req.Messages)
	}

	sysParts, ok := NormalizeContentParts(req.Messages[0].Content)
	if !ok {
		t.Fatalf("system content should be ContentParts, got %T", req.Messages[0].Content)
	}
	if len(sysParts) != 2 {
		t.Fatalf("system parts: got %d want 2", len(sysParts))
	}
	if sysParts[0].ExtraFields.Lookup("cache_control") != nil {
		t.Error("first system block should not have cache_control")
	}
	if sysParts[1].ExtraFields.Lookup("cache_control") == nil {
		t.Error("second system block missing cache_control ExtraFields")
	}

	var toolMsg *Message
	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			toolMsg = &req.Messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected tool message from tool_result")
	}
	if toolMsg.ExtraFields.Lookup("cache_control") == nil {
		t.Error("tool_result cache_control not preserved on Message.ExtraFields")
	}

	if len(req.Tools) != 1 {
		t.Fatalf("tools: got %d want 1", len(req.Tools))
	}
	if req.Tools[0]["cache_control"] == nil {
		t.Error("tool cache_control not preserved")
	}
}

func TestAnthropicToChatRequest_SingleTextWithCacheControl_NotCollapsed(t *testing.T) {
	anthReq := &AnthropicMessagesRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 16,
		Messages: []AnthropicMessage{{
			Role: "user",
			Content: []AnthropicContentBlock{{
				Type:         "text",
				Text:         "hello",
				CacheControl: json.RawMessage(`{"type":"ephemeral"}`),
			}},
		}},
	}
	req := AnthropicToChatRequest(anthReq)
	if len(req.Messages) != 1 {
		t.Fatalf("messages: got %d", len(req.Messages))
	}
	parts, ok := NormalizeContentParts(req.Messages[0].Content)
	if !ok {
		t.Fatalf("expected ContentParts to retain cache_control, got %T %#v", req.Messages[0].Content, req.Messages[0].Content)
	}
	if parts[0].ExtraFields.Lookup("cache_control") == nil {
		t.Error("cache_control lost after single-part collapse")
	}
}
