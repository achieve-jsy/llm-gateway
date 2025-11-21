package scheduler

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

type Router struct {
	routeTable  *RouteTable
	healthStore *HealthStore
	counters    map[string]uint64
	countersMu  sync.Mutex
}

func NewRouter(rt *RouteTable, hs *HealthStore) *Router {
	return &Router{
		routeTable:  rt,
		healthStore: hs,
		counters:    make(map[string]uint64),
	}
}

func (r *Router) Route(groupName string) (*RouteDecision, error) {
	start := time.Now()

	group := r.routeTable.Get(groupName)
	if group == nil {
		slog.Warn("router: group not found", "group", groupName)
		return nil, ErrGroupNotFound
	}

	slog.Info("router: group found", "group", groupName, "models", len(group.Models), "strategy", group.Strategy)

	healthyModels := r.healthStore.FilterHealthy(group.Models)
	if len(healthyModels) == 0 {
		slog.Warn("router: no healthy models", "group", groupName, "total", len(group.Models))
		return nil, ErrNoAvailableModel
	}

	slog.Info("router: healthy models", "group", groupName, "healthy", len(healthyModels))
	entry, err := r.selectEntry(group.Strategy, groupName, healthyModels)
	if err != nil {
		return nil, err
	}

	decision := &RouteDecision{
		GroupName:      group.GroupName,
		Strategy:       group.Strategy,
		ModelID:        fmt.Sprintf("%d", entry.ModelID),
		ModelName:      entry.ModelName,
		ChannelID:      fmt.Sprintf("%d", entry.ChannelID),
		ChannelType:    entry.ChannelType,
		ProviderName:   entry.ProviderName,
		Endpoint:       entry.Endpoint,
		Credential:     entry.Credential,
		DecisionTimeUs: time.Since(start).Microseconds(),
	}
	return decision, nil
}

func (r *Router) SelectByStrategy(strategy RoutingStrategy, groupName string, healthyModels []*ModelRoutingEntry) (*RouteDecision, error) {
	start := time.Now()

	entry, err := r.selectEntry(strategy, groupName, healthyModels)
	if err != nil {
		return nil, err
	}

	decision := &RouteDecision{
		Strategy:       strategy,
		GroupName:      groupName,
		ModelID:        fmt.Sprintf("%d", entry.ModelID),
		ModelName:      entry.ModelName,
		ChannelID:      fmt.Sprintf("%d", entry.ChannelID),
		ChannelType:    entry.ChannelType,
		ProviderName:   entry.ProviderName,
		Endpoint:       entry.Endpoint,
		Credential:     entry.Credential,
		DecisionTimeUs: time.Since(start).Microseconds(),
	}
	return decision, nil
}

func (r *Router) selectEntry(strategy RoutingStrategy, groupName string, healthyModels []*ModelRoutingEntry) (*ModelRoutingEntry, error) {
	switch strategy {
	case StrategyRoundRobin:
		return r.routeRoundRobin(groupName, healthyModels)
	case StrategyPriority:
		return r.routePriority(groupName, healthyModels)
	case StrategyWeighted:
		return r.routeWeighted(healthyModels)
	default:
		return nil, fmt.Errorf("unknown strategy: %s", strategy)
	}
}

func (r *Router) routeRoundRobin(groupName string, healthyModels []*ModelRoutingEntry) (*ModelRoutingEntry, error) {
	r.countersMu.Lock()
	idx := r.counters[groupName]
	r.counters[groupName] = (idx + 1) % uint64(len(healthyModels))
	r.countersMu.Unlock()

	return healthyModels[idx], nil
}

func (r *Router) routePriority(groupName string, healthyModels []*ModelRoutingEntry) (*ModelRoutingEntry, error) {
	sort.Slice(healthyModels, func(i, j int) bool {
		return healthyModels[i].Priority < healthyModels[j].Priority
	})

	minPriority := healthyModels[0].Priority
	samePrio := make([]*ModelRoutingEntry, 0)
	for _, m := range healthyModels {
		if m.Priority == minPriority {
			samePrio = append(samePrio, m)
		} else {
			break
		}
	}

	if len(samePrio) == 1 {
		return samePrio[0], nil
	}

	prioKey := fmt.Sprintf("%s:prio:%d", groupName, minPriority)
	r.countersMu.Lock()
	idx := r.counters[prioKey]
	r.counters[prioKey] = (idx + 1) % uint64(len(samePrio))
	r.countersMu.Unlock()

	return samePrio[idx], nil
}

func (r *Router) routeWeighted(healthyModels []*ModelRoutingEntry) (*ModelRoutingEntry, error) {
	var totalWeight int
	for _, m := range healthyModels {
		w := m.Weight
		if w == 0 {
			w = 1
		}
		totalWeight += w
	}

	rnd := rand.IntN(totalWeight)
	cumulative := 0
	for _, m := range healthyModels {
		w := m.Weight
		if w == 0 {
			w = 1
		}
		cumulative += w
		if rnd < cumulative {
			return m, nil
		}
	}

	return healthyModels[0], nil
}
