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
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/provider"
)

const (
	defaultRegion    = "us-east-1"
	defaultMaxTokens = 4096
)

// Config AWS Bedrock 适配器配置
type Config struct {
	AccessKeyID    string   // AWS Access Key ID
	SecretAccessKey string   // AWS Secret Access Key
	Region         string   // AWS Region，默认 us-east-1
	Models         []string // 支持的模型列表
}

// Adapter AWS Bedrock Provider 适配器
// 使用 Converse/ConverseStream API，支持所有 Bedrock 模型
type Adapter struct {
	config Config
	client *bedrockruntime.Client
}

// New 创建 Bedrock 适配器实例
func New(cfg Config) provider.Provider {
	if cfg.Region == "" {
		cfg.Region = defaultRegion
	}

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

// Chat 普通调用（非流式），使用 Converse API
func (a *Adapter) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	messages, system := convertMessages(req.Messages)
	infCfg := buildInferenceConfig(req)
	toolCfg := buildToolConfig(req)

	input := &bedrockruntime.ConverseInput{
		ModelId:         aws.String(req.Model),
		Messages:        messages,
		InferenceConfig: infCfg,
	}
	if len(system) > 0 {
		input.System = system
	}
	if toolCfg != nil {
		input.ToolConfig = toolCfg
	}

	output, err := a.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock Converse: %w", err)
	}

	return convertConverseOutput(output, req.Model), nil
}

// ChatStream 流式调用，使用 ConverseStream API
func (a *Adapter) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	messages, system := convertMessages(req.Messages)
	infCfg := buildInferenceConfig(req)
	toolCfg := buildToolConfig(req)

	input := &bedrockruntime.ConverseStreamInput{
		ModelId:         aws.String(req.Model),
		Messages:        messages,
		InferenceConfig: infCfg,
	}
	if len(system) > 0 {
		input.System = system
	}
	if toolCfg != nil {
		input.ToolConfig = toolCfg
	}

	output, err := a.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock ConverseStream: %w", err)
	}

	ch := make(chan model.StreamChunk, 32)
	go func() {
		defer close(ch)
		stream := output.GetStream()
		defer stream.Close()
		a.processConverseStream(stream, req.Model, ch)
	}()

	return ch, nil
}

// processConverseStream 处理 ConverseStream 事件流
func (a *Adapter) processConverseStream(stream *bedrockruntime.ConverseStreamEventStream, requestModel string, ch chan<- model.StreamChunk) {
	createdTime := time.Now().Unix()
	msgID := fmt.Sprintf("chatcmpl-%d", createdTime)

	var toolCallIndex int
	var currentToolID string
	var currentToolName string
	var inToolUse bool

	for event := range stream.Events() {
		switch v := event.(type) {
		case *brtypes.ConverseStreamOutputMemberMessageStart:
			// 发送 role 初始化
			role := string(v.Value.Role)
			chunk := &model.ChatCompletionChunk{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: createdTime,
				Model:   requestModel,
				Choices: []model.Choice{
					{
						Index: 0,
						Delta: &model.Delta{Role: role},
					},
				},
			}
			ch <- model.StreamChunk{Data: chunk}

		case *brtypes.ConverseStreamOutputMemberContentBlockStart:
			start := v.Value.Start
			if start == nil {
				continue
			}
			switch s := start.(type) {
			case *brtypes.ContentBlockStartMemberToolUse:
				inToolUse = true
				currentToolID = aws.ToString(s.Value.ToolUseId)
				currentToolName = aws.ToString(s.Value.Name)
				idx := toolCallIndex
				chunk := &model.ChatCompletionChunk{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: createdTime,
					Model:   requestModel,
					Choices: []model.Choice{
						{
							Index: 0,
							Delta: &model.Delta{
								ToolCallID:       currentToolID,
								ToolCallFunction: &model.ToolCallFunction{Name: currentToolName, Arguments: ""},
								ToolCallIndex:    &idx,
							},
						},
					},
				}
				ch <- model.StreamChunk{Data: chunk}
			default:
				inToolUse = false
			}

		case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
			delta := v.Value.Delta
			if delta == nil {
				continue
			}
			switch d := delta.(type) {
			case *brtypes.ContentBlockDeltaMemberText:
				chunk := &model.ChatCompletionChunk{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: createdTime,
					Model:   requestModel,
					Choices: []model.Choice{
						{
							Index: 0,
							Delta: &model.Delta{Content: d.Value},
						},
					},
				}
				ch <- model.StreamChunk{Data: chunk}
			case *brtypes.ContentBlockDeltaMemberToolUse:
				idx := toolCallIndex
				chunk := &model.ChatCompletionChunk{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: createdTime,
					Model:   requestModel,
					Choices: []model.Choice{
						{
							Index: 0,
							Delta: &model.Delta{
								ToolCallFunction: &model.ToolCallFunction{Arguments: aws.ToString(d.Value.Input)},
								ToolCallIndex:    &idx,
							},
						},
					},
				}
				ch <- model.StreamChunk{Data: chunk}
			}

		case *brtypes.ConverseStreamOutputMemberContentBlockStop:
			if inToolUse {
				toolCallIndex++
				inToolUse = false
			}

		case *brtypes.ConverseStreamOutputMemberMessageStop:
			finishReason := converseStopReasonToOpenAI(v.Value.StopReason)
			chunk := &model.ChatCompletionChunk{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: createdTime,
				Model:   requestModel,
				Choices: []model.Choice{
					{
						Index:        0,
						Delta:        &model.Delta{},
						FinishReason: &finishReason,
					},
				},
			}
			ch <- model.StreamChunk{Data: chunk}
			ch <- model.StreamChunk{Done: true}
			return

		case *brtypes.ConverseStreamOutputMemberMetadata:
			// usage 信息，忽略（SSE 中不发 usage）
			continue

		default:
			// 未知事件类型，忽略
			continue
		}
	}

	// 流正常结束（如果没收到 MessageStop）
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

// HealthCheck 健康检查：通过 Converse 请求验证凭证连通性
func (a *Adapter) HealthCheck(ctx context.Context) error {
	input := &bedrockruntime.ConverseInput{
		ModelId: aws.String("health-check-placeholder"),
		Messages: []brtypes.Message{
			{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: "hi"},
				},
			},
		},
	}

	_, err := a.client.Converse(ctx, input)
	if err != nil {
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

// ============== 转换逻辑 ==============

// convertMessages 将 OpenAI 格式消息转换为 Bedrock Converse 格式
// 返回 messages 和 system content blocks
func convertMessages(msgs []model.Message) ([]brtypes.Message, []brtypes.SystemContentBlock) {
	var system []brtypes.SystemContentBlock
	var messages []brtypes.Message
	var lastRole brtypes.ConversationRole

	for _, msg := range msgs {
		if msg.Role == "system" {
			system = append(system, &brtypes.SystemContentBlockMemberText{Value: msg.Content})
			continue
		}

		role := mapRole(msg.Role)
		content := []brtypes.ContentBlock{
			&brtypes.ContentBlockMemberText{Value: msg.Content},
		}

		// 确保第一条非 system 消息是 user
		if len(messages) == 0 && role != brtypes.ConversationRoleUser {
			messages = append(messages, brtypes.Message{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: "..."},
				},
			})
			lastRole = brtypes.ConversationRoleUser
		}

		// 合并相邻相同 role 的消息（Bedrock 要求严格交替）
		if role == lastRole && len(messages) > 0 {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, content...)
		} else {
			messages = append(messages, brtypes.Message{
				Role:    role,
				Content: content,
			})
		}
		lastRole = role
	}

	return messages, system
}

// mapRole 将 OpenAI role 映射为 Bedrock ConversationRole
func mapRole(role string) brtypes.ConversationRole {
	switch role {
	case "assistant":
		return brtypes.ConversationRoleAssistant
	default:
		return brtypes.ConversationRoleUser
	}
}

// buildInferenceConfig 构造推理配置
func buildInferenceConfig(req *model.ChatCompletionRequest) *brtypes.InferenceConfiguration {
	cfg := &brtypes.InferenceConfiguration{}
	hasField := false

	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		v := int32(*req.MaxTokens)
		cfg.MaxTokens = &v
		hasField = true
	} else {
		v := int32(defaultMaxTokens)
		cfg.MaxTokens = &v
		hasField = true
	}

	if req.Temperature != nil {
		v := float32(*req.Temperature)
		cfg.Temperature = &v
		hasField = true
	}

	if req.TopP != nil {
		v := float32(*req.TopP)
		cfg.TopP = &v
		hasField = true
	}

	// 处理 stop 参数
	if req.Stop != nil {
		switch s := req.Stop.(type) {
		case string:
			cfg.StopSequences = []string{s}
			hasField = true
		case []interface{}:
			for _, item := range s {
				if str, ok := item.(string); ok {
					cfg.StopSequences = append(cfg.StopSequences, str)
				}
			}
			if len(cfg.StopSequences) > 0 {
				hasField = true
			}
		}
	}

	if !hasField {
		return nil
	}
	return cfg
}

// buildToolConfig 构造工具配置（从 ChatCompletionRequest 中暂无 tools 字段，预留扩展）
func buildToolConfig(_ *model.ChatCompletionRequest) *brtypes.ToolConfiguration {
	// 当前 model.ChatCompletionRequest 未定义 tools 字段
	// 预留扩展接口，当请求中无 tools 时返回 nil
	return nil
}

// convertConverseOutput 将 Converse 响应转换为 OpenAI 格式
func convertConverseOutput(output *bedrockruntime.ConverseOutput, requestModel string) *model.ChatCompletionResponse {
	var content string
	var toolCalls []toolCallInfo

	if output.Output != nil {
		switch o := output.Output.(type) {
		case *brtypes.ConverseOutputMemberMessage:
			for _, block := range o.Value.Content {
				switch b := block.(type) {
				case *brtypes.ContentBlockMemberText:
					content += b.Value
				case *brtypes.ContentBlockMemberToolUse:
					inputJSON, _ := json.Marshal(b.Value.Input)
					toolCalls = append(toolCalls, toolCallInfo{
						ID:        aws.ToString(b.Value.ToolUseId),
						Name:      aws.ToString(b.Value.Name),
						Arguments: string(inputJSON),
					})
				}
			}
		}
	}

	finishReason := converseStopReasonToOpenAI(output.StopReason)

	result := &model.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
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

	// tool calls 信息附加到 content（OpenAI 格式中通过 Delta 传递，非流式时放在 message 中）
	// 当前 model.Message 不支持 tool_calls 字段，将 tool call 信息序列化到 content
	if len(toolCalls) > 0 && content == "" {
		// 如果只有 tool_calls 没有文本，序列化为 JSON
		tcJSON, _ := json.Marshal(toolCalls)
		result.Choices[0].Message.Content = string(tcJSON)
	}

	if output.Usage != nil {
		result.Usage = &model.Usage{
			PromptTokens:     int(aws.ToInt32(output.Usage.InputTokens)),
			CompletionTokens: int(aws.ToInt32(output.Usage.OutputTokens)),
			TotalTokens:      int(aws.ToInt32(output.Usage.InputTokens)) + int(aws.ToInt32(output.Usage.OutputTokens)),
		}
	}

	return result
}

// toolCallInfo 工具调用信息
type toolCallInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// converseStopReasonToOpenAI 将 Bedrock Converse StopReason 转换为 OpenAI finish_reason
func converseStopReasonToOpenAI(reason brtypes.StopReason) string {
	switch reason {
	case brtypes.StopReasonEndTurn:
		return "stop"
	case brtypes.StopReasonStopSequence:
		return "stop"
	case brtypes.StopReasonMaxTokens:
		return "length"
	case brtypes.StopReasonToolUse:
		return "tool_calls"
	default:
		return string(reason)
	}
}
