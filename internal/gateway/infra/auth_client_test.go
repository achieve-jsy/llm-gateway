package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAuthTestServer 创建一个模拟 auth 控制面的测试服务器，返回指定的 VerifyKeyResp。
func newAuthTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *AuthClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := NewAuthClient(srv.URL, "internal-token")
	return srv, client
}

func activeRespJSON(keyID string) []byte {
	resp := VerifyKeyResp{Code: 0, Message: "ok"}
	resp.Data.Status = "active"
	resp.Data.KeyID = keyID
	resp.Data.KeyName = "test-key"
	resp.Data.User.UserID = "user-1"
	b, _ := json.Marshal(resp)
	return b
}

// ─── 缓存命中路径 ─────────────────────────────────────────────────────────

func TestAuthClient_VerifyKey_CacheHit_NoHTTP(t *testing.T) {
	var callCount atomic.Int32
	_, client := newAuthTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Write(activeRespJSON("k1"))
	})

	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	kc := NewKeyCache(rdb)
	t.Cleanup(kc.Close)
	client.SetKeyCache(kc)

	ctx := context.Background()

	// 第一次：未命中，走 HTTP
	resp1, err := client.VerifyKey(ctx, "sk-testkey-0001")
	require.NoError(t, err)
	assert.Equal(t, "active", resp1.Data.Status)
	assert.Equal(t, int32(1), callCount.Load(), "第一次应发 HTTP")

	// 第二次：命中缓存，不走 HTTP
	resp2, err := client.VerifyKey(ctx, "sk-testkey-0001")
	require.NoError(t, err)
	assert.Equal(t, resp1.Data.KeyID, resp2.Data.KeyID)
	assert.Equal(t, int32(1), callCount.Load(), "第二次应命中缓存，不发 HTTP")
}

// ─── Singleflight 防并发穿透 ─────────────────────────────────────────────

func TestAuthClient_VerifyKey_Singleflight_ConcurrentCallsMerged(t *testing.T) {
	var callCount atomic.Int32
	// 模拟控制面有 20ms 延迟，让并发 goroutine 堆积
	_, client := newAuthTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Write(activeRespJSON("k-sf"))
	})
	// 不设置 KeyCache，强制所有请求都走 HTTP 路径（验证 singleflight 本身）

	ctx := context.Background()
	const goroutines = 40
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	results := make([]*VerifyKeyResp, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = client.VerifyKey(ctx, "sk-concurrent-key")
		}(i)
	}
	wg.Wait()

	// 所有请求应成功
	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d should not error", i)
	}
	// singleflight 合并：40 并发只应触发 1 次（或极少次）实际 HTTP
	assert.LessOrEqual(t, callCount.Load(), int32(3),
		"40 并发应合并为极少次 HTTP 调用，实际: %d", callCount.Load())
	// 所有结果一致
	for i, r := range results {
		if r != nil {
			assert.Equal(t, "active", r.Data.Status, "goroutine %d 结果应为 active", i)
		}
	}
}

// ─── 鉴权错误处理 ─────────────────────────────────────────────────────────

func TestAuthClient_VerifyKey_ServiceUnavailable(t *testing.T) {
	_, client := newAuthTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := client.VerifyKey(context.Background(), "sk-bad-key-0001")
	assert.Error(t, err, "5xx 应返回错误")
	assert.Contains(t, err.Error(), "auth service error 500")
}

func TestAuthClient_VerifyKey_DisabledKey_NotCached(t *testing.T) {
	// disabled 状态的 key 不应被写入缓存
	var callCount atomic.Int32
	_, client := newAuthTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		resp := VerifyKeyResp{Code: 0}
		resp.Data.Status = "disabled"
		resp.Data.KeyID = "k-disabled"
		b, _ := json.Marshal(resp)
		w.Write(b)
	})

	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	kc := NewKeyCache(rdb)
	t.Cleanup(kc.Close)
	client.SetKeyCache(kc)

	ctx := context.Background()

	// 连续两次请求 disabled key
	client.VerifyKey(ctx, "sk-disabled-key-001")
	client.VerifyKey(ctx, "sk-disabled-key-001")

	// disabled key 不应被缓存，每次都应重新请求
	assert.Equal(t, int32(2), callCount.Load(), "disabled key 不应被缓存，每次应发 HTTP")
}

// ─── 连接池验证（不使用裸 http.Client） ──────────────────────────────────

func TestAuthClient_UsesConnectionPool(t *testing.T) {
	// 验证 NewAuthClient 使用了经过配置的 Transport（MaxIdleConnsPerHost=100）
	// 而非裸 http.Client（默认 MaxIdleConnsPerHost=2）
	client := NewAuthClient("http://localhost:8081", "token")
	transport, ok := client.client.Transport.(*http.Transport)
	require.True(t, ok, "Transport 应为 *http.Transport")
	assert.Equal(t, 100, transport.MaxIdleConnsPerHost,
		"MaxIdleConnsPerHost 应为 100，而非默认的 2")
}
