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
