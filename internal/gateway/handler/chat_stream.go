package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/example/llm-gateway/internal/gateway/errormap"
)

// StreamDialect 表示当前 SSE 响应使用的协议方言。流中途出错时，错误帧的格式
// 由方言决定，取默认值表示 OpenAI / Responses 约定。
type StreamDialect int

const (
	// DialectOpenAI 使用 OpenAI / Responses 约定的 data: {"error":{...}} 帧。
	DialectOpenAI StreamDialect = iota
	// DialectAnthropic 使用 Anthropic Messages 约定的 event: error 事件。
	DialectAnthropic
)

// HandleStreamResponse 处理流式 SSE 响应，按 OpenAI 方言书写错误帧。
// 直接将上游 io.ReadCloser 的原始 SSE 字节流 zero-copy 转发给客户端。
// 调用方负责确保 stream 在返回前是有效的（非 nil）。
func HandleStreamResponse(c *gin.Context, stream io.ReadCloser) {
	HandleStreamResponseAs(c, stream, DialectOpenAI)
}

// HandleStreamResponseAs 处理指定方言的流式 SSE 响应。
func HandleStreamResponseAs(c *gin.Context, stream io.ReadCloser, dialect StreamDialect) {
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
			// 上游在流中途失败：必须显式告知客户端，不能只静默关闭连接。
			// 否则客户端会把“流正常结束但没有任何内容”当成成功回答
			// （Claude Code 报 "Streaming response ended before any complete
			// data was received" 后才做非流式重试）。
			if !errors.Is(readErr, io.EOF) {
				writeStreamError(c, readErr, dialect)
			}
			return
		}
	}
}

// writeStreamError 把流中途的上游错误写成客户端可见的 SSE 错误帧，并把错误登记到
// gin ctx：usage_collector 会据此落 error_code="upstream_error"，
// provider_tracker 也依赖它识别限流错误。
func writeStreamError(c *gin.Context, err error, dialect StreamDialect) {
	ue := errormap.MapProviderError(err)

	message := err.Error()
	code := "upstream_error"
	errType := "api_error"
	status := http.StatusBadGateway
	if ue != nil {
		if ue.Message != "" {
			message = ue.Message
		}
		if ue.Code != "" {
			code = ue.Code
		}
		if ue.HTTPStatus > 0 {
			status = ue.HTTPStatus
		}
		errType = mapAnthropicErrorType(ue.Type)
	}

	c.Set("error_message", message)
	c.Set("upstream_status", status)

	slog.Error("upstream stream error",
		"request_id", c.GetString("request_id"),
		"model", c.GetString("routed_model"),
		"error", message,
		"error_code", code,
	)

	if dialect == DialectAnthropic {
		body, marshalErr := json.Marshal(gin.H{
			"type": "error",
			"error": gin.H{
				"type":    errType,
				"message": message,
				"code":    code,
			},
		})
		if marshalErr != nil {
			return
		}
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", body)
	} else {
		body, marshalErr := json.Marshal(gin.H{
			"error": gin.H{
				"message": message,
				"type":    errType,
				"code":    code,
			},
		})
		if marshalErr != nil {
			return
		}
		c.Writer.Write([]byte("data: " + string(body) + "\n\n"))
	}
	c.Writer.Flush()
}
