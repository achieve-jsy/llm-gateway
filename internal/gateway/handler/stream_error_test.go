package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/example/llm-gateway/internal/core"
)

// upstreamStreamError 复刻 2026-10-10 生产事故的报文：
// Bedrock ConverseStream 在流内回了一个 validationException 事件（SDK 以 Read 错误
// 抛出），网关修复前把它吞掉，客户端只看到“正常结束但没有任何内容”。
func upstreamStreamError() error {
	return core.NewProviderError("bedrock", http.StatusBadRequest,
		`ValidationException: The model returned the following errors: {"error":{"code":"validation_error",`+
			`"message":"The task request was rejected by the target","param":null,"type":"invalid_request_error"}}`, nil)
}

// failingStream 先回放收到的 SSE 数据，随后以 err 结束。
type failingStream struct {
	chunks [][]byte
	idx    int
	err    error
	closed bool
}

func (s *failingStream) Read(p []byte) (int, error) {
	if s.idx < len(s.chunks) {
		n := copy(p, s.chunks[s.idx])
		s.idx++
		return n, nil
	}
	return 0, s.err
}

func (s *failingStream) Close() error { s.closed = true; return nil }

func newStreamTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("request_id", "req_stream_err")
	return c, w
}

func TestHandleStreamResponse_OpenAIDialectEmitsErrorFrame(t *testing.T) {
	c, w := newStreamTestContext()
	stream := &failingStream{
		chunks: [][]byte{[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")},
		err:    upstreamStreamError(),
	}

	HandleStreamResponse(c, stream)

	body := w.Body.String()
	if !strings.Contains(body, "partial") {
		t.Errorf("已产生的数据应照常下发，实际 %q", body)
	}
	if !strings.Contains(body, `data: {"error":`) {
		t.Fatalf("缺少 OpenAI 方言错误帧，实际 %q", body)
	}
	if !strings.Contains(body, "rejected by the target") {
		t.Errorf("错误帧应带上游报文，实际 %q", body)
	}
	if strings.Contains(body, "[DONE]") {
		t.Error("上游失败时不应再发 [DONE]，否则客户端会当成正常结束")
	}
	if msg := c.GetString("error_message"); !strings.Contains(msg, "rejected by the target") {
		t.Errorf("error_message 未登记，实际 %q", msg)
	}
	if !stream.closed {
		t.Error("流应被关闭")
	}
}

func TestHandleStreamResponse_AnthropicDialectEmitsErrorEvent(t *testing.T) {
	c, w := newStreamTestContext()
	stream := &failingStream{
		chunks: [][]byte{[]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")},
		err:    upstreamStreamError(),
	}

	HandleStreamResponseAs(c, stream, DialectAnthropic)

	body := w.Body.String()
	if !strings.Contains(body, "event: error\n") {
		t.Fatalf("缺少 Anthropic 方言的 event: error，实际 %q", body)
	}
	if !strings.Contains(body, `"type":"error"`) {
		t.Errorf("错误事件 JSON 结构不对，实际 %q", body)
	}
	if !strings.Contains(body, "rejected by the target") {
		t.Errorf("错误事件应带上游报文，实际 %q", body)
	}
	if strings.Contains(body, "message_stop") {
		t.Error("上游失败时不应发 message_stop，否则客户端会当成正常结束")
	}
	if n := openAIErrorFrames(body); n != 0 {
		t.Errorf("Anthropic 方言下不应混入 OpenAI 的 error 帧，实际 %d 个", n)
	}
	if msg := c.GetString("error_message"); msg == "" {
		t.Error("error_message 未登记")
	}
}

// openAIErrorFrames 统计「没有 event: 头、payload 形如 {"error":...}」的 data 帧数量，
// 用于区分 OpenAI 方言错误帧与 Anthropic 的 event: error 事件。
func openAIErrorFrames(body string) int {
	count := 0
	for _, block := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		hasEventHeader := false
		payload := ""
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "event:") {
				hasEventHeader = true
			}
			if strings.HasPrefix(line, "data:") {
				payload = strings.TrimSpace(line[5:])
			}
		}
		if hasEventHeader || payload == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			continue
		}
		if _, ok := m["error"]; ok {
			count++
		}
	}
	return count
}

// TestHandleStreamResponse_EOFEmitsNoErrorFrame 保证正常结束（EOF）不会多出错误帧。
func TestHandleStreamResponse_EOFEmitsNoErrorFrame(t *testing.T) {
	for _, dialect := range []StreamDialect{DialectOpenAI, DialectAnthropic} {
		c, w := newStreamTestContext()
		stream := &failingStream{
			chunks: [][]byte{[]byte("data: {\"choices\":[]}\n\n")},
			err:    io.EOF,
		}

		HandleStreamResponseAs(c, stream, dialect)

		body := w.Body.String()
		if strings.Contains(body, "error") {
			t.Errorf("dialect=%v 正常结束却写了错误帧：%q", dialect, body)
		}
		if msg := c.GetString("error_message"); msg != "" {
			t.Errorf("dialect=%v 正常结束却登记了 error_message=%q", dialect, msg)
		}
	}
}

// TestAnthropicStreamAdapter_UpstreamErrorPropagates 验证适配器不再把上游失败
// 伪装成一次“正常结束、内容为空”的回答（这正是 Claude Code 报
// "Streaming response ended before any complete data was received" 的原因）。
func TestAnthropicStreamAdapter_UpstreamErrorPropagates(t *testing.T) {
	upstream := &failingStream{
		chunks: [][]byte{[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")},
		err:    upstreamStreamError(),
	}
	adapter := newAnthropicStreamAdapter(upstream, "req_err", "grok-4.6", 0)

	out, err := io.ReadAll(adapter)
	if err == nil {
		t.Fatal("上游错误应沿着 pipe 传播给调用方，实际为 nil")
	}
	if !strings.Contains(err.Error(), "rejected by the target") {
		t.Errorf("传播的错误应保留上游报文，实际 %v", err)
	}
	if strings.Contains(string(out), "message_stop") {
		t.Error("上游失败时不应发出 message_stop")
	}
}

// TestAnthropicStreamAdapter_CleanEOFStillFinishes 现有行为回归：
// 正常到达 [DONE] 时仍然正常收尾。
func TestAnthropicStreamAdapter_CleanEOFStillFinishes(t *testing.T) {
	out := feedAnthropicStream(t,
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", 0)
	if !strings.Contains(out, "message_stop") {
		t.Fatalf("正常结束应发 message_stop，实际 %q", out)
	}
	if strings.Contains(out, "event: error") {
		t.Errorf("正常结束不应有 error 事件：%q", out)
	}
}

// TestMessagesStreamPath_UpstreamErrorReachesClient 按 /v1/messages 的真实组合
// （anthropic 适配器 + HandleStreamResponseAs）起一个 HTTP 服务，验证上游在流内
// 报错时客户端最终看到的是 event: error，而不是一个内容为空的正常结束。
func TestMessagesStreamPath_UpstreamErrorReachesClient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/v1/messages", func(c *gin.Context) {
		c.Set("request_id", "req_compose")
		upstream := &failingStream{
			chunks: [][]byte{[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"part\"}}]}\n\n")},
			err:    upstreamStreamError(),
		}
		adapter := newAnthropicStreamAdapter(upstream, c.GetString("request_id"), "grok-4.6", 0)
		HandleStreamResponseAs(c, adapter, DialectAnthropic)
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /v1/messages: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	out := string(body)
	if !strings.Contains(out, "event: error\n") {
		t.Fatalf("客户端应收到 event: error，实际 %q", out)
	}
	if !strings.Contains(out, "rejected by the target") {
		t.Errorf("错误事件应带上游报文，实际 %q", out)
	}
	if strings.Contains(out, "message_stop") {
		t.Error("上游失败时不应发 message_stop")
	}
}
