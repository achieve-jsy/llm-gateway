package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestApplyGroupNamesFromSnapshot 覆盖 R2 的胶水层：provider 快照（与 provider 条目同源）
// 携带的启用组名必须回流到组名缓存，使运行期新建/停用的组名无需等下一个控制面轮询
// （最长 30s，期间合法组名会被判成直连模型，症状同 2026-10-16 线上事故）。
//
// 同时固化两个安全边界：nil 快照/空组名不覆盖已有缓存、不 panic——旧控制面不写
// group_names 字段（omitempty → nil）与「确实没有任何启用组」无法区分，用空列表覆盖
// 会让合法组名被误判为直连模型。
func TestApplyGroupNamesFromSnapshot(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()

	gc := cache.NewGroupNameCache("test-token")
	require.False(t, gc.IsInitialized(), "前置条件：新缓存未就绪")

	// 快照缺失（nil）与缓存缺失都必须安全，且不得把缓存标成就绪
	require.False(t, applyGroupNamesFromSnapshot(nil, gc))
	require.False(t, applyGroupNamesFromSnapshot(nil, nil))
	require.False(t, applyGroupNamesFromSnapshot(&configsync.ProviderSnapshot{Version: 1}, gc))
	require.False(t, gc.IsInitialized(), "空 group_names 不得让缓存变成就绪")

	// 快照携带组名 → 回流成功（来源标记为 redis-snapshot，供 /ready 观测）
	snapshot := &configsync.ProviderSnapshot{
		Version:    7,
		UpdatedAt:  time.Date(2026, 10, 16, 14, 33, 4, 0, time.UTC),
		GroupNames: []string{"gpt-5.6-terra", "gpt-5.6-luna"},
	}
	require.True(t, applyGroupNamesFromSnapshot(snapshot, gc))
	require.True(t, gc.IsInitialized())
	require.True(t, gc.IsGroupName("gpt-5.6-terra"))
	require.Equal(t, cache.GroupNameCacheSourceRedisSnapshot, gc.Source())
	require.Equal(t, 2, gc.Len())

	// 旧控制面（无 group_names）绝不覆盖已有缓存
	require.False(t, applyGroupNamesFromSnapshot(&configsync.ProviderSnapshot{Version: 8}, gc))
	require.True(t, gc.IsGroupName("gpt-5.6-terra"), "空 group_names 不得清空已有组名")
	require.Equal(t, 2, gc.Len())
	require.True(t, gc.IsInitialized())

	// 经 Redis 的真实往返：控制面写入快照 → 网关读回 → 回流（覆盖 preheat 与热更新两条路径）
	require.NoError(t, configsync.SaveProviderSnapshot(ctx, rdb, snapshot))
	loaded, err := configsync.LoadProviderSnapshot(ctx, rdb)
	require.NoError(t, err)
	require.Equal(t, snapshot.GroupNames, loaded.GroupNames)

	fresh := cache.NewGroupNameCache("test-token")
	require.True(t, applyGroupNamesFromSnapshot(loaded, fresh))
	require.True(t, fresh.IsGroupName("gpt-5.6-luna"))
	require.Equal(t, 2, fresh.Len())
}
