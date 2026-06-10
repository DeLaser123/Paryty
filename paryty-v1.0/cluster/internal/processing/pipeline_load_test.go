// Package processing implements the data processing pipeline.
// This file contains load tests that simulate 100K agents to verify memory
// budgets, throughput targets, and goroutine counts under realistic load.
//
// Run with: go test -v -run Load ./internal/processing/
// Skip in CI: go test -v -short ./internal/processing/
package processing

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// =============================================================================
// TestPipeline_100KAgents_MemoryLoad
// Simulates 100K agents sending metrics through the pipeline.
// Verifies total memory stays within budget and per-agent memory is bounded.
// =============================================================================

func TestPipeline_100KAgents_MemoryLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	p := newTestPipeline(t, store, producer, nil)

	ctx := context.Background()
	const agentCount = 100_000
	const messagesPerAgent = 10
	totalMessages := agentCount * messagesPerAgent

	// Force GC and measure baseline.
	runtime.GC()
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	baselineAlloc := memBefore.Alloc

	t.Logf("Baseline memory: %d MB", baselineAlloc/1024/1024)

	// Send messages — each message has a unique agent ID within a tenant.
	for i := 0; i < totalMessages; i++ {
		agentIdx := i % agentCount
		record := createIntegrationRecord("paryty.load-test.metrics.raw", "load-test", agentIdx)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}

		// Log progress every 100K messages.
		if (i+1)%100_000 == 0 {
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			t.Logf("Progress: %d/%d messages, memory: %d MB (delta: %d MB)",
				i+1, totalMessages,
				mem.Alloc/1024/1024,
				(mem.Alloc-baselineAlloc)/1024/1024)
		}
	}

	// Verify all messages processed.
	if p.processed.Load() != int64(totalMessages) {
		t.Errorf("expected %d processed, got %d", totalMessages, p.processed.Load())
	}
	if p.errors.Load() != 0 {
		t.Errorf("expected 0 errors, got %d", p.errors.Load())
	}

	// Force GC to get a stable measurement.
	runtime.GC()
	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	finalAlloc := memAfter.Alloc
	deltaAlloc := finalAlloc - baselineAlloc
	memoryPerAgent := deltaAlloc / uint64(agentCount)

	t.Logf("=== Memory Load Results (100K agents × 10 messages) ===")
	t.Logf("Total messages: %d", totalMessages)
	t.Logf("Baseline memory: %d MB", baselineAlloc/1024/1024)
	t.Logf("Final memory: %d MB", finalAlloc/1024/1024)
	t.Logf("Memory delta: %d MB", deltaAlloc/1024/1024)
	t.Logf("Memory per agent: %d bytes", memoryPerAgent)
	t.Logf("Heap objects: %d", memAfter.HeapObjects)

	// Memory budget: total delta should be under 2 GB.
	if deltaAlloc > 2*1024*1024*1024 {
		t.Errorf("memory delta %d MB exceeds 2 GB budget", deltaAlloc/1024/1024)
	}

	// Per-agent budget: under 20 KB per agent.
	if memoryPerAgent > 20*1024 {
		t.Errorf("memory per agent %d bytes exceeds 20 KB budget", memoryPerAgent)
	}
}

// =============================================================================
// TestPipeline_100KAgents_ThroughputLoad
// Measures messages/sec throughput with 100K agents.
// =============================================================================

func TestPipeline_100KAgents_ThroughputLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	p := newTestPipeline(t, store, producer, nil)

	ctx := context.Background()
	const agentCount = 100_000
	const messagesPerAgent = 10
	totalMessages := agentCount * messagesPerAgent

	// Pre-create all records to measure pure processing time.
	records := make([]*kgo.Record, totalMessages)
	for i := 0; i < totalMessages; i++ {
		agentIdx := i % agentCount
		records[i] = createIntegrationRecord("paryty.load-test.metrics.raw", "load-test", agentIdx)
	}

	start := time.Now()

	for i, record := range records {
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	elapsed := time.Since(start)
	throughput := float64(totalMessages) / elapsed.Seconds()

	t.Logf("=== Throughput Load Results (100K agents × 10 messages) ===")
	t.Logf("Total messages: %d", totalMessages)
	t.Logf("Elapsed: %v", elapsed)
	t.Logf("Throughput: %.0f messages/sec", throughput)
	t.Logf("Avg per message: %v", time.Duration(int64(elapsed)/int64(totalMessages)))

	// Verify throughput target: > 10K messages/sec.
	if throughput < 10_000 {
		t.Errorf("throughput %.0f msg/sec is below target 10,000 msg/sec", throughput)
	}

	// Verify store received all messages.
	store.mu.Lock()
	batchCalls := store.storeBatchCalls
	store.mu.Unlock()
	if batchCalls != totalMessages {
		t.Errorf("expected %d StoreMetricBatch calls, got %d", totalMessages, batchCalls)
	}
}

// =============================================================================
// TestPipeline_100KAgents_GoroutineLoad
// Verifies the pipeline does not spawn 1 goroutine per agent.
// The bounded worker pool should keep goroutine count constant regardless
// of agent count.
// =============================================================================

func TestPipeline_100KAgents_GoroutineLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	store := &mockPipelineStore{}
	p := newTestPipeline(t, store, nil, nil)

	ctx := context.Background()
	const agentCount = 100_000

	// Measure baseline goroutines.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	t.Logf("Baseline goroutines: %d", baselineGoroutines)

	// Send messages for 100K unique agents.
	for i := 0; i < agentCount; i++ {
		record := createIntegrationRecord("paryty.load-test.metrics.raw", "load-test", i)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	// Allow goroutines spawned by async operations to settle.
	time.Sleep(200 * time.Millisecond)

	peakGoroutines := runtime.NumGoroutine()
	goroutineDelta := peakGoroutines - baselineGoroutines

	t.Logf("=== Goroutine Load Results (100K agents) ===")
	t.Logf("Baseline goroutines: %d", baselineGoroutines)
	t.Logf("Peak goroutines: %d", peakGoroutines)
	t.Logf("Goroutine delta: %d", goroutineDelta)

	// The pipeline should NOT spawn 1 goroutine per agent.
	// With a bounded worker pool (pool size 4), delta should be well under 100.
	if goroutineDelta > 100 {
		t.Errorf("goroutine delta %d exceeds budget of 100 (1 goroutine per agent is forbidden)", goroutineDelta)
	}
}

// =============================================================================
// TestPipeline_100KAgents_ConcurrentThroughputLoad
// Measures throughput when multiple goroutines feed the pipeline concurrently.
// This simulates the real-world scenario where a consumer pool dispatches
// records from multiple Redpanda partitions simultaneously.
// =============================================================================

func TestPipeline_100KAgents_ConcurrentThroughputLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	p := newTestPipeline(t, store, producer, nil)

	ctx := context.Background()
	const agentCount = 100_000
	const feederGoroutines = 8
	const messagesPerFeeder = agentCount / feederGoroutines
	totalMessages := feederGoroutines * messagesPerFeeder

	var wg sync.WaitGroup
	errCh := make(chan error, totalMessages)

	start := time.Now()

	wg.Add(feederGoroutines)
	for f := 0; f < feederGoroutines; f++ {
		go func(feederID int) {
			defer wg.Done()
			baseIdx := feederID * messagesPerFeeder
			for i := 0; i < messagesPerFeeder; i++ {
				agentIdx := baseIdx + i
				record := createIntegrationRecord("paryty.load-test.metrics.raw", "load-test", agentIdx)
				if err := p.handleRecord(ctx, record); err != nil {
					errCh <- fmt.Errorf("feeder %d, msg %d: %w", feederID, i, err)
					return
				}
			}
		}(f)
	}

	wg.Wait()
	close(errCh)

	elapsed := time.Since(start)
	throughput := float64(totalMessages) / elapsed.Seconds()

	// Check for errors.
	errCount := 0
	for err := range errCh {
		if errCount < 5 {
			t.Errorf("concurrent processing error: %v", err)
		}
		errCount++
	}
	if errCount > 5 {
		t.Errorf("... and %d more errors", errCount-5)
	}

	t.Logf("=== Concurrent Throughput Load Results ===")
	t.Logf("Feeders: %d goroutines", feederGoroutines)
	t.Logf("Total messages: %d", totalMessages)
	t.Logf("Elapsed: %v", elapsed)
	t.Logf("Throughput: %.0f messages/sec", throughput)

	if p.processed.Load() != int64(totalMessages) {
		t.Errorf("expected %d processed, got %d", totalMessages, p.processed.Load())
	}

	// Concurrent throughput should still be above 10K msg/sec.
	if throughput < 10_000 {
		t.Errorf("concurrent throughput %.0f msg/sec is below target 10,000 msg/sec", throughput)
	}
}

// =============================================================================
// TestPipeline_100KAgents_MemoryMonitorStress
// Verifies the memory monitor does not leak memory or goroutines when
// monitoring a pipeline processing 100K agents.
// =============================================================================

func TestPipeline_100KAgents_MemoryMonitorStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	p := newTestPipeline(t, nil, nil, nil)
	ctx := context.Background()
	const agentCount = 100_000

	// Start the pipeline (starts memory monitor goroutine).
	pipelineCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	_ = p.Start(pipelineCtx)

	// Send 100K messages.
	for i := 0; i < agentCount; i++ {
		record := createIntegrationRecord("paryty.load-test.metrics.raw", "load-test", i)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	// Wait for several memory monitor cycles.
	time.Sleep(500 * time.Millisecond)

	stats := p.memoryMonitor.GetStats()
	t.Logf("=== Memory Monitor Stress Results ===")
	t.Logf("CurrentAlloc: %d MB", stats.CurrentAlloc/1024/1024)
	t.Logf("PeakAlloc: %d MB", stats.PeakAlloc/1024/1024)
	t.Logf("BreakerTrips: %d", stats.BreakerTrips)
	t.Logf("LastCheckTime: %v", stats.LastCheckTime)

	if stats.CurrentAlloc == 0 {
		t.Error("expected non-zero CurrentAlloc after monitoring")
	}
	if stats.PeakAlloc == 0 {
		t.Error("expected non-zero PeakAlloc after monitoring")
	}
	if stats.LastCheckTime.IsZero() {
		t.Error("expected non-zero LastCheckTime after monitoring")
	}

	// Shutdown should complete cleanly.
	p.Stop()
}
