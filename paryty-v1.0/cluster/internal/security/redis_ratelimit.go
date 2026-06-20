package security

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RedisRateLimiter is a distributed token-bucket rate limiter backed by Redis.
// It uses Redis Lua scripts to ensure atomic operations for multi-pod deployments.
type RedisRateLimiter struct {
	rdb  *redis.Client
	rate float64 // tokens per second
	burst float64 // max tokens (burst size)
}

// NewRedisRateLimiter creates a new Redis-backed rate limiter.
// Parameters:
//   - rdb: Redis client connected to Dragonfly/Redis
//   - maxPerMinute: sustained request rate per minute
//   - burstSize: maximum burst above the sustained rate
func NewRedisRateLimiter(rdb *redis.Client, maxPerMinute, burstSize int) *RedisRateLimiter {
	if maxPerMinute <= 0 {
		maxPerMinute = 600
	}
	if burstSize <= 0 {
		burstSize = max(maxPerMinute/6, 10)
	}
	return &RedisRateLimiter{
		rdb:   rdb,
		rate:  float64(maxPerMinute) / 60.0, // convert to per-second
		burst: float64(burstSize),
	}
}

// TryConsume attempts to consume 1 token from the bucket for the given key.
// Returns (retryAfter, false) if no token is available; (0, true) on success.
// The key should be in the format "ratelimit:{tenantID}:{endpoint}".
func (l *RedisRateLimiter) TryConsume(ctx context.Context, key string) (time.Duration, bool) {
	// Lua script for atomic token bucket algorithm.
	// KEYS[1] = rate limit key
	// ARGV[1] = burst size (max tokens)
	// ARGV[2] = refill rate (tokens per second)
	// ARGV[3] = current timestamp (seconds since epoch)
	// ARGV[4] = TTL for key cleanup (seconds)
	script := redis.NewScript(`
		local key = KEYS[1]
		local burst = tonumber(ARGV[1])
		local rate = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local ttl = tonumber(ARGV[4])

		-- Get current bucket state
		local tokens = tonumber(redis.call('HGET', key, 'tokens') or burst)
		local last_fill = tonumber(redis.call('HGET', key, 'last_fill') or now)

		-- Calculate refill
		local elapsed = math.max(0, now - last_fill)
		tokens = math.min(burst, tokens + elapsed * rate)

		-- Try to consume a token
		local allowed = 0
		local retry_after = 0
		if tokens >= 1.0 then
			tokens = tokens - 1.0
			allowed = 1
		else
			-- Calculate retry after (time to get 1 token)
			retry_after = (1.0 - tokens) / rate
		end

		-- Update bucket state
		redis.call('HSET', key, 'tokens', tostring(tokens), 'last_fill', tostring(now))
		redis.call('EXPIRE', key, ttl)

		return {allowed, tostring(retry_after)}
	`)

	now := float64(time.Now().UnixMilli()) / 1000.0
	ttl := 600 // 10 minutes TTL for key cleanup

	result, err := script.Run(ctx, l.rdb, []string{key},
		l.burst, l.rate, now, ttl).Int64Slice()
	if err != nil {
		// On error, fail open (allow request) to prevent Redis outage from blocking all traffic.
		// Log the error in production.
		return 0, true
	}

	if len(result) < 2 {
		return 0, true
	}

	allowed := result[0] == 1
	retryAfterSeconds := float64(result[1]) / 1000.0 // Convert from milliseconds

	if allowed {
		return 0, true
	}
	return time.Duration(retryAfterSeconds * float64(time.Second)), false
}

// RateLimitWithRedis returns a Gin middleware that enforces per-tenant token-bucket
// rate limiting using Redis for distributed state. The tenant ID is read from the
// plan.CtxTenantID context key (set by the JWT auth middleware). If no tenant context
// is present, the global "anonymous" bucket is used.
func RateLimitWithRedis(rdb *redis.Client, cfg RateLimitConfig) gin.HandlerFunc {
	limiter := NewRedisRateLimiter(rdb, cfg.MaxRequestsPerMinute, cfg.BurstSize)
	return func(c *gin.Context) {
		// Determine tenant. Fall back to "anonymous" if no auth context.
		tenantID := "anonymous"
		if v, ok := c.Get("paryty:tenant_id"); ok {
			if tid, ok := v.(string); ok && tid != "" {
				tenantID = tid
			}
		}

		// Use endpoint path as the rate limit scope key.
		endpoint := c.Request.URL.Path
		key := fmt.Sprintf("ratelimit:%s:%s", tenantID, endpoint)

		retryAfter, ok := limiter.TryConsume(c.Request.Context(), key)
		if !ok {
			c.AbortWithStatusJSON(429, gin.H{
				"message": "rate limit exceeded",
				"details": gin.H{
					"retry_after_seconds": int(retryAfter.Seconds()),
				},
			})
			return
		}

		c.Next()
	}
}
