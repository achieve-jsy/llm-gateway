package providers

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/example/llm-gateway/internal/core"
)

// ResponsesProvider is the minimal interface needed by ChatViaResponses.
type ResponsesProvider interface {
	Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error)
	StreamResponses(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error)
}

// ConvertChatRequestToResponses converts a Chat Completions request into a
// Responses API request (LiteLLM Chat→Responses bridge direction).
func ConvertChatRequestToResponses(req *core.ChatRequest) (*core.ResponsesRequest, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("chat request is required", nil)
	}

	instructions, input, err := ConvertMessagesToResponsesInput(req.Messages)
	if err != nil {
		return nil, err
	}

	respReq := &core.ResponsesRequest{
		Model:             req.Model,
		Provider:          req.Provider,
		Input:             input,
		Instructions:      instructions,
		Tools:             normalizeChatToolsForResponses(req.Tools),
		ToolChoice:        normalizeChatToolChoiceForResponses(req.ToolChoice),
		ParallelToolCalls: req.ParallelToolCalls,
		Stream:            req.Stream,
		// Chat 的 stream_options.include_usage 在 Azure/OpenAI Responses 上会
		// unknown_parameter；用量改从 response.completed 取回。
		StreamOptions: nil,
		Reasoning:     buildResponsesReasoning(req),
		ExtraFields:   stripChatOnlyExtraFields(req.ExtraFields),
	}

	if maxOut := resolveMaxOutputTokens(req); maxOut != nil {
		respReq.MaxOutputTokens = maxOut
	}

	return respReq, nil
}

// ConvertMessagesToResponsesInput maps Chat messages to Responses input items.
// Leading system/developer messages become instructions (joined by newlines).
func ConvertMessagesToResponsesInput(messages []core.Message) (instructions string, input any, err error) {
	var systemParts []string
	items := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		switch role {
		case "system", "developer":
			text := strings.TrimSpace(core.ExtractTextContent(msg.Content))
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			content, ok := convertChatContentToResponsesInput(msg.Content, "user")
			if !ok {
				return "", nil, core.NewInvalidRequestError("invalid chat message content for responses bridge", nil)
			}
			items = append(items, map[string]any{
				"type":    "message",
				"role":    "user",
				"content": content,
			})
		case "assistant":
			text := strings.TrimSpace(core.ExtractTextContent(msg.Content))
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					items = append(items, map[string]any{
						"type":      "function_call",
						"call_id":   ResponsesFunctionCallCallID(tc.ID),
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
					})
				}
			}
			if len(msg.ToolCalls) == 0 || text != "" {
				content, ok := convertChatContentToResponsesInput(msg.Content, "assistant")
				if !ok {
					return "", nil, core.NewInvalidRequestError("invalid assistant message content for responses bridge", nil)
				}
				items = append(items, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"content": content,
				})
			}
		case "tool":
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				return "", nil, core.NewInvalidRequestError("tool message requires tool_call_id for responses bridge", nil)
			}
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  core.ExtractTextContent(msg.Content),
			})
		default:
			return "", nil, core.NewInvalidRequestError("unsupported chat message role for responses bridge: "+role, nil)
		}
	}

	instructions = strings.Join(systemParts, "\n")
	if len(items) == 0 && instructions != "" {
		// Responses API rejects empty input; promote system-only to a system input item.
		items = append(items, map[string]any{
			"type": "message",
			"role": "system",
			"content": []map[string]any{
				{"type": "input_text", "text": instructions},
			},
		})
		instructions = ""
	}
	if len(items) == 0 {
		return "", nil, core.NewInvalidRequestError("responses bridge requires at least one non-system message", nil)
	}
	return instructions, items, nil
}

func convertChatContentToResponsesInput(content any, role string) (any, bool) {
	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}

	switch c := content.(type) {
	case nil:
		return []map[string]any{{"type": textType, "text": ""}}, true
	case string:
		return []map[string]any{{"type": textType, "text": c}}, true
	case []core.ContentPart:
		items := make([]map[string]any, 0, len(c))
		for _, part := range c {
			switch part.Type {
			case "text":
				items = append(items, map[string]any{"type": textType, "text": part.Text})
			case "image_url":
				if part.ImageURL == nil {
					continue
				}
				items = append(items, map[string]any{
					"type":      "input_image",
					"image_url": part.ImageURL.URL,
				})
			}
		}
		if len(items) == 0 {
			items = append(items, map[string]any{"type": textType, "text": core.ExtractTextContent(content)})
		}
		return items, true
	case []any:
		normalized, ok := core.NormalizeContentParts(c)
		if !ok {
			return []map[string]any{{"type": textType, "text": core.ExtractTextContent(content)}}, true
		}
		return convertChatContentToResponsesInput(normalized, role)
	default:
		return []map[string]any{{"type": textType, "text": core.ExtractTextContent(content)}}, true
	}
}

func normalizeChatToolsForResponses(tools []map[string]any) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, normalizeChatToolForResponses(tool))
	}
	return out
}

func normalizeChatToolForResponses(tool map[string]any) map[string]any {
	if len(tool) == 0 {
		return tool
	}
	toolType, _ := tool["type"].(string)
	if strings.TrimSpace(toolType) != "function" {
		return cloneStringAnyMap(tool)
	}
	fn, ok := tool["function"].(map[string]any)
	if !ok {
		return cloneStringAnyMap(tool)
	}
	normalized := map[string]any{"type": "function"}
	for _, key := range []string{"name", "description", "parameters", "strict"} {
		if value, ok := fn[key]; ok {
			normalized[key] = value
		}
	}
	return normalized
}

func normalizeChatToolChoiceForResponses(choice any) any {
	switch c := choice.(type) {
	case string:
		return c
	case map[string]any:
		choiceType, _ := c["type"].(string)
		if strings.TrimSpace(choiceType) != "function" {
			return cloneStringAnyMap(c)
		}
		if _, ok := c["name"]; ok {
			return cloneStringAnyMap(c)
		}
		if fn, ok := c["function"].(map[string]any); ok {
			normalized := cloneStringAnyMap(c)
			delete(normalized, "function")
			if name, ok := fn["name"]; ok {
				normalized["name"] = name
			}
			return normalized
		}
		return cloneStringAnyMap(c)
	default:
		return choice
	}
}

func buildResponsesReasoning(req *core.ChatRequest) *core.Reasoning {
	effort, summary := extractChatReasoning(req)
	if effort == "" && summary == "" {
		return nil
	}
	return &core.Reasoning{Effort: effort, Summary: summary}
}

func extractChatReasoning(req *core.ChatRequest) (effort, summary string) {
	if req.Reasoning != nil {
		effort = strings.TrimSpace(req.Reasoning.Effort)
		summary = strings.TrimSpace(req.Reasoning.Summary)
	}

	if raw := req.ExtraFields.Lookup("reasoning_effort"); len(raw) > 0 {
		e, s := parseReasoningEffortValue(raw)
		if e != "" {
			effort = e
		}
		if s != "" && summary == "" {
			summary = s
		}
	} else if raw := req.ExtraFields.Lookup("reasoningEffort"); len(raw) > 0 {
		e, s := parseReasoningEffortValue(raw)
		if e != "" && effort == "" {
			effort = e
		}
		if s != "" && summary == "" {
			summary = s
		}
	}

	if summary == "" {
		for _, key := range []string{"reasoningSummary", "reasoning_summary"} {
			if raw := req.ExtraFields.Lookup(key); len(raw) > 0 {
				var s string
				if json.Unmarshal(raw, &s) == nil {
					summary = strings.TrimSpace(s)
					if summary != "" {
						break
					}
				}
			}
		}
	}
	return effort, summary
}

func parseReasoningEffortValue(raw json.RawMessage) (effort, summary string) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", ""
	}
	if trimmed[0] == '"' {
		_ = json.Unmarshal(raw, &effort)
		return strings.TrimSpace(effort), ""
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return "", ""
	}
	if v, ok := obj["effort"].(string); ok {
		effort = strings.TrimSpace(v)
	}
	if v, ok := obj["summary"].(string); ok {
		summary = strings.TrimSpace(v)
	}
	return effort, summary
}

func resolveMaxOutputTokens(req *core.ChatRequest) *int {
	if raw := req.ExtraFields.Lookup("max_completion_tokens"); len(raw) > 0 {
		var n int
		if json.Unmarshal(raw, &n) == nil {
			return &n
		}
	}
	if req.MaxTokens != nil {
		return req.MaxTokens
	}
	return nil
}

func stripChatOnlyExtraFields(fields core.UnknownJSONFields) core.UnknownJSONFields {
	if fields.IsEmpty() {
		return core.UnknownJSONFields{}
	}
	// Round-trip through ResponsesRequest marshaling to recover the raw extra object.
	body, err := json.Marshal(&core.ResponsesRequest{ExtraFields: fields})
	if err != nil {
		return core.CloneUnknownJSONFields(fields)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return core.CloneUnknownJSONFields(fields)
	}
	known := map[string]struct{}{
		"model": {}, "provider": {}, "input": {}, "instructions": {},
		"tools": {}, "tool_choice": {}, "parallel_tool_calls": {},
		"temperature": {}, "max_output_tokens": {}, "stream": {},
		"stream_options": {}, "metadata": {}, "reasoning": {},
	}
	extra := make(map[string]json.RawMessage)
	for k, v := range obj {
		if _, ok := known[k]; ok {
			continue
		}
		extra[k] = v
	}
	for _, key := range []string{
		"reasoning_effort", "reasoningEffort",
		"reasoningSummary", "reasoning_summary",
		"max_tokens", "max_completion_tokens",
		"temperature",
	} {
		delete(extra, key)
	}
	return core.UnknownJSONFieldsFromMap(extra)
}

// ConvertResponsesResponseToChat converts a Responses API response back to Chat Completions shape.
func ConvertResponsesResponseToChat(resp *core.ResponsesResponse) *core.ChatResponse {
	if resp == nil {
		return &core.ChatResponse{Object: "chat.completion", Choices: []core.Choice{}}
	}

	msg := core.ResponseMessage{Role: "assistant"}
	var textParts []string
	var reasoningParts []string
	var toolCalls []core.ToolCall
	finishReason := "stop"

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" || c.Type == "text" {
					textParts = append(textParts, c.Text)
				}
			}
		case "reasoning":
			reasoningParts = append(reasoningParts, extractReasoningSummaryText(item)...)
		case "function_call":
			finishReason = "tool_calls"
			toolCalls = append(toolCalls, core.ToolCall{
				ID:   ResponsesFunctionCallCallID(item.CallID),
				Type: "function",
				Function: core.FunctionCall{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}

	content := strings.Join(textParts, "")
	if content == "" && len(toolCalls) > 0 {
		msg.Content = nil
	} else {
		msg.Content = content
	}
	msg.ToolCalls = toolCalls
	if reasoning := strings.Join(reasoningParts, "\n"); reasoning != "" {
		if raw, err := json.Marshal(reasoning); err == nil {
			msg.ExtraFields = core.UnknownJSONFieldsFromMap(map[string]json.RawMessage{
				"reasoning_content": raw,
			})
		}
	}

	usage := core.Usage{}
	if resp.Usage != nil {
		usage = core.Usage{
			PromptTokens:            resp.Usage.InputTokens,
			CompletionTokens:        resp.Usage.OutputTokens,
			TotalTokens:             resp.Usage.TotalTokens,
			PromptTokensDetails:     resp.Usage.PromptTokensDetails,
			CompletionTokensDetails: resp.Usage.CompletionTokensDetails,
			RawUsage:                resp.Usage.RawUsage,
		}
	}

	id := resp.ID
	if id != "" && !strings.HasPrefix(id, "chatcmpl-") {
		id = "chatcmpl-" + strings.TrimPrefix(id, "resp_")
	}

	return &core.ChatResponse{
		ID:       id,
		Object:   "chat.completion",
		Created:  resp.CreatedAt,
		Model:    resp.Model,
		Provider: resp.Provider,
		Choices: []core.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: usage,
	}
}

func extractReasoningSummaryText(item core.ResponsesOutputItem) []string {
	parts := make([]string, 0, len(item.Summary)+len(item.Content))
	for _, c := range item.Summary {
		if (c.Type == "summary_text" || c.Type == "output_text" || c.Type == "text") && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	for _, c := range item.Content {
		if (c.Type == "summary_text" || c.Type == "output_text" || c.Type == "text") && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return parts
}

// ChatViaResponses implements Chat Completions by bridging to the Responses API.
func ChatViaResponses(ctx context.Context, p ResponsesProvider, req *core.ChatRequest) (*core.ChatResponse, error) {
	respReq, err := ConvertChatRequestToResponses(req)
	if err != nil {
		return nil, err
	}
	respReq.Stream = false

	resp, err := p.Responses(ctx, respReq)
	if err != nil {
		return nil, err
	}
	chatResp := ConvertResponsesResponseToChat(resp)
	if chatResp.Model == "" && req != nil {
		chatResp.Model = req.Model
	}
	return chatResp, nil
}

// StreamChatViaResponses implements streaming Chat Completions via Responses API.
func StreamChatViaResponses(ctx context.Context, p ResponsesProvider, req *core.ChatRequest, providerName string) (io.ReadCloser, error) {
	respReq, err := ConvertChatRequestToResponses(req)
	if err != nil {
		return nil, err
	}
	respReq.Stream = true

	stream, err := p.StreamResponses(ctx, respReq)
	if err != nil {
		return nil, err
	}

	model := ""
	if req != nil {
		model = req.Model
	}
	includeUsage := req != nil && req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	_ = providerName
	return NewResponsesToChatStreamConverter(stream, model, includeUsage), nil
}
