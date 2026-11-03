package quota

import (
	"context"
	"fmt"
	corequota "github.com/example/llm-gateway/internal/core/quota"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// 真实 Redis 端到端演练（opt-in）。
//
// 默认跳过；设置 QUOTA_DRILL_REDIS_ADDR 指向一个**本地/隔离**的 Redis 才会执行。
// 只用于上线前演练：验证租约式并发计数在真实 Redis（而非 miniredis）上的
// 脚本语义、原子性、幂等性与自愈路径。
//
// 严禁指向生产 Redis：脚本会写入并删除 quota:conc:lease: 前缀下的键。
//
// 运行方式：
//
//	QUOTA_DRILL_REDIS_ADDR=127.0.0.1:6399 go test ./internal/gateway/quota/ \
//	  -run TestE2EDrill_LeaseConcurrency_RealRedis -count=1 -v
func TestE2EDrill_LeaseConcurrency_RealRedis(t *testing.T) {
	addr := os.Getenv("QUOTA_DRILL_REDIS_ADDR")
	if addr == "" {
		t.Skip("QUOTA_DRILL_REDIS_ADDR 未设置，跳过真实 Redis 演练（默认不连任何 Redis）")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("无法连接演练 Redis %s: %v", addr, err)
	}
	server, _ := rdb.Info(context.Background(), "server").Result()
	version := ""
	for _, line := range strings.Split(server, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			version = line
			break
		}
	}
	t.Logf("演练目标 Redis: %s（%s）", addr, version)

	store := NewRedisStore(rdb)
	ctx := context.Background()
	const limit = 5
	key := ConcurrencyKey("user", "drill-user", "", "")
	defer func() {
		// 只清理本演练使用的键，不 FLUSHALL
		rdb.Del(ctx, key)
	}()

	// ── 场景 1：占满 limit，第 limit+1 个被拒 ──
	leases := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		leaseID, err := store.AcquireConcurrency(ctx, key, limit)
		if err != nil {
			t.Fatalf("第 %d 次获取应放行: %v", i+1, err)
		}
		leases = append(leases, leaseID)
	}
	if _, err := store.AcquireConcurrency(ctx, key, limit); err == nil {
		t.Fatal("第 limit+1 个应被拒绝")
	}
	if n, _ := rdb.ZCard(ctx, key).Result(); n != limit {
		t.Fatalf("占用条目应为 %d，实际 %d", limit, n)
	}
	t.Logf("场景1 通过：占满 %d 个槽，第 %d 个被拒，ZSET 条目=%d", limit, limit+1, limit)

	// ── 场景 2：逐条目 score = 到期时间戳，且真实 Redis 上确实只在 advance 后清理 ──
	scores, err := rdb.ZMScore(ctx, key, leases...).Result()
	if err != nil {
		t.Fatalf("ZMSCORE: %v", err)
	}
	nowMS := float64(time.Now().UnixMilli())
	wantMS := float64(concurrencyLease.Milliseconds())
	for i, s := range scores {
		if s <= nowMS {
			t.Fatalf("第 %d 条租约 score(%v) 应晚于当前时间", i+1, s)
		}
		if diff := s - nowMS - wantMS; diff < -5000 || diff > 5000 {
			t.Fatalf("第 %d 条租约 score 与 now+%v 偏差过大: %v", i+1, concurrencyLease, diff)
		}
	}
	t.Logf("场景2 通过：%d 条租约 score 均为 now+%v（毫秒级）", len(scores), concurrencyLease)

	// ── 场景 3：模拟「归还永久丢失」→ 到期后自愈（不改 lease 常量，直接改写 score 模拟时间流逝）──
	// 说明：lease 固定 360s（必须 > 300s 上游超时），演练不可能真等 6 分钟，
	// 因此把已占用条目的 score 改到过去，等价于「时间已经走过租约到期点」，
	// 再验证下一次 acquire 的 ZREMRANGEBYSCORE 清理与放行。
	past := float64(time.Now().Add(-time.Second).UnixMilli())
	for _, l := range leases {
		if err := rdb.ZAdd(ctx, key, redis.Z{Score: past, Member: l}).Err(); err != nil {
			t.Fatalf("改写 score 模拟过期: %v", err)
		}
	}
	if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
		t.Fatalf("丢失归还的租约到期后应自愈，实际仍被拒: %v", err)
	}
	t.Logf("场景3 通过：%d 条「丢失归还」的租约到期后被清理，新请求放行（自愈）", limit)

	// ── 场景 4：释放幂等（重复释放 no-op）与键清理 ──
	all, _ := rdb.ZRange(ctx, key, 0, -1).Result()
	for _, l := range append(all, all...) { // 每个租约释放两次：第二次必须是 no-op
		store.ReleaseConcurrency(ctx, key, l)
	}
	if n, _ := rdb.ZCard(ctx, key).Result(); n != 0 {
		t.Fatalf("全部释放后应无残留，实际 %d", n)
	}
	if rdb.Exists(ctx, key).Val() != 0 {
		t.Fatal("集合清空后键应被 DEL")
	}
	t.Log("场景4 通过：重复释放幂等，全部归还后键被删除")

	// ── 场景 5：并发不超发（真实 Redis + Lua 原子性）──
	const total = 200
	var ok, rejected atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
				rejected.Add(1)
				return
			}
			ok.Add(1)
		}()
	}
	wg.Wait()
	if ok.Load() != limit {
		t.Fatalf("并发 %d 次应恰好 %d 次成功，实际成功=%d 拒绝=%d", total, limit, ok.Load(), rejected.Load())
	}
	t.Logf("场景5 通过：%d 并发争抢 limit=%d，成功=%d 拒绝=%d（无超发）", total, limit, ok.Load(), rejected.Load())
	rdb.Del(ctx, key)

	// ── 场景 6：不同 key 前缀隔离（灰度期旧键不被新实现触碰）──
	legacyPrefixKey := fmt.Sprintf("quota:conc:user:%s", "drill-user")
	rdb.Del(ctx, legacyPrefixKey)
	if err := rdb.Set(ctx, legacyPrefixKey, 3, time.Minute).Err(); err != nil {
		t.Fatalf("写入旧前缀键: %v", err)
	}
	if _, err := store.AcquireConcurrency(ctx, key, limit); err != nil {
		t.Fatalf("新前缀获取不应受旧前缀键影响: %v", err)
	}
	if got := rdb.Get(ctx, legacyPrefixKey).Val(); got != "3" {
		t.Fatalf("旧前缀键不得被新实现改写，实际值=%q", got)
	}
	t.Logf("场景6 通过：新前缀 %q 与旧键 %q 互不干扰（旧键值保持 3）", corequota.ConcurrencyKeyPrefix, legacyPrefixKey)
	rdb.Del(ctx, key, legacyPrefixKey)
}
