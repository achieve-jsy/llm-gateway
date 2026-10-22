package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// TestAnthropicStream_MessageDeltaOverwritesEstimateInputTokens 验证缓存命中场景：
// message_start 显示估算 input_tokens（1524 占位），末尾 message_delta 携带真实未缓存
// input_tokens（14）+ cache_read（2754），借 SDK "overwrite when present, never add"
// 语义覆盖估算，使 input 与 cache_read 拆分不重叠，消除重复计费。
func TestAnthropicStream_MessageDeltaOverwritesEstimateInputTokens(t *testing.T) {
	sse := "" +
		"data: {\"id\":\"chatcmpl-9\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-9\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-9\",\"model\":\"claude-3-5-sonnet\",\"choices\":[],\"usage\":{\"prompt_tokens\":14,\"completion_tokens\":4,\"total_tokens\":2772,\"cache_read_input_tokens\":2754,\"cache_creation_input_tokens\":0}}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 1524))

	ms := findEvent(events, "message_start")
	if ms == nil {
		t.Fatalf("no message_start; got %v", eventTypes(events))
	}
	msUsage := ms["message"].(map[string]any)["usage"].(map[string]any)
	if v := msUsage["input_tokens"]; v != float64(1524) {
		t.Errorf("message_start.input_tokens = %v, want estimate 1524 (占位)", v)
	}

	md := findEvent(events, "message_delta")
	if md == nil {
		t.Fatalf("no message_delta; got %v", eventTypes(events))
	}
	mdUsage := md["usage"].(map[string]any)
	if v := mdUsage["input_tokens"]; v != float64(14) {
		t.Errorf("message_delta.input_tokens = %v, want real 14 (覆盖估算，未缓存部分)", v)
	}
	if v := mdUsage["cache_read_input_tokens"]; v != float64(2754) {
		t.Errorf("message_delta.cache_read_input_tokens = %v, want 2754", v)
	}
	// 最终快照：14 (input) + 2754 (cache_read) = 2768，拆分不重叠，无重复计费。
	total := mdUsage["input_tokens"].(float64) + mdUsage["cache_read_input_tokens"].(float64)
	if total != float64(2768) {
		t.Errorf("input(%v)+cache_read(%v)=%.0f, want 2768 (拆分不重叠)", mdUsage["input_tokens"], mdUsage["cache_read_input_tokens"], total)
	}
}

// TestAnthropicStream_CacheReadFromPromptTokensDetails 验证 OpenAI 兼容上游把命中数
// 放在 prompt_tokens_details.cached_tokens 时，流式也正确映射为 cache_read（与非流式
// ChatResponseToAnthropic 同源）。
func TestAnthropicStream_CacheReadFromPromptTokensDetails(t *testing.T) {
	sse := "" +
		"data: {\"id\":\"chatcmpl-a\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl-a\",\"model\":\"claude-3-5-sonnet\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl-a\",\"model\":\"claude-3-5-sonnet\",\"choices\":[],\"usage\":{\"prompt_tokens\":14,\"completion_tokens\":4,\"prompt_tokens_details\":{\"cached_tokens\":2754}}}\n\n" +
		"data: [DONE]\n\n"

	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 1524))
	md := findEvent(events, "message_delta")
	if md == nil {
		t.Fatalf("no message_delta; got %v", eventTypes(events))
	}
	usage := md["usage"].(map[string]any)
	if v := usage["cache_read_input_tokens"]; v != float64(2754) {
		t.Errorf("prompt_tokens_details.cached_tokens 未映射为 cache_read: got %v, want 2754", v)
	}
	if v := usage["cache_read_input_tokens"]; v == nil {
		t.Errorf("cache_read_input_tokens 未出现")
	}
}

// ──────────────────────────────────────────────
// tool_use 参数拼接（processChunk）回归测试
//
// 背景：OpenAI 流式协议要求把同一次工具调用（同一 index）每个分块里的
// function.arguments 按顺序无脑拼接，首块也不例外。修复前 processChunk 在
// tc.ID != ""（新工具调用首块）分支只发 content_block_start(input:{})，
// 把该块的 arguments 整块丢弃；遇到「首块即带完整参数」的后端（SGLang 0.5.22
// 的 deepseekv41 工具解析器）时，Claude Code 收到的 tool_use.input 恒为 {}，
// 报 InputValidationError: The required parameter `command` is missing。
//
// 以下用例分两类断言：(1) 完整 SSE 事件序列；(2) 每个 tool_use 块的 input
// 反序列化结果（逐字节拼接 + 字段级对象相等）。修复前应为红。
// ──────────────────────────────────────────────

// openAISSE 把若干 OpenAI chunk（map 形式）序列化为 SSE 文本，末尾补 [DONE]。
// 用 map 构造而不是手写字符串，避免多层 JSON 转义把用例写错（尤其 arguments
// 本身就是 JSON 的场景）。
func openAISSE(t *testing.T, chunks ...map[string]any) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range chunks {
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("marshal openai chunk: %v", err)
		}
		sb.WriteString("data: ")
		sb.Write(raw)
		sb.WriteString("\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")
	return sb.String()
}

// chunkWithToolCalls 构造 choices[0].delta.tool_calls 的 chunk。
func chunkWithToolCalls(toolCalls ...map[string]any) map[string]any {
	return map[string]any{
		"id":     "chatcmpl-tools",
		"object": "chat.completion.chunk",
		"model":  "deepseek-v41",
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"tool_calls": toolCalls},
		}},
	}
}

// finishChunk 构造 finish_reason 收尾 chunk（delta 为空）。
func finishChunk(reason string) map[string]any {
	return map[string]any{
		"id":     "chatcmpl-tools",
		"object": "chat.completion.chunk",
		"model":  "deepseek-v41",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": reason,
		}},
	}
}

// emptyDeltaChunk 构造 delta 为空的边界 chunk（不发任何内容）。
func emptyDeltaChunk() map[string]any {
	return map[string]any{
		"id":     "chatcmpl-tools",
		"object": "chat.completion.chunk",
		"model":  "deepseek-v41",
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{},
		}},
	}
}

// toolCallDeltaWithArgs 构造显式带 id/type/name/arguments 的 tool_calls delta
// （首块即带完整参数的后端形态）。id 为空时不下发 id（用于续传块）。
func toolCallDeltaWithArgs(idx int, id, name, args string) map[string]any {
	fn := map[string]any{"arguments": args}
	if name != "" {
		fn["name"] = name
	}
	d := map[string]any{"index": idx, "function": fn}
	if id != "" {
		d["id"] = id
		d["type"] = "function"
	}
	return d
}

// toolCallDeltaNameOnly 构造只有 name、没有 arguments 字段的首块 delta。
func toolCallDeltaNameOnly(idx int, id, name string) map[string]any {
	return map[string]any{
		"index":    idx,
		"id":       id,
		"type":     "function",
		"function": map[string]any{"name": name},
	}
}

// toolCallDeltaArgsOnly 构造续传块：只有 index + arguments，没有 id / name
// （OpenAI 官方风格，续传块不复述 id）。
func toolCallDeltaArgsOnly(idx int, args string) map[string]any {
	return map[string]any{
		"index":    idx,
		"function": map[string]any{"arguments": args},
	}
}

// toolUseBlockView 是一个 tool_use 内容块在 Anthropic SSE 事件流中的完整视图：
// 它的 content_block_start、挂在同一 index 上的所有 input_json_delta、以及
// content_block_stop，连同它们的事件序号，便于断言块内顺序与参数归属。
type toolUseBlockView struct {
	Index       int
	ID          string
	Name        string
	StartInput  map[string]any // content_block_start.content_block.input（协议要求空对象 {}）
	PartialJSON string         // 该 index 下所有 input_json_delta.partial_json 的顺序拼接
	StartPos    int            // content_block_start 在事件列表中的位置
	StopPos     int            // content_block_stop 位置，-1 表示缺失
	DeltaPos    []int          // 每个 input_json_delta 的位置
}

// eventIndex 读取事件的 index 字段（JSON 数字解析为 float64）。
func eventIndex(e map[string]any) int {
	v, _ := e["index"].(float64)
	return int(v)
}

// eventDelta 读取事件的 delta 对象。
func eventDelta(e map[string]any) map[string]any {
	d, _ := e["delta"].(map[string]any)
	return d
}

// collectToolUseBlocks 从事件流还原所有 tool_use 块（按 content_block_start 出现顺序）。
// 出现「没有对应 content_block_start 的 input_json_delta」或「同一 index 出现两个
// tool_use content_block_start」时直接 Fatal——两者都是协议级错误。
func collectToolUseBlocks(t *testing.T, events []map[string]any) []toolUseBlockView {
	t.Helper()
	blocks := []toolUseBlockView{}
	byIndex := map[int]int{} // content_block index -> blocks 下标

	for pos, e := range events {
		switch e["type"] {
		case "content_block_start":
			cb, _ := e["content_block"].(map[string]any)
			if cb == nil || cb["type"] != "tool_use" {
				continue
			}
			idx := eventIndex(e)
			if _, dup := byIndex[idx]; dup {
				t.Fatalf("content_block_start(tool_use, index=%d) 重复出现：同一 index 只能有一个 tool_use 块；事件序列 %v",
					idx, eventTypes(events))
			}
			id, _ := cb["id"].(string)
			name, _ := cb["name"].(string)
			input, _ := cb["input"].(map[string]any)
			blocks = append(blocks, toolUseBlockView{
				Index:      idx,
				ID:         id,
				Name:       name,
				StartInput: input,
				StartPos:   pos,
				StopPos:    -1,
			})
			byIndex[idx] = len(blocks) - 1
		case "content_block_delta":
			d := eventDelta(e)
			if d == nil || d["type"] != "input_json_delta" {
				continue
			}
			idx := eventIndex(e)
			bi, ok := byIndex[idx]
			if !ok {
				t.Fatalf("input_json_delta(index=%d) 之前没有对应的 content_block_start(tool_use)，参数无处落地；事件序列 %v",
					idx, eventTypes(events))
			}
			partial, _ := d["partial_json"].(string)
			blocks[bi].PartialJSON += partial
			blocks[bi].DeltaPos = append(blocks[bi].DeltaPos, pos)
		case "content_block_stop":
			if bi, ok := byIndex[eventIndex(e)]; ok && blocks[bi].StopPos < 0 {
				blocks[bi].StopPos = pos
			}
		}
	}
	return blocks
}

// toolBlockByID 按 tool_use id 取块视图（并行场景避免依赖块的出现顺序）。
func toolBlockByID(t *testing.T, blocks []toolUseBlockView, id string) toolUseBlockView {
	t.Helper()
	for _, b := range blocks {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("没有 id=%q 的 tool_use 块；实际得到 %d 个块: %+v", id, len(blocks), blocks)
	return toolUseBlockView{}
}

// assertToolBlockSequenced 断言一个 tool_use 块内部严格是
// content_block_start → input_json_delta… → content_block_stop：
// 每个 delta 必须落在本块自己的 start→stop 之间（delta 若在 stop 之后，客户端
// 按 index 累加时会找不到块而丢弃）。
// 没有 input_json_delta 时给出与生产事故对应的失败信息。
func assertToolBlockSequenced(t *testing.T, events []map[string]any, b toolUseBlockView) {
	t.Helper()
	if b.StopPos < 0 {
		t.Fatalf("tool_use 块(index=%d, id=%q) 缺少 content_block_stop；事件序列 %v",
			b.Index, b.ID, eventTypes(events))
	}
	if len(b.DeltaPos) == 0 {
		t.Fatalf("tool_use 块(index=%d, id=%q, name=%q) 没有任何 input_json_delta，"+
			"客户端拿到的 tool_use.input 只能是 {}（生产事故：InputValidationError: required parameter missing）；"+
			"事件序列 %v", b.Index, b.ID, b.Name, eventTypes(events))
	}
	for _, dp := range b.DeltaPos {
		if dp < b.StartPos || dp > b.StopPos {
			t.Fatalf("input_json_delta(pos=%d) 不在块(index=%d) 的 start(pos=%d)→stop(pos=%d) 之间；事件序列 %v",
				dp, b.Index, b.StartPos, b.StopPos, eventTypes(events))
		}
	}
}

// decodeJSONObject 把 JSON 字符串解析成对象，用于字段级断言（不做字符串包含判断）。
func decodeJSONObject(t *testing.T, what, raw string) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatalf("%s 不是合法 JSON 对象: %q: %v", what, raw, err)
	}
	return obj
}

// assertToolBlockArgs 断言块的 partial_json 顺序拼接结果逐字节等于 wantRaw，
// 且解析出的对象与 wantObj 完全相等。
func assertToolBlockArgs(t *testing.T, b toolUseBlockView, wantRaw string, wantObj map[string]any) {
	t.Helper()
	if b.PartialJSON != wantRaw {
		t.Errorf("tool_use 块(index=%d, id=%q) 的 partial_json 拼接 = %q, want %q",
			b.Index, b.ID, b.PartialJSON, wantRaw)
		return
	}
	if got := decodeJSONObject(t, "流式拼接结果", b.PartialJSON); !reflect.DeepEqual(got, wantObj) {
		t.Errorf("tool_use 块(index=%d, id=%q) input 解析 = %#v, want %#v", b.Index, b.ID, got, wantObj)
	}
}

// nonStreamingToolUseInput 走非流式 ChatResponseToAnthropic，返回指定 tool_use 块的
// input 解析结果，用于断言「流式拼出来的 input == 非流式 input」。
func nonStreamingToolUseInput(t *testing.T, toolID, toolName, args string) map[string]any {
	t.Helper()
	resp := &core.ChatResponse{
		ID:    "chatcmpl-nonstream",
		Model: "claude-3-5-sonnet",
		Choices: []core.Choice{{
			Message: core.ResponseMessage{
				Role: "assistant",
				ToolCalls: []core.ToolCall{{
					ID:       toolID,
					Type:     "function",
					Function: core.FunctionCall{Name: toolName, Arguments: args},
				}},
			},
			FinishReason: "tool_calls",
		}},
	}
	anth := core.ChatResponseToAnthropic(resp, "claude-3-5-sonnet")
	if anth == nil {
		t.Fatal("ChatResponseToAnthropic 返回 nil")
	}
	for _, blk := range anth.Content {
		if blk.Type == "tool_use" && blk.ID == toolID {
			return decodeJSONObject(t, "非流式 tool_use.input", string(blk.Input))
		}
	}
	t.Fatalf("非流式转换结果里没有 id=%q 的 tool_use 块: %+v", toolID, anth.Content)
	return nil
}

// TestAnthropicStream_ToolArgs_FirstChunkWithFullArguments 复现生产事故：
// SGLang 0.5.22 的 deepseekv41 工具解析器把 {id, name, arguments 全量} 放在同一个
// 首块里发出。修复前 tc.ID != "" 分支只发 content_block_start(input:{})，首块
// arguments 被整块丢弃，于是 tool_use.input 永远是 {}。
//
// 断言：(1) content_block_start(tool_use, input:{}) 之后紧接一条 input_json_delta，
// partial_json 严格等于原始 arguments；(2) 拼接结果与非流式转换结果字段级相等。
func TestAnthropicStream_ToolArgs_FirstChunkWithFullArguments(t *testing.T) {
	const args = `{"command": "git status", "description": "show working tree"}`
	wantObj := decodeJSONObject(t, "期望参数", args)

	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_9071", "Bash", args)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 120))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1；事件序列 %v", len(blocks), eventTypes(events))
	}
	b := blocks[0]
	if b.ID != "call_9071" {
		t.Errorf("tool_use.id = %q, want call_9071", b.ID)
	}
	if b.Name != "Bash" {
		t.Errorf("tool_use.name = %q, want Bash", b.Name)
	}
	if b.Index != 0 {
		t.Errorf("tool_use 块 index = %d, want 0", b.Index)
	}
	if b.StartInput == nil || len(b.StartInput) != 0 {
		t.Errorf("content_block_start.content_block.input = %#v, want 空对象 {}", b.StartInput)
	}

	// 关键断言一：start 之后紧接的必须是承载首块 arguments 的 input_json_delta。
	if b.StartPos+1 >= len(events) {
		t.Fatalf("content_block_start 之后没有任何事件")
	}
	next := events[b.StartPos+1]
	nextDelta := eventDelta(next)
	if next["type"] != "content_block_delta" || nextDelta == nil || nextDelta["type"] != "input_json_delta" {
		t.Fatalf("content_block_start 之后的事件 = %#v, want content_block_delta(input_json_delta)；事件序列 %v",
			next, eventTypes(events))
	}
	if got, _ := nextDelta["partial_json"].(string); got != args {
		t.Errorf("首块 input_json_delta.partial_json = %q, want %q（首块 arguments 被丢弃 = 生产事故根因）", got, args)
	}
	if got := eventIndex(next); got != b.Index {
		t.Errorf("首块 input_json_delta.index = %d, want %d", got, b.Index)
	}

	assertToolBlockSequenced(t, events, b)
	assertToolBlockArgs(t, b, args, wantObj)

	// 关键断言二：流式拼接结果必须与非流式 /v1/messages 转换一致。
	nonStream := nonStreamingToolUseInput(t, "call_9071", "Bash", args)
	if got := decodeJSONObject(t, "流式拼接结果", b.PartialJSON); !reflect.DeepEqual(got, nonStream) {
		t.Errorf("流式 input = %#v 与非流式 input = %#v 不一致", got, nonStream)
	}
}

// TestAnthropicStream_ToolArgs_FirstChunkFullArgs_ExactEventSequence 断言首块即带
// 完整参数时的完整 SSE 序列必须逐条为：
// message_start → content_block_start(tool_use) → content_block_delta(input_json_delta,
// 完整 JSON) → content_block_stop → message_delta(stop_reason=tool_use) → message_stop。
// 修复前缺失中间的 input_json_delta（序列退化为 5 条），Claude Code 因此拿到空 input。
func TestAnthropicStream_ToolArgs_FirstChunkFullArgs_ExactEventSequence(t *testing.T) {
	const args = `{"command": "ls", "description": "list files"}`
	wantObj := decodeJSONObject(t, "期望参数", args)

	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_seq", "Bash", args)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 64))

	want := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}
	got := eventTypes(events)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("事件序列 = %v, want %v（修复前缺 input_json_delta）", got, want)
	}

	// index 必须连续正确：同一 tool_use 块的 start/delta/stop 都是 index 0。
	start, delta, stop := events[1], events[2], events[3]
	for name, e := range map[string]map[string]any{"content_block_start": start, "content_block_delta": delta, "content_block_stop": stop} {
		if idx := eventIndex(e); idx != 0 {
			t.Errorf("%s.index = %d, want 0", name, idx)
		}
	}
	cb, _ := start["content_block"].(map[string]any)
	if cb["type"] != "tool_use" || cb["id"] != "call_seq" || cb["name"] != "Bash" {
		t.Errorf("content_block = %#v, want tool_use/call_seq/Bash", cb)
	}
	if input, ok := cb["input"].(map[string]any); !ok || len(input) != 0 {
		t.Errorf("content_block.input = %#v, want 空对象 {}", cb["input"])
	}
	d := eventDelta(delta)
	if d["type"] != "input_json_delta" {
		t.Errorf("delta.type = %v, want input_json_delta", d["type"])
	}
	if partial, _ := d["partial_json"].(string); partial != args {
		t.Errorf("delta.partial_json = %q, want 完整 %q", partial, args)
	}
	if out, stopReason := messageDeltaInfo(t, events); stopReason != "tool_use" {
		t.Errorf("message_delta.stop_reason = %q, want tool_use (output_tokens=%v)", stopReason, out)
	}

	// 同一份事件流再做一次对象级断言，避免只对字符串下判断。
	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1", len(blocks))
	}
	assertToolBlockArgs(t, blocks[0], args, wantObj)
}

// TestAnthropicStream_ToolArgs_ParallelCallsInSingleChunk 覆盖同一个分块里出现多个
// 工具调用（各自 index 不同、各自自带 id/name/完整 arguments）的并行场景。
// 断言：每个工具各自一个 content_block_start + 自己的 input_json_delta + 自己的
// content_block_stop，块内参数不串块，content_block index 连续为 0/1/2。
func TestAnthropicStream_ToolArgs_ParallelCallsInSingleChunk(t *testing.T) {
	const bashArgs = `{"command": "ls", "description": "list files"}`
	const readArgs = `{"file_path": "/tmp/a.go"}`
	const writeArgs = `{"file_path": "/tmp/b.go", "content": "package main\n"}`

	sse := openAISSE(t,
		chunkWithToolCalls(
			toolCallDeltaWithArgs(0, "call_bash", "Bash", bashArgs),
			toolCallDeltaWithArgs(1, "call_read", "Read", readArgs),
			toolCallDeltaWithArgs(2, "call_write", "Write", writeArgs),
		),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 200))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 3 {
		t.Fatalf("tool_use 块数量 = %d, want 3（每个并行工具一块）；事件序列 %v", len(blocks), eventTypes(events))
	}

	// content_block index 必须从 0 连续递增，且与出现顺序一致。
	for i, b := range blocks {
		if b.Index != i {
			t.Errorf("第 %d 个 tool_use 块 index = %d, want %d（index 必须连续）", i, b.Index, i)
		}
		assertToolBlockSequenced(t, events, b)
	}

	bash := toolBlockByID(t, blocks, "call_bash")
	read := toolBlockByID(t, blocks, "call_read")
	write := toolBlockByID(t, blocks, "call_write")
	if bash.Name != "Bash" || read.Name != "Read" || write.Name != "Write" {
		t.Errorf("name 与 id 配对错误: bash=%q read=%q write=%q", bash.Name, read.Name, write.Name)
	}

	assertToolBlockArgs(t, bash, bashArgs, decodeJSONObject(t, "Bash 期望参数", bashArgs))
	assertToolBlockArgs(t, read, readArgs, decodeJSONObject(t, "Read 期望参数", readArgs))
	assertToolBlockArgs(t, write, writeArgs, decodeJSONObject(t, "Write 期望参数", writeArgs))

	// 显式的串块检查（对象级相等之外再点明意图）：每个块只能有自己那份参数。
	if strings.Contains(bash.PartialJSON, "file_path") {
		t.Errorf("Bash 块里混入了别的工具的 file_path 参数: %q", bash.PartialJSON)
	}
	if strings.Contains(read.PartialJSON, "command") {
		t.Errorf("Read 块里混入了 Bash 的 command 参数: %q", read.PartialJSON)
	}
	if strings.Contains(write.PartialJSON, "command") || strings.Contains(write.PartialJSON, `"/tmp/a.go"`) {
		t.Errorf("Write 块里混入了别的工具的参数: %q", write.PartialJSON)
	}

	// 每个工具恰好一条 input_json_delta（本场景每个工具只有一块参数）。
	if n := countContentBlockDeltaType(events, "input_json_delta"); n != 3 {
		t.Errorf("input_json_delta 数量 = %d, want 3（每个工具一条）；事件序列 %v", n, eventTypes(events))
	}
	if n := countEvent(events, "content_block_start"); n != 3 {
		t.Errorf("content_block_start 数量 = %d, want 3", n)
	}
	if n := countEvent(events, "content_block_stop"); n != 3 {
		t.Errorf("content_block_stop 数量 = %d, want 3", n)
	}
	if _, stopReason := messageDeltaInfo(t, events); stopReason != "tool_use" {
		t.Errorf("message_delta.stop_reason = %q, want tool_use", stopReason)
	}
}

// TestAnthropicStream_ToolArgs_TraditionalChunked_Regression 回归保护：OpenAI 官方
// 风格（首块 arguments 为空串，后续分块续传且不带 id）的既有行为必须保持不变，
// 这里把完整事件序列与拼接结果都钉死，防止修首块时改坏分块式上游。
func TestAnthropicStream_ToolArgs_TraditionalChunked_Regression(t *testing.T) {
	const (
		part1 = `{"command":`
		part2 = ` "ls -la",`
		part3 = ` "timeout": 5000}`
		full  = part1 + part2 + part3
	)
	wantObj := decodeJSONObject(t, "期望参数", full)

	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_trad", "Bash", "")),
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, part1)),
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, part2)),
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, part3)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 80))

	want := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_delta",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("事件序列 = %v, want %v（分块式上游的既有行为）", got, want)
	}

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1（续传块没有 id，不能开新块）", len(blocks))
	}
	b := blocks[0]
	if b.ID != "call_trad" || b.Name != "Bash" {
		t.Errorf("tool_use id/name = %q/%q, want call_trad/Bash", b.ID, b.Name)
	}
	if len(b.DeltaPos) != 3 {
		t.Errorf("input_json_delta 数量 = %d, want 3（每块参数各一条）", len(b.DeltaPos))
	}
	assertToolBlockSequenced(t, events, b)
	assertToolBlockArgs(t, b, full, wantObj)
	if _, stopReason := messageDeltaInfo(t, events); stopReason != "tool_use" {
		t.Errorf("message_delta.stop_reason = %q, want tool_use", stopReason)
	}
}

// TestAnthropicStream_ToolArgs_EmptyFirstChunkThenContinuationWithoutID 覆盖
// OpenAI 官方风格：首块 arguments 显式为空串，后续分块只有 arguments（没有 id、
// 没有 name），中间还夹一个 delta:{} 空块。断言参数仍落在同一个 tool_use 块里，
// 不会因为「没有 id」而开新块或丢参数。
func TestAnthropicStream_ToolArgs_EmptyFirstChunkThenContinuationWithoutID(t *testing.T) {
	const (
		part1 = `{"file_path": "/tmp/x.go",`
		part2 = ` "old_string": "a"}`
		full  = part1 + part2
	)
	wantObj := decodeJSONObject(t, "期望参数", full)

	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_empty", "Edit", "")),
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, part1)),
		emptyDeltaChunk(),
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, part2)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 90))

	if n := countEvent(events, "content_block_start"); n != 1 {
		t.Fatalf("content_block_start 数量 = %d, want 1（续传块没有 id，不得开新块）；事件序列 %v",
			n, eventTypes(events))
	}
	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1", len(blocks))
	}
	b := blocks[0]
	if b.ID != "call_empty" || b.Name != "Edit" || b.Index != 0 {
		t.Errorf("tool_use id/name/index = %q/%q/%d, want call_empty/Edit/0", b.ID, b.Name, b.Index)
	}
	if len(b.DeltaPos) != 2 {
		t.Errorf("input_json_delta 数量 = %d, want 2（两个续传块）", len(b.DeltaPos))
	}
	assertToolBlockSequenced(t, events, b)
	assertToolBlockArgs(t, b, full, wantObj)
	if n := countEvent(events, "content_block_stop"); n != 1 {
		t.Errorf("content_block_stop 数量 = %d, want 1", n)
	}
}

// TestAnthropicStream_ToolArgs_InterleavedContinuationAcrossParallelBlocks 覆盖并行
// 场景下参数分块交错到达（OpenAI index 0,1,0,1）：每个块的参数必须落到它自己的
// content_block index 上。修复前续传分支一律写到「当前打开的块」，参数会串块。
func TestAnthropicStream_ToolArgs_InterleavedContinuationAcrossParallelBlocks(t *testing.T) {
	const (
		editPart1 = `{"file_path":`
		editPart2 = ` "/tmp/a.go", "old_string": "x"}`
		bashPart1 = `{"command":`
		bashPart2 = ` "go test ./...", "description": "run tests"}`
		editFull  = editPart1 + editPart2
		bashFull  = bashPart1 + bashPart2
	)
	editObj := decodeJSONObject(t, "Edit 期望参数", editFull)
	bashObj := decodeJSONObject(t, "Bash 期望参数", bashFull)

	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaWithArgs(0, "call_edit", "Edit", editPart1)),
		chunkWithToolCalls(toolCallDeltaWithArgs(1, "call_bash", "Bash", bashPart1)),
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, editPart2)),
		chunkWithToolCalls(toolCallDeltaArgsOnly(1, bashPart2)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 300))

	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 2 {
		t.Fatalf("tool_use 块数量 = %d, want 2；事件序列 %v", len(blocks), eventTypes(events))
	}
	edit := toolBlockByID(t, blocks, "call_edit")
	bash := toolBlockByID(t, blocks, "call_bash")
	if edit.Name != "Edit" || bash.Name != "Bash" {
		t.Errorf("name 与 id 配对错误: edit=%q bash=%q", edit.Name, bash.Name)
	}
	if edit.Index == bash.Index {
		t.Errorf("两个 tool_use 块的 content_block index 相同 (=%d)，必须各自独立", edit.Index)
	}

	// 这是本用例的核心：交错到达的续传参数必须按 OpenAI index 归位。
	assertToolBlockArgs(t, edit, editFull, editObj)
	assertToolBlockArgs(t, bash, bashFull, bashObj)
	if strings.Contains(edit.PartialJSON, "command") {
		t.Errorf("Edit 块里混入了 Bash 的 command 参数: %q", edit.PartialJSON)
	}
	if strings.Contains(bash.PartialJSON, "file_path") {
		t.Errorf("Bash 块里混入了 Edit 的 file_path 参数: %q", bash.PartialJSON)
	}
	for _, b := range []toolUseBlockView{edit, bash} {
		if len(b.DeltaPos) == 0 {
			t.Fatalf("tool_use 块(index=%d, id=%q) 没有任何 input_json_delta；事件序列 %v",
				b.Index, b.ID, eventTypes(events))
		}
		if b.StopPos < 0 {
			t.Fatalf("tool_use 块(index=%d, id=%q) 缺少 content_block_stop；事件序列 %v",
				b.Index, b.ID, eventTypes(events))
		}
	}
	if n := countEvent(events, "content_block_stop"); n != 2 {
		t.Errorf("content_block_stop 数量 = %d, want 2", n)
	}
	if _, stopReason := messageDeltaInfo(t, events); stopReason != "tool_use" {
		t.Errorf("message_delta.stop_reason = %q, want tool_use", stopReason)
	}
}

// TestAnthropicStream_ToolArgs_NameInFirstChunkArgsInSecond 覆盖首块只有 name、
// 没有 arguments 字段（不是空串，而是字段缺失），第二块才下发参数（没有 id）。
// 既有实现应当通过：本用例确认「修复不会只认首块带参的一种形态」。
func TestAnthropicStream_ToolArgs_NameInFirstChunkArgsInSecond(t *testing.T) {
	const args = `{"command": "pwd"}`
	wantObj := decodeJSONObject(t, "期望参数", args)

	sse := openAISSE(t,
		chunkWithToolCalls(toolCallDeltaNameOnly(0, "call_name_only", "Bash")),
		chunkWithToolCalls(toolCallDeltaArgsOnly(0, args)),
		finishChunk("tool_calls"),
	)
	events := parseAnthropicEvents(t, feedAnthropicStream(t, sse, 40))

	if n := countEvent(events, "content_block_start"); n != 1 {
		t.Fatalf("content_block_start 数量 = %d, want 1；事件序列 %v", n, eventTypes(events))
	}
	blocks := collectToolUseBlocks(t, events)
	if len(blocks) != 1 {
		t.Fatalf("tool_use 块数量 = %d, want 1", len(blocks))
	}
	b := blocks[0]
	if b.ID != "call_name_only" || b.Name != "Bash" {
		t.Errorf("tool_use id/name = %q/%q, want call_name_only/Bash（name 必须沿用首块）", b.ID, b.Name)
	}
	assertToolBlockSequenced(t, events, b)
	assertToolBlockArgs(t, b, args, wantObj)
}
