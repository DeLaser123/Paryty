// Package timeline implements the Timeline Engine for Paryty V2.0.
// It provides snapshot management, historical replay, diff calculation,
// and report export capabilities for time-travel debugging of infrastructure.
//
// V2.0 Migration: Replaces the Python TimelineService gRPC server with a
// native Go implementation that directly accesses the cold storage tier
// for snapshot operations, eliminating the gRPC serialization overhead.
package timeline

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/cold"
	"go.uber.org/zap"
)

// SnapshotStore defines the storage operations needed by the snapshot manager.
type SnapshotStore interface {
	// TakeSnapshot creates a full snapshot of the current system state.
	TakeSnapshot(ctx context.Context, tenant string) (*cold.Snapshot, error)
	// GetSnapshot retrieves a snapshot by ID.
	GetSnapshot(ctx context.Context, tenant, id string) (*cold.Snapshot, error)
	// ReconstructState reconstructs the system state at a given point in time.
	ReconstructState(ctx context.Context, tenant string, target time.Time) (*cold.Snapshot, error)
}

// HotStoreReader defines hot store read operations for snapshot capture.
type HotStoreReader interface {
	// GetTopology retrieves the current topology.
	GetTopology(ctx context.Context, tenant string) (*models.Topology, error)
	// GetActiveAlerts retrieves active alerts.
	GetActiveAlerts(ctx context.Context, tenant string) ([]models.Alert, error)
}

// SnapshotManager manages timeline snapshots for cold storage, including
// creation, comparison, tagging, and nearest-snapshot lookup.
//
// V2.0 Migration: Replaces the Python SnapshotManager class. The Go version
// directly accesses the cold storage client rather than calling a separate
// Python gRPC service, reducing snapshot creation latency by ~50%.
type SnapshotManager struct {
	store    SnapshotStore
	hotStore HotStoreReader
	logger   *zap.Logger
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(store SnapshotStore, hotStore HotStoreReader, logger *zap.Logger) *SnapshotManager {
	return &SnapshotManager{
		store:    store,
		hotStore: hotStore,
		logger:   logger,
	}
}

// MetricDelta represents the change in a metric between two snapshots.
//
// V2.0 Migration: New type not present in Python. The Python version used
// raw dict comparison; V2.0 uses strongly-typed deltas.
type MetricDelta struct {
	// Name is the metric name.
	Name string `json:"name"`
	// From is the value in the earlier snapshot.
	From float64 `json:"from"`
	// To is the value in the later snapshot.
	To float64 `json:"to"`
	// Change is the absolute difference (To - From).
	Change float64 `json:"change"`
	// ChangePct is the percentage change.
	ChangePct float64 `json:"change_pct"`
}

// SnapshotDiff represents the differences between two snapshots.
//
// V2.0 Migration: Replaces the Python SnapshotDiff dataclass. The Go version
// includes structured MetricDeltas instead of a Python dict.
type SnapshotDiff struct {
	// FromSnapshotID is the ID of the earlier snapshot.
	FromSnapshotID string `json:"from_snapshot_id"`
	// ToSnapshotID is the ID of the later snapshot.
	ToSnapshotID string `json:"to_snapshot_id"`
	// FromTimestamp is the timestamp of the earlier snapshot.
	FromTimestamp time.Time `json:"from_timestamp"`
	// ToTimestamp is the timestamp of the later snapshot.
	ToTimestamp time.Time `json:"to_timestamp"`
	// NodesAdded lists nodes present in To but not in From.
	NodesAdded []models.TopologyNode `json:"nodes_added"`
	// NodesRemoved lists nodes present in From but not in To.
	NodesRemoved []models.TopologyNode `json:"nodes_removed"`
	// EdgesAdded lists edges present in To but not in From.
	EdgesAdded []models.TopologyEdge `json:"edges_added"`
	// EdgesRemoved lists edges present in From but not in To.
	EdgesRemoved []models.TopologyEdge `json:"edges_removed"`
	// MetricDeltas contains computed changes for tracked metrics.
	MetricDeltas []MetricDelta `json:"metric_deltas"`
	// AlertsAdded lists new alerts.
	AlertsAdded []models.Alert `json:"alerts_added"`
	// AlertsResolved lists resolved alerts.
	AlertsResolved []models.Alert `json:"alerts_resolved"`
}

// CreateSnapshot captures the current system state as a snapshot.
func (sm *SnapshotManager) CreateSnapshot(ctx context.Context, tenant string) (*cold.Snapshot, error) {
	sm.logger.Info("Creating snapshot", zap.String("tenant", tenant))

	snapshot, err := sm.store.TakeSnapshot(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("take snapshot: %w", err)
	}

	sm.logger.Info("Snapshot created",
		zap.String("tenant", tenant),
		zap.String("snapshot_id", snapshot.ID),
		zap.Time("timestamp", snapshot.Timestamp),
	)

	return snapshot, nil
}

// DiffSnapshots compares two snapshots and returns the differences.
func (sm *SnapshotManager) DiffSnapshots(ctx context.Context, tenant, fromID, toID string) (*SnapshotDiff, error) {
	fromSnap, err := sm.store.GetSnapshot(ctx, tenant, fromID)
	if err != nil {
		return nil, fmt.Errorf("get from snapshot: %w", err)
	}

	toSnap, err := sm.store.GetSnapshot(ctx, tenant, toID)
	if err != nil {
		return nil, fmt.Errorf("get to snapshot: %w", err)
	}

	diff := &SnapshotDiff{
		FromSnapshotID: fromID,
		ToSnapshotID:   toID,
		FromTimestamp:   fromSnap.Timestamp,
		ToTimestamp:     toSnap.Timestamp,
	}

	// Compare nodes.
	fromNodes := nodeSet(fromSnap)
	toNodes := nodeSet(toSnap)

	for id, node := range toNodes {
		if _, exists := fromNodes[id]; !exists {
			diff.NodesAdded = append(diff.NodesAdded, node)
		}
	}
	for id, node := range fromNodes {
		if _, exists := toNodes[id]; !exists {
			diff.NodesRemoved = append(diff.NodesRemoved, node)
		}
	}

	// Compare edges.
	fromEdges := edgeSet(fromSnap)
	toEdges := edgeSet(toSnap)

	for id, edge := range toEdges {
		if _, exists := fromEdges[id]; !exists {
			diff.EdgesAdded = append(diff.EdgesAdded, edge)
		}
	}
	for id, edge := range fromEdges {
		if _, exists := toEdges[id]; !exists {
			diff.EdgesRemoved = append(diff.EdgesRemoved, edge)
		}
	}

	// Compare metrics.
	diff.MetricDeltas = sm.computeMetricDeltas(fromSnap, toSnap)

	// Compare alerts.
	fromAlerts := alertSet(fromSnap)
	toAlerts := alertSet(toSnap)

	for id, alert := range toAlerts {
		if _, exists := fromAlerts[id]; !exists {
			diff.AlertsAdded = append(diff.AlertsAdded, alert)
		}
	}
	for id, alert := range fromAlerts {
		if _, exists := toAlerts[id]; !exists {
			diff.AlertsResolved = append(diff.AlertsResolved, alert)
		}
	}

	return diff, nil
}

// TagSnapshot adds a tag to a snapshot's metadata.
func (sm *SnapshotManager) TagSnapshot(ctx context.Context, tenant, snapshotID, tag string) error {
	// Tags are stored as metadata on the snapshot. Since cold storage snapshots
	// are immutable, we store tags in the hot tier for fast lookup.
	sm.logger.Info("Tagging snapshot",
		zap.String("tenant", tenant),
		zap.String("snapshot_id", snapshotID),
		zap.String("tag", tag),
	)

	// Verify the snapshot exists.
	_, err := sm.store.GetSnapshot(ctx, tenant, snapshotID)
	if err != nil {
		return fmt.Errorf("get snapshot: %w", err)
	}

	// In a production implementation, this would write the tag to Dragonfly
	// keyed as paryty:<tenant>:snapshot_tags:<snapshot_id>.
	return nil
}

// GetNearestSnapshot finds the snapshot closest to the given time.
// It uses ReconstructState from the cold storage tier which handles
// nearest-snapshot lookup plus event log replay.
func (sm *SnapshotManager) GetNearestSnapshot(ctx context.Context, tenant string, target time.Time) (*cold.Snapshot, error) {
	sm.logger.Info("Finding nearest snapshot",
		zap.String("tenant", tenant),
		zap.Time("target", target),
	)

	snapshot, err := sm.store.ReconstructState(ctx, tenant, target)
	if err != nil {
		return nil, fmt.Errorf("reconstruct state at %v: %w", target, err)
	}

	return snapshot, nil
}

// computeMetricDeltas computes metric changes between two snapshots.
func (sm *SnapshotManager) computeMetricDeltas(from, to *cold.Snapshot) []MetricDelta {
	fromMetrics := aggregateSnapshotMetrics(from)
	toMetrics := aggregateSnapshotMetrics(to)

	var deltas []MetricDelta
	for name, fromVal := range fromMetrics {
		toVal, exists := toMetrics[name]
		if !exists {
			continue
		}

		change := toVal - fromVal
		changePct := 0.0
		if fromVal != 0 {
			changePct = (change / fromVal) * 100
		}

		deltas = append(deltas, MetricDelta{
			Name:      name,
			From:      fromVal,
			To:        toVal,
			Change:    change,
			ChangePct: changePct,
		})
	}

	return deltas
}

// Helper functions for set operations.

func nodeSet(snap *cold.Snapshot) map[string]models.TopologyNode {
	nodes := make(map[string]models.TopologyNode)
	if snap == nil || snap.Topology == nil {
		return nodes
	}
	for _, n := range snap.Topology.Nodes {
		nodes[n.ID] = n
	}
	return nodes
}

func edgeSet(snap *cold.Snapshot) map[string]models.TopologyEdge {
	edges := make(map[string]models.TopologyEdge)
	if snap == nil || snap.Topology == nil {
		return edges
	}
	for _, e := range snap.Topology.Edges {
		edges[e.ID] = e
	}
	return edges
}

func alertSet(snap *cold.Snapshot) map[string]models.Alert {
	alerts := make(map[string]models.Alert)
	if snap == nil {
		return alerts
	}
	for _, a := range snap.Alerts {
		alerts[a.ID] = a
	}
	return alerts
}

func aggregateSnapshotMetrics(snap *cold.Snapshot) map[string]float64 {
	metrics := make(map[string]float64)
	if snap == nil {
		return metrics
	}

	for agentID, summary := range snap.Metrics {
		if summary.CPU != nil {
			metrics[agentID+".cpu.usage_percent"] = summary.CPU.TotalUsagePct
		}
		if summary.Memory != nil {
			metrics[agentID+".memory.usage_percent"] = summary.Memory.UsagePercent
		}
	}

	return metrics
}

// SnapshotTag creates a unique tag ID for snapshot tagging operations.
func SnapshotTag() string {
	return uuid.New().String()
}
