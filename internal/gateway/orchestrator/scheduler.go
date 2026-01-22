package orchestrator

import (
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
)

type Scheduler interface {
	Route(groupName string, req *core.ChatRequest) (*scheduler.RouteDecision, error)
	Failover(groupName, failedModelID string, req *core.ChatRequest) (*scheduler.RouteDecision, error)
	FailoverExcluding(groupName string, excludeIDs []string, req *core.ChatRequest) (*scheduler.RouteDecision, error)
	GetGroupConfig(groupName string) *scheduler.GroupRoutingConfig
	NotifyFailure(modelID string)
	// SetCooldown 为指定模型设置 429 冷却期。
	SetCooldown(modelID string, duration time.Duration)
}
