package cron

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/observability/service"
)

// RollupCron 每小时聚合调度器
type RollupCron struct {
	svc      *service.RollupService
	cronExpr string
}

// NewRollupCron 创建 RollupCron
func NewRollupCron(svc *service.RollupService) *RollupCron {
	return &RollupCron{
		svc:      svc,
		cronExpr: "0 * * * *", // 每整点执行
	}
}

// Start 启动调度循环（每小时整日重算今天 + 昨天）
func (c *RollupCron) Start(ctx context.Context) {
	go func() {
		slog.Info("rollup_cron_started")
		// 等待到下一个整点
		ticker := time.NewTicker(time.Hour)
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