package orchestrator

import "github.com/example/llm-gateway/internal/gateway/scheduler"

type Scheduler interface {
	Route(groupName string) (*scheduler.RouteDecision, error)
	Failover(groupName, failedModelID string) (*scheduler.RouteDecision, error)
}
