package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// convertFromAnthropicResponse converts Anthropic response to core.ChatResponse
func convertFromAnthropicResponse(resp *anthropicResponse) *core.ChatResponse {
	content := extractTextContent(resp.Content)
	thinking := extractThinkingContent(resp.Content)
	toolCalls := extractToolCalls(resp.Content)

	finishReason := normalizeAnthropicStopReason(resp.StopReason)
	if finishReason == "" {
		finishReason = "stop"
	}

	usage := core.Usage{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}

	rawUsage := buildAnthropicRawUsage(resp.Usage)
	if len(rawUsage) > 0 {
		usage.RawUsage = rawUsage
	}

	msg := core.ResponseMessage{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}

	// Build ExtraFields with both reasoning_content (plain text for display)
	// and thinking_blocks (full structures with signatures for multi-turn passback).
	//
	// Claude Code needs thinking_blocks to include the encrypted `signature` field so
	// that Anthropic can verify continuity on the next request. Without it, multi-turn
	// extended-thinking conversations fail with "signature mismatch" errors.
	extraMap := make(map[string]json.RawMessage)

	if thinking != "" {
		if raw, err := json.Marshal(thinking); err == nil {
			extraMap["reasoning_content"] = raw
		}
	}

	if blocks := extractThinkingBlocks(resp.Content); len(blocks) > 0 {
		if raw, err := json.Marshal(blocks); err == nil {
			extraMap["thinking_blocks"] = raw
		}
	}

	if len(extraMap) > 0 {
		msg.ExtraFields = core.UnknownJSONFieldsFromMap(extraMap)
	}

	return &core.ChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Model:   resp.Model,
		Created: time.Now().Unix(),
		Choices: []core.Choice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: usage,
	}
}

// ChatCompletion sends a chat completion request to Anthropic
func (p *Provider) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	anthropicReq, err := convertToAnthropicRequest(req)
	if err != nil {
		return nil, err
	}

	var anthropicResp anthropicResponse
	err = p.client.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/messages",
		Body:     anthropicReq,
	}, &anthropicResp)
	if err != nil {
		return nil, err
	}

	return convertFromAnthropicResponse(&anthropicResp), nil
}
