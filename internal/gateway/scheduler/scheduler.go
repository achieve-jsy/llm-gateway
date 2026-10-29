package scheduler

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/example/llm-gateway/internal/core"
)

type Scheduler struct {
	router          *Router
	failoverHandler *FailoverHandler
	healthStore     *HealthStore
	configSyncer    *ConfigSyncer
	healthProber    *HealthProber
	eventPublisher  *EventPublisher
	config          SchedulerConfig
	ready           atomic.Bool
}

func New(cfg SchedulerConfig) (*Scheduler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	rt := NewRouteTable()
	hs := NewHealthStore(cfg.FailureThreshold, cfg.RecoverySuccessThreshold, cfg.QuickFailThreshold, cfg.QuickFailWindow)
	router := NewRouter(rt, hs)

	// Wire sticky store if Redis client is available
	if cfg.RedisClient != nil {
		// RedisClient is interface{} to avoid import cycle, cast to *redis.Client
		stickyStore := NewStickyStore(cfg.RedisClient)
		router.SetStickyStore(stickyStore)
	}

	failoverHandler := NewFailoverHandler(rt, hs, router)
	configSyncer := NewConfigSyncer(cfg.ControlPlaneURL, cfg.InternalToken, rt, hs, cfg.ConfigSyncInterval)
	healthProber := NewHealthProber(hs, rt, cfg.HealthProbeInterval, cfg.MaxProbeConcurrency, cfg.ChannelProbeTimeout)

	return &Scheduler{
		router:          router,
		failoverHandler: failoverHandler,
		healthStore:     hs,
		configSyncer:    configSyncer,
		healthProber:    healthProber,
		eventPublisher:  hs.Events(),
		config:          cfg,
	}, nil
}

func (s *Scheduler) Start(ctx context.Context) {
	go s.configSyncer.Run()
	go s.healthProber.Run()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !s.configSyncer.LastSyncAt().IsZero() {
					s.ready.Store(true)
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

func (s *Scheduler) Route(groupName string, req *core.ChatRequest) (*RouteDecision, error) {
	return s.router.Route(groupName, req)
}

func (s *Scheduler) Failover(groupName, failedModelID string, req *core.ChatRequest) (*RouteDecision, error) {
	return s.failoverHandler.Failover(groupName, failedModelID, req)
}

// FailoverExcluding 排除多个已失败模型后选出下一个候选（多轮 failover 使用）。
func (s *Scheduler) FailoverExcluding(groupName string, excludeIDs []string, req *core.ChatRequest) (*RouteDecision, error) {
	return s.failoverHandler.FailoverExcluding(groupName, excludeIDs, req)
}

// FailoverExcludingVideo 由调用方显式给出"是否必须保持视频能力"，
// 供没有 core.ChatRequest 的协议（/v1/responses）使用。
func (s *Scheduler) FailoverExcludingVideo(groupName string, excludeIDs []string, needsVideo bool, req *core.ChatRequest) (*RouteDecision, error) {
	return s.failoverHandler.FailoverExcludingVideo(groupName, excludeIDs, needsVideo, req)
}

// SetCooldown 为指定模型设置 429 冷却期，冷却期内不会被路由选中。
func (s *Scheduler) SetCooldown(modelID string, duration time.Duration) {
	s.healthStore.SetCooldown(modelID, duration)
}

// GetGroupConfig 返回指定组的路由配置（含 TimeoutSeconds 等元数据）。
// 若组不存在则返回 nil。
func (s *Scheduler) GetGroupConfig(groupName string) *GroupRoutingConfig {
	return s.router.routeTable.Get(groupName)
}

func (s *Scheduler) NotifyFailure(modelID string) {
	s.healthProber.NotifyFailure(modelID)
}

func (s *Scheduler) IsReady() bool {
	return s.ready.Load()
}

func (s *Scheduler) Shutdown() {
	s.configSyncer.Stop()
	s.healthProber.Stop()
}

// PersistSticky persists a sticky mapping after routing decision
func (s *Scheduler) PersistSticky(ctx context.Context, groupName, sessionKey string, decision *RouteDecision) {
	if s.router.stickyStore == nil {
		return
	}

	group := s.router.routeTable.Get(groupName)
	if group == nil || !group.StickyEnabled {
		return
	}

	ttl := time.Duration(group.StickyTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Hour // Default fallback
	}

	_ = s.router.stickyStore.Set(ctx, groupName, sessionKey, encodeStickyBinding(decision.ModelName, decision.ProviderName), ttl)
}

// UpdateSticky updates a sticky mapping after failover. Returns true if a pin
// was actually written (group has sticky enabled), false otherwise.
func (s *Scheduler) UpdateSticky(ctx context.Context, groupName, sessionKey, newModelName, newProviderName string) bool {
	if s.router.stickyStore == nil || sessionKey == "" {
		return false
	}

	group := s.router.routeTable.Get(groupName)
	if group == nil || !group.StickyEnabled {
		return false
	}

	ttl := time.Duration(group.StickyTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}

	return s.router.stickyStore.Set(ctx, groupName, sessionKey, encodeStickyBinding(newModelName, newProviderName), ttl) == nil
}
