// Package processing implements the data processing pipeline.
// This file implements the dependency graph for the correlator stage,
// tracking service-to-service relationships with change tracking,
// BFS path finding, and Dragonfly persistence for crash recovery.
package processing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// graphSnapshotKey is the Dragonfly key for persisted graph state.
const graphSnapshotKey = "paryty:pipeline:graph:snapshot"

// NodeType represents the type of a node in the dependency graph.
type NodeType string

const (
	// NodeTypeService represents a service in the dependency graph.
	NodeTypeService NodeType = "service"
	// NodeTypeProcess represents a process in the dependency graph.
	NodeTypeProcess NodeType = "process"
	// NodeTypeEndpoint represents an endpoint in the dependency graph.
	NodeTypeEndpoint NodeType = "endpoint"
)

// GraphNode represents a single node in the dependency graph.
// Nodes typically represent services, processes, or endpoints.
type GraphNode struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         NodeType          `json:"type"`
	AgentID      string            `json:"agent_id"`
	Labels       map[string]string `json:"labels"`
	FirstSeen    time.Time         `json:"first_seen"`
	LastSeen     time.Time         `json:"last_seen"`
	HealthStatus string            `json:"health_status"`
}

// GraphEdge represents a directed edge in the dependency graph,
// describing a relationship from SourceID to TargetID.
type GraphEdge struct {
	SourceID  string            `json:"source_id"`
	TargetID  string            `json:"target_id"`
	Protocol  string            `json:"protocol"`
	Port      uint32            `json:"port"`
	Frequency int64             `json:"frequency"`
	LatencyMs float64           `json:"latency_ms"`
	ErrorRate float64           `json:"error_rate"`
	Labels    map[string]string `json:"labels"`
	FirstSeen time.Time         `json:"first_seen"`
	LastSeen  time.Time         `json:"last_seen"`
}

// GraphChange records a single mutation to the graph for audit and
// real-time change streaming. Supported types: "node_added",
// "node_removed", "edge_added", "edge_removed", "edge_updated".
type GraphChange struct {
	Type      string     `json:"type"`
	Node      *GraphNode `json:"node,omitempty"`
	Edge      *GraphEdge `json:"edge,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
	AgentID   string     `json:"agent_id"`
}

// GraphStats holds aggregated statistics about the current graph state.
type GraphStats struct {
	NodeCount     int     `json:"node_count"`
	EdgeCount     int     `json:"edge_count"`
	AvgFanOut     float64 `json:"avg_fan_out"`
	MaxFanOut     int     `json:"max_fan_out"`
	IsolatedNodes int     `json:"isolated_nodes"`
}

// DependencyGraph is a thread-safe in-memory directed graph for tracking
// service-to-service dependencies. It supports change tracking, BFS shortest
// path finding, stale node eviction, and Dragonfly persistence for crash
// recovery.
//
// All public methods are safe for concurrent use.
type DependencyGraph struct {
	nodes     map[string]*GraphNode
	edges     map[string]*GraphEdge
	adjacency map[string][]string // nodeID -> outgoing targetIDs
	mu        sync.RWMutex
	logger    *slog.Logger
	changes   []GraphChange
	changesMu sync.Mutex
}

// NewDependencyGraph creates a new empty dependency graph.
// If logger is nil, the default slog logger is used.
func NewDependencyGraph(logger *slog.Logger) *DependencyGraph {
	if logger == nil {
		logger = slog.Default()
	}
	return &DependencyGraph{
		nodes:     make(map[string]*GraphNode),
		edges:     make(map[string]*GraphEdge),
		adjacency: make(map[string][]string),
		logger:    logger,
	}
}

// edgeKey returns the canonical map key for a directed edge.
func edgeKey(sourceID, targetID string) string {
	return sourceID + ":" + targetID
}

// recordChange appends a change record under the changes lock.
// Caller may hold g.mu; the two locks are independent (no deadlock).
func (g *DependencyGraph) recordChange(c GraphChange) {
	g.changesMu.Lock()
	defer g.changesMu.Unlock()
	g.changes = append(g.changes, c)
}

// removeNodeInternal removes a node and all edges referencing it.
// Caller MUST hold g.mu write lock.
func (g *DependencyGraph) removeNodeInternal(id string) {
	node := g.nodes[id]
	if node == nil {
		return
	}

	// Remove outgoing edges.
	if targets, ok := g.adjacency[id]; ok {
		for _, targetID := range targets {
			delete(g.edges, edgeKey(id, targetID))
		}
		delete(g.adjacency, id)
	}

	// Remove incoming edges by scanning all adjacency lists.
	for srcID, targets := range g.adjacency {
		filtered := make([]string, 0, len(targets))
		for _, targetID := range targets {
			if targetID == id {
				delete(g.edges, edgeKey(srcID, targetID))
			} else {
				filtered = append(filtered, targetID)
			}
		}
		g.adjacency[srcID] = filtered
	}

	g.recordChange(GraphChange{
		Type:      "node_removed",
		Node:      node,
		Timestamp: time.Now(),
		AgentID:   node.AgentID,
	})

	delete(g.nodes, id)
}

// AddOrUpdateNode adds a new node or updates an existing one.
// Returns true if a new node was created, false if an existing node was updated.
// No change is recorded for updates (only "node_added" is tracked).
func (g *DependencyGraph) AddOrUpdateNode(node *GraphNode) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if existing, ok := g.nodes[node.ID]; ok {
		existing.Name = node.Name
		existing.Type = node.Type
		existing.AgentID = node.AgentID
		existing.Labels = node.Labels
		existing.LastSeen = node.LastSeen
		existing.HealthStatus = node.HealthStatus
		return false
	}

	g.nodes[node.ID] = node
	if _, ok := g.adjacency[node.ID]; !ok {
		g.adjacency[node.ID] = []string{}
	}

	g.recordChange(GraphChange{
		Type:      "node_added",
		Node:      node,
		Timestamp: time.Now(),
		AgentID:   node.AgentID,
	})

	return true
}

// AddOrUpdateEdge adds a new edge or updates an existing one.
// Returns true if a new edge was created, false if an existing edge was updated.
// Records "edge_added" for new edges and "edge_updated" for updates.
func (g *DependencyGraph) AddOrUpdateEdge(edge *GraphEdge) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := edgeKey(edge.SourceID, edge.TargetID)

	if existing, ok := g.edges[key]; ok {
		existing.Protocol = edge.Protocol
		existing.Port = edge.Port
		existing.Frequency = edge.Frequency
		existing.LatencyMs = edge.LatencyMs
		existing.ErrorRate = edge.ErrorRate
		existing.Labels = edge.Labels
		existing.LastSeen = edge.LastSeen

		agentID := ""
		if srcNode, ok := g.nodes[edge.SourceID]; ok {
			agentID = srcNode.AgentID
		}

		g.recordChange(GraphChange{
			Type:      "edge_updated",
			Edge:      existing,
			Timestamp: time.Now(),
			AgentID:   agentID,
		})

		return false
	}

	g.edges[key] = edge
	g.adjacency[edge.SourceID] = append(g.adjacency[edge.SourceID], edge.TargetID)

	agentID := ""
	if srcNode, ok := g.nodes[edge.SourceID]; ok {
		agentID = srcNode.AgentID
	}

	g.recordChange(GraphChange{
		Type:      "edge_added",
		Edge:      edge,
		Timestamp: time.Now(),
		AgentID:   agentID,
	})

	return true
}

// RemoveStaleNodes removes all nodes whose LastSeen is older than maxAge
// from the current time. All edges referencing removed nodes are also deleted.
// Returns the IDs of removed nodes.
func (g *DependencyGraph) RemoveStaleNodes(maxAge time.Duration) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var removed []string

	for id, node := range g.nodes {
		if node.LastSeen.Before(cutoff) {
			removed = append(removed, id)
			g.removeNodeInternal(id)
		}
	}

	return removed
}

// GetNeighbors returns all nodes connected to the given node (both outgoing
// and incoming) along with the connecting edges. Returns empty slices if
// the node has no neighbors or does not exist.
func (g *DependencyGraph) GetNeighbors(nodeID string) ([]*GraphNode, []*GraphEdge) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var neighbors []*GraphNode
	var edges []*GraphEdge

	// Outgoing: nodeID -> target.
	if targets, ok := g.adjacency[nodeID]; ok {
		for _, targetID := range targets {
			if n, exists := g.nodes[targetID]; exists {
				neighbors = append(neighbors, n)
			}
			if e, exists := g.edges[edgeKey(nodeID, targetID)]; exists {
				edges = append(edges, e)
			}
		}
	}

	// Incoming: source -> nodeID.
	for srcID, targets := range g.adjacency {
		for _, targetID := range targets {
			if targetID == nodeID {
				if n, exists := g.nodes[srcID]; exists {
					neighbors = append(neighbors, n)
				}
				if e, exists := g.edges[edgeKey(srcID, targetID)]; exists {
					edges = append(edges, e)
				}
			}
		}
	}

	return neighbors, edges
}

// GetPath finds the shortest path from sourceID to targetID using BFS
// over directed edges. Returns the path as a slice of node IDs, or nil
// if no path exists or either node does not exist.
func (g *DependencyGraph) GetPath(sourceID, targetID string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, ok := g.nodes[sourceID]; !ok {
		return nil
	}
	if _, ok := g.nodes[targetID]; !ok {
		return nil
	}
	if sourceID == targetID {
		return []string{sourceID}
	}

	// BFS over directed outgoing edges.
	queue := []string{sourceID}
	visited := map[string]bool{sourceID: true}
	parent := map[string]string{sourceID: ""}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == targetID {
			// Reconstruct path from target back to source.
			var path []string
			for n := targetID; n != ""; n = parent[n] {
				path = append([]string{n}, path...)
			}
			return path
		}

		for _, neighbor := range g.adjacency[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				parent[neighbor] = current
				queue = append(queue, neighbor)
			}
		}
	}

	return nil
}

// GetChanges returns all recorded graph mutations and clears the internal
// change buffer. Returns nil if no changes are pending.
func (g *DependencyGraph) GetChanges() []GraphChange {
	g.changesMu.Lock()
	defer g.changesMu.Unlock()

	if len(g.changes) == 0 {
		return nil
	}

	changes := g.changes
	g.changes = nil
	return changes
}

// graphSnapshot is the serializable representation of the entire graph
// for Dragonfly persistence.
type graphSnapshot struct {
	Nodes []*GraphNode `json:"nodes"`
	Edges []*GraphEdge `json:"edges"`
}

// Snapshot serializes the entire graph to JSON, compresses with Zstd,
// and stores the result in Dragonfly at the graph snapshot key.
// If dragonfly is nil, this is a no-op.
func (g *DependencyGraph) Snapshot(ctx context.Context, dragonfly DragonflyClient) error {
	if dragonfly == nil {
		return nil
	}

	g.mu.RLock()
	snap := graphSnapshot{
		Nodes: make([]*GraphNode, 0, len(g.nodes)),
		Edges: make([]*GraphEdge, 0, len(g.edges)),
	}
	for _, node := range g.nodes {
		snap.Nodes = append(snap.Nodes, node)
	}
	for _, edge := range g.edges {
		snap.Edges = append(snap.Edges, edge)
	}
	g.mu.RUnlock()

	jsonData, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal graph snapshot: %w", err)
	}

	compressed, err := zstdCompress(jsonData)
	if err != nil {
		return fmt.Errorf("compress graph snapshot: %w", err)
	}

	if err := dragonfly.Set(ctx, graphSnapshotKey, compressed, 0); err != nil {
		return fmt.Errorf("store graph snapshot: %w", err)
	}

	g.logger.Info("graph state snapshotted",
		"nodes", len(snap.Nodes),
		"edges", len(snap.Edges),
		"json_bytes", len(jsonData),
		"compressed_bytes", len(compressed),
	)

	return nil
}

// Restore loads graph state from a previous snapshot in Dragonfly.
// Existing graph data is replaced entirely. Non-fatal if no snapshot
// exists (fresh start).
func (g *DependencyGraph) Restore(ctx context.Context, dragonfly DragonflyClient) error {
	if dragonfly == nil {
		return nil
	}

	compressed, err := dragonfly.Get(ctx, graphSnapshotKey)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			g.logger.Info("no graph snapshot found, starting fresh")
			return nil
		}
		return fmt.Errorf("get graph snapshot: %w", err)
	}

	jsonData, err := zstdDecompress([]byte(compressed))
	if err != nil {
		return fmt.Errorf("decompress graph snapshot: %w", err)
	}

	var snap graphSnapshot
	if err := json.Unmarshal(jsonData, &snap); err != nil {
		return fmt.Errorf("unmarshal graph snapshot: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Replace entire graph with restored state.
	g.nodes = make(map[string]*GraphNode, len(snap.Nodes))
	g.edges = make(map[string]*GraphEdge, len(snap.Edges))
	g.adjacency = make(map[string][]string, len(snap.Nodes))

	for _, node := range snap.Nodes {
		g.nodes[node.ID] = node
		g.adjacency[node.ID] = []string{}
	}

	for _, edge := range snap.Edges {
		key := edgeKey(edge.SourceID, edge.TargetID)
		g.edges[key] = edge
		g.adjacency[edge.SourceID] = append(g.adjacency[edge.SourceID], edge.TargetID)
	}

	g.logger.Info("graph state restored",
		"nodes", len(snap.Nodes),
		"edges", len(snap.Edges),
	)

	return nil
}

// Nodes returns a deep copy of all nodes in the graph.
// The returned slice and its elements are independent of the graph's
// internal state and safe to read without holding locks.
func (g *DependencyGraph) Nodes() []*GraphNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]*GraphNode, 0, len(g.nodes))
	for _, node := range g.nodes {
		cp := *node
		if node.Labels != nil {
			cp.Labels = make(map[string]string, len(node.Labels))
			for k, v := range node.Labels {
				cp.Labels[k] = v
			}
		}
		nodes = append(nodes, &cp)
	}
	return nodes
}

// Edges returns a deep copy of all edges in the graph.
// The returned slice and its elements are independent of the graph's
// internal state and safe to read without holding locks.
func (g *DependencyGraph) Edges() []*GraphEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	edges := make([]*GraphEdge, 0, len(g.edges))
	for _, edge := range g.edges {
		cp := *edge
		if edge.Labels != nil {
			cp.Labels = make(map[string]string, len(edge.Labels))
			for k, v := range edge.Labels {
				cp.Labels[k] = v
			}
		}
		edges = append(edges, &cp)
	}
	return edges
}

// NodeCount returns the number of nodes in the graph.
func (g *DependencyGraph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// EdgeCount returns the number of directed edges in the graph.
func (g *DependencyGraph) EdgeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.edges)
}

// AvgFanOut returns the average number of outgoing edges per node.
// Returns 0 if the graph is empty.
func (g *DependencyGraph) AvgFanOut() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.nodes) == 0 {
		return 0
	}

	total := 0
	for _, targets := range g.adjacency {
		total += len(targets)
	}
	return float64(total) / float64(len(g.nodes))
}

// MaxFanOut returns the maximum number of outgoing edges of any single node.
// Returns 0 if the graph is empty.
func (g *DependencyGraph) MaxFanOut() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	max := 0
	for _, targets := range g.adjacency {
		if len(targets) > max {
			max = len(targets)
		}
	}
	return max
}

// IsolatedNodeCount returns the number of nodes with no incoming or
// outgoing edges.
func (g *DependencyGraph) IsolatedNodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.isolatedNodeCountLocked()
}

// isolatedNodeCountLocked computes isolated node count. Caller must hold g.mu (read or write).
func (g *DependencyGraph) isolatedNodeCountLocked() int {
	hasEdge := make(map[string]bool, len(g.nodes))

	for srcID, targets := range g.adjacency {
		if len(targets) > 0 {
			hasEdge[srcID] = true
			for _, targetID := range targets {
				hasEdge[targetID] = true
			}
		}
	}

	count := 0
	for id := range g.nodes {
		if !hasEdge[id] {
			count++
		}
	}
	return count
}

// Stats returns aggregated graph statistics in a single call.
func (g *DependencyGraph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	stats := GraphStats{
		NodeCount: len(g.nodes),
		EdgeCount: len(g.edges),
	}

	if len(g.nodes) == 0 {
		return stats
	}

	totalOut := 0
	for _, targets := range g.adjacency {
		fanOut := len(targets)
		totalOut += fanOut
		if fanOut > stats.MaxFanOut {
			stats.MaxFanOut = fanOut
		}
	}

	stats.AvgFanOut = float64(totalOut) / float64(len(g.nodes))
	stats.IsolatedNodes = g.isolatedNodeCountLocked()

	return stats
}
