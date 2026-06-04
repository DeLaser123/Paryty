// Package processing implements the data processing pipeline.
// This file contains tests for the graph-based Correlator engine.
package processing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// newTestServiceMap creates a ServiceMap with default rules for tests.
func newTestServiceMap(t *testing.T) *ServiceMap {
	t.Helper()
	sm, err := NewServiceMap(nil, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create test ServiceMap: %v", err)
	}
	return sm
}

// newTestCorrelator creates a Correlator with default config for tests.
func newTestCorrelator(t *testing.T) *Correlator {
	t.Helper()
	return NewCorrelator(
		DefaultCorrelatorConfig(),
		newTestServiceMap(t),
		zap.NewNop(),
	)
}

// newTestBatch builds a MetricBatch for tests.
func newTestBatch(agentID string, procs ...models.ProcessMetrics) *models.MetricBatch {
	batch := &models.MetricBatch{
		AgentID:   agentID,
		Timestamp: time.Now(),
	}
	if len(procs) > 0 {
		batch.Processes = procs
	}
	return batch
}

// =============================================================================
// TestCorrelator_BasicCorrelation
// =============================================================================

func TestCorrelator_BasicCorrelation(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)
	ctx := context.Background()

	batch := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 200, Name: "redis-server", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 300, Name: "mypython", AgentID: "agent-1"},
	)

	result, err := corr.Correlate(ctx, batch, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify agent ID propagated.
	if result.AgentID != "agent-1" {
		t.Errorf("expected agent_id=agent-1, got %s", result.AgentID)
	}

	// Verify process-to-service mapping: expect 3 entries.
	if len(result.ProcessServices) != 3 {
		t.Fatalf("expected 3 process services, got %d: %v", len(result.ProcessServices), result.ProcessServices)
	}

	// Verify nginx resolves to nginx.
	key := "100:nginx"
	if svc, ok := result.ProcessServices[key]; !ok || svc != "nginx" {
		t.Errorf("expected process_services[%q]=nginx, got %q (exists=%v)", key, svc, ok)
	}

	// Verify redis-server resolves to redis.
	key = "200:redis-server"
	if svc, ok := result.ProcessServices[key]; !ok || svc != "redis" {
		t.Errorf("expected process_services[%q]=redis, got %q (exists=%v)", key, svc, ok)
	}

	// Verify graph has one node per unique service.
	stats := result.GraphStats
	if stats.NodeCount != 3 {
		t.Errorf("expected 3 graph nodes, got %d", stats.NodeCount)
	}

	// Verify timestamp is recent (within 5 seconds).
	if time.Since(result.Timestamp) > 5*time.Second {
		t.Errorf("result timestamp is too old: %v", result.Timestamp)
	}
}

// =============================================================================
// TestCorrelator_GraphUpdates
// =============================================================================

func TestCorrelator_GraphUpdates(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)
	ctx := context.Background()

	// First batch: two services.
	batch1 := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 200, Name: "postgres", AgentID: "agent-1"},
	)

	result1, err := corr.Correlate(ctx, batch1, nil)
	if err != nil {
		t.Fatalf("batch 1 error: %v", err)
	}

	if result1.GraphStats.NodeCount != 2 {
		t.Errorf("batch 1: expected 2 nodes, got %d", result1.GraphStats.NodeCount)
	}

	// Verify node_added topology changes.
	addedChanges := 0
	for _, c := range result1.TopologyChanges {
		if c.Type == "node_added" {
			addedChanges++
		}
	}
	if addedChanges != 2 {
		t.Errorf("batch 1: expected 2 node_added changes, got %d", addedChanges)
	}

	// Second batch: same agent, same services (update) + one new service.
	batch2 := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 200, Name: "postgres", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 400, Name: "etcd", AgentID: "agent-1"},
	)

	result2, err := corr.Correlate(ctx, batch2, nil)
	if err != nil {
		t.Fatalf("batch 2 error: %v", err)
	}

	// Total nodes should be 3 (nginx, postgres, etcd).
	if result2.GraphStats.NodeCount != 3 {
		t.Errorf("batch 2: expected 3 nodes, got %d", result2.GraphStats.NodeCount)
	}

	// Only etcd should be a new node_added.
	addedChanges = 0
	for _, c := range result2.TopologyChanges {
		if c.Type == "node_added" {
			addedChanges++
		}
	}
	if addedChanges != 1 {
		t.Errorf("batch 2: expected 1 node_added change (etcd), got %d", addedChanges)
	}

	// Verify graph contains the expected nodes.
	graph := corr.GetGraph()
	nodes := graph.Nodes()
	nodeNames := make(map[string]bool)
	for _, n := range nodes {
		nodeNames[n.Name] = true
	}

	for _, expected := range []string{"nginx", "postgresql", "etcd"} {
		if !nodeNames[expected] {
			t.Errorf("expected node %q in graph, not found", expected)
		}
	}
}

// =============================================================================
// TestCorrelator_TopologyChanges
// =============================================================================

func TestCorrelator_TopologyChanges(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)
	ctx := context.Background()

	// Batch with processes that create service nodes.
	batch := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 200, Name: "postgres", AgentID: "agent-1"},
	)

	// Network events that create edges between services.
	networkEvents := []models.NetworkEvent{
		{
			AgentID: "agent-1",
			TCP: []models.TCPEvent{
				{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", SrcPort: 40000, DstPort: 5432, State: "SYN_SENT"},
				{SrcIP: "10.0.0.1", DstIP: "10.0.0.2", SrcPort: 40001, DstPort: 5432, State: "ESTABLISHED"},
			},
		},
	}

	result, err := corr.Correlate(ctx, batch, networkEvents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 dependencies (10.0.0.2:5432 aggregated).
	if len(result.Dependencies) < 1 {
		t.Errorf("expected at least 1 dependency, got %d", len(result.Dependencies))
	}

	// Verify the dependency details.
	for _, dep := range result.Dependencies {
		if dep.TargetService == "10.0.0.2" && dep.Port == 5432 {
			if dep.Protocol != "tcp" {
				t.Errorf("expected protocol=tcp, got %s", dep.Protocol)
			}
			if dep.Frequency < 1 {
				t.Errorf("expected frequency >= 1, got %d", dep.Frequency)
			}
		}
	}

	// Should have topology changes for node additions.
	hasNodeAdded := false
	for _, c := range result.TopologyChanges {
		if c.Type == "node_added" {
			hasNodeAdded = true
		}
	}
	if !hasNodeAdded {
		t.Error("expected at least one node_added topology change")
	}

	// Graph should have nodes.
	if result.GraphStats.NodeCount < 2 {
		t.Errorf("expected at least 2 graph nodes, got %d", result.GraphStats.NodeCount)
	}
}

// =============================================================================
// TestCorrelator_StaleCleanup
// =============================================================================

func TestCorrelator_StaleCleanup(t *testing.T) {
	t.Parallel()

	config := DefaultCorrelatorConfig()
	config.StaleNodeTimeout = 100 * time.Millisecond

	corr := NewCorrelator(config, newTestServiceMap(t), zap.NewNop())
	ctx := context.Background()

	// Add two services.
	batch := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 200, Name: "postgres", AgentID: "agent-1"},
	)

	_, err := corr.Correlate(ctx, batch, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if corr.GetGraph().NodeCount() != 2 {
		t.Fatalf("expected 2 nodes before cleanup, got %d", corr.GetGraph().NodeCount())
	}

	// Wait for nodes to become stale.
	time.Sleep(150 * time.Millisecond)

	// Add one more service that should NOT be stale.
	batch2 := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 300, Name: "etcd", AgentID: "agent-1"},
	)

	_, err = corr.Correlate(ctx, batch2, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now cleanup stale nodes. postgres should be removed (not refreshed).
	nodesRemoved, edgesRemoved := corr.CleanupStale(ctx)

	if nodesRemoved < 1 {
		t.Errorf("expected at least 1 stale node removed, got %d", nodesRemoved)
	}
	_ = edgesRemoved // edgesRemoved may be 0 if no edges exist

	// nginx and etcd should still be present.
	graph := corr.GetGraph()
	nodeNames := make(map[string]bool)
	for _, n := range graph.Nodes() {
		nodeNames[n.Name] = true
	}

	if !nodeNames["nginx"] {
		t.Error("expected nginx node to survive cleanup")
	}
	if !nodeNames["etcd"] {
		t.Error("expected etcd node to survive cleanup")
	}
	if nodeNames["postgresql"] {
		t.Error("expected postgresql node to be removed by cleanup")
	}
}

// =============================================================================
// TestCorrelator_EventBuffering
// =============================================================================

func TestCorrelator_EventBuffering(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)

	// Initially buffer is empty.
	events := corr.GetBufferedEvents()
	if len(events) != 0 {
		t.Errorf("expected empty buffer, got %d events", len(events))
	}

	// Push network events.
	input := []models.NetworkEvent{
		{
			AgentID: "agent-1",
			TCP:     []models.TCPEvent{{DstIP: "10.0.0.1", DstPort: 80}},
		},
		{
			AgentID: "agent-2",
			TCP:     []models.TCPEvent{{DstIP: "10.0.0.2", DstPort: 443}},
		},
	}

	corr.BufferNetworkEvents(input)

	events = corr.GetBufferedEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 buffered events, got %d", len(events))
	}

	// Verify order is preserved (oldest first).
	if events[0].AgentID != "agent-1" {
		t.Errorf("expected first event agent_id=agent-1, got %s", events[0].AgentID)
	}
	if events[1].AgentID != "agent-2" {
		t.Errorf("expected second event agent_id=agent-2, got %s", events[1].AgentID)
	}

	// Push more events and verify retrieval.
	input2 := []models.NetworkEvent{
		{AgentID: "agent-3"},
	}
	corr.BufferNetworkEvents(input2)

	events = corr.GetBufferedEvents()
	if len(events) != 3 {
		t.Errorf("expected 3 buffered events after second push, got %d", len(events))
	}
}

// =============================================================================
// TestCorrelator_EmptyBatch
// =============================================================================

func TestCorrelator_EmptyBatch(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)
	ctx := context.Background()

	// Empty batch with no processes and no network events.
	batch := newTestBatch("agent-1")

	result, err := corr.Correlate(ctx, batch, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No processes means no service mappings.
	if len(result.ProcessServices) != 0 {
		t.Errorf("expected 0 process services, got %d", len(result.ProcessServices))
	}

	// No network events means no dependencies.
	if len(result.Dependencies) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(result.Dependencies))
	}

	// No topology changes.
	if len(result.TopologyChanges) != 0 {
		t.Errorf("expected 0 topology changes, got %d", len(result.TopologyChanges))
	}

	// Graph should be empty.
	if result.GraphStats.NodeCount != 0 {
		t.Errorf("expected 0 graph nodes, got %d", result.GraphStats.NodeCount)
	}
	if result.GraphStats.EdgeCount != 0 {
		t.Errorf("expected 0 graph edges, got %d", result.GraphStats.EdgeCount)
	}
}

// =============================================================================
// TestCorrelator_MultipleAgents
// =============================================================================

func TestCorrelator_MultipleAgents(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)
	ctx := context.Background()

	// Agent 1 reports nginx.
	batch1 := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
	)

	result1, err := corr.Correlate(ctx, batch1, nil)
	if err != nil {
		t.Fatalf("agent-1 error: %v", err)
	}

	if result1.GraphStats.NodeCount != 1 {
		t.Fatalf("after agent-1: expected 1 node, got %d", result1.GraphStats.NodeCount)
	}

	// Agent 2 also reports nginx.
	batch2 := newTestBatch("agent-2",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-2"},
	)

	result2, err := corr.Correlate(ctx, batch2, nil)
	if err != nil {
		t.Fatalf("agent-2 error: %v", err)
	}

	// Should have 2 separate nodes: nginx:agent-1 and nginx:agent-2.
	if result2.GraphStats.NodeCount != 2 {
		t.Fatalf("after agent-2: expected 2 nodes, got %d", result2.GraphStats.NodeCount)
	}

	graph := corr.GetGraph()
	nodes := graph.Nodes()

	nodeIDs := make(map[string]bool)
	for _, n := range nodes {
		nodeIDs[n.ID] = true
	}

	if !nodeIDs["nginx:agent-1"] {
		t.Error("expected node nginx:agent-1 in graph")
	}
	if !nodeIDs["nginx:agent-2"] {
		t.Error("expected node nginx:agent-2 in graph")
	}

	// Verify tenant isolation: each node belongs to its respective agent.
	for _, n := range nodes {
		if n.ID == "nginx:agent-1" && n.AgentID != "agent-1" {
			t.Errorf("node nginx:agent-1 has agent_id=%s, want agent-1", n.AgentID)
		}
		if n.ID == "nginx:agent-2" && n.AgentID != "agent-2" {
			t.Errorf("node nginx:agent-2 has agent_id=%s, want agent-2", n.AgentID)
		}
	}
}

// =============================================================================
// TestCorrelator_ContextCancellation
// =============================================================================

func TestCorrelator_ContextCancellation(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)

	// Pre-cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	batch := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 100, Name: "nginx", AgentID: "agent-1"},
	)

	_, err := corr.Correlate(ctx, batch, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// =============================================================================
// TestCorrelator_ServiceMapResolution
// =============================================================================

func TestCorrelator_ServiceMapResolution(t *testing.T) {
	t.Parallel()

	sm := newTestServiceMap(t)
	corr := NewCorrelator(DefaultCorrelatorConfig(), sm, zap.NewNop())
	ctx := context.Background()

	batch := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 1, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 2, Name: "nginx-worker", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 3, Name: "postgres", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 4, Name: "redis-server", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 5, Name: "mongod", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 6, Name: "node", AgentID: "agent-1"},
	)

	result, err := corr.Correlate(ctx, batch, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		pid      uint32
		procName string
		wantSvc  string
	}{
		{1, "nginx", "nginx"},
		{2, "nginx-worker", "nginx"},       // prefix match
		{3, "postgres", "postgresql"},       // rule match
		{4, "redis-server", "redis"},        // rule match
		{5, "mongod", "mongodb"},            // rule match
		{6, "node", "nodejs"},               // rule match
	}

	for _, tt := range tests {
		key := fmt.Sprintf("%d:%s", tt.pid, tt.procName)
		got, ok := result.ProcessServices[key]
		if !ok {
			t.Errorf("process %s (pid %d) not in ProcessServices", tt.procName, tt.pid)
			continue
		}
		if got != tt.wantSvc {
			t.Errorf("process %s (pid %d): expected service %q, got %q", tt.procName, tt.pid, tt.wantSvc, got)
		}
	}
}

// =============================================================================
// TestCorrelator_CustomServiceMapping
// =============================================================================

func TestCorrelator_CustomServiceMapping(t *testing.T) {
	t.Parallel()

	sm := newTestServiceMap(t)

	// Add custom override: "myapp" → "payments-service"
	sm.AddCustom("myapp", "payments-service")

	corr := NewCorrelator(DefaultCorrelatorConfig(), sm, zap.NewNop())
	ctx := context.Background()

	batch := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 1, Name: "myapp", AgentID: "agent-1"},
	)

	result, err := corr.Correlate(ctx, batch, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	key := "1:myapp"
	svc, ok := result.ProcessServices[key]
	if !ok {
		t.Fatalf("expected process %q in ProcessServices", key)
	}
	if svc != "payments-service" {
		t.Errorf("expected service name 'payments-service', got %q", svc)
	}
}

// =============================================================================
// TestCorrelator_GraphStats
// =============================================================================

func TestCorrelator_GraphStats(t *testing.T) {
	t.Parallel()

	corr := newTestCorrelator(t)
	ctx := context.Background()

	// Empty graph stats.
	stats := corr.GraphStats()
	if stats.NodeCount != 0 || stats.EdgeCount != 0 {
		t.Errorf("empty graph: expected 0 nodes/edges, got %d/%d", stats.NodeCount, stats.EdgeCount)
	}

	// Add some services.
	batch := newTestBatch("agent-1",
		models.ProcessMetrics{PID: 1, Name: "nginx", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 2, Name: "postgres", AgentID: "agent-1"},
		models.ProcessMetrics{PID: 3, Name: "redis-server", AgentID: "agent-1"},
	)

	_, err := corr.Correlate(ctx, batch, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats = corr.GraphStats()
	if stats.NodeCount != 3 {
		t.Errorf("expected 3 nodes, got %d", stats.NodeCount)
	}

	// Isolated nodes should be 3 (no edges yet).
	if stats.IsolatedNodes != 3 {
		t.Errorf("expected 3 isolated nodes, got %d", stats.IsolatedNodes)
	}
}

// =============================================================================
// TestCorrelator_EventBufferSize
// =============================================================================

func TestCorrelator_EventBufferSize(t *testing.T) {
	t.Parallel()

	config := DefaultCorrelatorConfig()
	config.EventBufferSize = 3

	corr := NewCorrelator(config, newTestServiceMap(t), zap.NewNop())

	// Push 5 events into a buffer of size 3.
	events := []models.NetworkEvent{
		{AgentID: "a1"},
		{AgentID: "a2"},
		{AgentID: "a3"},
		{AgentID: "a4"},
		{AgentID: "a5"},
	}
	corr.BufferNetworkEvents(events)

	// Buffer should only contain the last 3.
	buffered := corr.GetBufferedEvents()
	if len(buffered) != 3 {
		t.Fatalf("expected 3 buffered events (buffer size=3), got %d", len(buffered))
	}

	// Oldest should be a3 (a1 and a2 evicted).
	if buffered[0].AgentID != "a3" {
		t.Errorf("expected oldest buffered event to be a3, got %s", buffered[0].AgentID)
	}
	if buffered[2].AgentID != "a5" {
		t.Errorf("expected newest buffered event to be a5, got %s", buffered[2].AgentID)
	}
}

// =============================================================================
// BenchmarkCorrelator_1000Events
// =============================================================================

func BenchmarkCorrelator_1000Events(b *testing.B) {
	sm, err := NewServiceMap(nil, zap.NewNop())
	if err != nil {
		b.Fatalf("failed to create ServiceMap: %v", err)
	}

	corr := NewCorrelator(DefaultCorrelatorConfig(), sm, zap.NewNop())
	ctx := context.Background()

	// Pre-build a batch with 10 processes.
	procs := make([]models.ProcessMetrics, 10)
	for i := range procs {
		procs[i] = models.ProcessMetrics{
			PID:     uint32(100 + i),
			Name:    fmt.Sprintf("svc-%d", i),
			AgentID: "bench-agent",
		}
	}

	// Pre-build network events with 100 TCP connections.
	tcpEvents := make([]models.TCPEvent, 100)
	for i := range tcpEvents {
		tcpEvents[i] = models.TCPEvent{
			SrcIP:   "10.0.0.1",
			DstIP:   fmt.Sprintf("10.0.%d.%d", i/256, i%256),
			SrcPort: uint32(40000 + i),
			DstPort: uint32(80 + i%10),
			State:   "ESTABLISHED",
		}
	}
	networkEvents := []models.NetworkEvent{
		{AgentID: "bench-agent", TCP: tcpEvents},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		batch := &models.MetricBatch{
			AgentID:   "bench-agent",
			Timestamp: time.Now(),
			Processes: procs,
		}

		_, err := corr.Correlate(ctx, batch, networkEvents)
		if err != nil {
			b.Fatalf("correlate error: %v", err)
		}
	}
}
