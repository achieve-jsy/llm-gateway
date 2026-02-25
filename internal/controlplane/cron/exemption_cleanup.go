package cron

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/service"
)

// ExemptionCleanupCron 定时清理过期的配额豁免记录（每小时执行一次）
type ExemptionCleanupCron struct {
	svc      *service.QuotaExemptionService
	interval time.Duration
}

// NewExemptionCleanupCron 创建豁免清理定时任务
func NewExemptionCleanupCron(svc *service.QuotaExemptionService) *ExemptionCleanupCron {
	return &ExemptionCleanupCron{
		svc:      svc,
		interval: 1 * time.Hour,
	}
}

// Start 启动清理调度循环
func (c *ExemptionCleanupCron) Start(ctx context.Context) {
	go func() {
		slog.Info("exemption_cleanup_cron_started", "interval", c.interval)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("exemption_cleanup_cron_stopped")
				return
			case <-ticker.C:
				count, err := c.svc.CleanupExpired(ctx)
				if err != nil {
					slog.Error("exemption_cleanup_cron_error", "error", err)
				} else if count > 0 {
					slog.Info("exemption_cleanup_cron_completed", "cleaned", count)
				}
			}
		}
	}()
}

// RunOnce 立即执行一次清理（用于测试或手动触发）
func (c *ExemptionCleanupCron) RunOnce(ctx context.Context) (int64, error) {
	return c.svc.CleanupExpired(ctx)
}
