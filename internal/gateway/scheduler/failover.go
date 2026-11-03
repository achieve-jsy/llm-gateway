package scheduler

import (
	"fmt"
	"log/slog"

	"github.com/example/llm-gateway/internal/core"
)

type FailoverHandler struct {
	routeTable  *RouteTable
	healthStore *HealthStore
	router      *Router
}

func NewFailoverHandler(rt *RouteTable, hs *HealthStore, router *Router) *FailoverHandler {
	return &FailoverHandler{
		routeTable:  rt,
		healthStore: hs,
		router:      router,
	}
}

// Failover 排除单个失败模型后选出下一个候选（向后兼容入口）。
func (fh *FailoverHandler) Failover(groupName string, failedModelID string, req *core.ChatRequest) (*RouteDecision, error) {
	return fh.FailoverExcluding(groupName, []string{failedModelID}, req)
}

// FailoverExcluding 排除多个已失败/已尝试的模型 ID 后选出下一个候选。
// 用于多轮 failover 场景，确保每轮不会路由回已失败的节点。
func (fh *FailoverHandler) FailoverExcluding(groupName string, excludeIDs []string, req *core.ChatRequest) (*RouteDecision, error) {
	group := fh.routeTable.Get(groupName)
	if group == nil {
		return nil, ErrGroupNotFound
	}

	excludeSet := make(map[string]struct{}, len(excludeIDs))
	for _, id := range excludeIDs {
		excludeSet[id] = struct{}{}
	}

	remaining := make([]*ModelRoutingEntry, 0, len(group.Models))
	for _, m := range group.Models {
		if _, excluded := excludeSet[fmt.Sprintf("%d", m.ModelID)]; !excluded {
			remaining = append(remaining, m)
		}
	}

	healthyModels := fh.healthStore.FilterHealthy(remaining)
	if len(healthyModels) == 0 {
		return nil, ErrNoOtherModel
	}

	// 视频请求的 failover 同样只能落在声明支持视频的成员上：换到一个不支持视频的
	// 成员会让视频被静默丢弃、模型凭空作答，比直接失败更糟。
	needsVideo := req != nil && core.HasVideoContent(req.Messages)
	healthyModels = filterVideoCapable(healthyModels, needsVideo)
	if len(healthyModels) == 0 {
		if needsVideo {
			slog.Warn("failover: no video-capable models left", "group", groupName, "tried", len(excludeIDs))
			return nil, ErrNoVideoCapableModel
		}
		return nil, ErrNoOtherModel
	}

	return fh.router.SelectByStrategy(group.Strategy, groupName, healthyModels, req)
}
