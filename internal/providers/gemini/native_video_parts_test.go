package gemini

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

// 视频片段在 Gemini 原生通道上必须真的送达上游，不能被 switch 静默吞掉
// （历史缺陷：geminiPartsFromContentParts 没有 video 分支，返回 200 但模型只看到文字）。
func videoParts(v *core.VideoURLContent) []core.ContentPart {
	return []core.ContentPart{
		{Type: "text", Text: "描述这段视频"},
		{Type: "video_url", VideoURL: v},
	}
}

func TestGeminiPartsFromContentPartsVideoInlineData(t *testing.T) {
	out, err := geminiPartsFromContentParts(videoParts(&core.VideoURLContent{
		URL: "data:video/mp4;base64,AAAA",
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("期望 2 段（text + video），实际 %d 段", len(out))
	}
	blob := out[1].InlineData
	if blob == nil {
		t.Fatalf("data: URL 应转为 inline_data，实际 part=%+v", out[1])
	}
	if blob.MimeType != "video/mp4" || blob.Data != "AAAA" {
		t.Fatalf("inline_data 内容不符: %+v", blob)
	}
}

func TestGeminiPartsFromContentPartsVideoFileData(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		fileID  string
		wantURI string
	}{
		{"files api 完整 URI", "https://generativelanguage.googleapis.com/v1beta/files/abc", "", "https://generativelanguage.googleapis.com/v1beta/files/abc"},
		{"gcs 对象", "gs://bucket/v.mp4", "", "gs://bucket/v.mp4"},
		{"youtube 链接", "https://www.youtube.com/watch?v=abc", "", "https://www.youtube.com/watch?v=abc"},
		{"只给 file_id（files/ 前缀）", "", "files/abc", "https://generativelanguage.googleapis.com/v1beta/files/abc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := geminiPartsFromContentParts(videoParts(&core.VideoURLContent{URL: c.url, FileID: c.fileID}))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if len(out) != 2 || out[1].FileData == nil {
				t.Fatalf("应转为 file_data，实际 %+v", out)
			}
			if out[1].FileData.FileURI != c.wantURI {
				t.Fatalf("file_uri=%q，期望 %q", out[1].FileData.FileURI, c.wantURI)
			}
			if !strings.HasPrefix(out[1].FileData.MimeType, "video/") {
				t.Fatalf("mime_type 应为 video/*，实际 %q", out[1].FileData.MimeType)
			}
		})
	}
}

// Gemini 不会主动抓取任意 http(s) 视频地址：明确报错，而不是发个必然失败或丢内容的请求。
func TestGeminiPartsFromContentPartsVideoRejectsArbitraryURL(t *testing.T) {
	_, err := geminiPartsFromContentParts(videoParts(&core.VideoURLContent{URL: "https://example.com/a.mp4"}))
	if err == nil {
		t.Fatal("普通 http 视频地址应报错（提示改用 file_id / Files API）")
	}
	if !strings.Contains(err.Error(), "video_url") {
		t.Fatalf("报错信息应点名 video_url，实际: %v", err)
	}
}

// 未知片段类型必须 fail-closed：宁可 400，也不要静默丢内容后返回 200。
func TestGeminiPartsFromContentPartsUnknownTypeFailsClosed(t *testing.T) {
	_, err := geminiPartsFromContentParts([]core.ContentPart{{Type: "input_file"}})
	if err == nil {
		t.Fatal("未知 content part 类型应报错")
	}
	if !strings.Contains(err.Error(), "input_file") {
		t.Fatalf("报错信息应包含片段类型，实际: %v", err)
	}
}

func TestGeminiPartsFromContentPartsVideoMetadata(t *testing.T) {
	fps := 1.5
	start, end := 2.0, 12.0
	out, err := geminiPartsFromContentParts(videoParts(&core.VideoURLContent{
		URL:         "gs://bucket/v.mp4",
		FPS:         &fps,
		StartOffset: &start,
		EndOffset:   &end,
	}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	meta := out[1].VideoMetadata
	if meta == nil {
		t.Fatalf("应下发 video_metadata，实际 %+v", out[1])
	}
	if meta.FPS == nil || *meta.FPS != 1.5 {
		t.Fatalf("fps 应为 1.5，实际 %+v", meta.FPS)
	}
	if meta.StartOffset != "2s" || meta.EndOffset != "12s" {
		t.Fatalf("起止偏移应为 2s/12s，实际 %q/%q", meta.StartOffset, meta.EndOffset)
	}
}
