package bedrock

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// ──────────────────────────────────────────────
// tool 调用 ID 规整
// ──────────────────────────────────────────────

// Bedrock Converse 对 toolUseId 的约束由 Bedrock 自身的请求校验强制（与具体模型无关）：
//
//	ValidationException: N validation errors detected: Value at
//	'messages.156.member.content.1.member.toolUse.toolUseId' failed to satisfy
//	constraint: Member must satisfy regular expression pattern: [a-zA-Z0-9_.:-]+;
//	Member must have length less than or equal to 64
//
// Anthropic 系模型经 Converse 时约束更紧：tool_use_id 只允许 [a-zA-Z0-9_-]。
var (
	bedrockToolIDInvalid   = regexp.MustCompile(`[^a-zA-Z0-9_.:-]`)
	anthropicToolIDInvalid = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
)

const (
	// bedrockToolIDMaxLen 是 Converse 对单个 toolUseId 的长度上限。
	bedrockToolIDMaxLen = 64
	// bedrockToolIDHashLen 是改写 ID 时追加的原始 ID 摘要长度（用于保持唯一）。
	bedrockToolIDHashLen = 8
	// bedrockToolIDFallback 用于原始 ID 为空（全部字符非法）时的兜底前缀。
	bedrockToolIDFallback = "tool_use"
)

// sanitizeBedrockToolUseID 把客户端传来的 tool 调用 ID 规整成 Bedrock Converse
// 可接受的形态，算法对齐 LiteLLM 的 _sanitize_bedrock_tool_use_id：
//
//  1. 非法字符替换为 "_"；
//  2. 若被改写或超过 64 字符，则截断到 55 位并追加原始 ID 的 sha256 前 8 位，
//     保证两个只差一个被替换字符（或只差截断点之后内容）的 ID 仍映射到不同值。
//
// 该函数是纯函数：toolUse 与 toolResult 两侧用同一原始 ID 调用必然得到同一结果，
// 因此改写不会破坏「调用 ↔ 结果」的配对关系（Bedrock 会校验二者一致）。
func sanitizeBedrockToolUseID(id string, anthropic bool) string {
	invalid := bedrockToolIDInvalid
	if anthropic {
		invalid = anthropicToolIDInvalid
	}
	sanitized := invalid.ReplaceAllString(id, "_")
	if sanitized == "" {
		sanitized = bedrockToolIDFallback
	}
	if sanitized == id && len(sanitized) <= bedrockToolIDMaxLen {
		return sanitized
	}
	sum := sha256.Sum256([]byte(id))
	digest := hex.EncodeToString(sum[:])[:bedrockToolIDHashLen]
	if keep := bedrockToolIDMaxLen - bedrockToolIDHashLen - 1; len(sanitized) > keep {
		sanitized = sanitized[:keep]
	}
	return sanitized + "_" + digest
}

// sanitizeMessageToolUseIDs 就地规整 messages 中全部 toolUse / toolResult 的 ID。
//
// 只在 bedrockProvider 适配层做，不改动客户端请求本身：Anthropic 侧的 tool_use
// id 由客户端生成（Claude Code 会带上超过 64 字符的 ID），原样下发会被 Bedrock 拒绝。
func sanitizeMessageToolUseIDs(messages []brtypes.Message, modelID string) {
	anthropic := strings.HasPrefix(normalizeBedrockModelID(modelID), "anthropic.")
	for i := range messages {
		for _, block := range messages[i].Content {
			switch b := block.(type) {
			case *brtypes.ContentBlockMemberToolUse:
				if id := awssdk.ToString(b.Value.ToolUseId); id != "" {
					b.Value.ToolUseId = awssdk.String(sanitizeBedrockToolUseID(id, anthropic))
				}
			case *brtypes.ContentBlockMemberToolResult:
				if id := awssdk.ToString(b.Value.ToolUseId); id != "" {
					b.Value.ToolUseId = awssdk.String(sanitizeBedrockToolUseID(id, anthropic))
				}
			}
		}
	}
}

// ──────────────────────────────────────────────
// 工具 JSON Schema 的 pattern 兼容处理
// ──────────────────────────────────────────────

// 部分模型（实测 XAI Grok）的上游校验器不支持正则的环视（lookaround）、
// 反向引用等 RE2 之外的构造，只要工具的 JSON Schema 里出现这类 pattern，
// 整个请求会被拒绝，而且错误不指出字段：
//
//	ValidationException: The model returned the following errors:
//	{"error":{"code":"validation_error","message":"The task request was rejected by the target",
//	"param":null,"type":"invalid_request_error"}}
//
// 现场（2026-10-10）：Claude Code 2.1.296 的内置工具 ArtifactData 在 collection
// 字段的 pattern 中使用了负向前瞻 (?!\.\.?(?:\/|$))，网关把工具 schema 原样透传给
// Converse，导致 grok-4.6 的全部请求被拒；去掉该构造后同一请求立即恢复正常，
// 而 Claude 系模型对同样的 pattern 完全正常。
//
// 判定方式直接用 Go 的 regexp（RE2）能否编译，而不是维护一份构造黑名单：
// 环视、反向引用等“上游不支持”的构造在 RE2 下都编译失败，未来出现新的同类构造
// 也会被同一规则覆盖。
//
// patternUnsupported 判断一个 JSON Schema pattern 是否使用了目标模型不支持的
// 正则构造。
func patternUnsupported(pattern string) bool {
	_, err := regexp.Compile(pattern)
	return err != nil
}

// stripUnsupportedPatterns 返回 schema 的深拷贝，其中使用了不支持构造的
// "pattern" 关键字被整体删除；返回被删除的 pattern 数量。
//
// 只删 pattern 关键字，保留 type / properties / description / enum 等其余约束：
// 模型仍能从字段描述中获得语义约束，而请求不再被上游拒绝。
// 原 schema 不被修改——core.ChatRequest 在组内故障转移时会被复用。
func stripUnsupportedPatterns(schema map[string]any) (map[string]any, int) {
	if schema == nil {
		return nil, 0
	}
	out := make(map[string]any, len(schema))
	removed := 0
	for k, v := range schema {
		if k == "pattern" {
			if s, ok := v.(string); ok && patternUnsupported(s) {
				removed++
				continue
			}
		}
		nv, n := stripUnsupportedPatternsValue(v)
		out[k] = nv
		removed += n
	}
	return out, removed
}

func stripUnsupportedPatternsValue(v any) (any, int) {
	switch t := v.(type) {
	case map[string]any:
		return stripUnsupportedPatterns(t)
	case []any:
		out := make([]any, len(t))
		removed := 0
		for i, item := range t {
			nv, n := stripUnsupportedPatternsValue(item)
			out[i] = nv
			removed += n
		}
		return out, removed
	default:
		return v, 0
	}
}
