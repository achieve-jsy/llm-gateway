package planning

import "strings"

// TargetResolver 将 route decision 与 runtime metadata 合并成 UpstreamTarget。
// Resolver 不持有外部依赖，方便 shadow mode 与单测双跑。
type TargetResolver struct{}

func NewTargetResolver() TargetResolver { return TargetResolver{} }

func (TargetResolver) Resolve(input RouteInput) UpstreamTarget {
	publicModel := firstNonEmpty(input.PublicModel, input.Decision.ModelName, input.Metadata.ModelName, input.Decision.UpstreamModel, input.Metadata.UpstreamModel)
	logicalModel := firstNonEmpty(input.LogicalModel, publicModel)
	upstreamModel := firstNonEmpty(input.Metadata.UpstreamModel, input.Decision.UpstreamModel, input.Metadata.ModelName, input.Decision.ModelName, publicModel)

	policy := input.Metadata.UnsupportedParamPolicy
	if policy == "" {
		policy = UnsupportedParamPolicyPreserve
	}
	protocolOverride := input.Metadata.ProtocolMode
	if protocolOverride == "" {
		protocolOverride = ProtocolAuto
	}
	paramOverride := input.Metadata.ParamProfile
	if paramOverride == "" {
		paramOverride = ParamProfileAuto
	}

	return UpstreamTarget{
		PublicModel:             strings.TrimSpace(publicModel),
		LogicalModel:            strings.TrimSpace(logicalModel),
		BaseModel:               strings.TrimSpace(input.Metadata.BaseModel),
		UpstreamModel:           strings.TrimSpace(upstreamModel),
		ModelID:                 firstNonEmpty(input.Metadata.ModelID, input.Decision.ModelID),
		ProviderID:              firstNonEmpty(input.Metadata.ProviderID, input.Decision.ProviderID),
		ProviderName:            firstNonEmpty(input.Metadata.ProviderName, input.Decision.ProviderName),
		ProviderType:            firstNonEmpty(input.Metadata.ProviderType, input.Decision.ProviderType),
		ChannelID:               firstNonEmpty(input.Metadata.ChannelID, input.Decision.ChannelID),
		BaseURL:                 firstNonEmpty(input.Metadata.BaseURL, input.Decision.BaseURL),
		APIVersion:              firstNonEmpty(input.Metadata.APIVersion, input.Decision.APIVersion),
		CredentialRef:           firstNonEmpty(input.Metadata.CredentialRef, input.Decision.CredentialRef),
		ProtocolModeOverride:    protocolOverride,
		ParamProfileOverride:    paramOverride,
		UnsupportedParamPolicy:  policy,
		Capabilities:            input.Metadata.Capabilities,
		SupportedParamsOverride: append([]string(nil), input.Metadata.SupportedParamsOverride...),
		TransformOverrides:      cloneTransformOverrides(input.Metadata.TransformOverrides),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneTransformOverrides(in TransformOverrides) TransformOverrides {
	out := TransformOverrides{
		Drop: append([]string(nil), in.Drop...),
	}
	if len(in.Rename) > 0 {
		out.Rename = make(map[string]string, len(in.Rename))
		for k, v := range in.Rename {
			out.Rename[k] = v
		}
	}
	if len(in.Force) > 0 {
		out.Force = make(map[string]any, len(in.Force))
		for k, v := range in.Force {
			out.Force[k] = v
		}
	}
	return out
}
