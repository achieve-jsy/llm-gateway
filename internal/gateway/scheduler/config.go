package scheduler

import "time"

// SchedulerConfig 调度器配置
type SchedulerConfig struct {
	ControlPlaneURL          string        `json:"control_plane_url"`
	InternalToken            string        `json:"internal_token"`
	RedisClient              interface{}   `json:"-"` // *redis.Client, avoid import cycle
	ConfigSyncInterval       time.Duration `json:"config_sync_interval"`
	HealthProbeInterval      time.Duration `json:"health_probe_interval"`
	MaxProbeConcurrency      int           `json:"max_probe_concurrency"`
	FailureThreshold         int           `json:"failure_threshold"`
	RecoverySuccessThreshold int           `json:"recovery_success_threshold"`
	QuickFailWindow          time.Duration `json:"quick_fail_window"`
	QuickFailThreshold       int           `json:"quick_fail_threshold"`
	ChannelProbeTimeout      time.Duration `json:"channel_probe_timeout"`
}

// DefaultConfig 返回默认配置
func DefaultConfig(controlPlaneURL string) SchedulerConfig {
	return SchedulerConfig{
		ControlPlaneURL:          controlPlaneURL,
		ConfigSyncInterval:       5 * time.Second,
		HealthProbeInterval:      30 * time.Second,
		MaxProbeConcurrency:      50,
		FailureThreshold:         3,
		RecoverySuccessThreshold: 3,
		QuickFailWindow:          60 * time.Second,
		QuickFailThreshold:       5,
		ChannelProbeTimeout:      5 * time.Second,
	}
}

// Validate 校验配置合法性
func (c SchedulerConfig) Validate() error {
	if c.ControlPlaneURL == "" {
		return ErrInvalidConfig
	}
	if c.FailureThreshold < 1 {
		c.FailureThreshold = 3
	}
	if c.MaxProbeConcurrency < 1 {
		c.MaxProbeConcurrency = 50
	}
	return nil
}
