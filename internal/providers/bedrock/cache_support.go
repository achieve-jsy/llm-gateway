package bedrock

import (
	"strings"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// modelSupportsPromptCaching 判断模型是否接受 Bedrock CachePoint（prompt caching）块。
//
// 当前账号（us-east-1）实测：只有 Anthropic Claude 系列接受 CachePoint，
// OpenAI GPT / gpt-oss、xAI Grok、DeepSeek、Moonshot、Z.AI、MiniMax、Qwen
// 等一加上 CachePoint 就会整个请求被拒：
//
//	AccessDeniedException: You invoked an unsupported model or your request
//	did not allow prompt caching.
//
// Amazon Nova 官方文档支持 prompt caching，但本账号未开通其 on-demand 访问，
// 无法实测；这里按“宁可不缓存也不报错”的保守策略排除，需要时再放开。
func modelSupportsPromptCaching(modelID string) bool {
	return strings.HasPrefix(normalizeBedrockModelID(modelID), "anthropic.")
}

// stripCachePoints 移除 system / messages / tools 上的全部 CachePoint 块。
// 对不支持 prompt caching 的模型必须在请求发出前清理，否则请求会被上游拒绝。
func stripCachePoints(parts *converseParts) {
	if parts == nil {
		return
	}
	if parts.toolCfg != nil {
		tools := make([]brtypes.Tool, 0, len(parts.toolCfg.Tools))
		for _, tool := range parts.toolCfg.Tools {
			if _, isCachePoint := tool.(*brtypes.ToolMemberCachePoint); isCachePoint {
				continue
			}
			tools = append(tools, tool)
		}
		parts.toolCfg.Tools = tools
	}
	if len(parts.system) > 0 {
		system := make([]brtypes.SystemContentBlock, 0, len(parts.system))
		for _, block := range parts.system {
			if _, isCachePoint := block.(*brtypes.SystemContentBlockMemberCachePoint); isCachePoint {
				continue
			}
			system = append(system, block)
		}
		parts.system = system
	}
	for i := range parts.messages {
		content := parts.messages[i].Content
		if len(content) == 0 {
			continue
		}
		filtered := make([]brtypes.ContentBlock, 0, len(content))
		for _, block := range content {
			if _, isCachePoint := block.(*brtypes.ContentBlockMemberCachePoint); isCachePoint {
				continue
			}
			filtered = append(filtered, block)
		}
		parts.messages[i].Content = filtered
	}
}
