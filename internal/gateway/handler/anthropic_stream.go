package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
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
	estimatedInputTokens int                     // 本地估算的 input_tokens（无真实 usage 时的备用值）
	finishReason         string                  // 记录映射后的 Anthropic stop_reason
	inputTokens          int                     // 末尾 usage 提供的真实 prompt_tokens（未缓存部分，累计，供 message_delta 覆盖估算）
	outputTokens         int                     // 末尾 usage 提供的 completion_tokens（累计）
	cacheReadTokens      int                     // 末尾 usage 提供的 cache_read_input_tokens（累计，取最大）
	cacheCreationTokens  int                     // 末尾 usage 提供的 cache_creation_input_tokens（累计，取最大）
	nextBlockIndex       int                     // 下一个可用的 content block index（块按需分配）
	textBlockOpen        bool                    // 文本块是否处于打开状态
	textBlockIndex       int                     // 当前文本块的 content block index
	toolBlocks           map[int]*toolBlockState // OpenAI tool_calls[].index → tool_use 块状态
	toolBlockOrder       []*toolBlockState       // 按首次出现顺序记录所有工具调用（收尾时遍历）
	started              bool                    // whether message_start has been emitted
	finished             bool                    // whether message_stop has been emitted
	mu                   sync.Mutex
}

// toolBlockState 跟踪单个 OpenAI tool_call（按 tool_calls[].index 归属）与它对应的
// Anthropic tool_use content block。
//
// 修复前的实现只有一份 currentToolID/currentToolName 状态，且"是不是新工具调用"
// 只按 id 是否为空判定，由此产生两个缺陷：
//  1. 与 id/name 同块到达的 function.arguments 被整块丢弃（生产事故：SGLang 把一次
//     调用的 id/name/全量参数放在同一个分块里，客户端拿到的 tool_use.input 永远是 {}）；
//  2. 并行工具调用的参数按"当前打开的块"落地而不看 index，会静默串到别的工具块里。
//
// 这里按 index 维护独立状态，让每个 arguments 片段都能回到自己的块。
type toolBlockState struct {
	index       int             // OpenAI 流里的 tool_calls[].index
	id          string          // 上游给的 tool_call id（缺失时开块兜底生成）
	name        string          // 上游给的 function name（可能晚于 id 到达）
	blockIndex  int             // 对应的 Anthropic content block index；-1 表示尚未开块
	pendingArgs strings.Builder // name 未到达时暂存的 arguments，等 name 到齐再开块补发
	closed      bool            // 是否已发出 content_block_stop
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
			toolBlocks:           map[int]*toolBlockState{},
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

	// 上游流中途出错（如 Bedrock ConverseStream 回了一个 validationException 事件，
	// SDK 会把它作为 Read 错误抛出）：必须把错误沿着 pipe 传给
	// HandleStreamResponseAs，由它写出 Anthropic 的 event: error。
	//
	// 修复前这里忽略 scanner.Err() 并照样 flushAndFinish()，等于给客户端发了一个
	// “正常结束、内容为空”的 message_stop，Claude Code 因此报
	// "Streaming response ended before any complete data was received" 并做一次
	// 非流式重试（重试才拿到真正的 400）。
	if err := scanner.Err(); err != nil {
		slog.Error("anthropic stream: upstream stream error",
			"request_id", a.state.requestID,
			"model", a.state.model,
			"error", err.Error(),
		)
		a.writer.CloseWithError(fmt.Errorf("upstream stream error: %w", err))
		return
	}

	// 正常结束（[DONE] 之外的 EOF）：补齐收尾事件。
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
	outputTokens := a.state.outputTokens
	inputTokens := a.state.inputTokens
	cacheRead := a.state.cacheReadTokens
	cacheCreation := a.state.cacheCreationTokens
	a.state.mu.Unlock()

	a.closeAllBlocks()
	stopReason := a.inferStopReason()
	a.emitMessageDelta(stopReason, outputTokens, inputTokens, cacheRead, cacheCreation)
	a.emitRaw("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}

// closeAllBlocks 按 content block index 升序关闭所有打开的内容块（文本块与工具块混排），
// 最后再把「name 始终未到、但参数已攒下」的调用补发出来。
//
// 工具块（可能并行多个）与文本块都保持打开到收尾，避免把续传参数关在块外；块的 index 在
// 打开时分配，收尾按升序发 stop 以满足官方 SDK 的顺序假设。补发留到最后，是因为只有真正
// 收尾时才能确定"没有 name 可等了"，提前发会得到 name 为空的半成品块。
func (a *anthropicStreamAdapter) closeAllBlocks() {
	var indexes []int
	if a.state.textBlockOpen {
		a.state.textBlockOpen = false
		indexes = append(indexes, a.state.textBlockIndex)
	}
	for _, slot := range a.state.toolBlockOrder {
		if !slot.closed && slot.blockIndex >= 0 {
			slot.closed = true
			indexes = append(indexes, slot.blockIndex)
		}
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		a.emitContentBlockStop(index)
	}

	var pending []*toolBlockState
	for _, slot := range a.state.toolBlockOrder {
		if !slot.closed && slot.blockIndex < 0 && slot.pendingArgs.Len() > 0 {
			pending = append(pending, slot)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].index < pending[j].index })
	for _, slot := range pending {
		slog.Warn("anthropic stream: tool_call never got a function name; emitting tool_use block with empty name",
			"request_id", a.state.requestID, "model", a.state.model,
			"tool_call_index", slot.index, "tool_id", slot.id)
		a.openToolBlock(slot)
		payload := slot.pendingArgs.String()
		slot.pendingArgs.Reset()
		a.emitInputJSONDelta(slot.blockIndex, payload)
		a.closeToolBlock(slot)
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
		// prompt_tokens_details.cached_tokens：OpenAI 兼容上游把缓存命中数放在
		// 这里。与非流式 ChatResponseToAnthropic 的读取来源对齐，作为 cache_read
		// 的首选来源（回退 cache_read_input_tokens）。
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
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

	// 累积末尾 usage 的 completion_tokens 与 cache 字段（usage 为累计值，取 max
	// 以兼容增量式上游）。cache 字段可能仅在末尾 usage-only chunk 才出现，需在
	// message_delta 时透传，避免流式晚到的非零 cache 被丢。
	if chunk.Usage != nil {
		if chunk.Usage.CompletionTokens > a.state.outputTokens {
			a.state.outputTokens = chunk.Usage.CompletionTokens
		}
		if chunk.Usage.PromptTokens > a.state.inputTokens {
			a.state.inputTokens = chunk.Usage.PromptTokens
		}
		// cache_read 首选 prompt_tokens_details.cached_tokens（与非流式
		// ChatResponseToAnthropic 同源），回退 cache_read_input_tokens，避免上游把
		// 命中数放在不同字段导致流式/非流式口径不一。
		cacheRead := chunk.Usage.CacheReadInputTokens
		if chunk.Usage.PromptTokensDetails != nil && chunk.Usage.PromptTokensDetails.CachedTokens > 0 {
			cacheRead = chunk.Usage.PromptTokensDetails.CachedTokens
		}
		if cacheRead > a.state.cacheReadTokens {
			a.state.cacheReadTokens = cacheRead
		}
		if chunk.Usage.CacheCreationInputTokens > a.state.cacheCreationTokens {
			a.state.cacheCreationTokens = chunk.Usage.CacheCreationInputTokens
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
			stopReason := a.inferStopReason()
			// 透传本 chunk（多为末尾 usage-only chunk）的真实非零 cache 字段到
			// message_delta.usage。权衡：这与 Anthropic 官方 message_delta 仅含
			// output_tokens 的口径略有偏离，但可避免流式晚到的非零 cache 被丢。
			a.emitMessageDelta(stopReason, a.state.outputTokens, a.state.inputTokens,
				a.state.cacheReadTokens, a.state.cacheCreationTokens)
			a.emitRaw("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			a.state.finished = true
		}
		return
	}

	// 多 choice（n > 1）：Anthropic 的一条 message 只能装一份内容，把多个 choice 的
	// delta 灌进同一份状态会让文本串台、并行工具调用的参数互相污染。因此只转换第一个
	// choice，其余丢弃并告警（带 choices 数量，便于发现上游没按预期置 n=1）。
	if len(chunk.Choices) > 1 {
		slog.Warn("anthropic stream: upstream returned multiple choices; converting only the first",
			"request_id", a.state.requestID, "model", a.state.model, "choices", len(chunk.Choices))
	}
	choice := chunk.Choices[0]
	{
		// message_stop 已发出（usage-only chunk 提前收尾）后不再开内容块：否则会出现
		// message_stop 之后的 content_block_start，客户端直接判协议错误。
		if a.state.finished {
			return
		}
		delta := choice.Delta
		content := delta.Content
		toolCalls := delta.ToolCalls

		// Handle content (text) deltas。文本块与工具块各自持有自己的 block index，文本 delta
		// 不能顺手把工具块关掉：否则"参数被文本打断"的流会在块关闭后丢掉尾部参数
		// （拼出非法 JSON）。多块并存，收尾时按 index 升序统一 close。
		if content != nil && *content != "" {
			if !a.state.textBlockOpen {
				a.startTextBlock()
			}
			a.emitTextDelta(*content)
		}

		// Handle tool call deltas。每个 tool_call 严格按 index 归属到自己的 tool_use 块。
		for _, tc := range toolCalls {
			a.handleToolCallDelta(tc)
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

// handleToolCallDelta 处理一个 tool_calls 分块。
//
// OpenAI 流式协议中，同一个 index 的工具调用在每个分块里给出的 function.arguments
// 都是待拼接的片段，**第一块也不例外**（OpenAI 官方 SDK 的累加器就是无条件 append；
// 官方服务习惯把首块参数置空，但协议并不要求）。因此首块携带的非空 arguments 必须
// 紧跟 content_block_start 作为第一个 input_json_delta 发出，否则客户端拿到的
// tool_use.input 只有 {}。
func (a *anthropicStreamAdapter) handleToolCallDelta(tc openAIToolCallDelta) {
	slot := a.state.toolBlocks[tc.Index]
	// 已关闭的块无法再追加 delta：同一 index 之后又来的数据只能属于一次新的调用
	// （上游复用 index，或干脆省略 index 恒为 0）。不重建状态就会把整次调用丢掉。
	if slot != nil && slot.closed {
		slot = nil
	}
	// 同一 index 换了新的 tool_call id：同样视为新的调用，先收尾旧块，避免两次调用的
	// 参数拼进同一个块。
	if slot != nil && slot.id != "" && tc.ID != "" && slot.id != tc.ID {
		a.closeToolBlock(slot)
		slot = nil
	}
	if slot == nil {
		slot = &toolBlockState{index: tc.Index, blockIndex: -1}
		a.state.toolBlocks[tc.Index] = slot
		a.state.toolBlockOrder = append(a.state.toolBlockOrder, slot)
	}

	if tc.ID != "" {
		slot.id = tc.ID
	}
	if tc.Function == nil {
		return
	}
	if tc.Function.Name != "" {
		slot.name = tc.Function.Name
	}

	args := tc.Function.Arguments

	// 块已经发过 content_block_stop：Anthropic 的块关闭后无法再追加 delta，只能丢弃
	// 并留下告警，绝不能把参数写进当时恰好打开着的别的块（那是静默串台）。
	if slot.closed {
		if args != "" {
			slog.Warn("anthropic stream: dropping tool_call arguments for a closed block",
				"request_id", a.state.requestID, "model", a.state.model,
				"tool_call_index", slot.index, "tool_id", slot.id, "tool_name", slot.name)
		}
		return
	}

	if slot.blockIndex < 0 {
		// 没有 name 就先攒参数：content_block_start 里缺 name 会让客户端无法把
		// tool_use 匹配到工具定义上。name 到齐后再开块并补发已攒下的片段。
		if slot.name == "" {
			slot.pendingArgs.WriteString(args)
			return
		}
		a.openToolBlock(slot)
	}

	if args == "" && slot.pendingArgs.Len() == 0 {
		return
	}
	payload := slot.pendingArgs.String() + args
	slot.pendingArgs.Reset()
	a.emitInputJSONDelta(slot.blockIndex, payload)
}

// openToolBlock 分配 content block index 并发出 tool_use 的 content_block_start。
func (a *anthropicStreamAdapter) openToolBlock(slot *toolBlockState) {
	// 文本块与 tool_use 块按协议互斥：开工具块前先给文本块收尾。
	a.closeTextBlock()

	slot.blockIndex = a.state.nextBlockIndex
	a.state.nextBlockIndex++

	toolID := slot.id
	if toolID == "" {
		toolID = fmt.Sprintf("toolu_%d", slot.blockIndex)
	}
	block := map[string]any{
		"type":  "content_block_start",
		"index": slot.blockIndex,
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    toolID,
			"name":  slot.name,
			"input": map[string]any{},
		},
	}
	b, _ := json.Marshal(block)
	a.emitRaw(fmt.Sprintf("event: content_block_start\ndata: %s\n\n", string(b)))
}

// closeTextBlock emits content_block_stop for the open text block, if any.
func (a *anthropicStreamAdapter) closeTextBlock() {
	if !a.state.textBlockOpen {
		return
	}
	a.emitContentBlockStop(a.state.textBlockIndex)
	a.state.textBlockOpen = false
}

// closeToolBlock emits content_block_stop for a tool block that was actually opened.
func (a *anthropicStreamAdapter) closeToolBlock(slot *toolBlockState) {
	if slot == nil || slot.closed {
		return
	}
	slot.closed = true
	if slot.blockIndex < 0 {
		return
	}
	a.emitContentBlockStop(slot.blockIndex)
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

	// 总是携带 cache_read_input_tokens / cache_creation_input_tokens（触发 chunk
	// 缺失时缺省 0），使客户端在 message_start 即能看到 cache 口径，不因值为 0 而漏字段。
	usage := map[string]any{
		"input_tokens":                inputTokens,
		"output_tokens":               0,
		"cache_read_input_tokens":     cacheRead,
		"cache_creation_input_tokens": cacheCreation,
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

// startTextBlock 开一个文本块（分配新的 content block index）。
func (a *anthropicStreamAdapter) startTextBlock() {
	if a.state.textBlockOpen {
		return
	}

	a.state.textBlockOpen = true
	a.state.textBlockIndex = a.state.nextBlockIndex
	a.state.nextBlockIndex++
	block := map[string]any{
		"type":  "content_block_start",
		"index": a.state.textBlockIndex,
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
		"index": a.state.textBlockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	}
	b, _ := json.Marshal(delta)
	line := fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// emitInputJSONDelta emits a content_block_delta for input_json on the given block.
func (a *anthropicStreamAdapter) emitInputJSONDelta(index int, partialJSON string) {
	delta := map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": partialJSON,
		},
	}
	b, _ := json.Marshal(delta)
	line := fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// emitContentBlockStop emits a content_block_stop event for the given block.
func (a *anthropicStreamAdapter) emitContentBlockStop(index int) {
	stop := map[string]any{
		"type":  "content_block_stop",
		"index": index,
	}
	b, _ := json.Marshal(stop)
	line := fmt.Sprintf("event: content_block_stop\ndata: %s\n\n", string(b))
	a.emitRaw(line)
}

// inferStopReason 在流结束（末尾 usage-only chunk / [DONE] / EOF）但上游从未给出
// finish_reason 时兜底推导 stop_reason：已经发出过 tool_use 块就只能是 tool_use，
// 否则 end_turn。避免出现"有 tool_use 块却报 end_turn"的协议自相矛盾（Anthropic 的
// 口径是只要有工具调用 stop_reason 就是 tool_use）。必须在 closeAllBlocks 之后调用，
// 让收尾阶段补发的工具块也被算进去。
func (a *anthropicStreamAdapter) inferStopReason() string {
	hasToolBlock := false
	for _, slot := range a.state.toolBlockOrder {
		if slot.blockIndex >= 0 {
			hasToolBlock = true
			break
		}
	}
	// 上游给了 finish_reason 就优先用它；但 "stop"/"content_filter" 会被映射成 end_turn，
	// 而流里已经发出 tool_use 块时，end_turn 会让 agent 循环认为"这一轮说完了"从而不执行
	// 工具（部分后端确实把 tool_calls 报成 stop）。这种情况改成 tool_use。
	// 保留 max_tokens：参数可能被截断，客户端应继续生成而不是执行半个工具调用。
	if a.state.finishReason != "" {
		if a.state.finishReason == "end_turn" && hasToolBlock {
			return "tool_use"
		}
		return a.state.finishReason
	}
	if hasToolBlock {
		return "tool_use"
	}
	return "end_turn"
}

// emitMessageDelta emits a message_delta event.
//
// inputTokens 覆盖 message_start 的估算占位：末尾 usage-only chunk 的真实
// input_tokens（未缓存部分）通过 message_delta 下发，借 SDK "overwrite when present,
// never add" 的合并语义把估算修正为真实值，避免与 cache_read 重复计费。
// cacheRead/cacheCreation 仅在 >0 时附带，避免产生空洞的 0 值字段。权衡：这与
// Anthropic 官方 message_delta.usage 仅含 output_tokens 的口径略有偏离，但可让
// 流式末尾（usage-only chunk / finish chunk）晚到的真实非零 cache 透传到客户端，
// 避免 cache 被丢。缺省 0 时保持官方口径（不输出 cache 字段）。
func (a *anthropicStreamAdapter) emitMessageDelta(stopReason string, outputTokens, inputTokens, cacheRead, cacheCreation int) {
	usage := map[string]any{
		"output_tokens": outputTokens,
	}
	if inputTokens > 0 {
		usage["input_tokens"] = inputTokens
	}
	if cacheRead > 0 {
		usage["cache_read_input_tokens"] = cacheRead
	}
	if cacheCreation > 0 {
		usage["cache_creation_input_tokens"] = cacheCreation
	}
	delta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usage,
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
