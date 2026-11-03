package orchestrator

import (
	"encoding/json"
)

// StreamUsage holds token usage extracted from a streaming response chunk.
// Fields map to the context keys in internal/gateway/middleware/context_keys.go:
//   PromptTokens     -> "prompt_tokens"
//   CompletionTokens -> "completion_tokens"
//   ReasoningTokens  -> "reasoning_tokens"
//   CachedTokens     -> "cached_tokens"
type StreamUsage struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
	UsageMissing     bool
}

// StreamUsageParser extracts token usage from streaming SSE chunks.
// It handles both OpenAI-compatible and Anthropic event formats.
type StreamUsageParser struct{}

// NewStreamUsageParser creates a new StreamUsageParser.
func NewStreamUsageParser() *StreamUsageParser {
	return &StreamUsageParser{}
}

// ---- OpenAI-compatible chunk parsing ----

// openaiUsageChunk is a minimal struct for parsing the usage field
// from the final SSE chunk of an OpenAI-compatible streaming response.
//
// OpenAI final chunk format:
//
//	{
//	  "choices": [],
//	  "usage": {
//	    "prompt_tokens": 10,
//	    "completion_tokens": 20,
//	    "prompt_tokens_details": { "cached_tokens": 5 },
//	    "completion_tokens_details": { "reasoning_tokens": 3 }
//	  }
//	}
type openaiUsageChunk struct {
	Usage *openaiStreamUsage `json:"usage,omitempty"`
}

type openaiStreamUsage struct {
	PromptTokens            int                                `json:"prompt_tokens"`
	CompletionTokens        int                                `json:"completion_tokens"`
	PromptTokensDetails     *openaiPromptTokensDetails         `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openaiCompletionTokensDetails     `json:"completion_tokens_details,omitempty"`
}

type openaiPromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type openaiCompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// ParseOpenAIChunk attempts to extract StreamUsage from an OpenAI-compatible
// SSE data payload. Returns nil if the chunk does not contain usage info
// (most intermediate chunks won't have it; only the final chunk does).
//
// The caller should pass the raw JSON bytes from a single SSE "data:" line
// (without the "data: " prefix). Non-JSON lines such as "data: [DONE]"
// safely return nil.
func (p *StreamUsageParser) ParseOpenAIChunk(data []byte) *StreamUsage {
	var chunk openaiUsageChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil
	}
	if chunk.Usage == nil {
		return nil
	}

	u := &StreamUsage{
		PromptTokens:     chunk.Usage.PromptTokens,
		CompletionTokens: chunk.Usage.CompletionTokens,
	}

	if d := chunk.Usage.PromptTokensDetails; d != nil {
		u.CachedTokens = d.CachedTokens
	}
	if d := chunk.Usage.CompletionTokensDetails; d != nil {
		u.ReasoningTokens = d.ReasoningTokens
	}

	return u
}

// ---- Anthropic event parsing ----

// anthropicMessageStartEvent is a minimal parse target for the
// "message_start" SSE event from Anthropic.
//
// Anthropic message_start format:
//
//	{
//	  "type": "message_start",
//	  "message": {
//	    "usage": {
//	      "input_tokens": 10,
//	      "cache_read_input_tokens": 5,
//	      "cache_creation_input_tokens": 0
//	    }
//	  }
//	}
type anthropicMessageStartEvent struct {
	Type    string                  `json:"type"`
	Message *anthropicMsgStartBody  `json:"message,omitempty"`
}

type anthropicMsgStartBody struct {
	Usage *anthropicMsgStartUsage `json:"usage,omitempty"`
}

type anthropicMsgStartUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// anthropicMessageDeltaEvent is a minimal parse target for the
// "message_delta" SSE event from Anthropic.
//
// Anthropic message_delta format:
//
//	{
//	  "type": "message_delta",
//	  "usage": {
//	    "output_tokens": 100
//	  }
//	}
//
// For models with extended thinking (Claude 3.7+), the usage may also include
// thinking_tokens when thinking is enabled.
type anthropicMessageDeltaEvent struct {
	Type  string                    `json:"type"`
	Usage *anthropicDeltaUsage      `json:"usage,omitempty"`
}

type anthropicDeltaUsage struct {
	OutputTokens   int `json:"output_tokens"`
	ThinkingTokens int `json:"thinking_tokens,omitempty"`
}

// ParseAnthropicEvent attempts to extract StreamUsage from an Anthropic
// streaming SSE event. It handles:
//   - message_start:  input_tokens (prompt), cache_read/creation (cached)
//   - message_delta:  output_tokens (completion), thinking_tokens (reasoning)
//   - Other events:   returns nil
//
// The caller should pass the event type (e.g. "message_start") and the
// raw JSON bytes of the event data (without any "event:" / "data:" prefix).
func (p *StreamUsageParser) ParseAnthropicEvent(eventType string, data []byte) *StreamUsage {
	switch eventType {
	case "message_start":
		return p.parseAnthropicMessageStart(data)
	case "message_delta":
		return p.parseAnthropicMessageDelta(data)
	default:
		return nil
	}
}

func (p *StreamUsageParser) parseAnthropicMessageStart(data []byte) *StreamUsage {
	var evt anthropicMessageStartEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return nil
	}
	if evt.Message == nil || evt.Message.Usage == nil {
		return nil
	}

	u := &StreamUsage{
		PromptTokens: evt.Message.Usage.InputTokens,
	}

	// Anthropic cache_read_input_tokens and cache_creation_input_tokens both
	// count as "cached" tokens for billing/usage tracking purposes.
	u.CachedTokens = evt.Message.Usage.CacheReadInputTokens +
		evt.Message.Usage.CacheCreationInputTokens

	return u
}

func (p *StreamUsageParser) parseAnthropicMessageDelta(data []byte) *StreamUsage {
	var evt anthropicMessageDeltaEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return nil
	}
	if evt.Usage == nil {
		return nil
	}

	u := &StreamUsage{
		CompletionTokens: evt.Usage.OutputTokens,
		ReasoningTokens:  evt.Usage.ThinkingTokens,
	}

	return u
}
