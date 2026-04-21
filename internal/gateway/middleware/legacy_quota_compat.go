package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// GatewayGuard is retained for compatibility with callers that have not yet
// moved to the Redis-backed four-layer quota middleware.
type GatewayGuard struct {
	mu            sync.Mutex
	limitPerMin   int
	maxConcurrent int
	users         map[string]*guardState
}

type guardState struct {
	windowStart time.Time
	requests    int
	concurrent  int
}

func NewGatewayGuard(limitPerMin, maxConcurrent int) *GatewayGuard {
	return &GatewayGuard{
		limitPerMin: limitPerMin, maxConcurrent: maxConcurrent,
		users: make(map[string]*guardState),
	}
}

func (g *GatewayGuard) UpdateLimits(limitPerMin, maxConcurrent int) {
	g.mu.Lock()
	g.limitPerMin, g.maxConcurrent = limitPerMin, maxConcurrent
	g.mu.Unlock()
}

func (g *GatewayGuard) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, _ := c.Get("identity")
		id, _ := identity.(*gwmodel.UserIdentity)
		if id == nil || id.UserID == "" {
			c.Next()
			return
		}
		now := time.Now()
		g.mu.Lock()
		state := g.users[id.UserID]
		if state == nil {
			state = &guardState{windowStart: now}
			g.users[id.UserID] = state
		}
		if now.Sub(state.windowStart) >= time.Minute {
			state.windowStart, state.requests = now, 0
		}
		if g.maxConcurrent > 0 && state.concurrent >= g.maxConcurrent {
			g.mu.Unlock()
			writeLegacyRateLimit(c, "并发")
			return
		}
		if g.limitPerMin > 0 && state.requests >= g.limitPerMin {
			g.mu.Unlock()
			writeLegacyRateLimit(c, "RPM")
			return
		}
		state.concurrent++
		state.requests++
		g.mu.Unlock()
		defer func() {
			g.mu.Lock()
			state.concurrent--
			g.mu.Unlock()
		}()
		c.Next()
	}
}

type RateLimitConfig struct {
	GlobalRPM int
	StudioRPM int
	DeptRPM   int
	KeyRPM    int
}

var legacyRateScript = redis.NewScript(`
local value = redis.call('INCR', KEYS[1])
if value == 1 then redis.call('EXPIRE', KEYS[1], 60) end
if value > tonumber(ARGV[1]) then return 0 end
return 1
`)

func LayeredRateLimiter(rdb *redis.Client, resolve func(*gin.Context) RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rdb == nil || resolve == nil {
			c.Next()
			return
		}
		cfg := resolve(c)
		identity, _ := c.Get("identity")
		id, _ := identity.(*gwmodel.UserIdentity)
		if id == nil {
			c.Next()
			return
		}
		keySubject := id.UserID
		if id.KeyType == "service" {
			keySubject = id.KeyID
		}
		layers := []struct {
			limit int
			key   string
			label string
		}{
			{cfg.GlobalRPM, "global", "全局"},
			{cfg.StudioRPM, "studio:" + id.StudioID, "工作室"},
			{cfg.DeptRPM, "dept:" + id.DepartmentID, "部门"},
			{cfg.KeyRPM, "key:" + keySubject, "Key"},
		}
		for _, layer := range layers {
			if layer.limit <= 0 {
				continue
			}
			key := "rl:" + layer.key
			allowed, err := legacyRateScript.Run(c.Request.Context(), rdb, []string{key}, layer.limit).Int64()
			if err != nil {
				c.Next()
				return
			}
			if allowed == 0 {
				c.Header("Retry-After", strconv.Itoa(60))
				writeLegacyRateLimit(c, layer.label)
				return
			}
		}
		c.Next()
	}
}

func writeLegacyRateLimit(c *gin.Context, label string) {
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": gin.H{
		"message": fmt.Sprintf("%s RPM rate limit exceeded", label),
		"type":    "rate_limited", "code": "rate_limited", "param": label, "gate": "rate",
	}})
}
