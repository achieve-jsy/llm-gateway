package quota

import (
	"context"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupMiniRedis(t *testing.T) (*miniredis.Miniredis, *RedisStore) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, NewRedisStore(rdb)
}

// newStoreWithClient 起一个 miniredis 并同时返回底层 redis.Client，
// 供需要直接构造「旧实现语义」（裸 INCR/DECR）做对照实验的用例使用。
func newStoreWithClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
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
	return mr, rdb
}

func TestCheckRPM_Limit0_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	err := store.CheckRPM(context.Background(), "test:key", 0)
	if err != nil {
		t.Errorf("limit=0 should pass, got %v", err)
	}
}

func TestCheckRPM_UnderLimit_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	err := store.CheckRPM(context.Background(), "test:key", 10)
	if err != nil {
		t.Errorf("under limit should pass, got %v", err)
	}
}

func TestCheckRPM_AtLimit_Fails(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:key"
	// Fill up to limit
	for i := 0; i < 5; i++ {
		store.CheckRPM(context.Background(), key, 5)
	}
	err := store.CheckRPM(context.Background(), key, 5)
	if err == nil {
		t.Error("at limit should fail")
	}
}

func TestAcquireConcurrency_Limit0_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	leaseID, err := store.AcquireConcurrency(context.Background(), "test:conc", 0)
	if err != nil {
		t.Errorf("limit=0 should pass, got %v", err)
	}
	if leaseID != "" {
		t.Errorf("limit=0 must not allocate a lease, got %q", leaseID)
	}
}

func TestAcquireConcurrency_UnderLimit_Passes(t *testing.T) {
	_, store := setupMiniRedis(t)
	leaseID, err := store.AcquireConcurrency(context.Background(), "test:conc", 5)
	if err != nil {
		t.Errorf("under limit should pass, got %v", err)
	}
	if leaseID == "" {
		t.Error("successful acquire must return a non-empty lease id")
	}
}

func TestAcquireConcurrency_AtLimit_Fails(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc"
	// Fill up to limit
	for i := 0; i < 3; i++ {
		if _, err := store.AcquireConcurrency(context.Background(), key, 3); err != nil {
			t.Fatalf("acquire %d should pass, got %v", i+1, err)
		}
	}
	if _, err := store.AcquireConcurrency(context.Background(), key, 3); err == nil {
		t.Error("at limit should fail")
	}
}

// TestAcquireConcurrency_DistinctLeaseIDs 每次获取都必须生成互不相同的租约 id，
// 否则 ZSET member 会互相覆盖，导致并发数被少算（超发）。
func TestAcquireConcurrency_DistinctLeaseIDs(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc:distinct"
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		leaseID, err := store.AcquireConcurrency(context.Background(), key, 10)
		if err != nil {
			t.Fatalf("acquire %d: %v", i+1, err)
		}
		if seen[leaseID] {
			t.Fatalf("duplicate lease id on acquire %d: %q", i+1, leaseID)
		}
		seen[leaseID] = true
	}
}

func TestAcquireConcurrency_ReleaseThenAcquire(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc"
	// Fill to limit，记录各租约 id
	leases := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		leaseID, err := store.AcquireConcurrency(context.Background(), key, 2)
		if err != nil {
			t.Fatalf("acquire %d: %v", i+1, err)
		}
		leases = append(leases, leaseID)
	}
	// At limit, should fail
	if _, err := store.AcquireConcurrency(context.Background(), key, 2); err == nil {
		t.Error("at limit should fail")
	}
	// Release one（按租约 id 精确释放）
	store.ReleaseConcurrency(context.Background(), key, leases[0])
	// Now should pass
	if _, err := store.AcquireConcurrency(context.Background(), key, 2); err != nil {
		t.Errorf("after release should pass, got %v", err)
	}
}

// TestReleaseConcurrency_ByLeaseID_NoCrossRelease 释放只能移除自己的租约：
// 释放一个不存在的 lease id 不得影响任何在途租约。
func TestReleaseConcurrency_ByLeaseID_NoCrossRelease(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc:no-cross"
	leaseID, err := store.AcquireConcurrency(context.Background(), key, 1)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// 释放一个从未分配过的租约 id → 不得移除他人槽位
	store.ReleaseConcurrency(context.Background(), key, "not-a-real-lease")
	if _, err := store.AcquireConcurrency(context.Background(), key, 1); err == nil {
		t.Fatal("foreign lease id release must not free the only slot")
	}
	// 正确的租约 id 释放后才腾出槽位
	store.ReleaseConcurrency(context.Background(), key, leaseID)
	if _, err := store.AcquireConcurrency(context.Background(), key, 1); err != nil {
		t.Fatalf("release by own lease id should free the slot, got %v", err)
	}
}

func TestRPMKey(t *testing.T) {
	tests := []struct {
		subjectType, subjectID, layer, resourceID, expected string
	}{
		{"user", "u1", "", "", "quota:rpm:user:u1"},
		{"service_key", "k1", "", "", "quota:rpm:service_key:k1"},
		{"user", "u1", "group", "gpt-4", "quota:rpm:user:u1:group:gpt-4"},
		{"user", "u1", "model", "model-uuid", "quota:rpm:user:u1:model:model-uuid"},
	}
	for _, tt := range tests {
		got := RPMKey(tt.subjectType, tt.subjectID, tt.layer, tt.resourceID)
		if got != tt.expected {
			t.Errorf("RPMKey(%s,%s,%s,%s) = %s, want %s", tt.subjectType, tt.subjectID, tt.layer, tt.resourceID, got, tt.expected)
		}
	}
}

func TestConcurrencyKey(t *testing.T) {
	tests := []struct {
		subjectType, subjectID, layer, resourceID, expected string
	}{
		// 新格式期望值一律由 corequota 的 helper 构造，测试内不硬编码新前缀
		{"user", "u1", "", "", corequota.ConcurrencyKey("user", "u1", "", "")},
		{"service_key", "k1", "group", "gpt-4", corequota.ConcurrencyKey("service_key", "k1", "group", "gpt-4")},
	}
	for _, tt := range tests {
		got := ConcurrencyKey(tt.subjectType, tt.subjectID, tt.layer, tt.resourceID)
		if got != tt.expected {
			t.Errorf("ConcurrencyKey(%s,%s,%s,%s) = %s, want %s", tt.subjectType, tt.subjectID, tt.layer, tt.resourceID, got, tt.expected)
		}
	}
}

// TestConcurrencyKey_UsesNewPrefix 并发键必须使用与旧实现不同的新前缀：
// 灰度期新旧代码共存时，旧代码的 INCR/DECR 不得打到新实现的 ZSET 键上（否则 WRONGTYPE）。
func TestConcurrencyKey_UsesNewPrefix(t *testing.T) {
	key := ConcurrencyKey("user", "u1", "", "")
	if !strings.HasPrefix(key, corequota.ConcurrencyKeyPrefix) {
		t.Fatalf("concurrency key %q must use new prefix %q", key, corequota.ConcurrencyKeyPrefix)
	}
	// 旧形态来源收敛到 corequota.LegacyConcurrencyKeySubject，避免测试里散落旧前缀字面量
	legacyKey := corequota.LegacyConcurrencyKeySubject("user", "u1")
	if strings.HasPrefix(key, legacyKey) {
		t.Fatalf("concurrency key %q must not collide with legacy key %q", key, legacyKey)
	}
}

func TestCheckRPM_WindowExpiry_FastForward(t *testing.T) {
	mr, store := setupMiniRedis(t)
	key := "test:rpm:window"

	// Fill up to limit (5 requests)
	for i := 0; i < 5; i++ {
		err := store.CheckRPM(context.Background(), key, 5)
		if err != nil {
			t.Fatalf("request %d should pass, got %v", i+1, err)
		}
	}

	// 6th request should fail (at limit)
	err := store.CheckRPM(context.Background(), key, 5)
	if err == nil {
		t.Fatal("6th request should fail (at limit)")
	}

	// Fast-forward time by 61 seconds (window is 60s)
	mr.FastForward(61 * time.Second)

	// After window expires, request should pass again
	err = store.CheckRPM(context.Background(), key, 5)
	if err != nil {
		t.Errorf("after window expiry, request should pass, got %v", err)
	}
}

func TestCheckRPM_RedisDown_FailOpen(t *testing.T) {
	mr, store := setupMiniRedis(t)

	// Close miniredis to simulate Redis failure
	mr.Close()

	// RPM check should fail-open (allow request)
	err := store.CheckRPM(context.Background(), "test:key", 10)
	if err != nil {
		t.Errorf("Redis down should fail-open (pass), got error: %v", err)
	}
}

func TestAcquireConcurrency_RedisDown_FailOpen(t *testing.T) {
	mr, store := setupMiniRedis(t)

	// Close miniredis to simulate Redis failure
	mr.Close()

	// Concurrency acquire should fail-open (allow request)
	leaseID, err := store.AcquireConcurrency(context.Background(), "test:conc", 5)
	if err != nil {
		t.Errorf("Redis down should fail-open (pass), got error: %v", err)
	}
	// fail-open 时没有真实占用，不得伪造租约（释放路径据此跳过）
	if leaseID != "" {
		t.Errorf("fail-open must not allocate a lease, got %q", leaseID)
	}
}

// TestAcquireConcurrency_LeaseEntryAndKeyTTL 校验租约式存储的两个不变量：
//  1. 每个槽位是 ZSET 的一条 member（值为租约 id），score 为该租约的到期时间戳，
//     且到期时间 ≈ now + concurrencyLease；
//  2. key 级 TTL 只作兜底，且长于租约时长（保证集合里有未过期租约时键不会先过期）。
func TestAcquireConcurrency_LeaseEntryAndKeyTTL(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc:lease-shape"

	before := time.Now()
	leaseID, err := store.AcquireConcurrency(context.Background(), key, 10)
	if err != nil {
		t.Fatalf("acquire should pass, got %v", err)
	}
	after := time.Now()

	// 1) member 值 = 返回的租约 id
	members, err := store.rdb.ZRange(context.Background(), key, 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange: %v", err)
	}
	if len(members) != 1 || members[0] != leaseID {
		t.Fatalf("lease set should contain exactly the returned lease id, got %v (lease=%q)", members, leaseID)
	}

	// 2) score = 到期时间戳 ≈ now + concurrencyLease
	score, err := store.rdb.ZScore(context.Background(), key, leaseID).Result()
	if err != nil {
		t.Fatalf("zscore: %v", err)
	}
	expireAt := time.UnixMilli(int64(score))
	// 纳秒 → 毫秒截断会带来最多 1ms 偏差，故含 5ms 容差
	lo := before.Add(concurrencyLease).Add(-5 * time.Millisecond)
	hi := after.Add(concurrencyLease).Add(5 * time.Millisecond)
	if expireAt.Before(lo) || expireAt.After(hi) {
		t.Errorf("lease score should be acquire time + %v (window %v~%v), got %v", concurrencyLease, lo, hi, expireAt)
	}
	if concurrencyLease <= 300*time.Second {
		t.Errorf("lease duration %v must exceed the 300s upstream timeout", concurrencyLease)
	}

	// 3) key 级兜底 TTL 长于租约时长
	ttl := store.rdb.TTL(context.Background(), key).Val()
	if ttl <= concurrencyLease {
		t.Errorf("key fallback TTL %v must be longer than the lease duration %v", ttl, concurrencyLease)
	}
}

// TestAcquireConcurrency_LeaseExpiry_SelfHeals 本缺陷的核心回归用例：
// 模拟「获取后归还永久丢失」（15s 优雅退出强断长流式请求导致 DECR 丢失），
// 断言虚高的占用会随租约到期自动消失，用户不会被计数钉死在限额上。
//
// 该用例在旧的 INCR/DECR + 成功获取即续期实现下必然失败：
// 旧实现的键只有在 TTL 到期且期间无人续期时才清理，而残留的 INCR 计数不会逐条目过期。
func TestAcquireConcurrency_LeaseExpiry_SelfHeals(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc:self-heal"

	// 注入可控时钟：租约判定基于脚本入参 now 与成员 score 的比较，
	// 只有推进这个时钟才能真实覆盖「到期自愈」路径（无需真等 6 分钟）。
	fakeNow := time.Now()
	store.now = func() time.Time { return fakeNow }

	// 占满 limit=3，且全部不归还（模拟三次归还在进程被强杀时丢失）
	for i := 0; i < 3; i++ {
		if _, err := store.AcquireConcurrency(context.Background(), key, 3); err != nil {
			t.Fatalf("acquire %d should pass, got %v", i+1, err)
		}
	}
	// 限额被占用 → 新请求被拒（这就是用户看到的 429）
	if _, err := store.AcquireConcurrency(context.Background(), key, 3); err == nil {
		t.Fatal("limit is fully occupied by leaked leases, acquire should be rejected")
	}

	// 到期前 1 秒：仍应被拒（合法长请求的槽位不会提前被回收）
	fakeNow = fakeNow.Add(concurrencyLease - time.Second)
	if _, err := store.AcquireConcurrency(context.Background(), key, 3); err == nil {
		t.Fatal("leases must not expire before their deadline: acquire should still be rejected")
	}

	// 时间推进到租约到期之后（相当于真实世界等待 concurrencyLease）
	fakeNow = fakeNow.Add(2 * time.Second)

	// 自愈：丢失归还的槽位自动过期，计数回落，请求恢复放行
	if _, err := store.AcquireConcurrency(context.Background(), key, 3); err != nil {
		t.Fatalf("after lease expiry the slot must self-heal, got %v", err)
	}
}

// TestAcquireConcurrency_Concurrent_NoOverIssue 并发获取不超发：
// 100 个 goroutine 抢 limit=10，成功数必须恰好 10（Lua 内「清理+计数+占用」原子）。
func TestAcquireConcurrency_Concurrent_NoOverIssue(t *testing.T) {
	_, store := setupMiniRedis(t)
	key := "test:conc:over-issue"
	const limit = 10
	const total = 100

	var ok, rejected atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.AcquireConcurrency(context.Background(), key, limit); err != nil {
				rejected.Add(1)
				return
			}
			ok.Add(1)
		}()
	}
	wg.Wait()

	if ok.Load() != limit {
		t.Fatalf("exactly %d acquires must succeed, got %d (rejected=%d)", limit, ok.Load(), rejected.Load())
	}
	if rejected.Load() != total-limit {
		t.Fatalf("expected %d rejections, got %d", total-limit, rejected.Load())
	}
}

// TestReleaseConcurrency_AllReleased_KeyRemoved 全部按租约释放后键被清理，不留垃圾键。
func TestReleaseConcurrency_AllReleased_KeyRemoved(t *testing.T) {
	mr, store := setupMiniRedis(t)
	key := "test:conc:cleanup"

	leases := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		leaseID, err := store.AcquireConcurrency(context.Background(), key, 3)
		if err != nil {
			t.Fatalf("acquire %d: %v", i+1, err)
		}
		leases = append(leases, leaseID)
	}

	// 释放 2 条：键仍存在且余 1 条
	store.ReleaseConcurrency(context.Background(), key, leases[0])
	store.ReleaseConcurrency(context.Background(), key, leases[1])
	if got, err := store.rdb.ZCard(context.Background(), key).Result(); err != nil || got != 1 {
		t.Fatalf("after 2 releases zcard should be 1, got %d (err=%v)", got, err)
	}

	// 释放最后一条：键被 DEL
	store.ReleaseConcurrency(context.Background(), key, leases[2])
	if mr.Exists(key) {
		t.Fatalf("key %s should be deleted after all leases released", key)
	}
	if store.rdb.Exists(context.Background(), key).Val() != 0 {
		t.Fatalf("key %s should not exist after all leases released", key)
	}
}

// TestLegacyIncDecCounterPinsForever 对照实验（新旧语义判别力证明）：
//
// 用同一个 Redis 实例、同一个可控时钟，分别跑「旧实现」的裸 INCR/DECR 计数与
// 「新实现」的租约式计数，两者都模拟「获取后归还永久丢失」，然后推进时间：
//   - 旧实现：只要用户持续请求，成功获取就不断续期 TTL，计数被钉死在限额上，
//     时间推进整整一个租约时长后仍然恒拒（这正是生产事故：用户被持久锁死）；
//   - 新实现：租约逐条目到期，自动腾出槽位。
//
// 该用例证明 TestAcquireConcurrency_LeaseExpiry_SelfHeals /
// TestStability_ConcurrencyLease_SelfHealsOnLostRelease 真正测的是本缺陷本身，
// 而不是任何实现都能过的平凡断言。旧实现下「自愈」断言必然失败。
func TestLegacyIncDecCounterPinsForever(t *testing.T) {
	_, rdb := newStoreWithClient(t)
	const limit = 2

	// 可控时钟：让「推进时间」这一动作在可见时长内完成
	fakeNow := time.Now()

	// ── 旧实现：INCR 计数 + 成功获取即 EXPIRE 续期，归还靠 DECR ──
	legacyKey := corequota.LegacyConcurrencyKeySubject("user", "legacy-pinned") // 旧形态（灰度期旧代码写的键）
	legacyAcquire := func() bool {
		val := rdb.Incr(context.Background(), legacyKey).Val()
		if val > limit {
			rdb.Decr(context.Background(), legacyKey)
			return false
		}
		// 旧实现的核心缺陷：每次成功获取都续期，虚高值在用户活跃期间永不清理
		rdb.Expire(context.Background(), legacyKey, concurrencyKeyTTL)
		return true
	}
	for i := 0; i < limit; i++ {
		if !legacyAcquire() {
			t.Fatalf("legacy acquire %d should pass", i+1)
		}
	}

	// ── 新实现：租约式 ZSET ──
	newStore := NewRedisStore(rdb)
	newStore.now = func() time.Time { return fakeNow } // 仅注入时钟，不改变计数语义
	newKey := ConcurrencyKey("user", "legacy-pinned", "", "")
	for i := 0; i < limit; i++ {
		if _, err := newStore.AcquireConcurrency(context.Background(), newKey, limit); err != nil {
			t.Fatalf("lease acquire %d: %v", i+1, err)
		}
	}

	// 两者此刻都被「丢失归还」占满，新请求均被拒
	if legacyAcquire() {
		t.Fatal("legacy: limit is full right after leak, should reject")
	}
	if _, err := newStore.AcquireConcurrency(context.Background(), newKey, limit); err == nil {
		t.Fatal("lease: limit is full right after leak, should reject")
	}

	// 推进时间：整整一个租约时长之后
	fakeNow = fakeNow.Add(concurrencyLease + time.Second)

	// 旧实现：用户仍在活跃请求 → DECR 不生效但 INCR 成功就续期，计数被钉死在 limit
	// （超过旧实现 10 分钟 TTL 也依旧如此——活跃即续期）
	if legacyAcquire() {
		t.Fatal("legacy INCR/DECR counter unexpectedly recovered — 对照前提不成立")
	}
	if got := rdb.Get(context.Background(), legacyKey).Val(); got != "2" {
		t.Fatalf("legacy counter should stay pinned at 2, got %q", got)
	}

	// 新实现：丢失归还的槽位到期自动腾出 → 恢复放行
	if _, err := newStore.AcquireConcurrency(context.Background(), newKey, limit); err != nil {
		t.Fatalf("lease counter must self-heal after expiry, got %v", err)
	}

	t.Logf("对照结论：旧 INCR/DECR 计数钉死在 %s；租约式计数在 %v 后自愈", rdb.Get(context.Background(), legacyKey).Val(), concurrencyLease)
}

// TestAcquireConcurrency_NewAcquireDoesNotExtendOldLease 自愈权威唯一性回归：
// 新 acquire 绝不允许延长（续期）已有条目的有效期 —— 这是原缺陷无法自愈的根因
// （旧实现在通过分支无条件 EXPIRE，用户只要偶尔成功一次虚高计数就被无限续命）。
//
// 判别力对照：左侧（对照组）演示「续期式」旧语义把已有占用续命；右侧（新实现）
// 断言新 acquire 既不改写已有成员的 score，也会在旧成员 deadline 到达后由
// ZREMRANGEBYSCORE 清理。两组使用各自独立的时钟，避免相互干扰。
func TestAcquireConcurrency_NewAcquireDoesNotExtendOldLease(t *testing.T) {
	// ── 对照组：续期式语义（旧实现）──
	// 旧实现是「裸计数 + 成功获取即 EXPIRE 续期」，这里用同一 Redis 上的
	// 「把已有成员 score 重写为 now+lease」来等价复现其对占用时长的影响。
	t.Run("续期式旧语义会把旧占用续命", func(t *testing.T) {
		_, store := setupMiniRedis(t)
		base := time.Now()
		legacyNow := base
		store.now = func() time.Time { return legacyNow }
		key := "test:conc:extended"
		leaseID := "legacy-lease-A"

		seed := func(at time.Time) {
			t.Helper()
			z := redis.Z{Score: float64(at.Add(concurrencyLease).UnixMilli()), Member: leaseID}
			if err := store.rdb.ZAdd(context.Background(), key, z).Err(); err != nil {
				t.Fatalf("zadd: %v", err)
			}
		}
		seed(base)
		// 用户临近到期前又成功一次 → 旧实现续期，旧占用被续命
		legacyNow = base.Add(concurrencyLease - time.Second)
		seed(legacyNow)

		// 越过原 deadline 后，旧占用仍然在（这就是「被无限续命、永不清理」）
		if _, err := store.rdb.ZScore(context.Background(), key, leaseID).Result(); err != nil {
			t.Fatalf("re-armed lease should still exist: %v", err)
		}
	})

	// ── 新实现：新 acquire 不延长旧条目 ──
	_, store := setupMiniRedis(t)
	key := "test:conc:no-extend"
	base := time.Now()
	fakeNow := base
	store.now = func() time.Time { return fakeNow }

	leaseA, err := store.AcquireConcurrency(context.Background(), key, 5)
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	scoreABefore, err := store.rdb.ZScore(context.Background(), key, leaseA).Result()
	if err != nil {
		t.Fatalf("zscore A: %v", err)
	}

	// 临近 A 到期时再来一次成功获取（新成员 B）
	fakeNow = base.Add(concurrencyLease - time.Second)
	if _, err := store.AcquireConcurrency(context.Background(), key, 5); err != nil {
		t.Fatalf("acquire B: %v", err)
	}
	scoreAAfter, err := store.rdb.ZScore(context.Background(), key, leaseA).Result()
	if err != nil {
		t.Fatalf("zscore A after B: %v", err)
	}
	if scoreAAfter != scoreABefore {
		t.Fatalf("new acquire must NOT extend an existing lease: score A %v -> %v", scoreABefore, scoreAAfter)
	}

	// 越过 A 的 deadline：A 应被 ZREMRANGEBYSCORE 清理（不被续命）
	fakeNow = base.Add(concurrencyLease + time.Second)
	if _, err := store.AcquireConcurrency(context.Background(), key, 5); err != nil {
		t.Fatalf("acquire after A deadline: %v", err)
	}
	if sc, err := store.rdb.ZScore(context.Background(), key, leaseA).Result(); err != redis.Nil {
		t.Fatalf("expired lease A should have been purged by ZREMRANGEBYSCORE, got score=%v err=%v", sc, err)
	}
}

// TestCheckRPM_LuaFalseReturn_MustNotBeFailOpen RPM 侧同源缺陷形状：
// rpmCheckScript 返回数值 0/1，但若有人误改成返回 Lua false，空回复会解码为 redis.Nil，
// 与"Redis 故障"无法区分。同样必须显式报错，绝不能 fail-open 放行。
func TestCheckRPM_LuaFalseReturn_MustNotBeFailOpen(t *testing.T) {
	_, rdb := newStoreWithClient(t)
	store := NewRedisStore(rdb)
	ctx := context.Background()
	key := "test:rpm:lua-false-shape"

	orig := rpmCheckScript
	rpmCheckScript = redis.NewScript("return false")
	t.Cleanup(func() { rpmCheckScript = orig })

	if err := store.CheckRPM(ctx, key, 10); err == nil {
		t.Fatal("Lua false（空回复）必须被判为异常，绝不能 fail-open 放行")
	} else if !strings.Contains(err.Error(), "protocol_error") {
		t.Fatalf("应为协议异常错误，实际 %v", err)
	} else {
		t.Logf("缺陷形状被正确拒绝: %v", err)
	}
}

// TestReleaseConcurrency_ReturnCode_DistinguishesLostRelease 固定释放脚本的三态返回值语义，
// 并确认「已丢失归还」（返回 0）与正常归还（1/2）可区分——0 是 lost-release 的观测信号。
func TestReleaseConcurrency_ReturnCode_DistinguishesLostRelease(t *testing.T) {
	_, store := setupMiniRedis(t)
	ctx := context.Background()
	key := "test:conc:release-return-code"

	raw := func(leaseID string) int64 {
		v, err := concurrencyReleaseScript.Run(ctx, store.rdb, []string{key}, leaseID).Result()
		if err != nil {
			t.Fatalf("释放脚本解码失败（返回值应为整数）: %v", err)
		}
		n, ok := v.(int64)
		if !ok {
			t.Fatalf("释放脚本返回值类型应为 int64，实际 %T", v)
		}
		return n
	}

	lease, err := store.AcquireConcurrency(ctx, key, 5)
	if err != nil {
		t.Fatalf("获取租约: %v", err)
	}
	// 正常归还且集合仍非空 → 1（再加一个占用，确保删掉一个后集合不空）
	lease2, err := store.AcquireConcurrency(ctx, key, 5)
	if err != nil {
		t.Fatalf("获取第 2 个租约: %v", err)
	}
	if n := raw(lease); n != 1 {
		t.Fatalf("删除后集合非空应返回 1，实际 %d", n)
	}
	// 删除最后一个 → 2（键被 DEL）
	if n := raw(lease2); n != 2 {
		t.Fatalf("删除最后一个成员应返回 2（键已删），实际 %d", n)
	}
	// 已丢失归还：重复释放同一租约 → 0（这就是 lost-release 的观测信号）
	if n := raw(lease2); n != 0 {
		t.Fatalf("重复释放（租约已不存在）应返回 0，实际 %d", n)
	}
	// 对不存在的租约释放也返回 0，且不得影响他人槽位
	other, err := store.AcquireConcurrency(ctx, key, 5)
	if err != nil {
		t.Fatalf("获取租约: %v", err)
	}
	if n := raw("no-such-lease"); n != 0 {
		t.Fatalf("释放不存在的租约应返回 0，实际 %d", n)
	}
	if got := concVal(t, store.rdb, key); got != 1 {
		t.Fatalf("释放不存在的租约不得影响他人槽位，ZCARD=%d want 1", got)
	}
	// ReleaseConcurrency（丢弃形态）对该租约仍应正常释放
	store.ReleaseConcurrency(ctx, key, other)
	if got := concVal(t, store.rdb, key); got != 0 {
		t.Fatalf("ReleaseConcurrency 应清空槽位，ZCARD=%d", got)
	}
}
