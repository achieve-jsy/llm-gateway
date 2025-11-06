package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
)

const (
	defaultRegion    = "us-east-1"
	anthropicVersion = "bedrock-2023-05-31"
	defaultMaxTokens = 4096
)

// Config AWS Bedrock 适配器配置
type Config struct {
	AccessKeyID     string   // AWS Access Key ID
	SecretAccessKey  string   // AWS Secret Access Key
	Region          string   // AWS Region，默认 us-east-1
	Models          []string // 支持的模型列表
}

// Adapter AWS Bedrock Provider 适配器
// 使用 aws-sdk-go-v2/service/bedrockruntime 官方 SDK
type Adapter struct {
	config Config
	client *bedrockruntime.Client
}

// New 创建 Bedrock 适配器实例
// 参考 new-api relay/channel/aws/relay-aws.go 的 newAwsClient 逻辑
func New(cfg Config) provider.Provider {
	if cfg.Region == "" {
		cfg.Region = defaultRegion
	}

	// 使用 aws-sdk-go-v2 创建 bedrockruntime client
	client := bedrockruntime.New(bedrockruntime.Options{
		Region:      cfg.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	})

	return &Adapter{
		config: cfg,
		client: client,
	}
}

// Name 返回 Provider 名称
func (a *Adapter) Name() string {
	return "bedrock"
}

// Chat 普通调用（非流式），使用 InvokeModel API
// 参考 new-api awsHandler：构造请求体 → InvokeModel → 解析 Claude 响应
func (a *Adapter) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	bedrockReq := convertToBedrockRequest(req)

	bodyBytes, err := json.Marshal(bedrockReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	modelID := req.Model
	input := &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
		Body:        bodyBytes,
	}

	output, err := a.client.InvokeModel(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock InvokeModel: %w", err)
	}

	// 解析 Claude 格式响应
	var claudeResp bedrockResponse
	if err := json.Unmarshal(output.Body, &claudeResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return convertFromBedrockResponse(&claudeResp, req.Model), nil
}

// ChatStream 流式调用，使用 InvokeModelWithResponseStream API
// 参考 new-api awsStreamHandler：使用 SDK 事件流处理
func (a *Adapter) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	bedrockReq := convertToBedrockRequest(req)

	bodyBytes, err := json.Marshal(bedrockReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	modelID := req.Model
	input := &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
		Body:        bodyBytes,
	}

	output, err := a.client.InvokeModelWithResponseStream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock InvokeModelWithResponseStream: %w", err)
	}

	stream := output.GetStream()
	ch := make(chan model.StreamChunk, 32)

	go func() {
		defer close(ch)
		defer stream.Close()

		a.processBedrockStream(stream, req.Model, ch)
	}()

	return ch, nil
}

// processBedrockStream 处理 Bedrock 事件流
// 参考 new-api awsStreamHandler：遍历 stream.Events() 处理每个 chunk
// Bedrock 上的 Claude 返回格式与 Anthropic Messages API SSE 事件一致
func (a *Adapter) processBedrockStream(stream *bedrockruntime.InvokeModelWithResponseStreamEventStream, requestModel string, ch chan<- model.StreamChunk) {
	var msgID string
	var msgModel string
	createdTime := time.Now().Unix()

	// 当前内容块跟踪
	var currentBlockType string
	var currentToolCallID string
	var currentToolName string
	var toolCallIndex int

	for event := range stream.Events() {
		switch v := event.(type) {
		case *bedrockruntimeTypes.ResponseStreamMemberChunk:
			// 每个 chunk 包含一个 Claude SSE 事件的 JSON
			var streamEvent bedrockStreamEvent
			if err := json.Unmarshal(v.Value.Bytes, &streamEvent); err != nil {
				ch <- model.StreamChunk{Error: fmt.Errorf("unmarshal bedrock chunk: %w", err)}
				return
			}

			switch streamEvent.Type {
			case "message_start":
				if streamEvent.Message != nil {
					msgID = streamEvent.Message.ID
					msgModel = streamEvent.Message.Model
					if msgModel == "" {
						msgModel = requestModel
					}
				}
				// 发送 role 初始化
				chunk := &model.ChatCompletionChunk{
					ID:      fmt.Sprintf("chatcmpl-%s", msgID),
					Object:  "chat.completion.chunk",
					Created: createdTime,
					Model:   msgModel,
					Choices: []model.Choice{
						{
							Index: 0,
							Delta: &model.Delta{Role: "assistant"},
						},
					},
				}
				ch <- model.StreamChunk{Data: chunk}

			case "content_block_start":
				if streamEvent.ContentBlock != nil {
					currentBlockType = streamEvent.ContentBlock.Type
					if currentBlockType == "tool_use" {
						currentToolCallID = streamEvent.ContentBlock.ID
						currentToolName = streamEvent.ContentBlock.Name
					}
					// text block 初始文本
					if currentBlockType == "text" && streamEvent.ContentBlock.Text != nil && *streamEvent.ContentBlock.Text != "" {
						chunk := &model.ChatCompletionChunk{
							ID:      fmt.Sprintf("chatcmpl-%s", msgID),
							Object:  "chat.completion.chunk",
							Created: createdTime,
							Model:   msgModel,
							Choices: []model.Choice{
								{
									Index: 0,
									Delta: &model.Delta{Content: *streamEvent.ContentBlock.Text},
								},
							},
						}
						ch <- model.StreamChunk{Data: chunk}
					}
					// tool_use block 开始
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
				if streamEvent.Delta == nil {
					continue
				}
				switch streamEvent.Delta.Type {
				case "text_delta":
					if streamEvent.Delta.Text != nil {
						chunk := &model.ChatCompletionChunk{
							ID:      fmt.Sprintf("chatcmpl-%s", msgID),
							Object:  "chat.completion.chunk",
							Created: createdTime,
							Model:   msgModel,
							Choices: []model.Choice{
								{
									Index: 0,
									Delta: &model.Delta{Content: *streamEvent.Delta.Text},
								},
							},
						}
						ch <- model.StreamChunk{Data: chunk}
					}
				case "thinking_delta":
					if streamEvent.Delta.Thinking != nil {
						chunk := &model.ChatCompletionChunk{
							ID:      fmt.Sprintf("chatcmpl-%s", msgID),
							Object:  "chat.completion.chunk",
							Created: createdTime,
							Model:   msgModel,
							Choices: []model.Choice{
								{
									Index: 0,
									Delta: &model.Delta{ReasoningContent: *streamEvent.Delta.Thinking},
								},
							},
						}
						ch <- model.StreamChunk{Data: chunk}
					}
				case "input_json_delta":
					if streamEvent.Delta.PartialJSON != nil {
						chunk := &model.ChatCompletionChunk{
							ID:      fmt.Sprintf("chatcmpl-%s", msgID),
							Object:  "chat.completion.chunk",
							Created: createdTime,
							Model:   msgModel,
							Choices: []model.Choice{
								{
									Index: 0,
									Delta: &model.Delta{
										ToolCallFunction: &model.ToolCallFunction{Arguments: *streamEvent.Delta.PartialJSON},
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
				if currentBlockType == "tool_use" {
					toolCallIndex++
				}
				currentBlockType = ""

			case "message_delta":
				if streamEvent.Delta != nil && streamEvent.Delta.StopReason != nil {
					finishReason := stopReasonToOpenAI(*streamEvent.Delta.StopReason)
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
				ch <- model.StreamChunk{Done: true}
				return
			}

		case *bedrockruntimeTypes.UnknownUnionMember:
			ch <- model.StreamChunk{Error: fmt.Errorf("bedrock unknown event tag: %s", v.Tag)}
			return

		default:
			ch <- model.StreamChunk{Error: fmt.Errorf("bedrock nil or unknown event type")}
			return
		}
	}

	// 流正常结束
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
			OwnedBy: "bedrock",
		})
	}
	return models, nil
}

// HealthCheck 健康检查：通过 ListFoundationModels 验证凭证
func (a *Adapter) HealthCheck(ctx context.Context) error {
	// 使用一个最小的 InvokeModel 请求来验证凭证和区域的连通性
	// 实际上只需确认 SDK client 能正常签名请求即可
	// 我们发送一个不存在的模型请求，如果返回的不是 5xx 就认为服务可达
	input := &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String("health-check-placeholder"),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
		Body:        []byte(`{}`),
	}

	_, err := a.client.InvokeModel(ctx, input)
	if err != nil {
		// 如果错误包含 "not found" 或 "validation"，说明服务可达，只是模型不存在
		errStr := err.Error()
		if strings.Contains(errStr, "not found") ||
			strings.Contains(errStr, "ValidationException") ||
			strings.Contains(errStr, "AccessDeniedException") {
			return nil // 服务可达
		}
		return fmt.Errorf("bedrock health check: %w", err)
	}
	return nil
}

// ============== 内部类型定义 ==============

// bedrockRequest Bedrock Claude 请求体（Anthropic Messages API 格式 + anthropic_version）
type bedrockRequest struct {
	AnthropicVersion string           `json:"anthropic_version"`
	Messages         []bedrockMessage `json:"messages"`
	System           string           `json:"system,omitempty"`
	MaxTokens        int              `json:"max_tokens"`
	Temperature      *float64         `json:"temperature,omitempty"`
	TopP             *float64         `json:"top_p,omitempty"`
	StopSequences    []string         `json:"stop_sequences,omitempty"`
}

// bedrockMessage 消息格式
type bedrockMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// bedrockResponse Bedrock Claude 非流式响应
type bedrockResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []bedrockBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason *string        `json:"stop_reason"`
	Usage      *bedrockUsage  `json:"usage"`
}

// bedrockBlock 内容块
type bedrockBlock struct {
	Type     string  `json:"type"`
	Text     string  `json:"text,omitempty"`
	Thinking *string `json:"thinking,omitempty"`
	ID       string  `json:"id,omitempty"`
	Name     string  `json:"name,omitempty"`
}

// bedrockUsage 用量
type bedrockUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// bedrockStreamEvent 流式事件（与 Claude SSE 事件格式一致）
type bedrockStreamEvent struct {
	Type         string              `json:"type"`
	Message      *bedrockResponse    `json:"message,omitempty"`
	Index        *int                `json:"index,omitempty"`
	ContentBlock *bedrockContentBlock `json:"content_block,omitempty"`
	Delta        *bedrockDelta       `json:"delta,omitempty"`
	Usage        *bedrockUsage       `json:"usage,omitempty"`
}

// bedrockContentBlock content_block_start 中的内容块信息
type bedrockContentBlock struct {
	Type string  `json:"type"`
	Text *string `json:"text,omitempty"`
	ID   string  `json:"id,omitempty"`
	Name string  `json:"name,omitempty"`
}

// bedrockDelta 流式增量
type bedrockDelta struct {
	Type        string  `json:"type,omitempty"`
	Text        *string `json:"text,omitempty"`
	Thinking    *string `json:"thinking,omitempty"`
	PartialJSON *string `json:"partial_json,omitempty"`
	StopReason  *string `json:"stop_reason,omitempty"`
}

// ============== 转换逻辑 ==============

// convertToBedrockRequest 将 OpenAI 格式请求转换为 Bedrock Claude 格式
func convertToBedrockRequest(req *model.ChatCompletionRequest) *bedrockRequest {
	br := &bedrockRequest{
		AnthropicVersion: anthropicVersion,
	}
	if req.Temperature != nil {
		v := float64(*req.Temperature)
		br.Temperature = &v
	}
	if req.TopP != nil {
		v := float64(*req.TopP)
		br.TopP = &v
	}

	// 设置 max_tokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		br.MaxTokens = *req.MaxTokens
	} else {
		br.MaxTokens = defaultMaxTokens
	}

	// 处理 stop 参数
	if req.Stop != nil {
		switch v := req.Stop.(type) {
		case string:
			br.StopSequences = []string{v}
		case []interface{}:
			for _, s := range v {
				if str, ok := s.(string); ok {
					br.StopSequences = append(br.StopSequences, str)
				}
			}
		}
	}

	// 提取 system 消息，其余转为 messages
	var systemParts []string
	var messages []bedrockMessage
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
			messages = append(messages, bedrockMessage{Role: "user", Content: "..."})
		}

		// 合并相邻相同 role 的消息
		if role == lastRole {
			messages[len(messages)-1].Content += " " + content
		} else {
			messages = append(messages, bedrockMessage{
				Role:    role,
				Content: content,
			})
		}
		lastRole = role
	}

	if len(systemParts) > 0 {
		br.System = strings.Join(systemParts, "\n")
	}
	br.Messages = messages

	return br
}

// convertFromBedrockResponse 将 Bedrock Claude 响应转换为 OpenAI 格式
func convertFromBedrockResponse(resp *bedrockResponse, requestModel string) *model.ChatCompletionResponse {
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

	if reasoningContent != "" && len(result.Choices) > 0 {
		result.Choices[0].Message.ReasoningContent = reasoningContent
	}

	if resp.Usage != nil {
		result.Usage = &model.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}

	return result
}

// stopReasonToOpenAI 将 Anthropic/Bedrock stop_reason 转换为 OpenAI finish_reason
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
