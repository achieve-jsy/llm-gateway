package infra

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/core"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCacheWithRedis(t *testing.T) (*Cache, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
	})
	return NewCache(rdb), mr, rdb
}

func TestCache_GetSetModelList_RoundTrip(t *testing.T) {
	cache, _, _ := newCacheWithRedis(t)
	ctx := context.Background()

	models := []core.Model{
		{ID: "gpt-3.5", Object: "model", Created: 1, OwnedBy: "openai"},
		{ID: "gpt-4", Object: "model", Created: 2, OwnedBy: "openai"},
	}
	require.NoError(t, cache.SetModelList(ctx, models))

	got, version, err := cache.GetModelListWithVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, models, got)
	assert.Equal(t, int64(0), version)
}

func TestCache_GetModelList_Miss(t *testing.T) {
	cache, _, _ := newCacheWithRedis(t)
	got, version, err := cache.GetModelListWithVersion(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, int64(0), version)
}

func TestCache_GetModelList_UnmarshalError(t *testing.T) {
	cache, _, rdb := newCacheWithRedis(t)
	require.NoError(t, rdb.Set(context.Background(), "gateway:models:list", "not-json", 0).Err())

	_, _, err := cache.GetModelListWithVersion(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestCache_GetModelList_RedisError(t *testing.T) {
	cache, mr, _ := newCacheWithRedis(t)
	mr.Close() // 触发连接错误
	_, _, err := cache.GetModelListWithVersion(context.Background())
	require.Error(t, err)
}

func TestCache_GetSetModelList_WithVersion(t *testing.T) {
	cache, _, _ := newCacheWithRedis(t)
	ctx := context.Background()

	models := []core.Model{{ID: "gpt-4", Object: "model", Created: 2, OwnedBy: "openai"}}
	require.NoError(t, cache.SetModelListWithVersion(ctx, models, 99))

	got, version, err := cache.GetModelListWithVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, models, got)
	assert.Equal(t, int64(99), version)
}

func TestCache_IsAPIKeyValid(t *testing.T) {
	cache, _, rdb := newCacheWithRedis(t)
	ctx := context.Background()
	require.NoError(t, rdb.SAdd(ctx, "gateway:apikeys", "sk-good").Err())

	ok, err := cache.IsAPIKeyValid(ctx, "sk-good")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = cache.IsAPIKeyValid(ctx, "sk-bad")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCache_IsModelAvailable_DefaultOpen(t *testing.T) {
	cache, _, _ := newCacheWithRedis(t)
	// 未设置 key 时默认 fail-open
	ok, err := cache.IsModelAvailable(context.Background(), "gpt-4")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestCache_IsModelAvailable_ExplicitFlags(t *testing.T) {
	cache, _, _ := newCacheWithRedis(t)
	ctx := context.Background()

	require.NoError(t, cache.SetModelAvailable(ctx, "gpt-4", true))
	ok, err := cache.IsModelAvailable(ctx, "gpt-4")
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, cache.SetModelAvailable(ctx, "gpt-4", false))
	ok, err = cache.IsModelAvailable(ctx, "gpt-4")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCache_IsModelAvailable_RedisDownFailOpen(t *testing.T) {
	cache, mr, _ := newCacheWithRedis(t)
	mr.Close()
	ok, err := cache.IsModelAvailable(context.Background(), "x")
	require.NoError(t, err) // 连接错误时 fail-open
	assert.True(t, ok)
}
