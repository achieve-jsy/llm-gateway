package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestTraceCaptureSkipsHugeBody 验证超阈值请求体不会整包进 trace：
// 下游仍拿到完整 body，但 context 里只记录被省略的字节数。
func TestTraceCaptureSkipsHugeBody(t *testing.T) {
	const bodySize = 3 << 20 // 3MB > traceMaxBodyBytes
	var seen int
	r := gin.New()
	r.Use(TraceCapture())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		seen = len(b)
		if v, ok := c.Get("trace_req_body"); ok {
			t.Fatalf("trace_req_body should be unset for huge body, got %d bytes", len(v.([]byte)))
		}
		v, ok := c.Get("trace_req_body_omitted")
		if !ok {
			t.Fatal("trace_req_body_omitted should be set")
		}
		if v.(int64) != bodySize {
			t.Fatalf("omitted = %d, want %d", v.(int64), bodySize)
		}
		c.Status(200)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bytes.Repeat([]byte("a"), bodySize)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if seen != bodySize {
		t.Fatalf("downstream saw %d bytes, want %d", seen, bodySize)
	}
}

// TestTraceCaptureKeepsSmallBody 验证小请求体行为不变：进 trace 且是合法 JSON。
func TestTraceCaptureKeepsSmallBody(t *testing.T) {
	body := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)
	r := gin.New()
	r.Use(TraceCapture())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		v, ok := c.Get("trace_req_body")
		if !ok {
			t.Fatal("trace_req_body should be set for small body")
		}
		raw := v.([]byte)
		if !json.Valid(raw) {
			t.Fatalf("trace_req_body is not valid JSON: %q", string(raw))
		}
		c.Status(200)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
}
