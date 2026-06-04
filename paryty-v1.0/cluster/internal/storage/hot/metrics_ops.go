// Package hot — sorted-set time-series operations for Dragonfly.
//
// MetricsOps provides ZADD / ZRANGEBYSCORE / ZREMRANGEBYSCORE helpers that
// back the pipeline's hot-tier metric storage.  Every key is tenant-scoped
// (paryty:{tenant}:metrics:ts:{agent_id}:{metric_name}) so a shared
// Dragonfly instance can safely serve multiple tenants.
package hot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// metricTSScanCount is the COUNT hint for SCAN iterations over metric
	// time-series keys.
	metricTSScanCount int64 = 100
)

// ---- Key generation ----

// metricTimeSeriesKey returns the sorted-set key for a single metric
// time series.
//
//	paryty:{tenant}:metrics:ts:{agent_id}:{metric_name}
func metricTimeSeriesKey(tenant, agentID, metricName string) string {
	return fmt.Sprintf("paryty:%s:metrics:ts:%s:%s", tenant, agentID, metricName)
}

// metricTimeSeriesPattern returns the SCAN glob that matches every metric
// time-series key for a given tenant.
//
//	paryty:{tenant}:metrics:ts:*
func metricTimeSeriesPattern(tenant string) string {
	return fmt.Sprintf("paryty:%s:metrics:ts:*", tenant)
}

// ---- Domain types ----

// MetricValue is the JSON-encoded member stored in each sorted-set entry.
// The sorted-set score is the Unix timestamp (float64, supports sub-second).
type MetricValue struct {
	Value     float64   `json:"v"`
	Timestamp time.Time `json:"t"`
}

// MetricsOps provides sorted-set time-series operations on Dragonfly.
type MetricsOps struct {
	client *Client
	logger *zap.Logger
}

// NewMetricsOps creates a new MetricsOps instance.
func NewMetricsOps(client *Client, logger *zap.Logger) *MetricsOps {
	return &MetricsOps{
		client: client,
		logger: logger,
	}
}

// ---- Core CRUD ----

// StoreMetricTimeSeries appends a single metric data point to the sorted set
// identified by (tenant, agentID, metricName).
//
// The sorted-set score is the Unix timestamp in seconds (float64 to preserve
// sub-second precision).  The member is a JSON-encoded MetricValue.
//
// After the ZADD the key's TTL is refreshed to the provided ttl (or the
// client default when ttl is zero).
func (m *MetricsOps) StoreMetricTimeSeries(
	ctx context.Context,
	tenant, agentID, metricName string,
	value float64,
	timestamp time.Time,
	ttl time.Duration,
) error {
	if tenant == "" {
		return fmt.Errorf("tenant must not be empty")
	}
	if agentID == "" {
		return fmt.Errorf("agentID must not be empty")
	}
	if metricName == "" {
		return fmt.Errorf("metricName must not be empty")
	}

	mv := MetricValue{
		Value:     value,
		Timestamp: timestamp,
	}

	member, err := json.Marshal(mv)
	if err != nil {
		return fmt.Errorf("marshal metric value: %w", err)
	}

	key := metricTimeSeriesKey(tenant, agentID, metricName)
	score := float64(timestamp.UnixMicro()) / 1e6 // sub-second precision

	z := redis.Z{
		Score:  score,
		Member: member,
	}

	if err := m.client.rdb.ZAdd(ctx, key, z).Err(); err != nil {
		return fmt.Errorf("zadd %s: %w", key, err)
	}

	// Refresh TTL.
	expire := ttl
	if expire == 0 {
		expire = m.client.cfg.TTL
	}
	if err := m.client.rdb.Expire(ctx, key, expire).Err(); err != nil {
		return fmt.Errorf("expire %s: %w", key, err)
	}

	m.logger.Debug("stored metric time-series point",
		zap.String("tenant", tenant),
		zap.String("agent_id", agentID),
		zap.String("metric_name", metricName),
		zap.Float64("value", value),
		zap.Time("timestamp", timestamp),
	)

	return nil
}

// QueryMetricRange returns metric data points whose timestamp falls within
// [start, end] for the given (tenant, agentID, metricName) sorted set.
//
// Results are ordered oldest-first (ZRANGEBYSCORE default ordering).
// An empty (nil) slice is returned when no data points match.
func (m *MetricsOps) QueryMetricRange(
	ctx context.Context,
	tenant, agentID, metricName string,
	start, end time.Time,
) ([]MetricValue, error) {
	if tenant == "" {
		return nil, fmt.Errorf("tenant must not be empty")
	}
	if agentID == "" {
		return nil, fmt.Errorf("agentID must not be empty")
	}
	if metricName == "" {
		return nil, fmt.Errorf("metricName must not be empty")
	}

	key := metricTimeSeriesKey(tenant, agentID, metricName)

	minScore := fmt.Sprintf("%f", float64(start.UnixMicro())/1e6)
	maxScore := fmt.Sprintf("%f", float64(end.UnixMicro())/1e6)

	opt := &redis.ZRangeBy{
		Min: minScore,
		Max: maxScore,
	}

	results, err := m.client.rdb.ZRangeByScore(ctx, key, opt).Result()
	if err != nil {
		return nil, fmt.Errorf("zrangebyscore %s: %w", key, err)
	}

	values := make([]MetricValue, 0, len(results))
	for _, raw := range results {
		var mv MetricValue
		if err := json.Unmarshal([]byte(raw), &mv); err != nil {
			m.logger.Warn("skipping corrupt metric value",
				zap.String("key", key),
				zap.String("raw", raw),
				zap.Error(err),
			)
			continue
		}
		values = append(values, mv)
	}

	return values, nil
}

// ---- Purge (TTL-based eviction at the application level) ----

// PurgeExpiredMetrics removes all entries older than maxAge from a single
// metric time-series sorted set.
//
// It uses ZREMRANGEBYSCORE to delete entries with scores < (now - maxAge).
// Returns the number of members removed.
func (m *MetricsOps) PurgeExpiredMetrics(
	ctx context.Context,
	tenant, agentID, metricName string,
	maxAge time.Duration,
) (int64, error) {
	if tenant == "" {
		return 0, fmt.Errorf("tenant must not be empty")
	}
	if agentID == "" {
		return 0, fmt.Errorf("agentID must not be empty")
	}
	if metricName == "" {
		return 0, fmt.Errorf("metricName must not be empty")
	}

	key := metricTimeSeriesKey(tenant, agentID, metricName)
	cutoff := float64(time.Now().Add(-maxAge).UnixMicro()) / 1e6
	minScore := "-inf"
	maxScore := fmt.Sprintf("%f", cutoff)

	removed, err := m.client.rdb.ZRemRangeByScore(ctx, key, minScore, maxScore).Result()
	if err != nil {
		return 0, fmt.Errorf("zremrangebyscore %s: %w", key, err)
	}

	if removed > 0 {
		m.logger.Debug("purged expired metric points",
			zap.String("tenant", tenant),
			zap.String("agent_id", agentID),
			zap.String("metric_name", metricName),
			zap.Int64("removed", removed),
			zap.Duration("max_age", maxAge),
		)
	}

	return removed, nil
}

// PurgeAllExpiredMetrics scans all metric time-series keys for a tenant and
// purges entries older than maxAge from each one.
//
// Uses SCAN (not KEYS) to avoid blocking Dragonfly.
// Returns the total number of members removed across all keys.
func (m *MetricsOps) PurgeAllExpiredMetrics(
	ctx context.Context,
	tenant string,
	maxAge time.Duration,
) (int64, error) {
	if tenant == "" {
		return 0, fmt.Errorf("tenant must not be empty")
	}

	pattern := metricTimeSeriesPattern(tenant)
	var totalRemoved int64
	var cursor uint64

	for {
		keys, nextCursor, err := m.client.rdb.Scan(ctx, cursor, pattern, metricTSScanCount).Result()
		if err != nil {
			return totalRemoved, fmt.Errorf("scan %s: %w", pattern, err)
		}

		for _, key := range keys {
			cutoff := float64(time.Now().Add(-maxAge).UnixMicro()) / 1e6
			minScore := "-inf"
			maxScore := fmt.Sprintf("%f", cutoff)

			removed, err := m.client.rdb.ZRemRangeByScore(ctx, key, minScore, maxScore).Result()
			if err != nil {
				m.logger.Warn("failed to purge key",
					zap.String("key", key),
					zap.Error(err),
				)
				continue
			}
			totalRemoved += removed
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if totalRemoved > 0 {
		m.logger.Debug("purged all expired metric points for tenant",
			zap.String("tenant", tenant),
			zap.Int64("total_removed", totalRemoved),
			zap.Duration("max_age", maxAge),
		)
	}

	return totalRemoved, nil
}

// ---- Batch convenience ----

// StoreMetricBatchTimeSeries stores every field of a MetricBatch as a separate
// sorted-set entry.  Each metric dimension (cpu.usage, memory.used_bytes, etc.)
// becomes its own time-series sorted set keyed by
// paryty:{tenant}:metrics:ts:{agent_id}:{metric_name}.
//
// The function uses a Redis pipeline to minimise round-trips.
func (m *MetricsOps) StoreMetricBatchTimeSeries(
	ctx context.Context,
	tenant string,
	batch *models.MetricBatch,
	ttl time.Duration,
) error {
	if tenant == "" {
		return fmt.Errorf("tenant must not be empty")
	}
	if batch == nil {
		return fmt.Errorf("batch must not be nil")
	}

	agentID := batch.AgentID
	if agentID == "" {
		return fmt.Errorf("batch.AgentID must not be empty")
	}

	// Resolve effective TTL once.
	expire := ttl
	if expire == 0 {
		expire = m.client.cfg.TTL
	}

	pipe := m.client.rdb.Pipeline()
	keySet := make(map[string]struct{}) // track keys for EXPIRE batching

	// ---- CPU metrics ----
	for _, cpu := range batch.CPU {
		ts := cpu.Timestamp
		if ts.IsZero() {
			ts = batch.Timestamp
		}
		addZAdd(ctx, pipe, tenant, agentID, "cpu.usage_percent", cpu.TotalUsagePct, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "cpu.load_avg_1m", cpu.LoadAvg1m, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "cpu.load_avg_5m", cpu.LoadAvg5m, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "cpu.load_avg_15m", cpu.LoadAvg15m, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "cpu.frequency_mhz", cpu.FrequencyMHz, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "cpu.context_switches", float64(cpu.ContextSwitches), ts, keySet)
	}

	// ---- Memory metrics ----
	for _, mem := range batch.Memory {
		ts := mem.Timestamp
		if ts.IsZero() {
			ts = batch.Timestamp
		}
		addZAdd(ctx, pipe, tenant, agentID, "memory.usage_percent", mem.UsagePercent, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "memory.total_bytes", float64(mem.TotalBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "memory.used_bytes", float64(mem.UsedBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "memory.free_bytes", float64(mem.FreeBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "memory.available_bytes", float64(mem.AvailableBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "memory.cached_bytes", float64(mem.CachedBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "memory.swap_total_bytes", float64(mem.SwapTotalBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, "memory.swap_used_bytes", float64(mem.SwapUsedBytes), ts, keySet)
	}

	// ---- Disk metrics ----
	for _, disk := range batch.Disk {
		ts := disk.Timestamp
		if ts.IsZero() {
			ts = batch.Timestamp
		}
		// Prefix with device name to avoid collisions between disks.
		devicePrefix := "disk." + disk.Device + "."
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"total_bytes", float64(disk.TotalBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"used_bytes", float64(disk.UsedBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"free_bytes", float64(disk.FreeBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"read_bytes_per_sec", float64(disk.ReadBytesPerSec), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"write_bytes_per_sec", float64(disk.WriteBytesPerSec), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"iops_read", float64(disk.IOPSRead), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"iops_write", float64(disk.IOPSWrite), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"io_latency_ms", disk.IOLatencyMs, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"queue_depth", disk.QueueDepth, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, devicePrefix+"utilization_pct", disk.UtilizationPct, ts, keySet)
	}

	// ---- Network metrics ----
	for _, net := range batch.Network {
		ts := net.Timestamp
		if ts.IsZero() {
			ts = batch.Timestamp
		}
		// Prefix with interface name to avoid collisions between NICs.
		ifacePrefix := "network." + net.Interface + "."
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"rx_bytes_per_sec", float64(net.RxBytesPerSec), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"tx_bytes_per_sec", float64(net.TxBytesPerSec), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"rx_packets", float64(net.RxPackets), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"tx_packets", float64(net.TxPackets), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"errors", float64(net.Errors), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"estimated_rtt_ms", net.EstimatedRTTMs, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"total_rx_bytes", float64(net.TotalRxBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"total_tx_bytes", float64(net.TotalTxBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, ifacePrefix+"speed_mbps", float64(net.SpeedMbps), ts, keySet)
	}

	// ---- Process metrics ----
	for _, proc := range batch.Processes {
		ts := proc.Timestamp
		if ts.IsZero() {
			ts = batch.Timestamp
		}
		procPrefix := fmt.Sprintf("process.%d.%s.", proc.PID, proc.Name)
		addZAdd(ctx, pipe, tenant, agentID, procPrefix+"cpu_usage_percent", proc.CPUUsagePct, ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, procPrefix+"memory_bytes", float64(proc.MemoryBytes), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, procPrefix+"threads", float64(proc.Threads), ts, keySet)
		addZAdd(ctx, pipe, tenant, agentID, procPrefix+"fd_count", float64(proc.FdCount), ts, keySet)
	}

	// ---- Execute ZADD commands ----
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("pipeline exec zadd: %w", err)
	}

	// ---- Batch EXPIRE for all touched keys ----
	expirePipe := m.client.rdb.Pipeline()
	for key := range keySet {
		expirePipe.Expire(ctx, key, expire)
	}
	if _, err := expirePipe.Exec(ctx); err != nil {
		return fmt.Errorf("pipeline exec expire: %w", err)
	}

	m.logger.Debug("stored metric batch time-series",
		zap.String("tenant", tenant),
		zap.String("agent_id", agentID),
		zap.Int("cpu_entries", len(batch.CPU)),
		zap.Int("memory_entries", len(batch.Memory)),
		zap.Int("disk_entries", len(batch.Disk)),
		zap.Int("network_entries", len(batch.Network)),
		zap.Int("process_entries", len(batch.Processes)),
		zap.Int("unique_keys", len(keySet)),
	)

	return nil
}

// ---- Internal helpers ----

// addZAdd enqueues a ZADD command on the pipeline for a single metric data
// point.  It also records the key in keySet so that a single EXPIRE can be
// issued per unique key after the pipeline executes.
func addZAdd(
	ctx context.Context,
	pipe redis.Pipeliner,
	tenant, agentID, metricName string,
	value float64,
	timestamp time.Time,
	keySet map[string]struct{},
) {
	mv := MetricValue{
		Value:     value,
		Timestamp: timestamp,
	}
	member, err := json.Marshal(mv)
	if err != nil {
		// Marshalling a float64 and a time.Time should never fail; log and
		// skip rather than crashing the batch.
		slog.Error("marshal metric value in batch",
			"metric_name", metricName,
			"error", err,
		)
		return
	}

	key := metricTimeSeriesKey(tenant, agentID, metricName)
	score := float64(timestamp.UnixMicro()) / 1e6

	pipe.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: member,
	})

	keySet[key] = struct{}{}
}
