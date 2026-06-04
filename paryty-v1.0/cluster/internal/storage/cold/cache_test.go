package cold

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap/zaptest"
)

// ---- Test helpers ----

// newTestColdCache creates a ColdCache backed by a miniredis instance.
// The caller should use t.Cleanup for resource management.
func newTestColdCache(t *testing.T, opts ...CacheOption) (*ColdCache, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() { _ = rdb.Close() })

	logger := zaptest.NewLogger(t)
	cc := NewColdCache(rdb, logger, opts...)
	return cc, mr
}

// seedKeys adds n keys to the cache for the given tenant.
// Keys are named "key-0", "key-1", ..., "key-{n-1}".
func seedKeys(t *testing.T, cc *ColdCache, tenant string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key-%d", i)
		data := []byte(fmt.Sprintf("value-%d", i))
		if err := cc.Set(ctx, tenant, key, data, 0); err != nil {
			t.Fatalf("seed Set(%s): %v", key, err)
		}
	}
}

// ---- Key generation tests ----

func TestSnapshotKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tenant string
		id     string
		want   string
	}{
		{
			name:   "standard",
			tenant: "acme-corp",
			id:     "snap-42",
			want:   "paryty:acme-corp:cold:snapshot:snap-42",
		},
		{
			name:   "default tenant",
			tenant: "default",
			id:     "snap-1",
			want:   "paryty:default:cold:snapshot:snap-1",
		},
		{
			name:   "tenant with special chars",
			tenant: "org-123_test",
			id:     "snap-abc",
			want:   "paryty:org-123_test:cold:snapshot:snap-abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := snapshotKey(tc.tenant, tc.id)
			if got != tc.want {
				t.Errorf("snapshotKey(%q, %q) = %q, want %q", tc.tenant, tc.id, got, tc.want)
			}
		})
	}
}

func TestQueryResultKey(t *testing.T) {
	t.Parallel()

	got := queryResultKey("acme-corp", "abc123")
	want := "paryty:acme-corp:cold:query:abc123"
	if got != want {
		t.Errorf("queryResultKey(%q, %q) = %q, want %q", "acme-corp", "abc123", got, want)
	}
}

func TestColdKeyPattern(t *testing.T) {
	t.Parallel()

	got := coldKeyPattern("tenant-a")
	want := "paryty:tenant-a:cold:*"
	if got != want {
		t.Errorf("coldKeyPattern(%q) = %q, want %q", "tenant-a", got, want)
	}
}

// ---- Cache option tests ----

func TestNewColdCache_Defaults(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)

	if cc.maxSize != defaultMaxSize {
		t.Errorf("default maxSize = %d, want %d", cc.maxSize, defaultMaxSize)
	}
	if cc.ttl != defaultTTL {
		t.Errorf("default ttl = %v, want %v", cc.ttl, defaultTTL)
	}
}

func TestNewColdCache_WithMaxSize(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t, WithMaxSize(50))

	if cc.maxSize != 50 {
		t.Errorf("maxSize = %d, want 50", cc.maxSize)
	}
}

func TestNewColdCache_WithTTL(t *testing.T) {
	t.Parallel()

	custom := 30 * time.Minute
	cc, _ := newTestColdCache(t, WithTTL(custom))

	if cc.ttl != custom {
		t.Errorf("ttl = %v, want %v", cc.ttl, custom)
	}
}

func TestNewColdCache_InvalidMaxSize_Ignored(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t, WithMaxSize(-1))

	if cc.maxSize != defaultMaxSize {
		t.Errorf("negative maxSize should be ignored; got %d, want %d", cc.maxSize, defaultMaxSize)
	}
}

func TestNewColdCache_ZeroTTL_Ignored(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t, WithTTL(0))

	if cc.ttl != defaultTTL {
		t.Errorf("zero ttl should be ignored; got %v, want %v", cc.ttl, defaultTTL)
	}
}

// ---- CRUD tests ----

func TestColdCache_SetAndGet(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-set-get"

	// Set a value.
	data := []byte(`{"snapshot":"data","value":42}`)
	if err := cc.Set(ctx, tenant, "snap-1", data, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get the value back.
	got, err := cc.Get(ctx, tenant, "snap-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Get = %q, want %q", got, data)
	}

	// Verify LRU tracks the entry.
	if cc.Size() != 1 {
		t.Errorf("Size = %d, want 1", cc.Size())
	}
}

func TestColdCache_GetMiss(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	got, err := cc.Get(ctx, "tenant-x", "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for cache miss, got %q", got)
	}
}

func TestColdCache_SetCustomTTL(t *testing.T) {
	t.Parallel()

	cc, mr := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-custom-ttl"

	customTTL := 5 * time.Minute
	if err := cc.Set(ctx, tenant, "snap-ttl", []byte("data"), customTTL); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify the key exists in miniredis.
	fullKey := snapshotKey(tenant, "snap-ttl")
	ttl := mr.TTL(fullKey)
	if ttl <= 0 || ttl > customTTL {
		t.Errorf("TTL = %v, expected ~%v", ttl, customTTL)
	}
}

func TestColdCache_SetDefaultTTL(t *testing.T) {
	t.Parallel()

	cc, mr := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-default-ttl"

	// Pass zero TTL — should use the cache default.
	if err := cc.Set(ctx, tenant, "snap-dt", []byte("data"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	fullKey := snapshotKey(tenant, "snap-dt")
	ttl := mr.TTL(fullKey)
	if ttl <= 0 || ttl > defaultTTL {
		t.Errorf("TTL = %v, expected ~%v", ttl, defaultTTL)
	}
}

func TestColdCache_SetOverwrite(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-overwrite"

	if err := cc.Set(ctx, tenant, "snap-ow", []byte("first"), 0); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := cc.Set(ctx, tenant, "snap-ow", []byte("second"), 0); err != nil {
		t.Fatalf("Set second: %v", err)
	}

	got, err := cc.Get(ctx, tenant, "snap-ow")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("Get = %q, want %q", got, "second")
	}
}

// ---- GetOrFetch tests ----

func TestColdCache_GetOrFetch_CacheHit(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-gof-hit"

	// Pre-populate the cache.
	if err := cc.Set(ctx, tenant, "snap-hit", []byte("cached"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// GetOrFetch should return the cached value without calling fetch.
	called := false
	got, err := cc.GetOrFetch(ctx, tenant, "snap-hit", func(_ context.Context) ([]byte, error) {
		called = true
		return []byte("fetched"), nil
	})
	if err != nil {
		t.Fatalf("GetOrFetch: %v", err)
	}
	if called {
		t.Error("fetch function should not be called on cache hit")
	}
	if string(got) != "cached" {
		t.Errorf("GetOrFetch = %q, want %q", got, "cached")
	}
}

func TestColdCache_GetOrFetch_CacheMiss(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-gof-miss"

	called := false
	got, err := cc.GetOrFetch(ctx, tenant, "snap-miss", func(_ context.Context) ([]byte, error) {
		called = true
		return []byte("fetched"), nil
	})
	if err != nil {
		t.Fatalf("GetOrFetch: %v", err)
	}
	if !called {
		t.Error("fetch function should be called on cache miss")
	}
	if string(got) != "fetched" {
		t.Errorf("GetOrFetch = %q, want %q", got, "fetched")
	}

	// Verify the result was cached.
	got2, err := cc.Get(ctx, tenant, "snap-miss")
	if err != nil {
		t.Fatalf("Get after GetOrFetch: %v", err)
	}
	if string(got2) != "fetched" {
		t.Errorf("cached value = %q, want %q", got2, "fetched")
	}
}

func TestColdCache_GetOrFetch_FetchError(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-gof-err"

	_, err := cc.GetOrFetch(ctx, tenant, "snap-err", func(_ context.Context) ([]byte, error) {
		return nil, fmt.Errorf("fetch failed")
	})
	if err == nil {
		t.Fatal("expected error from fetch, got nil")
	}

	// Verify nothing was cached.
	if cc.Size() != 0 {
		t.Errorf("Size = %d, want 0 after failed fetch", cc.Size())
	}
}

func TestColdCache_GetOrFetch_NilFetchFn(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	_, err := cc.GetOrFetch(ctx, "tenant-x", "snap-nil", nil)
	if err == nil {
		t.Fatal("expected error for nil fetch function, got nil")
	}
}

// ---- Eviction tests ----

func TestColdCache_Eviction(t *testing.T) {
	t.Parallel()

	// Cache with max size 3.
	cc, _ := newTestColdCache(t, WithMaxSize(3))
	ctx := context.Background()
	tenant := "test-evict"

	// Fill the cache.
	seedKeys(t, cc, tenant, 3)

	if cc.Size() != 3 {
		t.Fatalf("Size = %d, want 3", cc.Size())
	}

	// Access key-1 and key-2 to refresh their LRU order.
	// key-0 is now the least recently used.
	_, _ = cc.Get(ctx, tenant, "key-1")
	_, _ = cc.Get(ctx, tenant, "key-2")

	// Insert key-3 — should evict key-0.
	if err := cc.Set(ctx, tenant, "key-3", []byte("value-3"), 0); err != nil {
		t.Fatalf("Set key-3: %v", err)
	}

	// Size should still be 3.
	if cc.Size() != 3 {
		t.Errorf("Size = %d, want 3 after eviction", cc.Size())
	}

	// key-0 should be evicted.
	got0, err := cc.Get(ctx, tenant, "key-0")
	if err != nil {
		t.Fatalf("Get key-0: %v", err)
	}
	if got0 != nil {
		t.Errorf("key-0 should be evicted, but got %q", got0)
	}

	// key-3 should be present.
	got3, err := cc.Get(ctx, tenant, "key-3")
	if err != nil {
		t.Fatalf("Get key-3: %v", err)
	}
	if string(got3) != "value-3" {
		t.Errorf("key-3 = %q, want %q", got3, "value-3")
	}
}

func TestColdCache_Eviction_MultipleEvictions(t *testing.T) {
	t.Parallel()

	// Cache with max size 2.
	cc, _ := newTestColdCache(t, WithMaxSize(2))
	ctx := context.Background()
	tenant := "test-multi-evict"

	// Insert 5 entries — 3 should be evicted.
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		data := []byte(fmt.Sprintf("value-%d", i))
		if err := cc.Set(ctx, tenant, key, data, 0); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}

	if cc.Size() != 2 {
		t.Errorf("Size = %d, want 2", cc.Size())
	}

	// Only the last 2 entries should remain.
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		got, err := cc.Get(ctx, tenant, key)
		if err != nil {
			t.Fatalf("Get %s: %v", key, err)
		}
		if i >= 3 {
			// Should be present.
			if got == nil {
				t.Errorf("key-%d should be present, got nil", i)
			}
		}
	}
}

// ---- Invalidate tests ----

func TestColdCache_Invalidate(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-inv"

	if err := cc.Set(ctx, tenant, "snap-del", []byte("data"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := cc.Invalidate(ctx, tenant, "snap-del"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	got, err := cc.Get(ctx, tenant, "snap-del")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after invalidation, got %q", got)
	}
}

func TestColdCache_Invalidate_NonExistent(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	// Invalidating a non-existent key should not error.
	if err := cc.Invalidate(ctx, "tenant-x", "nonexistent"); err != nil {
		t.Fatalf("Invalidate non-existent: %v", err)
	}
}

// ---- InvalidatePrefix tests ----

func TestColdCache_InvalidatePrefix(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-inv-prefix"

	// Store entries with different key kinds.
	if err := cc.Set(ctx, tenant, "snap-a", []byte("a"), 0); err != nil {
		t.Fatalf("Set snap-a: %v", err)
	}
	if err := cc.Set(ctx, tenant, "snap-b", []byte("b"), 0); err != nil {
		t.Fatalf("Set snap-b: %v", err)
	}

	// Verify both exist.
	if cc.Size() != 2 {
		t.Fatalf("Size = %d, want 2", cc.Size())
	}

	// Invalidate all snapshot keys.
	if err := cc.InvalidatePrefix(ctx, tenant, "snapshot:"); err != nil {
		t.Fatalf("InvalidatePrefix: %v", err)
	}

	// Both should be gone.
	gotA, _ := cc.Get(ctx, tenant, "snap-a")
	gotB, _ := cc.Get(ctx, tenant, "snap-b")
	if gotA != nil {
		t.Errorf("snap-a should be invalidated, got %q", gotA)
	}
	if gotB != nil {
		t.Errorf("snap-b should be invalidated, got %q", gotB)
	}
}

func TestColdCache_InvalidatePrefix_NoMatch(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-inv-nomatch"

	if err := cc.Set(ctx, tenant, "snap-x", []byte("x"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Invalidate with a prefix that doesn't match.
	if err := cc.InvalidatePrefix(ctx, tenant, "nonexistent:"); err != nil {
		t.Fatalf("InvalidatePrefix: %v", err)
	}

	// The original entry should still exist.
	got, _ := cc.Get(ctx, tenant, "snap-x")
	if string(got) != "x" {
		t.Errorf("snap-x should still exist, got %q", got)
	}
}

// ---- Size tests ----

func TestColdCache_Size(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-size"

	if cc.Size() != 0 {
		t.Errorf("initial Size = %d, want 0", cc.Size())
	}

	if err := cc.Set(ctx, tenant, "k1", []byte("v1"), 0); err != nil {
		t.Fatalf("Set k1: %v", err)
	}
	if cc.Size() != 1 {
		t.Errorf("Size after 1 set = %d, want 1", cc.Size())
	}

	if err := cc.Set(ctx, tenant, "k2", []byte("v2"), 0); err != nil {
		t.Fatalf("Set k2: %v", err)
	}
	if cc.Size() != 2 {
		t.Errorf("Size after 2 sets = %d, want 2", cc.Size())
	}
}

// ---- Clear tests ----

func TestColdCache_Clear(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-clear"

	seedKeys(t, cc, tenant, 5)

	if cc.Size() != 5 {
		t.Fatalf("Size = %d, want 5", cc.Size())
	}

	if err := cc.Clear(ctx, tenant); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if cc.Size() != 0 {
		t.Errorf("Size after Clear = %d, want 0", cc.Size())
	}

	// Verify all keys are gone from Dragonfly.
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%d", i)
		got, _ := cc.Get(ctx, tenant, key)
		if got != nil {
			t.Errorf("key-%d should be cleared, got %q", i, got)
		}
	}
}

func TestColdCache_Clear_NonExistentTenant(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	// Clearing a non-existent tenant should not error.
	if err := cc.Clear(ctx, "nonexistent-tenant"); err != nil {
		t.Fatalf("Clear non-existent tenant: %v", err)
	}
}

// ---- Tenant isolation tests ----

func TestColdCache_TenantIsolation(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	// Tenant A: store "snap-1" with value "alpha".
	if err := cc.Set(ctx, "tenant-a", "snap-1", []byte("alpha"), 0); err != nil {
		t.Fatalf("Set tenant-a: %v", err)
	}

	// Tenant B: store "snap-1" with value "beta".
	if err := cc.Set(ctx, "tenant-b", "snap-1", []byte("beta"), 0); err != nil {
		t.Fatalf("Set tenant-b: %v", err)
	}

	// Verify isolation.
	gotA, err := cc.Get(ctx, "tenant-a", "snap-1")
	if err != nil {
		t.Fatalf("Get tenant-a: %v", err)
	}
	gotB, err := cc.Get(ctx, "tenant-b", "snap-1")
	if err != nil {
		t.Fatalf("Get tenant-b: %v", err)
	}

	if string(gotA) != "alpha" {
		t.Errorf("tenant-a = %q, want %q", gotA, "alpha")
	}
	if string(gotB) != "beta" {
		t.Errorf("tenant-b = %q, want %q", gotB, "beta")
	}
}

func TestColdCache_TenantIsolation_ClearOnlyTargetTenant(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	// Store keys for two tenants.
	if err := cc.Set(ctx, "tenant-a", "snap-1", []byte("alpha"), 0); err != nil {
		t.Fatalf("Set tenant-a: %v", err)
	}
	if err := cc.Set(ctx, "tenant-b", "snap-1", []byte("beta"), 0); err != nil {
		t.Fatalf("Set tenant-b: %v", err)
	}

	// Clear only tenant-a.
	if err := cc.Clear(ctx, "tenant-a"); err != nil {
		t.Fatalf("Clear tenant-a: %v", err)
	}

	// tenant-a should be empty.
	gotA, _ := cc.Get(ctx, "tenant-a", "snap-1")
	if gotA != nil {
		t.Errorf("tenant-a should be cleared, got %q", gotA)
	}

	// tenant-b should be unaffected.
	gotB, _ := cc.Get(ctx, "tenant-b", "snap-1")
	if string(gotB) != "beta" {
		t.Errorf("tenant-b should be unaffected, got %q", gotB)
	}
}

func TestColdCache_TenantIsolation_KeysContainTenantSegment(t *testing.T) {
	t.Parallel()

	keys := []struct {
		name string
		key  string
	}{
		{"snapshotKey", snapshotKey("acme-corp", "snap-1")},
		{"queryResultKey", queryResultKey("acme-corp", "hash-1")},
	}

	for _, k := range keys {
		expectedPrefix := "paryty:acme-corp:cold:"
		if len(k.key) < len(expectedPrefix) || k.key[:len(expectedPrefix)] != expectedPrefix {
			t.Errorf("%s = %q does not have prefix %q", k.name, k.key, expectedPrefix)
		}
	}
}

// ---- Nil data tests ----

func TestColdCache_NilData_Get(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	got, err := cc.Get(ctx, "tenant-x", "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %q", got)
	}
}

func TestColdCache_NilData_SetNilSlice(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-nil-set"

	// Setting nil data should succeed (stored as empty value in Redis).
	if err := cc.Set(ctx, tenant, "snap-nil", nil, 0); err != nil {
		t.Fatalf("Set nil: %v", err)
	}

	// Should retrieve nil (redis returns empty bytes for nil values).
	got, err := cc.Get(ctx, tenant, "snap-nil")
	if err != nil {
		t.Fatalf("Get nil: %v", err)
	}
	// Note: go-redis may return empty []byte{} for nil SET, which is fine.
	if got != nil && len(got) > 0 {
		t.Errorf("expected empty/nil data, got %q", got)
	}
}

func TestColdCache_NilData_SetEmptySlice(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()
	tenant := "test-empty-set"

	if err := cc.Set(ctx, tenant, "snap-empty", []byte{}, 0); err != nil {
		t.Fatalf("Set empty: %v", err)
	}

	got, err := cc.Get(ctx, tenant, "snap-empty")
	if err != nil {
		t.Fatalf("Get empty: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

// ---- Validation tests ----

func TestColdCache_Get_EmptyTenant(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	_, err := cc.Get(ctx, "", "key")
	if err == nil {
		t.Error("expected error for empty tenant, got nil")
	}
}

func TestColdCache_Get_EmptyKey(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	_, err := cc.Get(ctx, "tenant", "")
	if err == nil {
		t.Error("expected error for empty key, got nil")
	}
}

func TestColdCache_Set_EmptyTenant(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	err := cc.Set(ctx, "", "key", []byte("data"), 0)
	if err == nil {
		t.Error("expected error for empty tenant, got nil")
	}
}

func TestColdCache_Set_EmptyKey(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	err := cc.Set(ctx, "tenant", "", []byte("data"), 0)
	if err == nil {
		t.Error("expected error for empty key, got nil")
	}
}

func TestColdCache_Invalidate_EmptyTenant(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	err := cc.Invalidate(ctx, "", "key")
	if err == nil {
		t.Error("expected error for empty tenant, got nil")
	}
}

func TestColdCache_InvalidatePrefix_EmptyTenant(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	err := cc.InvalidatePrefix(ctx, "", "prefix")
	if err == nil {
		t.Error("expected error for empty tenant, got nil")
	}
}

func TestColdCache_Clear_EmptyTenant(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t)
	ctx := context.Background()

	err := cc.Clear(ctx, "")
	if err == nil {
		t.Error("expected error for empty tenant, got nil")
	}
}

// ---- Concurrent access tests ----

func TestColdCache_ConcurrentSetAndGet(t *testing.T) {
	t.Parallel()

	cc, _ := newTestColdCache(t, WithMaxSize(50))
	ctx := context.Background()
	tenant := "test-concurrent"

	const goroutines = 10
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*opsPerGoroutine)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("g%d-key-%d", id, i)
				data := []byte(fmt.Sprintf("g%d-value-%d", id, i))

				if err := cc.Set(ctx, tenant, key, data, 0); err != nil {
					errCh <- fmt.Errorf("goroutine %d, Set %s: %w", id, key, err)
					continue
				}

				got, err := cc.Get(ctx, tenant, key)
				if err != nil {
					errCh <- fmt.Errorf("goroutine %d, Get %s: %w", id, key, err)
					continue
				}
				if got != nil && string(got) != string(data) {
					errCh <- fmt.Errorf("goroutine %d, Get %s: got %q, want %q", id, key, got, data)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

// ---- Key isolation tests ----

func TestColdCache_SnapshotKey_ContainsTenant(t *testing.T) {
	t.Parallel()

	key := snapshotKey("tenant-x", "snap-1")
	prefix := "paryty:tenant-x:cold:snapshot:"
	if len(key) < len(prefix) || key[:len(prefix)] != prefix {
		t.Errorf("snapshotKey = %q, expected prefix %q", key, prefix)
	}
}

func TestColdCache_QueryResultKey_ContainsTenant(t *testing.T) {
	t.Parallel()

	key := queryResultKey("tenant-x", "hash-1")
	prefix := "paryty:tenant-x:cold:query:"
	if len(key) < len(prefix) || key[:len(prefix)] != prefix {
		t.Errorf("queryResultKey = %q, expected prefix %q", key, prefix)
	}
}

func TestColdCache_SnapshotKey_DifferentTenantsNeverCollide(t *testing.T) {
	t.Parallel()

	keyA := snapshotKey("tenant-a", "same-id")
	keyB := snapshotKey("tenant-b", "same-id")
	if keyA == keyB {
		t.Errorf("different tenants produced colliding key %q", keyA)
	}
}

func TestColdCache_QueryResultKey_DifferentTenantsNeverCollide(t *testing.T) {
	t.Parallel()

	keyA := queryResultKey("tenant-a", "same-hash")
	keyB := queryResultKey("tenant-b", "same-hash")
	if keyA == keyB {
		t.Errorf("different tenants produced colliding key %q", keyA)
	}
}
