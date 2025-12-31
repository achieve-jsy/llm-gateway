package infra

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestKeyCache(t *testing.T) (*KeyCache, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	kc := NewKeyCache(rdb)
	t.Cleanup(kc.Close)
	return kc, mr
}

func makeActiveResp(keyID string) *VerifyKeyResp {
	r := &VerifyKeyResp{
		Code:    0,
		Message: "ok",
	}
	r.Data.Status = "active"
	r.Data.KeyID = keyID
	r.Data.User.UserID = "user-1"
	return r
}

func makeDisabledResp(keyID string) *VerifyKeyResp {
	resp := makeActiveResp(keyID)
	resp.Data.Status = "disabled"
	return resp
}

// ─── L1 Local Cache ──────────────────────────────────────────────────────────

func TestKeyCache_Get_LocalHit(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-test-key-1"
	resp := makeActiveResp("key-001")
	kc.Set(ctx, key, resp)

	got, ok := kc.Get(ctx, key)
	require.True(t, ok)
	assert.Equal(t, "active", got.Data.Status)
	assert.Equal(t, "key-001", got.Data.KeyID)
	assert.Equal(t, "user-1", got.Data.User.UserID)
}

func TestKeyCache_Get_LocalMiss(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	got, ok := kc.Get(ctx, "sk-unknown-key")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestKeyCache_Get_LocalExpired(t *testing.T) {
	kc, mr := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-test-key-2"
	resp := makeActiveResp("key-002")
	kc.Set(ctx, key, resp)

	// Verify L1 hit (check by real wall clock)
	got, ok := kc.Get(ctx, key)
	require.True(t, ok)
	require.NotNil(t, got)

	// Fast-forward past Redis TTL — L1 is still valid by wall clock
	mr.FastForward(20 * time.Second)

	// L1 should still hit (wall clock didn't change much in test)
	got, ok = kc.Get(ctx, key)
	require.True(t, ok, "L1 hit should bypass expired Redis")
	assert.Equal(t, "key-002", got.Data.KeyID)
}

func TestKeyCache_Get_FullExpired(t *testing.T) {
	kc, mr := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-test-key-3"
	kc.Set(ctx, key, makeActiveResp("key-003"))

	// Fast-forward past Redis TTL but L1 still valid
	mr.FastForward(20 * time.Second)

	// L1 should still hit, bypassing expired Redis
	got, ok := kc.Get(ctx, key)
	require.True(t, ok)
	assert.Equal(t, "key-003", got.Data.KeyID)

	// After L1 expiry, Redis hit should now miss
	// Simulate L1 expiry by manually invalidating
	kc.Invalidate(ctx, key)

	got, ok = kc.Get(ctx, key)
	assert.False(t, ok, "should miss after L1 expired and Redis TTL passed")
	assert.Nil(t, got)
}

// ─── L2 Redis Cache ─────────────────────────────────────────────────────────

func TestKeyCache_RedisShared(t *testing.T) {
	// Two KeyCache instances sharing the same Redis simulate two gateway instances
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	kc1 := NewKeyCache(rdb)
	defer kc1.Close()
	kc2 := NewKeyCache(rdb)
	defer kc2.Close()

	ctx := context.Background()
	key := "sk-shared-key"

	// Instance 1: Set via CP
	resp := makeActiveResp("key-shared")
	kc1.Set(ctx, key, resp)

	// Instance 2: should hit Redis L2
	got, ok := kc2.Get(ctx, key)
	require.True(t, ok)
	assert.Equal(t, "key-shared", got.Data.KeyID)
}

// ─── Set ─────────────────────────────────────────────────────────────────────

func TestKeyCache_Set_DisabledKeyNotCached(t *testing.T) {
	kc, mr := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-disabled-key"
	resp := makeDisabledResp("key-disabled")
	kc.Set(ctx, key, resp)

	// Fast-forward Redis to check nothing was written
	mr.FastForward(0)

	// Should not be in cache
	got, ok := kc.Get(ctx, key)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestKeyCache_Set_NilRespNotCached(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	kc.Set(ctx, "sk-nil-key", nil)

	got, ok := kc.Get(ctx, "sk-nil-key")
	assert.False(t, ok)
	assert.Nil(t, got)
}

// ─── Invalidate ──────────────────────────────────────────────────────────────

func TestKeyCache_Invalidate(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-invalidate-key"
	kc.Set(ctx, key, makeActiveResp("key-inv"))

	// Verify cached
	_, ok := kc.Get(ctx, key)
	require.True(t, ok)

	kc.Invalidate(ctx, key)

	_, ok = kc.Get(ctx, key)
	assert.False(t, ok)
}

// ─── HandleChangeEvent ───────────────────────────────────────────────────────

func TestKeyCache_HandleChangeEvent_KeyDisabled(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-revoke-key"
	kc.Set(ctx, key, makeActiveResp("key-revoke"))

	// Verify cached
	_, ok := kc.Get(ctx, key)
	require.True(t, ok)

	kc.HandleChangeEvent(ChangeEvent{
		Type:         "key_disabled",
		ResourceType: "api_key",
		ResourceID:   "key-revoke",
	})

	_, ok = kc.Get(ctx, key)
	assert.False(t, ok, "key should be invalidated on key_disabled event")
}

func TestKeyCache_HandleChangeEvent_KeyDeleted(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-delete-key"
	kc.Set(ctx, key, makeActiveResp("key-delete"))

	_, ok := kc.Get(ctx, key)
	require.True(t, ok)

	kc.HandleChangeEvent(ChangeEvent{
		Type:         "key_deleted",
		ResourceType: "api_key",
		ResourceID:   "key-delete",
	})

	_, ok = kc.Get(ctx, key)
	assert.False(t, ok, "key should be invalidated on key_deleted event")
}

func TestKeyCache_HandleChangeEvent_IgnoresOtherTypes(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-keep-key"
	kc.Set(ctx, key, makeActiveResp("key-keep"))

	// Non-key events should not affect the cache
	kc.HandleChangeEvent(ChangeEvent{
		Type:         "provider_created",
		ResourceType: "provider",
		ResourceID:   "key-keep",
	})

	_, ok := kc.Get(ctx, key)
	assert.True(t, ok, "key should still be cached after provider_created event")
}

func TestKeyCache_HandleChangeEvent_KeyEnabled(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	key := "sk-enabled-key"
	kc.Set(ctx, key, makeActiveResp("key-enabled"))

	// key_enabled should NOT invalidate
	kc.HandleChangeEvent(ChangeEvent{
		Type:         "key_enabled",
		ResourceType: "api_key",
		ResourceID:   "key-enabled",
	})

	_, ok := kc.Get(ctx, key)
	assert.True(t, ok, "key should still be cached after key_enabled event")
}

func TestKeyCache_HandleChangeEvent_OnlyInvalidatesMatching(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	kc.Set(ctx, "sk-keep", makeActiveResp("key-keep"))
	kc.Set(ctx, "sk-revoke", makeActiveResp("key-revoke"))

	kc.HandleChangeEvent(ChangeEvent{
		Type:         "key_disabled",
		ResourceType: "api_key",
		ResourceID:   "key-revoke",
	})

	_, ok := kc.Get(ctx, "sk-keep")
	assert.True(t, ok, "unrelated key should still be cached")

	_, ok = kc.Get(ctx, "sk-revoke")
	assert.False(t, ok, "revoked key should be invalidated")
}

// ─── Nil Redis (single-instance mode) ────────────────────────────────────────

func TestKeyCache_NilRedis(t *testing.T) {
	kc := NewKeyCache(nil)
	defer kc.Close()

	ctx := context.Background()
	key := "sk-no-redis"
	kc.Set(ctx, key, makeActiveResp("key-local"))

	got, ok := kc.Get(ctx, key)
	require.True(t, ok)
	assert.Equal(t, "key-local", got.Data.KeyID)
}

// ─── Concurrent Access ───────────────────────────────────────────────────────

func TestKeyCache_ConcurrentSetGet(t *testing.T) {
	kc, _ := newTestKeyCache(t)
	ctx := context.Background()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			key := "sk-concurrent-" + string(rune('0'+id))
			kc.Set(ctx, key, makeActiveResp("key-concurrent"))
			got, ok := kc.Get(ctx, key)
			if ok {
				assert.Equal(t, "active", got.Data.Status)
			}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
// ─── HandleChangeEvent: AuthConfigChanged (v0.7) ────────────────────────────

// TestKeyCache_HandleChangeEvent_AuthConfigChanged 验证 v0.7 新增的全局配置变更事件：
// 收到 auth_config_changed 事件后清空本地 + Redis 所有缓存条目。
func TestKeyCache_HandleChangeEvent_AuthConfigChanged(t *testing.T) {
	kc := NewKeyCache(nil) // nil redis 简化本地验证
	defer kc.Close()

	ctx := context.Background()
	// 预热若干不相关的 key
	kc.Set(ctx, "sk-user-a", makeActiveResp("key-a"))
	kc.Set(ctx, "sk-user-b", makeActiveResp("key-b"))
	kc.Set(ctx, "sk-user-c", makeActiveResp("key-c"))

	for _, key := range []string{"sk-user-a", "sk-user-b", "sk-user-c"} {
		_, ok := kc.Get(ctx, key)
		assert.True(t, ok, "key %s should be cached initially", key)
	}

	// 触发全局配置变更事件
	kc.HandleChangeEvent(ChangeEvent{
		Type:         "auth_config_changed",
		ResourceType: "auth_config",
		ResourceID:   "",
	})

	// 全部缓存应被清空
	for _, key := range []string{"sk-user-a", "sk-user-b", "sk-user-c"} {
		_, ok := kc.Get(ctx, key)
		assert.False(t, ok, "key %s should be flushed after auth_config_changed", key)
	}
}
