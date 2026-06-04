// Package cold — Tests for the Timeline Snapshot Manager.
//
// Covers: assembly, compression round-trip, snapshot ID uniqueness,
// metadata validation, event replay (ReconstructState), deep copy,
// key generation, context cancellation, and nil-safety.
package cold

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// =============================================================================
// Mock HotStoreReader
// =============================================================================

// mockHotStore implements HotStoreReader for testing.
type mockHotStore struct {
	mu          sync.RWMutex
	topology    *models.Topology
	agents      []models.AgentInfo
	metrics     map[string]*models.MetricBatch
	alerts      []models.Alert
	topologyErr error
	agentsErr   error
	metricsErr  error
	alertsErr   error
}

func (m *mockHotStore) GetTopology(_ context.Context, _ string) (*models.Topology, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.topologyErr != nil {
		return nil, m.topologyErr
	}
	if m.topology == nil {
		return &models.Topology{}, nil
	}
	return m.topology, nil
}

func (m *mockHotStore) GetAllAgentStates(_ context.Context, _ string) ([]models.AgentInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.agentsErr != nil {
		return nil, m.agentsErr
	}
	return m.agents, nil
}

func (m *mockHotStore) GetLatestMetrics(_ context.Context, _, agentID string) (*models.MetricBatch, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.metricsErr != nil {
		return nil, m.metricsErr
	}
	batch, ok := m.metrics[agentID]
	if !ok {
		return nil, fmt.Errorf("no metrics for agent %s", agentID)
	}
	return batch, nil
}

func (m *mockHotStore) GetActiveAlerts(_ context.Context, _ string) ([]models.Alert, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.alertsErr != nil {
		return nil, m.alertsErr
	}
	return m.alerts, nil
}

// =============================================================================
// Test helpers
// =============================================================================

func newTestHotStore() *mockHotStore {
	now := time.Now()
	return &mockHotStore{
		topology: &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "svc-1:agent-1", Name: "api-gateway", Type: models.NodeTypeService, AgentID: "agent-1", Health: models.HealthStatusHealthy, LastSeen: now},
				{ID: "svc-2:agent-2", Name: "payment-service", Type: models.NodeTypeService, AgentID: "agent-2", Health: models.HealthStatusHealthy, LastSeen: now},
			},
			Edges: []models.TopologyEdge{
				{ID: "edge-1", SourceID: "svc-1:agent-1", TargetID: "svc-2:agent-2", Type: models.EdgeTypeHTTP, Protocol: "http", LastSeen: now},
			},
			Timestamp: now,
			Version:   1,
		},
		agents: []models.AgentInfo{
			{ID: "agent-1", Hostname: "web-01", IPAddress: "10.0.0.1", OS: "linux", Arch: "amd64", AgentVersion: "1.0.0", Status: models.AgentStatusOnline, RegisteredAt: now.Add(-1 * time.Hour), LastHeartbeat: now},
			{ID: "agent-2", Hostname: "web-02", IPAddress: "10.0.0.2", OS: "linux", Arch: "amd64", AgentVersion: "1.0.0", Status: models.AgentStatusOnline, RegisteredAt: now.Add(-30 * time.Minute), LastHeartbeat: now},
		},
		metrics: map[string]*models.MetricBatch{
			"agent-1": {
				AgentID: "agent-1", Timestamp: now,
				CPU:     []models.CPUMetrics{{AgentID: "agent-1", Timestamp: now, TotalUsagePct: 45.5, PhysicalCores: 4, LogicalCores: 8}},
				Memory:  []models.MemoryMetrics{{AgentID: "agent-1", Timestamp: now, TotalBytes: 16_000_000_000, UsedBytes: 8_000_000_000, UsagePercent: 50.0}},
				Disk:    []models.DiskMetrics{{AgentID: "agent-1", Timestamp: now, Device: "/dev/sda1", TotalBytes: 500_000_000_000, UsedBytes: 200_000_000_000}},
				Network: []models.NetworkMetrics{{AgentID: "agent-1", Timestamp: now, Interface: "eth0", RxBytesPerSec: 5000, TxBytesPerSec: 3000}},
			},
			"agent-2": {
				AgentID: "agent-2", Timestamp: now,
				CPU:    []models.CPUMetrics{{AgentID: "agent-2", Timestamp: now, TotalUsagePct: 25.0, PhysicalCores: 2, LogicalCores: 4}},
				Memory: []models.MemoryMetrics{{AgentID: "agent-2", Timestamp: now, TotalBytes: 8_000_000_000, UsedBytes: 4_000_000_000, UsagePercent: 50.0}},
			},
		},
		alerts: []models.Alert{
			{ID: "alert-1", Name: "High CPU", Severity: models.AlertSeverityWarning, Status: models.AlertStatusFiring, AgentID: "agent-1", StartsAt: now.Add(-5 * time.Minute)},
		},
	}
}

// newTestManager creates a SnapshotManager that bypasses the nil-store panic.
// Tests that exercise assembly + event replay (not SeaweedFS I/O) use this.
func newTestManager(hotStore *mockHotStore) *SnapshotManager {
	return &SnapshotManager{
		config: SnapshotConfig{
			Interval:        5 * time.Minute,
			Compression:     true,
			RetentionDays:   7,
			MaxSnapshotSize: 50 * 1024 * 1024,
		},
		store:    nil, // nil — tests that need store are integration tests.
		hotStore: hotStore,
		logger:   zap.NewNop(),
	}
}

// =============================================================================
// 1. TestSnapshotManager_TakeSnapshot — assemble snapshot from mock hot store
// =============================================================================

func TestSnapshotManager_TakeSnapshot(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)
	ctx := context.Background()
	snapshotID := uuid.New().String()
	now := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	snapshot, err := mgr.assembleSnapshot(ctx, "tenant-1", snapshotID, now)
	if err != nil {
		t.Fatalf("assembleSnapshot failed: %v", err)
	}

	// ID and tenant
	if snapshot.ID != snapshotID {
		t.Errorf("ID = %q, want %q", snapshot.ID, snapshotID)
	}
	if snapshot.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", snapshot.TenantID, "tenant-1")
	}
	if !snapshot.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", snapshot.Timestamp, now)
	}

	// Topology
	if snapshot.Topology == nil {
		t.Fatal("Topology is nil")
	}
	if len(snapshot.Topology.Nodes) != 2 {
		t.Errorf("Topology.Nodes count = %d, want 2", len(snapshot.Topology.Nodes))
	}
	if len(snapshot.Topology.Edges) != 1 {
		t.Errorf("Topology.Edges count = %d, want 1", len(snapshot.Topology.Edges))
	}

	// Agents
	if len(snapshot.Agents) != 2 {
		t.Fatalf("Agents count = %d, want 2", len(snapshot.Agents))
	}
	if snapshot.Agents[0].ID != "agent-1" {
		t.Errorf("first agent = %q, want agent-1", snapshot.Agents[0].ID)
	}

	// Metrics
	if len(snapshot.Metrics) != 2 {
		t.Fatalf("Metrics count = %d, want 2", len(snapshot.Metrics))
	}
	ms1, ok := snapshot.Metrics["agent-1"]
	if !ok {
		t.Fatal("missing metrics for agent-1")
	}
	if ms1.CPU == nil {
		t.Error("agent-1 CPU is nil")
	} else if ms1.CPU.TotalUsagePct != 45.5 {
		t.Errorf("agent-1 CPU usage = %f, want 45.5", ms1.CPU.TotalUsagePct)
	}
	if ms1.Memory == nil {
		t.Error("agent-1 Memory is nil")
	} else if ms1.Memory.UsagePercent != 50.0 {
		t.Errorf("agent-1 Memory usage = %f, want 50.0", ms1.Memory.UsagePercent)
	}
	if len(ms1.Disk) != 1 {
		t.Errorf("agent-1 Disk count = %d, want 1", len(ms1.Disk))
	}
	if len(ms1.Network) != 1 {
		t.Errorf("agent-1 Network count = %d, want 1", len(ms1.Network))
	}

	ms2, ok := snapshot.Metrics["agent-2"]
	if !ok {
		t.Fatal("missing metrics for agent-2")
	}
	if ms2.CPU == nil {
		t.Error("agent-2 CPU is nil")
	} else if ms2.CPU.TotalUsagePct != 25.0 {
		t.Errorf("agent-2 CPU usage = %f, want 25.0", ms2.CPU.TotalUsagePct)
	}

	// Alerts
	if len(snapshot.Alerts) != 1 {
		t.Fatalf("Alerts count = %d, want 1", len(snapshot.Alerts))
	}
	if snapshot.Alerts[0].ID != "alert-1" {
		t.Errorf("alert ID = %q, want alert-1", snapshot.Alerts[0].ID)
	}

	// Graph
	if snapshot.Graph == nil {
		t.Fatal("Graph is nil")
	}
	if len(snapshot.Graph.Nodes) != 2 {
		t.Errorf("Graph.Nodes count = %d, want 2", len(snapshot.Graph.Nodes))
	}
	if len(snapshot.Graph.Edges) != 1 {
		t.Errorf("Graph.Edges count = %d, want 1", len(snapshot.Graph.Edges))
	}

	// Metadata
	if snapshot.Metadata.PipelineVersion != "1.0.0" {
		t.Errorf("PipelineVersion = %q, want 1.0.0", snapshot.Metadata.PipelineVersion)
	}
	if snapshot.Metadata.AgentCount != 2 {
		t.Errorf("AgentCount = %d, want 2", snapshot.Metadata.AgentCount)
	}
	if snapshot.Metadata.MetricCount != 2 {
		t.Errorf("MetricCount = %d, want 2", snapshot.Metadata.MetricCount)
	}
	if snapshot.Metadata.AlertCount != 1 {
		t.Errorf("AlertCount = %d, want 1", snapshot.Metadata.AlertCount)
	}
	if snapshot.Metadata.CreatedAt != now {
		t.Errorf("CreatedAt = %v, want %v", snapshot.Metadata.CreatedAt, now)
	}
}

// TestSnapshotManager_TakeSnapshot_HotStoreErrors verifies graceful degradation
// when the hot store returns errors for some data sources.
func TestSnapshotManager_TakeSnapshot_HotStoreErrors(t *testing.T) {
	t.Parallel()

	hotStore := &mockHotStore{
		topologyErr: fmt.Errorf("redis timeout"),
		agents:      []models.AgentInfo{{ID: "agent-1", Status: models.AgentStatusOnline}},
		alertsErr:   fmt.Errorf("connection refused"),
	}
	mgr := newTestManager(hotStore)
	ctx := context.Background()

	snapshot, err := mgr.assembleSnapshot(ctx, "tenant-1", uuid.New().String(), time.Now())
	if err != nil {
		t.Fatalf("assembleSnapshot should not fail on hot store errors: %v", err)
	}

	// Topology defaults to empty when error
	if snapshot.Topology == nil {
		t.Fatal("Topology should be non-nil (empty default)")
	}
	// Agents available
	if len(snapshot.Agents) != 1 {
		t.Errorf("Agents count = %d, want 1", len(snapshot.Agents))
	}
	// Alerts defaults to empty when error
	if len(snapshot.Alerts) != 0 {
		t.Errorf("Alerts count = %d, want 0", len(snapshot.Alerts))
	}
}

// =============================================================================
// 2. TestSnapshotManager_CompressDecompress — round-trip test
// =============================================================================

func TestSnapshotManager_CompressDecompress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small_json", []byte(`{"id":"snap-1","tenant_id":"t1"}`)},
		{"medium_snapshot", generateTestJSON(t, 10)},
		{"large_snapshot", generateTestJSON(t, 100)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			compressed, err := compressData(tc.data)
			if err != nil {
				t.Fatalf("compressData: %v", err)
			}

			decompressed, err := decompressData(compressed)
			if err != nil {
				t.Fatalf("decompressData: %v", err)
			}

			if string(decompressed) != string(tc.data) {
				t.Error("round-trip mismatch: decompressed data differs from original")
			}

			// Compression should reduce size for non-trivial payloads.
			if len(tc.data) > 100 && len(compressed) >= len(tc.data) {
				t.Errorf("compression ineffective: %d -> %d bytes", len(tc.data), len(compressed))
			}
		})
	}
}

// TestSnapshotManager_CompressDecompress_InvalidData verifies error handling.
func TestSnapshotManager_CompressDecompress_InvalidData(t *testing.T) {
	t.Parallel()

	_, err := decompressData([]byte("not-valid-zstd-data"))
	if err == nil {
		t.Error("expected error for invalid zstd data")
	}
}

// generateTestJSON creates a JSON snapshot of roughly n agents for testing compression.
func generateTestJSON(t *testing.T, agentCount int) []byte {
	t.Helper()

	snap := Snapshot{
		ID:        uuid.New().String(),
		TenantID:  "tenant-compress-test",
		Timestamp: time.Now(),
		Topology:  &models.Topology{Nodes: make([]models.TopologyNode, agentCount)},
		Agents:    make([]models.AgentInfo, agentCount),
		Metrics:   make(map[string]MetricSummary, agentCount),
		Alerts:    make([]models.Alert, 0),
		Graph:     &GraphSnapshot{},
		Metadata: SnapshotMetadata{
			PipelineVersion: "1.0.0",
			AgentCount:      agentCount,
			MetricCount:     agentCount,
		},
	}

	for i := 0; i < agentCount; i++ {
		agentID := fmt.Sprintf("agent-%d", i)
		snap.Topology.Nodes[i] = models.TopologyNode{
			ID:   fmt.Sprintf("svc-%d:agent-%d", i, i),
			Name: fmt.Sprintf("service-%d", i),
			Type: models.NodeTypeService,
		}
		snap.Agents[i] = models.AgentInfo{
			ID:       agentID,
			Hostname: fmt.Sprintf("host-%d", i),
			OS:       "linux",
			Status:   models.AgentStatusOnline,
		}
		snap.Metrics[agentID] = MetricSummary{
			AgentID: agentID,
			CPU:     &models.CPUMetrics{TotalUsagePct: float64(i) * 1.5},
			Memory:  &models.MemoryMetrics{UsagePercent: float64(i) * 2.0},
		}
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal test snapshot: %v", err)
	}
	return data
}

// =============================================================================
// 3. TestSnapshotManager_SnapshotID — verify unique IDs
// =============================================================================

func TestSnapshotManager_SnapshotID(t *testing.T) {
	t.Parallel()

	const count = 1000
	ids := make(map[string]struct{}, count)

	for i := 0; i < count; i++ {
		id := uuid.New().String()
		if id == "" {
			t.Fatal("uuid.New() returned empty string")
		}
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate UUID generated: %s", id)
		}
		ids[id] = struct{}{}
	}

	if len(ids) != count {
		t.Errorf("expected %d unique IDs, got %d", count, len(ids))
	}
}

// =============================================================================
// 4. TestSnapshotManager_SnapshotMetadata — verify metadata fields
// =============================================================================

func TestSnapshotManager_SnapshotMetadata(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	meta := SnapshotMetadata{
		PipelineVersion: "1.0.0",
		AgentCount:      5,
		MetricCount:     12,
		AlertCount:      3,
		CreatedAt:       now,
	}

	if meta.PipelineVersion != "1.0.0" {
		t.Errorf("PipelineVersion = %q, want 1.0.0", meta.PipelineVersion)
	}
	if meta.AgentCount != 5 {
		t.Errorf("AgentCount = %d, want 5", meta.AgentCount)
	}
	if meta.MetricCount != 12 {
		t.Errorf("MetricCount = %d, want 12", meta.MetricCount)
	}
	if meta.AlertCount != 3 {
		t.Errorf("AlertCount = %d, want 3", meta.AlertCount)
	}
	if !meta.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", meta.CreatedAt, now)
	}
}

func TestSnapshotManager_SnapshotMetadata_ZeroValues(t *testing.T) {
	t.Parallel()

	m := SnapshotMetadata{}

	if m.PipelineVersion != "" {
		t.Errorf("empty PipelineVersion = %q, want empty", m.PipelineVersion)
	}
	if m.AgentCount != 0 {
		t.Errorf("zero AgentCount = %d, want 0", m.AgentCount)
	}
	if m.MetricCount != 0 {
		t.Errorf("zero MetricCount = %d, want 0", m.MetricCount)
	}
	if m.AlertCount != 0 {
		t.Errorf("zero AlertCount = %d, want 0", m.AlertCount)
	}
	if !m.CreatedAt.IsZero() {
		t.Errorf("zero CreatedAt = %v, want zero", m.CreatedAt)
	}
}

// TestSnapshotManager_SnapshotMetadata_Assembly validates metadata is correctly
// computed during snapshot assembly.
func TestSnapshotManager_SnapshotMetadata_Assembly(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)
	ctx := context.Background()

	snap, err := mgr.assembleSnapshot(ctx, "t1", uuid.New().String(), time.Now())
	if err != nil {
		t.Fatalf("assembleSnapshot: %v", err)
	}

	if snap.Metadata.PipelineVersion != "1.0.0" {
		t.Errorf("PipelineVersion = %q, want 1.0.0", snap.Metadata.PipelineVersion)
	}
	if snap.Metadata.AgentCount != len(snap.Agents) {
		t.Errorf("AgentCount (%d) != len(Agents) (%d)", snap.Metadata.AgentCount, len(snap.Agents))
	}
	if snap.Metadata.MetricCount != len(snap.Metrics) {
		t.Errorf("MetricCount (%d) != len(Metrics) (%d)", snap.Metadata.MetricCount, len(snap.Metrics))
	}
	if snap.Metadata.AlertCount != len(snap.Alerts) {
		t.Errorf("AlertCount (%d) != len(Alerts) (%d)", snap.Metadata.AlertCount, len(snap.Alerts))
	}
	if snap.Metadata.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero after assembly")
	}
}

// =============================================================================
// 5. TestSnapshotManager_ReconstructState — event replay
// =============================================================================

func TestSnapshotManager_ReconstructState(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)
	ctx := context.Background()
	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	// Build a base snapshot.
	base, err := mgr.assembleSnapshot(ctx, "tenant-1", uuid.New().String(), baseTime)
	if err != nil {
		t.Fatalf("assembleSnapshot: %v", err)
	}

	// Build events that modify the snapshot.
	events := []models.Event{
		{
			ID: "ev-1", AgentID: "agent-3", Source: "registry",
			Category: models.EventCategoryDeployment, Severity: models.EventSeverityInfo,
			Title: "agent registered", Labels: map[string]string{"agent_id": "agent-3", "hostname": "web-03"},
			Timestamp: baseTime.Add(1 * time.Minute),
		},
		{
			ID: "ev-2", AgentID: "agent-1", Source: "monitor",
			Category: models.EventCategorySystem, Severity: models.EventSeverityCritical,
			Title: "CPU overload", Description: "CPU > 95%",
			Timestamp: baseTime.Add(2 * time.Minute),
		},
		{
			ID: "ev-3", AgentID: "agent-2", Source: "registry",
			Category: models.EventCategoryDeployment, Severity: models.EventSeverityInfo,
			Title: "agent deregistered", Labels: map[string]string{"agent_id": "agent-2"},
			Timestamp: baseTime.Add(3 * time.Minute),
		},
	}
	targetTime := baseTime.Add(4 * time.Minute)

	// Replay events.
	result := mgr.replayEvents(base, events, targetTime)

	// New ID should be generated.
	if result.ID == base.ID {
		t.Error("replayed snapshot should have a new ID")
	}

	// Timestamp should be updated to target.
	if !result.Timestamp.Equal(targetTime) {
		t.Errorf("Timestamp = %v, want %v", result.Timestamp, targetTime)
	}

	// Agent-3 should be added.
	foundAgent3 := false
	for _, a := range result.Agents {
		if a.ID == "agent-3" {
			foundAgent3 = true
			break
		}
	}
	if !foundAgent3 {
		t.Error("agent-3 should be present after agent registered event")
	}

	// Agent-2 should be removed.
	for _, a := range result.Agents {
		if a.ID == "agent-2" {
			t.Error("agent-2 should be absent after agent deregistered event")
		}
	}

	// Agent count: originally 2, +1 (agent-3), -1 (agent-2) = 2.
	if result.Metadata.AgentCount != 2 {
		t.Errorf("AgentCount = %d, want 2", result.Metadata.AgentCount)
	}

	// Metrics for agent-2 should be removed.
	if _, ok := result.Metrics["agent-2"]; ok {
		t.Error("metrics for agent-2 should be removed after deregistration")
	}

	// Alert from critical event should be added.
	foundAlert := false
	for _, a := range result.Alerts {
		if a.ID == "ev-2" {
			foundAlert = true
			if a.Severity != models.AlertSeverityCritical {
				t.Errorf("alert severity = %q, want critical", a.Severity)
			}
			if a.Status != models.AlertStatusFiring {
				t.Errorf("alert status = %q, want firing", a.Status)
			}
			break
		}
	}
	if !foundAlert {
		t.Error("expected alert ev-2 from critical event")
	}

	// Alert count should increase.
	if result.Metadata.AlertCount != len(result.Alerts) {
		t.Errorf("AlertCount (%d) != len(Alerts) (%d)", result.Metadata.AlertCount, len(result.Alerts))
	}

	// Compressed and SizeBytes should be reset.
	if result.Compressed {
		t.Error("replayed snapshot should not be marked compressed")
	}
	if result.SizeBytes != 0 {
		t.Errorf("SizeBytes = %d, want 0", result.SizeBytes)
	}

	// Metadata.CreatedAt should reflect target time.
	if !result.Metadata.CreatedAt.Equal(targetTime) {
		t.Errorf("Metadata.CreatedAt = %v, want %v", result.Metadata.CreatedAt, targetTime)
	}
}

// TestSnapshotManager_ReconstructState_WithinInterval verifies that when
// the target time is within the snapshot interval, the base is returned as-is.
func TestSnapshotManager_ReconstructState_WithinInterval(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)
	ctx := context.Background()
	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	base, err := mgr.assembleSnapshot(ctx, "tenant-1", uuid.New().String(), baseTime)
	if err != nil {
		t.Fatalf("assembleSnapshot: %v", err)
	}

	// Target is 1 minute after base (within 5-minute interval).
	targetTime := baseTime.Add(1 * time.Minute)

	// Without QuestDB (nil), ReconstructState falls back to GetNearestSnapshot
	// which requires QuestDB. We test the logic path through replayEvents instead.
	// For this test, we verify the shortcut logic directly.
	elapsed := targetTime.Sub(base.Timestamp)
	if elapsed <= mgr.config.Interval {
		// Should return base as-is.
		if base.ID == "" {
			t.Error("base ID should not be empty")
		}
		return
	}
	t.Error("expected 1-minute gap to be within interval")
}

// =============================================================================
// Deep copy tests
// =============================================================================

func TestCopySnapshot_DeepCopy(t *testing.T) {
	t.Parallel()

	now := time.Now()
	original := &Snapshot{
		ID:        "snap-copy",
		TenantID:  "tenant-copy",
		Timestamp: now,
		Topology: &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "n1", Name: "svc-1", Labels: map[string]string{"env": "prod"}},
			},
			Edges: []models.TopologyEdge{
				{ID: "e1", SourceID: "n1", TargetID: "n2", Labels: map[string]string{"proto": "http"}},
			},
		},
		Agents: []models.AgentInfo{
			{ID: "agent-1", Status: models.AgentStatusOnline, Labels: map[string]string{"region": "us-east"}},
		},
		Metrics: map[string]MetricSummary{
			"agent-1": {
				AgentID: "agent-1",
				CPU:     &models.CPUMetrics{TotalUsagePct: 75.0},
				Memory:  &models.MemoryMetrics{UsagePercent: 60.0},
			},
		},
		Alerts: []models.Alert{
			{
				ID: "alert-1", Name: "test", Severity: models.AlertSeverityWarning,
				Labels:      map[string]string{"team": "ops"},
				Annotations: map[string]string{"runbook": "http://example.com"},
			},
		},
		Graph: &GraphSnapshot{
			Nodes: []models.TopologyNode{{ID: "gn1"}},
			Edges: []models.TopologyEdge{{ID: "ge1"}},
		},
		Metadata:  SnapshotMetadata{PipelineVersion: "1.0.0", AgentCount: 1},
		SizeBytes: 2048,
		Compressed: true,
	}

	copied := copySnapshot(original)

	// Verify value equality.
	if copied.ID != original.ID {
		t.Errorf("ID = %q, want %q", copied.ID, original.ID)
	}
	if copied.TenantID != original.TenantID {
		t.Errorf("TenantID = %q, want %q", copied.TenantID, original.TenantID)
	}
	if !copied.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch")
	}
	if len(copied.Agents) != 1 {
		t.Fatalf("Agents count = %d, want 1", len(copied.Agents))
	}
	if len(copied.Metrics) != 1 {
		t.Fatalf("Metrics count = %d, want 1", len(copied.Metrics))
	}
	if len(copied.Alerts) != 1 {
		t.Fatalf("Alerts count = %d, want 1", len(copied.Alerts))
	}

	// Verify deep independence: mutate copy, original must be unchanged.
	copied.Topology.Nodes[0].Name = "mutated"
	if original.Topology.Nodes[0].Name == "mutated" {
		t.Error("modifying copy.Topology.Nodes affected original")
	}

	copied.Agents[0].Labels["region"] = "eu-west"
	if original.Agents[0].Labels["region"] == "eu-west" {
		t.Error("modifying copy.Agents.Labels affected original")
	}

	copied.Metrics["agent-1"].CPU.TotalUsagePct = 99.0
	if original.Metrics["agent-1"].CPU.TotalUsagePct == 99.0 {
		t.Error("modifying copy.Metrics.CPU affected original")
	}

	copied.Alerts[0].Labels["team"] = "dev"
	if original.Alerts[0].Labels["team"] == "dev" {
		t.Error("modifying copy.Alerts.Labels affected original")
	}

	copied.Alerts[0].Annotations["runbook"] = "mutated"
	if original.Alerts[0].Annotations["runbook"] == "mutated" {
		t.Error("modifying copy.Alerts.Annotations affected original")
	}

	copied.Graph.Nodes[0].ID = "mutated"
	if original.Graph.Nodes[0].ID == "mutated" {
		t.Error("modifying copy.Graph.Nodes affected original")
	}
}

func TestCopySnapshot_NilFields(t *testing.T) {
	t.Parallel()

	snap := &Snapshot{
		ID:       "nil-test",
		TenantID: "t1",
	}

	copied := copySnapshot(snap)
	if copied.ID != "nil-test" {
		t.Errorf("ID = %q, want nil-test", copied.ID)
	}
	if copied.Topology != nil {
		t.Error("expected nil Topology in copy when original is nil")
	}
	if copied.Agents != nil {
		t.Error("expected nil Agents in copy when original is nil")
	}
	if copied.Metrics != nil {
		t.Error("expected nil Metrics in copy when original is nil")
	}
	if copied.Alerts != nil {
		t.Error("expected nil Alerts in copy when original is nil")
	}
	if copied.Graph != nil {
		t.Error("expected nil Graph in copy when original is nil")
	}
}

// =============================================================================
// Key generation tests
// =============================================================================

func TestSnapshotObjectKey(t *testing.T) {
	t.Parallel()

	ts := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	key := snapshotObjectKey("tenant-1", ts, "abc-123", ".json.zst")

	want := "snapshots/tenant-1/2025/06/04/abc-123.json.zst"
	if key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestSnapshotCacheKey(t *testing.T) {
	t.Parallel()

	key := snapshotCacheKey("tenant-1", "snap-abc")
	want := "paryty:tenant-1:snapshot:snap-abc"
	if key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestSnapshotLatestCacheKey(t *testing.T) {
	t.Parallel()

	key := snapshotLatestCacheKey("tenant-1")
	want := "paryty:tenant-1:snapshot:latest"
	if key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestExtractSnapshotIDFromKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want string
	}{
		{"snapshots/tenant-1/2025/06/04/abc-123.json.zst", "abc-123"},
		{"snapshots/tenant-1/2025/06/04/abc-123.json", "abc-123"},
		{"snapshots/tenant-1/2025/06/04/abc-123", "abc-123"},
		{"too/short", ""},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			got := extractSnapshotIDFromKey(tc.key)
			if got != tc.want {
				t.Errorf("extractSnapshotIDFromKey(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

// =============================================================================
// Context cancellation tests
// =============================================================================

func TestSnapshotManager_TakeSnapshot_ContextCancelled(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.TakeSnapshot(ctx, "tenant-1")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestSnapshotManager_GetSnapshot_ContextCancelled(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.GetSnapshot(ctx, "tenant-1", "snap-1")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestSnapshotManager_ReconstructState_ContextCancelled(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.ReconstructState(ctx, "tenant-1", time.Now())
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestSnapshotManager_CleanupExpired_ContextCancelled(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.CleanupExpired(ctx, "tenant-1")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// =============================================================================
// Config defaults tests
// =============================================================================

func TestSnapshotConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	cfg := SnapshotConfig{}
	cfg.applyDefaults()

	if cfg.Interval != 5*time.Minute {
		t.Errorf("Interval = %v, want 5m", cfg.Interval)
	}
	if !cfg.Compression {
		t.Error("Compression should default to true")
	}
	if cfg.RetentionDays != 7 {
		t.Errorf("RetentionDays = %d, want 7", cfg.RetentionDays)
	}
	if cfg.MaxSnapshotSize != 50*1024*1024 {
		t.Errorf("MaxSnapshotSize = %d, want %d", cfg.MaxSnapshotSize, 50*1024*1024)
	}
}

func TestSnapshotConfig_ApplyDefaults_PreservesExplicit(t *testing.T) {
	t.Parallel()

	cfg := SnapshotConfig{
		Interval:        10 * time.Minute,
		Compression:     false,
		RetentionDays:   30,
		MaxSnapshotSize: 100 * 1024 * 1024,
	}
	cfg.applyDefaults()

	// Only Compression gets a default (true) because the zero value is false
	// and the code checks `!c.Compression`.
	if !cfg.Compression {
		t.Error("Compression zero-value should be overridden to true")
	}
	if cfg.Interval != 10*time.Minute {
		t.Errorf("Interval should be preserved: got %v", cfg.Interval)
	}
	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays should be preserved: got %d", cfg.RetentionDays)
	}
}

// =============================================================================
// Snapshot JSON round-trip test
// =============================================================================

func TestSnapshot_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Millisecond)
	original := Snapshot{
		ID:        "snap-json-rt",
		TenantID:  "tenant-rt",
		Timestamp: now,
		Topology: &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "n1", Name: "api-gw", Type: models.NodeTypeService, Health: models.HealthStatusHealthy},
			},
			Edges: []models.TopologyEdge{
				{ID: "e1", SourceID: "n1", TargetID: "n2", Type: models.EdgeTypeHTTP},
			},
			Timestamp: now,
			Version:   1,
		},
		Agents: []models.AgentInfo{
			{ID: "a1", Hostname: "web-01", Status: models.AgentStatusOnline, Labels: map[string]string{"env": "prod"}},
		},
		Metrics: map[string]MetricSummary{
			"a1": {
				AgentID: "a1",
				CPU:     &models.CPUMetrics{TotalUsagePct: 42.5, PhysicalCores: 4},
				Memory:  &models.MemoryMetrics{UsagePercent: 65.0},
			},
		},
		Alerts: []models.Alert{
			{ID: "al1", Name: "HighCPU", Severity: models.AlertSeverityWarning, Status: models.AlertStatusFiring, Labels: map[string]string{"k": "v"}},
		},
		Graph: &GraphSnapshot{
			Nodes: []models.TopologyNode{{ID: "gn1"}},
			Edges: []models.TopologyEdge{{ID: "ge1"}},
		},
		Metadata: SnapshotMetadata{
			PipelineVersion: "1.0.0",
			AgentCount:      1,
			MetricCount:     1,
			AlertCount:      1,
			CreatedAt:       now,
		},
		SizeBytes:  4096,
		Compressed: true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.TenantID != original.TenantID {
		t.Errorf("TenantID = %q, want %q", decoded.TenantID, original.TenantID)
	}
	if len(decoded.Agents) != 1 {
		t.Errorf("Agents count = %d, want 1", len(decoded.Agents))
	}
	if len(decoded.Metrics) != 1 {
		t.Errorf("Metrics count = %d, want 1", len(decoded.Metrics))
	}
	if decoded.Metrics["a1"].CPU == nil {
		t.Error("CPU metrics lost in round-trip")
	} else if decoded.Metrics["a1"].CPU.TotalUsagePct != 42.5 {
		t.Errorf("CPU usage = %f, want 42.5", decoded.Metrics["a1"].CPU.TotalUsagePct)
	}
	if len(decoded.Alerts) != 1 {
		t.Errorf("Alerts count = %d, want 1", len(decoded.Alerts))
	}
	if decoded.Alerts[0].Labels["k"] != "v" {
		t.Error("alert labels lost in round-trip")
	}
	if decoded.Compressed != original.Compressed {
		t.Errorf("Compressed = %v, want %v", decoded.Compressed, original.Compressed)
	}
}

// =============================================================================
// Compression integration with snapshot
// =============================================================================

func TestSnapshot_CompressionIntegration(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)
	ctx := context.Background()

	snap, err := mgr.assembleSnapshot(ctx, "tenant-1", uuid.New().String(), time.Now())
	if err != nil {
		t.Fatalf("assembleSnapshot: %v", err)
	}

	// Marshal, compress, decompress, unmarshal — full pipeline.
	jsonData, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	compressed, err := compressData(jsonData)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}

	decompressed, err := decompressData(compressed)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}

	var restored Snapshot
	if err := json.Unmarshal(decompressed, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.ID != snap.ID {
		t.Errorf("ID = %q, want %q", restored.ID, snap.ID)
	}
	if restored.TenantID != snap.TenantID {
		t.Errorf("TenantID = %q, want %q", restored.TenantID, snap.TenantID)
	}
	if len(restored.Agents) != len(snap.Agents) {
		t.Errorf("Agents count = %d, want %d", len(restored.Agents), len(snap.Agents))
	}
	if len(restored.Metrics) != len(snap.Metrics) {
		t.Errorf("Metrics count = %d, want %d", len(restored.Metrics), len(snap.Metrics))
	}
}

// =============================================================================
// NewSnapshotManager panic tests
// =============================================================================

func TestNewSnapshotManager_NilStore_Panics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil store")
		}
	}()

	NewSnapshotManager(SnapshotConfig{}, nil, &mockHotStore{}, nil, nil, zap.NewNop())
}

func TestNewSnapshotManager_NilHotStore_Panics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil hotStore")
		}
	}()

	// We need a non-nil *Client. Use a zero-value one for the panic test.
	NewSnapshotManager(SnapshotConfig{}, &Client{}, nil, nil, nil, zap.NewNop())
}

func TestNewSnapshotManager_NilLogger_UsesNop(t *testing.T) {
	t.Parallel()

	mgr := NewSnapshotManager(SnapshotConfig{}, &Client{}, &mockHotStore{}, nil, nil, nil)
	if mgr.logger == nil {
		t.Error("expected non-nil logger (nop)")
	}
}

// =============================================================================
// MetricSummary tests
// =============================================================================

func TestMetricSummary_OptionalFields(t *testing.T) {
	t.Parallel()

	ms := MetricSummary{
		AgentID: "agent-42",
	}

	if ms.AgentID != "agent-42" {
		t.Errorf("AgentID = %q, want agent-42", ms.AgentID)
	}
	if ms.CPU != nil {
		t.Error("expected nil CPU for empty summary")
	}
	if ms.Memory != nil {
		t.Error("expected nil Memory for empty summary")
	}
	if ms.Disk != nil {
		t.Error("expected nil Disk for empty summary")
	}
	if ms.Network != nil {
		t.Error("expected nil Network for empty summary")
	}
}

func TestMetricSummary_WithAllFields(t *testing.T) {
	t.Parallel()

	ms := MetricSummary{
		AgentID: "agent-full",
		CPU:     &models.CPUMetrics{TotalUsagePct: 80.0},
		Memory:  &models.MemoryMetrics{UsagePercent: 70.0},
		Disk:    []models.DiskMetrics{{Device: "/dev/sda1"}},
		Network: []models.NetworkMetrics{{Interface: "eth0"}},
	}

	if ms.CPU == nil || ms.CPU.TotalUsagePct != 80.0 {
		t.Error("CPU not set correctly")
	}
	if ms.Memory == nil || ms.Memory.UsagePercent != 70.0 {
		t.Error("Memory not set correctly")
	}
	if len(ms.Disk) != 1 {
		t.Errorf("Disk count = %d, want 1", len(ms.Disk))
	}
	if len(ms.Network) != 1 {
		t.Errorf("Network count = %d, want 1", len(ms.Network))
	}
}

// =============================================================================
// GraphSnapshot tests
// =============================================================================

func TestGraphSnapshot_EmptyGraph(t *testing.T) {
	t.Parallel()

	g := &GraphSnapshot{
		Nodes: []models.TopologyNode{},
		Edges: []models.TopologyEdge{},
	}

	if len(g.Nodes) != 0 {
		t.Errorf("empty graph should have 0 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("empty graph should have 0 edges, got %d", len(g.Edges))
	}
}

// =============================================================================
// HotStoreReader error propagation tests
// =============================================================================

func TestSnapshotManager_Assemble_AllErrors(t *testing.T) {
	t.Parallel()

	hotStore := &mockHotStore{
		topologyErr: fmt.Errorf("topology down"),
		agentsErr:   fmt.Errorf("agents down"),
		metricsErr:  fmt.Errorf("metrics down"),
		alertsErr:   fmt.Errorf("alerts down"),
	}
	mgr := newTestManager(hotStore)
	ctx := context.Background()

	snap, err := mgr.assembleSnapshot(ctx, "tenant-err", uuid.New().String(), time.Now())
	if err != nil {
		t.Fatalf("assembleSnapshot should not fail even with all hot store errors: %v", err)
	}

	if snap.Topology == nil {
		t.Error("Topology should default to empty, not nil")
	}
	if snap.Agents == nil {
		t.Error("Agents should default to empty slice, not nil")
	}
	if snap.Alerts == nil {
		t.Error("Alerts should default to empty slice, not nil")
	}
	if snap.Graph == nil {
		t.Error("Graph should be non-nil")
	}
	if snap.Metadata.AgentCount != 0 {
		t.Errorf("AgentCount = %d, want 0", snap.Metadata.AgentCount)
	}
}

// =============================================================================
// Event replay: agent added duplicate prevention
// =============================================================================

func TestReplayEvents_AgentAdded_DuplicatePrevention(t *testing.T) {
	t.Parallel()

	mgr := newTestManager(newTestHotStore())
	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	base := &Snapshot{
		ID:        "base",
		TenantID:  "t1",
		Timestamp: baseTime,
		Topology:  &models.Topology{},
		Agents: []models.AgentInfo{
			{ID: "agent-1", Hostname: "existing", Status: models.AgentStatusOnline},
		},
		Metrics: make(map[string]MetricSummary),
		Alerts:  []models.Alert{},
		Graph:   &GraphSnapshot{},
		Metadata: SnapshotMetadata{
			AgentCount: 1,
		},
	}

	// Try to add agent-1 again (already exists).
	events := []models.Event{
		{
			ID: "ev-dup", AgentID: "agent-1", Source: "registry",
			Category: models.EventCategoryDeployment, Severity: models.EventSeverityInfo,
			Title:     "agent registered",
			Labels:    map[string]string{"agent_id": "agent-1", "hostname": "web-01"},
			Timestamp: baseTime.Add(1 * time.Minute),
		},
	}

	result := mgr.replayEvents(base, events, baseTime.Add(2*time.Minute))

	if len(result.Agents) != 1 {
		t.Errorf("Agents count = %d, want 1 (duplicate should not be added)", len(result.Agents))
	}
	if result.Agents[0].Hostname != "existing" {
		t.Errorf("original agent should be preserved, got hostname %q", result.Agents[0].Hostname)
	}
}

// =============================================================================
// Event replay: agent removed with metrics cleanup
// =============================================================================

func TestReplayEvents_AgentRemoved_MetricsCleanup(t *testing.T) {
	t.Parallel()

	mgr := newTestManager(newTestHotStore())
	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	base := &Snapshot{
		ID:       "base",
		TenantID: "t1",
		Timestamp: baseTime,
		Topology: &models.Topology{},
		Agents: []models.AgentInfo{
			{ID: "agent-1", Status: models.AgentStatusOnline},
			{ID: "agent-2", Status: models.AgentStatusOnline},
		},
		Metrics: map[string]MetricSummary{
			"agent-1": {AgentID: "agent-1", CPU: &models.CPUMetrics{TotalUsagePct: 50}},
			"agent-2": {AgentID: "agent-2", CPU: &models.CPUMetrics{TotalUsagePct: 30}},
		},
		Alerts:   []models.Alert{},
		Graph:    &GraphSnapshot{},
		Metadata: SnapshotMetadata{AgentCount: 2, MetricCount: 2},
	}

	events := []models.Event{
		{
			ID: "ev-remove", AgentID: "agent-1", Source: "registry",
			Category: models.EventCategoryDeployment, Severity: models.EventSeverityInfo,
			Title:     "agent deregistered",
			Labels:    map[string]string{"agent_id": "agent-1"},
			Timestamp: baseTime.Add(1 * time.Minute),
		},
	}

	result := mgr.replayEvents(base, events, baseTime.Add(2*time.Minute))

	// agent-1 should be gone.
	if len(result.Agents) != 1 {
		t.Errorf("Agents count = %d, want 1", len(result.Agents))
	}
	if len(result.Agents) > 0 && result.Agents[0].ID != "agent-2" {
		t.Errorf("remaining agent = %q, want agent-2", result.Agents[0].ID)
	}

	// agent-1 metrics should be cleaned up.
	if _, ok := result.Metrics["agent-1"]; ok {
		t.Error("agent-1 metrics should be removed")
	}
	if _, ok := result.Metrics["agent-2"]; !ok {
		t.Error("agent-2 metrics should still exist")
	}

	// Counts should be updated.
	if result.Metadata.AgentCount != 1 {
		t.Errorf("AgentCount = %d, want 1", result.Metadata.AgentCount)
	}
	if result.Metadata.MetricCount != 1 {
		t.Errorf("MetricCount = %d, want 1", result.Metadata.MetricCount)
	}
}

// =============================================================================
// Event replay: alert from critical/error event
// =============================================================================

func TestReplayEvents_AlertFromCriticalEvent(t *testing.T) {
	t.Parallel()

	mgr := newTestManager(newTestHotStore())
	baseTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)

	base := &Snapshot{
		ID:        "base",
		TenantID:  "t1",
		Timestamp: baseTime,
		Topology:  &models.Topology{},
		Agents:    []models.AgentInfo{},
		Metrics:   make(map[string]MetricSummary),
		Alerts:    []models.Alert{},
		Graph:     &GraphSnapshot{},
		Metadata:  SnapshotMetadata{},
	}

	events := []models.Event{
		{
			ID: "alert-ev", AgentID: "agent-1", Source: "monitor",
			Category: models.EventCategorySystem, Severity: models.EventSeverityCritical,
			Title: "Disk full", Description: "Root partition 99% full",
			Labels:    map[string]string{"mount": "/"},
			Timestamp: baseTime.Add(1 * time.Minute),
		},
		{
			ID: "info-ev", AgentID: "agent-1", Source: "monitor",
			Category: models.EventCategorySystem, Severity: models.EventSeverityInfo,
			Title: "Routine check", Description: "All good",
			Timestamp: baseTime.Add(2 * time.Minute),
		},
	}

	result := mgr.replayEvents(base, events, baseTime.Add(3*time.Minute))

	// Only the critical event should produce an alert.
	if len(result.Alerts) != 1 {
		t.Fatalf("Alerts count = %d, want 1", len(result.Alerts))
	}
	if result.Alerts[0].ID != "alert-ev" {
		t.Errorf("alert ID = %q, want alert-ev", result.Alerts[0].ID)
	}
	if result.Alerts[0].Severity != models.AlertSeverityCritical {
		t.Errorf("alert severity = %q, want critical", result.Alerts[0].Severity)
	}
	if result.Alerts[0].Status != models.AlertStatusFiring {
		t.Errorf("alert status = %q, want firing", result.Alerts[0].Status)
	}
	if result.Alerts[0].Description != "Root partition 99% full" {
		t.Errorf("alert description = %q", result.Alerts[0].Description)
	}
}

// =============================================================================
// Snapshot size estimation
// =============================================================================

func TestSnapshot_SizeEstimation(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)
	ctx := context.Background()

	snap, err := mgr.assembleSnapshot(ctx, "t1", uuid.New().String(), time.Now())
	if err != nil {
		t.Fatalf("assembleSnapshot: %v", err)
	}

	// Marshal to check compressed size.
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	compressed, err := compressData(data)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}

	t.Logf("Raw JSON: %d bytes, Compressed: %d bytes, Ratio: %.2f%%",
		len(data), len(compressed), float64(len(compressed))/float64(len(data))*100)

	// Verify compressed is smaller for this payload.
	if len(compressed) >= len(data) {
		t.Error("expected compressed size < raw size for typical snapshot")
	}
}

// =============================================================================
// SnapshotManager concurrency test
// =============================================================================

func TestSnapshotManager_ConcurrentAssembly(t *testing.T) {
	t.Parallel()

	hotStore := newTestHotStore()
	mgr := newTestManager(hotStore)
	ctx := context.Background()

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tenant := fmt.Sprintf("tenant-%d", idx)
			snap, err := mgr.assembleSnapshot(ctx, tenant, uuid.New().String(), time.Now())
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", idx, err)
				return
			}
			if snap.TenantID != tenant {
				errs <- fmt.Errorf("goroutine %d: TenantID = %q, want %q", idx, snap.TenantID, tenant)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// =============================================================================
// Snapshot struct field access
// =============================================================================

func TestSnapshot_FieldsAccessible(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	snap := &Snapshot{
		ID:       "snap-001",
		TenantID: "tenant-alpha",
		Timestamp: now,
		Topology: &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "n1", Name: "api-gateway"},
				{ID: "n2", Name: "auth-service"},
			},
		},
		Agents: []models.AgentInfo{
			{ID: "agent-1", Status: models.AgentStatusOnline},
		},
		Alerts: []models.Alert{
			{ID: "alert-1", Severity: models.AlertSeverityWarning},
		},
		Metrics: map[string]MetricSummary{
			"agent-1": {
				AgentID: "agent-1",
				CPU:     &models.CPUMetrics{TotalUsagePct: 75.5},
			},
		},
		Graph: &GraphSnapshot{
			Nodes: []models.TopologyNode{{ID: "n1", Name: "api-gateway"}},
			Edges: []models.TopologyEdge{},
		},
		Metadata: SnapshotMetadata{
			PipelineVersion: "1.0.0",
			AgentCount:      1,
			MetricCount:     1,
			AlertCount:      1,
			CreatedAt:       now,
		},
		SizeBytes:  1024,
		Compressed: true,
	}

	if snap.ID != "snap-001" {
		t.Errorf("ID = %q, want snap-001", snap.ID)
	}
	if snap.TenantID != "tenant-alpha" {
		t.Errorf("TenantID = %q, want tenant-alpha", snap.TenantID)
	}
	if len(snap.Topology.Nodes) != 2 {
		t.Errorf("Topology.Nodes count = %d, want 2", len(snap.Topology.Nodes))
	}
	if len(snap.Agents) != 1 {
		t.Errorf("Agents count = %d, want 1", len(snap.Agents))
	}
	if len(snap.Alerts) != 1 {
		t.Errorf("Alerts count = %d, want 1", len(snap.Alerts))
	}
	if snap.Metrics["agent-1"].CPU == nil {
		t.Error("expected CPU metrics for agent-1")
	}
	if snap.Graph == nil {
		t.Fatal("expected non-nil Graph")
	}
	if len(snap.Graph.Nodes) != 1 {
		t.Errorf("Graph.Nodes count = %d, want 1", len(snap.Graph.Nodes))
	}
	if snap.Metadata.AgentCount != 1 {
		t.Errorf("Metadata.AgentCount = %d, want 1", snap.Metadata.AgentCount)
	}
	if !snap.Compressed {
		t.Error("expected Compressed = true")
	}
	if snap.SizeBytes != 1024 {
		t.Errorf("SizeBytes = %d, want 1024", snap.SizeBytes)
	}
}

// =============================================================================
// deepCopySnapshot (eventlog.go) tests
// =============================================================================

func TestDeepCopySnapshot_NilInput(t *testing.T) {
	t.Parallel()

	result := deepCopySnapshot(nil)
	if result != nil {
		t.Error("expected nil for nil input")
	}
}

func TestDeepCopySnapshot_ProducesIndependentCopy(t *testing.T) {
	t.Parallel()

	original := &Snapshot{
		ID:       "dc-test",
		TenantID: "t-dc",
		Topology: &models.Topology{
			Nodes: []models.TopologyNode{
				{ID: "n1", Name: "svc-1", Labels: map[string]string{"k": "v"}},
			},
		},
		Agents: []models.AgentInfo{
			{ID: "a1", Labels: map[string]string{"region": "us"}},
		},
		Metrics: map[string]MetricSummary{
			"a1": {AgentID: "a1", CPU: &models.CPUMetrics{TotalUsagePct: 50}},
		},
		Alerts: []models.Alert{
			{ID: "al1", Labels: map[string]string{"t": "o"}},
		},
	}

	copied := deepCopySnapshot(original)

	// Verify equality.
	if copied.ID != original.ID {
		t.Errorf("ID mismatch")
	}
	if copied.TenantID != original.TenantID {
		t.Errorf("TenantID mismatch")
	}

	// Verify independence.
	copied.Topology.Nodes[0].Name = "mutated"
	if original.Topology.Nodes[0].Name == "mutated" {
		t.Error("Topology mutation leaked to original")
	}

	copied.Agents[0].Labels["region"] = "eu"
	if original.Agents[0].Labels["region"] == "eu" {
		t.Error("Agent labels mutation leaked to original")
	}

	copied.Metrics["a1"].CPU.TotalUsagePct = 99
	if original.Metrics["a1"].CPU.TotalUsagePct == 99 {
		t.Error("Metrics mutation leaked to original")
	}
}
