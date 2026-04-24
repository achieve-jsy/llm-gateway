package planning

import "sort"

const auditPlanVersion = "upstream_planner_v2_shadow_1"

// ProtocolAudit 是可安全写入日志/trace/debug header 的计划摘要。
// 不包含 prompt、messages、tool schema、credential/API key 或 Force 覆盖值。
type ProtocolAudit struct {
	PlanVersion            string                 `json:"plan_version"`
	RequestKind            RequestKind            `json:"request_kind"`
	PublicModel            string                 `json:"public_model,omitempty"`
	LogicalModel           string                 `json:"logical_model,omitempty"`
	BaseModel              string                 `json:"base_model,omitempty"`
	UpstreamModel          string                 `json:"upstream_model,omitempty"`
	ModelID                string                 `json:"model_id,omitempty"`
	ProviderID             string                 `json:"provider_id,omitempty"`
	ProviderName           string                 `json:"provider_name,omitempty"`
	ProviderType           string                 `json:"provider_type,omitempty"`
	ChannelID              string                 `json:"channel_id,omitempty"`
	Protocol               ProtocolMode           `json:"protocol_mode"`
	ParamProfile           ParamProfile           `json:"param_profile"`
	UnsupportedParamPolicy UnsupportedParamPolicy `json:"unsupported_param_policy"`
	Adapter                string                 `json:"adapter,omitempty"`
	Method                 string                 `json:"method,omitempty"`
	Endpoint               string                 `json:"endpoint,omitempty"`
	ProtocolReason         string                 `json:"protocol_reason,omitempty"`
	ParamReason            string                 `json:"param_reason,omitempty"`
	RequestShape           AuditRequestShape      `json:"request_shape"`
	TransformRules         []AuditTransformRule   `json:"transform_rules,omitempty"`
	OverrideSummary        AuditOverrideSummary   `json:"override_summary,omitempty"`
}

type AuditRequestShape struct {
	Stream              bool `json:"stream,omitempty"`
	HasTools            bool `json:"has_tools,omitempty"`
	HasReasoningEffort  bool `json:"has_reasoning_effort,omitempty"`
	HasReasoningSummary bool `json:"has_reasoning_summary,omitempty"`
}

type AuditTransformRule struct {
	Action string `json:"action"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type AuditOverrideSummary struct {
	SupportedParams []string          `json:"supported_params,omitempty"`
	Drop            []string          `json:"drop,omitempty"`
	Rename          map[string]string `json:"rename,omitempty"`
	ForceKeys       []string          `json:"force_keys,omitempty"`
}

func BuildProtocolAudit(plan UpstreamPlan, shape RequestShape, protocolReason, paramReason string) ProtocolAudit {
	return ProtocolAudit{
		PlanVersion:            auditPlanVersion,
		RequestKind:            shape.Kind,
		PublicModel:            plan.Target.PublicModel,
		LogicalModel:           plan.Target.LogicalModel,
		BaseModel:              plan.Target.BaseModel,
		UpstreamModel:          plan.Target.UpstreamModel,
		ModelID:                plan.Target.ModelID,
		ProviderID:             plan.Target.ProviderID,
		ProviderName:           plan.Target.ProviderName,
		ProviderType:           plan.Target.ProviderType,
		ChannelID:              plan.Target.ChannelID,
		Protocol:               plan.Protocol,
		ParamProfile:           plan.ParamProfile,
		UnsupportedParamPolicy: plan.UnsupportedParamPolicy,
		Adapter:                plan.Adapter,
		Method:                 plan.Method,
		Endpoint:               plan.Endpoint,
		ProtocolReason:         protocolReason,
		ParamReason:            paramReason,
		RequestShape: AuditRequestShape{
			Stream:              shape.Stream,
			HasTools:            shape.HasTools,
			HasReasoningEffort:  shape.HasReasoningEffort,
			HasReasoningSummary: shape.HasReasoningSummary,
		},
		TransformRules:  auditTransformRules(plan.ParamRules),
		OverrideSummary: auditOverrideSummary(plan.Target),
	}
}

func (p UpstreamPlan) SafeAudit() ProtocolAudit {
	return p.Audit
}

func auditTransformRules(rules []ParamTransformRule) []AuditTransformRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]AuditTransformRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, AuditTransformRule{
			Action: rule.Action,
			From:   rule.From,
			To:     rule.To,
			Reason: rule.Reason,
		})
	}
	return out
}

func auditOverrideSummary(target UpstreamTarget) AuditOverrideSummary {
	summary := AuditOverrideSummary{
		SupportedParams: append([]string(nil), target.SupportedParamsOverride...),
		Drop:            append([]string(nil), target.TransformOverrides.Drop...),
	}
	if len(target.TransformOverrides.Rename) > 0 {
		summary.Rename = make(map[string]string, len(target.TransformOverrides.Rename))
		for k, v := range target.TransformOverrides.Rename {
			summary.Rename[k] = v
		}
	}
	if len(target.TransformOverrides.Force) > 0 {
		summary.ForceKeys = make([]string, 0, len(target.TransformOverrides.Force))
		for k := range target.TransformOverrides.Force {
			summary.ForceKeys = append(summary.ForceKeys, k)
		}
		sort.Strings(summary.ForceKeys)
	}
	sort.Strings(summary.SupportedParams)
	sort.Strings(summary.Drop)
	return summary
}
