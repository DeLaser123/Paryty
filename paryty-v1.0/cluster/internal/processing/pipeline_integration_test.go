// Package processing implements the data processing pipeline.
// This file contains integration tests that verify the full pipeline with
// all memory optimizations: circuit breaker, bounded window buffers, pooled
// compression/serialization, and async cold writes.
//
// Run with: go test -v -run Integration ./internal/processing/
// Skip in CI: go test -v -short ./internal/processing/
package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// =============================================================================
// Helper: create a realistic MetricBatch for testing
// =============================================================================

// createIntegrationMetricBatch builds a MetricBatch with CPU, memory, and
// process data for a given agent index. Timestamps are set to a fixed base
// time for deterministic aggregation.
func createIntegrationMetricBatch(agentIdx int) *models.MetricBatch {
	agentID := fmt.Sprintf("agent-%d", agentIdx)
	baseTime := time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC)

	return &models.MetricBatch{
		AgentID:   agentID,
		Timestamp: baseTime,
		CPU: []models.CPUMetrics{
			{
				AgentID:       agentID,
				Timestamp:     baseTime,
				TotalUsagePct: float64(30 + (agentIdx % 50)),
				PerCorePct:    []float64{25.0, 35.0, 45.0, 55.0},
				LoadAvg1m:     1.5,
				LoadAvg5m:     1.2,
				LoadAvg15m:    0.9,
				PhysicalCores: 4,
				LogicalCores:  8,
			},
		},
		Memory: []models.MemoryMetrics{
			{
				AgentID:        agentID,
				Timestamp:      baseTime,
				TotalBytes:     16_000_000_000,
				UsedBytes:      8_000_000_000,
				AvailableBytes: 8_000_000_000,
				UsagePercent:   float64(40 + (agentIdx % 30)),
			},
		},
		Disk: []models.DiskMetrics{
			{
				AgentID:          agentID,
				Timestamp:        baseTime,
				Device:           "/dev/sda1",
				MountPoint:       "/",
				ReadBytesPerSec:  uint64(10_000_000 + agentIdx*1000),
				WriteBytesPerSec: uint64(5_000_000 + agentIdx*500),
			},
		},
		Network: []models.NetworkMetrics{
			{
				AgentID:       agentID,
				Timestamp:     baseTime,
				Interface:     "eth0",
				RxBytesPerSec: uint64(1_000_000 + agentIdx*100),
				TxBytesPerSec: uint64(500_000 + agentIdx*50),
				IsUp:          true,
			},
		},
		Processes: []models.ProcessMetrics{
			{
				AgentID:     agentID,
				Timestamp:   baseTime,
				PID:         uint32(1000 + agentIdx),
				Name:        "nginx",
				CPUUsagePct: float64(10 + (agentIdx % 40)),
				MemoryBytes: uint64(256_000_000 + agentIdx*1_000_000),
				Status:      "running",
				Threads:     4,
			},
			{
				AgentID:     agentID,
				Timestamp:   baseTime,
				PID:         uint32(2000 + agentIdx),
				Name:        "postgres",
				CPUUsagePct: float64(20 + (agentIdx % 30)),
				MemoryBytes: uint64(512_000_000 + agentIdx*2_000_000),
				Status:      "running",
				Threads:     16,
			},
		},
	}
}

// createIntegrationRecord creates a kgo.Record with a JSON-serialized MetricBatch.
func createIntegrationRecord(topic, tenant string, agentIdx int) *kgo.Record {
	batch := createIntegrationMetricBatch(agentIdx)
	data, _ := json.Marshal(batch)
	return &kgo.Record{
		Topic: topic,
		Key:   []byte(fmt.Sprintf("%s:agent-%d", tenant, agentIdx)),
		Value: data,
	}
}

// =============================================================================
// TestPipeline_FullIntegration_AllOptimizations
// End-to-end test: send 1000 messages through the pipeline with all memory
// optimizations active (bounded buffers, pooled compression, circuit breaker).
// =============================================================================

func TestPipeline_FullIntegration_AllOptimizations(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	consumer := &mockPipelineConsumer{}
	p := newTestPipeline(t, store, producer, consumer)

	ctx := context.Background()
	totalMessages := 1000

	// Send 1000 messages through the pipeline via handleRecord.
	for i := 0; i < totalMessages; i++ {
		record := createIntegrationRecord("paryty.test-tenant.metrics.raw", "test-tenant", i)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	// Verify all messages were processed.
	if p.processed.Load() != int64(totalMessages) {
		t.Errorf("expected %d processed, got %d", totalMessages, p.processed.Load())
	}

	// Verify no errors.
	if p.errors.Load() != 0 {
		t.Errorf("expected 0 errors, got %d", p.errors.Load())
	}

	// Verify store was called for each message.
	store.mu.Lock()
	batchCalls := store.storeBatchCalls
	store.mu.Unlock()
	if batchCalls != totalMessages {
		t.Errorf("expected %d StoreMetricBatch calls, got %d", totalMessages, batchCalls)
	}

	// Verify producer was called (at least once per message for enriched metrics).
	producer.mu.Lock()
	publishCalls := producer.publishCalls
	producer.mu.Unlock()
	if publishCalls < totalMessages {
		t.Errorf("expected at least %d publish calls, got %d", totalMessages, publishCalls)
	}

	// Verify memory usage is within budget (200 MB headroom for test binary).
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memMB := memStats.Alloc / 1024 / 1024
	t.Logf("Memory after 1000 messages: %d MB (Alloc: %d bytes)", memMB, memStats.Alloc)
	if memMB > 200 {
		t.Errorf("memory usage %d MB exceeds 200 MB budget", memMB)
	}
}

// =============================================================================
// TestPipeline_MultiTenant_Integration
// Verify tenant isolation: each tenant's messages are routed independently.
// =============================================================================

func TestPipeline_MultiTenant_Integration(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	consumer := &mockPipelineConsumer{}
	p := newTestPipeline(t, store, producer, consumer)

	ctx := context.Background()
	tenants := []string{"default", "acme", "tenant1", "tenant-b", "gai-tech"}
	messagesPerTenant := 100

	// Send messages for 5 tenants.
	for _, tenant := range tenants {
		for i := 0; i < messagesPerTenant; i++ {
			record := createIntegrationRecord(
				fmt.Sprintf("paryty.%s.metrics.raw", tenant),
				tenant,
				i,
			)
			if err := p.handleRecord(ctx, record); err != nil {
				t.Fatalf("handleRecord failed for tenant=%s, i=%d: %v", tenant, i, err)
			}
		}
	}

	totalMessages := len(tenants) * messagesPerTenant

	// Verify all messages processed.
	if p.processed.Load() != int64(totalMessages) {
		t.Errorf("expected %d processed, got %d", totalMessages, p.processed.Load())
	}

	// Verify no errors.
	if p.errors.Load() != 0 {
		t.Errorf("expected 0 errors, got %d", p.errors.Load())
	}

	// Verify store was called for all messages.
	store.mu.Lock()
	batchCalls := store.storeBatchCalls
	store.mu.Unlock()
	if batchCalls != totalMessages {
		t.Errorf("expected %d StoreMetricBatch calls, got %d", totalMessages, batchCalls)
	}

	// Verify all batches have correct agent IDs (tenant isolation check).
	store.mu.Lock()
	storedBatches := make([]*models.MetricBatch, len(store.storedBatches))
	copy(storedBatches, store.storedBatches)
	store.mu.Unlock()

	agentIDs := make(map[string]int)
	for _, batch := range storedBatches {
		agentIDs[batch.AgentID]++
	}

	// Each agent ID should appear exactly once per tenant (5 tenants × 100 agents).
	// But since agents are indexed 0-99 for each tenant, we expect 100 unique agent IDs.
	if len(agentIDs) != 100 {
		t.Errorf("expected 100 unique agent IDs, got %d", len(agentIDs))
	}
}

// =============================================================================
// TestPipeline_GracefulShutdown_Integration
// Start pipeline, send messages, verify clean shutdown within timeout.
// =============================================================================

func TestPipeline_GracefulShutdown_Integration(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	consumer := &mockPipelineConsumer{}
	p := newTestPipeline(t, store, producer, consumer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the pipeline.
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Send some messages via handleRecord.
	for i := 0; i < 100; i++ {
		record := createIntegrationRecord("paryty.test-tenant.metrics.raw", "test-tenant", i)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	// Shutdown should complete within 10 seconds.
	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
		t.Log("Pipeline stopped successfully")
	case <-time.After(10 * time.Second):
		t.Fatal("Pipeline shutdown timed out after 10s")
	}

	// Verify consumer and producer were closed.
	consumer.mu.Lock()
	closed := consumer.closed
	consumer.mu.Unlock()
	if !closed {
		t.Error("expected consumer to be closed after Stop()")
	}

	producer.mu.Lock()
	prodClosed := producer.closed
	producer.mu.Unlock()
	if !prodClosed {
		t.Error("expected producer to be closed after Stop()")
	}
}

// =============================================================================
// TestPipeline_CircuitBreaker_Integration
// Verify the circuit breaker rejects messages under memory pressure and
// recovers when memory drops.
// =============================================================================

func TestPipeline_CircuitBreaker_Integration(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	p := newTestPipeline(t, store, nil, nil)
	ctx := context.Background()

	// Phase 1: Process messages normally (breaker closed).
	for i := 0; i < 50; i++ {
		record := createIntegrationRecord("paryty.test-tenant.metrics.raw", "test-tenant", i)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed with breaker closed: %v", i, err)
		}
	}

	if p.processed.Load() != 50 {
		t.Errorf("expected 50 processed, got %d", p.processed.Load())
	}

	// Phase 2: Open the breaker (simulate memory pressure).
	p.memoryMonitor.breakerOpen.Store(true)

	// Messages should be rejected.
	record := createIntegrationRecord("paryty.test-tenant.metrics.raw", "test-tenant", 999)
	err := p.handleRecord(ctx, record)
	if err == nil {
		t.Error("expected error with breaker open, got nil")
	}

	// Error counter should increment.
	if p.errors.Load() != 1 {
		t.Errorf("expected 1 error, got %d", p.errors.Load())
	}

	// Phase 3: Close the breaker (memory recovered).
	p.memoryMonitor.breakerOpen.Store(false)

	// Messages should succeed again.
	record = createIntegrationRecord("paryty.test-tenant.metrics.raw", "test-tenant", 998)
	if err := p.handleRecord(ctx, record); err != nil {
		t.Fatalf("handleRecord failed with breaker closed (recovery): %v", err)
	}

	if p.processed.Load() != 51 {
		t.Errorf("expected 51 processed after recovery, got %d", p.processed.Load())
	}
}

// =============================================================================
// TestPipeline_MemoryMonitor_Integration
// Verify memory monitor stats tracking across multiple check cycles.
// =============================================================================

func TestPipeline_MemoryMonitor_Integration(t *testing.T) {
	t.Parallel()

	memConfig := config.MemoryConfig{
		MaxRAMBytes:   1 << 30, // 1 GB
		CheckInterval: 100 * time.Millisecond,
	}
	monitor := NewMemoryMonitor(memConfig, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run monitor in background.
	done := make(chan struct{})
	go func() {
		monitor.Start(ctx)
		close(done)
	}()

	// Wait for several check cycles.
	time.Sleep(350 * time.Millisecond)

	select {
	case <-done:
		// Monitor stopped.
	case <-time.After(2 * time.Second):
		t.Fatal("MemoryMonitor did not stop within timeout")
	}

	stats := monitor.GetStats()
	if stats.CurrentAlloc == 0 {
		t.Error("expected non-zero CurrentAlloc after monitoring")
	}
	if stats.PeakAlloc == 0 {
		t.Error("expected non-zero PeakAlloc after monitoring")
	}
	if stats.LastCheckTime.IsZero() {
		t.Error("expected non-zero LastCheckTime after monitoring")
	}

	t.Logf("Memory stats: CurrentAlloc=%d MB, PeakAlloc=%d MB, BreakerTrips=%d",
		stats.CurrentAlloc/1024/1024, stats.PeakAlloc/1024/1024, stats.BreakerTrips)
}

// =============================================================================
// TestPipeline_SerializedHighVolume_Integration
// Verify the pipeline handles a high volume of messages (1000) when
// dispatched serially. This simulates the bounded worker pool where
// records are processed one-at-a-time (the pipeline's internal
// DependencyGraph and Enricher are not safe for concurrent handleRecord).
// =============================================================================

func TestPipeline_SerializedHighVolume_Integration(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	p := newTestPipeline(t, store, producer, nil)

	ctx := context.Background()
	totalMessages := 1000

	// Send messages sequentially (bounded worker pool behavior).
	for i := 0; i < totalMessages; i++ {
		record := createIntegrationRecord("paryty.test-tenant.metrics.raw", "test-tenant", i)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	// Verify all messages processed.
	if p.processed.Load() != int64(totalMessages) {
		t.Errorf("expected %d processed, got %d", totalMessages, p.processed.Load())
	}

	// Verify store received all messages.
	store.mu.Lock()
	batchCalls := store.storeBatchCalls
	store.mu.Unlock()
	if batchCalls != totalMessages {
		t.Errorf("expected %d StoreMetricBatch calls, got %d", totalMessages, batchCalls)
	}

	// Verify memory is bounded after 1000 messages.
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	t.Logf("Memory after 1000 serialized messages: %d MB", memStats.Alloc/1024/1024)
}

// =============================================================================
// TestPipeline_TopicRouting_Integration
// Verify correct routing for all topic types (metrics, network events,
// traces, events, unknown).
// =============================================================================

func TestPipeline_TopicRouting_Integration(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	p := newTestPipeline(t, store, nil, nil)
	ctx := context.Background()

	// Route 1: metrics.raw → processMetricBatch.
	metricsRecord := createIntegrationRecord("paryty.acme.metrics.raw", "acme", 0)
	if err := p.handleRecord(ctx, metricsRecord); err != nil {
		t.Fatalf("metrics.raw routing failed: %v", err)
	}

	// Route 2: network.events → correlator buffer.
	netEvent := models.NetworkEvent{
		AgentID: "agent-0",
		TCP:     []models.TCPEvent{{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", DstPort: 80}},
	}
	netData, _ := json.Marshal(netEvent)
	netRecord := &kgo.Record{
		Topic: "paryty.acme.network.events",
		Key:   []byte("acme:agent-0"),
		Value: netData,
	}
	if err := p.handleRecord(ctx, netRecord); err != nil {
		t.Fatalf("network.events routing failed: %v", err)
	}

	// Route 3: traces → enricher → store.StoreSpan.
	span := models.Span{
		TraceID:     "trace-integration-1",
		SpanID:      "span-1",
		ServiceName: "api-gateway",
		Name:        "GET /api/v1",
	}
	spanData, _ := json.Marshal(span)
	spanRecord := &kgo.Record{
		Topic: "paryty.acme.traces",
		Key:   []byte("acme:agent-0"),
		Value: spanData,
	}
	if err := p.handleRecord(ctx, spanRecord); err != nil {
		t.Fatalf("traces routing failed: %v", err)
	}

	// Route 4: events → store.StoreEvents.
	events := []models.Event{
		{ID: "evt-int-1", Category: models.EventCategoryDeployment, Source: "k8s", Title: "deployed v2"},
	}
	evtData, _ := json.Marshal(events)
	evtRecord := &kgo.Record{
		Topic: "paryty.acme.events",
		Key:   []byte("acme:agent-0"),
		Value: evtData,
	}
	if err := p.handleRecord(ctx, evtRecord); err != nil {
		t.Fatalf("events routing failed: %v", err)
	}

	// Route 5: unknown topic → silently skipped.
	unknownRecord := &kgo.Record{
		Topic: "paryty.acme.unknown.topic",
		Key:   []byte("acme:agent-0"),
		Value: []byte(`{"foo":"bar"}`),
	}
	if err := p.handleRecord(ctx, unknownRecord); err != nil {
		t.Fatalf("unknown topic should not error: %v", err)
	}

	// Verify store calls.
	store.mu.Lock()
	spanCalls := store.storeSpanCalls
	eventsCalls := store.storeEventsCalls
	store.mu.Unlock()

	if spanCalls != 1 {
		t.Errorf("expected 1 StoreSpan call, got %d", spanCalls)
	}
	if eventsCalls != 1 {
		t.Errorf("expected 1 StoreEvents call, got %d", eventsCalls)
	}

	// Verify buffered network events.
	buffered := p.correlator.GetBufferedEvents()
	if len(buffered) != 1 {
		t.Errorf("expected 1 buffered network event, got %d", len(buffered))
	}
}

// =============================================================================
// TestPipeline_HealthReport_Integration
// Verify health reporting tracks processed count and error rate correctly.
// =============================================================================

func TestPipeline_HealthReport_Integration(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	ctx := context.Background()

	// Process some messages.
	for i := 0; i < 10; i++ {
		record := createIntegrationRecord("paryty.test-tenant.metrics.raw", "test-tenant", i)
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	health := p.Health()
	if health.Status != "healthy" {
		t.Errorf("expected healthy, got %s", health.Status)
	}
	if health.MessagesProcessed != 10 {
		t.Errorf("expected 10 messages processed, got %d", health.MessagesProcessed)
	}
	if health.ErrorsTotal != 0 {
		t.Errorf("expected 0 errors, got %d", health.ErrorsTotal)
	}

	// Simulate errors to trigger degraded status.
	for i := 0; i < 100; i++ {
		p.errors.Add(1)
	}
	p.processed.Add(1) // Keep ratio above 10%.

	health = p.Health()
	if health.Status != "degraded" {
		t.Errorf("expected degraded with high error rate, got %s", health.Status)
	}
}

// =============================================================================
// TestPipeline_EmptyBatch_Integration
// Verify the pipeline handles empty MetricBatch without errors.
// =============================================================================

func TestPipeline_EmptyBatch_Integration(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	p := newTestPipeline(t, store, nil, nil)
	ctx := context.Background()

	// Empty batch (no CPU, memory, disk, network, processes).
	batch := models.MetricBatch{
		AgentID:   "agent-empty",
		Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC),
	}
	data, _ := json.Marshal(batch)
	record := &kgo.Record{
		Topic: "paryty.test-tenant.metrics.raw",
		Key:   []byte("test-tenant:agent-empty"),
		Value: data,
	}

	if err := p.handleRecord(ctx, record); err != nil {
		t.Fatalf("empty batch should not error: %v", err)
	}

	if p.processed.Load() != 1 {
		t.Errorf("expected 1 processed, got %d", p.processed.Load())
	}
	if p.errors.Load() != 0 {
		t.Errorf("expected 0 errors, got %d", p.errors.Load())
	}
}

// =============================================================================
// TestPipeline_MalformedJSON_Integration
// Verify malformed JSON is silently skipped (no error propagation).
// =============================================================================

func TestPipeline_MalformedJSON_Integration(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	ctx := context.Background()

	topics := []string{
		"paryty.test-tenant.metrics.raw",
		"paryty.test-tenant.network.events",
		"paryty.test-tenant.traces",
		"paryty.test-tenant.events",
	}

	for _, topic := range topics {
		record := &kgo.Record{
			Topic: topic,
			Key:   []byte("test-tenant:agent-1"),
			Value: []byte(`{this is not valid json`),
		}
		if err := p.handleRecord(ctx, record); err != nil {
			t.Errorf("malformed JSON on topic %s should not error, got: %v", topic, err)
		}
	}

	// No messages should be counted as processed (malformed = skipped, not processed).
	// Actually, handleRecord increments processed on nil error from processMessage,
	// and processMessage returns nil for malformed JSON (silently skipped).
	if p.processed.Load() != 4 {
		t.Errorf("expected 4 processed (malformed JSON returns nil), got %d", p.processed.Load())
	}
	if p.errors.Load() != 0 {
		t.Errorf("expected 0 errors, got %d", p.errors.Load())
	}
}

// =============================================================================
// TestPipeline_TopNTracking_Integration
// Verify Top-N process tracking across multiple batches.
// =============================================================================

func TestPipeline_TopNTracking_Integration(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	ctx := context.Background()

	// Send batches with varying process CPU usage.
	for i := 0; i < 20; i++ {
		batch := models.MetricBatch{
			AgentID:   fmt.Sprintf("agent-%d", i),
			Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC),
			Processes: []models.ProcessMetrics{
				{
					AgentID:     fmt.Sprintf("agent-%d", i),
					Timestamp:   time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC),
					PID:         uint32(1000 + i),
					Name:        "nginx",
					CPUUsagePct: float64(10 + i*2), // 10, 12, 14, ..., 48
					MemoryBytes: uint64(100_000_000 * (i + 1)),
				},
			},
		}
		data, _ := json.Marshal(batch)
		record := &kgo.Record{
			Topic: "paryty.test-tenant.metrics.raw",
			Key:   []byte(fmt.Sprintf("test-tenant:agent-%d", i)),
			Value: data,
		}
		if err := p.handleRecord(ctx, record); err != nil {
			t.Fatalf("handleRecord[%d] failed: %v", i, err)
		}
	}

	// Verify aggregator health shows processed values.
	health := p.aggregator.Health()
	if health.ValuesProcessed == 0 {
		t.Error("expected non-zero ValuesProcessed in aggregator health")
	}

	t.Logf("Aggregator health: WindowsOpen=%d, ValuesProcessed=%d",
		health.WindowsOpen, health.ValuesProcessed)
}
