package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// sensitiveHeaders 不采集的敏感 header（安全）
var sensitiveHeaders = map[string]bool{
	"authorization": true,
	"cookie":        true,
	"set-cookie":    true,
	"x-api-key":     true,
}

// TraceCapture 捕获请求头和请求体，注入 context 供 UsageCollector 使用。
// 仅对 POST /v1/chat/completions 生效，请求体限制 512KB。
//
// 内存优化说明：
//   - 流式响应：由 streamUsageObserver 在 ObservedSSEStream 旁路中逐 chunk
//     拼接 content/reasoning_content，流结束时写入 stream_assembled_response，
//     无需 responseBodyWriter buf 双写，峰值内存从 O(原始字节) 降为 O(文本长度)。
//   - 非流式响应：orchestrator 已将完整响应对象写入 resp_object，
//     UsageCollector 直接序列化，无需 buf。
//   - 因此本中间件不再注入 responseBodyWriter，仅捕获请求头和请求体。
//
// 注意：本中间件注册在 AuthMiddleware 之后，log_enabled 此时已可读。
func TraceCapture() gin.HandlerFunc {
	const maxReqBody = 512 * 1024 // 512 KB

	return func(c *gin.Context) {
		// 只处理 POST /v1/chat/completions
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		if c.Request.URL.Path != "/v1/chat/completions" {
			c.Next()
			return
		}

		// log_enabled 由 AuthMiddleware 写入 context，提前跳过不需要记录的请求
		if v, ok := c.Get("log_enabled"); ok {
			if enabled, ok := v.(bool); ok && !enabled {
				c.Next()
				return
			}
		}

		// 采集请求头（过滤敏感字段）
		headers := make(map[string]string, len(c.Request.Header))
		for k, v := range c.Request.Header {
			if !sensitiveHeaders[strings.ToLower(k)] && len(v) > 0 {
				headers[k] = v[0]
			}
		}
		if hb, err := json.Marshal(headers); err == nil {
			c.Set("trace_req_headers", hb)
		}

		// 读取请求体（读后恢复，让 handler 仍能读取）
		reqBody, err := io.ReadAll(io.LimitReader(c.Request.Body, maxReqBody))
		if err == nil && len(reqBody) > 0 {
			c.Request.Body = io.NopCloser(bytes.NewBuffer(reqBody))
			c.Set("trace_req_body", reqBody)
		}

		c.Next()
	}
}
