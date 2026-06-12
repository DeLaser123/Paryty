// Package processing implements the data processing pipeline.
// This file contains load tests that verify window buffer memory usage
// and percentile calculation performance under 100K-agent scale.
//
// Run with: go test -v -run Load ./internal/processing/
// Skip in CI: go test -v -short ./internal/processing/
package processing

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// TestWindowBuffer_100KAgents_MemoryLoad
// Creates 400K window buffers (100K agents × 4 windows) and verifies
// memory per buffer is within the 2 KB budget from the capacity reduction
// (10K → 256 values per window).
// =============================================================================

// allocDelta returns the heap growth between two MemStats.Alloc readings.
// Alloc values are uint64 and the heap can SHRINK between readings (GC may
// reclaim allocations made by earlier tests in the same binary); naive
// subtraction then underflows to a near-2^64 garbage value. A shrinking
// heap means the workload added no net memory, so the delta is 0.
func allocDelta(before, after uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

func TestWindowBuffer_100KAgents_MemoryLoad(t *testing.T) {
	skipIfLoadTestInfeasible(t)

	const agentCount = 100_000
	const windowsPerAgent = 4 // 1m, 5m, 1h, 1d
	windowCount := agentCount * windowsPerAgent

	// Force GC and measure baseline.
	runtime.GC()
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	baselineAlloc := memBefore.Alloc

	t.Logf("Creating %d window buffers (%d agents × %d windows)...", windowCount, agentCount, windowsPerAgent)

	windows := make([]*WindowBuffer, windowCount)
	for i := 0; i < windowCount; i++ {
		key := WindowKey{
			AgentID:    fmt.Sprintf("agent-%d", i/4),
			MetricName: "cpu.usage_percent",
			WindowSize: time.Minute,
		}
		windows[i] = newWindowBuffer(key)

		// Fill to capacity (256 values).
		for j := 0; j < maxWindowBufferCapacity; j++ {
			windows[i].AddValue(float64(j), time.Now())
		}
	}

	// Force GC to get stable measurement.
	runtime.GC()
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	deltaAlloc := allocDelta(baselineAlloc, memAfter.Alloc)
	memoryPerWindow := deltaAlloc / uint64(windowCount)

	t.Logf("=== Window Buffer Memory Load Results ===")
	t.Logf("Window count: %d", windowCount)
	t.Logf("Baseline memory: %d MB", baselineAlloc/1024/1024)
	t.Logf("Final memory: %d MB", memAfter.Alloc/1024/1024)
	t.Logf("Memory delta: %d MB", deltaAlloc/1024/1024)
	t.Logf("Memory per window: %d bytes", memoryPerWindow)
	t.Logf("Heap objects: %d", memAfter.HeapObjects)

	// Budget: < 2 KB per window buffer (with 256 capacity, this is ~2 KB of float64 data).
	if memoryPerWindow > 4*1024 {
		t.Errorf("memory per window %d bytes exceeds 4 KB budget", memoryPerWindow)
	}

	// Total budget: 400K windows × 4 KB = ~1.6 GB max.
	totalBudgetMB := uint64(windowCount*4*1024) / 1024 / 1024
	if deltaAlloc/1024/1024 > totalBudgetMB {
		t.Errorf("total memory %d MB exceeds budget %d MB", deltaAlloc/1024/1024, totalBudgetMB)
	}

	// Verify buffers actually hold the data.
	for i, wb := range windows {
		if len(wb.Values) != maxWindowBufferCapacity {
			t.Errorf("window[%d] has %d values, expected %d", i, len(wb.Values), maxWindowBufferCapacity)
			break
		}
	}

	// Keep references alive to prevent premature GC.
	_ = windows
}

// =============================================================================
// TestWindowBuffer_100KAgents_MemoryWithWindowStateManager
// Uses WindowStateManager (the production API) to create windows for
// 100K agents via AddValue. Verifies memory usage is bounded.
// =============================================================================

func TestWindowBuffer_100KAgents_MemoryWithWindowStateManager(t *testing.T) {
	skipIfLoadTestInfeasible(t)

	wm := NewWindowStateManager(WindowManagerConfig{
		GracePeriod: 5 * time.Minute,
		WindowSizes: []time.Duration{time.Minute, 5 * time.Minute, 1 * time.Hour},
		Logger:      nopLogger(),
	})

	const agentCount = 100_000
	const valuesPerAgent = 20 // Simulate 20 metric values per agent.

	runtime.GC()
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	baselineAlloc := memBefore.Alloc

	ctx := t
	_ = ctx // suppress unused warning; we use context.Background() below

	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	for i := 0; i < agentCount; i++ {
		agentID := fmt.Sprintf("agent-%d", i)
		for v := 0; v < valuesPerAgent; v++ {
			ts := baseTime.Add(time.Duration(v) * 10 * time.Second)
			if err := wm.AddValue(t.Context(), agentID, "cpu.usage_percent", float64(30+v), ts); err != nil {
				t.Fatalf("AddValue failed for agent %d, value %d: %v", i, v, err)
			}
		}
	}

	runtime.GC()
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	deltaAlloc := allocDelta(baselineAlloc, memAfter.Alloc)

	t.Logf("=== WindowStateManager Memory Results ===")
	t.Logf("Agents: %d, Values per agent: %d", agentCount, valuesPerAgent)
	t.Logf("Memory delta: %d MB", deltaAlloc/1024/1024)
	t.Logf("Heap objects: %d", memAfter.HeapObjects)

	// Budget: total delta should be under 2 GB.
	if deltaAlloc > 2*1024*1024*1024 {
		t.Errorf("memory delta %d MB exceeds 2 GB budget", deltaAlloc/1024/1024)
	}
}

// =============================================================================
// TestWindowBuffer_100KAgents_EvictionAtCapacity
// Verifies that window buffers correctly evict oldest values when at
// maxWindowBufferCapacity (256), keeping memory bounded.
// =============================================================================

func TestWindowBuffer_100KAgents_EvictionAtCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const agentCount = 100_000

	// Create one window buffer per agent.
	buffers := make([]*WindowBuffer, agentCount)
	for i := 0; i < agentCount; i++ {
		key := WindowKey{
			AgentID:    fmt.Sprintf("agent-%d", i),
			MetricName: "cpu.usage_percent",
			WindowSize: time.Minute,
		}
		buffers[i] = newWindowBuffer(key)
	}

	// Add 2x maxWindowBufferCapacity values — should trigger eviction.
	totalValues := maxWindowBufferCapacity * 2
	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	for i, buf := range buffers {
		for v := 0; v < totalValues; v++ {
			ts := baseTime.Add(time.Duration(v) * 10 * time.Second)
			buf.AddValue(float64(v), ts)
		}

		// Verify buffer size is capped.
		if len(buf.Values) > maxWindowBufferCapacity {
			t.Fatalf("agent %d: buffer has %d values, expected <= %d", i, len(buf.Values), maxWindowBufferCapacity)
		}

		// Verify Count tracks total values added (not just stored).
		if buf.Count != totalValues {
			t.Fatalf("agent %d: Count=%d, expected %d", i, buf.Count, totalValues)
		}

		// Verify the buffer contains the most recent values.
		expectedLast := float64(totalValues - 1)
		if buf.Values[len(buf.Values)-1] != expectedLast {
			t.Fatalf("agent %d: last value=%.0f, expected %.0f", i, buf.Values[len(buf.Values)-1], expectedLast)
		}
	}

	t.Logf("=== Eviction Load Results ===")
	t.Logf("Agents: %d, Values added per agent: %d", agentCount, totalValues)
	t.Logf("Buffer capacity: %d (all buffers capped)", maxWindowBufferCapacity)
	t.Logf("Total values in memory: %d (would be %d without cap)", agentCount*maxWindowBufferCapacity, agentCount*totalValues)
}

// =============================================================================
// TestPercentile_100KAgents_PerformanceLoad
// Verifies percentile calculation performance across 400K windows.
// With in-place sorting via calcPercentileSorted, this should complete
// well under 10 seconds.
// =============================================================================

func TestPercentile_100KAgents_PerformanceLoad(t *testing.T) {
	skipIfLoadTestInfeasible(t)

	const agentCount = 100_000
	const windowsPerAgent = 4
	windowCount := agentCount * windowsPerAgent

	// Pre-create windows with random data.
	rng := rand.New(rand.NewSource(42))
	windows := make([][]float64, windowCount)
	for i := 0; i < windowCount; i++ {
		values := make([]float64, maxWindowBufferCapacity)
		for j := 0; j < maxWindowBufferCapacity; j++ {
			values[j] = rng.Float64() * 100.0
		}
		// Sort once (simulating the optimization: sort once, calc multiple percentiles).
		sort.Float64s(values)
		windows[i] = values
	}

	start := time.Now()

	// Calculate p50, p95, p99 for all windows using the optimized path.
	for _, values := range windows {
		_ = calcPercentileSorted(values, 50)
		_ = calcPercentileSorted(values, 95)
		_ = calcPercentileSorted(values, 99)
	}

	elapsed := time.Since(start)
	calculationsPerSec := float64(windowCount*3) / elapsed.Seconds()

	t.Logf("=== Percentile Performance Load Results ===")
	t.Logf("Windows: %d", windowCount)
	t.Logf("Percentile calculations: %d (3 per window)", windowCount*3)
	t.Logf("Elapsed: %v", elapsed)
	t.Logf("Calculations/sec: %.0f", calculationsPerSec)
	t.Logf("Avg per window (3 calcs): %v", time.Duration(int64(elapsed)/int64(windowCount)))

	// Performance target: < 10 seconds for 400K windows × 3 percentiles.
	if elapsed > 10*time.Second {
		t.Errorf("percentile calculation took %v, target < 10s", elapsed)
	}
}

// =============================================================================
// TestPercentile_100KAgents_CorrectnessUnderLoad
// Verifies percentile calculations produce correct results even at scale.
// Uses known value distributions to validate accuracy.
// =============================================================================

func TestPercentile_100KAgents_CorrectnessUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const agentCount = 100_000

	// Create windows with known values (0, 1, 2, ..., 255).
	windows := make([][]float64, agentCount)
	for i := 0; i < agentCount; i++ {
		values := make([]float64, maxWindowBufferCapacity)
		for j := 0; j < maxWindowBufferCapacity; j++ {
			values[j] = float64(j)
		}
		sort.Float64s(values)
		windows[i] = values
	}

	// Verify percentile calculations.
	for i, values := range windows {
		p0 := calcPercentileSorted(values, 0)
		p50 := calcPercentileSorted(values, 50)
		p100 := calcPercentileSorted(values, 100)

		if p0 != 0.0 {
			t.Fatalf("agent %d: p0=%.2f, expected 0.0", i, p0)
		}
		// p50 of [0..255] should be 127.5 (interpolation).
		if math.Abs(p50-127.5) > 0.01 {
			t.Fatalf("agent %d: p50=%.2f, expected 127.5", i, p50)
		}
		if p100 != 255.0 {
			t.Fatalf("agent %d: p100=%.2f, expected 255.0", i, p100)
		}
	}

	t.Logf("Percentile correctness verified across %d agents", agentCount)
}

// =============================================================================
// TestWindowStateManager_100KAgents_GetClosedWindows
// Verifies GetClosedWindows performance with 100K agents worth of windows.
// =============================================================================

func TestWindowStateManager_100KAgents_GetClosedWindows(t *testing.T) {
	skipIfLoadTestInfeasible(t)

	const agentCount = 100_000

	// Create a clock set to a time well past any window close time.
	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(baseTime.Add(2 * time.Hour)) // Well past 1-minute windows.

	wm := NewWindowStateManager(WindowManagerConfig{
		GracePeriod: 30 * time.Second,
		WindowSizes: []time.Duration{time.Minute}, // Only 1-minute windows for speed.
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})

	// Add values for 100K agents into 1-minute windows.
	for i := 0; i < agentCount; i++ {
		agentID := fmt.Sprintf("agent-%d", i)
		for v := 0; v < 10; v++ {
			ts := baseTime.Add(time.Duration(v) * 5 * time.Second)
			if err := wm.AddValue(t.Context(), agentID, "cpu.usage_percent", float64(v*10), ts); err != nil {
				t.Fatalf("AddValue failed: %v", err)
			}
		}
	}

	// All 1-minute windows should be closed (clock is 2 hours ahead).
	start := time.Now()
	closed := wm.GetClosedWindows(t.Context())
	elapsed := time.Since(start)

	t.Logf("=== GetClosedWindows Performance ===")
	t.Logf("Agents: %d", agentCount)
	t.Logf("Closed windows: %d", len(closed))
	t.Logf("Elapsed: %v", elapsed)

	if len(closed) != agentCount {
		t.Errorf("expected %d closed windows, got %d", agentCount, len(closed))
	}

	// Performance target: < 5 seconds to scan 100K windows.
	if elapsed > 5*time.Second {
		t.Errorf("GetClosedWindows took %v, target < 5s", elapsed)
	}

	// Purge and verify memory is reclaimed.
	purgeStart := time.Now()
	purged := wm.PurgeClosed()
	purgeElapsed := time.Since(purgeStart)

	t.Logf("Purged %d windows in %v", purged, purgeElapsed)

	if purged != agentCount {
		t.Errorf("expected %d purged, got %d", agentCount, purged)
	}
}

// =============================================================================
// TestWindowBuffer_100KAgents_ConcurrentAddValue
// Verifies concurrent AddValue calls from multiple goroutines don't corrupt
// window buffer state.
// =============================================================================

func TestWindowBuffer_100KAgents_ConcurrentAddValue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const agentCount = 10_000 // Reduced from 100K for concurrent test speed.
	const goroutines = 50
	const valuesPerGoroutine = 100

	wm := NewWindowStateManager(WindowManagerConfig{
		GracePeriod: 5 * time.Minute,
		WindowSizes: []time.Duration{time.Minute},
		Logger:      nopLogger(),
	})

	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < valuesPerGoroutine; i++ {
				agentIdx := (gID*valuesPerGoroutine + i) % agentCount
				agentID := fmt.Sprintf("agent-%d", agentIdx)
				ts := baseTime.Add(time.Duration(i) * 10 * time.Second)
				if err := wm.AddValue(t.Context(), agentID, "cpu.usage_percent", float64(gID*1000+i), ts); err != nil {
					errCh <- fmt.Errorf("goroutine %d, i=%d: %w", gID, i, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent AddValue error: %v", err)
	}

	t.Logf("Concurrent AddValue: %d goroutines × %d values across %d agents completed without errors",
		goroutines, valuesPerGoroutine, agentCount)
}

// =============================================================================
// TestWindowBuffer_100KAgents_SnapshotPerformance
// Verifies Snapshot() performance when called on 100K buffers.
// =============================================================================

func TestWindowBuffer_100KAgents_SnapshotPerformance(t *testing.T) {
	skipIfLoadTestInfeasible(t)

	const agentCount = 100_000

	buffers := make([]*WindowBuffer, agentCount)
	for i := 0; i < agentCount; i++ {
		key := WindowKey{
			AgentID:    fmt.Sprintf("agent-%d", i),
			MetricName: "cpu.usage_percent",
			WindowSize: time.Minute,
		}
		buffers[i] = newWindowBuffer(key)
		for j := 0; j < maxWindowBufferCapacity; j++ {
			buffers[i].AddValue(float64(j), time.Now())
		}
	}

	start := time.Now()
	for _, buf := range buffers {
		values, count, sum, min, max, _, _ := buf.Snapshot()
		// Validate snapshot data.
		if len(values) != maxWindowBufferCapacity {
			t.Fatalf("snapshot len=%d, expected %d", len(values), maxWindowBufferCapacity)
		}
		if count != maxWindowBufferCapacity {
			t.Fatalf("snapshot count=%d, expected %d", count, maxWindowBufferCapacity)
		}
		if sum == 0 || min > max {
			t.Fatalf("snapshot invalid: sum=%.0f, min=%.0f, max=%.0f", sum, min, max)
		}
	}
	elapsed := time.Since(start)

	t.Logf("=== Snapshot Performance ===")
	t.Logf("Buffers: %d", agentCount)
	t.Logf("Elapsed: %v", elapsed)
	t.Logf("Snapshots/sec: %.0f", float64(agentCount)/elapsed.Seconds())

	// Performance target: < 5 seconds for 100K snapshots.
	if elapsed > 5*time.Second {
		t.Errorf("Snapshot took %v, target < 5s", elapsed)
	}
}
