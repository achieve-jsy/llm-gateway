package ehrclient

import (
	"math/rand"
	"sync"
	"time"
)

// RateLimiter 令牌桶限流器，控制对 EHR API 的请求速率
type RateLimiter struct {
	mu       sync.Mutex
	rate     float64 // tokens/sec
	maxTokens float64
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter 创建限流器，rate 单位：请求/秒
func NewRateLimiter(rate float64) *RateLimiter {
	return &RateLimiter{
		rate:      rate,
		maxTokens: rate, // 桶深 1 秒，允许瞬时突发
		tokens:    rate,
		lastTime:  time.Now(),
	}
}

// Wait 阻塞直到获取到一个令牌，ctx.Done 时返回 false
func (l *RateLimiter) Wait(ctxDone <-chan struct{}) bool {
	for {
		l.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(l.lastTime).Seconds()
		l.lastTime = now
		l.tokens += elapsed * l.rate
		if l.tokens > l.maxTokens {
			l.tokens = l.maxTokens
		}
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return true
		}
		// 还差多少 token
		shortage := 1 - l.tokens
		wait := time.Duration(float64(time.Second) * shortage / l.rate)
		l.mu.Unlock()

		select {
		case <-ctxDone:
			return false
		case <-time.After(wait + time.Duration(rand.Int63n(int64(20*time.Millisecond)))):
			continue
		}
	}
}

// SetRate 运行时修改速率
func (l *RateLimiter) SetRate(rate float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = rate
	if l.tokens > rate {
		l.tokens = rate
	}
	l.maxTokens = rate
}
