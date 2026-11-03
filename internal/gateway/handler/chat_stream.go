package handler

import (
	"bytes"
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

	// 按 SSE event 边界（\n\n）切分并逐事件 flush，确保每个 token 粒度的
	// event 独立到达客户端，避免 TCP 合并导致多个 event 被打包成一个视觉块。
	buf := make([]byte, 16*1024)
	var pending []byte

	for {
		n, readErr := stream.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)

			for {
				idx := bytes.Index(pending, []byte("\n\n"))
				if idx == -1 {
					break
				}
				if _, writeErr := c.Writer.Write(pending[:idx+2]); writeErr != nil {
					return
				}
				c.Writer.Flush()
				pending = pending[idx+2:]
			}
		}
		if readErr != nil {
			if len(pending) > 0 {
				c.Writer.Write(pending)
				c.Writer.Flush()
			}
			return
		}
	}
}
