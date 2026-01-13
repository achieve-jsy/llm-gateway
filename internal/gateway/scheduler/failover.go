package scheduler

import "fmt"

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
func (fh *FailoverHandler) Failover(groupName string, failedModelID string) (*RouteDecision, error) {
	return fh.FailoverExcluding(groupName, []string{failedModelID})
}

// FailoverExcluding 排除多个已失败/已尝试的模型 ID 后选出下一个候选。
// 用于多轮 failover 场景，确保每轮不会路由回已失败的节点。
func (fh *FailoverHandler) FailoverExcluding(groupName string, excludeIDs []string) (*RouteDecision, error) {
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

	return fh.SelectByStrategy(group.Strategy, groupName, healthyModels)
}

func (fh *FailoverHandler) SelectByStrategy(strategy RoutingStrategy, groupName string, healthyModels []*ModelRoutingEntry) (*RouteDecision, error) {
	return fh.router.SelectByStrategy(strategy, groupName, healthyModels)
}
