package hot

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap/zaptest"
)

// setupTestMetricsOps creates a MetricsOps connected to a local Dragonfly/Redis
// instance.  The test is skipped when the server is unreachable so CI runs
// without a live dependency.
func setupTestMetricsOps(t *testing.T) *MetricsOps {
	t.Helper()

	cfg := Config{
		Addr: "localhost:6379",
		TTL:  5 * time.Minute,
	}
	c := New(cfg)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Ping(ctx); err != nil {
		t.Skipf("skipping integration test: Dragonfly not reachable: %v", err)
	}

	logger := zaptest.NewLogger(t)
	return NewMetricsOps(c, logger)
}

// cleanupTestKeys deletes all test keys matching the given pattern.
func cleanupTestKeys(t *testing.T, ops *MetricsOps, tenant string) {
	t.Helper()
	ctx := context.Background()
	pattern := metricTimeSeriesPattern(tenant)
	var cursor uint64
	for {
		keys, nextCursor, err := ops.client.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = ops.client.rdb.Del(ctx, keys...).Err()
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// TestMetricTimeSeriesKey verifies the key format for metric time-series sorted sets.
func TestMetricTimeSeriesKey(t *testing.T) {
	tests := []struct {
		name       string
		tenant     string
		agentID    string
		metricName string
		want       string
	}{
		{
			name:       "standard",
			tenant:     "acme-corp",
			agentID:    "agent-42",
			metricName: "cpu.usage_percent",
			want:       "paryty:acme-corp:metrics:ts:agent-42:cpu.usage_percent",
		},
		{
			name:       "default tenant",
			tenant:     "default",
			agentID:    "agent-1",
			metricName: "memory.used_bytes",
			want:       "paryty:default:metrics:ts:agent-1:memory.used_bytes",
		},
		{
			name:       "device prefixed metric",
			tenant:     "t1",
			agentID:    "h1",
			metricName: "disk./dev/sda.utilization_pct",
			want:       "paryty:t1:metrics:ts:h1:disk./dev/sda.utilization_pct",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := metricTimeSeriesKey(tc.tenant, tc.agentID, tc.metricName)
			if got != tc.want {
				t.Errorf("metricTimeSeriesKey(%q, %q, %q) = %q, want %q",
					tc.tenant, tc.agentID, tc.metricName, got, tc.want)
			}
		})
	}
}

// TestMetricTimeSeriesPattern verifies the SCAN pattern format.
func TestMetricTimeSeriesPattern(t *testing.T) {
	got := metricTimeSeriesPattern("acme-corp")
	want := "paryty:acme-corp:metrics:ts:*"
	if got != want {
		t.Errorf("metricTimeSeriesPattern(%q) = %q, want %q", "acme-corp", got, want)
	}
}

// TestMetricTimeSeriesKey_TenantIsolation verifies that different tenants
// produce different keys for the same agent and metric name.
func TestMetricTimeSeriesKey_TenantIsolation(t *testing.T) {
	keyA := metricTimeSeriesKey("tenant-alpha", "agent-1", "cpu.usage_percent")
	keyB := metricTimeSeriesKey("tenant-beta", "agent-1", "cpu.usage_percent")
	if keyA == keyB {
		t.Errorf("different tenants produced colliding key %q", keyA)
	}
}

// ---- Integration tests (require Dragonfly) ----

// TestMetricsOps_StoreAndQuery stores several metric data points and verifies
// that a time-range query returns them in chronological order (oldest first).
func TestMetricsOps_StoreAndQuery(t *testing.T) {
	ops := setupTestMetricsOps(t)
	tenant := "test-store-query"
	cleanupTestKeys(t, ops, tenant)
	t.Cleanup(func() { cleanupTestKeys(t, ops, tenant) })

	ctx := context.Background()
	agentID := "agent-sq-1"
	metricName := "cpu.usage_percent"
	ttl := 1 * time.Minute

	base := time.Now().Truncate(time.Millisecond).Add(-5 * time.Minute)

	points := []struct {
		value float64
		ts    time.Time
	}{
		{42.5, base},
		{55.0, base.Add(1 * time.Minute)},
		{63.2, base.Add(2 * time.Minute)},
		{71.8, base.Add(3 * time.Minute)},
	}

	for _, p := range points {
		if err := ops.StoreMetricTimeSeries(ctx, tenant, agentID, metricName, p.value, p.ts, ttl); err != nil {
			t.Fatalf("StoreMetricTimeSeries: %v", err)
		}
	}

	// Query the full range.
	start := base.Add(-1 * time.Second)
	end := base.Add(3*time.Minute + 1*time.Second)

	results, err := ops.QueryMetricRange(ctx, tenant, agentID, metricName, start, end)
	if err != nil {
		t.Fatalf("QueryMetricRange: %v", err)
	}

	if len(results) != len(points) {
		t.Fatalf("expected %d results, got %d", len(points), len(results))
	}

	// Verify order (oldest first) and values.
	for i, p := range points {
		if results[i].Value != p.value {
			t.Errorf("point[%d].Value = %f, want %f", i, results[i].Value, p.value)
		}
		if !results[i].Timestamp.Equal(p.ts) {
			t.Errorf("point[%d].Timestamp = %v, want %v", i, results[i].Timestamp, p.ts)
		}
	}
}

// TestMetricsOps_QueryEmptyRange queries a time range with no data and verifies
// an empty (non-nil) slice is returned.
func TestMetricsOps_QueryEmptyRange(t *testing.T) {
	ops := setupTestMetricsOps(t)
	tenant := "test-empty-range"
	cleanupTestKeys(t, ops, tenant)
	t.Cleanup(func() { cleanupTestKeys(t, ops, tenant) })

	ctx := context.Background()
	agentID := "agent-er-1"
	metricName := "cpu.usage_percent"
	ttl := 1 * time.Minute

	base := time.Now().Truncate(time.Millisecond)

	// Store one point at base time.
	if err := ops.StoreMetricTimeSeries(ctx, tenant, agentID, metricName, 50.0, base, ttl); err != nil {
		t.Fatalf("StoreMetricTimeSeries: %v", err)
	}

	// Query a range that does NOT include base.
	start := base.Add(10 * time.Minute)
	end := base.Add(20 * time.Minute)

	results, err := ops.QueryMetricRange(ctx, tenant, agentID, metricName, start, end)
	if err != nil {
		t.Fatalf("QueryMetricRange: %v", err)
	}

	if results == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// TestMetricsOps_PurgeExpired stores old and recent metric data points, purges
// entries older than a short maxAge, and verifies only old entries are removed.
func TestMetricsOps_PurgeExpired(t *testing.T) {
	ops := setupTestMetricsOps(t)
	tenant := "test-purge"
	cleanupTestKeys(t, ops, tenant)
	t.Cleanup(func() { cleanupTestKeys(t, ops, tenant) })

	ctx := context.Background()
	agentID := "agent-purge-1"
	metricName := "memory.used_bytes"
	ttl := 5 * time.Minute

	now := time.Now().Truncate(time.Millisecond)

	// Store "old" entries (10 minutes ago).
	oldBase := now.Add(-10 * time.Minute)
	for i := 0; i < 5; i++ {
		ts := oldBase.Add(time.Duration(i) * time.Second)
		if err := ops.StoreMetricTimeSeries(ctx, tenant, agentID, metricName, float64(100+i), ts, ttl); err != nil {
			t.Fatalf("store old metric %d: %v", i, err)
		}
	}

	// Store "recent" entries (1 minute ago).
	recentBase := now.Add(-1 * time.Minute)
	for i := 0; i < 3; i++ {
		ts := recentBase.Add(time.Duration(i) * time.Second)
		if err := ops.StoreMetricTimeSeries(ctx, tenant, agentID, metricName, float64(200+i), ts, ttl); err != nil {
			t.Fatalf("store recent metric %d: %v", i, err)
		}
	}

	// Purge entries older than 5 minutes.
	removed, err := ops.PurgeExpiredMetrics(ctx, tenant, agentID, metricName, 5*time.Minute)
	if err != nil {
		t.Fatalf("PurgeExpiredMetrics: %v", err)
	}

	if removed != 5 {
		t.Errorf("PurgeExpiredMetrics removed %d, want 5", removed)
	}

	// Verify the recent entries still exist.
	allResults, err := ops.QueryMetricRange(ctx, tenant, agentID, metricName,
		now.Add(-2*time.Minute), now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("QueryMetricRange after purge: %v", err)
	}

	if len(allResults) != 3 {
		t.Errorf("expected 3 results after purge, got %d", len(allResults))
	}
}

// TestMetricsOps_ConcurrentStore exercises the sorted-set ZADD path from
// multiple goroutines simultaneously.  The primary goal is to verify no race
// conditions or panics (run with -race).
func TestMetricsOps_ConcurrentStore(t *testing.T) {
	ops := setupTestMetricsOps(t)
	tenant := "test-concurrent"
	cleanupTestKeys(t, ops, tenant)
	t.Cleanup(func() { cleanupTestKeys(t, ops, tenant) })

	ctx := context.Background()
	metricName := "cpu.usage_percent"
	ttl := 1 * time.Minute
	base := time.Now().Truncate(time.Millisecond).Add(-2 * time.Minute)

	const goroutines = 20
	const pointsPerGoroutine = 50

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*pointsPerGoroutine)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(agentIdx int) {
			defer wg.Done()
			agentID := fmt.Sprintf("agent-concurrent-%d", agentIdx)
			for i := 0; i < pointsPerGoroutine; i++ {
				ts := base.Add(time.Duration(i) * time.Second)
				if err := ops.StoreMetricTimeSeries(ctx, tenant, agentID, metricName,
					float64(agentIdx*1000+i), ts, ttl); err != nil {
					errCh <- fmt.Errorf("agent %d, point %d: %w", agentIdx, i, err)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent store error: %v", err)
	}

	// Verify each agent stored the expected number of points.
	for g := 0; g < goroutines; g++ {
		agentID := fmt.Sprintf("agent-concurrent-%d", g)
		results, err := ops.QueryMetricRange(ctx, tenant, agentID, metricName,
			base.Add(-1*time.Second), base.Add(time.Duration(pointsPerGoroutine)*time.Second))
		if err != nil {
			t.Errorf("query agent %d: %v", g, err)
			continue
		}
		if len(results) != pointsPerGoroutine {
			t.Errorf("agent %d: expected %d points, got %d", g, pointsPerGoroutine, len(results))
		}
	}
}

// TestMetricsOps_BatchStore stores a full MetricBatch and verifies that
// individual time-series keys were created for each metric dimension.
func TestMetricsOps_BatchStore(t *testing.T) {
	ops := setupTestMetricsOps(t)
	tenant := "test-batch"
	cleanupTestKeys(t, ops, tenant)
	t.Cleanup(func() { cleanupTestKeys(t, ops, tenant) })

	ctx := context.Background()
	ttl := 1 * time.Minute
	now := time.Now().Truncate(time.Millisecond).Add(-1 * time.Minute)

	batch := &models.MetricBatch{
		AgentID:   "agent-batch-1",
		Timestamp: now,
		CPU: []models.CPUMetrics{
			{
				AgentID:       "agent-batch-1",
				Timestamp:     now,
				TotalUsagePct: 75.3,
				LoadAvg1m:     2.5,
				LoadAvg5m:     1.8,
				LoadAvg15m:    1.2,
				FrequencyMHz:  3200.0,
			},
		},
		Memory: []models.MemoryMetrics{
			{
				AgentID:        "agent-batch-1",
				Timestamp:      now,
				UsagePercent:   68.9,
				TotalBytes:     16 * 1024 * 1024 * 1024,
				UsedBytes:      11 * 1024 * 1024 * 1024,
				FreeBytes:      5 * 1024 * 1024 * 1024,
				AvailableBytes: 5 * 1024 * 1024 * 1024,
				CachedBytes:    2 * 1024 * 1024 * 1024,
			},
		},
		Disk: []models.DiskMetrics{
			{
				AgentID:        "agent-batch-1",
				Timestamp:      now,
				Device:         "sda",
				TotalBytes:     500 * 1024 * 1024 * 1024,
				UsedBytes:      300 * 1024 * 1024 * 1024,
				FreeBytes:      200 * 1024 * 1024 * 1024,
				UtilizationPct: 60.0,
			},
		},
		Network: []models.NetworkMetrics{
			{
				AgentID:       "agent-batch-1",
				Timestamp:     now,
				Interface:     "eth0",
				RxBytesPerSec: 1024000,
				TxBytesPerSec: 512000,
				SpeedMbps:     1000,
			},
		},
	}

	if err := ops.StoreMetricBatchTimeSeries(ctx, tenant, batch, ttl); err != nil {
		t.Fatalf("StoreMetricBatchTimeSeries: %v", err)
	}

	// Verify representative time-series keys via QueryMetricRange.
	// CPU
	cpuResults, err := ops.QueryMetricRange(ctx, tenant, "agent-batch-1", "cpu.usage_percent",
		now.Add(-1*time.Second), now.Add(1*time.Second))
	if err != nil {
		t.Fatalf("query cpu key: %v", err)
	}
	if len(cpuResults) != 1 {
		t.Errorf("cpu.usage_percent: expected 1 entry, got %d", len(cpuResults))
	}
	if len(cpuResults) == 1 && cpuResults[0].Value != 75.3 {
		t.Errorf("cpu.usage_percent value = %f, want 75.3", cpuResults[0].Value)
	}

	// Memory
	memResults, err := ops.QueryMetricRange(ctx, tenant, "agent-batch-1", "memory.usage_percent",
		now.Add(-1*time.Second), now.Add(1*time.Second))
	if err != nil {
		t.Fatalf("query memory key: %v", err)
	}
	if len(memResults) != 1 {
		t.Errorf("memory.usage_percent: expected 1 entry, got %d", len(memResults))
	}
	if len(memResults) == 1 && memResults[0].Value != 68.9 {
		t.Errorf("memory.usage_percent value = %f, want 68.9", memResults[0].Value)
	}

	// Disk (device-prefixed)
	diskResults, err := ops.QueryMetricRange(ctx, tenant, "agent-batch-1", "disk.sda.utilization_pct",
		now.Add(-1*time.Second), now.Add(1*time.Second))
	if err != nil {
		t.Fatalf("query disk key: %v", err)
	}
	if len(diskResults) != 1 {
		t.Errorf("disk.sda.utilization_pct: expected 1 entry, got %d", len(diskResults))
	}

	// Network (interface-prefixed)
	netResults, err := ops.QueryMetricRange(ctx, tenant, "agent-batch-1", "network.eth0.rx_bytes_per_sec",
		now.Add(-1*time.Second), now.Add(1*time.Second))
	if err != nil {
		t.Fatalf("query network key: %v", err)
	}
	if len(netResults) != 1 {
		t.Errorf("network.eth0.rx_bytes_per_sec: expected 1 entry, got %d", len(netResults))
	}

	// Verify total key count for this tenant via SCAN.
	pattern := metricTimeSeriesPattern(tenant)
	var allKeys []string
	var cursor uint64
	for {
		keys, nextCursor, scanErr := ops.client.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}
		allKeys = append(allKeys, keys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	// Expected keys per metric category (matching StoreMetricBatchTimeSeries field counts):
	// CPU:      usage_percent, load_avg_1m, load_avg_5m, load_avg_15m, frequency_mhz, context_switches = 6
	// Memory:   usage_percent, total_bytes, used_bytes, free_bytes, available_bytes,
	//           cached_bytes, swap_total_bytes, swap_used_bytes = 8
	// Disk:     total_bytes, used_bytes, free_bytes, read_bytes_per_sec, write_bytes_per_sec,
	//           iops_read, iops_write, io_latency_ms, queue_depth, utilization_pct = 10
	// Network:  rx_bytes_per_sec, tx_bytes_per_sec, rx_packets, tx_packets, errors,
	//           estimated_rtt_ms, total_rx_bytes, total_tx_bytes, speed_mbps = 9
	// Total = 6 + 8 + 10 + 9 = 33
	const expectedKeys = 33
	if len(allKeys) != expectedKeys {
		t.Errorf("expected %d unique keys, got %d: %v", expectedKeys, len(allKeys), allKeys)
	}
}

// TestMetricsOps_BatchStoreEmptyBatch verifies that a nil or empty-agent batch
// returns an error (defensive programming).
func TestMetricsOps_BatchStoreValidation(t *testing.T) {
	ops := setupTestMetricsOps(t)
	ctx := context.Background()
	ttl := 1 * time.Minute

	// Nil batch
	err := ops.StoreMetricBatchTimeSeries(ctx, "tenant-x", nil, ttl)
	if err == nil {
		t.Error("expected error for nil batch, got nil")
	}

	// Empty agent ID
	err = ops.StoreMetricBatchTimeSeries(ctx, "tenant-x", &models.MetricBatch{}, ttl)
	if err == nil {
		t.Error("expected error for empty AgentID, got nil")
	}

	// Empty tenant
	err = ops.StoreMetricBatchTimeSeries(ctx, "", &models.MetricBatch{AgentID: "a1"}, ttl)
	if err == nil {
		t.Error("expected error for empty tenant, got nil")
	}
}

// TestMetricsOps_StoreValidation verifies input validation on store/query/purge.
func TestMetricsOps_StoreValidation(t *testing.T) {
	ops := setupTestMetricsOps(t)
	ctx := context.Background()

	// Empty tenant
	err := ops.StoreMetricTimeSeries(ctx, "", "a1", "cpu", 1.0, time.Now(), time.Minute)
	if err == nil {
		t.Error("expected error for empty tenant")
	}

	// Empty agentID
	err = ops.StoreMetricTimeSeries(ctx, "t", "", "cpu", 1.0, time.Now(), time.Minute)
	if err == nil {
		t.Error("expected error for empty agentID")
	}

	// Empty metricName
	err = ops.StoreMetricTimeSeries(ctx, "t", "a1", "", 1.0, time.Now(), time.Minute)
	if err == nil {
		t.Error("expected error for empty metricName")
	}

	// Query validation
	_, err = ops.QueryMetricRange(ctx, "", "a1", "cpu", time.Now(), time.Now())
	if err == nil {
		t.Error("expected error for empty tenant in query")
	}

	// Purge validation
	_, err = ops.PurgeExpiredMetrics(ctx, "", "a1", "cpu", time.Minute)
	if err == nil {
		t.Error("expected error for empty tenant in purge")
	}

	_, err = ops.PurgeAllExpiredMetrics(ctx, "", time.Minute)
	if err == nil {
		t.Error("expected error for empty tenant in purge-all")
	}
}

// TestMetricsOps_PurgeAllExpiredMetrics verifies that PurgeAllExpiredMetrics
// correctly scans and purges across multiple agents and metric names.
func TestMetricsOps_PurgeAllExpiredMetrics(t *testing.T) {
	ops := setupTestMetricsOps(t)
	tenant := "test-purge-all"
	cleanupTestKeys(t, ops, tenant)
	t.Cleanup(func() { cleanupTestKeys(t, ops, tenant) })

	ctx := context.Background()
	ttl := 5 * time.Minute
	now := time.Now().Truncate(time.Millisecond)
	oldBase := now.Add(-10 * time.Minute)

	// Store old entries across 2 agents and 2 metric names.
	agents := []string{"agent-pa-1", "agent-pa-2"}
	metrics := []string{"cpu.usage_percent", "memory.used_bytes"}
	for _, agentID := range agents {
		for _, metric := range metrics {
			for i := 0; i < 3; i++ {
				ts := oldBase.Add(time.Duration(i) * time.Second)
				if err := ops.StoreMetricTimeSeries(ctx, tenant, agentID, metric, float64(i), ts, ttl); err != nil {
					t.Fatalf("store: %v", err)
				}
			}
		}
	}

	// Total: 2 agents * 2 metrics * 3 points = 12 entries
	totalRemoved, err := ops.PurgeAllExpiredMetrics(ctx, tenant, 5*time.Minute)
	if err != nil {
		t.Fatalf("PurgeAllExpiredMetrics: %v", err)
	}

	if totalRemoved != 12 {
		t.Errorf("expected 12 removed, got %d", totalRemoved)
	}

	// Verify nothing remains.
	for _, agentID := range agents {
		for _, metric := range metrics {
			results, qErr := ops.QueryMetricRange(ctx, tenant, agentID, metric,
				oldBase.Add(-1*time.Second), now.Add(1*time.Second))
			if qErr != nil {
				t.Errorf("query %s/%s: %v", agentID, metric, qErr)
				continue
			}
			if len(results) != 0 {
				t.Errorf("agent %s metric %s: expected 0 results after purge, got %d",
					agentID, metric, len(results))
			}
		}
	}
}

// TestMetricsOps_QueryMetricRange verifies that time range boundaries are
// inclusive and that a partial range returns only matching entries.
func TestMetricsOps_QueryMetricRange_Partial(t *testing.T) {
	ops := setupTestMetricsOps(t)
	tenant := "test-partial-range"
	cleanupTestKeys(t, ops, tenant)
	t.Cleanup(func() { cleanupTestKeys(t, ops, tenant) })

	ctx := context.Background()
	agentID := "agent-pr-1"
	metricName := "cpu.usage_percent"
	ttl := 1 * time.Minute

	base := time.Now().Truncate(time.Millisecond).Add(-5 * time.Minute)

	// Store 5 points at 1-minute intervals.
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := ops.StoreMetricTimeSeries(ctx, tenant, agentID, metricName, float64(10+i*10), ts, ttl); err != nil {
			t.Fatalf("store point %d: %v", i, err)
		}
	}

	// Query a sub-range: minutes 1–3 (inclusive).
	start := base.Add(1 * time.Minute)
	end := base.Add(3 * time.Minute)

	results, err := ops.QueryMetricRange(ctx, tenant, agentID, metricName, start, end)
	if err != nil {
		t.Fatalf("QueryMetricRange: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results for partial range, got %d", len(results))
	}

	// Values should be 20, 30, 40.
	expectedValues := []float64{20, 30, 40}
	for i, want := range expectedValues {
		if results[i].Value != want {
			t.Errorf("result[%d].Value = %f, want %f", i, results[i].Value, want)
		}
	}
}

// TestMetricsOps_ScanCount verifies the scanCount constant is set.
func TestMetricTSScanCount(t *testing.T) {
	if metricTSScanCount <= 0 {
		t.Errorf("metricTSScanCount = %d, want > 0", metricTSScanCount)
	}
}

// TestMetricsOps_ScanPatternMatchesKey verifies the SCAN pattern matches
// time-series keys but NOT the existing "latest" metric keys.
func TestMetricTimeSeriesPattern_NoCrossMatch(t *testing.T) {
	tsKey := metricTimeSeriesKey("tenant-a", "agent-1", "cpu.usage_percent")
	latestKey := metricsKey("tenant-a", "agent-1")
	pattern := metricTimeSeriesPattern("tenant-a")

	// The TS key should be matched by the pattern's prefix.
	tsPrefix := pattern[:len(pattern)-1] // remove trailing "*"
	if len(tsKey) < len(tsPrefix) || tsKey[:len(tsPrefix)] != tsPrefix {
		t.Errorf("pattern %q does not match ts key %q", pattern, tsKey)
	}

	// The "latest" key should NOT share the TS prefix.
	if len(latestKey) >= len(tsPrefix) && latestKey[:len(tsPrefix)] == tsPrefix {
		t.Errorf("pattern %q unexpectedly matches latest key %q", pattern, latestKey)
	}
}

// Ensure we reference redis to keep the import (used in cleanupTestKeys indirectly via client).
var _ = redis.Nil
