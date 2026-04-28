package planning

type ParamDecision struct {
	Profile ParamProfile
	Reason  string
}

type ParamPlanner struct{}

func NewParamPlanner() ParamPlanner { return ParamPlanner{} }

func (ParamPlanner) Plan(target UpstreamTarget, _ RequestShape, protocol ProtocolMode) ParamDecision {
	if target.ParamProfileOverride != "" && target.ParamProfileOverride != ParamProfileAuto {
		return ParamDecision{Profile: target.ParamProfileOverride, Reason: "explicit_param_profile_override"}
	}

	switch protocol {
	case ProtocolOpenAIResponses:
		return ParamDecision{Profile: ParamProfileOpenAIResponses, Reason: "protocol_openai_responses"}
	case ProtocolOpenAIChat:
		if isReasoningChatModel(plannerModelName(target)) {
			return ParamDecision{Profile: ParamProfileOpenAIReasoningChat, Reason: "openai_reasoning_model"}
		}
		return ParamDecision{Profile: ParamProfileOpenAIStandard, Reason: "openai_standard_model"}
	case ProtocolOpenAICompatible:
		return ParamDecision{Profile: ParamProfileOpenAIStandard, Reason: "openai_compatible_standard"}
	case ProtocolPassthrough:
		return ParamDecision{Profile: ParamProfilePassthrough, Reason: "passthrough_protocol"}
	default:
		return ParamDecision{Profile: ParamProfilePassthrough, Reason: "unknown_protocol_passthrough"}
	}
}
