package handler

// Gemini 入站（/v1beta）此前完全没有脱敏：它的 body 不是 ChatRequest 形状，
// 加 handler 时被漏掉，结果 mask 模式下 chat/messages/responses 都已脱敏，
// 唯独 Gemini 把 prompt 原文发给了上游。
//
// 本测试锁住三件事：
//   1. 上游只看到占位符，拿不到原始凭证；
//   2. 响应里的占位符被还原成原文；
//   3. 脱敏生效时原生旁路必须被拦下（原生直发原始 body，与脱敏互斥）。

import (
	"context"
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

// geminiRedactBody 是 Gemini 原生入站的请求体，内含一枚内置规则能命中的凭证。
const geminiRedactBody = `{"contents":[{"role":"user","parts":[{"text":"我的 key 是 AKIAIOSFODNN7EXAMPLE 别外传"}]}]}`

// geminiRedactOrch 记录上游实际收到的文本；echo 时把收到的文本当回复原样返回。
type geminiRedactOrch struct {
	upstream string
	echo     bool
	native   bool // ChatNative 是否被调用（用于验证互斥）
}

func (o *geminiRedactOrch) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	o.upstream = redactMessageText(req)
	content := "ok"
	if o.echo {
		content = o.upstream
	}
	return &core.ChatResponse{
		ID: "chatcmpl-gemini-redact", Object: "chat.completion", Model: req.Model,
		Choices: []core.Choice{{Index: 0, Message: core.ResponseMessage{Role: "assistant", Content: content}}},
	}, nil
}

func (o *geminiRedactOrch) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, &core.GatewayError{StatusCode: http.StatusInternalServerError, Message: "not used"}
}

// 实现原生接口，用 native 标记暴露"原生分支被走到"这件事。
func (o *geminiRedactOrch) ChatNative(_ context.Context, _ *core.ChatRequest, body []byte, _ core.NativeRelayFormat) ([]byte, error) {
	o.native = true
	return body, nil
}

func (o *geminiRedactOrch) ChatNativeStream(_ context.Context, _ *core.ChatRequest, body []byte, _ core.NativeRelayFormat) (io.ReadCloser, error) {
	o.native = true
	return io.NopCloser(strings.NewReader("")), nil
}

func serveGeminiRedact(t *testing.T, orch *geminiRedactOrch, engine *redact.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewGeminiHandler(orch)
	h.SetRedactor(engine)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1beta/models/:modelAction", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// mask 模式下上游不得收到原始凭证，响应必须还原。
func TestGeminiHandler_RedactMask_UpstreamMasked_ResponseRestored(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &geminiRedactOrch{echo: true}

	w := serveGeminiRedact(t, orch, engine, geminiRedactBody)
	require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())

	// ① 上游只看到占位符
	assert.NotContains(t, orch.upstream, chatRedactSecret, "原始凭证不得发给上游（Gemini 入站曾整条绕过脱敏）")
	assert.Contains(t, orch.upstream, "{{", "上游应收到占位符")

	// ② 客户端看到还原后的原文
	assert.Contains(t, w.Body.String(), chatRedactSecret, "响应里的占位符必须还原成原文")
	assert.NotContains(t, w.Body.String(), "{{", "客户端不应看到占位符")
}

// 脱敏生效时不得走原生旁路：原生把原始 body 直发上游，脱敏改写的是解析后的
// ChatRequest，两者不可能同时成立。
func TestGeminiHandler_RedactMask_BlocksNativeRelay(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &geminiRedactOrch{}

	w := serveGeminiRedact(t, orch, engine, geminiRedactBody)
	require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())

	assert.False(t, orch.native, "mask 模式下原生旁路必须被拦下，否则原始 body 会直发上游")
	assert.Contains(t, orch.upstream, "{{", "被拦下后应走降级路径，且降级路径已脱敏")
}

// detect 模式不阻断原生旁路（与 chat/messages 的既有判据一致）。
func TestGeminiHandler_RedactDetect_AllowsNativeRelay(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeDetect, RulesOnly: true})
	orch := &geminiRedactOrch{}

	w := serveGeminiRedact(t, orch, engine, geminiRedactBody)
	require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())

	assert.True(t, orch.native, "detect 只观测不改写，不应阻断原生旁路")
}
