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

func TestAnthropicToChatRequest_ToolResultNestedContentPreservesCacheControl(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 1024,
		"messages": [{
			"role": "user",
			"content": [{
				"type": "tool_result",
				"tool_use_id": "toolu_nested",
				"cache_control": {"type": "ephemeral"},
				"content": [
					{"type": "text", "text": "result text", "cache_control": {"type": "ephemeral"}},
					{"type": "image", "source": {"type": "url", "url": "https://example.test/result.png"}, "cache_control": {"type": "ephemeral"}}
				]
			}]
		}]
	}`)

	var anthReq AnthropicMessagesRequest
	if err := json.Unmarshal(raw, &anthReq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	req := AnthropicToChatRequest(&anthReq)
	if len(req.Messages) != 1 {
		t.Fatalf("messages: got %d want 1", len(req.Messages))
	}
	toolMsg := req.Messages[0]
	if toolMsg.Role != "tool" {
		t.Fatalf("role: got %q want tool", toolMsg.Role)
	}
	if got := toolMsg.ExtraFields.Lookup("cache_control"); len(got) == 0 {
		t.Fatal("tool_result cache_control missing from Message.ExtraFields")
	}

	parts, ok := NormalizeContentParts(toolMsg.Content)
	if !ok {
		t.Fatalf("tool_result content: got %T want []ContentPart", toolMsg.Content)
	}
	if len(parts) != 2 {
		t.Fatalf("tool_result parts: got %d want 2", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "result text" {
		t.Errorf("text part: got %+v", parts[0])
	}
	if got := parts[0].ExtraFields.Lookup("cache_control"); len(got) == 0 {
		t.Error("nested text cache_control missing from ContentPart.ExtraFields")
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "https://example.test/result.png" {
		t.Errorf("image part: got %+v", parts[1])
	}
	if got := parts[1].ExtraFields.Lookup("cache_control"); len(got) == 0 {
		t.Error("nested image cache_control missing from ContentPart.ExtraFields")
	}
}
