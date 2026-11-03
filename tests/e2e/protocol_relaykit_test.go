package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/protocol"
)

// ─── 非流式端到端：Claude 请求 → relaykit → ChatRequest → 模拟上游 → ChatResponse → Claude 响应 ───

func TestE2E_ClaudeMessagesToChat_NonStream(t *testing.T) {
	// 1. 模拟上游返回 OpenAI Chat 响应
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := core.ChatResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Model:   "claude-sonnet-4-5",
			Created: time.Now().Unix(),
			Choices: []core.Choice{{
				Index: 0,
				Message: core.ResponseMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you today?",
				},
				FinishReason: "stop",
			}},
			Usage: core.Usage{
				PromptTokens:     10,
				CompletionTokens: 8,
				TotalTokens:      18,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockUpstream.Close()

	// 2. 构造 Claude Messages 请求
	claudeJSON := `{"model":"claude-sonnet-4-5","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}`

	// 3. 解码 → 转换 → 桥接
	claudeReq, err := protocol.DecodeClaudeRequest([]byte(claudeJSON))
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"claude-sonnet-4-5",
		"claude-sonnet-4-5",
	)

	result, err := protocol.ConvertRequest(session, claudeReq)
	if err != nil {
		t.Fatalf("ConvertRequest: %v", err)
	}

	bridgeDiags := protocol.HasUnsupportedBlocks(claudeReq)
	protocol.JoinDiagnostics(session, result, bridgeDiags)

	chatReq, err := protocol.BridgeToChatRequest(session, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	// 4. 验证桥接后的 ChatRequest
	if chatReq.Model != "claude-sonnet-4-5" {
		t.Errorf("Model: got %q, want %q", chatReq.Model, "claude-sonnet-4-5")
	}
	if chatReq.MaxTokens == nil || *chatReq.MaxTokens != 1024 {
		t.Errorf("MaxTokens: got %v, want 1024", chatReq.MaxTokens)
	}
	if len(chatReq.Messages) != 1 {
		t.Fatalf("Messages: got %d, want 1", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "user" {
		t.Errorf("Role: got %q, want user", chatReq.Messages[0].Role)
	}

	// 5. 模拟上游调用（用 mock 响应）
	chatResp := &core.ChatResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Model:   "claude-sonnet-4-5",
		Created: time.Now().Unix(),
		Choices: []core.Choice{{
			Index: 0,
			Message: core.ResponseMessage{
				Role:    "assistant",
				Content: "Hello! How can I help you today?",
			},
			FinishReason: "stop",
		}},
		Usage: core.Usage{
			PromptTokens:     10,
			CompletionTokens: 8,
			TotalTokens:      18,
		},
	}

	// 6. 回程：ChatResponse → Claude 格式
	claudeResult, err := protocol.BridgeChatResponseToSource(session, chatResp)
	if err != nil {
		t.Fatalf("BridgeChatResponseToSource: %v", err)
	}
	if claudeResult == nil {
		t.Fatal("BridgeChatResponseToSource returned nil")
	}

	// 7. 验证 Claude 响应格式
	claudeRespJSON, err := json.Marshal(claudeResult.Value)
	if err != nil {
		t.Fatalf("Marshal Claude response: %v", err)
	}

	var claudeResp map[string]any
	if err := json.Unmarshal(claudeRespJSON, &claudeResp); err != nil {
		t.Fatalf("Unmarshal Claude response: %v", err)
	}

	// Anthropic Messages 响应必须包含这些字段
	if _, ok := claudeResp["id"]; !ok {
		t.Error("Claude response missing 'id'")
	}
	if _, ok := claudeResp["type"]; !ok {
		t.Error("Claude response missing 'type'")
	}
	if v, ok := claudeResp["type"].(string); ok && v != "message" {
		t.Errorf("type: got %q, want message", v)
	}
	if _, ok := claudeResp["role"]; !ok {
		t.Error("Claude response missing 'role'")
	}
	if content, ok := claudeResp["content"]; !ok {
		t.Error("Claude response missing 'content'")
	} else {
		contentArr, ok := content.([]any)
		if !ok || len(contentArr) == 0 {
			t.Error("Claude response content is empty or not an array")
		}
	}
	if _, ok := claudeResp["usage"]; !ok {
		t.Error("Claude response missing 'usage'")
	}

	t.Logf("✅ E2E non-stream: Claude → Chat → Claude roundtrip OK")
	t.Logf("   diagnostics: %d", len(session.Diagnostics))
	t.Logf("   Claude response keys: %v", keysOf(claudeResp))
}

// ─── 流式端到端：Claude 请求 → OpenAI SSE 上游 → relaykit StreamAdapter → Anthropic SSE ───

func TestE2E_ClaudeMessagesToChat_Streaming(t *testing.T) {
	// 1. 模拟 OpenAI SSE 上游
	openAIChunks := []string{
		`{"id":"chatcmpl-123","object":"chat.completion.chunk","created":1,"model":"claude-sonnet-4-5","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"chatcmpl-123","object":"chat.completion.chunk","created":1,"model":"claude-sonnet-4-5","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-123","object":"chat.completion.chunk","created":1,"model":"claude-sonnet-4-5","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-123","object":"chat.completion.chunk","created":1,"model":"claude-sonnet-4-5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
	}

	// 2. 构造上游 SSE 流
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for _, chunk := range openAIChunks {
			pw.Write([]byte("data: " + chunk + "\n\n"))
		}
		pw.Write([]byte("data: [DONE]\n\n"))
	}()

	// 3. 创建 session + stream adapter
	claudeJSON := `{"model":"claude-sonnet-4-5","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}],"stream":true}`

	claudeReq, err := protocol.DecodeClaudeRequest([]byte(claudeJSON))
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"claude-sonnet-4-5",
		"claude-sonnet-4-5",
	)

	// 请求阶段转换（用于获取 estimatedInputTokens 等上下文）
	result, _ := protocol.ConvertRequest(session, claudeReq)
	bridgeDiags := protocol.HasUnsupportedBlocks(claudeReq)
	protocol.JoinDiagnostics(session, result, bridgeDiags)

	// 4. 用 StreamAdapter 包装上游流
	adapter := protocol.NewStreamAdapter(pr, session, "claude-sonnet-4-5", 10, "msg_123")

	// 5. 读取 Anthropic SSE 输出
	var output strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := adapter.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	adapter.Close()

	outputStr := output.String()

	// 6. 验证 Anthropic SSE 格式
	if !strings.Contains(outputStr, "event: message_start") {
		t.Error("Stream output missing 'event: message_start'")
	}
	if !strings.Contains(outputStr, "event: content_block_start") {
		t.Error("Stream output missing 'event: content_block_start'")
	}
	if !strings.Contains(outputStr, "event: content_block_delta") {
		t.Error("Stream output missing 'event: content_block_delta'")
	}
	if !strings.Contains(outputStr, "event: content_block_stop") {
		t.Error("Stream output missing 'event: content_block_stop'")
	}
	if !strings.Contains(outputStr, "event: message_delta") {
		t.Error("Stream output missing 'event: message_delta'")
	}
	if !strings.Contains(outputStr, "event: message_stop") {
		t.Error("Stream output missing 'event: message_stop'")
	}

	// 7. 验证每行 data: 后的 JSON 是合法 JSON
	lines := strings.Split(outputStr, "\n")
	jsonCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" {
				continue
			}
			var v any
			if err := json.Unmarshal([]byte(payload), &v); err != nil {
				t.Errorf("Invalid JSON in SSE data: %q (error: %v)", payload[:min(80, len(payload))], err)
			}
			jsonCount++
		}
	}
	if jsonCount < 5 {
		t.Errorf("Expected at least 5 JSON data lines, got %d", jsonCount)
	}

	t.Logf("✅ E2E stream: %d SSE lines, %d JSON payloads", len(lines), jsonCount)
}

// ─── 工具调用端到端 ───

func TestE2E_ClaudeMessagesToChat_ToolUse(t *testing.T) {
	claudeJSON := `{
  "model": "claude-sonnet-4-5",
  "max_tokens": 1024,
  "messages": [{"role": "user", "content": "What is the weather in San Francisco?"}],
  "tools": [{
    "name": "get_weather",
    "description": "Get the current weather for a location",
    "input_schema": {
      "type": "object",
      "properties": {
        "location": {"type": "string", "description": "City name"}
      },
      "required": ["location"]
    }
  }]
}`

	claudeReq, err := protocol.DecodeClaudeRequest([]byte(claudeJSON))
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"claude-sonnet-4-5",
		"claude-sonnet-4-5",
	)

	result, err := protocol.ConvertRequest(session, claudeReq)
	if err != nil {
		t.Fatalf("ConvertRequest: %v", err)
	}

	bridgeDiags := protocol.HasUnsupportedBlocks(claudeReq)
	protocol.JoinDiagnostics(session, result, bridgeDiags)

	chatReq, err := protocol.BridgeToChatRequest(session, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	// 验证工具定义被正确桥接
	if len(chatReq.Tools) != 1 {
		t.Fatalf("Tools: got %d, want 1", len(chatReq.Tools))
	}
	tool := chatReq.Tools[0]
	if toolType, ok := tool["type"].(string); !ok || toolType != "function" {
		t.Errorf("Tool type: got %v, want function", tool["type"])
	}
	if fn, ok := tool["function"].(map[string]any); ok {
		if name, ok := fn["name"].(string); !ok || name != "get_weather" {
			t.Errorf("Tool name: got %v, want get_weather", fn["name"])
		}
	} else {
		t.Error("Tool missing 'function' field")
	}

	t.Logf("✅ E2E tool use: tool definition survived roundtrip")
}

// ─── 损失诊断端到端 ───

func TestE2E_Diagnostics_LossHeader(t *testing.T) {
	session := protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		protocol.DefaultConvOptions(),
		"claude-sonnet-4-5",
		"claude-sonnet-4-5",
	)

	// 模拟转换诊断
	session.Diagnostics = []types.ConversionDiagnostic{
		{Code: "protocol.unsupported.video", Severity: types.ConversionDiagnosticWarning, Message: "video content block not supported"},
		{Code: "protocol.bridge.temperature", Severity: types.ConversionDiagnosticWarning, Message: "temperature precision lost"},
		{Code: "protocol.unsupported.server_tool", Severity: types.ConversionDiagnosticError, Message: "server_tool_use not supported"},
	}

	header := protocol.FormatLossHeader(session.Diagnostics)
	if header == "" {
		t.Error("FormatLossHeader returned empty for non-empty diagnostics")
	}
	if !strings.Contains(header, "protocol.unsupported.video") {
		t.Errorf("Loss header missing expected code: %s", header)
	}

	// 验证 safe 策略拒绝 error 级别
	err := protocol.ApplyLossPolicy(types.ConversionLossPolicySafe, session.Diagnostics)
	if err == nil {
		t.Error("ApplyLossPolicy(safe) should reject errors")
	}

	// 验证 allow 策略放行
	err = protocol.ApplyLossPolicy(types.ConversionLossPolicyAllow, session.Diagnostics)
	if err != nil {
		t.Errorf("ApplyLossPolicy(allow) should not reject: %v", err)
	}

	t.Logf("✅ E2E diagnostics: loss header=%q", header)
}

// ─── 策略优先级端到端 ───

func TestE2E_Policy_Priority(t *testing.T) {
	cfg := protocol.PolicyConfig{
		DefaultMode: "legacy",
		PerEntry:    map[string]string{"messages": "shadow"},
		PerKey:      map[string]string{"key-vip": "active"},
	}
	policy, err := protocol.LoadPolicy(cfg)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}

	// Key 覆盖 entry
	if mode := policy.ModeFor("key-vip", "messages"); mode != protocol.ModeActive {
		t.Errorf("VIP key should get active, got %s", mode)
	}

	// Entry 覆盖 default
	if mode := policy.ModeFor("key-normal", "messages"); mode != protocol.ModeShadow {
		t.Errorf("messages entry should get shadow, got %s", mode)
	}

	// Default fallback
	if mode := policy.ModeFor("key-normal", "responses"); mode != protocol.ModeLegacy {
		t.Errorf("default should be legacy, got %s", mode)
	}

	t.Logf("✅ E2E policy: Key > Entry > Default priority verified")
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
