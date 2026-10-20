package bedrock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/example/llm-gateway/internal/core"
)

func TestSanitizeBedrockToolUseID(t *testing.T) {
	longID := strings.Repeat("a", 100)
	digestA := hex.EncodeToString(func() []byte { s := sha256.Sum256([]byte(longID)); return s[:] }())[:bedrockToolIDHashLen]

	tests := []struct {
		name      string
		id        string
		anthropic bool
		want      string
	}{
		{
			name: "合规 ID 原样保留",
			id:   "toolu_01ABCdef-123",
			want: "toolu_01ABCdef-123",
		},
		{
			name: "超过 64 字符：截断 + 原始 ID 摘要",
			id:   longID,
			want: strings.Repeat("a", 55) + "_" + digestA,
		},
		{
			name: "非法字符替换为下划线（含 '.' 与 ':' 对 Anthropic 模型非法）",
			id:   "toolu/a b?c",
			want: "toolu_a_b_c_" + shortDigest("toolu/a b?c"),
		},
		{
			name:      "Anthropic 模型额外禁止点和冒号",
			id:        "toolu.a:b",
			anthropic: true,
			want:      "toolu_a_b_" + shortDigest("toolu.a:b"),
		},
		{
			name:      "非 Anthropic 模型保留点和冒号",
			id:        "toolu.a:b",
			anthropic: false,
			want:      "toolu.a:b",
		},
		{
			name: "空 ID 走兜底前缀",
			id:   "",
			want: bedrockToolIDFallback + "_" + shortDigest(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeBedrockToolUseID(tt.id, tt.anthropic)
			if got != tt.want {
				t.Fatalf("sanitizeBedrockToolUseID(%q) = %q, want %q", tt.id, got, tt.want)
			}
			if len(got) > bedrockToolIDMaxLen {
				t.Fatalf("结果长度 %d 超过 Converse 上限 %d", len(got), bedrockToolIDMaxLen)
			}
		})
	}
}

// TestSanitizeBedrockToolUseID_UniqueAfterTruncation 保证两个只在截断点之后
// 不同的 ID 不会被改写成同一个值（否则 toolUse / toolResult 会串台）。
func TestSanitizeBedrockToolUseID_UniqueAfterTruncation(t *testing.T) {
	base := strings.Repeat("a", 80)
	a := sanitizeBedrockToolUseID(base+"1", false)
	b := sanitizeBedrockToolUseID(base+"2", false)
	if a == b {
		t.Fatalf("截断后 ID 冲突：%q == %q", a, b)
	}
	if len(a) != bedrockToolIDMaxLen || len(b) != bedrockToolIDMaxLen {
		t.Fatalf("长度应为 %d，实际 %d / %d", bedrockToolIDMaxLen, len(a), len(b))
	}
}

// TestSanitizeBedrockToolUseID_IdempotentForMatching 保证同一原始 ID 的
// toolUse 与 toolResult 两侧改写结果一致（Bedrock 会校验配对关系）。
func TestSanitizeBedrockToolUseID_IdempotentForMatching(t *testing.T) {
	id := "call_" + strings.Repeat("x", 70)
	if sanitizeBedrockToolUseID(id, false) != sanitizeBedrockToolUseID(id, false) {
		t.Fatal("同一 ID 两次改写结果不一致")
	}
}

func TestSanitizeMessageToolUseIDs(t *testing.T) {
	longID := strings.Repeat("z", 80)
	messages := []brtypes.Message{{
		Role: brtypes.ConversationRoleAssistant,
		Content: []brtypes.ContentBlock{
			&brtypes.ContentBlockMemberToolUse{
				Value: brtypes.ToolUseBlock{ToolUseId: ptrString(longID)},
			},
			&brtypes.ContentBlockMemberToolResult{
				Value: brtypes.ToolResultBlock{ToolUseId: ptrString(longID)},
			},
		},
	}}

	sanitizeMessageToolUseIDs(messages, "global.xai.grok-4.6")

	useBlock := messages[0].Content[0].(*brtypes.ContentBlockMemberToolUse)
	resultBlock := messages[0].Content[1].(*brtypes.ContentBlockMemberToolResult)
	useID := stringValue(useBlock.Value.ToolUseId)
	resultID := stringValue(resultBlock.Value.ToolUseId)

	if len(useID) != bedrockToolIDMaxLen {
		t.Fatalf("toolUse id 未被规整，长度 %d", len(useID))
	}
	if useID != resultID {
		t.Fatalf("toolUse 与 toolResult 的 ID 不一致：%q vs %q", useID, resultID)
	}
}

func TestStripUnsupportedPatterns(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"collection": map[string]any{
				"type":    "string",
				"pattern": `^(?!\.\.?(?:\/|$))[A-Za-z0-9_]{1,200}$`,
			},
			"name": map[string]any{
				"type":    "string",
				"pattern": `^[A-Za-z0-9_-]{0,63}$`,
			},
			"nested": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":    "string",
					"pattern": `^back\1ref$`,
				},
			},
		},
	}

	out, removed := stripUnsupportedPatterns(schema)
	if removed != 2 {
		t.Fatalf("应移除 2 个不支持的 pattern，实际 %d", removed)
	}

	props := out["properties"].(map[string]any)
	if _, ok := props["collection"].(map[string]any)["pattern"]; ok {
		t.Error("负向前瞻 pattern 应被移除")
	}
	if _, ok := props["nested"].(map[string]any)["items"].(map[string]any)["pattern"]; ok {
		t.Error("反向引用 pattern 应被移除")
	}
	if got := props["name"].(map[string]any)["pattern"]; got != `^[A-Za-z0-9_-]{0,63}$` {
		t.Errorf("受支持的 pattern 不应被改动，实际 %v", got)
	}
	if got := props["collection"].(map[string]any)["type"]; got != "string" {
		t.Errorf("其余约束应保留，type = %v", got)
	}

	// 原 schema 不能被修改：core.ChatRequest 在故障转移时会被复用。
	if _, ok := schema["properties"].(map[string]any)["collection"].(map[string]any)["pattern"]; !ok {
		t.Error("原 schema 被就地修改了")
	}
}

// TestBuildConverseParts_ToolSchemaPatternCompat 验证请求级行为：
// xAI 模型的工具 schema 会被剥离环视 pattern，Anthropic 模型保持不变。
func TestBuildConverseParts_ToolSchemaPatternCompat(t *testing.T) {
	lookahead := `^(?!\.\.?(?:\/|$))[A-Za-z0-9_]{1,200}$`
	req := &core.ChatRequest{
		Model:    "global.xai.grok-4.6",
		Messages: []core.Message{{Role: "user", Content: "hi"}},
		Tools: []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "ArtifactData",
				"description": "reads a document",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"collection": map[string]any{"type": "string", "pattern": lookahead},
					},
				},
			},
		}},
	}

	p := &Provider{}
	parts, err := p.buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts: %v", err)
	}
	if got := toolSchemaPattern(t, parts, "ArtifactData"); got != "" {
		t.Errorf("xAI 模型的 lookaround pattern 应被剥离，实际 %q", got)
	}

	// Anthropic 模型实测支持该 pattern，必须原样保留。
	req.Model = "global.anthropic.claude-sonnet-4-6"
	parts, err = p.buildConverseParts(req)
	if err != nil {
		t.Fatalf("buildConverseParts(claude): %v", err)
	}
	if got := toolSchemaPattern(t, parts, "ArtifactData"); got != lookahead {
		t.Errorf("Anthropic 模型的 pattern 应保留，实际 %q", got)
	}

	// 客户端请求对象不被修改。
	if got := req.Tools[0]["function"].(map[string]any)["parameters"].(map[string]any)["properties"].(map[string]any)["collection"].(map[string]any)["pattern"]; got != lookahead {
		t.Errorf("请求对象被就地修改：%v", got)
	}
}

// toolSchemaPattern 从 Converse 请求里取出指定工具 schema 的 collection.pattern。
func toolSchemaPattern(t *testing.T, parts converseParts, toolName string) string {
	t.Helper()
	if parts.toolCfg == nil {
		t.Fatal("toolCfg 为空")
	}
	for _, tool := range parts.toolCfg.Tools {
		spec, ok := tool.(*brtypes.ToolMemberToolSpec)
		if !ok || stringValue(spec.Value.Name) != toolName {
			continue
		}
		js, ok := spec.Value.InputSchema.(*brtypes.ToolInputSchemaMemberJson)
		if !ok {
			t.Fatalf("工具 %s 的 InputSchema 类型异常：%T", toolName, spec.Value.InputSchema)
		}
		raw, err := js.Value.MarshalSmithyDocument()
		if err != nil {
			t.Fatalf("序列化工具 schema 失败：%v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("反序列化工具 schema 失败：%v", err)
		}
		props, _ := decoded["properties"].(map[string]any)
		collection, _ := props["collection"].(map[string]any)
		pattern, _ := collection["pattern"].(string)
		return pattern
	}
	t.Fatalf("未找到工具 %s", toolName)
	return ""
}

func shortDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:bedrockToolIDHashLen]
}

func ptrString(s string) *string { return &s }

func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// TestStreamEndError 覆盖“无 SDK 错误但零事件”的兜底判定。
func TestStreamEndError(t *testing.T) {
	if err := streamEndError(true); err != nil {
		t.Errorf("收到过 messageStart 的正常关闭不应报错，实际 %v", err)
	}
	if err := streamEndError(false); !errors.Is(err, errEmptyConverseStream) {
		t.Errorf("零事件关闭应判定为上游失败，实际 %v", err)
	}
}
