# Phase 3 Hardened Specification — Processing Pipeline

**Version:** 1.0.0
**Status:** LOCKED — All architectural decisions finalized
**Target LOC:** ~10,000 (Go)
**Estimated Effort:** 4-6 weeks for a senior Go engineer

---

## Table of Contents

1. [Phase 3 Overview & Decisions](#1-phase-3-overview--decisions)
2. [Pre-Phase Setup](#2-pre-phase-setup)
3. [Layer 11: Tumbling Window Aggregator](#3-layer-11-tumbling-window-aggregator)
4. [Layer 12: Correlator & Dependency Graph](#4-layer-12-correlator--dependency-graph)
5. [Layer 13: Enricher & Metadata Injection](#5-layer-13-enricher--metadata-injection)
6. [Layer 14: Pipeline Service Binary](#6-layer-14-pipeline-service-binary)
7. [Configuration Changes](#7-configuration-changes)
8. [Verification Gates](#8-verification-gates)
9. [Performance Targets](#9-performance-targets)
10. [Contingency & Rollback](#10-contingency--rollback)
11. [Appendices](#11-appendices)

---

## 1. Phase 3 Overview & Decisions

### 1.1 What Phase 3 Delivers

Phase 3 transforms the Paryty cluster from a pass-through ingestion pipeline into an intelligent processing engine. The three processing services (Aggregator, Correlator, Enricher) are consolidated into a single **Pipeline Service** binary that processes messages through three sequential stages: Aggregate → Correlate → Enrich.

**Before Phase 3:** Raw metrics flow through ingestion → Redpanda → separate aggregator/correlator/enricher binaries (stubs).

**After Phase 3:** A single pipeline binary consumes from Redpanda, applies windowed aggregation, correlates across data sources, enriches with metadata, and writes to all three storage tiers.

### 1.2 Architectural Decisions (LOCKED)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Aggregation Window Type | **A — Tumbling windows** | Fixed, non-overlapping time buckets (1m, 5m, 1h, 1d). Simpler to implement, lower memory overhead, deterministic window boundaries. |
| 2 | Processing Pipeline Architecture | **B — Pipeline service** | Single binary with aggregate → correlate → enrich. Lower latency (no intermediate topics), simpler deployment, single consumer group. |
| 3 | DB Protocol Inspection | **B — Defer to Phase 4** | Keep Phase 3 focused on Go processing logic. DB inspection requires eBPF changes already scoped for Phase 4. |
| 4 | Enrichment Metadata Source | **A — Agent labels** | Simple metadata from agent registration. No Kubernetes API dependency. Labels are already in the AgentInfo struct. |

### 1.3 What Gets Built

| Layer | Component | Language | LOC | Description |
|-------|-----------|----------|-----|-------------|
| 11 | Tumbling Window Aggregator | Go | ~4,000 | Windowed aggregation with grace periods, downsampling, Top-N tracking |
| 12 | Correlator & Dependency Graph | Go | ~4,000 | Trace-metric correlation, dependency graph builder, topology change detection |
| 13 | Enricher & Metadata Injection | Go | ~2,000 | Agent label injection, service mapping, tag propagation |
| 14 | Pipeline Service Binary | Go | ~1,000 | Single binary combining all 3 stages with shared config and graceful shutdown |

### 1.4 What Gets Deprecated

The following existing files are **replaced** by the pipeline service:

| File | Action | Reason |
|------|--------|--------|
| `cluster/cmd/aggregator/main.go` | **Delete** | Replaced by pipeline binary |
| `cluster/cmd/correlator/main.go` | **Delete** | Replaced by pipeline binary |
| `cluster/cmd/enricher/main.go` | **Delete** | Replaced by pipeline binary |
| `cluster/internal/processing/aggregator.go` | **Rewrite** | Current implementation is stateless; needs windowed state management |
| `cluster/internal/processing/correlator.go` | **Rewrite** | Current implementation has no persistent dependency graph |
| `cluster/internal/processing/enricher.go` | **Enhance** | Current implementation is minimal; needs label propagation and service mapping |

### 1.5 Existing Code Assessment

**What exists (stubs/minimal):**
- `aggregator.go` (277 lines): Stateless aggregation — computes avg/min/max/percentile on a single batch. No windowing, no state, no downsampling.
- `correlator.go` (171 lines): Basic process-to-service mapping and dependency extraction. No persistent graph, no trace correlation.
- `enricher.go` (148 lines): Minimal label injection. No service mapping, no tag propagation.
- `processing_test.go` (411 lines): Good test coverage for current stubs. Tests will need updating.

**What needs to be built from scratch:**
- Tumbling window state manager
- Window persistence (Dragonfly snapshots)
- Downsampling engine
- Top-N tracker
- Dependency graph (persistent, in-memory)
- Trace-metric correlator
- Pipeline orchestrator
- Grace period handler

---

## 2. Pre-Phase Setup

### 2.1 Go Dependencies

Add to `cluster/go.mod`:

```
# Already present (verify):
github.com/twmb/franz-go          # Kafka/Redpanda client
github.com/redis/go-redis/v9      # Dragonfly client
github.com/jackc/pgx/v5           # QuestDB client
github.com/minio/minio-go/v7      # SeaweedFS S3 client
go.uber.org/zap                    # Structured logging

# New for Phase 3:
github.com/emirpasic/gods/v2      # Thread-safe data structures (Top-N heap)
github.com/hashicorp/golang-lru/v2 # LRU cache for service mapping
```

### 2.2 New Package Structure

```
cluster/internal/processing/
├── aggregator.go          # REWRITE — Windowed aggregation engine
├── aggregator_test.go     # NEW — Comprehensive windowed tests
├── correlator.go          # REWRITE — Correlation with dependency graph
├── correlator_test.go     # NEW — Graph and correlation tests
├── enricher.go            # ENHANCE — Label propagation and service mapping
├── enricher_test.go       # UPDATE — New enrichment tests
├── window.go              # NEW — Tumbling window state manager
├── window_test.go         # NEW — Window boundary and grace period tests
├── downsampler.go         # NEW — Retention-based downsampling
├── downsampler_test.go    # NEW — Downsampling tests
├── topn.go                # NEW — Top-N tracker (heap-based)
├── topn_test.go           # NEW — Top-N accuracy tests
├── graph.go               # NEW — Dependency graph (adjacency list)
├── graph_test.go          # NEW — Graph traversal and change detection tests
├── servicemap.go          # NEW — Process-to-service mapping rules
├── servicemap_test.go     # NEW — Service mapping tests
├── pipeline.go            # NEW — Pipeline orchestrator
├── pipeline_test.go       # NEW — End-to-end pipeline tests
├── processing_test.go     # UPDATE — Existing tests (keep for backward compat)
└── helpers.go             # NEW — Shared utility functions (avg, percentile, etc.)
```

### 2.3 New Redpanda Topics

Add to `cluster/internal/stream/topics.go`:

```go
const (
    // Existing (keep):
    TopicMetricsRaw      = "paryty.metrics.raw"
    TopicMetricsAgg      = "paryty.metrics.aggregated"
    TopicTraces          = "paryty.traces"
    TopicEvents          = "paryty.events"
    TopicNetworkEvents   = "paryty.network.events"
    TopicTopologyChanges = "paryty.topology.changes"
    TopicAlerts          = "paryty.alerts"
    TopicDLQ             = "paryty.dead-letter"

    // New for Phase 3:
    TopicMetricsEnriched = "paryty.metrics.enriched"      // Enriched metrics output
    TopicCorrelations    = "paryty.correlations"           // Correlation results
    TopicDependencyGraph = "paryty.dependency.graph"       // Graph snapshots
)
```

---

## 3. Layer 11: Tumbling Window Aggregator

### 3.1 Overview

The Aggregator replaces the current stateless aggregation with a stateful, windowed aggregation engine. It buffers incoming metrics in tumbling (fixed, non-overlapping) time windows and emits aggregated results when each window closes.

**Window sizes:** 1 minute, 5 minutes, 1 hour, 1 day
**Grace period:** 30 seconds (allows late-arriving data after window close)
**State:** In-memory with periodic Dragonfly snapshots for crash recovery

### 3.2 Window State Manager

**File:** `cluster/internal/processing/window.go` (~400 LOC)

The WindowState manages tumbling windows for all agents and metric types.

```go
// package processing

// WindowKey uniquely identifies a window.
type WindowKey struct {
    AgentID    string
    MetricName string
    WindowSize time.Duration
    WindowStart time.Time  // Aligned to window boundary
}

// WindowBuffer holds raw values for a single window.
type WindowBuffer struct {
    Key       WindowKey
    Values    []float64
    Count     int64
    Sum       float64
    Min       float64
    Max       float64
    FirstSeen time.Time
    LastSeen  time.Time
    Closed    bool      // True after grace period expires
    mu        sync.RWMutex
}

// WindowStateManager manages all tumbling windows.
type WindowStateManager struct {
    windows   sync.Map  // map[WindowKey]*WindowBuffer
    gracePeriod time.Duration
    windowSizes []time.Duration
    logger    *zap.Logger
    snapshotInterval time.Duration
    dragonfly DragonflyClient  // Interface for snapshot persistence
    stopCh    chan struct{}
}

// NewWindowStateManager creates a new window state manager.
//
// PARAMETERS:
//   - gracePeriod: how long after window close to accept late data (default 30s)
//   - windowSizes: list of window durations to maintain (1m, 5m, 1h, 1d)
//   - dragonfly: client for persisting window snapshots
//   - logger: structured logger
//
// RETURNS: initialized WindowStateManager
// ERRORS: none (state manager is always valid)
func NewWindowStateManager(
    gracePeriod time.Duration,
    windowSizes []time.Duration,
    dragonfly DragonflyClient,
    logger *zap.Logger,
) *WindowStateManager

// AddValue adds a metric value to all applicable windows.
//
// This method calculates the aligned window start time for each window size,
// creates or retrieves the WindowBuffer, and adds the value.
//
// PARAMETERS:
//   - agentID: the agent that produced the metric
//   - metricName: e.g. "cpu.usage_percent"
//   - value: the metric value
//   - timestamp: when the metric was collected
//
// ERRORS: none (creates windows on demand)
func (w *WindowStateManager) AddValue(
    agentID string,
    metricName string,
    value float64,
    timestamp time.Time,
)

// GetClosedWindows returns all windows whose grace period has expired.
//
// A window is "closed" when: current_time > window_end + gracePeriod
// Closed windows are marked as Closed=true and returned for aggregation.
//
// RETURNS: slice of closed WindowBuffers
// SIDE EFFECT: marks returned windows as Closed
func (w *WindowStateManager) GetClosedWindows() []*WindowBuffer

// PurgeClosed removes closed windows from memory.
//
// Should be called after closed windows have been aggregated and stored.
//
// PARAMETERS:
//   - keys: WindowKeys to remove
func (w *WindowStateManager) PurgeClosed(keys []WindowKey)

// Snapshot persists all open windows to Dragonfly.
//
// Called periodically (every 30s) for crash recovery.
// Window state is stored as JSON under key "paryty:pipeline:window:snapshot".
//
// ERRORS: returns error if Dragonfly write fails
func (w *WindowStateManager) Snapshot(ctx context.Context) error

// Restore loads window state from Dragonfly snapshot.
//
// Called at startup to recover from a previous crash.
// If no snapshot exists, starts with empty state.
//
// ERRORS: returns error if Dragonfly read fails (non-fatal, starts fresh)
func (w *WindowStateManager) Restore(ctx context.Context) error

// Start begins the background goroutine for window management.
//
// The background goroutine:
//   1. Checks for closed windows every 1 second
//   2. Snapshots to Dragonfly every snapshotInterval
//   3. Purges windows older than 24 hours
//
// Stop via StopCh or context cancellation.
func (w *WindowStateManager) Start(ctx context.Context)

// Stop gracefully shuts down the window state manager.
func (w *WindowStateManager) Stop()
```

### 3.3 Window Alignment Logic

```go
// alignToWindow calculates the window start time for a given timestamp.
//
// EXAMPLES (window size = 1 minute):
//   - 12:34:56 → 12:34:00
//   - 12:35:00 → 12:35:00
//   - 12:35:59 → 12:35:00
//
// EXAMPLES (window size = 1 hour):
//   - 12:34:56 → 12:00:00
//   - 13:00:00 → 13:00:00
//
// ALGORITHM:
//   unix_seconds = timestamp.Unix()
//   aligned = (unix_seconds / window_seconds) * window_seconds
//   return time.Unix(aligned, 0).UTC()
func alignToWindow(t time.Time, windowSize time.Duration) time.Time
```

### 3.4 Aggregation Engine

**File:** `cluster/internal/processing/aggregator.go` (~800 LOC, REWRITE)

```go
// package processing

// AggregatorConfig holds aggregator configuration.
type AggregatorConfig struct {
    WindowSizes    []time.Duration  // [1m, 5m, 1h, 1d]
    GracePeriod    time.Duration    // 30s
    SnapshotInterval time.Duration  // 30s
    TopNSize       int              // 10 (top 10 processes/containers)
}

// Aggregator is the windowed aggregation engine.
type Aggregator struct {
    config    AggregatorConfig
    windows   *WindowStateManager
    topn      *TopNTracker
    downsampler *Downsampler
    logger    *zap.Logger
    metrics   *AggregatorMetrics  // Internal metrics (prometheus)
}

// NewAggregator creates a new windowed aggregator.
//
// PARAMETERS:
//   - config: aggregation configuration
//   - dragonfly: client for window snapshots and downsampling
//   - logger: structured logger
//
// RETURNS: initialized Aggregator
// ERRORS: returns error if dragonfly client is nil
func NewAggregator(
    config AggregatorConfig,
    dragonfly DragonflyClient,
    logger *zap.Logger,
) (*Aggregator, error)

// Process ingests a metric batch into the windowed aggregation engine.
//
// For each metric in the batch:
//   1. Extracts numeric values (CPU%, memory bytes, disk I/O, network I/O)
//   2. Adds values to all applicable tumbling windows
//   3. Updates Top-N trackers
//   4. Returns any windows that have closed (ready for output)
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - batch: incoming metric batch from a single agent
//   - timestamp: when the batch was collected (from the agent)
//
// RETURNS:
//   - []AggregatedMetric: aggregated results from closed windows
//   - []TopNResult: current top-N rankings (if any changed)
//
// ERRORS: returns error if window state operations fail
func (a *Aggregator) Process(
    ctx context.Context,
    batch *models.MetricBatch,
    timestamp time.Time,
) ([]models.AggregatedMetric, []TopNResult, error)

// aggregateWindow computes aggregated metrics from a closed window buffer.
//
// For each window, produces:
//   - avg, min, max, p50, p90, p99, count, sum
//
// PARAMETERS:
//   - window: closed WindowBuffer with raw values
//
// RETURNS: slice of AggregatedMetric (one per aggregation type)
func (a *Aggregator) aggregateWindow(window *WindowBuffer) []models.AggregatedMetric

// extractMetricValues extracts all numeric metric values from a batch.
//
// Returns a map of metric_name → value for all metrics in the batch.
// Metric names follow the convention: "{category}.{field}"
//
// EXAMPLES:
//   - "cpu.usage_percent" → 45.2
//   - "memory.usage_percent" → 72.8
//   - "disk.read_bytes_per_sec" → 1024000
//   - "network.rx_bytes_per_sec" → 512000
func (a *Aggregator) extractMetricValues(
    batch *models.MetricBatch,
) map[string]float64
```

### 3.5 Downsampling Engine

**File:** `cluster/internal/processing/downsampler.go` (~300 LOC)

The downsampler converts high-resolution aggregated metrics into lower-resolution versions for long-term storage.

```go
// package processing

// DownsamplingRule defines a retention and downsampling policy.
type DownsamplingRule struct {
    SourceWindow   time.Duration  // e.g. 1m
    TargetWindow   time.Duration  // e.g. 5m
    RetentionDays  int            // Keep source data for N days
}

// DefaultDownsamplingRules:
//   - 1m  → 5m   after 7 days  (keep 1m for 7 days)
//   - 5m  → 1h   after 30 days (keep 5m for 30 days)
//   - 1h  → 1d   after 90 days (keep 1h for 90 days)
//   - 1d  → kept forever

// Downsampler handles retention-based downsampling of aggregated metrics.
type Downsampler struct {
    rules      []DownsamplingRule
    dragonfly  DragonflyClient
    questdb    QuestDBClient
    logger     *zap.Logger
}

// NewDownsampler creates a new downsampler.
func NewDownsampler(
    rules []DownsamplingRule,
    dragonfly DragonflyClient,
    questdb QuestDBClient,
    logger *zap.Logger,
) *Downsampler

// Downsample runs the downsampling pipeline.
//
// Called periodically (every 1 hour) by the pipeline service.
//
// For each downsampling rule:
//   1. Query QuestDB for aggregated metrics older than retention period
//   2. Group by (agent_id, metric_name, target_window)
//   3. Compute target aggregations (avg of avgs, max of maxes, etc.)
//   4. Write downsampled metrics to QuestDB
//   5. Delete source metrics older than retention period
//
// PARAMETERS:
//   - ctx: context for cancellation
//
// ERRORS: returns error if any step fails (non-fatal, retried next cycle)
func (d *Downsampler) Downsample(ctx context.Context) error

// queryForDownsampling queries QuestDB for metrics to downsample.
//
// PARAMETERS:
//   - sourceWindow: source aggregation window
//   - olderThan: only include metrics older than this
//
// RETURNS: grouped metrics ready for downsampling
func (d *Downsampler) queryForDownsampling(
    ctx context.Context,
    sourceWindow time.Duration,
    olderThan time.Time,
) ([]DownsampleGroup, error)

// DownsampleGroup holds metrics grouped for downsampling.
type DownsampleGroup struct {
    AgentID    string
    MetricName string
    Window     time.Duration
    Values     []AggregatedMetric
}
```

### 3.6 Top-N Tracker

**File:** `cluster/internal/processing/topn.go` (~300 LOC)

```go
// package processing

// TopNResult represents a Top-N ranking entry.
type TopNResult struct {
    Rank       int       `json:"rank"`
    AgentID    string    `json:"agent_id"`
    Name       string    `json:"name"`        // Process name or container name
    Category   string    `json:"category"`     // "cpu", "memory", "disk", "network"
    Value      float64   `json:"value"`
    Window     time.Duration `json:"window"`
    Timestamp  time.Time `json:"timestamp"`
}

// TopNTracker tracks the top N items across multiple categories.
//
// Uses a min-heap to efficiently maintain the top N items.
// When a new value arrives that is larger than the smallest item
// in the heap, the smallest item is replaced.
type TopNTracker struct {
    n         int                            // Top N size
    trackers  map[string]*categoryTracker    // category → tracker
    mu        sync.RWMutex
    logger    *zap.Logger
}

// categoryTracker tracks top N for a single category.
type categoryTracker struct {
    category string
    entries  []TopNEntry  // Min-heap by value
    index    map[string]int  // name → heap index (for updates)
    maxSize  int
}

// TopNEntry is a single entry in the Top-N tracker.
type TopNEntry struct {
    Name      string
    AgentID   string
    Value     float64
    Timestamp time.Time
}

// NewTopNTracker creates a new Top-N tracker.
//
// PARAMETERS:
//   - n: number of top items to track (e.g. 10)
//   - categories: list of categories to track
//     e.g. ["cpu.process", "memory.process", "disk.device", "network.interface"]
//   - logger: structured logger
func NewTopNTracker(
    n int,
    categories []string,
    logger *zap.Logger,
) *TopNTracker

// Update adds or updates an entry in the Top-N tracker.
//
// If the entry already exists (same name), updates its value.
// If the entry is new and larger than the smallest in the heap, it replaces it.
// If the heap is not full, the entry is added directly.
//
// PARAMETERS:
//   - category: which category to update
//   - name: entry name (e.g. process name "nginx")
//   - agentID: agent that reported this entry
//   - value: metric value
//   - timestamp: when the value was observed
func (t *TopNTracker) Update(
    category string,
    name string,
    agentID string,
    value float64,
    timestamp time.Time,
)

// GetTopN returns the current top N entries for a category.
//
// Returns entries sorted by value descending (highest first).
//
// PARAMETERS:
//   - category: which category to query
//
// RETURNS: sorted slice of TopNResult
func (t *TopNTracker) GetTopN(category string) []TopNResult

// GetChanged returns entries that changed since the last call.
//
// Useful for only publishing Top-N updates when rankings change.
//
// RETURNS: map of category → changed entries
func (t *TopNTracker) GetChanged() map[string][]TopNResult
```

### 3.7 Aggregation Pseudo-Code

```
FOR each incoming MetricBatch from Redpanda:
    timestamp = batch.Timestamp
    
    // 1. Extract all metric values
    values = extractMetricValues(batch)
    // values = {"cpu.usage_percent": 45.2, "memory.usage_percent": 72.8, ...}
    
    // 2. Add to all applicable windows
    FOR each (metricName, value) in values:
        windows.AddValue(batch.AgentID, metricName, value, timestamp)
    
    // 3. Update Top-N trackers
    FOR each process in batch.Processes:
        topn.Update("cpu.process", process.Name, batch.AgentID, process.CPUPercent, timestamp)
        topn.Update("memory.process", process.Name, batch.AgentID, process.MemoryBytes, timestamp)
    
    // 4. Check for closed windows
    closedWindows = windows.GetClosedWindows()
    
    // 5. Aggregate closed windows
    results = []
    FOR each window in closedWindows:
        aggregated = aggregateWindow(window)
        // aggregated contains: avg, min, max, p50, p90, p99, count, sum
        results.append(aggregated)
    
    // 6. Get Top-N changes
    topnChanges = topn.GetChanged()
    
    RETURN results, topnChanges
```

### 3.8 Window Lifecycle Diagram

```
Time →  |---Window 1---|---Window 2---|---Window 3---|
        12:00:00       12:01:00       12:02:00       12:03:00

Data arrives at 12:00:15 → goes to Window 1
Data arrives at 12:00:45 → goes to Window 1
Data arrives at 12:01:05 → goes to Window 2

At 12:01:00: Window 1 boundary reached, but NOT closed yet (grace period)
At 12:01:30: Window 1 grace period expires → CLOSED → aggregate & emit

Late data at 12:01:20 → goes to Window 1 (within grace period)
Late data at 12:01:35 → DISCARDED (Window 1 already closed)
                        → Goes to Window 2 (if within Window 2 boundary)
```

---

## 4. Layer 12: Correlator & Dependency Graph

### 4.1 Overview

The Correlator builds relationships between different data sources: metrics, network events, traces, and logs. It maintains a persistent, in-memory dependency graph that tracks service-to-service relationships over time.

### 4.2 Dependency Graph

**File:** `cluster/internal/processing/graph.go` (~500 LOC)

```go
// package processing

// GraphNode represents a service or endpoint in the dependency graph.
type GraphNode struct {
    ID          string            `json:"id"`          // Unique node ID
    Name        string            `json:"name"`        // Human-readable name
    Type        NodeType          `json:"type"`        // "service", "process", "endpoint"
    AgentID     string            `json:"agent_id"`    // Reporting agent
    Labels      map[string]string `json:"labels"`      // Metadata labels
    FirstSeen   time.Time         `json:"first_seen"`
    LastSeen    time.Time         `json:"last_seen"`
    HealthStatus string           `json:"health_status"` // "healthy", "degraded", "unhealthy"
}

// NodeType enumerates graph node types.
type NodeType string

const (
    NodeTypeService   NodeType = "service"
    NodeTypeProcess   NodeType = "process"
    NodeTypeEndpoint  NodeType = "endpoint"
)

// GraphEdge represents a dependency between two nodes.
type GraphEdge struct {
    SourceID    string            `json:"source_id"`
    TargetID    string            `json:"target_id"`
    Protocol    string            `json:"protocol"`     // "tcp", "http", "grpc"
    Port        uint32            `json:"port"`
    Frequency   int64             `json:"frequency"`    // Connection count
    LatencyMs   float64           `json:"latency_ms"`   // Average latency
    ErrorRate   float64           `json:"error_rate"`   // Error percentage
    Labels      map[string]string `json:"labels"`
    FirstSeen   time.Time         `json:"first_seen"`
    LastSeen    time.Time         `json:"last_seen"`
}

// DependencyGraph is an in-memory directed graph of service dependencies.
//
// Thread-safe: all operations use RWMutex.
// Periodic snapshots: graph is persisted to Dragonfly every 60 seconds.
type DependencyGraph struct {
    nodes     map[string]*GraphNode    // nodeID → node
    edges     map[string]*GraphEdge    // "sourceID:targetID" → edge
    adjacency map[string][]string      // nodeID → []neighborNodeIDs
    mu        sync.RWMutex
    logger    *zap.Logger
    
    // Change tracking
    changes   []GraphChange
    changesMu sync.Mutex
}

// GraphChange represents a change detected in the dependency graph.
type GraphChange struct {
    Type      string     `json:"type"`      // "node_added", "node_removed", "edge_added", "edge_removed", "edge_updated"
    Node      *GraphNode `json:"node,omitempty"`
    Edge      *GraphEdge `json:"edge,omitempty"`
    Timestamp time.Time  `json:"timestamp"`
    AgentID   string     `json:"agent_id"`
}

// NewDependencyGraph creates a new empty dependency graph.
func NewDependencyGraph(logger *zap.Logger) *DependencyGraph

// AddOrUpdateNode adds a new node or updates an existing one.
//
// If the node already exists (same ID), updates LastSeen and Labels.
// If the node is new, creates it and records a "node_added" change.
//
// PARAMETERS:
//   - node: the node to add or update
//
// RETURNS: true if a new node was created, false if updated
func (g *DependencyGraph) AddOrUpdateNode(node *GraphNode) bool

// AddOrUpdateEdge adds a new edge or updates an existing one.
//
// If the edge already exists (same source:target), updates frequency,
// latency, error rate, and LastSeen. If new, creates and records change.
//
// PARAMETERS:
//   - edge: the edge to add or update
//
// RETURNS: true if a new edge was created, false if updated
func (g *DependencyGraph) AddOrUpdateEdge(edge *GraphEdge) bool

// RemoveStaleNodes removes nodes not seen for longer than maxAge.
//
// PARAMETERS:
//   - maxAge: maximum age before a node is considered stale
//
// RETURNS: slice of removed node IDs
func (g *DependencyGraph) RemoveStaleNodes(maxAge time.Duration) []string

// GetNeighbors returns all neighbors of a node.
//
// PARAMETERS:
//   - nodeID: the node to query
//
// RETURNS: slice of neighbor nodes and connecting edges
func (g *DependencyGraph) GetNeighbors(nodeID string) ([]*GraphNode, []*GraphEdge)

// GetPath finds the shortest path between two nodes.
//
// Uses BFS for unweighted shortest path.
//
// PARAMETERS:
//   - sourceID: starting node
//   - targetID: destination node
//
// RETURNS: ordered slice of node IDs forming the path, empty if no path
func (g *DependencyGraph) GetPath(sourceID, targetID string) []string

// GetChanges returns and clears all recorded changes since last call.
//
// Used by the pipeline to publish changes to the topology.changes topic.
//
// RETURNS: slice of GraphChange
func (g *DependencyGraph) GetChanges() []GraphChange

// Snapshot persists the entire graph to Dragonfly.
//
// Stored as JSON under key "paryty:pipeline:graph:snapshot".
// Uses Zstd compression for the serialized graph.
//
// ERRORS: returns error if serialization or storage fails
func (g *DependencyGraph) Snapshot(ctx context.Context, dragonfly DragonflyClient) error

// Restore loads the graph from a Dragonfly snapshot.
//
// ERRORS: returns error if deserialization fails (non-fatal, starts fresh)
func (g *DependencyGraph) Restore(ctx context.Context, dragonfly DragonflyClient) error

// Nodes returns a copy of all nodes in the graph.
func (g *DependencyGraph) Nodes() []*GraphNode

// Edges returns a copy of all edges in the graph.
func (g *DependencyGraph) Edges() []*GraphEdge
```

### 4.3 Correlator Engine

**File:** `cluster/internal/processing/correlator.go` (~800 LOC, REWRITE)

```go
// package processing

// CorrelatorConfig holds correlator configuration.
type CorrelatorConfig struct {
    StaleNodeTimeout  time.Duration  // 5m — remove nodes not seen for 5 minutes
    GraphSnapshotInterval time.Duration  // 60s
    EventBufferSize   int            // 10000 — max network events to buffer
    CorrelationWindow time.Duration  // 30s — time window for correlating events
}

// Correlator correlates data across metrics, network events, and traces.
type Correlator struct {
    config     CorrelatorConfig
    graph      *DependencyGraph
    eventBuf   *RingBuffer         // Circular buffer for network events
    serviceMap *ServiceMap         // Process-to-service mapping
    logger     *zap.Logger
    metrics    *CorrelatorMetrics  // Internal metrics
}

// NewCorrelator creates a new correlator.
//
// PARAMETERS:
//   - config: correlator configuration
//   - serviceMap: process-to-service mapping rules
//   - logger: structured logger
//
// RETURNS: initialized Correlator
func NewCorrelator(
    config CorrelatorConfig,
    serviceMap *ServiceMap,
    logger *zap.Logger,
) *Correlator

// Correlate processes a metric batch and network events, updating the
// dependency graph and detecting topology changes.
//
// PROCESSING STEPS:
//   1. Map processes to services using service map
//   2. Create/update graph nodes for each service
//   3. Extract dependencies from network events
//   4. Create/update graph edges for each dependency
//   5. Detect topology changes (new connections, removed connections)
//   6. Correlate metrics with graph nodes (attach CPU/mem to nodes)
//   7. Detect anomalies (sudden frequency changes, error spikes)
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - batch: metric batch from a single agent
//   - networkEvents: network events from the same agent (within correlation window)
//
// RETURNS:
//   - *CorrelationResult: correlation output with changes and dependencies
//
// ERRORS: returns error if graph operations fail
func (c *Correlator) Correlate(
    ctx context.Context,
    batch *models.MetricBatch,
    networkEvents []models.NetworkEvent,
) (*CorrelationResult, error)

// CorrelationResult is the output of correlation.
type CorrelationResult struct {
    AgentID          string                  `json:"agent_id"`
    Timestamp        time.Time               `json:"timestamp"`
    ProcessServices  map[string]string       `json:"process_services"`   // pid:name → service_name
    Dependencies     []Dependency            `json:"dependencies"`
    TopologyChanges  []GraphChange           `json:"topology_changes"`
    GraphStats       GraphStats              `json:"graph_stats"`
}

// GraphStats holds statistics about the dependency graph.
type GraphStats struct {
    NodeCount    int     `json:"node_count"`
    EdgeCount    int     `json:"edge_count"`
    AvgFanOut    float64 `json:"avg_fan_out"`     // Average edges per node
    MaxFanOut    int     `json:"max_fan_out"`     // Maximum edges from a single node
    IsolatedNodes int    `json:"isolated_nodes"`  // Nodes with no connections
}

// BufferNetworkEvents adds network events to the circular buffer.
//
// Events are buffered for correlation with later metric batches.
// Old events are automatically evicted when the buffer is full.
//
// PARAMETERS:
//   - events: network events to buffer
func (c *Correlator) BufferNetworkEvents(events []models.NetworkEvent)

// GetGraph returns the current dependency graph (read-only snapshot).
func (c *Correlator) GetGraph() *DependencyGraph

// CleanupStale removes stale nodes and edges from the graph.
//
// Should be called periodically (every 1 minute).
//
// PARAMETERS:
//   - ctx: context for cancellation
//
// RETURNS: number of nodes and edges removed
func (c *Correlator) CleanupStale(ctx context.Context) (nodesRemoved, edgesRemoved int)
```

### 4.4 Service Map

**File:** `cluster/internal/processing/servicemap.go` (~300 LOC)

```go
// package processing

// ServiceMappingRule defines how to map a process name to a service name.
type ServiceMappingRule struct {
    Match       string `yaml:"match"`        // Process name prefix or regex
    ServiceName string `yaml:"service_name"` // Mapped service name
    IsRegex     bool   `yaml:"is_regex"`     // Whether Match is a regex
}

// DefaultServiceMappingRules provides built-in process-to-service mappings.
var DefaultServiceMappingRules = []ServiceMappingRule{
    {Match: "nginx", ServiceName: "nginx"},
    {Match: "postgres", ServiceName: "postgresql"},
    {Match: "redis-server", ServiceName: "redis"},
    {Match: "mongod", ServiceName: "mongodb"},
    {Match: "node", ServiceName: "nodejs"},
    {Match: "python3", ServiceName: "python"},
    {Match: "java", ServiceName: "java"},
    {Match: "etcd", ServiceName: "etcd"},
    {Match: "kube-apiserver", ServiceName: "kubernetes-api"},
    {Match: "kubelet", ServiceName: "kubelet"},
}

// ServiceMap maps process names to service names.
//
// Thread-safe: uses RWMutex for concurrent access.
// Supports both prefix matching and regex matching.
type ServiceMap struct {
    rules     []ServiceMappingRule
    custom    map[string]string  // Exact match overrides
    regexps   []*regexp.Regexp   // Compiled regex patterns
    mu        sync.RWMutex
    logger    *zap.Logger
}

// NewServiceMap creates a new service map from rules.
//
// PARAMETERS:
//   - rules: mapping rules (nil for defaults)
//   - logger: structured logger
//
// RETURNS: initialized ServiceMap
// ERRORS: returns error if any regex rule fails to compile
func NewServiceMap(
    rules []ServiceMappingRule,
    logger *zap.Logger,
) (*ServiceMap, error)

// Resolve maps a process name to a service name.
//
// Resolution order:
//   1. Exact match in custom overrides
//   2. Prefix match in rules
//   3. Regex match in rules
//   4. Return process name as-is (fallback)
//
// PARAMETERS:
//   - processName: the process name (e.g. "nginx-worker")
//
// RETURNS: service name (e.g. "nginx")
func (s *ServiceMap) Resolve(processName string) string

// AddCustom adds a custom process-to-service mapping.
//
// Overrides any rule-based mapping for this process name.
//
// PARAMETERS:
//   - processName: exact process name
//   - serviceName: mapped service name
func (s *ServiceMap) AddCustom(processName, serviceName string)

// RemoveCustom removes a custom mapping.
func (s *ServiceMap) RemoveCustom(processName string)
```

### 4.5 Correlation Pseudo-Code

```
FOR each incoming MetricBatch + NetworkEvents:
    
    // 1. Process → Service mapping
    processServices = {}
    FOR each process in batch.Processes:
        serviceName = serviceMap.Resolve(process.Name)
        processServices[process.PID + ":" + process.Name] = serviceName
        
        // Create/update graph node
        graph.AddOrUpdateNode(&GraphNode{
            ID: serviceName + ":" + batch.AgentID,
            Name: serviceName,
            Type: NodeTypeService,
            AgentID: batch.AgentID,
            LastSeen: now,
        })
    
    // 2. Extract dependencies from network events
    dependencies = []
    FOR each event in networkEvents:
        FOR each tcp in event.TCP:
            targetNode = graph.FindNodeByIP(tcp.DstIP)
            IF targetNode != nil:
                edge = &GraphEdge{
                    SourceID: currentNodeID,
                    TargetID: targetNode.ID,
                    Protocol: "tcp",
                    Port: tcp.DstPort,
                    Frequency: 1,
                }
                graph.AddOrUpdateEdge(edge)
                dependencies.append(edge)
        
        FOR each http in event.HTTP:
            targetNode = graph.FindNodeByHost(http.Host)
            IF targetNode != nil:
                edge = &GraphEdge{
                    SourceID: currentNodeID,
                    TargetID: targetNode.ID,
                    Protocol: "http",
                    Frequency: 1,
                }
                graph.AddOrUpdateEdge(edge)
                dependencies.append(edge)
    
    // 3. Detect topology changes
    changes = graph.GetChanges()
    
    // 4. Compute graph stats
    stats = GraphStats{
        NodeCount: graph.NodeCount(),
        EdgeCount: graph.EdgeCount(),
        AvgFanOut: graph.AvgFanOut(),
        MaxFanOut: graph.MaxFanOut(),
        IsolatedNodes: graph.IsolatedNodeCount(),
    }
    
    RETURN CorrelationResult{
        AgentID: batch.AgentID,
        ProcessServices: processServices,
        Dependencies: dependencies,
        TopologyChanges: changes,
        GraphStats: stats,
    }
```

---

## 5. Layer 13: Enricher & Metadata Injection

### 5.1 Overview

The Enricher adds metadata to all processed data before it's stored. It uses agent labels (from registration) to inject environment, region, team, and other contextual information. It also handles service name propagation and custom tag injection.

### 5.2 Enricher Engine

**File:** `cluster/internal/processing/enricher.go` (~500 LOC, ENHANCE)

```go
// package processing

// EnricherConfig holds enricher configuration.
type EnricherConfig struct {
    // StandardLabelKeys are label keys that are always propagated.
    StandardLabelKeys []string  // ["env", "region", "team", "service", "version"]
    
    // MaxLabelsPerMetric limits label count to prevent cardinality explosion.
    MaxLabelsPerMetric int  // 20
    
    // LabelPrefix is prepended to all agent labels in metric labels.
    LabelPrefix string  // "agent."
}

// DefaultEnricherConfig returns sensible defaults.
func DefaultEnricherConfig() EnricherConfig

// Enricher enriches data with metadata from agent labels.
type Enricher struct {
    config     EnricherConfig
    agentCache *AgentCache  // Local cache of agent info
    logger     *zap.Logger
    metrics    *EnricherMetrics
}

// AgentCache caches agent info to avoid repeated Dragonfly lookups.
type AgentCache struct {
    cache    *lru.Cache[string, *models.AgentInfo]  // LRU cache
    dragonfly DragonflyClient
    ttl       time.Duration  // Cache entry TTL (5 minutes)
    mu        sync.RWMutex
}

// NewEnricher creates a new enricher.
//
// PARAMETERS:
//   - config: enricher configuration
//   - dragonfly: client for agent info lookups
//   - logger: structured logger
//
// RETURNS: initialized Enricher
func NewEnricher(
    config EnricherConfig,
    dragonfly DragonflyClient,
    logger *zap.Logger,
) (*Enricher, error)

// EnrichBatch enriches a metric batch with agent metadata.
//
// PROCESSING STEPS:
//   1. Look up agent info from cache (or Dragonfly)
//   2. Inject standard labels into all metric types
//   3. Propagate service name from correlation
//   4. Add processing metadata (pipeline version, timestamp)
//   5. Validate label cardinality
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - batch: metric batch to enrich
//   - correlationResult: correlation output (for service name propagation)
//
// RETURNS: enriched metric batch
// ERRORS: returns error if enrichment fails (non-fatal, returns original batch)
func (e *Enricher) EnrichBatch(
    ctx context.Context,
    batch *models.MetricBatch,
    correlationResult *CorrelationResult,
) (*models.MetricBatch, error)

// EnrichAggregated enriches aggregated metrics with agent metadata.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - metrics: aggregated metrics to enrich
//   - agentID: agent that produced the metrics
//
// RETURNS: enriched aggregated metrics
func (e *Enricher) EnrichAggregated(
    ctx context.Context,
    metrics []models.AggregatedMetric,
    agentID string,
) ([]models.AggregatedMetric, error)

// EnrichTopology enriches topology changes with agent metadata.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - changes: topology changes to enrich
//   - agentID: agent that detected the changes
//
// RETURNS: enriched topology changes
func (e *Enricher) EnrichTopology(
    ctx context.Context,
    changes []GraphChange,
    agentID string,
) ([]GraphChange, error)

// getAgentInfo retrieves agent info from cache or Dragonfly.
//
// Cache hit: returns immediately.
// Cache miss: queries Dragonfly, caches result, returns.
// Dragonfly miss: returns minimal AgentInfo with just the ID.
func (e *Enricher) getAgentInfo(ctx context.Context, agentID string) (*models.AgentInfo, error)

// buildLabels constructs the final label map for a metric.
//
// Merges:
//   - Standard labels (env, region, team, etc.)
//   - Agent labels (from registration)
//   - Processing metadata (pipeline_version, enriched_at)
//   - Correlation metadata (service_name, if available)
//
// Enforces MaxLabelsPerMetric limit.
func (e *Enricher) buildLabels(
    agentInfo *models.AgentInfo,
    extraLabels map[string]string,
) map[string]string
```

### 5.3 Enrichment Pseudo-Code

```
FOR each item to enrich:
    
    // 1. Get agent info (cached)
    agentInfo = agentCache.Get(batch.AgentID)
    IF agentInfo == nil:
        agentInfo = dragonfly.GetAgentState(batch.AgentID)
        IF agentInfo == nil:
            agentInfo = &AgentInfo{ID: batch.AgentID}  // Minimal
        agentCache.Set(batch.AgentID, agentInfo)
    
    // 2. Build label map
    labels = {}
    
    // Standard labels from agent registration
    FOR each key in ["env", "region", "team", "service", "version"]:
        IF value, ok := agentInfo.Labels[key]; ok:
            labels["agent." + key] = value
    
    // All agent labels (up to MaxLabelsPerMetric)
    FOR each (key, value) in agentInfo.Labels:
        IF len(labels) < MaxLabelsPerMetric:
            labels["agent." + key] = value
    
    // Processing metadata
    labels["pipeline.version"] = "1.0.0"
    labels["pipeline.enriched_at"] = time.Now().UTC().Format(time.RFC3339)
    
    // Correlation metadata (if available)
    IF correlationResult != nil:
        serviceName = correlationResult.ProcessServices[processKey]
        IF serviceName != "":
            labels["service.name"] = serviceName
    
    // 3. Inject labels into all metric types
    FOR each cpu in batch.CPU:
        cpu.Labels = merge(cpu.Labels, labels)
    FOR each mem in batch.Memory:
        mem.Labels = merge(mem.Labels, labels)
    // ... same for disk, network, process, container
    
    RETURN enriched batch
```

---

## 6. Layer 14: Pipeline Service Binary

### 6.1 Overview

The Pipeline Service is a single binary that replaces the three separate aggregator/correlator/enricher binaries. It reads from Redpanda, processes messages through all three stages sequentially, and writes to storage tiers.

### 6.2 Pipeline Orchestrator

**File:** `cluster/internal/processing/pipeline.go` (~400 LOC)

```go
// package processing

// PipelineConfig holds pipeline configuration.
type PipelineConfig struct {
    Aggregator  AggregatorConfig  `yaml:"aggregator"`
    Correlator  CorrelatorConfig  `yaml:"correlator"`
    Enricher    EnricherConfig    `yaml:"enricher"`
    
    // Input topics
    InputTopics []string  // ["paryty.metrics.raw", "paryty.network.events", "paryty.traces", "paryty.events"]
    
    // Consumer group
    ConsumerGroup string  // "paryty-pipeline"
    
    // Batch processing
    BatchSize    int           // 100 — process up to N messages per batch
    BatchTimeout time.Duration // 1s — flush batch after timeout
    
    // Health
    HealthCheckInterval time.Duration  // 10s
}

// Pipeline is the main processing pipeline orchestrator.
type Pipeline struct {
    config      PipelineConfig
    aggregator  *Aggregator
    correlator  *Correlator
    enricher    *Enricher
    downsampler *Downsampler
    store       *storage.Store
    stream      *stream.StreamEngine
    producer    *stream.Producer
    logger      *zap.Logger
    metrics     *PipelineMetrics
    
    // Lifecycle
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// NewPipeline creates a new pipeline orchestrator.
//
// PARAMETERS:
//   - config: pipeline configuration
//   - store: storage orchestrator (hot/warm/cold)
//   - streamEngine: Redpanda stream engine
//   - dragonfly: Dragonfly client (for snapshots)
//   - questdb: QuestDB client (for downsampling)
//   - logger: structured logger
//
// RETURNS: initialized Pipeline
// ERRORS: returns error if any component fails to initialize
func NewPipeline(
    config PipelineConfig,
    store *storage.Store,
    streamEngine *stream.StreamEngine,
    dragonfly DragonflyClient,
    questdb QuestDBClient,
    logger *zap.Logger,
) (*Pipeline, error)

// Start starts the pipeline.
//
// Starts the following goroutines:
//   1. Consumer: reads from Redpanda topics
//   2. Processor: processes messages through aggregate → correlate → enrich
//   3. Window manager: checks for closed windows, snapshots
//   4. Downsampler: periodic downsampling (every 1 hour)
//   5. Graph cleanup: removes stale nodes (every 1 minute)
//   6. Health reporter: reports pipeline health (every 10 seconds)
//
// All goroutines respect context cancellation for graceful shutdown.
func (p *Pipeline) Start(ctx context.Context) error

// Stop gracefully shuts down the pipeline.
//
// Waits for in-flight processing to complete (up to 30s timeout).
// Finalizes any open windows. Takes a final graph snapshot.
func (p *Pipeline) Stop()

// processMessage processes a single Redpanda message through the pipeline.
//
// ROUTING LOGIC:
//   - messages from "metrics.raw" → aggregate → correlate → enrich → store
//   - messages from "network.events" → buffer for correlation
//   - messages from "traces" → enrich → store (warm)
//   - messages from "events" → enrich → store (cold)
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - topic: source topic
//   - key: message key (agent_id)
//   - value: message value (JSON)
//
// ERRORS: returns error if processing fails (message will be retried)
func (p *Pipeline) processMessage(
    ctx context.Context,
    topic string,
    key string,
    value []byte,
) error

// processMetricBatch processes a metric batch through all 3 stages.
//
// STAGE 1 — AGGREGATE:
//   - Add metrics to tumbling windows
//   - Update Top-N trackers
//   - Collect any closed window results
//
// STAGE 2 — CORRELATE:
//   - Correlate with buffered network events
//   - Update dependency graph
//   - Detect topology changes
//
// STAGE 3 — ENRICH:
//   - Inject agent labels
//   - Propagate service names
//   - Add processing metadata
//
// STORE:
//   - Write enriched batch to hot storage (Dragonfly)
//   - Write enriched batch to warm storage (QuestDB ILP)
//   - Write aggregated metrics to "metrics.aggregated" topic
//   - Write topology changes to "topology.changes" topic
//   - Write to cold storage if trace/event data
func (p *Pipeline) processMetricBatch(
    ctx context.Context,
    batch *models.MetricBatch,
) error

// processNetworkEvents buffers network events for correlation.
func (p *Pipeline) processNetworkEvents(
    ctx context.Context,
    events []models.NetworkEvent,
) error

// processTrace stores a trace span in warm storage.
func (p *Pipeline) processTrace(
    ctx context.Context,
    span *models.Span,
) error

// processEvents stores events in cold storage.
func (p *Pipeline) processEvents(
    ctx context.Context,
    events []models.Event,
) error

// runWindowFlush periodically checks for closed windows and flushes results.
//
// Runs every 1 second. When windows close:
//   1. Aggregates closed windows
//   2. Enriches aggregated results
//   3. Publishes to "metrics.aggregated" topic
//   4. Stores in warm storage (QuestDB)
func (p *Pipeline) runWindowFlush(ctx context.Context)

// runDownsampling runs the downsampling pipeline periodically.
//
// Runs every 1 hour. Applies retention rules and creates lower-resolution data.
func (p *Pipeline) runDownsampling(ctx context.Context)

// runGraphCleanup removes stale nodes from the dependency graph.
//
// Runs every 1 minute. Removes nodes not seen for 5 minutes.
// Publishes removed nodes as topology changes.
func (p *Pipeline) runGraphCleanup(ctx context.Context)

// runSnapshot periodically snapshots window state and graph to Dragonfly.
//
// Runs every 30 seconds for windows, every 60 seconds for graph.
func (p *Pipeline) runSnapshot(ctx context.Context)

// Health returns the current pipeline health status.
func (p *Pipeline) Health() PipelineHealth

// PipelineHealth represents the health of the pipeline.
type PipelineHealth struct {
    Status           string           `json:"status"`  // "healthy", "degraded", "unhealthy"
    AggregatorHealth AggregatorHealth `json:"aggregator"`
    CorrelatorHealth CorrelatorHealth `json:"correlator"`
    EnricherHealth   EnricherHealth   `json:"enricher"`
    Uptime           time.Duration    `json:"uptime"`
    MessagesProcessed int64           `json:"messages_processed"`
    ErrorsTotal      int64            `json:"errors_total"`
}
```

### 6.3 Pipeline Main Entry Point

**File:** `cluster/cmd/pipeline/main.go` (~200 LOC, NEW)

```go
// package main

// main is the entry point for the Paryty Pipeline service.
//
// The Pipeline service replaces the separate aggregator, correlator,
// and enricher binaries with a single unified processing pipeline.
//
// ENVIRONMENT VARIABLES:
//   - REDPANDA_URL: Redpanda broker addresses (default: "localhost:9092")
//   - DRAGONFLY_URL: Dragonfly address (default: "redis://localhost:6379")
//   - QUESTDB_URL: QuestDB ILP address (default: "localhost:9009")
//   - SEAWEEDFS_URL: SeaweedFS endpoint (default: "http://localhost:8333")
//   - LOG_LEVEL: Log level (default: "info")
//   - PIPELINE_CONSUMER_GROUP: Consumer group (default: "paryty-pipeline")
//   - AGGREGATION_WINDOWS: Comma-separated window sizes (default: "1m,5m,1h,1d")
//   - GRACE_PERIOD: Window grace period (default: "30s")
//   - TOP_N_SIZE: Top-N tracker size (default: "10")
func main() {
    // 1. Initialize logger (zap.NewProduction)
    // 2. Load config from environment variables
    // 3. Initialize storage (Dragonfly, QuestDB, SeaweedFS)
    // 4. Initialize stream engine (Redpanda)
    // 5. Create pipeline with all components
    // 6. Start pipeline
    // 7. Wait for SIGINT/SIGTERM
    // 8. Graceful shutdown (30s timeout)
}
```

### 6.4 Data Flow Diagram

```
Redpanda Topics                    Pipeline Service                     Storage
─────────────                     ─────────────────                    ───────

metrics.raw ─────┐
                 │
network.events ──┤
                 ├──→ [Consumer] ──→ [Aggregate] ──→ [Correlate] ──→ [Enrich] ──→ Dragonfly (hot)
traces ──────────┤                                      │              │           QuestDB (warm)
                 │                                      │              │           SeaweedFS (cold)
events ──────────┘                                      │              │
                                                        ▼              ▼
                                              topology.changes    metrics.aggregated
                                              (Redpanda)          (Redpanda)
```

### 6.5 Message Processing Flow

```
1. Consumer receives message from Redpanda
2. Route by topic:
   a. metrics.raw → MetricBatch
      i.   Parse JSON → models.MetricBatch
      ii.  STAGE 1: aggregator.Process(batch)
           - Add to tumbling windows
           - Update Top-N
           - Collect closed windows → []AggregatedMetric
      iii. STAGE 2: correlator.Correlate(batch, bufferedNetworkEvents)
           - Map processes to services
           - Update dependency graph
           - Detect topology changes
      iv.  STAGE 3: enricher.EnrichBatch(batch, correlationResult)
           - Inject agent labels
           - Propagate service names
      v.   STORE: store.StoreMetricBatch(enrichedBatch)
      vi.  PUBLISH: producer.Publish("metrics.enriched", enrichedBatch)
      vii. If closed windows:
           - enricher.EnrichAggregated(aggregatedMetrics)
           - producer.Publish("metrics.aggregated", enrichedMetrics)
           - store.StoreAggregated(enrichedMetrics, warm)
      viii. If topology changes:
           - enricher.EnrichTopology(changes)
           - producer.Publish("topology.changes", enrichedChanges)
           - store.SetTopology(graph, hot)
   
   b. network.events → []NetworkEvent
      i. Parse JSON → []models.NetworkEvent
      ii. correlator.BufferNetworkEvents(events)
   
   c. traces → Span
      i. Parse JSON → models.Span
      ii. enricher.EnrichSpan(span, agentInfo)
      iii. store.StoreSpan(enrichedSpan)
   
   d. events → []Event
      i. Parse JSON → []models.Event
      ii. store.StoreEvents(events)
```

---

## 7. Configuration Changes

### 7.1 Cluster Config

**File:** `configs/cluster/cluster.yaml` — Add pipeline section:

```yaml
# Existing sections (keep):
# ingestion, stream_engine, storage, query

# REPLACE processing section:
processing:
  pipeline:
    consumer_group: "paryty-pipeline"
    input_topics:
      - "paryty.metrics.raw"
      - "paryty.network.events"
      - "paryty.traces"
      - "paryty.events"
    batch_size: 100
    batch_timeout: "1s"
    health_check_interval: "10s"
  
  aggregator:
    window_sizes:
      - "1m"
      - "5m"
      - "1h"
      - "1d"
    grace_period: "30s"
    snapshot_interval: "30s"
    top_n_size: 10
  
  correlator:
    stale_node_timeout: "5m"
    graph_snapshot_interval: "60s"
    event_buffer_size: 10000
    correlation_window: "30s"
  
  enricher:
    standard_label_keys:
      - "env"
      - "region"
      - "team"
      - "service"
      - "version"
    max_labels_per_metric: 20
    label_prefix: "agent."
  
  downsampler:
    rules:
      - source_window: "1m"
        target_window: "5m"
        retention_days: 7
      - source_window: "5m"
        target_window: "1h"
        retention_days: 30
      - source_window: "1h"
        target_window: "1d"
        retention_days: 90
    run_interval: "1h"
```

### 7.2 Topic Configuration

Add to `cluster/internal/stream/topics.go`:

```go
// EnsureTopics creates all required topics for Phase 3.
func (m *TopicManager) EnsureTopics(ctx context.Context) error {
    topics := []struct {
        Name       string
        Partitions int32
        Replication int16
    }{
        {TopicMetricsRaw, 12, 1},
        {TopicMetricsAgg, 12, 1},
        {TopicTraces, 6, 1},
        {TopicEvents, 6, 1},
        {TopicNetworkEvents, 12, 1},
        {TopicTopologyChanges, 3, 1},
        {TopicAlerts, 3, 1},
        {TopicDLQ, 3, 1},
        // New Phase 3 topics:
        {TopicMetricsEnriched, 12, 1},
        {TopicCorrelations, 6, 1},
        {TopicDependencyGraph, 3, 1},
    }
    
    for _, t := range topics {
        if err := m.EnsureTopic(ctx, t.Name, t.Partitions, t.Replication); err != nil {
            return fmt.Errorf("ensure topic %s: %w", t.Name, err)
        }
    }
    return nil
}
```

---

## 8. Verification Gates

### Gate 1: Window Aggregation Correctness (Automated)

```go
// TestTumblingWindow_BasicAggregation
// 1. Create aggregator with 1m window
// 2. Send 5 CPU metrics within same 1m window
// 3. Advance time past window close + grace period
// 4. Verify: avg, min, max, p50, p90, p99 are correct
// 5. Verify: window is purged after processing

// TestTumblingWindow_LateData
// 1. Create aggregator with 1m window, 30s grace period
// 2. Send metrics at T+0s (within window)
// 3. Advance to T+65s (window closed, within grace)
// 4. Send late metric
// 5. Verify: late metric is included in window aggregation
// 6. Advance to T+95s (grace period expired)
// 7. Send metric
// 8. Verify: metric goes to NEXT window, not the closed one

// TestTumblingWindow_MultipleWindows
// 1. Create aggregator with 1m, 5m windows
// 2. Send metrics over 6 minutes
// 3. Verify: 6 one-minute windows produced
// 4. Verify: 1 five-minute window produced (covering minutes 0-4)
// 5. Verify: another five-minute window started (minute 5+)

// TestTumblingWindow_EmptyWindow
// 1. Create aggregator with 1m window
// 2. Skip a full minute (no data)
// 3. Advance past window close
// 4. Verify: no aggregated metric emitted for empty window
```

### Gate 2: Top-N Accuracy (Automated)

```go
// TestTopN_BasicRanking
// 1. Create TopN tracker with N=5
// 2. Send 10 process metrics with different CPU values
// 3. Verify: top 5 returned in correct order
// 4. Verify: values are correct

// TestTopN_UpdateExisting
// 1. Create TopN tracker with N=3
// 2. Add 3 processes: A=10, B=20, C=30
// 3. Update A to 100
// 4. Verify: top 3 is now A=100, C=30, B=20

// TestTopN_NewEntryBeatsOld
// 1. Create TopN tracker with N=2
// 2. Add 3 processes: A=10, B=20, C=30
// 3. Verify: top 2 is C=30, B=20 (A is evicted)
// 4. Update A to 100
// 5. Verify: top 2 is A=100, C=30 (B is evicted)
```

### Gate 3: Dependency Graph Correctness (Automated)

```go
// TestGraph_AddNode
// 1. Add 3 nodes
// 2. Verify: graph has 3 nodes, 0 edges
// 3. Verify: GetChanges returns 3 "node_added" changes

// TestGraph_AddEdge
// 1. Add 2 nodes and 1 edge between them
// 2. Verify: graph has 2 nodes, 1 edge
// 3. Verify: GetNeighbors returns correct neighbors

// TestGraph_UpdateExistingEdge
// 1. Add edge with frequency=1
// 2. Update same edge with frequency=2
// 3. Verify: edge frequency is 2 (not 2 separate edges)

// TestGraph_StaleNodeRemoval
// 1. Add 3 nodes with different LastSeen times
// 2. Call RemoveStaleNodes(maxAge=5m)
// 3. Verify: only fresh nodes remain

// TestGraph_SnapshotRestore
// 1. Build graph with 10 nodes and 15 edges
// 2. Snapshot to Dragonfly
// 3. Create new graph, restore from Dragonfly
// 4. Verify: all nodes and edges match

// TestGraph_ShortestPath
// 1. Build graph: A→B→C→D
// 2. Verify: GetPath(A, D) returns [A, B, C, D]
// 3. Add shortcut: A→D
// 4. Verify: GetPath(A, D) returns [A, D]
// 5. Verify: GetPath(A, E) returns [] (no path)

// TestGraph_GetChanged
// 1. Add 5 nodes
// 2. GetChanges() → returns 5 changes
// 3. GetChanges() → returns 0 changes (cleared)
// 4. Add 2 more nodes
// 5. GetChanges() → returns 2 changes
```

### Gate 4: Service Mapping (Automated)

```go
// TestServiceMap_DefaultRules
// 1. Create ServiceMap with default rules
// 2. Verify: "nginx-worker" → "nginx"
// 3. Verify: "postgres" → "postgresql"
// 4. Verify: "redis-server" → "redis"
// 4. Verify: "my-custom-app" → "my-custom-app" (fallback)

// TestServiceMap_CustomOverride
// 1. Create ServiceMap with defaults
// 2. Add custom: "nginx" → "my-nginx-gateway"
// 3. Verify: "nginx-worker" → "my-nginx-gateway"

// TestServiceMap_RegexRule
// 1. Create ServiceMap with regex rule: "app-.*-service" → "app-service"
// 2. Verify: "app-auth-service" → "app-service"
// 3. Verify: "app-payment-service" → "app-service"
```

### Gate 5: Enrichment Correctness (Automated)

```go
// TestEnricher_LabelInjection
// 1. Create enricher with standard labels ["env", "region"]
// 2. Agent has labels: {"env": "prod", "region": "us-east-1", "team": "platform"}
// 3. Enrich a metric batch
// 4. Verify: all metrics have "agent.env=prod", "agent.region=us-east-1"
// 5. Verify: "agent.team=platform" is also included

// TestEnricher_CardinalityLimit
// 1. Create enricher with MaxLabelsPerMetric=5
// 2. Agent has 10 labels
// 3. Enrich a metric batch
// 4. Verify: no metric has more than 5 labels
// 5. Verify: standard labels are always included (priority)

// TestEnricher_ServiceNamePropagation
// 1. Create enricher
// 2. Correlation result has: {"nginx:1234": "nginx-gateway"}
// 3. Enrich metric batch with process PID=1234, name="nginx"
// 4. Verify: metric has "service.name=nginx-gateway"

// TestEnricher_AgentCacheHit
// 1. Create enricher with LRU cache
// 2. Enrich batch for agent-1
// 3. Verify: first call hits Dragonfly
// 4. Enrich another batch for agent-1
// 5. Verify: second call hits cache (no Dragonfly call)

// TestEnricher_MissingAgent
// 1. Create enricher
// 2. Enrich batch for unknown agent
// 3. Verify: no error, batch is enriched with minimal metadata
```

### Gate 6: Pipeline Integration (Automated)

```go
// TestPipeline_EndToEnd
// 1. Start pipeline with in-memory storage
// 2. Publish MetricBatch to "metrics.raw"
// 3. Wait for processing
// 4. Verify: enriched batch stored in hot storage
// 5. Verify: aggregated metrics published to "metrics.aggregated"
// 6. Verify: agent labels injected

// TestPipeline_WindowFlush
// 1. Start pipeline with 1m window
// 2. Publish 5 metrics within same window
// 3. Advance time past window close + grace
// 4. Trigger window flush
// 5. Verify: aggregated metrics published
// 6. Verify: aggregated metrics stored in warm storage

// TestPipeline_NetworkEventBuffering
// 1. Start pipeline
// 2. Publish NetworkEvent to "network.events"
// 3. Publish MetricBatch to "metrics.raw"
// 4. Verify: correlator uses buffered network events
// 5. Verify: topology changes detected

// TestPipeline_TopologyChanges
// 1. Start pipeline
// 2. Publish MetricBatch with processes
// 3. Publish NetworkEvent with TCP connections
// 4. Verify: dependency graph has nodes and edges
// 5. Verify: topology changes published to "topology.changes"

// TestPipeline_GracefulShutdown
// 1. Start pipeline
// 2. Publish metrics
// 3. Send SIGTERM
// 4. Verify: in-flight processing completes
// 5. Verify: window state snapshotted
// 6. Verify: graph snapshotted
// 7. Verify: process exits cleanly

// TestPipeline_CrashRecovery
// 1. Start pipeline, process some metrics
// 2. Force snapshot
// 3. Stop pipeline (simulate crash)
// 4. Start new pipeline instance
// 5. Verify: window state restored from snapshot
// 6. Verify: graph restored from snapshot
```

### Gate 7: Downsampling (Automated)

```go
// TestDownsampler_1mTo5m
// 1. Insert 5 minutes of 1m aggregated metrics into QuestDB
// 2. Run downsampler with rule: 1m→5m, retention=7 days
// 3. Verify: 5m aggregated metric created
// 4. Verify: 5m metric is avg of 1m avgs, max of 1m maxes, etc.

// TestDownsampler_RetentionEnforcement
// 1. Insert 1m metrics older than 7 days
// 2. Run downsampler
// 3. Verify: old 1m metrics deleted
// 4. Verify: 5m downsampled metrics created
```

### Gate 8: Performance Benchmarks (Automated)

```go
// BenchmarkAggregator_1000Metrics
// 1. Generate 1000 MetricBatch messages
// 2. Process all through aggregator
// 3. Target: < 50ms per message (p99)

// BenchmarkCorrelator_1000Events
// 1. Generate 1000 NetworkEvent batches
// 2. Process through correlator
// 3. Target: < 20ms per batch (p99)

// BenchmarkEnricher_1000Batches
// 1. Generate 1000 MetricBatch messages
// 2. Enrich all through enricher
// 3. Target: < 5ms per batch (p99)

// BenchmarkPipeline_Throughput
// 1. Publish 10,000 messages to pipeline
// 2. Measure total processing time
// 3. Target: > 1,000 messages/second

// BenchmarkWindowManager_10000Windows
// 1. Create 10,000 windows across 100 agents
// 2. Add values to all windows
// 3. Measure GetClosedWindows() latency
// 4. Target: < 10ms for GetClosedWindows()

// BenchmarkGraph_1000Nodes
// 1. Build graph with 1000 nodes, 5000 edges
// 2. Benchmark AddOrUpdateNode, AddOrUpdateEdge
// 3. Benchmark GetNeighbors, GetPath
// 4. Target: < 1ms for any single operation
```

### Gate 9: Manual Verification (Optional)

```
□ Start pipeline with docker-compose
□ Publish sample metrics via test producer
□ Verify aggregated metrics appear in Redpanda UI
□ Verify enriched metrics stored in QuestDB (query via HTTP)
□ Verify dependency graph in Dragonfly (redis-cli GET paryty:pipeline:graph:snapshot)
□ Verify window state in Dragonfly (redis-cli GET paryty:pipeline:window:snapshot)
□ Kill pipeline, restart — verify state recovery
□ Run load test (1000 msg/s) — verify no message loss
□ Check pipeline health endpoint — verify all components healthy
□ Verify downsampling creates 5m metrics from 1m data
```

---

## 9. Performance Targets

### 9.1 Latency Targets

| Operation | Target (p50) | Target (p99) | Measurement |
|-----------|-------------|-------------|-------------|
| Message processing (end-to-end) | < 10ms | < 50ms | Time from Redpanda receive to storage write |
| Window aggregation | < 5ms | < 20ms | Time to aggregate a closed window |
| Correlation | < 10ms | < 30ms | Time to correlate batch + events |
| Enrichment | < 2ms | < 5ms | Time to inject labels |
| Graph operation (add/update) | < 1ms | < 5ms | Single node or edge operation |
| Window flush (all closed windows) | < 100ms | < 500ms | All closed windows in one cycle |
| Graph snapshot | < 500ms | < 2s | Serialize entire graph to Dragonfly |

### 9.2 Throughput Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| Messages/second | > 2,000 | Steady-state pipeline throughput |
| Metric batches/second | > 1000 | MetricBatch messages processed |
| Network events/second | > 4,000 | NetworkEvent messages buffered |
| Windows managed | > 20,000 | Concurrent open windows (100 agents × 4 sizes × 25 metrics) |
| Graph nodes | > 10,000 | Maximum graph size before degradation |

### 9.3 Resource Targets

| Resource | Target | Notes |
|----------|--------|-------|
| Memory (pipeline) | < 512 MB | Including window buffers and graph |
| Memory (per window) | < 1 KB | 100 values × 8 bytes + overhead |
| Memory (per graph node) | < 500 bytes | Node + adjacency list entry |
| CPU (idle) | < 1% | When no messages are being processed |
| CPU (busy) | < 50% | At 1,000 msg/s |
| Disk (snapshots) | < 10 MB | Window + graph snapshots in Dragonfly |

---

## 10. Contingency & Rollback

### 10.1 Rollback Strategy

**Scenario 1: Pipeline binary fails to start**
- Keep old aggregator/correlator/enricher binaries
- Pipeline is a new binary, not a replacement of existing cmd directories
- Switch back: redeploy old binaries, point to same Redpanda topics

**Scenario 2: Window state corruption**
- Pipeline detects corrupted snapshot on startup
- Falls back to empty state (no windows)
- Rebuilds windows from incoming data within 1 minute

**Scenario 3: Graph state corruption**
- Pipeline detects corrupted graph snapshot on startup
- Falls back to empty graph
- Rebuilds graph from incoming data within 5 minutes

**Scenario 4: Memory pressure from too many windows**
- Implement window eviction: oldest windows purged first
- Reduce window sizes (drop 1d window, keep 1m/5m/1h)
- Increase grace period to reduce window churn

**Scenario 5: Downsampling produces incorrect results**
- Downsampling is idempotent — can be re-run
- Keep source data for full retention period before deleting
- Alert on downsampling anomalies (value range checks)

### 10.2 Degradation Modes

| Mode | Trigger | Behavior |
|------|---------|----------|
| Full | All components healthy | Aggregate → Correlate → Enrich → Store |
| No Correlation | Graph snapshot fails | Skip correlation, still aggregate and enrich |
| No Enrichment | Agent cache fails | Skip enrichment, still aggregate and correlate |
| No Downsampling | QuestDB unavailable | Skip downsampling, retry next cycle |
| Minimal | Multiple failures | Pass-through mode: store raw data, no processing |

### 10.3 Monitoring Alerts

| Alert | Condition | Severity |
|-------|-----------|----------|
| Pipeline Lag | Consumer lag > 10,000 messages | Warning |
| Pipeline Lag Critical | Consumer lag > 100,000 messages | Critical |
| Window Flush Delay | Window flush > 5 seconds | Warning |
| Graph Size | Graph nodes > 10,000 | Warning |
| Snapshot Failure | Any snapshot fails | Warning |
| Processing Error Rate | Error rate > 1% | Warning |
| Processing Error Rate Critical | Error rate > 10% | Critical |
| Memory Usage | Pipeline memory > 1 GB | Warning |
| Downsampling Failure | Downsampling cycle fails | Warning |

---

## 11. Appendices

### Appendix A: Helper Functions

**File:** `cluster/internal/processing/helpers.go` (~150 LOC)

```go
// package processing

// avg computes the arithmetic mean of a slice of float64.
func avg(values []float64) float64

// percentile computes the p-th percentile using linear interpolation.
func percentile(values []float64, p float64) float64

// median computes the 50th percentile.
func median(values []float64) float64

// stddev computes the standard deviation.
func stddev(values []float64) float64

// sum computes the sum of a slice of float64.
func sum(values []float64) float64

// RingBuffer is a fixed-size circular buffer.
//
// Used for buffering network events in the correlator.
// When full, oldest entries are overwritten.
type RingBuffer struct {
    data   []interface{}
    size   int
    head   int
    tail   int
    count  int
    mu     sync.RWMutex
}

func NewRingBuffer(size int) *RingBuffer
func (rb *RingBuffer) Push(item interface{})
func (rb *RingBuffer) GetAll() []interface{}
func (rb *RingBuffer) Len() int
func (rb *RingBuffer) Clear()
```

### Appendix B: Internal Metrics

```go
// AggregatorMetrics tracks aggregator performance.
type AggregatorMetrics struct {
    WindowsOpen     prometheus.Gauge
    WindowsClosed   prometheus.Counter
    ValuesProcessed prometheus.Counter
    FlushDuration   prometheus.Histogram
    FlushSize       prometheus.Histogram
}

// CorrelatorMetrics tracks correlator performance.
type CorrelatorMetrics struct {
    GraphNodes       prometheus.Gauge
    GraphEdges       prometheus.Gauge
    CorrelationsTotal prometheus.Counter
    TopologyChanges   prometheus.Counter
    CorrelationDuration prometheus.Histogram
}

// EnricherMetrics tracks enricher performance.
type EnricherMetrics struct {
    EnrichmentsTotal prometheus.Counter
    CacheHits        prometheus.Counter
    CacheMisses      prometheus.Counter
    EnrichmentDuration prometheus.Histogram
    LabelCardinality prometheus.Histogram
}

// PipelineMetrics tracks overall pipeline performance.
type PipelineMetrics struct {
    MessagesReceived  prometheus.Counter
    MessagesProcessed prometheus.Counter
    MessagesErrors    prometheus.Counter
    ProcessingDuration prometheus.Histogram
    ConsumerLag       prometheus.Gauge
}
```

### Appendix C: Dragonfly Key Schema

```
# Window snapshots
paryty:pipeline:window:snapshot          → JSON (compressed with Zstd)

# Graph snapshots
paryty:pipeline:graph:snapshot           → JSON (compressed with Zstd)

# Agent state (existing, used by enricher)
paryty:agent:{agent_id}:state            → JSON (AgentInfo)

# Topology (existing, updated by pipeline)
paryty:topology                          → JSON (Topology)

# Pipeline health
paryty:pipeline:health                   → JSON (PipelineHealth)

# Downsampling cursor
paryty:pipeline:downsampler:cursor       → JSON (last processed timestamp per rule)
```

### Appendix D: Files to Create/Modify

| File | Action | LOC | Description |
|------|--------|-----|-------------|
| `cluster/internal/processing/window.go` | CREATE | ~400 | Tumbling window state manager |
| `cluster/internal/processing/window_test.go` | CREATE | ~300 | Window tests |
| `cluster/internal/processing/aggregator.go` | REWRITE | ~800 | Windowed aggregation engine |
| `cluster/internal/processing/aggregator_test.go` | CREATE | ~400 | Aggregation tests |
| `cluster/internal/processing/correlator.go` | REWRITE | ~800 | Correlation with dependency graph |
| `cluster/internal/processing/correlator_test.go` | CREATE | ~400 | Correlation tests |
| `cluster/internal/processing/enricher.go` | ENHANCE | ~500 | Label propagation and service mapping |
| `cluster/internal/processing/enricher_test.go` | UPDATE | ~300 | Enrichment tests |
| `cluster/internal/processing/graph.go` | CREATE | ~500 | Dependency graph |
| `cluster/internal/processing/graph_test.go` | CREATE | ~400 | Graph tests |
| `cluster/internal/processing/topn.go` | CREATE | ~300 | Top-N tracker |
| `cluster/internal/processing/topn_test.go` | CREATE | ~200 | Top-N tests |
| `cluster/internal/processing/downsampler.go` | CREATE | ~300 | Downsampling engine |
| `cluster/internal/processing/downsampler_test.go` | CREATE | ~200 | Downsampling tests |
| `cluster/internal/processing/servicemap.go` | CREATE | ~300 | Service mapping |
| `cluster/internal/processing/servicemap_test.go` | CREATE | ~200 | Service mapping tests |
| `cluster/internal/processing/pipeline.go` | CREATE | ~400 | Pipeline orchestrator |
| `cluster/internal/processing/pipeline_test.go` | CREATE | ~500 | Pipeline integration tests |
| `cluster/internal/processing/helpers.go` | CREATE | ~150 | Shared utilities |
| `cluster/cmd/pipeline/main.go` | CREATE | ~200 | Pipeline service entry point |
| `cluster/internal/stream/topics.go` | MODIFY | +15 | New topic constants |
| `configs/cluster/cluster.yaml` | MODIFY | +50 | Pipeline configuration |
| **TOTAL** | | **~7,450** | |

### Appendix E: Go Coding Discipline

```
1. ERROR HANDLING
   - All errors are wrapped with context: fmt.Errorf("operation: %w", err)
   - Non-fatal errors are logged and skipped (enrichment, downsampling)
   - Fatal errors cause graceful shutdown (stream connection, storage)
   - DLQ for messages that fail processing after 3 retries

2. CONCURRENCY
   - sync.RWMutex for graph and service map (read-heavy workloads)
   - sync.Map for window state (write-heavy, per-key locking)
   - Channel-based communication between pipeline stages
   - Context cancellation for all goroutines

3. TESTING
   - Table-driven tests for all public functions
   - Test helpers in helpers_test.go (newTestLogger, newTestBatch, etc.)
   - In-memory implementations of DragonflyClient, QuestDBClient for testing
   - Integration tests use testcontainers for Redpanda

4. LOGGING
   - Structured logging with zap
   - Debug: per-message processing details
   - Info: window flushes, topology changes, pipeline lifecycle
   - Warn: malformed messages, missing agent info, late data
   - Error: storage failures, processing errors
   - Fatal: stream connection failure, storage connection failure

5. METRICS
   - Prometheus metrics for all components
   - Histograms for latencies, counters for throughput, gauges for state
   - Labels: agent_id, topic, metric_name, window_size

6. CONFIGURATION
   - YAML config with environment variable overrides
   - Sensible defaults for all settings
   - Validation at startup (fail fast)
   - Hot reload not required (restart for config changes)

7. DOCUMENTATION
   - Godoc comments on all exported types and functions
   - Package-level doc comment explaining purpose
   - Example tests for complex functions
```

### Appendix F: Related Phase Specifications

| Phase | Spec | Description |
|-------|------|-------------|
| Phase 1 | `docs/phase-1-hardened-spec.md` | Foundation & Data Pipeline (Ingestion, Storage, Stream) |
| Phase 2 | `docs/phase-2-hardened-spec.md` | eBPF Network Observer (TCP, DNS, HTTP) |
| Phase 3 | **This document** | Processing Pipeline (Aggregator, Correlator, Enricher) |
| Phase 4 | TBD | DB Protocol Inspection (PostgreSQL, MySQL, Redis) |
| Phase 5 | TBD | Query & Intelligence (REST API, WebSocket, GraphQL, SSE) |
| Phase 6 | TBD | Frontend Visualization (Dashboard, Topology, Alerts) |
| Phase 7 | TBD | Security & RBAC (Auth, Authorization, Audit) |
| Phase 8 | TBD | Production Hardening (HA, Multi-tenant, Disaster Recovery) |

---

**END OF PHASE 3 HARDENED SPECIFICATION**
