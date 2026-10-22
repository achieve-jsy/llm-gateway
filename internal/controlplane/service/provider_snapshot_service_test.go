package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	controlmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingModelRepo 计数型 ModelRepoInterface：只实现 ListAvailable，
// 用于统计「真正执行了多少次全量快照重建」。其余方法由嵌入的接口提供（本文件不会调用）。
type countingModelRepo struct {
	ModelRepoInterface

	calls  atomic.Int64
	delay  time.Duration
	models []controlmodel.Model
	err    error
}

func (r *countingModelRepo) ListAvailable() ([]controlmodel.Model, error) {
	r.calls.Add(1)
	if r.delay > 0 {
		// 模拟真实全量重建的耗时（生产环境一次 ListAvailable 约几百毫秒到数秒）
		time.Sleep(r.delay)
	}
	return r.models, r.err
}

func (r *countingModelRepo) callCount() int64 { return r.calls.Load() }

// newTestSnapshotRefresher 创建 miniredis 支撑的 ProviderSnapshotRefresher。
// rdb 必须非 nil，否则 RefreshAvailableSnapshot 会在 nil 判断处直接返回，走不到重建逻辑。
func newTestSnapshotRefresher(t *testing.T, repo ModelRepoInterface) (*ProviderSnapshotRefresher, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err, "start miniredis")
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewProviderSnapshotRefresher(repo, rdb), rdb
}

// TestProviderSnapshotScheduleRefreshCoalescesBurst 验证突发请求合并：
// 72 个模型同时被自动禁用时，实际只执行 1 次在飞重建 + 最多 1 次补跑重建。
func TestProviderSnapshotScheduleRefreshCoalescesBurst(t *testing.T) {
	repo := &countingModelRepo{delay: 300 * time.Millisecond}
	refresher, _ := newTestSnapshotRefresher(t, repo)
	refresher.minInterval = 0 // 关闭节流，只验证单飞 + dirty 合并

	var wg sync.WaitGroup
	for i := 0; i < 72; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refresher.ScheduleRefresh(context.Background())
		}()
	}
	wg.Wait()

	require.True(t, refresher.WaitIdle(10*time.Second), "调度队列应在超时前空闲")
	assert.Equal(t, int64(2), repo.callCount(), "一波 72 次请求应合并为 1~2 次全量重建")
	assert.Equal(t, int64(2), refresher.AutoRebuildCount())
}

// TestProviderSnapshotScheduleRefreshThrottlesSpreadBurst 验证错峰到达的禁用事件
// 会被最小重建间隔节流合并，而不是每个事件各重建一次。
func TestProviderSnapshotScheduleRefreshThrottlesSpreadBurst(t *testing.T) {
	repo := &countingModelRepo{}
	refresher, _ := newTestSnapshotRefresher(t, repo)
	refresher.minInterval = 500 * time.Millisecond

	refresher.ScheduleRefresh(context.Background())
	for i := 0; i < 10; i++ {
		time.Sleep(10 * time.Millisecond)
		refresher.ScheduleRefresh(context.Background())
	}

	require.True(t, refresher.WaitIdle(10*time.Second))
	assert.LessOrEqual(t, repo.callCount(), int64(2), "节流窗口内的错峰请求应合并为最多 2 次重建")
	assert.GreaterOrEqual(t, repo.callCount(), int64(1))
}

// TestProviderSnapshotScheduleRefreshCanceledContext 验证：
// 已取消的上下文不再安排重建，也不会留下卡住的单飞闸门。
func TestProviderSnapshotScheduleRefreshCanceledContext(t *testing.T) {
	repo := &countingModelRepo{}
	refresher, _ := newTestSnapshotRefresher(t, repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	refresher.ScheduleRefresh(ctx)

	assert.Equal(t, int64(0), repo.callCount(), "已取消的请求不应触发重建")
	assert.True(t, refresher.WaitIdle(200*time.Millisecond))

	// 后续正常请求仍然可用（调度器没有被卡死）
	refresher.ScheduleRefresh(context.Background())
	require.True(t, refresher.WaitIdle(5*time.Second))
	assert.Equal(t, int64(1), repo.callCount())
	assert.Equal(t, int64(1), refresher.AutoRebuildCount())
}

// TestProviderSnapshotScheduleRefreshContinuesAfterError 验证重建失败不会卡住单飞闸门。
func TestProviderSnapshotScheduleRefreshContinuesAfterError(t *testing.T) {
	repo := &countingModelRepo{err: assert.AnError}
	refresher, _ := newTestSnapshotRefresher(t, repo)

	refresher.ScheduleRefresh(context.Background())
	require.True(t, refresher.WaitIdle(5*time.Second), "重建失败后调度队列也必须回到空闲")
	assert.Equal(t, int64(1), repo.callCount())

	repo.err = nil
	refresher.ScheduleRefresh(context.Background())
	require.True(t, refresher.WaitIdle(5*time.Second))
	assert.Equal(t, int64(2), repo.callCount())
}

// TestProviderSnapshotRefreshAvailableSnapshotUnchanged 验证同步重建路径语义未变
// （其他 CRUD 调用方仍依赖同步返回 version）。
func TestProviderSnapshotRefreshAvailableSnapshotUnchanged(t *testing.T) {
	repo := &countingModelRepo{}
	refresher, rdb := newTestSnapshotRefresher(t, repo)

	version, err := refresher.RefreshAvailableSnapshot(context.Background())
	require.NoError(t, err)
	assert.NotZero(t, version)
	assert.Equal(t, int64(1), repo.callCount())

	snapshot, err := configsync.LoadProviderSnapshot(context.Background(), rdb)
	require.NoError(t, err)
	assert.Equal(t, version, snapshot.Version)
	assert.Empty(t, snapshot.Entries)

	// 同步路径不经过调度器
	assert.Equal(t, int64(0), refresher.AutoRebuildCount())
}

// TestProviderSnapshotScheduleRefreshNilSafety 验证 nil 接收者不会 panic。
func TestProviderSnapshotScheduleRefreshNilSafety(t *testing.T) {
	var refresher *ProviderSnapshotRefresher
	assert.NotPanics(t, func() {
		refresher.ScheduleRefresh(context.Background())
	})
	assert.True(t, refresher.WaitIdle(10*time.Millisecond))
	assert.Equal(t, int64(0), refresher.AutoRebuildCount())

	// modelRepo / rdb 为 nil 时同步重建直接返回 0，不报错（保持既有行为）
	empty := NewProviderSnapshotRefresher(nil, nil)
	version, err := empty.RefreshAvailableSnapshot(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, int64(0), version)
}
