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
		slog.Warn("quota: redis rpm check error, fail-open", "key", key, "error", err)
		return nil // fail-open
	}
	if result == 0 {
		return fmt.Errorf("rpm_exceeded")
	}
	return nil
}

// AcquireConcurrency 获取并发许可。
// 返回 nil=通过，非 nil=超限。
func (s *RedisStore) AcquireConcurrency(ctx context.Context, key string, limit int) error {
	if limit <= 0 {
		return nil // 0 = 不限
	}
	val, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		slog.Warn("quota: redis concurrency acquire error, fail-open", "key", key, "error", err)
		return nil // fail-open
	}
	if val > int64(limit) {
		// 超限：回滚
		s.rdb.Decr(ctx, key)
		return fmt.Errorf("concurrency_exceeded")
	}
	// 设置 TTL 兜底
	s.rdb.Expire(ctx, key, concurrencyKeyTTL)
	return nil
}

// ReleaseConcurrency 释放并发许可。
func (s *RedisStore) ReleaseConcurrency(ctx context.Context, key string) {
	val, err := s.rdb.Decr(ctx, key).Result()
	if err != nil {
		slog.Warn("quota: redis concurrency release error", "key", key, "error", err)
		return
	}
	if val <= 0 {
		// 清理已归零的键
		s.rdb.Del(ctx, key)
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
