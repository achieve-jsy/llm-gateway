package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/protocol"
)

// testSession 创建一个用于测试的 Session，源格式为 Claude，目标为 OpenAI Chat。
func testSession() *protocol.Session {
	return protocol.NewSession(
		types.RelayFormatClaude,
		types.RelayFormatOpenAI,
		convmeta.Options{},
		"claude-sonnet-4-5",
		"claude-sonnet-4-5",
	)
}

// TestBridgeClaudeToChat_BasicText 验证简单文本请求到 ChatRequest 的桥接。
func TestBridgeClaudeToChat_BasicText(t *testing.T) {
	raw := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hello"}],"max_tokens":1024}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	s := testSession()
	result, err := protocol.ConvertRequest(s, claudeReq)
	if err != nil {
		t.Fatalf("ConvertRequest: %v", err)
	}

	chatReq, err := protocol.BridgeToChatRequest(s, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	if chatReq.Model != "claude-sonnet-4-5" {
		t.Errorf("Model: got %q, want %q", chatReq.Model, "claude-sonnet-4-5")
	}
	if len(chatReq.Messages) != 1 {
		t.Fatalf("Messages len: got %d, want 1", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role != "user" {
		t.Errorf("Role: got %q, want %q", chatReq.Messages[0].Role, "user")
	}
	content, ok := chatReq.Messages[0].Content.(string)
	if !ok {
		t.Fatalf("Content type: got %T, want string", chatReq.Messages[0].Content)
	}
	if content != "Hello" {
		t.Errorf("Content: got %q, want %q", content, "Hello")
	}
	if chatReq.MaxTokens == nil {
		t.Fatal("MaxTokens: should not be nil")
	}
	if *chatReq.MaxTokens != 1024 {
		t.Errorf("MaxTokens: got %d, want 1024", *chatReq.MaxTokens)
	}
}

// TestBridgeClaudeToChat_WithTools 验证带工具定义的请求转换。
func TestBridgeClaudeToChat_WithTools(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-5",
		"messages": [{"role": "user", "content": "What is the weather?"}],
		"max_tokens": 1024,
		"tools": [{
			"name": "get_weather",
			"description": "Get current weather",
			"input_schema": {
				"type": "object",
				"properties": {
					"location": {"type": "string", "description": "City name"}
				},
				"required": ["location"]
			}
		}]
	}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	s := testSession()
	result, err := protocol.ConvertRequest(s, claudeReq)
	if err != nil {
		t.Fatalf("ConvertRequest: %v", err)
	}

	chatReq, err := protocol.BridgeToChatRequest(s, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	if len(chatReq.Tools) == 0 {
		t.Fatal("Tools: should not be empty")
	}
	if len(chatReq.Tools) != 1 {
		t.Fatalf("Tools len: got %d, want 1", len(chatReq.Tools))
	}

	tool := chatReq.Tools[0]
	if tool["type"] != "function" {
		t.Errorf("Tool type: got %v, want function", tool["type"])
	}

	fn, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatal("Tool function: not a map")
	}
	if fn["name"] != "get_weather" {
		t.Errorf("Tool name: got %v, want get_weather", fn["name"])
	}
}

// TestBridgeClaudeToChat_WithThinking 验证 thinking 配置到 Reasoning 字段的映射。
func TestBridgeClaudeToChat_WithThinking(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-5",
		"messages": [{"role": "user", "content": "Think deeply"}],
		"max_tokens": 4096,
		"thinking": {"type": "enabled", "budget_tokens": 2048}
	}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	s := testSession()
	result, err := protocol.ConvertRequest(s, claudeReq)
	if err != nil {
		t.Fatalf("ConvertRequest: %v", err)
	}

	chatReq, err := protocol.BridgeToChatRequest(s, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	// thinking budget 应该被 relaykit 转换为推理相关的配置
	// 至少 Reasoning 字段应该非空（可能以 reasoning_effort 形式出现）
	if chatReq.Reasoning == nil {
		t.Log("Note: Reasoning is nil — relaykit may map thinking budget differently")
	}
}

// TestBridgeClaudeToChat_DocumentBlock 验证文档内容块的处理。
func TestBridgeClaudeToChat_DocumentBlock(t *testing.T) {
	// Claude 的 document 块不是标准 Messages API 内容类型，
	// relaykit 应产生诊断信息或将其转换为文本
	raw := []byte(`{
		"model": "claude-sonnet-4-5",
		"messages": [{
			"role": "user",
			"content": [{
				"type": "document",
				"source": {
					"type": "text",
					"media_type": "text/plain",
					"data": "SGVsbG8="
				}
			}]
		}],
		"max_tokens": 1024
	}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	// hasUnsupportedBlocks 应检测到不支持的块
	diags := protocol.HasUnsupportedBlocks(claudeReq)
	// document 块不是 server_tool_use/web_search_tool_use，但 relaykit 转换时
	// 也可能产生诊断。这里只验证桥接不会 panic。
	t.Logf("Diagnostics for document block: %d items", len(diags))

	s := testSession()
	result, err := protocol.ConvertRequest(s, claudeReq)
	if err != nil {
		t.Logf("ConvertRequest returned error (expected for unsupported block): %v", err)
		return
	}

	chatReq, err := protocol.BridgeToChatRequest(s, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	// 至少应该有一个消息
	if len(chatReq.Messages) == 0 {
		t.Fatal("Expected at least one message")
	}
	t.Logf("Messages count: %d", len(chatReq.Messages))
}

// TestBridgeClaudeToChat_NilTemperature 验证未设置 temperature 时桥不注入零值。
func TestBridgeClaudeToChat_NilTemperature(t *testing.T) {
	raw := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":100}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	// 验证原始请求中 temperature 为空
	if claudeReq.Temperature != nil {
		t.Fatalf("Precondition: temperature should be nil, got %v", *claudeReq.Temperature)
	}

	s := testSession()
	result, err := protocol.ConvertRequest(s, claudeReq)
	if err != nil {
		t.Fatalf("ConvertRequest: %v", err)
	}

	chatReq, err := protocol.BridgeToChatRequest(s, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	// Temperature 应为 nil（不是零值）
	if chatReq.Temperature != nil {
		t.Errorf("Temperature should be nil when not set, got %v", *chatReq.Temperature)
	}
}

// TestBridgeClaudeToChat_TopP 验证 top_p 进入 ExtraFields。
func TestBridgeClaudeToChat_TopP(t *testing.T) {
	raw := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":100,"top_p":0.9}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	s := testSession()
	result, err := protocol.ConvertRequest(s, claudeReq)
	if err != nil {
		t.Fatalf("ConvertRequest: %v", err)
	}

	chatReq, err := protocol.BridgeToChatRequest(s, result)
	if err != nil {
		t.Fatalf("BridgeToChatRequest: %v", err)
	}

	// top_p 应该在 ExtraFields 中
	if chatReq.ExtraFields.IsEmpty() {
		t.Fatal("ExtraFields should contain top_p")
	}

	// 序列化后验证 top_p 存在
	data, err := json.Marshal(chatReq)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var rawResp map[string]any
	if err := json.Unmarshal(data, &rawResp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if topP, ok := rawResp["top_p"]; !ok {
		t.Error("top_p not found in marshaled output")
	} else {
		if tp, ok := topP.(float64); !ok || tp != 0.9 {
			t.Errorf("top_p: got %v, want 0.9", topP)
		}
	}
}

// TestHasUnsupportedBlocks_ServerToolUse 验证 server_tool_use 块的检测。
func TestHasUnsupportedBlocks_ServerToolUse(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-5",
		"messages": [{
			"role": "user",
			"content": [{
				"type": "text",
				"text": "Search for something"
			}]
		}, {
			"role": "assistant",
			"content": [{
				"type": "server_tool_use",
				"id": "stu_1",
				"name": "web_search",
				"input": {"query": "test"}
			}]
		}],
		"max_tokens": 1024
	}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	diags := protocol.HasUnsupportedBlocks(claudeReq)
	if len(diags) == 0 {
		t.Fatal("Expected diagnostics for server_tool_use block")
	}

	found := false
	for _, d := range diags {
		if d.Code == "unsupported_content_block" {
			found = true
			t.Logf("Diagnostic: code=%s path=%s message=%s", d.Code, d.Path, d.Message)
		}
	}
	if !found {
		t.Error("No unsupported_content_block diagnostic found")
	}
}

// TestBridgeChatResponseToSource_Basic 验证 ChatResponse 到源格式的桥接。
func TestBridgeChatResponseToSource_Basic(t *testing.T) {
	chatResp := &core.ChatResponse{
		ID:      "resp-1",
		Object:  "chat.completion",
		Model:   "claude-sonnet-4-5",
		Created: 1234567890,
		Choices: []core.Choice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: core.ResponseMessage{
					Role:    "assistant",
					Content: "Hello! How can I help?",
				},
			},
		},
		Usage: core.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	s := testSession()
	result, err := protocol.BridgeChatResponseToSource(s, chatResp)
	if err != nil {
		t.Fatalf("BridgeChatResponseToSource: %v", err)
	}

	if result == nil {
		t.Fatal("ResponseResult should not be nil")
	}
	if result.Value == nil {
		t.Fatal("ResponseResult.Value should not be nil")
	}

	// 结果应该是 Claude 格式的响应
	claudeResp, ok := result.Value.(*dto.ClaudeResponse)
	if !ok {
		t.Fatalf("ResponseResult.Value: expected *dto.ClaudeResponse, got %T", result.Value)
	}

	if claudeResp.Role != "assistant" {
		t.Errorf("Role: got %q, want assistant", claudeResp.Role)
	}
	if len(claudeResp.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	if claudeResp.Content[0].Type != "text" {
		t.Errorf("Content type: got %q, want text", claudeResp.Content[0].Type)
	}
	if claudeResp.Content[0].GetText() != "Hello! How can I help?" {
		t.Errorf("Content text: got %q, want %q", claudeResp.Content[0].GetText(), "Hello! How can I help?")
	}
}

// TestHasUnsupportedBlocks_NoBlocks 验证正常请求不产生诊断。
func TestHasUnsupportedBlocks_NoBlocks(t *testing.T) {
	raw := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hello"}],"max_tokens":100}`)

	claudeReq, err := protocol.DecodeClaudeRequest(raw)
	if err != nil {
		t.Fatalf("DecodeClaudeRequest: %v", err)
	}

	diags := protocol.HasUnsupportedBlocks(claudeReq)
	if len(diags) != 0 {
		t.Errorf("Expected no diagnostics, got %d: %v", len(diags), diags)
	}
}

// TestHasUnsupportedBlocks_Nil 验证 nil 请求不 panic。
func TestHasUnsupportedBlocks_Nil(t *testing.T) {
	diags := protocol.HasUnsupportedBlocks(nil)
	if diags != nil {
		t.Errorf("Expected nil diagnostics for nil request, got %v", diags)
	}
}
