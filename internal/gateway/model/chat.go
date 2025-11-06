package model

// ChatCompletionRequest OpenAI 兼容请求结构
type ChatCompletionRequest struct {
	Model       string    `json:"model" binding:"required"`
	Messages    []Message `json:"messages" binding:"required,min=1,dive"`
	Stream      bool      `json:"stream"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	N           int       `json:"n,omitempty"`
	Stop        any       `json:"stop,omitempty"`
	User        string    `json:"user,omitempty"`
}

// Message 消息结构
type Message struct {
	Role             string `json:"role" binding:"required,oneof=system user assistant"`
	Content          string `json:"content" binding:"required"`
	ReasoningContent string `json:"reasoning_content,omitempty"` // reasoning/thinking 内容
}

// ChatCompletionResponse 普通响应结构
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice 选择项
type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Delta   `json:"delta,omitempty"`
	FinishReason *string  `json:"finish_reason"`
}

// Delta 流式增量内容
type Delta struct {
	Role             string            `json:"role,omitempty"`
	Content          string            `json:"content,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"` // reasoning/thinking 增量
	ToolCallID       string            `json:"tool_call_id,omitempty"`      // tool_call 的 ID（仅 Claude tool_use 场景）
	ToolCallFunction *ToolCallFunction `json:"tool_call_function,omitempty"` // tool_call 函数信息
	ToolCallIndex    *int              `json:"tool_call_index,omitempty"`   // tool_call 索引
}

// ToolCallFunction tool call 中的函数信息
type ToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Usage Token 使用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk 流式响应 chunk
type ChatCompletionChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// StreamChunk channel 中传递的流式数据块
type StreamChunk struct {
	Data  *ChatCompletionChunk
	Error error
	Done  bool
}

// ModelsResponse GET /v1/models 响应
type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ModelInfo 模型信息
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
