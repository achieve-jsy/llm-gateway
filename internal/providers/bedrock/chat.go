package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brdoc "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/core"
)

// ChatCompletion runs a Bedrock Converse call and normalizes the result into
// an OpenAI-compatible chat response.
func (p *Provider) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, core.NewInvalidRequestError("bedrock chat request is required", nil)
	}

	parts, err := buildConverseParts(req)
	if err != nil {
		return nil, err
	}

	out, err := p.runtime.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:         parts.modelID,
		Messages:        parts.messages,
		System:          parts.system,
		InferenceConfig: parts.infCfg,
		ToolConfig:      parts.toolCfg,
	})
	if err != nil {
		return nil, mapAWSError(err)
	}

	return convertConverseOutput(req.Model, out), nil
}

// converseParts holds the shared pieces of a Converse / ConverseStream request
// so the two API entry points can stay otherwise independent.
type converseParts struct {
	modelID  *string
	system   []brtypes.SystemContentBlock
	messages []brtypes.Message
	infCfg   *brtypes.InferenceConfiguration
	toolCfg  *brtypes.ToolConfiguration
}

// buildConverseParts translates a core.ChatRequest into the Bedrock Converse
// shape. Tool definitions and tool_choice are forwarded only for models that
// accept them; unsupported families will surface their own validation errors.
func buildConverseParts(req *core.ChatRequest) (converseParts, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return converseParts{}, core.NewInvalidRequestError("bedrock chat request requires a model", nil)
	}

	system, messages, err := convertMessages(req.Messages)
	if err != nil {
		return converseParts{}, err
	}

	parts := converseParts{
		modelID:  awssdk.String(model),
		messages: messages,
	}
	if len(system) > 0 {
		parts.system = system
	}

	infCfg := &brtypes.InferenceConfiguration{}
	hasInfCfg := false
	if mt := resolveMaxTokens(req); mt > 0 {
		if mt > math.MaxInt32 {
			return converseParts{}, core.NewInvalidRequestError(
				fmt.Sprintf("max_tokens %d exceeds the maximum of %d", mt, math.MaxInt32), nil)
		}
		infCfg.MaxTokens = awssdk.Int32(int32(mt))
		hasInfCfg = true
	}
	if req.Temperature != nil {
		infCfg.Temperature = awssdk.Float32(float32(*req.Temperature))
		hasInfCfg = true
	}
	if topP, ok := resolveTopP(req); ok {
		infCfg.TopP = awssdk.Float32(float32(topP))
		hasInfCfg = true
	}
	if hasInfCfg {
		parts.infCfg = infCfg
	}

	toolCfg, err := convertTools(req.Tools, req.ToolChoice)
	if err != nil {
		return converseParts{}, err
	}
	// Bedrock Converse requires toolConfig to be present whenever any message
	// contains toolUse or toolResult content blocks, with the actual tool
	// definitions. When the conversation history has tool blocks but the
	// caller did not provide tools, we inject a dummy tool to satisfy the
	// Bedrock API constraint (matching LiteLLM's add_dummy_tool approach).
	if toolCfg == nil && messagesContainToolBlocks(messages) {
		toolCfg = &brtypes.ToolConfiguration{
			Tools: []brtypes.Tool{
				&brtypes.ToolMemberToolSpec{
					Value: brtypes.ToolSpecification{
						Name:        awssdk.String("dummy_tool"),
						Description: awssdk.String("This is a dummy tool call"),
						InputSchema: &brtypes.ToolInputSchemaMemberJson{
							Value: toDocument(map[string]interface{}{
								"type":       "object",
								"properties": map[string]interface{}{},
							}),
						},
					},
				},
			},
		}
	}
	parts.toolCfg = toolCfg

	return parts, nil
}

func messagesContainToolBlocks(messages []brtypes.Message) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			switch block.(type) {
			case *brtypes.ContentBlockMemberToolUse, *brtypes.ContentBlockMemberToolResult:
				return true
			}
		}
	}
	return false
}

func convertMessages(messages []core.Message) ([]brtypes.SystemContentBlock, []brtypes.Message, error) {
	system := make([]brtypes.SystemContentBlock, 0)
	out := make([]brtypes.Message, 0, len(messages))

	appendOrMerge := func(role brtypes.ConversationRole, content []brtypes.ContentBlock) {
		if len(content) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, content...)
			return
		}
		out = append(out, brtypes.Message{Role: role, Content: content})
	}
	flushToolResults := func(blocks []brtypes.ContentBlock) []brtypes.ContentBlock {
		if len(blocks) == 0 {
			return nil
		}
		appendOrMerge(brtypes.ConversationRoleUser, blocks)
		return nil
	}

	var pendingToolResults []brtypes.ContentBlock
	for _, msg := range messages {
		if msg.Role != "tool" {
			pendingToolResults = flushToolResults(pendingToolResults)
		}
		switch msg.Role {
		case "system", "developer":
			sysBlocks := convertSystemContentBlocks(msg.Content)
			if len(sysBlocks) == 0 {
				continue
			}
			system = append(system, sysBlocks...)
		case "tool":
			block, err := convertToolResultMessage(msg)
			if err != nil {
				return nil, nil, err
			}
			pendingToolResults = append(pendingToolResults, block)
		case "user":
			blocks, err := convertUserContentBlocks(msg.Content)
			if err != nil {
				return nil, nil, err
			}
			appendOrMerge(brtypes.ConversationRoleUser, blocks)
		case "assistant":
			blocks, err := convertAssistantMessage(msg)
			if err != nil {
				return nil, nil, err
			}
			appendOrMerge(brtypes.ConversationRoleAssistant, blocks)
		default:
			return nil, nil, core.NewInvalidRequestError("unsupported message role: "+msg.Role, nil)
		}
	}
	flushToolResults(pendingToolResults)

	return system, out, nil
}

var imageDownloadClient = &http.Client{Timeout: 30 * time.Second}

// hasCacheControl reports whether a ContentPart carries cache_control in its ExtraFields.
// OpenCode and Claude Code attach {"type":"ephemeral"} to mark prompt-cache boundaries.
func hasCacheControl(part core.ContentPart) bool {
	return part.ExtraFields.Lookup("cache_control") != nil
}

// systemCacheMinChars 是触发自动 CachePoint 注入的 system 内容最小字符数。
// ~2000 chars ≈ 500 tokens，低于 Bedrock 最小缓存粒度 1024 token 的两倍，
// 对小 system prompt 不划算但也无害（只是多一点点 cache_write 费用）。
// 实践中 OpenCode/Claude Code 的 system prompt 通常 > 10 万字符，远超阈值。
const systemCacheMinChars = 2000

// convertSystemContentBlocks converts a system message content value into
// Bedrock SystemContentBlock slice.
//
// 自动注入策略：
//  1. 若客户端在某个 text part 上携带了 cache_control 标记，在该 block 后插入 CachePoint。
//  2. 若客户端未携带任何 cache_control（OpenAI 格式或旧 Anthropic 客户端），
//     且 system 内容总字符数 >= systemCacheMinChars，则在所有 system block 末尾
//     自动追加一个 CachePoint，缓存整段 system prompt。
func convertSystemContentBlocks(content any) []brtypes.SystemContentBlock {
	parts, ok := core.NormalizeContentParts(content)
	if !ok {
		text := core.ExtractTextContent(content)
		if text == "" {
			return nil
		}
		blocks := []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: text}}
		if len(text) >= systemCacheMinChars {
			blocks = append(blocks, &brtypes.SystemContentBlockMemberCachePoint{
				Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault},
			})
		}
		return blocks
	}

	blocks := make([]brtypes.SystemContentBlock, 0, len(parts)*2)
	clientHasCacheControl := false
	totalChars := 0
	for _, part := range parts {
		if part.Type != "text" && part.Type != "input_text" {
			continue
		}
		if part.Text == "" {
			continue
		}
		totalChars += len(part.Text)
		blocks = append(blocks, &brtypes.SystemContentBlockMemberText{Value: part.Text})
		if hasCacheControl(part) {
			clientHasCacheControl = true
			blocks = append(blocks, &brtypes.SystemContentBlockMemberCachePoint{
				Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault},
			})
		}
	}
	// 客户端未标记 cache_control 且内容足够大时，自动在末尾注入 CachePoint。
	if !clientHasCacheControl && totalChars >= systemCacheMinChars {
		blocks = append(blocks, &brtypes.SystemContentBlockMemberCachePoint{
			Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault},
		})
	}
	return blocks
}

func convertUserContentBlocks(content any) ([]brtypes.ContentBlock, error) {
	parts, ok := core.NormalizeContentParts(content)
	if !ok {
		text := core.ExtractTextContent(content)
		if text == "" {
			return nil, nil
		}
		return []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: text}}, nil
	}

	blocks := make([]brtypes.ContentBlock, 0, len(parts)*2)
	for _, part := range parts {
		switch part.Type {
		case "text", "input_text":
			if part.Text != "" {
				blocks = append(blocks, &brtypes.ContentBlockMemberText{Value: part.Text})
				if hasCacheControl(part) {
					blocks = append(blocks, &brtypes.ContentBlockMemberCachePoint{
						Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault},
					})
				}
			}
		case "image_url", "input_image":
			if part.ImageURL == nil || part.ImageURL.URL == "" {
				continue
			}
			imgBlock, err := convertImageBlock(part.ImageURL.URL, part.ImageURL.MediaType)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, &brtypes.ContentBlockMemberImage{Value: *imgBlock})
			if hasCacheControl(part) {
				blocks = append(blocks, &brtypes.ContentBlockMemberCachePoint{
					Value: brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault},
				})
			}
		}
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return blocks, nil
}

func convertImageBlock(raw, mediaTypeHint string) (*brtypes.ImageBlock, error) {
	if strings.HasPrefix(raw, "data:") {
		return convertDataURLImageBlock(raw, mediaTypeHint)
	}
	return convertURLImageBlock(raw)
}

func convertDataURLImageBlock(raw, mediaTypeHint string) (*brtypes.ImageBlock, error) {
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return nil, core.NewInvalidRequestError("bedrock image data URL is missing a comma", nil)
	}
	meta := raw[len("data:"):comma]
	tokens := strings.Split(meta, ";")
	if len(tokens) == 0 {
		return nil, core.NewInvalidRequestError("bedrock image data URL is missing a media type", nil)
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
		return nil, core.NewInvalidRequestError("bedrock image data URL must be base64-encoded", nil)
	}
	imgFormat, err := parseImageFormat(mediaType)
	if err != nil {
		return nil, err
	}
	data := raw[comma+1:]
	if data == "" {
		return nil, core.NewInvalidRequestError("bedrock image data URL is missing image data", nil)
	}
	bytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, core.NewInvalidRequestError("bedrock image data URL base64 decode failed: "+err.Error(), err)
	}
	return &brtypes.ImageBlock{
		Format: imgFormat,
		Source: &brtypes.ImageSourceMemberBytes{Value: bytes},
	}, nil
}

func convertURLImageBlock(rawURL string) (*brtypes.ImageBlock, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, core.NewInvalidRequestError("bedrock image_url must be a data: URL or http/https URL", nil)
	}
	resp, err := imageDownloadClient.Get(rawURL)
	if err != nil {
		return nil, core.NewInvalidRequestError("failed to download bedrock image: "+err.Error(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, core.NewInvalidRequestError(fmt.Sprintf("failed to download bedrock image: HTTP %d", resp.StatusCode), nil)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
	if err != nil {
		return nil, core.NewInvalidRequestError("failed to read bedrock image: "+err.Error(), err)
	}
	imgFormat := detectImageFormatFromContentType(resp.Header.Get("Content-Type"))
	return &brtypes.ImageBlock{
		Format: imgFormat,
		Source: &brtypes.ImageSourceMemberBytes{Value: body},
	}, nil
}

func parseImageFormat(mediaType string) (brtypes.ImageFormat, error) {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/jpeg", "image/jpg":
		return brtypes.ImageFormatJpeg, nil
	case "image/png":
		return brtypes.ImageFormatPng, nil
	case "image/gif":
		return brtypes.ImageFormatGif, nil
	case "image/webp":
		return brtypes.ImageFormatWebp, nil
	default:
		return "", core.NewInvalidRequestError("bedrock image media type is not supported: "+mediaType, nil)
	}
}

func detectImageFormatFromContentType(ct string) brtypes.ImageFormat {
	switch {
	case strings.Contains(ct, "image/png"):
		return brtypes.ImageFormatPng
	case strings.Contains(ct, "image/gif"):
		return brtypes.ImageFormatGif
	case strings.Contains(ct, "image/webp"):
		return brtypes.ImageFormatWebp
	default:
		return brtypes.ImageFormatJpeg
	}
}

func convertAssistantMessage(msg core.Message) ([]brtypes.ContentBlock, error) {
	blocks := make([]brtypes.ContentBlock, 0, 1+len(msg.ToolCalls))
	if text := core.ExtractTextContent(msg.Content); text != "" {
		blocks = append(blocks, &brtypes.ContentBlockMemberText{Value: text})
	}
	for _, call := range msg.ToolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			return nil, core.NewInvalidRequestError("assistant tool_call.function.name is required", nil)
		}
		toolID := strings.TrimSpace(call.ID)
		if toolID == "" {
			return nil, core.NewInvalidRequestError("assistant tool_call.id is required", nil)
		}
		input, err := decodeToolArgs(call.Function.Arguments)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, &brtypes.ContentBlockMemberToolUse{
			Value: brtypes.ToolUseBlock{
				Name:      awssdk.String(name),
				ToolUseId: awssdk.String(toolID),
				Input:     toDocument(input),
			},
		})
	}
	return blocks, nil
}

func convertToolResultMessage(msg core.Message) (brtypes.ContentBlock, error) {
	toolID := strings.TrimSpace(msg.ToolCallID)
	if toolID == "" {
		return nil, core.NewInvalidRequestError("tool message is missing tool_call_id", nil)
	}
	text := core.ExtractTextContent(msg.Content)
	content := []brtypes.ToolResultContentBlock{
		&brtypes.ToolResultContentBlockMemberText{Value: text},
	}
	return &brtypes.ContentBlockMemberToolResult{
		Value: brtypes.ToolResultBlock{
			ToolUseId: awssdk.String(toolID),
			Content:   content,
		},
	}, nil
}

func convertTools(tools []map[string]any, toolChoice any) (*brtypes.ToolConfiguration, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	// Bedrock has no ToolChoiceMemberNone; passing tools with no choice would
	// let the model auto-call. Honor an explicit "none" by suppressing tools
	// entirely, matching OpenAI semantics.
	if isToolChoiceNone(toolChoice) {
		return nil, nil
	}
	out := &brtypes.ToolConfiguration{Tools: make([]brtypes.Tool, 0, len(tools))}
	for _, tool := range tools {
		toolType, _ := tool["type"].(string)
		if toolType != "function" {
			return nil, core.NewInvalidRequestError("unsupported tool type: "+toolType, nil)
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			return nil, core.NewInvalidRequestError("tool.function must be an object", nil)
		}
		name, _ := fn["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil, core.NewInvalidRequestError("tool.function.name is required", nil)
		}
		spec := brtypes.ToolSpecification{
			Name: awssdk.String(name),
		}
		if desc, _ := fn["description"].(string); desc != "" {
			spec.Description = awssdk.String(desc)
		}
		params, hasParams := fn["parameters"]
		if !hasParams || params == nil {
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		paramsObj, ok := params.(map[string]any)
		if !ok {
			return nil, core.NewInvalidRequestError("tool.function.parameters must be an object", nil)
		}
		spec.InputSchema = &brtypes.ToolInputSchemaMemberJson{Value: toDocument(paramsObj)}
		out.Tools = append(out.Tools, &brtypes.ToolMemberToolSpec{Value: spec})
	}

	choice, err := convertToolChoice(toolChoice)
	if err != nil {
		return nil, err
	}
	out.ToolChoice = choice
	return out, nil
}

func isToolChoiceNone(choice any) bool {
	switch c := choice.(type) {
	case string:
		return strings.TrimSpace(c) == "none"
	case map[string]any:
		t, _ := c["type"].(string)
		return t == "none"
	}
	return false
}

func convertToolChoice(choice any) (brtypes.ToolChoice, error) {
	switch c := choice.(type) {
	case nil:
		return nil, nil
	case string:
		switch strings.TrimSpace(c) {
		case "", "auto":
			return &brtypes.ToolChoiceMemberAuto{}, nil
		case "required":
			return &brtypes.ToolChoiceMemberAny{}, nil
		case "none":
			return nil, nil
		default:
			return nil, core.NewInvalidRequestError("unsupported tool_choice value: "+c, nil)
		}
	case map[string]any:
		t, _ := c["type"].(string)
		switch t {
		case "auto":
			return &brtypes.ToolChoiceMemberAuto{}, nil
		case "required", "any":
			return &brtypes.ToolChoiceMemberAny{}, nil
		case "function":
			fn, _ := c["function"].(map[string]any)
			name, _ := fn["name"].(string)
			if strings.TrimSpace(name) == "" {
				return nil, core.NewInvalidRequestError("tool_choice.function.name is required", nil)
			}
			return &brtypes.ToolChoiceMemberTool{
				Value: brtypes.SpecificToolChoice{Name: awssdk.String(name)},
			}, nil
		case "none":
			return nil, nil
		}
	}
	return nil, core.NewInvalidRequestError("unsupported tool_choice", nil)
}

// toDocument wraps an arbitrary Go value into the Bedrock document.Interface
// used by Converse for free-form JSON fields (tool input, tool schemas).
func toDocument(v any) brdoc.Interface {
	return brdoc.NewLazyDocument(v)
}

// resolveMaxTokens returns the caller-requested output token cap, falling back
// to OpenAI's max_completion_tokens (e.g. reasoning-model API style) when
// max_tokens is omitted. Returns 0 when neither is present so the caller can
// leave InferenceConfiguration.MaxTokens unset.
func resolveMaxTokens(req *core.ChatRequest) int {
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		return *req.MaxTokens
	}
	if raw := req.ExtraFields.Lookup("max_completion_tokens"); len(raw) > 0 {
		var n int
		if err := json.Unmarshal(raw, &n); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// resolveTopP extracts top_p from req.ExtraFields. core.ChatRequest does not
// surface top_p as a typed field, so we look it up in the catch-all map the
// JSON decoder populates for unknown OpenAI parameters.
func resolveTopP(req *core.ChatRequest) (float64, bool) {
	raw := req.ExtraFields.Lookup("top_p")
	if len(raw) == 0 {
		return 0, false
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}

func decodeToolArgs(raw string) (any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}, nil
	}
	var out any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, core.NewInvalidRequestError("tool_call.function.arguments must be valid JSON: "+err.Error(), err)
	}
	return out, nil
}

func convertConverseOutput(model string, out *bedrockruntime.ConverseOutput) *core.ChatResponse {
	resp := &core.ChatResponse{
		ID:       fmt.Sprintf("bedrock-%d", time.Now().UnixNano()),
		Object:   "chat.completion",
		Model:    model,
		Provider: providerName,
		Created:  time.Now().Unix(),
	}

	msg := core.ResponseMessage{Role: "assistant"}
	var textParts []string

	if out.Output != nil {
		if mem, ok := out.Output.(*brtypes.ConverseOutputMemberMessage); ok {
			for _, block := range mem.Value.Content {
				switch b := block.(type) {
				case *brtypes.ContentBlockMemberText:
					if b.Value != "" {
						textParts = append(textParts, b.Value)
					}
				case *brtypes.ContentBlockMemberToolUse:
					call, err := convertToolUseToCall(b.Value)
					if err == nil {
						msg.ToolCalls = append(msg.ToolCalls, call)
					}
				}
			}
		}
	}
	msg.Content = strings.Join(textParts, "\n\n")

	resp.Choices = []core.Choice{{
		Index:        0,
		Message:      msg,
		FinishReason: mapStopReason(out.StopReason, len(msg.ToolCalls) > 0),
	}}

	if out.Usage != nil {
		resp.Usage = core.Usage{
			PromptTokens:     int(awssdk.ToInt32(out.Usage.InputTokens)),
			CompletionTokens: int(awssdk.ToInt32(out.Usage.OutputTokens)),
			TotalTokens:      int(awssdk.ToInt32(out.Usage.TotalTokens)),
		}
		if raw := bedrockUsageExtras(out.Usage); len(raw) > 0 {
			resp.Usage.RawUsage = raw
		}
	}

	return resp
}

func convertToolUseToCall(use brtypes.ToolUseBlock) (core.ToolCall, error) {
	args := "{}"
	if use.Input != nil {
		// MarshalSmithyDocument returns canonical JSON for the input value,
		// which is what the OpenAI tool_calls.function.arguments field expects.
		if buf, err := use.Input.MarshalSmithyDocument(); err == nil && len(buf) > 0 {
			args = string(buf)
		}
	}
	return core.ToolCall{
		ID:   awssdk.ToString(use.ToolUseId),
		Type: "function",
		Function: core.FunctionCall{
			Name:      awssdk.ToString(use.Name),
			Arguments: args,
		},
	}, nil
}

// mapStopReason normalizes Bedrock StopReason to OpenAI finish_reason values.
// hasToolCalls allows distinguishing a tool_use stop that produced calls
// (mapped to tool_calls) from a malformed stream that did not.
func mapStopReason(sr brtypes.StopReason, hasToolCalls bool) string {
	switch sr {
	case brtypes.StopReasonEndTurn, brtypes.StopReasonStopSequence:
		return "stop"
	case brtypes.StopReasonMaxTokens, brtypes.StopReasonModelContextWindowExceeded:
		return "length"
	case brtypes.StopReasonToolUse:
		if hasToolCalls {
			return "tool_calls"
		}
		return string(sr)
	case "":
		return "stop"
	default:
		return string(sr)
	}
}

func bedrockUsageExtras(u *brtypes.TokenUsage) map[string]any {
	if u == nil {
		return nil
	}
	out := make(map[string]any)
	if u.CacheReadInputTokens != nil {
		out["cache_read_input_tokens"] = int(*u.CacheReadInputTokens)
	}
	// 统一使用 cache_creation_input_tokens（与 Anthropic 约定一致），
	// 供 ExtractCostInput / stream_usage_observer 读取，避免键名不匹配漏算成本。
	if u.CacheWriteInputTokens != nil {
		out["cache_creation_input_tokens"] = int(*u.CacheWriteInputTokens)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
