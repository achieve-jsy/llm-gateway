package scheduler

import (
	"context"
	"sync/atomic"
	"time"
)

type Scheduler struct {
	router          *Router
	failoverHandler *FailoverHandler
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

func (s *Scheduler) Route(groupName string) (*RouteDecision, error) {
	return s.router.Route(groupName)
}

func (s *Scheduler) Failover(groupName, failedModelID string) (*RouteDecision, error) {
	return s.failoverHandler.Failover(groupName, failedModelID)
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
