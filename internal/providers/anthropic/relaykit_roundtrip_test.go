package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/example/llm-gateway/internal/protocol"
)

// 这个测试回答一个具体问题：当客户端按 Anthropic 原生格式发请求、
// 上游也是 Anthropic 系模型时，我们「Claude → Chat → Anthropic」的中转
// 路径会不会丢掉客户端原本能表达的语义？
//
// new-api/relaykit 在源格式与目标格式相同时直接透传 DTO（零转换）。
// 我们走两次转换，本测试把差异固定下来，防止它悄悄扩大。

const claudeNativeRequest = `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 4096,
  "temperature": 0.7,
  "top_k": 40,
  "top_p": 0.9,
  "stop_sequences": ["END"],
  "system": [
    {"type": "text", "text": "You are helpful.", "cache_control": {"type": "ephemeral"}}
  ],
  "thinking": {"type": "enabled", "budget_tokens": 2048},
  "metadata": {"user_id": "user-abc-123"},
  "messages": [
    {"role": "user", "content": [
      {"type": "text", "text": "What is in this image?", "cache_control": {"type": "ephemeral"}},
      {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}}
    ]}
  ],
  "tools": [
    {"name": "get_weather", "description": "Get weather", "input_schema": {"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}, "cache_control": {"type": "ephemeral"}}
  ],
  "tool_choice": {"type": "auto"}
}`

// TestClaudeRoundTripLoss 记录「Claude → Chat → Anthropic」往返后的语义损失。
// 每个 t.Logf("LOSS: ...") 都是一处已知的、相对 new-api 透传路径的差距。
func TestClaudeRoundTripLoss(t *testing.T) {
	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"claude-sonnet-4-5",
		"claude-sonnet-4-5",
	)

	claudeReq, err := protocol.DecodeClaudeRequest([]byte(claudeNativeRequest))
	if err != nil {
		t.Fatalf("decode claude request: %v", err)
	}

	result, err := protocol.ConvertRequest(session, claudeReq)
	if err != nil {
		t.Fatalf("convert claude->chat: %v", err)
	}
	for _, d := range session.Diagnostics {
		t.Logf("relaykit diagnostic: code=%s path=%s msg=%s", d.Code, d.Path, d.Message)
	}

	chatReq, err := protocol.BridgeToChatRequest(session, result)
	if err != nil {
		t.Fatalf("bridge to core chat: %v", err)
	}

	back, err := convertToAnthropicRequest(chatReq)
	if err != nil {
		t.Fatalf("convert chat->anthropic: %v", err)
	}

	final, _ := json.Marshal(back)
	finalStr := string(final)

	// 结构性字段：必须还在，否则是 bug 而不是差距。
	if len(back.StopSequences) == 0 {
		t.Errorf("stop_sequences lost entirely")
	}
	if back.System == nil {
		t.Errorf("system lost entirely")
	}
	if len(back.Tools) == 0 {
		t.Errorf("tools lost entirely")
	}
	if back.Thinking == nil {
		t.Errorf("thinking config lost entirely")
	}

	// 语义性字段：这些是相对 new-api 透传路径的真实差距。
	if !strings.Contains(finalStr, "cache_control") {
		t.Logf("LOSS: cache_control dropped (Anthropic prompt caching disabled -> higher cost)")
	}
	if !strings.Contains(finalStr, "top_k") {
		t.Logf("LOSS: top_k dropped (Anthropic top_k sampling not expressible in Chat)")
	}
	if !strings.Contains(finalStr, "user_id") {
		t.Logf("LOSS: metadata.user_id dropped (upstream user attribution lost)")
	}
	if !strings.Contains(finalStr, "top_p") {
		t.Logf("LOSS: top_p dropped")
	}

	t.Logf("final upstream anthropic request: %s", finalStr)
}
