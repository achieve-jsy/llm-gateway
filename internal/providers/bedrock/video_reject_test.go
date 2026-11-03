package bedrock

import (
	"strings"
	"testing"
)

// TestBedrockRejectsVideoPart Bedrock（Converse）协议没有视频内容块，
// 遇到 video 片段必须显式报错，而不是静默丢弃后让模型"凭空回答"。
func TestBedrockRejectsVideoPart(t *testing.T) {
	content := []any{
		map[string]any{"type": "text", "text": "what happens in this video?"},
		map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://a/b.mp4"}},
	}

	blocks, err := convertUserContentBlocks(content)
	if err == nil {
		t.Fatalf("expected error for video part, got %d blocks", len(blocks))
	}
	msg := err.Error()
	// 仓库内错误文案统一中文，断言中文关键字 + provider 名
	if !strings.Contains(msg, "视频") {
		t.Fatalf("error should mention 视频: %v", err)
	}
	if !strings.Contains(strings.ToLower(msg), "bedrock") {
		t.Fatalf("error should mention provider: %v", err)
	}
}
