package providers

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
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
	// toolAnnounced 记录某个 function_call 是否已向下游发过「身份帧」（id/name/type）。
	// 与 toolArgsSent 分开：没发 added 的流里参数可能先从 delta 落地，身份信息要等
	// output_item.done / completed 才拿得到，两个状态必须独立判断。
	toolAnnounced map[string]bool
	// toolArgsSent 记录某个 function_call（按归一化 key）是否已向下游发过
	// 真正的参数帧（delta 或完整参数）。用于在 output_item.done / completed
	// 回填参数时不重复下发，避免下游 arguments 翻倍。
	toolArgsSent map[string]bool
	nextToolIdx  int
}

// NewResponsesToChatStreamConverter creates a Responses→Chat SSE converter.
func NewResponsesToChatStreamConverter(reader io.ReadCloser, model string, includeUsage bool) *ResponsesToChatStreamConverter {
	return &ResponsesToChatStreamConverter{
		reader:        reader,
		model:         model,
		chatID:        "chatcmpl-" + uuid.New().String(),
		created:       time.Now().Unix(),
		includeUsage:  includeUsage,
		buffer:        streaming.NewStreamBuffer(4096),
		lineBuffer:    streaming.NewStreamBuffer(1024),
		toolIndex:     make(map[string]int),
		toolAnnounced: make(map[string]bool),
		toolArgsSent:  make(map[string]bool),
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
		return c.toolArgsDeltaChunks(evt)
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
	// id / call_id 都缺失的兼容实现只能靠 output_index 定位：宁可首帧 id 为空，
	// 也不能像改前那样直接 return nil 把 name 与首帧参数整块丢掉。
	key := responsesToolKey(itemID, callID, responsesOutputIndex(evt, item))
	if key == "" {
		return nil
	}
	name, _ := item["name"].(string)
	idx := c.nextToolIdx
	c.nextToolIdx++
	c.toolIndex[key] = idx
	c.toolAnnounced[key] = true

	// 首帧参数不能假定为空：OpenAI 官方形态下 added 的 arguments 恒为空串，
	// 正文随后由 response.function_call_arguments.delta 分片下发；但部分兼容
	// 实现（以及缓存重放的响应）会在 added 里就给出完整 JSON。若这里继续硬编码
	// ""，这些上游的参数会被永久丢弃、下游只能拿到空 arguments。
	// 空串时下发行为与过去完全一致（分片拼接不受影响）。
	initialArgs := responsesToolArguments(item)
	if initialArgs != "" {
		c.toolArgsSent[key] = true
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
	return c.emitToolArguments(item, responsesOutputIndex(evt, item))
}

// toolArgsDoneChunks 处理 response.function_call_arguments.done。
// 同类缺陷的第四个入口：上游也可能「只发 done、不发 delta」，把完整参数
// 一次性放在 arguments 字段里。此时该 tool_call 从没发过参数帧，需要补一帧；
// 正常走 delta 的情况下 toolArgsSent 已置位，不会重复下发。
// 真实 OpenAI 只带 item_id + arguments，但部分兼容实现会额外带 name / call_id，
// 这里一并透传，才能让下游拿到完整的 tool_call 身份。
func (c *ResponsesToChatStreamConverter) toolArgsDoneChunks(evt map[string]any) []json.RawMessage {
	item := map[string]any{}
	for _, k := range []string{"item_id", "call_id", "name", "arguments"} {
		if v, ok := evt[k]; ok {
			item[k] = v
		}
	}
	// arguments.* 事件用 item_id 引用同一个调用，映射成 id 交给统一的 key 归一化。
	if v, ok := item["item_id"]; ok {
		item["id"] = v
	}
	return c.emitToolArguments(item, responsesOutputIndex(evt, nil))
}

func (c *ResponsesToChatStreamConverter) toolArgsDeltaChunks(evt map[string]any) []json.RawMessage {
	delta, _ := evt["delta"].(string)
	if delta == "" {
		return nil
	}
	itemID, _ := evt["item_id"].(string)
	key := responsesToolKey(itemID, "", responsesOutputIndex(evt, nil))
	idx, ok := c.toolIndex[key]
	if !ok {
		idx = c.nextToolIdx
		c.nextToolIdx++
		c.toolIndex[key] = idx
	}
	c.toolArgsSent[key] = true
	// 参数分片帧不发 role 帧：这是既有行为，保持与 backfill 帧一致（见
	// emitToolArguments 的说明），避免给 delta 路径引入新分块。
	c.sentRole = true
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

// responsesToolKey 归一化「事件 / 输出 item → 内部 tool_call key」。
// 上游对同一调用的标识并不统一：added/done/completed 可能给 id、只给 call_id，
// 个别兼容实现两者都不给；arguments.delta 又用 item_id 引用。统一成
// 「id > call_id > output_index 合成」的 key，added / done / delta / completed
// 四处才会落到同一份状态上（否则 name、参数会被拆到两个 tool_call 里）。
func responsesToolKey(itemID, callID string, outputIndex int) string {
	if itemID != "" {
		return itemID
	}
	if callID != "" {
		return callID
	}
	if outputIndex >= 0 {
		return "output_index:" + strconv.Itoa(outputIndex)
	}
	return ""
}

// responsesOutputIndex 取事件的 output_index（取不到返回 -1）：优先事件顶层，
// 其次 item 内部（个别实现只把 output_index 放在 item 上）。
func responsesOutputIndex(evt, item map[string]any) int {
	for _, m := range []map[string]any{evt, item} {
		if m == nil {
			continue
		}
		switch v := m["output_index"].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return int(n)
			}
		}
	}
	return -1
}

// responsesToolArguments 取出 function_call item 的 arguments 文本。
//
// 两个兼容要点：
//  1. 裸 "{}" 是本仓库既有的「参数还没开始」占位约定（见 anthropic 通道的
//     PlaceholderObject）。若原样透传，后续 delta 会拼成 "{}{...}" 这种非法
//     JSON，因此这里按「无参数」返回空串、也不置 toolArgsSent；若最终确实没有
//     任何真参数，收尾时再补一个 "{}"。
//  2. 标准 Responses API 是 JSON 字符串，但少数兼容实现直接给对象/数组，
//     这里一并序列化成 JSON 文本，避免类型断言失败时参数被静默丢弃。
func responsesToolArguments(item map[string]any) string {
	switch v := item["arguments"].(type) {
	case string:
		if strings.TrimSpace(v) == "{}" {
			return ""
		}
		return strings.TrimSpace(v)
	case map[string]any, []any:
		body, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		if string(body) == "{}" {
			return ""
		}
		return string(body)
	}
	return ""
}

// emitToolArguments 为某个 tool_call 补参数帧（身份帧 / 参数帧，幂等）。
//
// 为什么需要它：上游给出 arguments 的位置并不统一 —— 可能在 added 里首帧即完整、
// 可能只在 output_item.done 里给、也可能只在 completed 的 response.output[*] 里给。
// 下游 Chat Completions 的 arguments 是「逐帧拼接」语义，所以每种形态都必须保证
// 恰好有一份参数落地；已下发过（delta 或完整参数）时不再补，否则参数会翻倍。
//
// 身份（id/name/type）与参数由两个独立状态跟踪：
//   - toolAnnounced：是否已发过身份帧；
//   - toolArgsSent：是否已发过参数帧。
//
// 于是「只有 delta、没有 added」的流也能在 completed 时补一帧身份（arguments 补空串），
// 反过来「只有 done 带参数」的流也不会因为缺 name 就把身份钉死成空值。
//
// 参数缺失或为空串时用 "{}" 收口：added 帧里已下发过 arguments: ""，拼上 "{}"
// 正好是合法 JSON（参考 anthropic 通道的 PlaceholderObject 思路）。
func (c *ResponsesToChatStreamConverter) emitToolArguments(item map[string]any, outputIndex int) []json.RawMessage {
	itemID, callID := responsesToolItemIdent(item)
	key := responsesToolKey(itemID, callID, outputIndex)
	if key == "" {
		return nil
	}
	name, _ := item["name"].(string)
	announced := c.toolAnnounced[key]
	argsSent := c.toolArgsSent[key]
	// 没有任何可补的东西：参数已发过，且身份要么已发过、要么这份 item 里也没带
	// name（等后续带 name 的事件再补，不要用空 name 把身份钉死）。
	if argsSent && (announced || name == "") {
		return nil
	}

	args := responsesToolArguments(item)
	switch {
	case argsSent:
		// 参数已经从 delta 分片发过，这一帧只补身份；arguments 必须补空串，
		// 否则下游拼接结果会翻倍。
		args = ""
	case args == "":
		args = "{}"
	}

	idx, ok := c.toolIndex[key]
	if !ok {
		idx = c.nextToolIdx
		c.nextToolIdx++
		c.toolIndex[key] = idx
	}
	fn := map[string]any{"arguments": args}
	toolCall := map[string]any{"index": idx, "function": fn}
	if !announced && name != "" {
		fn["name"] = name
		toolCall["id"] = callID
		toolCall["type"] = "function"
		c.toolAnnounced[key] = true
	}
	if !argsSent {
		c.toolArgsSent[key] = true
	}
	// 与 toolArgsDeltaChunks 一致：backfill 帧不发 role 帧（delta 路径从来不发），
	// 两条路径的行为必须一致，避免下游收到形态不同的分块序列。
	c.sentRole = true

	return []json.RawMessage{c.chunk(map[string]any{
		"tool_calls": []map[string]any{toolCall},
	}, nil)}
}

func (c *ResponsesToChatStreamConverter) completedChunks(evt map[string]any) []json.RawMessage {
	c.finished = true
	finishReason := "stop"
	var resp map[string]any
	var argsFrames []json.RawMessage
	if r, ok := evt["response"].(map[string]any); ok {
		resp = r
		if output, ok := resp["output"].([]any); ok {
			for pos, item := range output {
				itemMap, _ := item.(map[string]any)
				if itemMap == nil || itemMap["type"] != "function_call" {
					continue
				}
				finishReason = "tool_calls"
				// 有的上游（部分兼容实现/缓存重放）added 与 delta 都不给参数，
				// 完整 arguments 只出现在 response.output[*].arguments；也可能
				// 之前只发过 delta、从没发过 added（缺 id/name）。
				// 这里按「缺什么补什么」的幂等逻辑收口：参数没发过就补参数帧，
				// 身份没发过就补身份帧，都发过则跳过（避免参数翻倍 / 重复帧）。
				// pos 即该 item 的 output_index，用于 id/call_id 全缺时的兜底 key。
				argsFrames = append(argsFrames, c.emitToolArguments(itemMap, pos)...)
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
