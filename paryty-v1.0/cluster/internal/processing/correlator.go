// Package processing implements the data processing pipeline.
// This file implements the graph-based correlation engine for the correlator
// stage, mapping processes to services, extracting TCP dependencies, tracking
// topology changes, and maintaining a DependencyGraph for service discovery.
package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// Default correlator configuration values.
const (
	defaultStaleNodeTimeout      = 5 * time.Minute
	defaultGraphSnapshotInterval = 60 * time.Second
	defaultEventBufferSize       = 10000
	defaultCorrelationWindow     = 30 * time.Second
)

// CorrelatorConfig holds tunable parameters for the Correlator.
type CorrelatorConfig struct {
	// StaleNodeTimeout is the duration after which a node with no updates
	// is considered stale and eligible for cleanup. Default: 5m.
	StaleNodeTimeout time.Duration

	// GraphSnapshotInterval is the interval at which the graph is persisted
	// to Dragonfly. Default: 60s.
	GraphSnapshotInterval time.Duration

	// EventBufferSize is the maximum number of network events retained in
	// the ring buffer. Default: 10000.
	EventBufferSize int

	// CorrelationWindow is the time window for correlating events. Default: 30s.
	CorrelationWindow time.Duration
}

// DefaultCorrelatorConfig returns a CorrelatorConfig with production defaults.
func DefaultCorrelatorConfig() CorrelatorConfig {
	return CorrelatorConfig{
		StaleNodeTimeout:      defaultStaleNodeTimeout,
		GraphSnapshotInterval: defaultGraphSnapshotInterval,
		EventBufferSize:       defaultEventBufferSize,
		CorrelationWindow:     defaultCorrelationWindow,
	}
}

// Correlator is a graph-based correlation engine that maps processes to
// services, extracts network dependencies, and tracks topology changes.
//
// It maintains an in-memory DependencyGraph of service-to-service relationships,
// a RingBuffer of recent network events for temporal correlation, and a
// ServiceMap for process-to-service resolution.
//
// Thread-safe. All public methods are safe for concurrent use.
type Correlator struct {
	config     CorrelatorConfig
	graph      *DependencyGraph
	eventBuf   *RingBuffer
	serviceMap *ServiceMap
	logger     *zap.Logger
}

// CorrelationResult contains the output of a single correlation pass.
// It captures the process-to-service mapping, discovered dependencies,
// topology graph changes, and current graph statistics.
type CorrelationResult struct {
	// AgentID is the identifier of the agent that submitted the batch.
	AgentID string `json:"agent_id"`

	// Timestamp is when the correlation was performed.
	Timestamp time.Time `json:"timestamp"`

	// ProcessServices maps "pid:process_name" to resolved service names.
	ProcessServices map[string]string `json:"process_services"`

	// Dependencies lists all service-to-service dependencies discovered
	// in this correlation pass.
	Dependencies []Dependency `json:"dependencies"`

	// TopologyChanges lists graph mutations (node/edge additions, updates,
	// removals) that occurred during this correlation pass.
	TopologyChanges []GraphChange `json:"topology_changes"`

	// GraphStats is a snapshot of aggregated graph statistics.
	GraphStats GraphStats `json:"graph_stats"`
}

// Dependency represents a directed service-to-service dependency discovered
// from TCP/HTTP network events.
type Dependency struct {
	// SourceService is the resolved name of the source service.
	SourceService string `json:"source_service"`

	// TargetService is the resolved name of the target service.
	TargetService string `json:"target_service"`

	// Protocol is the transport protocol (e.g., "tcp", "http").
	Protocol string `json:"protocol"`

	// Port is the destination port number.
	Port uint32 `json:"port"`

	// Frequency is the number of times this dependency was observed.
	Frequency int64 `json:"frequency"`

	// LatencyMs is the average observed latency in milliseconds.
	LatencyMs float64 `json:"latency_ms"`

	// ErrorRate is the fraction of failed requests (0.0 – 1.0).
	ErrorRate float64 `json:"error_rate"`
}

// NewCorrelator creates a new Correlator with the given configuration,
// service map, and logger.
//
// The graph uses a default slog logger internally; all correlator-level
// logging goes through the provided zap logger.
//
// Panics if logger is nil.
func NewCorrelator(config CorrelatorConfig, serviceMap *ServiceMap, logger *zap.Logger) *Correlator {
	if logger == nil {
		panic("correlator: logger must not be nil")
	}
	if serviceMap == nil {
		panic("correlator: serviceMap must not be nil")
	}

	// Apply zero-value defaults.
	if config.StaleNodeTimeout == 0 {
		config.StaleNodeTimeout = defaultStaleNodeTimeout
	}
	if config.GraphSnapshotInterval == 0 {
		config.GraphSnapshotInterval = defaultGraphSnapshotInterval
	}
	if config.EventBufferSize == 0 {
		config.EventBufferSize = defaultEventBufferSize
	}
	if config.CorrelationWindow == 0 {
		config.CorrelationWindow = defaultCorrelationWindow
	}

	return &Correlator{
		config:     config,
		graph:      NewDependencyGraph(nil),
		eventBuf:   NewRingBuffer(config.EventBufferSize),
		serviceMap: serviceMap,
		logger:     logger,
	}
}

// Correlate performs a 7-step correlation pipeline on the given metric batch
// and network events:
//
//  1. Map processes to services using ServiceMap.
//  2. Create or update graph nodes for each discovered service.
//  3. Extract dependencies from TCP events in the network events.
//  4. Create or update graph edges for each dependency.
//  5. Detect topology changes recorded by the graph.
//  6. Correlate batch metrics with graph nodes.
//  7. Compute graph statistics.
//
// Returns a CorrelationResult containing all discoveries, or an error if
// the context is cancelled.
func (c *Correlator) Correlate(ctx context.Context, batch *models.MetricBatch, networkEvents []models.NetworkEvent) (*CorrelationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("correlator: context cancelled: %w", err)
	}

	now := time.Now()
	result := &CorrelationResult{
		AgentID:   batch.AgentID,
		Timestamp: now,
	}

	// ── Step 1: Map processes to services ──────────────────────────────
	processServices := c.mapProcessToServices(batch)
	result.ProcessServices = processServices

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("correlator: context cancelled after step 1: %w", err)
	}

	// ── Step 2: Create/update graph nodes for each service ─────────────
	c.updateGraphNodes(batch, processServices, now)

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("correlator: context cancelled after step 2: %w", err)
	}

	// ── Step 3: Extract dependencies from TCP events ───────────────────
	dependencies := c.extractDependencies(networkEvents, processServices, batch.AgentID, now)

	// ── Step 4: Create/update graph edges for each dependency ──────────
	c.updateGraphEdges(dependencies, now)
	result.Dependencies = dependencies

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("correlator: context cancelled after step 4: %w", err)
	}

	// ── Step 5: Detect topology changes ────────────────────────────────
	changes := c.graph.GetChanges()
	result.TopologyChanges = changes

	// ── Step 6: Correlate metrics with graph nodes ─────────────────────
	c.correlateMetrics(batch, processServices, now)

	// ── Step 7: Compute graph statistics ───────────────────────────────
	result.GraphStats = c.graph.Stats()

	c.logger.Debug("Correlation complete",
		zap.String("agent_id", batch.AgentID),
		zap.Int("process_services", len(processServices)),
		zap.Int("dependencies", len(dependencies)),
		zap.Int("topology_changes", len(changes)),
		zap.Int("graph_nodes", result.GraphStats.NodeCount),
		zap.Int("graph_edges", result.GraphStats.EdgeCount),
	)

	return result, nil
}

// BufferNetworkEvents pushes network events into the ring buffer for
// temporal correlation. When the buffer is full, oldest events are evicted.
func (c *Correlator) BufferNetworkEvents(events []models.NetworkEvent) {
	for _, event := range events {
		c.eventBuf.Push(event)
	}
	c.logger.Debug("Buffered network events",
		zap.Int("count", len(events)),
		zap.Int("buffer_size", c.eventBuf.Len()),
	)
}

// GetBufferedEvents returns a copy of all network events currently in
// the ring buffer, oldest first. The events remain in the buffer.
func (c *Correlator) GetBufferedEvents() []models.NetworkEvent {
	raw := c.eventBuf.GetAll()
	events := make([]models.NetworkEvent, 0, len(raw))
	for _, item := range raw {
		if ne, ok := item.(models.NetworkEvent); ok {
			events = append(events, ne)
		}
	}
	return events
}

// DrainBufferedEvents returns all buffered network events and clears the
// buffer atomically. Unlike GetBufferedEvents, drained events are removed
// and will not appear in subsequent calls. Returns nil if the buffer is empty.
func (c *Correlator) DrainBufferedEvents() []models.NetworkEvent {
	raw := c.eventBuf.DrainAll()
	events := make([]models.NetworkEvent, 0, len(raw))
	for _, item := range raw {
		if ne, ok := item.(models.NetworkEvent); ok {
			events = append(events, ne)
		}
	}
	return events
}

// GetGraph returns the underlying DependencyGraph.
// The graph is thread-safe and may be inspected concurrently.
func (c *Correlator) GetGraph() *DependencyGraph {
	return c.graph
}

// CleanupStale removes graph nodes and edges whose LastSeen is older than
// the configured StaleNodeTimeout. Returns the counts of removed nodes
// and edges.
func (c *Correlator) CleanupStale(ctx context.Context) (nodesRemoved, edgesRemoved int) {
	if err := ctx.Err(); err != nil {
		c.logger.Warn("CleanupStale called with cancelled context", zap.Error(err))
		return 0, 0
	}

	// Count edges before cleanup for diffing.
	edgesBefore := c.graph.EdgeCount()

	removedIDs := c.graph.RemoveStaleNodes(c.config.StaleNodeTimeout)
	nodesRemoved = len(removedIDs)

	// Removed nodes also cascade-remove their edges, so compute delta.
	edgesAfter := c.graph.EdgeCount()
	edgesRemoved = edgesBefore - edgesAfter

	if nodesRemoved > 0 || edgesRemoved > 0 {
		c.logger.Info("Stale cleanup complete",
			zap.Int("nodes_removed", nodesRemoved),
			zap.Int("edges_removed", edgesRemoved),
		)
	}

	return nodesRemoved, edgesRemoved
}

// SnapshotGraph persists the current graph state to Dragonfly for crash
// recovery. If dragonfly is nil, this is a no-op.
func (c *Correlator) SnapshotGraph(ctx context.Context, dragonfly DragonflyClient) error {
	if err := c.graph.Snapshot(ctx, dragonfly); err != nil {
		return fmt.Errorf("correlator: snapshot graph: %w", err)
	}
	return nil
}

// RestoreGraph loads a previously persisted graph state from Dragonfly,
// replacing the current graph entirely. If no snapshot exists, the graph
// starts fresh. If dragonfly is nil, this is a no-op.
func (c *Correlator) RestoreGraph(ctx context.Context, dragonfly DragonflyClient) error {
	if err := c.graph.Restore(ctx, dragonfly); err != nil {
		return fmt.Errorf("correlator: restore graph: %w", err)
	}
	return nil
}

// GraphStats returns aggregated statistics about the current dependency graph.
func (c *Correlator) GraphStats() GraphStats {
	return c.graph.Stats()
}

// ─── internal helpers ─────────────────────────────────────────────────────

// mapProcessToServices resolves each process in the batch to a service name
// using the ServiceMap. Returns a map of "pid:process_name" → service_name.
func (c *Correlator) mapProcessToServices(batch *models.MetricBatch) map[string]string {
	result := make(map[string]string, len(batch.Processes))

	for _, proc := range batch.Processes {
		serviceName := c.serviceMap.Resolve(proc.Name)
		if serviceName == "" {
			continue
		}
		key := fmt.Sprintf("%d:%s", proc.PID, proc.Name)
		result[key] = serviceName
	}

	return result
}

// updateGraphNodes creates or updates a graph node for each unique service
// discovered in the processServices map. Node ID format: "serviceName:agentID".
func (c *Correlator) updateGraphNodes(batch *models.MetricBatch, processServices map[string]string, now time.Time) {
	seen := make(map[string]bool, len(processServices))

	for _, serviceName := range processServices {
		if seen[serviceName] {
			continue
		}
		seen[serviceName] = true

		nodeID := serviceName + ":" + batch.AgentID
		c.graph.AddOrUpdateNode(&GraphNode{
			ID:           nodeID,
			Name:         serviceName,
			Type:         NodeTypeService,
			AgentID:      batch.AgentID,
			FirstSeen:    now,
			LastSeen:     now,
			HealthStatus: "healthy",
		})
	}
}

// extractDependencies builds Dependency entries from TCP events in the
// provided network events. Dependencies are aggregated by destination IP:port.
//
// SourceService is resolved as follows (V1.0 — no PID-to-IP mapping from
// eBPF data yet):
//   - Build the set of unique service names from processServices.
//   - If exactly one service runs on this agent, use it as the source.
//   - Otherwise, fall back to agentID as the source identifier.
//
// TargetService is set to the destination IP (DNS resolution to service
// names requires Phase 4+ DNS correlation).
func (c *Correlator) extractDependencies(networkEvents []models.NetworkEvent, processServices map[string]string, agentID string, now time.Time) []Dependency {
	type depKey struct {
		dstIP   string
		dstPort uint32
	}

	// Build set of unique service names from the batch.
	serviceSet := make(map[string]bool)
	for _, serviceName := range processServices {
		serviceSet[serviceName] = true
	}

	// Resolve source service name for this agent.
	sourceService := resolveSourceService(serviceSet, agentID)

	depMap := make(map[depKey]*Dependency)

	for _, event := range networkEvents {
		for _, tcp := range event.TCP {
			key := depKey{dstIP: tcp.DstIP, dstPort: tcp.DstPort}
			if dep, ok := depMap[key]; ok {
				dep.Frequency++
			} else {
				depMap[key] = &Dependency{
					SourceService: sourceService,
					TargetService: tcp.DstIP,
					Protocol:      "tcp",
					Port:          tcp.DstPort,
					Frequency:     1,
				}
			}
		}
	}

	result := make([]Dependency, 0, len(depMap))
	for _, dep := range depMap {
		result = append(result, *dep)
	}

	return result
}

// resolveSourceService picks the best source service identifier from the
// unique service names found on the local agent. If there is exactly one
// service, it is used. Otherwise, agentID is returned as a safe fallback
// that still allows graph edge creation (one node per agent).
func resolveSourceService(serviceSet map[string]bool, agentID string) string {
	if len(serviceSet) == 1 {
		for svc := range serviceSet {
			return svc
		}
	}
	return agentID
}

// updateGraphEdges creates or updates graph edges for each dependency.
// The source node is derived from the dependency's SourceService (if set)
// and the agent ID from the first node we can find. When SourceService is
// empty, the dependency is skipped for graph edge creation.
func (c *Correlator) updateGraphEdges(dependencies []Dependency, now time.Time) {
	for _, dep := range dependencies {
		if dep.SourceService == "" || dep.TargetService == "" {
			continue
		}

		c.graph.AddOrUpdateEdge(&GraphEdge{
			SourceID:  dep.SourceService,
			TargetID:  dep.TargetService,
			Protocol:  dep.Protocol,
			Port:      dep.Port,
			Frequency: dep.Frequency,
			LatencyMs: dep.LatencyMs,
			ErrorRate: dep.ErrorRate,
			FirstSeen: now,
			LastSeen:  now,
		})
	}
}

// correlateMetrics updates graph node health based on metrics in the batch.
// For now this records LastSeen timestamps; future work will use CPU/memory
// thresholds for health degradation.
func (c *Correlator) correlateMetrics(batch *models.MetricBatch, processServices map[string]string, now time.Time) {
	seen := make(map[string]bool)

	for _, serviceName := range processServices {
		if seen[serviceName] {
			continue
		}
		seen[serviceName] = true

		// Update the node's LastSeen to keep it alive.
		nodeID := serviceName + ":" + batch.AgentID
		c.graph.AddOrUpdateNode(&GraphNode{
			ID:           nodeID,
			Name:         serviceName,
			Type:         NodeTypeService,
			AgentID:      batch.AgentID,
			FirstSeen:    now,
			LastSeen:     now,
			HealthStatus: "healthy",
		})
	}
}
