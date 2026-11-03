package redact

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

const sampleSecret = "glpat-AbCdEfGhIjKlMnOpQrSt"

func newEngine(t *testing.T, mode string) *Engine {
	t.Helper()
	e, err := New(Config{Mode: mode, RulesOnly: true})
	if err != nil {
		t.Fatalf("New(%s): %v", mode, err)
	}
	return e
}

// mask 模式必须把凭证换成占位符，restore 后逐字节还原。
func TestMaskRestoreRoundTrip(t *testing.T) {
	e := newEngine(t, ModeMask)
	original := "帮我看看这个 token 为什么失效：" + sampleSecret + " 谢谢"
	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: original}}}

	s := e.NewSession()
	defer s.Close()
	if err := s.ProtectChat(context.Background(), req); err != nil {
		t.Fatalf("ProtectChat: %v", err)
	}
	masked, _ := req.Messages[0].Content.(string)
	if masked == original {
		t.Fatal("凭证未被脱敏")
	}
	if strings.Contains(masked, sampleSecret) {
		t.Fatalf("脱敏后仍含原文: %q", masked)
	}
	if len(s.Hits()) == 0 {
		t.Fatal("未记录命中")
	}

	resp := &core.ChatResponse{Choices: []core.Choice{{Message: core.ResponseMessage{
		Content: "问题在于 " + masked[strings.Index(masked, "{{"):],
	}}}}
	s.RestoreChat(resp)
	got, _ := resp.Choices[0].Message.Content.(string)
	if !strings.Contains(got, sampleSecret) {
		t.Fatalf("响应未还原: %q", got)
	}
}

// detect 模式不得改动请求内容。
func TestDetectDoesNotMutate(t *testing.T) {
	e := newEngine(t, ModeDetect)
	original := "token=" + sampleSecret
	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: original}}}

	s := e.NewSession()
	defer s.Close()
	if err := s.ProtectChat(context.Background(), req); err != nil {
		t.Fatalf("ProtectChat: %v", err)
	}
	if got, _ := req.Messages[0].Content.(string); got != original {
		t.Fatalf("detect 模式改写了内容: %q", got)
	}
	if len(s.Hits()) == 0 {
		t.Fatal("detect 模式未记录命中")
	}
}

// off 模式什么都不做，也不应报错。
func TestOffIsNoop(t *testing.T) {
	e := newEngine(t, ModeOff)
	if e.Enabled() || e.DetectOnly() {
		t.Fatal("off 模式状态错误")
	}
	original := "token=" + sampleSecret
	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: original}}}
	s := e.NewSession()
	defer s.Close()
	if err := s.ProtectChat(context.Background(), req); err != nil {
		t.Fatalf("ProtectChat: %v", err)
	}
	if got, _ := req.Messages[0].Content.(string); got != original {
		t.Fatal("off 模式改写了内容")
	}
}

// 非文本字段（工具定义、图片 part）必须原样保留。
func TestStructuredFieldsUntouched(t *testing.T) {
	e := newEngine(t, ModeMask)
	req := &core.ChatRequest{
		Messages: []core.Message{{
			Role: "user",
			Content: []core.ContentPart{
				{Type: "text", Text: "看看 " + sampleSecret},
				{Type: "image_url", ImageURL: &core.ImageURLContent{URL: "data:image/png;base64," + sampleSecret}},
			},
		}},
		Tools: []map[string]any{{"type": "function", "function": map[string]any{"name": sampleSecret}}},
	}
	s := e.NewSession()
	defer s.Close()
	if err := s.ProtectChat(context.Background(), req); err != nil {
		t.Fatalf("ProtectChat: %v", err)
	}
	parts, _ := req.Messages[0].Content.([]core.ContentPart)
	if !strings.Contains(parts[0].Text, "{{") {
		t.Fatalf("text part 未脱敏: %q", parts[0].Text)
	}
	if parts[1].ImageURL.URL != "data:image/png;base64,"+sampleSecret {
		t.Fatal("image part 被改动")
	}
	if req.Tools[0]["function"].(map[string]any)["name"] != sampleSecret {
		t.Fatal("工具定义被改动")
	}
}

// trace 落库的正文也必须脱敏：否则 mask 只保护了「出网」，call_traces 里还是明文。
func TestProtectTraceBodyMasksPersistedRequest(t *testing.T) {
	e := newEngine(t, ModeMask)
	s := e.NewSession()
	defer s.Close()

	// 先走 typed 路径拿到占位符，trace 必须复用同一个，否则两边对不上。
	masked, err := s.ProtectText(context.Background(), "token 是 "+sampleSecret)
	if err != nil {
		t.Fatalf("ProtectText: %v", err)
	}
	placeholder := strings.TrimPrefix(masked, "token 是 ")

	raw := []byte(`{"model":"gpt-x","messages":[{"role":"user","content":"token 是 ` + sampleSecret + `"}],"system":"备用 ` + sampleSecret + `"}`)
	out := s.ProtectTraceBody(context.Background(), raw)

	if bytes.Contains(out, []byte(sampleSecret)) {
		t.Fatalf("trace 正文仍有明文: %s", out)
	}
	if !bytes.Contains(out, []byte(placeholder)) {
		t.Fatalf("trace 正文没有复用上游的占位符 %s: %s", placeholder, out)
	}
	if !json.Valid(out) {
		t.Fatalf("trace 正文被写坏成非 JSON: %s", out)
	}
}

// off / detect 不改写 trace：detect 时上游拿到的本来就是原文，trace 与之一致才是事实。
func TestProtectTraceBodyNoopOutsideMask(t *testing.T) {
	for _, mode := range []string{ModeOff, ModeDetect} {
		e := newEngine(t, mode)
		s := e.NewSession()
		raw := []byte(`{"a":"` + sampleSecret + `"}`)
		if got := s.ProtectTraceBody(context.Background(), raw); !bytes.Equal(got, raw) {
			t.Fatalf("%s 模式不该改写 trace 正文: %s", mode, got)
		}
		s.Close()
	}
}
