package anthropic

import (
	"encoding/json"
	"testing"
)

func TestTrimExcessCacheControl_UnderLimit(t *testing.T) {
	cc := json.RawMessage(`{"type":"ephemeral"}`)
	req := &anthropicRequest{
		System: []anthropicContentBlock{
			{Type: "text", Text: "sys", CacheControl: cc},
		},
		Messages: []anthropicMessage{
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "hello", CacheControl: cc},
			}},
			{Role: "assistant", Content: []anthropicContentBlock{
				{Type: "text", Text: "hi", CacheControl: cc},
			}},
		},
	}
	// 3 markers, limit 4 → no change
	trimExcessCacheControl(req, 4)

	sysBlocks := req.System.([]anthropicContentBlock)
	if len(sysBlocks[0].CacheControl) == 0 {
		t.Error("system cache_control should be preserved")
	}
	msgs := req.Messages
	if b := msgs[0].Content.([]anthropicContentBlock); len(b[0].CacheControl) == 0 {
		t.Error("user message cache_control should be preserved")
	}
	if b := msgs[1].Content.([]anthropicContentBlock); len(b[0].CacheControl) == 0 {
		t.Error("assistant message cache_control should be preserved")
	}
}

func TestTrimExcessCacheControl_ExactlyAtLimit(t *testing.T) {
	cc := json.RawMessage(`{"type":"ephemeral"}`)
	req := &anthropicRequest{
		System: []anthropicContentBlock{
			{Type: "text", Text: "sys", CacheControl: cc},
		},
		Messages: []anthropicMessage{
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "m1", CacheControl: cc},
			}},
			{Role: "assistant", Content: []anthropicContentBlock{
				{Type: "text", Text: "m2", CacheControl: cc},
			}},
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "m3", CacheControl: cc},
			}},
		},
	}
	// 4 markers, limit 4 → no change
	trimExcessCacheControl(req, 4)

	count := countCacheControl(req)
	if count != 4 {
		t.Errorf("expected 4 cache_control markers, got %d", count)
	}
}

func TestTrimExcessCacheControl_OverLimit(t *testing.T) {
	cc := json.RawMessage(`{"type":"ephemeral"}`)
	req := &anthropicRequest{
		System: []anthropicContentBlock{
			{Type: "text", Text: "sys", CacheControl: cc},
		},
		Messages: []anthropicMessage{
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "m1", CacheControl: cc},
			}},
			{Role: "assistant", Content: []anthropicContentBlock{
				{Type: "text", Text: "m2", CacheControl: cc},
			}},
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "m3", CacheControl: cc},
			}},
			{Role: "assistant", Content: []anthropicContentBlock{
				{Type: "text", Text: "m4", CacheControl: cc},
			}},
		},
	}
	// 5 markers (1 system + 4 messages), limit 4 → remove earliest message marker
	trimExcessCacheControl(req, 4)

	count := countCacheControl(req)
	if count != 4 {
		t.Errorf("expected 4 cache_control markers after trim, got %d", count)
	}

	// System should be preserved
	sysBlocks := req.System.([]anthropicContentBlock)
	if len(sysBlocks[0].CacheControl) == 0 {
		t.Error("system cache_control should be preserved")
	}

	// First message marker should be removed (lowest priority)
	if b := req.Messages[0].Content.([]anthropicContentBlock); len(b[0].CacheControl) != 0 {
		t.Error("earliest message cache_control should be removed")
	}

	// Last message marker should be preserved (highest priority)
	if b := req.Messages[3].Content.([]anthropicContentBlock); len(b[0].CacheControl) == 0 {
		t.Error("latest message cache_control should be preserved")
	}
}

func TestTrimExcessCacheControl_ManyOverLimit(t *testing.T) {
	cc := json.RawMessage(`{"type":"ephemeral"}`)
	req := &anthropicRequest{
		System: []anthropicContentBlock{
			{Type: "text", Text: "sys1", CacheControl: cc},
			{Type: "text", Text: "sys2", CacheControl: cc},
		},
		Messages: []anthropicMessage{
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "m1", CacheControl: cc},
			}},
			{Role: "assistant", Content: []anthropicContentBlock{
				{Type: "text", Text: "m2", CacheControl: cc},
			}},
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "m3", CacheControl: cc},
			}},
			{Role: "assistant", Content: []anthropicContentBlock{
				{Type: "text", Text: "m4", CacheControl: cc},
			}},
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "m5", CacheControl: cc},
			}},
		},
	}
	// 7 markers (2 system + 5 messages), limit 4 → keep 2 system + 2 latest messages
	trimExcessCacheControl(req, 4)

	count := countCacheControl(req)
	if count != 4 {
		t.Errorf("expected 4 cache_control markers after trim, got %d", count)
	}

	// Both system markers preserved
	sysBlocks := req.System.([]anthropicContentBlock)
	if len(sysBlocks[0].CacheControl) == 0 || len(sysBlocks[1].CacheControl) == 0 {
		t.Error("system cache_control markers should be preserved")
	}

	// First 3 message markers removed, last 2 preserved
	if b := req.Messages[0].Content.([]anthropicContentBlock); len(b[0].CacheControl) != 0 {
		t.Error("message[0] cache_control should be removed")
	}
	if b := req.Messages[1].Content.([]anthropicContentBlock); len(b[0].CacheControl) != 0 {
		t.Error("message[1] cache_control should be removed")
	}
	if b := req.Messages[2].Content.([]anthropicContentBlock); len(b[0].CacheControl) != 0 {
		t.Error("message[2] cache_control should be removed")
	}
	if b := req.Messages[3].Content.([]anthropicContentBlock); len(b[0].CacheControl) == 0 {
		t.Error("message[3] cache_control should be preserved")
	}
	if b := req.Messages[4].Content.([]anthropicContentBlock); len(b[0].CacheControl) == 0 {
		t.Error("message[4] cache_control should be preserved")
	}
}

func TestTrimExcessCacheControl_NoMarkers(t *testing.T) {
	req := &anthropicRequest{
		System: "plain system prompt",
		Messages: []anthropicMessage{
			{Role: "user", Content: "hello"},
		},
	}
	// Should not panic
	trimExcessCacheControl(req, 4)
}

func TestTrimExcessCacheControl_MultipleBlocksPerMessage(t *testing.T) {
	cc := json.RawMessage(`{"type":"ephemeral"}`)
	req := &anthropicRequest{
		Messages: []anthropicMessage{
			{Role: "user", Content: []anthropicContentBlock{
				{Type: "text", Text: "part1", CacheControl: cc},
				{Type: "text", Text: "part2", CacheControl: cc},
				{Type: "text", Text: "part3", CacheControl: cc},
			}},
			{Role: "assistant", Content: []anthropicContentBlock{
				{Type: "text", Text: "reply1", CacheControl: cc},
				{Type: "text", Text: "reply2", CacheControl: cc},
			}},
		},
	}
	// 5 markers, limit 4 → remove earliest (part1)
	trimExcessCacheControl(req, 4)

	count := countCacheControl(req)
	if count != 4 {
		t.Errorf("expected 4 cache_control markers after trim, got %d", count)
	}

	// First block of first message removed
	blocks := req.Messages[0].Content.([]anthropicContentBlock)
	if len(blocks[0].CacheControl) != 0 {
		t.Error("earliest block cache_control should be removed")
	}
	// Later blocks preserved
	if len(blocks[1].CacheControl) == 0 {
		t.Error("block[1] cache_control should be preserved")
	}
}

// countCacheControl counts all cache_control markers in a request.
func countCacheControl(req *anthropicRequest) int {
	count := 0
	if blocks, ok := req.System.([]anthropicContentBlock); ok {
		for _, b := range blocks {
			if len(b.CacheControl) > 0 {
				count++
			}
		}
	}
	for _, msg := range req.Messages {
		if blocks, ok := msg.Content.([]anthropicContentBlock); ok {
			for _, b := range blocks {
				if len(b.CacheControl) > 0 {
					count++
				}
			}
		}
	}
	return count
}
