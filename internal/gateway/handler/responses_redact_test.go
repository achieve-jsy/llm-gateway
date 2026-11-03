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

// responsesRedactSecret 是内置规则稳定能命中的凭证（AWS Access Key ID），
// 出现在用户正文、instructions 与工具结果里，三处都必须被遮。
const responsesRedactSecret = "AKIAIOSFODNN7EXAMPLE"

// responsesToolSecret 只出现在 tools 定义里：工具定义一律不动，必须原样到上游。
const responsesToolSecret = "AKIAABCDEFGHIJKLMNOP"

// responsesRedactOrch 记录上游真实收到的请求（序列化后的样子），不连真上游。
type responsesRedactOrch struct {
	calls    int
	upstream string // 上游收到的完整请求 JSON（脱敏后的样子）
	echo     bool   // true: 把收到的第一段文本当回复内容（验证还原）
	stream   func(req *core.ResponsesRequest) (io.ReadCloser, error)
}

func (f *responsesRedactOrch) Responses(_ context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	f.calls++
	b, _ := json.Marshal(req)
	f.upstream = string(b)
	resp := &core.ResponsesResponse{ID: "resp_redact", Object: "response", Model: req.Model, Status: "completed"}
	if f.echo {
		resp.Output = []core.ResponsesOutputItem{{
			ID: "msg_1", Type: "message", Role: "assistant", Status: "completed",
			Content: []core.ResponsesContentItem{{Type: "output_text", Text: responsesFirstText(req)}},
		}}
	}
	return resp, nil
}

func (f *responsesRedactOrch) ResponsesStream(_ context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	f.calls++
	b, _ := json.Marshal(req)
	f.upstream = string(b)
	return f.stream(req)
}

// responsesFirstText 取出 input 第一段纯文本（message content 的 input_text 或字符串）。
func responsesFirstText(req *core.ResponsesRequest) string {
	els, ok := req.Input.([]core.ResponsesInputElement)
	if !ok || len(els) == 0 {
		return ""
	}
	switch c := els[0].Content.(type) {
	case string:
		return c
	case []any:
		for _, p := range c {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

const responsesRedactBody = `{
  "model": "gpt-4o",
  "instructions": "系统提示里也带了 AKIAIOSFODNN7EXAMPLE",
  "tools": [{"type": "function", "name": "f", "description": "文档示例 AKIAABCDEFGHIJKLMNOP 不许改"}],
  "input": [
    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "我的 key 是 AKIAIOSFODNN7EXAMPLE 别外传"}]},
    {"type": "function_call_output", "call_id": "call_1", "output": "工具读到的 AKIAIOSFODNN7EXAMPLE 内容"}
  ]
}`

const responsesRedactStreamBody = `{
  "model": "gpt-4o", "stream": true,
  "input": [{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "我的 key 是 AKIAIOSFODNN7EXAMPLE 别外传"}]}]
}`

func serveResponsesRedact(t *testing.T, orch *responsesRedactOrch, engine *redact.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewResponsesHandler(orch)
	h.SetRedactor(engine)
	router.POST("/v1/responses", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ① mask 模式下上游拿不到任何原文（正文 / instructions / 工具结果），工具定义不动；
// ② 响应 output_text 里的占位符被还原。
func TestResponsesHandler_RedactMask_UpstreamMasked_ResponseRestored(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &responsesRedactOrch{echo: true}

	w := serveResponsesRedact(t, orch, engine, responsesRedactBody)
	require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())
	require.Equal(t, 1, orch.calls)

	// ① 上游只看到占位符
	assert.NotContains(t, orch.upstream, responsesRedactSecret, "原始凭证不得发给上游")
	assert.Contains(t, orch.upstream, "{{", "上游应收到占位符")
	assert.Contains(t, orch.upstream, "call_1", "tool_call id 不得改动")
	assert.Contains(t, orch.upstream, responsesToolSecret, "工具定义一律不动")

	// ② 客户端看到的是还原后的原文
	var resp core.ResponsesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Output, 1)
	require.Len(t, resp.Output[0].Content, 1)
	assert.Contains(t, resp.Output[0].Content[0].Text, responsesRedactSecret, "响应里的占位符必须还原")
	assert.NotContains(t, resp.Output[0].Content[0].Text, "{{", "客户端不应看到占位符")
}

// 流式：raw SSE 逐 event 还原，占位符被切在两帧之间也要拼回来。
func TestResponsesHandler_RedactMask_StreamRestored(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &responsesRedactOrch{}
	orch.stream = func(req *core.ResponsesRequest) (io.ReadCloser, error) {
		text := responsesFirstText(req)
		ph := redactPlaceholder(t, text)
		at := strings.Index(text, ph)
		mid := at + len(ph)/2
		frame := func(delta string) string {
			b, err := json.Marshal(gin.H{"type": "response.output_text.delta", "delta": delta})
			require.NoError(t, err)
			return "event: response.output_text.delta\ndata: " + string(b) + "\n\n"
		}
		return io.NopCloser(strings.NewReader(frame(text[:mid]) + frame(text[mid:]))), nil
	}

	w := serveResponsesRedact(t, orch, engine, responsesRedactStreamBody)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, orch.upstream, responsesRedactSecret, "原始凭证不得发给上游")

	out := w.Body.String()
	assert.Contains(t, out, responsesRedactSecret, "流里的占位符必须还原")
	assert.NotContains(t, out, "{{", "客户端不应看到任何占位符")
}

// ③ Protect 失败 → 500 + redaction_failed，绝不调用上游（也就不会 failover）。
func TestResponsesHandler_RedactProtectFailure_FailsClosed(t *testing.T) {
	// MaxInputBytes 很小，任何非空文本都会让 Protect 失败。
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true, MaxInputBytes: 8})
	orch := &responsesRedactOrch{}

	w := serveResponsesRedact(t, orch, engine, responsesRedactBody)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Zero(t, orch.calls, "保护失败时不得调用上游")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "应返回统一错误体: %s", w.Body.String())
	assert.Equal(t, "redaction_failed", errObj["code"])
}
