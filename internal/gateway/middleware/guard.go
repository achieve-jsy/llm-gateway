package middleware

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

type rateCounter struct {
	count   int
	resetAt time.Time
}

// GatewayGuard provides per-API-key rate limiting and concurrency control.
type GatewayGuard struct {
	mu sync.Mutex
	// rate counters
	rateData      map[string]*rateCounter
	limitPerMin   int32
	// concurrency tracking
	inFlight      map[string]int32
	maxConcurrent int32
}

func NewGatewayGuard(limitPerMin, maxConcurrent int) *GatewayGuard {
	return &GatewayGuard{
		rateData:      make(map[string]*rateCounter),
		limitPerMin:   int32(limitPerMin),
		inFlight:      make(map[string]int32),
		maxConcurrent: int32(maxConcurrent),
	}
}

func (g *GatewayGuard) UpdateLimits(limitPerMin, maxConcurrent int) {
	atomic.StoreInt32(&g.limitPerMin, int32(limitPerMin))
	atomic.StoreInt32(&g.maxConcurrent, int32(maxConcurrent))
}

func (g *GatewayGuard) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		identityVal, exists := c.Get("identity")
		if !exists {
			c.Next()
			return
		}
		identity, ok := identityVal.(*gwmodel.UserIdentity)
		if !ok || identity.UserID == "" {
			c.Next()
			return
		}
		key := identity.UserID

		maxConcurrent := int(atomic.LoadInt32(&g.maxConcurrent))
		if maxConcurrent > 0 {
			if !g.acquire(key, maxConcurrent) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": gin.H{
						"type":    "concurrency_limit",
						"message": "too many concurrent requests",
						"code":    "concurrency_limited",
					},
				})
				return
			}
			defer g.release(key)
		}

		limitPerMin := int(atomic.LoadInt32(&g.limitPerMin))
		if limitPerMin > 0 && !g.checkRate(key, limitPerMin) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"type":    "rate_limit_error",
					"message": "too many requests, please slow down",
					"code":    "rate_limited",
				},
			})
			return
		}

		c.Next()
	}
}

func (g *GatewayGuard) acquire(key string, maxConcurrent int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	current := g.inFlight[key]
	if current >= int32(maxConcurrent) {
		return false
	}
	g.inFlight[key] = current + 1
	return true
}

func (g *GatewayGuard) release(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	current := g.inFlight[key]
	if current <= 1 {
		delete(g.inFlight, key)
	} else {
		g.inFlight[key] = current - 1
	}
}

func (g *GatewayGuard) checkRate(key string, limit int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	counter, ok := g.rateData[key]
	if !ok || now.After(counter.resetAt) {
		counter = &rateCounter{count: 0, resetAt: now.Add(time.Minute)}
		g.rateData[key] = counter
	}
	counter.count++
	return counter.count <= limit
}

// cleanupRateData periodically removes stale counters to prevent memory leaks.
func (g *GatewayGuard) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			g.mu.Lock()
			now := time.Now()
			for k, c := range g.rateData {
				if now.After(c.resetAt) {
					delete(g.rateData, k)
				}
			}
			g.mu.Unlock()
		}
	}()
}