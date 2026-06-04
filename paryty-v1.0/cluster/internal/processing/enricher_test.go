// Package processing implements the data processing pipeline.
// This file contains tests for the enricher stage, covering label
// injection, cardinality limits, service name propagation, agent
// caching, missing agent handling, processing metadata, aggregated
// metric enrichment, and default configuration.
package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// countingDragonfly wraps a testDragonfly and tracks the number of Get calls.
type countingDragonfly struct {
	inner    *testDragonfly
	mu       sync.Mutex
	getCount int
}

func newCountingDragonfly() *countingDragonfly {
	return &countingDragonfly{
		inner: newTestDragonfly(),
	}
}

func (cd *countingDragonfly) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return cd.inner.Set(ctx, key, value, ttl)
}

func (cd *countingDragonfly) Get(ctx context.Context, key string) (string, error) {
	cd.mu.Lock()
	cd.getCount++
	cd.mu.Unlock()
	return cd.inner.Get(ctx, key)
}

func (cd *countingDragonfly) GetCount() int {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	return cd.getCount
}

// storeAgentInfo serialises and stores an AgentInfo in the mock Dragonfly.
func storeAgentInfo(t *testing.T, df *testDragonfly, agent *models.AgentInfo) {
	t.Helper()
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal agent info: %v", err)
	}
	key := fmt.Sprintf(agentStateKeyFmt, agent.ID)
	if err := df.Set(context.Background(), key, string(data), 0); err != nil {
		t.Fatalf("store agent info: %v", err)
	}
}

// newTestEnricherAgent creates a test AgentInfo with the given labels.
func newTestEnricherAgent(id string, labels map[string]string) *models.AgentInfo {
	return &models.AgentInfo{
		ID:            id,
		Hostname:      "host-" + id,
		OS:            "linux",
		Arch:          "x86_64",
		AgentVersion:  "1.0.0",
		Labels:        labels,
		Status:        models.AgentStatusOnline,
		RegisteredAt:  time.Now(),
		LastHeartbeat: time.Now(),
	}
}

// newTestMetricBatch creates a minimal MetricBatch for testing.
func newTestMetricBatch(agentID string) *models.MetricBatch {
	return &models.MetricBatch{
		AgentID:   agentID,
		Timestamp: time.Now(),
		CPU:       []models.CPUMetrics{{AgentID: agentID, TotalUsagePct: 50.0}},
		Memory:    []models.MemoryMetrics{{AgentID: agentID, TotalBytes: 8000, UsedBytes: 4000}},
		Disk:      []models.DiskMetrics{{AgentID: agentID, Device: "sda"}},
		Network:   []models.NetworkMetrics{{AgentID: agentID, Interface: "eth0"}},
		Processes: []models.ProcessMetrics{{AgentID: agentID, PID: 1, Name: "nginx"}},
	}
}

// =============================================================================
// TestEnricher_DefaultConfig
// =============================================================================

func TestEnricher_DefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultEnricherConfig()

	if cfg.MaxLabelsPerMetric != 20 {
		t.Errorf("expected MaxLabelsPerMetric=20, got %d", cfg.MaxLabelsPerMetric)
	}
	if cfg.LabelPrefix != "agent." {
		t.Errorf("expected LabelPrefix=agent., got %q", cfg.LabelPrefix)
	}

	expectedKeys := []string{"env", "region", "team", "service", "version"}
	if len(cfg.StandardLabelKeys) != len(expectedKeys) {
		t.Fatalf("expected %d standard label keys, got %d", len(expectedKeys), len(cfg.StandardLabelKeys))
	}
	for i, key := range expectedKeys {
		if cfg.StandardLabelKeys[i] != key {
			t.Errorf("StandardLabelKeys[%d] = %q, want %q", i, cfg.StandardLabelKeys[i], key)
		}
	}
}

// =============================================================================
// TestEnricher_LabelInjection
// =============================================================================

func TestEnricher_LabelInjection(t *testing.T) {
	t.Parallel()

	labels := map[string]string{
		"env":    "prod",
		"region": "us-east-1",
		"team":   "platform",
		"custom": "value",
	}

	agent := newTestEnricherAgent("agent-1", labels)
	df := newTestDragonfly()
	storeAgentInfo(t, df, agent)

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, df, zap.NewNop())
	ctx := context.Background()

	batch := newTestMetricBatch("agent-1")

	result, err := enricher.EnrichBatch(ctx, batch, nil)
	if err != nil {
		t.Fatalf("EnrichBatch failed: %v", err)
	}

	// Verify AgentID set on all metric types.
	if result.CPU[0].AgentID != "agent-1" {
		t.Errorf("CPU agent_id: got %q, want %q", result.CPU[0].AgentID, "agent-1")
	}
	if result.Memory[0].AgentID != "agent-1" {
		t.Errorf("Memory agent_id: got %q, want %q", result.Memory[0].AgentID, "agent-1")
	}
	if result.Disk[0].AgentID != "agent-1" {
		t.Errorf("Disk agent_id: got %q, want %q", result.Disk[0].AgentID, "agent-1")
	}
	if result.Network[0].AgentID != "agent-1" {
		t.Errorf("Network agent_id: got %q, want %q", result.Network[0].AgentID, "agent-1")
	}
	if result.Processes[0].AgentID != "agent-1" {
		t.Errorf("Processes agent_id: got %q, want %q", result.Processes[0].AgentID, "agent-1")
	}

	// Verify labels are correctly built via EnrichAggregated.
	aggMetrics := []models.AggregatedMetric{
		{AgentID: "agent-1", Name: "cpu.usage", AggType: models.AggregationAvg, Value: 50.0},
	}

	enriched, err := enricher.EnrichAggregated(ctx, aggMetrics, "agent-1")
	if err != nil {
		t.Fatalf("EnrichAggregated failed: %v", err)
	}

	if len(enriched) != 1 {
		t.Fatalf("expected 1 enriched metric, got %d", len(enriched))
	}

	lbls := enriched[0].Labels
	if lbls["agent.env"] != "prod" {
		t.Errorf("expected agent.env=prod, got %q", lbls["agent.env"])
	}
	if lbls["agent.region"] != "us-east-1" {
		t.Errorf("expected agent.region=us-east-1, got %q", lbls["agent.region"])
	}
	if lbls["agent.team"] != "platform" {
		t.Errorf("expected agent.team=platform, got %q", lbls["agent.team"])
	}
	if lbls["agent.custom"] != "value" {
		t.Errorf("expected agent.custom=value, got %q", lbls["agent.custom"])
	}
}

// =============================================================================
// TestEnricher_CardinalityLimit
// =============================================================================

func TestEnricher_CardinalityLimit(t *testing.T) {
	t.Parallel()

	// Create agent with 10 labels (3 standard + 7 non-standard).
	agentLabels := map[string]string{
		"env":    "prod",
		"region": "us-east-1",
		"team":   "platform",
		"svc":    "api",
		"ver":    "1.0",
		"foo1":   "bar1",
		"foo2":   "bar2",
		"foo3":   "bar3",
		"foo4":   "bar4",
		"foo5":   "bar5",
	}

	agent := newTestEnricherAgent("agent-1", agentLabels)
	df := newTestDragonfly()
	storeAgentInfo(t, df, agent)

	cfg := EnricherConfig{
		StandardLabelKeys:  []string{"env", "region", "team"},
		MaxLabelsPerMetric: 5,
		LabelPrefix:        "agent.",
	}

	enricher := NewEnricher(cfg, df, zap.NewNop())
	ctx := context.Background()

	aggMetrics := []models.AggregatedMetric{
		{AgentID: "agent-1", Name: "cpu.usage", AggType: models.AggregationAvg, Value: 50.0},
	}

	enriched, err := enricher.EnrichAggregated(ctx, aggMetrics, "agent-1")
	if err != nil {
		t.Fatalf("EnrichAggregated failed: %v", err)
	}

	lbls := enriched[0].Labels

	// Verify no metric exceeds 5 labels.
	if len(lbls) > 5 {
		t.Errorf("expected ≤ 5 labels, got %d: %v", len(lbls), lbls)
	}

	// Verify all standard labels are present (highest priority).
	if lbls["agent.env"] != "prod" {
		t.Errorf("standard label agent.env missing: got %q", lbls["agent.env"])
	}
	if lbls["agent.region"] != "us-east-1" {
		t.Errorf("standard label agent.region missing: got %q", lbls["agent.region"])
	}
	if lbls["agent.team"] != "platform" {
		t.Errorf("standard label agent.team missing: got %q", lbls["agent.team"])
	}
}

// =============================================================================
// TestEnricher_ServiceNamePropagation
// =============================================================================

func TestEnricher_ServiceNamePropagation(t *testing.T) {
	t.Parallel()

	agent := newTestEnricherAgent("agent-1", map[string]string{"env": "prod"})
	df := newTestDragonfly()
	storeAgentInfo(t, df, agent)

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, df, zap.NewNop())
	ctx := context.Background()

	batch := newTestMetricBatch("agent-1")

	correlationResult := &CorrelationResult{
		AgentID:   "agent-1",
		Timestamp: time.Now(),
		ProcessServices: map[string]string{
			"1:nginx": "web-frontend",
			"2:redis": "cache-layer",
		},
	}

	result, err := enricher.EnrichBatch(ctx, batch, correlationResult)
	if err != nil {
		t.Fatalf("EnrichBatch failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Verify buildLabels correctly incorporates service name from correlation.
	extraLabels := map[string]string{
		"service.name":     "web-frontend",
		"pipeline.version": pipelineVersion,
	}

	builtLabels := enricher.buildLabels(
		&models.AgentInfo{
			ID:     "agent-1",
			Labels: map[string]string{"env": "prod"},
		},
		extraLabels,
	)

	if builtLabels["service.name"] != "web-frontend" {
		t.Errorf("expected service.name=web-frontend, got %q", builtLabels["service.name"])
	}
	if builtLabels["pipeline.version"] != pipelineVersion {
		t.Errorf("expected pipeline.version=%s, got %q", pipelineVersion, builtLabels["pipeline.version"])
	}
}

// =============================================================================
// TestEnricher_AgentCacheHit
// =============================================================================

func TestEnricher_AgentCacheHit(t *testing.T) {
	t.Parallel()

	agent := newTestEnricherAgent("agent-1", map[string]string{"env": "prod"})
	cdf := newCountingDragonfly()
	// Store the agent in the counting dragonfly mock.
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal agent: %v", err)
	}
	key := fmt.Sprintf(agentStateKeyFmt, "agent-1")
	if err := cdf.Set(context.Background(), key, string(data), 0); err != nil {
		t.Fatalf("store agent: %v", err)
	}

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, cdf, zap.NewNop())
	ctx := context.Background()

	// First call — should hit Dragonfly.
	info1, err := enricher.getAgentInfo(ctx, "agent-1")
	if err != nil {
		t.Fatalf("first getAgentInfo failed: %v", err)
	}
	if info1.ID != "agent-1" {
		t.Errorf("first call: expected ID=agent-1, got %q", info1.ID)
	}

	firstCount := cdf.GetCount()
	if firstCount != 1 {
		t.Errorf("first call: expected 1 Dragonfly Get, got %d", firstCount)
	}

	// Second call — should hit cache, no additional Dragonfly Get.
	info2, err := enricher.getAgentInfo(ctx, "agent-1")
	if err != nil {
		t.Fatalf("second getAgentInfo failed: %v", err)
	}
	if info2.ID != "agent-1" {
		t.Errorf("second call: expected ID=agent-1, got %q", info2.ID)
	}

	secondCount := cdf.GetCount()
	if secondCount != 1 {
		t.Errorf("second call: expected 1 Dragonfly Get (cache hit), got %d", secondCount)
	}

	// Third call with different agent — should hit Dragonfly again.
	data2, _ := json.Marshal(newTestEnricherAgent("agent-2", nil))
	key2 := fmt.Sprintf(agentStateKeyFmt, "agent-2")
	_ = cdf.Set(context.Background(), key2, string(data2), 0)

	info3, err := enricher.getAgentInfo(ctx, "agent-2")
	if err != nil {
		t.Fatalf("third getAgentInfo (different agent) failed: %v", err)
	}
	if info3.ID != "agent-2" {
		t.Errorf("third call: expected ID=agent-2, got %q", info3.ID)
	}

	thirdCount := cdf.GetCount()
	if thirdCount != 2 {
		t.Errorf("third call: expected 2 Dragonfly Gets (new agent), got %d", thirdCount)
	}
}

// =============================================================================
// TestEnricher_MissingAgent
// =============================================================================

func TestEnricher_MissingAgent(t *testing.T) {
	t.Parallel()

	// Use nil dragonfly — all lookups return minimal AgentInfo.
	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, nil, zap.NewNop())
	ctx := context.Background()

	batch := newTestMetricBatch("unknown-agent")

	result, err := enricher.EnrichBatch(ctx, batch, nil)
	if err != nil {
		t.Fatalf("EnrichBatch with unknown agent should not error, got: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// AgentID should still be stamped (from minimal AgentInfo).
	if result.CPU[0].AgentID != "unknown-agent" {
		t.Errorf("expected CPU agent_id=unknown-agent, got %q", result.CPU[0].AgentID)
	}
	if result.Memory[0].AgentID != "unknown-agent" {
		t.Errorf("expected Memory agent_id=unknown-agent, got %q", result.Memory[0].AgentID)
	}
	if result.Disk[0].AgentID != "unknown-agent" {
		t.Errorf("expected Disk agent_id=unknown-agent, got %q", result.Disk[0].AgentID)
	}
	if result.Network[0].AgentID != "unknown-agent" {
		t.Errorf("expected Network agent_id=unknown-agent, got %q", result.Network[0].AgentID)
	}
	if result.Processes[0].AgentID != "unknown-agent" {
		t.Errorf("expected Processes agent_id=unknown-agent, got %q", result.Processes[0].AgentID)
	}

	// EnrichAggregated with unknown agent should also work without error.
	aggMetrics := []models.AggregatedMetric{
		{AgentID: "unknown-agent", Name: "cpu.usage", AggType: models.AggregationAvg, Value: 50.0},
	}
	enriched, err := enricher.EnrichAggregated(ctx, aggMetrics, "unknown-agent")
	if err != nil {
		t.Fatalf("EnrichAggregated with unknown agent should not error, got: %v", err)
	}
	if len(enriched) != 1 {
		t.Fatalf("expected 1 enriched metric, got %d", len(enriched))
	}
}

// =============================================================================
// TestEnricher_ProcessingMetadata
// =============================================================================

func TestEnricher_ProcessingMetadata(t *testing.T) {
	t.Parallel()

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, nil, zap.NewNop())

	// Truncate to second precision because RFC3339 only carries second resolution.
	before := time.Now().UTC().Truncate(time.Second)

	// Use buildLabels directly to verify processing metadata.
	extraLabels := map[string]string{
		"pipeline.version":    pipelineVersion,
		"pipeline.enriched_at": time.Now().UTC().Format(time.RFC3339),
	}

	builtLabels := enricher.buildLabels(
		&models.AgentInfo{
			ID:     "agent-1",
			Labels: map[string]string{"env": "test"},
		},
		extraLabels,
	)

	after := time.Now().UTC().Truncate(time.Second)

	if builtLabels["pipeline.version"] != "1.0.0" {
		t.Errorf("expected pipeline.version=1.0.0, got %q", builtLabels["pipeline.version"])
	}

	enrichedAt, ok := builtLabels["pipeline.enriched_at"]
	if !ok {
		t.Fatal("expected pipeline.enriched_at label to be present")
	}

	parsedTime, err := time.Parse(time.RFC3339, enrichedAt)
	if err != nil {
		t.Fatalf("pipeline.enriched_at is not valid RFC3339: %q, error: %v", enrichedAt, err)
	}

	if parsedTime.Before(before) || parsedTime.After(after.Add(time.Second)) {
		t.Errorf("pipeline.enriched_at %v is outside expected range [%v, %v]", parsedTime, before, after)
	}
}

// =============================================================================
// TestEnricher_EnrichAggregated
// =============================================================================

func TestEnricher_EnrichAggregated(t *testing.T) {
	t.Parallel()

	agentLabels := map[string]string{
		"env":    "staging",
		"region": "eu-west-1",
		"team":   "backend",
	}
	agent := newTestEnricherAgent("agent-agg", agentLabels)
	df := newTestDragonfly()
	storeAgentInfo(t, df, agent)

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, df, zap.NewNop())
	ctx := context.Background()

	inputMetrics := []models.AggregatedMetric{
		{
			AgentID:   "agent-agg",
			Name:      "cpu.usage_percent",
			Window:    time.Minute,
			AggType:   models.AggregationAvg,
			Value:     42.5,
			Timestamp: time.Now(),
		},
		{
			AgentID:   "agent-agg",
			Name:      "cpu.usage_percent",
			Window:    time.Minute,
			AggType:   models.AggregationP99,
			Value:     95.0,
			Timestamp: time.Now(),
		},
		{
			AgentID:   "agent-agg",
			Name:      "memory.used_bytes",
			Labels:    map[string]string{"existing": "label"},
			Window:    time.Minute,
			AggType:   models.AggregationSum,
			Value:     4000000,
			Timestamp: time.Now(),
		},
	}

	result, err := enricher.EnrichAggregated(ctx, inputMetrics, "agent-agg")
	if err != nil {
		t.Fatalf("EnrichAggregated failed: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 enriched metrics, got %d", len(result))
	}

	// Verify all metrics received agent labels.
	for i, m := range result {
		if m.Labels["agent.env"] != "staging" {
			t.Errorf("metric[%d] agent.env: got %q, want %q", i, m.Labels["agent.env"], "staging")
		}
		if m.Labels["agent.region"] != "eu-west-1" {
			t.Errorf("metric[%d] agent.region: got %q, want %q", i, m.Labels["agent.region"], "eu-west-1")
		}
		if m.Labels["agent.team"] != "backend" {
			t.Errorf("metric[%d] agent.team: got %q, want %q", i, m.Labels["agent.team"], "backend")
		}
	}

	// Verify existing labels are preserved on metric[2].
	if result[2].Labels["existing"] != "label" {
		t.Errorf("metric[2] existing label lost: got %q", result[2].Labels["existing"])
	}

	// Verify original metrics are not mutated.
	if inputMetrics[0].Labels != nil {
		t.Error("input metric[0] labels should not be mutated")
	}
	if inputMetrics[2].Labels["agent.env"] == "staging" {
		t.Error("input metric[2] labels should not be mutated with agent labels")
	}
}

// =============================================================================
// TestEnricher_EnrichAggregated_NilDragonfly
// =============================================================================

func TestEnricher_EnrichAggregated_NilDragonfly(t *testing.T) {
	t.Parallel()

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, nil, zap.NewNop())
	ctx := context.Background()

	aggMetrics := []models.AggregatedMetric{
		{AgentID: "agent-x", Name: "test", AggType: models.AggregationAvg, Value: 1.0},
	}

	result, err := enricher.EnrichAggregated(ctx, aggMetrics, "agent-x")
	if err != nil {
		t.Fatalf("EnrichAggregated with nil dragonfly should not error, got: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(result))
	}

	// Should still have processing metadata and standard labels (with empty values).
	if result[0].Labels == nil {
		t.Fatal("expected non-nil labels")
	}
}

// =============================================================================
// TestEnricher_EnrichTopology
// =============================================================================

func TestEnricher_EnrichTopology(t *testing.T) {
	t.Parallel()

	agent := newTestEnricherAgent("agent-topo", map[string]string{
		"env":  "production",
		"team": "infra",
	})
	df := newTestDragonfly()
	storeAgentInfo(t, df, agent)

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, df, zap.NewNop())
	ctx := context.Background()

	changes := []GraphChange{
		{
			Type: "node_added",
			Node: &GraphNode{
				ID:      "svc-a:agent-topo",
				Name:    "svc-a",
				Type:    NodeTypeService,
				AgentID: "agent-topo",
			},
			Timestamp: time.Now(),
			AgentID:   "agent-topo",
		},
		{
			Type: "edge_added",
			Edge: &GraphEdge{
				SourceID: "svc-a:agent-topo",
				TargetID: "svc-b:agent-topo",
				Protocol: "http",
			},
			Timestamp: time.Now(),
			AgentID:   "agent-topo",
		},
	}

	result, err := enricher.EnrichTopology(ctx, changes, "agent-topo")
	if err != nil {
		t.Fatalf("EnrichTopology failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(result))
	}

	// Verify node labels.
	nodeLbls := result[0].Node.Labels
	if nodeLbls["agent.env"] != "production" {
		t.Errorf("node agent.env: got %q, want %q", nodeLbls["agent.env"], "production")
	}
	if nodeLbls["agent.team"] != "infra" {
		t.Errorf("node agent.team: got %q, want %q", nodeLbls["agent.team"], "infra")
	}

	// Verify edge labels.
	edgeLbls := result[1].Edge.Labels
	if edgeLbls["agent.env"] != "production" {
		t.Errorf("edge agent.env: got %q, want %q", edgeLbls["agent.env"], "production")
	}
	if edgeLbls["agent.team"] != "infra" {
		t.Errorf("edge agent.team: got %q, want %q", edgeLbls["agent.team"], "infra")
	}

	// Verify original changes are not mutated.
	if changes[0].Node != nil && changes[0].Node.Labels != nil {
		if changes[0].Node.Labels["agent.env"] == "production" {
			t.Error("original change node labels should not be mutated")
		}
	}
}

// =============================================================================
// TestEnricher_ContextCancellation
// =============================================================================

func TestEnricher_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, nil, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	batch := newTestMetricBatch("agent-1")
	_, err := enricher.EnrichBatch(ctx, batch, nil)
	if err == nil {
		t.Error("expected error on cancelled context, got nil")
	}

	_, err = enricher.EnrichAggregated(ctx, nil, "agent-1")
	if err == nil {
		t.Error("expected error on cancelled context for EnrichAggregated, got nil")
	}

	_, err = enricher.EnrichTopology(ctx, nil, "agent-1")
	if err == nil {
		t.Error("expected error on cancelled context for EnrichTopology, got nil")
	}
}

// =============================================================================
// BenchmarkEnricher_1000Batches
// =============================================================================

func BenchmarkEnricher_1000Batches(b *testing.B) {
	agent := newTestEnricherAgent("bench-agent", map[string]string{
		"env":     "prod",
		"region":  "us-east-1",
		"team":    "platform",
		"tier":    "frontend",
		"version": "2.0",
	})
	df := newTestDragonfly()
	data, _ := json.Marshal(agent)
	key := fmt.Sprintf(agentStateKeyFmt, "bench-agent")
	_ = df.Set(context.Background(), key, string(data), 0)

	cfg := DefaultEnricherConfig()
	enricher := NewEnricher(cfg, df, zap.NewNop())
	ctx := context.Background()

	corrResult := &CorrelationResult{
		AgentID: "bench-agent",
		ProcessServices: map[string]string{
			"1:nginx": "web-service",
		},
	}

	batch := &models.MetricBatch{
		AgentID: "bench-agent",
		CPU:     make([]models.CPUMetrics, 4),
		Memory:  make([]models.MemoryMetrics, 1),
		Disk:    make([]models.DiskMetrics, 2),
		Network: make([]models.NetworkMetrics, 2),
		Processes: []models.ProcessMetrics{
			{PID: 1, Name: "nginx"},
			{PID: 2, Name: "redis"},
		},
	}

	// Warm the cache.
	_, _ = enricher.EnrichBatch(ctx, batch, corrResult)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := enricher.EnrichBatch(ctx, batch, corrResult)
		if err != nil {
			b.Fatalf("EnrichBatch failed: %v", err)
		}
	}
}
