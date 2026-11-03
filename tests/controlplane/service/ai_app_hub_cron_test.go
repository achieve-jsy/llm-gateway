package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/service"
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

func TestSyncCronRunsImmediatelyAndStopsOnCancel(t *testing.T) {
	runner := &countingRunner{}
	ctx, cancel := context.WithCancel(context.Background())

	service.StartAIAppHubSyncCron(ctx, runner, 10*time.Millisecond)

	require.Eventually(t, func() bool { return runner.count() >= 1 }, time.Second, 5*time.Millisecond)
	require.Equal(t, service.ScopeAll, runner.firstScope().Kind, "定时任务跑全量")

	cancel()
	time.Sleep(30 * time.Millisecond)
	stopped := runner.count()
	time.Sleep(60 * time.Millisecond)
	require.Equal(t, stopped, runner.count(), "取消后不再触发")
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
