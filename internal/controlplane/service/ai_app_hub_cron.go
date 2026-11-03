package service

import (
	"context"
	"log/slog"
	"time"
)

// SyncRunner 定时任务只需要"跑一次全量"，抽成接口便于测试与复用。
type SyncRunner interface {
	Sync(ctx context.Context, scope SyncScope) (HubSyncResult, error)
}

// StartAIAppHubSyncCron 启动后台同步：先立即跑一次（冷启动补齐），之后按 interval 循环。
//
// 单次失败只记日志不退出——EHR 抖动不该让进程里的定时器停摆。
func StartAIAppHubSyncCron(ctx context.Context, runner SyncRunner, interval time.Duration) {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	go func() {
		slog.Info("ai_app_hub_sync_cron_started", "interval", interval.String())
		runAIAppHubSyncOnce(ctx, runner)
		for {
			select {
			case <-ctx.Done():
				slog.Info("ai_app_hub_sync_cron_stopped")
				return
			case <-time.After(interval):
				runAIAppHubSyncOnce(ctx, runner)
			}
		}
	}()
}

func runAIAppHubSyncOnce(ctx context.Context, runner SyncRunner) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("ai_app_hub_sync_cron_panic", "panic", rec)
		}
	}()
	result, err := runner.Sync(ctx, SyncScope{Kind: ScopeAll})
	if err != nil {
		slog.Error("ai_app_hub_sync_cron_failed", "error", err)
		return
	}
	slog.Info("ai_app_hub_sync_cron_done",
		"departments", result.Departments, "studios", result.Studios, "duration_ms", result.DurationMs)
}
