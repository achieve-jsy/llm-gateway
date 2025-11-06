package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
)

const (
	defaultBaseURL   = "https://api.anthropic.com/v1"
	anthropicVersion = "2023-06-01"
	defaultMaxTokens = 4096
)

// Config Anthropic 适配器配置
type Config struct {
	APIKey  string   // Anthropic API Key
	BaseURL string   // 基础 URL，默认 https://api.anthropic.com/v1
	Models  []string // 支持的模型列表
}

// Adapter Anthropic Provider 适配器
type Adapter struct {
	config Config
	client *http.Client
}

// New 创建 Anthropic 适配器实例
func New(cfg Config) provider.Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	return &Adapter{
		config: cfg,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Name 返回 Provider 名称
func (a *Adapter) Name() string {
	return "anthropic"
}

// Chat 普通调用（非流式）
func (a *Adapter) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	claudeReq := convertRequest(req, false)

	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic api error [%d]: %s", resp.StatusCode, string(respBody))
	}

	var claudeResp claudeResponse
	if err := json.Unmarshal(respBody, &claudeResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if claudeResp.Error != nil && claudeResp.Error.Type != "" {
		return nil, fmt.Errorf("anthropic error: %s: %s", claudeResp.Error.Type, claudeResp.Error.Message)
	}

	return convertResponse(&claudeResp, req.Model), nil
}

// ChatStream 流式调用，返回 chunk channel
// 参考 new-api 的 Claude SSE 事件处理逻辑：
// - message_start: 提取消息 ID、模型、input_tokens
// - content_block_start: 标记内容块类型（text/thinking/tool_use）
// - content_block_delta: 增量文本/thinking/tool_call arguments
// - content_block_stop: 内容块结束
// - message_delta: stop_reason + output_tokens
// - message_stop: 流结束
func (a *Adapter) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	claudeReq := convertRequest(req, true)

	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic api error [%d]: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan model.StreamChunk, 32)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		a.processClaudeStream(resp.Body, req.Model, ch)
	}()

	return ch, nil
}

// processClaudeStream 处理 Claude SSE 事件流，转换为 OpenAI 格式 chunk
// 核心逻辑参考 new-api relay/channel/claude/relay-claude.go
func (a *Adapter) processClaudeStream(body io.Reader, requestModel string, ch chan<- model.StreamChunk) {
	var msgID string
	var msgModel string
	createdTime := time.Now().Unix()

	// 当前内容块跟踪
	var currentBlockType string  // "text" / "thinking" / "tool_use"
	var currentToolCallID string // tool_use block 的 ID
	var currentToolName string   // tool_use block 的函数名
	var toolCallIndex int        // tool_call 索引

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := scanner.Text()

		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// 跳过 event: 行（我们从 data 的 type 字段判断事件类型）
		if strings.HasPrefix(line, "event:") {
			continue
		}

		// 解析 data: 行
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}

		var event claudeStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			ch <- model.StreamChunk{Error: fmt.Errorf("unmarshal claude event: %w", err)}
			return
		}

		// 检查是否有错误
		if event.Error != nil && event.Error.Type != "" {
			ch <- model.StreamChunk{Error: fmt.Errorf("anthropic stream error: %s: %s", event.Error.Type, event.Error.Message)}
			return
		}

		switch event.Type {
		case "message_start":
			// 提取消息 ID 和模型
			if event.Message != nil {
				msgID = event.Message.ID
				msgModel = event.Message.Model
				if msgModel == "" {
					msgModel = requestModel
				}
			}
			// 发送 role 初始化 chunk
			chunk := &model.ChatCompletionChunk{
				ID:      fmt.Sprintf("chatcmpl-%s", msgID),
				Object:  "chat.completion.chunk",
				Created: createdTime,
				Model:   msgModel,
				Choices: []model.Choice{
					{
						Index: 0,
						Delta: &model.Delta{
							Role: "assistant",
						},
					},
				},
			}
			ch <- model.StreamChunk{Data: chunk}

		case "content_block_start":
			// 标记新的内容块类型
			if event.ContentBlock != nil {
				currentBlockType = event.ContentBlock.Type
				if currentBlockType == "tool_use" {
					currentToolCallID = event.ContentBlock.ID
					currentToolName = event.ContentBlock.Name
				}
				// 如果 text block 有初始文本，直接发送
				if currentBlockType == "text" && event.ContentBlock.Text != nil && *event.ContentBlock.Text != "" {
					chunk := &model.ChatCompletionChunk{
						ID:      fmt.Sprintf("chatcmpl-%s", msgID),
						Object:  "chat.completion.chunk",
						Created: createdTime,
						Model:   msgModel,
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									Content: *event.ContentBlock.Text,
								},
							},
						},
					}
					ch <- model.StreamChunk{Data: chunk}
				}
				// tool_use block 开始：发送 tool_call 初始化
				if currentBlockType == "tool_use" {
					chunk := &model.ChatCompletionChunk{
						ID:      fmt.Sprintf("chatcmpl-%s", msgID),
						Object:  "chat.completion.chunk",
						Created: createdTime,
						Model:   msgModel,
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									ToolCallID:       currentToolCallID,
									ToolCallFunction: &model.ToolCallFunction{Name: currentToolName, Arguments: ""},
									ToolCallIndex:    &toolCallIndex,
								},
							},
						},
					}
					ch <- model.StreamChunk{Data: chunk}
				}
			}

		case "content_block_delta":
			if event.Delta == nil {
				continue
			}

			switch event.Delta.Type {
			case "text_delta":
				// 普通文本增量
				if event.Delta.Text != nil {
					chunk := &model.ChatCompletionChunk{
						ID:      fmt.Sprintf("chatcmpl-%s", msgID),
						Object:  "chat.completion.chunk",
						Created: createdTime,
						Model:   msgModel,
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									Content: *event.Delta.Text,
								},
							},
						},
					}
					ch <- model.StreamChunk{Data: chunk}
				}

			case "thinking_delta":
				// thinking/reasoning 增量（extended thinking）
				if event.Delta.Thinking != nil {
					chunk := &model.ChatCompletionChunk{
						ID:      fmt.Sprintf("chatcmpl-%s", msgID),
						Object:  "chat.completion.chunk",
						Created: createdTime,
						Model:   msgModel,
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									ReasoningContent: *event.Delta.Thinking,
								},
							},
						},
					}
					ch <- model.StreamChunk{Data: chunk}
				}

			case "input_json_delta":
				// tool_call arguments 增量
				if event.Delta.PartialJSON != nil {
					chunk := &model.ChatCompletionChunk{
						ID:      fmt.Sprintf("chatcmpl-%s", msgID),
						Object:  "chat.completion.chunk",
						Created: createdTime,
						Model:   msgModel,
						Choices: []model.Choice{
							{
								Index: 0,
								Delta: &model.Delta{
									ToolCallFunction: &model.ToolCallFunction{Arguments: *event.Delta.PartialJSON},
									ToolCallIndex:    &toolCallIndex,
								},
							},
						},
					}
					ch <- model.StreamChunk{Data: chunk}
				}

			case "signature_delta":
				// 加密签名块，忽略
				continue
			}

		case "content_block_stop":
			// 内容块结束
			if currentBlockType == "tool_use" {
				toolCallIndex++
			}
			currentBlockType = ""

		case "message_delta":
			// 消息结束，包含 stop_reason 和 output_tokens
			if event.Delta != nil && event.Delta.StopReason != nil {
				finishReason := stopReasonToOpenAI(*event.Delta.StopReason)
				chunk := &model.ChatCompletionChunk{
					ID:      fmt.Sprintf("chatcmpl-%s", msgID),
					Object:  "chat.completion.chunk",
					Created: createdTime,
					Model:   msgModel,
					Choices: []model.Choice{
						{
							Index:        0,
							Delta:        &model.Delta{},
							FinishReason: &finishReason,
						},
					},
				}
				ch <- model.StreamChunk{Data: chunk}
			}

		case "message_stop":
			// 流结束
			ch <- model.StreamChunk{Done: true}
			return

		case "error":
			// 错误事件
			if event.Error != nil {
				ch <- model.StreamChunk{Error: fmt.Errorf("anthropic stream error: %s: %s", event.Error.Type, event.Error.Message)}
			} else {
				ch <- model.StreamChunk{Error: fmt.Errorf("anthropic stream error: unknown")}
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- model.StreamChunk{Error: fmt.Errorf("read stream: %w", err)}
		return
	}

	// 流正常结束但未收到 message_stop
	ch <- model.StreamChunk{Done: true}
}

// ListModels 返回配置中的模型列表
func (a *Adapter) ListModels(_ context.Context) ([]model.ModelInfo, error) {
	models := make([]model.ModelInfo, 0, len(a.config.Models))
	now := time.Now().Unix()
	for _, m := range a.config.Models {
		models = append(models, model.ModelInfo{
			ID:      m,
			Object:  "model",
			Created: now,
			OwnedBy: "anthropic",
		})
	}
	return models, nil
}

// HealthCheck 健康检查
func (a *Adapter) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, a.config.BaseURL+"/messages", nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}
	a.setHeaders(httpReq)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("health check request: %w", err)
	}
	defer resp.Body.Close()

	// Anthropic 返回 405 表示服务可达但 GET 不允许，认为健康
	if resp.StatusCode >= 500 {
		return fmt.Errorf("anthropic health check failed with status: %d", resp.StatusCode)
	}
	return nil
}

// setHeaders 设置 Anthropic API 请求头
func (a *Adapter) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.config.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

// ============== 内部类型定义 ==============

// claudeRequest Anthropic Messages API 请求体
type claudeRequest struct {
	Model         string           `json:"model"`
	Messages      []claudeMessage  `json:"messages"`
	System        string           `json:"system,omitempty"`
	MaxTokens     int              `json:"max_tokens"`
	Stream        bool             `json:"stream,omitempty"`
	Temperature   *float64         `json:"temperature,omitempty"`
	TopP          *float64         `json:"top_p,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
	Tools         []claudeTool     `json:"tools,omitempty"`
	ToolChoice    *claudeToolChoice `json:"tool_choice,omitempty"`
}

// claudeMessage Anthropic 消息格式
type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// claudeTool Anthropic tool 定义
type claudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// claudeToolChoice tool_choice 配置
type claudeToolChoice struct {
	Type string `json:"type"` // auto / any / tool / none
	Name string `json:"name,omitempty"`
}

// claudeResponse Anthropic 非流式响应
type claudeResponse struct {
	ID         string           `json:"id"`
	Type       string           `json:"type"`
	Role       string           `json:"role"`
	Content    []claudeBlock    `json:"content"`
	Model      string           `json:"model"`
	StopReason *string          `json:"stop_reason"`
	Usage      *claudeUsage     `json:"usage"`
	Error      *claudeAPIError  `json:"error,omitempty"`
}

// claudeBlock 内容块
type claudeBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	Thinking *string         `json:"thinking,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

// claudeUsage 用量信息
type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// claudeAPIError API 错误
type claudeAPIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// claudeStreamEvent 流式响应事件（统一结构）
type claudeStreamEvent struct {
	Type         string             `json:"type"`
	Message      *claudeResponse    `json:"message,omitempty"`
	Index        *int               `json:"index,omitempty"`
	ContentBlock *claudeContentBlock `json:"content_block,omitempty"`
	Delta        *claudeDelta       `json:"delta,omitempty"`
	Usage        *claudeUsage       `json:"usage,omitempty"`
	Error        *claudeAPIError    `json:"error,omitempty"`
}

// claudeContentBlock content_block_start 中的内容块信息
type claudeContentBlock struct {
	Type string  `json:"type"` // text / thinking / tool_use
	Text *string `json:"text,omitempty"`
	ID   string  `json:"id,omitempty"`
	Name string  `json:"name,omitempty"`
}

// claudeDelta 流式增量
type claudeDelta struct {
	Type        string  `json:"type,omitempty"`
	Text        *string `json:"text,omitempty"`
	Thinking    *string `json:"thinking,omitempty"`
	PartialJSON *string `json:"partial_json,omitempty"`
	StopReason  *string `json:"stop_reason,omitempty"`
}

// ============== 转换逻辑 ==============

// convertRequest 将 OpenAI 格式请求转换为 Anthropic 格式
// 参考 new-api RequestOpenAI2ClaudeMessage：
// - 提取 system 消息
// - 确保第一条消息是 user（如果不是则补一条）
// - 合并相邻相同 role 的消息
func convertRequest(req *model.ChatCompletionRequest, stream bool) *claudeRequest {
	claudeReq := &claudeRequest{
		Model:  req.Model,
		Stream: stream,
	}
	if req.Temperature != nil {
		v := float64(*req.Temperature)
		claudeReq.Temperature = &v
	}
	if req.TopP != nil {
		v := float64(*req.TopP)
		claudeReq.TopP = &v
	}

	// 设置 max_tokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		claudeReq.MaxTokens = *req.MaxTokens
	} else {
		claudeReq.MaxTokens = defaultMaxTokens
	}

	// 处理 stop 参数
	if req.Stop != nil {
		switch v := req.Stop.(type) {
		case string:
			claudeReq.StopSequences = []string{v}
		case []interface{}:
			for _, s := range v {
				if str, ok := s.(string); ok {
					claudeReq.StopSequences = append(claudeReq.StopSequences, str)
				}
			}
		}
	}

	// 提取 system 消息，其余转为 Anthropic messages
	var systemParts []string
	var messages []claudeMessage
	var lastRole string

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemParts = append(systemParts, msg.Content)
			continue
		}

		role := msg.Role
		content := msg.Content

		// 确保第一条非 system 消息是 user
		if len(messages) == 0 && role != "user" {
			messages = append(messages, claudeMessage{Role: "user", Content: "..."})
		}

		// 合并相邻相同 role 的消息（参考 new-api 逻辑）
		if role == lastRole && role != "tool" {
			messages[len(messages)-1].Content += " " + content
		} else {
			messages = append(messages, claudeMessage{
				Role:    role,
				Content: content,
			})
		}
		lastRole = role
	}

	if len(systemParts) > 0 {
		claudeReq.System = strings.Join(systemParts, "\n")
	}
	claudeReq.Messages = messages

	return claudeReq
}

// convertResponse 将 Anthropic 响应转换为 OpenAI 格式
// 参考 new-api ResponseClaude2OpenAI：处理 text/thinking/tool_use 块
func convertResponse(resp *claudeResponse, requestModel string) *model.ChatCompletionResponse {
	var content string
	var reasoningContent string

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "thinking":
			if block.Thinking != nil {
				reasoningContent += *block.Thinking
			}
		}
	}

	finishReason := ""
	if resp.StopReason != nil {
		finishReason = stopReasonToOpenAI(*resp.StopReason)
	}

	result := &model.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", resp.ID),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestModel,
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: &finishReason,
			},
		},
	}

	// 如果有 reasoning_content，附加到 delta（非流式场景通过 Message 字段传递）
	if reasoningContent != "" && len(result.Choices) > 0 {
		result.Choices[0].Message.ReasoningContent = reasoningContent
	}

	// 设置 usage
	if resp.Usage != nil {
		result.Usage = &model.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}

	return result
}

// stopReasonToOpenAI 将 Anthropic stop_reason 转换为 OpenAI finish_reason
// 参考 new-api stopReasonClaude2OpenAI
func stopReasonToOpenAI(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}
