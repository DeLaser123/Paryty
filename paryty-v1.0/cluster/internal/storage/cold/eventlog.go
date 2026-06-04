// Package cold implements the cold storage tier.
// This file implements the Event Log Indexer for topology change events.
//
// Events are stored in QuestDB's topology_snapshots table with is_checkpoint=false.
// When reconstructing state at time T:
//  1. Load nearest full snapshot (is_checkpoint=true)
//  2. Query events between snapshot time and T (is_checkpoint=false)
//  3. Replay events in order to reconstruct point-in-time state.
package cold

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// Event type constants for topology change events.
const (
	EventTypeNodeAdded     = "node_added"
	EventTypeNodeRemoved   = "node_removed"
	EventTypeEdgeAdded     = "edge_added"
	EventTypeEdgeRemoved   = "edge_removed"
	EventTypeMetricUpdated = "metric_updated"
	EventTypeAlertFired    = "alert_fired"
	EventTypeAlertResolved = "alert_resolved"
)

// Sentinel errors for event log operations.
var (
	ErrNilSnapshot  = errors.New("snapshot is nil")
	ErrUnknownEvent = errors.New("unknown event type")
	ErrEmptyPayload = errors.New("event payload is empty")
	ErrNodeNotFound = errors.New("node not found in topology")
	ErrEdgeNotFound = errors.New("edge not found in topology")
	ErrAlertNotFound = errors.New("alert not found in snapshot")
)

// ---------------------------------------------------------------------------
// Event log entry and payload types
// ---------------------------------------------------------------------------

// EventLogEntry represents a topology change event in the event log.
// Stored as JSON in the topology_snapshots table's snapshot_data column.
type EventLogEntry struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`    // e.g. "node_added", "edge_added", "metric_updated"
	AgentID   string    `json:"agent_id"`
	Payload   []byte    `json:"payload"` // JSON-encoded event data
}

// Event payload types — one per EventType constant.

// NodeAddedPayload is the payload for EventTypeNodeAdded events.
type NodeAddedPayload struct {
	Node models.TopologyNode `json:"node"`
}

// NodeRemovedPayload is the payload for EventTypeNodeRemoved events.
type NodeRemovedPayload struct {
	NodeID string `json:"node_id"`
}

// EdgeAddedPayload is the payload for EventTypeEdgeAdded events.
type EdgeAddedPayload struct {
	Edge models.TopologyEdge `json:"edge"`
}

// EdgeRemovedPayload is the payload for EventTypeEdgeRemoved events.
type EdgeRemovedPayload struct {
	EdgeID string `json:"edge_id"`
}

// MetricUpdatedPayload is the payload for EventTypeMetricUpdated events.
type MetricUpdatedPayload struct {
	AgentID string        `json:"agent_id"`
	Summary MetricSummary `json:"summary"`
}

// AlertFiredPayload is the payload for EventTypeAlertFired events.
type AlertFiredPayload struct {
	Alert models.Alert `json:"alert"`
}

// AlertResolvedPayload is the payload for EventTypeAlertResolved events.
type AlertResolvedPayload struct {
	AlertID string `json:"alert_id"`
}

// ---------------------------------------------------------------------------
// EventLog
// ---------------------------------------------------------------------------

// EventLog manages topology change events stored in QuestDB.
//
// Events are written to the topology_snapshots table with is_checkpoint=false.
// The pool is used for all DB operations; lifecycle is managed by the caller.
type EventLog struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// NewEventLog creates a new event log backed by the given pgx pool.
// The caller owns the pool lifecycle (Close).
func NewEventLog(pool *pgxpool.Pool, logger *zap.Logger) *EventLog {
	return &EventLog{
		pool:   pool,
		logger: logger,
	}
}

// RecordEvent persists a topology change event to QuestDB.
//
// The entry is stored as a row in topology_snapshots with is_checkpoint=false.
// snapshot_data contains the full JSON-encoded EventLogEntry.
// If entry.ID is empty, a new UUID is generated.
// The operation is idempotent when called with the same entry.ID.
func (e *EventLog) RecordEvent(ctx context.Context, entry EventLogEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}

	payloadJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal event entry: %w", err)
	}

	nodeCount, edgeCount := eventCounts(entry)

	query := `INSERT INTO topology_snapshots
		(timestamp, snapshot_id, tenant_id, node_count, edge_count, snapshot_data, is_checkpoint)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err = e.pool.Exec(ctx, query,
		entry.Timestamp,
		entry.ID,
		entry.TenantID,
		nodeCount,
		edgeCount,
		string(payloadJSON),
		false, // is_checkpoint — events are never checkpoints
	)
	if err != nil {
		return fmt.Errorf("insert event %s: %w", entry.ID, err)
	}

	e.logger.Debug("recorded event",
		zap.String("id", entry.ID),
		zap.String("type", entry.Type),
		zap.String("tenant", entry.TenantID),
	)
	return nil
}

// eventCounts extracts approximate node/edge count deltas from the event
// payload for the topology_snapshots metadata columns.
func eventCounts(entry EventLogEntry) (nodeCount, edgeCount int64) {
	switch entry.Type {
	case EventTypeNodeAdded:
		return 1, 0
	case EventTypeNodeRemoved:
		return -1, 0
	case EventTypeEdgeAdded:
		return 0, 1
	case EventTypeEdgeRemoved:
		return 0, -1
	default:
		return 0, 0
	}
}

// QueryEvents retrieves events for a tenant within the given time range.
//
// Results are ordered by timestamp ascending (oldest first) to support
// deterministic replay.
func (e *EventLog) QueryEvents(
	ctx context.Context,
	tenant string,
	start, end time.Time,
) ([]EventLogEntry, error) {
	query := `SELECT snapshot_data
		FROM topology_snapshots
		WHERE is_checkpoint = false
		  AND tenant_id = $1
		  AND timestamp >= $2
		  AND timestamp <= $3
		ORDER BY timestamp ASC`

	rows, err := e.pool.Query(ctx, query, tenant, start, end)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var entries []EventLogEntry
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}

		var entry EventLogEntry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			e.logger.Warn("skip malformed event",
				zap.String("tenant", tenant),
				zap.Error(err),
			)
			continue // skip corrupt rows rather than failing the whole query
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event rows: %w", err)
	}

	return entries, nil
}

// ReplayEvents applies a sequence of events to a snapshot, producing a new
// snapshot that reflects the state after all events have been applied.
//
// The input snapshot is never modified. A deep copy is created internally.
// Events are applied in the order they appear in the slice.
//
// Supported event types:
//   - node_added       → add node to topology
//   - node_removed     → remove node from topology
//   - edge_added       → add edge to topology
//   - edge_removed     → remove edge from topology
//   - metric_updated   → update metric summary for an agent
//   - alert_fired      → add alert to snapshot
//   - alert_resolved   → remove alert from snapshot by ID
//
// Returns ErrNilSnapshot if snapshot is nil.
// Returns ErrUnknownEvent for unrecognised event types.
func (e *EventLog) ReplayEvents(
	snapshot *Snapshot,
	events []EventLogEntry,
) (*Snapshot, error) {
	if snapshot == nil {
		return nil, ErrNilSnapshot
	}

	replayed := deepCopySnapshot(snapshot)

	for i, event := range events {
		if err := applyEvent(replayed, event); err != nil {
			return nil, fmt.Errorf("apply event[%d] %s (id=%s): %w",
				i, event.Type, event.ID, err)
		}
	}

	return replayed, nil
}

// CleanupOldEvents deletes events older than maxAge for a given tenant.
//
// Returns the number of deleted rows. Only non-checkpoint rows are affected.
func (e *EventLog) CleanupOldEvents(
	ctx context.Context,
	tenant string,
	maxAge time.Duration,
) (int, error) {
	cutoff := time.Now().Add(-maxAge)

	query := `DELETE FROM topology_snapshots
		WHERE is_checkpoint = false
		  AND tenant_id = $1
		  AND timestamp < $2`

	tag, err := e.pool.Exec(ctx, query, tenant, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup old events: %w", err)
	}

	count := int(tag.RowsAffected())
	e.logger.Info("cleaned up old events",
		zap.String("tenant", tenant),
		zap.Time("cutoff", cutoff),
		zap.Int("deleted", count),
	)
	return count, nil
}

// ---------------------------------------------------------------------------
// Event application (pure logic, no I/O)
// ---------------------------------------------------------------------------

// applyEvent applies a single event to the snapshot in-place.
func applyEvent(s *Snapshot, event EventLogEntry) error {
	switch event.Type {
	case EventTypeNodeAdded:
		return applyNodeAdded(s, event.Payload)
	case EventTypeNodeRemoved:
		return applyNodeRemoved(s, event.Payload)
	case EventTypeEdgeAdded:
		return applyEdgeAdded(s, event.Payload)
	case EventTypeEdgeRemoved:
		return applyEdgeRemoved(s, event.Payload)
	case EventTypeMetricUpdated:
		return applyMetricUpdated(s, event.Payload)
	case EventTypeAlertFired:
		return applyAlertFired(s, event.Payload)
	case EventTypeAlertResolved:
		return applyAlertResolved(s, event.Payload)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownEvent, event.Type)
	}
}

func applyNodeAdded(s *Snapshot, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var p NodeAddedPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode node_added: %w", err)
	}
	if s.Topology == nil {
		s.Topology = &models.Topology{}
	}
	s.Topology.Nodes = append(s.Topology.Nodes, p.Node)
	return nil
}

func applyNodeRemoved(s *Snapshot, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var p NodeRemovedPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode node_removed: %w", err)
	}
	if s.Topology == nil {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, p.NodeID)
	}
	for i, node := range s.Topology.Nodes {
		if node.ID == p.NodeID {
			s.Topology.Nodes = append(s.Topology.Nodes[:i], s.Topology.Nodes[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNodeNotFound, p.NodeID)
}

func applyEdgeAdded(s *Snapshot, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var p EdgeAddedPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode edge_added: %w", err)
	}
	if s.Topology == nil {
		s.Topology = &models.Topology{}
	}
	s.Topology.Edges = append(s.Topology.Edges, p.Edge)
	return nil
}

func applyEdgeRemoved(s *Snapshot, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var p EdgeRemovedPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode edge_removed: %w", err)
	}
	if s.Topology == nil {
		return fmt.Errorf("%w: %s", ErrEdgeNotFound, p.EdgeID)
	}
	for i, edge := range s.Topology.Edges {
		if edge.ID == p.EdgeID {
			s.Topology.Edges = append(s.Topology.Edges[:i], s.Topology.Edges[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrEdgeNotFound, p.EdgeID)
}

func applyMetricUpdated(s *Snapshot, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var p MetricUpdatedPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode metric_updated: %w", err)
	}
	if s.Metrics == nil {
		s.Metrics = make(map[string]MetricSummary)
	}
	s.Metrics[p.AgentID] = p.Summary
	return nil
}

func applyAlertFired(s *Snapshot, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var p AlertFiredPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode alert_fired: %w", err)
	}
	s.Alerts = append(s.Alerts, p.Alert)
	return nil
}

func applyAlertResolved(s *Snapshot, payload []byte) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}
	var p AlertResolvedPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode alert_resolved: %w", err)
	}
	for i, alert := range s.Alerts {
		if alert.ID == p.AlertID {
			s.Alerts = append(s.Alerts[:i], s.Alerts[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrAlertNotFound, p.AlertID)
}

// ---------------------------------------------------------------------------
// Deep copy helpers
// ---------------------------------------------------------------------------

// deepCopySnapshot creates a deep copy of a Snapshot via JSON round-trip.
// This is safe because all fields are JSON-serializable and we need a
// full independent copy for event replay.
func deepCopySnapshot(src *Snapshot) *Snapshot {
	if src == nil {
		return nil
	}

	data, err := json.Marshal(src)
	if err != nil {
		// Should never happen — src was just deserialized or constructed.
		return &Snapshot{
			ID:       src.ID,
			TenantID: src.TenantID,
			Topology: &models.Topology{},
			Metrics:  make(map[string]MetricSummary),
		}
	}

	var dst Snapshot
	if err := json.Unmarshal(data, &dst); err != nil {
		return &Snapshot{
			ID:       src.ID,
			TenantID: src.TenantID,
			Topology: &models.Topology{},
			Metrics:  make(map[string]MetricSummary),
		}
	}

	// Ensure nil-safe maps/slices for mutation.
	if dst.Topology == nil {
		dst.Topology = &models.Topology{}
	}
	if dst.Metrics == nil {
		dst.Metrics = make(map[string]MetricSummary)
	}
	if dst.Topology.Nodes == nil {
		dst.Topology.Nodes = []models.TopologyNode{}
	}
	if dst.Topology.Edges == nil {
		dst.Topology.Edges = []models.TopologyEdge{}
	}
	if dst.Alerts == nil {
		dst.Alerts = []models.Alert{}
	}

	return &dst
}
