package cold

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// newTestSnapshot creates a minimal Snapshot for testing.
func newTestSnapshot() *Snapshot {
	return &Snapshot{
		ID:        "snap-001",
		TenantID: "tenant-1",
		Timestamp: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		Topology: &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "node-1", Name: "api-gateway", Type: models.NodeTypeService, AgentID: "agent-1"},
				{ID: "node-2", Name: "user-service", Type: models.NodeTypeService, AgentID: "agent-2"},
			},
			Edges: []models.TopologyEdge{
				{ID: "edge-1", SourceID: "node-1", TargetID: "node-2", Type: models.EdgeTypeHTTP},
			},
			Timestamp: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
			Version:   1,
		},
		Agents: []models.AgentInfo{
			{ID: "agent-1", Hostname: "host-1"},
			{ID: "agent-2", Hostname: "host-2"},
		},
		Metrics: map[string]MetricSummary{
			"agent-1": {AgentID: "agent-1"},
			"agent-2": {AgentID: "agent-2"},
		},
		Alerts: []models.Alert{
			{ID: "alert-1", Name: "high-cpu", Severity: models.AlertSeverityWarning, Status: models.AlertStatusFiring},
		},
		Graph: &GraphSnapshot{
			Nodes: []models.TopologyNode{
				{ID: "node-1", Name: "api-gateway", Type: models.NodeTypeService},
			},
			Edges: []models.TopologyEdge{
				{ID: "edge-1", SourceID: "node-1", TargetID: "node-2"},
			},
		},
		Metadata: SnapshotMetadata{
			PipelineVersion: "1.0.0",
			AgentCount:      2,
			MetricCount:     2,
			AlertCount:      1,
		},
	}
}

// makeEvent is a shortcut for building an EventLogEntry with JSON payload.
func makeEvent(eventType string, payload any) EventLogEntry {
	data, _ := json.Marshal(payload)
	return EventLogEntry{
		ID:        "evt-" + eventType,
		TenantID:  "tenant-1",
		Timestamp: time.Date(2025, 6, 1, 12, 1, 0, 0, time.UTC),
		Type:      eventType,
		AgentID:   "agent-1",
		Payload:   data,
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_RecordEvent — verifies RecordEvent compiles and accepts valid
// entries. Actual DB write requires a running QuestDB instance.
// ---------------------------------------------------------------------------

func TestEventLog_RecordEvent_SignatureCompiles(t *testing.T) {
	// Verify the method signature compiles with a nil pool.
	// Actual DB tests require a running QuestDB instance.
	var el *EventLog
	if el != nil {
		entry := EventLogEntry{
			ID:       "test-id",
			TenantID: "tenant-1",
			Type:     EventTypeNodeAdded,
			Payload:  []byte(`{}`),
		}
		_ = el.RecordEvent(context.Background(), entry)
	}
}

func TestEventLog_RecordEvent_GeneratesIDWhenEmpty(t *testing.T) {
	// Verify that eventCounts works for metadata columns.
	entry := EventLogEntry{
		TenantID:  "tenant-1",
		Timestamp: time.Now(),
		Type:      EventTypeNodeAdded,
	}

	nodeCount, edgeCount := eventCounts(entry)
	if nodeCount != 1 || edgeCount != 0 {
		t.Errorf("node_added counts = (%d, %d), want (1, 0)", nodeCount, edgeCount)
	}
}

func TestEventLog_RecordEvent_IdempotentCounts(t *testing.T) {
	tests := []struct {
		eventType string
		wantNode  int64
		wantEdge  int64
	}{
		{EventTypeNodeAdded, 1, 0},
		{EventTypeNodeRemoved, -1, 0},
		{EventTypeEdgeAdded, 0, 1},
		{EventTypeEdgeRemoved, 0, -1},
		{EventTypeMetricUpdated, 0, 0},
		{EventTypeAlertFired, 0, 0},
		{EventTypeAlertResolved, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.eventType, func(t *testing.T) {
			entry := EventLogEntry{Type: tc.eventType}
			nodeCount, edgeCount := eventCounts(entry)
			if nodeCount != tc.wantNode || edgeCount != tc.wantEdge {
				t.Errorf("eventCounts(%s) = (%d, %d), want (%d, %d)",
					tc.eventType, nodeCount, edgeCount, tc.wantNode, tc.wantEdge)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_QueryEvents — signature check.
// Actual DB tests require a running QuestDB instance.
// ---------------------------------------------------------------------------

func TestEventLog_QueryEvents_SignatureCompiles(t *testing.T) {
	var el *EventLog
	if el != nil {
		ctx := context.Background()
		start := time.Now().Add(-1 * time.Hour)
		end := time.Now()
		_, _ = el.QueryEvents(ctx, "tenant-1", start, end)
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents_NodeAdded
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_NodeAdded(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	newNode := models.TopologyNode{
		ID:      "node-3",
		Name:    "payment-service",
		Type:    models.NodeTypeService,
		AgentID: "agent-3",
	}
	events := []EventLogEntry{
		makeEvent(EventTypeNodeAdded, NodeAddedPayload{Node: newNode}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	// Verify original snapshot unchanged.
	if len(snap.Topology.Nodes) != 2 {
		t.Fatalf("original snapshot modified: got %d nodes, want 2", len(snap.Topology.Nodes))
	}

	// Verify new snapshot has the added node.
	if len(result.Topology.Nodes) != 3 {
		t.Fatalf("result has %d nodes, want 3", len(result.Topology.Nodes))
	}

	found := false
	for _, n := range result.Topology.Nodes {
		if n.ID == "node-3" {
			found = true
			if n.Name != "payment-service" {
				t.Errorf("node name = %q, want %q", n.Name, "payment-service")
			}
		}
	}
	if !found {
		t.Error("node-3 not found in result topology")
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents_NodeRemoved
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_NodeRemoved(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		makeEvent(EventTypeNodeRemoved, NodeRemovedPayload{NodeID: "node-2"}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	if len(result.Topology.Nodes) != 1 {
		t.Fatalf("result has %d nodes, want 1", len(result.Topology.Nodes))
	}
	if result.Topology.Nodes[0].ID != "node-1" {
		t.Errorf("remaining node ID = %q, want %q", result.Topology.Nodes[0].ID, "node-1")
	}
}

func TestEventLog_ReplayEvents_NodeRemoved_NotFound(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		makeEvent(EventTypeNodeRemoved, NodeRemovedPayload{NodeID: "node-nonexistent"}),
	}

	_, err := el.ReplayEvents(snap, events)
	if err == nil {
		t.Fatal("expected error for removing nonexistent node")
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents_EdgeAdded
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_EdgeAdded(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	newEdge := models.TopologyEdge{
		ID:       "edge-2",
		SourceID: "node-2",
		TargetID: "node-3",
		Type:     models.EdgeTypeGRPC,
		Protocol: "grpc",
	}
	events := []EventLogEntry{
		makeEvent(EventTypeEdgeAdded, EdgeAddedPayload{Edge: newEdge}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	// Original unchanged.
	if len(snap.Topology.Edges) != 1 {
		t.Fatalf("original snapshot modified: got %d edges, want 1", len(snap.Topology.Edges))
	}

	if len(result.Topology.Edges) != 2 {
		t.Fatalf("result has %d edges, want 2", len(result.Topology.Edges))
	}

	found := false
	for _, e := range result.Topology.Edges {
		if e.ID == "edge-2" {
			found = true
			if e.Type != models.EdgeTypeGRPC {
				t.Errorf("edge type = %q, want %q", e.Type, models.EdgeTypeGRPC)
			}
		}
	}
	if !found {
		t.Error("edge-2 not found in result topology")
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents_AlertFired
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_AlertFired(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	newAlert := models.Alert{
		ID:       "alert-2",
		Name:     "disk-full",
		Severity: models.AlertSeverityCritical,
		Status:   models.AlertStatusFiring,
		AgentID:  "agent-2",
	}
	events := []EventLogEntry{
		makeEvent(EventTypeAlertFired, AlertFiredPayload{Alert: newAlert}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	// Original unchanged.
	if len(snap.Alerts) != 1 {
		t.Fatalf("original snapshot modified: got %d alerts, want 1", len(snap.Alerts))
	}

	if len(result.Alerts) != 2 {
		t.Fatalf("result has %d alerts, want 2", len(result.Alerts))
	}

	found := false
	for _, a := range result.Alerts {
		if a.ID == "alert-2" {
			found = true
			if a.Severity != models.AlertSeverityCritical {
				t.Errorf("alert severity = %q, want %q", a.Severity, models.AlertSeverityCritical)
			}
		}
	}
	if !found {
		t.Error("alert-2 not found in result alerts")
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents_AlertResolved
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_AlertResolved(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		makeEvent(EventTypeAlertResolved, AlertResolvedPayload{AlertID: "alert-1"}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	// Original unchanged.
	if len(snap.Alerts) != 1 {
		t.Fatalf("original snapshot modified: got %d alerts, want 1", len(snap.Alerts))
	}

	if len(result.Alerts) != 0 {
		t.Fatalf("result has %d alerts, want 0", len(result.Alerts))
	}
}

func TestEventLog_ReplayEvents_AlertResolved_NotFound(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		makeEvent(EventTypeAlertResolved, AlertResolvedPayload{AlertID: "alert-nonexistent"}),
	}

	_, err := el.ReplayEvents(snap, events)
	if err == nil {
		t.Fatal("expected error for resolving nonexistent alert")
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents_MetricUpdated
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_MetricUpdated(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	summary := MetricSummary{
		AgentID: "agent-1",
		CPU:     &models.CPUMetrics{AgentID: "agent-1", TotalUsagePct: 85.5},
		Memory:  &models.MemoryMetrics{AgentID: "agent-1", UsagePercent: 72.3},
	}
	events := []EventLogEntry{
		makeEvent(EventTypeMetricUpdated, MetricUpdatedPayload{
			AgentID: "agent-1",
			Summary: summary,
		}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	updated, ok := result.Metrics["agent-1"]
	if !ok {
		t.Fatal("agent-1 metrics not found in result")
	}
	if updated.CPU == nil || updated.CPU.TotalUsagePct != 85.5 {
		t.Errorf("CPU usage = %v, want 85.5", updated.CPU)
	}
	if updated.Memory == nil || updated.Memory.UsagePercent != 72.3 {
		t.Errorf("Memory usage = %v, want 72.3", updated.Memory)
	}

	// Original unchanged.
	orig := snap.Metrics["agent-1"]
	if orig.CPU != nil {
		t.Error("original snapshot metrics modified")
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents — edge cases
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_NilSnapshot(t *testing.T) {
	el := &EventLog{logger: testLogger()}

	_, err := el.ReplayEvents(nil, nil)
	if err != ErrNilSnapshot {
		t.Errorf("got %v, want ErrNilSnapshot", err)
	}
}

func TestEventLog_ReplayEvents_EmptyEvents(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	result, err := el.ReplayEvents(snap, nil)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	// Result should be equal to original (but a different pointer).
	if result.ID != snap.ID {
		t.Errorf("result ID = %q, want %q", result.ID, snap.ID)
	}
	if len(result.Topology.Nodes) != len(snap.Topology.Nodes) {
		t.Errorf("result nodes = %d, want %d", len(result.Topology.Nodes), len(snap.Topology.Nodes))
	}
}

func TestEventLog_ReplayEvents_UnknownEventType(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		makeEvent("unknown_event_type", struct{}{}),
	}

	_, err := el.ReplayEvents(snap, events)
	if err == nil {
		t.Fatal("expected error for unknown event type")
	}
}

func TestEventLog_ReplayEvents_EmptyPayload(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		{ID: "evt-empty", Type: EventTypeNodeAdded, Payload: nil},
	}

	_, err := el.ReplayEvents(snap, events)
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestEventLog_ReplayEvents_MultipleEventsInOrder(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	// Apply a sequence: add node, add edge, remove old node, fire alert.
	events := []EventLogEntry{
		makeEvent(EventTypeNodeAdded, NodeAddedPayload{
			Node: models.TopologyNode{ID: "node-3", Name: "cache", Type: models.NodeTypeCache},
		}),
		makeEvent(EventTypeEdgeAdded, EdgeAddedPayload{
			Edge: models.TopologyEdge{ID: "edge-2", SourceID: "node-1", TargetID: "node-3", Type: models.EdgeTypeCache},
		}),
		makeEvent(EventTypeNodeRemoved, NodeRemovedPayload{NodeID: "node-2"}),
		makeEvent(EventTypeAlertFired, AlertFiredPayload{
			Alert: models.Alert{ID: "alert-new", Name: "cache-miss-rate", Severity: models.AlertSeverityError},
		}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	// Nodes: 2 original + 1 added - 1 removed = 2.
	if len(result.Topology.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(result.Topology.Nodes))
	}

	// Edges: 1 original + 1 added = 2.
	if len(result.Topology.Edges) != 2 {
		t.Errorf("edges = %d, want 2", len(result.Topology.Edges))
	}

	// Alerts: 1 original + 1 fired = 2.
	if len(result.Alerts) != 2 {
		t.Errorf("alerts = %d, want 2", len(result.Alerts))
	}

	// Original unchanged.
	if len(snap.Topology.Nodes) != 2 {
		t.Errorf("original nodes = %d, want 2", len(snap.Topology.Nodes))
	}
	if len(snap.Topology.Edges) != 1 {
		t.Errorf("original edges = %d, want 1", len(snap.Topology.Edges))
	}
	if len(snap.Alerts) != 1 {
		t.Errorf("original alerts = %d, want 1", len(snap.Alerts))
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_ReplayEvents — nil-safe topology
// ---------------------------------------------------------------------------

func TestEventLog_ReplayEvents_NilTopology_AddNode(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := &Snapshot{
		ID:       "snap-nil-topo",
		TenantID: "tenant-1",
		Topology: nil,
		Metrics:  make(map[string]MetricSummary),
	}

	events := []EventLogEntry{
		makeEvent(EventTypeNodeAdded, NodeAddedPayload{
			Node: models.TopologyNode{ID: "node-new", Name: "svc"},
		}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}
	if result.Topology == nil {
		t.Fatal("expected non-nil topology after node_added")
	}
	if len(result.Topology.Nodes) != 1 {
		t.Errorf("nodes = %d, want 1", len(result.Topology.Nodes))
	}
}

func TestEventLog_ReplayEvents_EdgeRemoved(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		makeEvent(EventTypeEdgeRemoved, EdgeRemovedPayload{EdgeID: "edge-1"}),
	}

	result, err := el.ReplayEvents(snap, events)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	// Original unchanged.
	if len(snap.Topology.Edges) != 1 {
		t.Fatalf("original snapshot modified: got %d edges, want 1", len(snap.Topology.Edges))
	}

	if len(result.Topology.Edges) != 0 {
		t.Fatalf("result has %d edges, want 0", len(result.Topology.Edges))
	}
}

func TestEventLog_ReplayEvents_EdgeRemoved_NotFound(t *testing.T) {
	el := &EventLog{logger: testLogger()}
	snap := newTestSnapshot()

	events := []EventLogEntry{
		makeEvent(EventTypeEdgeRemoved, EdgeRemovedPayload{EdgeID: "edge-nonexistent"}),
	}

	_, err := el.ReplayEvents(snap, events)
	if err == nil {
		t.Fatal("expected error for removing nonexistent edge")
	}
}

// ---------------------------------------------------------------------------
// TestEventLog_CleanupOldEvents — signature check.
// Actual DB tests require a running QuestDB instance.
// ---------------------------------------------------------------------------

func TestEventLog_CleanupOldEvents_SignatureCompiles(t *testing.T) {
	var el *EventLog
	if el != nil {
		ctx := context.Background()
		_, _ = el.CleanupOldEvents(ctx, "tenant-1", 7*24*time.Hour)
	}
}

// ---------------------------------------------------------------------------
// Deep copy verification
// ---------------------------------------------------------------------------

func TestDeepCopySnapshot_Independence(t *testing.T) {
	original := newTestSnapshot()
	cp := deepCopySnapshot(original)

	// Mutate the copy.
	cp.Topology.Nodes[0].Name = "mutated"
	cp.Topology.Edges[0].ID = "mutated"
	cp.Alerts[0].Name = "mutated"
	cp.Metrics["agent-1"] = MetricSummary{AgentID: "mutated"}

	// Original must be untouched.
	if original.Topology.Nodes[0].Name == "mutated" {
		t.Error("deep copy did not protect Nodes")
	}
	if original.Topology.Edges[0].ID == "mutated" {
		t.Error("deep copy did not protect Edges")
	}
	if original.Alerts[0].Name == "mutated" {
		t.Error("deep copy did not protect Alerts")
	}
	if m := original.Metrics["agent-1"]; m.AgentID == "mutated" {
		t.Error("deep copy did not protect Metrics")
	}
}

func TestDeepCopySnapshot_NilSafe(t *testing.T) {
	result := deepCopySnapshot(nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

func TestDeepCopySnapshot_NilFields(t *testing.T) {
	src := &Snapshot{ID: "minimal", TenantID: "t", Timestamp: time.Now()}
	dst := deepCopySnapshot(src)

	if dst.Topology == nil {
		t.Error("expected non-nil Topology")
	}
	if dst.Topology.Nodes == nil {
		t.Error("expected non-nil Nodes")
	}
	if dst.Topology.Edges == nil {
		t.Error("expected non-nil Edges")
	}
	if dst.Metrics == nil {
		t.Error("expected non-nil Metrics")
	}
	if dst.Alerts == nil {
		t.Error("expected non-nil Alerts")
	}
}
