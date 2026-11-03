package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

// SyncRunner 定时任务只需要"跑一次全量"，抽成接口便于测试与复用。
type SyncRunner interface {
	Sync(ctx context.Context, scope SyncScope) (HubSyncResult, error)
}

const (
	// defaultSyncCron 每天 02:00（配合 defaultSyncTZ 即北京时间）。
	defaultSyncCron = "0 2 * * *"
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
	return &SyncSchedule{loc: loc, sched: sched}, nil
}

// Next 返回 lastSuccess 之后的下一次触发时刻（已在配置时区内）。
func (s *SyncSchedule) Next(lastSuccess time.Time) time.Time {
	return s.sched.Next(lastSuccess.In(s.loc))
}

// IsDue 判断现在是否已经越过"上一次成功之后的那一次触发"。
//
// 这也是补跑判据：重启发生在 02:00 之后时，Next(昨天成功) = 今天 02:00 ≤ now → 该跑。
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
	// syncLastFailureKey 最近一次彻底失败的时间。失败后冷却一段时间再试，
	// 避免上游宕机时每个 tick 都重跑一整轮重试。
	syncLastFailureKey = "ai_app_hub:sync:last_failure"
	// syncLastSuccessBuildKey 最近一次成功同步的构建时间戳（秒）。
	//
	// 蓝绿两色都跑同一个 cron：切流后备用色是老镜像，谁先到点谁抢到锁，
	// 当天的同步就可能由旧代码执行（实测发生过：旧色先跑，把 last_success 写掉，
	// 新代码当天再也不会跑）。用构建时间戳比较，让更新的构建补跑一次、旧构建让位。
	syncLastSuccessBuildKey = "ai_app_hub:sync:last_success_build"

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
	return s.readTime(ctx, syncLastSuccessKey)
}

func (s *SyncStateStore) readTime(ctx context.Context, key string) (time.Time, bool, error) {
	raw, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// 值坏了等价于没有记录：宁可多跑一次，也不要因为脏值卡住调度。
		slog.Warn("ai_app_hub_sync_state_corrupt", "key", key, "value", raw, "error", err)
		return time.Time{}, false, nil
	}
	return t, true, nil
}

func (s *SyncStateStore) MarkSuccess(ctx context.Context, t time.Time) error {
	// 成功即清掉失败标记，避免冷却窗口继续挡着下一轮。
	if err := s.rdb.Del(ctx, syncLastFailureKey).Err(); err != nil {
		slog.Warn("ai_app_hub_sync_clear_failure_failed", "error", err)
	}
	return s.rdb.Set(ctx, syncLastSuccessKey, t.UTC().Format(time.RFC3339), syncLastSuccessTTL).Err()
}

// MarkSuccessBuild 记下这次成功同步来自哪个构建（APP_BUILD_TIME，秒）。
func (s *SyncStateStore) MarkSuccessBuild(ctx context.Context, build int64) error {
	if build <= 0 {
		return nil
	}
	return s.rdb.Set(ctx, syncLastSuccessBuildKey, strconv.FormatInt(build, 10), syncLastSuccessTTL).Err()
}

// LastSuccessBuild 最近一次成功同步的构建时间戳；没有记录返回 (0, false, nil)。
func (s *SyncStateStore) LastSuccessBuild(ctx context.Context) (int64, bool, error) {
	raw, err := s.rdb.Get(ctx, syncLastSuccessBuildKey).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	n, convErr := strconv.ParseInt(raw, 10, 64)
	if convErr != nil {
		slog.Warn("ai_app_hub_sync_state_corrupt", "key", syncLastSuccessBuildKey, "value", raw, "error", convErr)
		return 0, false, nil
	}
	return n, true, nil
}

func (s *SyncStateStore) MarkFailure(ctx context.Context, t time.Time) error {
	return s.rdb.Set(ctx, syncLastFailureKey, t.UTC().Format(time.RFC3339), syncLastSuccessTTL).Err()
}

// LastFailure 最近一次彻底失败的时间；(zero,false,nil) 表示没有记录。
func (s *SyncStateStore) LastFailure(ctx context.Context) (time.Time, bool, error) {
	return s.readTime(ctx, syncLastFailureKey)
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

// ---------- 调度周期 ----------

const (
	defaultSyncTick     = time.Minute
	defaultSyncAttempts = 3
	// defaultSyncRetryAfter 失败后至少隔这么久再开始下一轮（上游长时间宕机时的自我限流）。
	defaultSyncRetryAfter = 30 * time.Minute
)

var defaultSyncBackoff = []time.Duration{30 * time.Second, 2 * time.Minute}

// SyncCronOptions 定时同步的全部可调参数。零值可用：Tick/Attempts/Backoff/Now/Sleep
// 都有默认值，测试只覆盖需要固定的那几个。
type SyncCronOptions struct {
	Schedule   *SyncSchedule
	State      *SyncStateStore
	Runner     SyncRunner
	Tick       time.Duration
	Attempts   int
	RetryAfter time.Duration
	Backoff    []time.Duration
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration) error
	// Build 本实例的构建时间戳（秒），见 BuildEpoch。0 表示不参与构建优先级判断。
	Build int64
}

// BuildEpoch 从 APP_BUILD_TIME 读本实例的构建时间戳（秒），由部署脚本注入。
// 读不到返回 0：那就退回"只有到点才跑"的老行为，不影响同步本身。
func BuildEpoch() int64 {
	raw := strings.TrimSpace(os.Getenv("APP_BUILD_TIME"))
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		slog.Warn("ai_app_hub_sync_build_env_invalid", "value", raw, "error", err)
		return 0
	}
	return n
}

func (o SyncCronOptions) withDefaults() SyncCronOptions {
	if o.Tick <= 0 {
		o.Tick = defaultSyncTick
	}
	if o.Attempts <= 0 {
		o.Attempts = defaultSyncAttempts
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = defaultSyncRetryAfter
	}
	if len(o.Backoff) == 0 {
		o.Backoff = defaultSyncBackoff
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	return o
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RunSyncCycle 跑一轮：到点判定 → 抢 leader 锁 → 重试执行 → 成功后记账。
//
// 导出是为了让测试直接驱动一轮，不必依赖真实时钟。返回 ran=false 表示本轮没有执行
// （未到点、锁被占、手动同步进行中），attempts 是实际尝试次数。
func RunSyncCycle(ctx context.Context, opts SyncCronOptions) (ran bool, attempts int, err error) {
	opts = opts.withDefaults()
	if opts.Schedule == nil || opts.State == nil || opts.Runner == nil {
		return false, 0, errors.New("sync cron: schedule/state/runner 都是必需的")
	}

	last, hasLast, err := opts.State.LastSuccess(ctx)
	if err != nil {
		slog.Warn("ai_app_hub_sync_last_success_read_failed", "error", err)
		return false, 0, err
	}
	due := !hasLast || opts.Schedule.IsDue(last, opts.Now())
	if !due && opts.Build > 0 {
		// 本实例的构建比上次成功同步的构建更新（刚发版）→ 立刻补跑一次；
		// 反之说明是刚被切下去的旧色，让位给新色，别用旧代码覆盖当天数据。
		if prev, ok, berr := opts.State.LastSuccessBuild(ctx); berr != nil {
			slog.Warn("ai_app_hub_sync_last_build_read_failed", "error", berr)
		} else if !ok || opts.Build > prev {
			due = true
		}
	}
	if !due {
		return false, 0, nil
	}

	// 失败冷却：上游宕着的时候，每个 tick 都重试只会把 3 次尝试放大成上千次拉取。
	if failedAt, hasFailure, ferr := opts.State.LastFailure(ctx); ferr == nil && hasFailure {
		if opts.Now().Before(failedAt.Add(opts.RetryAfter)) {
			return false, 0, nil
		}
	}

	release, locked := opts.State.TryLock(ctx)
	if !locked {
		slog.Info("ai_app_hub_sync_cycle_skipped", "reason", "lock held")
		return false, 0, nil
	}
	defer release()

	for i := 1; i <= opts.Attempts; i++ {
		_, err = opts.Runner.Sync(ctx, SyncScope{Kind: ScopeAll})
		if err == nil {
			if markErr := opts.State.MarkSuccess(ctx, opts.Now()); markErr != nil {
				slog.Warn("ai_app_hub_sync_mark_success_failed", "error", markErr)
			}
			if buildErr := opts.State.MarkSuccessBuild(ctx, opts.Build); buildErr != nil {
				slog.Warn("ai_app_hub_sync_mark_build_failed", "error", buildErr)
			}
			return true, i, nil
		}
		// 手动触发正在进行：不是失败，不重试也不记账。
		if errors.Is(err, ErrSyncInProgress) {
			slog.Info("ai_app_hub_sync_cycle_skipped", "reason", "sync in progress")
			return false, i, nil
		}
		slog.Warn("ai_app_hub_sync_attempt_failed", "attempt", i, "error", err)
		if i < opts.Attempts {
			d := opts.Backoff[len(opts.Backoff)-1]
			if i-1 < len(opts.Backoff) {
				d = opts.Backoff[i-1]
			}
			if sleepErr := opts.Sleep(ctx, d); sleepErr != nil {
				return true, i, sleepErr
			}
		}
	}
	if markErr := opts.State.MarkFailure(ctx, opts.Now()); markErr != nil {
		slog.Warn("ai_app_hub_sync_mark_failure_failed", "error", markErr)
	}
	return true, opts.Attempts, err
}

// StartAIAppHubSyncCron 起后台调度：启动先跑一轮（没有 last_success 时即冷启动补齐），
// 之后每 Tick 判定一次。恢复不了的 panic 只记日志，不让定时器停摆。
func StartAIAppHubSyncCron(ctx context.Context, opts SyncCronOptions) {
	opts = opts.withDefaults()
	go func() {
		slog.Info("ai_app_hub_sync_cron_started", "tick", opts.Tick.String())
		ticker := time.NewTicker(opts.Tick)
		defer ticker.Stop()

		runOnce := func() {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("ai_app_hub_sync_cron_panic", "panic", rec)
				}
			}()
			ran, attempts, err := RunSyncCycle(ctx, opts)
			slog.Info("ai_app_hub_sync_cycle",
				"ran", ran, "attempts", attempts, "error", err)
		}

		runOnce()
		for {
			select {
			case <-ctx.Done():
				slog.Info("ai_app_hub_sync_cron_stopped")
				return
			case <-ticker.C:
				runOnce()
			}
		}
	}()
}
