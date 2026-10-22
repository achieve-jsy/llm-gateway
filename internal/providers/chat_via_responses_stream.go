package providers

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/example/llm-gateway/internal/streaming"
)

// ResponsesToChatStreamConverter wraps a Responses API SSE stream and converts
// it to OpenAI Chat Completions SSE (chat.completion.chunk + [DONE]).
type ResponsesToChatStreamConverter struct {
	reader       io.ReadCloser
	model        string
	chatID       string
	created      int64
	includeUsage bool
	buffer       streaming.StreamBuffer
	lineBuffer   streaming.StreamBuffer
	closed       bool
	sentRole     bool
	finished     bool
	toolIndex    map[string]int
	// toolArgsSent 记录某个 function_call（按归一化 item id）是否已向下游发过
	// 真正的参数帧（delta 或完整参数）。用于在 output_item.done / completed
	// 回填参数时不重复下发，避免下游 arguments 翻倍。
	toolArgsSent map[string]bool
	nextToolIdx  int
}

// NewResponsesToChatStreamConverter creates a Responses→Chat SSE converter.
func NewResponsesToChatStreamConverter(reader io.ReadCloser, model string, includeUsage bool) *ResponsesToChatStreamConverter {
	return &ResponsesToChatStreamConverter{
		reader:       reader,
		model:        model,
		chatID:       "chatcmpl-" + uuid.New().String(),
		created:      time.Now().Unix(),
		includeUsage: includeUsage,
		buffer:       streaming.NewStreamBuffer(4096),
		lineBuffer:   streaming.NewStreamBuffer(1024),
		toolIndex:    make(map[string]int),
		toolArgsSent: make(map[string]bool),
	}
}

func (c *ResponsesToChatStreamConverter) Read(p []byte) (int, error) {
	if c.closed {
		return 0, io.EOF
	}
	if c.buffer.Len() > 0 {
		return c.buffer.Read(p), nil
	}
	if c.finished {
		c.closed = true
		return 0, io.EOF
	}

	tempBuf := make([]byte, 1024)
	nr, readErr := c.reader.Read(tempBuf)
	if nr > 0 {
		c.lineBuffer.AppendBytes(tempBuf[:nr])
		for {
			unread := c.lineBuffer.Unread()
			idx := bytes.IndexByte(unread, '\n')
			if idx == -1 {
				break
			}
			line := bytes.TrimSpace(unread[:idx])
			c.lineBuffer.Consume(idx + 1)
			if len(line) == 0 {
				continue
			}
			data, ok := bytes.CutPrefix(line, []byte("data:"))
			if !ok {
				continue
			}
			data = bytes.TrimSpace(data)
			if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
				continue
			}
			for _, ev := range c.convertEvent(string(data)) {
				c.buffer.AppendString("data: ")
				c.buffer.AppendBytes(ev)
				c.buffer.AppendString("\n\n")
			}
			if c.finished {
				c.buffer.AppendString("data: [DONE]\n\n")
			}
		}
		if c.buffer.Len() > 0 {
			return c.buffer.Read(p), nil
		}
	}

	if readErr != nil {
		if readErr == io.EOF {
			if !c.finished {
				c.finished = true
				c.buffer.AppendString("data: [DONE]\n\n")
				if c.buffer.Len() > 0 {
					return c.buffer.Read(p), nil
				}
			}
			c.closed = true
			return 0, io.EOF
		}
		return 0, readErr
	}
	return 0, nil
}

func (c *ResponsesToChatStreamConverter) Close() error {
	c.closed = true
	if c.reader != nil {
		return c.reader.Close()
	}
	return nil
}

func (c *ResponsesToChatStreamConverter) convertEvent(payload string) []json.RawMessage {
	var evt map[string]any
	if err := json.Unmarshal([]byte(payload), &evt); err != nil {
		return nil
	}
	evtType, _ := evt["type"].(string)
	switch evtType {
	case "response.output_text.delta":
		delta, _ := evt["delta"].(string)
		return c.textDeltaChunks(delta)
	case "response.reasoning_summary_text.delta":
		delta, _ := evt["delta"].(string)
		return c.reasoningDeltaChunks(delta)
	case "response.output_item.added":
		return c.toolAddedChunks(evt)
	case "response.output_item.done":
		return c.toolDoneChunks(evt)
	case "response.function_call_arguments.delta":
		delta, _ := evt["delta"].(string)
		itemID, _ := evt["item_id"].(string)
		return c.toolArgsDeltaChunks(itemID, delta)
	case "response.function_call_arguments.done":
		return c.toolArgsDoneChunks(evt)
	case "response.completed":
		return c.completedChunks(evt)
	case "response.failed", "response.incomplete":
		return c.failedChunks(evt)
	default:
		return nil
	}
}

func (c *ResponsesToChatStreamConverter) textDeltaChunks(delta string) []json.RawMessage {
	out := make([]json.RawMessage, 0, 2)
	if !c.sentRole {
		c.sentRole = true
		out = append(out, c.chunk(map[string]any{
			"role":    "assistant",
			"content": "",
		}, nil))
	}
	if delta != "" {
		out = append(out, c.chunk(map[string]any{"content": delta}, nil))
	}
	return out
}

func (c *ResponsesToChatStreamConverter) reasoningDeltaChunks(delta string) []json.RawMessage {
	if delta == "" {
		return nil
	}
	out := make([]json.RawMessage, 0, 2)
	if !c.sentRole {
		c.sentRole = true
		out = append(out, c.chunk(map[string]any{
			"role": "assistant",
		}, nil))
	}
	out = append(out, c.chunk(map[string]any{"reasoning_content": delta}, nil))
	return out
}

func (c *ResponsesToChatStreamConverter) toolAddedChunks(evt map[string]any) []json.RawMessage {
	item, _ := evt["item"].(map[string]any)
	if item == nil {
		return nil
	}
	itemType, _ := item["type"].(string)
	if itemType != "function_call" {
		return nil
	}
	itemID, callID := responsesToolItemIdent(item)
	if itemID == "" {
		return nil
	}
	name, _ := item["name"].(string)
	idx := c.nextToolIdx
	c.nextToolIdx++
	c.toolIndex[itemID] = idx

	// 首帧参数不能假定为空：OpenAI 官方形态下 added 的 arguments 恒为空串，
	// 正文随后由 response.function_call_arguments.delta 分片下发；但部分兼容
	// 实现（以及缓存重放的响应）会在 added 里就给出完整 JSON。若这里继续硬编码
	// ""，这些上游的参数会被永久丢弃、下游只能拿到空 arguments。
	// 空串时下发行为与过去完全一致（分片拼接不受影响）。
	initialArgs := responsesToolArguments(item)
	if initialArgs != "" {
		c.toolArgsSent[itemID] = true
	}

	out := make([]json.RawMessage, 0, 2)
	if !c.sentRole {
		c.sentRole = true
		out = append(out, c.chunk(map[string]any{
			"role": "assistant",
		}, nil))
	}
	out = append(out, c.chunk(map[string]any{
		"tool_calls": []map[string]any{{
			"index": idx,
			"id":    callID,
			"type":  "function",
			"function": map[string]any{
				"name":      name,
				"arguments": initialArgs,
			},
		}},
	}, nil))
	return out
}

// toolDoneChunks 处理 response.output_item.done。
// 部分兼容实现（含缓存回放的响应）在 added 里给空 arguments，完整参数要到
// done 的 item.arguments 才给出；这里若该 tool_call 还没发过参数帧就补一帧。
func (c *ResponsesToChatStreamConverter) toolDoneChunks(evt map[string]any) []json.RawMessage {
	item, _ := evt["item"].(map[string]any)
	if item == nil {
		return nil
	}
	if itemType, _ := item["type"].(string); itemType != "function_call" {
		return nil
	}
	return c.emitToolArguments(item)
}

// toolArgsDoneChunks 处理 response.function_call_arguments.done。
// 同类缺陷的第四个入口：上游也可能「只发 done、不发 delta」，把完整参数
// 一次性放在 arguments 字段里。此时该 tool_call 从没发过参数帧，需要补一帧；
// 正常走 delta 的情况下 toolArgsSent 已置位，不会重复下发。
func (c *ResponsesToChatStreamConverter) toolArgsDoneChunks(evt map[string]any) []json.RawMessage {
	itemID, _ := evt["item_id"].(string)
	args, _ := evt["arguments"].(string)
	return c.emitToolArguments(map[string]any{"id": itemID, "arguments": args})
}

func (c *ResponsesToChatStreamConverter) toolArgsDeltaChunks(itemID, delta string) []json.RawMessage {
	if delta == "" {
		return nil
	}
	idx, ok := c.toolIndex[itemID]
	if !ok {
		idx = c.nextToolIdx
		c.nextToolIdx++
		c.toolIndex[itemID] = idx
	}
	c.toolArgsSent[itemID] = true
	if !c.sentRole {
		c.sentRole = true
	}
	return []json.RawMessage{c.chunk(map[string]any{
		"tool_calls": []map[string]any{{
			"index": idx,
			"function": map[string]any{
				"arguments": delta,
			},
		}},
	}, nil)}
}

// responsesToolItemIdent 归一化 function_call item 的标识。
// 同一调用在 added / output_item.done / completed 三处可能只带 id、只带
// call_id 或两者都有，而 arguments.delta 用 item_id 引用同一个调用；
// 统一按「id 优先、缺失退回 call_id」做 key，三处才对得上同一份状态。
func responsesToolItemIdent(item map[string]any) (itemID, callID string) {
	itemID, _ = item["id"].(string)
	callID, _ = item["call_id"].(string)
	if itemID == "" {
		itemID = callID
	}
	if callID == "" {
		callID = itemID
	}
	return itemID, callID
}

// responsesToolArguments 取出 function_call item 的完整参数文本。
// 标准 Responses API 是 JSON 字符串；少数兼容实现直接给对象/数组，
// 这里一并序列化成 JSON 文本，避免类型断言失败时参数被静默丢弃。
func responsesToolArguments(item map[string]any) string {
	switch v := item["arguments"].(type) {
	case string:
		return v
	case map[string]any, []any:
		if body, err := json.Marshal(v); err == nil {
			return string(body)
		}
	}
	return ""
}

// emitToolArguments 为某个 tool_call 补一帧完整参数（幂等：已发过参数帧则跳过）。
//
// 为什么需要它：上游给出 arguments 的位置并不统一 —— 可能在 added 里首帧即完整、
// 可能只在 output_item.done 里给、也可能只在 completed 的 response.output[*] 里给。
// 下游 Chat Completions 的 arguments 是「逐帧拼接」语义，所以每种形态都必须保证
// 恰好有一份参数落地；已下发过（delta 或完整参数）时不再补，否则参数会翻倍。
//
// 参数缺失或为空串时用 "{}" 收口：added 帧里已下发过 arguments: ""，拼上 "{}"
// 正好是合法 JSON（参考 anthropic 通道的 PlaceholderObject 思路，占位 {} 与真参数
// 靠 toolArgsSent 区分）。若该 tool_call 从未 announce 过（上游没发 added），
// 这一帧同时带上 id / name / type，让下游 SDK 能构造出完整调用。
func (c *ResponsesToChatStreamConverter) emitToolArguments(item map[string]any) []json.RawMessage {
	itemID, callID := responsesToolItemIdent(item)
	if itemID == "" || c.toolArgsSent[itemID] {
		return nil
	}
	args := responsesToolArguments(item)
	if args == "" {
		args = "{}"
	}

	idx, announced := c.toolIndex[itemID]
	if !announced {
		idx = c.nextToolIdx
		c.nextToolIdx++
		c.toolIndex[itemID] = idx
	}
	fn := map[string]any{"arguments": args}
	toolCall := map[string]any{"index": idx, "function": fn}
	if !announced {
		name, _ := item["name"].(string)
		fn["name"] = name
		toolCall["id"] = callID
		toolCall["type"] = "function"
	}
	c.toolArgsSent[itemID] = true

	out := make([]json.RawMessage, 0, 2)
	if !c.sentRole {
		c.sentRole = true
		out = append(out, c.chunk(map[string]any{
			"role": "assistant",
		}, nil))
	}
	return append(out, c.chunk(map[string]any{
		"tool_calls": []map[string]any{toolCall},
	}, nil))
}

func (c *ResponsesToChatStreamConverter) completedChunks(evt map[string]any) []json.RawMessage {
	c.finished = true
	finishReason := "stop"
	var resp map[string]any
	var argsFrames []json.RawMessage
	if r, ok := evt["response"].(map[string]any); ok {
		resp = r
		if output, ok := resp["output"].([]any); ok {
			for _, item := range output {
				itemMap, _ := item.(map[string]any)
				if itemMap == nil || itemMap["type"] != "function_call" {
					continue
				}
				finishReason = "tool_calls"
				// 有的上游（部分兼容实现/缓存重放）added 与 delta 都不给参数，
				// 完整 arguments 只出现在 response.output[*].arguments。
				// 对「从未发过参数帧」的 tool_call 在这里补一帧，否则下游永远拿到空参数；
				// 已有参数帧（含 delta 分片）的不再重复下发，避免参数翻倍。
				argsFrames = append(argsFrames, c.emitToolArguments(itemMap)...)
			}
		}
		if id, ok := resp["id"].(string); ok && id != "" {
			c.chatID = "chatcmpl-" + strings.TrimPrefix(id, "resp_")
		}
		if model, ok := resp["model"].(string); ok && model != "" {
			c.model = model
		}
	}
	// 兜底：参数只从 output_item.done / function_call_arguments.done 来过、
	// completed 的 output 里没有 function_call 时，仍应按 tool_calls 收尾。
	if finishReason == "stop" && len(c.toolIndex) > 0 {
		finishReason = "tool_calls"
	}

	out := make([]json.RawMessage, 0, len(argsFrames)+2)
	out = append(out, argsFrames...)
	out = append(out, c.chunk(map[string]any{}, &finishReason))
	out = append(out, c.usageChunksFromResp(resp)...)
	return out
}

// failedChunks 处理上游 response.failed / response.incomplete 事件：
// 流式响应在 200 之后失败时，之前 convertEvent 的 default 分支会静默丢弃，
// 导致客户端拿到空流且 usage_missing。这里显式结束流、按需发射 usage，
// 并发出独立 {"error":{...}} 事件（对齐 OpenAI 流式错误形状，SDK 会抛异常，
// observer 侧识别并记录 error_message）。
func (c *ResponsesToChatStreamConverter) failedChunks(evt map[string]any) []json.RawMessage {
	c.finished = true
	var resp map[string]any
	if r, ok := evt["response"].(map[string]any); ok {
		resp = r
		if id, ok := resp["id"].(string); ok && id != "" {
			c.chatID = "chatcmpl-" + strings.TrimPrefix(id, "resp_")
		}
		if model, ok := resp["model"].(string); ok && model != "" {
			c.model = model
		}
	}

	out := make([]json.RawMessage, 0, 2)
	out = append(out, c.usageChunksFromResp(resp)...)
	out = append(out, c.errorChunk(resp, evtTypeName(evt)))
	return out
}

func evtTypeName(evt map[string]any) string {
	t, _ := evt["type"].(string)
	return t
}

// errorChunk 构造 OpenAI 流式错误事件 {"error":{message,type,code,param}}。
// 优先取 response.error 的字段，缺失 message 时按 status / incomplete_details 拼兜底。
func (c *ResponsesToChatStreamConverter) errorChunk(resp map[string]any, evtType string) json.RawMessage {
	errObj := map[string]any{"type": "upstream_error"}
	if resp != nil {
		if e, ok := resp["error"].(map[string]any); ok {
			for _, k := range []string{"message", "code", "type", "param"} {
				if v, ok := e[k].(string); ok && v != "" {
					errObj[k] = v
				}
			}
		}
	}
	if _, ok := errObj["message"]; !ok {
		msg := "upstream responses stream ended with status " + evtType
		if resp != nil {
			if status, ok := resp["status"].(string); ok && status != "" {
				msg = "upstream responses stream ended with status " + status
			}
			if d, ok := resp["incomplete_details"].(map[string]any); ok {
				if reason, ok := d["reason"].(string); ok && reason != "" {
					msg += ": " + reason
				}
			}
		}
		errObj["message"] = msg
	}
	body, _ := json.Marshal(map[string]any{"error": errObj})
	return body
}

// usageChunksFromResp 按需发射 Chat Completions 格式的 usage-only chunk。
// completedChunks 与 failedChunks 共用；usage 来自 response.completed 事件的
// response.usage，仅在客户端请求 include_usage 时下发。
func (c *ResponsesToChatStreamConverter) usageChunksFromResp(resp map[string]any) []json.RawMessage {
	if !c.includeUsage || resp == nil {
		return nil
	}
	usage, ok := resp["usage"].(map[string]any)
	if !ok {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"id":      c.chatID,
		"object":  "chat.completion.chunk",
		"created": c.created,
		"model":   c.model,
		"choices": []map[string]any{},
		"usage":   chatUsageFromResponsesUsageMap(usage),
	})
	return []json.RawMessage{body}
}

// chatUsageFromResponsesUsageMap 将 Responses usage 映射为 Chat Completions usage。
// 必须透传 input_tokens_details.cached_tokens → prompt_tokens_details.cached_tokens，
// 否则 Azure/OpenAI 流式桥接落库会永远显示缓存命中率为 0。
func chatUsageFromResponsesUsageMap(usage map[string]any) map[string]any {
	if usage == nil {
		return map[string]any{}
	}
	chatUsage := map[string]any{
		"prompt_tokens":     usage["input_tokens"],
		"completion_tokens": usage["output_tokens"],
		"total_tokens":      usage["total_tokens"],
	}
	if details := firstMap(usage, "input_tokens_details", "prompt_tokens_details"); details != nil {
		chatUsage["prompt_tokens_details"] = details
	}
	if details := firstMap(usage, "output_tokens_details", "completion_tokens_details"); details != nil {
		chatUsage["completion_tokens_details"] = details
	}
	return chatUsage
}

func firstMap(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		if v, ok := m[k].(map[string]any); ok && v != nil {
			return v
		}
	}
	return nil
}

func (c *ResponsesToChatStreamConverter) chunk(delta map[string]any, finishReason *string) json.RawMessage {
	choice := map[string]any{
		"index": 0,
		"delta": delta,
	}
	if finishReason != nil {
		choice["finish_reason"] = *finishReason
	} else {
		choice["finish_reason"] = nil
	}
	body, _ := json.Marshal(map[string]any{
		"id":      c.chatID,
		"object":  "chat.completion.chunk",
		"created": c.created,
		"model":   c.model,
		"choices": []map[string]any{choice},
	})
	return body
}
