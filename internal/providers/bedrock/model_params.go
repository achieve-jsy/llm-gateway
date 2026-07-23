package bedrock

import "strings"

// inferenceProfilePrefixes 是 Bedrock 跨区域推理配置文件（inference profile）
// 的地区前缀，例如 "global.openai.gpt-5.6-sol" / "us.anthropic.claude-sonnet-4-6"。
// 判断模型族时需要先把它们剥掉。
var inferenceProfilePrefixes = map[string]struct{}{
	"global": {},
	"us":     {},
	"us-gov": {},
	"eu":     {},
	"ap":     {},
	"ca":     {},
	"sa":     {},
	"me":     {},
	"af":     {},
	"il":     {},
}

// bedrockOpenAIMinMaxTokens 是 Bedrock 上 OpenAI GPT 系列 / xAI Grok 系列的
// max_output_tokens 下限。小于该值上游会直接拒绝：
//
//	ValidationException: Invalid 'max_output_tokens': integer below minimum value.
//	Expected a value >= 16, but got 8 instead.
const bedrockOpenAIMinMaxTokens int32 = 16

// modelParamSupport 描述某个上游模型对 Converse 推理参数的支持情况。
//
// Bedrock 上并非所有模型都接受完整的 InferenceConfiguration：OpenAI GPT 系列
// 与 xAI Grok 系列不支持 temperature / topP（也不支持 stopSequences），原样透传
// 会让整个请求返回 400，而不是被忽略：
//
//	ValidationException: This model doesn't support the temperature field.
type modelParamSupport struct {
	// Temperature 表示是否可以把客户端的 temperature 下发给该模型。
	Temperature bool
	// TopP 表示是否可以把客户端的 top_p 下发给该模型。
	TopP bool
	// MinMaxTokens 是下发 max_tokens 时的下限，0 表示无特殊下限。
	MinMaxTokens int32
}

// paramSupportForModel 按上游模型 ID 的厂商前缀推断参数支持情况。
//
// 规则按厂商前缀而非具体版本号匹配，这样后续新增的 gpt-* / grok-* 型号
// 会自动落到同样的限制上，不需要每次发新模型都改代码。未识别的厂商保持
// 放开（与历史行为一致），由上游自行校验。
func paramSupportForModel(modelID string) modelParamSupport {
	id := normalizeBedrockModelID(modelID)
	switch {
	case strings.HasPrefix(id, "openai.gpt-oss"):
		// gpt-oss 支持 temperature / top_p，只是不支持 stopSequences；
		// 网关目前不下发 stopSequences，因此这里无需额外限制。
		return modelParamSupport{Temperature: true, TopP: true, MinMaxTokens: 1}
	case strings.HasPrefix(id, "openai."), strings.HasPrefix(id, "xai."):
		return modelParamSupport{Temperature: false, TopP: false, MinMaxTokens: bedrockOpenAIMinMaxTokens}
	default:
		return modelParamSupport{Temperature: true, TopP: true, MinMaxTokens: 1}
	}
}

// normalizeBedrockModelID 去掉 "{provider}/" 与跨区域推理配置文件前缀，
// 得到用于判断模型族的裸模型 ID。
func normalizeBedrockModelID(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	id = stripProviderPrefix(id, providerName)
	for {
		idx := strings.Index(id, ".")
		if idx <= 0 {
			break
		}
		if _, ok := inferenceProfilePrefixes[id[:idx]]; !ok {
			break
		}
		id = id[idx+1:]
	}
	return id
}
