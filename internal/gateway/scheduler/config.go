package scheduler

import "time"

// SchedulerConfig 调度器配置
//
// 注意：gateway 不再主动探测上游健康。
// 原先的 HealthProbeInterval / MaxProbeConcurrency / ChannelProbeTimeout 已移除——
// 那套探测打的是 {endpoint}/health，与协议无关，多数厂商不提供该端点，
// 返回的 404 会被错记成「渠道不健康」。
// 渠道级健康由 control-plane 的探测负责；gateway 只根据真实请求结果标记状态。
type SchedulerConfig struct {
	ControlPlaneURL    string        `json:"control_plane_url"`
	InternalToken      string        `json:"internal_token"`
	RedisClient        interface{}   `json:"-"` // *redis.Client, avoid import cycle
	ConfigSyncInterval time.Duration `json:"config_sync_interval"`
	FailureThreshold   int           `json:"failure_threshold"`

	// UnhealthyTTL 是模型被判不健康后的观察期。
	// 过期后重新纳入候选池（半开），由真实请求验证是否已恢复。
	// 这是移除主动探测后唯一的不健康状态退出路径：
	// 期间该模型被 FilterHealthy 排除，过期后放回来再试，仍坏则再次被标记。
	UnhealthyTTL time.Duration `json:"unhealthy_ttl"`

	QuickFailWindow    time.Duration `json:"quick_fail_window"`
	QuickFailThreshold int           `json:"quick_fail_threshold"`
}

// DefaultConfig 返回默认配置
func DefaultConfig(controlPlaneURL string) SchedulerConfig {
	return SchedulerConfig{
		ControlPlaneURL:    controlPlaneURL,
		ConfigSyncInterval: 5 * time.Second,
		FailureThreshold:   3,
		UnhealthyTTL:       60 * time.Second,
		QuickFailWindow:    60 * time.Second,
		QuickFailThreshold: 5,
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
	if c.UnhealthyTTL <= 0 {
		c.UnhealthyTTL = 60 * time.Second
	}
	return nil
}
