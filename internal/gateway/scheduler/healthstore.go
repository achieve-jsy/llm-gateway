package scheduler

import (
	"fmt"
	"sync"
	"time"
)

// HealthStore tracks the health status of models and provides health-based
// filtering. It is safe for concurrent use.
type HealthStore struct {
	mu                  sync.RWMutex
	data                map[string]*ModelHealthStatus
	cooldowns           map[string]time.Time // 429 冷却截止时间（key: modelID string）
	events              *EventPublisher
	failureThreshold    int
	unhealthyTTL        time.Duration
	quickFailWindow     time.Duration
	quickFailThreshold  int
	failureRingCapacity int
}

// NewHealthStore creates a HealthStore with the given thresholds.
func NewHealthStore(failureThreshold, quickFailThreshold int, quickFailWindow, unhealthyTTL time.Duration) *HealthStore {
	if unhealthyTTL <= 0 {
		unhealthyTTL = 60 * time.Second
	}
	return &HealthStore{
		data:                make(map[string]*ModelHealthStatus),
		cooldowns:           make(map[string]time.Time),
		events:              &EventPublisher{},
		failureThreshold:    failureThreshold,
		unhealthyTTL:        unhealthyTTL,
		quickFailWindow:     quickFailWindow,
		quickFailThreshold:  quickFailThreshold,
		failureRingCapacity: defaultRingCapacity,
	}
}

// SetCooldown 为指定模型设置 429 冷却截止时间。
// 在冷却期内，FilterHealthy 会将其排除在候选池之外。
func (hs *HealthStore) SetCooldown(modelID string, duration time.Duration) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.cooldowns[modelID] = time.Now().Add(duration)
}

// Events returns the EventPublisher for subscribing to health events.
func (hs *HealthStore) Events() *EventPublisher {
	return hs.events
}

// Get returns the health status for the given model ID, or nil if not tracked.
func (hs *HealthStore) Get(modelID string) *ModelHealthStatus {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	return hs.data[modelID]
}

// FilterHealthy filters the given model entries, returning only those whose
// health status is Healthy or not yet tracked (treated as Healthy), and that
// are not currently in cooldown (from a recent 429 response).
// 兜底逻辑：若所有模型均被排除，返回原始全量列表，避免服务中断。
func (hs *HealthStore) FilterHealthy(models []*ModelRoutingEntry) []*ModelRoutingEntry {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	now := time.Now()
	result := make([]*ModelRoutingEntry, 0, len(models))
	for _, m := range models {
		modelKey := fmt.Sprintf("%d", m.ModelID)
		// 冷却期内排除（429 rate limit 引起）
		if coolUntil, ok := hs.cooldowns[modelKey]; ok && now.Before(coolUntil) {
			continue
		}
		status, tracked := hs.data[modelKey]
		// 未追踪（从未失败过）或状态明确为 Healthy，则纳入候选
		if !tracked || status.Status == HealthHealthy {
			result = append(result, m)
			continue
		}
		// 不健康状态超过观察期后放回候选池（半开）：
		// gateway 已不再主动探测上游，没有别的路径能把状态改回 Healthy。
		// 放回来后由真实请求验证——成功即正常服务，失败则立刻重新标记。
		if now.Sub(status.LastStateChange) >= hs.unhealthyTTL {
			result = append(result, m)
		}
	}

	// 兜底：全部标记为不健康/冷却时退化为全量，避免 0 个模型导致请求直接失败。
	// 此时 Failover 机制会在实际转发失败时继续处理。
	if len(result) == 0 {
		return models
	}
	return result
}

// RecordFailure records a failure for the given model ID. It updates the failure
// ring buffer, increments consecutive failures, checks failure and quick-fail
// thresholds, and transitions to Unhealthy if thresholds are met.
func (hs *HealthStore) RecordFailure(modelID string) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	status := hs.getOrCreateLocked(modelID)
	now := time.Now()

	status.FailureWindow.Record(now)
	status.ConsecutiveFailures++
	status.ConsecutiveSuccesses = 0
	status.LastFailTime = now

	// Quick fail detection
	if now.Sub(status.QuickFailWindowStart) > hs.quickFailWindow {
		status.QuickFailCount = 0
		status.QuickFailWindowStart = now
	}
	status.QuickFailCount++

	oldStatus := status.Status

	shouldMarkUnhealthy := status.ConsecutiveFailures >= hs.failureThreshold ||
		status.QuickFailCount >= hs.quickFailThreshold

	if shouldMarkUnhealthy && status.Status == HealthHealthy {
		status.Status = HealthUnhealthy
		status.LastStateChange = now
		hs.emitEvent(modelID, "", status.ModelName, oldStatus, HealthUnhealthy, "failure threshold exceeded")
	}
}

// CleanOrphaned removes health entries for model IDs not in the active set.
func (hs *HealthStore) CleanOrphaned(activeModelIDs []string) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	activeSet := make(map[string]struct{}, len(activeModelIDs))
	for _, id := range activeModelIDs {
		activeSet[id] = struct{}{}
	}

	for key := range hs.data {
		if _, ok := activeSet[key]; !ok {
			delete(hs.data, key)
		}
	}
}

// getOrCreateLocked returns the health status for the given key, creating a new
// Healthy entry if one does not exist. Must be called with the write lock held.
func (hs *HealthStore) getOrCreateLocked(modelID string) *ModelHealthStatus {
	status, ok := hs.data[modelID]
	if !ok {
		now := time.Now()
		status = &ModelHealthStatus{
			Status:          HealthHealthy,
			FailureWindow:   NewFailureRingBuffer(hs.failureRingCapacity),
			CreatedAt:       now,
			LastStateChange: now,
		}
		hs.data[modelID] = status
	}
	return status
}

// emitEvent emits a HealthEvent through the internal event publisher.
// Must be called with the write lock held.
func (hs *HealthStore) emitEvent(modelID, groupID, modelName string, oldStatus, newStatus HealthStatus, reason string) {
	hs.events.Publish(HealthEvent{
		ModelID:   modelID,
		GroupID:   groupID,
		ModelName: modelName,
		OldStatus: oldStatus,
		NewStatus: newStatus,
		Reason:    reason,
		Timestamp: time.Now(),
	})
}
