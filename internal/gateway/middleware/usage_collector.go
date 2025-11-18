package middleware

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
)

// UsageCollector 注入 Collector 到请求处理链
// 在 c.Next() 后构造 UsageEvent 并异步投递埋点
// 仅对 LLM 调用路径（/v1/chat/completions, /v1/embeddings 等）生效
func UsageCollector(collector *gwobs.Collector, traceWriter ...*gwobs.TraceWriter) gin.HandlerFunc {
	var tw *gwobs.TraceWriter
	if len(traceWriter) > 0 {
		tw = traceWriter[0]
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// 仅采集 LLM 调用，跳过健康检查和管理接口
		if path == "/health" || path == "/ready" {
			c.Next()
			return
		}

		// start_time 由 RequestID 中间件设置，以便所有阶段耗时使用统一基准
		start := time.Now()
		if st, ok := c.Get("start_time"); ok {
			if t, ok := st.(time.Time); ok {
				start = t
			}
		}

		// 提前拿 key_name，系统探针请求跳过埋点和 trace
		// __system_ 前缀为内部保留命名空间，不计入用量统计
		if isSystemKey(c) {
			c.Next()
			return
		}

		c.Next()

		totalMs := int(time.Since(start).Milliseconds())

		// 判断该 key 是否开启日志记录（耗时/Token 始终计算，仅日志写入受此标志影响）
		logEnabled := true
		if v, ok := c.Get("log_enabled"); ok {
			if b, ok := v.(bool); ok {
				logEnabled = b
			}
		}
		log.Printf("[trace] key=%s log_enabled=%v", c.GetString("key_name"), logEnabled)

		// ── Usage 埋点：始终写入（耗时/Token/费用，无论 log_enabled） ──
		if collector != nil {
			event := collector.NewUsageEvent(c)
			event.Status = c.Writer.Status()
			event.DurationMs = int64(totalMs)
			collector.Emit(event)
		}

		// ── Trace 记录（提示词/响应内容）：仅 log_enabled=true 时写入 ──
		if tw == nil || !logEnabled {
			return
		}
		reqID, _ := c.Get("request_id")
		reqIDStr, _ := reqID.(string)
		if reqIDStr == "" {
			return
		}

		te := gwobs.TraceEvent{
			RequestID: reqIDStr,
			TS:        start,
		}

		// 各阶段耗时
		te.StageTotalMs = &totalMs
		if v, ok := getInt16(c, "t_auth_ms"); ok {
			te.StageAuthMs = &v
		}
		if v, ok := getInt16(c, "t_route_ms"); ok {
			te.StageRouteMs = &v
		}
		if v, ok := getInt16(c, "t_upstream_ms"); ok {
			te.StageUpstreamMs = &v
		}
		if v, ok := c.Get("first_token_ms"); ok {
			if ms, ok := v.(int); ok {
				te.StageTTFTMs = &ms
			}
		}
		if v, ok := c.Get("t_ttfb_ms"); ok {
			if ms, ok := v.(int); ok {
				te.StageTTFBMs = &ms
			}
		}

		// 请求头
		if rh, ok := c.Get("trace_req_headers"); ok {
			if b, ok := rh.([]byte); ok {
				te.RequestHeadersRaw = b
			}
		}

		// 请求体
		if rb, ok := c.Get("trace_req_body"); ok {
			if b, ok := rb.([]byte); ok {
				te.RequestBodyRaw = b
			}
		}

		// 响应体：从 TraceCapture 注入的 responseBodyWriter 直接读取
		const maxRespBody = 2 * 1024 * 1024
		isStream, _ := c.Get("stream")
		if isStream == true {
			// 流式：解析 SSE 缓冲区重组完整回复
			if rw, ok := c.Get("trace_resp_writer"); ok {
				if rbw, ok := rw.(*responseBodyWriter); ok {
					if assembled := assembleSSEResponse(rbw.buf.Bytes()); len(assembled) > 0 {
						te.ResponseBodyRaw = assembled
					}
				}
			}
			// 兜底：仅存 usage 摘要
			if len(te.ResponseBodyRaw) == 0 {
				if usage, ok := c.Get("usage"); ok {
					if b, err := json.Marshal(map[string]any{"usage": usage, "stream": true}); err == nil {
						te.ResponseBodyRaw = b
					}
				}
			}
		} else if rw, ok := c.Get("trace_resp_writer"); ok {
			if rbw, ok := rw.(*responseBodyWriter); ok {
				buf := rbw.buf.Bytes()
				if len(buf) > 0 && len(buf) <= maxRespBody {
					te.ResponseBodyRaw = make([]byte, len(buf))
					copy(te.ResponseBodyRaw, buf)
				}
			}
		}

		// 错误信息
		if v, ok := c.Get("error_message"); ok {
			if msg, ok := v.(string); ok && msg != "" {
				te.ErrorMessage = msg
				te.ErrorCode = "upstream_error"
			}
		}
		if c.Writer.Status() >= 400 && te.ErrorMessage == "" {
			te.ErrorCode = "http_error"
		}

		tw.Emit(te)
	}
}

// isSystemKey 判断当前请求是否使用了系统保留 key（__system_ 前缀）
// 系统探针/健康检查请求不写入用量日志和 trace
func isSystemKey(c *gin.Context) bool {
	v, ok := c.Get("key_name")
	if !ok {
		return false
	}
	name, ok := v.(string)
	return ok && len(name) >= 9 && name[:9] == "__system_"
}

// assembleSSEResponse 解析原始 SSE 字节流，重组成标准 Chat Completion JSON
// 格式：data: {...chunk...}\n\n  ...  data: [DONE]\n\n
func assembleSSEResponse(raw []byte) []byte {
	type delta struct {
		Role    string `json:"role,omitempty"`
		Content string `json:"content,omitempty"`
	}
	type choice struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	}
	type chunk struct {
		ID      string                 `json:"id,omitempty"`
		Model   string                 `json:"model,omitempty"`
		Created int64                  `json:"created,omitempty"`
		Usage   map[string]any         `json:"usage,omitempty"`
		Choices []choice               `json:"choices,omitempty"`
	}

	var sb strings.Builder
	var lastID, lastModel string
	var lastCreated int64
	var lastUsage map[string]any

	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimPrefix(line, []byte("data: "))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		var c chunk
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}
		if c.ID != "" {
			lastID = c.ID
		}
		if c.Model != "" {
			lastModel = c.Model
		}
		if c.Created != 0 {
			lastCreated = c.Created
		}
		if c.Usage != nil {
			lastUsage = c.Usage
		}
		if len(c.Choices) > 0 {
			sb.WriteString(c.Choices[0].Delta.Content)
		}
	}

	content := sb.String()
	if content == "" && lastUsage == nil {
		return nil
	}

	synthesized := map[string]any{
		"id":      lastID,
		"model":   lastModel,
		"created": lastCreated,
		"object":  "chat.completion",
		"stream":  true,
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
	if lastUsage != nil {
		synthesized["usage"] = lastUsage
	}
	b, err := json.Marshal(synthesized)
	if err != nil {
		return nil
	}
	return b
}

func getInt16(c *gin.Context, key string) (int16, bool) {
	v, ok := c.Get(key)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int16:
		return n, true
	case int:
		return int16(n), true
	case int64:
		return int16(n), true
	}
	return 0, false
}