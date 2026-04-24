package planning

import (
	"strconv"
	"strings"
)

type ProtocolDecision struct {
	Protocol ProtocolMode
	Reason   string
}

type ProtocolPlanner struct{}

func NewProtocolPlanner() ProtocolPlanner { return ProtocolPlanner{} }

func (ProtocolPlanner) Plan(target UpstreamTarget, shape RequestShape) ProtocolDecision {
	if target.ProtocolModeOverride != "" && target.ProtocolModeOverride != ProtocolAuto {
		return ProtocolDecision{Protocol: target.ProtocolModeOverride, Reason: "explicit_protocol_override"}
	}
	if shape.Kind == RequestKindResponses {
		return ProtocolDecision{Protocol: ProtocolOpenAIResponses, Reason: "request_kind_responses"}
	}

	providerType := normalizeProviderType(target.ProviderType)
	if isOpenAIProtocolProvider(providerType) {
		model := plannerModelName(target)
		if shouldUseOpenAIResponses(model, shape) {
			return ProtocolDecision{Protocol: ProtocolOpenAIResponses, Reason: "gpt5_4_plus_tools_reasoning_effort"}
		}
		return ProtocolDecision{Protocol: ProtocolOpenAIChat, Reason: "openai_provider_default_chat"}
	}

	return ProtocolDecision{Protocol: ProtocolPassthrough, Reason: "provider_passthrough_default"}
}

func isOpenAIProtocolProvider(providerType string) bool {
	switch normalizeProviderType(providerType) {
	case "openai", "azure":
		return true
	default:
		return false
	}
}

func shouldUseOpenAIResponses(model string, shape RequestShape) bool {
	if !isGPT5Model(model) || isGPT5SearchModel(model) || !shape.HasReasoningEffort {
		return false
	}
	if shape.HasReasoningSummary {
		return true
	}
	return isGPT54PlusModel(model) && shape.HasTools
}

func plannerModelName(target UpstreamTarget) string {
	// base_model 是真实模型族判断的单源；没有时才回退 upstream model。
	return firstNonEmpty(target.BaseModel, target.UpstreamModel, target.LogicalModel, target.PublicModel)
}

func normalizeProviderType(providerType string) string {
	return strings.ToLower(strings.TrimSpace(providerType))
}

func normalizeModelName(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return m
}

func isOSeriesModel(model string) bool {
	m := normalizeModelName(model)
	return len(m) >= 2 && m[0] == 'o' && m[1] >= '0' && m[1] <= '9'
}

func isGPT5ChatModel(model string) bool {
	return strings.HasPrefix(normalizeModelName(model), "gpt-5-chat")
}

func isGPT5Model(model string) bool {
	m := normalizeModelName(model)
	if isGPT5ChatModel(m) {
		return false
	}
	return m == "gpt-5" || strings.HasPrefix(m, "gpt-5-") || strings.HasPrefix(m, "gpt-5.")
}

func isGPT54PlusModel(model string) bool {
	m := normalizeModelName(model)
	if !strings.HasPrefix(m, "gpt-5.") {
		return false
	}
	version := strings.TrimPrefix(m, "gpt-5.")
	if i := strings.IndexAny(version, ".-"); i >= 0 {
		version = version[:i]
	}
	major, err := strconv.Atoi(version)
	if err != nil {
		return false
	}
	return major >= 4
}

func isGPT5SearchModel(model string) bool {
	m := normalizeModelName(model)
	return strings.Contains(m, "gpt-5") && strings.Contains(m, "search")
}

func isReasoningChatModel(model string) bool {
	return isOSeriesModel(model) || isGPT5Model(model)
}
