package core

import (
	"strings"
	"testing"
)

// TestAnthropicMessagesRejectVideoBlock Anthropic Messages 协议没有视频内容块；
// 遇到 video_url/input_video 必须报错，而不是静默丢弃（静默丢弃会让上游模型
// 在没有视频的情况下回答，用户看到的是"表面上成功"的错误答案）。
func TestAnthropicMessagesRejectVideoBlock(t *testing.T) {
	anthReq := &AnthropicMessagesRequest{
		Model:     "claude-test",
		MaxTokens: 128,
		Messages: []AnthropicMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "what happens in this video?"},
				map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://a/b.mp4"}},
			},
		}},
	}

	_, err := AnthropicToChatRequest(anthReq)
	if err == nil {
		t.Fatal("expected error for video block")
	}
	msg := err.Error()
	// 仓库内错误文案统一中文，断言中文关键字 + provider 名
	if !strings.Contains(msg, "视频") {
		t.Fatalf("error should mention 视频: %v", err)
	}
	if !strings.Contains(strings.ToLower(msg), "anthropic") {
		t.Fatalf("error should mention provider: %v", err)
	}
}

// TestAnthropicMessagesKeepUnknownBlocksLoose 钉子：未知 block 的既有宽松行为
// 不能被这次改动波及（只显式拒绝 video）。
func TestAnthropicMessagesKeepUnknownBlocksLoose(t *testing.T) {
	anthReq := &AnthropicMessagesRequest{
		Model: "claude-test",
		Messages: []AnthropicMessage{{
			Role:    "user",
			Content: []any{map[string]any{"type": "some_future_block", "payload": "x"}},
		}},
	}
	if _, err := AnthropicToChatRequest(anthReq); err != nil {
		t.Fatalf("unknown block types must stay lenient, got error: %v", err)
	}
}
