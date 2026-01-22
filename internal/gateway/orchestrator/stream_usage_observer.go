package orchestrator

import (
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// streamUsageObserver 旁路解析流式 SSE chunk，提取末尾 usage、首 token 延迟
// 以及完整的助手回复文本，在流关闭时写回 Gin context。
//
// 关键设计：content/reasoning_content 在 OnJSONEvent 里逐片拼接，
// 流关闭时直接构造合成 JSON 写入 context，无需 responseBodyWriter 的 buf 双写，
// 彻底消除流式响应的内存副本。
type streamUsageObserver struct {
	gc               *gin.Context
	start            time.Time
	firstTokenAt     time.Time
	usage            *core.Usage
	// 流式回复文本累积（逐 chunk 拼接，避免保留原始字节）
	contentBuf       strings.Builder
	reasoningBuf     strings.Builder
	// 最后一个 chunk 的元数据（用于合成响应 JSON）
	lastID      string
	lastModel   string
	lastCreated int64
}

func newStreamUsageObserver(gc *gin.Context, start time.Time) *streamUsageObserver {
	o := &streamUsageObserver{gc: gc, start: start}
	// 预分配内容缓冲区，减少长输出时的扩容次数
	o.contentBuf.Grow(2048)
	return o
}

// OnJSONEvent 每个 SSE JSON chunk 触发一次（按流顺序）。payload 视为只读。
func (o *streamUsageObserver) OnJSONEvent(payload map[string]any) {
	if o == nil {
		return
	}

	// 记录元数据（id/model/created 取最后一个有值的 chunk）
	if id, ok := payload["id"].(string); ok && id != "" {
		o.lastID = id
	}
	if m, ok := payload["model"].(string); ok && m != "" {
		o.lastModel = m
	}
	if c, ok := payload["created"].(float64); ok && c > 0 {
		o.lastCreated = int64(c)
	}

	// 逐 chunk 拼接 content / reasoning_content
	if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
		if first, ok := choices[0].(map[string]any); ok {
			if delta, ok := first["delta"].(map[string]any); ok {
				if c, ok := delta["content"].(string); ok && c != "" {
					if o.firstTokenAt.IsZero() {
						o.firstTokenAt = time.Now()
					}
					o.contentBuf.WriteString(c)
				}
				if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
					if o.firstTokenAt.IsZero() {
						o.firstTokenAt = time.Now()
					}
					o.reasoningBuf.WriteString(rc)
				}
				// 兼容 Bedrock/Anthropic 等 provider：delta 中 "text" 字段
				if t, ok := delta["text"].(string); ok && t != "" {
					if o.firstTokenAt.IsZero() {
						o.firstTokenAt = time.Now()
					}
					o.contentBuf.WriteString(t)
				}
			}
			// 兼容 Bedrock content_block_delta 格式：
			// {"choices":[{"content_block":{"type":"text","text":"..."}}]}
			if cb, ok := first["content_block"].(map[string]any); ok {
				if t, ok := cb["text"].(string); ok && t != "" {
					if o.firstTokenAt.IsZero() {
						o.firstTokenAt = time.Now()
					}
					o.contentBuf.WriteString(t)
				}
			}
		}
	}

	// 兜底：如果 chunk 顶层有 delta（部分 provider 不套在 choices 里）
	if o.firstTokenAt.IsZero() {
		if delta, ok := payload["delta"].(map[string]any); ok {
			if t, ok := delta["text"].(string); ok && t != "" {
				o.firstTokenAt = time.Now()
				o.contentBuf.WriteString(t)
			}
			if c, ok := delta["content"].(string); ok && c != "" {
				o.firstTokenAt = time.Now()
				o.contentBuf.WriteString(c)
			}
		}
	}

	// 末尾 usage chunk：直接从 map 提取字段，避免 json.Marshal+Unmarshal 双往返
	if raw, ok := payload["usage"]; ok && raw != nil {
		if usageMap, ok := raw.(map[string]any); ok {
			u := core.Usage{
				PromptTokens:     toInt(usageMap["prompt_tokens"]),
				CompletionTokens: toInt(usageMap["completion_tokens"]),
				TotalTokens:      toInt(usageMap["total_tokens"]),
			}
			// v0.7: 兼容 Responses API 的 input_tokens/output_tokens 命名
			// Responses API 不使用 prompt_tokens/completion_tokens，但 usage_collector
			// 依赖 PromptTokens/CompletionTokens 做成本估算。
			if u.PromptTokens == 0 {
				u.PromptTokens = toInt(usageMap["input_tokens"])
			}
			if u.CompletionTokens == 0 {
				u.CompletionTokens = toInt(usageMap["output_tokens"])
			}
			if u.TotalTokens == 0 {
				u.TotalTokens = toInt(usageMap["total_tokens"])
				if u.TotalTokens == 0 {
					u.TotalTokens = u.PromptTokens + u.CompletionTokens
				}
			}
			// 提取 OpenAI-compatible prompt_tokens_details（含 cached_tokens）
			if details, ok := usageMap["prompt_tokens_details"].(map[string]any); ok {
				u.PromptTokensDetails = &core.PromptTokensDetails{
					CachedTokens: toInt(details["cached_tokens"]),
					AudioTokens:  toInt(details["audio_tokens"]),
					TextTokens:   toInt(details["text_tokens"]),
					ImageTokens:  toInt(details["image_tokens"]),
				}
			}
			// 提取 DeepSeek / Anthropic / Bedrock 缓存字段到 RawUsage
			rawKeys := []string{
				"prompt_cache_hit_tokens",    // DeepSeek
				"prompt_cache_miss_tokens",   // DeepSeek
				"cache_read_input_tokens",    // Anthropic / Bedrock
				"cache_creation_input_tokens", // Anthropic / Bedrock
			}
			for _, k := range rawKeys {
				if v := usageMap[k]; v != nil {
					if u.RawUsage == nil {
						u.RawUsage = make(map[string]any)
					}
					u.RawUsage[k] = v
				}
			}
			if u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0 {
				o.usage = &u
			}
		}
	}
}

// OnStreamClose 流关闭时回写 Gin context。
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
		} else {
			// ms <= 0 极罕见（首 token 与 start 几乎同时），用 TTFB 兜底
			if v, ok := o.gc.Get("t_ttfb_ms"); ok {
				if ttfb, ok := v.(int); ok && ttfb > 0 {
					o.gc.Set("first_token_ms", ttfb)
				}
			}
		}
	} else {
		// firstTokenAt 未设置：tool_use / 全空 content / 格式兼容问题等场景
		// 用 t_ttfb_ms 作为 TTFT 兜底，保证流式请求该字段不为 null
		if v, ok := o.gc.Get("t_ttfb_ms"); ok {
			if ms, ok := v.(int); ok && ms > 0 {
				o.gc.Set("first_token_ms", ms)
			}
		}
	}

	// 构造合成响应 JSON，供 TraceWriter 记录（不依赖 buf）
	content := o.contentBuf.String()
	reasoning := o.reasoningBuf.String()
	if content != "" || reasoning != "" || o.usage != nil {
		msg := map[string]any{
			"role":    "assistant",
			"content": content,
		}
		if reasoning != "" {
			msg["reasoning_content"] = reasoning
		}
		synthesized := map[string]any{
			"id":      o.lastID,
			"model":   o.lastModel,
			"created": o.lastCreated,
			"object":  "chat.completion",
			"stream":  true,
			"choices": []map[string]any{
				{"message": msg, "finish_reason": "stop"},
			},
		}
		if o.usage != nil {
			synthesized["usage"] = o.usage
		}
		o.gc.Set("stream_assembled_response", synthesized)
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

// toInt 从 JSON 数字（float64）安全转换为 int，非数字返回 0。
func toInt(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
