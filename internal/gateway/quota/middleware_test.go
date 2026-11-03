package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
		DefaultUserRPM: 3, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
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
		DefaultServiceKeyRPM: 2, DefaultServiceKeyMaxConcurrency: 0,
		ServiceKeys: map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
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
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 0,
		Users: map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{
			"gpt-4": {RPM: 2, MaxConcurrency: 0},
		},
		Models: map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}
	handlerCalls := 0
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", identity)
		c.Set("request_model", "gpt-4")
	}, mw.Handler(), func(c *gin.Context) {
		handlerCalls++
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	doRequest(r)
	doRequest(r)
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("group limit should reject after 2, got %d", w.Code)
	}
	if handlerCalls != 2 {
		t.Errorf("downstream handler should be called twice, got %d", handlerCalls)
	}

	// 验证响应体是单个合法 JSON 文档且包含 layer=group。
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body should be one valid JSON document: %v; body=%q", err, w.Body.String())
	}
	errObj, _ := body["error"].(map[string]interface{})
	details, _ := errObj["details"].(map[string]interface{})
	if details["layer"] != "group" {
		t.Errorf("expected layer=group, got %v", details["layer"])
	}
}

// Test 5: 并发控制
func TestMiddleware_Concurrency(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 1,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
	})
	identity := &gwmodel.UserIdentity{UserID: "u1", KeyType: "personal"}

	// 第一个请求阻塞以占用并发槽；如果被拒的第二个请求错误进入 handler，
	// 它会直接返回，让测试快速报告调用次数/响应体错误，而不是永久死锁。
	r := gin.New()
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	var handlerCalls atomic.Int32
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", identity)
	}, mw.Handler(), func(c *gin.Context) {
		if handlerCalls.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 第一个请求占用并发槽
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		firstDone <- w
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first request did not enter downstream handler")
	}
	firstReleased := false
	defer func() {
		if !firstReleased {
			close(releaseFirst)
		}
	}()

	// 第二个请求应被并发限制拒绝
	w := doRequest(r)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("concurrent request should be rejected, got %d", w.Code)
	}
	if got := handlerCalls.Load(); got != 1 {
		t.Errorf("rejected request should not enter downstream handler, got %d calls", got)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body should be one valid JSON document: %v; body=%q", err, w.Body.String())
	}

	close(releaseFirst)
	firstReleased = true
	select {
	case firstResponse := <-firstDone:
		if firstResponse.Code != http.StatusOK {
			t.Errorf("first request should complete successfully, got %d", firstResponse.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("first request did not finish after release")
	}
}

// Test 6: 豁免跳过全部检查
func TestMiddleware_Exemption(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 1,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
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
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
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
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 0,
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
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
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
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
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

// ──────────────────────────────────────────────────────────────────────────────
// ModelQuotaGate Tests
// ──────────────────────────────────────────────────────────────────────────────

// setupModelGateTest creates test fixtures for ModelQuotaGate
func setupModelGateTest(t *testing.T) (*miniredis.Miniredis, *ModelQuotaGate, *SnapshotHolder) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := NewSnapshotHolder()
	store := NewRedisStore(rdb)
	gate := NewModelQuotaGate(holder, store)
	return mr, gate, holder
}

// Test 12: 模型层配额 - 未达上限放行
func TestModelQuotaGate_BeforeUpstream_UnderLimit(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{
			"model-1": {RPM: 10, MaxConcurrency: 5},
		},
	})

	r := gin.New()
	r.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 请求 3 次，都应通过
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// 手动调用 BeforeUpstream
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		err := gate.BeforeUpstream(c, "user", "u1", "model-1")
		if err != nil {
			t.Errorf("request %d should pass, got error: %v", i+1, err)
		}
	}
}

// Test 13: 模型层配额 - 达到上限返回 429
func TestModelQuotaGate_BeforeUpstream_AtLimit(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{
			"model-1": {RPM: 2, MaxConcurrency: 0},
		},
	})

	// 前 2 次应通过
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		err := gate.BeforeUpstream(c, "user", "u1", "model-1")
		if err != nil {
			t.Errorf("request %d should pass, got error: %v", i+1, err)
		}
	}

	// 第 3 次应被拒绝
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	err := gate.BeforeUpstream(c, "user", "u1", "model-1")
	if err == nil {
		t.Error("request 3 should be rejected, got nil error")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}

	// 验证响应体包含 layer=model
	var body map[string]interface{}
	json.NewDecoder(strings.NewReader(w.Body.String())).Decode(&body)
	errObj, _ := body["error"].(map[string]interface{})
	details, _ := errObj["details"].(map[string]interface{})
	if details["layer"] != "model" {
		t.Errorf("expected layer=model, got %v", details["layer"])
	}
}

// Test 14: 模型层配额 - limit=0 表示不限制
func TestModelQuotaGate_BeforeUpstream_LimitZero(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{
			"model-1": {RPM: 0, MaxConcurrency: 0}, // 0 = 不限
		},
	})

	// 请求 100 次，都应通过
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		err := gate.BeforeUpstream(c, "user", "u1", "model-1")
		if err != nil {
			t.Errorf("request %d should pass (limit=0 means unlimited), got error: %v", i+1, err)
		}
	}
}

// Test 15: 模型层配额 - 多模型独立计数
func TestModelQuotaGate_BeforeUpstream_MultiModelIsolation(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{
			"model-1": {RPM: 1, MaxConcurrency: 0},
			"model-2": {RPM: 1, MaxConcurrency: 0},
		},
	})

	// model-1 用满配额
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	err := gate.BeforeUpstream(c, "user", "u1", "model-1")
	if err != nil {
		t.Errorf("model-1 request 1 should pass, got error: %v", err)
	}

	// model-1 第 2 次应被拒绝
	req = httptest.NewRequest(http.MethodPost, "/test", nil)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	err = gate.BeforeUpstream(c, "user", "u1", "model-1")
	if err == nil {
		t.Error("model-1 request 2 should be rejected")
	}

	// model-2 仍应通过（独立计数）
	req = httptest.NewRequest(http.MethodPost, "/test", nil)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	err = gate.BeforeUpstream(c, "user", "u1", "model-2")
	if err != nil {
		t.Errorf("model-2 request 1 should pass (independent from model-1), got error: %v", err)
	}
}

// Test 16: 模型层配额 - Redis 故障时 fail-open
func TestModelQuotaGate_BeforeUpstream_RedisFailOpen(t *testing.T) {
	mr, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{
			"model-1": {RPM: 1, MaxConcurrency: 0},
		},
	})

	// 先用满配额
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	err := gate.BeforeUpstream(c, "user", "u1", "model-1")
	if err != nil {
		t.Errorf("first request should pass, got error: %v", err)
	}

	// 关闭 Redis 模拟故障
	mr.Close()

	// Redis 故障时应 fail-open（放行）
	req = httptest.NewRequest(http.MethodPost, "/test", nil)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = req
	err = gate.BeforeUpstream(c, "user", "u1", "model-1")
	if err != nil {
		t.Errorf("redis fail-open should pass, got error: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 租约式并发计数：请求级幂等 + 失败路径不残留租约（reviewer 预审意见 2/3）
// ──────────────────────────────────────────────────────────────────────────────

// newGateTestContext 构造一个独立的 gin context（ModelQuotaGate 直接调用用）。
func newGateTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, w
}

// TestModelQuotaGate_RepeatedAcquire_SameRequestIsIdempotent failover/重试路径会在同一请求内
// 对同一模型 key 重复调用 BeforeUpstream，而释放只发生一次。要求：
//  1. 重复调用复用同一个租约 id，只占一个槽；
//  2. 令牌列表不重复登记（不会重复释放）；
//  3. 释放一次后槽位彻底归零。
func TestModelQuotaGate_RepeatedAcquire_SameRequestIsIdempotent(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{"m-uuid": {RPM: 0, MaxConcurrency: 1}},
	})
	concKey := ConcurrencyKey("user", "u-idem", "model", "m-uuid")

	c, _ := newGateTestContext()
	// 同一请求内 3 次 acquire（模拟首次 + 两次 failover 重试）
	for i := 0; i < 3; i++ {
		if err := gate.BeforeUpstream(c, "user", "u-idem", "m-uuid"); err != nil {
			t.Fatalf("acquire %d should pass (same request must be idempotent), got %v", i+1, err)
		}
	}

	// 只占一个槽（limit=1 且未被自身耗满）
	if v := concVal(t, gate.store.rdb, concKey); v != 1 {
		t.Fatalf("repeated acquire in one request must hold exactly 1 slot, got %d", v)
	}
	// 令牌只登记一次
	tokens, _ := c.Get(quotaReleaseTokensKey)
	list, _ := tokens.([]releaseToken)
	if len(list) != 1 {
		t.Fatalf("token list must contain exactly 1 entry, got %d (%+v)", len(list), list)
	}
	// 一次释放即彻底归零（键被删除）
	gate.store.ReleaseConcurrency(c.Request.Context(), concKey, list[0].leaseID)
	if v := concVal(t, gate.store.rdb, concKey); v != 0 {
		t.Fatalf("after single release the slot must be freed, got %d", v)
	}
	// 另一个请求仍能正常获取（说明没有把槽位耗在自我重复上）
	c2, _ := newGateTestContext()
	if err := gate.BeforeUpstream(c2, "user", "u-idem", "m-uuid"); err != nil {
		t.Fatalf("a fresh request should still acquire the free slot, got %v", err)
	}
}

// TestQuotaMiddleware_ConcurrencyGateFailure_NoLeaseResidue 并发闸失败（第 2 层）时，
// BeforeUpstream 必须已归还第 1 层已获取的租约，且不得登记任何令牌。
// 该契约保证了 budget.go:47-49 返回 err 时无需（也不得）再调 AfterUpstream。
func TestQuotaMiddleware_ConcurrencyGateFailure_NoLeaseResidue(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 1,
		// group 层 limit=1，先被外部占满 → 组层并发闸失败
		Groups: map[string]quota.LayerLimits{"gpt-4": {RPM: 0, MaxConcurrency: 1}},
		Users:  map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
	})
	subjectKey := ConcurrencyKey("user", "u-gf", "", "")
	groupKey := ConcurrencyKey("user", "u-gf", "group", "gpt-4")
	// 外部占用 group 层唯一槽位
	if _, err := mw.store.AcquireConcurrency(t.Context(), groupKey, 1); err != nil {
		t.Fatalf("pre-occupy group slot: %v", err)
	}

	c, w := newGateTestContext()
	c.Set("identity", &gwmodel.UserIdentity{UserID: "u-gf", KeyType: "personal"})
	c.Set("request_model", "gpt-4")

	err := mw.BeforeUpstream(c, "gpt-4")
	if err == nil {
		t.Fatal("group concurrency gate should reject")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	// 第 1 层（subject）的租约必须已归还
	if v := concVal(t, mw.store.rdb, subjectKey); v != 0 {
		t.Fatalf("subject-layer lease must be released on group gate failure, got %d", v)
	}
	// 不得登记待释放令牌（否则调用方若再调 AfterUpstream 会重复释放）
	if v, ok := c.Get(quotaReleaseTokensKey); ok {
		if list, _ := v.([]releaseToken); len(list) != 0 {
			t.Fatalf("no release token should be registered on failure path, got %+v", list)
		}
	}
}

// TestQuotaMiddleware_RPMGateFailure_NoLeaseResidue RPM 闸失败（第 1 层）时，
// 必须归还该层刚获取的并发租约，且不得登记令牌。
func TestQuotaMiddleware_RPMGateFailure_NoLeaseResidue(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		// subject 层 RPM=1：连续第二次请求即被 RPM 闸拒绝
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 5,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
	})
	subjectKey := ConcurrencyKey("user", "u-rf", "", "")

	// 第一次请求正常通过并释放（消耗掉 RPM 配额）
	c1, _ := newGateTestContext()
	c1.Set("identity", &gwmodel.UserIdentity{UserID: "u-rf", KeyType: "personal"})
	if err := mw.BeforeUpstream(c1, ""); err != nil {
		t.Fatalf("1st request should pass, got %v", err)
	}
	mw.AfterUpstream(c1)

	// 第二次请求：RPM 闸拒绝 → 刚获取的并发租约必须回滚
	c2, w2 := newGateTestContext()
	c2.Set("identity", &gwmodel.UserIdentity{UserID: "u-rf", KeyType: "personal"})
	err := mw.BeforeUpstream(c2, "")
	if err == nil {
		t.Fatal("2nd request should be rejected by RPM gate")
	}
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w2.Code)
	}
	if v := concVal(t, mw.store.rdb, subjectKey); v != 0 {
		t.Fatalf("concurrency lease must be rolled back on RPM failure, got %d", v)
	}
	if v, ok := c2.Get(quotaReleaseTokensKey); ok {
		if list, _ := v.([]releaseToken); len(list) != 0 {
			t.Fatalf("no release token should be registered on RPM failure path, got %+v", list)
		}
	}
}

// TestModelQuotaGate_ReusedLease_NotReleasedByRPMPath 复用型租约在 RPM 失败时不得被移除：
// 本次调用并未新占槽，该租约仍由请求（前序成功路径）持有，须留给正常释放路径。
// 同时验证：RPM 失败路径不会因为「复用」而白占新槽。
func TestModelQuotaGate_ReusedLease_NotReleasedByRPMPath(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{"m-uuid": {RPM: 1, MaxConcurrency: 5}},
	})
	concKey := ConcurrencyKey("user", "u-reuse-rpm", "model", "m-uuid")

	c, _ := newGateTestContext()
	// 首次：并发+模型 RPM 均通过，登记 1 个令牌
	if err := gate.BeforeUpstream(c, "user", "u-reuse-rpm", "m-uuid"); err != nil {
		t.Fatalf("first acquire should pass, got %v", err)
	}
	// 第二次（failover 重试）：复用租约，但模型 RPM=1 已满 → 拒绝
	err := gate.BeforeUpstream(c, "user", "u-reuse-rpm", "m-uuid")
	if err == nil {
		t.Fatal("second call should be rejected by model RPM gate")
	}
	// 复用的租约仍在（不能被 RPM 失败路径误删），且总槽位仍为 1
	if v := concVal(t, gate.store.rdb, concKey); v != 1 {
		t.Fatalf("reused lease must survive RPM failure, got %d active leases", v)
	}
	// 令牌列表仍只有 1 条（未重复登记）
	tokens, _ := c.Get(quotaReleaseTokensKey)
	list, _ := tokens.([]releaseToken)
	if len(list) != 1 {
		t.Fatalf("token list must stay at 1 entry after reused RPM failure, got %d", len(list))
	}
	// 正常释放后归零
	gate.store.ReleaseConcurrency(c.Request.Context(), concKey, list[0].leaseID)
	if v := concVal(t, gate.store.rdb, concKey); v != 0 {
		t.Fatalf("after release slot must be freed, got %d", v)
	}
}

// TestQuotaMiddleware_PreflightEarlyExit_ReleasesPriorLayerLease 关键回归：
// subject 层已成功获取租约、随后 group 层 RPM 闸失败时，BeforeUpstream 必须
//  1. 归还 subject 层已获取的租约（preflight 早退自行清理）；
//  2. 不写入 quotaReleaseTokensKey —— 这样 budget.go 在 err!=nil 时
//     （chat.go 不调用 AfterUpstream）不会发生任何重复释放。
func TestQuotaMiddleware_PreflightEarlyExit_ReleasesPriorLayerLease(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		// subject 层 RPM/并发宽松，group 层 RPM=1（第 2 层 RPM 闸失败）
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 5,
		Groups: map[string]quota.LayerLimits{"gpt-4": {RPM: 1, MaxConcurrency: 0}},
		Users:  map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{},
	})
	subjectConcKey := ConcurrencyKey("user", "u-preflight", "", "")

	// 把 group 层 RPM 窗口填满，使第 2 层 RPM 检查必然失败
	groupRPMKey := RPMKey("user", "u-preflight", "group", "gpt-4")
	if err := mw.store.CheckRPM(t.Context(), groupRPMKey, 1); err != nil {
		t.Fatalf("pre-fill group rpm: %v", err)
	}

	c, w := newGateTestContext()
	c.Set("identity", &gwmodel.UserIdentity{UserID: "u-preflight", KeyType: "personal"})
	c.Set("request_model", "gpt-4")

	err := mw.BeforeUpstream(c, "gpt-4")
	if err == nil {
		t.Fatal("group RPM gate should reject")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	// 1) 前一层已获取的租约必须被归还，不得残留
	if v := concVal(t, mw.store.rdb, subjectConcKey); v != 0 {
		t.Fatalf("prior layer lease must be released on later layer failure, got %d", v)
	}
	// 2) 失败路径不得写入令牌列表（调用方据此判断无需 AfterUpstream）
	if v, ok := c.Get(quotaReleaseTokensKey); ok {
		if list, _ := v.([]releaseToken); len(list) != 0 {
			t.Fatalf("failure path must not register release tokens, got %+v", list)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 租约 id 粒度契约（captain 要求 6）：粒度 = (request, key)
// ──────────────────────────────────────────────────────────────────────────────

// TestModelQuotaGate_DifferentKeysInSameRequest_IndependentLeases failover 切到**不同 model key**
// 时，各层/各模型必须各自持有独立租约（不是复用），释放时各自归还。
func TestModelQuotaGate_DifferentKeysInSameRequest_IndependentLeases(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{
			"m-1": {RPM: 0, MaxConcurrency: 1},
			"m-2": {RPM: 0, MaxConcurrency: 1},
		},
	})
	c, _ := newGateTestContext()

	// 同一请求内先命中 m-1，再 failover 到 m-2
	if err := gate.BeforeUpstream(c, "user", "u-multi", "m-1"); err != nil {
		t.Fatalf("acquire m-1: %v", err)
	}
	if err := gate.BeforeUpstream(c, "user", "u-multi", "m-2"); err != nil {
		t.Fatalf("acquire m-2 (failover 目标不同 key 必须独立获取): %v", err)
	}

	key1 := ConcurrencyKey("user", "u-multi", "model", "m-1")
	key2 := ConcurrencyKey("user", "u-multi", "model", "m-2")
	if v := concVal(t, gate.store.rdb, key1); v != 1 {
		t.Fatalf("m-1 应持有 1 条独立租约，实际 %d", v)
	}
	if v := concVal(t, gate.store.rdb, key2); v != 1 {
		t.Fatalf("m-2 应持有 1 条独立租约（不得复用 m-1 的），实际 %d", v)
	}

	tokensRaw, _ := c.Get(quotaReleaseTokensKey)
	tokens, _ := tokensRaw.([]releaseToken)
	if len(tokens) != 2 {
		t.Fatalf("两个不同 key 应登记 2 条令牌，实际 %d (%+v)", len(tokens), tokens)
	}
	if tokens[0].leaseID == tokens[1].leaseID {
		t.Fatal("不同 key 的租约 id 必须不同（租赁粒度 = (request, key)）")
	}
	for _, tok := range tokens {
		if tok.leaseID == "" {
			t.Fatalf("令牌必须携带非空 leaseID，否则该层释放是 no-op 并持续泄漏: %+v", tok)
		}
	}

	// 逐条释放后两个 key 均归零
	for _, tok := range tokens {
		gate.store.ReleaseConcurrency(c.Request.Context(), tok.key, tok.leaseID)
	}
	if v := concVal(t, gate.store.rdb, key1); v != 0 {
		t.Fatalf("m-1 释放后应归零，实际 %d", v)
	}
	if v := concVal(t, gate.store.rdb, key2); v != 0 {
		t.Fatalf("m-2 释放后应归零，实际 %d", v)
	}
}

// TestModelQuotaGate_ConcurrencyRejected_DoesNotHoldLease 失败即不持有：
// 并发闸超限时 BeforeUpstream 不得留下任何租约，也不得登记令牌
// （因此"拿不到 id 用于复用"不是问题——失败路径本来就不持有）。
func TestModelQuotaGate_ConcurrencyRejected_DoesNotHoldLease(t *testing.T) {
	_, gate, holder := setupModelGateTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 0,
		Users:  map[string]quota.LayerLimits{},
		Groups: map[string]quota.LayerLimits{},
		Models: map[string]quota.LayerLimits{"m-full": {RPM: 0, MaxConcurrency: 1}},
	})
	concKey := ConcurrencyKey("user", "u-fail", "model", "m-full")

	// 外部占满唯一槽位
	if _, err := gate.store.AcquireConcurrency(t.Context(), concKey, 1); err != nil {
		t.Fatalf("pre-occupy: %v", err)
	}

	c, _ := newGateTestContext()
	err := gate.BeforeUpstream(c, "user", "u-fail", "m-full")
	if err == nil {
		t.Fatal("超限时应返回错误")
	}
	// 失败路径不持有：不登记令牌
	if v, ok := c.Get(quotaReleaseTokensKey); ok {
		if list, _ := v.([]releaseToken); len(list) != 0 {
			t.Fatalf("失败路径不得登记令牌，实际 %+v", list)
		}
	}
	// 且不得把外部那条租约弄丢（拒绝不能影响已存在的占用）
	if v := concVal(t, gate.store.rdb, concKey); v != 1 {
		t.Fatalf("拒绝时不得改动已有占用，实际 %d", v)
	}
	// 失败后该请求不持有租约：再次调用（仍超限）依旧是干净失败
	c2, _ := newGateTestContext()
	if err := gate.BeforeUpstream(c2, "user", "u-fail", "m-full"); err == nil {
		t.Fatal("仍应超限失败")
	}
	if v := concVal(t, gate.store.rdb, concKey); v != 1 {
		t.Fatalf("连续失败不得叠加占用，实际 %d", v)
	}
}

// TestReleaseConcurrency_EmptyLeaseID_NoopNoPanic 释放空 lease id 必须是无副作用 no-op 且不 panic。
//
// 构造上不会发生（tokens 仅在 acquire 成功后才追加，且 acquire 失败路径立即 releaseAll），
// 这里作为防御性契约固定：空 id 绝不能误删他人成员。
func TestReleaseConcurrency_EmptyLeaseID_NoopNoPanic(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc:empty-id"

	leaseID, err := store.AcquireConcurrency(context.Background(), key, 5)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("释放空 lease id 不得 panic: %v", rec)
			}
		}()
		store.ReleaseConcurrency(context.Background(), key, "")
		store.ReleaseConcurrency(context.Background(), "", "")
	}()

	// 他人成员必须原封不动
	if v := concVal(t, store.rdb, key); v != 1 {
		t.Fatalf("空 lease id 释放不得删除任何成员，实际剩下 %d 条", v)
	}
	if _, err := store.rdb.ZScore(context.Background(), key, leaseID).Result(); err != nil {
		t.Fatalf("原租约成员应仍然存在: %v", err)
	}
}

// TestConcurrencyKey_SharedAuthority_SingleDefinition 跨进程唯一权威定义回归闸门：
// 网关与控制面（internal/controlplane/service）必须解析出**逐字节相同**的并发键。
// 两个包各自从 internal/core/quota 取格式；若任一侧回退为本地字面量，
// 本用例即失败（历史上正是这种漂移让控制面读数恒为 0）。
func TestConcurrencyKey_SharedAuthority_SingleDefinition(t *testing.T) {
	userID := "3f2b8c1e-0000-4000-8000-000000000042"
	gatewayKey := ConcurrencyKey("user", userID, "", "")
	sharedKey := quota.ConcurrencyKeySubject("user", userID)

	if gatewayKey != sharedKey {
		t.Fatalf("网关键 %q 与共享定义 %q 不一致：两侧不得各自实现键格式", gatewayKey, sharedKey)
	}
	if want := "quota:conc:lease:user:" + userID; gatewayKey != want {
		t.Fatalf("网关并发键 = %q, want %q", gatewayKey, want)
	}
	// 分层作用域必须与主体层不同（各自独立限额，不能互相替代）
	modelKey := ConcurrencyKey("user", userID, "model", "m-uuid")
	if modelKey == gatewayKey {
		t.Fatal("model 层键必须与主体层键不同")
	}
	if want := "quota:conc:lease:user:" + userID + ":model:m-uuid"; modelKey != want {
		t.Fatalf("model 层键 = %q, want %q", modelKey, want)
	}

	// 读侧区间也来自共享定义：左开、严格大于 now，且与写入侧清理边界一致
	now := time.UnixMilli(1_789_000_000_123)
	min, max := quota.ConcurrencyInFlightRange(now)
	if min != "(1789000000123" || max != "+inf" {
		t.Fatalf("在飞统计区间 = (%q,%q)，期望左开 (now +inf", min, max)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 超限必须是「业务拒绝」：非 nil error、不 fail-open、不留垃圾成员
// （对应 captain/engineer-obs 报告的「Lua false → redis.Nil → 限额静默失效」缺陷形状）
// ──────────────────────────────────────────────────────────────────────────────

// TestAcquireConcurrency_LimitPlusOne_ReturnsNonNilErrorAndNoResidue
// 断言的是**缺陷的真实形状**：若脚本用 Lua false 表示超限，go-redis 会解码成
// (nil, redis.Nil)，于是 AcquireConcurrency 返回 ("", nil) —— 调用方视为"获取成功"，
// 限额静默失效。因此本用例同时断言：
//  1. 第 limit+1 次必须返回**非 nil error**（只断言 lease=="" 会漏掉该缺陷）；
//  2. 该 error 是「超限」类业务判定，而不是 fail-open 协议异常；
//  3. 超限路径不得在 ZSET 留下任何成员（ZCARD 不增长）；
//  4. 集合里只有 limit 条成员，且都是**非空** lease id。
func TestAcquireConcurrency_LimitPlusOne_ReturnsNonNilErrorAndNoResidue(t *testing.T) {
	_, store := setupMiniRedis(t)
	ctx := context.Background()
	const limit = 3
	key := "test:conc:limit-plus-one"

	leases := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		lease, err := store.AcquireConcurrency(ctx, key, limit)
		if err != nil {
			t.Fatalf("第 %d 次（< limit）应放行: %v", i+1, err)
		}
		if lease == "" {
			t.Fatalf("第 %d 次放行必须返回非空 lease id（空值会使释放退化为 no-op）", i+1)
		}
		leases = append(leases, lease)
	}
	before := concVal(t, store.rdb, key)
	if before != limit {
		t.Fatalf("占满后 ZCARD 应为 %d，实际 %d", limit, before)
	}

	// 第 limit+1 次：必须报错，且不能是"空 lease + nil error"
	lease, err := store.AcquireConcurrency(ctx, key, limit)
	if err == nil {
		t.Fatalf("超限必须返回非 nil error，实际 lease=%q err=nil —— 这正是「Lua false → redis.Nil → "+
			"被 fail-open 吞掉 → 限额静默失效」的缺陷形状", lease)
	}
	if lease != "" {
		t.Fatalf("超限时不得返回 lease id，实际 %q", lease)
	}
	if !strings.Contains(err.Error(), "concurrency_exceeded") {
		t.Fatalf("超限应是业务判定错误，实际 %v（若含 protocol_error/fail-open 说明被误判为故障）", err)
	}
	if strings.Contains(err.Error(), "protocol_error") {
		t.Fatalf("超限被误判为协议异常（脚本返回了空回复？）：%v", err)
	}

	// 不得留下垃圾成员
	if after := concVal(t, store.rdb, key); after != before {
		t.Fatalf("超限路径不得写入成员：ZCARD %d -> %d", before, after)
	}
	// 集合内成员必须恰好是那 limit 条非空租约
	members, err := store.rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRANGE: %v", err)
	}
	if len(members) != limit {
		t.Fatalf("成员数应为 %d，实际 %d（%v）", limit, len(members), members)
	}
	for i, m := range members {
		if m == "" {
			t.Fatalf("第 %d 个成员为空（空 lease id 会互相覆盖并导致释放 no-op）", i)
		}
		if m != leases[i] {
			t.Fatalf("第 %d 个成员应为 %q，实际 %q", i, leases[i], m)
		}
	}
}

// TestAcquireConcurrency_LuaFalseReturn_MustNotBeFailOpen 直接复现被报告的缺陷形状：
// 若脚本（误）用 Lua `false` 表示超限，RESP2 下它是空回复、go-redis 解码为 redis.Nil。
// 调用方绝不能把它当成 Redis 故障而 fail-open（返回 ("", nil)），否则限额静默失效。
//
// 做法：注册一个返回 Lua false 的脚本到演练 Redis，再用同一 RedisStore 触发
// redis.Nil 路径，断言返回的是**非 nil error**（拒绝）而不是 ("", nil)（放行）。
func TestAcquireConcurrency_LuaFalseReturn_MustNotBeFailOpen(t *testing.T) {
	_, rdb := newStoreWithClient(t)
	ctx := context.Background()
	key := "test:conc:lua-false-shape"

	// 手工注册（eval 一次）一个恒定返回 Lua false 的脚本，模拟缺陷版本
	scriptText := "return false"
	sha, err := rdb.ScriptLoad(ctx, scriptText).Result()
	if err != nil {
		t.Fatalf("SCRIPT LOAD: %v", err)
	}

	store := NewRedisStore(rdb)
	// 让 store 使用的"获取"脚本换成这个缺陷脚本：通过同名 SHA 触发 EVALSHA
	// （go-redis 的 Script.Run 会先用 EVALSHA；预先加载该脚本内容即可命中）
	// 这里直接把脚本对象替换为返回 false 的等价脚本
	orig := concurrencyAcquireScript
	concurrencyAcquireScript = redis.NewScript(scriptText)
	t.Cleanup(func() { concurrencyAcquireScript = orig })

	lease, err := store.AcquireConcurrency(ctx, key, 5)
	if err == nil {
		t.Fatalf("Lua false（空回复）必须被判为拒绝/异常，绝不能 fail-open 放行："+
			"实际 lease=%q err=nil（sha=%s）", lease, sha)
	}
	if lease != "" {
		t.Fatalf("异常路径不得返回 lease id，实际 %q", lease)
	}
	t.Logf("缺陷形状被正确拒绝: %v", err)

	// 且不得在 ZSET 留下成员
	if n := concVal(t, rdb, key); n != 0 {
		t.Fatalf("异常路径不得写入成员，实际 ZCARD=%d", n)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 空 lease id 语义（captain 正式口径）：
//   空 lease id + nil error ⇒ 不持有租约（"不限" 或 "fail-open"），无需释放
//   非空 lease id           ⇒ 持有租约，必须释放
// 两个用例分别固定"不限"与"fail-open"这两条合法路径，防止后人把空 id 改成报错
// 而破坏 unlimited / fail-open 语义。
// ──────────────────────────────────────────────────────────────────────────────

// TestAcquireConcurrency_LimitZero_EmptyLeaseNeedsNoRelease 「不限」路径：
// limit<=0 时返回 ("", nil)，且不占用任何槽位——因此调用方无需释放，
// 对空 id 调用 ReleaseConcurrency 必须是安全 no-op（releaseAll 会这么做）。
func TestAcquireConcurrency_LimitZero_EmptyLeaseNeedsNoRelease(t *testing.T) {
	_, store := setupMiniRedis(t)
	ctx := context.Background()

	for _, limit := range []int{0, -1, -100} {
		key := "test:conc:unlimited"
		lease, err := store.AcquireConcurrency(ctx, key, limit)
		if err != nil {
			t.Fatalf("limit=%d（不限）不得报错，实际 %v", limit, err)
		}
		if lease != "" {
			t.Fatalf("limit=%d（不限）不应持有租约，实际 lease=%q", limit, lease)
		}
		// 不限路径不得写入任何成员
		if n := concVal(t, store.rdb, key); n != 0 {
			t.Fatalf("limit=%d（不限）不得占用槽位，实际 ZCARD=%d", limit, n)
		}
		// 对空 id 释放必须安全（releaseAll 会这么做）
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("对空 lease id 释放不得 panic: %v", r)
				}
			}()
			store.ReleaseConcurrency(ctx, key, "")
		}()
		if n := concVal(t, store.rdb, key); n != 0 {
			t.Fatalf("空 id 释放后仍应为 0，实际 %d", n)
		}
		// 不限路径必须能反复放行（不是"只放行一次"）
		for i := 0; i < 3; i++ {
			if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
				t.Fatalf("不限应始终放行，第 %d 次失败: %v", i+1, err)
			}
		}
	}
}

// TestAcquireConcurrency_RedisDown_EmptyLeaseNeedsNoRelease fail-open 路径：
// Redis 故障时返回 ("", nil)——放行但无真实占用，因此无需释放，且不得 panic。
func TestAcquireConcurrency_RedisDown_EmptyLeaseNeedsNoRelease(t *testing.T) {
	mr, store := setupMiniRedis(t)
	ctx := context.Background()
	key := "test:conc:failopen-empty-id"

	mr.Close() // 制造真实故障
	lease, err := store.AcquireConcurrency(ctx, key, 5)
	if err != nil {
		t.Fatalf("Redis 故障应 fail-open（放行），实际报错: %v", err)
	}
	if lease != "" {
		t.Fatalf("fail-open 不应返回租约 id（无真实占用），实际 %q", lease)
	}
	// fail-open 下释放空 id 必须安全无副作用（releaseAll 会对它调用释放）
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("fail-open 后释放空 lease id 不得 panic: %v", r)
			}
		}()
		store.ReleaseConcurrency(ctx, key, "")
	}()
}

// TestAcquireConcurrency_SustainedOverLimit_DoesNotGrowZCARD 直接断言（非结构性推论）：
// 连续多次超限被拒后，ZCARD 必须保持不变（等于 limit），不因拒绝而增长。
func TestAcquireConcurrency_SustainedOverLimit_DoesNotGrowZCARD(t *testing.T) {
	_, store := setupMiniRedis(t)
	ctx := context.Background()
	const limit = 3
	key := "test:conc:no-residue-sustained"

	for i := 0; i < limit; i++ {
		if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
			t.Fatalf("第 %d 次应放行: %v", i+1, err)
		}
	}
	before := concVal(t, store.rdb, key)
	if before != limit {
		t.Fatalf("占满后 ZCARD 应为 %d，实际 %d", limit, before)
	}

	// 连续 10 次超限：每次都必须被拒绝，且集合大小逐次核对不变
	for i := 0; i < 10; i++ {
		lease, err := store.AcquireConcurrency(ctx, key, limit)
		if err == nil {
			t.Fatalf("第 %d 次超限必须拒绝，实际 lease=%q err=nil", i+1, lease)
		}
		if lease != "" {
			t.Fatalf("第 %d 次超限不得返回 lease id，实际 %q", i+1, lease)
		}
		if n := concVal(t, store.rdb, key); n != before {
			t.Fatalf("第 %d 次被拒后 ZCARD 必须保持 %d，实际 %d（拒绝路径留下了残留成员）",
				i+1, before, n)
		}
	}
	// 成员必须仍是原来那 limit 条非空租约
	members, err := store.rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRANGE: %v", err)
	}
	if len(members) != limit {
		t.Fatalf("最终成员数应为 %d，实际 %d", limit, len(members))
	}
	for i, m := range members {
		if m == "" {
			t.Fatalf("第 %d 个成员为空（空成员会互相覆盖并导致释放 no-op）", i)
		}
	}
}
