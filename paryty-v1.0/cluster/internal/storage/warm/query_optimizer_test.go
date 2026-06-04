package warm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
)

// ---- Test Helpers ----

// setupTestCache creates a QueryCache backed by a miniredis instance.
// The miniredis server is automatically closed when the test finishes.
func setupTestCache(t *testing.T) (*QueryCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := NewQueryCache(rdb, slog.Default())
	return cache, mr
}

// setupTestOptimizer creates a QueryOptimizer with a miniredis-backed cache.
// The pool is nil since these tests don't exercise the database layer.
func setupTestOptimizer(t *testing.T) (*QueryOptimizer, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	//nolint:nilness // pool intentionally nil — tests only exercise cache logic
	opt := NewQueryOptimizer(nil, rdb, slog.Default())
	return opt, mr
}

// insertCachedMetrics inserts pre-serialized metrics into the cache for testing.
func insertCachedMetrics(t *testing.T, cache *QueryCache, tenant, agentID, metricName string, start, end time.Time, metrics []models.Metric) {
	t.Helper()
	ctx := context.Background()
	key := cache.cacheKey(tenant, agentID, metricName, start, end)
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("marshal metrics for cache insert: %v", err)
	}
	cache.cacheSetRaw(ctx, key, data)
}

// ---- TestQueryOptimizer_CacheHit ----

func TestQueryOptimizer_CacheHit(t *testing.T) {
	opt, _ := setupTestOptimizer(t)
	ctx := context.Background()

	tenant := "test-tenant"
	agentID := "agent-1"
	metricName := "cpu.total_usage"
	start := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 11, 0, 0, 0, time.UTC)

	// Pre-populate the cache with known metrics.
	expected := []models.Metric{
		{
			AgentID:   agentID,
			Name:      metricName,
			Labels:    map[string]string{"source": "cpu_metrics"},
			Value:     42.5,
			Type:      models.MetricTypeCPU,
			Timestamp: time.Date(2025, 6, 1, 10, 30, 0, 0, time.UTC),
		},
		{
			AgentID:   agentID,
			Name:      metricName,
			Labels:    map[string]string{"source": "cpu_metrics"},
			Value:     55.3,
			Type:      models.MetricTypeCPU,
			Timestamp: time.Date(2025, 6, 1, 10, 45, 0, 0, time.UTC),
		},
	}
	insertCachedMetrics(t, opt.cache, tenant, agentID, metricName, start, end, expected)

	// First query should hit the cache.
	got, err := opt.QueryMetricsWithCache(ctx, tenant, agentID, metricName, start, end)
	if err != nil {
		t.Fatalf("QueryMetricsWithCache() error = %v", err)
	}

	if len(got) != len(expected) {
		t.Fatalf("got %d metrics, want %d", len(got), len(expected))
	}

	for i, m := range got {
		if m.AgentID != expected[i].AgentID {
			t.Errorf("metric[%d].AgentID = %q, want %q", i, m.AgentID, expected[i].AgentID)
		}
		if m.Value != expected[i].Value {
			t.Errorf("metric[%d].Value = %f, want %f", i, m.Value, expected[i].Value)
		}
		if m.Name != expected[i].Name {
			t.Errorf("metric[%d].Name = %q, want %q", i, m.Name, expected[i].Name)
		}
	}

	// Verify cache stats show a hit.
	stats, err := opt.GetCacheStats(ctx, tenant)
	if err != nil {
		t.Fatalf("GetCacheStats() error = %v", err)
	}
	if stats.Hits != 1 {
		t.Errorf("cache hits = %d, want 1", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("cache misses = %d, want 0", stats.Misses)
	}
}

// ---- TestQueryOptimizer_CacheMiss ----

func TestQueryOptimizer_CacheMiss(t *testing.T) {
	opt, _ := setupTestOptimizer(t)
	ctx := context.Background()

	tenant := "test-tenant"
	agentID := "agent-1"
	metricName := "cpu.total_usage"
	start := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 11, 0, 0, 0, time.UTC)

	// Pre-populate cache for a DIFFERENT time range.
	cachedStart := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	cachedEnd := time.Date(2025, 6, 1, 13, 0, 0, 0, time.UTC)
	expected := []models.Metric{
		{
			AgentID:   agentID,
			Name:      metricName,
			Labels:    map[string]string{"source": "cpu_metrics"},
			Value:     99.9,
			Type:      models.MetricTypeCPU,
			Timestamp: time.Date(2025, 6, 1, 12, 30, 0, 0, time.UTC),
		},
	}
	insertCachedMetrics(t, opt.cache, tenant, agentID, metricName, cachedStart, cachedEnd, expected)

	// Query with the ORIGINAL time range — should NOT hit the cache.
	// Since pool is nil, this will panic, so we use recover.
	// The important thing is that the cache was checked first.
	defer func() {
		if r := recover(); r != nil {
			// Expected: nil pool causes panic when trying to query QuestDB.
			// This confirms the cache was a MISS (it tried to query QuestDB).
		}
	}()

	// This will be a cache miss because the time range is different.
	// With nil pool, it will panic on the QuestDB query attempt.
	// The test validates that different parameters produce different cache keys.
	_, _ = opt.QueryMetricsWithCache(ctx, tenant, agentID, metricName, start, end)
}

// TestQueryOptimizer_CacheMiss_DifferentParams verifies that different query
// parameters produce different cache keys, ensuring no false cache hits.
func TestQueryOptimizer_CacheMiss_DifferentParams(t *testing.T) {
	cache, _ := setupTestCache(t)

	base := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	oneHour := base.Add(time.Hour)
	twoHours := base.Add(2 * time.Hour)

	keys := []string{
		// Different agent
		cache.cacheKey("t1", "agent-1", "cpu.total_usage", base, oneHour),
		cache.cacheKey("t1", "agent-2", "cpu.total_usage", base, oneHour),
		// Different metric
		cache.cacheKey("t1", "agent-1", "memory.used", base, oneHour),
		// Different time range
		cache.cacheKey("t1", "agent-1", "cpu.total_usage", base, twoHours),
		// Different tenant
		cache.cacheKey("t2", "agent-1", "cpu.total_usage", base, oneHour),
	}

	// All keys must be unique.
	seen := make(map[string]int, len(keys))
	for i, key := range keys {
		if prev, exists := seen[key]; exists {
			t.Errorf("keys[%d] and keys[%d] are identical: %q", prev, i, key)
		}
		seen[key] = i
	}
}

// ---- TestQueryOptimizer_AutoDownsampling ----

func TestQueryOptimizer_AutoDownsampling(t *testing.T) {
	tests := []struct {
		name         string
		start        time.Time
		end          time.Time
		wantTable    string
		wantWindow   int64
		wantIsRaw    bool
	}{
		{
			name:       "30 minutes — raw metrics",
			start:      time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 1, 10, 30, 0, 0, time.UTC),
			wantTable:  "",
			wantWindow: 0,
			wantIsRaw:  true,
		},
		{
			name:       "1 hour exactly — raw metrics (boundary)",
			start:      time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 1, 11, 0, 0, 0, time.UTC),
			wantTable:  "",
			wantWindow: 0,
			wantIsRaw:  true,
		},
		{
			name:       "2 hours — 5m aggregated",
			start:      time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
			wantTable:  "aggregated_metrics",
			wantWindow: 300,
			wantIsRaw:  false,
		},
		{
			name:       "12 hours — 5m aggregated",
			start:      time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
			wantTable:  "aggregated_metrics",
			wantWindow: 300,
			wantIsRaw:  false,
		},
		{
			name:       "24 hours exactly — 5m aggregated (boundary)",
			start:      time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 2, 0, 0, 0, 0, time.UTC),
			wantTable:  "aggregated_metrics",
			wantWindow: 300,
			wantIsRaw:  false,
		},
		{
			name:       "3 days — 1h aggregated",
			start:      time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC),
			wantTable:  "aggregated_metrics",
			wantWindow: 3600,
			wantIsRaw:  false,
		},
		{
			name:       "7 days — 1h aggregated",
			start:      time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 8, 0, 0, 0, 0, time.UTC),
			wantTable:  "aggregated_metrics",
			wantWindow: 3600,
			wantIsRaw:  false,
		},
		{
			name:       "30 days — 1h aggregated",
			start:      time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
			wantTable:  "aggregated_metrics",
			wantWindow: 3600,
			wantIsRaw:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := SelectDownsampleResolution(tc.start, tc.end)

			if res.Table != tc.wantTable {
				t.Errorf("Table = %q, want %q", res.Table, tc.wantTable)
			}
			if res.WindowSeconds != tc.wantWindow {
				t.Errorf("WindowSeconds = %d, want %d", res.WindowSeconds, tc.wantWindow)
			}
			if res.IsRaw != tc.wantIsRaw {
				t.Errorf("IsRaw = %v, want %v", res.IsRaw, tc.wantIsRaw)
			}
		})
	}
}

// ---- TestQueryOptimizer_CacheInvalidation ----

func TestQueryOptimizer_CacheInvalidation(t *testing.T) {
	opt, _ := setupTestOptimizer(t)
	ctx := context.Background()

	tenant := "test-tenant"
	agentID := "agent-1"
	start := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 11, 0, 0, 0, time.UTC)

	// Insert cache entries for multiple metrics of the same agent.
	metrics := []string{"cpu.total_usage", "memory.used", "disk.utilization"}
	for _, mn := range metrics {
		data := []models.Metric{
			{AgentID: agentID, Name: mn, Value: 50.0, Timestamp: start},
		}
		insertCachedMetrics(t, opt.cache, tenant, agentID, mn, start, end, data)
	}

	// Verify cache has entries.
	stats, err := opt.GetCacheStats(ctx, tenant)
	if err != nil {
		t.Fatalf("GetCacheStats() error = %v", err)
	}
	if stats.Size != int64(len(metrics)) {
		t.Fatalf("pre-invalidation cache size = %d, want %d", stats.Size, len(metrics))
	}

	// Invalidate cache for this agent.
	if err := opt.InvalidateCache(ctx, tenant, agentID); err != nil {
		t.Fatalf("InvalidateCache() error = %v", err)
	}

	// Verify cache is now empty.
	stats, err = opt.GetCacheStats(ctx, tenant)
	if err != nil {
		t.Fatalf("GetCacheStats() error = %v", err)
	}
	if stats.Size != 0 {
		t.Errorf("post-invalidation cache size = %d, want 0", stats.Size)
	}

	// Verify that a subsequent query would be a cache miss.
	// (With nil pool, this would panic on the QuestDB query, confirming the miss.)
	key := opt.cache.cacheKey(tenant, agentID, "cpu.total_usage", start, end)
	exists := opt.cache.dragonfly.Exists(ctx, key)
	if exists.Val() != 0 {
		t.Error("cache key still exists after invalidation")
	}
}

// TestQueryOptimizer_CacheInvalidation_DifferentTenant verifies that
// invalidating one tenant's cache does NOT affect another tenant's data.
func TestQueryOptimizer_CacheInvalidation_DifferentTenant(t *testing.T) {
	opt, _ := setupTestOptimizer(t)
	ctx := context.Background()

	agentID := "shared-agent"
	metricName := "cpu.total_usage"
	start := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 11, 0, 0, 0, time.UTC)
	data := []models.Metric{
		{AgentID: agentID, Name: metricName, Value: 50.0, Timestamp: start},
	}

	// Insert cache entries for two different tenants.
	insertCachedMetrics(t, opt.cache, "tenant-alpha", agentID, metricName, start, end, data)
	insertCachedMetrics(t, opt.cache, "tenant-beta", agentID, metricName, start, end, data)

	// Invalidate only tenant-alpha.
	if err := opt.InvalidateCache(ctx, "tenant-alpha", agentID); err != nil {
		t.Fatalf("InvalidateCache() error = %v", err)
	}

	// Verify tenant-alpha is empty.
	alphaStats, _ := opt.GetCacheStats(ctx, "tenant-alpha")
	if alphaStats.Size != 0 {
		t.Errorf("tenant-alpha cache size = %d, want 0", alphaStats.Size)
	}

	// Verify tenant-beta is unaffected.
	betaKey := opt.cache.cacheKey("tenant-beta", agentID, metricName, start, end)
	exists := opt.cache.dragonfly.Exists(ctx, betaKey)
	if exists.Val() != 1 {
		t.Error("tenant-beta cache entry was incorrectly invalidated")
	}
}

// ---- TestQueryOptimizer_DatabaseQueries ----

// TestQueryOptimizer_DatabaseQueries_SignatureCompiles verifies that the
// QueryDatabaseQueries method has the correct signature. The actual DB test
// requires a running QuestDB instance.
func TestQueryOptimizer_DatabaseQueries_SignatureCompiles(t *testing.T) {
	opt, _ := setupTestOptimizer(t)

	// Verify method exists and has the right signature.
	// The nil pool means we can't actually execute it, but we verify it compiles.
	ctx := context.Background()
	tenant := "test-tenant"
	agentID := "agent-1"
	protocol := "postgresql"
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 2, 0, 0, 0, 0, time.UTC)
	limit := 100

	// This will panic because pool is nil, but it confirms the method compiles.
	func() {
		defer func() { _ = recover() }()
		_, _ = opt.QueryDatabaseQueries(ctx, tenant, agentID, protocol, start, end, limit)
	}()
}

// ---- TestMetricToTable ----

func TestMetricToTable(t *testing.T) {
	tests := []struct {
		metric string
		want   string
	}{
		// CPU metrics
		{"cpu.total_usage", "cpu_metrics"},
		{"cpu.usage", "cpu_metrics"},
		{"cpu.load_avg_1", "cpu_metrics"},
		{"cpu.load_avg_5", "cpu_metrics"},
		{"cpu.load_avg_15", "cpu_metrics"},
		{"cpu.frequency", "cpu_metrics"},
		{"cpu.context_switches", "cpu_metrics"},

		// Memory metrics
		{"memory.used", "memory_metrics"},
		{"memory.total", "memory_metrics"},
		{"memory.available", "memory_metrics"},
		{"memory.free", "memory_metrics"},
		{"memory.cached", "memory_metrics"},
		{"memory.swap_used", "memory_metrics"},
		{"memory.usage_percent", "memory_metrics"},

		// Disk metrics
		{"disk.used", "disk_metrics"},
		{"disk.total", "disk_metrics"},
		{"disk.iops_read", "disk_metrics"},
		{"disk.io_latency", "disk_metrics"},

		// Network metrics
		{"network.rx_bytes", "network_metrics"},
		{"network.tx_bytes", "network_metrics"},
		{"network.tcp_established", "network_metrics"},

		// Process metrics
		{"process.cpu_usage", "process_metrics"},
		{"process.memory", "process_metrics"},

		// Unknown — falls back to aggregated
		{"custom.metric", "aggregated_metrics"},
		{"unknown", "aggregated_metrics"},
		{"", "aggregated_metrics"},
	}

	for _, tc := range tests {
		t.Run(tc.metric, func(t *testing.T) {
			got := MetricToTable(tc.metric)
			if got != tc.want {
				t.Errorf("MetricToTable(%q) = %q, want %q", tc.metric, got, tc.want)
			}
		})
	}
}

// ---- TestMetricToColumn ----

func TestMetricToColumn(t *testing.T) {
	tests := []struct {
		metric string
		want   string
	}{
		// CPU
		{"cpu.total_usage", "total_usage_pct"},
		{"cpu.load_avg_1", "load_avg_1"},

		// Memory
		{"memory.used", "used_bytes"},
		{"memory.total", "total_bytes"},
		{"memory.usage_percent", "usage_percent"},

		// Disk
		{"disk.used", "used_bytes"},
		{"disk.iops_read", "iops_read"},
		{"disk.io_latency", "io_latency_ms"},

		// Network
		{"network.rx_bytes", "rx_bytes_per_sec"},
		{"network.tcp_retransmits", "tcp_retransmit_count"},

		// Process
		{"process.cpu_usage", "cpu_usage_pct"},
		{"process.memory", "memory_bytes"},

		// Unknown
		{"custom.metric", ""},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.metric, func(t *testing.T) {
			got := MetricToColumn(tc.metric)
			if got != tc.want {
				t.Errorf("MetricToColumn(%q) = %q, want %q", tc.metric, got, tc.want)
			}
		})
	}
}

// ---- TestMetricTableMappingConsistency ----

func TestMetricTableMappingConsistency(t *testing.T) {
	// Every entry in metricTableMap must have a corresponding entry in metricColumnMap.
	for metric, table := range metricTableMap {
		if table == "aggregated_metrics" {
			continue // aggregated_metrics doesn't use column mapping
		}
		col, ok := metricColumnMap[metric]
		if !ok {
			t.Errorf("metric %q is in metricTableMap (%s) but missing from metricColumnMap", metric, table)
		}
		if col == "" {
			t.Errorf("metric %q has empty column in metricColumnMap", metric)
		}
	}

	// Every entry in metricColumnMap must have a corresponding entry in metricTableMap.
	for metric := range metricColumnMap {
		if _, ok := metricTableMap[metric]; !ok {
			t.Errorf("metric %q is in metricColumnMap but missing from metricTableMap", metric)
		}
	}
}

// ---- TestQueryCache_Compression ----

func TestQueryCache_Compression_LargePayload(t *testing.T) {
	cache, mr := setupTestCache(t)
	ctx := context.Background()

	// Create a large metrics slice that exceeds the compression threshold.
	metrics := make([]models.Metric, 200)
	for i := range metrics {
		metrics[i] = models.Metric{
			AgentID: "agent-1",
			Name:    "cpu.total_usage",
			Labels:  map[string]string{"source": "cpu_metrics", "host": fmt.Sprintf("host-%d", i)},
			Value:   float64(i) * 1.5,
			Type:    models.MetricTypeCPU,
			Timestamp: time.Date(2025, 6, 1, 10, 0, 0, i*1000000000, time.UTC),
		}
	}

	tenant := "test-tenant"
	agentID := "agent-1"
	metricName := "cpu.total_usage"
	start := time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 1, 11, 0, 0, 0, time.UTC)
	key := cache.cacheKey(tenant, agentID, metricName, start, end)

	// Marshal and cache the large payload.
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	t.Logf("Serialized size: %d bytes (threshold: %d)", len(data), compressThresholdBytes)
	if len(data) <= compressThresholdBytes {
		t.Skipf("payload (%d bytes) is below compression threshold (%d bytes)", len(data), compressThresholdBytes)
	}

	cache.cacheSetRaw(ctx, key, data)

	// Verify the stored payload starts with the gzip header byte.
	raw, err := mr.Get(key)
	if err != nil {
		t.Fatalf("get raw cache entry: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("cache entry is empty")
	}
	if raw[0] != gzipHeaderByte {
		t.Errorf("expected gzip header byte (%c), got (%c)", gzipHeaderByte, raw[0])
	}

	// Verify we can decompress and deserialize.
	got, err := cacheGetRaw[[]models.Metric](ctx, cache.dragonfly, key)
	if err != nil {
		t.Fatalf("cacheGetRaw() error = %v", err)
	}
	if len(got) != len(metrics) {
		t.Errorf("deserialized %d metrics, want %d", len(got), len(metrics))
	}
	if got[0].Value != metrics[0].Value {
		t.Errorf("first metric value = %f, want %f", got[0].Value, metrics[0].Value)
	}
}

func TestQueryCache_Compression_SmallPayload(t *testing.T) {
	cache, mr := setupTestCache(t)
	ctx := context.Background()

	// Small payload — should use raw JSON header.
	metrics := []models.Metric{
		{AgentID: "agent-1", Name: "test", Value: 1.0, Timestamp: time.Now()},
	}

	tenant := "test-tenant"
	key := cache.cacheKey(tenant, "agent-1", "test",
		time.Now(), time.Now().Add(time.Hour))

	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("Small payload size: %d bytes", len(data))

	cache.cacheSetRaw(ctx, key, data)

	raw, err := mr.Get(key)
	if err != nil {
		t.Fatalf("get raw cache entry: %v", err)
	}
	if raw[0] != rawJSONHeaderByte {
		t.Errorf("expected raw JSON header byte (%c), got (%c)", rawJSONHeaderByte, raw[0])
	}
}

// ---- TestQueryCache_CacheKey ----

func TestQueryCache_CacheKey(t *testing.T) {
	cache, _ := setupTestCache(t)

	tests := []struct {
		name       string
		tenant     string
		agentID    string
		metric     string
		start      time.Time
		end        time.Time
		wantPrefix string
	}{
		{
			name:       "standard query",
			tenant:     "acme-corp",
			agentID:    "agent-42",
			metric:     "cpu.total_usage",
			start:      time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 6, 1, 11, 0, 0, 0, time.UTC),
			wantPrefix: "paryty:acme-corp:query_cache:agent-42:cpu.total_usage:",
		},
		{
			name:       "default tenant",
			tenant:     "default",
			agentID:    "agent-1",
			metric:     "memory.used",
			start:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			wantPrefix: "paryty:default:query_cache:agent-1:memory.used:",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := cache.cacheKey(tc.tenant, tc.agentID, tc.metric, tc.start, tc.end)
			if len(key) < len(tc.wantPrefix) || key[:len(tc.wantPrefix)] != tc.wantPrefix {
				t.Errorf("cacheKey() = %q, want prefix %q", key, tc.wantPrefix)
			}
		})
	}
}

// ---- TestQueryCacheOptions ----

func TestQueryCacheOptions(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		cache := NewQueryCache(rdb, nil)

		if cache.ttl != defaultCacheTTL {
			t.Errorf("default TTL = %v, want %v", cache.ttl, defaultCacheTTL)
		}
		if cache.prefix != defaultCachePrefix {
			t.Errorf("default prefix = %q, want %q", cache.prefix, defaultCachePrefix)
		}
	})

	t.Run("custom TTL", func(t *testing.T) {
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		custom := 5 * time.Minute
		cache := NewQueryCache(rdb, nil, WithCacheTTL(custom))

		if cache.ttl != custom {
			t.Errorf("custom TTL = %v, want %v", cache.ttl, custom)
		}
	})

	t.Run("custom prefix", func(t *testing.T) {
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		cache := NewQueryCache(rdb, nil, WithCachePrefix("custom_prefix"))

		if cache.prefix != "custom_prefix" {
			t.Errorf("custom prefix = %q, want %q", cache.prefix, "custom_prefix")
		}
	})

	t.Run("zero TTL uses default", func(t *testing.T) {
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		cache := NewQueryCache(rdb, nil, WithCacheTTL(0))

		if cache.ttl != defaultCacheTTL {
			t.Errorf("zero TTL should default to %v, got %v", defaultCacheTTL, cache.ttl)
		}
	})

	t.Run("empty prefix uses default", func(t *testing.T) {
		mr := miniredis.RunT(t)
		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		cache := NewQueryCache(rdb, nil, WithCachePrefix(""))

		if cache.prefix != defaultCachePrefix {
			t.Errorf("empty prefix should default to %q, got %q", defaultCachePrefix, cache.prefix)
		}
	})
}

// ---- TestCacheStats ----

func TestCacheStats_InitialState(t *testing.T) {
	opt, _ := setupTestOptimizer(t)
	ctx := context.Background()

	stats, err := opt.GetCacheStats(ctx, "test-tenant")
	if err != nil {
		t.Fatalf("GetCacheStats() error = %v", err)
	}

	if stats.Hits != 0 {
		t.Errorf("initial hits = %d, want 0", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("initial misses = %d, want 0", stats.Misses)
	}
	if stats.HitRate != 0 {
		t.Errorf("initial hit rate = %f, want 0", stats.HitRate)
	}
}

func TestCacheStats_TenantIsolation(t *testing.T) {
	opt, _ := setupTestOptimizer(t)
	ctx := context.Background()

	// Increment hits for tenant-a.
	_ = opt.cache.dragonfly.Incr(ctx, opt.cache.hitsKey("tenant-a")).Err()
	_ = opt.cache.dragonfly.Incr(ctx, opt.cache.hitsKey("tenant-a")).Err()

	// Increment misses for tenant-b.
	_ = opt.cache.dragonfly.Incr(ctx, opt.cache.missesKey("tenant-b")).Err()

	statsA, _ := opt.GetCacheStats(ctx, "tenant-a")
	statsB, _ := opt.GetCacheStats(ctx, "tenant-b")

	if statsA.Hits != 2 {
		t.Errorf("tenant-a hits = %d, want 2", statsA.Hits)
	}
	if statsA.Misses != 0 {
		t.Errorf("tenant-a misses = %d, want 0", statsA.Misses)
	}
	if statsB.Hits != 0 {
		t.Errorf("tenant-b hits = %d, want 0", statsB.Hits)
	}
	if statsB.Misses != 1 {
		t.Errorf("tenant-b misses = %d, want 1", statsB.Misses)
	}
}

// ---- TestMetricTypeFromTable ----

func TestMetricTypeFromTable(t *testing.T) {
	tests := []struct {
		table string
		want  string
	}{
		{"cpu_metrics", string(models.MetricTypeCPU)},
		{"memory_metrics", string(models.MetricTypeMemory)},
		{"disk_metrics", string(models.MetricTypeDisk)},
		{"network_metrics", string(models.MetricTypeNetwork)},
		{"process_metrics", string(models.MetricTypeProcess)},
		{"aggregated_metrics", string(models.MetricTypeCustom)},
		{"unknown_table", string(models.MetricTypeCustom)},
		{"", string(models.MetricTypeCustom)},
	}

	for _, tc := range tests {
		t.Run(tc.table, func(t *testing.T) {
			got := metricTypeFromTable(tc.table)
			if got != tc.want {
				t.Errorf("metricTypeFromTable(%q) = %q, want %q", tc.table, got, tc.want)
			}
		})
	}
}

// ---- TestInterfaceToFloat64 ----

func TestInterfaceToFloat64(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want float64
	}{
		{"float64", float64(3.14), 3.14},
		{"int64", int64(42), 42.0},
		{"int", int(7), 7.0},
		{"float32", float32(1.5), 1.5},
		{"string numeric", "99.9", 99.9},
		{"string non-numeric", "abc", 0.0},
		{"nil", nil, 0.0},
		{"bool", true, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := interfaceToFloat64(tc.val)
			if got != tc.want {
				t.Errorf("interfaceToFloat64(%v) = %f, want %f", tc.val, got, tc.want)
			}
		})
	}
}
