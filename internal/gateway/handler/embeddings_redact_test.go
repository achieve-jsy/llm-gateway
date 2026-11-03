package handler

import (
	"context"
	"encoding/json"
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

// embRedactSecret 是内置规则稳定能命中的凭证（AWS Access Key ID）。
const embRedactSecret = "AKIAIOSFODNN7EXAMPLE"

// redactEmbeddingsOrch 记录上游实际收到的 input，不连真上游。
type redactEmbeddingsOrch struct {
	calls int
	input any
}

func (f *redactEmbeddingsOrch) Embeddings(_ context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	f.calls++
	f.input = req.Input
	return &core.EmbeddingResponse{
		Object: "list",
		Model:  req.Model,
		Data: []core.EmbeddingData{
			{Object: "embedding", Embedding: json.RawMessage("[0.1,0.2]"), Index: 0},
		},
		Usage: core.EmbeddingUsage{PromptTokens: 3, TotalTokens: 3},
	}, nil
}

func serveEmbeddingsRedact(t *testing.T, orch *redactEmbeddingsOrch, engine *redact.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewEmbeddingsHandler(orch)
	h.SetRedactor(engine)
	router.POST("/v1/embeddings", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ① mask 模式下上游收到的 input 里没有原始凭证（嵌入响应没有文本，无需还原）。
func TestEmbeddingsHandler_RedactMask_UpstreamMasked(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &redactEmbeddingsOrch{}

	w := serveEmbeddingsRedact(t, orch, engine,
		`{"model":"text-embedding-3-small","input":"我的 key 是 `+embRedactSecret+` 别外传"}`)
	require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())
	require.Equal(t, 1, orch.calls)

	upstream, ok := orch.input.(string)
	require.True(t, ok, "上游 input 形态变了: %T", orch.input)
	assert.NotContains(t, upstream, embRedactSecret, "原始凭证不得发给上游")
	assert.Contains(t, upstream, "{{", "上游应收到占位符")

	// 响应只有向量，客户端直接拿到，不需要（也不应）出现占位符
	assert.NotContains(t, w.Body.String(), "{{")
}

// ② 非文本形态逐字节不变：token 数组（数字/嵌套数组）原样转发，只有字符串元素被脱敏。
func TestEmbeddingsHandler_RedactTokenArraysUnchanged(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})

	t.Run("flat token array", func(t *testing.T) {
		orch := &redactEmbeddingsOrch{}
		w := serveEmbeddingsRedact(t, orch, engine, `{"model":"m","input":[1,2,3]}`)
		require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())
		got, err := json.Marshal(orch.input)
		require.NoError(t, err)
		assert.Equal(t, "[1,2,3]", string(got), "token 数组必须逐字节原样转发")
	})

	t.Run("nested token array", func(t *testing.T) {
		orch := &redactEmbeddingsOrch{}
		w := serveEmbeddingsRedact(t, orch, engine, `{"model":"m","input":[[1,2],[3,4]]}`)
		require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())
		got, err := json.Marshal(orch.input)
		require.NoError(t, err)
		assert.Equal(t, "[[1,2],[3,4]]", string(got), "嵌套 token 数组必须逐字节原样转发")
	})

	t.Run("mixed array only strings masked", func(t *testing.T) {
		orch := &redactEmbeddingsOrch{}
		w := serveEmbeddingsRedact(t, orch, engine,
			`{"model":"m","input":["`+embRedactSecret+`",7]}`)
		require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())

		items, ok := orch.input.([]any)
		require.True(t, ok, "上游 input 形态变了: %T", orch.input)
		require.Len(t, items, 2)
		assert.Equal(t, float64(7), items[1], "数字形态不得被改写")
		s, ok := items[0].(string)
		require.True(t, ok)
		assert.NotContains(t, s, embRedactSecret)
		assert.Contains(t, s, "{{")
	})
}

// 强类型 token 数组（[]int / [][]int）不经 JSON 时也必须原样返回。
func TestProtectEmbeddingInput_StrongTypedTokensUnchanged(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	session := engine.NewSession()
	defer session.Close()

	tokens := []int{1, 2, 3}
	out, err := protectEmbeddingInput(context.Background(), session, tokens)
	require.NoError(t, err)
	assert.Equal(t, tokens, out)

	nested := [][]int{{1, 2}, {3}}
	out, err = protectEmbeddingInput(context.Background(), session, nested)
	require.NoError(t, err)
	assert.Equal(t, nested, out)

	// []string 走逐项脱敏
	list := []string{"我的 key 是 " + embRedactSecret, "普通文本"}
	out, err = protectEmbeddingInput(context.Background(), session, list)
	require.NoError(t, err)
	masked, ok := out.([]string)
	require.True(t, ok)
	assert.NotContains(t, masked[0], embRedactSecret)
	assert.Contains(t, masked[0], "{{")
	assert.Equal(t, "普通文本", masked[1])
}

// ③ Protect 失败 → 500 + redaction_failed，绝不调用上游。
func TestEmbeddingsHandler_RedactProtectFailure_FailsClosed(t *testing.T) {
	// MaxInputBytes 很小，任何非空文本都会让 Protect 失败。
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true, MaxInputBytes: 8})
	orch := &redactEmbeddingsOrch{}

	w := serveEmbeddingsRedact(t, orch, engine, `{"model":"m","input":"这段内容超过八个字节"}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Zero(t, orch.calls, "保护失败时不得调用上游")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "应返回统一错误体: %s", w.Body.String())
	assert.Equal(t, "redaction_failed", errObj["code"])
}
