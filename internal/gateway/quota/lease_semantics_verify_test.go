package quota

// 租约式并发计数的语义与回归断言集（纯断言，不改动任何业务实现代码）。
//
// 覆盖：超限拒绝不增长 ZSET（显式断言而非结构性推论）、limit 边界、空 lease id 双重合法语义、
// 业务超限 ≠ fail-open（并发 + RPM 两条路径，含计量装置自检）、fail-open 不阻塞、
// 长请求跨越 300s 上游超时不被误判过期、灰度新旧前缀不 WRONGTYPE。

import (
	"context"
	"fmt"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestVerifierT3_RejectsDoNotGrowZSet 显式断言「超限拒绝不给 ZSET 增加成员」：
// 每被拒一次就断言一次 ZCARD 恒等于 limit，而不是依赖「拒绝分支在 ZADD 之前 return」
// 这一结构性推论。
func TestVerifierT3_RejectsDoNotGrowZSet(t *testing.T) {
	_, store := setupMiniRedis(t)
	ctx := context.Background()
	key := ConcurrencyKey("user", "u-t3-zcard", "", "")
	const limit = 3

	assertZCard := func(want int64, stage string) {
		t.Helper()
		got, err := store.rdb.ZCard(ctx, key).Result()
		if err != nil {
			t.Fatalf("%s: zcard: %v", stage, err)
		}
		if got != want {
			t.Fatalf("%s: zcard 必须恒为 %d，实际 %d", stage, want, got)
		}
	}

	leases := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		id, err := store.AcquireConcurrency(ctx, key, limit)
		if err != nil {
			t.Fatalf("第 %d 次获取（limit 内）应放行，got %v", i+1, err)
		}
		if id == "" {
			t.Fatalf("第 %d 次获取必须返回非空 lease id", i+1)
		}
		leases = append(leases, id)
	}
	assertZCard(limit, "填满到 limit 后")

	const rejects = 25
	for i := 0; i < rejects; i++ {
		id, err := store.AcquireConcurrency(ctx, key, limit)
		if err == nil {
			t.Fatalf("第 %d 次超限获取必须返回非 nil error（这是缺陷形态的关键断言）", i+1)
		}
		if id != "" {
			t.Fatalf("第 %d 次超限获取不得返回 lease id，got %q", i+1, id)
		}
		assertZCard(limit, fmt.Sprintf("第 %d 次拒绝后", i+1))
	}

	members, err := store.rdb.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("zrange: %v", err)
	}
	if len(members) != limit {
		t.Fatalf("ZSET 必须恰好 %d 个成员，实际 %d", limit, len(members))
	}
	seen := make(map[string]bool, len(members))
	for _, m := range members {
		seen[m] = true
	}
	for _, id := range leases {
		if !seen[id] {
			t.Fatalf("成功获取的租约 %q 不在 ZSET 中：%v", id, members)
		}
	}
	t.Logf("ZCARD 在 %d 次连续拒绝后稳定为 %d（未被拒绝撑大）", rejects, limit)
}

// TestVerifierT3_LimitBoundary 边界语义：恰好 limit 个放行且 lease id 互异；
// 第 limit+1 个必须同时满足 err != nil 与 lease == ""；limit<=0（0 与负数）不限且不写键。
func TestVerifierT3_LimitBoundary(t *testing.T) {
	_, store := setupMiniRedis(t)
	ctx := context.Background()
	key := ConcurrencyKey("user", "u-t3-boundary", "", "")
	const limit = 5

	ids := make(map[string]bool, limit)
	for i := 0; i < limit; i++ {
		id, err := store.AcquireConcurrency(ctx, key, limit)
		if err != nil {
			t.Fatalf("第 %d 次（== limit）应放行，got %v", i+1, err)
		}
		if id == "" {
			t.Fatalf("第 %d 次 lease id 不得为空", i+1)
		}
		if ids[id] {
			t.Fatalf("lease id %q 重复 —— 成员唯一性是 ZSET 按条目计数正确的前提", id)
		}
		ids[id] = true
	}

	id, err := store.AcquireConcurrency(ctx, key, limit)
	if err == nil {
		t.Fatalf("第 %d 次（limit+1）必须被拒", limit+1)
	}
	if id != "" {
		t.Fatalf("被拒的获取不得返回 lease id，got %q", id)
	}
	t.Logf("limit=%d：前 %d 个全部放行且 lease id 互异；第 %d 个被拒（err=%v, lease=%q）",
		limit, limit, limit+1, err, id)

	for _, l := range []int{0, -1} {
		k := ConcurrencyKey("user", fmt.Sprintf("u-t3-nolimit-%d", l+2), "", "")
		id, err := store.AcquireConcurrency(ctx, k, l)
		if err != nil {
			t.Fatalf("limit=%d 应不限（nil error），got %v", l, err)
		}
		if id != "" {
			t.Fatalf("limit=%d 不得分配租约，got %q", l, id)
		}
		if n := store.rdb.Exists(ctx, k).Val(); n != 0 {
			t.Fatalf("limit=%d 不得创建键，exists=%d", l, n)
		}
	}
}

// TestVerifierT3_EmptyLeaseID_DualLegalSemantics 空 lease id 的两条合法路径：
// 「不限」(limit<=0) 与「fail-open」(Redis 故障) 都返回 ("", nil)，含义是"不持有租约"；
// 两条路径都不得在 ZSET 留下成员；对空 id 调用 ReleaseConcurrency 必须无副作用、不 panic。
func TestVerifierT3_EmptyLeaseID_DualLegalSemantics(t *testing.T) {
	ctx := context.Background()

	// 路径 1：不限
	_, store := setupMiniRedis(t)
	unlimitedKey := ConcurrencyKey("user", "u-t3-unlimited", "", "")
	id, err := store.AcquireConcurrency(ctx, unlimitedKey, 0)
	if err != nil || id != "" {
		t.Fatalf("不限路径必须返回 (\"\", nil)，got (%q, %v)", id, err)
	}
	if n := store.rdb.Exists(ctx, unlimitedKey).Val(); n != 0 {
		t.Fatalf("不限路径不得在 ZSET 留下成员，exists=%d", n)
	}

	// 空 id 释放：在一个真实有成员的键上调用，不得误删他人槽位
	fullKey := ConcurrencyKey("user", "u-t3-empty-release", "", "")
	const limit = 2
	for i := 0; i < limit; i++ {
		if _, err := store.AcquireConcurrency(ctx, fullKey, limit); err != nil {
			t.Fatalf("填充第 %d 次：%v", i+1, err)
		}
	}
	store.ReleaseConcurrency(ctx, fullKey, "") // 必须安全返回，不得 panic
	if got := store.rdb.ZCard(ctx, fullKey).Val(); got != limit {
		t.Fatalf("以空 lease id 释放不得触碰 ZSET：zcard=%d want %d", got, limit)
	}

	// 路径 2：fail-open（Redis 故障）
	mrDown, storeDown := setupMiniRedis(t)
	mrDown.Close()
	downKey := ConcurrencyKey("user", "u-t3-failopen", "", "")
	id, err = storeDown.AcquireConcurrency(ctx, downKey, 5)
	if err != nil {
		t.Fatalf("Redis 故障必须 fail-open（nil error），got %v", err)
	}
	if id != "" {
		t.Fatalf("fail-open 不得伪造租约，got %q", id)
	}
	// 空 id 释放必须直接返回，不得向已故障的 Redis 发命令
	storeDown.ReleaseConcurrency(ctx, downKey, "")

	t.Log("两条合法路径（不限 / fail-open）均返回空 lease id 且不在 ZSET 留成员；空 id 释放无副作用")
}

// TestVerifierT3_OverLimitIsNotFailOpen_BusinessVsInfra 区分「业务超限」与「Redis 故障」：
// fail-open 计数在业务超限时必须不增长，只有真实 Redis 故障才允许增长。
// counter==0 这种空断言没有判别力，故先做"装置自检"：真实故障必须让它 = 1。
// 若装置不可用（同进程内 metricsOnce 已被其它用例消费），显式 Skip 并提示隔离执行，
// 以避免把一个测不出东西的断言伪装成通过。
func TestVerifierT3_OverLimitIsNotFailOpen_BusinessVsInfra(t *testing.T) {
	ctx := context.Background()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(ctx) })

	// ── 装置自检：真实 Redis 故障必须让 fail-open 计数 = 1 ──
	mrDown, storeDown := setupMiniRedis(t)
	mrDown.Close()
	if _, err := storeDown.AcquireConcurrency(ctx, ConcurrencyKey("user", "u-t3-fo", "", ""), 5); err != nil {
		t.Fatalf("Redis 故障应 fail-open，got %v", err)
	}
	if got := verifierT3FailOpenCount(t, reader, "concurrency"); got != 1 {
		t.Skipf("OTel 计量装置在本进程不可用（metricsOnce 已被其它用例消费，fail-open 计数=%d）；"+
			"请以 -run TestVerifierT3_OverLimitIsNotFailOpen 隔离执行本用例获得有效断言", got)
	}

	// ── 断言 1：并发业务超限不得增加 fail-open 计数 ──
	_, store := setupMiniRedis(t)
	key := ConcurrencyKey("user", "u-t3-biz", "", "")
	const limit = 2
	for i := 0; i < limit; i++ {
		if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
			t.Fatalf("填充第 %d 次：%v", i+1, err)
		}
	}
	for i := 0; i < 10; i++ {
		if _, err := store.AcquireConcurrency(ctx, key, limit); err == nil {
			t.Fatal("超限获取必须被拒")
		}
	}
	if got := verifierT3FailOpenCount(t, reader, "concurrency"); got != 1 {
		t.Fatalf("业务超限被误判为 fail-open：gate=concurrency 计数从 1 涨到 %d", got)
	}

	// ── 断言 2：RPM 业务超限同样不得增加 fail-open 计数 ──
	rpmKey := RPMKey("user", "u-t3-biz-rpm", "", "")
	if err := store.CheckRPM(ctx, rpmKey, 1); err != nil {
		t.Fatalf("首次 RPM 应放行，got %v", err)
	}
	if err := store.CheckRPM(ctx, rpmKey, 1); err == nil {
		t.Fatal("超限 RPM 必须被拒")
	}
	if got := verifierT3FailOpenCount(t, reader, "rpm"); got != 0 {
		t.Fatalf("RPM 业务超限被误判为 fail-open：gate=rpm 计数=%d（应为 0）", got)
	}

	t.Log("业务超限（并发 10 次 + RPM 1 次）均未污染 fail-open 计数；真实 Redis 故障计数=1")
}

// verifierT3FailOpenCount 读取 gateway_quota_fail_open_total 中指定 gate 的累计值。
func verifierT3FailOpenCount(t *testing.T, reader *sdkmetric.ManualReader, gate string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gateway_quota_fail_open_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if v, has := dp.Attributes.Value(attribute.Key("gate")); has && v.AsString() == gate {
					return dp.Value
				}
			}
		}
	}
	return 0
}

// TestVerifierT3_FailOpenDoesNotBlock 故障路径必须快速返回：获取与 RPM 检查都不得长时间阻塞。
func TestVerifierT3_FailOpenDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	mr, store := setupMiniRedis(t)
	mr.Close()

	const budget = 5 * time.Second

	start := time.Now()
	if _, err := store.AcquireConcurrency(ctx, ConcurrencyKey("user", "u-t3-slow", "", ""), 5); err != nil {
		t.Fatalf("并发获取应 fail-open，got %v", err)
	}
	acquireCost := time.Since(start)

	start = time.Now()
	if err := store.CheckRPM(ctx, RPMKey("user", "u-t3-slow", "", ""), 5); err != nil {
		t.Fatalf("RPM 检查应 fail-open，got %v", err)
	}
	rpmCost := time.Since(start)

	if acquireCost > budget || rpmCost > budget {
		t.Fatalf("fail-open 阻塞过久：acquire=%v rpm=%v（预算 %v）", acquireCost, rpmCost, budget)
	}
	t.Logf("fail-open 无长阻塞：acquire=%v rpm=%v（均 < %v）", acquireCost, rpmCost, budget)
}

// TestVerifierT3_LongRequestHeldAcrossUpstreamTimeout 长请求保活：
// 租约时长必须大于网关上游最长超时（internal/httpclient/client.go:70 的 defaultLongTimeout=300s），
// 否则合法长请求会在自己仍在飞的时候被判过期、槽位被他人抢占（实际并发超过限额）。
func TestVerifierT3_LongRequestHeldAcrossUpstreamTimeout(t *testing.T) {
	ctx := context.Background()
	const upstreamTimeout = 300 * time.Second

	if concurrencyLease <= upstreamTimeout {
		t.Fatalf("concurrencyLease=%v 必须严格大于上游最长超时 %v", concurrencyLease, upstreamTimeout)
	}
	if concurrencyKeyTTL <= concurrencyLease {
		t.Fatalf("key 级兜底 TTL=%v 必须大于租约时长 %v", concurrencyKeyTTL, concurrencyLease)
	}
	t.Logf("concurrencyLease=%v > defaultLongTimeout=%v（裕度 %v），key TTL=%v",
		concurrencyLease, upstreamTimeout, concurrencyLease-upstreamTimeout, concurrencyKeyTTL)

	_, store := setupMiniRedis(t)
	key := ConcurrencyKey("user", "u-t3-long", "", "")
	base := time.Now()
	fakeNow := base
	store.now = func() time.Time { return fakeNow }

	// limit=1：整个槽位被这一条长请求持有
	lease, err := store.AcquireConcurrency(ctx, key, 1)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if lease == "" {
		t.Fatal("lease 不得为空")
	}

	// 在上游超时窗口内（含 299s / 300s / 330s 三点）槽位必须仍被占用 → 第二请求被拒
	for _, el := range []time.Duration{
		upstreamTimeout - time.Second,
		upstreamTimeout,
		upstreamTimeout + 30*time.Second,
	} {
		fakeNow = base.Add(el)
		if _, err := store.AcquireConcurrency(ctx, key, 1); err == nil {
			t.Fatalf("在 +%v 时合法长请求的槽位被误判过期：第二请求被错误放行", el)
		}
	}

	// 超过租约时长后才允许自愈
	fakeNow = base.Add(concurrencyLease + time.Second)
	if _, err := store.AcquireConcurrency(ctx, key, 1); err != nil {
		t.Fatalf("租约到期后必须自愈，got %v", err)
	}
}

// TestVerifierT3_LostReleaseSelfHeals 本缺陷的核心性质（由验证者独立断言，不复用实现者的用例）：
// 模拟「获取后归还永久丢失」（15s 优雅退出强断长流式请求导致 DECR 丢失），
// 断言虚高的占用随租约到期自动回落、用户不会被永久钉死在限额上。
//
// 该用例的判别力由 verifier 的对照实验证明：在隔离副本中把实现还原为旧 INCR/DECR 语义后，
// 本用例必然失败（见 t3 报告中的两次运行对比）。
func TestVerifierT3_LostReleaseSelfHeals(t *testing.T) {
	ctx := context.Background()
	_, store := setupMiniRedis(t)
	key := ConcurrencyKey("user", "u-t3-leak", "", "")
	const limit = 3

	base := time.Now()
	fakeNow := base
	store.now = func() time.Time { return fakeNow }

	// 占满 limit，全部不释放（归还丢失）
	for i := 0; i < limit; i++ {
		if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
			t.Fatalf("第 %d 次获取应放行，got %v", i+1, err)
		}
	}
	// 用户侧此时看到的是 429
	if _, err := store.AcquireConcurrency(ctx, key, limit); err == nil {
		t.Fatal("限额被丢失归还占满时，新请求必须被拒")
	}

	// 到期前一刻：仍不得回收（合法长请求的槽位不能被提前抢走）
	fakeNow = base.Add(concurrencyLease - time.Second)
	if _, err := store.AcquireConcurrency(ctx, key, limit); err == nil {
		t.Fatal("租约未到期前不得回收槽位")
	}

	// 推进到租约到期之后：无需任何人工干预，虚高计数自动回落。
	// 下面这一行断言就是「自愈」性质本身；在旧 INCR/DECR 语义下必然失败（见 t3 报告对照实验）。
	fakeNow = base.Add(concurrencyLease + time.Second)
	if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
		t.Fatalf("租约到期后必须自愈，got %v", err)
	}
	// 3 条陈旧租约已被清理，只剩本次新获取的 1 条 —— 计数确实回落而非继续累积
	if got := store.rdb.ZCard(ctx, key).Val(); got != 1 {
		t.Fatalf("自愈后 ZCARD 应为 1（陈旧租约被清理），got %d", got)
	}
	t.Logf("丢失 %d 条归还后：到期前恒拒，推进 %v 后自动回落（ZCARD=%d），用户不再被永久锁死",
		limit, concurrencyLease, 1)
}

// TestVerifierT3_GrayCoexistence_NewPrefixAvoidsWrongType 灰度/蓝绿共存：
// 新实现必须使用新前缀，旧代码对旧前缀键执行 INCR/DECR 不与新实现互相 WRONGTYPE。
func TestVerifierT3_GrayCoexistence_NewPrefixAvoidsWrongType(t *testing.T) {
	ctx := context.Background()
	_, store := setupMiniRedis(t)
	rdb := store.rdb

	oldKey := "quota:conc:user:legacy-u1" // 旧实现（裸 INCR/DECR）的键形态
	newKey := ConcurrencyKey("user", "legacy-u1", "", "")

	if !strings.HasPrefix(newKey, corequota.ConcurrencyKeyPrefix) {
		t.Fatalf("新键 %q 必须使用新前缀 %q", newKey, corequota.ConcurrencyKeyPrefix)
	}
	if newKey == oldKey {
		t.Fatal("新旧键必须不同，否则灰度期新旧实现会互相 WRONGTYPE")
	}

	// 旧代码继续写旧键：INCR/DECR 正常
	if v := rdb.Incr(ctx, oldKey).Val(); v != 1 {
		t.Fatalf("旧前缀 INCR 应为 1，got %d", v)
	}
	if v := rdb.Decr(ctx, oldKey).Val(); v != 0 {
		t.Fatalf("旧前缀 DECR 应为 0，got %d", v)
	}

	// 新实现在新键上正常工作
	if _, err := store.AcquireConcurrency(ctx, newKey, 2); err != nil {
		t.Fatalf("新键 acquire: %v", err)
	}

	if tp := rdb.Type(ctx, oldKey).Val(); tp != "string" {
		t.Fatalf("旧键类型应为 string，got %s", tp)
	}
	if tp := rdb.Type(ctx, newKey).Val(); tp != "zset" {
		t.Fatalf("新键类型应为 zset，got %s", tp)
	}

	// 反向证明：若新实现复用旧前缀（对 string 键做 ZADD），必然 WRONGTYPE
	err := rdb.ZAdd(ctx, oldKey, redis.Z{Score: 1, Member: "probe"}).Err()
	if err == nil {
		t.Fatal("对旧前缀 string 键 ZADD 应当 WRONGTYPE（说明复用旧前缀会导致灰度事故）")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "WRONGTYPE") {
		t.Fatalf("期望 WRONGTYPE，got %v", err)
	}

	t.Logf("旧键 %s=string（INCR/DECR 可用），新键 %s=zset；复用旧前缀会 WRONGTYPE，故新前缀是灰度必需",
		oldKey, newKey)
}
