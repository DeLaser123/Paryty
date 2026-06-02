# Phase 4 Hardened Specification — Storage Layer Completion & DB Inspection

**Version:** 1.0.0
**Status:** LOCKED — All architectural decisions finalized
**Target LOC:** ~13,000 (Go + Rust + C eBPF)
**Estimated Effort:** 5-7 weeks for a senior engineer

---

## Table of Contents

1. [Phase 4 Overview & Decisions](#1-phase-4-overview--decisions)
2. [Pre-Phase Setup](#2-pre-phase-setup)
3. [Layer 14: Hot Store Productionization](#3-layer-14-hot-store-productionization)
4. [Layer 15: Warm Store Productionization](#4-layer-15-warm-store-productionization)
5. [Layer 16: Cold Store & Timeline Snapshots](#5-layer-16-cold-store--timeline-snapshots)
6. [Layer 17: eBPF Database Protocol Inspection](#6-layer-17-ebpf-database-protocol-inspection)
7. [Configuration Changes](#7-configuration-changes)
8. [Verification Gates](#8-verification-gates)
9. [Performance Targets](#9-performance-targets)
10. [Contingency & Rollback](#10-contingency--rollback)
11. [Appendices](#11-appendices)

---

## 1. Phase 4 Overview & Decisions

### 1.1 What Phase 4 Delivers

Phase 4 makes the 3-tier storage system production-ready and adds eBPF database protocol inspection. The storage layer moves from basic CRUD operations to production-grade features: optimistic concurrency, high-throughput ingestion, query optimization, retention management, and timeline snapshots for the digital twin.

**Before Phase 4:** Basic hot/warm/cold storage with simple CRUD. DB inspection is a stub.

**After Phase 4:** Production-grade storage with atomic topology updates, hybrid ILP+REST ingestion, timeline snapshots with event-sourced inter-snapshot replay, LRU-cached cold store retrieval, and full PostgreSQL/MySQL/Redis query extraction via eBPF.

### 1.2 Architectural Decisions (LOCKED)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Hot Store Concurrent Topology Updates | **A — WATCH/MULTI/EXEC** | Optimistic locking. Zero overhead when no conflict, automatic retry on contention. |
| 2 | QuestDB Ingestion Method | **C — Hybrid ILP + REST** | ILP for real-time metrics (low latency), REST API for batch/backfill (high throughput). |
| 3 | Timeline Snapshot Architecture | **A+C — Full snapshots + Event log** | Full snapshots every 5 minutes as checkpoints. Redpanda event log for inter-snapshot replay. |
| 4 | Cold Store Query Cache | **B — Dragonfly LRU Cache** | Cache recently accessed snapshots in Dragonfly. ~100 snapshots (~1GB) covers 90% of queries. |
| 5 | eBPF DB Protocol Inspection | **B — Full query extraction + TLS fallback** | Parse PostgreSQL/MySQL/Redis protocols for full query visibility. Fall back to connection-only for TLS. |

### 1.3 What Gets Built

| Layer | Component | Language | LOC | Description |
|-------|-----------|----------|-----|-------------|
| 14 | Hot Store Productionization | Go | ~3,000 | Atomic topology updates, sorted set metrics, alert state machine, connection tracking |
| 15 | Warm Store Productionization | Go | ~4,000 | Schema migration, ILP+REST hybrid ingestion, query optimization, retention management |
| 16 | Cold Store & Timeline | Go | ~3,000 | Timeline snapshots (full + event log), LRU-cached retrieval, snapshot compaction |
| 17 | eBPF DB Inspection | Rust + C | ~3,000 | PostgreSQL/MySQL/Redis protocol parsing, TLS fallback, ring buffer events |

### 1.4 Existing Code Assessment

**Hot Store (`cluster/internal/storage/hot/dragonfly.go`, 213 lines):**
- Basic SET/GET with JSON serialization
- 5-minute TTL on all keys
- No sorted sets, no atomic operations, no WATCH/MULTI/EXEC
- Keys: `paryty:topology:current`, `paryty:metrics:{agent_id}:latest`, etc.

**Warm Store (`cluster/internal/storage/warm/questdb.go`, 262 lines):**
- pgxpool connection, basic INSERT and SELECT
- No ILP client, no batch optimization, no schema migration
- Tables: cpu_metrics, memory_metrics, disk_metrics, network_metrics, spans, metrics, aggregated_metrics

**Cold Store (`cluster/internal/storage/cold/seaweedfs.go`, 211 lines):**
- minio-go client with basic PutObject/GetObject
- Date-based key schema: `metrics/2024/01/15/agent-id.json`
- No compression, no snapshots, no caching, no retrieval optimization

**DB Inspector (`agent/src/ebpf/db_inspector.rs`, 43 lines):**
- Stub with `inspect()` returning `Ok(None)`
- DbQueryEvent struct already defined (protocol, query, query_type, latency_ms, row_count, etc.)
- Proto already defined in `ebpf.proto` (DbQueryEvent message with all fields)

---

## 2. Pre-Phase Setup

### 2.1 Go Dependencies

Add to `cluster/go.mod`:

```
# Already present (verify):
github.com/redis/go-redis/v9       # Dragonfly client
github.com/jackc/pgx/v5            # QuestDB PostgreSQL wire protocol
github.com/minio/minio-go/v7       # SeaweedFS S3 client

# New for Phase 4:
github.com/klauspost/compress      # Zstd compression for snapshots (already in use via franz-go, verify)
github.com/hashicorp/golang-lru/v2 # LRU cache for cold store retrieval
```

### 2.2 Rust Dependencies

Add to `agent/Cargo.toml`:

```toml
# Already present (verify):
libbpf-rs = "0.24"      # eBPF Rust bindings
libbpf-sys = "1.3"      # eBPF C bindings
anyhow = "1"            # Error handling

# New for Phase 4:
pgwire = "0.20"         # PostgreSQL wire protocol parser (for protocol detection)
# OR implement raw byte parsing (preferred for eBPF — no external deps in kernel path)
```

### 2.3 New Package Structure

```
cluster/internal/storage/
├── hot/
│   ├── dragonfly.go           # ENHANCE — Atomic ops, sorted sets, WATCH/MULTI/EXEC
│   ├── dragonfly_test.go      # NEW — Concurrency and atomic operation tests
│   ├── topology_ops.go        # NEW — Topology state management (1,000 LOC)
│   ├── topology_ops_test.go   # NEW
│   ├── metrics_ops.go         # NEW — Sorted set metrics, range queries (1,000 LOC)
│   ├── metrics_ops_test.go    # NEW
│   ├── alert_ops.go           # NEW — Alert state machine (500 LOC)
│   ├── alert_ops_test.go      # NEW
│   └── connection_ops.go      # NEW — Agent connection tracking (500 LOC)
│   └── connection_ops_test.go # NEW
├── warm/
│   ├── questdb.go             # ENHANCE — Schema migration, hybrid ingestion
│   ├── questdb_test.go        # UPDATE
│   ├── schema.go              # NEW — Schema definitions and migration (1,000 LOC)
│   ├── schema_test.go         # NEW
│   ├── ilp_writer.go          # NEW — ILP (InfluxDB Line Protocol) writer (800 LOC)
│   ├── ilp_writer_test.go     # NEW
│   ├── rest_writer.go         # NEW — REST API bulk insert (700 LOC)
│   ├── rest_writer_test.go    # NEW
│   ├── query_optimizer.go     # NEW — Query optimization, caching, downsampling (1,000 LOC)
│   ├── query_optimizer_test.go# NEW
│   └── retention.go           # NEW — Retention management (500 LOC)
│   └── retention_test.go      # NEW
├── cold/
│   ├── seaweedfs.go           # ENHANCE — Compression, parallel uploads
│   ├── seaweedfs_test.go      # UPDATE
│   ├── snapshot.go            # NEW — Timeline snapshot manager (1,500 LOC)
│   ├── snapshot_test.go       # NEW
│   ├── eventlog.go            # NEW — Redpanda event log indexer (800 LOC)
│   ├── eventlog_test.go       # NEW
│   ├── cache.go               # NEW — Dragonfly LRU cache for cold queries (700 LOC)
│   └── cache_test.go          # NEW
├── store.go                   # ENHANCE — New methods for snapshot, retention, cache
└── store_test.go              # UPDATE

agent/src/ebpf/
├── db_inspector.rs            # REWRITE — Full protocol parsing (1,500 LOC)
├── pg_protocol.rs             # NEW — PostgreSQL wire protocol parser (500 LOC)
├── mysql_protocol.rs          # NEW — MySQL protocol parser (500 LOC)
├── redis_protocol.rs          # NEW — Redis RESP protocol parser (400 LOC)
├── ebpf/
│   └── db_probe.c             # NEW — C eBPF program for DB packet capture (300 LOC)
└── common.h                   # MODIFY — Add db_event struct
```

---

## 3. Layer 14: Hot Store Productionization

### 3.1 Overview

The hot store (Dragonfly) is upgraded from basic key-value operations to production-grade features: atomic topology updates with optimistic locking, sorted set metrics for time-range queries, an alert state machine for lifecycle tracking, and connection tracking for agent health.

### 3.2 Topology State Management

**File:** `cluster/internal/storage/hot/topology_ops.go` (~1,000 LOC)

```go
// package hot

// TopologyOps provides atomic topology operations with optimistic locking.
type TopologyOps struct {
    client *Client
    logger *zap.Logger
}

// UpdateTopology atomically updates the topology using WATCH/MULTI/EXEC.
//
// OPTIMISTIC LOCKING FLOW:
//   1. WATCH paryty:topology:current
//   2. GET current topology
//   3. Apply mutation function
//   4. MULTI
//   5. SET new topology
//   6. EXEC
//   7. If EXEC fails (key changed), retry up to 3 times
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - mutate: function that takes current topology and returns modified topology
//
// ERRORS: returns error after 3 retries or if mutation function fails
func (t *TopologyOps) UpdateTopology(
    ctx context.Context,
    mutate func(current *models.Topology) (*models.Topology, error),
) error

// GetTopologyWithVersion retrieves topology with its version (for optimistic locking).
//
// RETURNS: topology and version string (Redis WATCH value)
func (t *TopologyOps) GetTopologyWithVersion(ctx context.Context) (*models.Topology, string, error)

// ApplyDiff applies a topology diff atomically.
//
// The diff is computed by the pipeline's dependency graph and contains
// only the changes (nodes/edges added/removed/updated).
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - diff: topology diff to apply
//
// ERRORS: returns error if diff is invalid or atomic update fails
func (t *TopologyOps) ApplyDiff(ctx context.Context, diff *TopologyDiff) error

// TopologyDiff represents changes to the topology.
type TopologyDiff struct {
    NodesAdded   []models.TopologyNode   `json:"nodes_added"`
    NodesRemoved []string                `json:"nodes_removed"` // Node IDs
    NodesUpdated []models.TopologyNode   `json:"nodes_updated"`
    EdgesAdded   []models.TopologyEdge   `json:"edges_added"`
    EdgesRemoved []string                `json:"edges_removed"` // Edge IDs
    EdgesUpdated []models.TopologyEdge   `json:"edges_updated"`
    Timestamp    time.Time               `json:"timestamp"`
    Version      string                  `json:"version"` // For optimistic locking
}
```

### 3.3 Live Metrics Optimization

**File:** `cluster/internal/storage/hot/metrics_ops.go` (~1,000 LOC)

```go
// package hot

// MetricsOps provides optimized metrics operations using sorted sets.
type MetricsOps struct {
    client *Client
    logger *zap.Logger
}

// StoreMetricTimeSeries stores a metric value in a sorted set.
//
// Key: paryty:metrics:ts:{agent_id}:{metric_name}
// Score: unix timestamp (float64 for sub-second precision)
// Value: JSON-encoded MetricValue
//
// This enables efficient range queries (ZRANGEBYSCORE) for the last N minutes.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - agentID: agent that produced the metric
//   - metricName: e.g. "cpu.usage_percent"
//   - value: metric value
//   - timestamp: when the metric was collected
//   - ttl: how long to keep the data (default 5 minutes)
func (m *MetricsOps) StoreMetricTimeSeries(
    ctx context.Context,
    agentID string,
    metricName string,
    value float64,
    timestamp time.Time,
    ttl time.Duration,
) error

// QueryMetricRange queries metrics within a time range from sorted sets.
//
// Uses ZRANGEBYSCORE to efficiently retrieve metrics between start and end.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - agentID: agent to query
//   - metricName: metric to query
//   - start: start of time range
//   - end: end of time range
//
// RETURNS: ordered slice of MetricValue (oldest first)
func (m *MetricsOps) QueryMetricRange(
    ctx context.Context,
    agentID string,
    metricName string,
    start time.Time,
    end time.Time,
) ([]MetricValue, error)

// MetricValue is a single metric data point.
type MetricValue struct {
    Value     float64   `json:"v"`
    Timestamp time.Time `json:"t"`
}

// PurgeExpiredMetrics removes metrics older than TTL.
//
// Called periodically (every 1 minute) to prevent memory bloat.
// Uses ZREMRANGEBYSCORE to efficiently remove old entries.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - maxAge: remove metrics older than this
//
// RETURNS: number of entries removed
func (m *MetricsOps) PurgeExpiredMetrics(ctx context.Context, maxAge time.Duration) (int64, error)
```

### 3.4 Alert State Machine

**File:** `cluster/internal/storage/hot/alert_ops.go` (~500 LOC)

```go
// package hot

// AlertState represents the lifecycle state of an alert.
type AlertState string

const (
    AlertStateFiring       AlertState = "firing"
    AlertStateResolved     AlertState = "resolved"
    AlertStateAcknowledged AlertState = "acknowledged"
    AlertStateSilenced     AlertState = "silenced"
)

// AlertOps provides alert lifecycle management.
type AlertOps struct {
    client *Client
    logger *zap.Logger
}

// TransitionAlert transitions an alert to a new state.
//
// VALID TRANSITIONS:
//   - firing → resolved, acknowledged, silenced
//   - acknowledged → firing, resolved, silenced
//   - silenced → firing, resolved
//   - resolved → (terminal, can only be re-fired as new alert)
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - alertID: unique alert identifier
//   - newState: target state
//   - reason: human-readable reason for transition
//
// ERRORS: returns error if transition is invalid or alert not found
func (a *AlertOps) TransitionAlert(
    ctx context.Context,
    alertID string,
    newState AlertState,
    reason string,
) error

// DeduplicateAlert checks if an alert with the same fingerprint already exists.
//
// Alert fingerprint is computed from: (rule_id, labels_hash)
// If the alert exists and is firing, returns the existing alert (no duplicate).
// If the alert exists and is resolved, creates a new firing alert.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - alert: alert to check
//
// RETURNS: true if this is a duplicate (already firing), false if new
func (a *AlertOps) DeduplicateAlert(ctx context.Context, alert *models.Alert) (bool, error)

// GetActiveAlertsByGroup retrieves active alerts grouped by service/tenant.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - groupBy: field to group by ("service", "tenant", "severity")
//
// RETURNS: map of group → alerts
func (a *AlertOps) GetActiveAlertsByGroup(
    ctx context.Context,
    groupBy string,
) (map[string][]models.Alert, error)
```

### 3.5 Connection Tracking

**File:** `cluster/internal/storage/hot/connection_ops.go` (~500 LOC)

```go
// package hot

// ConnectionInfo tracks an active agent connection.
type ConnectionInfo struct {
    AgentID     string    `json:"agent_id"`
    SessionID   string    `json:"session_id"`
    RemoteAddr  string    `json:"remote_addr"`
    ConnectedAt time.Time `json:"connected_at"`
    LastHeartbeat time.Time `json:"last_heartbeat"`
    Capabilities  []string  `json:"capabilities"`
}

// ConnectionOps provides agent connection tracking.
type ConnectionOps struct {
    client *Client
    logger *zap.Logger
}

// TrackConnection registers or updates an agent connection.
//
// Key: paryty:connections:{agent_id}
// TTL: 30 seconds (refreshed on each heartbeat)
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - info: connection information
func (c *ConnectionOps) TrackConnection(ctx context.Context, info *ConnectionInfo) error

// RefreshHeartbeat updates the last heartbeat time for an agent.
//
// Called on each Heartbeat RPC from the agent.
// Extends the TTL on the connection key.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - agentID: agent to refresh
func (c *ConnectionOps) RefreshHeartbeat(ctx context.Context, agentID string) error

// DetectDisconnectedAgents finds agents that haven't heartbeated recently.
//
// Scans all paryty:connections:* keys and checks last_heartbeat.
// Returns agents with last_heartbeat older than threshold.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - threshold: consider disconnected if no heartbeat for this long
//
// RETURNS: slice of disconnected ConnectionInfo
func (c *ConnectionOps) DetectDisconnectedAgents(
    ctx context.Context,
    threshold time.Duration,
) ([]ConnectionInfo, error)

// GetActiveConnections returns all active agent connections.
func (c *ConnectionOps) GetActiveConnections(ctx context.Context) ([]ConnectionInfo, error)
```

### 3.6 Hot Store Pseudo-Code: Atomic Topology Update

```
FUNCTION UpdateTopology(mutate func):
    retries = 0
    MAX_RETRIES = 3
    
    WHILE retries < MAX_RETRIES:
        // 1. WATCH the topology key
        WATCH "paryty:topology:current"
        
        // 2. Read current topology
        current = GET "paryty:topology:current"
        IF current == nil:
            current = empty Topology
        
        // 3. Apply mutation
        newTopology, err = mutate(current)
        IF err != nil:
            UNWATCH
            RETURN err
        
        // 4. Atomic write
        MULTI
        SET "paryty:topology:current" = serialize(newTopology) EX 300
        result = EXEC
        
        IF result == SUCCESS:
            RETURN nil  // Committed successfully
        
        // EXEC failed — key was modified by another client
        retries++
        backoff(retries * 10ms)  // 10ms, 20ms, 30ms
    
    RETURN error("topology update failed after 3 retries — high contention")
```

---

## 4. Layer 15: Warm Store Productionization

### 4.1 Overview

The warm store (QuestDB) is upgraded from basic INSERT/SELECT to production-grade features: schema migration, hybrid ILP+REST ingestion for high throughput, query optimization with caching, and automated retention management.

### 4.2 Schema Design & Migration

**File:** `cluster/internal/storage/warm/schema.go` (~1,000 LOC)

```go
// package warm

// SchemaVersion tracks the current schema version.
const SchemaVersion = 3

// SchemaDefinition holds all table definitions for QuestDB.
type SchemaDefinition struct {
    Version int
    Tables  []TableDefinition
}

// TableDefinition defines a QuestDB table.
type TableDefinition struct {
    Name        string
    Columns     []ColumnDefinition
    PartitionBy string  // "DAY", "HOUR", "NONE"
    TTL         string  // e.g. "30d" for retention
    Dedup       bool    // Whether to deduplicate rows
}

// ColumnDefinition defines a column in a QuestDB table.
type ColumnDefinition struct {
    Name     string
    Type     string  // "TIMESTAMP", "SYMBOL", "DOUBLE", "LONG", "STRING"
    Indexed  bool    // Whether to create a symbol index
    Nullable bool
}

// DefaultSchema returns the production schema for Paryty.
func DefaultSchema() SchemaDefinition

// SchemaManager handles schema creation and migration.
type SchemaManager struct {
    pool   *pgxpool.Pool
    logger *zap.Logger
}

// NewSchemaManager creates a new schema manager.
func NewSchemaManager(pool *pgxpool.Pool, logger *zap.Logger) *SchemaManager

// Migrate ensures the database schema matches the expected definition.
//
// PROCESS:
//   1. Query current schema version from paryty_schema_version table
//   2. If version < SchemaVersion, apply missing migrations
//   3. Each migration is a SQL script that alters tables
//   4. After migration, update schema version
//
// Migrations are idempotent — safe to run multiple times.
//
// PARAMETERS:
//   - ctx: context for cancellation
//
// ERRORS: returns error if migration fails
func (s *SchemaManager) Migrate(ctx context.Context) error

// EnsureTable creates a table if it doesn't exist.
//
// Uses QuestDB's CREATE TABLE IF NOT EXISTS with WAL enabled.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - def: table definition
func (s *SchemaManager) EnsureTable(ctx context.Context, def TableDefinition) error

// GetSchemaVersion returns the current schema version from the database.
func (s *SchemaManager) GetSchemaVersion(ctx context.Context) (int, error)
```

**Table Definitions:**

```sql
-- Metrics table (generic time-series)
CREATE TABLE IF NOT EXISTS metrics (
    agent_id SYMBOL,
    name SYMBOL,
    labels STRING,
    value DOUBLE,
    type SYMBOL,
    timestamp TIMESTAMP
) TIMESTAMP(timestamp) PARTITION BY DAY WAL
DEDUP ENABLED UPSERT KEYS(agent_id, name, timestamp);

-- CPU metrics
CREATE TABLE IF NOT EXISTS cpu_metrics (
    agent_id SYMBOL,
    timestamp TIMESTAMP,
    total_usage_percent DOUBLE,
    load_average_1m DOUBLE,
    load_average_5m DOUBLE,
    load_average_15m DOUBLE,
    per_core STRING  -- JSON array of per-core values
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Memory metrics
CREATE TABLE IF NOT EXISTS memory_metrics (
    agent_id SYMBOL,
    timestamp TIMESTAMP,
    total_bytes LONG,
    used_bytes LONG,
    available_bytes LONG,
    usage_percent DOUBLE,
    swap_total_bytes LONG,
    swap_used_bytes LONG
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Disk metrics
CREATE TABLE IF NOT EXISTS disk_metrics (
    agent_id SYMBOL,
    device SYMBOL,
    timestamp TIMESTAMP,
    read_bytes_per_sec DOUBLE,
    write_bytes_per_sec DOUBLE,
    read_iops DOUBLE,
    write_iops DOUBLE,
    usage_percent DOUBLE,
    total_bytes LONG,
    used_bytes LONG
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Network metrics
CREATE TABLE IF NOT EXISTS network_metrics (
    agent_id SYMBOL,
    interface SYMBOL,
    timestamp TIMESTAMP,
    rx_bytes_per_sec DOUBLE,
    tx_bytes_per_sec DOUBLE,
    rx_packets_per_sec DOUBLE,
    tx_packets_per_sec DOUBLE,
    errors_in LONG,
    errors_out LONG
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Process metrics
CREATE TABLE IF NOT EXISTS process_metrics (
    agent_id SYMBOL,
    pid LONG,
    name SYMBOL,
    timestamp TIMESTAMP,
    cpu_percent DOUBLE,
    memory_bytes LONG,
    memory_percent DOUBLE,
    status SYMBOL,
    command STRING
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Container metrics
CREATE TABLE IF NOT EXISTS container_metrics (
    agent_id SYMBOL,
    container_id SYMBOL,
    name SYMBOL,
    timestamp TIMESTAMP,
    cpu_percent DOUBLE,
    memory_bytes LONG,
    memory_limit_bytes LONG,
    network_rx_bytes DOUBLE,
    network_tx_bytes DOUBLE,
    status SYMBOL
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Aggregated metrics
CREATE TABLE IF NOT EXISTS aggregated_metrics (
    agent_id SYMBOL,
    name SYMBOL,
    labels STRING,
    window_duration LONG,  -- Duration in nanoseconds
    agg_type SYMBOL,       -- avg, min, max, p50, p90, p99, count, sum
    value DOUBLE,
    sample_count LONG,
    timestamp TIMESTAMP
) TIMESTAMP(timestamp) PARTITION BY DAY WAL
DEDUP ENABLED UPSERT KEYS(agent_id, name, window_duration, agg_type, timestamp);

-- Spans (traces)
CREATE TABLE IF NOT EXISTS spans (
    trace_id SYMBOL,
    span_id SYMBOL,
    parent_span_id SYMBOL,
    name SYMBOL,
    kind SYMBOL,
    service_name SYMBOL,
    start_time TIMESTAMP,
    end_time TIMESTAMP,
    duration LONG,
    status SYMBOL,
    attributes STRING  -- JSON map
) TIMESTAMP(start_time) PARTITION BY DAY WAL;

-- Database query events (from eBPF)
CREATE TABLE IF NOT EXISTS db_queries (
    agent_id SYMBOL,
    protocol SYMBOL,       -- postgresql, mysql, redis
    query_type SYMBOL,     -- SELECT, INSERT, UPDATE, DELETE, GET, SET
    table_name SYMBOL,     -- Extracted table/key name
    database SYMBOL,
    destination_ip SYMBOL,
    destination_port LONG,
    pid LONG,
    process_name SYMBOL,
    latency_ms DOUBLE,
    row_count LONG,
    error_message STRING,
    query_sample STRING,   -- First 256 chars of query (for debugging)
    timestamp TIMESTAMP
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Topology snapshots (for timeline)
CREATE TABLE IF NOT EXISTS topology_snapshots (
    snapshot_id SYMBOL,
    timestamp TIMESTAMP,
    node_count LONG,
    edge_count LONG,
    snapshot_data STRING,  -- JSON (compressed in application layer)
    is_checkpoint BOOLEAN  -- true for full snapshots, false for deltas
) TIMESTAMP(timestamp) PARTITION BY DAY WAL;

-- Schema version tracking
CREATE TABLE IF NOT EXISTS paryty_schema_version (
    version LONG,
    applied_at TIMESTAMP,
    description STRING
);
```

### 4.3 High-Throughput Ingestion

**File:** `cluster/internal/storage/warm/ilp_writer.go` (~800 LOC)

```go
// package warm

// ILPWriter writes metrics to QuestDB using InfluxDB Line Protocol.
//
// ILP is a text-based protocol optimized for high-throughput time-series ingestion.
// Format: table,tag1=val1,tag2=val2 field1=val1,field2=val2 unix_timestamp_ns
//
// Example:
//   cpu_metrics,agent_id=agent-1 total_usage_percent=45.2,load_average_1m=1.5 1704067200000000000
type ILPWriter struct {
    conn    net.Conn
    addr    string
    logger  *zap.Logger
    buf     *bufio.Writer
    mu      sync.Mutex
    metrics *ILPMetrics
    closed  bool
}

// ILPMetrics tracks ILP writer performance.
type ILPMetrics struct {
    RowsWritten   prometheus.Counter
    WriteLatency  prometheus.Histogram
    Errors        prometheus.Counter
    BufferSize    prometheus.Gauge
}

// NewILPWriter creates a new ILP writer.
//
// Connects to QuestDB ILP endpoint (default port 9009).
// Uses a buffered writer for batch efficiency.
//
// PARAMETERS:
//   - addr: QuestDB ILP address (e.g. "localhost:9009")
//   - logger: structured logger
//
// RETURNS: connected ILPWriter
// ERRORS: returns error if connection fails
func NewILPWriter(addr string, logger *zap.Logger) (*ILPWriter, error)

// WriteMetric writes a single metric via ILP.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - table: target table name
//   - tags: map of tag name → value (used for SYMBOL columns)
//   - fields: map of field name → value (used for numeric columns)
//   - timestamp: metric timestamp
//
// ERRORS: returns error if write fails
func (w *ILPWriter) WriteMetric(
    ctx context.Context,
    table string,
    tags map[string]string,
    fields map[string]float64,
    timestamp time.Time,
) error

// WriteBatch writes a batch of metrics via ILP.
//
// Buffers all metrics and flushes in a single network write.
// This is 10-100x more efficient than individual writes.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - metrics: slice of ILPMetric to write
//
// ERRORS: returns error if flush fails
func (w *ILPWriter) WriteBatch(ctx context.Context, metrics []ILPMetric) error

// ILPMetric is a single metric ready for ILP ingestion.
type ILPMetric struct {
    Table     string
    Tags      map[string]string
    Fields    map[string]float64
    Timestamp time.Time
}

// Flush forces a buffer flush to QuestDB.
func (w *ILPWriter) Flush() error

// Close flushes remaining data and closes the connection.
func (w *ILPWriter) Close() error
```

**File:** `cluster/internal/storage/warm/rest_writer.go` (~700 LOC)

```go
// package warm

// RESTWriter writes data to QuestDB using the REST API.
//
// The REST API supports bulk inserts via the /exec endpoint and
// COPY command for CSV data. This is used for batch operations
// like downsampling and backfill where throughput matters more than latency.
type RESTWriter struct {
    endpoint string
    client   *http.Client
    logger   *zap.Logger
}

// NewRESTWriter creates a new REST writer.
func NewRESTWriter(endpoint string, logger *zap.Logger) *RESTWriter

// ExecuteQuery executes a SQL query via REST API.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - query: SQL query to execute
//   - args: query parameters
//
// RETURNS: query result as JSON
// ERRORS: returns error if query fails
func (w *RESTWriter) ExecuteQuery(ctx context.Context, query string, args ...interface{}) (json.RawMessage, error)

// BulkInsertCSV inserts data via COPY command.
//
// Converts data to CSV format and uses QuestDB's COPY command
// for high-throughput bulk insertion.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - table: target table name
//   - columns: column names
//   - data: rows of data (each row is a slice of interface{})
//
// ERRORS: returns error if insert fails
func (w *RESTWriter) BulkInsertCSV(
    ctx context.Context,
    table string,
    columns []string,
    data [][]interface{},
) error

// BulkInsertAggregated inserts downsampled aggregated metrics.
//
// Specialized method for the downsampler to write large batches
// of aggregated metrics efficiently.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - metrics: aggregated metrics to insert
//
// ERRORS: returns error if insert fails
func (w *RESTWriter) BulkInsertAggregated(
    ctx context.Context,
    metrics []models.AggregatedMetric,
) error
```

### 4.4 Query Optimization

**File:** `cluster/internal/storage/warm/query_optimizer.go` (~1,000 LOC)

```go
// package warm

// QueryOptimizer adds caching and optimization to QuestDB queries.
type QueryOptimizer struct {
    pool      *pgxpool.Pool
    cache     *QueryCache
    downsampler *DownsamplingQuery
    logger    *zap.Logger
}

// QueryCache caches query results in Dragonfly.
type QueryCache struct {
    dragonfly *redis.Client
    ttl       time.Duration  // 30 seconds default
    prefix    string         // "paryty:query_cache:"
}

// NewQueryOptimizer creates a new query optimizer.
func NewQueryOptimizer(
    pool *pgxpool.Pool,
    dragonfly *redis.Client,
    logger *zap.Logger,
) *QueryOptimizer

// QueryMetricsWithCache queries metrics with Dragonfly caching.
//
// FLOW:
//   1. Check Dragonfly cache for (agentID, metricName, start, end)
//   2. If cache hit: return cached result
//   3. If cache miss: query QuestDB, cache result, return
//
// Cache key format: paryty:query_cache:{agentID}:{metricName}:{start}:{end}
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - agentID: agent to query
//   - metricName: metric to query
//   - start, end: time range
//
// RETURNS: metrics from cache or QuestDB
func (q *QueryOptimizer) QueryMetricsWithCache(
    ctx context.Context,
    agentID string,
    metricName string,
    start time.Time,
    end time.Time,
) ([]models.Metric, error)

// QueryWithDownsampling queries metrics with automatic downsampling.
//
// If the time range is large (> 1 hour), automatically uses 5m aggregated metrics.
// If the time range is very large (> 24 hours), uses 1h aggregated metrics.
//
// This prevents returning millions of data points for large time ranges.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - agentID: agent to query
//   - metricName: metric to query
//   - start, end: time range
//
// RETURNS: metrics (auto-downsampled if needed)
func (q *QueryOptimizer) QueryWithDownsampling(
    ctx context.Context,
    agentID string,
    metricName string,
    start time.Time,
    end time.Time,
) ([]models.Metric, error)

// InvalidateCache invalidates cached queries for a specific agent.
//
// Called when new metrics are ingested to prevent stale data.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - agentID: agent whose cache to invalidate
func (q *QueryOptimizer) InvalidateCache(ctx context.Context, agentID string) error

// QueryDatabaseQueries queries eBPF database query events.
//
// This is a new query type for Phase 4 — querying the db_queries table
// populated by the eBPF DB inspector.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - agentID: agent to query (empty for all agents)
//   - protocol: database protocol filter (empty for all)
//   - start, end: time range
//   - limit: max results
//
// RETURNS: database query events
func (q *QueryOptimizer) QueryDatabaseQueries(
    ctx context.Context,
    agentID string,
    protocol string,
    start time.Time,
    end time.Time,
    limit int,
) ([]DbQueryRecord, error)

// DbQueryRecord represents a database query event from QuestDB.
type DbQueryRecord struct {
    AgentID         string    `json:"agent_id"`
    Protocol        string    `json:"protocol"`
    QueryType       string    `json:"query_type"`
    TableName       string    `json:"table_name"`
    Database        string    `json:"database"`
    DestinationIP   string    `json:"destination_ip"`
    DestinationPort int       `json:"destination_port"`
    PID             int       `json:"pid"`
    ProcessName     string    `json:"process_name"`
    LatencyMs       float64   `json:"latency_ms"`
    RowCount        int64     `json:"row_count"`
    ErrorMessage    string    `json:"error_message"`
    QuerySample     string    `json:"query_sample"`
    Timestamp       time.Time `json:"timestamp"`
}
```

### 4.5 Retention Management

**File:** `cluster/internal/storage/warm/retention.go` (~500 LOC)

```go
// package warm

// RetentionRule defines a data retention policy.
type RetentionRule struct {
    Table         string        `json:"table"`
    MaxAge        time.Duration `json:"max_age"`
    ArchiveToCold bool          `json:"archive_to_cold"` // Archive before deleting
}

// DefaultRetentionRules returns sensible defaults.
var DefaultRetentionRules = []RetentionRule{
    {Table: "cpu_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
    {Table: "memory_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
    {Table: "disk_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
    {Table: "network_metrics", MaxAge: 30 * 24 * time.Hour, ArchiveToCold: true},
    {Table: "process_metrics", MaxAge: 7 * 24 * time.Hour, ArchiveToCold: false},
    {Table: "container_metrics", MaxAge: 7 * 24 * time.Hour, ArchiveToCold: false},
    {Table: "aggregated_metrics", MaxAge: 90 * 24 * time.Hour, ArchiveToCold: true},
    {Table: "spans", MaxAge: 14 * 24 * time.Hour, ArchiveToCold: true},
    {Table: "db_queries", MaxAge: 14 * 24 * time.Hour, ArchiveToCold: true},
    {Table: "topology_snapshots", MaxAge: 7 * 24 * time.Hour, ArchiveToCold: true},
}

// RetentionManager handles automated data retention.
type RetentionManager struct {
    pool      *pgxpool.Pool
    rules     []RetentionRule
    coldStore ColdArchiver  // Interface for archiving to cold storage
    logger    *zap.Logger
}

// ColdArchiver is the interface for archiving data to cold storage.
type ColdArchiver interface {
    ArchiveTable(ctx context.Context, table string, before time.Time) error
}

// NewRetentionManager creates a new retention manager.
func NewRetentionManager(
    pool *pgxpool.Pool,
    rules []RetentionRule,
    coldStore ColdArchiver,
    logger *zap.Logger,
) *RetentionManager

// RunRetention runs the retention pipeline.
//
// Called periodically (every 1 hour) by the pipeline service.
//
// For each retention rule:
//   1. If archive_to_cold: export data older than MaxAge to SeaweedFS
//   2. Delete data older than MaxAge from QuestDB
//   3. Log results
//
// PARAMETERS:
//   - ctx: context for cancellation
//
// ERRORS: returns error if any rule fails (non-fatal, retried next cycle)
func (r *RetentionManager) RunRetention(ctx context.Context) error

// DropPartition drops a QuestDB partition for a table.
//
// QuestDB partitions data by day/hour. Dropping a partition is instant
// and doesn't require scanning the table.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - table: table name
//   - partition: partition to drop (e.g. "2024-01-15")
func (r *RetentionManager) DropPartition(ctx context.Context, table string, partition string) error
```

---

## 5. Layer 16: Cold Store & Timeline Snapshots

### 5.1 Overview

The cold store is upgraded with Zstd compression, timeline snapshots (full checkpoints + Redpanda event log), and an LRU cache in Dragonfly for fast retrieval of recent snapshots.

**Decision 3 (A+C Hybrid):**
- **Full snapshots** every 5 minutes → stored in SeaweedFS as checkpoints
- **Redpanda event log** → stored in QuestDB for inter-snapshot replay
- **Reconstruction:** Load nearest full snapshot, then replay events from Redpanda/QuestDB

### 5.2 Timeline Snapshot Manager

**File:** `cluster/internal/storage/cold/snapshot.go` (~1,500 LOC)

```go
// package cold

// SnapshotConfig holds snapshot configuration.
type SnapshotConfig struct {
    Interval        time.Duration  // 5 minutes
    Compression     bool           // true (Zstd)
    RetentionDays   int            // 7 days
    MaxSnapshotSize int64          // 50 MB max per snapshot
}

// Snapshot represents a full timeline checkpoint.
type Snapshot struct {
    ID          string                 `json:"id"`
    Timestamp   time.Time              `json:"timestamp"`
    Topology    *models.Topology       `json:"topology"`
    Agents      []models.AgentInfo     `json:"agents"`
    Metrics     map[string]MetricSummary `json:"metrics"` // agent_id → summary
    Alerts      []models.Alert         `json:"alerts"`
    Graph       *GraphSnapshot         `json:"graph"`
    Metadata    SnapshotMetadata       `json:"metadata"`
    SizeBytes   int64                  `json:"size_bytes"`
    Compressed  bool                   `json:"compressed"`
}

// MetricSummary holds the latest metrics for an agent at snapshot time.
type MetricSummary struct {
    AgentID    string             `json:"agent_id"`
    CPU        *models.CPUMetrics `json:"cpu,omitempty"`
    Memory     *models.MemoryMetrics `json:"memory,omitempty"`
    Disk       []models.DiskMetrics `json:"disk,omitempty"`
    Network    []models.NetworkMetrics `json:"network,omitempty"`
}

// GraphSnapshot captures the dependency graph state.
type GraphSnapshot struct {
    Nodes []models.TopologyNode `json:"nodes"`
    Edges []models.TopologyEdge `json:"edges"`
}

// SnapshotMetadata holds metadata about the snapshot.
type SnapshotMetadata struct {
    PipelineVersion string    `json:"pipeline_version"`
    AgentCount      int       `json:"agent_count"`
    MetricCount     int       `json:"metric_count"`
    AlertCount      int       `json:"alert_count"`
    CreatedAt       time.Time `json:"created_at"`
}

// SnapshotManager manages timeline snapshots.
type SnapshotManager struct {
    config     SnapshotConfig
    store      *Store           // Cold store for uploading
    hotStore   HotStoreReader   // Interface for reading current state
    dragonfly  *redis.Client    // For LRU cache
    questdb    *pgxpool.Pool    // For event log queries
    logger     *zap.Logger
    metrics    *SnapshotMetrics
}

// HotStoreReader is the interface for reading current state from hot storage.
type HotStoreReader interface {
    GetTopology(ctx context.Context) (*models.Topology, error)
    GetAllAgentStates(ctx context.Context) ([]models.AgentInfo, error)
    GetLatestMetrics(ctx context.Context, agentID string) (*models.MetricBatch, error)
    GetActiveAlerts(ctx context.Context) ([]models.Alert, error)
}

// NewSnapshotManager creates a new snapshot manager.
func NewSnapshotManager(
    config SnapshotConfig,
    store *Store,
    hotStore HotStoreReader,
    dragonfly *redis.Client,
    questdb *pgxpool.Pool,
    logger *zap.Logger,
) *SnapshotManager

// TakeSnapshot creates a full timeline snapshot.
//
// PROCESS:
//   1. Read current topology from Dragonfly
//   2. Read all agent states from Dragonfly
//   3. Read latest metrics for each agent
//   4. Read active alerts
//   5. Read dependency graph from pipeline
//   6. Assemble Snapshot struct
//   7. Compress with Zstd (if enabled)
//   8. Upload to SeaweedFS: snapshots/{date}/{snapshot_id}.json.zst
//   9. Cache in Dragonfly: paryty:snapshot:latest
//   10. Record snapshot metadata in QuestDB: topology_snapshots table
//
// PARAMETERS:
//   - ctx: context for cancellation
//
// RETURNS: the created snapshot
// ERRORS: returns error if any step fails
func (s *SnapshotManager) TakeSnapshot(ctx context.Context) (*Snapshot, error)

// GetSnapshot retrieves a snapshot by ID.
//
// FLOW:
//   1. Check Dragonfly LRU cache (paryty:snapshot:{id})
//   2. If cache hit: decompress and return
//   3. If cache miss: fetch from SeaweedFS
//   4. Cache in Dragonfly for future requests
//   5. Decompress and return
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - id: snapshot ID
//
// RETURNS: the snapshot
func (s *SnapshotManager) GetSnapshot(ctx context.Context, id string) (*Snapshot, error)

// GetNearestSnapshot finds the snapshot closest to a given timestamp.
//
// Queries QuestDB topology_snapshots table to find the nearest checkpoint.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - target: desired timestamp
//
// RETURNS: nearest snapshot (before or at target time)
func (s *SnapshotManager) GetNearestSnapshot(ctx context.Context, target time.Time) (*Snapshot, error)

// ReconstructState reconstructs the system state at a given point in time.
//
// ALGORITHM (A+C Hybrid):
//   1. Find nearest full snapshot before target time
//   2. Load snapshot
//   3. Query Redpanda event log for events between snapshot time and target
//   4. Replay events on top of snapshot state
//   5. Return reconstructed state
//
// This gives us point-in-time reconstruction at any granularity
// without storing full snapshots every second.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - target: desired timestamp to reconstruct
//
// RETURNS: reconstructed state at target time
// ERRORS: returns error if snapshot or event retrieval fails
func (s *SnapshotManager) ReconstructState(
    ctx context.Context,
    target time.Time,
) (*Snapshot, error)

// CleanupExpired removes snapshots older than retention period.
//
// Called periodically (every 1 hour).
// Deletes from both SeaweedFS and Dragonfly cache.
//
// PARAMETERS:
//   - ctx: context for cancellation
//
// RETURNS: number of snapshots deleted
func (s *SnapshotManager) CleanupExpired(ctx context.Context) (int, error)
```

### 5.3 Event Log Indexer

**File:** `cluster/internal/storage/cold/eventlog.go` (~800 LOC)

```go
// package cold

// EventLogEntry represents a topology change event in the event log.
type EventLogEntry struct {
    ID        string    `json:"id"`
    Timestamp time.Time `json:"timestamp"`
    Type      string    `json:"type"`      // "node_added", "edge_added", "metric_updated", etc.
    AgentID   string    `json:"agent_id"`
    Payload   []byte    `json:"payload"`   // JSON-encoded event data
}

// EventLog manages the Redpanda event log for inter-snapshot replay.
//
// Events are stored in QuestDB's topology_snapshots table with is_checkpoint=false.
// When reconstructing state at time T, we:
//   1. Load nearest full snapshot (is_checkpoint=true)
//   2. Query events between snapshot time and T (is_checkpoint=false)
//   3. Replay events in order
type EventLog struct {
    pool   *pgxpool.Pool
    logger *zap.Logger
}

// NewEventLog creates a new event log.
func NewEventLog(pool *pgxpool.Pool, logger *zap.Logger) *EventLog

// RecordEvent records a topology change event.
//
// Called by the pipeline when topology changes are detected.
// Events are written to QuestDB for persistence and queryability.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - entry: event to record
func (e *EventLog) RecordEvent(ctx context.Context, entry EventLogEntry) error

// QueryEvents queries events within a time range.
//
// Used for inter-snapshot replay: get all events between a snapshot
// timestamp and the target reconstruction time.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - start: start of time range (inclusive)
//   - end: end of time range (inclusive)
//
// RETURNS: ordered slice of events (oldest first)
func (e *EventLog) QueryEvents(
    ctx context.Context,
    start time.Time,
    end time.Time,
) ([]EventLogEntry, error)

// ReplayEvents applies a sequence of events to a snapshot.
//
// For each event:
//   - node_added → add node to topology
//   - node_removed → remove node from topology
//   - edge_added → add edge to topology
//   - edge_removed → remove edge from topology
//   - metric_updated → update metric summary
//   - alert_fired → add to alerts
//   - alert_resolved → remove from alerts
//
// PARAMETERS:
//   - snapshot: base snapshot to modify
//   - events: events to apply in order
//
// RETURNS: modified snapshot
func (e *EventLog) ReplayEvents(
    snapshot *Snapshot,
    events []EventLogEntry,
) (*Snapshot, error)

// CleanupOldEvents removes events older than retention period.
//
// Events are kept for 7 days (same as snapshots).
// After 7 days, they're no longer needed since we have full snapshots.
func (e *EventLog) CleanupOldEvents(ctx context.Context, maxAge time.Duration) (int, error)
```

### 5.4 Cold Store LRU Cache

**File:** `cluster/internal/storage/cold/cache.go` (~700 LOC)

```go
// package cold

// ColdCache provides LRU caching for cold store queries in Dragonfly.
//
// Caches recently accessed snapshots and query results to avoid
// repeated SeaweedFS fetches (which have 100-500ms latency).
//
// Cache structure:
//   - paryty:cold:snapshot:{id} → compressed snapshot JSON (TTL: 1 hour)
//   - paryty:cold:query:{hash} → query result JSON (TTL: 30 seconds)
//   - paryty:cold:lru:order → sorted set of cache keys by access time
type ColdCache struct {
    dragonfly *redis.Client
    maxSize   int           // Max number of cached entries
    ttl       time.Duration // Default TTL
    logger    *zap.Logger
}

// NewColdCache creates a new cold store cache.
//
// PARAMETERS:
//   - dragonfly: Redis-compatible client (Dragonfly)
//   - maxSize: maximum number of cached entries (default: 100)
//   - ttl: default TTL for cached entries (default: 1 hour)
//   - logger: structured logger
func NewColdCache(
    dragonfly *redis.Client,
    maxSize int,
    ttl time.Duration,
    logger *zap.Logger,
) *ColdCache

// Get retrieves a cached snapshot.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - key: cache key
//
// RETURNS: cached data, or nil if not cached
func (c *ColdCache) Get(ctx context.Context, key string) ([]byte, error)

// Set stores data in the cache.
//
// If cache is full (size > maxSize), evicts least recently used entries.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - key: cache key
//   - data: data to cache
//   - ttl: custom TTL (0 for default)
func (c *ColdCache) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error

// GetOrFetch retrieves from cache, or fetches and caches if miss.
//
// This is the primary API for cold store queries.
//
// PARAMETERS:
//   - ctx: context for cancellation
//   - key: cache key
//   - fetch: function to fetch data if cache miss
//
// RETURNS: data from cache or fetch function
func (c *ColdCache) GetOrFetch(
    ctx context.Context,
    key string,
    fetch func(ctx context.Context) ([]byte, error),
) ([]byte, error)

// Invalidate removes a specific entry from the cache.
func (c *ColdCache) Invalidate(ctx context.Context, key string) error

// InvalidatePrefix removes all entries matching a prefix.
//
// Used to invalidate all cached queries for a specific agent.
func (c *ColdCache) InvalidatePrefix(ctx context.Context, prefix string) error
```

### 5.5 Timeline Reconstruction Pseudo-Code

```
FUNCTION ReconstructState(targetTime):
    // 1. Find nearest full snapshot before target
    snapshot = queryQuestDB("SELECT * FROM topology_snapshots 
                             WHERE is_checkpoint = true AND timestamp <= ? 
                             ORDER BY timestamp DESC LIMIT 1", targetTime)
    
    IF snapshot == nil:
        RETURN error("no snapshot found before target time")
    
    // 2. Load snapshot from SeaweedFS (with LRU cache)
    snapshotData = coldCache.GetOrFetch(
        "paryty:cold:snapshot:" + snapshot.ID,
        func() {
            return seaweedFS.GetObject("snapshots", snapshot.ID + ".json.zst")
        }
    )
    baseSnapshot = decompressAndDeserialize(snapshotData)
    
    // 3. If target time is within 5 minutes of snapshot, return as-is
    IF targetTime - snapshot.Timestamp < 5 minutes:
        RETURN baseSnapshot
    
    // 4. Query event log for events between snapshot and target
    events = queryQuestDB("SELECT * FROM topology_snapshots 
                          WHERE is_checkpoint = false 
                          AND timestamp > ? AND timestamp <= ? 
                          ORDER BY timestamp ASC", 
                          snapshot.Timestamp, targetTime)
    
    // 5. Replay events on top of snapshot
    reconstructed = baseSnapshot
    FOR each event in events:
        SWITCH event.Type:
            CASE "node_added":
                reconstructed.Topology.Nodes.append(event.Payload)
            CASE "node_removed":
                reconstructed.Topology.Nodes.remove(event.Payload.nodeID)
            CASE "edge_added":
                reconstructed.Topology.Edges.append(event.Payload)
            CASE "edge_removed":
                reconstructed.Topology.Edges.remove(event.Payload.edgeID)
            CASE "metric_updated":
                reconstructed.Metrics[event.Payload.agentID] = event.Payload.summary
            CASE "alert_fired":
                reconstructed.Alerts.append(event.Payload)
            CASE "alert_resolved":
                reconstructed.Alerts.remove(event.Payload.alertID)
    
    RETURN reconstructed
```

---

## 6. Layer 17: eBPF Database Protocol Inspection

### 6.1 Overview

The eBPF DB Inspector adds full query extraction for PostgreSQL, MySQL, and Redis protocols. It reuses the existing kprobe infrastructure from Phase 2 (tcp_sendmsg/recvmsg) but adds protocol-specific parsing for database traffic.

**Supported Protocols:**
- **PostgreSQL**: Simple Query Protocol, Extended Query Protocol, CommandComplete responses
- **MySQL**: COM_QUERY, COM_STMT_EXECUTE, OK/ERR responses
- **Redis**: RESP protocol (inline and array formats)

**TLS Fallback:** If payload doesn't match any known protocol but destination port is 5432/3306/6379, report as "encrypted connection" with connection-only metadata.

### 6.2 C eBPF Program for DB Packet Capture

**File:** `agent/src/ebpf/ebpf/db_probe.c` (~300 LOC)

```c
// db_probe.c — eBPF program for capturing database protocol packets
//
// Attaches to kretprobe on tcp_recvmsg to inspect incoming database responses.
// Uses the same ring buffer infrastructure as tcp_tracker.

#include "common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

// Maximum bytes to capture from payload (for protocol detection)
#define MAX_PAYLOAD_CAPTURE 256

// Database ports
#define PG_PORT   5432
#define MYSQL_PORT 3306
#define REDIS_PORT 6379

// Ring buffer for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 20);  // 1MB ring buffer
} db_events SEC(".maps");

// Per-socket state for tracking request-response pairs
struct db_socket_state {
    u64 request_ts;       // Timestamp of last request
    u32 pid;              // Process ID
    u16 dport;            // Destination port
    u8  protocol;         // Detected protocol (0=unknown, 1=pg, 2=mysql, 3=redis)
    char payload[MAX_PAYLOAD_CAPTURE];  // First bytes of payload
    u32 payload_len;      // Captured payload length
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, u64);     // Socket pointer
    __type(value, struct db_socket_state);
} db_socket_states SEC(".maps");

// Process info map (shared with tcp_tracker)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, u32);     // PID
    __type(value, struct process_info);
} process_map SEC(".maps");

// detect_protocol inspects the first bytes of a payload to identify the DB protocol.
//
// PostgreSQL: First byte is 'Q' (Query), 'P' (Parse), 'B' (Bind), 'E' (Execute),
//             'C' (CommandComplete), 'D' (DataRow), etc.
// MySQL:      First 4 bytes are length (3 bytes) + sequence_id (1 byte).
//             COM_QUERY = 0x03, COM_STMT_EXECUTE = 0x17
// Redis:      First byte is '*' (array), '+' (simple string), '-' (error),
//             ':' (integer), '$' (bulk string), or inline command
static __always_inline u8 detect_protocol(const char *data, u32 len, u16 port) {
    if (len < 1) return 0;
    
    // Check by port first for efficiency
    if (port == PG_PORT) {
        // PostgreSQL: first byte is a message type
        char first = data[0];
        if (first == 'Q' || first == 'P' || first == 'B' || first == 'E' ||
            first == 'C' || first == 'D' || first == 'T' || first == 'R' ||
            first == 'K' || first == 'S' || first == 'Z' || first == 'N') {
            return 1;  // PostgreSQL
        }
    }
    
    if (port == REDIS_PORT) {
        // Redis RESP: first byte is '*', '+', '-', ':', '$'
        // Or inline command (letter)
        char first = data[0];
        if (first == '*' || first == '+' || first == '-' || first == ':' || first == '$') {
            return 3;  // Redis RESP
        }
        // Inline commands start with a letter
        if ((first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')) {
            return 3;  // Redis inline
        }
    }
    
    if (port == MYSQL_PORT && len >= 5) {
        // MySQL: 3-byte length + 1-byte sequence + payload
        // COM_QUERY = 0x03, COM_STMT_EXECUTE = 0x17
        u8 cmd = data[4];
        if (cmd == 0x03 || cmd == 0x17 || cmd == 0x0e || cmd == 0x16) {
            return 2;  // MySQL
        }
    }
    
    // If port matches but protocol doesn't, it's likely TLS-encrypted
    if (port == PG_PORT || port == MYSQL_PORT || port == REDIS_PORT) {
        return 4;  // Encrypted database connection
    }
    
    return 0;  // Not a database connection
}

// SEC("kretprobe/tcp_recvmsg")
// Inspects incoming packets for database protocol data.
int BPF_KRETPROBE(tcp_recvmsg_ret, int ret) {
    if (ret <= 0) return 0;
    
    u64 pid_tgid = bpf_get_current_pid_tgid();
    u32 pid = pid_tgid >> 32;
    
    // Get socket from the saved state
    u64 sock_key = pid_tgid;
    struct db_socket_state *state = bpf_map_lookup_elem(&db_socket_states, &sock_key);
    if (!state) return 0;
    
    // Only process database ports
    if (state->dport != PG_PORT && state->dport != MYSQL_PORT && state->dport != REDIS_PORT) {
        bpf_map_delete_elem(&db_socket_states, &sock_key);
        return 0;
    }
    
    // Detect protocol from captured payload
    u8 protocol = detect_protocol(state->payload, state->payload_len, state->dport);
    if (protocol == 0) {
        bpf_map_delete_elem(&db_socket_states, &sock_key);
        return 0;
    }
    
    // Allocate event in ring buffer
    struct db_event *evt = bpf_ringbuf_reserve(&db_events, sizeof(struct db_event), 0);
    if (!evt) {
        bpf_map_delete_elem(&db_socket_states, &sock_key);
        return 0;
    }
    
    // Fill event
    evt->pid = pid;
    evt->protocol = protocol;
    evt->dport = state->dport;
    evt->timestamp = bpf_ktime_get_ns();
    evt->response_bytes = ret;
    
    // Copy payload for userspace parsing
    u32 copy_len = state->payload_len;
    if (copy_len > MAX_PAYLOAD_CAPTURE) copy_len = MAX_PAYLOAD_CAPTURE;
    __builtin_memcpy(evt->payload, state->payload, copy_len);
    evt->payload_len = copy_len;
    
    // Get process name
    bpf_get_current_comm(evt->process_name, sizeof(evt->process_name));
    
    bpf_ringbuf_submit(evt, 0);
    bpf_map_delete_elem(&db_socket_states, &sock_key);
    
    return 0;
}
```

### 6.3 Common Header Update

**File:** `agent/src/ebpf/ebpf/common.h` — Add db_event struct:

```c
// Database event for ring buffer
struct db_event {
    u32 pid;                      // Process ID
    u8  protocol;                 // 1=PostgreSQL, 2=MySQL, 3=Redis, 4=Encrypted
    u16 dport;                    // Destination port
    u64 timestamp;                // Nanosecond timestamp
    s32 response_bytes;           // Bytes in response (for row count estimation)
    u32 payload_len;              // Captured payload length
    char payload[256];            // First 256 bytes of payload
    char process_name[16];        // Process name
    char dest_ip[46];             // Destination IP (IPv4/IPv6)
};
```

### 6.4 Rust Userspace DB Inspector

**File:** `agent/src/ebpf/db_inspector.rs` (~1,500 LOC, REWRITE)

```rust
//! Database Protocol Inspector
//!
//! Parses database protocol messages from eBPF-captured payloads.
//! Supports PostgreSQL, MySQL, and Redis protocols with TLS fallback.

use anyhow::{Context, Result};
use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

/// Database query event (output of inspection).
#[derive(Debug, Clone)]
pub struct DbQueryEvent {
    pub protocol: String,
    pub query: String,
    pub query_type: String,
    pub table_name: String,
    pub database: String,
    pub latency_ms: f64,
    pub row_count: u64,
    pub error_message: String,
    pub source_ip: String,
    pub destination_ip: String,
    pub destination_port: u16,
    pub pid: u32,
    pub process_name: String,
    pub is_encrypted: bool,
}

/// Protocol detector and parser.
pub struct DbInspector {
    /// Pending requests for latency calculation (keyed by socket)
    pending: Mutex<HashMap<u64, PendingRequest>>,
    /// Statistics
    stats: Mutex<InspectorStats>,
}

/// Pending request for latency calculation.
struct PendingRequest {
    timestamp: Instant,
    protocol: String,
    query: String,
}

/// Inspector statistics.
#[derive(Debug, Default)]
struct InspectorStats {
    postgres_queries: u64,
    mysql_queries: u64,
    redis_commands: u64,
    encrypted_connections: u64,
    parse_errors: u64,
}

/// Database protocol type.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum DbProtocol {
    Unknown = 0,
    PostgreSQL = 1,
    MySQL = 2,
    Redis = 3,
    Encrypted = 4,
}

impl DbInspector {
    pub fn new() -> Self {
        Self {
            pending: Mutex::new(HashMap::new()),
            stats: Mutex::new(InspectorStats::default()),
        }
    }

    /// Inspect a captured payload and extract database query information.
    ///
    /// PARAMETERS:
    ///   - payload: first 256 bytes of the packet
    ///   - port: destination port
    ///   - pid: process ID
    ///   - process_name: process name
    ///   - dest_ip: destination IP address
    ///
    /// RETURNS: Some(DbQueryEvent) if a database query was detected, None otherwise
    pub fn inspect(
        &self,
        payload: &[u8],
        port: u16,
        pid: u32,
        process_name: &str,
        dest_ip: &str,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.is_empty() {
            return Ok(None);
        }

        let protocol = self.detect_protocol(payload, port);
        match protocol {
            DbProtocol::PostgreSQL => self.parse_postgresql(payload, pid, process_name, dest_ip, port),
            DbProtocol::MySQL => self.parse_mysql(payload, pid, process_name, dest_ip, port),
            DbProtocol::Redis => self.parse_redis(payload, pid, process_name, dest_ip, port),
            DbProtocol::Encrypted => Ok(Some(DbQueryEvent {
                protocol: "encrypted".to_string(),
                query: String::new(),
                query_type: "UNKNOWN".to_string(),
                table_name: String::new(),
                database: String::new(),
                latency_ms: 0.0,
                row_count: 0,
                error_message: String::new(),
                source_ip: String::new(),
                destination_ip: dest_ip.to_string(),
                destination_port: port,
                pid,
                process_name: process_name.to_string(),
                is_encrypted: true,
            })),
            DbProtocol::Unknown => Ok(None),
        }
    }

    /// Detect the database protocol from payload bytes.
    fn detect_protocol(&self, payload: &[u8], port: u16) -> DbProtocol {
        if payload.is_empty() {
            return DbProtocol::Unknown;
        }

        match port {
            5432 => {
                // PostgreSQL message types
                match payload[0] {
                    b'Q' | b'P' | b'B' | b'E' | b'C' | b'D' | b'T' | b'R' | b'K' | b'S' | b'Z' | b'N' | b'A' | b'I' => DbProtocol::PostgreSQL,
                    _ => {
                        // TLS ClientHello starts with 0x16 0x03
                        if payload.len() >= 2 && payload[0] == 0x16 && payload[1] == 0x03 {
                            DbProtocol::Encrypted
                        } else {
                            DbProtocol::Unknown
                        }
                    }
                }
            }
            6379 => {
                // Redis RESP protocol
                match payload[0] {
                    b'*' | b'+' | b'-' | b':' | b'$' => DbProtocol::Redis,
                    // Inline commands
                    b if b.is_ascii_alphabetic() => DbProtocol::Redis,
                    _ => DbProtocol::Unknown,
                }
            }
            3306 => {
                // MySQL: 3-byte length + 1-byte sequence + command
                if payload.len() >= 5 {
                    let cmd = payload[4];
                    match cmd {
                        0x03 | 0x17 | 0x0e | 0x16 | 0x04 | 0x01 => DbProtocol::MySQL,
                        _ => DbProtocol::Unknown,
                    }
                } else {
                    DbProtocol::Unknown
                }
            }
            _ => DbProtocol::Unknown,
        }
    }

    // === PostgreSQL Protocol Parser ===

    /// Parse a PostgreSQL Simple Query Protocol message.
    ///
    /// Format: 'Q' + 4-byte length + null-terminated SQL string
    ///
    /// Example: "Q\x00\x00\x00\x19SELECT * FROM users WHERE id = 1\x00"
    fn parse_postgresql(
        &self,
        payload: &[u8],
        pid: u32,
        process_name: &str,
        dest_ip: &str,
        port: u16,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.len() < 5 {
            return Ok(None);
        }

        let msg_type = payload[0];
        let _length = u32::from_be_bytes([payload[1], payload[2], payload[3], payload[4]]);

        match msg_type {
            // Query message (client → server)
            b'Q' => {
                let query_bytes = &payload[5..];
                let query = self.read_null_string(query_bytes);
                let (query_type, table_name) = self.parse_sql(&query);

                // Track for latency calculation
                let socket_key = self.socket_key(pid, port);
                self.pending.lock().unwrap().insert(socket_key, PendingRequest {
                    timestamp: Instant::now(),
                    protocol: "postgresql".to_string(),
                    query: query.clone(),
                });

                self.stats.lock().unwrap().postgres_queries += 1;

                Ok(Some(DbQueryEvent {
                    protocol: "postgresql".to_string(),
                    query: self.truncate_query(&query),
                    query_type,
                    table_name,
                    database: String::new(),
                    latency_ms: 0.0,  // Updated when response arrives
                    row_count: 0,
                    error_message: String::new(),
                    source_ip: String::new(),
                    destination_ip: dest_ip.to_string(),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                }))
            }
            // CommandComplete (server → client)
            b'C' => {
                let response = self.read_null_string(&payload[5..]);
                let row_count = self.parse_pg_command_complete(&response);

                // Update latency for pending request
                let socket_key = self.socket_key(pid, port);
                let latency = if let Some(pending) = self.pending.lock().unwrap().remove(&socket_key) {
                    pending.timestamp.elapsed().as_secs_f64() * 1000.0
                } else {
                    0.0
                };

                Ok(Some(DbQueryEvent {
                    protocol: "postgresql".to_string(),
                    query: String::new(),
                    query_type: "RESPONSE".to_string(),
                    table_name: String::new(),
                    database: String::new(),
                    latency_ms: latency,
                    row_count,
                    error_message: String::new(),
                    source_ip: String::new(),
                    destination_ip: dest_ip.to_string(),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                }))
            }
            // ErrorResponse (server → client)
            b'E' => {
                let error = self.read_null_string(&payload[5..]);

                Ok(Some(DbQueryEvent {
                    protocol: "postgresql".to_string(),
                    query: String::new(),
                    query_type: "ERROR".to_string(),
                    table_name: String::new(),
                    database: String::new(),
                    latency_ms: 0.0,
                    row_count: 0,
                    error_message: error,
                    source_ip: String::new(),
                    destination_ip: dest_ip.to_string(),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                }))
            }
            _ => Ok(None),
        }
    }

    // === MySQL Protocol Parser ===

    /// Parse a MySQL COM_QUERY message.
    ///
    /// Format: 3-byte length + 1-byte sequence + 0x03 (COM_QUERY) + SQL string
    fn parse_mysql(
        &self,
        payload: &[u8],
        pid: u32,
        process_name: &str,
        dest_ip: &str,
        port: u16,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.len() < 5 {
            return Ok(None);
        }

        let cmd = payload[4];
        match cmd {
            // COM_QUERY
            0x03 => {
                let query_bytes = &payload[5..];
                let query = String::from_utf8_lossy(query_bytes).to_string();
                let (query_type, table_name) = self.parse_sql(&query);

                self.stats.lock().unwrap().mysql_queries += 1;

                Ok(Some(DbQueryEvent {
                    protocol: "mysql".to_string(),
                    query: self.truncate_query(&query),
                    query_type,
                    table_name,
                    database: String::new(),
                    latency_ms: 0.0,
                    row_count: 0,
                    error_message: String::new(),
                    source_ip: String::new(),
                    destination_ip: dest_ip.to_string(),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                }))
            }
            _ => Ok(None),
        }
    }

    // === Redis Protocol Parser ===

    /// Parse a Redis RESP command.
    ///
    /// RESP Array format: *N\r\n$len\r\narg1\r\n$len\r\narg2\r\n...
    /// Inline format: COMMAND arg1 arg2\r\n
    fn parse_redis(
        &self,
        payload: &[u8],
        pid: u32,
        process_name: &str,
        dest_ip: &str,
        port: u16,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.is_empty() {
            return Ok(None);
        }

        let payload_str = String::from_utf8_lossy(payload);

        let (command, key_prefix) = if payload[0] == b'*' {
            // RESP array format
            self.parse_redis_resp(&payload_str)?
        } else if payload[0].is_ascii_alphabetic() {
            // Inline command
            self.parse_redis_inline(&payload_str)?
        } else {
            return Ok(None);
        };

        self.stats.lock().unwrap().redis_commands += 1;

        Ok(Some(DbQueryEvent {
            protocol: "redis".to_string(),
            query: command.clone(),
            query_type: self.redis_command_type(&command),
            table_name: key_prefix,
            database: String::new(),
            latency_ms: 0.0,
            row_count: 0,
            error_message: String::new(),
            source_ip: String::new(),
            destination_ip: dest_ip.to_string(),
            destination_port: port,
            pid,
            process_name: process_name.to_string(),
            is_encrypted: false,
        }))
    }

    // === Helper Methods ===

    /// Parse SQL to extract operation type and table name.
    ///
    /// Examples:
    ///   "SELECT * FROM users WHERE id = 1" → ("SELECT", "users")
    ///   "INSERT INTO orders (id, total) VALUES (1, 100)" → ("INSERT", "orders")
    ///   "UPDATE products SET price = 50 WHERE id = 1" → ("UPDATE", "products")
    ///   "DELETE FROM sessions WHERE expired = true" → ("DELETE", "sessions")
    ///   "GET mykey" → ("GET", "mykey")
    fn parse_sql(&self, query: &str) -> (String, String) {
        let upper = query.trim().to_uppercase();
        let words: Vec<&str> = upper.split_whitespace().collect();
        
        if words.is_empty() {
            return ("UNKNOWN".to_string(), String::new());
        }

        let query_type = words[0].to_string();
        let table_name = match query_type.as_str() {
            "SELECT" => {
                // Find "FROM" keyword
                words.iter().position(|&w| w == "FROM")
                    .and_then(|i| words.get(i + 1))
                    .map(|s| s.to_lowercase())
                    .unwrap_or_default()
            }
            "INSERT" => {
                // Find "INTO" keyword
                words.iter().position(|&w| w == "INTO")
                    .and_then(|i| words.get(i + 1))
                    .map(|s| s.to_lowercase().trim_end_matches('(').to_string())
                    .unwrap_or_default()
            }
            "UPDATE" => {
                words.get(1).map(|s| s.to_lowercase()).unwrap_or_default()
            }
            "DELETE" => {
                words.iter().position(|&w| w == "FROM")
                    .and_then(|i| words.get(i + 1))
                    .map(|s| s.to_lowercase())
                    .unwrap_or_default()
            }
            _ => String::new(),
        };

        (query_type, table_name)
    }

    /// Parse PostgreSQL CommandComplete response to extract row count.
    ///
    /// Format: "COMMAND N" where N is the row count
    /// Examples: "SELECT 5", "INSERT 0 1", "UPDATE 3", "DELETE 2"
    fn parse_pg_command_complete(&self, response: &str) -> u64 {
        let parts: Vec<&str> = response.split_whitespace().collect();
        parts.last()
            .and_then(|s| s.parse::<u64>().ok())
            .unwrap_or(0)
    }

    /// Read a null-terminated string from bytes.
    fn read_null_string(&self, data: &[u8]) -> String {
        let end = data.iter().position(|&b| b == 0).unwrap_or(data.len());
        String::from_utf8_lossy(&data[..end]).to_string()
    }

    /// Truncate query to 1KB for storage.
    fn truncate_query(&self, query: &str) -> String {
        if query.len() > 1024 {
            format!("{}...[truncated]", &query[..1024])
        } else {
            query.to_string()
        }
    }

    /// Generate a socket key for pending request tracking.
    fn socket_key(&self, pid: u32, port: u16) -> u64 {
        ((pid as u64) << 16) | (port as u64)
    }

    /// Parse Redis RESP array to extract command and key.
    fn parse_redis_resp(&self, data: &str) -> Result<(String, String)> {
        // Simple parser: extract first two elements
        let lines: Vec<&str> = data.split("\r\n").collect();
        let mut args = Vec::new();
        let mut i = 0;
        while i < lines.len() {
            if lines[i].starts_with('$') {
                if i + 1 < lines.len() {
                    args.push(lines[i + 1].to_string());
                }
                i += 2;
            } else {
                i += 1;
            }
        }
        let command = args.first().cloned().unwrap_or_default();
        let key_prefix = args.get(1)
            .map(|k| k.split(':').next().unwrap_or(k).to_string())
            .unwrap_or_default();
        Ok((command, key_prefix))
    }

    /// Parse Redis inline command.
    fn parse_redis_inline(&self, data: &str) -> Result<(String, String)> {
        let parts: Vec<&str> = data.trim().split_whitespace().collect();
        let command = parts.first().unwrap_or(&"").to_string();
        let key_prefix = parts.get(1)
            .map(|k| k.split(':').next().unwrap_or(k).to_string())
            .unwrap_or_default();
        Ok((command, key_prefix))
    }

    /// Classify Redis command type.
    fn redis_command_type(&self, command: &str) -> String {
        let cmd = command.to_uppercase();
        match cmd.as_str() {
            "GET" | "MGET" | "HGET" | "HGETALL" | "LRANGE" | "SMEMBERS" | "ZRANGE" => "READ".to_string(),
            "SET" | "MSET" | "HSET" | "LPUSH" | "RPUSH" | "SADD" | "ZADD" | "DEL" | "HDEL" => "WRITE".to_string(),
            "INCR" | "DECR" | "INCRBY" | "DECRBY" => "WRITE".to_string(),
            "EXPIRE" | "TTL" | "PERSIST" => "KEY".to_string(),
            "PING" | "INFO" | "DBSIZE" | "FLUSHDB" => "ADMIN".to_string(),
            _ => "OTHER".to_string(),
        }
    }

    /// Get current statistics.
    pub fn stats(&self) -> InspectorStats {
        self.stats.lock().unwrap().clone()
    }
}

impl Default for DbInspector {
    fn default() -> Self {
        Self::new()
    }
}
```

### 6.5 Proto Update

The existing `ebpf.proto` already has the `DbQueryEvent` message with all needed fields. No proto changes required.

### 6.6 Integration with Pipeline

The DB query events flow through the same pipeline as other eBPF events:

```
eBPF kprobe → ring buffer → Rust userspace parser → DbQueryEvent → gRPC → Cluster ingestion
→ Redpanda (paryty.network.events) → Pipeline enricher → QuestDB (db_queries table)
```

---

## 7. Configuration Changes

### 7.1 Cluster Config Update

**File:** `configs/cluster/cluster.yaml` — Add storage productionization:

```yaml
# Existing sections updated:

storage:
  hot:
    addr: "redis://dragonfly:6379"
    pool_size: 20
    # New for Phase 4:
    topology:
      lock_retries: 3
      lock_backoff_ms: 10
    metrics:
      sorted_set_ttl: "5m"
      purge_interval: "1m"
    alerts:
      dedup_enabled: true
    connections:
      heartbeat_ttl: "30s"
      disconnect_threshold: "60s"
  
  warm:
    addr: "questdb:8812"
    database: "paryty"
    username: "paryty"
    password: "${QUESTDB_PASSWORD}"
    max_conns: 20
    # New for Phase 4:
    ilp:
      addr: "questdb:9009"
      buffer_size: 10000
      flush_interval: "1s"
    rest:
      endpoint: "http://questdb:9000"
    schema:
      auto_migrate: true
    retention:
      run_interval: "1h"
      rules:
        - table: "cpu_metrics"
          max_age_days: 30
          archive_to_cold: true
        - table: "memory_metrics"
          max_age_days: 30
          archive_to_cold: true
        - table: "aggregated_metrics"
          max_age_days: 90
          archive_to_cold: true
        - table: "db_queries"
          max_age_days: 14
          archive_to_cold: true
  
  cold:
    endpoint: "seaweedfs:8333"
    # New for Phase 4:
    snapshots:
      interval: "5m"
      compression: true
      retention_days: 7
    cache:
      enabled: true
      max_entries: 100
      ttl: "1h"

# Agent config update:
agent:
  ebpf:
    enabled: true
    tcp_connections: true
    dns_resolution: true
    http_inspection: true
    db_inspection: true        # NEW — enables DB protocol inspection
    db_protocols:              # NEW — which DB protocols to parse
      - postgresql
      - mysql
      - redis
    db_payload_bytes: 256      # NEW — bytes to capture per packet
```

---

## 8. Verification Gates

### Gate 1: Atomic Topology Updates (Automated)

```go
// TestTopologyOps_ConcurrentUpdate
// 1. Start 10 goroutines all updating topology simultaneously
// 2. Each adds a unique node
// 3. Verify: all 10 nodes present in final topology
// 4. Verify: no data loss (optimistic locking prevents conflicts)

// TestTopologyOps_WatchConflict
// 1. Start two goroutines
// 2. Goroutine A: WATCH, read, sleep 100ms, write
// 3. Goroutine B: WATCH, read, write immediately
// 4. Verify: Goroutine A's EXEC fails, retries, succeeds
// 5. Verify: final topology contains both changes

// TestTopologyOps_ApplyDiff
// 1. Store initial topology with 5 nodes, 4 edges
// 2. Apply diff: add 2 nodes, remove 1 edge
// 3. Verify: topology has 7 nodes, 3 edges
```

### Gate 2: Hybrid Ingestion (Automated)

```go
// TestILPWriter_WriteBatch
// 1. Connect to QuestDB ILP endpoint
// 2. Write 10,000 metrics via ILP
// 3. Verify: all metrics queryable via SQL
// 4. Verify: latency < 1 second for 10K metrics

// TestRESTWriter_BulkInsert
// 1. Generate 100,000 aggregated metrics
// 2. Insert via REST API COPY command
// 3. Verify: all metrics queryable
// 4. Verify: latency < 5 seconds for 100K metrics

// TestHybridIngestion_RealtimeVsBatch
// 1. Send 100 metrics via ILP (real-time path)
// 2. Send 10,000 metrics via REST (batch path)
// 3. Verify: ILP metrics available within 100ms
// 4. Verify: REST metrics available within 5 seconds
```

### Gate 3: Schema Migration (Automated)

```go
// TestSchemaManager_FirstRun
// 1. Drop all tables
// 2. Run Migrate()
// 3. Verify: all tables created with correct schema
// 4. Verify: schema version = SchemaVersion

// TestSchemaManager_Idempotent
// 1. Run Migrate() twice
// 2. Verify: no errors, schema unchanged
// 3. Verify: data preserved

// TestSchemaManager_Upgrade
// 1. Set schema version to SchemaVersion - 1
// 2. Run Migrate()
// 3. Verify: new columns/tables added
// 4. Verify: existing data preserved
```

### Gate 4: Timeline Snapshots (Automated)

```go
// TestSnapshotManager_TakeSnapshot
// 1. Store topology and metrics in hot storage
// 2. Take snapshot
// 3. Verify: snapshot contains correct topology, agents, metrics
// 4. Verify: snapshot uploaded to SeaweedFS
// 5. Verify: snapshot cached in Dragonfly

// TestSnapshotManager_LRUHit
// 1. Take snapshot
// 2. GetSnapshot (cache miss → fetches from SeaweedFS)
// 3. GetSnapshot again (cache hit → no SeaweedFS fetch)
// 4. Verify: second call is 10x faster

// TestSnapshotManager_ReconstructState
// 1. Take full snapshot at T=0
// 2. Record events at T=1m, T=2m, T=3m
// 3. Reconstruct state at T=2m
// 4. Verify: reconstructed state reflects events at T=1m and T=2m
// 5. Verify: event at T=3m NOT included

// TestEventLog_ReplayEvents
// 1. Create base snapshot with 3 nodes
// 2. Create events: node_added, edge_added, node_removed
// 3. Replay events on snapshot
// 4. Verify: final state has 2 nodes, 1 edge (3 + 1 - 1 = 3... wait, 3 + 1 added - 1 removed = 3)
// 5. Verify: new edge present
```

### Gate 5: eBPF DB Inspection (Automated)

```go
// TestDbInspector_PostgresqlQuery
// 1. Capture "Q\x00\x00\x00\x19SELECT * FROM users\x00"
// 2. Verify: protocol="postgresql", query_type="SELECT", table_name="users"
// 3. Verify: no parse errors

// TestDbInspector_MysqlQuery
// 1. Capture MySQL COM_QUERY packet
// 2. Verify: protocol="mysql", query extracted correctly

// TestDbInspector_RedisResp
// 1. Capture "*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n"
// 2. Verify: protocol="redis", command="SET", key_prefix="mykey"
// 3. Verify: query_type="WRITE"

// TestDbInspector_RedisInline
// 1. Capture "GET mykey\r\n"
// 2. Verify: protocol="redis", command="GET", key_prefix="mykey"
// 3. Verify: query_type="READ"

// TestDbInspector_TlsFallback
// 1. Capture TLS ClientHello (0x16 0x03 ...) to port 5432
// 2. Verify: protocol="encrypted", is_encrypted=true
// 3. Verify: no query extracted

// TestDbInspector_NonDatabasePort
// 1. Capture random payload to port 8080
// 2. Verify: returns None (not a database connection)
```

### Gate 6: Query Optimization (Automated)

```go
// TestQueryOptimizer_CacheHit
// 1. Query metrics (cache miss → QuestDB)
// 2. Query same metrics again (cache hit → Dragonfly)
// 3. Verify: second query is 10x faster
// 4. Verify: results identical

// TestQueryOptimizer_AutoDownsampling
// 1. Query 1-hour range → uses raw metrics
// 2. Query 24-hour range → uses 5m aggregated metrics
// 3. Query 7-day range → uses 1h aggregated metrics
// 4. Verify: different data resolutions returned

// TestQueryOptimizer_CacheInvalidation
// 1. Query metrics (cache miss → cache result)
// 2. Invalidate cache for agent
// 3. Query same metrics (cache miss → fresh query)
// 4. Verify: fresh data returned
```

### Gate 7: Retention Management (Automated)

```go
// TestRetentionManager_RunRetention
// 1. Insert metrics 31 days old
// 2. Run retention (30-day rule)
// 3. Verify: old metrics deleted from QuestDB
// 4. Verify: old metrics archived to SeaweedFS (if archive_to_cold=true)

// TestRetentionManager_DropPartition
// 1. Create partition for 2024-01-01
// 2. Drop partition
// 3. Verify: partition gone, other partitions intact
```

### Gate 8: Performance Benchmarks (Automated)

```go
// BenchmarkTopologyOps_AtomicUpdate — Target: < 5ms per update
// BenchmarkILPWriter_10KMetrics — Target: < 1 second
// BenchmarkRESTWriter_100KMetrics — Target: < 5 seconds
// BenchmarkQueryOptimizer_CacheHit — Target: < 1ms
// BenchmarkQueryOptimizer_CacheMiss — Target: < 50ms
// BenchmarkSnapshotManager_TakeSnapshot — Target: < 2 seconds
// BenchmarkSnapshotManager_ReconstructState — Target: < 500ms
// BenchmarkColdCache_GetOrFetch — Target: < 10ms (cache hit), < 200ms (cache miss)
// BenchmarkDbInspector_ParsePostgresql — Target: < 100μs per query
// BenchmarkDbInspector_ParseRedis — Target: < 50μs per command
```

---

## 9. Performance Targets

### 9.1 Latency Targets

| Operation | Target (p50) | Target (p99) | Measurement |
|-----------|-------------|-------------|-------------|
| Topology atomic update | < 2ms | < 10ms | WATCH/MULTI/EXEC cycle |
| ILP single metric write | < 1ms | < 5ms | Time to buffer in ILP writer |
| ILP batch flush (10K metrics) | < 500ms | < 2s | Time to flush buffer to QuestDB |
| REST bulk insert (100K rows) | < 3s | < 10s | COPY command execution |
| Query with cache hit | < 2ms | < 10ms | Dragonfly GET |
| Query with cache miss | < 50ms | < 200ms | QuestDB query |
| Snapshot creation | < 1s | < 3s | Full snapshot cycle |
| Snapshot retrieval (cached) | < 5ms | < 20ms | Dragonfly GET + decompress |
| Snapshot retrieval (uncached) | < 200ms | < 500ms | SeaweedFS GET + decompress |
| State reconstruction | < 500ms | < 2s | Snapshot + event replay |
| DB query parse (PostgreSQL) | < 50μs | < 200μs | Protocol detection + extraction |
| DB query parse (Redis) | < 20μs | < 100μs | RESP parsing |

### 9.2 Throughput Targets

| Metric | Target | Notes |
|--------|--------|-------|
| ILP ingestion rate | > 100,000 rows/sec | Sustained write throughput |
| REST bulk insert rate | > 500,000 rows/sec | Batch operations |
| Query throughput | > 500 queries/sec | With cache enabled |
| Snapshot frequency | 1 per 5 minutes | Full checkpoint |
| Event log throughput | > 10,000 events/sec | Inter-snapshot events |

### 9.3 Resource Targets

| Resource | Target | Notes |
|----------|--------|-------|
| Dragonfly memory (hot store) | < 2 GB | Including query cache |
| Dragonfly memory (cold cache) | < 1 GB | 100 snapshots × ~10MB |
| QuestDB disk | < 100 GB | 30 days of metrics |
| SeaweedFS disk | < 50 GB | 7 days of snapshots + archives |
| eBPF memory | < 10 MB | Ring buffers + maps |

---

## 10. Contingency & Rollback

### 10.1 Rollback Strategy

**Scenario 1: Schema migration fails**
- Migration is idempotent — can be re-run
- Each migration is a separate SQL script
- Rollback: restore from QuestDB WAL checkpoint

**Scenario 2: ILP connection unstable**
- Fall back to REST API for all writes
- ILP writer has automatic reconnection with exponential backoff
- Monitor: ILP connection count, write error rate

**Scenario 3: Snapshot creation too slow**
- Increase snapshot interval (5m → 10m)
- Reduce snapshot scope (skip metrics, only topology)
- Async snapshot creation (don't block pipeline)

**Scenario 4: eBPF DB inspection causes performance issues**
- Disable per-protocol: set `db_protocols: [postgresql]` (only one)
- Reduce payload capture: `db_payload_bytes: 128` (from 256)
- Disable entirely: `db_inspection: false`

**Scenario 5: Cold cache memory pressure**
- Reduce `max_entries` from 100 to 50
- Reduce `ttl` from 1h to 30m
- Disable cache: `cache.enabled: false`

### 10.2 Degradation Modes

| Mode | Trigger | Behavior |
|------|---------|----------|
| Full | All components healthy | Full storage + DB inspection |
| No ILP | ILP connection fails | REST-only ingestion |
| No Cache | Dragonfly unavailable | Direct SeaweedFS queries |
| No Snapshots | Snapshot creation fails | Event log only (no checkpoints) |
| No DB Inspection | eBPF load fails | Connection-only detection |
| Minimal | Multiple failures | Basic CRUD only, no optimization |

---

## 11. Appendices

### Appendix A: Dragonfly Key Schema (Phase 4 Additions)

```
# Existing keys (keep):
paryty:topology:current              → JSON (Topology)
paryty:metrics:{agent_id}:latest     → JSON (MetricBatch)
paryty:alerts:active                 → JSON ([]Alert)
paryty:agent:{agent_id}              → JSON (AgentInfo)
paryty:health:{agent_id}             → JSON (HealthReport)

# New for Phase 4:
paryty:topology:version              → string (optimistic lock version)
paryty:metrics:ts:{agent}:{metric}   → sorted set (score=timestamp, value=MetricValue)
paryty:connections:{agent_id}        → JSON (ConnectionInfo)
paryty:alerts:fingerprint:{hash}     → string (alert state)
paryty:snapshot:latest               → compressed JSON (latest snapshot)
paryty:snapshot:{id}                 → compressed JSON (cached snapshot)
paryty:cold:snapshot:{id}            → compressed JSON (LRU cache)
paryty:cold:query:{hash}             → JSON (query cache)
paryty:query_cache:{key}             → JSON (query result cache)
paryty:pipeline:window:snapshot      → JSON (window state)
paryty:pipeline:graph:snapshot       → JSON (dependency graph)
```

### Appendix B: Files to Create/Modify

| File | Action | LOC | Description |
|------|--------|-----|-------------|
| `cluster/internal/storage/hot/topology_ops.go` | CREATE | ~1,000 | Atomic topology updates |
| `cluster/internal/storage/hot/topology_ops_test.go` | CREATE | ~400 | Concurrency tests |
| `cluster/internal/storage/hot/metrics_ops.go` | CREATE | ~1,000 | Sorted set metrics |
| `cluster/internal/storage/hot/metrics_ops_test.go` | CREATE | ~300 | Range query tests |
| `cluster/internal/storage/hot/alert_ops.go` | CREATE | ~500 | Alert state machine |
| `cluster/internal/storage/hot/alert_ops_test.go` | CREATE | ~200 | State transition tests |
| `cluster/internal/storage/hot/connection_ops.go` | CREATE | ~500 | Connection tracking |
| `cluster/internal/storage/hot/connection_ops_test.go` | CREATE | ~200 | Disconnect detection tests |
| `cluster/internal/storage/warm/schema.go` | CREATE | ~1,000 | Schema definitions and migration |
| `cluster/internal/storage/warm/schema_test.go` | CREATE | ~300 | Migration tests |
| `cluster/internal/storage/warm/ilp_writer.go` | CREATE | ~800 | ILP writer |
| `cluster/internal/storage/warm/ilp_writer_test.go` | CREATE | ~300 | ILP tests |
| `cluster/internal/storage/warm/rest_writer.go` | CREATE | ~700 | REST bulk insert |
| `cluster/internal/storage/warm/rest_writer_test.go` | CREATE | ~200 | REST tests |
| `cluster/internal/storage/warm/query_optimizer.go` | CREATE | ~1,000 | Query caching and optimization |
| `cluster/internal/storage/warm/query_optimizer_test.go` | CREATE | ~300 | Cache tests |
| `cluster/internal/storage/warm/retention.go` | CREATE | ~500 | Retention management |
| `cluster/internal/storage/warm/retention_test.go` | CREATE | ~200 | Retention tests |
| `cluster/internal/storage/cold/snapshot.go` | CREATE | ~1,500 | Timeline snapshot manager |
| `cluster/internal/storage/cold/snapshot_test.go` | CREATE | ~500 | Snapshot and reconstruction tests |
| `cluster/internal/storage/cold/eventlog.go` | CREATE | ~800 | Event log indexer |
| `cluster/internal/storage/cold/eventlog_test.go` | CREATE | ~300 | Replay tests |
| `cluster/internal/storage/cold/cache.go` | CREATE | ~700 | LRU cache |
| `cluster/internal/storage/cold/cache_test.go` | CREATE | ~200 | Cache eviction tests |
| `cluster/internal/storage/store.go` | ENHANCE | +200 | New methods |
| `cluster/internal/storage/store_test.go` | UPDATE | +100 | New test cases |
| `agent/src/ebpf/db_inspector.rs` | REWRITE | ~1,500 | Full protocol parsing |
| `agent/src/ebpf/ebpf/db_probe.c` | CREATE | ~300 | C eBPF program |
| `agent/src/ebpf/ebpf/common.h` | MODIFY | +30 | db_event struct |
| `configs/cluster/cluster.yaml` | MODIFY | +80 | Storage and DB inspection config |
| **TOTAL** | | **~13,230** | |

### Appendix C: Files to Keep Unchanged

The following files are NOT modified in Phase 4:

- `cluster/internal/storage/hot/dragonfly.go` — Base client (new code in separate files)
- `cluster/internal/storage/warm/questdb.go` — Base client (new code in separate files)
- `cluster/internal/storage/cold/seaweedfs.go` — Base client (new code in separate files)
- `proto/paryty/v1/ebpf.proto` — Already has DbQueryEvent message
- `agent/src/ebpf/mod.rs` — Module structure unchanged
- `agent/src/ebpf/loader.rs` — eBPF loader unchanged
- `agent/src/ebpf/tcp_tracker.rs` — TCP tracker unchanged
- `agent/src/ebpf/http_inspector.rs` — HTTP inspector unchanged
- `agent/src/ebpf/dns_mapper.rs` — DNS mapper unchanged

### Appendix D: Go Coding Discipline

```
1. OPTIMISTIC LOCKING
   - Always retry on WATCH/MULTI/EXEC failure
   - Exponential backoff: 10ms, 20ms, 30ms
   - Max 3 retries before returning error
   - Log each retry attempt

2. ILP WRITER
   - Buffer writes and flush periodically (every 1 second)
   - Flush on buffer size threshold (10,000 rows)
   - Auto-reconnect on connection loss
   - Track rows written, errors, latency

3. SCHEMA MIGRATION
   - Idempotent: safe to run multiple times
   - Each migration is a separate SQL script
   - Track version in paryty_schema_version table
   - Never delete data during migration

4. RETENTION
   - Archive to cold storage before deleting
   - Use QuestDB partition dropping (instant, no scan)
   - Log all retention actions
   - Never delete the most recent partition

5. SNAPSHOTS
   - Compress with Zstd before uploading
   - Cache in Dragonfly after creation
   - Async creation (don't block pipeline)
   - Validate snapshot integrity after creation

6. EDB INSPECTION
   - Parse only first 256 bytes (no full payload)
   - TLS detection: check for 0x16 0x03 header
   - Port-based protocol hint, then verify with payload
   - Graceful degradation: connection-only for encrypted
```

---

**END OF PHASE 4 HARDENED SPECIFICATION**
