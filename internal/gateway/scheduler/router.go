package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/core"
)

type Router struct {
	routeTable  *RouteTable
	healthStore *HealthStore
	stickyStore *StickyStore
	counters    map[string]uint64
	countersMu  sync.Mutex
	classifier  *ComplexityClassifier
}

func NewRouter(rt *RouteTable, hs *HealthStore) *Router {
	return &Router{
		routeTable:  rt,
		healthStore: hs,
		stickyStore: nil, // Set via SetStickyStore after Redis client available
		counters:    make(map[string]uint64),
		classifier:  NewComplexityClassifier(),
	}
}

// SetStickyStore injects the Redis-backed sticky store (called during scheduler setup)
func (r *Router) SetStickyStore(store *StickyStore) {
	r.stickyStore = store
}

func (r *Router) Route(groupName string, req *core.ChatRequest) (*RouteDecision, error) {
	start := time.Now()

	group := r.routeTable.Get(groupName)
	if group == nil {
		slog.Warn("router: group not found", "group", groupName)
		return nil, ErrGroupNotFound
	}

	slog.Debug("router: group found", "group", groupName, "models", len(group.Models), "strategy", group.Strategy)

	healthyModels := r.healthStore.FilterHealthy(group.Models)
	if len(healthyModels) == 0 {
		slog.Warn("router: no healthy models", "group", groupName, "total", len(group.Models))
		return nil, ErrNoAvailableModel
	}

	slog.Debug("router: healthy models", "group", groupName, "healthy", len(healthyModels))

	// Sticky routing（strategy 选择之前）。req 可能为 nil（embeddings/rerank 组路由）。
	if group.StickyEnabled && r.stickyStore != nil && req != nil && req.SessionKey != "" {
		if entry := r.trySticky(groupName, req.SessionKey, healthyModels); entry != nil {
			decision := buildDecision(group, entry, start)
			decision.StickyUsed = true
			return decision, nil
		}
	}

	if group.Strategy == StrategyAuto {
		entry, meta, err := r.routeAutoWithMeta(healthyModels, req)
		if err != nil {
			return nil, err
		}
		decision := buildDecision(group, entry, start)
		if meta != nil {
			decision.ComplexityTier = meta.Tier
			decision.ComplexityScore = meta.Score
			decision.ReasoningMarkers = meta.ReasoningMarkers
			decision.BiasUpApplied = meta.BiasUp
		}
		return decision, nil
	}

	entry, err := r.selectEntry(group.Strategy, groupName, healthyModels, req)
	if err != nil {
		return nil, err
	}

	return buildDecision(group, entry, start), nil
}

func buildDecision(group *GroupRoutingConfig, entry *ModelRoutingEntry, start time.Time) *RouteDecision {
	return &RouteDecision{
		GroupName:      group.GroupName,
		Strategy:       group.Strategy,
		ModelID:        strconv.FormatUint(uint64(entry.ModelID), 10),
		ModelName:      entry.ModelName,
		ModelType:      entry.ModelType,
		ChannelID:      strconv.FormatUint(uint64(entry.ChannelID), 10),
		ChannelType:    entry.ChannelType,
		ProviderName:   entry.ProviderName,
		Endpoint:       entry.Endpoint,
		Credential:     entry.Credential,
		DecisionTimeUs: time.Since(start).Microseconds(),
	}
}

func (r *Router) SelectByStrategy(strategy RoutingStrategy, groupName string, healthyModels []*ModelRoutingEntry, req *core.ChatRequest) (*RouteDecision, error) {
	start := time.Now()

	entry, err := r.selectEntry(strategy, groupName, healthyModels, req)
	if err != nil {
		return nil, err
	}

	decision := &RouteDecision{
		Strategy:       strategy,
		GroupName:      groupName,
		ModelID:        strconv.FormatUint(uint64(entry.ModelID), 10),
		ModelName:      entry.ModelName,
		ChannelID:      strconv.FormatUint(uint64(entry.ChannelID), 10),
		ChannelType:    entry.ChannelType,
		ProviderName:   entry.ProviderName,
		Endpoint:       entry.Endpoint,
		Credential:     entry.Credential,
		DecisionTimeUs: time.Since(start).Microseconds(),
	}
	return decision, nil
}

func (r *Router) selectEntry(strategy RoutingStrategy, groupName string, healthyModels []*ModelRoutingEntry, req *core.ChatRequest) (*ModelRoutingEntry, error) {
	switch strategy {
	case StrategyRoundRobin:
		return r.routeRoundRobin(groupName, healthyModels)
	case StrategyPriority:
		return r.routePriority(groupName, healthyModels)
	case StrategyWeighted:
		return r.routeWeighted(healthyModels)
	case StrategyAuto:
		return r.routeAuto(healthyModels, req)
	default:
		return nil, fmt.Errorf("unknown strategy: %s", strategy)
	}
}

func (r *Router) routeRoundRobin(groupName string, healthyModels []*ModelRoutingEntry) (*ModelRoutingEntry, error) {
	r.countersMu.Lock()
	idx := r.counters[groupName]
	r.counters[groupName] = (idx + 1) % uint64(len(healthyModels))
	r.countersMu.Unlock()

	return healthyModels[idx%uint64(len(healthyModels))], nil
}

func (r *Router) routePriority(groupName string, healthyModels []*ModelRoutingEntry) (*ModelRoutingEntry, error) {
	minPriority := healthyModels[0].Priority
	for _, m := range healthyModels {
		if m.Priority < minPriority {
			minPriority = m.Priority
		}
	}
	samePrio := make([]*ModelRoutingEntry, 0, len(healthyModels))
	for _, m := range healthyModels {
		if m.Priority == minPriority {
			samePrio = append(samePrio, m)
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

	return samePrio[idx%uint64(len(samePrio))], nil
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

// AutoMeta 记录 auto 策略本次路由的分类元信息
type AutoMeta struct {
	Tier             string
	Score            float64
	ReasoningMarkers int
	BiasUp           bool
}

// routeAutoWithMeta 按难度分档路由，返回选中的候选 + 审计元信息。
func (r *Router) routeAutoWithMeta(healthyModels []*ModelRoutingEntry, req *core.ChatRequest) (*ModelRoutingEntry, *AutoMeta, error) {
	if len(healthyModels) == 0 {
		return nil, nil, ErrNoAvailableModel
	}
	if len(healthyModels) == 1 {
		return healthyModels[0], &AutoMeta{Tier: "single", Score: 0}, nil
	}

	result := r.classifier.Classify(req)
	targetTier := result.Tier
	biasUp := false

	candidates := filterByTier(healthyModels, targetTier)

	cur := targetTier
	for len(candidates) == 0 {
		next := upgradeTier(cur)
		if next == cur {
			break
		}
		cur = next
		candidates = filterByTier(healthyModels, cur)
		biasUp = true
	}

	if len(candidates) == 0 {
		candidates = healthyModels
		biasUp = true
	}

	selected, err := r.routeWeighted(candidates)
	if err != nil {
		return nil, nil, err
	}

	slog.Info("auto routed",
		"tier", result.Tier,
		"score", fmt.Sprintf("%.3f", result.Score),
		"markers", result.ReasoningMarkers,
		"bias_up", biasUp,
		"hard_floor", result.HardFloor,
		"pool_size", len(candidates),
		"detail", result.Detail,
	)

	return selected, &AutoMeta{
		Tier:             string(result.Tier),
		Score:            result.Score,
		ReasoningMarkers: result.ReasoningMarkers,
		BiasUp:           biasUp,
	}, nil
}

// routeAuto 是 routeAutoWithMeta 的薄包装，供 selectEntry 使用。
func (r *Router) routeAuto(healthyModels []*ModelRoutingEntry, req *core.ChatRequest) (*ModelRoutingEntry, error) {
	entry, _, err := r.routeAutoWithMeta(healthyModels, req)
	return entry, err
}

// trySticky checks if there's a valid sticky pin for this session
func (r *Router) trySticky(groupName, sessionKey string, healthyModels []*ModelRoutingEntry) *ModelRoutingEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	pinnedModel, err := r.stickyStore.Get(ctx, groupName, sessionKey)
	if err != nil || pinnedModel == "" {
		return nil // No pin or Redis error
	}

	// Validate: is pinned model still in healthy pool?
	for _, m := range healthyModels {
		if m.ModelName == pinnedModel {
			sessionKeyPrefix := sessionKey
			if len(sessionKey) > 8 {
				sessionKeyPrefix = sessionKey[:8] + "..."
			}
			slog.Info("sticky: using pinned model",
				"group", groupName,
				"session_key", sessionKeyPrefix,
				"model_name", pinnedModel,
			)
			return m
		}
	}

	// Pinned model is not healthy or not in pool - fall through to strategy
	sessionKeyPrefix := sessionKey
	if len(sessionKey) > 8 {
		sessionKeyPrefix = sessionKey[:8] + "..."
	}
	slog.Info("sticky: pinned model not available, routing fresh",
		"group", groupName,
		"session_key", sessionKeyPrefix,
		"pinned_model", pinnedModel,
	)
	return nil
}
