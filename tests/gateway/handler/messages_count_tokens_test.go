package handler_test

// /v1/messages/count_tokens 的覆盖。
//
// 这个端点此前零测试：它是纯本地估算、不打上游、不产生计费，但返回值直接
// 影响客户端的分片/截断决策，算错会让客户端把请求裁坏。
// Gemini 侧的 :countTokens 有两个测试（gemini_native_e2e_test.go），
// 这里补齐 Anthropic 侧。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func postCountTokens(t *testing.T, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	h := handler.NewMessagesHandler(&capturingMessagesOrchestrator{})
	r.POST("/v1/messages/count_tokens", h.HandleCountTokens)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var out map[string]any
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	}
	return w, out
}

func TestCountTokens_ReturnsPositiveEstimate(t *testing.T) {
	w, out := postCountTokens(t, `{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hello world"}]}`)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, out, "input_tokens")
	tokens, ok := out["input_tokens"].(float64)
	require.True(t, ok, "input_tokens 必须是数字")
	require.Greater(t, tokens, float64(0), "估算值必须为正")
}

// 中文不能被严重低估：历史实现用 totalChars/4，中文一字接近一 token，
// 那个口径会把中文请求估成实际的 1/4，客户端据此截断就会丢内容。
func TestCountTokens_ChineseNotUnderestimated(t *testing.T) {
	// 40 个中文字符。合理估算应在几十的量级；totalChars/4 会给出 10。
	text := strings.Repeat("中", 40)
	body := `{"model":"claude-opus-4-6","messages":[{"role":"user","content":"` + text + `"}]}`

	w, out := postCountTokens(t, body)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	tokens := out["input_tokens"].(float64)
	require.Greater(t, tokens, float64(10),
		"中文 40 字的估算不应退化到 totalChars/4=10 那一档（实际得到 %v）", tokens)
	require.Less(t, tokens, float64(200), "估算不应离奇偏大（实际 %v）", tokens)
}

// 空内容也必须给正数：返回 0 会让客户端逻辑出现除零/空分片。
func TestCountTokens_NeverZero(t *testing.T) {
	w, out := postCountTokens(t, `{"model":"claude-opus-4-6","messages":[{"role":"user","content":""}]}`)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.GreaterOrEqual(t, out["input_tokens"].(float64), float64(1),
		"估算值下限必须是 1")
}

// 视频块必须 400：Anthropic 协议没有视频输入，静默丢弃会让用户以为模型看过视频。
func TestCountTokens_RejectsVideoBlock(t *testing.T) {
	body := `{"model":"claude-opus-4-6","messages":[{"role":"user","content":[
		{"type":"text","text":"这段视频讲了什么"},
		{"type":"video_url","video_url":{"url":"https://a/b.mp4"}}
	]}]}`

	w, _ := postCountTokens(t, body)
	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}

// 非法 JSON 必须 400。
func TestCountTokens_InvalidJSON(t *testing.T) {
	w, _ := postCountTokens(t, `{not json`)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}
