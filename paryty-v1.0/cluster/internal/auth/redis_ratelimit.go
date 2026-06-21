package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DistributedRateLimiter provides per-key brute-force protection backed by
// Dragonfly (Redis-compatible). It uses Redis INCR + EXPIRE with TTL-based
// windowing. When Redis is unreachable the limiter fails-open to preserve
// availability — the in-memory fallback in AuthHandler catches the overflow.
type DistributedRateLimiter struct {
	client *redis.Client
}

// NewDistributedRateLimiter creates a rate limiter backed by the given Redis
// client. Pass nil to disable (all calls to Allow return true immediately).
func NewDistributedRateLimiter(client *redis.Client) *DistributedRateLimiter {
	return &DistributedRateLimiter{client: client}
}

// Allow returns (allowed bool, retryAfter time.Duration).
//
// Uses Redis INCR + EXPIRE. If the counter exceeds maxAttempts within the
// window, the request is denied and retryAfter reports the remaining TTL.
//
// Fails open: if the Redis client is nil or the Redis command returns an
// error, the call is allowed. This avoids a Dragonfly outage from blocking
// all auth traffic; the in-memory rate limiter provides a secondary backstop.
func (d *DistributedRateLimiter) Allow(ctx context.Context, key string, maxAttempts int, window time.Duration) (bool, time.Duration) {
	if d.client == nil {
		return true, 0 // fail open — no Redis configured
	}

	redisKey := fmt.Sprintf("ratelimit:%s", key)
	count, err := d.client.Incr(ctx, redisKey).Result()
	if err != nil {
		return true, 0 // fail open — Redis unreachable
	}

	// Set expiry on first increment so keys don't accumulate forever.
	if count == 1 {
		d.client.Expire(ctx, redisKey, window) //nolint:errcheck
	}

	if count > int64(maxAttempts) {
		ttl, _ := d.client.TTL(ctx, redisKey).Result()
		if ttl <= 0 {
			ttl = window
		}
		return false, ttl
	}
	return true, 0
}

// LoginKey returns the SHA-256 hex digest of the email for use as a
// distributed rate-limit key. Hashing prevents plain-text email addresses
// from appearing in Redis keys (which are logged by Dragonfly slowlog).
func LoginKey(email string) string {
	h := sha256.Sum256([]byte(email))
	return fmt.Sprintf("login:%x", h)
}

// RegisterKey returns a rate-limit key scoped to an IP address.
func RegisterKey(ip string) string {
	return fmt.Sprintf("register:%s", ip)
}
