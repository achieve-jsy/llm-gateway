package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// TestRequestBodyLoggerKeepsFullBody 验证 debug 日志中间件不会因为只在日志里
// 记录前 64KB 就把下游 handler 能读到的请求体也截断。
func TestRequestBodyLoggerKeepsFullBody(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	const bodySize = 200 << 10 // 200KB > 64KB
	var seen int
	r := gin.New()
	r.Use(RequestBodyLogger())
	r.POST("/x", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		seen = len(b)
		c.Status(200)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", bytes.NewReader(bytes.Repeat([]byte("a"), bodySize)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if seen != bodySize {
		t.Fatalf("downstream saw %d bytes, want %d", seen, bodySize)
	}
}
