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
	requestID       string
	model           string
	openBlockIndex  int  // current content block index
	hasTextBlock    bool // whether a text block has been started
	hasToolBlock    bool // whether at least one tool block exists
	textBlockOpen   bool // whether a text content block is currently open
	toolBlockOpen   bool // whether a tool content block is currently open
	currentToolID   string
	currentToolName string
	started         bool // whether message_start has been emitted
	finished        bool // whether the stream has ended
	mu              sync.Mutex
}

// newAnthropicStreamAdapter creates a new streaming adapter.
func newAnthropicStreamAdapter(upstream io.ReadCloser, requestID, model string) io.ReadCloser {
	pr, pw := io.Pipe()
	adapter := &anthropicStreamAdapter{
		upstream: upstream,
		reader:   pr,
		writer:   pw,
		state: &anthropicStreamState{
			requestID: requestID,
			model:     model,
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

	// If scanner error or EOF without [DONE], still flush
	if err := scanner.Err(); err != nil {
		a.state.mu.Lock()
		if !a.state.finished {
			a.state.finished = true
			a.state.mu.Unlock()
			a.closeAllBlocks()
			a.emitMessageDelta("end_turn", 0)
			a.emitRaw("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		} else {
			a.state.mu.Unlock()
		}
	} else {
		// EOF but no [DONE] — some providers don't send it
		a.flushAndFinish()
	}
}

// flushAndFinish closes all open blocks and emits final events.
func (a *anthropicStreamAdapter) flushAndFinish() {
	a.state.mu.Lock()
	if a.state.finished {
		a.state.mu.Unlock()
		return
	}
	a.state.finished = true
	a.state.mu.Unlock()

	a.closeAllBlocks()
	a.emitMessageDelta("end_turn", 0)
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
			Role      string                   `json:"role,omitempty"`
			Content   *string                  `json:"content,omitempty"`
			ToolCalls []openAIToolCallDelta    `json:"tool_calls,omitempty"`
		} `json:"delta"`
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices,omitempty"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
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

	// On first chunk with choices, emit message_start.
	if !a.state.started && len(chunk.Choices) > 0 {
		a.emitMessageStart(chunk)
	}

	if len(chunk.Choices) == 0 {
		// Handle usage-only final chunk (from stream_options include_usage)
		if chunk.Usage != nil {
			a.closeAllBlocks()
			a.emitMessageDelta("end_turn", chunk.Usage.CompletionTokens)
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

		// Handle finish reason
		if choice.FinishReason != "" && !a.state.finished {
			a.closeAllBlocks()
			outputTokens := 0
			if chunk.Usage != nil {
				outputTokens = chunk.Usage.CompletionTokens
			}
			stopReason := mapFinishReason(choice.FinishReason)
			a.emitMessageDelta(stopReason, outputTokens)
			a.emitRaw("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			a.state.finished = true
		}
	}

	// Handle standalone usage (no finish reason) — some providers send usage
	// in a separate chunk before [DONE].
	if chunk.Usage != nil && !a.state.finished {
		// Don't close blocks or emit message_stop here; wait for finish_reason or [DONE].
	}
}

// emitMessageStart writes the message_start event.
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

	msg := map[string]any{
		"type":        "message_start",
		"message": map[string]any{
			"id":         id,
			"type":       "message",
			"role":       "assistant",
			"content":    []any{},
			"model":      model,
			"stop_reason": nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
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
			"type":        "input_json_delta",
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

