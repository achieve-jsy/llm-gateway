package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	providerKeyTTL       = 120 * time.Second
	concurrencyKeyPrefix = "concurrency:provider:"
	ratelimitKeyPrefix   = "ratelimit:provider:"
)

// ProviderTrackerCtxKey is the gin context key for the Redis client used by provider tracking.
const ProviderTrackerCtxKey = "provider_tracker_rdb"

// RateLimitStatus is the JSON structure stored in ratelimit:provider:* keys.
// Must match the struct read by GetAccountAvailability in ops_handler.go.
type RateLimitStatus struct {
	IsRateLimited         bool  `json:"is_rate_limited"`
	RateLimitRemaining    int   `json:"rate_limit_remaining"`
	RateLimitResetSeconds int   `json:"rate_limit_reset_seconds"`
	ErrorCount            int64 `json:"error_count"`
	MaxCapacity           int64 `json:"max_capacity,omitempty"`
}

// ProviderTracker returns a middleware that writes provider concurrency and rate limit
// state to Redis for the control plane's ops dashboard.
//
// On pre-request: increments concurrency:provider:<name> (simple integer counter).
// On post-request: decrements concurrency:provider:<name>.
// On rate-limited (429) responses: writes ratelimit:provider:<name> JSON status.
//
// Redis keys written:
//   - concurrency:provider:<name> → int (active count), TTL 120s
//   - ratelimit:provider:<name>   → JSON RateLimitStatus, TTL 120s
func ProviderTracker(rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rdb == nil {
			c.Next()
			return
		}

		// Store rdb in context for TrackProviderRequest to use
		c.Set(ProviderTrackerCtxKey, rdb)

		c.Next()

		// Post-request: read provider and release concurrency slot
		provider := c.GetString("provider")
		if provider == "" {
			return
		}

		ctx := context.Background()
		concurrencyKey := concurrencyKeyPrefix + provider

		pipe := rdb.Pipeline()
		// Decrement active count (release slot)
		pipe.Decr(ctx, concurrencyKey)
		pipe.Expire(ctx, concurrencyKey, providerKeyTTL)

		// Write rate limit status if response was 429
		status := c.Writer.Status()
		if status == http.StatusTooManyRequests {
			rateKey := ratelimitKeyPrefix + provider
			rlStatus := RateLimitStatus{
				IsRateLimited:         true,
				RateLimitRemaining:    0,
				RateLimitResetSeconds: 60,
				ErrorCount:           1,
			}
			b, _ := json.Marshal(rlStatus)
			pipe.Set(ctx, rateKey, string(b), providerKeyTTL)
		}

		// Also write rate limit status if error message indicates rate limiting
		if errMsg := c.GetString("error_message"); errMsg != "" && isRateLimitError(errMsg) {
			rateKey := ratelimitKeyPrefix + provider
			rlStatus := RateLimitStatus{
				IsRateLimited:         true,
				RateLimitRemaining:    0,
				RateLimitResetSeconds: 60,
				ErrorCount:           1,
			}
			b, _ := json.Marshal(rlStatus)
			pipe.Set(ctx, rateKey, string(b), providerKeyTTL)
		}

		pipe.Exec(ctx)
	}
}

// TrackProviderRequest increments the active concurrency counter for the given provider.
// Call this from the handler after the provider is known (post-dispatch, pre-upstream).
// The corresponding decrement is handled by ProviderTracker middleware in post-request.
func TrackProviderRequest(c *gin.Context, provider string) {
	if provider == "" {
		return
	}
	rdbVal, ok := c.Get(ProviderTrackerCtxKey)
	if !ok {
		return
	}
	rdb, ok := rdbVal.(*redis.Client)
	if !ok || rdb == nil {
		return
	}

	key := concurrencyKeyPrefix + provider
	ctx := context.Background()
	pipe := rdb.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, providerKeyTTL)
	pipe.Exec(ctx)
}

// WriteProviderRateLimit writes a rate limit status to Redis for the given provider.
// This is a public helper that can be called from anywhere that detects a provider rate limit.
func WriteProviderRateLimit(rdb *redis.Client, provider string, status RateLimitStatus) {
	if rdb == nil || provider == "" {
		return
	}
	key := ratelimitKeyPrefix + provider
	b, _ := json.Marshal(status)
	ctx := context.Background()
	rdb.Set(ctx, key, string(b), providerKeyTTL)
}

// WriteProviderConcurrency sets the initial concurrency state for a provider.
// Useful for initialization or setting max capacity from configuration.
func WriteProviderConcurrency(rdb *redis.Client, provider string, maxCapacity int64) {
	if rdb == nil || provider == "" {
		return
	}
	key := concurrencyKeyPrefix + provider
	ctx := context.Background()
	pipe := rdb.Pipeline()
	// Initialize active count to 0
	pipe.Set(ctx, key, 0, providerKeyTTL)
	// Write max capacity in the rate limit key
	rateKey := ratelimitKeyPrefix + provider
	rlStatus := RateLimitStatus{
		IsRateLimited: false,
		MaxCapacity:   maxCapacity,
	}
	b, _ := json.Marshal(rlStatus)
	pipe.Set(ctx, rateKey, string(b), providerKeyTTL)
	pipe.Exec(ctx)
}

func isRateLimitError(msg string) bool {
	return containsAny(msg, "rate limit", "rate_limited", "too many requests", "429", "quota exceeded")
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}