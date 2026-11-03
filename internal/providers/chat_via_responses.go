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
//
// 参数映射对齐 LiteLLM
// litellm/completion_extras/litellm_responses_transformation/transformation.py
// 的 _map_optional_params_to_responses_api_request：白名单透传 + 显式 remap，
// 不把 Chat-only 顶层字段原样丢给 /responses。
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
		Temperature:       req.Temperature,
		Stream:            req.Stream,
		// Chat 的 stream_options.include_usage 在 Azure/OpenAI Responses 上会
		// unknown_parameter；用量改从 response.completed 取回。
		// LiteLLM 仅透传 include_obfuscation，见 applyLiteLLMChatOptionalParams。
		StreamOptions: nil,
		Metadata:      nil,
		Reasoning:     buildResponsesReasoning(req),
	}

	if maxOut := resolveMaxOutputTokens(req); maxOut != nil {
		respReq.MaxOutputTokens = maxOut
	}

	applyLiteLLMChatOptionalParams(req, respReq)
	SanitizeResponsesRequest(respReq)

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

// responsesAPIOptionalParamKeys 对齐 LiteLLM ResponsesAPIOptionalRequestParams
// 中可透传到 /responses 的字段（已由 typed 字段承接的除外）。
var responsesAPIOptionalParamKeys = map[string]struct{}{
	"include":                {},
	"store":                  {},
	"background":             {},
	"top_p":                  {},
	"truncation":             {},
	"user":                   {},
	"service_tier":           {},
	"safety_identifier":      {},
	"prompt":                 {},
	"max_tool_calls":         {},
	"prompt_cache_key":       {},
	"prompt_cache_retention": {},
	"top_logprobs":           {},
	"partial_images":         {},
	"context_management":     {},
	"previous_response_id":   {},
	"text":                   {},
}

// applyLiteLLMChatOptionalParams 对齐 LiteLLM
// _map_optional_params_to_responses_api_request：只映射已知字段，丢弃 Chat-only 噪音。
func applyLiteLLMChatOptionalParams(req *core.ChatRequest, respReq *core.ResponsesRequest) {
	if req == nil || respReq == nil {
		return
	}
	applyResponsesOptionalExtras(req.ExtraFields, respReq)
	if raw := req.ExtraFields.Lookup("web_search_options"); len(raw) > 0 && string(raw) != "null" {
		tool := map[string]any{"type": "web_search"}
		var opts map[string]any
		if json.Unmarshal(raw, &opts) == nil {
			for k, v := range opts {
				tool[k] = v
			}
		}
		respReq.Tools = append(respReq.Tools, tool)
	}
}

func transformChatResponseFormatToTextFormat(raw json.RawMessage) map[string]any {
	var rf map[string]any
	if json.Unmarshal(raw, &rf) != nil || len(rf) == 0 {
		return nil
	}
	formatType, _ := rf["type"].(string)
	switch formatType {
	case "json_schema":
		js, _ := rf["json_schema"].(map[string]any)
		if js == nil {
			js = map[string]any{}
		}
		name, _ := js["name"].(string)
		if name == "" {
			name = "response_schema"
		}
		schema, _ := js["schema"].(map[string]any)
		if schema == nil {
			schema = map[string]any{}
		}
		strict, _ := js["strict"].(bool)
		return map[string]any{
			"type":   "json_schema",
			"name":   name,
			"schema": schema,
			"strict": strict,
		}
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "text":
		return map[string]any{"type": "text"}
	default:
		return nil
	}
}

func truncateUserRaw(raw json.RawMessage) json.RawMessage {
	var user string
	if json.Unmarshal(raw, &user) != nil {
		return raw
	}
	if len(user) <= 64 {
		return raw
	}
	encoded, err := json.Marshal(user[:64])
	if err != nil {
		return raw
	}
	return encoded
}

// SanitizeResponsesRequest 出站防火墙：把误入的 Chat Completions / Claude 顶层字段
// 收敛为 Responses 合法形态（对齐 LiteLLM + OpenAI Responses 文档）。
// 直连 /responses 与 Chat→Responses 桥接都会走这里。
func SanitizeResponsesRequest(req *core.ResponsesRequest) {
	if req == nil {
		return
	}
	// Chat Completions 的 stream_options.include_usage 对 Responses 非法。
	req.StreamOptions = nil
	// Claude / CC Switch 注入的 thinking → reasoning（或丢弃），避免 Unknown parameter: 'thinking'
	applyThinkingToReasoning(req)
	applyResponsesOptionalExtras(req.ExtraFields, req)
}

// applyThinkingToReasoning 处理 Anthropic/Claude Code/CC Switch 注入的顶层 thinking。
// OpenAI/Azure Responses 只认 reasoning；thinking 必须剥离，否则上游 400。
func applyThinkingToReasoning(req *core.ResponsesRequest) {
	if req == nil {
		return
	}
	raw := req.ExtraFields.Lookup("thinking")
	if len(raw) == 0 || string(raw) == "null" {
		return
	}

	// 已有 reasoning.effort 时只剥 thinking，不覆盖客户端明确指定的推理强度。
	if req.Reasoning != nil && strings.TrimSpace(req.Reasoning.Effort) != "" {
		return
	}

	effort := mapThinkingPayloadToEffort(raw)
	if effort == "" {
		return
	}
	if req.Reasoning == nil {
		req.Reasoning = &core.Reasoning{Effort: effort}
		return
	}
	req.Reasoning.Effort = effort
}

// mapThinkingPayloadToEffort 将 Claude thinking 载荷粗映射为 OpenAI reasoning.effort。
// 支持：
//   - {"type":"enabled","budget_tokens":N}
//   - {"type":"adaptive"} / {"type":"disabled"}
//   - 字符串 "low"|"medium"|"high" 等
func mapThinkingPayloadToEffort(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return ""
		}
		s = strings.ToLower(strings.TrimSpace(s))
		switch s {
		case "none", "minimal", "low", "medium", "high", "xhigh":
			return s
		default:
			return ""
		}
	}

	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	if t, _ := obj["type"].(string); strings.EqualFold(t, "disabled") {
		return "none"
	}
	if t, _ := obj["type"].(string); strings.EqualFold(t, "adaptive") {
		return "medium"
	}
	if effort, _ := obj["effort"].(string); strings.TrimSpace(effort) != "" {
		return strings.ToLower(strings.TrimSpace(effort))
	}

	budget := 0
	switch v := obj["budget_tokens"].(type) {
	case float64:
		budget = int(v)
	case int:
		budget = v
	case json.Number:
		n, _ := v.Int64()
		budget = int(n)
	}
	switch {
	case budget <= 0:
		if t, _ := obj["type"].(string); strings.EqualFold(t, "enabled") {
			return "medium"
		}
		return ""
	case budget < 1024:
		return "low"
	case budget < 8192:
		return "medium"
	default:
		return "high"
	}
}

// applyResponsesOptionalExtras 将 ExtraFields 收敛为 Responses 白名单字段，
// 并完成 verbosity / response_format → text.* 的 LiteLLM/OpenAI 映射。
// thinking / reasoning_effort 等 Chat/Claude-only 字段不在白名单，出站会被丢弃。
func applyResponsesOptionalExtras(src core.UnknownJSONFields, respReq *core.ResponsesRequest) {
	if respReq == nil {
		return
	}

	out := map[string]json.RawMessage{}
	textObj := map[string]any{}

	if raw := src.Lookup("text"); len(raw) > 0 && string(raw) != "null" {
		_ = json.Unmarshal(raw, &textObj)
	}
	if raw := src.Lookup("response_format"); len(raw) > 0 && string(raw) != "null" {
		if format := transformChatResponseFormatToTextFormat(raw); format != nil {
			if _, has := textObj["format"]; !has {
				textObj["format"] = format
			}
		}
	}
	if raw := src.Lookup("verbosity"); len(raw) > 0 && string(raw) != "null" {
		if _, has := textObj["verbosity"]; !has {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				textObj["verbosity"] = v
			}
		}
	}
	if len(textObj) > 0 {
		if encoded, err := json.Marshal(textObj); err == nil {
			out["text"] = encoded
		}
	}

	for key := range responsesAPIOptionalParamKeys {
		if key == "text" {
			continue
		}
		if raw := src.Lookup(key); len(raw) > 0 && string(raw) != "null" {
			if key == "user" {
				out[key] = truncateUserRaw(raw)
				continue
			}
			out[key] = raw
		}
	}

	if raw := src.Lookup("stream_options"); len(raw) > 0 && string(raw) != "null" {
		var so map[string]any
		if json.Unmarshal(raw, &so) == nil {
			if obf, ok := so["include_obfuscation"]; ok {
				if encoded, err := json.Marshal(map[string]any{"include_obfuscation": obf}); err == nil {
					out["stream_options"] = encoded
				}
			}
		}
	}

	if raw := src.Lookup("metadata"); len(raw) > 0 && string(raw) != "null" && respReq.Metadata == nil {
		var md map[string]string
		if json.Unmarshal(raw, &md) == nil {
			respReq.Metadata = md
		} else {
			out["metadata"] = raw
		}
	}

	respReq.ExtraFields = core.UnknownJSONFieldsFromMap(out)
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
