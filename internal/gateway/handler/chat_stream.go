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

	// 逐块读取并在每次写入后 flush，确保 SSE 事件实时下发给客户端。
	// 不能用 io.Copy：gin/net-http 的 ResponseWriter 自带缓冲，小块 SSE 事件
	// 会在缓冲区里攒到一定大小或请求结束才发出，表现为"流式请求却同步返回"。
	buf := make([]byte, 16*1024)
	for {
		n, readErr := stream.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				return
			}
			c.Writer.Flush()
		}
		if readErr != nil {
			return
		}
	}
}
