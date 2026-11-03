package handler_test

// /v1/responses legacy 路径的 E2E。
//
// 此前 /v1/responses 只有 native 旁路的测试；legacy 路径（policy 未配置 active 时
// 的默认行为）零覆盖，而它承载默认流量。本文件补上这条路径的基本正确性：
// 请求体解码 → orchestrator.Responses → 原样返回 Responses 格式。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// legacyResponsesOrch 只实现 Responses 路径，不实现原生接口、不实现 Chat 接口。
// responses legacy 路径直接调 orchestrator.Responses，不经过 Chat。
type legacyResponsesOrch struct {
	calls int32
	last  *core.ResponsesRequest
}

func (o *legacyResponsesOrch) Responses(_ context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	atomic.AddInt32(&o.calls, 1)
	o.last = req
	return &core.ResponsesResponse{
		ID:        "resp-legacy",
		Object:    "response",
		CreatedAt: 1700000000,
		Model:     req.Model,
		Status:    "completed",
		Output: []core.ResponsesOutputItem{{
			Type:   "message",
			ID:     "msg-1",
			Role:   "assistant",
			Status: "completed",
			Content: []core.ResponsesContentItem{{
				Type: "output_text",
				Text: "hello from legacy",
			}},
		}},
		Usage: &core.ResponsesUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7},
	}, nil
}

func (o *legacyResponsesOrch) ResponsesStream(_ context.Context, _ *core.ResponsesRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("data: {}\n\n")), nil
}

func TestResponsesLegacyPath_E2E(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &legacyResponsesOrch{}
	h := handler.NewResponsesHandler(orch)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("key_id", "test-key")
		c.Set("request_id", "req-resp-legacy")
	})
	r.POST("/v1/responses", h.Handle)

	body := `{"model":"gpt-4o-mini","input":"hello"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.EqualValues(t, 1, atomic.LoadInt32(&orch.calls), "legacy 路径必须调用 orchestrator.Responses 一次")
	require.NotNil(t, orch.last)
	require.Equal(t, "gpt-4o-mini", orch.last.Model, "请求必须解码出 model 并传给 orchestrator")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "response", resp["object"])
	require.Equal(t, "resp-legacy", resp["id"])
}

// legacy 路径收到非法 JSON 必须 400，不能把坏请求打到上游。
func TestResponsesLegacyPath_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	orch := &legacyResponsesOrch{}
	h := handler.NewResponsesHandler(orch)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/responses", h.Handle)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	require.EqualValues(t, 0, atomic.LoadInt32(&orch.calls), "解码失败不得调用上游")
}
