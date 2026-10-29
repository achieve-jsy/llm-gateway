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
	"sync"

	"github.com/gin-gonic/gin"
)

const (
	// maxTraceBodyPreallocBytes 按 Content-Length 预分配请求体容量的上限（64 MB）。
	// 防御客户端谎报超大 Content-Length 导致一次性 make 巨量内存；
	// 真实 body 超过该上限时，超出部分由 bytes.Buffer 按需增长补齐。
	maxTraceBodyPreallocBytes int64 = 64 << 20
	// traceBodyCopyBufSize 读取请求体时复用的拷贝缓冲大小（64 KB）。
	traceBodyCopyBufSize = 64 << 10
	// traceMaxBodyBytes 单个请求正文写入 trace 的字节上限（2 MB）。
	// 视频/音频 base64 请求体动辄数十 MB，整包 sanitize（JSON 双序列化 + 递归改写）
	// 会把峰值内存放大 3-4 倍，因此超限时只记录体积，不落原文。
	traceMaxBodyBytes int64 = 2 << 20
)

// traceBodyCopyBufPool 复用请求体读取用的 64KB 拷贝缓冲，避免每次请求都临时分配一块缓冲。
var traceBodyCopyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, traceBodyCopyBufSize)
		return &b
	},
}

// readTraceRequestBody 读取请求体全文，返回独立于请求的 []byte 副本。
// 无正文（长度 0）或读取中断时返回 nil，调用方据此跳过正文采集。
//
// 与 io.ReadAll 的差异（内存优化）：优先按 Content-Length 预留容量（封顶 maxTraceBodyPreallocBytes），
// 避免长度已知时 append 仍按 2 倍增长的分配 churn（30 MB body 实测 2.18× → 1.00×）；
// 读取缓冲从 traceBodyCopyBufPool 复用，避免每次请求临时分配 64KB。
func readTraceRequestBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}

	// 按 Content-Length 预分配容量；ContentLength <= 0（长度未知）时不预分配
	prealloc := 0
	if cl := r.ContentLength; cl > 0 {
		if cl > maxTraceBodyPreallocBytes {
			prealloc = int(maxTraceBodyPreallocBytes)
		} else {
			prealloc = int(cl)
		}
	}

	buf := bytes.NewBuffer(make([]byte, 0, prealloc))

	chunkPtr := traceBodyCopyBufPool.Get().(*[]byte)
	chunk := (*chunkPtr)[:traceBodyCopyBufSize]
	defer traceBodyCopyBufPool.Put(chunkPtr)

	for {
		n, err := r.Body.Read(chunk)
		if n > 0 {
			// buf.Write 会拷贝一份，返回的 []byte 不引用池化缓冲
			buf.Write(chunk[:n])
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			// 读取中断：与原 io.ReadAll 错误路径一致，放弃采集正文
			return nil
		}
	}

	if buf.Len() == 0 {
		return nil
	}
	return buf.Bytes()
}

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

		// 体积可控（Content-Length 已知且 ≤ traceMaxBodyBytes）时走既有路径：
		// 池化缓冲整包读取 + 按 MaxParseBytesInTrace 分流（≤1 MB 做 sanitize，
		// 否则以原始字节直存 trace_req_body_raw），保证普通请求的落库内容与内存特征不变。
		// 只有「声明超过 2 MB」或「长度未知（chunked）」时才改用 peekBody 只读前
		// traceMaxBodyBytes（+1 字节用于判断是否超限），避免视频/音频 base64 这类
		// 数十 MB 的 payload 整包驻留（sanitize 峰值约为 body 的 3-4 倍）：
		// 超限时只记录字节数（trace_req_body_omitted），其余字节原样还给 handler。
		if cl := c.Request.ContentLength; cl > 0 && cl <= traceMaxBodyBytes {
			if body := readTraceRequestBody(c.Request); len(body) > 0 {
				// 无论走哪条路径，都必须把 body 恢复给下游 handler
				c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
				if len(body) <= defaultTraceSanitizeLimits.MaxParseBytesInTrace {
					c.Set("trace_req_body", sanitizeJSONRequestBody(body))
				} else {
					c.Set("trace_req_body_raw", body)
				}
			}
		} else {
			head, restored, truncated := peekBody(c.Request.Body, traceMaxBodyBytes+1)
			c.Request.Body = restored
			if truncated && int64(len(head)) > traceMaxBodyBytes {
				size := int64(len(head))
				if c.Request.ContentLength > 0 {
					size = c.Request.ContentLength
				}
				c.Set("trace_req_body_omitted", size)
			} else if len(head) > 0 {
				if len(head) <= defaultTraceSanitizeLimits.MaxParseBytesInTrace {
					c.Set("trace_req_body", sanitizeJSONRequestBody(head))
				} else {
					c.Set("trace_req_body_raw", head)
				}
			}
		}

		c.Next()
	}
}

// TraceSanitizeLimits 控制写入 call_traces 的请求/响应正文在落库前的裁剪上限。
// MaxStringLenInTrace 是每个文本字段值的字符上限：默认调高到 500_000，保证语义分析
// 所需的完整上下文不被截断（原先 2048 会切断长 message content，破坏 token 用量/语义分析报告）。
// MaxBase64LenInTrace 是 base64 data-URI payload 的保留上限：图片二进制体积巨大，
// 仍以人类可读占位符替代，避免单条 trace 把表体积撑爆。
// MaxParseBytesInTrace 是请求体做 JSON sanitize 解析的字节上限：请求体超过该值时
// 完全跳过 json.Unmarshal / sanitizeValue / json.Marshal，直接以原始字节作为 trace 正文，
// 避免大 body 的对象树与输出副本造成约 3 倍 body 的峰值内存。
type TraceSanitizeLimits struct {
	MaxStringLenInTrace  int
	MaxBase64LenInTrace  int
	MaxParseBytesInTrace int
}

// defaultTraceSanitizeLimits 默认上限：文本字段关键上下文不截断，base64 仍用占位符，
// 超过 1 MB 的请求体不再解析（直存原始字节）。
var defaultTraceSanitizeLimits = TraceSanitizeLimits{
	MaxStringLenInTrace:  500_000,
	MaxBase64LenInTrace:  64,
	MaxParseBytesInTrace: 1 << 20,
}

// SetTraceSanitizeLimits 覆盖全局默认上限（在 main 启动时按配置调用一次，之后只读）。
// 各字段按「> 0 才覆盖」处理，为 0 的字段保留默认值。
func SetTraceSanitizeLimits(limits TraceSanitizeLimits) {
	if limits.MaxStringLenInTrace > 0 {
		defaultTraceSanitizeLimits.MaxStringLenInTrace = limits.MaxStringLenInTrace
	}
	if limits.MaxBase64LenInTrace > 0 {
		defaultTraceSanitizeLimits.MaxBase64LenInTrace = limits.MaxBase64LenInTrace
	}
	if limits.MaxParseBytesInTrace > 0 {
		defaultTraceSanitizeLimits.MaxParseBytesInTrace = limits.MaxParseBytesInTrace
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

// TraceLogEnabled 报告当前请求是否允许采集正文/响应。
// 与 TraceCapture 的门控保持一致：AuthMiddleware 写入的 log_enabled=false 时不得采集，
// 避免给"已关闭日志"的 Key 偷偷记正文。
func TraceLogEnabled(c *gin.Context) bool {
	v, ok := c.Get("log_enabled")
	if !ok {
		return true
	}
	enabled, ok := v.(bool)
	return !ok || enabled
}

// SanitizeJSONRequestBody 导出给需要自行采集正文的直通路径（视频/素材）复用同一套截断策略，
// 避免出现第二套 sanitize 实现导致正文口径不一致。
func SanitizeJSONRequestBody(raw []byte) []byte {
	return sanitizeJSONRequestBody(raw)
}

// CaptureTraceRequestBody 供已自行读取过 body 的 handler 补采请求正文。
// TraceCapture 只覆盖 chat 类白名单路径；视频/素材是直通路径，body 本来就在 handler 手里，
// 由 handler 主动登记，避免为了采集再读一次 Body。
//
// 门控放在这里（而不是只依赖下游 TraceWriter）是为了与 TraceCapture 一致：
// log_enabled=false 时连 sanitize/序列化都不做。
// 响应侧无此开销（只是登记对象引用），直接复用 SetTraceResponseObject。
func CaptureTraceRequestBody(c *gin.Context, body []byte) {
	if len(body) == 0 || !TraceLogEnabled(c) {
		return
	}
	c.Set("trace_req_body", SanitizeJSONRequestBody(body))
}
