package quota

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	rpmWindowMS = 60 * 1000 // 60s 滑动窗口（毫秒）
	// 并发键 TTL 兜底：防进程崩溃泄漏
	concurrencyKeyTTL = 30 * time.Minute
)

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

// concurrencyAcquireScript 原子并发获取 Lua。
// KEYS[1]：并发键；ARGV[1]：限额；ARGV[2]：TTL 兜底(秒)
// 返回：1=通过；0=超限
var concurrencyAcquireScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local val = redis.call('INCR', key)
if val > limit then
  redis.call('DECR', key)
  return 0
end
redis.call('EXPIRE', key, ttl)
return 1
`)

// concurrencyReleaseScript 原子并发释放 Lua。
// KEYS[1]：并发键
// 返回：DECR 后的值；若 <=0 则 DEL key 后再返回 0
var concurrencyReleaseScript = redis.NewScript(`
local key = KEYS[1]
local val = redis.call('DECR', key)
if val <= 0 then
  redis.call('DEL', key)
  return 0
end
return val
`)

// RedisStore 配额 Redis 存储层：RPM 滑动窗口 + 并发 INCR/DECR。
type RedisStore struct {
	rdb *redis.Client
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

// AcquireConcurrency 获取并发许可（Lua 原子操作，避免 INCR->check->DECR 竞态）。
// 返回 nil=通过，非 nil=超限。
func (s *RedisStore) AcquireConcurrency(ctx context.Context, key string, limit int) error {
	if limit <= 0 {
		return nil // 0 = 不限
	}
	result, err := concurrencyAcquireScript.Run(ctx, s.rdb, []string{key}, limit, int64(concurrencyKeyTTL.Seconds())).Int64()
	if err != nil {
		// Redis fail-open：记录日志和 metric，放行请求
		slog.Warn("quota: redis concurrency acquire error, fail-open", "key", key, "error", err)
		RecordFailOpen(ctx, "concurrency")
		return nil // fail-open
	}
	if result == 0 {
		return fmt.Errorf("concurrency_exceeded")
	}
	return nil
}

// ReleaseConcurrency 释放并发许可（Lua 原子操作，避免双重释放导致 counter 负数）。
func (s *RedisStore) ReleaseConcurrency(ctx context.Context, key string) {
	_, err := concurrencyReleaseScript.Run(ctx, s.rdb, []string{key}).Result()
	if err != nil {
		slog.Warn("quota: redis concurrency release error", "key", key, "error", err)
	}
}

// RPMKey 构造 RPM 限流键。
func RPMKey(subjectType, subjectID, layer, resourceID string) string {
	if resourceID == "" {
		return fmt.Sprintf("quota:rpm:%s:%s", subjectType, subjectID)
	}
	return fmt.Sprintf("quota:rpm:%s:%s:%s:%s", subjectType, subjectID, layer, resourceID)
}

// ConcurrencyKey 构造并发限流键。
func ConcurrencyKey(subjectType, subjectID, layer, resourceID string) string {
	if resourceID == "" {
		return fmt.Sprintf("quota:conc:%s:%s", subjectType, subjectID)
	}
	return fmt.Sprintf("quota:conc:%s:%s:%s:%s", subjectType, subjectID, layer, resourceID)
}