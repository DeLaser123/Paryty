package processing

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

func newTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// =============================================================================
// Aggregator Tests
// =============================================================================

func TestAggregator_AggregateCPU(t *testing.T) {
	agg := NewAggregator(newTestLogger())
	ctx := context.Background()

	batch := &models.MetricBatch{
		AgentID: "test-agent-1",
		CPU: []models.CPUMetrics{
			{AgentID: "test-agent-1", TotalUsagePct: 10.0},
			{AgentID: "test-agent-1", TotalUsagePct: 20.0},
			{AgentID: "test-agent-1", TotalUsagePct: 30.0},
			{AgentID: "test-agent-1", TotalUsagePct: 40.0},
			{AgentID: "test-agent-1", TotalUsagePct: 50.0},
		},
	}

	result, err := agg.Aggregate(ctx, batch, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce 6 aggregated metrics (avg, max, min, p50, p90, p99)
	if len(result) != 6 {
		t.Fatalf("expected 6 aggregated metrics, got %d", len(result))
	}

	// Verify avg = 30.0
	avgMetric := findAgg(result, "cpu.usage_percent", models.AggregationAvg)
	if avgMetric == nil {
		t.Fatal("missing avg metric")
	}
	if math.Abs(avgMetric.Value-30.0) > 0.01 {
		t.Errorf("expected avg=30.0, got %f", avgMetric.Value)
	}

	// Verify max = 50.0
	maxMetric := findAgg(result, "cpu.usage_percent", models.AggregationMax)
	if maxMetric == nil {
		t.Fatal("missing max metric")
	}
	if math.Abs(maxMetric.Value-50.0) > 0.01 {
		t.Errorf("expected max=50.0, got %f", maxMetric.Value)
	}

	// Verify min = 10.0
	minMetric := findAgg(result, "cpu.usage_percent", models.AggregationMin)
	if minMetric == nil {
		t.Fatal("missing min metric")
	}
	if math.Abs(minMetric.Value-10.0) > 0.01 {
		t.Errorf("expected min=10.0, got %f", minMetric.Value)
	}

	// Verify p50 = 30.0 (median of [10,20,30,40,50])
	p50Metric := findAgg(result, "cpu.usage_percent", models.AggregationP50)
	if p50Metric == nil {
		t.Fatal("missing p50 metric")
	}
	if math.Abs(p50Metric.Value-30.0) > 0.01 {
		t.Errorf("expected p50=30.0, got %f", p50Metric.Value)
	}
}

func TestAggregator_AggregateMemory(t *testing.T) {
	agg := NewAggregator(newTestLogger())
	ctx := context.Background()

	batch := &models.MetricBatch{
		AgentID: "test-agent-1",
		Memory: []models.MemoryMetrics{
			{AgentID: "test-agent-1", TotalBytes: 1000, UsedBytes: 500},
			{AgentID: "test-agent-1", TotalBytes: 1000, UsedBytes: 800},
		},
	}

	result, err := agg.Aggregate(ctx, batch, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce 1 aggregated metric (avg memory usage)
	if len(result) != 1 {
		t.Fatalf("expected 1 aggregated metric, got %d", len(result))
	}

	// avg of [50%, 80%] = 65%
	if math.Abs(result[0].Value-65.0) > 0.01 {
		t.Errorf("expected memory avg=65.0%%, got %f", result[0].Value)
	}
}

func TestAggregator_AggregateEmpty(t *testing.T) {
	agg := NewAggregator(newTestLogger())
	ctx := context.Background()

	batch := &models.MetricBatch{
		AgentID: "test-agent-1",
	}

	result, err := agg.Aggregate(ctx, batch, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 aggregated metrics for empty batch, got %d", len(result))
	}
}

func TestAggregator_AggregateDisk(t *testing.T) {
	agg := NewAggregator(newTestLogger())
	ctx := context.Background()

	batch := &models.MetricBatch{
		AgentID: "test-agent-1",
		Disk: []models.DiskMetrics{
			{AgentID: "test-agent-1", ReadBytesPerSec: 100, WriteBytesPerSec: 200},
			{AgentID: "test-agent-1", ReadBytesPerSec: 300, WriteBytesPerSec: 400},
		},
	}

	result, err := agg.Aggregate(ctx, batch, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should produce 2 aggregated metrics (avg read, avg write)
	if len(result) != 2 {
		t.Fatalf("expected 2 aggregated metrics, got %d", len(result))
	}

	readMetric := findAgg(result, "disk.read_bytes_per_sec", models.AggregationAvg)
	if readMetric == nil {
		t.Fatal("missing disk read metric")
	}
	if math.Abs(readMetric.Value-200.0) > 0.01 {
		t.Errorf("expected disk read avg=200, got %f", readMetric.Value)
	}
}

// =============================================================================
// Correlator Tests
// =============================================================================

func TestCorrelator_Correlate(t *testing.T) {
	corr := NewCorrelator(newTestLogger())
	ctx := context.Background()

	batch := &models.MetricBatch{
		AgentID: "test-agent-1",
		Processes: []models.ProcessMetrics{
			{PID: 1, Name: "nginx", AgentID: "test-agent-1"},
			{PID: 2, Name: "postgres", AgentID: "test-agent-1"},
			{PID: 3, Name: "redis", AgentID: "test-agent-1"},
		},
	}

	networkEvents := []models.NetworkEvent{
		{
			TCP: []models.TCPEvent{
				{DstIP: "10.0.0.2", DstPort: 5432, State: "SYN_SENT"},
				{DstIP: "10.0.0.3", DstPort: 6379, State: "SYN_SENT"},
			},
			HTTP: []models.HTTPEvent{
				{Host: "api.example.com"},
			},
		},
		{
			TCP: []models.TCPEvent{
				{DstIP: "10.0.0.2", DstPort: 5432, State: "ESTABLISHED"},
			},
		},
	}

	result, err := corr.Correlate(ctx, batch, networkEvents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AgentID != "test-agent-1" {
		t.Errorf("expected agent_id=test-agent-1, got %s", result.AgentID)
	}

	// Should map processes to services
	if len(result.ProcessServices) != 3 {
		t.Errorf("expected 3 process services, got %d", len(result.ProcessServices))
	}

	// Should extract dependencies
	if len(result.Dependencies) < 2 {
		t.Errorf("expected at least 2 dependencies, got %d", len(result.Dependencies))
	}

	// Should detect topology changes (SYN_SENT events)
	if len(result.TopologyChanges) < 2 {
		t.Errorf("expected at least 2 topology changes, got %d", len(result.TopologyChanges))
	}
}

func TestCorrelator_InferServiceName(t *testing.T) {
	corr := NewCorrelator(newTestLogger())

	tests := []struct {
		process  string
		expected string
	}{
		{"nginx", "nginx"},
		{"nginx-worker", "nginx"},
		{"postgres", "postgresql"},
		{"redis", "redis"},
		{"node", "nodejs"},
		{"unknown-service", "unknown-service"},
	}

	for _, tt := range tests {
		result := corr.inferServiceName(tt.process)
		if result != tt.expected {
			t.Errorf("inferServiceName(%q) = %q, want %q", tt.process, result, tt.expected)
		}
	}
}

func TestCorrelator_ExtractDependencies(t *testing.T) {
	corr := NewCorrelator(newTestLogger())

	events := []models.NetworkEvent{
		{
			TCP: []models.TCPEvent{
				{DstIP: "10.0.0.1", DstPort: 80},
				{DstIP: "10.0.0.1", DstPort: 80},
				{DstIP: "10.0.0.2", DstPort: 5432},
			},
			HTTP: []models.HTTPEvent{
				{Host: "api.example.com"},
				{Host: "api.example.com"},
			},
		},
	}

	deps := corr.extractDependencies(events)

	// Should have 3 unique dependencies
	if len(deps) != 3 {
		t.Fatalf("expected 3 dependencies, got %d", len(deps))
	}

	// Find the 10.0.0.1:80 dependency - should have frequency 2
	for _, dep := range deps {
		if dep.Target == "10.0.0.1" && dep.Port == 80 {
			if dep.Frequency != 2 {
				t.Errorf("expected frequency=2 for 10.0.0.1:80, got %d", dep.Frequency)
			}
			if dep.Protocol != "tcp" {
				t.Errorf("expected protocol=tcp, got %s", dep.Protocol)
			}
		}
		if dep.Target == "api.example.com" {
			if dep.Frequency != 2 {
				t.Errorf("expected frequency=2 for api.example.com, got %d", dep.Frequency)
			}
			if dep.Protocol != "http" {
				t.Errorf("expected protocol=http, got %s", dep.Protocol)
			}
		}
	}
}

// =============================================================================
// Enricher Tests
// =============================================================================

func TestEnricher_EnrichBatch(t *testing.T) {
	enricher := NewEnricher(newTestLogger())
	ctx := context.Background()

	batch := &models.MetricBatch{
		AgentID:   "test-agent-1",
		CPU:       []models.CPUMetrics{{TotalUsagePct: 50.0}},
		Memory:    []models.MemoryMetrics{{TotalBytes: 1000, UsedBytes: 500}},
		Disk:      []models.DiskMetrics{{Device: "sda"}},
		Network:   []models.NetworkMetrics{{Interface: "eth0"}},
		Processes: []models.ProcessMetrics{{PID: 1, Name: "nginx"}},
	}

	agentInfo := &models.AgentInfo{
		ID:       "test-agent-1",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "x86_64",
		Labels:   map[string]string{"env": "test", "region": "us-east-1"},
	}

	result, err := enricher.EnrichBatch(ctx, batch, agentInfo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify CPU metrics enriched
	if result.CPU[0].AgentID != "test-agent-1" {
		t.Errorf("expected CPU agent_id=test-agent-1, got %s", result.CPU[0].AgentID)
	}

	// Verify memory metrics enriched
	if result.Memory[0].AgentID != "test-agent-1" {
		t.Errorf("expected memory agent_id=test-agent-1, got %s", result.Memory[0].AgentID)
	}

	// Verify disk metrics enriched
	if result.Disk[0].AgentID != "test-agent-1" {
		t.Errorf("expected disk agent_id=test-agent-1, got %s", result.Disk[0].AgentID)
	}

	// Verify network metrics enriched
	if result.Network[0].AgentID != "test-agent-1" {
		t.Errorf("expected network agent_id=test-agent-1, got %s", result.Network[0].AgentID)
	}

	// Verify process metrics enriched
	if result.Processes[0].AgentID != "test-agent-1" {
		t.Errorf("expected process agent_id=test-agent-1, got %s", result.Processes[0].AgentID)
	}
}

func TestEnricher_EnrichMetric(t *testing.T) {
	enricher := NewEnricher(newTestLogger())
	ctx := context.Background()

	metric := &models.Metric{
		AgentID: "test-agent-1",
		Name:    "cpu.usage",
		Value:   50.0,
	}

	labels := map[string]string{"env": "prod", "host": "server-1"}
	result := enricher.EnrichMetric(ctx, metric, labels)

	if result.Labels["env"] != "prod" {
		t.Errorf("expected label env=prod, got %s", result.Labels["env"])
	}
	if result.Labels["host"] != "server-1" {
		t.Errorf("expected label host=server-1, got %s", result.Labels["host"])
	}
}

func TestEnricher_EnrichTopology(t *testing.T) {
	enricher := NewEnricher(newTestLogger())
	ctx := context.Background()

	node := &models.TopologyNode{
		ID:   "node-1",
		Name: "api-gateway",
	}

	agentInfo := &models.AgentInfo{
		ID:           "agent-1",
		Hostname:     "host-1",
		OS:           "linux",
		Arch:         "x86_64",
		AgentVersion: "1.0.0",
		Labels:       map[string]string{"env": "prod"},
	}

	result := enricher.EnrichTopology(ctx, node, agentInfo)

	if result.Labels["agent_id"] != "agent-1" {
		t.Errorf("expected label agent_id=agent-1, got %s", result.Labels["agent_id"])
	}
	if result.Labels["hostname"] != "host-1" {
		t.Errorf("expected label hostname=host-1, got %s", result.Labels["hostname"])
	}
	if result.Metadata["os"] != "linux" {
		t.Errorf("expected metadata os=linux, got %s", result.Metadata["os"])
	}
	if result.Labels["env"] != "prod" {
		t.Errorf("expected label env=prod, got %s", result.Labels["env"])
	}
}

// =============================================================================
// Helper functions
// =============================================================================

func findAgg(metrics []models.AggregatedMetric, name string, aggType models.AggregationType) *models.AggregatedMetric {
	for _, m := range metrics {
		if m.Name == name && m.AggType == aggType {
			return &m
		}
	}
	return nil
}
