package handler

// 多 choice（n > 1）的形态：OpenAI 的 n>1 是一次请求返回多份补全，而 Anthropic 的一条
// message 只能装一份内容。因此网关只转换 choices[0]，其余忽略并告警——若把多个 choice
// 的 delta 灌进同一份状态，文本会串台、并行工具调用会互相污染参数。

import (
	"strings"
	"testing"
)

// multiChoiceChunk 构造一个带多个 choice 的 OpenAI 流式 chunk（每个 choice 各自一个 delta）。
func multiChoiceChunk(deltas ...map[string]any) map[string]any {
	choices := make([]any, 0, len(deltas))
	for i, d := range deltas {
		choices = append(choices, map[string]any{"index": i, "delta": d})
	}
	return map[string]any{
		"id": "chatcmpl-multi", "object": "chat.completion.chunk", "model": "m",
		"choices": choices,
	}
}

// collectTextDeltas 拼接所有 text_delta 的文本。
func collectTextDeltas(events []map[string]any) string {
	var sb strings.Builder
	for _, e := range events {
		if e["type"] != "content_block_delta" {
			continue
		}
		d := eventDelta(e)
		if d["type"] != "text_delta" {
			continue
		}
		text, _ := d["text"].(string)
		sb.WriteString(text)
	}
	return sb.String()
}

// TestAnthropicStream_MultiChoice_OnlyFirstChoiceConverted 断言 n=2 时文本只来自 choices[0]。
func TestAnthropicStream_MultiChoice_OnlyFirstChoiceConverted(t *testing.T) {
	sse := openAISSE(t,
		multiChoiceChunk(
			map[string]any{"content": "a"},
			map[string]any{"content": "b"},
		),
		finishChunk("stop"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	if got := collectTextDeltas(events); got != "a" {
		t.Errorf("文本拼接 = %q, want %q（n>1 时只转换第一个 choice，否则文本串台）", got, "a")
	}
}

// TestAnthropicStream_MultiChoice_ToolCallsOnlyFromFirstChoice 断言 n=2 时工具调用只来自 choices[0]。
func TestAnthropicStream_MultiChoice_ToolCallsOnlyFromFirstChoice(t *testing.T) {
	const argsA = `{"command": "ls"}`
	const argsB = `{"command": "pwd"}`
	sse := openAISSE(t,
		multiChoiceChunk(
			map[string]any{"tool_calls": []any{toolCallDeltaWithArgs(0, "call_a", "Bash", argsA)}},
			map[string]any{"tool_calls": []any{toolCallDeltaWithArgs(0, "call_b", "Bash", argsB)}},
		),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1（只转换第一个 choice）；事件序列 %v", len(blocks), eventTypes(events))
	}
	if blocks[0].ID != "call_a" {
		t.Errorf("tool_use id = %q, want call_a（不能混入其它 choice 的调用）", blocks[0].ID)
	}
	assertToolBlockSequenced(t, events, blocks[0])
	assertToolBlockArgs(t, blocks[0], argsA, decodeJSONObject(t, "期望参数", argsA))
}

// TestAnthropicStream_MultiChoice_EmptyChoices 是防 panic 守卫：choices 为空数组时
// 不能 panic，也不能发出内容块。
func TestAnthropicStream_MultiChoice_EmptyChoices(t *testing.T) {
	sse := openAISSE(t, map[string]any{
		"id": "chatcmpl-empty", "object": "chat.completion.chunk", "model": "m",
		"choices": []any{},
	})
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	if n := countEvent(events, "content_block_start"); n != 0 {
		t.Errorf("content_block_start 数量 = %d, want 0；事件序列 %v", n, eventTypes(events))
	}
}

// TestAnthropicStream_MultiChoice_FinishReasonFromIgnoredChoice 覆盖"finish_reason 只挂在
// 被忽略的 choice 上"的形态（部分上游这么发）。内容仍然只取 choices[0]，但收尾原因必须
// 取到，否则 max_tokens / content_filter 会被误报成 end_turn（旧实现会遍历所有 choice，
// 所以这是"只转换第一个"引入的回归风险）。
func TestAnthropicStream_MultiChoice_FinishReasonFromIgnoredChoice(t *testing.T) {
	chunk := map[string]any{
		"id": "chatcmpl-fin", "object": "chat.completion.chunk", "model": "m",
		"choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"content": "a"}},
			map[string]any{"index": 1, "delta": map[string]any{}, "finish_reason": "length"},
		},
	}
	events := parseAnthropicEvents(t, feedAnthropicStream(t, openAISSE(t, chunk), 50))

	if _, stop := messageDeltaInfo(t, events); stop != "max_tokens" {
		t.Errorf("stop_reason = %q, want max_tokens（finish_reason 在被忽略的 choice 上也不能丢）", stop)
	}
	if got := collectTextDeltas(events); got != "a" {
		t.Errorf("文本拼接 = %q, want a（内容仍只取第一个 choice）", got)
	}
}

// TestAnthropicStream_MultiChoice_SingleChoicePerChunk 覆盖 n>1 的**真实**流式形态：
// 上游每个 chunk 只带一个 choice，靠 choice.index 区分（官方 SDK 就按 index 累加）。
// 按数组下标取"第一个元素"会把 index=1 的内容当成第一份，串台照旧发生。
func TestAnthropicStream_MultiChoice_SingleChoicePerChunk(t *testing.T) {
	first := map[string]any{
		"id": "chatcmpl-split", "object": "chat.completion.chunk", "model": "m",
		"choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"content": "a1"}},
			map[string]any{"index": 1, "delta": map[string]any{"content": "b1"}},
		},
	}
	second := map[string]any{
		"id": "chatcmpl-split", "object": "chat.completion.chunk", "model": "m",
		"choices": []any{map[string]any{"index": 1, "delta": map[string]any{"content": "b2"}}},
	}
	events := parseAnthropicEvents(t, feedAnthropicStream(t, openAISSE(t, first, second, finishChunk("stop")), 50))

	if got := collectTextDeltas(events); got != "a1" {
		t.Errorf("文本拼接 = %q, want %q（只有 choice.index==0 的内容能进这条 message）", got, "a1")
	}
}

// TestAnthropicStream_MultiChoice_ToolCallFromIgnoredIndex 覆盖"某个 chunk 只带 index=1 的
// 工具调用"：它属于被丢弃的那一份内容，绝不能变成 tool_use 块（否则客户端会去执行它）。
func TestAnthropicStream_MultiChoice_ToolCallFromIgnoredIndex(t *testing.T) {
	const argsB = `{"command": "pwd"}`
	events := parseAnthropicEvents(t, feedAnthropicStream(t, openAISSE(t,
		multiChoiceChunk(
			map[string]any{"content": "a"},
			map[string]any{"tool_calls": []any{toolCallDeltaWithArgs(0, "call_b", "Bash", argsB)}},
		),
		map[string]any{
			"id": "chatcmpl-only-b", "object": "chat.completion.chunk", "model": "m",
			"choices": []any{map[string]any{"index": 1, "delta": map[string]any{"content": "b-more"}}},
		},
		finishChunk("tool_calls"),
	), 50))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 0 {
		t.Errorf("tool_use 块数量 = %d, want 0（被丢弃的 choice 里的工具调用不能下发）；ids=%v",
			len(blocks), func() []string {
				ids := make([]string, 0, len(blocks))
				for _, b := range blocks {
					ids = append(ids, b.ID)
				}
				return ids
			}())
	}
	if got := collectTextDeltas(events); got != "a" {
		t.Errorf("文本拼接 = %q, want a（index=1 的文本同样不能混进来）", got)
	}
}

// TestAnthropicStream_UsageOnlyChunk_EmitsMessageStart 覆盖"整条流只有一个 usage-only
// chunk（choices 为空）"的形态：Anthropic 协议要求 message_start 是第一条事件，
// 缺了它只发 message_delta/message_stop 是协议违例（SDK 会报意外事件顺序）。
func TestAnthropicStream_UsageOnlyChunk_EmitsMessageStart(t *testing.T) {
	sse := openAISSE(t, map[string]any{
		"id": "chatcmpl-usage", "object": "chat.completion.chunk", "model": "m",
		"choices": []any{},
		"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 0, "total_tokens": 7},
	})
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 50))

	if len(events) == 0 {
		t.Fatal("没有任何事件")
	}
	if got := events[0]["type"]; got != "message_start" {
		t.Errorf("首个事件 = %v, want message_start（否则客户端收到的是非法事件序列）", got)
	}
	if n := countEvent(events, "message_start"); n != 1 {
		t.Errorf("message_start 数量 = %d, want 1", n)
	}
}
