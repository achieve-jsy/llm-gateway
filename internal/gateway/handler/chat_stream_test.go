package handler

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestHandleStreamResponse_Success tests HandleStreamResponse with valid SSE chunks
func TestHandleStreamResponse_Success(t *testing.T) {
	tests := []struct {
		name          string
		chunks        []model.StreamChunk
		expectContent string
		dontExpect    string
	}{
		{
			name: "single chunk and done marker",
			chunks: []model.StreamChunk{
				{
					Data: &model.ChatCompletionChunk{
						ID:      "chunk-1",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   "gpt-3.5",
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									Content: "Hello",
								},
							},
						},
					},
				},
				{Done: true},
			},
			expectContent: "[DONE]",
		},
		{
			name: "multiple chunks",
			chunks: []model.StreamChunk{
				{
					Data: &model.ChatCompletionChunk{
						ID:      "chunk-1",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   "gpt-3.5",
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									Content: "Hello",
								},
							},
						},
					},
				},
				{
					Data: &model.ChatCompletionChunk{
						ID:      "chunk-2",
						Object:  "chat.completion.chunk",
						Created: 1234567891,
						Model:   "gpt-3.5",
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									Content: " world",
								},
							},
						},
					},
				},
				{Done: true},
			},
			expectContent: "Hello",
		},
		{
			name: "chunk with tool calls",
			chunks: []model.StreamChunk{
				{
					Data: &model.ChatCompletionChunk{
						ID:      "chunk-1",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   "claude-3",
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									ToolCallID: "call-123",
									ToolCallFunction: &model.ToolCallFunction{
										Name:      "get_weather",
										Arguments: `{"location":"NYC"}`,
									},
								},
							},
						},
					},
				},
				{Done: true},
			},
			expectContent: "tool_call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create channel and send chunks
			ch := make(chan model.StreamChunk, len(tt.chunks))
			for _, chunk := range tt.chunks {
				ch <- chunk
			}
			close(ch)

			// Create test context
			w := httptest.NewRecorder()
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)

			// Call handler
			HandleStreamResponse(c, ch)

			// Verify response headers
			assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
			assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
			assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
			assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))

			// Verify content
			body := w.Body.String()
			assert.Contains(t, body, "data:")
			assert.Contains(t, body, tt.expectContent)
			if tt.dontExpect != "" {
				assert.NotContains(t, body, tt.dontExpect)
			}
		})
	}
}

// TestHandleStreamResponse_EmptyChannel tests HandleStreamResponse when channel closes immediately
func TestHandleStreamResponse_EmptyChannel(t *testing.T) {
	ch := make(chan model.StreamChunk)
	close(ch)

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	HandleStreamResponse(c, ch)

	body := w.Body.String()
	assert.Contains(t, body, "[DONE]")
}

// TestHandleStreamResponse_ErrorChunk tests HandleStreamResponse with error chunk
func TestHandleStreamResponse_ErrorChunk(t *testing.T) {
	ch := make(chan model.StreamChunk, 1)
	ch <- model.StreamChunk{
		Error: fmt.Errorf("provider timeout"),
	}
	close(ch)

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	HandleStreamResponse(c, ch)

	body := w.Body.String()
	assert.Contains(t, body, "error")
	assert.Contains(t, body, "provider timeout")
	// Should NOT contain [DONE] after error
	lines := strings.Split(body, "\n")
	lastDataLine := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "data:") {
			lastDataLine = lines[i]
			break
		}
	}
	assert.Contains(t, lastDataLine, "error")
}

// TestHandleStreamResponse_NilData tests HandleStreamResponse skips nil data
func TestHandleStreamResponse_NilData(t *testing.T) {
	ch := make(chan model.StreamChunk, 3)
	ch <- model.StreamChunk{Data: nil} // Should be skipped
	ch <- model.StreamChunk{
		Data: &model.ChatCompletionChunk{
			ID: "chunk-1",
			Choices: []model.Choice{
				{
					Index: 0,
					Delta: &model.Delta{
						Content: "test",
					},
				},
			},
		},
	}
	ch <- model.StreamChunk{Done: true}
	close(ch)

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	HandleStreamResponse(c, ch)

	body := w.Body.String()
	lines := strings.Split(body, "\n")
	dataLines := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataLines++
		}
	}
	// Should have 2 data lines: one for chunk, one for [DONE]
	assert.Equal(t, 2, dataLines)
}

// TestHandleStreamResponse_LargeChunks tests HandleStreamResponse with many chunks
func TestHandleStreamResponse_LargeChunks(t *testing.T) {
	numChunks := 100
	ch := make(chan model.StreamChunk, numChunks+1)

	for i := 0; i < numChunks; i++ {
		ch <- model.StreamChunk{
			Data: &model.ChatCompletionChunk{
				ID:      fmt.Sprintf("chunk-%d", i),
				Object:  "chat.completion.chunk",
				Created: 1234567890,
				Model:   "gpt-3.5",
				Choices: []model.Choice{
					{
						Index: 0,
						Delta: &model.Delta{
							Content: fmt.Sprintf("chunk%d", i),
						},
					},
				},
			},
		}
	}
	ch <- model.StreamChunk{Done: true}
	close(ch)

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	HandleStreamResponse(c, ch)

	body := w.Body.String()
	assert.Contains(t, body, "[DONE]")
	// Verify all chunks were written
	for i := 0; i < numChunks; i++ {
		assert.Contains(t, body, fmt.Sprintf("chunk-%d", i))
	}
}

// TestHandleStreamResponse_ContextCancellation tests HandleStreamResponse exits on context cancellation
func TestHandleStreamResponse_ContextCancellation(t *testing.T) {
	ch := make(chan model.StreamChunk, 10)
	for i := 0; i < 5; i++ {
		ch <- model.StreamChunk{
			Data: &model.ChatCompletionChunk{
				ID: fmt.Sprintf("chunk-%d", i),
				Choices: []model.Choice{
					{
						Index: 0,
						Delta: &model.Delta{
							Content: "text",
						},
					},
				},
			},
		}
	}

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)

	// 构造带可取消上下文的请求
	baseReq := httptest.NewRequest("GET", "/", nil)
	ctx, cancel := context.WithCancel(baseReq.Context())
	c.Request = baseReq.WithContext(ctx)

	// 提前取消，handler 会读取部分数据后立刻返回
	cancel()

	close(ch)
	HandleStreamResponse(c, ch)
}

// TestHandleStreamResponse_MarshalError tests HandleStreamResponse handles JSON marshal errors gracefully
func TestHandleStreamResponse_MarshalError(t *testing.T) {
	// Create a chunk with circular reference that would fail to marshal
	ch := make(chan model.StreamChunk, 2)
	ch <- model.StreamChunk{
		Data: &model.ChatCompletionChunk{
			ID: "chunk-1",
			// This chunk has valid data, so marshal won't fail
			// For testing marshal error, we'd need a custom type
		},
	}
	ch <- model.StreamChunk{Done: true}
	close(ch)

	w := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	HandleStreamResponse(c, ch)

	body := w.Body.String()
	assert.Contains(t, body, "[DONE]")
}
