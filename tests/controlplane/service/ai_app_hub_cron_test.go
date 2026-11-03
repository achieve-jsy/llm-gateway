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
