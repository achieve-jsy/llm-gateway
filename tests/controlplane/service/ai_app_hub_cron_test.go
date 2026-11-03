package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type countingRunner struct {
	mu     sync.Mutex
	calls  int
	scopes []service.SyncScope
}

func (r *countingRunner) Sync(_ context.Context, scope service.SyncScope) (service.HubSyncResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.scopes = append(r.scopes, scope)
	return service.HubSyncResult{Scope: string(scope.Kind)}, nil
}

func (r *countingRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *countingRunner) firstScope() service.SyncScope {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.scopes) == 0 {
		return service.SyncScope{}
	}
	return r.scopes[0]
}


func TestLoadSyncScheduleDefaults(t *testing.T) {
	env := map[string]string{}
	s, err := service.LoadSyncSchedule(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	require.NotNil(t, s)

	// 2026-11-02 03:00 +08 已成功 → 下一次是 11-03 03:00 +08
	last := time.Date(2026, 11, 2, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	want := time.Date(2026, 11, 3, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	require.True(t, s.Next(last).Equal(want), "下一次应是明天 03:00（比较时刻，不比较 Location 指针）")
}

func TestLoadSyncScheduleDisabledWhenCronEmpty(t *testing.T) {
	env := map[string]string{"AI_APP_HUB_SYNC_CRON": ""}
	s, err := service.LoadSyncSchedule(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	require.Nil(t, s, "空表达式是运维开关，返回 nil 表示不启动")
}

func TestLoadSyncScheduleRejectsBadCron(t *testing.T) {
	env := map[string]string{"AI_APP_HUB_SYNC_CRON": "not-a-cron"}
	_, err := service.LoadSyncSchedule(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.Error(t, err)
}

func TestIsDueUsesConfiguredTimezone(t *testing.T) {
	env := map[string]string{}
	s, err := service.LoadSyncSchedule(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)

	// 昨天 03:00 成功；现在 10:20（重启补跑）→ 该跑
	last := time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.FixedZone("CST", 8*3600))
	require.True(t, s.IsDue(last, now))

	// 今天 03:00 已成功，03:00:30 再 tick → 不跑
	last = time.Date(2026, 11, 2, 3, 0, 0, 0, time.FixedZone("CST", 8*3600))
	require.False(t, s.IsDue(last, time.Date(2026, 11, 2, 3, 0, 30, 0, time.FixedZone("CST", 8*3600))))

	// UTC 容器视角：2026-11-02T19:00Z 就是上海 11-03 03:00
	require.True(t, s.IsDue(last, time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)))
}

func TestLoadSyncScheduleFallsBackOnBadTimezone(t *testing.T) {
	env := map[string]string{"AI_APP_HUB_SYNC_TZ": "Mars/Base"}
	s, err := service.LoadSyncSchedule(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	require.NotNil(t, s, "时区坏掉不能阻止调度启动，回退 time.Local")
}
func newSyncStateStoreForTest(t *testing.T) (*service.SyncStateStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return service.NewSyncStateStore(redis.NewClient(&redis.Options{Addr: mr.Addr()})), mr
}

func TestSyncStateStoreLastSuccessRoundTrip(t *testing.T) {
	store, _ := newSyncStateStoreForTest(t)
	ctx := context.Background()

	_, ok, err := store.LastSuccess(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	want := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	require.NoError(t, store.MarkSuccess(ctx, want))

	got, ok, err := store.LastSuccess(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.WithinDuration(t, want, got, time.Second)
}

func TestSyncStateStoreLockIsExclusiveAndOwnerScoped(t *testing.T) {
	store, _ := newSyncStateStoreForTest(t)
	ctx := context.Background()

	release, ok := store.TryLock(ctx)
	require.True(t, ok)
	_, second := store.TryLock(ctx)
	require.False(t, second, "同一时刻只允许一个持有者")

	release()
	_, third := store.TryLock(ctx)
	require.True(t, third, "释放后可以重新获取")
}
type flakyRunner struct {
	calls atomic.Int32 // 后台 goroutine 也在调，读写都要原子
	errs  []error      // 按调用序号返回；耗尽后返回 nil
}

func (r *flakyRunner) Sync(context.Context, service.SyncScope) (service.HubSyncResult, error) {
	n := int(r.calls.Add(1))
	if n-1 < len(r.errs) && r.errs[n-1] != nil {
		return service.HubSyncResult{}, r.errs[n-1]
	}
	return service.HubSyncResult{Scope: "all"}, nil
}

func (r *flakyRunner) count() int { return int(r.calls.Load()) }

func syncScheduleForTest(t *testing.T) *service.SyncSchedule {
	t.Helper()
	sched, err := service.LoadSyncSchedule(func(k string) (string, bool) {
		if k == "AI_APP_HUB_SYNC_CRON" {
			return "0 3 * * *", true
		}
		return "", false
	})
	require.NoError(t, err)
	require.NotNil(t, sched)
	return sched
}

func cronOptsForTest(t *testing.T, runner service.SyncRunner, now time.Time) (service.SyncCronOptions, *service.SyncStateStore) {
	t.Helper()
	store, _ := newSyncStateStoreForTest(t)
	return service.SyncCronOptions{
		Schedule: syncScheduleForTest(t),
		State:    store,
		Runner:   runner,
		Attempts: 3,
		Now:      func() time.Time { return now },
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}, store
}

func TestRunSyncCycleRunsOnFirstStart(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, attempts, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, runner.count(), "首次没有 last_success，立即跑一次")

	_, ok, err := store.LastSuccess(context.Background())
	require.NoError(t, err)
	require.True(t, ok, "成功后才记账")

	// 同一个周期内再 tick：不重复
	ran, _, err = service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.False(t, ran)
	require.Equal(t, 1, runner.count())
}

func TestRunSyncCycleCatchesUpAfterRestart(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)
	require.NoError(t, store.MarkSuccess(context.Background(), time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)))

	ran, _, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, ran, "昨天成功后错过今天 03:00，起来要补跑")
}

func TestRunSyncCycleSkipsWhenLockHeld(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)
	_, ok := store.TryLock(context.Background())
	require.True(t, ok)

	ran, _, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.False(t, ran)
	require.Zero(t, runner.count())
}

func TestRunSyncCycleRetriesThenSucceeds(t *testing.T) {
	runner := &flakyRunner{errs: []error{errors.New("boom"), errors.New("boom")}}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, attempts, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, 3, attempts)
	require.Equal(t, 3, runner.count())
	_, ok, err := store.LastSuccess(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRunSyncCycleGivesUpAndDoesNotMarkSuccess(t *testing.T) {
	runner := &flakyRunner{errs: []error{errors.New("a"), errors.New("b"), errors.New("c")}}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, attempts, err := service.RunSyncCycle(context.Background(), opts)
	require.Error(t, err)
	require.True(t, ran)
	require.Equal(t, 3, attempts)
	_, ok, err := store.LastSuccess(context.Background())
	require.NoError(t, err)
	require.False(t, ok, "失败不得记账，否则当天会静默漏跑")
}

func TestRunSyncCycleSkipsRetryWhenSyncInProgress(t *testing.T) {
	runner := &flakyRunner{errs: []error{service.ErrSyncInProgress}}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	opts, store := cronOptsForTest(t, runner, now)

	ran, _, err := service.RunSyncCycle(context.Background(), opts)
	require.NoError(t, err, "手动触发正在进行不算失败")
	require.False(t, ran)
	require.Equal(t, 1, runner.count(), "不重试")
	_, ok, _ := store.LastSuccess(context.Background())
	require.False(t, ok)
}

func TestStartSyncCronTicksAndStopsOnCancel(t *testing.T) {
	runner := &flakyRunner{}
	now := time.Date(2026, 11, 2, 10, 20, 0, 0, time.UTC)
	store, _ := newSyncStateStoreForTest(t)
	ctx, cancel := context.WithCancel(context.Background())

	service.StartAIAppHubSyncCron(ctx, service.SyncCronOptions{
		Schedule: syncScheduleForTest(t), State: store, Runner: runner,
		Tick: 5 * time.Millisecond, Attempts: 1,
		Now:   func() time.Time { return now },
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	require.Eventually(t, func() bool { return runner.count() >= 1 }, time.Second, 5*time.Millisecond)

	cancel()
	time.Sleep(30 * time.Millisecond)
	stopped := runner.count()
	time.Sleep(60 * time.Millisecond)
	require.Equal(t, stopped, runner.count())
}