package middleware

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/infra"
)

// UsageCollector extracts LLM usage metrics after handler completion
// and sends them to a channel for batch processing.
type UsageCollector struct {
	ch chan<- *infra.UsageEvent
}

// NewUsageCollector creates a UsageCollector with the given output channel.
func NewUsageCollector(ch chan<- *infra.UsageEvent) *UsageCollector {
	return &UsageCollector{ch: ch}
}

// Handler returns a Gin middleware that calls c.Next() first, then extracts
// usage data from gin.Context and sends a UsageEvent (non-blocking).
func (uc *UsageCollector) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		requestID := c.GetString(string(RequestIDKey))
		keyID := c.GetString(string(KeyIDKey))
		userID := c.GetString(string(UserIDKey))
		deptIDStr := c.GetString(string(DeptIDKey))
		model := c.GetString(string(ModelKey))
		providerType := c.GetString(string(ProviderTypeKey))
		providerModelIDStr := c.GetString(string(ProviderModelIDKey))
		sessionIDStr := c.GetString(string(SessionIDKey))

		isStream := c.GetBool(string(IsStreamKey))

		promptTokens := c.GetInt(string(PromptTokensKey))
		completionTokens := c.GetInt(string(CompletionTokensKey))
		reasoningTokens := c.GetInt(string(ReasoningTokensKey))
		cachedTokens := c.GetInt(string(CachedTokensKey))

		var deptID *string
		if deptIDStr != "" {
			deptID = &deptIDStr
		}

		var providerModelID *string
		if providerModelIDStr != "" {
			providerModelID = &providerModelIDStr
		}

		var sessionID *string
		if sessionIDStr != "" {
			sessionID = &sessionIDStr
		}

		var ttftMs *int
		if v, ok := c.Get(string(TTFTKey)); ok {
			if val, convOK := v.(int); convOK {
				ttftMs = &val
			}
		}

		var durationMs *int
		if v, ok := c.Get(string(DurationKey)); ok {
			if val, convOK := v.(int); convOK {
				durationMs = &val
			}
		}

		var providerDurationMs *int
		if v, ok := c.Get(string(ProviderDurationKey)); ok {
			if val, convOK := v.(int); convOK {
				providerDurationMs = &val
			}
		}

		status := c.GetString("status")
		if status == "" {
			status = "success"
		}

		event := &infra.UsageEvent{
			RequestID:          requestID,
			KeyID:              keyID,
			UserID:             userID,
			DeptID:             deptID,
			Model:              model,
			ProviderType:       providerType,
			ProviderModelID:    providerModelID,
			IsStream:           isStream,
			SessionID:          sessionID,
			StatDate:           time.Now(),
			PromptTokens:       promptTokens,
			CompletionTokens:   completionTokens,
			ReasoningTokens:    reasoningTokens,
			CachedTokens:       cachedTokens,
			TTFTMs:             ttftMs,
			DurationMs:         durationMs,
			ProviderDurationMs: providerDurationMs,
			Status:             status,
		}

		// 从上下文收集请求消息和响应文本用于 call_payloads
		if messagesRaw, exists := c.Get(string(MessagesKey)); exists {
			if msgs, ok := messagesRaw.([]core.Message); ok {
				var ems []infra.Message
				for _, m := range msgs {
					content, _ := m.Content.(string)
					ems = append(ems, infra.Message{Role: m.Role, Content: content})
				}
				event.Messages = ems
			}
		}
		if responseTextRaw, exists := c.Get(string(ResponseTextKey)); exists {
			if s, ok := responseTextRaw.(string); ok {
				event.ResponseText = s
			}
		}

		// 计算 payload 字节大小
		var byteSize int
		if len(event.Messages) > 0 {
			if b, err := json.Marshal(event.Messages); err == nil {
				byteSize += len(b)
			}
		}
		byteSize += len(event.ResponseText)
		event.ByteSize = byteSize

		select {
		case uc.ch <- event:
			infra.UsageMetricsInstance.Emitted.Inc()
		default:
			infra.UsageMetricsInstance.Dropped.Inc()
		}
	}
}
