package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/streaming"
)

// StreamChatCompletion returns a raw response body for streaming (caller must close)
func (p *Provider) StreamChatCompletion(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	anthropicReq, err := convertToAnthropicRequest(req)
	if err != nil {
		return nil, err
	}
	anthropicReq.Stream = true

	stream, err := p.client.DoStream(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/messages",
		Body:     anthropicReq,
	})
	if err != nil {
		return nil, err
	}

	// Honour stream_options.include_usage: emit a standalone usage-only chunk
	// (choices:[]) before [DONE] so clients can reliably collect token counts.
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	return newStreamConverter(stream, req.Model, includeUsage), nil
}

// streamConverter wraps an Anthropic stream and converts it to OpenAI format
type streamConverter struct {
	reader            *bufio.Reader
	body              io.ReadCloser
	model             string
	msgID             string
	nextToolCallIndex int
	toolCalls         map[int]*streamToolCallState
	thinkingBlocks    map[int]bool // tracks which content block indices are thinking blocks
	// thinkingBlockStates accumulates the full thinking + signature for each
	// thinking block by content-block index. This is needed to reconstruct
	// thinking_blocks in the finish delta so Claude Code can pass them back
	// verbatim on the next turn (Anthropic verifies the signature).
	thinkingBlockStates map[int]*streamThinkingBlockState
	usage               anthropicUsage
	hasUsage            bool
	buffer              streaming.StreamBuffer
	closed              bool
	emittedToolCalls    bool
	// includeUsage controls whether a standalone usage-only chunk is emitted
	// after the finish chunk, conforming to the OpenAI stream_options.include_usage spec.
	includeUsage   bool
	sentUsageChunk bool
}

// streamThinkingBlockState accumulates thinking text and signature for one block.
type streamThinkingBlockState struct {
	thinking  strings.Builder
	signature string
}

type streamToolCallState struct {
	ID                string
	Name              string
	Arguments         strings.Builder
	Index             int
	Started           bool
	PlaceholderObject bool
}

func newStreamConverter(body io.ReadCloser, model string, includeUsage bool) *streamConverter {
	return &streamConverter{
		reader:              bufio.NewReader(body),
		body:                body,
		model:               model,
		toolCalls:           make(map[int]*streamToolCallState),
		thinkingBlocks:      make(map[int]bool),
		thinkingBlockStates: make(map[int]*streamThinkingBlockState),
		buffer:              streaming.NewStreamBuffer(1024),
		includeUsage:        includeUsage,
	}
}

func anthropicChatUsagePayload(usage *anthropicUsage) map[string]any {
	if usage == nil {
		return nil
	}

	payload := map[string]any{
		"prompt_tokens":     usage.InputTokens,
		"completion_tokens": usage.OutputTokens,
		"total_tokens":      usage.InputTokens + usage.OutputTokens,
	}
	if usage.CacheReadInputTokens > 0 {
		payload["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens > 0 {
		payload["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
	}
	return payload
}

func (sc *streamConverter) Read(p []byte) (n int, err error) {
	// If we have buffered data, return it first
	if sc.buffer.Len() > 0 {
		return sc.buffer.Read(p), nil
	}

	if sc.closed {
		sc.releaseBuffer()
		return 0, io.EOF
	}

	// Read the next SSE event from Anthropic
	for {
		line, err := sc.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				// When stream_options.include_usage is set, emit a standalone usage-only
				// chunk (choices:[]) before [DONE], matching the OpenAI specification.
				// This lets clients like Claude Code reliably collect token usage by
				// watching for the chunk where choices is empty and usage is non-null.
				if sc.includeUsage && !sc.sentUsageChunk && sc.hasUsage {
					sc.sentUsageChunk = true
					usageChunk := sc.formatUsageOnlyChunk()
					if usageChunk != "" {
						sc.buffer.AppendString(usageChunk)
					}
				}
				// Send final [DONE] message
				sc.buffer.AppendString("data: [DONE]\n\n")
				n = sc.buffer.Read(p)
				sc.closed = true
				_ = sc.body.Close() //nolint:errcheck
				return n, nil
			}
			return 0, err
		}

		n, handled, err := consumeAnthropicSSELine(p, line, sc.body, &sc.buffer, sc.convertEvent)
		if err != nil {
			sc.closed = true
			sc.releaseBuffer()
			return 0, err
		}
		if handled {
			if n == 0 {
				continue
			}
			return n, nil
		}
	}
}

func (sc *streamConverter) Close() error {
	if sc.closed {
		sc.releaseBuffer()
		return nil
	}
	sc.closed = true
	sc.releaseBuffer()
	return sc.body.Close()
}

func (sc *streamConverter) releaseBuffer() {
	sc.buffer.Release()
}

func (sc *streamConverter) mapStreamStopReason(reason string) string {
	// Preserve raw "tool_use" when the upstream stream never produced any
	// tool call deltas. This avoids claiming OpenAI-style tool calls for a
	// malformed or partial Anthropic stream.
	if reason == "tool_use" && !sc.emittedToolCalls {
		return reason
	}
	return normalizeAnthropicStopReason(reason)
}

func (sc *streamConverter) formatChatChunk(delta map[string]any, finishReason any, usage *anthropicUsage) string {
	chunk := map[string]any{
		"id":       sc.msgID,
		"object":   "chat.completion.chunk",
		"created":  time.Now().Unix(),
		"model":    sc.model,
		"provider": "anthropic",
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			},
		},
	}
	if usage != nil {
		chunk["usage"] = anthropicChatUsagePayload(usage)
	}

	jsonData, err := json.Marshal(chunk)
	if err != nil {
		slog.Error("failed to marshal chat completion chunk", "error", err, "msg_id", sc.msgID)
		return ""
	}

	return fmt.Sprintf("data: %s\n\n", jsonData)
}

// formatUsageOnlyChunk returns an OpenAI-spec usage-only SSE chunk:
//
//	{"choices":[], "usage":{...}}
//
// This is emitted as the last data frame before [DONE] when
// stream_options.include_usage is true, matching the OpenAI streaming contract.
func (sc *streamConverter) formatUsageOnlyChunk() string {
	if !sc.hasUsage {
		return ""
	}
	chunk := map[string]any{
		"id":       sc.msgID,
		"object":   "chat.completion.chunk",
		"created":  time.Now().Unix(),
		"model":    sc.model,
		"provider": "anthropic",
		"choices":  []any{},
		"usage":    anthropicChatUsagePayload(&sc.usage),
	}
	jsonData, err := json.Marshal(chunk)
	if err != nil {
		slog.Error("failed to marshal usage-only chunk", "error", err, "msg_id", sc.msgID)
		return ""
	}
	return fmt.Sprintf("data: %s\n\n", jsonData)
}

func (sc *streamConverter) convertEvent(event *anthropicStreamEvent) string {
	switch event.Type {
	case "message_start":
		role := ""
		if event.Message != nil {
			sc.msgID = event.Message.ID
			if mergeAnthropicUsage(&sc.usage, &event.Message.Usage) {
				sc.hasUsage = true
			}
			role = strings.TrimSpace(event.Message.Role)
		}
		if mergeAnthropicUsage(&sc.usage, event.Usage) {
			sc.hasUsage = true
		}
		if event.Message != nil {
			if role == "" {
				role = "assistant"
			}
			return sc.formatChatChunk(map[string]any{
				"role": role,
			}, nil, nil)
		}
		return ""

	case "content_block_start":
		if event.ContentBlock != nil && event.ContentBlock.Type == "thinking" {
			sc.thinkingBlocks[event.Index] = true
			sc.thinkingBlockStates[event.Index] = &streamThinkingBlockState{}
			return ""
		}
		if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
			state := &streamToolCallState{
				ID:    event.ContentBlock.ID,
				Name:  event.ContentBlock.Name,
				Index: sc.nextToolCallIndex,
			}
			sc.nextToolCallIndex++

			initialArguments := extractInitialToolArguments(event.ContentBlock.Input)
			state.PlaceholderObject = initialArguments == "{}"
			if state.PlaceholderObject {
				sc.toolCalls[event.Index] = state
				return ""
			}
			if initialArguments != "" {
				_, _ = state.Arguments.WriteString(initialArguments)
			}
			state.Started = true
			sc.toolCalls[event.Index] = state
			sc.emittedToolCalls = true

			return sc.formatChatChunk(map[string]any{
				"tool_calls": []map[string]any{
					{
						"index": state.Index,
						"id":    state.ID,
						"type":  "function",
						"function": map[string]any{
							"name":      state.Name,
							"arguments": initialArguments,
						},
					},
				},
			}, nil, nil)
		}
		return ""

	case "content_block_delta":
		if event.Delta == nil {
			return ""
		}

		switch event.Delta.Type {
		case "thinking_delta":
			if sc.thinkingBlocks[event.Index] && event.Delta.Thinking != "" {
				// Accumulate for thinking_blocks reconstruction at finish time.
				if state, ok := sc.thinkingBlockStates[event.Index]; ok {
					state.thinking.WriteString(event.Delta.Thinking)
				}
				return sc.formatChatChunk(map[string]any{
					"reasoning_content": event.Delta.Thinking,
				}, nil, nil)
			}
		case "signature_delta":
			// Store the signature so we can include it in thinking_blocks at finish.
			// The signature is Anthropic-internal and must be preserved verbatim for
			// multi-turn extended thinking — it is not forwarded as a delta to clients.
			if state, ok := sc.thinkingBlockStates[event.Index]; ok {
				state.signature = event.Delta.Signature
			}
			return ""
		case "text_delta":
			if event.Delta.Text != "" {
				return sc.formatChatChunk(map[string]any{
					"content": event.Delta.Text,
				}, nil, nil)
			}
		case "input_json_delta":
			if event.Delta.PartialJSON == "" {
				return ""
			}
			state := sc.toolCalls[event.Index]
			if state == nil {
				return ""
			}
			if state.PlaceholderObject {
				state.Arguments = strings.Builder{}
				state.PlaceholderObject = false
			}
			_, _ = state.Arguments.WriteString(event.Delta.PartialJSON)
			if !state.Started {
				state.Started = true
				sc.emittedToolCalls = true
				return sc.formatChatChunk(map[string]any{
					"tool_calls": []map[string]any{
						{
							"index": state.Index,
							"id":    state.ID,
							"type":  "function",
							"function": map[string]any{
								"name":      state.Name,
								"arguments": event.Delta.PartialJSON,
							},
						},
					},
				}, nil, nil)
			}
			sc.emittedToolCalls = true
			return sc.formatChatChunk(map[string]any{
				"tool_calls": []map[string]any{
					{
						"index": state.Index,
						"function": map[string]any{
							"arguments": event.Delta.PartialJSON,
						},
					},
				},
			}, nil, nil)
		}

	case "content_block_stop":
		state := sc.toolCalls[event.Index]
		if state != nil && !state.Started && state.PlaceholderObject {
			state.Started = true
			sc.emittedToolCalls = true
			return sc.formatChatChunk(map[string]any{
				"tool_calls": []map[string]any{
					{
						"index": state.Index,
						"id":    state.ID,
						"type":  "function",
						"function": map[string]any{
							"name":      state.Name,
							"arguments": "{}",
						},
					},
				},
			}, nil, nil)
		}
		return ""

	case "message_delta":
		if mergeAnthropicUsage(&sc.usage, event.Usage) {
			sc.hasUsage = true
		}
		// Emit chunk if we have stop_reason or usage data
		if (event.Delta != nil && event.Delta.StopReason != "") || event.Usage != nil {
			var finishReason any
			if event.Delta != nil && event.Delta.StopReason != "" {
				finishReason = sc.mapStreamStopReason(event.Delta.StopReason)
			}
			var usage *anthropicUsage
			if sc.hasUsage {
				usage = &sc.usage
			}
			// Build the finish delta, injecting thinking_blocks when present.
			// Clients (e.g. Claude Code) use thinking_blocks to echo back the
			// thinking content with its signature on the next request turn.
			finishDelta := map[string]any{}
			if blocks := sc.buildThinkingBlocksPayload(); len(blocks) > 0 {
				finishDelta["thinking_blocks"] = blocks
			}
			return sc.formatChatChunk(finishDelta, finishReason, usage)
		}

	case "message_stop":
		return ""
	}

	return ""
}

// buildThinkingBlocksPayload returns the accumulated thinking blocks as a slice
// of maps ready for JSON serialisation. The slice preserves the original content
// block order (ascending index). Blocks with neither thinking text nor signature
// are omitted. Returns nil when no thinking blocks were collected.
func (sc *streamConverter) buildThinkingBlocksPayload() []map[string]any {
	if len(sc.thinkingBlockStates) == 0 {
		return nil
	}

	// Collect indices in ascending order to preserve block ordering.
	indices := make([]int, 0, len(sc.thinkingBlockStates))
	for idx := range sc.thinkingBlockStates {
		indices = append(indices, idx)
	}
	// Simple insertion sort — block counts are tiny (usually 1–3).
	for i := 1; i < len(indices); i++ {
		for j := i; j > 0 && indices[j] < indices[j-1]; j-- {
			indices[j], indices[j-1] = indices[j-1], indices[j]
		}
	}

	var out []map[string]any
	for _, idx := range indices {
		state := sc.thinkingBlockStates[idx]
		thinking := state.thinking.String()
		if thinking == "" && state.signature == "" {
			continue
		}
		block := map[string]any{
			"type":      "thinking",
			"thinking":  thinking,
			"signature": state.signature,
		}
		out = append(out, block)
	}
	return out
}
