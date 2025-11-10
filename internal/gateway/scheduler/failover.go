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

func (fh *FailoverHandler) Failover(groupName string, failedModelID string) (*RouteDecision, error) {
	group := fh.routeTable.Get(groupName)
	if group == nil {
		return nil, ErrGroupNotFound
	}

	remaining := make([]*ModelRoutingEntry, 0, len(group.Models))
	for _, m := range group.Models {
		if fmt.Sprintf("%d", m.ModelID) != failedModelID {
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
