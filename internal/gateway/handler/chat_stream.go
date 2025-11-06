package handler

import (
	"encoding/json"
	"fmt"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

// HandleStreamResponse 处理流式 SSE 响应
// 从 chunk channel 读取数据，逐条写出 SSE event
func HandleStreamResponse(c *gin.Context, ch <-chan model.StreamChunk) {
	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	c.Status(200)

	clientCtx := c.Request.Context()
	flusher := c.Writer

	for {
		select {
		case <-clientCtx.Done():
			// 客户端断开连接
			return
		case chunk, ok := <-ch:
			if !ok {
				// channel 已关闭，写 [DONE] 结束
				writeSSEData(flusher, "[DONE]")
				flusher.Flush()
				return
			}

			// 错误 chunk：写错误 event 后结束
			if chunk.Error != nil {
				errData := fmt.Sprintf(`{"error":{"message":"%s"}}`, chunk.Error.Error())
				writeSSEData(flusher, errData)
				flusher.Flush()
				return
			}

			// 完成标记：写 [DONE] 结束
			if chunk.Done {
				writeSSEData(flusher, "[DONE]")
				flusher.Flush()
				return
			}

			// 正常 chunk：序列化并写出
			if chunk.Data != nil {
				data, err := json.Marshal(chunk.Data)
				if err != nil {
					continue
				}
				writeSSEData(flusher, string(data))
				flusher.Flush()
			}
		}
	}
}

// writeSSEData 写入 SSE data 行
func writeSSEData(w gin.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
}
