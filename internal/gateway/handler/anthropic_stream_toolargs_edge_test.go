package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

// 本文件补齐 anthropic_stream_toolargs 用例未覆盖的边界：分别验证
//   - 工具块打开期间到达的文本 delta 不再被丢弃（修复前的 S1）；
//   - 上游缺 id（只给 index/name/arguments）时仍能开块并保住参数；
//   - name 一直没到、只有参数时，收尾阶段仍能把整次调用补出来；
//   - 同一 index 被新的 tool_call id 复用时切成两个块，参数不混；
//   - 没有 finish_reason（只有 [DONE]）时 flush 路径也能按序收尾所有并行块。

// TestAnthropicStream_ToolArgs_TextDeltaAfterToolBlock 覆盖"工具块打开期间来了文本
// delta"：修复前该文本会被直接丢弃（只有 tool 块打开时 startTextBlock 不触发、
// emitTextDelta 也不执行），客户端因此丢字。
func TestAnthropicStream_ToolArgs_TextDeltaAfterToolBlock(t *testing.T) {
	const args = `{"command": "ls"}`
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_ls", "Bash", args)),
		// 工具块之后又来了文本 delta（部分后端会在工具调用后补一句说明）。
		map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "done"}}},
		},
		finishChunk("stop"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1; 事件序列 %v", len(blocks), eventTypes(events))
	}
	assertToolBlockSequenced(t, events, blocks[0])
	assertToolBlockArgs(t, blocks[0], args, decodeJSONObject(t, "期望参数", args))

	// 文本 delta 必须真的发出来，且落在文本块自己的 start/stop 之间。
	var text strings.Builder
	textIdx := -1
	for _, e := range events {
		if e["type"] != "content_block_delta" {
			continue
		}
		d := eventDelta(e)
		if d["type"] != "text_delta" {
			continue
		}
		text.WriteString(d["text"].(string))
		textIdx = eventIndex(e)
	}
	if text.String() != "done" {
		t.Errorf("文本 delta 拼接 = %q, want %q（工具块打开期间的文本不能被丢）", text.String(), "done")
	}
	if textIdx >= 0 && textIdx == blocks[0].Index {
		t.Errorf("文本 delta 与 tool_use 块共用了 index %d", textIdx)
	}
}

// TestAnthropicStream_ToolArgs_NoIDWithNameAndArgs 覆盖上游只给 index/name/arguments、
// 完全不给 id 的形态：修复前的实现只在 id 非空时才开块，整次调用会被静默丢弃，
// 但 finish_reason 仍然映射成 stop_reason=tool_use。
func TestAnthropicStream_ToolArgs_NoIDWithNameAndArgs(t *testing.T) {
	const args = `{"path": "/tmp/x"}`
	sse := openAISSE(t,
		chunkWithToolCalls(map[string]any{
			"index":    0,
			"type":     "function",
			"function": map[string]any{"name": "Read", "arguments": args},
		}),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1（缺 id 也必须开块）; 事件序列 %v", len(blocks), eventTypes(events))
	}
	if blocks[0].ID == "" {
		t.Errorf("缺 id 时必须生成兜底 id，实际为空")
	}
	if blocks[0].Name != "Read" {
		t.Errorf("name = %q, want Read", blocks[0].Name)
	}
	assertToolBlockSequenced(t, events, blocks[0])
	assertToolBlockArgs(t, blocks[0], args, decodeJSONObject(t, "期望参数", args))
}

// TestAnthropicStream_ToolArgs_NameNeverArrives 覆盖"只有参数、从没有 name"的上游：
// 开块时不能发出 name 为空的半成品，但收尾阶段必须把这次调用补出来（否则客户端看到
// stop_reason=tool_use 却没有任何 tool_use 块，等于静默丢调用）。
func TestAnthropicStream_ToolArgs_NameNeverArrives(t *testing.T) {
	const args = `{"a": 1}`
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, args)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1; 事件序列 %v", len(blocks), eventTypes(events))
	}
	assertToolBlockSequenced(t, events, blocks[0])
	assertToolBlockArgs(t, blocks[0], args, decodeJSONObject(t, "期望参数", args))
}

// TestAnthropicStream_ToolArgs_IndexReusedByNewCallID 覆盖上游复用同一个 tool_calls
// index（或干脆省略 index）跑两次调用的情况：必须切成两个块，参数各自归属。
func TestAnthropicStream_ToolArgs_IndexReusedByNewCallID(t *testing.T) {
	const argsA = `{"x": 1}`
	const argsB = `{"y": 2}`
	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_a", "ToolA", argsA)),
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

// TestAnthropicStream_ToolArgs_ParallelThenNoFinishChunk 覆盖没有 finish_reason、只有
// [DONE] 的收尾路径：并行工具块必须按 content block index 升序收尾，且每个块参数完整。
func TestAnthropicStream_ToolArgs_ParallelThenNoFinishChunk(t *testing.T) {
	const argsA = `{"command": "git status"}`
	const argsB = `{"todos": [{"content": "x", "status": "pending"}]}`
	sse := openAISSE(t, // 注意：没有 finishChunk，直接 [DONE]
		chunkWithToolCalls(
			toolCallDeltaWithArgs(0, "call_bash", "Bash", argsA),
			toolCallDeltaWithArgs(1, "call_todo", "TodoWrite", argsB),
		),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 2 {
		t.Fatalf("tool_use 块数量 = %d, want 2; 事件序列 %v", len(blocks), eventTypes(events))
	}
	for i, b := range blocks {
		assertToolBlockSequenced(t, events, b)
		if b.Index != i {
			t.Errorf("块顺序: blocks[%d].Index = %d, want %d", i, b.Index, i)
		}
	}
	assertToolBlockArgs(t, toolBlockByID(t, blocks, "call_bash"), argsA, decodeJSONObject(t, "期望参数 A", argsA))
	assertToolBlockArgs(t, toolBlockByID(t, blocks, "call_todo"), argsB, decodeJSONObject(t, "期望参数 B", argsB))

	// 收尾必须恰好一次，且 stop_reason 为 tool_use。
	if n := countEvent(events, "message_stop"); n != 1 {
		t.Errorf("message_stop 数量 = %d, want 1", n)
	}
	if _, stop := messageDeltaInfo(t, events); stop != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", stop)
	}
}

// 保证 json 包被使用（部分断言走字符串比较，保留兜底引用避免 import 未使用）。
var _ = json.Valid
