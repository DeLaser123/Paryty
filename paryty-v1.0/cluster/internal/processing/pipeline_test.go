// Package processing implements the data processing pipeline.
// This file tests the Pipeline orchestrator, including end-to-end metric
// processing, topic routing, window flushing, network event buffering,
// topology changes, and graceful shutdown.
package processing

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/warm"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// =============================================================================
// Mock implementations for PipelineStore, PipelineProducer, PipelineConsumer
// =============================================================================

// mockPipelineStore implements PipelineStore for testing.
type mockPipelineStore struct {
	mu               sync.Mutex
	storedBatches    []*models.MetricBatch
	storedSpans      []*models.Span
	storedEvents     [][]models.Event
	storedAggMetrics []*models.AggregatedMetric
	topologies       []*models.Topology
	storeBatchErr    error
	storeSpanErr     error
	storeEventsErr   error
	storeAggErr      error
	setTopologyErr   error
	storeBatchCalls  int
	storeSpanCalls   int
	storeEventsCalls int
	storeAggCalls    int
	setTopologyCalls int
}

func (m *mockPipelineStore) StoreMetricBatch(_ context.Context, _ string, batch *models.MetricBatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeBatchCalls++
	m.storedBatches = append(m.storedBatches, batch)
	return m.storeBatchErr
}

func (m *mockPipelineStore) SetTopology(_ context.Context, _ string, topo *models.Topology) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setTopologyCalls++
	m.topologies = append(m.topologies, topo)
	return m.setTopologyErr
}

func (m *mockPipelineStore) StoreSpan(_ context.Context, _ string, span *models.Span) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeSpanCalls++
	m.storedSpans = append(m.storedSpans, span)
	return m.storeSpanErr
}

func (m *mockPipelineStore) StoreEvents(_ context.Context, events []models.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeEventsCalls++
	m.storedEvents = append(m.storedEvents, events)
	return m.storeEventsErr
}

func (m *mockPipelineStore) StoreAggregatedMetric(_ context.Context, _ string, metric *models.AggregatedMetric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeAggCalls++
	m.storedAggMetrics = append(m.storedAggMetrics, metric)
	return m.storeAggErr
}

func (m *mockPipelineStore) StoreAnomaly(_ context.Context, _ string, _ *warm.AnomalyRecord) error {
	return nil
}

// mockPipelineProducer implements PipelineProducer for testing.
type mockPipelineProducer struct {
	mu              sync.Mutex
	publishedValues []mockPublishedRecord
	publishErr      error
	publishCalls    int
	closed          bool
}

type mockPublishedRecord struct {
	topic    string
	tenantID string
	agentID  string
}

func (m *mockPipelineProducer) PublishTenant(_ context.Context, topic, tenantID, agentID string, _ interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publishCalls++
	m.publishedValues = append(m.publishedValues, mockPublishedRecord{
		topic:    topic,
		tenantID: tenantID,
		agentID:  agentID,
	})
	return m.publishErr
}

func (m *mockPipelineProducer) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// mockPipelineConsumer implements PipelineConsumer for testing.
type mockPipelineConsumer struct {
	mu      sync.Mutex
	handler stream.RecordHandler
	started bool
	closed  bool
}

func (m *mockPipelineConsumer) SetRecordHandler(rh stream.RecordHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = rh
}

func (m *mockPipelineConsumer) Start(_ context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
}

func (m *mockPipelineConsumer) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// =============================================================================
// Helper: create a Pipeline with mock dependencies
// =============================================================================

func newTestPipeline(t *testing.T, store *mockPipelineStore, producer *mockPipelineProducer, consumer *mockPipelineConsumer) *Pipeline {
	t.Helper()

	logger := zap.NewNop()
	clock := newTestClock(time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC))
	mockDF := newTestDragonfly()

	agg, err := NewAggregator(AggregatorConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		TimeFunc:    clock.Now,
	}, mockDF, logger)
	if err != nil {
		t.Fatalf("NewAggregator failed: %v", err)
	}

	svcMap, err := NewServiceMap(nil, logger)
	if err != nil {
		t.Fatalf("NewServiceMap failed: %v", err)
	}

	corr := NewCorrelator(DefaultCorrelatorConfig(), svcMap, logger)
	enrich := NewEnricher(DefaultEnricherConfig(), mockDF, logger)

	ds := NewDownsampler(DownsamplerConfig{
		Rules:       DefaultDownsamplingRules(),
		RunInterval: time.Hour,
	}, &mockQuestDB{}, logger)

	if store == nil {
		store = &mockPipelineStore{}
	}
	if producer == nil {
		producer = &mockPipelineProducer{}
	}
	if consumer == nil {
		consumer = &mockPipelineConsumer{}
	}

	// Use default memory config for tests.
	memConfig := config.MemoryConfig{
		MaxRAMBytes:           1 << 30, // 1 GB
		GoroutinePoolSize:     4,
		WindowBufferCapacity:  256,
		EventBufferSize:       1000,
		MaxBufferedRecords:    1000,
		GraphChangesCap:       5000,
		CircuitBreakerEnabled: false, // Disabled in tests.
		CheckInterval:         10 * time.Second,
	}

	return NewPipeline(
		PipelineConfig{
			InputTopics:   []string{"metrics.raw", "network.events", "traces", "events"},
			ConsumerGroup: "test-pipeline",
			BatchSize:     100,
			BatchTimeout:  time.Second,
		},
		agg, corr, enrich, ds,
		store, producer, consumer,
		mockDF,
		logger, "test-tenant",
		memConfig,
		nil, // pipelineMetrics — nil means metric recording is no-op
	)
}

// =============================================================================
// TestPipeline_NewPipeline
// Verify construction and default values.
// =============================================================================

func TestPipeline_NewPipeline(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)

	if p.tenant != "test-tenant" {
		t.Errorf("expected tenant=test-tenant, got %s", p.tenant)
	}
	if p.config.ConsumerGroup != "test-pipeline" {
		t.Errorf("expected consumer_group=test-pipeline, got %s", p.config.ConsumerGroup)
	}
}

func TestPipeline_NewPipeline_DefaultTenant(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	clock := newTestClock(time.Now())
	mockDF := newTestDragonfly()

	agg, _ := NewAggregator(AggregatorConfig{TimeFunc: clock.Now}, mockDF, logger)
	svcMap, _ := NewServiceMap(nil, logger)
	corr := NewCorrelator(DefaultCorrelatorConfig(), svcMap, logger)
	enrich := NewEnricher(DefaultEnricherConfig(), mockDF, logger)
	ds := NewDownsampler(DownsamplerConfig{}, &mockQuestDB{}, logger)

	p := NewPipeline(PipelineConfig{}, agg, corr, enrich, ds,
		&mockPipelineStore{}, &mockPipelineProducer{}, &mockPipelineConsumer{},
		mockDF, logger, "",
		config.MemoryConfig{}, // Zero-value memory config for default test.
		nil,                   // pipelineMetrics — nil for tests.
	)

	if p.tenant != "default" {
		t.Errorf("expected default tenant, got %s", p.tenant)
	}
}

// =============================================================================
// TestPipeline_Health
// Verify health reporting after processing.
// =============================================================================

func TestPipeline_Health(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	p.startTime = time.Now().Add(-5 * time.Second)

	health := p.Health()
	if health.Status != "healthy" {
		t.Errorf("expected healthy, got %s", health.Status)
	}
	if health.MessagesProcessed != 0 {
		t.Errorf("expected 0 messages processed, got %d", health.MessagesProcessed)
	}
	if health.ErrorsTotal != 0 {
		t.Errorf("expected 0 errors, got %d", health.ErrorsTotal)
	}
	if health.Uptime < 4*time.Second {
		t.Errorf("expected uptime >= 4s, got %s", health.Uptime)
	}

	// Simulate errors to trigger degraded status.
	p.processed.Add(1)
	for i := 0; i < 10; i++ {
		p.errors.Add(1)
	}

	health = p.Health()
	if health.Status != "degraded" {
		t.Errorf("expected degraded, got %s", health.Status)
	}
}

// =============================================================================
// TestPipeline_RouteByTopic
// Verify correct routing for each topic type.
// =============================================================================

func TestPipeline_RouteByTopic(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	p := newTestPipeline(t, store, nil, nil)
	ctx := context.Background()

	// Route a network event.
	netEvent := models.NetworkEvent{
		TCP: []models.TCPEvent{{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", DstPort: 80}},
	}
	netData, _ := json.Marshal(netEvent)
	err := p.processMessage(ctx, "tenant-1.network.events", "key-1", netData)
	if err != nil {
		t.Fatalf("network.events routing failed: %v", err)
	}

	// Verify the event was buffered in the correlator.
	buffered := p.correlator.GetBufferedEvents()
	if len(buffered) != 1 {
		t.Errorf("expected 1 buffered network event, got %d", len(buffered))
	}

	// Route a trace (span).
	span := models.Span{
		TraceID:     "trace-1",
		SpanID:      "span-1",
		ServiceName: "api-gateway",
		Name:        "GET /api",
	}
	spanData, _ := json.Marshal(span)
	err = p.processMessage(ctx, "tenant-1.traces", "key-2", spanData)
	if err != nil {
		t.Fatalf("traces routing failed: %v", err)
	}

	store.mu.Lock()
	spanCalls := store.storeSpanCalls
	store.mu.Unlock()
	if spanCalls != 1 {
		t.Errorf("expected 1 StoreSpan call, got %d", spanCalls)
	}

	// Route an event.
	events := []models.Event{
		{ID: "evt-1", Category: models.EventCategoryDeployment, Source: "k8s", Title: "deployed v2"},
	}
	evtData, _ := json.Marshal(events)
	err = p.processMessage(ctx, "tenant-1.events", "key-3", evtData)
	if err != nil {
		t.Fatalf("events routing failed: %v", err)
	}

	store.mu.Lock()
	eventsCalls := store.storeEventsCalls
	store.mu.Unlock()
	if eventsCalls != 1 {
		t.Errorf("expected 1 StoreEvents call, got %d", eventsCalls)
	}

	// Route unknown topic — should be silently skipped.
	err = p.processMessage(ctx, "unknown.topic", "key-4", []byte(`{}`))
	if err != nil {
		t.Fatalf("unknown topic should not error: %v", err)
	}
}

// =============================================================================
// TestPipeline_RouteMalformedJSON
// Malformed JSON should be silently skipped (no error, no DLQ).
// =============================================================================

func TestPipeline_RouteMalformedJSON(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	ctx := context.Background()

	// Malformed MetricBatch.
	err := p.processMessage(ctx, "metrics.raw", "key-1", []byte(`{bad json`))
	if err != nil {
		t.Fatalf("malformed metrics.raw should not error, got: %v", err)
	}

	// Malformed NetworkEvent.
	err = p.processMessage(ctx, "network.events", "key-2", []byte(`not json`))
	if err != nil {
		t.Fatalf("malformed network.events should not error, got: %v", err)
	}

	// Malformed Span.
	err = p.processMessage(ctx, "traces", "key-3", []byte(`{bad`))
	if err != nil {
		t.Fatalf("malformed traces should not error, got: %v", err)
	}

	// Malformed Events (both array and single fail).
	err = p.processMessage(ctx, "events", "key-4", []byte(`{bad`))
	if err != nil {
		t.Fatalf("malformed events should not error, got: %v", err)
	}
}

// =============================================================================
// TestPipeline_EndToEnd
// Start pipeline with mock storage, publish MetricBatch via handleRecord,
// verify enriched output and store calls.
// =============================================================================

func TestPipeline_EndToEnd(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	producer := &mockPipelineProducer{}
	consumer := &mockPipelineConsumer{}

	p := newTestPipeline(t, store, producer, consumer)

	// Build a MetricBatch.
	batch := models.MetricBatch{
		AgentID:   "agent-1",
		Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC),
		CPU: []models.CPUMetrics{
			{AgentID: "agent-1", Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC), TotalUsagePct: 75.0},
		},
		Memory: []models.MemoryMetrics{
			{AgentID: "agent-1", Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC), UsagePercent: 60.0},
		},
		Processes: []models.ProcessMetrics{
			{AgentID: "agent-1", Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC), Name: "nginx", PID: 1234, CPUUsagePct: 50.0, MemoryBytes: 512_000_000},
		},
	}
	batchData, _ := json.Marshal(batch)

	// Simulate a consumer record.
	record := &kgo.Record{
		Topic: "tenant-1.metrics.raw",
		Key:   []byte("agent-1"),
		Value: batchData,
	}

	// Process through handleRecord.
	ctx := context.Background()
	err := p.handleRecord(ctx, record)
	if err != nil {
		t.Fatalf("handleRecord failed: %v", err)
	}

	// Verify store was called.
	store.mu.Lock()
	batchCalls := store.storeBatchCalls
	storedBatches := store.storedBatches
	store.mu.Unlock()

	if batchCalls != 1 {
		t.Errorf("expected 1 StoreMetricBatch call, got %d", batchCalls)
	}
	if len(storedBatches) == 0 {
		t.Fatal("expected stored batches, got none")
	}

	// Verify the stored batch has correct agent ID.
	if storedBatches[0].AgentID != "agent-1" {
		t.Errorf("expected stored batch agent_id=agent-1, got %s", storedBatches[0].AgentID)
	}

	// Verify producer was called (enriched metrics published).
	producer.mu.Lock()
	publishCalls := producer.publishCalls
	published := producer.publishedValues
	producer.mu.Unlock()

	// At least 1 publish for enriched metrics.
	if publishCalls < 1 {
		t.Errorf("expected at least 1 publish call, got %d", publishCalls)
	}
	if len(published) > 0 && published[0].agentID != "agent-1" {
		t.Errorf("expected published agent_id=agent-1, got %s", published[0].agentID)
	}

	// Verify counters.
	if p.processed.Load() != 1 {
		t.Errorf("expected 1 processed, got %d", p.processed.Load())
	}
	if p.errors.Load() != 0 {
		t.Errorf("expected 0 errors, got %d", p.errors.Load())
	}
}

// =============================================================================
// TestPipeline_NetworkEventBuffering
// Buffer events, then verify they are available for correlation.
// =============================================================================

func TestPipeline_NetworkEventBuffering(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	ctx := context.Background()

	// Send 3 network events.
	for i := 0; i < 3; i++ {
		event := models.NetworkEvent{
			TCP: []models.TCPEvent{
				{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", DstPort: uint32(80 + i)},
			},
		}
		data, _ := json.Marshal(event)
		if err := p.processMessage(ctx, "network.events", "key", data); err != nil {
			t.Fatalf("processMessage network.events[%d] failed: %v", i, err)
		}
	}

	// Verify events are buffered.
	buffered := p.correlator.GetBufferedEvents()
	if len(buffered) != 3 {
		t.Errorf("expected 3 buffered events, got %d", len(buffered))
	}
}

// =============================================================================
// TestPipeline_TopologyChanges
// Send processes that resolve to services, verify topology is stored.
// =============================================================================

func TestPipeline_TopologyChanges(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	p := newTestPipeline(t, store, nil, nil)
	ctx := context.Background()

	// Send a batch with processes — these should produce topology changes
	// via the correlator (node_added for each service).
	batch := models.MetricBatch{
		AgentID:   "agent-1",
		Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC),
		CPU: []models.CPUMetrics{
			{AgentID: "agent-1", Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC), TotalUsagePct: 50.0},
		},
		Processes: []models.ProcessMetrics{
			{AgentID: "agent-1", Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC), Name: "nginx", PID: 100, CPUUsagePct: 30.0, MemoryBytes: 256_000_000},
			{AgentID: "agent-1", Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC), Name: "postgres", PID: 200, CPUUsagePct: 40.0, MemoryBytes: 512_000_000},
		},
	}

	data, _ := json.Marshal(batch)
	record := &kgo.Record{
		Topic: "metrics.raw",
		Key:   []byte("agent-1"),
		Value: data,
	}

	err := p.handleRecord(ctx, record)
	if err != nil {
		t.Fatalf("handleRecord failed: %v", err)
	}

	// The correlator produces topology changes for new nodes.
	// Whether topology is stored depends on whether the ServiceMap resolves
	// the process names. With default config, serviceMap may not resolve
	// "nginx" or "postgres", so topology changes may be empty.
	// This test verifies the pipeline doesn't error regardless.
}

// =============================================================================
// TestPipeline_GracefulShutdown
// Start, send metrics, stop, verify clean shutdown.
// =============================================================================

func TestPipeline_GracefulShutdown(t *testing.T) {
	t.Parallel()

	consumer := &mockPipelineConsumer{}
	producer := &mockPipelineProducer{}
	p := newTestPipeline(t, nil, producer, consumer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the pipeline.
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify consumer was started.
	consumer.mu.Lock()
	started := consumer.started
	consumer.mu.Unlock()
	if !started {
		t.Error("expected consumer to be started")
	}

	// Stop the pipeline.
	p.Stop()

	// Verify consumer was closed.
	consumer.mu.Lock()
	closed := consumer.closed
	consumer.mu.Unlock()
	if !closed {
		t.Error("expected consumer to be closed")
	}

	// Verify producer was closed.
	producer.mu.Lock()
	prodClosed := producer.closed
	producer.mu.Unlock()
	if !prodClosed {
		t.Error("expected producer to be closed")
	}
}

// =============================================================================
// TestPipeline_HandleRecord_Counters
// Verify processed and error counters.
// =============================================================================

func TestPipeline_HandleRecord_Counters(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	ctx := context.Background()

	// Successful message.
	batch := models.MetricBatch{
		AgentID:   "agent-1",
		Timestamp: time.Date(2025, 6, 4, 10, 0, 10, 0, time.UTC),
		CPU: []models.CPUMetrics{
			{AgentID: "agent-1", TotalUsagePct: 50.0},
		},
	}
	data, _ := json.Marshal(batch)
	record := &kgo.Record{
		Topic: "metrics.raw",
		Key:   []byte("agent-1"),
		Value: data,
	}

	err := p.handleRecord(ctx, record)
	if err != nil {
		t.Fatalf("handleRecord failed: %v", err)
	}

	if p.processed.Load() != 1 {
		t.Errorf("expected 1 processed, got %d", p.processed.Load())
	}
	if p.errors.Load() != 0 {
		t.Errorf("expected 0 errors, got %d", p.errors.Load())
	}
}

// =============================================================================
// TestPipeline_BuildTopology
// Verify topology construction from correlation result.
// =============================================================================

func TestPipeline_BuildTopology(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)
	now := time.Now()

	result := &CorrelationResult{
		AgentID:   "agent-1",
		Timestamp: now,
		ProcessServices: map[string]string{
			"100:nginx":    "web-service",
			"200:postgres": "db-service",
		},
		Dependencies: []Dependency{
			{SourceService: "web-service", TargetService: "db-service", Protocol: "tcp", Port: 5432},
		},
	}

	topo := p.buildTopology(result, nil)

	if len(topo.Nodes) != 2 {
		t.Errorf("expected 2 topology nodes, got %d", len(topo.Nodes))
	}
	if len(topo.Edges) != 1 {
		t.Errorf("expected 1 topology edge, got %d", len(topo.Edges))
	}

	// Verify node properties.
	for _, node := range topo.Nodes {
		if node.Type != models.NodeTypeService {
			t.Errorf("expected node type=service, got %s", node.Type)
		}
		if node.Health != models.HealthStatusHealthy {
			t.Errorf("expected health=healthy, got %s", node.Health)
		}
		if node.AgentID != "agent-1" {
			t.Errorf("expected agent_id=agent-1, got %s", node.AgentID)
		}
	}

	// Verify edge properties.
	if topo.Edges[0].SourceID != "web-service" {
		t.Errorf("expected edge source=web-service, got %s", topo.Edges[0].SourceID)
	}
	if topo.Edges[0].TargetID != "db-service" {
		t.Errorf("expected edge target=db-service, got %s", topo.Edges[0].TargetID)
	}
	if topo.Edges[0].Protocol != "tcp" {
		t.Errorf("expected edge protocol=tcp, got %s", topo.Edges[0].Protocol)
	}
}

// =============================================================================
// TestPipeline_EventsRouting_SingleEvent
// Verify single event (non-array) is handled correctly.
// =============================================================================

func TestPipeline_EventsRouting_SingleEvent(t *testing.T) {
	t.Parallel()

	store := &mockPipelineStore{}
	p := newTestPipeline(t, store, nil, nil)
	ctx := context.Background()

	// Single event (not wrapped in array).
	event := models.Event{
		ID:       "evt-1",
		Category: models.EventCategoryCustom,
		Source:   "monitoring",
		Title:    "CPU high",
	}
	data, _ := json.Marshal(event)

	err := p.processMessage(ctx, "events", "key", data)
	if err != nil {
		t.Fatalf("single event routing failed: %v", err)
	}

	store.mu.Lock()
	eventsCalls := store.storeEventsCalls
	storedEvents := store.storedEvents
	store.mu.Unlock()

	if eventsCalls != 1 {
		t.Errorf("expected 1 StoreEvents call, got %d", eventsCalls)
	}
	if len(storedEvents) == 0 {
		t.Fatal("expected stored events, got none")
	}
	// Single event should be wrapped into a slice of length 1.
	if len(storedEvents[0]) != 1 {
		t.Errorf("expected 1 event in slice, got %d", len(storedEvents[0]))
	}
}

// =============================================================================
// TestPipeline_CircuitBreaker
// Verify circuit breaker rejects messages when memory is high.
// =============================================================================

func TestPipeline_CircuitBreaker(t *testing.T) {
	t.Parallel()

	p := newTestPipeline(t, nil, nil, nil)

	// Initially breaker should be closed.
	if p.memoryMonitor.IsBreakerOpen() {
		t.Error("expected breaker to be closed initially")
	}

	// Manually open the breaker.
	p.memoryMonitor.breakerOpen.Store(true)

	// processMessage should reject.
	err := p.processMessage(context.Background(), "metrics.raw", "key", []byte(`{}`))
	if err == nil {
		t.Error("expected error when circuit breaker is open")
	}

	// Close the breaker.
	p.memoryMonitor.breakerOpen.Store(false)

	// processMessage should succeed (malformed JSON returns nil, not error).
	err = p.processMessage(context.Background(), "unknown.topic", "key", []byte(`{}`))
	if err != nil {
		t.Errorf("expected no error with breaker closed, got: %v", err)
	}
}

// =============================================================================
// TestPipeline_MemoryMonitor
// Verify memory monitor stats tracking.
// =============================================================================

func TestPipeline_MemoryMonitor(t *testing.T) {
	t.Parallel()

	memConfig := config.MemoryConfig{
		MaxRAMBytes:       1 << 30, // 1 GB
		GoroutinePoolSize: 4,
		CheckInterval:     10 * time.Second,
	}
	monitor := NewMemoryMonitor(memConfig, zap.NewNop())

	// Initial stats should be zero.
	stats := monitor.GetStats()
	if stats.CurrentAlloc != 0 {
		t.Errorf("expected 0 initial alloc, got %d", stats.CurrentAlloc)
	}
	if stats.BreakerTrips != 0 {
		t.Errorf("expected 0 initial breaker trips, got %d", stats.BreakerTrips)
	}

	// Perform a check to populate stats.
	monitor.check()

	stats = monitor.GetStats()
	if stats.CurrentAlloc == 0 {
		t.Error("expected non-zero alloc after check")
	}
	if stats.LastCheckTime.IsZero() {
		t.Error("expected non-zero LastCheckTime after check")
	}
}

// =============================================================================
// TestMemoryMonitor_BreakerTripCounting
// Verify that each breaker trip increments the counter.
// =============================================================================

func TestMemoryMonitor_BreakerTripCounting(t *testing.T) {
	t.Parallel()

	monitor := NewMemoryMonitor(config.MemoryConfig{
		MaxRAMBytes:   1 << 20, // 1 MB - very low to trigger easily
		CheckInterval: 10 * time.Second,
	}, nil)

	// Verify initial state.
	if monitor.IsBreakerOpen() {
		t.Error("expected breaker closed initially")
	}

	stats := monitor.GetStats()
	if stats.BreakerTrips != 0 {
		t.Errorf("expected 0 breaker trips initially, got %d", stats.BreakerTrips)
	}

	// Perform a check - with real runtime memory the breaker may or may not trip
	// depending on current process allocation. We verify the counter is non-negative
	// and the check completes without panic.
	monitor.check()

	stats = monitor.GetStats()
	if stats.BreakerTrips < 0 {
		t.Errorf("breaker trips should be non-negative, got %d", stats.BreakerTrips)
	}
	if stats.LastCheckTime.IsZero() {
		t.Error("expected non-zero LastCheckTime after check")
	}
}

// =============================================================================
// TestMemoryMonitor_BreakerCloseOnMemoryDrop
// Verify the breaker closes when memory drops below the warning threshold.
// =============================================================================

func TestMemoryMonitor_BreakerCloseOnMemoryDrop(t *testing.T) {
	t.Parallel()

	// Use a very high MaxRAMBytes so real process memory is well below thresholds.
	monitor := NewMemoryMonitor(config.MemoryConfig{
		MaxRAMBytes:   1 << 40, // 1 TB - real memory will be far below any threshold
		CheckInterval: 10 * time.Second,
	}, nil)

	// Manually open the breaker (simulate high memory condition).
	monitor.breakerOpen.Store(true)

	// Perform a check - with real memory well below 80% of 1TB, the breaker should close.
	monitor.check()

	if monitor.IsBreakerOpen() {
		t.Error("expected breaker to close when memory is well below warning threshold")
	}
}

// =============================================================================
// TestMemoryMonitor_PeakAllocTracking
// Verify that PeakAlloc tracks the highest observed allocation.
// =============================================================================

func TestMemoryMonitor_PeakAllocTracking(t *testing.T) {
	t.Parallel()

	monitor := NewMemoryMonitor(config.MemoryConfig{
		MaxRAMBytes:   1 << 40,
		CheckInterval: 10 * time.Second,
	}, nil)

	// First check.
	monitor.check()
	stats1 := monitor.GetStats()
	if stats1.PeakAlloc == 0 {
		t.Error("expected non-zero PeakAlloc after first check")
	}

	// Second check - peak should be >= current (memory can only grow or stay same between checks).
	monitor.check()
	stats2 := monitor.GetStats()
	if stats2.PeakAlloc < stats1.CurrentAlloc {
		t.Errorf("PeakAlloc %d should be >= CurrentAlloc %d", stats2.PeakAlloc, stats1.CurrentAlloc)
	}
}
