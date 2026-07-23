package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// feedAnthropicStream pipes raw OpenAI-SSE bytes into a new
// anthropicStreamAdapter (with the given local estimatedInputTokens) and
// returns the full converted Anthropic-SSE output.
func feedAnthropicStream(t *testing.T, sse string, estimatedInputTokens int) string {
	t.Helper()
	upstream := io.NopCloser(bytes.NewReader([]byte(sse)))
	adapter := newAnthropicStreamAdapter(upstream, "req_test_123", "claude-3-5-sonnet-20241022", estimatedInputTokens)
	out, err := io.ReadAll(adapter)
	if err != nil {
		t.Fatalf("read adapted stream: %v", err)
	}
	return string(out)
}

// parseAnthropicEvents splits the emitted Anthropic SSE into a slice of event
// maps keyed by their "type" field. Each event is a separate "\n\n"-delimited
// block carrying a single "data:" line.
func parseAnthropicEvents(t *testing.T, output string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, block := range strings.Split(output, "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		var dataStr string
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data:") {
				dataStr = strings.TrimSpace(line[5:])
				break
			}
		}
		if dataStr == "" || dataStr == "[DONE]" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(dataStr), &m); err != nil {
			t.Fatalf("bad event JSON %q: %v", dataStr, err)
		}
		events = append(events, m)
	}
	return events
}

// findEvent returns the first event of the given Anthropic type.
func findEvent(events []map[string]any, typ string) map[string]any {
	for _, e := range events {
		if e["type"] == typ {
			return e
		}
	}
	return nil
}

// eventTypes returns the ordered list of event "type" values.
func eventTypes(events []map[string]any) []string {
	var out []string
	for _, e := range events {
		if t, ok := e["type"].(string); ok {
			out = append(out, t)
		}
	}
	return out
}

// countEvent returns how many events of the given type appear.
func countEvent(events []map[string]any, typ string) int {
	n := 0
	for _, e := range events {
		if e["type"] == typ {
			n++
		}
	}
	return n
}

// messageStartInputTokens extracts message_start.message.usage.input_tokens.
func messageStartInputTokens(t *testing.T, events []map[string]any) float64 {
	t.Helper()
	ms := findEvent(events, "message_start")
	if ms == nil {
		t.Fatalf("no message_start event; got types: %v", eventTypes(events))
	}
	msg, ok := ms["message"].(map[string]any)
	if !ok {
		t.Fatalf("message_start.message missing: %v", ms)
	}
	usage, ok := msg["usage"].(map[string]any)
	if !ok {
		t.Fatalf("message_start.message.usage missing: %v", msg)
	}
	v, _ := usage["input_tokens"].(float64)
	return v
}

// messageDeltaInfo extracts message_delta.delta.stop_reason and usage.output_tokens.
func messageDeltaInfo(t *testing.T, events []map[string]any) (outputTokens float64, stopReason string) {
	t.Helper()
	md := findEvent(events, "message_delta")
	if md == nil {
		t.Fatalf("no message_delta event; got types: %v", eventTypes(events))
	}
	usage, ok := md["usage"].(map[string]any)
	if !ok {
		t.Fatalf("message_delta.usage missing: %v", md)
	}
	out, _ := usage["output_tokens"].(float64)
	delta, _ := md["delta"].(map[string]any)
	stop, _ := delta["stop_reason"].(string)
	return out, stop
}

// indexOfType returns the index of the first event of the given type, or -1.
func indexOfType(events []map[string]any, typ string) int {
	for i, e := range events {
		if e["type"] == typ {
			return i
		}
	}
	return -1
}

// TestAnthropicStream_TriggerChunkRealPromptTokens uses the real prompt_tokens
// from the first (trigger) chunk — the local estimate must NOT override it.
func TestAnthropicStream_TriggerChunkRealPromptTokens(t *testing.T) {
	// estimate=999 must be overridden by the trigger chunk's prompt_tokens=25.
	sse := "" +
		"data: {\"id\":\"chatcmpl-2\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hi\"}}],\"usage\":{\"prompt_tokens\":25,\"completion_tokens\":3,\"total_tokens\":28}}\n\n" +
		"data: {\"id\":\"chatcmpl-2\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 999))

	if got := messageStartInputTokens(t, events); got != 25 {
		t.Errorf("message_start.input_tokens = %v, want 25 (real trigger value overrides estimate)", got)
	}
	out, stop := messageDeltaInfo(t, events)
	if out != 3 {
		t.Errorf("message_delta.output_tokens = %v, want 3 (from trigger usage)", out)
	}
	if stop != "end_turn" {
		t.Errorf("message_delta.stop_reason = %q, want end_turn", stop)
	}
}

// TestAnthropicStream_NoRealUsage_UsesLocalEstimate verifies that when the
// trigger chunk has no prompt_tokens, message_start uses the non-zero local
// estimate (never hardcoded 0).
func TestAnthropicStream_NoRealUsage_UsesLocalEstimate(t *testing.T) {
	// No usage at all → message_start.input_tokens must be the estimate (non-zero).
	sse := "" +
		"data: {\"id\":\"chatcmpl-5\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-5\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" there\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-5\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 100))

	if got := messageStartInputTokens(t, events); got != 100 {
		t.Errorf("message_start.input_tokens = %v, want 100 (local estimate, non-zero)", got)
	}
	if findEvent(events, "message_stop") == nil {
		t.Errorf("expected message_stop; got %v", eventTypes(events))
	}
}

// TestAnthropicStream_TrailingUsageChunk_NoDuplicateMessageDelta verifies the
// core fix for the trailing usage-only chunk: finish_reason must not cause a
// second message_delta once the usage-only chunk has emitted message_stop, and
// message_delta must use the trailing usage completion_tokens.
func TestAnthropicStream_TrailingUsageChunk_NoDuplicateMessageDelta(t *testing.T) {
	// OpenAI-style upstream: finish chunk has NO usage; usage only in the
	// trailing {"choices":[],"usage":{...}} chunk.
	sse := "" +
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":5,\"total_tokens\":17}}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 100))

	out, stop := messageDeltaInfo(t, events)
	if out != 5 {
		t.Errorf("message_delta.output_tokens = %v, want 5 (trailing usage completion_tokens)", out)
	}
	if stop != "end_turn" {
		t.Errorf("message_delta.stop_reason = %q, want end_turn", stop)
	}

	// Exactly one message_delta and one message_stop, with message_delta BEFORE
	// message_stop (no protocol乱序), and nothing after message_stop.
	if n := countEvent(events, "message_delta"); n != 1 {
		t.Errorf("message_delta count = %d, want 1", n)
	}
	if n := countEvent(events, "message_stop"); n != 1 {
		t.Errorf("message_stop count = %d, want 1", n)
	}
	types := eventTypes(events)
	if types[0] != "message_start" {
		t.Errorf("first event type = %q, want message_start", types[0])
	}
	if mdi, msi := indexOfType(events, "message_delta"), indexOfType(events, "message_stop"); mdi < 0 || msi < 0 || mdi > msi {
		t.Errorf("expected message_delta before message_stop (md=%d, ms=%d)", mdi, msi)
	}
	if n := countEvent(events, "content_block_stop"); n != 1 {
		t.Errorf("content_block_stop count = %d, want 1", n)
	}
}

// TestAnthropicStream_ToolUse_EmptyContentBoundaryNoPanic verifies the tool_use
// path plus empty-content boundaries do not panic, with correct stop_reason and
// a single message_delta.
func TestAnthropicStream_ToolUse_EmptyContentBoundaryNoPanic(t *testing.T) {
	// estimate=200 (should be used since the trigger chunk has no usage).
	sse := "" +
		"data: {\"id\":\"chatcmpl-3\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		// empty content boundary (delta:{}), nothing else — must not panic
		"data: {\"id\":\"chatcmpl-3\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-3\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-3\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-3\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"Beijing\\\"}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-3\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-3\",\"object\":\"chat.completion.chunk\",\"model\":\"claude-3-5-sonnet\",\"choices\":[],\"usage\":{\"prompt_tokens\":30,\"completion_tokens\":8,\"total_tokens\":38}}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 200))

	if got := messageStartInputTokens(t, events); got != 200 {
		t.Errorf("message_start.input_tokens = %v, want 200 (estimate, no trigger usage)", got)
	}
	out, stop := messageDeltaInfo(t, events)
	if out != 8 {
		t.Errorf("message_delta.output_tokens = %v, want 8", out)
	}
	if stop != "tool_use" {
		t.Errorf("message_delta.stop_reason = %q, want tool_use", stop)
	}
	if n := countEvent(events, "content_block_start"); n != 1 {
		t.Errorf("content_block_start count = %d, want 1 (tool_use block)", n)
	}
	if n := countContentBlockDeltaType(events, "input_json_delta"); n < 1 {
		t.Errorf("expected at least 1 input_json_delta, got %d", n)
	}
	if n := countEvent(events, "content_block_stop"); n != 1 {
		t.Errorf("content_block_stop count = %d, want 1", n)
	}
	if n := countEvent(events, "message_delta"); n != 1 {
		t.Errorf("message_delta count = %d, want 1", n)
	}
	if findEvent(events, "message_stop") == nil {
		t.Errorf("expected message_stop; got %v", eventTypes(events))
	}
}

// TestAnthropicStream_CacheTokensPassthrough verifies cache_* tokens flow into
// message_start.usage when the trigger chunk carries them.
func TestAnthropicStream_CacheTokensPassthrough(t *testing.T) {
	sse := "" +
		"data: {\"id\":\"chatcmpl-4\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":1,\"total_tokens\":10,\"cache_read_input_tokens\":4,\"cache_creation_input_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 100))

	ms := findEvent(events, "message_start")
	if ms == nil {
		t.Fatalf("no message_start; got %v", eventTypes(events))
	}
	msg := ms["message"].(map[string]any)
	usage := msg["usage"].(map[string]any)
	if v := usage["cache_read_input_tokens"]; v != float64(4) {
		t.Errorf("cache_read_input_tokens = %v, want 4", v)
	}
	if v := usage["cache_creation_input_tokens"]; v != float64(2) {
		t.Errorf("cache_creation_input_tokens = %v, want 2", v)
	}
	if v := usage["input_tokens"]; v != float64(9) {
		t.Errorf("input_tokens = %v, want 9", v)
	}
}

// countContentBlockDeltaType counts content_block_delta events whose
// delta.type equals the given string.
func countContentBlockDeltaType(events []map[string]any, deltaType string) int {
	n := 0
	for _, e := range events {
		if e["type"] != "content_block_delta" {
			continue
		}
		delta, _ := e["delta"].(map[string]any)
		if t, _ := delta["type"].(string); t == deltaType {
			n++
		}
	}
	return n
}

// TestEstimatePromptTokens_ChineseAccuracyRegression 用船长实测样本验证估算偏差：
// 517 个中文真实值 342，估算应明显收窄（不再 ~1.5 倍高估）。estimatePromptTokens 会
// 计入每条消息的结构 overhead（此处为单条消息 +3）。
func TestEstimatePromptTokens_ChineseAccuracyRegression(t *testing.T) {
	const unit = "中文"
	parts := make([]rune, 0, 517)
	for len(parts) < 517 {
		parts = append(parts, []rune(unit)...)
	}
	msgs := []core.Message{{Role: "user", Content: string(parts[:517])}}

	got := estimatePromptTokens(msgs)
	// 校准后 int(517*0.66)=341 + 3 overhead = 344，贴近真实 342（±5）。
	if got < 337 || got > 347 {
		t.Errorf("estimatePromptTokens(517 CJK) = %d, want 342±5 (real measured); old estimate was 517+3=520", got)
	}
}

// TestEstimatePromptTokens_EnglishRegression 验证英文文本仍按约 4 字符/token 估算，
// 不因 CJK 校准而回归。
func TestEstimatePromptTokens_EnglishRegression(t *testing.T) {
	msgs := []core.Message{{Role: "user", Content: "hello world test here, this is a long english sentence for token estimation."}}
	got := estimatePromptTokens(msgs)
	if got < 12 || got > 28 {
		t.Errorf("estimatePromptTokens(english) = %d, want ~18-24 (4 chars/token + overhead)", got)
	}
}

// TestEstimatePromptTokens_EmptyContentNoPanic 覆盖空 content 边界，不应 panic，
// 且非空消息数下结果≥1。
func TestEstimatePromptTokens_EmptyContentNoPanic(t *testing.T) {
	got := estimatePromptTokens([]core.Message{
		{Role: "system", Content: ""},
		{Role: "user", Content: ""},
	})
	if got < 1 {
		t.Errorf("estimatePromptTokens(empty msgs) = %d, want >= 1 (structure overhead)", got)
	}
	if got := estimatePromptTokens(nil); got != 0 {
		t.Errorf("estimatePromptTokens(nil) = %d, want 0", got)
	}
}

// TestEstimatePromptTokens_MixedLanguages 覆盖中英混合 + tool_use 结构消息：不 panic，
// 且结果为正的合理值。
func TestEstimatePromptTokens_MixedLanguages(t *testing.T) {
	msgs := []core.Message{
		{Role: "system", Content: "You are a helpful assistant. 你是助手。"},
		{Role: "user", Content: "What is 分布式架构?"},
		{Role: "assistant", ToolCalls: []core.ToolCall{{ID: "call_1", Type: "function", Function: core.FunctionCall{Name: "lookup", Arguments: `{"q":"x"}`}}}},
	}
	got := estimatePromptTokens(msgs) // 不应 panic
	if got < 3 {
		t.Errorf("estimatePromptTokens(mixed) = %d, want >= 3 (3 messages, each overhead>=1)", got)
	}
}

// TestChatResponseToAnthropic_CacheTokensPassthrough 覆盖非流式 /v1/messages 路径：
// CacheReadInputTokens / CacheCreationInputTokens 不再因值为非零而丢失。
func TestChatResponseToAnthropic_CacheTokensPassthrough(t *testing.T) {
	resp := &core.ChatResponse{
		ID:    "msg_1",
		Model: "claude-3-5-sonnet",
		Usage: core.Usage{
			PromptTokens:     100,
			CompletionTokens: 20,
			RawUsage: map[string]any{
				"cache_read_input_tokens":     int64(30),
				"cache_creation_input_tokens": int64(5),
			},
		},
		Choices: []core.Choice{
			{Message: core.ResponseMessage{Role: "assistant", Content: "hi"}, FinishReason: "stop"},
		},
	}

	anth := core.ChatResponseToAnthropic(resp, "claude-3-5-sonnet")
	if anth == nil {
		t.Fatal("ChatResponseToAnthropic returned nil")
	}
	if anth.Usage.CacheReadInputTokens == nil || *anth.Usage.CacheReadInputTokens != 30 {
		t.Errorf("CacheReadInputTokens = %v, want 30", anth.Usage.CacheReadInputTokens)
	}
	if anth.Usage.CacheCreationInputTokens == nil || *anth.Usage.CacheCreationInputTokens != 5 {
		t.Errorf("CacheCreationInputTokens = %v, want 5 (previously never set)", anth.Usage.CacheCreationInputTokens)
	}
	if anth.Usage.InputTokens != 100 || anth.Usage.OutputTokens != 20 {
		t.Errorf("Input/Output tokens mismatch: %+v", anth.Usage)
	}
}

// TestChatResponseToAnthropic_CacheTokensFromPromptDetails 覆盖 OpenAI-compatible
// 口径：cache_read 来自 prompt_tokens_details.cached_tokens，且 cache_creation 缺失时
// 不输出零值字段（omitempty）。
func TestChatResponseToAnthropic_CacheTokensFromPromptDetails(t *testing.T) {
	resp := &core.ChatResponse{
		ID:    "msg_2",
		Model: "gpt-4o",
		Usage: core.Usage{
			PromptTokens:        50,
			CompletionTokens:    10,
			PromptTokensDetails: &core.PromptTokensDetails{CachedTokens: 12},
		},
		Choices: []core.Choice{
			{Message: core.ResponseMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
		},
	}

	anth := core.ChatResponseToAnthropic(resp, "gpt-4o")
	if anth.Usage.CacheReadInputTokens == nil || *anth.Usage.CacheReadInputTokens != 12 {
		t.Errorf("CacheReadInputTokens = %v, want 12", anth.Usage.CacheReadInputTokens)
	}
	if anth.Usage.CacheCreationInputTokens != nil {
		t.Errorf("CacheCreationInputTokens = %v, want nil when no cache_creation", anth.Usage.CacheCreationInputTokens)
	}
}

// TestAnthropicStream_MessageStartAlwaysCarriesCache 验证 message_start.usage 总是带
// cache_read_input_tokens / cache_creation_input_tokens 字段（触发 chunk 无 usage 时缺省 0），
// 不因值为 0 而漏字段。
func TestAnthropicStream_MessageStartAlwaysCarriesCache(t *testing.T) {
	sse := "" +
		"data: {\"id\":\"chatcmpl-6\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hello\"}}]}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 100))

	ms := findEvent(events, "message_start")
	if ms == nil {
		t.Fatalf("no message_start; got %v", eventTypes(events))
	}
	usage := ms["message"].(map[string]any)["usage"].(map[string]any)
	if v, ok := usage["cache_read_input_tokens"]; !ok || v != float64(0) {
		t.Errorf("cache_read_input_tokens = %v (present=%v), want 0 always present", v, ok)
	}
	if v, ok := usage["cache_creation_input_tokens"]; !ok || v != float64(0) {
		t.Errorf("cache_creation_input_tokens = %v (present=%v), want 0 always present", v, ok)
	}
}

// TestAnthropicStream_TrailingCacheTokensFlowToMessageDelta 验证末尾 usage-only chunk
// 携带的真实非零 cache 字段会透传到 message_delta.usage（此前会被丢弃）。
func TestAnthropicStream_TrailingCacheTokensFlowToMessageDelta(t *testing.T) {
	sse := "" +
		"data: {\"id\":\"chatcmpl-7\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-7\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-7\",\"model\":\"claude-3-5-sonnet\",\"choices\":[],\"usage\":{\"completion_tokens\":5,\"cache_read_input_tokens\":7,\"cache_creation_input_tokens\":3}}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 100))

	md := findEvent(events, "message_delta")
	if md == nil {
		t.Fatalf("no message_delta; got %v", eventTypes(events))
	}
	usage := md["usage"].(map[string]any)
	if v := usage["cache_read_input_tokens"]; v != float64(7) {
		t.Errorf("message_delta.cache_read_input_tokens = %v, want 7", v)
	}
	if v := usage["cache_creation_input_tokens"]; v != float64(3) {
		t.Errorf("message_delta.cache_creation_input_tokens = %v, want 3", v)
	}
	if v := usage["output_tokens"]; v != float64(5) {
		t.Errorf("message_delta.output_tokens = %v, want 5", v)
	}
}

// TestAnthropicStream_MessageDeltaOmitsZeroCache 验证 message_delta 在 cache 为 0 时
// 保持官方口径（仅 output_tokens），不输出空洞的 0 值 cache 字段。
func TestAnthropicStream_MessageDeltaOmitsZeroCache(t *testing.T) {
	sse := "" +
		"data: {\"id\":\"chatcmpl-8\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-8\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-8\",\"model\":\"claude-3-5-sonnet\",\"choices\":[],\"usage\":{\"completion_tokens\":4}}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 100))

	md := findEvent(events, "message_delta")
	if md == nil {
		t.Fatalf("no message_delta; got %v", eventTypes(events))
	}
	usage := md["usage"].(map[string]any)
	if _, ok := usage["cache_read_input_tokens"]; ok {
		t.Error("message_delta.cache_read_input_tokens should be omitted when 0")
	}
	if _, ok := usage["cache_creation_input_tokens"]; ok {
		t.Error("message_delta.cache_creation_input_tokens should be omitted when 0")
	}
}

// TestHandleCountTokens_UsesCalibratedEstimate 验证 /v1/messages/count_tokens 改用
// 校准估算：517 个中文不再被旧 totalChars/4（≈129）严重低估，而是贴近真实 342±5。
func TestHandleCountTokens_UsesCalibratedEstimate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const unit = "中文"
	parts := make([]rune, 0, 517)
	for len(parts) < 517 {
		parts = append(parts, []rune(unit)...)
	}
	body := `{"model":"claude-3-5-sonnet","max_tokens":1024,"messages":[{"role":"user","content":"` + string(parts[:517]) + `"}]}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h := &MessagesHandler{}
	h.HandleCountTokens(c)

	if w.Code != http.StatusOK {
		t.Fatalf("count_tokens status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	got, _ := resp["input_tokens"].(float64)
	// 旧 totalChars/4 用原始字节长度估算，对 CJK 不贴合 token 语义；校准估算 ≈344，
	// 贴近真实 342±5，消除中文偏差。
	if got < 337 || got > 347 {
		t.Errorf("count_tokens input_tokens = %v, want 342±5 (calibrated CJK estimate)", got)
	}
}
