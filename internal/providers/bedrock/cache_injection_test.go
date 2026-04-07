package bedrock

import (
	"encoding/json"
	"fmt"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/core"
)

// ─── helpers ───────────────────────────────────────────────────────────────

func makeTextMessage(role, text string) core.Message {
	return core.Message{Role: role, Content: text}
}

// makeMessageWithCacheControl builds a message whose content is a []any of
// content-part maps carrying a cache_control extra field — the same shape the
// JSON decoder produces when a client attaches cache_control markers.
func makeMessageWithCacheControl(role, text string) core.Message {
	return core.Message{
		Role: role,
		Content: []any{
			map[string]any{
				"type":          "text",
				"text":          text,
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
	}
}

func countCachePoints(messages []brtypes.Message) int {
	n := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if _, ok := block.(*brtypes.ContentBlockMemberCachePoint); ok {
				n++
			}
		}
	}
	return n
}

func countToolCachePoints(toolCfg *brtypes.ToolConfiguration) int {
	if toolCfg == nil {
		return 0
	}
	n := 0
	for _, t := range toolCfg.Tools {
		if _, ok := t.(*brtypes.ToolMemberCachePoint); ok {
			n++
		}
	}
	return n
}

// makeTools builds N tool definitions in the JSON-decoded map shape that
// convertTools expects. The name must be a plain string (not *string).
func makeTools(count int) []map[string]any {
	tools := make([]map[string]any, count)
	for i := range tools {
		tools[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        fmt.Sprintf("tool_%c", 'a'+i),
				"description": "a tool",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		}
	}
	return tools
}

// suppress unused-import warning for json (used indirectly via core types)
var _ = json.Marshal

// ─── hasCacheControlInMessages ────────────────────────────────────────────

func TestHasCacheControlInMessages_NoMarkers(t *testing.T) {
	msgs := []core.Message{
		makeTextMessage("system", "You are a helpful assistant."),
		makeTextMessage("user", "Hello"),
		makeTextMessage("assistant", "Hi there"),
	}
	if hasCacheControlInMessages(msgs) {
		t.Error("expected no cache_control markers")
	}
}

func TestHasCacheControlInMessages_WithMarker(t *testing.T) {
	msgs := []core.Message{
		makeMessageWithCacheControl("system", "You are a helpful assistant."),
		makeTextMessage("user", "Hello"),
	}
	if !hasCacheControlInMessages(msgs) {
		t.Error("expected cache_control marker to be detected")
	}
}

// ─── injectToolCachePoint ─────────────────────────────────────────────────

func TestInjectToolCachePoint_NilConfig(t *testing.T) {
	injectToolCachePoint(nil) // should not panic
}

func TestInjectToolCachePoint_EmptyTools(t *testing.T) {
	cfg := &brtypes.ToolConfiguration{Tools: []brtypes.Tool{}}
	injectToolCachePoint(cfg)
	if len(cfg.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(cfg.Tools))
	}
}

func TestInjectToolCachePoint_WithTools(t *testing.T) {
	cfg := &brtypes.ToolConfiguration{
		Tools: []brtypes.Tool{
			&brtypes.ToolMemberToolSpec{
				Value: brtypes.ToolSpecification{Name: awssdk.String("tool_a")},
			},
			&brtypes.ToolMemberToolSpec{
				Value: brtypes.ToolSpecification{Name: awssdk.String("tool_b")},
			},
		},
	}
	injectToolCachePoint(cfg)
	if got := countToolCachePoints(cfg); got != 1 {
		t.Errorf("expected 1 tool CachePoint, got %d", got)
	}
	if len(cfg.Tools) != 3 {
		t.Errorf("expected 3 tools (2 specs + 1 CachePoint), got %d", len(cfg.Tools))
	}
}

// ─── injectConversationCachePoints ────────────────────────────────────────

func TestInjectConversationCachePoints_ShortConversation(t *testing.T) {
	// Fewer than conversationCacheMinMessages → no injection
	messages := []brtypes.Message{
		{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
			&brtypes.ContentBlockMemberText{Value: "hello"},
		}},
		{Role: brtypes.ConversationRoleAssistant, Content: []brtypes.ContentBlock{
			&brtypes.ContentBlockMemberText{Value: "hi"},
		}},
	}
	injectConversationCachePoints(messages)
	if got := countCachePoints(messages); got != 0 {
		t.Errorf("expected 0 CachePoints for short conversation, got %d", got)
	}
}

func TestInjectConversationCachePoints_LongConversation(t *testing.T) {
	// Build a 20-message conversation (well above the threshold)
	messages := make([]brtypes.Message, 20)
	for i := range messages {
		role := brtypes.ConversationRoleUser
		if i%2 == 1 {
			role = brtypes.ConversationRoleAssistant
		}
		messages[i] = brtypes.Message{
			Role: role,
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "message"},
			},
		}
	}
	injectConversationCachePoints(messages)
	if got := countCachePoints(messages); got != 1 {
		t.Errorf("expected 1 CachePoint for long conversation, got %d", got)
	}
}

func TestInjectConversationCachePoints_ExactThreshold(t *testing.T) {
	messages := make([]brtypes.Message, conversationCacheMinMessages)
	for i := range messages {
		role := brtypes.ConversationRoleUser
		if i%2 == 1 {
			role = brtypes.ConversationRoleAssistant
		}
		messages[i] = brtypes.Message{
			Role: role,
			Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "msg"},
			},
		}
	}
	injectConversationCachePoints(messages)
	if got := countCachePoints(messages); got != 1 {
		t.Errorf("expected 1 CachePoint at threshold, got %d", got)
	}
}

// ─── buildConverseParts (integration) ─────────────────────────────────────

func TestBuildConverseParts_AutoInject_WithoutClientCacheControl(t *testing.T) {
	// Simulate a user like 徐耿皓: many messages, few tools, no cache_control
	msgs := []core.Message{makeTextMessage("system", "You are helpful.")}
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, makeTextMessage(role, "turn "+string(rune('0'+i))))
	}

	req := &core.ChatRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: msgs,
		Tools:    makeTools(3),
	}

	parts, err := buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}

	// Tool CachePoint should be injected
	if got := countToolCachePoints(parts.toolCfg); got != 1 {
		t.Errorf("expected 1 tool CachePoint, got %d", got)
	}

	// Conversation CachePoint should be injected (20 messages > threshold)
	if got := countCachePoints(parts.messages); got != 1 {
		t.Errorf("expected 1 conversation CachePoint, got %d", got)
	}
}

func TestBuildConverseParts_NoAutoInject_WithClientCacheControl(t *testing.T) {
	// Simulate a user like 刘思远: client manages its own cache_control
	msgs := []core.Message{
		makeMessageWithCacheControl("system", "You are helpful."),
		makeTextMessage("user", "hello"),
		makeTextMessage("assistant", "hi"),
	}

	req := &core.ChatRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: msgs,
		Tools:    makeTools(3),
	}

	parts, err := buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}

	// Should NOT auto-inject because client already has cache_control
	if got := countToolCachePoints(parts.toolCfg); got != 0 {
		t.Errorf("expected 0 auto tool CachePoints (client manages), got %d", got)
	}
	if got := countCachePoints(parts.messages); got != 0 {
		t.Errorf("expected 0 auto conversation CachePoints (client manages), got %d", got)
	}
}

func TestBuildConverseParts_ShortConversation_NoConversationCachePoint(t *testing.T) {
	// Short conversation: only system + 1 turn → no conversation CachePoint
	msgs := []core.Message{
		makeTextMessage("system", "You are helpful."),
		makeTextMessage("user", "hello"),
		makeTextMessage("assistant", "hi"),
	}

	req := &core.ChatRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: msgs,
		Tools:    makeTools(3),
	}

	parts, err := buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}

	// Tool CachePoint should still be injected
	if got := countToolCachePoints(parts.toolCfg); got != 1 {
		t.Errorf("expected 1 tool CachePoint, got %d", got)
	}

	// But no conversation CachePoint (only 2 messages after system extraction)
	if got := countCachePoints(parts.messages); got != 0 {
		t.Errorf("expected 0 conversation CachePoints for short conv, got %d", got)
	}
}

func TestBuildConverseParts_NoTools_NoToolCachePoint(t *testing.T) {
	msgs := []core.Message{
		makeTextMessage("system", "You are helpful."),
		makeTextMessage("user", "hello"),
	}

	req := &core.ChatRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: msgs,
	}

	parts, err := buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}

	if parts.toolCfg != nil {
		t.Error("expected nil toolCfg when no tools provided")
	}
}
