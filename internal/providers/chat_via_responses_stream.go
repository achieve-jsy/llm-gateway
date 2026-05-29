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
	case "response.function_call_arguments.delta":
		delta, _ := evt["delta"].(string)
		itemID, _ := evt["item_id"].(string)
		return c.toolArgsDeltaChunks(itemID, delta)
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
	itemID, _ := item["id"].(string)
	if itemID == "" {
		itemID, _ = item["call_id"].(string)
	}
	callID, _ := item["call_id"].(string)
	if callID == "" {
		callID = itemID
	}
	name, _ := item["name"].(string)
	idx := c.nextToolIdx
	c.nextToolIdx++
	c.toolIndex[itemID] = idx

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
				"arguments": "",
			},
		}},
	}, nil))
	return out
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

func (c *ResponsesToChatStreamConverter) completedChunks(evt map[string]any) []json.RawMessage {
	c.finished = true
	finishReason := "stop"
	var resp map[string]any
	if r, ok := evt["response"].(map[string]any); ok {
		resp = r
		if output, ok := resp["output"].([]any); ok {
			for _, item := range output {
				itemMap, _ := item.(map[string]any)
				if itemMap != nil && itemMap["type"] == "function_call" {
					finishReason = "tool_calls"
					break
				}
			}
		}
		if id, ok := resp["id"].(string); ok && id != "" {
			c.chatID = "chatcmpl-" + strings.TrimPrefix(id, "resp_")
		}
		if model, ok := resp["model"].(string); ok && model != "" {
			c.model = model
		}
	}

	out := []json.RawMessage{c.chunk(map[string]any{}, &finishReason)}
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
// completedChunks 与 failedChunks 共用：上游只在使用 include=["usage"] 时才回传 usage。
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
