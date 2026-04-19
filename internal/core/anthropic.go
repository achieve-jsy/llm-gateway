// Package core — Anthropic Messages API types and converters.
package core

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────────────────────────
// Anthropic Messages API request types
// ──────────────────────────────────────────────

// AnthropicMessagesRequest represents a request to the Anthropic Messages API.
type AnthropicMessagesRequest struct {
	Model         string                `json:"model"`
	MaxTokens     int                   `json:"max_tokens"`
	Messages      []AnthropicMessage    `json:"messages"`
	System        any                   `json:"system,omitempty"`          // string or []AnthropicSystemBlock
	Tools         []AnthropicTool       `json:"tools,omitempty"`           // Anthropic tool definition
	ToolChoice    any                   `json:"tool_choice,omitempty"`     // string or AnthropicToolChoice
	Stream        bool                  `json:"stream,omitempty"`
	Temperature   *float64              `json:"temperature,omitempty"`
	TopP          *float64              `json:"top_p,omitempty"`
	TopK          *int                  `json:"top_k,omitempty"`
	StopSequences []string              `json:"stop_sequences,omitempty"`
	Metadata      map[string]string     `json:"metadata,omitempty"`
	Thinking      *AnthropicThinking    `json:"thinking,omitempty"`
}

// AnthropicMessage is a single message in an Anthropic conversation.
// Content is either a plain string or []AnthropicContentBlock.
type AnthropicMessage struct {
	Role    string `json:"role"`            // "user" or "assistant"
	Content any    `json:"content"`         // string or []AnthropicContentBlock
}

// AnthropicContentBlock is a discriminated content block inside a message.
type AnthropicContentBlock struct {
	Type         string                `json:"type"`                    // "text", "image", "tool_use", "tool_result", "thinking", "redacted_thinking"
	Text         string                `json:"text,omitempty"`
	Source       *AnthropicImageSource `json:"source,omitempty"`         // type="image"
	ID           string                `json:"id,omitempty"`             // type="tool_use"
	Name         string                `json:"name,omitempty"`           // type="tool_use"
	Input        json.RawMessage       `json:"input,omitempty"`          // type="tool_use" — tool arguments
	ToolUseID    string                `json:"tool_use_id,omitempty"`    // type="tool_result"
	Content      any                   `json:"content,omitempty"`        // type="tool_result" — string or []AnthropicContentBlock
	IsError      bool                  `json:"is_error,omitempty"`       // type="tool_result"
	Thinking     string                `json:"thinking,omitempty"`       // type="thinking"
	Signature    string                `json:"signature,omitempty"`      // type="redacted_thinking"
	CacheControl json.RawMessage       `json:"cache_control,omitempty"` // prompt-cache breakpoint
}

// AnthropicImageSource references an image for content blocks of type "image".
type AnthropicImageSource struct {
	Type      string `json:"type"`                // "base64" or "url"
	MediaType string `json:"media_type,omitempty"` // e.g. "image/png"
	Data      string `json:"data,omitempty"`       // base64-encoded data
	URL       string `json:"url,omitempty"`        // URL (when type="url")
}

// AnthropicTool defines a tool that can be used by the model.
type AnthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`            // JSON Schema object
	CacheControl json.RawMessage `json:"cache_control,omitempty"` // prompt-cache breakpoint (usually on last tool)
}

// AnthropicToolChoice controls which tool (if any) the model should use.
type AnthropicToolChoice struct {
	Type string `json:"type"`          // "auto", "any", "tool"
	Name string `json:"name,omitempty"` // required when type="tool"
}

// AnthropicThinking enables extended thinking.
type AnthropicThinking struct {
	Type          string `json:"type"`                     // "enabled"
	BudgetTokens  int    `json:"budget_tokens,omitempty"`  // max thinking tokens
}

// AnthropicSystemBlock is a system message content block.
type AnthropicSystemBlock struct {
	Type         string          `json:"type"` // "text"
	Text         string          `json:"text"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"` // prompt-cache breakpoint
}

// ──────────────────────────────────────────────
// Anthropic Messages API response types
// ──────────────────────────────────────────────

// AnthropicMessagesResponse represents a response from the Anthropic Messages API.
type AnthropicMessagesResponse struct {
	ID           string                    `json:"id"`
	Type         string                    `json:"type"`                     // "message"
	Role         string                    `json:"role"`                     // "assistant"
	Content      []AnthropicContentBlock   `json:"content"`
	Model        string                    `json:"model"`
	StopReason   string                    `json:"stop_reason,omitempty"`    // "end_turn", "max_tokens", "stop_sequence", "tool_use"
	StopSequence string                    `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage            `json:"usage"`
}

// AnthropicUsage contains token usage information.
type AnthropicUsage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheCreationInputTokens  *int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens      *int `json:"cache_read_input_tokens,omitempty"`
}

// ──────────────────────────────────────────────
// Converters: Anthropic → ChatRequest (OpenAI)
// ──────────────────────────────────────────────

// AnthropicToChatRequest converts an Anthropic Messages API request
// to the internal ChatRequest (OpenAI-compatible) format.
func AnthropicToChatRequest(anthReq *AnthropicMessagesRequest) *ChatRequest {
	req := &ChatRequest{
		Model:      anthReq.Model,
		Stream:     anthReq.Stream,
		Messages:   convertAnthropicMessages(anthReq.Messages),
	}
	if anthReq.MaxTokens > 0 {
		maxTok := anthReq.MaxTokens
		req.MaxTokens = &maxTok
	}
	if anthReq.Temperature != nil {
		req.Temperature = anthReq.Temperature
	}
	// TopP and StopSequences are passed through ExtraFields
	// since ChatRequest does not have direct fields for them.
	_ = anthReq.TopP
	_ = anthReq.StopSequences

	// Convert tools to OpenAI format
	req.Tools = convertAnthropicTools(anthReq.Tools)

	// Convert tool_choice
	req.ToolChoice = convertAnthropicToolChoice(anthReq.ToolChoice)

	// Add system message if present
	req.Messages = prependSystemMessage(anthReq.System, req.Messages)

	return req
}

// convertAnthropicMessages converts Anthropic messages (with content blocks)
// to OpenAI messages (string content + tool_calls array).
// decodeContentBlocks converts []any (JSON-decoded) to []AnthropicContentBlock.
// This handles the case where JSON unmarshaling produces []any instead of
// []AnthropicContentBlock when the Content field type is any.
func decodeContentBlocks(content any) []AnthropicContentBlock {
	arr, ok := content.([]any)
	if !ok {
		return nil
	}
	var blocks []AnthropicContentBlock
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		block := AnthropicContentBlock{}
		if t, ok := m["type"].(string); ok {
			block.Type = t
		}
		if id, ok := m["id"].(string); ok {
			block.ID = id
		}
		if text, ok := m["text"].(string); ok {
			block.Text = text
		}
		if name, ok := m["name"].(string); ok {
			block.Name = name
		}
		if toolID, ok := m["tool_use_id"].(string); ok {
			block.ToolUseID = toolID
		}
		if thinking, ok := m["thinking"].(string); ok {
			block.Thinking = thinking
		}
		if sig, ok := m["signature"].(string); ok {
			block.Signature = sig
		}
		if isError, ok := m["is_error"].(bool); ok {
			block.IsError = isError
		}
		// Source: *AnthropicImageSource
		if sourceMap, ok := m["source"].(map[string]any); ok {
			src := &AnthropicImageSource{}
			if t, ok := sourceMap["type"].(string); ok {
				src.Type = t
			}
			if mt, ok := sourceMap["media_type"].(string); ok {
				src.MediaType = mt
			}
			if d, ok := sourceMap["data"].(string); ok {
				src.Data = d
			}
			if u, ok := sourceMap["url"].(string); ok {
				src.URL = u
			}
			block.Source = src
		}
		// Input: json.RawMessage — re-marshal from map
		if inputMap, ok := m["input"].(map[string]any); ok {
			if raw, err := json.Marshal(inputMap); err == nil {
				block.Input = raw
			}
		}
		// Content: any — pass through as-is
		if c, exists := m["content"]; exists {
			block.Content = c
		}
		if cc, exists := m["cache_control"]; exists {
			block.CacheControl = rawCacheControlFromAny(cc)
		}
		if block.Type != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// rawCacheControlFromAny normalizes a decoded cache_control value to json.RawMessage.
func rawCacheControlFromAny(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case json.RawMessage:
		return CloneRawJSON(t)
	case []byte:
		return CloneRawJSON(json.RawMessage(t))
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		return raw
	}
}

// cacheControlExtraFields lifts Anthropic cache_control into ContentPart/Message ExtraFields
// so Bedrock / native Anthropic adapters can re-emit CachePoint / cache_control.
func cacheControlExtraFields(cc json.RawMessage) UnknownJSONFields {
	if len(cc) == 0 || string(cc) == "null" {
		return UnknownJSONFields{}
	}
	return UnknownJSONFieldsFromMap(map[string]json.RawMessage{
		"cache_control": CloneRawJSON(cc),
	})
}

func convertAnthropicMessages(anthMsgs []AnthropicMessage) []Message {
	if len(anthMsgs) == 0 {
		return nil
	}
	msgs := make([]Message, 0, len(anthMsgs))
	for _, anthMsg := range anthMsgs {
		converted := convertOneAnthropicMessage(anthMsg)
		msgs = append(msgs, converted...)
	}
	return msgs
}

// convertOneAnthropicMessage converts a single Anthropic message to one or more
// OpenAI messages. A user message with tool_result blocks may produce
// multiple OpenAI "tool" role messages.
func convertOneAnthropicMessage(anthMsg AnthropicMessage) []Message {
	switch content := anthMsg.Content.(type) {
	case string:
		return []Message{{Role: anthMsg.Role, Content: content}}
	case nil:
		return []Message{{Role: anthMsg.Role, Content: ""}}
	case []AnthropicContentBlock:
		return flattenAnthropicBlocksToMessages(anthMsg.Role, content)
	case []any:
		// JSON decoding produces []any for array content; re-marshal and decode
		// into typed blocks.
		blocks := decodeContentBlocks(content)
		if len(blocks) > 0 {
			return flattenAnthropicBlocksToMessages(anthMsg.Role, blocks)
		}
		return []Message{{Role: anthMsg.Role, Content: fmt.Sprintf("%v", content)}}
	default:
		return []Message{{Role: anthMsg.Role, Content: fmt.Sprintf("%v", content)}}
	}
}

// flattenAnthropicBlocksToMessages processes content blocks and produces
// OpenAI messages. It handles:
//   - text → string content or ContentPart
//   - image → ContentPart with image_url
//   - tool_use (assistant) → ToolCall entries
//   - tool_result (user) → separate messages with role "tool"
func flattenAnthropicBlocksToMessages(role string, blocks []AnthropicContentBlock) []Message {
	var (
		result       []Message
		textParts    []ContentPart
		toolCalls    []ToolCall
		toolResults  []Message
	)

	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, ContentPart{
					Type:        "text",
					Text:        block.Text,
					ExtraFields: cacheControlExtraFields(block.CacheControl),
				})
			}
		case "image":
			part := convertAnthropicImage(block)
			if part != nil {
				if extra := cacheControlExtraFields(block.CacheControl); !extra.IsEmpty() {
					part.ExtraFields = extra
				}
				textParts = append(textParts, *part)
			}
		case "tool_use":
			tc := ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
				ExtraFields: cacheControlExtraFields(block.CacheControl),
			}
			toolCalls = append(toolCalls, tc)
		case "tool_result":
			// tool_result becomes a separate "tool" role message
			toolMsg := Message{
				Role:        "tool",
				ToolCallID:  block.ToolUseID,
				ExtraFields: cacheControlExtraFields(block.CacheControl),
			}
			switch resContent := block.Content.(type) {
			case string:
				toolMsg.Content = resContent
			case []AnthropicContentBlock:
				// Extract text from sub-blocks (could also contain images)
				var parts []ContentPart
				for _, sub := range resContent {
					if sub.Type == "text" && sub.Text != "" {
						parts = append(parts, ContentPart{Type: "text", Text: sub.Text})
					} else if sub.Type == "image" {
						if p := convertAnthropicImage(sub); p != nil {
							parts = append(parts, *p)
						}
					}
				}
				if len(parts) == 1 && parts[0].Type == "text" {
					toolMsg.Content = parts[0].Text
				} else if len(parts) > 0 {
					toolMsg.Content = parts
				}
			case []any:
				// JSON decoding produces []any; decode into typed blocks.
				subBlocks := decodeContentBlocks(resContent)
				var parts []ContentPart
				for _, sub := range subBlocks {
					if sub.Type == "text" && sub.Text != "" {
						parts = append(parts, ContentPart{Type: "text", Text: sub.Text})
					} else if sub.Type == "image" {
						if p := convertAnthropicImage(sub); p != nil {
							parts = append(parts, *p)
						}
					}
				}
				if len(parts) == 1 && parts[0].Type == "text" {
					toolMsg.Content = parts[0].Text
				} else if len(parts) > 0 {
					toolMsg.Content = parts
				}
			case nil:
				toolMsg.Content = ""
			default:
				toolMsg.Content = fmt.Sprintf("%v", resContent)
			}
			toolResults = append(toolResults, toolMsg)
		}
	}

	// For assistant messages: tool_calls are bundled into the single assistant message.
	// For user messages that mix tool_result blocks with text/image blocks, OpenAI/Azure
	// requires all tool role messages to appear BEFORE any following user text message.
	// Emitting text first would produce an invalid sequence:
	//   assistant (tool_calls) → user (text) → tool
	// instead of the required:
	//   assistant (tool_calls) → tool → user (text)
	// So always emit tool results first, then the text/image content message.

	// Emit tool result messages first (only present on user-role messages).
	result = append(result, toolResults...)

	// Build the primary message (assistant with tool_calls, or user with content).
	// 单 text part 若带 cache_control，必须保留为 ContentPart，不能折叠成 string（否则 ExtraFields 丢失）。
	if len(textParts) > 0 || len(toolCalls) > 0 {
		msg := Message{Role: role}
		if len(textParts) == 1 && textParts[0].Type == "text" && len(toolCalls) == 0 && textParts[0].ExtraFields.IsEmpty() {
			msg.Content = textParts[0].Text
		} else if len(textParts) > 0 {
			msg.Content = textParts
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		result = append(result, msg)
	}

	return result
}

// convertAnthropicImage converts an Anthropic image block to an OpenAI ContentPart.
func convertAnthropicImage(block AnthropicContentBlock) *ContentPart {
	if block.Source == nil {
		return nil
	}
	switch block.Source.Type {
	case "base64":
		mediaType := block.Source.MediaType
		if mediaType == "" {
			mediaType = "image/png"
		}
		url := fmt.Sprintf("data:%s;base64,%s", mediaType, block.Source.Data)
		return &ContentPart{
			Type:     "image_url",
			ImageURL: &ImageURLContent{URL: url, Detail: "auto"},
		}
	case "url":
		return &ContentPart{
			Type:     "image_url",
			ImageURL: &ImageURLContent{URL: block.Source.URL, Detail: "auto"},
		}
	default:
		return nil
	}
}

// convertAnthropicTools converts Anthropic tool definitions to OpenAI
// function-calling format ([]map[string]any).
func convertAnthropicTools(anthTools []AnthropicTool) []map[string]any {
	if len(anthTools) == 0 {
		return nil
	}
	defaultSchema := map[string]any{"type": "object", "properties": map[string]any{}}
	tools := make([]map[string]any, 0, len(anthTools))
	for _, tool := range anthTools {
		// parameters 必须是 map[string]any（object），provider 层会做 type assertion。
		// InputSchema 为空/null/非 object 时，兜底为空 schema。
		var params map[string]any
		if len(tool.InputSchema) > 0 && string(tool.InputSchema) != "null" {
			if err := json.Unmarshal(tool.InputSchema, &params); err != nil || params == nil {
				params = defaultSchema
			}
		} else {
			params = defaultSchema
		}
		t := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  params,
			},
		}
		if len(tool.CacheControl) > 0 && string(tool.CacheControl) != "null" {
			var cc any
			if err := json.Unmarshal(tool.CacheControl, &cc); err == nil && cc != nil {
				t["cache_control"] = cc
			}
		}
		tools = append(tools, t)
	}
	return tools
}

// convertAnthropicToolChoice converts Anthropic tool_choice to OpenAI
// tool_choice (string or map).
func convertAnthropicToolChoice(tc any) any {
	if tc == nil {
		return nil
	}

	switch v := tc.(type) {
	case string:
		switch v {
		case "any":
			return "required"
		case "auto":
			return "auto"
		case "none":
			return "none"
		default:
			return v
		}
	case map[string]any:
		// {"type": "tool", "name": "xxx"}
		if t, ok := v["type"].(string); ok && t == "tool" {
			if name, ok := v["name"].(string); ok {
				return map[string]any{
					"type": "function",
					"function": map[string]any{
						"name": name,
					},
				}
			}
		}
		// {"type": "any"} or {"type": "auto"}
		if t, ok := v["type"].(string); ok {
			if t == "any" {
				return "required"
			}
			return t
		}
		return v
	default:
		return tc
	}
}

// prependSystemMessage adds system instructions as the first message
// (with role "system") if present.
//
// 若 system 为带 cache_control 的 block 数组，保留为 []ContentPart（ExtraFields），
// 供 Bedrock 在对应 text block 后插入 CachePoint；无断点时仍折叠为普通 string。
func prependSystemMessage(system any, msgs []Message) []Message {
	if system == nil {
		return msgs
	}

	parts := systemToContentParts(system)
	if len(parts) == 0 {
		return msgs
	}

	var content any
	if len(parts) == 1 && parts[0].ExtraFields.IsEmpty() {
		content = parts[0].Text
	} else {
		content = parts
	}

	result := make([]Message, 0, len(msgs)+1)
	result = append(result, Message{Role: "system", Content: content})
	result = append(result, msgs...)
	return result
}

// systemToContentParts normalizes Anthropic system (string / blocks) into ContentParts,
// preserving per-block cache_control.
func systemToContentParts(system any) []ContentPart {
	switch s := system.(type) {
	case string:
		if s == "" {
			return nil
		}
		return []ContentPart{{Type: "text", Text: s}}
	case []AnthropicSystemBlock:
		parts := make([]ContentPart, 0, len(s))
		for _, block := range s {
			if block.Type != "text" || block.Text == "" {
				continue
			}
			parts = append(parts, ContentPart{
				Type:        "text",
				Text:        block.Text,
				ExtraFields: cacheControlExtraFields(block.CacheControl),
			})
		}
		return parts
	case []any:
		parts := make([]ContentPart, 0, len(s))
		for _, item := range s {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t != "" && t != "text" {
				continue
			}
			text, _ := m["text"].(string)
			if text == "" {
				continue
			}
			parts = append(parts, ContentPart{
				Type:        "text",
				Text:        text,
				ExtraFields: cacheControlExtraFields(rawCacheControlFromAny(m["cache_control"])),
			})
		}
		return parts
	case map[string]any:
		text, _ := s["text"].(string)
		if text == "" {
			return nil
		}
		return []ContentPart{{
			Type:        "text",
			Text:        text,
			ExtraFields: cacheControlExtraFields(rawCacheControlFromAny(s["cache_control"])),
		}}
	default:
		text := fmt.Sprintf("%v", s)
		if text == "" || text == "<nil>" {
			return nil
		}
		return []ContentPart{{Type: "text", Text: text}}
	}
}

// ──────────────────────────────────────────────
// Converters: ChatResponse → Anthropic
// ──────────────────────────────────────────────

// ChatResponseToAnthropic converts an internal ChatResponse (OpenAI format)
// to an Anthropic Messages API response.
func ChatResponseToAnthropic(resp *ChatResponse, model string) *AnthropicMessagesResponse {
	if resp == nil {
		return nil
	}

	anthResp := &AnthropicMessagesResponse{
		ID:         resp.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		StopReason: mapOpenAIFinishReason(resp.Choices),
		Usage: AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}

	// Cache tokens from details
	if resp.Usage.PromptTokensDetails != nil {
		if resp.Usage.PromptTokensDetails.CachedTokens > 0 {
			v := resp.Usage.PromptTokensDetails.CachedTokens
			anthResp.Usage.CacheReadInputTokens = &v
		}
	}

	// Convert choices to content blocks
	for _, choice := range resp.Choices {
		msg := choice.Message
		blocks := responseMessageToAnthropicBlocks(&msg)
		anthResp.Content = append(anthResp.Content, blocks...)
	}

	// Ensure Content is never null (Anthropic API requires an array)
	if anthResp.Content == nil {
		anthResp.Content = []AnthropicContentBlock{}
	}

	return anthResp
}

// mapOpenAIFinishReason maps OpenAI finish reasons to Anthropic stop_reason.
func mapOpenAIFinishReason(choices []Choice) string {
	if len(choices) == 0 {
		return ""
	}
	reason := choices[0].FinishReason
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "end_turn"
	default:
		return reason
	}
}

// responseMessageToAnthropicBlocks converts an OpenAI response message
// to Anthropic content blocks.
func responseMessageToAnthropicBlocks(msg *ResponseMessage) []AnthropicContentBlock {
	var blocks []AnthropicContentBlock

	// Add text content
	if msg.Content != nil {
		switch content := msg.Content.(type) {
		case string:
			if content != "" {
				blocks = append(blocks, AnthropicContentBlock{
					Type: "text",
					Text: content,
				})
			}
		case []ContentPart:
			for _, part := range content {
				if part.Type == "text" && part.Text != "" {
					blocks = append(blocks, AnthropicContentBlock{
						Type: "text",
						Text: part.Text,
					})
				}
			}
		case []any:
			for _, p := range content {
				if m, ok := p.(map[string]any); ok {
					if t, _ := m["type"].(string); t == "text" {
						if text, _ := m["text"].(string); text != "" {
							blocks = append(blocks, AnthropicContentBlock{
								Type: "text",
								Text: text,
							})
						}
					}
				}
			}
		}
	}

	// Add tool call content
	for _, tc := range msg.ToolCalls {
		rawInput := json.RawMessage(tc.Function.Arguments)
		if len(rawInput) == 0 {
			rawInput = json.RawMessage("{}")
		}
		// Ensure it's valid JSON
		if !json.Valid(rawInput) {
			// Wrap string as JSON
			escaped, _ := json.Marshal(tc.Function.Arguments)
			rawInput = escaped
		}
		blocks = append(blocks, AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: rawInput,
		})
	}

	return blocks
}