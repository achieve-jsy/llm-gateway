package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/core/quota"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// stability_test.go 覆盖四层配额系统的稳定性 / 竞态 / 集成场景。
//
// 与已有单元测试的边界：
//   - redis_store_test.go：store 层单元（limit0 / under / at-limit / release / TTL / 窗口 / fail-open）
//   - middleware_test.go：middleware 层单元（11 + 5 ModelQuotaGate）
//   - 本文件聚焦：端到端恢复、四层叠加、豁免不计数、fail-open 并发、快照热更新原子性、
//     limit=0 vs 缺省继承的非对称语义、-race 下的大规模并发计数准确性。
//
// 所有用例独立 setup（各自 miniredis + holder），无跨用例状态依赖。

// ──────────────────────────────────────────────────────────────────────────────
// 共享 fixture
// ──────────────────────────────────────────────────────────────────────────────

// newStabilityStore 起一个独立 miniredis + RedisStore，返回 mr 与 store 供断言。
func newStabilityStore(t *testing.T) (*miniredis.Miniredis, *RedisStore, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		rdb.Close()
		mr.Close()
	})
	return mr, NewRedisStore(rdb), rdb
}

// newStabilityStoreFastFail 起一个 miniredis + 关闭重试的 RedisStore，专供 Redis-down
// fail-open 测试。默认 go-redis 在连接失败时会重试 5 次（带退避），导致用例耗时数十秒；
// 这里 MaxRetries=0 + 短 DialTimeout 让故障即时返回，fail-open 路径快速走通。
func newStabilityStoreFastFail(t *testing.T) (*miniredis.Miniredis, *RedisStore, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:       mr.Addr(),
		MaxRetries: -1, // 不重试，连接失败立即返回 error
		DialTimeout: 200 * time.Millisecond,
	})
	t.Cleanup(func() {
		rdb.Close()
		mr.Close()
	})
	return mr, NewRedisStore(rdb), rdb
}

// identityOf 构造 UserIdentity。
func identityOf(userID, keyID, keyType string) *gwmodel.UserIdentity {
	return &gwmodel.UserIdentity{UserID: userID, KeyID: keyID, KeyType: keyType}
}

// setupMiddlewareTestFastFail 与 setupMiddlewareTest 同构，但 Redis 客户端关闭重试，
// 专供 Redis-down fail-open 路径，避免 go-redis 默认 5 次重试 + 退避导致的数十秒阻塞。
func setupMiddlewareTestFastFail(t *testing.T) (*miniredis.Miniredis, *QuotaMiddleware, *SnapshotHolder) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:        mr.Addr(),
		MaxRetries:  -1,
		DialTimeout: 200 * time.Millisecond,
	})
	t.Cleanup(func() {
		rdb.Close()
		mr.Close()
	})
	holder := NewSnapshotHolder()
	store := NewRedisStore(rdb)
	return mr, NewMiddleware(holder, store), holder
}

// emptySnapshot 构造一个四层 map 都初始化的快照，避免 nil map 误判。
func emptySnapshot() *quota.QuotaSnapshot {
	return &quota.QuotaSnapshot{
		Version:           1,
		DefaultUserRPM: 1000, DefaultUserMaxConcurrency: 1000,
		DefaultServiceKeyRPM: 1000, DefaultServiceKeyMaxConcurrency: 1000,
		Users:             map[string]quota.LayerLimits{},
		ServiceKeys:       map[string]quota.LayerLimits{},
		Groups:            map[string]quota.LayerLimits{},
		Models:            map[string]quota.LayerLimits{},
	}
}

// doReq 发一次 POST 请求到 r，返回 recorder。
func doReq(r *gin.Engine) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doReqWithBody 发带 JSON body 的 POST 请求（用于验证 extractModelParam 从 body 解析 model）。
func doReqWithBody(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// concVal 读取并发键当前值（int），键不存在返回 0。
func concVal(t *testing.T, rdb *redis.Client, key string) int64 {
	t.Helper()
	v, err := rdb.Get(context.Background(), key).Int64()
	if err == redis.Nil {
		return 0
	}
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	return v
}

// rpmCount 读取 RPM ZSET 当前成员数。
func rpmCount(t *testing.T, rdb *redis.Client, key string) int64 {
	t.Helper()
	v, err := rdb.ZCard(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("zcard %s: %v", key, err)
	}
	return v
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 1：RPM 触顶拒绝 + 窗口过期恢复（middleware 端到端 + 边界）
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_RPM_RejectThenWindowRecovery 端到端：填满 → 第 N+1 拒绝 →
// 快进 < 窗口仍拒绝 → 快进 > 窗口恢复通过。
// 单元层已有 redis_store_test TestCheckRPM_WindowExpiry_FastForward，此处补 middleware
// 端到端与窗口边界（30s 仍拒、61s 恢复）。
func TestStability_RPM_RejectThenWindowRecovery(t *testing.T) {
	mr, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 3, DefaultUserMaxConcurrency: 0,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	id := identityOf("u-rpm", "", "personal")
	r := newTestRouter(mw, id, "")

	// 前 3 次通过
	for i := 0; i < 3; i++ {
		if w := doReq(r); w.Code != http.StatusOK {
			t.Fatalf("req %d should pass, got %d", i+1, w.Code)
		}
	}
	// 第 4 次触顶拒绝
	if w := doReq(r); w.Code != http.StatusTooManyRequests {
		t.Fatalf("req 4 should be 429, got %d", w.Code)
	}

	// 快进 30s（< 60s 窗口）→ 仍拒绝
	mr.FastForward(30 * time.Second)
	if w := doReq(r); w.Code != http.StatusTooManyRequests {
		t.Fatalf("after 30s (< window) should still be 429, got %d", w.Code)
	}

	// 快进到总 61s → 窗口过期，恢复通过
	mr.FastForward(31 * time.Second)
	if w := doReq(r); w.Code != http.StatusOK {
		t.Fatalf("after 61s (> window) should recover 200, got %d", w.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 2：并发触顶拒绝 + Release 恢复 + 过释放不变负
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_Concurrency_FillRejectReleaseRecover store 层：占满 → 拒绝 →
// Release 一个 → 恢复。比 redis_store_test ReleaseThenAcquire 多 asserts：归零后键被删除。
func TestStability_Concurrency_FillRejectReleaseRecover(t *testing.T) {
	mr, store, rdb := newStabilityStore(t)
	key := "test:conc:recover"

	// 占满 limit=2
	for i := 0; i < 2; i++ {
		if err := store.AcquireConcurrency(context.Background(), key, 2); err != nil {
			t.Fatalf("acquire %d: %v", i+1, err)
		}
	}
	// 第 3 次拒绝
	if err := store.AcquireConcurrency(context.Background(), key, 2); err == nil {
		t.Fatal("3rd acquire should be rejected")
	}
	// Release 一个 → 恢复
	store.ReleaseConcurrency(context.Background(), key)
	if err := store.AcquireConcurrency(context.Background(), key, 2); err != nil {
		t.Fatalf("after release should recover, got %v", err)
	}
	// 全部释放后键归零并被删除
	store.ReleaseConcurrency(context.Background(), key)
	store.ReleaseConcurrency(context.Background(), key)
	if v := concVal(t, rdb, key); v != 0 {
		t.Fatalf("after full release key should be gone (0), got %d", v)
	}
	if mr.Exists(key) {
		t.Fatalf("key %s should be DELeted when reaching 0", key)
	}
}

// TestStability_Concurrency_OverRelease_NeverNegative Release 次数 > Acquire
// 不应使计数变负。多次过度释放后，键应被清理且后续 Acquire 仍正确从 1 计数。
func TestStability_Concurrency_OverRelease_NeverNegative(t *testing.T) {
	_, store, rdb := newStabilityStore(t)
	key := "test:conc:over-release"

	// Acquire 1 次
	if err := store.AcquireConcurrency(context.Background(), key, 10); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// Release 3 次（过度释放 2 次）
	for i := 0; i < 3; i++ {
		store.ReleaseConcurrency(context.Background(), key)
	}
	// 不能为负：键应已归零被删
	if v := concVal(t, rdb, key); v != 0 {
		t.Fatalf("over-release must not go negative, got %d", v)
	}
	// 后续 Acquire 应从 1 开始（不是从负数 +1 = 0 再 +1）
	if err := store.AcquireConcurrency(context.Background(), key, 10); err != nil {
		t.Fatalf("acquire after over-release: %v", err)
	}
	if v := concVal(t, rdb, key); v != 1 {
		t.Fatalf("after over-release + acquire, val should be 1, got %d", v)
	}
}

// TestStability_Middleware_Concurrency_ReleaseMidFlight middleware 端到端：
// 一个长占用请求 → 第二个被拒 → 第一个完成后 → 第三个恢复通过。
func TestStability_Middleware_Concurrency_ReleaseMidFlight(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 1,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	id := identityOf("u-conc", "", "personal")

	r := gin.New()
	blockCh := make(chan struct{})
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", id)
	}, mw.Handler(), func(c *gin.Context) {
		<-blockCh
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 第一个请求占用并发槽
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}()
	time.Sleep(50 * time.Millisecond)

	// 第二个请求被并发拒绝
	if w := doReq(r); w.Code != http.StatusTooManyRequests {
		t.Fatalf("2nd concurrent req should be 429, got %d", w.Code)
	}

	// 释放第一个 → 第三个恢复通过
	close(blockCh)
	time.Sleep(50 * time.Millisecond) // 等待 releaseAll 完成
	if w := doReq(r); w.Code != http.StatusOK {
		t.Fatalf("after release 3rd req should recover 200, got %d", w.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 3：四层叠加 —— 单请求同时过 subject+group+model，各层独立触顶不串扰
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_FourLayers_IndependentCounting 单一请求路径串联 middleware(subject+group)
// 与 ModelQuotaGate(model)，验证：
//  1. 三层各自独立计数（不同 key 不串扰）；
//  2. 任一层触顶即拒，其余层计数不受影响；
//  3. service_key 层经 ResolveSubject 也可独立触顶。
//
// 注：四层中 user 与 service_key 为互斥主体（同一 identity 只命中其一），
// 这里用两组 identity 验证 user 主体下 subject+group+model 三闸门独立，
// 并用 service_key identity 验证第四个维度。
func TestStability_FourLayers_IndependentCounting(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	gate := &ModelQuotaGate{holder: holder, store: mw.store}
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 100,
		DefaultServiceKeyRPM: 100, DefaultServiceKeyMaxConcurrency: 100,
		Users:             map[string]quota.LayerLimits{},
		ServiceKeys:       map[string]quota.LayerLimits{"sk-1": {RPM: 2, MaxConcurrency: 0}},
		Groups:            map[string]quota.LayerLimits{"gpt-4": {RPM: 2, MaxConcurrency: 0}},
		Models:            map[string]quota.LayerLimits{"m-uuid": {RPM: 2, MaxConcurrency: 0}},
	})

	// 构造一条 Post 链：middleware -> handler(内调 ModelQuotaGate) -> ok
	id := identityOf("u-four", "", "personal")
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", id)
		c.Set("request_model", "gpt-4")
	}, mw.Handler(), func(c *gin.Context) {
		if err := gate.BeforeUpstream(c, "user", "u-four", "m-uuid"); err != nil {
			return // BeforeUpstream 已写 429
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 跑 2 次（subject=100, group=2, model=2 都未触顶）→ 全通过
	for i := 0; i < 2; i++ {
		if w := doReq(r); w.Code != http.StatusOK {
			t.Fatalf("req %d should pass, got %d (body=%s)", i+1, w.Code, w.Body.String())
		}
	}

	// 第 3 次：group 层先于 model 触顶？顺序：middleware 先 group(2 满)，应在 group 拒。
	// 但 group/group 与 model 都已满 2 → middleware group 先拒。
	w := doReq(r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("req 3 should be 429 (group or model hit), got %d", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("req 3 response should be one valid JSON document: %v; body=%q", err, w.Body.String())
	}
	errObj, _ := body["error"].(map[string]interface{})
	details, _ := errObj["details"].(map[string]interface{})
	layer, _ := details["layer"].(string)
	if layer != "group" {
		t.Fatalf("req 3 should trip group layer (checked before model), got layer=%v", layer)
	}

	// 验证 subject 与 model 计数互不串扰：
	// subject(user) RPM key 应有 3 条（3 次都过了 subject 层后才在 group 拒）
	svmKey := RPMKey("user", "u-four", "", "")
	if c := rpmCount(t, mw.store.rdb, svmKey); c != 3 {
		t.Errorf("subject layer should count all 3 attempts, got %d", c)
	}
	// model ZSET：第 3 次在 group 层被拒（BeforeUpstream 未执行），故 model 只计 2
	modelKey := RPMKey("user", "u-four", "model", "m-uuid")
	if c := rpmCount(t, mw.store.rdb, modelKey); c != 2 {
		t.Errorf("model layer should count 2 (gate not reached on 3rd), got %d", c)
	}
}

// TestStability_FourLayers_ServiceKeySubject_Independent service_key 主体层独立触顶。
func TestStability_FourLayers_ServiceKeySubject_Independent(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultServiceKeyRPM: 100, DefaultServiceKeyMaxConcurrency: 100,
		ServiceKeys:       map[string]quota.LayerLimits{"sk-A": {RPM: 1, MaxConcurrency: 0}},
		Groups:            map[string]quota.LayerLimits{},
		Models:            map[string]quota.LayerLimits{},
	})
	skA := identityOf("u-x", "sk-A", "service")
	skB := identityOf("u-x", "sk-B", "service") // 不同 key，独立计数
	rA := newTestRouter(mw, skA, "")
	rB := newTestRouter(mw, skB, "")

	// sk-A 用满 1
	if w := doReq(rA); w.Code != http.StatusOK {
		t.Fatalf("sk-A req 1 pass, got %d", w.Code)
	}
	// sk-A 第 2 拒
	if w := doReq(rA); w.Code != http.StatusTooManyRequests {
		t.Fatalf("sk-A req 2 should 429, got %d", w.Code)
	}
	// sk-B 仍通过（独立计数，不串扰）
	if w := doReq(rB); w.Code != http.StatusOK {
		t.Fatalf("sk-B should pass (independent), got %d", w.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 3b（BUG 探针）：四层全链路下 model 层并发令牌被泄漏 —— 已确认缺陷
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_ModelConcurrencyToken_Released 验证 model 层并发令牌在请求结束后被正确释放。
//
// 历史 bug（已修复）：单请求经 middleware(subject+group) 再进 ModelQuotaGate(model)，
// 请求结束后 model 层并发令牌不会被释放。根因：middleware.go 在 c.Next() 后用
// “调用 c.Next() 之前”捕获的局部 tokens 切片做 releaseAll；而 ModelQuotaGate.BeforeUpstream
// 执行于 handler（c.Next() 内），它把 model token 通过 append 写回 context 中的切片。
// 切片 append 在容量不足时分配新底层数组 → middleware 局部 tokens 指向旧数组，
// 感知不到 model token → releaseAll 只放 subject+group，model 并发计数只增不减。
//
// 修复：middleware 在 c.Next() 后从 context 回读 quota_release_tokens 的最终列表再 release。
// 本用例断言修复后行为：请求结束后 model 并发计数归零，第 3 次请求也能通过。
func TestStability_ModelConcurrencyToken_Released(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	gate := &ModelQuotaGate{holder: holder, store: mw.store}
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 0,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{"m-uuid": {RPM: 0, MaxConcurrency: 2}},
	})
	id := identityOf("u-leak", "", "personal")
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", id)
	}, mw.Handler(), func(c *gin.Context) {
		if err := gate.BeforeUpstream(c, "user", "u-leak", "m-uuid"); err != nil {
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	modelConcKey := ConcurrencyKey("user", "u-leak", "model", "m-uuid")

	// 跑 2 次完整请求，每次都应 acquire+release model 并发 → 计数始终归零
	for i := 0; i < 2; i++ {
		if w := doReq(r); w.Code != http.StatusOK {
			t.Fatalf("req %d should pass, got %d (body=%s)", i+1, w.Code, w.Body.String())
		}
	}

	// 修复后：请求结束后 model 并发计数 = 0（令牌已被正确释放）
	if got := concVal(t, mw.store.rdb, modelConcKey); got != 0 {
		t.Fatalf("model 并发令牌泄漏：期望 release 后 val=0，实际 val=%d", got)
	}

	// 第 3 次请求：limit=2 未被泄漏占满 → 应正常通过（200），不应 429
	w := doReq(r)
	if w.Code != http.StatusOK {
		t.Errorf("修复后第 3 次请求应通过（200），got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestStability_FourLayers_HappyPath_WithinLimits 对照组：各层都未触顶时全链路放行，
// 且 subject+group 层计数被正确释放（model 层令牌泄漏由上一用例单独固化）。
func TestStability_FourLayers_HappyPath_WithinLimits(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	gate := &ModelQuotaGate{holder: holder, store: mw.store}
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 0, DefaultUserMaxConcurrency: 0,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{"gpt-4": {RPM: 0, MaxConcurrency: 2}},
		Models:      map[string]quota.LayerLimits{"m-uuid": {RPM: 0, MaxConcurrency: 2}},
	})
	id := identityOf("u-happy", "", "personal")
	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", id)
		c.Set("request_model", "gpt-4")
	}, mw.Handler(), func(c *gin.Context) {
		if err := gate.BeforeUpstream(c, "user", "u-happy", "m-uuid"); err != nil {
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 单次请求通过 → group 并发令牌被释放归零
	if w := doReq(r); w.Code != http.StatusOK {
		t.Fatalf("happy-path req should pass, got %d (body=%s)", w.Code, w.Body.String())
	}
	groupConcKey := ConcurrencyKey("user", "u-happy", "group", "gpt-4")
	if v := concVal(t, mw.store.rdb, groupConcKey); v != 0 {
		t.Errorf("group concurrency token should be released to 0, got %d", v)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 4：豁免跳过 —— 不触碰 RPM / 并发计数器（Redis 键为空）
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_Exemption_DoesNotTouchCounters middleware_test TestMiddleware_Exemption
// 只 assert 放行；本用例进一步断言 Redis 中该主体的 RPM 并发键完全不存在（真跳过，
// 而非“计数后豁免放行”）。
func TestStability_Exemption_DoesNotTouchCounters(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 1,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
		Exemptions: []quota.QuotaExemption{
			{SubjectType: "user", SubjectID: "u-exempt", Reason: "emergency", ExpiresAt: time.Now().Add(1 * time.Hour)},
		},
	})
	id := identityOf("u-exempt", "", "personal")
	r := newTestRouter(mw, id, "")

	// 豁免用户连发 5 次，均应通过
	for i := 0; i < 5; i++ {
		if w := doReq(r); w.Code != http.StatusOK {
			t.Fatalf("exempt req %d should pass, got %d", i+1, w.Code)
		}
	}

	// 断言：豁免主体的 RPM 与并发键均不存在（未被触碰）
	rpmK := RPMKey("user", "u-exempt", "", "")
	concK := ConcurrencyKey("user", "u-exempt", "", "")
	if c := rpmCount(t, mw.store.rdb, rpmK); c != 0 {
		t.Errorf("exempt subject must not touch RPM counter, got %d entries", c)
	}
	if v := concVal(t, mw.store.rdb, concK); v != 0 {
		t.Errorf("exempt subject must not touch concurrency counter, got %d", v)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 5：fail-open 全场景 —— Redis 关闭下并发请求全放行、不 panic、Release 不炸
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_FailOpen_RedisDown_ConcurrentAndRelease Redis 关闭下：
//   - CheckRPM / AcquireConcurrency 均 nil（放行）
//   - 并发 50 goroutine 同时请求 middleware 全 200，无 panic
//   - ReleaseConcurrency 在 Redis down 时不 panic
func TestStability_FailOpen_RedisDown_ConcurrentAndRelease(t *testing.T) {
	mr, mw, holder := setupMiddlewareTestFastFail(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 1,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	id := identityOf("u-failopen", "", "personal")
	r := newTestRouter(mw, id, "")

	// 关闭 Redis
	mr.Close()

	var wg sync.WaitGroup
	var bad atomic.Int64
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					bad.Add(1)
					t.Errorf("panic under redis-down: %v", rec)
				}
			}()
			w := doReq(r)
			if w.Code != http.StatusOK {
				bad.Add(1)
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("fail-open: %d requests did not pass / panicked", bad.Load())
	}

	// ReleaseConcurrency 在 Redis down 下也不 panic
	store := mw.store
	store.ReleaseConcurrency(context.Background(), "quota:conc:user:u-failopen")
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 6：快照热更新 —— SetSnapshot 后 middleware 立即生效；并发读写 SnapshotHolder 无 race
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_SnapshotHotUpdate_ImmediateEffect holder.Set 后下一个请求立即用新 limit。
func TestStability_SnapshotHotUpdate_ImmediateEffect(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	// 初始宽松：RPM=1000
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 1000, DefaultUserMaxConcurrency: 0,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	id := identityOf("u-hot", "", "personal")
	r := newTestRouter(mw, id, "")

	// 宽松下通过
	if w := doReq(r); w.Code != http.StatusOK {
		t.Fatalf("loose limit should pass, got %d", w.Code)
	}

	// 热替换为严格：RPM=1（已用 1 → 下一个应即拒）
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 1, DefaultUserMaxConcurrency: 0,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{},
		Models:      map[string]quota.LayerLimits{},
	})
	if w := doReq(r); w.Code != http.StatusTooManyRequests {
		t.Fatalf("after hot-update to RPM=1, next req should be 429, got %d", w.Code)
	}
}

// TestStability_SnapshotHolder_ConcurrentReadWriteRace 并发：N reader 调 Get，
// 同时 writer 周期性 Set。供 `go test -race` 检测数据竞争。
func TestStability_SnapshotHolder_ConcurrentReadWriteRace(t *testing.T) {
	holder := NewSnapshotHolder()
	holder.Set(emptySnapshot())

	var stop atomic.Bool
	var wg sync.WaitGroup

	// readers
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				if s := holder.Get(); s != nil {
					_ = s.DefaultUserRPM
				}
			}
		}()
	}
	// writer：不断替换快照
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			s := emptySnapshot()
			s.DefaultUserRPM = 1 + (i % 50)
			holder.Set(s)
		}
		stop.Store(true)
	}()
	wg.Wait()
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 7：limit=0 vs 缺省继承的非对称语义
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_LimitZero_vs_MissingInherit 显式 0 = 不限（恒通过且不计数）；
// 缺省（map 中无条目）= 继承：subject 层继承 default，group/model 层继承为 0（不限）。
// 此非对称是设计要点，专门固化。
func TestStability_LimitZero_vs_MissingInherit(t *testing.T) {
	_, store, rdb := newStabilityStore(t)

	// limit=0 → 恒通过且不计数（RPM ZSET 不应被写）
	key0 := "test:rpm:zero"
	for i := 0; i < 20; i++ {
		if err := store.CheckRPM(context.Background(), key0, 0); err != nil {
			t.Fatalf("limit=0 req %d should pass, got %v", i+1, err)
		}
	}
	if c := rpmCount(t, rdb, key0); c != 0 {
		t.Errorf("limit=0 must NOT write RPM zset, got %d entries", c)
	}

	// 并发 limit=0 同理不计数
	keyc0 := "test:conc:zero"
	for i := 0; i < 20; i++ {
		if err := store.AcquireConcurrency(context.Background(), keyc0, 0); err != nil {
			t.Fatalf("limit=0 conc req %d should pass, got %v", i+1, err)
		}
	}
	if v := concVal(t, rdb, keyc0); v != 0 {
		t.Errorf("limit=0 must NOT INCR concurrency, got %d", v)
	}

	// 缺省继承非对称（resolver 层，纯函数）：
	// subject 缺省 → default（继承，非 0）
	snap := &quota.QuotaSnapshot{
		DefaultUserRPM: 42, DefaultUserMaxConcurrency: 7,
		DefaultServiceKeyRPM: 99, DefaultServiceKeyMaxConcurrency: 11,
		Users:             map[string]quota.LayerLimits{},
		Groups:            map[string]quota.LayerLimits{},
		Models:            map[string]quota.LayerLimits{},
	}
	if r := ResolveSubjectLimits(snap, "user", "absent"); r.RPM != 42 || r.MaxConcurrency != 7 || r.Source != "default_user" {
		t.Errorf("subject missing must inherit default_user, got %+v", r)
	}
	// group 缺省 → 0（不限，非继承 default）
	if r := ResolveGroupLimits(snap, "absent-group"); r.RPM != 0 || r.MaxConcurrency != 0 || r.Source != "unset" {
		t.Errorf("group missing must be 0/unlimited (not inherit), got %+v", r)
	}
	// model 缺省 → 0（不限，非继承 default）
	if r := ResolveModelLimits(snap, "absent-model"); r.RPM != 0 || r.MaxConcurrency != 0 || r.Source != "unset" {
		t.Errorf("model missing must be 0/unlimited (not inherit), got %+v", r)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// 场景 8：并发压测（稳态）—— goroutine 并发 Acquire/Release/CheckRPM，-race 下计数准确
// ──────────────────────────────────────────────────────────────────────────────

// TestStability_ConcurrencyStress_AcquireRelease_CountAccurate 1000 次 acquire/release
// 混合并发，limit=50。断言：最终计数归 0（每次成对）、期间无负值、-race 无竞争。
// 仅有部分 acquire 会成功（超 50 即拒），但每个成功 acquire 必有对应 release。
func TestStability_ConcurrencyStress_AcquireRelease_CountAccurate(t *testing.T) {
	_, store, rdb := newStabilityStore(t)
	key := "test:conc:stress"
	limit := 50
	total := 1000

	var wg sync.WaitGroup
	var acquired, rejected atomic.Int64
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.AcquireConcurrency(context.Background(), key, limit); err != nil {
				rejected.Add(1)
				return
			}
			acquired.Add(1)
			// 模拟短持有时长后释放
			store.ReleaseConcurrency(context.Background(), key)
		}()
	}
	wg.Wait()

	// 最终计数应归零（每个成功 acquire 都已 release）
	if v := concVal(t, rdb, key); v != 0 {
		t.Fatalf("after stress, concurrency val should be 0, got %d (acquired=%d rejected=%d)",
			v, acquired.Load(), rejected.Load())
	}
	if acquired.Load()+rejected.Load() != int64(total) {
		t.Fatalf("acquired+rejected=%d != total=%d", acquired.Load()+rejected.Load(), total)
	}
	// 健全性：limit=50 下应既有成功也有拒绝（纯顺序下成功≤total，并发下必有拒绝交替）
	// 不做强断言（miniredis 单线程串行化命令，拒绝数取决于调度），仅 log。
	t.Logf("stress done: acquired=%d rejected=%d", acquired.Load(), rejected.Load())
}

// TestStability_RPMStress_Concurrent_CountAccurate 并发 500 次 CheckRPM，limit=50。
// 断言：最终 ZSET 成员数 ≤ 50（limit 卡住），不超限、不重复计数错误。
func TestStability_RPMStress_Concurrent_CountAccurate(t *testing.T) {
	_, store, rdb := newStabilityStore(t)
	key := "test:rpm:stress"
	limit := 50
	total := 500

	var wg sync.WaitGroup
	var passed, blocked atomic.Int64
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.CheckRPM(context.Background(), key, limit); err != nil {
				blocked.Add(1)
			} else {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()

	cnt := rpmCount(t, rdb, key)
	if cnt > int64(limit) {
		t.Fatalf("RPM zset count %d exceeds limit %d (race bug)", cnt, limit)
	}
	if passed.Load() != int64(limit) {
		t.Errorf("expected exactly %d passed (limit), got %d", limit, passed.Load())
	}
	if passed.Load()+blocked.Load() != int64(total) {
		t.Errorf("passed+blocked=%d != total=%d", passed.Load()+blocked.Load(), total)
	}
	t.Logf("rpm stress: passed=%d blocked=%d zset=%d", passed.Load(), blocked.Load(), cnt)
}

// TestStability_Store_RedisDown_NoPanic 最小补：Redis 关闭下 store 三方法均不 panic。
func TestStability_Store_RedisDown_NoPanic(t *testing.T) {
	mr, store, _ := newStabilityStoreFastFail(t)
	mr.Close()
	for i := 0; i < 10; i++ {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("panic with redis down: %v", rec)
				}
			}()
			_ = store.CheckRPM(context.Background(), "k", 10)
			_ = store.AcquireConcurrency(context.Background(), "k", 10)
			store.ReleaseConcurrency(context.Background(), "k")
		}()
	}
}

// TestStability_GroupLayer_ParsesModelFromBody 验证 BUG-2 修复：extractModelParam
// 从真实 request body 的 model 字段解析出组名，使 group 层配额在生产链路下生效。
//
// 修复前：extractModelParam 读 c.Get("request_model")（无人 set）+ c.Query("model")
//（body 里的 model 不在 query）→ groupName 恒空 → group 层 check 被跳过。
// 修复后：优先复用 trace_req_body / 回退自行读 body 解析 model，group 层正常检查。
//
// 本用例**不再** c.Set("request_model", ...)，改用真实 JSON body 携带 model 字段，
// 覆盖既有 FourLayers 测试（line 318 通过 set 绕过）所遗漏的生产路径。
func TestStability_GroupLayer_ParsesModelFromBody(t *testing.T) {
	_, mw, holder := setupMiddlewareTest(t)
	holder.Set(&quota.QuotaSnapshot{
		DefaultUserRPM: 100, DefaultUserMaxConcurrency: 100,
		Users:       map[string]quota.LayerLimits{},
		Groups:      map[string]quota.LayerLimits{"gpt-4": {RPM: 2, MaxConcurrency: 0}},
		Models:      map[string]quota.LayerLimits{},
	})

	id := identityOf("u-grp", "", "personal")
	r := gin.New()
	// 注意：不 set request_model，模拟真实生产链路（仅依赖 body 解析）
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("identity", id)
	}, mw.Handler(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`

	// 前 2 次：未触 group 上限 → 200
	for i := 0; i < 2; i++ {
		w := doReqWithBody(r, body)
		if w.Code != http.StatusOK {
			t.Fatalf("req %d should pass (group limit=2 not yet hit), got %d (body=%s)", i+1, w.Code, w.Body.String())
		}
	}

	// 第 3 次：group RPM=2 满 → 429 layer=group（证明 group 层 check 真实生效）
	w := doReqWithBody(r, body)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("req 3 should be 429 (group RPM hit), got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("req 3 response should be one valid JSON document: %v; body=%q", err, w.Body.String())
	}
	errObj, _ := resp["error"].(map[string]interface{})
	details, _ := errObj["details"].(map[string]interface{})
	layer, _ := details["layer"].(string)
	if layer != "group" {
		t.Fatalf("req 3 should trip group layer, got layer=%v (full=%v)", layer, errObj)
	}

	// subject 层应计 3（3 次都过了 subject 才在 group 拒），group 层应计 2（第 3 次在 group 拒）
	if c := rpmCount(t, mw.store.rdb, RPMKey("user", "u-grp", "", "")); c != 3 {
		t.Errorf("subject layer should count 3 attempts, got %d", c)
	}
	if c := rpmCount(t, mw.store.rdb, RPMKey("user", "u-grp", "group", "gpt-4")); c != 2 {
		t.Errorf("group layer should count 2 (3rd rejected at group), got %d", c)
	}
}
