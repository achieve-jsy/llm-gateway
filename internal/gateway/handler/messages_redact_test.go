package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/redact"
)

const anthRedactSecret = "glpat-AbCdEfGhIjKlMnOpQrSt"

// redactFakeOrchestrator 记录 handler 交给上游的请求（即脱敏后的版本），
// 并把请求里的文本原样“回吐”成响应，用来验证响应侧的还原。
type redactFakeOrchestrator struct {
	mu          sync.Mutex
	chatReq     *core.ChatRequest
	streamReq   *core.ChatRequest
	chatCalls   int
	streamCalls int
}

func (f *redactFakeOrchestrator) recordChat(req *core.ChatRequest) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatCalls++
	f.chatReq = req
	content, _ := req.Messages[0].Content.(string)
	return content
}

func (f *redactFakeOrchestrator) recordStream(req *core.ChatRequest) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streamCalls++
	f.streamReq = req
	content, _ := req.Messages[0].Content.(string)
	return content
}

// Chat 把（脱敏后的）请求文本原样回吐，模拟上游复述了占位符。
func (f *redactFakeOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	content := f.recordChat(req)
	return &core.ChatResponse{Model: req.Model, Choices: []core.Choice{{
		Message: core.ResponseMessage{Role: "assistant", Content: content},
	}}}, nil
}

// ChatStream 把回吐文本拆成两个 OpenAI chunk 下发，并且把占位符故意从中切开：
// 还原必须跨 event 生效，否则客户端会看到半截 {{...。
func (f *redactFakeOrchestrator) ChatStream(_ context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	content := f.recordStream(req)
	return io.NopCloser(strings.NewReader(splitPlaceholderSSE(content))), nil
}

func (f *redactFakeOrchestrator) recordedChat() (*core.ChatRequest, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chatReq, f.chatCalls
}

func (f *redactFakeOrchestrator) recordedStream() (*core.ChatRequest, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamReq, f.streamCalls
}

func newRedactTestEngine(t *testing.T, cfg redact.Config) *redact.Engine {
	t.Helper()
	cfg.RulesOnly = true
	e, err := redact.New(cfg)
	if err != nil {
		t.Fatalf("redact.New(%+v): %v", cfg, err)
	}
	return e
}

func newMessagesHandlerForRedact(t *testing.T, engine *redact.Engine, orch ChatOrchestratorInterface) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewMessagesHandler(orch)
	h.SetRedactor(engine)
	r := gin.New()
	r.POST("/v1/messages", h.HandleMessages)
	return r
}

func anthropicBody(t *testing.T, stream bool) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model":      "claude-test",
		"max_tokens": 64,
		"stream":     stream,
		"messages": []map[string]any{{
			"role":    "user",
			"content": "帮我看看这个 token 为什么失效：" + anthRedactSecret + " 谢谢",
		}},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return body
}

// splitPlaceholderSSE 生成 OpenAI SSE：正文被拆成两个 delta，切点落在占位符中间。
func splitPlaceholderSSE(content string) string {
	first, second := content, ""
	if i := strings.Index(content, "{{"); i >= 0 {
		if j := strings.Index(content[i:], "}}"); j > 0 {
			end := i + j + 2
			cut := (i + end) / 2
			first, second = content[:cut], content[cut:]
		}
	}
	chunk := func(text string, finish string) string {
		b, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{"content": text},
				"finish_reason": finish,
			}},
		})
		return "data: " + string(b) + "\n\n"
	}
	return chunk(first, "") + chunk(second, "stop") + "data: [DONE]\n\n"
}

// TestMessagesRedact_MaskRequestAndRestoreResponse 非流式：上游只看到占位符，
// 客户端看到还原后的原文（落库的 resp_object 也必须是还原后的版本）。
func TestMessagesRedact_MaskRequestAndRestoreResponse(t *testing.T) {
	orch := &redactFakeOrchestrator{}
	h := NewMessagesHandler(orch)
	h.SetRedactor(newRedactTestEngine(t, redact.Config{Mode: redact.ModeMask}))

	var respObject any
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/messages", func(c *gin.Context) {
		h.HandleMessages(c)
		respObject = c.MustGet("resp_object")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(anthropicBody(t, false)))))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d，body=%s", rec.Code, rec.Body.String())
	}
	sent, calls := orch.recordedChat()
	if calls != 1 {
		t.Fatalf("上游调用次数 %d，want 1", calls)
	}
	upstream, _ := sent.Messages[0].Content.(string)
	if strings.Contains(upstream, anthRedactSecret) {
		t.Fatalf("上游请求仍含原文：%q", upstream)
	}
	if !strings.Contains(upstream, "{{") {
		t.Fatalf("上游请求没有占位符：%q", upstream)
	}

	body := rec.Body.String()
	if strings.Contains(body, "{{") {
		t.Fatalf("响应仍含占位符：%s", body)
	}
	if !strings.Contains(body, anthRedactSecret) {
		t.Fatalf("响应未还原原文：%s", body)
	}

	anthResp, ok := respObject.(*core.AnthropicMessagesResponse)
	if !ok {
		t.Fatalf("resp_object 类型 %T", respObject)
	}
	// 落库/追踪的必须是还原后的版本，不能把占位符写进库。
	if len(anthResp.Content) == 0 || !strings.Contains(anthResp.Content[0].Text, anthRedactSecret) {
		t.Fatalf("resp_object 未还原：%+v", anthResp.Content)
	}
}

// TestMessagesRedact_StreamRestoresSplitPlaceholder 流式：占位符被上游拆到两个
// text_delta 里，也要跨 event 还原，客户端不该看到 {{ }}。
func TestMessagesRedact_StreamRestoresSplitPlaceholder(t *testing.T) {
	orch := &redactFakeOrchestrator{}
	r := newMessagesHandlerForRedact(t, newRedactTestEngine(t, redact.Config{Mode: redact.ModeMask}), orch)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(string(anthropicBody(t, true))))
	if err != nil {
		t.Fatalf("POST /v1/messages: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)

	if strings.Contains(body, "{{") {
		t.Fatalf("流式响应仍含占位符：%s", body)
	}
	if !strings.Contains(body, anthRedactSecret) {
		t.Fatalf("流式响应未还原原文：%s", body)
	}
	if !strings.Contains(body, "message_stop") {
		t.Errorf("缺少 message_stop：%s", body)
	}
	if req, calls := orch.recordedStream(); calls != 1 || req == nil {
		t.Fatalf("上游流式调用次数 %d", calls)
	}
}

// TestMessagesRedact_FailClosedOnProtectError 保护失败时必须终止请求：
// 不调用上游、不走 failover、返回错误响应（绝不把原文发出去）。
func TestMessagesRedact_FailClosedOnProtectError(t *testing.T) {
	// MaxInputBytes 足够小，任何正文都会触发 ErrTooLarge，模拟保护失败。
	orch := &redactFakeOrchestrator{}
	r := newMessagesHandlerForRedact(t, newRedactTestEngine(t, redact.Config{Mode: redact.ModeMask, MaxInputBytes: 8}), orch)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(anthropicBody(t, false)))))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("失败应终止请求，状态码 %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "redaction_failed") {
		t.Fatalf("错误响应缺少 redaction_failed：%s", rec.Body.String())
	}
	if _, calls := orch.recordedChat(); calls != 0 {
		t.Fatalf("保护失败后仍调用了上游 %d 次（fail-open）", calls)
	}
}

// TestMessagesRedact_OffForwardsOriginal off 模式行为不变（不给线上引入差异）。
func TestMessagesRedact_OffForwardsOriginal(t *testing.T) {
	orch := &redactFakeOrchestrator{}
	r := newMessagesHandlerForRedact(t, nil, orch)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(anthropicBody(t, false)))))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d body=%s", rec.Code, rec.Body.String())
	}
	sent, _ := orch.recordedChat()
	if got, _ := sent.Messages[0].Content.(string); !strings.Contains(got, anthRedactSecret) {
		t.Fatalf("off 模式不应改写请求：%q", got)
	}
	if !strings.Contains(rec.Body.String(), anthRedactSecret) {
		t.Fatalf("off 模式响应未透传原文：%s", rec.Body.String())
	}
}
