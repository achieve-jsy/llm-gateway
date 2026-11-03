package bedrock

import (
	"testing"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
)

func TestTrimExcessCachePoints_UnderLimit(t *testing.T) {
	parts := &converseParts{
		system: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: "sys"},
			&brtypes.SystemContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
		},
		messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "hello"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
		},
		toolCfg: &brtypes.ToolConfiguration{
			Tools: []brtypes.Tool{
				&brtypes.ToolMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			},
		},
	}
	// 3 CachePoints, limit 4 → no change
	trimExcessCachePoints(parts, 4)

	total := countSystemCachePoints(parts.system) + countToolCachePoints(parts.toolCfg) + countMessageCachePoints(parts.messages)
	if total != 3 {
		t.Errorf("expected 3 CachePoints, got %d", total)
	}
}

func TestTrimExcessCachePoints_OverLimit(t *testing.T) {
	cp := &brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}}
	parts := &converseParts{
		system: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: "sys"},
			&brtypes.SystemContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
		},
		messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m1"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleAssistant, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m2"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m3"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleAssistant, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m4"},
				cp,
			}},
		},
		toolCfg: &brtypes.ToolConfiguration{
			Tools: []brtypes.Tool{
				&brtypes.ToolMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			},
		},
	}
	// 6 CachePoints (1 system + 1 tool + 4 messages), limit 4 → remove 2 earliest message CPs
	trimExcessCachePoints(parts, 4)

	total := countSystemCachePoints(parts.system) + countToolCachePoints(parts.toolCfg) + countMessageCachePoints(parts.messages)
	if total != 4 {
		t.Errorf("expected 4 CachePoints after trim, got %d", total)
	}

	// System preserved
	if countSystemCachePoints(parts.system) != 1 {
		t.Error("system CachePoint should be preserved")
	}
	// Tool preserved
	if countToolCachePoints(parts.toolCfg) != 1 {
		t.Error("tool CachePoint should be preserved")
	}
	// First message CP removed
	if countMessageCachePoints([]brtypes.Message{parts.messages[0]}) != 0 {
		t.Error("earliest message CachePoint should be removed")
	}
	// Last message CP preserved
	if countMessageCachePoints([]brtypes.Message{parts.messages[3]}) != 1 {
		t.Error("latest message CachePoint should be preserved")
	}
}

func TestTrimExcessCachePoints_ManyOverLimit(t *testing.T) {
	parts := &converseParts{
		system: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: "sys1"},
			&brtypes.SystemContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			&brtypes.SystemContentBlockMemberText{Value: "sys2"},
			&brtypes.SystemContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
		},
		messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m1"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleAssistant, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m2"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m3"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
		},
		toolCfg: &brtypes.ToolConfiguration{
			Tools: []brtypes.Tool{
				&brtypes.ToolMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			},
		},
	}
	// 6 CachePoints (2 system + 1 tool + 3 messages), limit 4 → keep 2 sys + 1 tool + 1 msg
	trimExcessCachePoints(parts, 4)

	total := countSystemCachePoints(parts.system) + countToolCachePoints(parts.toolCfg) + countMessageCachePoints(parts.messages)
	if total != 4 {
		t.Errorf("expected 4 CachePoints after trim, got %d", total)
	}
	if countSystemCachePoints(parts.system) != 2 {
		t.Error("system CachePoints should be preserved")
	}
	if countToolCachePoints(parts.toolCfg) != 1 {
		t.Error("tool CachePoint should be preserved")
	}
	if countMessageCachePoints(parts.messages) != 1 {
		t.Error("should keep exactly 1 message CachePoint")
	}
	// The kept message CP should be the latest one (messages[2])
	if countMessageCachePoints([]brtypes.Message{parts.messages[2]}) != 1 {
		t.Error("latest message CachePoint should be the one preserved")
	}
}

func TestTrimExcessCachePoints_NoCachePoints(t *testing.T) {
	parts := &converseParts{
		system: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: "sys"},
		},
		messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "hello"},
			}},
		},
	}
	// Should not panic
	trimExcessCachePoints(parts, 4)
}

func TestTrimExcessCachePoints_NilToolCfg(t *testing.T) {
	parts := &converseParts{
		system: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: "sys"},
			&brtypes.SystemContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
		},
		messages: []brtypes.Message{
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m1"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleAssistant, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m2"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleUser, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m3"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
			{Role: brtypes.ConversationRoleAssistant, Content: []brtypes.ContentBlock{
				&brtypes.ContentBlockMemberText{Value: "m4"},
				&brtypes.ContentBlockMemberCachePoint{Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}},
			}},
		},
		toolCfg: nil,
	}
	// 5 CachePoints (1 system + 4 messages), nil toolCfg, limit 4 → remove 1 earliest msg CP
	trimExcessCachePoints(parts, 4)

	total := countSystemCachePoints(parts.system) + countToolCachePoints(parts.toolCfg) + countMessageCachePoints(parts.messages)
	if total != 4 {
		t.Errorf("expected 4 CachePoints after trim, got %d", total)
	}
}

// Ensure unused import doesn't break
var _ = awssdk.String
