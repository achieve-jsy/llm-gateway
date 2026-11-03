package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// SyncRunner 定时任务只需要"跑一次全量"，抽成接口便于测试与复用。
type SyncRunner interface {
	Sync(ctx context.Context, scope SyncScope) (HubSyncResult, error)
}

const (
	// defaultSyncCron 每天 03:00（配合 defaultSyncTZ 即北京时间）。
	defaultSyncCron = "0 3 * * *"
	defaultSyncTZ   = "Asia/Shanghai"

	envSyncCron = "AI_APP_HUB_SYNC_CRON"
	envSyncTZ   = "AI_APP_HUB_SYNC_TZ"
)

// syncCronParser 5 字段（分 时 日 月 周），与 scheduled_report_service 一致。
var syncCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// SyncSchedule 一个解析好的定时表达式 + 时区。
//
// 字段私有：外部只该用 IsDue/Next，避免调用方自己拼时间比较把时区搞错。
type SyncSchedule struct {
	expr  string
	loc   *time.Location
	sched cron.Schedule
}

// LoadSyncSchedule 从环境读定时配置。getenv 语义与 os.LookupEnv 一致，
// 这样"没配置"（用默认值）和"配成空串"（运维开关，关闭调度）才可区分。
//
// 返回 (nil, nil) 表示显式关闭，表达式非法返回 error；时区解析失败回退 time.Local
// 并告警——时区坏掉不该让整个定时任务起不来。
func LoadSyncSchedule(getenv func(string) (string, bool)) (*SyncSchedule, error) {
	expr := defaultSyncCron
	if v, ok := getenv(envSyncCron); ok {
		if v == "" {
			return nil, nil
		}
		expr = v
	}
	sched, err := syncCronParser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("parse %s=%q: %w", envSyncCron, expr, err)
	}

	tz := defaultSyncTZ
	if v, ok := getenv(envSyncTZ); ok && v != "" {
		tz = v
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		slog.Warn("ai_app_hub_sync_tz_fallback", "error", err, "fallback", time.Local.String())
		loc = time.Local
	}
	return &SyncSchedule{expr: expr, loc: loc, sched: sched}, nil
}

// Next 返回 lastSuccess 之后的下一次触发时刻（已在配置时区内）。
func (s *SyncSchedule) Next(lastSuccess time.Time) time.Time {
	return s.sched.Next(lastSuccess.In(s.loc))
}

// IsDue 判断现在是否已经越过"上一次成功之后的那一次触发"。
//
// 这也是补跑判据：重启发生在 03:00 之后时，Next(昨天成功) = 今天 03:00 ≤ now → 该跑。
func (s *SyncSchedule) IsDue(lastSuccess, now time.Time) bool {
	return !now.In(s.loc).Before(s.Next(lastSuccess))
}

func envOr(getenv func(string) (string, bool), key, def string) string {
	if v, ok := getenv(key); ok && v != "" {
		return v
	}
	return def
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
