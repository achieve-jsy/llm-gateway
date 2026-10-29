package orchestrator

import (
	"context"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
)

type Scheduler interface {
	Route(groupName string, req *core.ChatRequest) (*scheduler.RouteDecision, error)
	Failover(groupName, failedModelID string, req *core.ChatRequest) (*scheduler.RouteDecision, error)
	FailoverExcluding(groupName string, excludeIDs []string, req *core.ChatRequest) (*scheduler.RouteDecision, error)
	// FailoverExcludingVideo 与 FailoverExcluding 相同，但由调用方显式给出
	// "是否必须保持视频能力"。/v1/responses 没有 core.ChatRequest，
	// 用它把视频约束带进 failover（否则会换到不支持视频的成员）。
	FailoverExcludingVideo(groupName string, excludeIDs []string, needsVideo bool, req *core.ChatRequest) (*scheduler.RouteDecision, error)
	GetGroupConfig(groupName string) *scheduler.GroupRoutingConfig
	NotifyFailure(modelID string)
	// SetCooldown 为指定模型设置 429 冷却期。
	SetCooldown(modelID string, duration time.Duration)
	// PersistSticky 持久化 sticky 映射
	PersistSticky(ctx context.Context, groupName, sessionKey string, decision *scheduler.RouteDecision)
	// UpdateSticky 更新 sticky 映射（用于 failover）。返回是否真正写入（组开启 sticky 时）。
	UpdateSticky(ctx context.Context, groupName, sessionKey, newModelName, newProviderName string) bool
}
