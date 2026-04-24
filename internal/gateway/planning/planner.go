package planning

// RequestShape 是 planner 判断协议/参数画像所需的安全请求摘要。
// 它只描述是否存在某类字段，不包含 prompt、tool schema 或参数值。
type RequestShape struct {
	Kind                RequestKind
	Stream              bool
	HasTools            bool
	HasReasoningEffort  bool
	HasReasoningSummary bool
}

// ParamTransformRule 是计划中的参数处理规则说明。Phase 2 仅审计，不执行。
type ParamTransformRule struct {
	Action string `json:"action"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// UpstreamPlan 表示一次上游调用“怎么调”。
type UpstreamPlan struct {
	Target UpstreamTarget

	Protocol     ProtocolMode
	ParamProfile ParamProfile

	Method   string
	Endpoint string

	ModelForUpstream string
	ModelForClient   string

	ParamRules []ParamTransformRule

	UnsupportedParamPolicy UnsupportedParamPolicy
	RequestBodyProfile     RequestBodyProfile
	ResponseProfile        ResponseProfile
	StreamProfile          StreamProfile

	Adapter string
	Audit   ProtocolAudit
}

// Planner 组合协议选择与参数画像选择，生成 shadow-mode UpstreamPlan。
type Planner struct {
	ProtocolPlanner ProtocolPlanner
	ParamPlanner    ParamPlanner
}

func NewPlanner() Planner {
	return Planner{
		ProtocolPlanner: NewProtocolPlanner(),
		ParamPlanner:    NewParamPlanner(),
	}
}

func (p Planner) Plan(target UpstreamTarget, shape RequestShape) UpstreamPlan {
	if shape.Kind == "" {
		shape.Kind = RequestKindChat
	}
	protocolDecision := p.ProtocolPlanner.Plan(target, shape)
	paramDecision := p.ParamPlanner.Plan(target, shape, protocolDecision.Protocol)

	plan := UpstreamPlan{
		Target:                 target,
		Protocol:               protocolDecision.Protocol,
		ParamProfile:           paramDecision.Profile,
		Method:                 methodForProtocol(protocolDecision.Protocol),
		Endpoint:               endpointForProtocol(protocolDecision.Protocol),
		ModelForUpstream:       target.UpstreamModel,
		ModelForClient:         target.PublicModel,
		ParamRules:             paramRulesForProfile(paramDecision.Profile),
		UnsupportedParamPolicy: target.UnsupportedParamPolicy,
		RequestBodyProfile:     requestBodyProfileForProtocol(protocolDecision.Protocol),
		ResponseProfile:        responseProfileForProtocol(protocolDecision.Protocol),
		StreamProfile:          streamProfileForProtocol(protocolDecision.Protocol),
		Adapter:                adapterFor(target.ProviderType, protocolDecision.Protocol),
	}
	plan.Audit = BuildProtocolAudit(plan, shape, protocolDecision.Reason, paramDecision.Reason)
	return plan
}

func methodForProtocol(protocol ProtocolMode) string {
	switch protocol {
	case ProtocolOpenAIChat, ProtocolOpenAIResponses:
		return "POST"
	default:
		return "POST"
	}
}

func endpointForProtocol(protocol ProtocolMode) string {
	switch protocol {
	case ProtocolOpenAIResponses:
		return "/responses"
	case ProtocolOpenAIChat, ProtocolOpenAICompatible:
		return "/chat/completions"
	case ProtocolAnthropicMessages:
		return "/messages"
	default:
		return ""
	}
}

func requestBodyProfileForProtocol(protocol ProtocolMode) RequestBodyProfile {
	switch protocol {
	case ProtocolOpenAIResponses:
		return RequestBodyProfileOpenAIResponses
	case ProtocolOpenAIChat, ProtocolOpenAICompatible:
		return RequestBodyProfileOpenAIChat
	default:
		return RequestBodyProfilePassthrough
	}
}

func responseProfileForProtocol(protocol ProtocolMode) ResponseProfile {
	switch protocol {
	case ProtocolOpenAIResponses:
		return ResponseProfileOpenAIResponses
	case ProtocolOpenAIChat, ProtocolOpenAICompatible:
		return ResponseProfileOpenAIChat
	default:
		return ResponseProfilePassthrough
	}
}

func streamProfileForProtocol(protocol ProtocolMode) StreamProfile {
	switch protocol {
	case ProtocolOpenAIResponses:
		return StreamProfileOpenAIResponses
	case ProtocolOpenAIChat, ProtocolOpenAICompatible:
		return StreamProfileOpenAIChat
	default:
		return StreamProfilePassthrough
	}
}

func adapterFor(providerType string, protocol ProtocolMode) string {
	providerType = normalizeProviderType(providerType)
	switch protocol {
	case ProtocolOpenAIResponses:
		if providerType == "azure" {
			return "azure_responses"
		}
		return "openai_responses"
	case ProtocolOpenAIChat:
		if providerType == "azure" {
			return "azure_chat"
		}
		return "openai_chat"
	case ProtocolOpenAICompatible:
		return "openai_compatible"
	case ProtocolPassthrough:
		return "passthrough"
	default:
		if providerType != "" {
			return providerType + "_" + string(protocol)
		}
		return string(protocol)
	}
}

func paramRulesForProfile(profile ParamProfile) []ParamTransformRule {
	switch profile {
	case ParamProfileOpenAIReasoningChat:
		return []ParamTransformRule{
			{Action: "rename_if_absent", From: "max_tokens", To: "max_completion_tokens", Reason: "openai reasoning chat uses max_completion_tokens"},
			{Action: "drop", From: "max_tokens", Reason: "avoid duplicate token limit for reasoning chat"},
			{Action: "drop", From: "temperature", Reason: "openai reasoning chat does not support temperature"},
			{Action: "normalize", From: "reasoningEffort", To: "reasoning_effort", Reason: "chat reasoning effort alias"},
			{Action: "normalize", From: "reasoning.effort", To: "reasoning_effort", Reason: "responses-style reasoning alias"},
			{Action: "drop", From: "reasoningSummary", Reason: "chat completions does not accept reasoning summary"},
			{Action: "drop", From: "reasoning_summary", Reason: "chat completions does not accept reasoning summary"},
		}
	case ParamProfileOpenAIResponses:
		return []ParamTransformRule{
			{Action: "bridge", From: "chat", To: "responses", Reason: "request shape requires responses protocol"},
		}
	default:
		return nil
	}
}
