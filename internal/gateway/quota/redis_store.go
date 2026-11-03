package quota

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	corequota "github.com/example/llm-gateway/internal/core/quota"
	"github.com/redis/go-redis/v9"
)

const (
	// rpmWindowMS RPM 滑动窗口大小，取自跨进程共享常量（控制面只读展示使用同一窗口）。
	rpmWindowMS = corequota.RPMWindowMS
	// concurrencyLease 单条并发租约的时长。
	//
	// 依据：网关上游 HTTP 客户端的最长超时是 300s（internal/httpclient/client.go 的
	// defaultLongTimeout），即单个请求最多在飞 5 分钟。租约必须覆盖「请求还在飞」的整个
	// 生命周期，否则合法长请求会在自己的生命周期内被判定过期，导致实际并发超过限额。
	// 取 360s = 300s 上游超时 + 60s 余量，覆盖建连/重试/流式收尾与时钟漂移。
	//
	// 语义变化（本缺陷的修复核心）：槽位不再靠「成功获取时 EXPIRE 续期」保活，
	// 而是每个请求一条带期限的租约条目；进程被强杀 / 15s 优雅退出强断长流式请求导致
	// 归还丢失时，该条目会在租约到期后自动消失，计数器自愈，不会永久钉死。
	concurrencyLease = 360 * time.Second
	// concurrencyKeyTTL 并发键的 key 级兜底 TTL：防进程在「ZADD 成功后、到期清理前」
	// 一直不访问导致空键长期残留（正常路径下 ZREM 清空后会立即 DEL）。
	// 取租约时长 + 60s，保证只要集合里还有未过期租约，键本身绝不会先过期。
	concurrencyKeyTTL = concurrencyLease + 60*time.Second
)

// leaseSeq 生成本进程内单调递增的序列号，用于拼装唯一租约 id。
// 时间戳（纳秒）本身在极端并发下可能重复，叠加序号可保证 member 唯一；
// 唯一性是 ZSET 按条目标记槽位的前提（重复 member 会互相覆盖导致少计数）。
var leaseSeq atomic.Uint64

// newLeaseID 生成唯一租约 id。
func newLeaseID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), leaseSeq.Add(1))
}

// rpmCheckScript 滑动窗口 RPM 限流 Lua。
// KEYS[1]：限流键；ARGV[1]：当前时间戳(ms)；ARGV[2]：窗口大小(ms)；ARGV[3]：限额
// 返回：1=通过；0=超限
var rpmCheckScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local cutoff = now - window
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
local count = redis.call('ZCARD', key)
if count >= limit then
  return 0
end
redis.call('ZADD', key, now, now .. '-' .. math.random(1000000))
redis.call('PEXPIRE', key, window + 1000)
return 1
`)

// concurrencyAcquireScript 原子并发获取 Lua（租约式）。
// KEYS[1]：并发键；ARGV[1]：限额；ARGV[2]：当前时间戳(ms)；
//
//	ARGV[3]：租约到期时间戳(ms)；ARGV[4]：租约 id；ARGV[5]：key 级兜底 TTL(秒)
//
// 返回：1=通过（调用方持有 leaseID）；0=超限。
// 刻意返回整数状态而非 Lua false：false 在 RESP2 下是空回复，go-redis 会把它解释成
// redis.Nil 错误，与真实的 Redis 故障无法区分（会把「超限拒绝」误判成 fail-open 放行）。
//
// 单脚本内先清理已过期条目再计数，保证「清理 + 判定 + 占用」原子，既不超发，
// 也让丢失归还的槽位在到期后自动腾出。
var concurrencyAcquireScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local now = tonumber(ARGV[2])
local expireAt = tonumber(ARGV[3])
local leaseID = ARGV[4]
local keyTTL = tonumber(ARGV[5])
redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
local count = redis.call('ZCARD', key)
if count >= limit then
  return 0
end
redis.call('ZADD', key, expireAt, leaseID)
redis.call('EXPIRE', key, keyTTL)
return 1
`)

// concurrencyReleaseScript 原子并发释放 Lua（按租约 id 精确移除）。
// KEYS[1]：并发键；ARGV[1]：租约 id
// 返回：1=删除了 1 条租约且集合非空；2=删除了 1 条租约且集合已空并 DEL；
// 0=该租约不存在（已被到期清理或重复释放）
var concurrencyReleaseScript = redis.NewScript(`
local key = KEYS[1]
local leaseID = ARGV[1]
local removed = redis.call('ZREM', key, leaseID)
if removed == 0 then
  return 0
end
if redis.call('ZCARD', key) == 0 then
  redis.call('DEL', key)
  return 2
end
return 1
`)

// RedisStore 配额 Redis 存储层：RPM 滑动窗口 + 并发租约（ZSET，逐条目过期）。
type RedisStore struct {
	rdb *redis.Client
	// now 时钟注入点，默认 time.Now。
	//
	// 存在的唯一目的是让测试能推进时间，验证「租约到期后槽位自愈」这一核心性质：
	// 租约判定完全基于脚本入参 now 与成员 score 的比较，miniredis 的 FastForward
	// 只推进其自身 TTL 时钟、不会改变 Go 侧 time.Now()，无法覆盖该路径。
	// 生产路径恒为 nil（走 time.Now），不引入任何行为差异。
	now func() time.Time
}

// nowTime 返回当前时间（未注入时钟时用系统时钟）。
func (s *RedisStore) nowTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// NewRedisStore 创建 Redis 配额存储。
func NewRedisStore(rdb *redis.Client) *RedisStore {
	return &RedisStore{rdb: rdb}
}

// CheckRPM 检查 RPM 滑动窗口是否超限。
// 返回 nil=通过，非 nil=超限。
func (s *RedisStore) CheckRPM(ctx context.Context, key string, limit int) error {
	if limit <= 0 {
		return nil // 0 = 不限
	}
	now := time.Now().UnixMilli()
	result, err := rpmCheckScript.Run(ctx, s.rdb, []string{key}, now, rpmWindowMS, limit).Int64()
	if err != nil {
		// redis.Nil 单独判定：本脚本恒定返回整数，出现空回复说明脚本语义/版本异常，
		// 属**协议异常**而非 Redis 不可用，必须显式报错而不是被 fail-open 吞掉
		//（历史上「用 Lua false 表示超限」正是踩了这个坑：false 在 RESP2 下是空回复，
		//  go-redis 解释为 redis.Nil，于是超限业务判定被误当成故障而放行）。
		if errors.Is(err, redis.Nil) {
			slog.Error("quota: rpm check returned nil reply (protocol anomaly)", "key", key, "error", err)
			return fmt.Errorf("rpm_check_protocol_error: %w", err)
		}
		// Redis fail-open：记录日志和 metric，放行请求
		slog.Warn("quota: redis rpm check error, fail-open", "key", key, "error", err)
		RecordFailOpen(ctx, "rpm")
		return nil // fail-open
	}
	if result == 0 {
		return fmt.Errorf("rpm_exceeded")
	}
	return nil
}

// AcquireConcurrency 获取并发许可（Lua 原子操作：清理过期租约 → 计数 → 占用）。
//
// 契约（三态，互不混淆）：
//   - 通过：返回**非空** lease id 且 error == nil；
//   - 超限：**业务判定**，返回 ("", 非 nil error)，绝不等价于通过，也绝不走 fail-open；
//   - 不限 / Redis 故障：返回 ("", nil)，见下方空 lease id 语义。
//
// 空 lease id 语义（**不是**错误，不得"顺手"改成报错）：
//
//	空 lease id + nil error ⇒ **不持有租约**（"不限"或"fail-open"两种**合法**情形），无需释放；
//	非空 lease id          ⇒ 持有租约，必须原样交给 ReleaseConcurrency 释放。
//
// 两条合法路径（二者都不写入任何 ZSET 成员）：
//  1. limit <= 0（该层显式不限，直接放行，本就不占用任何槽位）；
//  2. Redis 故障 fail-open（放行但无真实占用，故无槽位可释放）。
//
// 因此 ReleaseConcurrency 对空 id 直接返回是**正确**的幂等行为。
// 反过来，若把空 id 一律当作内部错误，就会把"不限"与"fail-open"这两条合法路径误判为失败、
// 破坏既有语义，因此这里刻意不这么做。
//
// 与之相对，通过路径返回空 lease id 属**内部错误**（见函数末尾的兜底判断）：那种情况下
// 槽位已被 ZADD 占用却无法释放，必须显式报错而不是静默放行。
func (s *RedisStore) AcquireConcurrency(ctx context.Context, key string, limit int) (string, error) {
	if limit <= 0 {
		return "", nil // 0 = 不限：不占槽、无需释放
	}
	now := s.nowTime()
	leaseID := newLeaseID()
	result, err := concurrencyAcquireScript.Run(ctx, s.rdb, []string{key},
		limit,
		now.UnixMilli(),
		now.Add(concurrencyLease).UnixMilli(),
		leaseID,
		int64(concurrencyKeyTTL.Seconds()),
	).Int64()
	if err != nil {
		// redis.Nil 单独判定：本脚本恒定返回整数，出现空回复说明脚本语义/版本异常，
		// 属**协议异常**而非 Redis 不可用。历史上「用 Lua false 表示超限」正是踩了这个坑：
		// false 在 RESP2 下是空回复，go-redis 解释为 redis.Nil，于是「超限」这个业务判定
		// 被 fail-open 分支当成 Redis 故障而放行，导致限额静默失效。这里显式报错，
		// 宁可拒绝也不放行，避免同类缺陷再次被吞掉。
		if errors.Is(err, redis.Nil) {
			slog.Error("quota: concurrency acquire returned nil reply (protocol anomaly)", "key", key, "error", err)
			return "", fmt.Errorf("concurrency_acquire_protocol_error: %w", err)
		}
		// Redis fail-open：记录日志和 metric，放行请求
		slog.Warn("quota: redis concurrency acquire error, fail-open", "key", key, "error", err)
		RecordFailOpen(ctx, "concurrency")
		return "", nil // fail-open
	}
	if result == 0 {
		return "", fmt.Errorf("concurrency_exceeded")
	}
	if result != 1 {
		// 未知状态码：脚本与调用方约定被破坏，按拒绝处理（不放行）
		return "", fmt.Errorf("concurrency_acquire_unexpected_result: %d", result)
	}
	if leaseID == "" {
		// 契约兜底：通过路径必须带非空租约 id，否则释放会退化成 no-op 并持续泄漏
		return "", fmt.Errorf("concurrency_acquire_empty_lease_id")
	}
	return leaseID, nil
}

// ReleaseConcurrency 释放并发许可（Lua 原子操作，按租约 id 精确 ZREM）。
//
// 重复释放、过期后释放都不会影响他人槽位（ZREM 不存在的 member 返回 0）；
// 集合清空时删除键，不留垃圾。leaseID 为空（例如 fail-open 路径）时跳过。
//
// 关于返回值：脚本用 1/2/0 区分「删除成功且键非空 / 删除成功且键已删 / 该租约不存在」。
// 其中 0 是「丢失归还」的可观测信号——正常释放应命中已有租约，返回 0 说明归还时该租约
// 已不存在（租约先到期被清理，或发生重复释放）。这里只打 Debug 级日志保留这条信号，
// 刻意**不**引入计数器/告警：长请求的租约自然到期后归还同样返回 0（属正常稳定态），
// 把它做成计数会在正常流量下持续累加，制造无判别力的噪声。
func (s *RedisStore) ReleaseConcurrency(ctx context.Context, key, leaseID string) {
	if leaseID == "" {
		return // fail-open 时没有真实租约，无需也不应释放
	}
	result, err := concurrencyReleaseScript.Run(ctx, s.rdb, []string{key}, leaseID).Result()
	if err != nil {
		slog.Warn("quota: redis concurrency release error", "key", key, "lease_id", leaseID, "error", err)
		return
	}
	if n, ok := result.(int64); ok && n == 0 {
		// 仅用于事后排查「归还是否命中」，默认级别下不产生噪声
		slog.Debug("quota: concurrency lease already gone on release",
			"key", key, "lease_id", leaseID)
	}
}

// RPMKey 构造 RPM 限流键（委托给跨进程共享的 corequota.RPMKey，避免两侧硬编码漂移）。
func RPMKey(subjectType, subjectID, layer, resourceID string) string {
	return corequota.RPMKey(subjectType, subjectID, layer, resourceID)
}

// ConcurrencyKey 构造并发限流键（租约式 ZSET，使用共享前缀避免灰度期与旧实现 WRONGTYPE 冲突）。
func ConcurrencyKey(subjectType, subjectID, layer, resourceID string) string {
	return corequota.ConcurrencyKey(subjectType, subjectID, layer, resourceID)
}
