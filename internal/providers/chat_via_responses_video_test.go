package providers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

// Chat -> Responses 方言的转换曾把 video_url 静默丢弃（switch 只有 text / image_url），
// 导致 Responses 通道收到视频时返回 200 但上游只看到文字。
func chatReqWith(content ...core.ContentPart) *core.ChatRequest {
	return &core.ChatRequest{
		Model: "probe",
		Messages: []core.Message{{
			Role:    "user",
			Content: content,
		}},
	}
}

func TestConvertChatRequestToResponsesKeepsVideo(t *testing.T) {
	req := chatReqWith(
		core.ContentPart{Type: "text", Text: "描述这段视频"},
		core.ContentPart{Type: "video_url", VideoURL: &core.VideoURLContent{URL: "https://example.com/a.mp4"}},
	)
	resp, err := ConvertChatRequestToResponses(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	b, _ := json.Marshal(resp)
	got := string(b)
	if !strings.Contains(got, "input_video") {
		t.Fatalf("应输出 input_video 片段，实际: %s", got)
	}
	if !strings.Contains(got, "https://example.com/a.mp4") {
		t.Fatalf("视频地址丢失，实际: %s", got)
	}
}

func TestConvertChatRequestToResponsesKeepsVideoFileID(t *testing.T) {
	req := chatReqWith(
		core.ContentPart{Type: "video_url", VideoURL: &core.VideoURLContent{FileID: "file-abc"}},
	)
	resp, err := ConvertChatRequestToResponses(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	b, _ := json.Marshal(resp)
	if !strings.Contains(string(b), "file-abc") {
		t.Fatalf("file_id 丢���，实际: %s", string(b))
	}
}

// 未知片段/不支持片段必须 fail-closed，不能静默丢弃后当成纯文本请求。
func TestConvertChatRequestToResponsesRejectsUnsupportedPart(t *testing.T) {
	_, err := ConvertChatRequestToResponses(chatReqWith(core.ContentPart{Type: "input_file"}))
	if err == nil {
		t.Fatal("未知 content part 类型应报错")
	}
	if !strings.Contains(err.Error(), "input_file") {
		t.Fatalf("报错信息应包含片段类型，实际: %v", err)
	}
}
