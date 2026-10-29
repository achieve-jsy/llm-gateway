package handler

// 评审补充的边界用例：文本打断参数、同 index 连续调用、finish_reason 与实际块不一致、
// 空参数工具、id 晚到等形态。断言方式与 anthropic_stream_test.go 的 helper 一致。

import (
	"strings"
	"testing"
)

// TestAnthropicStream_ToolArgs_TextInterruptsArguments 覆盖"参数被文本 delta 打断"的
// 形态（args → text → args，续传块不带 id）。修复过程中曾出现回归：文本 delta 把工具块
// 关掉，尾部参数落到了已关闭的块上被丢弃，拼出非法 JSON。
func TestAnthropicStream_ToolArgs_TextInterruptsArguments(t *testing.T) {
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_ls", "Bash", `{"comm`)),
		map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "OK"}}},
		},
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, `and": "ls"}`)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1; 事件序列 %v", len(blocks), eventTypes(events))
	}
	assertToolBlockSequenced(t, events, blocks[0])
	want := `{"command": "ls"}`
	assertToolBlockArgs(t, blocks[0], want, decodeJSONObject(t, "期望参数", want))

	var text strings.Builder
	for _, e := range events {
		if e["type"] != "content_block_delta" {
			continue
		}
		if d := eventDelta(e); d["type"] == "text_delta" {
			text.WriteString(d["text"].(string))
		}
	}
	if text.String() != "OK" {
		t.Errorf("文本 delta 拼接 = %q, want OK（参数被文本打断时文本也不能丢）", text.String())
	}
}

// TestAnthropicStream_ToolArgs_SameIndexNewIDAfterText 覆盖「文本打断后同 index 又来了
// 新的调用 id」：必须切成两个块。修复过程中曾出现回归：已关闭的 slot 被复用，第二个
// 调用整次消失。
func TestAnthropicStream_ToolArgs_SameIndexNewIDAfterText(t *testing.T) {
	const argsA = `{"a": 1}`
	const argsB = `{"b": 2}`
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_a", "ToolA", argsA)),
		map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "next"}}},
		},
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_b", "ToolB", argsB)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 2 {
		t.Fatalf("tool_use 块数量 = %d, want 2（同 index 的新 id 必须另开块）; 事件序列 %v",
			len(blocks), eventTypes(events))
	}
	a := toolBlockByID(t, blocks, "call_a")
	b := toolBlockByID(t, blocks, "call_b")
	assertToolBlockSequenced(t, events, a)
	assertToolBlockSequenced(t, events, b)
	assertToolBlockArgs(t, a, argsA, decodeJSONObject(t, "期望参数 A", argsA))
	assertToolBlockArgs(t, b, argsB, decodeJSONObject(t, "期望参数 B", argsB))
}

// TestAnthropicStream_ToolArgs_FinishReasonStopWithToolBlock 覆盖上游把 tool_calls 报成
// finish_reason="stop" 的后端：流里已经有 tool_use 块，stop_reason 必须是 tool_use，
// 否则 agent 循环会直接结束、工具永不执行。
func TestAnthropicStream_ToolArgs_FinishReasonStopWithToolBlock(t *testing.T) {
	const args = `{"command": "ls"}`
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_ls", "Bash", args)),
		finishChunk("stop"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	if _, stop := messageDeltaInfo(t, events); stop != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use（已有 tool_use 块时不能报 end_turn）", stop)
	}
}

// TestAnthropicStream_ToolArgs_TextAndToolCallsInSameChunk 覆盖单个 chunk 同时带文本与
// tool_calls 的形态：文本块占 index 0、工具块占 index 1，两者都完整。
// （JSON 对象的字段顺序不影响结果：processChunk 固定先处理 content 再处理 tool_calls。）
func TestAnthropicStream_ToolArgs_TextAndToolCallsInSameChunk(t *testing.T) {
	const args = `{"command": "ls"}`
	sse := openAISSE(t,
		map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
				"content":    "running",
				"tool_calls": []any{toolCallDeltaWithArgs(0, "call_ls", "Bash", args)},
			}}},
		},
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1; 事件序列 %v", len(blocks), eventTypes(events))
	}
	if blocks[0].Index != 1 {
		t.Errorf("tool_use 块 index = %d, want 1（文本块占用 0）", blocks[0].Index)
	}
	assertToolBlockSequenced(t, events, blocks[0])
	assertToolBlockArgs(t, blocks[0], args, decodeJSONObject(t, "期望参数", args))
}

// TestAnthropicStream_ToolArgs_SameIndexThreeSequentialCalls 覆盖同 index 连续三次调用
// （A→B→C，中间没有文本）：必须三块、参数不串，守住 closed-reuse 的修复。
func TestAnthropicStream_ToolArgs_SameIndexThreeSequentialCalls(t *testing.T) {
	args := []string{`{"n": 1}`, `{"n": 2}`, `{"n": 3}`}
	ids := []string{"call_1", "call_2", "call_3"}
	chunks := make([]map[string]any, 0, 3)
	for i := range ids {
		chunks = append(chunks, chunkWithToolCalls(toolCallDeltaWithArgs(0, ids[i], "Tool", args[i])))
	}
	chunks = append(chunks, finishChunk("tool_calls"))
	events := parseAnthropicEvents(t, feedAnthropicStream(t, openAISSE(t, chunks...), 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 3 {
		t.Fatalf("tool_use 块数量 = %d, want 3; 事件序列 %v", len(blocks), eventTypes(events))
	}
	for i, id := range ids {
		b := toolBlockByID(t, blocks, id)
		assertToolBlockSequenced(t, events, b)
		assertToolBlockArgs(t, b, args[i], decodeJSONObject(t, "期望参数", args[i]))
	}
}

// TestAnthropicStream_ToolArgs_IDArrivesAfterBlockStart 覆盖 id 晚于 name/参数到达：
// 块已经用兜底 id 打开，后到的 id 不能导致再开一个块。
func TestAnthropicStream_ToolArgs_IDArrivesAfterBlockStart(t *testing.T) {
	const args = `{"command": "ls"}`
	sse := openAISSE(t,
		chunkWithToolCalls(map[string]any{
			"index":    0,
			"type":     "function",
			"function": map[string]any{"name": "Bash", "arguments": args},
		}),
		chunkWithToolCalls(map[string]any{"index": 0, "id": "call_late", "type": "function"}),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1（id 晚到不能另开块）; 事件序列 %v",
			len(blocks), eventTypes(events))
	}
	if blocks[0].ID == "" {
		t.Errorf("块必须有 id（兜底或上游值）")
	}
	assertToolBlockSequenced(t, events, blocks[0])
	assertToolBlockArgs(t, blocks[0], args, decodeJSONObject(t, "期望参数", args))
}

// TestAnthropicStream_ToolArgs_EmptyArgumentsToolInputEmptyObject 覆盖空参数工具
// （首块 id+name、没有 arguments 字段）：客户端应拿到 input={}（无 delta 也可），
// 且 stop_reason 仍是 tool_use。
func TestAnthropicStream_ToolArgs_EmptyArgumentsToolInputEmptyObject(t *testing.T) {
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaNameOnly(0, "call_noargs", "TodoRead")),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1; 事件序列 %v", len(blocks), eventTypes(events))
	}
	if blocks[0].Name != "TodoRead" {
		t.Errorf("name = %q, want TodoRead", blocks[0].Name)
	}
	if blocks[0].StopPos < 0 {
		t.Errorf("tool_use 块缺少 content_block_stop")
	}
	if blocks[0].PartialJSON != "" {
		t.Errorf("无参数工具不该有 input_json_delta，实际拼接 = %q", blocks[0].PartialJSON)
	}
	if _, stop := messageDeltaInfo(t, events); stop != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", stop)
	}
}

// TestAnthropicStream_ToolArgs_TextToolText 覆盖 text → tool → text 的形态：
// 期望两个文本块(index 0/2) + 一个工具块(index 1)，文本拼接为 "ab"，index 密集升序。
func TestAnthropicStream_ToolArgs_TextToolText(t *testing.T) {
	const args = `{"command": "ls"}`
	sse := openAISSE(t,
		map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "a"}}},
		},
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_ls", "Bash", args)),
		map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "b"}}},
		},
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1; 事件序列 %v", len(blocks), eventTypes(events))
	}
	if blocks[0].Index != 1 {
		t.Errorf("tool_use 块 index = %d, want 1（文本块 0 之后）", blocks[0].Index)
	}
	assertToolBlockSequenced(t, events, blocks[0])
	assertToolBlockArgs(t, blocks[0], args, decodeJSONObject(t, "期望参数", args))

	// 两个文本块（index 0 与 2）的文本拼起来必须是 "ab"，且都各自有 start/stop。
	texts := map[int]string{}
	open := map[int]bool{}
	for _, e := range events {
		switch e["type"] {
		case "content_block_start":
			cb, _ := e["content_block"].(map[string]any)
			if cb != nil && cb["type"] == "text" {
				open[eventIndex(e)] = true
			}
		case "content_block_delta":
			d := eventDelta(e)
			if d["type"] == "text_delta" {
				texts[eventIndex(e)] += d["text"].(string)
			}
		}
	}
	if texts[0] != "a" || texts[2] != "b" {
		t.Errorf("文本块拼接 = %v, want map[0:a 2:b]", texts)
	}
	if !open[0] || !open[2] {
		t.Errorf("两个文本块都应发出 content_block_start，实际 %v", open)
	}
	if n := countEvent(events, "content_block_stop"); n != 3 {
		t.Errorf("content_block_stop 数量 = %d, want 3", n)
	}
}

// TestAnthropicStream_MalformedFrameDoesNotSwallowRest 覆盖「上游中途发了一帧非法 JSON」。
//
// 转换器用 dataBuf 攒多行 data；若坏帧让 dataBuf 一直非空且一直非法，后续**所有**分块
// 都会被拼进这个坏缓冲、永不被处理——整条流的尾巴被静默吞掉，最后仍按正常收尾发
// message_stop，看起来像「模型自己说完了」。这是本轮排查 E2E 时真实撞到的形态：
// 用例第一版手写 SSE 漏了反斜杠，于是「参数丢了」其实是坏帧把整段吞了。
func TestAnthropicStream_MalformedFrameDoesNotSwallowRest(t *testing.T) {
	good1 := `{"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"a"}}]}`
	bad := `{"choices":[{"index":0,"delta":{"content":"BROKEN` // 缺右括号与引号
	good2 := `{"id":"c","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"content":"b"},"finish_reason":"stop"}]}`
	sse := "data: " + good1 + "\n\n" +
		"data: " + bad + "\n\n" +
		"data: " + good2 + "\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))
	if got := collectTextDeltas(events); got != "ab" {
		t.Errorf("文本拼接 = %q, want \"ab\"（坏帧只能丢弃自己，不能吞掉后面的分块）", got)
	}
}
