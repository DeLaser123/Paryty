// Package hot implements the hot storage tier using Dragonfly (Redis-compatible).
// This file implements topology state management with optimistic locking using
// WATCH/MULTI/EXEC for safe concurrent topology updates.
package hot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// topologyUpdateMaxRetries is the maximum number of retry attempts
	// when a WATCH/MULTI/EXEC transaction fails due to a concurrent modification.
	topologyUpdateMaxRetries = 10

	// topologyUpdateBaseBackoff is the base duration for exponential backoff
	// between retry attempts. Actual delay = baseBackoff * attemptNumber.
	topologyUpdateBaseBackoff = 10 * time.Millisecond
)

// topologyVersionKey returns the Dragonfly key for the topology version counter.
// Used for optimistic locking alongside the topology data key.
func topologyVersionKey(tenant string) string {
	return fmt.Sprintf("paryty:%s:topology:version", tenant)
}

// TopologyDiff describes a set of incremental changes to apply to the current topology.
// Used by the correlator stage to publish partial updates without replacing the entire graph.
type TopologyDiff struct {
	NodesAdded   []models.TopologyNode `json:"nodes_added"`
	NodesRemoved []string              `json:"nodes_removed"`
	NodesUpdated []models.TopologyNode `json:"nodes_updated"`
	EdgesAdded   []models.TopologyEdge `json:"edges_added"`
	EdgesRemoved []string              `json:"edges_removed"`
	EdgesUpdated []models.TopologyEdge `json:"edges_updated"`
	Timestamp    time.Time             `json:"timestamp"`
	Version      string                `json:"version"`
}

// TopologyOps provides atomic topology state management operations with
// optimistic locking. All mutations use WATCH/MULTI/EXEC to prevent lost
// updates under concurrent access.
type TopologyOps struct {
	client *Client
	logger *zap.Logger
}

// NewTopologyOps creates a new TopologyOps instance.
//
// Parameters:
//   - client: the Dragonfly hot storage client.
//   - logger: structured logger. Must not be nil.
func NewTopologyOps(client *Client, logger *zap.Logger) *TopologyOps {
	return &TopologyOps{
		client: client,
		logger: logger,
	}
}

// UpdateTopology atomically reads, mutates, and writes the topology for a tenant
// using WATCH/MULTI/EXEC optimistic locking.
//
// Flow:
//  1. WATCH the topology key.
//  2. GET current topology (nil → empty topology).
//  3. Call mutate function to produce the new topology.
//  4. MULTI → SET new topology + INCR version → EXEC.
//  5. If EXEC fails (key was modified concurrently), retry with exponential backoff.
//
// The mutate function receives the current topology and must return a new topology.
// If mutate returns an error, the update is aborted immediately (no retry).
//
// Returns an error if all retries are exhausted or if mutate fails.
func (topoOps *TopologyOps) UpdateTopology(
	ctx context.Context,
	tenant string,
	mutate func(current *models.Topology) (*models.Topology, error),
) error {
	topoKey := topologyKey(tenant)
	verKey := topologyVersionKey(tenant)
	ttl := topoOps.client.cfg.TTL

	for attempt := 0; attempt <= topologyUpdateMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := topologyUpdateBaseBackoff * time.Duration(attempt)
			topoOps.logger.Debug("retrying topology update after conflict",
				zap.String("tenant", tenant),
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-ctx.Done():
				return fmt.Errorf("topology update cancelled during backoff: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		// Step 1: WATCH the topology key.
		// This tells Redis/Dragonfly to monitor the key for changes.
		// If the key is modified before EXEC, the transaction will be aborted.
		err := topoOps.client.rdb.Watch(ctx, func(tx *redis.Tx) error {
			// Step 2: GET the current topology.
			current, err := topoOps.getCurrentTopology(ctx, tx, tenant)
			if err != nil {
				return fmt.Errorf("get current topology: %w", err)
			}

			// Step 3: Apply the caller's mutation function.
			updated, err := mutate(current)
			if err != nil {
				return fmt.Errorf("mutate topology: %w", err)
			}

			// Ensure the updated topology has the correct timestamp and version.
			updated.Timestamp = time.Now()
			updated.Version = current.Version + 1

			// Step 4: Serialize the new topology.
			data, err := json.Marshal(updated)
			if err != nil {
				return fmt.Errorf("marshal topology: %w", err)
			}

			// Step 5: Execute MULTI → SET + INCR atomically.
			// All commands inside the pipeline are buffered and executed
			// atomically at Exec time. If any watched key was modified,
			// Exec returns redis.TxFailedErr.
			pipe := tx.TxPipeline()
			pipe.Set(ctx, topoKey, data, ttl)
			pipe.Incr(ctx, verKey)
			pipe.Expire(ctx, verKey, ttl)

			if _, err := pipe.Exec(ctx); err != nil {
				return fmt.Errorf("exec transaction: %w", err)
			}

			return nil
		}, topoKey)

		if err == nil {
			topoOps.logger.Debug("topology updated successfully",
				zap.String("tenant", tenant),
				zap.Int("attempt", attempt),
			)
			return nil
		}

		// If the error is not a transaction conflict, fail immediately.
		// This includes context cancellation, marshal errors, etc.
		if !isRetryableError(err) {
			return fmt.Errorf("update topology for tenant %q: %w", tenant, err)
		}

		topoOps.logger.Warn("topology update conflict, will retry",
			zap.String("tenant", tenant),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
	}

	return fmt.Errorf("topology update failed after %d retries for tenant %q: %w",
		topologyUpdateMaxRetries, tenant, ErrTopologyConflict)
}

// ErrTopologyConflict is returned when all retry attempts are exhausted
// due to concurrent modifications on the topology key.
var ErrTopologyConflict = fmt.Errorf("topology update conflict: max retries exceeded")

// isRetryableError determines whether an error from a WATCH/MULTI/EXEC
// transaction is a conflict that should be retried.
//
// Retryable errors:
//   - Any error wrapping redis.TxFailedErr (transaction aborted due to watched key change).
//   - redis.Nil (key was deleted during the transaction).
//
// Non-retryable errors (returned immediately):
//   - Context cancellation.
//   - Marshal/unmarshal errors.
//   - Network errors.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, redis.TxFailedErr) {
		return true
	}
	if errors.Is(err, redis.Nil) {
		return true
	}
	return false
}

// getCurrentTopology reads the current topology for a tenant within a WATCH
// callback. If the key does not exist, returns an empty topology (not an error).
func (topoOps *TopologyOps) getCurrentTopology(
	ctx context.Context,
	tx *redis.Tx,
	tenant string,
) (*models.Topology, error) {
	data, err := tx.Get(ctx, topologyKey(tenant)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return &models.Topology{
				Nodes:     []models.TopologyNode{},
				Edges:     []models.TopologyEdge{},
				Timestamp: time.Now(),
				Version:   0,
			}, nil
		}
		return nil, fmt.Errorf("get topology: %w", err)
	}

	var topo models.Topology
	if err := json.Unmarshal(data, &topo); err != nil {
		return nil, fmt.Errorf("unmarshal topology: %w", err)
	}
	return &topo, nil
}

// GetTopologyWithVersion retrieves the current topology and its version counter
// for a tenant. This is a non-transactional read (no WATCH).
//
// Returns:
//   - *models.Topology: the current topology, or nil if not found.
//   - string: the version counter value, or "" if not found.
//   - error: any Redis or deserialization error.
//
// Note: This method returns (nil, "", nil) when the key does not exist.
// Callers should check for nil topology to distinguish "not found" from an error.
func (topoOps *TopologyOps) GetTopologyWithVersion(
	ctx context.Context,
	tenant string,
) (*models.Topology, string, error) {
	topoKey := topologyKey(tenant)
	verKey := topologyVersionKey(tenant)

	// Use a pipeline to fetch both keys in a single round trip.
	pipe := topoOps.client.rdb.Pipeline()
	topoCmd := pipe.Get(ctx, topoKey)
	verCmd := pipe.Get(ctx, verKey)

	if _, err := pipe.Exec(ctx); err != nil {
		// If the error is redis.Nil on the topology key, the topology doesn't exist.
		// The version key may or may not exist.
		if topoCmd.Err() == redis.Nil {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("pipeline get topology with version: %w", err)
	}

	// Parse the topology.
	data, err := topoCmd.Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("get topology data: %w", err)
	}

	var topo models.Topology
	if err := json.Unmarshal(data, &topo); err != nil {
		return nil, "", fmt.Errorf("unmarshal topology: %w", err)
	}

	// Parse the version (may not exist for legacy data).
	version, _ := verCmd.Result()
	if verCmd.Err() != nil && verCmd.Err() != redis.Nil {
		topoOps.logger.Debug("version key read failed (non-fatal)",
			zap.String("tenant", tenant),
			zap.Error(verCmd.Err()),
		)
	}

	return &topo, version, nil
}

// ApplyDiff applies a TopologyDiff to the current topology atomically using
// WATCH/MULTI/EXEC. This is a convenience wrapper around UpdateTopology.
//
// The diff is applied in this order:
//  1. Remove nodes (by ID). Also removes edges referencing the removed nodes.
//  2. Remove edges (by ID).
//  3. Update nodes (by ID, match in Nodes slice).
//  4. Update edges (by ID, match in Edges slice).
//  5. Add new nodes.
//  6. Add new edges.
//
// Returns an error if the transaction fails after all retries or if the diff
// is structurally invalid (e.g., references non-existent nodes in edges).
func (topoOps *TopologyOps) ApplyDiff(
	ctx context.Context,
	tenant string,
	diff *TopologyDiff,
) error {
	if diff == nil {
		return fmt.Errorf("apply diff: diff is nil")
	}

	return topoOps.UpdateTopology(ctx, tenant, func(current *models.Topology) (*models.Topology, error) {
		updated, err := applyTopologyDiff(current, diff)
		if err != nil {
			return nil, fmt.Errorf("apply diff: %w", err)
		}
		return updated, nil
	})
}

// applyTopologyDiff applies a TopologyDiff to a Topology, returning the new
// topology. This is a pure function (no I/O) to enable unit testing.
//
// Diff application order:
//  1. Remove nodes (by ID from NodesRemoved). Also removes edges referencing them.
//  2. Remove edges (by ID from EdgesRemoved).
//  3. Update existing nodes (by ID match).
//  4. Update existing edges (by ID match).
//  5. Append new nodes (from NodesAdded).
//  6. Append new edges (from EdgesAdded).
func applyTopologyDiff(current *models.Topology, diff *TopologyDiff) (*models.Topology, error) {
	result := copyTopology(current)

	// Build lookup indices for fast removal.
	removedNodeIDs := make(map[string]struct{}, len(diff.NodesRemoved))
	for _, id := range diff.NodesRemoved {
		removedNodeIDs[id] = struct{}{}
	}

	removedEdgeIDs := make(map[string]struct{}, len(diff.EdgesRemoved))
	for _, id := range diff.EdgesRemoved {
		removedEdgeIDs[id] = struct{}{}
	}

	// Step 1: Remove nodes by ID. Also remove edges that reference removed nodes.
	// This must happen BEFORE edge removal by ID to correctly cascade.
	if len(removedNodeIDs) > 0 {
		// Remove nodes.
		filteredNodes := make([]models.TopologyNode, 0, len(result.Nodes))
		for _, node := range result.Nodes {
			if _, removed := removedNodeIDs[node.ID]; !removed {
				filteredNodes = append(filteredNodes, node)
			}
		}
		result.Nodes = filteredNodes

		// Remove edges referencing removed nodes.
		filteredEdges := make([]models.TopologyEdge, 0, len(result.Edges))
		for _, edge := range result.Edges {
			_, srcRemoved := removedNodeIDs[edge.SourceID]
			_, tgtRemoved := removedNodeIDs[edge.TargetID]
			if !srcRemoved && !tgtRemoved {
				filteredEdges = append(filteredEdges, edge)
			}
		}
		result.Edges = filteredEdges
	}

	// Step 2: Remove edges by ID.
	if len(removedEdgeIDs) > 0 {
		filtered := make([]models.TopologyEdge, 0, len(result.Edges))
		for _, edge := range result.Edges {
			if _, removed := removedEdgeIDs[edge.ID]; !removed {
				filtered = append(filtered, edge)
			}
		}
		result.Edges = filtered
	}

	// Step 3: Update existing nodes by ID.
	for _, updated := range diff.NodesUpdated {
		found := false
		for i := range result.Nodes {
			if result.Nodes[i].ID == updated.ID {
				result.Nodes[i] = updated
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("updated node %q not found in topology", updated.ID)
		}
	}

	// Step 4: Update existing edges by ID.
	for _, updated := range diff.EdgesUpdated {
		found := false
		for i := range result.Edges {
			if result.Edges[i].ID == updated.ID {
				result.Edges[i] = updated
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("updated edge %q not found in topology", updated.ID)
		}
	}

	// Step 5: Add new nodes.
	result.Nodes = append(result.Nodes, diff.NodesAdded...)

	// Step 6: Add new edges.
	result.Edges = append(result.Edges, diff.EdgesAdded...)

	return result, nil
}

// copyTopology creates a deep copy of a Topology, including independent slices
// for Nodes and Edges. The copy is safe to mutate without affecting the original.
func copyTopology(src *models.Topology) *models.Topology {
	dst := &models.Topology{
		Timestamp: src.Timestamp,
		Version:   src.Version,
	}

	if src.Nodes != nil {
		dst.Nodes = make([]models.TopologyNode, len(src.Nodes))
		copy(dst.Nodes, src.Nodes)
	}

	if src.Edges != nil {
		dst.Edges = make([]models.TopologyEdge, len(src.Edges))
		copy(dst.Edges, src.Edges)
	}

	return dst
}
