package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTraceDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))
	return db
}

func startTraceWriter(t *testing.T, db *gorm.DB) (*gwobs.TraceWriter, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tw := gwobs.NewTraceWriter(db, 64, 1, 50*time.Millisecond)
	go tw.Run(ctx)
	return tw, cancel
}

func newTraceTestRouter(t *testing.T, tw *gwobs.TraceWriter, logEnabled bool, register func(*gin.Engine)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.Use(func(c *gin.Context) {
		c.Set("log_enabled", logEnabled)
		c.Set("key_name", "test-key")
		c.Set("key_id", "key-test-1")
	})
	r.Use(middleware.TraceCapture())
	r.Use(middleware.UsageCollector(nil, tw, nil))
	register(r)
	return r
}

func waitTrace(t *testing.T, db *gorm.DB, requestID string) obsmodel.CallTrace {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var trace obsmodel.CallTrace
		if err := db.First(&trace, "request_id = ?", requestID).Error; err == nil {
			return trace
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("trace not found for request_id=%s", requestID)
	return obsmodel.CallTrace{}
}

func postJSON(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "trace-test-req-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTracePipeline_Messages_CapturesRequestAndResponse(t *testing.T) {
	db := setupTraceDB(t)
	tw, cancel := startTraceWriter(t, db)
	defer cancel()

	r := newTraceTestRouter(t, tw, true, func(r *gin.Engine) {
		r.POST("/v1/messages", func(c *gin.Context) {
			c.Set("stream", false)
			c.Set("resp_object", gin.H{
				"type": "message",
				"content": []gin.H{
					{"type": "text", "text": "你好，我是助手"},
				},
			})
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	})

	body := `{"model":"claude-opus-4-6","max_tokens":1024,"messages":[{"role":"user","content":"用户提问内容"}]}`
	w := postJSON(t, r, "/v1/messages", body)
	require.Equal(t, http.StatusOK, w.Code)

	trace := waitTrace(t, db, "trace-test-req-1")
	require.NotEmpty(t, trace.RequestBody)
	require.NotEmpty(t, trace.ResponseBody)

	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(trace.RequestBody, &reqBody))
	assert.Equal(t, "claude-opus-4-6", reqBody["model"])

	var respBody map[string]any
	require.NoError(t, json.Unmarshal(trace.ResponseBody, &respBody))
	assert.Equal(t, "message", respBody["type"])
}

func TestTracePipeline_CountTokens_CapturesRequestAndResponse(t *testing.T) {
	db := setupTraceDB(t)
	tw, cancel := startTraceWriter(t, db)
	defer cancel()

	r := newTraceTestRouter(t, tw, true, func(r *gin.Engine) {
		r.POST("/v1/messages/count_tokens", func(c *gin.Context) {
			middleware.SetTraceResponseObject(c, gin.H{"input_tokens": 42})
			c.JSON(http.StatusOK, gin.H{"input_tokens": 42})
		})
	})

	body := `{"model":"claude-opus-4-6","messages":[{"role":"user","content":"count me"}]}`
	w := postJSON(t, r, "/v1/messages/count_tokens", body)
	require.Equal(t, http.StatusOK, w.Code)

	trace := waitTrace(t, db, "trace-test-req-1")
	require.NotEmpty(t, trace.RequestBody)
	require.NotEmpty(t, trace.ResponseBody)

	var respBody map[string]any
	require.NoError(t, json.Unmarshal(trace.ResponseBody, &respBody))
	assert.Equal(t, float64(42), respBody["input_tokens"])
}

func TestTracePipeline_Responses_CapturesRequestAndResponse(t *testing.T) {
	db := setupTraceDB(t)
	tw, cancel := startTraceWriter(t, db)
	defer cancel()

	r := newTraceTestRouter(t, tw, true, func(r *gin.Engine) {
		r.POST("/v1/responses", func(c *gin.Context) {
			c.Set("stream", false)
			c.Set("resp_object", gin.H{"id": "resp-1", "output": []gin.H{{"type": "message"}}})
			c.JSON(http.StatusOK, gin.H{"id": "resp-1"})
		})
	})

	body := `{"model":"gpt-4o","input":"hello codex"}`
	w := postJSON(t, r, "/v1/responses", body)
	require.Equal(t, http.StatusOK, w.Code)

	trace := waitTrace(t, db, "trace-test-req-1")
	require.NotEmpty(t, trace.RequestBody)
	require.NotEmpty(t, trace.ResponseBody)
}

func TestTracePipeline_Embeddings_CapturesRequestAndResponse(t *testing.T) {
	db := setupTraceDB(t)
	tw, cancel := startTraceWriter(t, db)
	defer cancel()

	r := newTraceTestRouter(t, tw, true, func(r *gin.Engine) {
		r.POST("/v1/embeddings", func(c *gin.Context) {
			c.Set("stream", false)
			c.Set("resp_object", gin.H{
				"object": "list",
				"data":   []gin.H{{"embedding": []float64{0.1, 0.2}}},
			})
			c.JSON(http.StatusOK, gin.H{"object": "list"})
		})
	})

	body := `{"model":"text-embedding-3-small","input":"embed this"}`
	w := postJSON(t, r, "/v1/embeddings", body)
	require.Equal(t, http.StatusOK, w.Code)

	trace := waitTrace(t, db, "trace-test-req-1")
	require.NotEmpty(t, trace.RequestBody)
	require.NotEmpty(t, trace.ResponseBody)
}

func TestTracePipeline_ChatCompletions_StillWorks(t *testing.T) {
	db := setupTraceDB(t)
	tw, cancel := startTraceWriter(t, db)
	defer cancel()

	r := newTraceTestRouter(t, tw, true, func(r *gin.Engine) {
		r.POST("/v1/chat/completions", func(c *gin.Context) {
			c.Set("stream", false)
			c.Set("resp_object", gin.H{
				"choices": []gin.H{{"message": gin.H{"role": "assistant", "content": "ok"}}},
			})
			c.JSON(http.StatusOK, gin.H{"id": "chat-1"})
		})
	})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	w := postJSON(t, r, "/v1/chat/completions", body)
	require.Equal(t, http.StatusOK, w.Code)

	trace := waitTrace(t, db, "trace-test-req-1")
	require.NotEmpty(t, trace.RequestBody)
	require.NotEmpty(t, trace.ResponseBody)
}

func TestTracePipeline_LogDisabled_SkipsTraceBody(t *testing.T) {
	db := setupTraceDB(t)
	tw, cancel := startTraceWriter(t, db)
	defer cancel()

	r := newTraceTestRouter(t, tw, false, func(r *gin.Engine) {
		r.POST("/v1/messages", func(c *gin.Context) {
			c.Set("stream", false)
			c.Set("resp_object", gin.H{"type": "message"})
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})
	})

	body := `{"model":"claude-opus-4-6","messages":[{"role":"user","content":"secret"}]}`
	w := postJSON(t, r, "/v1/messages", body)
	require.Equal(t, http.StatusOK, w.Code)

	time.Sleep(300 * time.Millisecond)
	var count int64
	require.NoError(t, db.Model(&obsmodel.CallTrace{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestTracePipeline_StreamResponseFallback(t *testing.T) {
	db := setupTraceDB(t)
	tw, cancel := startTraceWriter(t, db)
	defer cancel()

	r := newTraceTestRouter(t, tw, true, func(r *gin.Engine) {
		r.POST("/v1/messages", func(c *gin.Context) {
			c.Set("stream", true)
			// 模拟 observer 未拼接成功，但 handler 登记了错误响应
			middleware.SetTraceResponseObject(c, gin.H{
				"type":  "error",
				"error": gin.H{"type": "overloaded_error", "message": "upstream busy"},
			})
			c.JSON(http.StatusServiceUnavailable, gin.H{"type": "error"})
		})
	})

	body := `{"model":"claude-opus-4-6","stream":true,"messages":[{"role":"user","content":"stream"}]}`
	w := postJSON(t, r, "/v1/messages", body)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	trace := waitTrace(t, db, "trace-test-req-1")
	require.NotEmpty(t, trace.RequestBody)
	require.NotEmpty(t, trace.ResponseBody)

	var respBody map[string]any
	require.NoError(t, json.Unmarshal(trace.ResponseBody, &respBody))
	assert.Equal(t, "error", respBody["type"])
}
