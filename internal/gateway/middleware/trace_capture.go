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

		// 读取请求体（读后恢复完整 body，让 handler 仍能正常读取）
		// 注意：io.LimitReader 只读前 maxReqBody 字节，超限部分还在原始 Body 里；
		// 必须把"已读部分 + 剩余部分"一起拼回去，否则 handler 拿到截断的 JSON 会 400。
		rawBody := c.Request.Body
		limited, err := io.ReadAll(io.LimitReader(rawBody, maxReqBody))
		if err == nil && len(limited) > 0 {
			// 拼回完整 body：已读部分 + 原始流中剩余部分
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewBuffer(limited), rawBody))
			c.Set("trace_req_body", sanitizeChatRequestBody(limited))
		}

		c.Next()
	}
}

// sanitizeChatRequestBody 解析 chat completion 请求体，将 messages 中
// 每条消息的 content 截断（保留前 200 字符），避免把完整上下文存入 trace 表。
// 非 chat 请求或不合法 JSON 原样返回。
func sanitizeChatRequestBody(raw []byte) []byte {
	const maxContentLen = 200

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}

	messages, ok := body["messages"].([]any)
	if !ok {
		return raw
	}

	for i, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"]
		if !ok {
			continue
		}
		switch v := content.(type) {
		case string:
			if len(v) > maxContentLen {
				msg["content"] = v[:maxContentLen] + "..."
			}
		case []any:
			for j, part := range v {
				p, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if s, ok := p["text"].(string); ok && len(s) > maxContentLen {
					p["text"] = s[:maxContentLen] + "..."
					v[j] = p
				}
			}
			msg["content"] = v
		}
		messages[i] = msg
	}
	body["messages"] = messages

	b, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return b
}
