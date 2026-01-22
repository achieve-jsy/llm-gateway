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
