package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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

// traceCapturePaths 需要采集请求正文的 LLM 调用路径（OpenAI + Anthropic + Responses + Embeddings + Rerank）。
var traceCapturePaths = map[string]struct{}{
	"/v1/chat/completions":      {},
	"/v1/messages":              {},
	"/v1/messages/count_tokens": {},
	"/v1/responses":             {},
	"/v1/embeddings":            {},
	"/v1/rerank":                {},
}

func isTraceCapturePath(path string) bool {
	_, ok := traceCapturePaths[path]
	return ok
}

// TraceCapture 捕获请求头和请求体，注入 context 供 UsageCollector 使用。
// 对所有 LLM 调用路径（chat / messages / responses / embeddings）生效。
//
// 与 LiteLLM 对齐的 sanitize 策略（见 sanitizeJSONRequestBody）：
//   - base64 data-URI 超过 64 字符时替换为大小占位符（不丢弃）
//   - 每个文本字段超过 2048 字符时保留首尾 + 截断标记（JSON 结构始终合法）
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
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || !isTraceCapturePath(c.Request.URL.Path) {
			c.Next()
			return
		}

		// 所有 LLM 调用路径统一采集请求正文（log_enabled 门控已废弃：应收尽收，
		// 避免"某部分人上下文缺失"）。系统探针 key 由 UsageCollector 的 isSystemKey 单独跳过。
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

		// 读取完整请求体并恢复给 handler。
		// sanitizeJSONRequestBody 在此同步执行：io.ReadAll（内存拷贝）+ JSON 双序列化。
		// 对普通大小请求体（< 10KB）影响约 0.1–1ms，是可接受的。
		// 更大的延迟优化已通过 KeyCache TTL 延长解决（key_cache.go）。
		if body, err := io.ReadAll(c.Request.Body); err == nil && len(body) > 0 {
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
			c.Set("trace_req_body", sanitizeJSONRequestBody(body))
		}

		c.Next()
	}
}

// TraceSanitizeLimits 控制写入 call_traces 的请求/响应正文在落库前的裁剪上限。
// MaxStringLenInTrace 是每个文本字段值的字符上限：默认调高到 500_000，保证语义分析
// 所需的完整上下文不被截断（原先 2048 会切断长 message content，破坏 token 用量/语义分析报告）。
// MaxBase64LenInTrace 是 base64 data-URI payload 的保留上限：图片二进制体积巨大，
// 仍以人类可读占位符替代，避免单条 trace 把表体积撑爆。
type TraceSanitizeLimits struct {
	MaxStringLenInTrace int
	MaxBase64LenInTrace int
}

// defaultTraceSanitizeLimits 默认上限：文本字段关键上下文不截断，base64 仍用占位符。
var defaultTraceSanitizeLimits = TraceSanitizeLimits{
	MaxStringLenInTrace: 500_000,
	MaxBase64LenInTrace: 64,
}

// SetTraceSanitizeLimits 覆盖全局默认上限（在 main 启动时按配置调用一次，之后只读）。
func SetTraceSanitizeLimits(limits TraceSanitizeLimits) {
	if limits.MaxStringLenInTrace > 0 {
		defaultTraceSanitizeLimits.MaxStringLenInTrace = limits.MaxStringLenInTrace
	}
	if limits.MaxBase64LenInTrace > 0 {
		defaultTraceSanitizeLimits.MaxBase64LenInTrace = limits.MaxBase64LenInTrace
	}
}

// dataURIRe 匹配 data:mime;base64,<payload> 格式
var dataURIRe = regexp.MustCompile(`data:([^;]+);base64,([A-Za-z0-9+/=]+)`)

// sanitizeJSONRequestBody 对 JSON 请求体做两层 sanitize，与 LiteLLM 策略对齐：
//
//  1. base64 截断（对应 LiteLLM truncate_base64_in_messages）：
//     messages content 中内嵌的 data-URI base64 payload 超过 maxBase64LenInTrace 字符时，
//     替换为 "data:mime;base64,[base64_data truncated: X.XX KB]"，而非整体丢弃。
//
//  2. 字符串字段截断（对应 LiteLLM _sanitize_request_body_for_spend_logs_payload）：
//     每个 string 字段超过 maxStringLenInTrace 字符时，保留首 35% + 尾 65% + 截断标记，
//     截断发生在字段值内部，JSON 结构始终合法。
//
// 非合法 JSON 或解析出错时原样返回，保证调用方始终拿到 []byte（不返回 nil）。
func sanitizeJSONRequestBody(raw []byte) []byte {
	return sanitizeJSONRequestBodyWithLimits(raw, defaultTraceSanitizeLimits)
}

// sanitizeJSONRequestBodyWithLimits 以显式上限执行两层 sanitize（供生产与测试复用）。
func sanitizeJSONRequestBodyWithLimits(raw []byte, limits TraceSanitizeLimits) []byte {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		// 非合法 JSON，原样返回；调用方的 json.Valid 会决定是否写库
		return raw
	}

	sanitizeValue(body, limits)

	b, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return b
}

// sanitizeChatRequestBody 保留旧名供测试引用。
func sanitizeChatRequestBody(raw []byte) []byte {
	return sanitizeJSONRequestBody(raw)
}

// sanitizeValue 递归处理 map/slice/string，原地修改 map 与 slice 中的值。
func sanitizeValue(v any, limits TraceSanitizeLimits) any {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			val[k] = sanitizeValue(child, limits)
		}
		return val
	case []any:
		for i, child := range val {
			val[i] = sanitizeValue(child, limits)
		}
		return val
	case string:
		return sanitizeString(val, limits)
	}
	return v
}

// sanitizeString 对单个字符串值执行两层处理：
//  1. 替换 base64 data-URI 中过长的 payload
//  2. 若整体仍超长，保留首尾 + 插入截断标记
func sanitizeString(s string, limits TraceSanitizeLimits) string {
	// 第一层：base64 data-URI 替换
	s = dataURIRe.ReplaceAllStringFunc(s, func(m string) string { return replaceBase64DataURI(m, limits) })

	// 第二层：字符串整体长度截断
	if len(s) > limits.MaxStringLenInTrace {
		limit := limits.MaxStringLenInTrace
		startN := limit * 35 / 100
		endN := limit - startN
		skipped := len(s) - startN - endN
		s = s[:startN] +
			fmt.Sprintf("...(truncated, skipped %d chars)...", skipped) +
			s[len(s)-endN:]
	}
	return s
}

// replaceBase64DataURI 将单个 base64 data-URI 中超长的 payload 替换为大小占位符。
func replaceBase64DataURI(match string, limits TraceSanitizeLimits) string {
	sub := dataURIRe.FindStringSubmatch(match)
	if len(sub) != 3 {
		return match
	}
	mimeType := sub[1]
	payload := sub[2]
	if len(payload) <= limits.MaxBase64LenInTrace {
		return match
	}
	// 计算原始字节大小（base64 解码长度约为 payload*3/4）
	rawLen := base64.StdEncoding.DecodedLen(len(payload))
	size := formatByteSize(rawLen)
	return fmt.Sprintf("data:%s;base64,[base64_data truncated: %s]", mimeType, size)
}

// formatByteSize 将字节数格式化为人类可读字符串（KB / MB）。
func formatByteSize(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(n)/1024/1024)
	case n >= 1024:
		return fmt.Sprintf("%.2f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
