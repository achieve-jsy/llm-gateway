package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestHandleStreamResponse_Success tests HandleStreamResponse with valid SSE stream
func TestHandleStreamResponse_Success(t *testing.T) {
	tests := []struct {
		name          string
		sseData       string
		expectContent string
	}{
		{
			name:          "single chunk and done marker",
			sseData:       "data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\"}\n\ndata: [DONE]\n\n",
			expectContent: "[DONE]",
		},
		{
			name:          "multiple chunks",
			sseData:       "data: {\"id\":\"chunk-1\"}\n\ndata: {\"id\":\"chunk-2\"}\n\ndata: [DONE]\n\n",
			expectContent: "chunk-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := io.NopCloser(strings.NewReader(tt.sseData))

			w := httptest.NewRecorder()
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)

			HandleStreamResponse(c, stream)

			// Verify response headers
			assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
			assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
			assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
			assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))

			// Verify content is forwarded verbatim
			body := w.Body.String()
			assert.Contains(t, body, tt.expectContent)
		})
	}
}

// TestHandleStreamResponse_EmptyStream tests HandleStreamResponse with empty body
func TestHandleStreamResponse_EmptyStream(t *testing.T) {
	stream := io.NopCloser(strings.NewReader(""))

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	HandleStreamResponse(c, stream)
	// Should not panic; response code is 200 (written by WriteHeader).
	assert.Equal(t, 200, w.Code)
}

// TestHandleStreamResponse_LargeStream tests HandleStreamResponse with a larger payload
func TestHandleStreamResponse_LargeStream(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("data: {\"index\":" + string(rune('0'+i%10)) + "}\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")

	stream := io.NopCloser(strings.NewReader(sb.String()))

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	HandleStreamResponse(c, stream)

	body := w.Body.String()
	assert.Contains(t, body, "[DONE]")
	assert.Contains(t, body, "data:")
}
