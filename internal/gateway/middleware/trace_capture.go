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

// responseBodyWriter 包装 gin.ResponseWriter，同时将响应体写入 buf
type responseBodyWriter struct {
	gin.ResponseWriter
	buf *bytes.Buffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

// TraceCapture 捕获请求体，并将 responseBodyWriter 注入 context 供 UsageCollector 使用
// 仅对 /v1/chat/completions 生效，请求体限制 512KB
func TraceCapture() gin.HandlerFunc {
	const maxReqBody = 512 * 1024 // 512 KB

	return func(c *gin.Context) {
		// 只处理 POST /v1/chat/completions
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path != "/v1/chat/completions" {
			c.Next()
			return
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

		// 注入 responseBodyWriter，供内层中间件（UsageCollector）直接读取
		rbw := &responseBodyWriter{ResponseWriter: c.Writer, buf: &bytes.Buffer{}}
		c.Writer = rbw
		c.Set("trace_resp_writer", rbw)

		c.Next()
	}
}
