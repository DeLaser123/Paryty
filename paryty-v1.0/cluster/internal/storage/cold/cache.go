// Package cold — LRU cache layer for cold storage data in Dragonfly.
//
// ColdCache sits in front of the SeaweedFS cold tier and stores recently
// accessed snapshots and query results in Dragonfly (hot store).  An LRU
// eviction policy ensures bounded memory usage: when the cache reaches
// maxSize entries, the least-recently-used entry is evicted from both
// the LRU tracker and Dragonfly.
//
// All keys are tenant-scoped (paryty:{tenant}:cold:...) to guarantee
// strict tenant isolation in a shared Dragonfly instance.
package cold

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// defaultMaxSize is the maximum number of cached entries when no
	// WithMaxSize option is provided.
	defaultMaxSize = 100

	// defaultTTL is the default TTL for cached entries when no per-entry
	// TTL is provided via Set or WithTTL.
	defaultTTL = 1 * time.Hour

	// scanCount is the COUNT hint for SCAN iterations.
	scanCount int64 = 100
)

// ---- Key generation ----
//
// Every key is namespaced as paryty:{tenant}:cold:{kind}:{id} to guarantee
// strict tenant isolation in shared Dragonfly instances.

// snapshotKey returns the Dragonfly key for a cached snapshot.
//
//	paryty:{tenant}:cold:snapshot:{id}
func snapshotKey(tenant, id string) string {
	return fmt.Sprintf("paryty:%s:cold:snapshot:%s", tenant, id)
}

// queryResultKey returns the Dragonfly key for a cached query result.
//
//	paryty:{tenant}:cold:query:{hash}
func queryResultKey(tenant, hash string) string {
	return fmt.Sprintf("paryty:%s:cold:query:%s", tenant, hash)
}

// coldKeyPattern returns the SCAN glob matching every cold-cache key for a tenant.
//
//	paryty:{tenant}:cold:*
func coldKeyPattern(tenant string) string {
	return fmt.Sprintf("paryty:%s:cold:*", tenant)
}

// ---- Options ----

// CacheOption configures a ColdCache instance.
type CacheOption func(*ColdCache)

// WithMaxSize sets the maximum number of entries the LRU tracker holds.
// When the limit is reached, the least-recently-used entry is evicted
// from both Dragonfly and the in-memory LRU.
func WithMaxSize(n int) CacheOption {
	return func(c *ColdCache) {
		if n > 0 {
			c.maxSize = n
		}
	}
}

// WithTTL overrides the default TTL for cached entries.
func WithTTL(d time.Duration) CacheOption {
	return func(c *ColdCache) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// ---- ColdCache ----

// ColdCache provides an LRU-bounded cache layer for cold storage data.
// Recently accessed entries are stored in Dragonfly; the LRU metadata
// (access order) is held in-process via hashicorp/golang-lru.
//
// The cache is safe for concurrent use.
type ColdCache struct {
	dragonfly *redis.Client
	maxSize   int
	ttl       time.Duration
	logger    *zap.Logger
	mu        sync.Mutex            // protects lru mutations that need atomic read+evict
	lru       *lru.Cache[string, struct{}]
}

// NewColdCache creates a new ColdCache backed by the given Dragonfly client.
// Default max size is 100 entries; default TTL is 1 hour.
func NewColdCache(dragonfly *redis.Client, logger *zap.Logger, opts ...CacheOption) *ColdCache {
	cc := &ColdCache{
		dragonfly: dragonfly,
		maxSize:   defaultMaxSize,
		ttl:       defaultTTL,
		logger:    logger,
	}

	for _, opt := range opts {
		opt(cc)
	}

	// The LRU tracks key access order only (no stored data).  When a key is
	// evicted from the LRU we also delete it from Dragonfly via the eviction
	// callback.
	cache, err := lru.NewWithEvict[string, struct{}](
		cc.maxSize,
		func(key string, _ struct{}) {
			// Best-effort deletion — if Dragonfly is unreachable the entry
			// will expire via TTL anyway.  We cannot use the request context
			// here because the eviction callback fires synchronously from
			// Add, which already holds the LRU internal lock.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			if delErr := cc.dragonfly.Del(ctx, key).Err(); delErr != nil {
				cc.logger.Warn("lru eviction: failed to delete key from dragonfly",
					zap.String("key", key),
					zap.Error(delErr),
				)
			} else {
				cc.logger.Debug("lru eviction: deleted key from dragonfly",
					zap.String("key", key),
				)
			}
		},
	)
	if err != nil {
		// lru.NewWithEvict only returns an error for maxSize <= 0, which is
		// guarded by WithMaxSize.  This is a programming error — panic.
		cc.logger.Fatal("failed to create lru cache", zap.Error(err))
	}

	cc.lru = cache
	return cc
}

// ---- Core CRUD ----

// Get retrieves cached data from Dragonfly for the given tenant and key.
// Returns (nil, nil) when the entry is not in the cache (miss).
//
// On a hit the LRU access order is refreshed so the entry is not evicted.
func (cc *ColdCache) Get(ctx context.Context, tenant, key string) ([]byte, error) {
	if tenant == "" {
		return nil, fmt.Errorf("tenant must not be empty")
	}
	if key == "" {
		return nil, fmt.Errorf("key must not be empty")
	}

	fullKey := snapshotKey(tenant, key)

	// Check LRU membership first (fast path, no network I/O).
	cc.mu.Lock()
	_, exists := cc.lru.Get(fullKey) // Get refreshes access order
	cc.mu.Unlock()

	if !exists {
		// The key is not tracked by the LRU.  It may still exist in Dragonfly
		// (e.g. set by another process or not yet evicted by TTL), but we
		// treat it as a miss to keep the LRU consistent.
		return nil, nil
	}

	data, err := cc.dragonfly.Get(ctx, fullKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key expired in Dragonfly but still tracked by LRU.  Remove
			// the stale LRU entry.
			cc.mu.Lock()
			cc.lru.Remove(fullKey)
			cc.mu.Unlock()

			return nil, nil
		}
		return nil, fmt.Errorf("get %s: %w", fullKey, err)
	}

	cc.logger.Debug("cache hit",
		zap.String("tenant", tenant),
		zap.String("key", key),
		zap.Int("size_bytes", len(data)),
	)

	return data, nil
}

// Set stores data in Dragonfly and registers the key in the LRU tracker.
// If the LRU is full, the oldest entry is evicted before insertion.
//
// When ttl is zero, the cache default TTL is used.
func (cc *ColdCache) Set(ctx context.Context, tenant, key string, data []byte, ttl time.Duration) error {
	if tenant == "" {
		return fmt.Errorf("tenant must not be empty")
	}
	if key == "" {
		return fmt.Errorf("key must not be empty")
	}

	effectiveTTL := ttl
	if effectiveTTL <= 0 {
		effectiveTTL = cc.ttl
	}

	fullKey := snapshotKey(tenant, key)

	// Write to Dragonfly first — if the write fails we don't track the key
	// in the LRU so there's no stale metadata.
	if err := cc.dragonfly.Set(ctx, fullKey, data, effectiveTTL).Err(); err != nil {
		return fmt.Errorf("set %s: %w", fullKey, err)
	}

	// Register in LRU.  If the cache is full, lru.Add will evict the
	// oldest entry and invoke the eviction callback (which deletes it
	// from Dragonfly).
	cc.mu.Lock()
	cc.lru.Add(fullKey, struct{}{})
	cc.mu.Unlock()

	cc.logger.Debug("cache set",
		zap.String("tenant", tenant),
		zap.String("key", key),
		zap.Int("size_bytes", len(data)),
		zap.Duration("ttl", effectiveTTL),
	)

	return nil
}

// GetOrFetch retrieves data from the cache.  On a miss it calls the fetch
// function, stores the result in the cache, and returns it.
//
// This is the primary entry point for cold-store reads that should benefit
// from caching.
func (cc *ColdCache) GetOrFetch(
	ctx context.Context,
	tenant, key string,
	fetch func(ctx context.Context) ([]byte, error),
) ([]byte, error) {
	if fetch == nil {
		return nil, fmt.Errorf("fetch function must not be nil")
	}

	// Attempt cache hit.
	data, err := cc.Get(ctx, tenant, key)
	if err != nil {
		return nil, err
	}
	if data != nil {
		return data, nil
	}

	// Cache miss — call the fetch function.
	data, err = fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	// Cache the fetched result.  Use the default TTL.
	if cacheErr := cc.Set(ctx, tenant, key, data, 0); cacheErr != nil {
		// Log but don't fail the read — the data is valid even if we
		// couldn't cache it.
		cc.logger.Warn("failed to cache fetched data",
			zap.String("tenant", tenant),
			zap.String("key", key),
			zap.Error(cacheErr),
		)
	}

	return data, nil
}

// Invalidate removes a single entry from both Dragonfly and the LRU tracker.
func (cc *ColdCache) Invalidate(ctx context.Context, tenant, key string) error {
	if tenant == "" {
		return fmt.Errorf("tenant must not be empty")
	}
	if key == "" {
		return fmt.Errorf("key must not be empty")
	}

	fullKey := snapshotKey(tenant, key)

	// Remove from LRU first so concurrent Gets see a miss immediately.
	cc.mu.Lock()
	cc.lru.Remove(fullKey)
	cc.mu.Unlock()

	// Remove from Dragonfly.
	if err := cc.dragonfly.Del(ctx, fullKey).Err(); err != nil {
		return fmt.Errorf("del %s: %w", fullKey, err)
	}

	cc.logger.Debug("cache invalidated",
		zap.String("tenant", tenant),
		zap.String("key", key),
	)

	return nil
}

// InvalidatePrefix removes all entries matching a key prefix from both
// Dragonfly and the LRU tracker.  Uses SCAN (not KEYS) to avoid blocking
// Dragonfly.
//
// The prefix is applied within the tenant scope:
//
//	SCAN pattern: paryty:{tenant}:cold:{prefix}*
func (cc *ColdCache) InvalidatePrefix(ctx context.Context, tenant, prefix string) error {
	if tenant == "" {
		return fmt.Errorf("tenant must not be empty")
	}
	if prefix == "" {
		return fmt.Errorf("prefix must not be empty")
	}

	scanPattern := fmt.Sprintf("paryty:%s:cold:%s*", tenant, prefix)
	var cursor uint64
	var deleted int

	for {
		keys, nextCursor, err := cc.dragonfly.Scan(ctx, cursor, scanPattern, scanCount).Result()
		if err != nil {
			return fmt.Errorf("scan %s: %w", scanPattern, err)
		}

		for _, k := range keys {
			// Remove from LRU.
			cc.mu.Lock()
			cc.lru.Remove(k)
			cc.mu.Unlock()

			// Remove from Dragonfly.
			if delErr := cc.dragonfly.Del(ctx, k).Err(); delErr != nil {
				cc.logger.Warn("failed to delete key during prefix invalidation",
					zap.String("key", k),
					zap.Error(delErr),
				)
				continue
			}
			deleted++
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if deleted > 0 {
		cc.logger.Debug("cache prefix invalidated",
			zap.String("tenant", tenant),
			zap.String("prefix", prefix),
			zap.Int("deleted", deleted),
		)
	}

	return nil
}

// Size returns the current number of entries tracked by the LRU.
func (cc *ColdCache) Size() int {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.lru.Len()
}

// Clear removes all cached entries for a tenant from both Dragonfly and the
// LRU tracker.  Uses SCAN to find all tenant-scoped keys.
func (cc *ColdCache) Clear(ctx context.Context, tenant string) error {
	if tenant == "" {
		return fmt.Errorf("tenant must not be empty")
	}

	pattern := coldKeyPattern(tenant)
	var cursor uint64
	var deleted int

	for {
		keys, nextCursor, err := cc.dragonfly.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return fmt.Errorf("scan %s: %w", pattern, err)
		}

		for _, k := range keys {
			// Remove from LRU.
			cc.mu.Lock()
			cc.lru.Remove(k)
			cc.mu.Unlock()

			// Remove from Dragonfly.
			if delErr := cc.dragonfly.Del(ctx, k).Err(); delErr != nil {
				cc.logger.Warn("failed to delete key during clear",
					zap.String("key", k),
					zap.Error(delErr),
				)
				continue
			}
			deleted++
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	cc.logger.Debug("cache cleared",
		zap.String("tenant", tenant),
		zap.Int("deleted", deleted),
	)

	return nil
}
