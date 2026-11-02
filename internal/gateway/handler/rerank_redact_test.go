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

// rrRedactSecret 是内置规则稳定能命中的凭证（AWS Access Key ID）。
const rrRedactSecret = "AKIAIOSFODNN7EXAMPLE"

// redactRerankOrch 记录上游实际收到的 query/documents，并按 return_documents
// 语义回显上游看到的（即脱敏后的）文档原文，用于验证还原。
type redactRerankOrch struct {
	calls int
	query any
	docs  []any
}

func (f *redactRerankOrch) Rerank(_ context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	f.calls++
	f.query = req.Query
	f.docs = req.Documents

	results := make([]core.RerankResult, 0, len(req.Documents))
	for i, d := range req.Documents {
		r := core.RerankResult{Index: i, RelevanceScore: 1 - float64(i)/10}
		switch x := d.(type) {
		case string:
			r.Document = &core.RerankDocument{Text: x}
		case map[string]any:
			if t, ok := x["text"].(string); ok {
				r.Document = &core.RerankDocument{Text: t}
			}
		}
		results = append(results, r)
	}
	return &core.RerankResponse{
		ID:      "rerank-redact",
		Model:   req.Model,
		Results: results,
		Usage:   core.RerankUsage{TotalTokens: 10},
	}, nil
}

func serveRerankRedact(t *testing.T, orch *redactRerankOrch, engine *redact.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewRerankHandler(orch)
	h.SetRedactor(engine)
	router.POST("/v1/rerank", h.Handle)

	req := httptest.NewRequest(http.MethodPost, "/v1/rerank", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ① mask 模式下上游收到的 query/documents 里没有原始凭证；
// 响应回显的文档（return_documents）必须还原成原文。
func TestRerankHandler_RedactMask_UpstreamMasked_ResponseRestored(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	orch := &redactRerankOrch{}

	body := `{"model":"bge-reranker-v2-m3","return_documents":true,` +
		`"query":"我的 key 是 ` + rrRedactSecret + `",` +
		`"documents":["文档里的 ` + rrRedactSecret + `","普通文档",{"text":"对象里的 ` + rrRedactSecret + `"}]}`

	w := serveRerankRedact(t, orch, engine, body)
	require.Equal(t, http.StatusOK, w.Code, "响应体: %s", w.Body.String())
	require.Equal(t, 1, orch.calls)

	// query 已脱敏
	q, ok := orch.query.(string)
	require.True(t, ok, "上游 query 形态变了: %T", orch.query)
	assert.NotContains(t, q, rrRedactSecret, "原始凭证不得发给上游")
	assert.Contains(t, q, "{{", "上游应收到占位符")

	// documents 的 string 与 {text} 均已脱敏
	require.Len(t, orch.docs, 3)
	assert.NotContains(t, orch.docs[0], rrRedactSecret)
	assert.Contains(t, orch.docs[0], "{{")
	assert.Equal(t, "普通文档", orch.docs[1])
	textObj, ok := orch.docs[2].(map[string]any)
	require.True(t, ok)
	textVal, ok := textObj["text"].(string)
	require.True(t, ok)
	assert.NotContains(t, textVal, rrRedactSecret)
	assert.Contains(t, textVal, "{{")

	// 响应回显的文档必须还原
	var resp core.RerankResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 3)
	require.NotNil(t, resp.Results[0].Document)
	assert.Contains(t, resp.Results[0].Document.Text, rrRedactSecret, "回显文档里的占位符必须还原")
	assert.NotContains(t, resp.Results[0].Document.Text, "{{")
	require.NotNil(t, resp.Results[2].Document)
	assert.Contains(t, resp.Results[2].Document.Text, rrRedactSecret)
	assert.NotContains(t, resp.Results[2].Document.Text, "{{")
}

// ② 视觉模态（image / video）逐字节不变，即使 URL 里出现形似凭证的串；
// 纯文本形态的 query / document 才脱敏。
// image/video 请求会被 handler 的多模态路由闸门要求 rerank_vl 模型，
// 因此这里直接验证脱敏函数本身（形态判断逻辑的唯一入口）。
func TestProtectRerankInput_VisualModalitiesUnchanged(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true})
	session := engine.NewSession()
	defer session.Close()

	imgURL := "https://cdn.example.com/a.png?token=" + rrRedactSecret
	videoURL := "https://cdn.example.com/v.mp4#tag=" + rrRedactSecret
	req := &core.RerankRequest{
		Query: map[string]any{"image": imgURL},
		Documents: []any{
			map[string]any{"image": imgURL},
			map[string]any{"video": videoURL},
			"文本里的 " + rrRedactSecret,
			map[string]any{"text": "对象里的 " + rrRedactSecret},
		},
	}

	require.NoError(t, protectRerankInput(context.Background(), session, req))

	// image / video：值与形态都不得变化
	assert.Equal(t, map[string]any{"image": imgURL}, req.Query)
	assert.Equal(t, map[string]any{"image": imgURL}, req.Documents[0])
	assert.Equal(t, map[string]any{"video": videoURL}, req.Documents[1])
	rawQuery, err := json.Marshal(req.Query)
	require.NoError(t, err)
	assert.Equal(t, `{"image":"`+imgURL+`"}`, string(rawQuery), "image query 必须逐字节不变")

	// 文本形态仍然脱敏
	assert.Contains(t, req.Documents[2], "{{")
	assert.NotContains(t, req.Documents[2], rrRedactSecret)
	textObj := req.Documents[3].(map[string]any)
	textVal, ok := textObj["text"].(string)
	require.True(t, ok)
	assert.Contains(t, textVal, "{{")
	assert.NotContains(t, textVal, rrRedactSecret)
}

// ③ Protect 失败 → 500 + redaction_failed，绝不调用上游。
func TestRerankHandler_RedactProtectFailure_FailsClosed(t *testing.T) {
	engine := newRedactEngine(t, redact.Config{Mode: redact.ModeMask, RulesOnly: true, MaxInputBytes: 8})
	orch := &redactRerankOrch{}

	w := serveRerankRedact(t, orch, engine,
		`{"model":"bge-reranker-v2-m3","query":"这段查询超过八个字节","documents":["短"]}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Zero(t, orch.calls, "保护失败时不得调用上游")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj, ok := resp["error"].(map[string]any)
	require.True(t, ok, "应返回统一错误体: %s", w.Body.String())
	assert.Equal(t, "redaction_failed", errObj["code"])
}
