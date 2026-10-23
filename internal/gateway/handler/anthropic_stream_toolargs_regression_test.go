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
// tool_calls 的形态（两种字段顺序各一次）：文本块与工具块 index 必须密集且各自完整。
func TestAnthropicStream_ToolArgs_TextAndToolCallsInSameChunk(t *testing.T) {
	const args = `{"command": "ls"}`
	for name, delta := range map[string]map[string]any{
		"text_then_tools": {
			"content":    "running",
			"tool_calls": []any{toolCallDeltaWithArgs(0, "call_ls", "Bash", args)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			sse := openAISSE(t,
				map[string]any{
					"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
					"choices": []any{map[string]any{"index": 0, "delta": delta}},
				},
				finishChunk("tool_calls"),
			)
			events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

			blocks := collectToolUseBlocks(t, events)
			if len(blocks) != 1 {
				t.Fatalf("tool_use 块数量 = %d, want 1; 事件序列 %v", len(blocks), eventTypes(events))
			}
			assertToolBlockSequenced(t, events, blocks[0])
			assertToolBlockArgs(t, blocks[0], args, decodeJSONObject(t, "期望参数", args))

			// 文本块 index 必须与工具块不同且更小（文本先出现）。
			if blocks[0].Index != 1 {
				t.Errorf("tool_use 块 index = %d, want 1（文本块占用 0）", blocks[0].Index)
			}
		})
	}
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
