package binding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer 创建一个模拟控制面的 httptest.Server。
// handler 可以自定义响应行为（计数、延迟等）。
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "test-token")
	return srv, c
}

// ─── Cache 基本读写 ──────────────────────────────────────────────────────────

func TestGetKeyGroups_CacheHit_NoHTTP(t *testing.T) {
	var callCount atomic.Int32
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		groups := []BoundGroup{{ID: 1, Name: "gpt-group", Enabled: true}}
		json.NewEncoder(w).Encode(groups)
	})

	ctx := context.Background()

	// 第一次：缓存未命中，应触发一次 HTTP
	groups1, err := client.GetKeyGroups(ctx, "key-001")
	require.NoError(t, err)
	assert.Len(t, groups1, 1)
	assert.Equal(t, int32(1), callCount.Load(), "第一次应发起 HTTP 请求")

	// 第二次：缓存命中，不应再触发 HTTP
	groups2, err := client.GetKeyGroups(ctx, "key-001")
	require.NoError(t, err)
	assert.Equal(t, groups1, groups2)
	assert.Equal(t, int32(1), callCount.Load(), "缓存命中，不应再发 HTTP")
}

func TestGetKeyGroups_CacheTTL_Expiry(t *testing.T) {
	var callCount atomic.Int32
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode([]BoundGroup{{ID: 1, Name: "g1", Enabled: true}})
	})

	// 注入一个已过期的缓存条目
	client.cache.m.Store("key-ttl", &keyGroupEntry{
		groups: []BoundGroup{{ID: 99, Name: "stale", Enabled: true}},
		expiry: time.Now().Add(-1 * time.Second), // 已过期
	})

	ctx := context.Background()
	groups, err := client.GetKeyGroups(ctx, "key-ttl")
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load(), "过期缓存应触发 HTTP 刷新")
	assert.Equal(t, "g1", groups[0].Name, "应返回最新数据而非过期数据")
}

func TestGetKeyGroups_Invalidate_BypassCache(t *testing.T) {
	var callCount atomic.Int32
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode([]BoundGroup{{ID: 1, Name: "new-group", Enabled: true}})
	})

	ctx := context.Background()

	// 预热缓存
	_, err := client.GetKeyGroups(ctx, "key-inv")
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	// 主动 Invalidate（模拟 Key 禁用事件）
	client.Cache().Invalidate("key-inv")

	// 再次请求，应触发 HTTP
	_, err = client.GetKeyGroups(ctx, "key-inv")
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load(), "Invalidate 后应重新请求 HTTP")
}

// ─── Singleflight 防缓存穿透 ──────────────────────────────────────────────

func TestGetKeyGroups_Singleflight_ConcurrentCallsMerged(t *testing.T) {
	var callCount atomic.Int32
	// 模拟控制面有 10ms 延迟
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(10 * time.Millisecond)
		json.NewEncoder(w).Encode([]BoundGroup{{ID: 1, Name: "merged-group", Enabled: true}})
	})

	ctx := context.Background()
	const goroutines = 50
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	results := make([][]BoundGroup, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = client.GetKeyGroups(ctx, "key-sf")
		}(i)
	}
	wg.Wait()

	// 所有 goroutine 应成功
	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d should not error", i)
	}
	// singleflight 合并：50 个并发只触发 1 次（或极少次）HTTP
	assert.LessOrEqual(t, callCount.Load(), int32(3),
		"50 并发应合并为极少次 HTTP 调用，实际: %d", callCount.Load())
	// 所有结果一致
	for i, r := range results {
		assert.Equal(t, "merged-group", r[0].Name, "goroutine %d 结果不一致", i)
	}
}

// ─── HTTP 错误处理 ────────────────────────────────────────────────────────

func TestGetKeyGroups_NotFound_ReturnsNil(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	groups, err := client.GetKeyGroups(context.Background(), "key-missing")
	require.NoError(t, err)
	assert.Nil(t, groups, "404 应返回 nil 而非 error")
}

func TestGetKeyGroups_ServerError_ReturnsError(t *testing.T) {
	_, client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := client.GetKeyGroups(context.Background(), "key-err")
	assert.Error(t, err, "5xx 应返回 error")
}

// ─── ContainsGroup ────────────────────────────────────────────────────────

func TestContainsGroup(t *testing.T) {
	groups := []BoundGroup{
		{ID: 1, Name: "gpt-4", Enabled: true},
		{ID: 2, Name: "claude", Enabled: false},
	}
	assert.True(t, ContainsGroup(groups, "gpt-4"), "启用的组应匹配")
	assert.False(t, ContainsGroup(groups, "claude"), "禁用的组不应匹配")
	assert.False(t, ContainsGroup(groups, "llama"), "不存在的组不应匹配")
}

// ─── KeyGroupCache 单元测试 ────────────────────────────────────────────────

func TestKeyGroupCache_SetGet(t *testing.T) {
	c := &KeyGroupCache{}
	groups := []BoundGroup{{ID: 1, Name: "g1", Enabled: true}}
	c.Set("k1", groups)

	got, ok := c.Get("k1")
	require.True(t, ok)
	assert.Equal(t, groups, got)
}

func TestKeyGroupCache_Invalidate(t *testing.T) {
	c := &KeyGroupCache{}
	c.Set("k2", []BoundGroup{{ID: 2, Name: "g2", Enabled: true}})
	c.Invalidate("k2")
	_, ok := c.Get("k2")
	assert.False(t, ok, "Invalidate 后应无法命中")
}

func TestKeyGroupCache_Expiry(t *testing.T) {
	c := &KeyGroupCache{}
	// 手动写入一个已过期的条目
	c.m.Store("k3", &keyGroupEntry{
		groups: []BoundGroup{{ID: 3, Name: "g3", Enabled: true}},
		expiry: time.Now().Add(-1 * time.Second),
	})
	_, ok := c.Get("k3")
	assert.False(t, ok, "过期条目应返回 false")
}
