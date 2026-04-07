package bedrock

import (
	"encoding/json"
	"testing"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/core"
)

// TestMessagesProtocol_CacheControl_EndToEnd 模拟 Claude Desktop /v1/messages 请求：
// 原始 Anthropic JSON → AnthropicToChatRequest → buildConverseParts，
// 断言 system / tool_result / tools 断点都落到 Bedrock CachePoint。
func TestMessagesProtocol_CacheControl_EndToEnd(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 256,
		"stream": true,
		"system": [
			{"type": "text", "text": "x-anthropic-billing-header: test"},
			{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude.", "cache_control": {"type": "ephemeral"}}
		],
		"messages": [
			{"role": "user", "content": "list files"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "toolu_e2e_1", "name": "Bash", "input": {"command": "ls"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_e2e_1", "content": "a.go\nb.go", "cache_control": {"type": "ephemeral"}}
			]}
		],
		"tools": [
			{
				"name": "Bash",
				"description": "run a shell command",
				"input_schema": {"type": "object", "properties": {"command": {"type": "string"}}, "required": ["command"]}
			},
			{
				"name": "Read",
				"description": "read a file",
				"input_schema": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]},
				"cache_control": {"type": "ephemeral"}
			}
		]
	}`)

	var anthReq core.AnthropicMessagesRequest
	if err := json.Unmarshal(raw, &anthReq); err != nil {
		t.Fatalf("unmarshal anthropic request: %v", err)
	}

	chatReq := core.AnthropicToChatRequest(&anthReq)
	if chatReq == nil {
		t.Fatal("AnthropicToChatRequest returned nil")
	}
	// 网关调度后会改成上游 model id；此处直接用 Bedrock 可识别名
	chatReq.Model = "global.anthropic.claude-sonnet-4-6"

	parts, err := buildConverseParts(chatReq)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}

	sysCP := countSystemCachePoints(parts.system)
	if sysCP < 1 {
		t.Fatalf("system CachePoint missing: got %d (explicit system cache_control should survive /v1/messages conversion)", sysCP)
	}

	msgCP := countCachePoints(parts.messages)
	if msgCP < 1 {
		t.Fatalf("message CachePoint missing: got %d (tool_result cache_control should become ContentBlock CachePoint)", msgCP)
	}

	toolCP := countToolCachePoints(parts.toolCfg)
	if toolCP < 1 {
		t.Fatalf("tool CachePoint missing: got %d (tools[].cache_control should become ToolMemberCachePoint)", toolCP)
	}

	// 显式 tool 断点存在时不应再追加 auto tool CachePoint（总数应为 1）
	if toolCP != 1 {
		t.Errorf("tool CachePoints: got %d want 1 (explicit only, no duplicate auto)", toolCP)
	}

	// 客户端已管理 message 断点时，不应再自动插 conversation CachePoint
	// （tool_result 那 1 个是显式的；auto conversation 会变成 ≥2）
	if msgCP != 1 {
		t.Errorf("message CachePoints: got %d want 1 (explicit tool_result only)", msgCP)
	}

	assertLastSystemIsCachePoint(t, parts.system)
	assertHasToolResultThenCachePoint(t, parts.messages)
}

func TestMessagesProtocol_NoClientCache_AutoInjectTools(t *testing.T) {
	// 客户端故意不传任何 cache_control：应默认开启 tools（+ 长对话）缓存
	raw := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 64,
		"system": "You are a helpful assistant with a reasonably long system prompt for caching thresholds when needed.",
		"messages": [
			{"role": "user", "content": "m0"},
			{"role": "assistant", "content": "a0"},
			{"role": "user", "content": "m1"},
			{"role": "assistant", "content": "a1"},
			{"role": "user", "content": "m2"},
			{"role": "assistant", "content": "a2"},
			{"role": "user", "content": "m3"}
		],
		"tools": [
			{"name": "Bash", "description": "shell", "input_schema": {"type": "object", "properties": {}}},
			{"name": "Read", "description": "read", "input_schema": {"type": "object", "properties": {}}}
		]
	}`)
	var anthReq core.AnthropicMessagesRequest
	if err := json.Unmarshal(raw, &anthReq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	chatReq := core.AnthropicToChatRequest(&anthReq)
	chatReq.Model = "global.anthropic.claude-sonnet-4-6"

	parts, err := buildConverseParts(chatReq)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}
	if got := countToolCachePoints(parts.toolCfg); got != 1 {
		t.Errorf("auto tool CachePoint: got %d want 1", got)
	}
	if got := countCachePoints(parts.messages); got != 1 {
		t.Errorf("auto conversation CachePoint: got %d want 1", got)
	}
}

func assertLastSystemIsCachePoint(t *testing.T, system []brtypes.SystemContentBlock) {
	t.Helper()
	if len(system) == 0 {
		t.Fatal("empty system")
	}
	if _, ok := system[len(system)-1].(*brtypes.SystemContentBlockMemberCachePoint); !ok {
		t.Errorf("expected trailing system CachePoint, last=%T", system[len(system)-1])
	}
}

func assertHasToolResultThenCachePoint(t *testing.T, messages []brtypes.Message) {
	t.Helper()
	for _, msg := range messages {
		for i, block := range msg.Content {
			if _, ok := block.(*brtypes.ContentBlockMemberToolResult); !ok {
				continue
			}
			if i+1 >= len(msg.Content) {
				t.Fatal("tool_result not followed by CachePoint")
			}
			if _, ok := msg.Content[i+1].(*brtypes.ContentBlockMemberCachePoint); !ok {
				t.Fatalf("block after tool_result is %T, want CachePoint", msg.Content[i+1])
			}
			return
		}
	}
	t.Fatal("no tool_result block found in converse messages")
}
