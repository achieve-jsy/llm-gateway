package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// defaultMaxTokensEnvVar overrides the fallback applied when callers omit
// max_tokens. Anthropic requires the field on every /v1/messages request, so
// GoModel injects this value to keep the OpenAI-compatible surface lenient.
const defaultMaxTokensEnvVar = "ANTHROPIC_DEFAULT_MAX_TOKENS"

// fallbackMaxTokens is the safe default used when the env var is unset or
// invalid.
const fallbackMaxTokens = 4096

var invalidDefaultMaxTokensWarnOnce sync.Once

func resolveDefaultMaxTokens() int {
	raw := strings.TrimSpace(os.Getenv(defaultMaxTokensEnvVar))
	if raw == "" {
		return fallbackMaxTokens
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		invalidDefaultMaxTokensWarnOnce.Do(func() {
			slog.Warn("invalid "+defaultMaxTokensEnvVar+"; using fallback",
				"value", raw, "fallback", fallbackMaxTokens)
		})
		return fallbackMaxTokens
	}
	return n
}

// applyReasoning configures thinking and effort on an anthropicRequest.
// Opus 4.6 and Sonnet 4.6 use adaptive thinking with output_config.effort.
// Older models and Haiku 4.6 use manual thinking with budget_tokens.
//
// 预算来源优先级：客户端显式给出的 BudgetTokens > 由 Effort 档位反推。
// 反推只是近似（low 会落到 5000），拿它覆盖显式预算会把预算和 max_tokens
// 一起放大，直接改变计费口径。
func applyReasoning(req *anthropicRequest, model string, r *core.Reasoning) {
	effort := r.Effort
	if isAdaptiveThinkingModel(model) {
		req.Thinking = &anthropicThinking{Type: "adaptive"}
		req.OutputConfig = &anthropicOutputConfig{Effort: normalizeEffort(effort)}
	} else {
		budget := reasoningEffortToBudgetTokens(effort)
		if r.BudgetTokens != nil && *r.BudgetTokens > 0 {
			budget = *r.BudgetTokens
		}
		req.Thinking = &anthropicThinking{
			Type:         "enabled",
			BudgetTokens: budget,
		}
		if req.MaxTokens <= budget {
			adjusted := budget + 1024
			slog.Info("MaxTokens adjusted for extended thinking",
				"original", req.MaxTokens, "adjusted", adjusted)
			req.MaxTokens = adjusted
		}
	}

	if req.Temperature != nil {
		if *req.Temperature != 1.0 {
			slog.Warn("temperature overridden to nil; extended thinking requires temperature=1",
				"original_temperature", *req.Temperature)
			req.Temperature = nil
		}
	}
}

// reasoningEffortToBudgetTokens 把 effort 档位映射成思考预算。
//
// 注意 normalizeEffort 只认 low/medium/high，且把任何非法值都归一到 low ——
// 所以 "low" 必须显式列出来。它此前落进 default，导致"最低档"和"未知值"
// 共用 5000，比 medium 只小一半；配合 applyReasoning 的 max_tokens 抬升，
// 一个 max_tokens=64 的低档请求会被抬到 6024。
func reasoningEffortToBudgetTokens(effort string) int {
	switch normalizeEffort(effort) {
	case "low":
		return 2000
	case "medium":
		return 10000
	case "high":
		return 20000
	default:
		return 5000
	}
}

func convertOpenAIToolsToAnthropic(tools []map[string]any) ([]anthropicTool, error) {
	out := make([]anthropicTool, 0, len(tools))
	for _, tool := range tools {
		toolType, _ := tool["type"].(string)
		if toolType != "function" {
			return nil, core.NewInvalidRequestError("unsupported tool type", nil)
		}

		function, ok := tool["function"].(map[string]any)
		if !ok {
			return nil, core.NewInvalidRequestError("tool.function must be an object", nil)
		}

		name, _ := function["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, core.NewInvalidRequestError("tool.function.name is required", nil)
		}

		description, _ := function["description"].(string)
		inputSchema, hasParameters := function["parameters"]
		if !hasParameters || inputSchema == nil {
			inputSchema = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		} else {
			schema, ok := inputSchema.(map[string]any)
			if !ok {
				return nil, core.NewInvalidRequestError("tool.function.parameters must be an object", nil)
			}
			if schemaType, ok := schema["type"].(string); ok && schemaType != "" && schemaType != "object" {
				return nil, core.NewInvalidRequestError("tool.function.parameters must define an object schema", nil)
			}
			inputSchema = schema
		}

		out = append(out, anthropicTool{
			Name:        name,
			Description: description,
			InputSchema: inputSchema.(map[string]any),
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func convertOpenAIToolChoiceToAnthropic(choice any) (*anthropicToolChoice, bool, error) {
	switch c := choice.(type) {
	case nil:
		return nil, false, nil
	case string:
		switch strings.TrimSpace(c) {
		case "", "auto":
			return &anthropicToolChoice{Type: "auto"}, false, nil
		case "required":
			return &anthropicToolChoice{Type: "any"}, false, nil
		case "none":
			return nil, true, nil
		default:
			return nil, false, core.NewInvalidRequestError("unsupported tool_choice value", nil)
		}
	case map[string]any:
		choiceType, _ := c["type"].(string)
		switch choiceType {
		case "auto", "any":
			return &anthropicToolChoice{Type: choiceType}, false, nil
		case "none":
			return nil, true, nil
		case "function":
			if function, ok := c["function"].(map[string]any); ok {
				name, _ := function["name"].(string)
				if strings.TrimSpace(name) != "" {
					return &anthropicToolChoice{Type: "tool", Name: name}, false, nil
				}
			}
			return nil, false, core.NewInvalidRequestError("tool_choice.function.name is required", nil)
		case "tool":
			name, _ := c["name"].(string)
			if name == "" {
				if function, ok := c["function"].(map[string]any); ok {
					name, _ = function["name"].(string)
				}
			}
			if strings.TrimSpace(name) == "" {
				return nil, false, core.NewInvalidRequestError("tool_choice.name is required", nil)
			}
			return &anthropicToolChoice{Type: "tool", Name: name}, false, nil
		default:
			return nil, false, core.NewInvalidRequestError("unsupported tool_choice type", nil)
		}
	default:
		return nil, false, core.NewInvalidRequestError("tool_choice must be a string or object", nil)
	}
}

func applyParallelToolCalls(choice *anthropicToolChoice, parallelToolCalls *bool) *anthropicToolChoice {
	if choice == nil || parallelToolCalls == nil || *parallelToolCalls {
		return choice
	}

	out := *choice
	disableParallelToolUse := true
	out.DisableParallelToolUse = &disableParallelToolUse
	return &out
}

func parseToolCallArguments(arguments string) (any, error) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]any{}, nil
	}

	var parsed any
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("tool arguments must contain exactly one JSON object")
		}
		return nil, err
	}
	if _, ok := parsed.(map[string]any); !ok {
		return nil, fmt.Errorf("tool arguments must be a JSON object")
	}
	return parsed, nil
}

func buildAnthropicMessageContent(msg core.Message) (any, error) {
	const maxToolCallsPerMessage = 1024

	if msg.Role == "tool" {
		toolUseID := strings.TrimSpace(msg.ToolCallID)
		if toolUseID == "" {
			return nil, core.NewInvalidRequestError("tool message is missing tool_call_id", nil)
		}
		return []anthropicContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: toolUseID,
				Content:   core.ExtractTextContent(msg.Content),
			},
		}, nil
	}

	content, err := convertMessageContentToAnthropic(msg.Content)
	if err != nil {
		return nil, err
	}

	// Extract thinking_blocks from ExtraFields and prepend them to the assistant
	// content. Anthropic requires thinking blocks to appear before any text or
	// tool_use blocks in multi-turn conversations, and verifies their signatures.
	//
	// Claude Code stores thinking_blocks as a top-level field on the assistant
	// message (not inside content), so they land in msg.ExtraFields after
	// JSON deserialisation.
	var prependBlocks []anthropicContentBlock
	if msg.Role == "assistant" {
		if raw := msg.ExtraFields.Lookup("thinking_blocks"); len(raw) > 0 {
			var parsed []anthropicContentBlock
			if err := json.Unmarshal(raw, &parsed); err == nil {
				for _, b := range parsed {
					switch b.Type {
					case "thinking":
						if b.Thinking != "" || b.Signature != "" {
							prependBlocks = append(prependBlocks, anthropicContentBlock{
								Type:      "thinking",
								Thinking:  b.Thinking,
								Signature: b.Signature,
							})
						}
					case "redacted_thinking":
						if b.Data != "" {
							prependBlocks = append(prependBlocks, anthropicContentBlock{
								Type: "redacted_thinking",
								Data: b.Data,
							})
						}
					}
				}
			}
		}
	}

	// No tool calls and no prepended blocks — return content directly.
	if len(msg.ToolCalls) == 0 && len(prependBlocks) == 0 {
		return content, nil
	}
	if len(msg.ToolCalls) > maxToolCallsPerMessage {
		return nil, core.NewInvalidRequestError("too many tool calls in message", nil)
	}

	blocks := make([]anthropicContentBlock, 0, len(prependBlocks)+len(msg.ToolCalls)+1)

	// Thinking blocks must come first.
	blocks = append(blocks, prependBlocks...)

	// Then text / image content from msg.Content (skipping any thinking blocks
	// that may already be in content from convertMessageContentToAnthropic, to
	// avoid duplication — they are already in prependBlocks).
	switch c := content.(type) {
	case string:
		if strings.TrimSpace(c) != "" {
			blocks = append(blocks, anthropicContentBlock{
				Type: "text",
				Text: c,
			})
		}
	case []anthropicContentBlock:
		for _, b := range c {
			if b.Type == "thinking" || b.Type == "redacted_thinking" {
				// Already prepended from ExtraFields; skip duplicates from content.
				if len(prependBlocks) > 0 {
					continue
				}
			}
			blocks = append(blocks, b)
		}
	}

	// Tool_use blocks last.
	for _, toolCall := range msg.ToolCalls {
		toolCallID := providers.ResponsesFunctionCallCallID(strings.TrimSpace(toolCall.ID))
		toolName := strings.TrimSpace(toolCall.Function.Name)
		if toolName == "" {
			return nil, core.NewInvalidRequestError("tool_call.function.name is required", nil)
		}
		input, err := parseToolCallArguments(toolCall.Function.Arguments)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    toolCallID,
			Name:  toolName,
			Input: input,
		})
	}
	return blocks, nil
}

// stripAnthropicPrefix 从模型名中剥离 "anthropic/" 前缀（大小写不敏感）。
// 例如 "ANTHROPIC/claude-sonnet-4-6" → "claude-sonnet-4-6"
// 如果不带前缀或前缀不匹配，返回原名。
func stripAnthropicPrefix(model string) string {
	if idx := strings.Index(model, "/"); idx > 0 {
		prefix := model[:idx]
		if strings.EqualFold(prefix, "anthropic") {
			return model[idx+1:]
		}
	}
	return model
}

// convertToAnthropicRequest converts core.ChatRequest to Anthropic format.
func convertToAnthropicRequest(req *core.ChatRequest) (*anthropicRequest, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("anthropic chat request is required", nil)
	}

	// 兼容路由表传递的 "{provider}/" 前缀格式（如 "ANTHROPIC/claude-sonnet-4-6"）。
	// Anthropic API 只需要 model ID 部分。
	modelName := stripAnthropicPrefix(req.Model)

	anthropicReq := &anthropicRequest{
		Model:         modelName,
		Messages:      make([]anthropicMessage, 0, len(req.Messages)),
		Temperature:   req.Temperature,
		Stream:        req.Stream,
		StopSequences: stopSequencesFromExtra(req.ExtraFields),
	}

	if req.MaxTokens != nil {
		anthropicReq.MaxTokens = *req.MaxTokens
	} else {
		anthropicReq.MaxTokens = resolveDefaultMaxTokens()
	}

	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		applyReasoning(anthropicReq, modelName, req.Reasoning)
	}

	tools, err := convertOpenAIToolsToAnthropic(req.Tools)
	if err != nil {
		return nil, err
	}
	anthropicReq.Tools = tools
	if toolChoice, disableTools, err := convertOpenAIToolChoiceToAnthropic(req.ToolChoice); err != nil {
		return nil, err
	} else if err := validateAnthropicToolChoice(toolChoice, anthropicReq.Tools, disableTools); err != nil {
		return nil, err
	} else if disableTools {
		anthropicReq.Tools = nil
	} else if len(anthropicReq.Tools) > 0 {
		if toolChoice == nil && req.ParallelToolCalls != nil && !*req.ParallelToolCalls {
			toolChoice = &anthropicToolChoice{Type: "auto"}
		}
		toolChoice = applyParallelToolCalls(toolChoice, req.ParallelToolCalls)
		anthropicReq.ToolChoice = toolChoice
	}

	// LiteLLM-style sanitization（始终开启）：补齐孤儿 tool_result、删除无效 tool、空 content 占位。
	messages := core.SanitizeToolMessages(req.Messages)

	// Convert OpenAI messages to Anthropic format.
	// Anthropic requires strict user/assistant alternation. Consecutive
	// tool messages must be merged into a single user message — otherwise
	// Anthropic rejects the request with:
	//   "tool_use ids were found without tool_result blocks immediately after"
	// Reference: LiteLLM does the same in anthropic_messages_pt().
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "system" {
			systemContent, err := buildAnthropicSystemContent(msg.Content)
			if err != nil {
				return nil, err
			}
			anthropicReq.System = appendAnthropicSystemContent(anthropicReq.System, systemContent)
			continue
		}

		if msg.Role == "tool" {
			// Merge consecutive tool messages into a single user message.
			var mergedBlocks []anthropicContentBlock
			for i < len(messages) && messages[i].Role == "tool" {
				content, err := buildAnthropicMessageContent(messages[i])
				if err != nil {
					return nil, normalizeAnthropicRequestError(err)
				}
				if blocks, ok := content.([]anthropicContentBlock); ok {
					mergedBlocks = append(mergedBlocks, blocks...)
				}
				i++
			}
			// Back up one — the outer loop's i++ will advance past the
			// last tool message.
			i--
			anthropicReq.Messages = append(anthropicReq.Messages, anthropicMessage{
				Role:    "user",
				Content: mergedBlocks,
			})
			continue
		}

		content, err := buildAnthropicMessageContent(msg)
		if err != nil {
			return nil, normalizeAnthropicRequestError(err)
		}
		role := msg.Role
		anthropicReq.Messages = append(anthropicReq.Messages, anthropicMessage{
			Role:    role,
			Content: content,
		})
	}

	// Anthropic API 限制整个请求最多 4 个 cache_control 断点。
	// 客户端（Claude Code 等）在长对话中可能标记超过 4 个，需要裁剪。
	trimExcessCacheControl(anthropicReq, 4)

	return anthropicReq, nil
}

// maxCacheControlBreakpoints is the Anthropic API limit on cache_control blocks
// across the entire request (system + tools + messages combined).
const maxCacheControlBreakpoints = 4

// cacheControlLocation identifies where a cache_control marker lives so we can
// selectively remove excess markers.
type cacheControlLocation struct {
	kind  int // 0=system, 1=tool, 2=message
	index int // index within the slice
	block *anthropicContentBlock
}

// trimExcessCacheControl ensures the request carries at most `limit` cache_control
// breakpoints. When the limit is exceeded, it removes markers with the following
// priority (lowest priority removed first):
//  1. Earlier message-level markers (keep the latest — most valuable for caching)
//  2. Tool-level markers
//  3. System-level markers (always kept — system prompts are large and stable)
func trimExcessCacheControl(req *anthropicRequest, limit int) {
	var locations []cacheControlLocation

	// Collect system cache_control locations.
	if blocks, ok := req.System.([]anthropicContentBlock); ok {
		for i := range blocks {
			if len(blocks[i].CacheControl) > 0 {
				locations = append(locations, cacheControlLocation{kind: 0, index: i, block: &blocks[i]})
			}
		}
	}

	// Collect message cache_control locations.
	for mi := range req.Messages {
		if blocks, ok := req.Messages[mi].Content.([]anthropicContentBlock); ok {
			for bi := range blocks {
				if len(blocks[bi].CacheControl) > 0 {
					locations = append(locations, cacheControlLocation{kind: 2, index: mi*10000 + bi, block: &blocks[bi]})
				}
			}
		}
	}

	if len(locations) <= limit {
		return
	}

	// Sort by removal priority: system (kind=0) is highest priority to keep,
	// messages (kind=2) with lower index are lowest priority to keep.
	// We remove from the front of the sorted list.
	// Strategy: keep all system markers; among message markers, keep the LAST ones.
	var systemLocs, msgLocs []cacheControlLocation
	for _, loc := range locations {
		switch loc.kind {
		case 0:
			systemLocs = append(systemLocs, loc)
		default:
			msgLocs = append(msgLocs, loc)
		}
	}

	// How many message markers can we keep?
	keepMsg := limit - len(systemLocs)
	if keepMsg < 0 {
		// Even system exceeds limit (shouldn't happen in practice); trim system too.
		keepMsg = 0
		// Remove excess system markers (keep the last ones).
		removeCount := len(systemLocs) - limit
		for i := 0; i < removeCount; i++ {
			systemLocs[i].block.CacheControl = nil
		}
		return
	}

	if len(msgLocs) <= keepMsg {
		return
	}

	// Remove the earliest message markers, keep the latest ones.
	removeCount := len(msgLocs) - keepMsg
	for i := 0; i < removeCount; i++ {
		msgLocs[i].block.CacheControl = nil
	}
}

// convertResponsesRequestToAnthropic converts a canonical Responses request by
// first mapping it onto shared chat semantics and then translating that semantic
// request into Anthropic's native message payload.
func convertResponsesRequestToAnthropic(req *core.ResponsesRequest) (*anthropicRequest, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("anthropic responses request is required", nil)
	}

	chatReq, err := providers.ConvertResponsesRequestToChat(req)
	if err != nil {
		return nil, err
	}
	return convertToAnthropicRequest(chatReq)
}

// convertDecodedBatchItemToAnthropic translates a canonical known batch item
// using the same semantic mapping path as normal chat and responses requests.
func convertDecodedBatchItemToAnthropic(decoded *core.DecodedBatchItemRequest) (*anthropicRequest, error) {
	if decoded == nil {
		return nil, core.NewInvalidRequestError("decoded anthropic batch request is required", nil)
	}

	return core.DispatchDecodedBatchItem(decoded, core.DecodedBatchItemHandlers[*anthropicRequest]{
		Chat: func(req *core.ChatRequest) (*anthropicRequest, error) {
			if req == nil {
				return nil, core.NewInvalidRequestError("anthropic chat request is required", nil)
			}
			if req.Stream {
				return nil, core.NewInvalidRequestError("streaming is not supported for native batch", nil)
			}
			params, err := convertToAnthropicRequest(req)
			if err != nil {
				return nil, err
			}
			params.Stream = false
			return params, nil
		},
		Responses: func(req *core.ResponsesRequest) (*anthropicRequest, error) {
			if req == nil {
				return nil, core.NewInvalidRequestError("anthropic responses request is required", nil)
			}
			if req.Stream {
				return nil, core.NewInvalidRequestError("streaming is not supported for native batch", nil)
			}
			params, err := convertResponsesRequestToAnthropic(req)
			if err != nil {
				return nil, err
			}
			params.Stream = false
			return params, nil
		},
		Embeddings: func(*core.EmbeddingRequest) (*anthropicRequest, error) {
			return nil, core.NewInvalidRequestError("anthropic does not support native embedding batches", nil)
		},
		Default: func(decoded *core.DecodedBatchItemRequest) (*anthropicRequest, error) {
			return nil, core.NewInvalidRequestError(fmt.Sprintf("unsupported anthropic batch url: %s", decoded.Endpoint), nil)
		},
	})
}

func appendAnthropicSystemText(existing, next string) string {
	if next == "" {
		return existing
	}
	if existing == "" {
		return next
	}
	return existing + "\n\n" + next
}

func appendAnthropicSystemContent(existing, next any) any {
	if isEmptyAnthropicSystemContent(next) {
		return existing
	}
	if isEmptyAnthropicSystemContent(existing) {
		return next
	}

	if existingText, ok := existing.(string); ok {
		if nextText, ok := next.(string); ok {
			return appendAnthropicSystemText(existingText, nextText)
		}
	}

	blocks := make([]anthropicContentBlock, 0)
	blocks = append(blocks, anthropicSystemBlocks(existing)...)
	blocks = append(blocks, anthropicSystemBlocks(next)...)
	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

func isEmptyAnthropicSystemContent(content any) bool {
	switch c := content.(type) {
	case nil:
		return true
	case string:
		return c == ""
	case []anthropicContentBlock:
		return len(c) == 0
	default:
		return false
	}
}

func anthropicSystemBlocks(content any) []anthropicContentBlock {
	switch c := content.(type) {
	case string:
		if c == "" {
			return nil
		}
		return []anthropicContentBlock{{Type: "text", Text: c}}
	case []anthropicContentBlock:
		return c
	default:
		return nil
	}
}

func buildAnthropicSystemContent(content any) (any, error) {
	if !core.HasStructuredContent(content) {
		text := core.ExtractTextContent(content)
		if text == "" {
			return nil, nil
		}
		return text, nil
	}

	parts, ok := core.NormalizeContentParts(content)
	if !ok {
		return nil, core.NewInvalidRequestError("unsupported anthropic chat content format", nil)
	}

	blocks := make([]anthropicContentBlock, 0, len(parts))
	hasCacheControl := false
	for _, part := range parts {
		if part.Type != "text" {
			return nil, core.NewInvalidRequestError("anthropic system messages only support text content", nil)
		}
		if part.Text == "" {
			continue
		}
		cacheControl, err := anthropicCacheControlFromExtra(part.ExtraFields)
		if err != nil {
			return nil, err
		}
		if len(cacheControl) > 0 {
			hasCacheControl = true
		}
		blocks = append(blocks, anthropicContentBlock{
			Type:         "text",
			Text:         part.Text,
			CacheControl: cacheControl,
		})
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if !hasCacheControl {
		return core.ExtractTextContent(parts), nil
	}
	return blocks, nil
}

func anthropicCacheControlFromExtra(extraFields core.UnknownJSONFields) (json.RawMessage, error) {
	raw := extraFields.Lookup("cache_control")
	if len(raw) == 0 {
		return nil, nil
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] != '{' {
		return nil, core.NewInvalidRequestError("anthropic cache_control must be an object", nil)
	}
	return core.CloneRawJSON(trimmed), nil
}

// stopSequencesFromExtra maps the OpenAI-compatible stop field (a string or an
// array of strings, carried in the request's extra fields) to Anthropic's
// stop_sequences. Empty or malformed values yield no sequences.
func stopSequencesFromExtra(extraFields core.UnknownJSONFields) []string {
	raw := bytes.TrimSpace(extraFields.Lookup("stop"))
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}

	switch raw[0] {
	case '"':
		var single string
		if err := json.Unmarshal(raw, &single); err != nil || single == "" {
			return nil
		}
		return []string{single}
	case '[':
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil
		}
		sequences := make([]string, 0, len(list))
		for _, item := range list {
			if item != "" {
				sequences = append(sequences, item)
			}
		}
		if len(sequences) == 0 {
			return nil
		}
		return sequences
	default:
		return nil
	}
}

func convertMessageContentToAnthropic(content any) (any, error) {
	if !core.HasStructuredContent(content) {
		return core.ExtractTextContent(content), nil
	}

	parts, ok := core.NormalizeContentParts(content)
	if !ok {
		return nil, core.NewInvalidRequestError("unsupported anthropic chat content format", nil)
	}

	blocks := make([]anthropicContentBlock, 0, len(parts))
	for _, part := range parts {
		cacheControl, err := anthropicCacheControlFromExtra(part.ExtraFields)
		if err != nil {
			return nil, err
		}
		switch part.Type {
		case "text":
			if part.Text == "" {
				continue
			}
			blocks = append(blocks, anthropicContentBlock{
				Type:         "text",
				Text:         part.Text,
				CacheControl: cacheControl,
			})
		case "thinking":
			// Multi-turn extended thinking: the client echoes back the assistant's
			// thinking block (including the encrypted signature) from the previous
			// turn. Anthropic requires these to be present and unmodified so it can
			// verify signature continuity. Drop silently if both thinking text and
			// signature are absent (degenerate block that would be rejected anyway).
			thinking := lookupStringFromExtraFields(part.ExtraFields, "thinking")
			signature := lookupStringFromExtraFields(part.ExtraFields, "signature")
			if thinking == "" && signature == "" && part.Text == "" {
				continue
			}
			if thinking == "" {
				thinking = part.Text // fallback: text field carries the thinking content
			}
			blocks = append(blocks, anthropicContentBlock{
				Type:      "thinking",
				Thinking:  thinking,
				Signature: signature,
			})
		case "redacted_thinking":
			// Redacted thinking blocks carry an encrypted `data` field that must be
			// passed back verbatim. Dropping it breaks the conversation chain.
			data := lookupStringFromExtraFields(part.ExtraFields, "data")
			if data == "" {
				continue
			}
			blocks = append(blocks, anthropicContentBlock{
				Type: "redacted_thinking",
				Data: data,
			})
		case "image_url":
			if part.ImageURL == nil || part.ImageURL.URL == "" {
				return nil, core.NewInvalidRequestError("anthropic image content requires image_url.url", nil)
			}
			source, err := anthropicImageSource(part.ImageURL.URL, part.ImageURL.MediaType)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, anthropicContentBlock{
				Type:         "image",
				Source:       source,
				CacheControl: cacheControl,
			})
		case "input_audio":
			return nil, core.NewInvalidRequestError("anthropic chat does not support input_audio content", nil)
		default:
			return nil, core.NewInvalidRequestError("unsupported anthropic chat content part type: "+part.Type, nil)
		}
	}
	if len(blocks) == 0 {
		return "", nil
	}
	return blocks, nil
}

func anthropicImageSource(raw, mediaTypeHint string) (*anthropicContentSource, error) {
	if strings.HasPrefix(raw, "data:") {
		comma := strings.IndexByte(raw, ',')
		if comma < 0 {
			return nil, core.NewInvalidRequestError("invalid anthropic image data URL", nil)
		}

		meta := raw[len("data:"):comma]
		tokens := strings.Split(meta, ";")
		if len(tokens) == 0 {
			return nil, core.NewInvalidRequestError("anthropic image data URL is missing a media type", nil)
		}

		mediaType := strings.TrimSpace(tokens[0])
		if mediaType == "" {
			mediaType = strings.TrimSpace(mediaTypeHint)
		}

		hasBase64 := false
		for _, token := range tokens[1:] {
			if strings.EqualFold(strings.TrimSpace(token), "base64") {
				hasBase64 = true
				break
			}
		}
		if !hasBase64 {
			return nil, core.NewInvalidRequestError("anthropic image data URL must be base64-encoded", nil)
		}

		if mediaType == "" {
			return nil, core.NewInvalidRequestError("anthropic image data URL is missing a media type", nil)
		}
		if !isAllowedAnthropicImageMediaType(mediaType) {
			return nil, core.NewInvalidRequestError("anthropic image media type is not supported: "+mediaType, nil)
		}

		data := raw[comma+1:]
		if data == "" {
			return nil, core.NewInvalidRequestError("anthropic image data URL is missing image data", nil)
		}

		return &anthropicContentSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      data,
		}, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, core.NewInvalidRequestError("anthropic chat image_url must be a data: URL or http/https URL", nil)
	}

	return &anthropicContentSource{
		Type: "url",
		URL:  raw,
	}, nil
}

func isAllowedAnthropicImageMediaType(mediaType string) bool {
	_, ok := allowedAnthropicImageMediaTypes[strings.ToLower(strings.TrimSpace(mediaType))]
	return ok
}

func normalizeAnthropicRequestError(err error) error {
	if gatewayErr, ok := err.(*core.GatewayError); ok {
		return gatewayErr
	}
	message := "invalid tool_call.function.arguments JSON"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return core.NewInvalidRequestError(message, err)
}

func validateAnthropicToolChoice(toolChoice *anthropicToolChoice, tools []anthropicTool, disableTools bool) error {
	if disableTools || toolChoice == nil || len(tools) > 0 {
		return nil
	}
	return core.NewInvalidRequestError("tool_choice requires at least one tool", nil)
}

func prefixAnthropicBatchItemError(index int, err error) error {
	if gatewayErr, ok := errors.AsType[*core.GatewayError](err); ok {
		prefixed := *gatewayErr
		prefixed.Message = fmt.Sprintf("batch item %d: %s", index, gatewayErr.Message)
		return &prefixed
	}
	return core.NewInvalidRequestError(fmt.Sprintf("batch item %d: %v", index, err), err)
}

// lookupStringFromExtraFields extracts a plain string value from ExtraFields by key.
// Returns "" if the key is absent or the value is not a JSON string.
func lookupStringFromExtraFields(fields core.UnknownJSONFields, key string) string {
	raw := fields.Lookup(key)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
