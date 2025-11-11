package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateCounter struct {
	count   int
	resetAt time.Time
}

var (
	rateMu   sync.Mutex
	rateData = map[string]*rateCounter{}
)

func RateLimit(limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		now := time.Now()
		rateMu.Lock()
		counter, ok := rateData[key]
		if !ok || now.After(counter.resetAt) {
			counter = &rateCounter{count: 0, resetAt: now.Add(time.Minute)}
			rateData[key] = counter
		}
		counter.count++
		blocked := counter.count > limit
		rateMu.Unlock()
		if blocked {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{"type": "rate_limit_error", "message": "too many requests", "code": "rate_limited"},
			})
			return
		}
		c.Next()
	}
}

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
