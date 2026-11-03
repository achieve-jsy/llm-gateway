package providers_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// TestConvertResponsesInputVideo 验证 /v1/responses 的 input_video 片段被归一为
// chat 侧的 video_url 片段（而不是被丢弃或退化成文本）。
func TestConvertResponsesInputVideo(t *testing.T) {
	fps1 := 1.0
	cases := []struct {
		name     string
		in       []any
		wantURL  string
		wantFPS  *float64
		wantFile string
	}{
		{
			name: "object form",
			in: []any{map[string]any{
				"type":      "input_video",
				"video_url": map[string]any{"url": "https://a/b.mp4", "fps": float64(1)},
			}},
			wantURL: "https://a/b.mp4",
			wantFPS: &fps1,
		},
		{
			name: "string form",
			in: []any{map[string]any{
				"type":      "input_video",
				"video_url": "data:video/mp4;base64,AAA",
			}},
			wantURL: "data:video/mp4;base64,AAA",
		},
		{
			name: "file_id only",
			in: []any{map[string]any{
				"type":      "input_video",
				"video_url": map[string]any{"file_id": "file-1"},
			}},
			wantFile: "file-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := providers.ConvertResponsesContentToChatContent(tc.in)
			if !ok {
				t.Fatalf("conversion rejected input_video: %#v", tc.in)
			}
			parts, isParts := out.([]core.ContentPart)
			if !isParts {
				t.Fatalf("want []core.ContentPart, got %T (%#v)", out, out)
			}
			if len(parts) != 1 || parts[0].Type != "video_url" || parts[0].VideoURL == nil {
				t.Fatalf("unexpected parts: %#v", parts)
			}
			if parts[0].VideoURL.URL != tc.wantURL {
				t.Fatalf("url = %q, want %q", parts[0].VideoURL.URL, tc.wantURL)
			}
			if parts[0].VideoURL.FileID != tc.wantFile {
				t.Fatalf("file_id = %q, want %q", parts[0].VideoURL.FileID, tc.wantFile)
			}
			if tc.wantFPS == nil {
				if parts[0].VideoURL.FPS != nil {
					t.Fatalf("fps = %v, want nil", *parts[0].VideoURL.FPS)
				}
			} else if parts[0].VideoURL.FPS == nil || *parts[0].VideoURL.FPS != *tc.wantFPS {
				t.Fatalf("fps = %v, want %v", parts[0].VideoURL.FPS, *tc.wantFPS)
			}
		})
	}
}
