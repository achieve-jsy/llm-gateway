package gemini

import (
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/example/llm-gateway/internal/protocol"
)

// 这些测试验证真实的跨渠道链路：
// Gemini 原生 SSE → gemini adapter 归一化 → relaykit 协议转换 → Anthropic SSE。
// 每一段单独测试都过、接起来崩掉的场景，只有这种串接测试才能发现。

// geminiNativeSSE 是一段真实的 Gemini 原生流式响应样本（含文本、工具调用、usage）。
const geminiNativeSSE = `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"index":0}],"responseId":"resp-g1","modelVersion":"gemini-3.5-flash"}

data: {"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"},"index":0}],"responseId":"resp-g1","modelVersion":"gemini-3.5-flash"}

data: {"candidates":[{"content":{"parts":[{"text":"!"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15},"responseId":"resp-g1","modelVersion":"gemini-3.5-flash"}

`

// TestGeminiStreamToAnthropic 验证 Gemini 原生流经完整链路后产出合法的 Anthropic SSE。
func TestGeminiStreamToAnthropic(t *testing.T) {
	normalized := newGeminiNativeStream(
		io.NopCloser(strings.NewReader(geminiNativeSSE)),
		"gemini-3.5-flash",
		true,
		"vertex",
	)

	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"gemini-3.5-flash",
		"gemini-3.5-flash",
	)

	adapted := protocol.NewStreamAdapter(normalized, session, "gemini-3.5-flash", 10, "resp-g1")
	defer adapted.Close()

	raw, err := io.ReadAll(adapted)
	if err != nil {
		t.Fatalf("read adapted stream: %v", err)
	}
	out := string(raw)

	// 必须产出 Anthropic 事件序列，且不能泄漏内部计费字段。
	for _, want := range []string{
		"event: message_start",
		"event: content_block_delta",
		"event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "event: message_start"); n != 1 {
		t.Errorf("message_start count = %d, want 1", n)
	}
	if strings.Contains(out, "billing_usage") || strings.Contains(out, "claude_cache_creation") {
		t.Errorf("internal billing fields leaked:\n%s", out)
	}
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "world") {
		t.Errorf("text content lost:\n%s", out)
	}
}

// TestGeminiStreamToolCallToAnthropic 验证 Gemini 的 functionCall 经完整链路变成 Anthropic tool_use。
func TestGeminiStreamToolCallToAnthropic(t *testing.T) {
	const sse = `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"location":"SF"}}}],"role":"model"},"finishReason":"STOP","index":0}],"responseId":"resp-g2","modelVersion":"gemini-3.5-flash"}

`

	normalized := newGeminiNativeStream(
		io.NopCloser(strings.NewReader(sse)),
		"gemini-3.5-flash",
		false,
		"vertex",
	)
	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"gemini-3.5-flash",
		"gemini-3.5-flash",
	)
	adapted := protocol.NewStreamAdapter(normalized, session, "gemini-3.5-flash", 10, "resp-g2")
	defer adapted.Close()

	raw, err := io.ReadAll(adapted)
	if err != nil {
		t.Fatalf("read adapted stream: %v", err)
	}
	out := string(raw)

	if !strings.Contains(out, "get_weather") {
		t.Errorf("tool name lost:\n%s", out)
	}
	if !strings.Contains(out, "tool_use") {
		t.Errorf("tool_use block not produced:\n%s", out)
	}
}
