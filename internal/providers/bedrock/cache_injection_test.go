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

func TestHasCacheControlInMessages_ToolResultExtraFields(t *testing.T) {
	msgs := []core.Message{{
		Role:       "tool",
		ToolCallID: "t1",
		Content:    "ok",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"cache_control": json.RawMessage(`{"type":"ephemeral"}`),
		}),
	}}
	if !hasCacheControlInMessages(msgs) {
		t.Error("expected Message.ExtraFields cache_control to be detected")
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

	parts, err := (&Provider{}).buildConverseParts(req)
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
	// 客户端自管 message 断点时：不注入 conversation CachePoint；
	// 但 tools 未显式标记时仍自动注入 tool CachePoint（兼容 Claude Desktop 漏标 tools）。
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

	parts, err := (&Provider{}).buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}

	if got := countToolCachePoints(parts.toolCfg); got != 1 {
		t.Errorf("expected 1 auto tool CachePoint when tools lack cache_control, got %d", got)
	}
	if got := countCachePoints(parts.messages); got != 0 {
		t.Errorf("expected 0 auto conversation CachePoints (client manages messages), got %d", got)
	}
	// system 上的显式 cache_control 应落成 CachePoint
	if got := countSystemCachePoints(parts.system); got != 1 {
		t.Errorf("expected 1 system CachePoint from client marker, got %d", got)
	}
}

func countSystemCachePoints(system []brtypes.SystemContentBlock) int {
	n := 0
	for _, b := range system {
		if _, ok := b.(*brtypes.SystemContentBlockMemberCachePoint); ok {
			n++
		}
	}
	return n
}

func TestBuildConverseParts_MessagesProtocol_ToolResultCacheControl(t *testing.T) {
	// 模拟 /v1/messages → AnthropicToChatRequest 后的形态：
	// system ContentParts 带 cache_control；最后一条 tool 消息 ExtraFields 带 cache_control。
	sysParts := []core.ContentPart{{
		Type: "text",
		Text: "You are Claude Code.",
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"cache_control": json.RawMessage(`{"type":"ephemeral"}`),
		}),
	}}
	toolMsg := core.Message{
		Role:       "tool",
		ToolCallID: "toolu_1",
		Content:    `{"ok":true}`,
		ExtraFields: core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
			"cache_control": json.RawMessage(`{"type":"ephemeral"}`),
		}),
	}
	req := &core.ChatRequest{
		Model: "anthropic.claude-sonnet-4-6",
		Messages: []core.Message{
			{Role: "system", Content: sysParts},
			{Role: "user", Content: "run"},
			{
				Role: "assistant",
				ToolCalls: []core.ToolCall{{
					ID:   "toolu_1",
					Type: "function",
					Function: core.FunctionCall{
						Name:      "Bash",
						Arguments: `{"command":"ls"}`,
					},
				}},
			},
			toolMsg,
		},
		Tools: makeTools(2),
	}

	parts, err := (&Provider{}).buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}
	if got := countSystemCachePoints(parts.system); got != 1 {
		t.Errorf("system CachePoint: got %d want 1", got)
	}
	if got := countCachePoints(parts.messages); got != 1 {
		t.Errorf("message CachePoint from tool_result: got %d want 1", got)
	}
	if got := countToolCachePoints(parts.toolCfg); got != 1 {
		t.Errorf("auto tool CachePoint: got %d want 1", got)
	}
}

func TestBuildConverseParts_ExplicitToolCacheControl(t *testing.T) {
	tools := makeTools(2)
	tools[1]["cache_control"] = map[string]any{"type": "ephemeral"}
	req := &core.ChatRequest{
		Model: "anthropic.claude-sonnet-4-6",
		Messages: []core.Message{
			makeTextMessage("system", "sys"),
			makeTextMessage("user", "hi"),
		},
		Tools: tools,
	}
	parts, err := (&Provider{}).buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}
	// 显式 tool 断点 1 个；因已有 tools 级 cache_control，不再追加 auto
	if got := countToolCachePoints(parts.toolCfg); got != 1 {
		t.Errorf("tool CachePoints: got %d want 1", got)
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

	parts, err := (&Provider{}).buildConverseParts(req)
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

	parts, err := (&Provider{}).buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}

	if parts.toolCfg != nil {
		t.Error("expected nil toolCfg when no tools provided")
	}
}
