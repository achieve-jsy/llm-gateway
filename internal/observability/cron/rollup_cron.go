package cron

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/observability/service"
)

// RollupCron 聚合调度器（每 5 分钟重算当天 + 昨天 usage_daily）
type RollupCron struct {
	svc      *service.RollupService
	interval time.Duration
}

// NewRollupCron 创建 RollupCron
func NewRollupCron(svc *service.RollupService) *RollupCron {
	return &RollupCron{
		svc:      svc,
		interval: 5 * time.Minute,
	}
}

// Start 启动调度循环
func (c *RollupCron) Start(ctx context.Context) {
	go func() {
		slog.Info("rollup_cron_started", "interval", c.interval)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("rollup_cron_stopped")
				return
			case <-ticker.C:
				if err := c.svc.RunTodayAndYesterday(ctx); err != nil {
					slog.Error("rollup_cron_error", "error", err)
				}
			}
		}
	}()
}

// RunOnce 立即执行一次（用于测试或手动触发）
func (c *RollupCron) RunOnce(ctx context.Context) error {
	return c.svc.RunTodayAndYesterday(ctx)
}