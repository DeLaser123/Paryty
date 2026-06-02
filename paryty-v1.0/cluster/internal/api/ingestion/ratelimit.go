// Rate limiting for the ingestion gRPC adapter.
// Uses per-agent token-bucket rate limiting with automatic stale entry cleanup.
package api

import (
	"context"
	"sync"
	"time"
)

// cleanupInterval is how often stale rate-limiter entries are purged.
const cleanupInterval = 5 * time.Minute

// staleThreshold is the duration after which an unused bucket is considered stale.
const staleThreshold = 10 * time.Minute

// RateLimiter implements per-agent token-bucket rate limiting.
// It is safe for concurrent use.
type RateLimiter struct {
	buckets      sync.Map // map[string]*tokenBucket
	maxPerMinute int
}

// tokenBucket is a per-agent token bucket.
type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	lastRefill time.Time
	lastAccess time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a RateLimiter that allows maxPerMinute requests per agent per minute.
// Call StartCleanup with a context to begin periodic stale entry removal.
func NewRateLimiter(maxPerMinute int) *RateLimiter {
	if maxPerMinute <= 0 {
		maxPerMinute = 10000 // safe default matching DefaultMaxBatchesPerMin
	}
	return &RateLimiter{
		maxPerMinute: maxPerMinute,
	}
}

// StartCleanup launches a background goroutine that removes stale bucket entries
// every cleanupInterval. The goroutine exits when ctx is cancelled.
func (rl *RateLimiter) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rl.cleanup()
			}
		}
	}()
}

// Allow checks whether the agent identified by agentID is allowed to proceed.
// It returns true if a token is available and decrements the token count.
// Returns false if the rate limit is exceeded.
func (rl *RateLimiter) Allow(agentID string) bool {
	bucket := rl.getOrCreateBucket(agentID)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(bucket.lastRefill)
	bucket.tokens += elapsed.Seconds() * (float64(rl.maxPerMinute) / 60.0)
	if bucket.tokens > bucket.maxTokens {
		bucket.tokens = bucket.maxTokens
	}
	bucket.lastRefill = now
	bucket.lastAccess = now

	if bucket.tokens < 1.0 {
		return false
	}

	bucket.tokens -= 1.0
	return true
}

// getOrCreateBucket returns the token bucket for the given agent, creating one if needed.
func (rl *RateLimiter) getOrCreateBucket(agentID string) *tokenBucket {
	if v, ok := rl.buckets.Load(agentID); ok {
		return v.(*tokenBucket)
	}

	bucket := &tokenBucket{
		tokens:     float64(rl.maxPerMinute),
		maxTokens:  float64(rl.maxPerMinute),
		lastRefill: time.Now(),
		lastAccess: time.Now(),
	}

	actual, _ := rl.buckets.LoadOrStore(agentID, bucket)
	return actual.(*tokenBucket)
}

// cleanup removes buckets that have not been accessed within the staleThreshold.
func (rl *RateLimiter) cleanup() {
	now := time.Now()
	rl.buckets.Range(func(key, value any) bool {
		bucket := value.(*tokenBucket)
		bucket.mu.Lock()
		stale := now.Sub(bucket.lastAccess) > staleThreshold
		bucket.mu.Unlock()

		if stale {
			rl.buckets.Delete(key)
		}
		return true
	})
}
