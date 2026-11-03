package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
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

// ---------- 跨重启状态与多实例互斥 ----------

const (
	// syncLastSuccessKey 最近一次成功的时间（RFC3339）。只有成功才更新，
	// 所以"失败后重启"不会被误判成"今天已经跑过"。
	syncLastSuccessKey = "ai_app_hub:sync:last_success"
	// syncLeaderKey 多实例/蓝绿互斥的 leader 锁。
	syncLeaderKey = "ai_app_hub:sync:leader"

	syncLastSuccessTTL = 30 * 24 * time.Hour
	syncLeaderTTL      = 10 * time.Minute
)

// releaseLeaderScript 只在 value 仍是自己时删除，避免删掉别人抢到的锁。
var releaseLeaderScript = redis.NewScript(
	"if redis.call(\"GET\", KEYS[1]) == ARGV[1] then\n" +
		"  return redis.call(\"DEL\", KEYS[1])\n" +
		"end\n" +
		"return 0\n")

// SyncStateStore 定时同步的跨进程状态：last_success + leader 锁。
type SyncStateStore struct {
	rdb        *redis.Client
	instanceID string
}

func NewSyncStateStore(rdb *redis.Client) *SyncStateStore {
	host, _ := os.Hostname()
	return &SyncStateStore{rdb: rdb, instanceID: fmt.Sprintf("%s:%d", host, os.Getpid())}
}

// LastSuccess 读最近一次成功时间；键不存在返回 (zero, false, nil)。
func (s *SyncStateStore) LastSuccess(ctx context.Context) (time.Time, bool, error) {
	raw, err := s.rdb.Get(ctx, syncLastSuccessKey).Result()
	if err == redis.Nil {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// 值坏了等价于没有记录：宁可多跑一次，也不要因为脏值卡住调度。
		slog.Warn("ai_app_hub_sync_last_success_corrupt", "value", raw, "error", err)
		return time.Time{}, false, nil
	}
	return t, true, nil
}

func (s *SyncStateStore) MarkSuccess(ctx context.Context, t time.Time) error {
	return s.rdb.Set(ctx, syncLastSuccessKey, t.UTC().Format(time.RFC3339), syncLastSuccessTTL).Err()
}

// TryLock 抢本轮执行权；拿不到返回 (nil, false)。SetNX 报错也按"本轮跳过"处理：
// 宁可漏跑一次，也不要两个实例并发写同一批行。
func (s *SyncStateStore) TryLock(ctx context.Context) (func(), bool) {
	ok, err := s.rdb.SetNX(ctx, syncLeaderKey, s.instanceID, syncLeaderTTL).Result()
	if err != nil {
		slog.Warn("ai_app_hub_sync_lock_failed", "error", err)
		return nil, false
	}
	if !ok {
		return nil, false
	}
	return func() {
		_ = releaseLeaderScript.Run(context.Background(), s.rdb, []string{syncLeaderKey}, s.instanceID).Err()
	}, true
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
