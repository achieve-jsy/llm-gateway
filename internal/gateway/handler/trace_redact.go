package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/example/llm-gateway/internal/gateway/redact"
)

// traceBodyKeys 是 TraceCapture 写进 gin context 的两个正文键：
// 普通请求走 sanitize 后的 trace_req_body，大正文走原始字节的 trace_req_body_raw。
var traceBodyKeys = []string{"trace_req_body", "trace_req_body_raw"}

// protectTraceBody 把落库用的请求正文换成脱敏后的版本。
//
// 时序决定的：TraceCapture 是中间件，在 handler 之前就把原始字节抓进了 context，
// 所以在 handler 里脱敏只能覆盖「出网」那条路径，call_traces 里仍是明文。
// 这里用同一个 session 覆写 context 里的字节，落库内容与上游一致。
func protectTraceBody(c *gin.Context, session *redact.Session) {
	if session == nil {
		return
	}
	for _, key := range traceBodyKeys {
		v, ok := c.Get(key)
		if !ok {
			continue
		}
		raw, ok := v.([]byte)
		if !ok || len(raw) == 0 {
			continue
		}
		c.Set(key, session.ProtectTraceBody(c.Request.Context(), raw))
	}
}
