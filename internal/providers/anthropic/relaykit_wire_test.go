package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/example/llm-gateway/internal/protocol"
)

// TestOpenAICompatibleChannelKeepsFields 验证 26 个 openai-compatible 渠道的真实路径：
// Claude 请求 → relaykit → core.ChatRequest → JSON 序列化（上游收到什么）。
// 这些渠道的 body 就是 ChatRequest.MarshalJSON 的输出，所以只要 ExtraFields 合并正确，
// top_p / stop / frequency_penalty 等就不是丢失项。
func TestOpenAICompatibleChannelKeepsFields(t *testing.T) {
	const req = `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 1024,
  "temperature": 0.7,
  "top_p": 0.9,
  "top_k": 40,
  "stop_sequences": ["END"],
  "metadata": {"user_id": "u-1"},
  "messages": [{"role": "user", "content": "hi"}]
}`

	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"claude-sonnet-4-5", "claude-sonnet-4-5",
	)
	cr, err := protocol.DecodeClaudeRequest([]byte(req))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	res, err := protocol.ConvertRequest(session, cr)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	chatReq, err := protocol.BridgeToChatRequest(session, res)
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}

	// 上游实际收到的字节
	wire, err := json.Marshal(chatReq)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(wire)
	t.Logf("upstream body: %s", body)

	for _, field := range []string{"top_p", "stop", "temperature", "max_tokens"} {
		if !strings.Contains(body, "\""+field+"\"") {
			t.Errorf("field %q missing from upstream Chat body", field)
		}
	}
	// 已知不在 Chat 协议里的，确认我们没谎报
	for _, field := range []string{"top_k", "user_id"} {
		if strings.Contains(body, "\""+field+"\"") {
			t.Logf("note: %q unexpectedly present", field)
		} else {
			t.Logf("KNOWN GAP: %q not expressible in Chat body for openai-compatible channels", field)
		}
	}
}
