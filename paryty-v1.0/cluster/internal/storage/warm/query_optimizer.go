// Package warm — Query optimizer for QuestDB warm storage.
//
// Adds Dragonfly-backed result caching and automatic time-based downsampling
// to QuestDB queries. For large time ranges, the optimizer selects pre-aggregated
// tables (SAMPLE BY windows) instead of scanning raw metric rows, reducing
// query latency by orders of magnitude for dashboard and historical queries.
//
// Cache keys are tenant-scoped to maintain strict tenant isolation:
//
//	paryty:{tenant}:query_cache:{hash}
//
// Cache stats (hits/misses) are tracked per-tenant in Dragonfly counters.
package warm

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
)

// ---- Constants ----

const (
	// defaultCacheTTL is the default TTL for cached query results.
	defaultCacheTTL = 30 * time.Second

	// defaultCachePrefix is the default key prefix for query cache entries.
	defaultCachePrefix = "query_cache"

	// defaultQueryLimit is the maximum number of rows returned from QuestDB
	// when no explicit limit is specified. Prevents unbounded result sets
	// from exhausting memory on wide time ranges.
	defaultQueryLimit = 10000

	// compressThresholdBytes is the minimum payload size (in bytes) before
	// gzip compression is applied. Below this threshold, the overhead of
	// compression outweighs the storage savings.
	compressThresholdBytes = 1024

	// gzipHeaderByte is the first byte prepended to gzip-compressed cache
	// entries. The cache reader checks this byte to decide whether to
	// decompress. Any first byte that is NOT this value is treated as raw JSON
	// (backward compatibility with entries written before compression was added).
	gzipHeaderByte = byte('G')

	// rawJSONHeaderByte is the first byte prepended to uncompressed JSON cache
	// entries. This ensures a consistent header format for all cache entries.
	rawJSONHeaderByte = byte('J')
)

// ---- Metric-to-Table Mapping ----

// metricTableMap maps dotted metric names to their QuestDB table.
// The aggregator emits metrics like "cpu.total_usage" which map to the
// cpu_metrics table's total_usage_pct column.
//
// Metrics not in this map fall back to the aggregated_metrics table,
// which stores pre-aggregated windowed data for any metric name.
var metricTableMap = map[string]string{
	// CPU metrics → cpu_metrics table
	"cpu.usage":            "cpu_metrics",
	"cpu.total_usage":      "cpu_metrics",
	"cpu.load_avg_1":       "cpu_metrics",
	"cpu.load_avg_5":       "cpu_metrics",
	"cpu.load_avg_15":      "cpu_metrics",
	"cpu.frequency":        "cpu_metrics",
	"cpu.context_switches": "cpu_metrics",
	"cpu.cores_physical":   "cpu_metrics",
	"cpu.cores_logical":    "cpu_metrics",

	// Memory metrics → memory_metrics table
	"memory.used":          "memory_metrics",
	"memory.total":         "memory_metrics",
	"memory.available":     "memory_metrics",
	"memory.free":          "memory_metrics",
	"memory.cached":        "memory_metrics",
	"memory.swap_used":     "memory_metrics",
	"memory.swap_total":    "memory_metrics",
	"memory.usage_percent": "memory_metrics",

	// Disk metrics → disk_metrics table
	"disk.used":        "disk_metrics",
	"disk.total":       "disk_metrics",
	"disk.free":        "disk_metrics",
	"disk.read_bytes":  "disk_metrics",
	"disk.write_bytes": "disk_metrics",
	"disk.iops_read":   "disk_metrics",
	"disk.iops_write":  "disk_metrics",
	"disk.io_latency":  "disk_metrics",
	"disk.utilization": "disk_metrics",

	// Network metrics → network_metrics table
	"network.rx_bytes":        "network_metrics",
	"network.tx_bytes":        "network_metrics",
	"network.rx_packets":      "network_metrics",
	"network.tx_packets":      "network_metrics",
	"network.rx_dropped":      "network_metrics",
	"network.tx_dropped":      "network_metrics",
	"network.errors":          "network_metrics",
	"network.estimated_rtt":   "network_metrics",
	"network.tcp_established": "network_metrics",
	"network.tcp_time_wait":   "network_metrics",
	"network.tcp_retransmits": "network_metrics",

	// Process metrics → process_metrics table
	"process.cpu_usage":    "process_metrics",
	"process.memory":       "process_metrics",
	"process.threads":      "process_metrics",
	"process.fd_count":     "process_metrics",
	"process.disk_read":    "process_metrics",
	"process.disk_written": "process_metrics",
}

// metricColumnMap maps dotted metric names to the column name in their table.
// Each entry in metricTableMap MUST have a corresponding entry here.
var metricColumnMap = map[string]string{
	// CPU
	"cpu.usage":            "total_usage_pct",
	"cpu.total_usage":      "total_usage_pct",
	"cpu.load_avg_1":       "load_avg_1",
	"cpu.load_avg_5":       "load_avg_5",
	"cpu.load_avg_15":      "load_avg_15",
	"cpu.frequency":        "frequency_mhz",
	"cpu.context_switches": "context_switches",
	"cpu.cores_physical":   "physical_cores",
	"cpu.cores_logical":    "logical_cores",

	// Memory
	"memory.used":          "used_bytes",
	"memory.total":         "total_bytes",
	"memory.available":     "available_bytes",
	"memory.free":          "free_bytes",
	"memory.cached":        "cached_bytes",
	"memory.swap_used":     "swap_used_bytes",
	"memory.swap_total":    "swap_total_bytes",
	"memory.usage_percent": "usage_percent",

	// Disk
	"disk.used":        "used_bytes",
	"disk.total":       "total_bytes",
	"disk.free":        "free_bytes",
	"disk.read_bytes":  "read_bytes_per_sec",
	"disk.write_bytes": "write_bytes_per_sec",
	"disk.iops_read":   "iops_read",
	"disk.iops_write":  "iops_write",
	"disk.io_latency":  "io_latency_ms",
	"disk.utilization": "utilization_pct",

	// Network
	"network.rx_bytes":        "rx_bytes_per_sec",
	"network.tx_bytes":        "tx_bytes_per_sec",
	"network.rx_packets":      "rx_packets",
	"network.tx_packets":      "tx_packets",
	"network.rx_dropped":      "rx_dropped",
	"network.tx_dropped":      "tx_dropped",
	"network.errors":          "errors",
	"network.estimated_rtt":   "estimated_rtt_ms",
	"network.tcp_established": "tcp_established",
	"network.tcp_time_wait":   "tcp_time_wait",
	"network.tcp_retransmits": "tcp_retransmit_count",

	// Process
	"process.cpu_usage":    "cpu_usage_pct",
	"process.memory":       "memory_bytes",
	"process.threads":      "threads",
	"process.fd_count":     "fd_count",
	"process.disk_read":    "disk_read_bytes",
	"process.disk_written": "disk_written_bytes",
}

// ---- Types ----

// DbQueryRecord represents a single database query captured via eBPF.
// It stores the full context of an observed SQL query execution including
// the target database, query type, latency, and optional error information.
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

// CacheStats holds hit/miss statistics for the query cache.
// Counters are stored in Dragonfly and persist across process restarts
// (within the counter TTL window).
type CacheStats struct {
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	HitRate float64 `json:"hit_rate"`
	Size    int64   `json:"size"`
}

// DownsampleResolution describes the query strategy for a time range.
// The optimizer selects a resolution based on the requested time span
// to balance query latency against data granularity.
type DownsampleResolution struct {
	// Table is the QuestDB table to query (e.g., "cpu_metrics", "aggregated_metrics").
	Table string
	// WindowSeconds is the aggregation window in seconds.
	// Zero means raw (un-aggregated) data.
	WindowSeconds int64
	// IsRaw indicates whether this resolution queries raw metric tables
	// (vs. the aggregated_metrics table).
	IsRaw bool
}

// ---- QueryCache ----

// QueryCache provides Dragonfly-backed caching for QuestDB query results.
//
// Cache entries are stored with a configurable TTL (default 30s) and are
// automatically gzip-compressed when the serialized payload exceeds 1KB.
// Each cache entry is prefixed with a header byte indicating compression:
//   - 'G' = gzip-compressed JSON
//   - 'J' = raw JSON (below compression threshold)
//
// All keys are namespaced as paryty:{tenant}:query_cache: to guarantee
// strict tenant isolation in the shared Dragonfly instance.
type QueryCache struct {
	dragonfly *redis.Client
	ttl       time.Duration
	prefix    string
	logger    *slog.Logger
}

// CacheOption is a functional option for configuring QueryCache.
type CacheOption func(*QueryCache)

// WithCacheTTL sets the TTL for cached query results.
// If d is zero or negative, the default TTL (30s) is used.
func WithCacheTTL(d time.Duration) CacheOption {
	return func(c *QueryCache) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// WithCachePrefix sets the key prefix for cached entries.
// The prefix is inserted into the key as: paryty:{tenant}:{prefix}:{hash}
func WithCachePrefix(p string) CacheOption {
	return func(c *QueryCache) {
		if p != "" {
			c.prefix = p
		}
	}
}

// NewQueryCache creates a new QueryCache backed by the given Dragonfly client.
// If logger is nil, a default slog logger is used.
func NewQueryCache(dragonfly *redis.Client, logger *slog.Logger, opts ...CacheOption) *QueryCache {
	if logger == nil {
		logger = slog.Default()
	}
	c := &QueryCache{
		dragonfly: dragonfly,
		ttl:       defaultCacheTTL,
		prefix:    defaultCachePrefix,
		logger:    logger,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// cacheKey builds a tenant-scoped cache key from query parameters.
// The key includes all parameters that affect the query result to ensure
// that different queries never collide in the cache.
func (c *QueryCache) cacheKey(tenant, agentID, metricName string, start, end time.Time) string {
	var b strings.Builder
	// Pre-allocate: prefix + tenant + agent + metric + timestamps.
	b.Grow(len(c.prefix) + len(tenant) + len(agentID) + len(metricName) + 64)
	b.WriteString("paryty:")
	b.WriteString(tenant)
	b.WriteByte(':')
	b.WriteString(c.prefix)
	b.WriteByte(':')
	b.WriteString(agentID)
	b.WriteByte(':')
	b.WriteString(metricName)
	b.WriteByte(':')
	b.WriteString(start.UTC().Format(time.RFC3339))
	b.WriteByte(':')
	b.WriteString(end.UTC().Format(time.RFC3339))
	return b.String()
}

// hitsKey returns the Dragonfly key for cache hit counters for a tenant.
func (c *QueryCache) hitsKey(tenant string) string {
	return fmt.Sprintf("paryty:%s:query_cache_stats:hits", tenant)
}

// missesKey returns the Dragonfly key for cache miss counters for a tenant.
func (c *QueryCache) missesKey(tenant string) string {
	return fmt.Sprintf("paryty:%s:query_cache_stats:misses", tenant)
}

// ---- Cache Internal Helpers ----

// cacheSetRaw stores raw JSON bytes in the cache with a compression header.
// Payloads exceeding compressThresholdBytes are gzip-compressed before storage.
// The header byte ('G' or 'J') is prepended so the reader can detect compression.
//
// This method never returns an error that should cause a query to fail —
// cache writes are best-effort. Errors are logged and the query proceeds.
func (c *QueryCache) cacheSetRaw(ctx context.Context, key string, data []byte) {
	var payload []byte

	if len(data) > compressThresholdBytes {
		var buf bytes.Buffer
		buf.WriteByte(gzipHeaderByte)
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(data); err != nil {
			c.logger.Warn("gzip compress cache entry failed, storing uncompressed",
				"key", key, "error", err)
			payload = make([]byte, 1+len(data))
			payload[0] = rawJSONHeaderByte
			copy(payload[1:], data)
		} else {
			if err := gz.Close(); err != nil {
				c.logger.Warn("gzip close failed", "key", key, "error", err)
				payload = make([]byte, 1+len(data))
				payload[0] = rawJSONHeaderByte
				copy(payload[1:], data)
			} else {
				payload = buf.Bytes()
			}
		}
	} else {
		payload = make([]byte, 1+len(data))
		payload[0] = rawJSONHeaderByte
		copy(payload[1:], data)
	}

	if err := c.dragonfly.Set(ctx, key, payload, c.ttl).Err(); err != nil {
		c.logger.Warn("cache set failed (non-fatal)", "key", key, "error", err)
	}
}

// cacheGetRaw retrieves a cache entry and deserializes it into the target.
// It transparently handles gzip-compressed entries (header byte 'G') and
// raw JSON entries (header byte 'J'). If decompression fails, it falls back
// to interpreting the payload as raw JSON for backward compatibility.
func cacheGetRaw[T any](ctx context.Context, rdb *redis.Client, key string) (T, error) {
	var zero T

	payload, err := rdb.Get(ctx, key).Bytes()
	if err != nil {
		return zero, err
	}

	if len(payload) < 2 {
		return zero, fmt.Errorf("cache entry too short: %d bytes", len(payload))
	}

	header := payload[0]
	body := payload[1:]

	var jsonBytes []byte
	switch header {
	case gzipHeaderByte:
		gz, gzErr := gzip.NewReader(bytes.NewReader(body))
		if gzErr != nil {
			// Compressed flag but can't decompress — fall back to raw JSON
			// (handles corrupt entries or unexpected format).
			jsonBytes = payload
			break
		}
		decompressed, readErr := io.ReadAll(gz)
		_ = gz.Close()
		if readErr != nil {
			jsonBytes = payload
		} else {
			jsonBytes = decompressed
		}
	case rawJSONHeaderByte:
		jsonBytes = body
	default:
		// Unknown header — assume legacy format (entire payload is JSON).
		jsonBytes = payload
	}

	var result T
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return zero, fmt.Errorf("unmarshal cached result: %w", err)
	}
	return result, nil
}

// ---- QueryOptimizer ----

// QueryOptimizer adds caching and automatic downsampling to QuestDB queries.
//
// It wraps a pgxpool.Pool (QuestDB) and a redis.Client (Dragonfly) to provide:
//   - Result caching with configurable TTL (default 30s)
//   - Automatic downsample resolution selection based on time range
//   - Per-tenant cache hit/miss statistics
//   - db_queries table querying for database-level observability
//
// All methods are safe for concurrent use.
type QueryOptimizer struct {
	pool   *pgxpool.Pool
	cache  *QueryCache
	logger *slog.Logger
}

// NewQueryOptimizer creates a new QueryOptimizer.
// The pool connects to QuestDB (warm tier), the dragonfly client to the
// hot tier for caching. If logger is nil, a default slog logger is used.
func NewQueryOptimizer(pool *pgxpool.Pool, dragonfly *redis.Client, logger *slog.Logger) *QueryOptimizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &QueryOptimizer{
		pool:   pool,
		cache:  NewQueryCache(dragonfly, logger),
		logger: logger,
	}
}

// ---- Metric-to-Table Helpers ----

// MetricToTable maps a dotted metric name (e.g., "cpu.total_usage") to the
// QuestDB table name that stores the raw data for that metric.
// Returns "aggregated_metrics" for unknown metrics.
func MetricToTable(metricName string) string {
	if table, ok := metricTableMap[metricName]; ok {
		return table
	}
	return "aggregated_metrics"
}

// MetricToColumn maps a dotted metric name to the column name in the
// corresponding QuestDB table. Returns an empty string for unknown metrics
// (which use the aggregated_metrics table and don't need a column mapping).
func MetricToColumn(metricName string) string {
	if col, ok := metricColumnMap[metricName]; ok {
		return col
	}
	return ""
}

// ---- Downsample Resolution ----

// SelectDownsampleResolution selects the optimal query strategy based on the
// requested time range. The thresholds are:
//
//   - Range ≤ 1 hour:   raw metrics table, no aggregation (full granularity)
//   - Range ≤ 24 hours: aggregated_metrics with 5-minute windows
//   - Range > 24 hours: aggregated_metrics with 1-hour windows
//
// The window parameter returns the aggregation window duration that should be
// used in the aggregated_metrics query. For raw queries, window is zero.
func SelectDownsampleResolution(start, end time.Time) DownsampleResolution {
	rangeDur := end.Sub(start)

	switch {
	case rangeDur <= time.Hour:
		return DownsampleResolution{
			Table:         "", // Caller must select via MetricToTable
			WindowSeconds: 0,
			IsRaw:         true,
		}
	case rangeDur <= 24*time.Hour:
		return DownsampleResolution{
			Table:         "aggregated_metrics",
			WindowSeconds: 300, // 5-minute windows
			IsRaw:         false,
		}
	default:
		return DownsampleResolution{
			Table:         "aggregated_metrics",
			WindowSeconds: 3600, // 1-hour windows
			IsRaw:         false,
		}
	}
}

// ---- Query Operations ----

// QueryMetricsWithCache executes a metric query with Dragonfly-backed caching.
//
// On cache hit, the result is returned directly from Dragonfly (< 1ms).
// On cache miss, the query is executed against QuestDB, the result is cached,
// and then returned.
//
// The cache key includes all query parameters (tenant, agentID, metricName,
// start, end) to ensure that different queries never produce false hits.
//
// Cache failures are non-fatal: the query result is still returned even if
// caching fails. This ensures that Dragonfly outages do not affect query
// availability.
func (qo *QueryOptimizer) QueryMetricsWithCache(ctx context.Context, tenant, agentID, metricName string, start, end time.Time) ([]models.Metric, error) {
	key := qo.cache.cacheKey(tenant, agentID, metricName, start, end)

	// Try cache first.
	if metrics, err := cacheGetRaw[[]models.Metric](ctx, qo.cache.dragonfly, key); err == nil {
		// Cache hit.
		_ = qo.cache.dragonfly.Incr(ctx, qo.cache.hitsKey(tenant)).Err()
		qo.logger.Debug("cache hit", "key", key, "count", len(metrics))
		return metrics, nil
	}

	// Cache miss — query QuestDB.
	_ = qo.cache.dragonfly.Incr(ctx, qo.cache.missesKey(tenant)).Err()
	qo.logger.Debug("cache miss, querying QuestDB", "key", key)

	metrics, err := qo.queryRawMetrics(ctx, tenant, agentID, metricName, start, end)
	if err != nil {
		return nil, fmt.Errorf("query metrics with cache: %w", err)
	}

	// Ensure we always cache a non-nil slice (prevents JSON "null").
	if metrics == nil {
		metrics = []models.Metric{}
	}

	// Cache the result (best-effort).
	data, err := json.Marshal(metrics)
	if err != nil {
		qo.logger.Warn("marshal metrics for cache failed (non-fatal)", "error", err)
		return metrics, nil
	}
	qo.cache.cacheSetRaw(ctx, key, data)

	return metrics, nil
}

// QueryWithDownsampling executes a metric query with automatic time-based
// downsampling. The resolution is selected based on the requested time range:
//
//   - ≤ 1 hour:   raw metric tables (full granularity, ~10s data points)
//   - ≤ 24 hours: aggregated_metrics with 5m window
//   - > 24 hours: aggregated_metrics with 1h window
//
// This dramatically reduces query latency for large time ranges by reading
// far fewer rows from QuestDB. For example, a 7-day query at 10s granularity
// would scan ~60,480 rows per metric; with 1h aggregation, it scans ~168 rows.
//
// Results are NOT cached by this method — use QueryMetricsWithCache for cached
// queries. This method is intended for direct queries where the caller manages
// their own caching or where caching is not desired.
func (qo *QueryOptimizer) QueryWithDownsampling(ctx context.Context, tenant, agentID, metricName string, start, end time.Time) ([]models.Metric, error) {
	res := SelectDownsampleResolution(start, end)

	if res.IsRaw {
		// Raw query — select the specific metric table.
		table := MetricToTable(metricName)
		if table == "aggregated_metrics" {
			// Unknown metric — fall back to aggregated table with 5m window.
			return qo.queryAggregatedMetrics(ctx, tenant, agentID, metricName, 300, start, end)
		}
		return qo.queryRawTable(ctx, tenant, agentID, metricName, table, start, end)
	}

	// Aggregated query.
	return qo.queryAggregatedMetrics(ctx, tenant, agentID, metricName, res.WindowSeconds, start, end)
}

// InvalidateCache removes all cached query results for a given tenant and agent.
// This should be called when new data arrives for an agent to prevent stale
// results from being served.
//
// Uses SCAN to find matching keys instead of KEYS to avoid blocking Dragonfly
// on large key spaces. Returns nil if no matching keys are found.
func (qo *QueryOptimizer) InvalidateCache(ctx context.Context, tenant, agentID string) error {
	// Build a pattern that matches all cache keys for this tenant/agent.
	pattern := fmt.Sprintf("paryty:%s:%s:%s:*", tenant, qo.cache.prefix, agentID)

	var cursor uint64
	var totalDeleted int64

	for {
		keys, nextCursor, err := qo.cache.dragonfly.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("scan cache keys for invalidation: %w", err)
		}

		if len(keys) > 0 {
			deleted, err := qo.cache.dragonfly.Del(ctx, keys...).Result()
			if err != nil {
				qo.logger.Warn("delete cache keys failed",
					"pattern", pattern, "count", len(keys), "error", err)
			} else {
				totalDeleted += deleted
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if totalDeleted > 0 {
		qo.logger.Info("cache invalidated",
			"tenant", tenant, "agent_id", agentID, "deleted", totalDeleted)
	}
	return nil
}

// QueryDatabaseQueries queries the db_queries table for database-level
// observability data. This table captures SQL query executions observed
// via eBPF probes on the target host.
//
// Results are filtered by tenant (strict isolation), agent, protocol
// (e.g., "mysql", "postgresql"), and time range. The limit parameter
// caps the number of returned rows (0 = defaultQueryLimit).
func (qo *QueryOptimizer) QueryDatabaseQueries(ctx context.Context, tenant, agentID, protocol string, start, end time.Time, limit int) ([]DbQueryRecord, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	var (
		query string
		args  []interface{}
	)

	if protocol != "" {
		query = `SELECT agent_id, protocol, query_type, table_name, database,
			destination_ip, destination_port, pid, process_name,
			latency_ms, row_count, error_message, query_sample, timestamp
		FROM db_queries
		WHERE tenant_id = $1 AND agent_id = $2 AND protocol = $3
			AND timestamp >= $4 AND timestamp <= $5
		ORDER BY timestamp DESC
		LIMIT $6`
		args = []interface{}{tenant, agentID, protocol, start, end, limit}
	} else {
		query = `SELECT agent_id, protocol, query_type, table_name, database,
			destination_ip, destination_port, pid, process_name,
			latency_ms, row_count, error_message, query_sample, timestamp
		FROM db_queries
		WHERE tenant_id = $1 AND agent_id = $2
			AND timestamp >= $3 AND timestamp <= $4
		ORDER BY timestamp DESC
		LIMIT $5`
		args = []interface{}{tenant, agentID, start, end, limit}
	}

	rows, err := qo.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query db_queries: %w", err)
	}
	defer rows.Close()

	var records []DbQueryRecord
	for rows.Next() {
		var r DbQueryRecord
		var portInt sql.NullInt32
		var pidInt sql.NullInt32
		var latencyMs sql.NullFloat64
		var rowCount sql.NullInt64
		var tableName, database, dstIP, processName, errMsg, querySample sql.NullString

		if err := rows.Scan(
			&r.AgentID, &r.Protocol, &r.QueryType,
			&tableName, &database, &dstIP, &portInt,
			&pidInt, &processName,
			&latencyMs, &rowCount, &errMsg, &querySample,
			&r.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan db_query: %w", err)
		}

		r.TableName = nullStr(tableName)
		r.Database = nullStr(database)
		r.DestinationIP = nullStr(dstIP)
		r.DestinationPort = int(nullInt32(portInt))
		r.PID = int(nullInt32(pidInt))
		r.ProcessName = nullStr(processName)
		r.LatencyMs = nullFloat64(latencyMs)
		r.RowCount = nullInt64(rowCount)
		r.ErrorMessage = nullStr(errMsg)
		r.QuerySample = nullStr(querySample)

		records = append(records, r)
	}

	if records == nil {
		return []DbQueryRecord{}, rows.Err()
	}
	return records, rows.Err()
}

// GetCacheStats returns hit/miss statistics for the query cache of a tenant.
// The size field reports the total number of keys matching the cache pattern
// for the tenant (approximated via SCAN).
func (qo *QueryOptimizer) GetCacheStats(ctx context.Context, tenant string) (CacheStats, error) {
	hits, err := qo.cache.dragonfly.Get(ctx, qo.cache.hitsKey(tenant)).Int64()
	if err != nil && err != redis.Nil {
		return CacheStats{}, fmt.Errorf("get cache hits: %w", err)
	}

	misses, err := qo.cache.dragonfly.Get(ctx, qo.cache.missesKey(tenant)).Int64()
	if err != nil && err != redis.Nil {
		return CacheStats{}, fmt.Errorf("get cache misses: %w", err)
	}

	total := hits + misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
		// Round to 4 decimal places for clean JSON output.
		hitRate = math.Round(hitRate*10000) / 10000
	}

	// Count cache keys for this tenant.
	size, err := qo.cacheSize(ctx, tenant)
	if err != nil {
		qo.logger.Warn("cache size count failed", "tenant", tenant, "error", err)
	}

	return CacheStats{
		Hits:    hits,
		Misses:  misses,
		HitRate: hitRate,
		Size:    size,
	}, nil
}

// cacheSize counts the number of cache keys for a tenant using SCAN.
func (qo *QueryOptimizer) cacheSize(ctx context.Context, tenant string) (int64, error) {
	pattern := fmt.Sprintf("paryty:%s:%s:*", tenant, qo.cache.prefix)
	var size int64
	var cursor uint64

	for {
		keys, nextCursor, err := qo.cache.dragonfly.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return 0, fmt.Errorf("scan cache size: %w", err)
		}
		size += int64(len(keys))
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return size, nil
}

// ---- Internal Query Methods ----

// queryRawMetrics queries a raw metric table (e.g., cpu_metrics) for a specific
// metric column. The table is selected via MetricToTable based on the metric name.
func (qo *QueryOptimizer) queryRawMetrics(ctx context.Context, tenant, agentID, metricName string, start, end time.Time) ([]models.Metric, error) {
	table := MetricToTable(metricName)
	if table == "aggregated_metrics" {
		// Unknown metric — can't query a specific column.
		// Fall back to aggregated_metrics with a 5m window.
		return qo.queryAggregatedMetrics(ctx, tenant, agentID, metricName, 300, start, end)
	}
	return qo.queryRawTable(ctx, tenant, agentID, metricName, table, start, end)
}

// queryRawTable queries a specific raw metric table for a single metric column.
// The metricColumnMap determines which column to select.
func (qo *QueryOptimizer) queryRawTable(ctx context.Context, tenant, agentID, metricName, table string, start, end time.Time) ([]models.Metric, error) {
	column := MetricToColumn(metricName)
	if column == "" {
		// No column mapping — fall back to aggregated_metrics.
		return qo.queryAggregatedMetrics(ctx, tenant, agentID, metricName, 300, start, end)
	}

	// SAFETY: table and column come from our internal maps, not user input.
	// They are constant strings, so this is NOT a SQL injection vector.
	query := fmt.Sprintf(
		`SELECT agent_id, %s, timestamp
		FROM %s
		WHERE tenant_id = $1 AND agent_id = $2
			AND timestamp >= $3 AND timestamp <= $4
		ORDER BY timestamp DESC
		LIMIT $5`,
		column, table,
	)

	rows, err := qo.pool.Query(ctx, query, tenant, agentID, start, end, defaultQueryLimit)
	if err != nil {
		return nil, fmt.Errorf("query raw %s: %w", table, err)
	}
	defer rows.Close()

	var metrics []models.Metric
	for rows.Next() {
		var m models.Metric
		var val interface{}
		if err := rows.Scan(&m.AgentID, &val, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("scan raw %s: %w", table, err)
		}
		m.Name = metricName
		m.Labels = map[string]string{"source": table}
		m.Type = models.MetricType(metricTypeFromTable(table))
		m.Value = interfaceToFloat64(val)
		metrics = append(metrics, m)
	}

	if metrics == nil {
		return []models.Metric{}, rows.Err()
	}
	return metrics, rows.Err()
}

// queryAggregatedMetrics queries the aggregated_metrics table for a specific
// metric name, aggregation window, and time range.
func (qo *QueryOptimizer) queryAggregatedMetrics(ctx context.Context, tenant, agentID, metricName string, windowSeconds int64, start, end time.Time) ([]models.Metric, error) {
	query := `SELECT agent_id, value, timestamp
		FROM aggregated_metrics
		WHERE tenant_id = $1 AND agent_id = $2 AND name = $3
			AND window = $4 AND agg_type = 'avg'
			AND timestamp >= $5 AND timestamp <= $6
		ORDER BY timestamp DESC
		LIMIT $7`

	rows, err := qo.pool.Query(ctx, query,
		tenant, agentID, metricName, windowSeconds, start, end, defaultQueryLimit)
	if err != nil {
		return nil, fmt.Errorf("query aggregated metrics: %w", err)
	}
	defer rows.Close()

	var metrics []models.Metric
	for rows.Next() {
		var m models.Metric
		if err := rows.Scan(&m.AgentID, &m.Value, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("scan aggregated metric: %w", err)
		}
		m.Name = metricName
		m.Labels = map[string]string{
			"source": "aggregated_metrics",
			"window": fmt.Sprintf("%ds", windowSeconds),
		}
		m.Type = models.MetricTypeCustom
		metrics = append(metrics, m)
	}

	if metrics == nil {
		return []models.Metric{}, rows.Err()
	}
	return metrics, rows.Err()
}

// ---- Nullable Type Helpers ----

// nullStr extracts the string value from a sql.NullString.
func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// nullInt32 extracts the int32 value from a sql.NullInt32.
func nullInt32(ni sql.NullInt32) int32 {
	if ni.Valid {
		return ni.Int32
	}
	return 0
}

// nullInt64 extracts the int64 value from a sql.NullInt64.
func nullInt64(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

// nullFloat64 extracts the float64 value from a sql.NullFloat64.
func nullFloat64(nf sql.NullFloat64) float64 {
	if nf.Valid {
		return nf.Float64
	}
	return 0
}

// interfaceToFloat64 converts a database column value (scanned as interface{})
// to float64. Handles common database types: float64, int64, int, float32, string.
func interfaceToFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case float32:
		return float64(val)
	case string:
		// Attempt to parse string values (QuestDB may return numeric strings).
		var f float64
		if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}

// metricTypeFromTable derives a MetricType constant from a QuestDB table name.
// Returns "custom" for unrecognized tables.
func metricTypeFromTable(table string) string {
	switch table {
	case "cpu_metrics":
		return string(models.MetricTypeCPU)
	case "memory_metrics":
		return string(models.MetricTypeMemory)
	case "disk_metrics":
		return string(models.MetricTypeDisk)
	case "network_metrics":
		return string(models.MetricTypeNetwork)
	case "process_metrics":
		return string(models.MetricTypeProcess)
	default:
		return string(models.MetricTypeCustom)
	}
}
