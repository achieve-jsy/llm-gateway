package middleware_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Batch 1 · B4 的「DEBUG 正文日志对 multipart / 图像路径跳过」。
//
// 为什么要测：RequestBodyLogger 在 DEBUG 级会把【截断到 64KB 的原始正文】打进日志。
// 图像链路的正文可能是 base64 图片数据或 multipart 二进制，「半个 base64 串」既无
// 阅读价值又违反「base64 不入日志」的既定策略，因此整条 /v1/images/* 必须跳过。

// recordHandler 把 slog 记录收集到内存，供断言「是否记录了 request_body」。
type recordHandler struct {
	mu      sync.Mutex
	records []map[string]any
	level   slog.Level
}

func (h *recordHandler) Enabled(_ context.Context, level slog.Level) bool { return level >= h.level }

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{"msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, attrs)
	h.mu.Unlock()
	return nil
}

func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordHandler) requestBodyLogs() []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []map[string]any
	for _, rec := range h.records {
		if rec["msg"] == "request_body" {
			out = append(out, rec)
		}
	}
	return out
}

// withDebugSlog 把默认 logger 换成收集器（DEBUG 级），返回收集器与恢复函数。
func withDebugSlog(t *testing.T) *recordHandler {
	t.Helper()
	handler := &recordHandler{level: slog.LevelDebug}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return handler
}

func newBodyLoggerRouter(t *testing.T) (*gin.Engine, *string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var seen string
	router := gin.New()
	router.Use(middleware.RequestBodyLogger())
	router.POST("/*path", func(c *gin.Context) {
		body, err := c.GetRawData()
		require.NoError(t, err)
		seen = string(body)
		c.Status(http.StatusOK)
	})
	return router, &seen
}

func TestRequestBodyLogger_SkipsImagePaths(t *testing.T) {
	// 形状贴近真实的 generations 正文：prompt 里内嵌一段 base64 data-URI。
	const base64Payload = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8AAAwAB/AGtE1c8AAAAAElFTkSuQmCC"
	body := `{"model":"image-group","prompt":"data:image/png;base64,` + base64Payload + `"}`

	handler := withDebugSlog(t)
	router, seen := newBodyLoggerRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, body, *seen, "跳过日志不代表消费 body：下游 handler 必须仍能完整读取")

	logs := handler.requestBodyLogs()
	assert.Empty(t, logs, "图像生成路径不得记录正文（正文可能含 base64 图片数据）")

	// 再确认整份日志里也没有 base64 片段（防止其它字段夹带）。
	handler.mu.Lock()
	defer handler.mu.Unlock()
	for _, rec := range handler.records {
		if s, ok := rec["body"].(string); ok {
			assert.NotContains(t, s, base64Payload)
		}
	}
}

func TestRequestBodyLogger_SkipsImageEditsMultipart(t *testing.T) {
	handler := withDebugSlog(t)
	router, seen := newBodyLoggerRouter(t)

	// 一段含二进制外观字节的 multipart 正文（无需真的可解析：中间件只看路径）。
	body := "--BOUNDARY\r\nContent-Disposition: form-data; name=\"image[]\"; filename=\"a.png\"\r\n" +
		"Content-Type: image/png\r\n\r\n\x89PNG\r\n\x1a\n\x00\x00\r\n--BOUNDARY--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUNDARY")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, body, *seen)
	assert.Empty(t, handler.requestBodyLogs(), "图像编辑（multipart）路径不得记录正文")
}

func TestRequestBodyLogger_StillLogsChatPath(t *testing.T) {
	// 对照组：非媒体路径的 DEBUG 正文日志行为不变（否则 B4 的改动会误伤排障能力）。
	handler := withDebugSlog(t)
	router, seen := newBodyLoggerRouter(t)

	payload := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, payload, *seen, "对照组：body 读取后必须重置给下游")

	logs := handler.requestBodyLogs()
	require.Len(t, logs, 1, "chat 路径在 DEBUG 下仍应记录正文")
	assert.Equal(t, payload, logs[0]["body"])
	assert.Equal(t, "/v1/chat/completions", logs[0]["path"])
}

func TestRequestBodyLogger_NotDroppedWhenDebugDisabled(t *testing.T) {
	// DEBUG 关闭时中间件整体是无操作（既有语义不能回归）。
	handler := &recordHandler{level: slog.LevelInfo}
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	router, seen := newBodyLoggerRouter(t)
	payload := `{"model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, payload, *seen)
	assert.Empty(t, handler.requestBodyLogs())
}
