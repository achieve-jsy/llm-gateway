package scheduler

// RoutingStrategy 路由策略类型
type RoutingStrategy string

const (
	StrategyRoundRobin RoutingStrategy = "round_robin"
	StrategyPriority   RoutingStrategy = "priority"
	StrategyWeighted   RoutingStrategy = "weighted"
	StrategyAuto       RoutingStrategy = "auto"
)

// Valid 校验策略值是否合法
func (s RoutingStrategy) Valid() bool {
	switch s {
	case StrategyRoundRobin, StrategyPriority, StrategyWeighted, StrategyAuto:
		return true
	}
	return false
}

// HealthStatus 模型健康状态
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
)

// RouteDecision 路由决策结果
type RouteDecision struct {
	GroupName      string          `json:"group_name"`
	Strategy       RoutingStrategy `json:"strategy"`
	ModelID        string          `json:"model_id"`
	ModelName      string          `json:"model_name"`
	ModelType      string          `json:"model_type"`
	ChannelID      string          `json:"channel_id"`
	ChannelType    string          `json:"channel_type"`
	ProviderName   string          `json:"provider_name"`
	Endpoint       string          `json:"endpoint"`
	Credential     string          `json:"credential"`
	DecisionTimeUs int64           `json:"decision_time_us"`
	// auto 审计字段（仅 strategy=auto 时填充）
	ComplexityTier          string         `json:"complexity_tier,omitempty"`
	ParamStyle              string         `json:"param_style,omitempty"`
	UpstreamModelID         string         `json:"upstream_model_id,omitempty"`
	BaseModel               string         `json:"base_model"`
	ProtocolMode            string         `json:"protocol_mode"`
	ParamProfile            string         `json:"param_profile"`
	UnsupportedParamPolicy  string         `json:"unsupported_param_policy"`
	SupportedParamsOverride map[string]any `json:"supported_params_override"`
	TransformOverrides      map[string]any `json:"transform_overrides"`
	ComplexityScore         float64        `json:"complexity_score,omitempty"`
	ReasoningMarkers        int            `json:"reasoning_markers,omitempty"`
	BiasUpApplied           bool           `json:"bias_up_applied,omitempty"`
	// sticky 审计字段
	StickyUsed bool `json:"sticky_used,omitempty"`
}
