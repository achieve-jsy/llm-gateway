package middleware_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceCapturePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		path       string
		logEnabled bool
		wantBody   bool
	}{
		{"/v1/chat/completions", true, true},
		{"/v1/messages", true, true},
		{"/v1/messages/count_tokens", true, true},
		{"/v1/responses", true, true},
		{"/v1/embeddings", true, true},
		{"/v1/models", true, false},
		{"/v1/chat/completions", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var gotBody []byte
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("log_enabled", tt.logEnabled)
			})
			r.Use(middleware.TraceCapture())
			handler := func(c *gin.Context) {
				if v, ok := c.Get("trace_req_body"); ok {
					gotBody, _ = v.([]byte)
				}
				c.Status(http.StatusOK)
			}
			r.POST("/v1/chat/completions", handler)
			r.POST("/v1/messages", handler)
			r.POST("/v1/messages/count_tokens", handler)
			r.POST("/v1/responses", handler)
			r.POST("/v1/embeddings", handler)
			r.GET("/v1/models", handler)

			body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
			method := http.MethodPost
			if tt.path == "/v1/models" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, strings.NewReader(body))
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			if tt.wantBody {
				require.NotEmpty(t, gotBody)
				assert.True(t, bytes.Contains(gotBody, []byte(`"hi"`)))
			} else {
				assert.Empty(t, gotBody)
			}
		})
	}
}

func TestSetTraceResponseObject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	middleware.SetTraceResponseObject(c, map[string]string{"ok": "1"})

	stream, _ := c.Get("stream")
	assert.Equal(t, false, stream)
	obj, ok := c.Get("resp_object")
	require.True(t, ok)
	assert.Equal(t, map[string]string{"ok": "1"}, obj)
}
