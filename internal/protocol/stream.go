package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
)

// DefaultConvOptions 返回 relaykit 转换所需的默认 Options。
func DefaultConvOptions() convmeta.Options {
	return convmeta.Options{}
}

// StreamAdapter 包装上游 OpenAI SSE 流，通过 relaykit 转换为 Anthropic SSE 输出。
type StreamAdapter struct {
	upstream io.ReadCloser
	reader   *io.PipeReader
	writer   *io.PipeWriter
	session  *Session
	wg       sync.WaitGroup
}

// NewStreamAdapter 创建一个流式转换适配器。
func NewStreamAdapter(upstream io.ReadCloser, session *Session, model string, estimatedInputTokens int, responseID string) io.ReadCloser {
	pr, pw := io.Pipe()
	adapter := &StreamAdapter{
		upstream: upstream,
		reader:   pr,
		writer:   pw,
		session:  session,
	}
	adapter.wg.Add(1)
	go adapter.convert(model, estimatedInputTokens, responseID)
	return adapter
}

// Read 实现 io.Reader。
func (a *StreamAdapter) Read(p []byte) (n int, err error) {
	return a.reader.Read(p)
}

// Close 实现 io.Closer。
func (a *StreamAdapter) Close() error {
	a.upstream.Close()
	err := a.reader.Close()
	a.wg.Wait()
	return err
}

// sseNewline 是 SSE 协议的事件分隔符常量。
const sseNewline = "\n"

// sseDoubleNewline 是 SSE 协议的事件终止符常量。
const sseDoubleNewline = "\n\n"

// convert 是核心转换 goroutine。
func (a *StreamAdapter) convert(model string, estimatedInputTokens int, responseID string) {
	defer a.wg.Done()
	defer a.writer.Close()

	state, err := relayconvert.NewResponseStreamState(
		a.session.TargetFormat,
		a.session.SourceFormat,
		relayconvert.ResponseStreamOptions{
			ID:      responseID,
			Model:   model,
			Created: 0,
		},
	)
	if err != nil {
		a.writer.CloseWithError(fmt.Errorf("stream adapter: create stream state: %w", err))
		return
	}

	info := &a.session.Values
	ctx := context.Background()
	sseReader := NewSSEReader(a.upstream, 0)

	for {
		frame, err := sseReader.ReadFrame()
		if err == io.EOF {
			a.finalizeAndFlush(ctx, info, state)
			return
		}
		if err != nil {
			a.writer.CloseWithError(fmt.Errorf("stream adapter: upstream read error: %w", err))
			return
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := json.Unmarshal(frame.Data, &chunk); err != nil {
			continue
		}

		// message_start / content_block_* / message_delta 全部由 relaykit 的
		// 状态机产出；这里再手写一次会造成重复事件（Claude 客户端会直接报错）。
		results, err := relayconvert.ConvertStreamResponseChunk(ctx, info, state, &chunk)
		if err != nil {
			a.writer.CloseWithError(fmt.Errorf("stream adapter: convert chunk: %w", err))
			return
		}

		for _, result := range results {
			a.emitResponseResult(result)
		}
	}
}

func (a *StreamAdapter) finalizeAndFlush(ctx context.Context, info convmeta.Meta, state *relayconvert.ResponseStreamState) {
	results, err := relayconvert.FinalizeStreamResponse(ctx, info, state)
	if err != nil {
		a.writer.CloseWithError(fmt.Errorf("stream adapter: finalize stream: %w", err))
		return
	}
	for _, result := range results {
		a.emitResponseResult(result)
	}
}

func (a *StreamAdapter) emitResponseResult(result relayconvert.ResponseResult) {
	if result.Value == nil {
		return
	}

	// relaykit 的 Anthropic 结果直接就是事件体；Responses 结果外面还套了一层
	// Type/Payload 结构（无 json tag）。统一在这里解包，否则客户端会收到
	// {"Type":...,"Payload":...} 这种不符合任何协议的壳。
	eventType, data := normalizeStreamEvent(json.Marshal, result.Value)
	if len(data) == 0 {
		return
	}

	var buf bytes.Buffer
	if eventType != "" {
		buf.WriteString("event: ")
		buf.WriteString(eventType)
		buf.WriteString(sseNewline)
	}
	buf.WriteString("data: ")
	buf.Write(data)
	buf.WriteString(sseDoubleNewline)
	a.writer.Write(buf.Bytes())
}

// emitMessageStart 在流开始时发出 Anthropic message_start 事件。
func (a *StreamAdapter) emitMessageStart(chunk dto.ChatCompletionsStreamResponse, model string, estimatedInputTokens int) {
	id := chunk.Id
	if id == "" {
		id = a.session.ID
	}
	chunkModel := chunk.Model
	if chunkModel == "" {
		chunkModel = model
	}

	inputTokens := estimatedInputTokens
	var cacheRead, cacheCreation int
	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			inputTokens = chunk.Usage.PromptTokens
		}
		cacheRead = chunk.Usage.PromptCacheHitTokens
	}

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
			"model":         chunkModel,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         usage,
		},
	}

	var buf bytes.Buffer
	b, _ := json.Marshal(msg)
	buf.WriteString("event: message_start")
	buf.WriteString(sseNewline)
	buf.WriteString("data: ")
	buf.Write(b)
	buf.WriteString(sseDoubleNewline)
	a.writer.Write(buf.Bytes())
}