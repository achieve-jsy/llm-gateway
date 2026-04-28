package openai

import (
	"encoding/json"

	"github.com/example/llm-gateway/internal/adapters"
	"github.com/example/llm-gateway/internal/protocol/transform"
)

const AdapterName = "openai_chat"

type ChatAdapter struct{}

var _ adapters.Adapter = (*ChatAdapter)(nil)
var _ adapters.ChatBodyTransformer = (*ChatAdapter)(nil)

func NewChatAdapter() *ChatAdapter { return &ChatAdapter{} }

func (a *ChatAdapter) Name() string { return AdapterName }

func (a *ChatAdapter) TransformChatBody(plan adapters.Plan, request any) (map[string]any, error) {
	raw, err := TransformChatBodyRaw(plan, request)
	if err != nil {
		return nil, err
	}
	return rawMapToAny(raw)
}

func TransformChatBodyRaw(plan adapters.Plan, request any) (map[string]json.RawMessage, error) {
	profile := adapters.ParamProfileAuto
	if plan != nil {
		profile = plan.GetParamProfile()
	}
	if profile == "" || profile == adapters.ParamProfileAuto {
		profile = adapters.ParamProfileOpenAIStandard
	}

	var rules []transform.ParamTransformRule
	switch profile {
	case adapters.ParamProfileOpenAIReasoningChat:
		rules = ReasoningChatRules()
	case adapters.ParamProfileOpenAIStandard, adapters.ParamProfilePassthrough:
		rules = StandardChatRules()
	default:
		rules = StandardChatRules()
	}
	if plan != nil && plan.GetModelForUpstream() != "" {
		rules = append(rules, transform.ForceLiteral("model", plan.GetModelForUpstream()))
	}
	return transform.ApplyRules(request, rules...)
}

func StandardChatRules() []transform.ParamTransformRule {
	return nil
}

func ReasoningChatRules() []transform.ParamTransformRule {
	return []transform.ParamTransformRule{
		transform.Rename("max_tokens", "max_completion_tokens"),
		transform.Drop("temperature"),
		transform.DropReasoningSummary(),
		transform.NormalizeReasoningEffort(),
	}
}

func rawMapToAny(raw map[string]json.RawMessage) (map[string]any, error) {
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		var decoded any
		if len(value) == 0 {
			out[key] = nil
			continue
		}
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, err
		}
		out[key] = decoded
	}
	return out, nil
}
