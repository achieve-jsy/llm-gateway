package core

import "testing"

// TestHasVideoInput 覆盖 /v1/responses 的三种承载形态：input_video 片段、
// video_url 片段、message 内层 content。failover 靠它保持视频能力约束，
// 漏判会让视频被换到不支持视频的成员上（静默丢帧）。
func TestHasVideoInput(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  bool
	}{
		{"字符串输入", "讲讲这个视频", false},
		{"nil", nil, false},
		{"纯文本片段", []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": "hi"}},
		}}, false},
		{"顶层 input_video", []any{map[string]any{
			"type": "input_video", "video_url": map[string]any{"url": "https://a/b.mp4"},
		}}, true},
		{"message 内层 video_url", []any{map[string]any{
			"type": "message", "role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "看这个"},
				map[string]any{"type": "video_url", "video_url": map[string]any{"url": "https://a/b.mp4", "fps": 2}},
			},
		}}, true},
		{"单对象形式", map[string]any{
			"type": "input_video", "file_id": "file-123",
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasVideoInput(tc.input); got != tc.want {
				t.Fatalf("HasVideoInput() = %v, want %v", got, tc.want)
			}
		})
	}
}
