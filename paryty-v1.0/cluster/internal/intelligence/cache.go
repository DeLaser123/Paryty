package intelligence

import (
	"strings"
	"sync"
	"time"
)

// IntelligenceCache provides a thread-safe TTL-based cache with LRU eviction
// for intelligence results (forecasts and anomaly detections).
//
// V2.0 Migration: Replaces the Python Redis-based cache with an in-process
// LRU cache. This eliminates the Redis dependency for caching intelligence
// results while maintaining the same TTL semantics. For multi-instance
// deployments, the Dragonfly hot tier can be used instead.
type IntelligenceCache struct {
	entries    map[string]*cacheEntry
	order      []string // LRU order: oldest at front, newest at back.
	mu         sync.RWMutex
	defaultTTL time.Duration
	maxEntries int
}

// cacheEntry is a single cached value with expiration.
type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// NewIntelligenceCache creates a new intelligence cache.
//
// Parameters:
//   - defaultTTL: default time-to-live for cached entries
//   - maxEntries: maximum number of entries before LRU eviction
func NewIntelligenceCache(defaultTTL time.Duration, maxEntries int) *IntelligenceCache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}

	return &IntelligenceCache{
		entries:    make(map[string]*cacheEntry),
		defaultTTL: defaultTTL,
		maxEntries: maxEntries,
	}
}

// Get retrieves a value from the cache. Returns the value and true if found
// and not expired, nil and false otherwise.
func (c *IntelligenceCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		// Entry has expired — remove it.
		c.Delete(key)
		return nil, false
	}

	// Move to back of LRU order (most recently used).
	c.mu.Lock()
	c.moveToBack(key)
	c.mu.Unlock()

	return entry.value, true
}

// Set stores a value in the cache with the default TTL.
func (c *IntelligenceCache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL stores a value in the cache with a specific TTL.
func (c *IntelligenceCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update it.
	if _, exists := c.entries[key]; exists {
		c.entries[key] = &cacheEntry{
			value:     value,
			expiresAt: time.Now().Add(ttl),
		}
		c.moveToBack(key)
		return
	}

	// Evict if at capacity.
	for len(c.entries) >= c.maxEntries {
		c.evictOldest()
	}

	c.entries[key] = &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	c.order = append(c.order, key)
}

// Delete removes a value from the cache.
func (c *IntelligenceCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
	c.removeFromOrder(key)
}

// DeleteByPrefix removes all entries whose key starts with the given prefix.
func (c *IntelligenceCache) DeleteByPrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var remaining []string
	for _, key := range c.order {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		} else {
			remaining = append(remaining, key)
		}
	}
	c.order = remaining
}

// Clear removes all entries from the cache.
func (c *IntelligenceCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
	c.order = nil
}

// Size returns the current number of entries in the cache.
func (c *IntelligenceCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// evictOldest removes the oldest (least recently used) entry.
// Must be called with c.mu held.
func (c *IntelligenceCache) evictOldest() {
	if len(c.order) == 0 {
		return
	}

	oldest := c.order[0]
	delete(c.entries, oldest)
	c.order = c.order[1:]
}

// moveToBack moves a key to the back of the LRU order.
// Must be called with c.mu held.
func (c *IntelligenceCache) moveToBack(key string) {
	c.removeFromOrder(key)
	c.order = append(c.order, key)
}

// removeFromOrder removes a key from the LRU order list.
// Must be called with c.mu held.
func (c *IntelligenceCache) removeFromOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
