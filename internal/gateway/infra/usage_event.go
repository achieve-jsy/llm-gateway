package infra

import "time"

// UsageEvent carries usage data between the hot-path middleware and background batch writer.
type UsageEvent struct {
	RequestID          string    `json:"request_id"`
	KeyID              string    `json:"key_id"`
	UserID             string    `json:"user_id"`
	DeptID             *string   `json:"dept_id"`
	StudioID           *string   `json:"studio_id"`
	Model              string    `json:"model"`
	ProviderType       string    `json:"provider_type"`
	ProviderModelID    *string   `json:"provider_model_id"`
	IsStream           bool      `json:"is_stream"`
	SessionID          *string   `json:"session_id"`
	StatDate           time.Time `json:"stat_date"`
	PromptTokens       int       `json:"prompt_tokens"`
	CompletionTokens   int       `json:"completion_tokens"`
	ReasoningTokens    int       `json:"reasoning_tokens"`
	CachedTokens       int       `json:"cached_tokens"`
	TTFTMs             *int      `json:"ttft_ms"`
	DurationMs         *int      `json:"duration_ms"`
	ProviderDurationMs *int      `json:"provider_duration_ms"`
	Status             string    `json:"status"`
	UsageMissing       bool      `json:"usage_missing"`
	Messages           []Message `json:"messages,omitempty"`
	ResponseText       string    `json:"response_text,omitempty"`
	Tools              []Tool    `json:"tools,omitempty"`
	ByteSize           int       `json:"byte_size,omitempty"`
	Truncated          bool      `json:"truncated,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
