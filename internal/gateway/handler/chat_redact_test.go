package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/redact"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chatRedactSecret 是内置规则稳定能命中的凭证（AWS Access Key ID）。
const chatRedactSecret = "AKIAIOSFODNN7EXAMPLE"

// redactFakeOrch 记录上游实际收到的文本，不连真上游。
type redactFakeOrch struct {
	chatHits int
	upstream string // 上游收到的 message 文本（脱敏后的样子）
	echo     bool   // true: 把收到的文本原样当回复内容（验证还原）
	stream   func(req *core.ChatRequest) (io.ReadCloser, error)
}

func (f *redactFakeOrch) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	f.chatHits++
	f.upstream = redactMessageText(req)
	if f.echo {
		return &core.ChatResponse{
			ID: "chatcmpl-redact", Object: "chat.completion", Model: req.Model,
			Choices: []core.Choice{{Index: 0, Message: core.ResponseMessage{Role: "assistant", Content: f.upstream}}},
		}, nil
	}
	return &core.ChatResponse{ID: "chatcmpl-redact", Object: "chat.completion", Model: req.Model}, nil
}

func (f *redactFakeOrch) ChatStream(_ context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	f.chatHits++
	f.upstream = redactMessageText(req)
	return f.stream(req)
}

// redactMessageText 取出第一条消息的纯文本内容。
func redactMessageText(req *core.ChatRequest) string {
	if req == nil || len(req.Messages) == 0 {
		return ""
	}
	if s, ok := req.Messages[0].Content.(string); ok {
		return s
	}
	return ""
}

// redactPlaceholder 返回文本里第一个占位符（形如 {{API_KEY_xxx}}）。
func redactPlaceholder(t *testing.T, text string) string {
	t.Helper()
	a := strings.Index(text, "{{")
	b := strings.Index(text, "}}")
	require.True(t, a >= 0 && b > a, "期望上游文本里含占位符，实际: %q", text)
	return text[a : b+2]
}

// sseEvent 拼一个 OpenAI 风格的 content delta 帧。
func sseEvent(t *testing.T, content string) string {
	t.Helper()
	b, err := json.Marshal(gin.H{"choices": []gin.H{{"delta": gin.H{"content": content}}}})
	require.NoError(t, err)
	return "data: " + string(b) + "\n\n"
}

func newRedactEngine(t *testing.T, cfg redact.Config) *redact.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine, err := redact.New(cfg)
	require.NoError(t, err)
	return engine
}

const redactUserBody = "{\"model\":\"gpt-4\",\"messages\":[{\"role\":\"user\",\"content\":\"我的 key 是 AKIAIOSFODNN7EXAMPLE 别外传\"}]}"
const redactUserStreamBody = "{\"model\":\"gpt-4\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"我的 key 是 AKIAIOSFODNN7EXAMPLE 别外传\"}]}"

func serveRedact(t *testing.T, orch *redactFakeOrch, engine *redact.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	h := NewChatHandler(orch)
	h.SetRedactor(engine)
	router.POST("/v1/chat/completions", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ① mask 模式下上游收到的请求里没有原始凭证；② 响应里的占位符被还原。
func TestChatHandler_RedactMask_UpstreamMasked_ResponseRestored(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &redactFakeOrch{echo: true}

	w := serveRedact(t, orch, engine, redactUserBody)
	require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())

	// ① 上游只看到占位符
	assert.NotContains(t, orch.upstream, chatRedactSecret, "原始凭证不得发给上游")
	assert.Contains(t, orch.upstream, "{{", "上游应收到占位符")

	// ② 客户端看到的是还原后的原文
	var resp struct {
		Choices []struct {
			Message struct {
				Content string "json:\"content\""
			} "json:\"message\""
		} "json:\"choices\""
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Choices, 1)
	assert.Contains(t, resp.Choices[0].Message.Content, chatRedactSecret, "响应里的占位符必须还原")
	assert.NotContains(t, resp.Choices[0].Message.Content, "{{")
}

// 流式：占位符被切在两帧之间也要还原。
func TestChatHandler_RedactMask_StreamRestored(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &redactFakeOrch{}
	orch.stream = func(req *core.ChatRequest) (io.ReadCloser, error) {
		text := redactMessageText(req)
		ph := redactPlaceholder(t, text)
		at := strings.Index(text, ph)
		mid := at + len(ph)/2
		// 故意把占位符从中间切开，跨两帧送出
		return io.NopCloser(strings.NewReader(
			sseEvent(t, text[:mid]) + sseEvent(t, text[mid:]) + "data: [DONE]\n\n",
		)), nil
	}

	w := serveRedact(t, orch, engine, redactUserStreamBody)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, orch.upstream, chatRedactSecret, "原始凭证不得发给上游")

	out := w.Body.String()
	assert.Contains(t, out, chatRedactSecret, "流里的占位符必须还原")
	assert.NotContains(t, out, "{{", "客户端不应看到任何占位符")
}

// ③ Protect 失败 → 直接报错，绝不调用上游（也就不会 failover）。
func TestChatHandler_RedactProtectFailure_FailsClosed(t *testing.T) {
	// MaxInputBytes 很小，任何非空消息都会让 Protect 失败。
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true, MaxInputBytes: 8})
	orch := &redactFakeOrch{}

	w := serveRedact(t, orch, engine, "{\"model\":\"gpt-4\",\"messages\":[{\"role\":\"user\",\"content\":\"这段内容超过八个字节\"}]}")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Zero(t, orch.chatHits, "保护失败时不得调用上游")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "应返回统一错误体: %s", w.Body.String())
	// 与 /v1/messages 对齐的专用错误码：客户端可区分隐私保护失败与上游故障
	assert.Equal(t, "redaction_failed", errObj["code"])
}
