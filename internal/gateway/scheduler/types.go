package scheduler

// RoutingStrategy 路由策略类型
type RoutingStrategy string

const (
	StrategyRoundRobin RoutingStrategy = "round_robin"
	StrategyPriority   RoutingStrategy = "priority"
	StrategyWeighted   RoutingStrategy = "weighted"
)

// Valid 校验策略值是否合法
func (s RoutingStrategy) Valid() bool {
	switch s {
	case StrategyRoundRobin, StrategyPriority, StrategyWeighted:
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
	ChannelID      string          `json:"channel_id"`
	ChannelType    string          `json:"channel_type"`
	ProviderName   string          `json:"provider_name"`
	Endpoint       string          `json:"endpoint"`
	Credential     string          `json:"credential"`
	DecisionTimeUs int64           `json:"decision_time_us"`
}
