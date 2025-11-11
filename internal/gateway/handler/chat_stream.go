package handler

import (
	"io"

	"github.com/gin-gonic/gin"
)

// HandleStreamResponse 处理流式 SSE 响应
// 直接将上游 io.ReadCloser 的原始 SSE 字节流 zero-copy 转发给客户端。
// 调用方负责确保 stream 在返回前是有效的（非 nil）。
func HandleStreamResponse(c *gin.Context, stream io.ReadCloser) {
	defer stream.Close()

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 立即写出响应头（含 200 状态码），让浏览器/客户端可以立刻开始消费 SSE 流
	c.Writer.WriteHeader(200)
	c.Writer.Flush()

	// Zero-copy pipe: upstream SSE bytes → client
	// io.Copy terminates when the stream is exhausted or the client disconnects.
	_, _ = io.Copy(c.Writer, stream)
	c.Writer.Flush()
}
