package orchestrator

import (
	"time"

	"github.com/example/llm-gateway/internal/gateway/scheduler"
)

type Scheduler interface {
	Route(groupName string) (*scheduler.RouteDecision, error)
	Failover(groupName, failedModelID string) (*scheduler.RouteDecision, error)
	// FailoverExcluding 排除多个已尝试模型后选出下一个候选（多轮 failover 使用）。
	FailoverExcluding(groupName string, excludeIDs []string) (*scheduler.RouteDecision, error)
	GetGroupConfig(groupName string) *scheduler.GroupRoutingConfig
	NotifyFailure(modelID string)
	// SetCooldown 为指定模型设置 429 冷却期。
	SetCooldown(modelID string, duration time.Duration)
}
