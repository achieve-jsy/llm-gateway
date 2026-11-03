package protocol

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/example/llm-gateway/internal/core"
)

// BridgeToChatRequest 将 relaykit 的 RequestResult 映射为内部 ChatRequest。
//
// result.Value 期望为 *dto.GeneralOpenAIRequest（relaykit 的标准 OpenAI Chat 格式）。
// Bridge 将其中所有字段映射到 core.ChatRequest，未映射字段落入 ExtraFields。
// 同时将 session.Values.UpstreamModelName 设置为目标模型名。
func BridgeToChatRequest(session *Session, result *relayconvert.RequestResult) (*core.ChatRequest, error) {
	if result == nil {
		return nil, fmt.Errorf("request result is nil")
	}

	// relaykit ConvertRequest 返回的是转换后的 DTO
	oaiReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		// 也尝试值类型
		if val, ok2 := result.Value.(dto.GeneralOpenAIRequest); ok2 {
			oaiReq = &val
		} else {
			return nil, fmt.Errorf("unsupported request result type: %T", result.Value)
		}
	}

	chatReq := &core.ChatRequest{
		Model: oaiReq.Model,
	}

	// Temperature: relaykit 使用 *float64，直接传递
	if oaiReq.Temperature != nil {
		chatReq.Temperature = oaiReq.Temperature
	}

	// MaxTokens: relaykit 使用 *uint，core 使用 *int
	if oaiReq.MaxTokens != nil {
		mt := int(*oaiReq.MaxTokens)
		chatReq.MaxTokens = &mt
	}

	// Stream
	if oaiReq.Stream != nil {
		chatReq.Stream = *oaiReq.Stream
	}

	// StreamOptions
	if oaiReq.StreamOptions != nil {
		chatReq.StreamOptions = &core.StreamOptions{
			IncludeUsage: oaiReq.StreamOptions.IncludeUsage,
		}
	}

	// Reasoning: 从 relaykit 的 ReasoningEffort 字段构建。
	//
	// 同时必须搬运 ReasoningConversion.BudgetTokens —— relaykit 的契约明确要求
	// 「exact budgets must survive multi-step routes」。只留 Effort 会让下游
	// 用粗粒度档位反推预算（1024 → low → 5000），把 max_tokens 一起放大。
	if oaiReq.ReasoningEffort != "" || oaiReq.ReasoningConversion != nil {
		r := &core.Reasoning{Effort: oaiReq.ReasoningEffort}
		if oaiReq.ReasoningConversion != nil {
			r.BudgetTokens = oaiReq.ReasoningConversion.BudgetTokens
			if r.Effort == "" {
				r.Effort = oaiReq.ReasoningConversion.Effort
			}
		}
		if r.Effort != "" || r.BudgetTokens != nil {
			chatReq.Reasoning = r
		}
	}

	// Messages 转换
	chatReq.Messages = convertMessages(oaiReq.Messages)

	// Tools 转换：dto.ToolCallRequest → map[string]any
	chatReq.Tools = convertTools(oaiReq.Tools)

	// ToolChoice 透传
	chatReq.ToolChoice = oaiReq.ToolChoice

	// ParallelToolCalls
	if oaiReq.ParallelTooCalls != nil {
		chatReq.ParallelToolCalls = oaiReq.ParallelTooCalls
	}

	// 设置上游模型名（调度后的实际模型）
	if session != nil && chatReq.Model != "" {
		session.Values.UpstreamModelName = chatReq.Model
	}

	// ExtraFields：收集未显式映射的字段
	extra := make(map[string]json.RawMessage)
	addExtra := func(key string, raw json.RawMessage) {
		if len(raw) > 0 && string(raw) != "null" {
			if _, exists := extra[key]; !exists {
				extra[key] = raw
			}
		}
	}

	// TopP 不在 core.ChatRequest 的顶层字段中，放入 ExtraFields
	if oaiReq.TopP != nil {
		if b, err := json.Marshal(oaiReq.TopP); err == nil {
			addExtra("top_p", b)
		}
	}

	// TopK
	if oaiReq.TopK != nil {
		if b, err := json.Marshal(oaiReq.TopK); err == nil {
			addExtra("top_k", b)
		}
	}

	// Stop
	if oaiReq.Stop != nil {
		if b, err := json.Marshal(oaiReq.Stop); err == nil {
			addExtra("stop", b)
		}
	}

	// FrequencyPenalty
	if oaiReq.FrequencyPenalty != nil {
		if b, err := json.Marshal(oaiReq.FrequencyPenalty); err == nil {
			addExtra("frequency_penalty", b)
		}
	}

	// PresencePenalty
	if oaiReq.PresencePenalty != nil {
		if b, err := json.Marshal(oaiReq.PresencePenalty); err == nil {
			addExtra("presence_penalty", b)
		}
	}

	// Seed
	if oaiReq.Seed != nil {
		if b, err := json.Marshal(oaiReq.Seed); err == nil {
			addExtra("seed", b)
		}
	}

	// N
	if oaiReq.N != nil {
		if b, err := json.Marshal(oaiReq.N); err == nil {
			addExtra("n", b)
		}
	}

	// ResponseFormat
	if oaiReq.ResponseFormat != nil {
		if b, err := json.Marshal(oaiReq.ResponseFormat); err == nil {
			addExtra("response_format", b)
		}
	}

	// User
	if len(oaiReq.User) > 0 && string(oaiReq.User) != "null" {
		addExtra("user", oaiReq.User)
	}

	if len(extra) > 0 {
		chatReq.ExtraFields = core.UnknownJSONFieldsFromMap(extra)
	}

	return chatReq, nil
}

// BridgeChatResponseToSource 将内部 ChatResponse 通过 relaykit 转换为源格式。
//
// 先构建 relaykit 的 OpenAITextResponse DTO，再调用 relaykit 的 ConvertResponse
// 将 OpenAI Chat 响应转为 session.SourceFormat。
func BridgeChatResponseToSource(session *Session, chatResp *core.ChatResponse) (*relayconvert.ResponseResult, error) {
	if session == nil {
		return nil, fmt.Errorf("session is required")
	}
	if chatResp == nil {
		return nil, fmt.Errorf("chat response is nil")
	}

	// 构建 relaykit 的 OpenAITextResponse
	oaiResp := &dto.OpenAITextResponse{
		Id:      chatResp.ID,
		Model:   chatResp.Model,
		Object:  chatResp.Object,
		Created: chatResp.Created,
		Usage: dto.Usage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
		},
	}

	// 填充 Choices
	for _, choice := range chatResp.Choices {
		oaiChoice := dto.OpenAITextResponseChoice{
			Index:        choice.Index,
			FinishReason: choice.FinishReason,
			Message: dto.Message{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
			},
		}

		// ToolCalls
		if len(choice.Message.ToolCalls) > 0 {
			toolCallsJSON, err := json.Marshal(choice.Message.ToolCalls)
			if err == nil {
				oaiChoice.Message.ToolCalls = toolCallsJSON
			}
		}

		oaiResp.Choices = append(oaiResp.Choices, oaiChoice)
	}

	// 调用 relaykit 的 ConvertResponse 转回源格式
	return relayconvert.ConvertResponse(
		context.Background(),
		&session.Values,
		session.SourceFormat,
		oaiResp,
	)
}

// hasUnsupportedBlocks 检查 Claude 请求中是否包含桥不支持的内容块类型。
// 返回诊断信息列表，空列表表示全部支持。
func HasUnsupportedBlocks(req *dto.ClaudeRequest) []types.ConversionDiagnostic {
	if req == nil {
		return nil
	}

	var diags []types.ConversionDiagnostic

	for mi, msg := range req.Messages {
		blocks, err := msg.ParseContent()
		if err != nil {
			continue
		}
		for bi, block := range blocks {
			switch block.Type {
			case "server_tool_use", "web_search_tool_use":
				diags = append(diags, types.ConversionDiagnostic{
					Code:     "unsupported_content_block",
					Path:     fmt.Sprintf("messages[%d].content[%d]", mi, bi),
					Message:  fmt.Sprintf("content block type %q is not supported by the bridge", block.Type),
					Severity: types.ConversionDiagnosticError,
					From:     types.RelayFormatClaude,
					To:       types.RelayFormatOpenAI,
				})
			}
		}
	}

	return diags
}

// convertMessages 将 relaykit 的 []dto.Message 转换为 core 的 []Message。
func convertMessages(msgs []dto.Message) []core.Message {
	if len(msgs) == 0 {
		return nil
	}

	result := make([]core.Message, 0, len(msgs))
	for _, m := range msgs {
		cm := core.Message{
			Role:       m.Role,
			ToolCallID: m.ToolCallId,
		}

		// Content: string 透传，[]MediaContent 转为 []ContentPart
		cm.Content = convertMessageContent(m.Content)

		// ToolCalls: 从 json.RawMessage 解析
		if len(m.ToolCalls) > 0 && string(m.ToolCalls) != "null" {
			var tcs []core.ToolCall
			if err := json.Unmarshal(m.ToolCalls, &tcs); err == nil {
				cm.ToolCalls = tcs
			}
		}

		result = append(result, cm)
	}

	return result
}

// convertMessageContent 将 relaykit 的消息内容（string 或 []MediaContent）
// 转换为 core 的 MessageContent（string 或 []ContentPart）。
func convertMessageContent(content any) any {
	if content == nil {
		return ""
	}

	switch c := content.(type) {
	case string:
		return c
	case []dto.MediaContent:
		parts := make([]core.ContentPart, 0, len(c))
		for _, mc := range c {
			cp := core.ContentPart{
				Type: mc.Type,
				Text: mc.Text,
			}

			// ImageUrl
			if mc.ImageUrl != nil {
				cp.ImageURL = convertImageUrl(mc.ImageUrl)
			}

			// InputAudio
			if mc.InputAudio != nil {
				cp.InputAudio = convertInputAudio(mc.InputAudio)
			}

			// CacheControl → ExtraFields
			if len(mc.CacheControl) > 0 && string(mc.CacheControl) != "null" {
				cp.ExtraFields = core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
					"cache_control": mc.CacheControl,
				})
			}

			// VideoUrl 暂不支持，跳过
			if mc.VideoUrl != nil {
				continue
			}

			parts = append(parts, cp)
		}
		if len(parts) == 0 {
			return ""
		}
		if len(parts) == 1 && parts[0].Type == "text" && parts[0].ExtraFields.IsEmpty() {
			return parts[0].Text
		}
		return parts
	case []any:
		// JSON 解码后可能是 []any，重新序列化后解码
		raw, err := json.Marshal(c)
		if err != nil {
			return fmt.Sprintf("%v", c)
		}
		var mcs []dto.MediaContent
		if err := json.Unmarshal(raw, &mcs); err != nil {
			return fmt.Sprintf("%v", c)
		}
		return convertMessageContent(mcs)
	default:
		return fmt.Sprintf("%v", c)
	}
}

// convertImageUrl 将 relaykit 的 image_url（any）转换为 *core.ImageURLContent。
func convertImageUrl(imageURL any) *core.ImageURLContent {
	if imageURL == nil {
		return nil
	}

	switch img := imageURL.(type) {
	case *dto.MessageImageUrl:
		return &core.ImageURLContent{
			URL:       img.Url,
			Detail:    img.Detail,
			MediaType: img.MimeType,
		}
	case dto.MessageImageUrl:
		return &core.ImageURLContent{
			URL:       img.Url,
			Detail:    img.Detail,
			MediaType: img.MimeType,
		}
	case map[string]any:
		url, _ := img["url"].(string)
		detail, _ := img["detail"].(string)
		mediaType, _ := img["media_type"].(string)
		if url == "" {
			return nil
		}
		return &core.ImageURLContent{
			URL:       url,
			Detail:    detail,
			MediaType: mediaType,
		}
	default:
		// 尝试 JSON 往返
		raw, err := json.Marshal(imageURL)
		if err != nil {
			return nil
		}
		var miu dto.MessageImageUrl
		if err := json.Unmarshal(raw, &miu); err != nil {
			return nil
		}
		if miu.Url == "" {
			return nil
		}
		return &core.ImageURLContent{
			URL:       miu.Url,
			Detail:    miu.Detail,
			MediaType: miu.MimeType,
		}
	}
}

// convertInputAudio 将 relaykit 的 input_audio（any）转换为 *core.InputAudioContent。
func convertInputAudio(inputAudio any) *core.InputAudioContent {
	if inputAudio == nil {
		return nil
	}

	switch audio := inputAudio.(type) {
	case *dto.MessageInputAudio:
		return &core.InputAudioContent{
			Data:   audio.Data,
			Format: audio.Format,
		}
	case dto.MessageInputAudio:
		return &core.InputAudioContent{
			Data:   audio.Data,
			Format: audio.Format,
		}
	case map[string]any:
		data, _ := audio["data"].(string)
		format, _ := audio["format"].(string)
		if data == "" || format == "" {
			return nil
		}
		return &core.InputAudioContent{
			Data:   data,
			Format: format,
		}
	default:
		raw, err := json.Marshal(inputAudio)
		if err != nil {
			return nil
		}
		var mia dto.MessageInputAudio
		if err := json.Unmarshal(raw, &mia); err != nil {
			return nil
		}
		if mia.Data == "" || mia.Format == "" {
			return nil
		}
		return &core.InputAudioContent{
			Data:   mia.Data,
			Format: mia.Format,
		}
	}
}

// convertTools 将 relaykit 的 []dto.ToolCallRequest 转换为 []map[string]any。
func convertTools(tools []dto.ToolCallRequest) []map[string]any {
	if len(tools) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		raw, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		result = append(result, m)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
