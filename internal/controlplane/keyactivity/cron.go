package keyactivity

import (
	"context"
	"log/slog"
	"time"
)

// KeyActivityCron 定时驱动 Scanner.ScanOnce（仿 internal/observability/cron/rollup_cron.go）
// 读取 system_configs[key_activity.scan_interval_sec] 作为扫描周期，默认 60 秒。
type KeyActivityCron struct {
	scanner *Scanner
}

// NewCron 创建 Cron 实例
func NewCron(scanner *Scanner, _ ConfigProvider) *KeyActivityCron {
	return &KeyActivityCron{scanner: scanner}
}

// Start 启动后台 goroutine：按配置周期定时调用 ScanOnce
func (c *KeyActivityCron) Start(ctx context.Context) {
	go func() {
		slog.Info("key_activity_cron_started")
		for {
			interval := c.getInterval(ctx)
			select {
			case <-ctx.Done():
				slog.Info("key_activity_cron_stopped")
				return
			case <-time.After(interval):
				if err := c.scanner.ScanOnce(ctx); err != nil {
					slog.Error("key_activity_scan_error", "error", err)
				}
			}
		}
	}()
}

// RunOnce 立即执行一轮扫描（用于启动补跑、冷启动触发、测试）
func (c *KeyActivityCron) RunOnce(ctx context.Context) error {
	return c.scanner.ScanOnce(ctx)
}

// getInterval 通过 Scanner 访问配置读取 scan_interval_sec，默认 60 秒
func (c *KeyActivityCron) getInterval(ctx context.Context) time.Duration {
	cfg := c.scanner.cfgProv.GetKeyActivityConfig(ctx)
	if cfg == nil || cfg.ScanIntervalSec <= 0 {
		return 60 * time.Second
	}
	return time.Duration(cfg.ScanIntervalSec) * time.Second
}
