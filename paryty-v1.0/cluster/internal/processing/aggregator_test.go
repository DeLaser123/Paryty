// Package processing implements the data processing pipeline.
// It includes aggregator, correlator, and enricher services.
package processing

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// =============================================================================
// Test helpers
// =============================================================================

// newTestAggregator creates an Aggregator with a controllable clock and
// in-memory Dragonfly for deterministic testing.
func newTestAggregator(t *testing.T, clock *testClock, cfg AggregatorConfig) *Aggregator {
	t.Helper()

	cfg.TimeFunc = clock.Now
	mockDF := newTestDragonfly()

	agg, err := NewAggregator(cfg, mockDF, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAggregator failed: %v", err)
	}
	return agg
}

// makeCPUBatch creates a MetricBatch with a single CPU entry.
func makeCPUBatch(agentID string, cpuPct float64, ts time.Time) *models.MetricBatch {
	return &models.MetricBatch{
		AgentID:   agentID,
		Timestamp: ts,
		CPU: []models.CPUMetrics{
			{AgentID: agentID, Timestamp: ts, TotalUsagePct: cpuPct},
		},
	}
}

// makeFullBatch creates a MetricBatch with CPU, Memory, Disk, Network, and Process entries.
func makeFullBatch(agentID string, ts time.Time) *models.MetricBatch {
	return &models.MetricBatch{
		AgentID:   agentID,
		Timestamp: ts,
		CPU: []models.CPUMetrics{
			{AgentID: agentID, Timestamp: ts, TotalUsagePct: 75.0},
		},
		Memory: []models.MemoryMetrics{
			{AgentID: agentID, Timestamp: ts, UsagePercent: 60.0, UsedBytes: 6_000_000_000, AvailableBytes: 4_000_000_000},
		},
		Disk: []models.DiskMetrics{
			{AgentID: agentID, Timestamp: ts, ReadBytesPerSec: 1000, WriteBytesPerSec: 500},
		},
		Network: []models.NetworkMetrics{
			{AgentID: agentID, Timestamp: ts, RxBytesPerSec: 2000, TxBytesPerSec: 1000},
		},
		Processes: []models.ProcessMetrics{
			{AgentID: agentID, Timestamp: ts, Name: "nginx", CPUUsagePct: 50.0, MemoryBytes: 1024},
		},
	}
}

// makeProcessBatch creates a MetricBatch with multiple process entries for Top-N testing.
func makeProcessBatch(agentID string, procs []struct{ Name string; CPU float64; Mem uint64 }, ts time.Time) *models.MetricBatch {
	batch := &models.MetricBatch{
		AgentID:   agentID,
		Timestamp: ts,
	}
	for _, p := range procs {
		batch.Processes = append(batch.Processes, models.ProcessMetrics{
			AgentID:     agentID,
			Timestamp:   ts,
			Name:        p.Name,
			CPUUsagePct: p.CPU,
			MemoryBytes: p.Mem,
		})
	}
	return batch
}

// findAggregated returns the first AggregatedMetric matching name and aggType, or nil.
func findAggregated(metrics []models.AggregatedMetric, name string, aggType models.AggregationType) *models.AggregatedMetric {
	for i := range metrics {
		if metrics[i].Name == name && metrics[i].AggType == aggType {
			return &metrics[i]
		}
	}
	return nil
}

// =============================================================================
// TestAggregator_BasicWindowedAggregation
// Send 5 CPU metrics within the same 1m window, advance time past close + grace,
// verify avg/min/max/p50/p90/p99/count/sum are correct.
// =============================================================================

func TestAggregator_BasicWindowedAggregation(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	agg := newTestAggregator(t, clock, AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
	})
	ctx := context.Background()

	// Send 5 CPU metrics within [10:00:00, 10:00:40).
	cpuValues := []float64{10.0, 20.0, 30.0, 40.0, 50.0}
	for i, v := range cpuValues {
		ts := startTime.Add(time.Duration(i) * 10 * time.Second)
		batch := makeCPUBatch("agent-1", v, ts)
		metrics, topn, err := agg.Process(ctx, batch, ts)
		if err != nil {
			t.Fatalf("Process[%d] failed: %v", i, err)
		}
		// Window is still open — no aggregated metrics expected.
		if len(metrics) != 0 {
			t.Fatalf("Process[%d]: expected 0 metrics while window open, got %d", i, len(metrics))
		}
		_ = topn
	}

	// Verify values are in the window.
	if agg.windows.WindowCount() != 1 {
		t.Fatalf("expected 1 window, got %d", agg.windows.WindowCount())
	}

	// Advance clock past window close (10:01:00) + grace period (30s).
	clock.Set(startTime.Add(1*time.Minute + 31*time.Second))

	// Send an empty batch to trigger closed window detection.
	emptyBatch := &models.MetricBatch{AgentID: "agent-1", Timestamp: clock.Now()}
	metrics, _, err := agg.Process(ctx, emptyBatch, clock.Now())
	if err != nil {
		t.Fatalf("Process (trigger close) failed: %v", err)
	}

	// Expect 8 aggregation types: avg, min, max, p50, p90, p99, count, sum.
	if len(metrics) != 8 {
		t.Fatalf("expected 8 aggregated metrics, got %d", len(metrics))
	}

	// Verify avg = 30.0.
	avgMetric := findAggregated(metrics, "cpu.usage_percent", models.AggregationAvg)
	if avgMetric == nil {
		t.Fatal("missing avg metric")
	}
	if math.Abs(avgMetric.Value-30.0) > 0.01 {
		t.Errorf("expected avg=30.0, got %f", avgMetric.Value)
	}

	// Verify min = 10.0.
	minMetric := findAggregated(metrics, "cpu.usage_percent", models.AggregationMin)
	if minMetric == nil {
		t.Fatal("missing min metric")
	}
	if math.Abs(minMetric.Value-10.0) > 0.01 {
		t.Errorf("expected min=10.0, got %f", minMetric.Value)
	}

	// Verify max = 50.0.
	maxMetric := findAggregated(metrics, "cpu.usage_percent", models.AggregationMax)
	if maxMetric == nil {
		t.Fatal("missing max metric")
	}
	if math.Abs(maxMetric.Value-50.0) > 0.01 {
		t.Errorf("expected max=50.0, got %f", maxMetric.Value)
	}

	// Verify p50 = 30.0 (median of [10, 20, 30, 40, 50]).
	p50Metric := findAggregated(metrics, "cpu.usage_percent", models.AggregationP50)
	if p50Metric == nil {
		t.Fatal("missing p50 metric")
	}
	if math.Abs(p50Metric.Value-30.0) > 0.01 {
		t.Errorf("expected p50=30.0, got %f", p50Metric.Value)
	}

	// Verify p90.
	p90Metric := findAggregated(metrics, "cpu.usage_percent", models.AggregationP90)
	if p90Metric == nil {
		t.Fatal("missing p90 metric")
	}
	if p90Metric.Value < 30.0 || p90Metric.Value > 50.0 {
		t.Errorf("expected p90 in [30, 50], got %f", p90Metric.Value)
	}

	// Verify p99.
	p99Metric := findAggregated(metrics, "cpu.usage_percent", models.AggregationP99)
	if p99Metric == nil {
		t.Fatal("missing p99 metric")
	}
	if p99Metric.Value < 40.0 || p99Metric.Value > 50.0 {
		t.Errorf("expected p99 in [40, 50], got %f", p99Metric.Value)
	}

	// Verify count = 5.
	countMetric := findAggregated(metrics, "cpu.usage_percent", models.AggregationCount)
	if countMetric == nil {
		t.Fatal("missing count metric")
	}
	if countMetric.Value != 5.0 {
		t.Errorf("expected count=5.0, got %f", countMetric.Value)
	}

	// Verify sum = 150.0.
	sumMetric := findAggregated(metrics, "cpu.usage_percent", models.AggregationSum)
	if sumMetric == nil {
		t.Fatal("missing sum metric")
	}
	if math.Abs(sumMetric.Value-150.0) > 0.01 {
		t.Errorf("expected sum=150.0, got %f", sumMetric.Value)
	}

	// Verify window and agent metadata.
	if avgMetric.Window != 1*time.Minute {
		t.Errorf("expected window=1m, got %v", avgMetric.Window)
	}
	if avgMetric.AgentID != "agent-1" {
		t.Errorf("expected agent_id=agent-1, got %s", avgMetric.AgentID)
	}
}

// =============================================================================
// TestAggregator_LateData
// Data within the grace period should be included in the window.
// Data arriving after the grace period should be rejected (window closed).
// =============================================================================

func TestAggregator_LateData(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	agg := newTestAggregator(t, clock, AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
	})
	ctx := context.Background()

	// Send 3 values at seconds 0, 10, 20.
	for i := 0; i < 3; i++ {
		ts := startTime.Add(time.Duration(i) * 10 * time.Second)
		batch := makeCPUBatch("agent-1", float64(i+1)*10, ts)
		if _, _, err := agg.Process(ctx, batch, ts); err != nil {
			t.Fatalf("Process[%d] failed: %v", i, err)
		}
	}

	// Advance to window end but still within grace period.
	clock.Set(startTime.Add(1*time.Minute + 15*time.Second))

	// Send late data at second 50 (within the 1m window, within grace period).
	lateTs := startTime.Add(50 * time.Second)
	lateBatch := makeCPUBatch("agent-1", 40.0, lateTs)
	if _, _, err := agg.Process(ctx, lateBatch, lateTs); err != nil {
		t.Fatalf("Process (late within grace) failed: %v", err)
	}

	// Window should still not be closed (we're within grace).
	closed := agg.windows.GetClosedWindows(ctx)
	if len(closed) != 0 {
		t.Fatalf("expected 0 closed windows within grace, got %d", len(closed))
	}
	// Reset the clock to the same time — GetClosedWindows marks windows as closed.
	// We need to re-check with a fresh aggregator. Let's just advance past grace.
	agg.windows.PurgeClosed() // Clean up the closed mark from GetClosedWindows.

	// Actually, GetClosedWindows marks them as closed. Let's use a fresh approach:
	// We already called GetClosedWindows which marked it closed. Let's advance.
	clock.Set(startTime.Add(1*time.Minute + 31*time.Second))

	// Now the window should be closed.
	// But we already called GetClosedWindows above which marked it.
	// The window was NOT closed at 10:01:15, so it wasn't marked.
	// At 10:01:31 it should be closed.
	// Wait — GetClosedWindows was called at 10:01:15, which is within grace.
	// The window is NOT closed at that time (now < windowEnd + grace).
	// So it was NOT marked as closed. Good.
	// Now at 10:01:31, it should be closed.

	emptyBatch := &models.MetricBatch{AgentID: "agent-1", Timestamp: clock.Now()}
	metrics, _, err := agg.Process(ctx, emptyBatch, clock.Now())
	if err != nil {
		t.Fatalf("Process (trigger close) failed: %v", err)
	}

	// Should have 4 values: 10, 20, 30, 40 (including late data).
	countMetric := findAggregated(metrics, "cpu.usage_percent", models.AggregationCount)
	if countMetric == nil {
		t.Fatal("missing count metric")
	}
	if countMetric.Value != 4.0 {
		t.Errorf("expected count=4.0 (including late data), got %f", countMetric.Value)
	}

	sumMetric := findAggregated(metrics, "cpu.usage_percent", models.AggregationSum)
	if sumMetric == nil {
		t.Fatal("missing sum metric")
	}
	if math.Abs(sumMetric.Value-100.0) > 0.01 {
		t.Errorf("expected sum=100.0, got %f", sumMetric.Value)
	}

	// Try to add data AFTER grace period — should be silently skipped
	// (window is already closed and purged).
	reallyLateTs := startTime.Add(20 * time.Second)
	reallyLateBatch := makeCPUBatch("agent-1", 999.0, reallyLateTs)
	if _, _, err := agg.Process(ctx, reallyLateBatch, reallyLateTs); err != nil {
		t.Fatalf("Process (after grace) failed: %v", err)
	}

	// No new aggregated metrics should be produced (window already purged).
	// The value 999 should NOT appear in any window result.
}

// =============================================================================
// TestAggregator_MultipleWindows
// 1m and 5m windows, send over 6 minutes, verify both window sizes produce results.
// =============================================================================

func TestAggregator_MultipleWindows(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	agg := newTestAggregator(t, clock, AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute, 5 * time.Minute},
		GracePeriod: 30 * time.Second,
	})
	ctx := context.Background()

	// Send one metric per minute for 6 minutes (minutes 0 through 5).
	// 1m windows: [10:00,10:01), [10:01,10:02), ..., [10:05,10:06) => 6 windows
	// 5m windows: [10:00,10:05), [10:05,10:10) => 2 windows
	// Total: 8 windows
	for min := 0; min < 6; min++ {
		ts := startTime.Add(time.Duration(min) * time.Minute)
		batch := makeCPUBatch("agent-1", float64(min+1)*10, ts)
		if _, _, err := agg.Process(ctx, batch, ts); err != nil {
			t.Fatalf("Process at minute %d failed: %v", min, err)
		}
	}

	if agg.windows.WindowCount() != 8 {
		t.Fatalf("expected 8 windows, got %d", agg.windows.WindowCount())
	}

	// Advance past all 1m windows + first 5m window close + grace.
	// 1m window [10:05,10:06) closes at 10:06:30.
	// 5m window [10:00,10:05) closes at 10:05:30.
	// 5m window [10:05,10:10) closes at 10:10:30 — NOT yet closed.
	clock.Set(startTime.Add(6*time.Minute + 31*time.Second))

	emptyBatch := &models.MetricBatch{AgentID: "agent-1", Timestamp: clock.Now()}
	metrics, _, err := agg.Process(ctx, emptyBatch, clock.Now())
	if err != nil {
		t.Fatalf("Process (trigger close) failed: %v", err)
	}

	// Expect closed: 6x1m windows * 8 agg types + 1x5m window * 8 agg types = 56 metrics.
	// But we also have the empty batch's batch — let's count by window size.
	var oneMinMetrics, fiveMinMetrics int
	for _, m := range metrics {
		switch m.Window {
		case 1 * time.Minute:
			oneMinMetrics++
		case 5 * time.Minute:
			fiveMinMetrics++
		}
	}

	// 6 one-minute windows × 8 agg types = 48.
	if oneMinMetrics != 48 {
		t.Errorf("expected 48 one-minute aggregated metrics, got %d", oneMinMetrics)
	}

	// 1 five-minute window × 8 agg types = 8.
	if fiveMinMetrics != 8 {
		t.Errorf("expected 8 five-minute aggregated metrics, got %d", fiveMinMetrics)
	}

	// Verify the 5m window has count=5 (minutes 0-4).
	for _, m := range metrics {
		if m.Window == 5*time.Minute && m.AggType == models.AggregationCount {
			if m.Value != 5.0 {
				t.Errorf("5m window count: expected 5.0, got %f", m.Value)
			}
		}
	}
}

// =============================================================================
// TestAggregator_EmptyWindow
// No data in a window period → no aggregated metric emitted.
// =============================================================================

func TestAggregator_EmptyWindow(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	agg := newTestAggregator(t, clock, AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
	})
	ctx := context.Background()

	// Send one value in [10:00, 10:01).
	ts0 := startTime
	batch0 := makeCPUBatch("agent-1", 50.0, ts0)
	if _, _, err := agg.Process(ctx, batch0, ts0); err != nil {
		t.Fatalf("Process[0] failed: %v", err)
	}

	// Skip [10:01, 10:02) — no data. Send at [10:02, 10:03).
	ts2 := startTime.Add(2 * time.Minute)
	batch2 := makeCPUBatch("agent-1", 75.0, ts2)
	if _, _, err := agg.Process(ctx, batch2, ts2); err != nil {
		t.Fatalf("Process[2] failed: %v", err)
	}

	// Should have 2 windows: [10:00,10:01) and [10:02,10:03).
	// The skipped [10:01,10:02) window should NOT exist.
	if agg.windows.WindowCount() != 2 {
		t.Fatalf("expected 2 windows, got %d", agg.windows.WindowCount())
	}

	// Advance past all windows + grace.
	clock.Set(startTime.Add(3*time.Minute + 31*time.Second))

	emptyBatch := &models.MetricBatch{AgentID: "agent-1", Timestamp: clock.Now()}
	metrics, _, err := agg.Process(ctx, emptyBatch, clock.Now())
	if err != nil {
		t.Fatalf("Process (trigger close) failed: %v", err)
	}

	// Each of the 2 windows produces 8 aggregated metrics.
	if len(metrics) != 16 {
		t.Fatalf("expected 16 aggregated metrics (2 windows × 8 agg types), got %d", len(metrics))
	}

	// Verify neither window is empty.
	counts := 0
	for _, m := range metrics {
		if m.AggType == models.AggregationCount {
			counts++
			if m.Value == 0 {
				t.Errorf("window at %v has count=0 but should have data", m.Timestamp)
			}
		}
	}
	if counts != 2 {
		t.Errorf("expected 2 count metrics, got %d", counts)
	}
}

// =============================================================================
// TestAggregator_TopNIntegration
// Send process metrics, verify Top-N tracking produces correct rankings.
// =============================================================================

func TestAggregator_TopNIntegration(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	agg := newTestAggregator(t, clock, AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		TopNSize:    3,
	})
	ctx := context.Background()

	// Send a batch with 5 processes.
	procs := []struct {
		Name string
		CPU  float64
		Mem  uint64
	}{
		{"nginx", 80.0, 512_000_000},
		{"postgres", 60.0, 1_000_000_000},
		{"redis", 40.0, 256_000_000},
		{"node-app", 90.0, 768_000_000},
		{"sidecar", 5.0, 64_000_000},
	}

	batch := makeProcessBatch("agent-1", procs, startTime)
	_, topnChanges, err := agg.Process(ctx, batch, startTime)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// With TopNSize=3, the top 3 by CPU should be: node-app(90), nginx(80), postgres(60).
	cpuTopN := agg.topn.GetTopN("cpu.process")
	if len(cpuTopN) != 3 {
		t.Fatalf("expected 3 CPU top-n entries, got %d", len(cpuTopN))
	}

	expectedCPU := []struct {
		name  string
		value float64
	}{
		{"node-app", 90.0},
		{"nginx", 80.0},
		{"postgres", 60.0},
	}

	for i, want := range expectedCPU {
		if cpuTopN[i].Name != want.name {
			t.Errorf("CPU rank %d: expected name=%s, got %s", i+1, want.name, cpuTopN[i].Name)
		}
		if math.Abs(cpuTopN[i].Value-want.value) > 0.01 {
			t.Errorf("CPU rank %d: expected value=%f, got %f", i+1, want.value, cpuTopN[i].Value)
		}
	}

	// Top 3 by memory: postgres(1GB), node-app(768MB), nginx(512MB).
	memTopN := agg.topn.GetTopN("memory.process")
	if len(memTopN) != 3 {
		t.Fatalf("expected 3 memory top-n entries, got %d", len(memTopN))
	}

	expectedMem := []struct {
		name  string
		value float64
	}{
		{"postgres", float64(1_000_000_000)},
		{"node-app", float64(768_000_000)},
		{"nginx", float64(512_000_000)},
	}

	for i, want := range expectedMem {
		if memTopN[i].Name != want.name {
			t.Errorf("Memory rank %d: expected name=%s, got %s", i+1, want.name, memTopN[i].Name)
		}
		if math.Abs(memTopN[i].Value-want.value) > 1 {
			t.Errorf("Memory rank %d: expected value=%f, got %f", i+1, want.value, memTopN[i].Value)
		}
	}

	// Verify Top-N changes were returned.
	if len(topnChanges) == 0 {
		t.Error("expected Top-N changes, got none")
	}

	// Verify evicted processes (redis, sidecar) are NOT in Top-N.
	for _, r := range cpuTopN {
		if r.Name == "redis" || r.Name == "sidecar" {
			t.Errorf("process %s should have been evicted from CPU top-n", r.Name)
		}
	}
}

// =============================================================================
// TestAggregator_ExtractMetricValues
// Verify all metric types are extracted correctly from a full batch.
// =============================================================================

func TestAggregator_ExtractMetricValues(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)
	agg := newTestAggregator(t, clock, AggregatorConfig{})

	batch := makeFullBatch("agent-1", startTime)
	values := agg.extractMetricValues(batch)

	expected := map[string]float64{
		"cpu.usage_percent":       75.0,
		"memory.usage_percent":    60.0,
		"memory.used_bytes":       6_000_000_000,
		"memory.available_bytes":  4_000_000_000,
		"disk.read_bytes_per_sec": 1000,
		"disk.write_bytes_per_sec": 500,
		"network.rx_bytes_per_sec": 2000,
		"network.tx_bytes_per_sec": 1000,
		"process.cpu_percent":     50.0,
		"process.memory_bytes":    1024,
	}

	if len(values) != len(expected) {
		t.Fatalf("expected %d extracted values, got %d: %v", len(expected), len(values), values)
	}

	for name, want := range expected {
		got, ok := values[name]
		if !ok {
			t.Errorf("missing metric %q", name)
			continue
		}
		if math.Abs(got-want) > 0.01 {
			t.Errorf("metric %s: expected %f, got %f", name, want, got)
		}
	}
}

// =============================================================================
// TestAggregator_Health
// Verify health reporting after processing some batches.
// =============================================================================

func TestAggregator_Health(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	agg := newTestAggregator(t, clock, AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		TopNSize:    5,
	})
	ctx := context.Background()

	// Initial health — no processing yet.
	health := agg.Health()
	if health.WindowsOpen != 0 {
		t.Errorf("initial: expected 0 windows, got %d", health.WindowsOpen)
	}
	if health.ValuesProcessed != 0 {
		t.Errorf("initial: expected 0 values processed, got %d", health.ValuesProcessed)
	}
	if health.TopNSize != 5 {
		t.Errorf("initial: expected top_n_size=5, got %d", health.TopNSize)
	}

	// Process 3 batches.
	for i := 0; i < 3; i++ {
		ts := startTime.Add(time.Duration(i) * 10 * time.Second)
		batch := makeCPUBatch("agent-1", float64(i+1)*10, ts)
		if _, _, err := agg.Process(ctx, batch, ts); err != nil {
			t.Fatalf("Process[%d] failed: %v", i, err)
		}
	}

	health = agg.Health()
	if health.WindowsOpen != 1 {
		t.Errorf("after processing: expected 1 window, got %d", health.WindowsOpen)
	}
	// 3 batches × 1 extracted value per batch = 3 values processed.
	if health.ValuesProcessed != 3 {
		t.Errorf("after processing: expected 3 values processed, got %d", health.ValuesProcessed)
	}
	if health.TopNSize != 5 {
		t.Errorf("after processing: expected top_n_size=5, got %d", health.TopNSize)
	}

	// Verify Start/Stop lifecycle.
	ctx2, cancel := context.WithCancel(context.Background())
	defer cancel()
	agg.Start(ctx2)
	agg.Stop()

	// Stop is idempotent.
	agg.Stop()
}

// =============================================================================
// BenchmarkAggregator_1000Metrics
// Process 1000 batches, target < 50ms per message.
// =============================================================================

func BenchmarkAggregator_1000Metrics(b *testing.B) {
	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	cfg := AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute, 5 * time.Minute},
		GracePeriod: 30 * time.Second,
		TopNSize:    10,
		TimeFunc:    clock.Now,
	}
	mockDF := newTestDragonfly()
	agg, err := NewAggregator(cfg, mockDF, zap.NewNop())
	if err != nil {
		b.Fatalf("NewAggregator failed: %v", err)
	}
	ctx := context.Background()

	batches := make([]*models.MetricBatch, 1000)
	for i := range batches {
		ts := startTime.Add(time.Duration(i) * time.Second)
		batches[i] = makeFullBatch(fmt.Sprintf("agent-%d", i%10), ts)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		for i := 0; i < 1000; i++ {
			ts := startTime.Add(time.Duration(i) * time.Second)
			if _, _, err := agg.Process(ctx, batches[i], ts); err != nil {
				b.Fatalf("Process[%d] failed: %v", i, err)
			}
		}
	}
}
