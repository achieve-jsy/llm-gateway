package quota

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/core/quota"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupMiddlewareTest 创建测试用的 middleware + miniredis + snapshot holder
func setupMiddlewareTest(t *testing.T) (*miniredis.Miniredis, *QuotaMiddleware, *SnapshotHolder) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := NewSnapshotHolder()
	store := NewRedisStore(rdb)
	mw := NewMiddleware(holder, store)
	return mr, mw, holder
}

// newTestRouter 创建带配额中间件的测试路由
func newTestRouter(mw *QuotaMiddleware, identity *gwmodel.UserIdentity, modelName string) *gin.Engine {
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		// 模拟 auth middleware 设置的 context
		if identity != nil {
			c.Set("identity", identity)
		}
		if modelName != "" {
			c.Set("request_model", modelName)
		}
	}, mw.Handler(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func doRequest(r *gin.Engine) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Test 1: 系统探针 key 豁免
func TestMiddleware_SystemProbeExempt(t *testing.T) {
	_, mw, _ := setupMiddlewareTest(t)
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("key_name", "__system_probe")
		c.Set("identity", &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"})
	}, mw.Handler(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	w := doRequest(r)
	if w.Code != http.StatusOK {
		t.Errorf("system probe should pass, got %d", w.Code)
	}
}

// Test 2: 用户 RPM 限制
func TestMiddleware_UserRPM(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 3, MaxConcurrency: 0},
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "")

	// 前 3 次应通过
	for i := 0; i < 3; i++ {
		w := doRequest(r)
		if w.Code != http.StatusOK {
			t.Errorf("request %d should pass, got %d", i+1, w.Code)
		}
	}
	// 第 4 次应被拒
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("request 4 should be rejected, got %d", w.Code)
	}
}

// Test 3: Service Key 独立计数
func TestMiddleware_ServiceKeyIndependent(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultServiceKey: quota.LayerLimits{RPM: 2, MaxConcurrency: 0},
		ServiceKeys:       map[string]quota.LayerLimits{},
		Groups:            map[string]quota.LayerLimits{},
		Models:            map[string]quota.LayerLimits{},
	})

	sk1 := &gwmodel.UserIdentity{UserID: "u1", KeyID: "sk-1", KeyType: "service"}
	sk2 := &gwmodel.UserIdentity{UserID: "u1", KeyID: "sk-2", KeyType: "service"}

	r1 := newTestRouter(mw, sk1, "")
	r2 := newTestRouter(mw, sk2, "")

	// sk-1 用满 2 次
	doRequest(r1)
	doRequest(r1)
	w := doRequest(r1)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("sk-1 should be rejected after 2, got %d", w.Code)
	}

	// sk-2 仍可通过（独立计数）
	w = doRequest(r2)
	if w.Code != http.StatusOK {
		t.Errorf("sk-2 should still pass, got %d", w.Code)
	}
}

// Test 4: 组层配额
func TestMiddleware_GroupLayer(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 100, MaxConcurrency: 0},
		Users:       map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{
			"gpt-4": {RPM: 2, MaxConcurrency: 0},
		},
		Models: map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "gpt-4")

	doRequest(r)
	doRequest(r)
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("group limit should reject after 2, got %d", w.Code)
	}
	// 验证响应体包含 layer=group
	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	errObj, _ := body["error"].(map[string]interface{})
	if errObj["layer"] != "group" {
		t.Errorf("expected layer=group, got %v", errObj["layer"])
	}
}

// Test 5: 并发控制
func TestMiddleware_Concurrency(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 0, MaxConcurrency: 1},
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}

	// 使用阻塞 handler 模拟并发占用
	r := gin.New()
	blockCh := make(chan struct{})
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", identity)
	}, mw.Handler(), func(c *gin.Context) {
		<-blockCh // 阻塞直到释放
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 第一个请求占用并发槽
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
	}()
	time.Sleep(50 * time.Millisecond) // 等待第一个请求进入 handler

	// 第二个请求应被并发限制拒绝
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("concurrent request should be rejected, got %d", w.Code)
	}

	close(blockCh) // 释放第一个请求
}

// Test 6: 豁免跳过全部检查
func TestMiddleware_Exemption(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 1, MaxConcurrency: 1},
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
		Exemptions: []quota.QuotaExemption{
			{
				SubjectType: "user",
				SubjectID:   "u1",
				Reason:      "emergency",
				ExpiresAt:   time.Now().Add(1 * time.Hour),
			},
		},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "")

	// 即使超过 RPM 限制也应通过（豁免）
	for i := 0; i < 5; i++ {
		w := doRequest(r)
		if w.Code != http.StatusOK {
			t.Errorf("exempt user request %d should pass, got %d", i+1, w.Code)
		}
	}
}

// Test 7: 过期豁免不生效
func TestMiddleware_ExpiredExemption(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 1, MaxConcurrency: 0},
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
		Exemptions: []quota.QuotaExemption{
			{
				SubjectType: "user",
				SubjectID:   "u1",
				Reason:      "expired",
				ExpiresAt:   time.Now().Add(-1 * time.Hour), // 已过期
			},
		},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "")

	doRequest(r) // 第一次通过
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expired exemption should not bypass, got %d", w.Code)
	}
}

// Test 8: nil snapshot fallback
func TestMiddleware_NilSnapshotFallback(t *testing.T) {
	_, mw, _ := setupMiddlewareTest(t)
	// holder 不设 snapshot，使用 fallback 默认值 (60 RPM)
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "")

	// fallback 60 RPM，前几次应通过
	for i := 0; i < 5; i++ {
		w := doRequest(r)
		if w.Code != http.StatusOK {
			t.Errorf("nil snapshot fallback request %d should pass, got %d", i+1, w.Code)
		}
	}
}

// Test 9: 覆盖配额（用户级覆盖默认）
func TestMiddleware_UserOverride(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 100, MaxConcurrency: 0},
		Users: map[string]quota.LayerLimits{
			"u1": {RPM: 1, MaxConcurrency: 0},
		},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "")

	doRequest(r)
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("user override should limit to 1 RPM, got %d", w.Code)
	}
}

// Test 10: Redis fail-open（Redis 故障时放行）
func TestMiddleware_RedisFailOpen(t *testing.T) {
	mr, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 1, MaxConcurrency: 0},
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "")

	// 先用满配额
	doRequest(r)

	// 关闭 Redis 模拟故障
	mr.Close()

	// Redis 故障时应 fail-open（放行）
	w := doRequest(r)
	if w.Code != http.StatusOK {
		t.Errorf("redis fail-open should pass, got %d", w.Code)
	}
}

// Test 11: 429 响应 headers
func TestMiddleware_429Headers(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUser: quota.LayerLimits{RPM: 1, MaxConcurrency: 0},
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	r := newTestRouter(mw, identity, "")

	doRequest(r)
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	// 检查必要的 headers
	if w.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
	if w.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("missing X-RateLimit-Limit header")
	}
	if w.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("expected X-RateLimit-Remaining=0, got %s", w.Header().Get("X-RateLimit-Remaining"))
	}
	if w.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("missing X-RateLimit-Reset header")
	}
}
