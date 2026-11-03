package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/require"
)

// blockingFetcher 在 FetchDepartments 里阻塞，用来制造"同步进行中"。
type blockingFetcher struct {
	started chan struct{}
	release chan struct{}
}

func (f *blockingFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	close(f.started)
	<-f.release
	return []ehrclient.Department{{ID: xlabDeptID, Name: "战略发展中心", Type: 2}}, nil
}

// errFetcher 永远返回上游错误。
type errFetcher struct{ err error }

func (f errFetcher) FetchDepartments(context.Context) ([]ehrclient.Department, error) {
	return nil, f.err
}

func TestSyncRejectsConcurrentRun(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	f := &blockingFetcher{started: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewAIAppHubSyncService(f, repo, settings, service.DefaultSyncRules())

	done := make(chan error, 1)
	go func() {
		_, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
		done <- err
	}()

	select {
	case <-f.started:
	case <-time.After(2 * time.Second):
		t.Fatal("第一次同步没有进入 FetchDepartments")
	}

	_, err := svc.Sync(context.Background(), service.SyncScope{Kind: service.ScopeAll})
	require.Error(t, err)
	require.True(t, errors.Is(err, service.ErrSyncInProgress), "got %v", err)

	close(f.release)
	require.NoError(t, <-done, "第一次同步应当正常结束")
}

func TestSyncFailureKeepsLocalData(t *testing.T) {
	db, settings := newAIHubTestDBWithSettings(t)
	repo := repository.NewAIAppHubRepo(db)
	ctx := context.Background()

	ok := &mutableFetcher{items: []ehrclient.Department{
		{ID: xlabDeptID, Name: "战略发展中心", Type: 2},
		{ID: pubDeptID, Name: "公共关系事业部", Type: 2},
	}}
	svc := service.NewAIAppHubSyncService(ok, repo, settings, service.DefaultSyncRules())
	_, err := svc.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.NoError(t, err)

	before, err := repo.ListStudios(ctx, false)
	require.NoError(t, err)
	require.Len(t, before, 2)

	broken := service.NewAIAppHubSyncService(errFetcher{err: ehrclient.ErrUpstream}, repo, settings, service.DefaultSyncRules())
	_, err = broken.Sync(ctx, service.SyncScope{Kind: service.ScopeAll})
	require.Error(t, err)

	after, err := repo.ListStudios(ctx, false)
	require.NoError(t, err)
	require.Len(t, after, 2, "上游失败不得清空本地数据")

	status, err := settings.GetValue("ai_app_hub.last_sync_status")
	require.NoError(t, err)
	require.Equal(t, "failed", status)
	detail, err := settings.GetValue("ai_app_hub.last_sync_error")
	require.NoError(t, err)
	require.NotEmpty(t, detail)
}
