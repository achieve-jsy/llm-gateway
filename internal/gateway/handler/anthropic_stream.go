package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// ──────────────────────────────────────────────
// AnthropicStreamAdapter converts OpenAI SSE chunks
// to Anthropic Messages API SSE events on the fly.
// ──────────────────────────────────────────────

// anthropicStreamAdapter wraps an OpenAI SSE io.ReadCloser and emits
// Anthropic-format SSE events by parsing each OpenAI chunk and converting
// deltas into content blocks.
type anthropicStreamAdapter struct {
	// upstream is the original OpenAI SSE stream (io.ReadCloser).
	upstream io.ReadCloser

	// reader reads from a pipe that we write Anthropic-format SSE into.
	reader *io.PipeReader
	writer *io.PipeWriter

	// wg waits for the conversion goroutine to finish.
	wg sync.WaitGroup

	// state tracks the streaming conversion state.
	state *anthropicStreamState
}

// anthropicStreamState tracks which content blocks are open and the
// current block index.
type anthropicStreamState struct {
	requestID            string
	model                string
	estimatedInputTokens int    // 本地估算的 input_tokens（无真实 usage 时的备用值）
	finishReason         string // 记录映射后的 Anthropic stop_reason
	outputTokens         int    // 末尾 usage 提供的 completion_tokens（累计）
	openBlockIndex       int    // current content block index
	hasTextBlock         bool   // whether a text block has been started
	hasToolBlock         bool   // whether at least one tool block exists
	textBlockOpen        bool   // whether a text content block is currently open
	toolBlockOpen        bool   // whether a tool content block is currently open
	currentToolID        string
	currentToolName      string
	started              bool // whether message_start has been emitted
	finished             bool // whether message_stop has been emitted
	mu                   sync.Mutex
}

// newAnthropicStreamAdapter creates a new streaming adapter.
//
// estimatedInputTokens 是消息请求的本地估算 input_tokens（CJK 感知 tokenizer），
// 在触发 chunk 未携带真实 prompt_tokens 时用作 message_start 的非零兜底值。
func newAnthropicStreamAdapter(upstream io.ReadCloser, requestID, model string, estimatedInputTokens int) io.ReadCloser {
	pr, pw := io.Pipe()
	adapter := &anthropicStreamAdapter{
		upstream: upstream,
		reader:   pr,
		writer:   pw,
		state: &anthropicStreamState{
			requestID:            requestID,
			model:                model,
			estimatedInputTokens: estimatedInputTokens,
		},
	}
	adapter.wg.Add(1)
	go adapter.convert()
	return adapter
}

// Read implements io.Reader.
func (a *anthropicStreamAdapter) Read(p []byte) (n int, err error) {
	return a.reader.Read(p)
}

// Close implements io.Closer. It closes the upstream and signals the pipe.
func (a *anthropicStreamAdapter) Close() error {
	a.upstream.Close()
	return a.reader.Close()
}

// convert reads OpenAI SSE chunks from the upstream, converts them to
// Anthropic SSE events, and writes them to the pipe.
func (a *anthropicStreamAdapter) convert() {
	defer a.wg.Done()
	defer a.writer.Close()

	scanner := bufio.NewScanner(a.upstream)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	// Buffer for accumulating SSE "data:" lines across multiple lines.
	var dataBuf bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and event-type lines (event: ...)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			continue
		}
		if strings.HasPrefix(line, ":") {
			// Comment lines
			continue
		}

		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(line[5:])
			if payload == "[DONE]" {
				a.flushAndFinish()
				return
			}
			dataBuf.WriteString(payload)
			// The data might continue on the next line if there's no newline after "data:".
			// But in our case, each SSE event is on a single "data:" line.
			// Check if we have a full JSON object.
			if !json.Valid(dataBuf.Bytes()) {
				continue
			}
			a.processChunk(dataBuf.Bytes())
			dataBuf.Reset()
		}
	}

	// If scanner error or EOF without [DONE], still flush (guarded idempotent).
	a.flushAndFinish()
}

// flushAndFinish closes all open blocks and emits final events. It is
// guarded by state.finished so any stream-termination path (usage-only chunk,
// [DONE], EOF, or scanner error) emits message_stop exactly once. It uses the
// recorded finish_reason (if any) and the trailing usage completion_tokens.
func (a *anthropicStreamAdapter) flushAndFinish() {
	a.state.mu.Lock()
	if a.state.finished {
		a.state.mu.Unlock()
		return
	}
	a.state.finished = true
	stopReason := a.state.finishReason
	outputTokens := a.state.outputTokens
	a.state.mu.Unlock()

	if stopReason == "" {
		stopReason = "end_turn"
	}
	a.closeAllBlocks()
	a.emitMessageDelta(stopReason, outputTokens)
	a.emitRaw("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}

// closeAllBlocks emits content_block_stop for any open text/tool blocks.
func (a *anthropicStreamAdapter) closeAllBlocks() {
	if a.state.textBlockOpen {
		a.emitContentBlockStop()
		a.state.textBlockOpen = false
	}
	if a.state.toolBlockOpen {
		a.emitContentBlockStop()
		a.state.toolBlockOpen = false
	}
}

// openAIChatChunk is the structure of a single OpenAI SSE chunk.
type openAIChatChunk struct {
	ID                string `json:"id,omitempty"`
	Object            string `json:"object,omitempty"`
	Model             string `json:"model,omitempty"`
	Created           int64  `json:"created,omitempty"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
	Choices           []struct {
		Delta struct {
			Role      string                `json:"role,omitempty"`
			Content   *string               `json:"content,omitempty"`
			ToolCalls []openAIToolCallDelta `json:"tool_calls,omitempty"`
		} `json:"delta"`
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices,omitempty"`
	Usage *struct {
		PromptTokens             int `json:"prompt_tokens"`
		CompletionTokens         int `json:"completion_tokens"`
		TotalTokens              int `json:"total_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage,omitempty"`
}

// openAIToolCallDelta represents a tool call delta in a streaming chunk.
type openAIToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function *struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

// processChunk parses an OpenAI chunk and emits corresponding Anthropic events.
func (a *anthropicStreamAdapter) processChunk(data []byte) {
	var chunk openAIChatChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		// If we can't parse it, forward raw as fallback? No — just skip.
		return
	}

	// 累积末尾 usage 的 completion_tokens（usage 为累计值，取 max 以兼容增量式上游）。
	if chunk.Usage != nil {
		if chunk.Usage.CompletionTokens > a.state.outputTokens {
			a.state.outputTokens = chunk.Usage.CompletionTokens
		}
	}

	// On first chunk with choices, emit message_start immediately to preserve
	// TTFB. The input_tokens uses the trigger chunk's real prompt_tokens when
	// available, otherwise the local estimate (non-zero).
	if !a.state.started && len(chunk.Choices) > 0 {
		a.emitMessageStart(chunk)
	}

	if len(chunk.Choices) == 0 {
		// Handle usage-only final chunk (from stream_options include_usage).
		// 用 finished 守卫：若 finish_reason 路径尚未发出 message_stop（即
		// state.finished==false），在此补发 message_delta + message_stop，并用本
		// chunk 的 completion_tokens 作为 output_tokens；若 message_stop 已发出
		//（例如 [DONE]/EOF 先到达或 finish_reason 已发射）则跳过，避免协议乱序重复。
		if chunk.Usage != nil && !a.state.finished {
			a.closeAllBlocks()
			stopReason := a.state.finishReason
			if stopReason == "" {
				stopReason = "end_turn"
			}
			a.emitMessageDelta(stopReason, chunk.Usage.CompletionTokens)
			a.emitRaw("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			a.state.finished = true
		}
		return
	}

	for _, choice := range chunk.Choices {
		delta := choice.Delta
		content := delta.Content
		toolCalls := delta.ToolCalls

		// Handle content (text) deltas
		if content != nil && *content != "" {
			if !a.state.textBlockOpen && !a.state.toolBlockOpen {
				// No text block open — start one (or close tool block first)
				a.startTextBlock()
			}
			// Emit text delta
			if a.state.textBlockOpen {
				a.emitTextDelta(*content)
			}
		}

		// Handle tool call deltas
		for _, tc := range toolCalls {
			if tc.ID != "" {
				// New tool call: close any open block, start a new tool block
				a.closeAllBlocks()
				a.state.currentToolID = tc.ID
				a.state.currentToolName = ""
				if tc.Function != nil {
					a.state.currentToolName = tc.Function.Name
				}
				a.startToolBlock()
			} else if tc.Function != nil {
				// Accumulating tool call arguments
				if tc.Function.Name != "" && a.state.currentToolName == "" {
					a.state.currentToolName = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					if a.state.toolBlockOpen {
						a.emitInputJSONDelta(tc.Function.Arguments)
					}
				}
			}
		}

		// Handle finish reason：只记录 stop_reason 并关闭内容块，不在此发射
		// message_delta / message_stop —— 统一由末尾 usage chunk（或 [DONE]/EOF）
		// 发射，确保 message_delta 使用末尾 usage 的 completion_tokens，且不会重复。
		if choice.FinishReason != "" && !a.state.finished {
			a.state.finishReason = mapFinishReason(choice.FinishReason)
			a.closeAllBlocks()
		}
	}
}

// emitMessageStart writes the message_start event, carrying the real
// input_tokens from the trigger chunk when available, otherwise the local
// estimate (never hardcoded 0).
func (a *anthropicStreamAdapter) emitMessageStart(chunk openAIChatChunk) {
	a.state.mu.Lock()
	if a.state.started {
		a.state.mu.Unlock()
		return
	}
	a.state.started = true
	a.state.mu.Unlock()

	id := chunk.ID
	if id == "" {
		id = a.state.requestID
	}
	model := chunk.Model
	if model == "" {
		model = a.state.model
	}

	// 优先用触发 chunk 的真实 prompt_tokens，否则用本地估算（非零）。
	inputTokens := a.state.estimatedInputTokens
	var cacheRead, cacheCreation int
	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			inputTokens = chunk.Usage.PromptTokens
		}
		cacheRead = chunk.Usage.CacheReadInputTokens
		cacheCreation = chunk.Usage.CacheCreationInputTokens
	}

	usage := map[string]any{
		"input_tokens":  inputTokens,
		"output_tokens": 0,
	}
	if cacheRead > 0 {
		usage["cache_read_input_tokens"] = cacheRead
	}
	if cacheCreation > 0 {
		usage["cache_creation_input_tokens"] = cacheCreation
	}

	msg := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            id,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         usage,
		},
	}
	b, _ := json.Marshal(msg)
	line := fmt.Sprintf("event: message_start\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// startTextBlock emits a content_block_start for a text block.
func (a *anthropicStreamAdapter) startTextBlock() {
	// If a text block is already open, close it first (shouldn't happen normally)
	if a.state.textBlockOpen {
		return
	}

	a.state.textBlockOpen = true
	a.state.hasTextBlock = true
	block := map[string]any{
		"type":  "content_block_start",
		"index": a.state.openBlockIndex,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	}
	b, _ := json.Marshal(block)
	line := fmt.Sprintf("event: content_block_start\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// emitTextDelta emits a content_block_delta for text.
func (a *anthropicStreamAdapter) emitTextDelta(text string) {
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": a.state.openBlockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	}
	b, _ := json.Marshal(delta)
	line := fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// startToolBlock emits a content_block_start for a tool_use block.
func (a *anthropicStreamAdapter) startToolBlock() {
	if a.state.toolBlockOpen {
		return
	}

	a.state.toolBlockOpen = true
	a.state.hasToolBlock = true

	toolID := a.state.currentToolID
	if toolID == "" {
		toolID = fmt.Sprintf("toolu_%d", a.state.openBlockIndex)
	}
	toolName := a.state.currentToolName

	block := map[string]any{
		"type":  "content_block_start",
		"index": a.state.openBlockIndex,
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    toolID,
			"name":  toolName,
			"input": map[string]any{},
		},
	}
	b, _ := json.Marshal(block)
	line := fmt.Sprintf("event: content_block_start\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// emitInputJSONDelta emits a content_block_delta for input_json.
func (a *anthropicStreamAdapter) emitInputJSONDelta(partialJSON string) {
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": a.state.openBlockIndex,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": partialJSON,
		},
	}
	b, _ := json.Marshal(delta)
	line := fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// emitContentBlockStop emits a content_block_stop event for the current block.
func (a *anthropicStreamAdapter) emitContentBlockStop() {
	stop := map[string]any{
		"type":  "content_block_stop",
		"index": a.state.openBlockIndex,
	}
	b, _ := json.Marshal(stop)
	line := fmt.Sprintf("event: content_block_stop\ndata: %s\n\n", string(b))
	a.emitRaw(line)
	a.state.openBlockIndex++
}

// emitMessageDelta emits a message_delta event.
func (a *anthropicStreamAdapter) emitMessageDelta(stopReason string, outputTokens int) {
	delta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	}
	b, _ := json.Marshal(delta)
	line := fmt.Sprintf("event: message_delta\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// emitRaw writes raw bytes to the pipe.
func (a *anthropicStreamAdapter) emitRaw(s string) {
	a.writer.Write([]byte(s))
}

// mapFinishReason converts an OpenAI finish reason to Anthropic format.
func mapFinishReason(reason string) string {
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

// AnthropicStreamResponse contains the conversion result for non-streaming
// responses, and is used as a type marker.
var _ io.ReadCloser = (*anthropicStreamAdapter)(nil)
