// Package core — Anthropic Messages API types and converters.
package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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
	Type      string                `json:"type"`                // "text", "image", "tool_use", "tool_result", "thinking", "redacted_thinking"
	Text      string                `json:"text,omitempty"`
	Source    *AnthropicImageSource `json:"source,omitempty"`    // type="image"
	ID        string                `json:"id,omitempty"`        // type="tool_use"
	Name      string                `json:"name,omitempty"`      // type="tool_use"
	Input     json.RawMessage       `json:"input,omitempty"`     // type="tool_use" — tool arguments
	ToolUseID string                `json:"tool_use_id,omitempty"` // type="tool_result"
	Content   any                   `json:"content,omitempty"`   // type="tool_result" — string or []AnthropicContentBlock
	IsError   bool                  `json:"is_error,omitempty"`  // type="tool_result"
	Thinking  string                `json:"thinking,omitempty"`  // type="thinking"
	Signature string                `json:"signature,omitempty"` // type="redacted_thinking"
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
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"` // JSON Schema object
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
	Type string `json:"type"` // "text"
	Text string `json:"text"`
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

	// Sanitize messages for non-Anthropic providers
	req.Messages = sanitizeMessagesForProviders(req.Messages)

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
		if block.Type != "" {
			// Skip thinking and redacted_thinking blocks — they are Anthropic-specific
			// and should not be passed to non-Anthropic providers.
			if block.Type == "thinking" || block.Type == "redacted_thinking" {
				continue
			}
			blocks = append(blocks, block)
		}
	}
	return blocks
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
				textParts = append(textParts, ContentPart{Type: "text", Text: block.Text})
			}
		case "image":
			part := convertAnthropicImage(block)
			if part != nil {
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
			}
			toolCalls = append(toolCalls, tc)
		case "tool_result":
			// tool_result becomes a separate "tool" role message
			toolMsg := Message{
				Role:       "tool",
				ToolCallID: block.ToolUseID,
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

	// Build the primary message (assistant with tool_calls, or user with content)
	if len(textParts) > 0 || len(toolCalls) > 0 {
		msg := Message{Role: role}
		if len(textParts) == 1 && textParts[0].Type == "text" && len(toolCalls) == 0 {
			msg.Content = textParts[0].Text
		} else if len(textParts) > 0 {
			msg.Content = textParts
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
		}
		result = append(result, msg)
	}

	// Append tool result messages after the primary message
	result = append(result, toolResults...)
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
func prependSystemMessage(system any, msgs []Message) []Message {
	if system == nil {
		return msgs
	}

	var systemText string
	switch s := system.(type) {
	case string:
		systemText = s
	case []AnthropicSystemBlock:
		var b strings.Builder
		for _, block := range s {
			if block.Type == "text" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(block.Text)
			}
		}
		systemText = b.String()
	case []any:
		// JSON decoding produces []any for array system blocks; decode each map.
		var b strings.Builder
		for _, item := range s {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "text" {
					if text, ok := m["text"].(string); ok && text != "" {
						if b.Len() > 0 {
							b.WriteString("\n\n")
						}
						b.WriteString(text)
					}
				}
			}
		}
		systemText = b.String()
	case map[string]any:
		// Try to extract text from a single system block object
		if t, ok := s["text"].(string); ok {
			systemText = t
		}
	default:
		systemText = fmt.Sprintf("%v", s)
	}

	if systemText == "" {
		return msgs
	}

	result := make([]Message, 0, len(msgs)+1)
	result = append(result, Message{Role: "system", Content: systemText})
	result = append(result, msgs...)
	return result
}

// ──────────────────────────────────────────────
// Message sanitization for non-Anthropic providers
// ──────────────────────────────────────────────

// sanitizeMessagesForProviders applies a series of heuristics to convert
// Anthropic-format messages into a format suitable for non-Anthropic providers.
// These are best-effort sanitizations; the upstream provider may still reject
// the request if messages are fundamentally incompatible.
func sanitizeMessagesForProviders(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	msgs = dropThinkingBlocks(msgs)
	msgs = sanitizeToolUseIDs(msgs)
	msgs = flattenToolMessageContent(msgs)
	msgs = dropOrphanedToolResults(msgs)
	msgs = addDummyToolResults(msgs)
	msgs = mergeConsecutiveSameRole(msgs)
	msgs = ensureFirstMessageIsUser(msgs)
	msgs = sanitizeEmptyContent(msgs)
	return msgs
}

// toolUseIDPattern matches any character that is NOT a valid tool_use/tool_call ID character.
var toolUseIDPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitizeToolUseIDs replaces invalid characters in ToolCall IDs with underscores.
func sanitizeToolUseIDs(msgs []Message) []Message {
	for i := range msgs {
		if msgs[i].ToolCallID != "" {
			msgs[i].ToolCallID = toolUseIDPattern.ReplaceAllString(msgs[i].ToolCallID, "_")
		}
		for j := range msgs[i].ToolCalls {
			if msgs[i].ToolCalls[j].ID != "" {
				msgs[i].ToolCalls[j].ID = toolUseIDPattern.ReplaceAllString(msgs[i].ToolCalls[j].ID, "_")
			}
		}
	}
	return msgs
}

// dropThinkingBlocks removes content blocks of type "thinking" or "redacted_thinking"
// from messages that use structured content arrays.
func dropThinkingBlocks(msgs []Message) []Message {
	for i := range msgs {
		parts, ok := msgs[i].Content.([]ContentPart)
		if !ok {
			continue
		}
		filtered := make([]ContentPart, 0, len(parts))
		for _, p := range parts {
			if p.Type == "thinking" || p.Type == "redacted_thinking" {
				continue
			}
			filtered = append(filtered, p)
		}
		if len(filtered) == 0 {
			msgs[i].Content = ""
		} else if len(filtered) == 1 && filtered[0].Type == "text" {
			msgs[i].Content = filtered[0].Text
		} else {
			msgs[i].Content = filtered
		}
	}
	return msgs
}

// flattenToolMessageContent converts tool message content to a plain string.
// Some providers (e.g., OpenAI v1) require tool content to be a string.
func flattenToolMessageContent(msgs []Message) []Message {
	for i := range msgs {
		if msgs[i].Role != "tool" {
			continue
		}
		switch v := msgs[i].Content.(type) {
		case string:
			// Already a string, nothing to do
		case nil:
			msgs[i].Content = ""
		default:
			// Attempt JSON serialization, fall back to fmt
			if b, err := json.Marshal(v); err == nil {
				msgs[i].Content = string(b)
			} else {
				msgs[i].Content = fmt.Sprintf("%v", v)
			}
		}
	}
	return msgs
}

// dropOrphanedToolResults removes tool-role messages whose ToolCallID does not
// match any tool_call ID in any preceding assistant message.
func dropOrphanedToolResults(msgs []Message) []Message {
	// Collect all tool_call IDs from assistant messages.
	validIDs := make(map[string]struct{})
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			validIDs[tc.ID] = struct{}{}
		}
	}
	filtered := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			if _, found := validIDs[m.ToolCallID]; !found {
				continue // drop orphaned tool result
			}
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// addDummyToolResults inserts a dummy tool result for each tool_call that
// lacks a matching tool-role response, ensuring tool call/result pairing.
func addDummyToolResults(msgs []Message) []Message {
	// Collect tool_call IDs from assistant messages.
	pendingIDs := make(map[string]struct{})
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			pendingIDs[tc.ID] = struct{}{}
		}
	}
	if len(pendingIDs) == 0 {
		return msgs
	}
	// Remove IDs that already have a matching tool result.
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID != "" {
			delete(pendingIDs, m.ToolCallID)
		}
	}
	if len(pendingIDs) == 0 {
		return msgs
	}
	// Append dummy tool results at the end.
	for id := range pendingIDs {
		msgs = append(msgs, Message{
			Role:       "tool",
			ToolCallID: id,
			Content:    ".",
		})
	}
	return msgs
}

// mergeConsecutiveSameRole merges adjacent messages with the same role
// (user+user, assistant+assistant) into a single message.
func mergeConsecutiveSameRole(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}
	merged := make([]Message, 0, len(msgs))
	merged = append(merged, msgs[0])
	for i := 1; i < len(msgs); i++ {
		last := &merged[len(merged)-1]
		cur := msgs[i]
		if last.Role == cur.Role && last.Role != "system" && last.Role != "tool" {
			last.Content = mergeContent(last.Content, cur.Content)
			last.ToolCalls = append(last.ToolCalls, cur.ToolCalls...)
		} else {
			merged = append(merged, cur)
		}
	}
	return merged
}

// mergeContent concatenates two message content values into one.
func mergeContent(a, b any) any {
	if a == nil || a == "" {
		return b
	}
	if b == nil || b == "" {
		return a
	}
	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		return aStr + "\n" + bStr
	}
	// Convert both to []ContentPart
	parts := asContentParts(a)
	parts = append(parts, asContentParts(b)...)
	return parts
}

// asContentParts converts a message content value to a slice of ContentParts.
func asContentParts(v any) []ContentPart {
	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		return []ContentPart{{Type: "text", Text: val}}
	case []ContentPart:
		return val
	case []any:
		parts := make([]ContentPart, 0, len(val))
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "text" {
					if text, _ := m["text"].(string); text != "" {
						parts = append(parts, ContentPart{Type: "text", Text: text})
					}
				}
			}
		}
		return parts
	default:
		return nil
	}
}

// ensureFirstMessageIsUser ensures the first message (after system) has role "user".
// If not, a placeholder user message is inserted at the start.
func ensureFirstMessageIsUser(msgs []Message) []Message {
	if len(msgs) == 0 {
		return []Message{{Role: "user", Content: "."}}
	}
	// Skip leading system messages
	startIdx := 0
	for startIdx < len(msgs) && msgs[startIdx].Role == "system" {
		startIdx++
	}
	if startIdx >= len(msgs) {
		return append(msgs, Message{Role: "user", Content: "."})
	}
	if msgs[startIdx].Role == "user" {
		return msgs
	}
	// Insert a placeholder user message before the first non-user, non-system message.
	placeholder := Message{Role: "user", Content: "."}
	result := make([]Message, 0, len(msgs)+1)
	result = append(result, msgs[:startIdx]...)
	result = append(result, placeholder)
	result = append(result, msgs[startIdx:]...)
	return result
}

// sanitizeEmptyContent replaces empty string content with "." for all messages.
// Some providers reject messages with empty content.
func sanitizeEmptyContent(msgs []Message) []Message {
	for i := range msgs {
		if s, ok := msgs[i].Content.(string); ok && s == "" {
			msgs[i].Content = "."
		} else if msgs[i].Content == nil {
			msgs[i].Content = "."
		}
	}
	return msgs
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