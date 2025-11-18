package orchestrator

import (
	"encoding/json"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// streamUsageObserver 旁路解析流式 SSE chunk，提取末尾 usage 与首 token 延迟，
// 在流关闭时写回 Gin context，供埋点采集器（Logger 中间件 c.Next() 之后）计量与计价。
//
// 背景：流式响应原本是 zero-copy 透传，从不解析 chunk，导致 token/成本全部记为 0
// （usage_missing=true）。本 observer 复用 streaming.ObservedSSEStream 的旁路解析能力，
// 不改变转发给客户端的字节流。
type streamUsageObserver struct {
	gc           *gin.Context
	start        time.Time
	firstTokenAt time.Time
	usage        *core.Usage
}

func newStreamUsageObserver(gc *gin.Context, start time.Time) *streamUsageObserver {
	return &streamUsageObserver{gc: gc, start: start}
}

// OnJSONEvent 每个 SSE JSON chunk 触发一次（按流顺序）。payload 视为只读。
func (o *streamUsageObserver) OnJSONEvent(payload map[string]any) {
	if o == nil {
		return
	}

	// 首 token 延迟：记录首个携带内容增量的 chunk 时间
	if o.firstTokenAt.IsZero() && chunkHasContent(payload) {
		o.firstTokenAt = time.Now()
	}

	// 末尾 usage chunk：上游在 include_usage 注入后于流末返回。
	// 重新序列化后用 core.Usage 解析，保留 prompt/completion 明细及 cache 字段（RawUsage）。
	if raw, ok := payload["usage"]; ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			var u core.Usage
			if err := json.Unmarshal(b, &u); err == nil &&
				(u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0) {
				o.usage = &u
			}
		}
	}
}

// OnStreamClose 流关闭时回写 Gin context。该调用发生在请求 goroutine（io.Copy 之后），
// 早于 Logger 中间件的采集器读取，因此无并发问题。
func (o *streamUsageObserver) OnStreamClose() {
	if o == nil || o.gc == nil {
		return
	}
	if o.usage != nil {
		o.gc.Set("usage", *o.usage)
	}
	if !o.firstTokenAt.IsZero() {
		if ms := int(o.firstTokenAt.Sub(o.start).Milliseconds()); ms > 0 {
			o.gc.Set("first_token_ms", ms)
		}
	}
}

// chunkHasContent 判断 chunk 是否包含可见内容增量（content 或 reasoning_content）。
func chunkHasContent(payload map[string]any) bool {
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return false
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return false
	}
	delta, ok := first["delta"].(map[string]any)
	if !ok {
		return false
	}
	if c, ok := delta["content"].(string); ok && c != "" {
		return true
	}
	if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
		return true
	}
	return false
}
