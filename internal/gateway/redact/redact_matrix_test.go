package redact

// 适配层（internal/gateway/redact）行为测试：SSE 跨帧还原、fail-closed、模式矩阵、
// content 形态覆盖。凭证样本全部为合成值，不含任何真实密钥。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/privacy"
)

const (
	// synthToken / synthMail 是合成样本，非真实凭证。
	synthToken = "glpat-AbCdEfGhIjKlMnOpQrSt"
	synthMail  = "alice@company.internal"
)

// ---- 假 ResponseWriter：记录服务端真实写出的 SSE 字节 ----

type recWriter struct {
	hdr    http.Header
	status int
	buf    bytes.Buffer
}

func newSSERecorder() *recWriter {
	h := http.Header{}
	h.Set("Content-Type", "text/event-stream")
	return &recWriter{hdr: h}
}

func (r *recWriter) Header() http.Header         { return r.hdr }
func (r *recWriter) Write(b []byte) (int, error) { return r.buf.Write(b) }
func (r *recWriter) WriteHeader(code int)        { r.status = code }

// sseChunk 构造一帧 OpenAI Chat Completions 流式事件（data: 单行 JSON + 空行）。
func sseChunk(t *testing.T, delta string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":     "chatcmpl-test",
		"object": "chat.completion.chunk",
		"choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"content": delta}},
		},
	})
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	return "data: " + string(body) + "\n\n"
}

// sseDeltas 把客户端收到的原始字节按帧解析回 content 增量拼接，返回拼接结果与帧数。
func sseDeltas(t *testing.T, raw string) (string, int) {
	t.Helper()
	var out strings.Builder
	frames := 0
	for _, ev := range strings.Split(raw, "\n\n") {
		if ev == "" {
			continue
		}
		data, ok := strings.CutPrefix(ev, "data: ")
		if !ok {
			t.Fatalf("帧不是 data: 行: %q", ev)
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("帧不是合法 JSON: %v (%q)", err, data)
		}
		if len(chunk.Choices) != 1 {
			t.Fatalf("帧 choices 数量 = %d, want 1: %q", len(chunk.Choices), data)
		}
		out.WriteString(chunk.Choices[0].Delta.Content)
		frames++
	}
	return out.String(), frames
}

// placeholderOf 取文本里第一个占位符（形如 {{KIND_xxxxxxxx}}）。
func placeholderOf(text string) string {
	i := strings.Index(text, "{{")
	if i < 0 {
		return ""
	}
	j := strings.Index(text[i:], "}}")
	if j < 0 {
		return ""
	}
	return text[i : i+j+2]
}

// 占位符被相邻两帧切开时，客户端拼起来的文本必须与原文逐字节一致，且看不到占位符。
func TestRestoringWriterSSECrossChunk(t *testing.T) {
	ctx := context.Background()
	e := newEngine(t, ModeMask)
	original := "帮我看看这个 token 为什么失效：" + synthToken + " 谢谢"

	s := e.NewSession()
	defer s.Close()

	masked, err := s.ProtectText(ctx, original)
	if err != nil {
		t.Fatalf("ProtectText: %v", err)
	}
	ph := placeholderOf(masked)
	if ph == "" {
		t.Fatalf("脱敏结果里没有占位符: %q", masked)
	}
	at := strings.Index(masked, ph)

	cases := []struct {
		name string
		cut  int
	}{
		{"第一帧以 {{ 结尾", at + 2},
		{"第一帧以占位符前半结尾", at + len(ph)/2},
		{"第一帧切在占位符 hash 开头", at + len(ph) - 9}, // 占位符去掉 "hash}}" 部分
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newSSERecorder()
			rw := s.RestoringWriter(w)
			if rw == nil {
				t.Fatal("mask 模式 RestoringWriter 返回 nil")
			}

			if _, err := rw.Write([]byte(sseChunk(t, masked[:tc.cut]))); err != nil {
				t.Fatalf("write frame1: %v", err)
			}
			// 可能成为占位符开头的帧必须被扣住，不能带着 {{ 直接下发给客户端
			if w.buf.Len() != 0 {
				t.Fatalf("以半截占位符结尾的帧未被扣住，客户端已收到: %q", w.buf.String())
			}

			if _, err := rw.Write([]byte(sseChunk(t, masked[tc.cut:]))); err != nil {
				t.Fatalf("write frame2: %v", err)
			}
			rw.Finish()

			got := w.buf.String()
			if strings.Contains(got, "{{") {
				t.Fatalf("客户端看到占位符残留: %q", got)
			}
			joined, frames := sseDeltas(t, got)
			if frames != 2 {
				t.Fatalf("下发帧数 = %d, want 2（扣住的帧应在下一帧到达后补发）: %q", frames, got)
			}
			if joined != original {
				t.Fatalf("客户端拼接结果与原文不一致:\nwant: %q\ngot:  %q", original, joined)
			}
		})
	}
}

// 流结束时仍扣着一帧（占位符没有后半截）时，Finish 必须把它原样刷出，不能吞帧。
func TestRestoringWriterFinishFlushesResidual(t *testing.T) {
	e := newEngine(t, ModeMask)
	s := e.NewSession()
	defer s.Close()

	w := newSSERecorder()
	rw := s.RestoringWriter(w)
	if rw == nil {
		t.Fatal("mask 模式 RestoringWriter 返回 nil")
	}

	residual := "这句话没有后半截：{{"
	if _, err := rw.Write([]byte(sseChunk(t, residual))); err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.buf.Len() != 0 {
		t.Fatalf("残留帧未扣住: %q", w.buf.String())
	}

	rw.Finish()

	got := w.buf.String()
	if got == "" {
		t.Fatal("Finish 未刷出残留帧")
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("残留帧不是完整 SSE 帧: %q", got)
	}
	joined, frames := sseDeltas(t, got)
	if frames != 1 {
		t.Fatalf("下发帧数 = %d, want 1: %q", frames, got)
	}
	if joined != residual {
		t.Fatalf("残留帧被改动:\nwant: %q\ngot:  %q", residual, joined)
	}
}

// 模式矩阵：off 不改写、detect 只记命中、mask 改写并可还原。
func TestModeMatrix(t *testing.T) {
	ctx := context.Background()
	original := "我的 token 是 " + synthToken

	t.Run("off 不改写", func(t *testing.T) {
		e := newEngine(t, ModeOff)
		if e.Mode() != ModeOff || e.Enabled() || e.DetectOnly() {
			t.Fatalf("off 状态错误: mode=%q enabled=%v detect=%v", e.Mode(), e.Enabled(), e.DetectOnly())
		}
		s := e.NewSession()
		defer s.Close()
		out, err := s.ProtectText(ctx, original)
		if err != nil {
			t.Fatalf("ProtectText: %v", err)
		}
		if out != original {
			t.Fatalf("off 模式改写了内容: %q", out)
		}
		if len(s.Hits()) != 0 {
			t.Fatalf("off 模式记录了命中: %d", len(s.Hits()))
		}
		// 约定：off 模式返回 nil，调用方必须判空后再替换 writer。
		if rw := s.RestoringWriter(newSSERecorder()); rw != nil {
			t.Fatal("off 模式 RestoringWriter 应为 nil")
		}
	})

	t.Run("detect 只记命中不改写", func(t *testing.T) {
		e := newEngine(t, ModeDetect)
		if !e.DetectOnly() || e.Enabled() {
			t.Fatalf("detect 状态错误: enabled=%v detect=%v", e.Enabled(), e.DetectOnly())
		}
		s := e.NewSession()
		defer s.Close()
		req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: original}}}
		if err := s.ProtectChat(ctx, req); err != nil {
			t.Fatalf("ProtectChat: %v", err)
		}
		if got, _ := req.Messages[0].Content.(string); got != original {
			t.Fatalf("detect 模式改写了内容: %q", got)
		}
		if len(s.Hits()) == 0 {
			t.Fatal("detect 模式未记录命中")
		}
	})

	t.Run("mask 改写且可还原", func(t *testing.T) {
		e := newEngine(t, ModeMask)
		if !e.Enabled() || e.DetectOnly() {
			t.Fatalf("mask 状态错误: enabled=%v detect=%v", e.Enabled(), e.DetectOnly())
		}
		s := e.NewSession()
		defer s.Close()
		req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: original}}}
		if err := s.ProtectChat(ctx, req); err != nil {
			t.Fatalf("ProtectChat: %v", err)
		}
		masked, _ := req.Messages[0].Content.(string)
		if masked == original || strings.Contains(masked, synthToken) {
			t.Fatalf("mask 模式未脱敏: %q", masked)
		}
		if restored := s.RestoreText(masked); restored != original {
			t.Fatalf("还原不一致:\nwant: %q\ngot:  %q", original, restored)
		}
	})
}

// 同一会话内同一凭证必须得到同一占位符：上游前缀缓存依赖这个稳定性。
func TestPlaceholderStableWithinSession(t *testing.T) {
	ctx := context.Background()
	e := newEngine(t, ModeMask)
	s := e.NewSession()
	defer s.Close()

	first, err := s.ProtectText(ctx, "第一次出现："+synthToken)
	if err != nil {
		t.Fatalf("ProtectText: %v", err)
	}
	second, err := s.ProtectText(ctx, "第二次出现："+synthToken)
	if err != nil {
		t.Fatalf("ProtectText: %v", err)
	}
	ph1, ph2 := placeholderOf(first), placeholderOf(second)
	if ph1 == "" || ph2 == "" {
		t.Fatalf("未生成占位符: %q / %q", first, second)
	}
	if ph1 != ph2 {
		t.Fatalf("同一凭证得到不同占位符: %q != %q", ph1, ph2)
	}

	// 同一段文本内重复出现，也必须是同一占位符，且能全部还原。
	dup := synthToken + " 和 " + synthToken
	maskedDup, err := s.ProtectText(ctx, dup)
	if err != nil {
		t.Fatalf("ProtectText: %v", err)
	}
	if n := strings.Count(maskedDup, ph1); n != 2 {
		t.Fatalf("重复出现得到 %d 个同一占位符, want 2: %q", n, maskedDup)
	}
	if restored := s.RestoreText(maskedDup); restored != dup {
		t.Fatalf("重复占位符还原不一致:\nwant: %q\ngot:  %q", dup, restored)
	}
}

// Content 形态覆盖：文本被改写，非文本 part 一律不动。
func TestContentShapes(t *testing.T) {
	ctx := context.Background()
	e := newEngine(t, ModeMask)
	text := "我的 token 是 " + synthToken
	imageURL := "data:image/png;base64," + synthToken

	t.Run("string content", func(t *testing.T) {
		s := e.NewSession()
		defer s.Close()
		req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: text}}}
		if err := s.ProtectChat(ctx, req); err != nil {
			t.Fatalf("ProtectChat: %v", err)
		}
		masked, _ := req.Messages[0].Content.(string)
		if strings.Contains(masked, synthToken) {
			t.Fatalf("string content 未脱敏: %q", masked)
		}
		if restored := s.RestoreText(masked); restored != text {
			t.Fatalf("还原不一致: %q", restored)
		}
	})

	t.Run("[]core.ContentPart", func(t *testing.T) {
		s := e.NewSession()
		defer s.Close()
		req := &core.ChatRequest{Messages: []core.Message{{
			Role: "user",
			Content: []core.ContentPart{
				{Type: "text", Text: text},
				{Type: "image_url", ImageURL: &core.ImageURLContent{URL: imageURL}},
			},
		}}}
		if err := s.ProtectChat(ctx, req); err != nil {
			t.Fatalf("ProtectChat: %v", err)
		}
		parts, ok := req.Messages[0].Content.([]core.ContentPart)
		if !ok {
			t.Fatalf("content 形态被改变: %T", req.Messages[0].Content)
		}
		if strings.Contains(parts[0].Text, synthToken) {
			t.Fatalf("text part 未脱敏: %q", parts[0].Text)
		}
		if parts[1].ImageURL.URL != imageURL {
			t.Fatalf("image part 被改动: %q", parts[1].ImageURL.URL)
		}
		if restored := s.RestoreText(parts[0].Text); restored != text {
			t.Fatalf("还原不一致: %q", restored)
		}
	})

	t.Run("Anthropic system []any", func(t *testing.T) {
		s := e.NewSession()
		defer s.Close()
		image := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": synthToken}}
		req := &core.AnthropicMessagesRequest{
			System: []any{
				map[string]any{"type": "text", "text": text},
				image,
			},
			Messages: []core.AnthropicMessage{{Role: "user", Content: "你好"}},
		}
		if err := s.ProtectAnthropic(ctx, req); err != nil {
			t.Fatalf("ProtectAnthropic: %v", err)
		}
		blocks, ok := req.System.([]any)
		if !ok {
			t.Fatalf("system 形态被改变: %T", req.System)
		}
		gotText, _ := blocks[0].(map[string]any)["text"].(string)
		if strings.Contains(gotText, synthToken) {
			t.Fatalf("system text 块未脱敏: %q", gotText)
		}
		src, _ := blocks[1].(map[string]any)["source"].(map[string]any)
		if src["data"] != synthToken {
			t.Fatalf("非文本 system 块被改动: %v", src["data"])
		}
		if restored := s.RestoreText(gotText); restored != text {
			t.Fatalf("还原不一致: %q", restored)
		}
	})

	t.Run("Anthropic message []any blocks", func(t *testing.T) {
		s := e.NewSession()
		defer s.Close()
		req := &core.AnthropicMessagesRequest{
			Messages: []core.AnthropicMessage{{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": text},
					map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": synthToken}},
				},
			}},
		}
		if err := s.ProtectAnthropic(ctx, req); err != nil {
			t.Fatalf("ProtectAnthropic: %v", err)
		}
		blocks, ok := req.Messages[0].Content.([]any)
		if !ok {
			t.Fatalf("content 形态被改变: %T", req.Messages[0].Content)
		}
		gotText, _ := blocks[0].(map[string]any)["text"].(string)
		if strings.Contains(gotText, synthToken) {
			t.Fatalf("message text 块未脱敏: %q", gotText)
		}
		src, _ := blocks[1].(map[string]any)["source"].(map[string]any)
		if src["data"] != synthToken {
			t.Fatalf("图片块被改动: %v", src["data"])
		}
	})

	// 缺口：ProtectContent 只认 string / []core.ContentPart / []any，
	// []core.AnthropicContentBlock 会落进 default 分支原样返回（不脱敏）。
	// JSON 解码路径产出的是 []any，所以线上主路径不受影响；直接构造强类型块则会漏。
	// 生产代码（content.go）补上 case []core.AnthropicContentBlock 后去掉本 skip。
	t.Run("[]core.AnthropicContentBlock", func(t *testing.T) {
		e := newEngine(t, ModeMask)
		s := e.NewSession()
		defer s.Close()
		req := &core.AnthropicMessagesRequest{
			Messages: []core.AnthropicMessage{{
				Role: "user",
				Content: []core.AnthropicContentBlock{
					{Type: "text", Text: "看看 " + synthToken},
					{Type: "image", Source: &core.AnthropicImageSource{Type: "base64", Data: synthToken}},
				},
			}},
		}
		if err := s.ProtectAnthropic(ctx, req); err != nil {
			t.Fatalf("ProtectAnthropic: %v", err)
		}
		blocks, ok := req.Messages[0].Content.([]core.AnthropicContentBlock)
		if !ok {
			t.Fatalf("content 形态被改变: %T", req.Messages[0].Content)
		}
		if strings.Contains(blocks[0].Text, synthToken) {
			t.Fatalf("强类型 text 块未脱敏: %q", blocks[0].Text)
		}
		if blocks[1].Source == nil || blocks[1].Source.Data != synthToken {
			t.Fatal("image 块被改动")
		}
		// 响应侧同样要能还原
		resp := &core.AnthropicMessagesResponse{Content: []core.AnthropicContentBlock{{Type: "text", Text: blocks[0].Text}}}
		s.RestoreAnthropic(resp)
		if !strings.Contains(resp.Content[0].Text, synthToken) {
			t.Fatalf("强类型块响应未还原: %q", resp.Content[0].Text)
		}
	})
}

// fail-closed：Block 动作必须返回错误且不把原文交回调用方。
func TestBlockActionFailClosed(t *testing.T) {
	ctx := context.Background()
	original := "我的 token 是 " + synthToken

	e, err := New(Config{Mode: ModeMask, RulesOnly: true, Actions: map[string]string{"GITLAB_TOKEN": privacy.Block}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s := e.NewSession()
	defer s.Close()
	out, err := s.ProtectText(ctx, original)
	if err == nil {
		t.Fatal("Block 动作未返回错误（fail-open）")
	}
	if !errors.Is(err, privacy.ErrBlocked) {
		t.Fatalf("错误不是 ErrBlocked: %v", err)
	}
	if out != "" {
		t.Fatalf("Block 时返回了内容，可能把原文放行: %q", out)
	}

	// 整请求同样终止
	s2 := e.NewSession()
	defer s2.Close()
	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: original}}}
	if err := s2.ProtectChat(ctx, req); err == nil {
		t.Fatal("ProtectChat 未因 Block 返回错误（调用方无法 fail-closed）")
	}

	// detect 模式永不因 Block 中断（只报不改）
	de, err := New(Config{Mode: ModeDetect, RulesOnly: true, Actions: map[string]string{"GITLAB_TOKEN": privacy.Block}})
	if err != nil {
		t.Fatalf("New(detect): %v", err)
	}
	ds := de.NewSession()
	defer ds.Close()
	dreq := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: original}}}
	if err := ds.ProtectChat(ctx, dreq); err != nil {
		t.Fatalf("detect 模式不应因 Block 报错: %v", err)
	}
	if got, _ := dreq.Messages[0].Content.(string); got != original {
		t.Fatalf("detect 模式改写了内容: %q", got)
	}
	if len(ds.Hits()) == 0 {
		t.Fatal("detect 模式未记录命中")
	}

	// 模型不可用（privacy.ErrModel）无法在此构造：redact.Config 不暴露 Model 注入点，
	// 非 RulesOnly 会触发权重下载（需网络）。属于集成测试范畴，此处跳过。
	t.Run("模型不可用", func(t *testing.T) {
		t.Skip("redact.Config 不暴露 Model 注入点；非 RulesOnly 需下载权重（网络），单测不可构造")
	})
}
