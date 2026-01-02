package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// rateLimitScript 滑动窗口限流 Lua（60s 窗口，ZSET 实现）。
// KEYS[1]：限流键；ARGV[1]：当前时间戳(ms)；ARGV[2]：窗口大小(ms)；ARGV[3]：限额
// 返回：1=通过；0=超限
var rateLimitScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local cutoff = now - window
-- 清理过期成员
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
-- 计数
local count = redis.call('ZCARD', key)
if count >= limit then
  return 0
end
-- 记录本次请求
redis.call('ZADD', key, now, now .. '-' .. math.random(1000000))
redis.call('PEXPIRE', key, window + 1000)
return 1
`)

const (
	rlWindowMS = 60 * 1000 // 60s 滑动窗口（毫秒）
)

// RateLimitConfig 分层限速配置（由 verify-key 快照或 settings 提供）。
type RateLimitConfig struct {
	GlobalRPM  int // 全局 RPM（0 = 不限）
	StudioRPM  int // 工作室 RPM（0 = 不限）
	DeptRPM    int // 部门 RPM（0 = 不限）
	KeyRPM     int // Key 级 RPM（0 = 不限）
}

// LayeredRateLimiter 分层限速中间件（③关）。
// 按 global > studio > dept > key 四层，最严胜出。
// Service Key 按 key_id 独立计（不并入创建者 user_id 桶）。
// Redis 不可用时 fail-open。
func LayeredRateLimiter(rdb *redis.Client, cfg func(*gin.Context) RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rdb == nil {
			c.Next()
			return
		}

		identityVal, exists := c.Get("identity")
		if !exists {
			c.Next()
			return
		}
		id, ok := identityVal.(*gwmodel.UserIdentity)
		if !ok {
			c.Next()
			return
		}

		config := cfg(c)
		now := time.Now().UnixMilli()

		// 构造各层（key, limit）
		type layer struct {
			key   string
			limit int
			label string
		}
		layers := []layer{}

		if config.GlobalRPM > 0 {
			layers = append(layers, layer{"rl:global", config.GlobalRPM, "全局"})
		}
		if config.StudioRPM > 0 && id.StudioID != "" {
			layers = append(layers, layer{
				fmt.Sprintf("rl:studio:%s", id.StudioID), config.StudioRPM, "工作室",
			})
		}
		if config.DeptRPM > 0 && id.DepartmentID != "" {
			layers = append(layers, layer{
				fmt.Sprintf("rl:dept:%s", id.DepartmentID), config.DeptRPM, "部门",
			})
		}
		if config.KeyRPM > 0 {
			var rlKey string
			if id.KeyType == "service" {
				// Service Key 独立按 key_id 计，不并入创建者 UserID 桶
				rlKey = fmt.Sprintf("rl:key:%s", id.KeyID)
			} else {
				// 个人 Key 按 UserID
				rlKey = fmt.Sprintf("rl:user:%s", id.UserID)
			}
			layers = append(layers, layer{rlKey, config.KeyRPM, "Key"})
		}

		for _, l := range layers {
			result, err := rateLimitScript.Run(
				context.Background(), rdb,
				[]string{l.key},
				now, rlWindowMS, l.limit,
			).Int64()
			if err != nil {
				// Redis 不可用：fail-open，记录告警
				slog.Warn("rate_limiter: redis error, fail-open",
					"layer", l.label, "key", l.key, "error", err)
				continue
			}
			if result == 0 {
				c.Header("Retry-After", "60")
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": gin.H{
						"message": fmt.Sprintf("速率超限：%s RPM 已达上限，请稍后重试", l.label),
						"type":    "rate_limited",
						"code":    "rate_limited",
						"param":   l.label,
						"gate":    "rate",
					},
				})
				return
			}
		}

		c.Next()
	}
}
