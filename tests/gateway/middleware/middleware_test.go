package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		existingID  string
		wantNew     bool
	}{
		{
			name:        "注入新的 X-Request-ID",
			description: "请求无 X-Request-ID header 时，中间件应生成 UUID 并设入 response header",
			existingID:  "",
			wantNew:     true,
		},
		{
			name:        "保留已有的 X-Request-ID",
			description: "请求已携带 X-Request-ID header 时，中间件应原样透传",
			existingID:  "existing-request-id-abc",
			wantNew:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedID string
			router := gin.New()
			router.Use(middleware.RequestID())
			router.GET("/test", func(c *gin.Context) {
				val, _ := c.Get("request_id")
				capturedID, _ = val.(string)
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			if tt.existingID != "" {
				req.Header.Set("X-Request-ID", tt.existingID)
			}
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			responseID := w.Header().Get("X-Request-ID")
			assert.NotEmpty(t, responseID)
			assert.Equal(t, capturedID, responseID)

			if tt.wantNew {
				assert.NotEqual(t, "", capturedID)
				assert.Len(t, capturedID, 36) // UUID 格式 8-4-4-4-12
			} else {
				assert.Equal(t, tt.existingID, capturedID)
			}
		})
	}
}

func TestLogger_NoPanic(t *testing.T) {
	// 测试目的：Logger 中间件在正常请求下不 panic，请求能正常完成
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.Use(middleware.Logger())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// /health 路径也不应 panic
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

func TestErrorHandler_CatchesPanic(t *testing.T) {
	// 测试目的：ErrorHandler 捕获下游 handler panic，返回 500 而非进程崩溃
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/test", func(c *gin.Context) {
		panic("something went terribly wrong")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal_error")
}

func TestErrorHandler_NormalRequest(t *testing.T) {
	// 测试目的：ErrorHandler 对正常请求透明，不影响结果
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestRequestBodyLogger_NormalRequest(t *testing.T) {
	// 测试目的：RequestBodyLogger 不阻塞正常请求，body 仍可被下游读取
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestBodyLogger())
	router.POST("/test", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusOK, string(body))
	})

	payload := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/test", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "gpt-4")
}

func TestRequestBodyLogger_GETRequest(t *testing.T) {
	// 测试目的：GET 请求无 body，RequestBodyLogger 应正常 Next 不做额外操作
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestBodyLogger())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
