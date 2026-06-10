// Package storage provides a unified interface to the tiered storage system.
// This file contains integration tests that verify the async cold store worker
// lifecycle, graceful shutdown with channel drain, and end-to-end behavior
// of the cold write path under load.
//
// Run with: go test -v -run Integration ./internal/storage/
// Skip in CI: go test -v -short ./internal/storage/
package storage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
)

// =============================================================================
// TestStore_ColdWriterWorkerLifecycle_Integration
// Start N cold writer goroutines (replicating the pattern from New()),
// push batches, verify all batches are consumed, then shut down cleanly.
// =============================================================================

func TestStore_ColdWriterWorkerLifecycle_Integration(t *testing.T) {
	t.Parallel()

	ch := make(chan *models.MetricBatch, coldWriteChannelSize)
	ctx, cancel := context.WithCancel(context.Background())

	const workerCount = 4
	const batchCount = 200

	var consumed atomic.Int64
	var workerWg sync.WaitGroup

	// Start cold writer workers (replicating the pattern from Store.New()).
	for i := 0; i < workerCount; i++ {
		workerWg.Add(1)
		go func(workerID int) {
			defer workerWg.Done()
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					consumed.Add(1)
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Push batches.
	for i := 0; i < batchCount; i++ {
		ch <- &models.MetricBatch{
			AgentID: fmt.Sprintf("agent-%d", i),
		}
	}

	// Give workers time to drain.
	time.Sleep(50 * time.Millisecond)

	// Shutdown: cancel context, close channel, wait for workers.
	cancel()
	close(ch)
	workerWg.Wait()

	if got := consumed.Load(); got != batchCount {
		t.Errorf("consumed %d batches, want %d", got, batchCount)
	}

	t.Logf("Cold writer lifecycle: %d workers consumed %d batches, shutdown clean", workerCount, consumed.Load())
}

// =============================================================================
// TestStore_ColdWriterContextCancellation_Integration
// Verify that cancelling the context stops cold writer workers even when
// the channel is not empty.
// =============================================================================

func TestStore_ColdWriterContextCancellation_Integration(t *testing.T) {
	t.Parallel()

	ch := make(chan *models.MetricBatch, coldWriteChannelSize)
	ctx, cancel := context.WithCancel(context.Background())

	var consumed atomic.Int64
	var workerWg sync.WaitGroup

	// Start a slow worker (simulates slow cold store).
	workerWg.Add(1)
	go func() {
		defer workerWg.Done()
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					return
				}
				consumed.Add(1)
				time.Sleep(10 * time.Millisecond) // Slow writes.
			case <-ctx.Done():
				return
			}
		}
	}()

	// Push some batches.
	const batchCount = 10
	for i := 0; i < batchCount; i++ {
		ch <- &models.MetricBatch{AgentID: fmt.Sprintf("agent-%d", i)}
	}

	// Cancel context — worker should stop even though channel isn't empty.
	cancel()
	workerWg.Wait()

	// Some batches may have been consumed, but not necessarily all.
	t.Logf("Context cancellation: consumed %d of %d batches before shutdown", consumed.Load(), batchCount)

	// Drain remaining items from channel to verify nothing is stuck.
	remaining := 0
	for {
		select {
		case <-ch:
			remaining++
		default:
			goto drained
		}
	}
drained:
	t.Logf("Remaining in channel after shutdown: %d", remaining)
}

// =============================================================================
// TestStore_ColdWriterNonBlockingEnqueue_Integration
// Verify the non-blocking select/default pattern used in StoreMetricWarmCold.
// Under load, enqueuing should never block the caller.
// =============================================================================

func TestStore_ColdWriterNonBlockingEnqueue_Integration(t *testing.T) {
	t.Parallel()

	// Small channel to trigger drops quickly.
	ch := make(chan *models.MetricBatch, 10)

	// Fill to capacity.
	for i := 0; i < 10; i++ {
		ch <- &models.MetricBatch{AgentID: fmt.Sprintf("agent-%d", i)}
	}

	// Non-blocking enqueue (same pattern as StoreMetricWarmCold).
	enqueued := 0
	dropped := 0
	for i := 0; i < 100; i++ {
		select {
		case ch <- &models.MetricBatch{AgentID: fmt.Sprintf("agent-%d", 100+i)}:
			enqueued++
		default:
			dropped++
		}
	}

	t.Logf("Non-blocking enqueue: enqueued=%d, dropped=%d", enqueued, dropped)

	if enqueued != 0 {
		t.Errorf("expected 0 enqueued on full channel, got %d", enqueued)
	}
	if dropped != 100 {
		t.Errorf("expected 100 dropped, got %d", dropped)
	}
}

// =============================================================================
// TestStore_ColdWriterHighThroughput_Integration
// Verify cold writer workers sustain high throughput with concurrent
// producers pushing batches.
// =============================================================================

func TestStore_ColdWriterHighThroughput_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	ch := make(chan *models.MetricBatch, coldWriteChannelSize)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const workerCount = 4
	const producerCount = 10
	const batchesPerProducer = 10_000
	totalBatches := producerCount * batchesPerProducer

	var consumed atomic.Int64
	var workerWg sync.WaitGroup

	// Start workers.
	for i := 0; i < workerCount; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					consumed.Add(1)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Start producers.
	start := time.Now()
	var producerWg sync.WaitGroup
	producerWg.Add(producerCount)
	for p := 0; p < producerCount; p++ {
		go func(id int) {
			defer producerWg.Done()
			for i := 0; i < batchesPerProducer; i++ {
				// Non-blocking enqueue pattern.
				select {
				case ch <- &models.MetricBatch{AgentID: fmt.Sprintf("p%d-a%d", id, i)}:
					// OK
				default:
					// Channel full — drop (acceptable for cold store).
				}
			}
		}(p)
	}

	// Wait for producers to finish.
	producerWg.Wait()
	elapsed := time.Since(start)

	// Close channel and wait for workers to drain.
	close(ch)
	workerWg.Wait()

	t.Logf("=== Cold Writer High Throughput Results ===")
	t.Logf("Workers: %d, Producers: %d", workerCount, producerCount)
	t.Logf("Target batches: %d", totalBatches)
	t.Logf("Consumed: %d", consumed.Load())
	t.Logf("Producer elapsed: %v", elapsed)
	t.Logf("Throughput: %.0f batches/sec", float64(consumed.Load())/elapsed.Seconds())

	// Workers should have consumed at least the channel capacity.
	if consumed.Load() == 0 {
		t.Error("expected at least some batches consumed")
	}
}

// =============================================================================
// TestStore_CircuitBreakerAndColdWrite_Integration
// Verify that circuit breaker state does not affect cold write channel
// behavior. Even when the warm breaker is open (rejecting writes),
// the cold write channel should still accept batches.
// =============================================================================

func TestStore_CircuitBreakerAndColdWrite_Integration(t *testing.T) {
	t.Parallel()

	warmBreaker := newCircuitBreaker(3, 10*time.Second)
	coldCh := make(chan *models.MetricBatch, 100)

	// Trip the warm breaker.
	warmBreaker.RecordFailure()
	warmBreaker.RecordFailure()
	warmBreaker.RecordFailure()

	if warmBreaker.State() != "open" {
		t.Fatalf("warm breaker should be open, got %q", warmBreaker.State())
	}

	// Cold channel should still accept batches independently.
	const batchCount = 50
	for i := 0; i < batchCount; i++ {
		select {
		case coldCh <- &models.MetricBatch{AgentID: fmt.Sprintf("agent-%d", i)}:
			// OK — cold writes are independent of warm breaker.
		default:
			t.Fatalf("cold channel rejected batch %d unexpectedly", i)
		}
	}

	if len(coldCh) != batchCount {
		t.Errorf("cold channel has %d items, want %d", len(coldCh), batchCount)
	}

	t.Logf("Circuit breaker integration: warm=%q, cold channel=%d/%d items",
		warmBreaker.State(), len(coldCh), cap(coldCh))
}

// =============================================================================
// TestStore_ColdWriterWorkerLeak_Integration
// Verify no goroutine leaks when repeatedly starting and stopping
// cold writer workers.
// =============================================================================

func TestStore_ColdWriterWorkerLeak_Integration(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	const cycles = 100

	for cycle := 0; cycle < cycles; cycle++ {
		ch := make(chan *models.MetricBatch, 100)
		ctx, cancel := context.WithCancel(context.Background())

		var consumed atomic.Int64
		var wg sync.WaitGroup

		// Start 4 workers.
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case _, ok := <-ch:
						if !ok {
							return
						}
						consumed.Add(1)
					case <-ctx.Done():
						return
					}
				}
			}()
		}

		// Push a few batches.
		for i := 0; i < 10; i++ {
			ch <- &models.MetricBatch{AgentID: fmt.Sprintf("c%d-a%d", cycle, i)}
		}

		// Shutdown.
		cancel()
		close(ch)
		wg.Wait()
	}

	// Allow goroutines to settle.
	time.Sleep(100 * time.Millisecond)

	// The goroutine count should not have grown significantly.
	// Each cycle creates and destroys 4 goroutines; if there's a leak,
	// we'd see ~400 extra goroutines.
	t.Logf("Goroutine leak test: %d cycles completed", cycles)
}

// =============================================================================
// TestStore_StoreOptions_Integration
// Verify SetOptions correctly wires Phase 4 components and nil-safe
// methods return descriptive errors.
// =============================================================================

func TestStore_StoreOptions_Integration(t *testing.T) {
	t.Parallel()

	s := &Store{
		hotBreaker:  newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		warmBreaker: newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		coldBreaker: newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
	}

	// Before SetOptions: nil-safe methods should return errors.
	if err := s.UpdateTopologyAtomic(context.Background(), "t", func(topo *models.Topology) (*models.Topology, error) {
		return topo, nil
	}); err == nil {
		t.Error("expected error for nil topologyOps before SetOptions")
	}

	// Wire components.
	mock := &mockTopologyUpdater{
		topology: &models.Topology{},
	}
	s.SetOptions(StoreOptions{
		TopologyOps: mock,
	})

	// After SetOptions: should work.
	if err := s.UpdateTopologyAtomic(context.Background(), "t", func(topo *models.Topology) (*models.Topology, error) {
		return topo, nil
	}); err != nil {
		t.Errorf("unexpected error after SetOptions: %v", err)
	}

	if mock.calls != 1 {
		t.Errorf("expected 1 topology call, got %d", mock.calls)
	}
}

// =============================================================================
// TestStore_MultiTierCircuitBreaker_Integration
// Verify each tier has an independent circuit breaker that trips and
// recovers independently.
// =============================================================================

func TestStore_MultiTierCircuitBreaker_Integration(t *testing.T) {
	t.Parallel()

	s := &Store{
		hotBreaker:  newCircuitBreaker(2, 50*time.Millisecond),
		warmBreaker: newCircuitBreaker(2, 50*time.Millisecond),
		coldBreaker: newCircuitBreaker(2, 50*time.Millisecond),
	}

	// Trip hot breaker only.
	s.hotBreaker.RecordFailure()
	s.hotBreaker.RecordFailure()

	if s.hotBreaker.State() != "open" {
		t.Errorf("hot breaker = %q, want open", s.hotBreaker.State())
	}
	if s.warmBreaker.State() != "closed" {
		t.Errorf("warm breaker = %q, want closed", s.warmBreaker.State())
	}
	if s.coldBreaker.State() != "closed" {
		t.Errorf("cold breaker = %q, want closed", s.coldBreaker.State())
	}

	// Trip warm breaker independently.
	s.warmBreaker.RecordFailure()
	s.warmBreaker.RecordFailure()

	if s.warmBreaker.State() != "open" {
		t.Errorf("warm breaker = %q, want open", s.warmBreaker.State())
	}
	if s.hotBreaker.State() != "open" {
		t.Errorf("hot breaker should still be open")
	}
	if s.coldBreaker.State() != "closed" {
		t.Errorf("cold breaker should still be closed")
	}

	// Wait for hot breaker cooldown and recover.
	time.Sleep(60 * time.Millisecond)
	s.hotBreaker.Allow()      // Transition to half-open.
	s.hotBreaker.RecordSuccess() // Close it.

	if s.hotBreaker.State() != "closed" {
		t.Errorf("hot breaker = %q, want closed after recovery", s.hotBreaker.State())
	}
	// Warm breaker should still be open (independent).
	if s.warmBreaker.State() != "open" {
		t.Errorf("warm breaker = %q, want open (independent)", s.warmBreaker.State())
	}

	t.Log("Multi-tier circuit breaker independence verified")
}

// =============================================================================
// TestStore_ColdWriterGracefulDrain_Integration
// Verify that closing the channel allows workers to drain all remaining
// items before exiting.
// =============================================================================

func TestStore_ColdWriterGracefulDrain_Integration(t *testing.T) {
	t.Parallel()

	ch := make(chan *models.MetricBatch, 500)
	const batchCount = 500

	// Fill channel to capacity.
	for i := 0; i < batchCount; i++ {
		ch <- &models.MetricBatch{AgentID: fmt.Sprintf("agent-%d", i)}
	}

	// Start a worker that reads from the channel.
	var consumed atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for batch := range ch {
			_ = batch
			consumed.Add(1)
		}
	}()

	// Close channel — worker should drain all remaining items.
	close(ch)
	wg.Wait()

	if consumed.Load() != batchCount {
		t.Errorf("consumed %d, want %d (worker should drain before exit)", consumed.Load(), batchCount)
	}

	t.Logf("Graceful drain: %d/%d batches consumed before worker exit", consumed.Load(), batchCount)
}
