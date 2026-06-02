// Package warm implements the warm storage tier using QuestDB (time-series database).
// This tier stores historical data for time-range queries and analysis.
//
// Data ingestion uses ILP (InfluxDB Line Protocol) for high-throughput writes
// with PG INSERT as a fallback. Tables are auto-created via EnsureTables on startup.
package warm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
)

// Config contains configuration for the QuestDB client.
type Config struct {
	Addr     string `yaml:"addr" json:"addr"`
	ILPAddr  string `yaml:"ilp_addr" json:"ilp_addr"`
	Database string `yaml:"database" json:"database"`
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
	MaxConns int    `yaml:"max_conns" json:"max_conns"`
}

// Client is the QuestDB warm storage client.
type Client struct {
	pool     *pgxpool.Pool
	cfg      Config
	ilpSender *ILPSender // nil if ILPAddr is not configured
}

// New creates a new QuestDB client. It initializes the PG connection pool,
// optionally connects the ILP sender, and ensures all required tables exist.
func New(ctx context.Context, cfg Config) (*Client, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s/%s",
		cfg.Username, cfg.Password, cfg.Addr, cfg.Database)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxConns)

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	c := &Client{
		pool: pool,
		cfg:  cfg,
	}

	// Initialize ILP sender if the ILP address is configured.
	if cfg.ILPAddr != "" {
		sender, err := NewILPSender(cfg.ILPAddr)
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("create ILP sender: %w", err)
		}
		c.ilpSender = sender
	}

	// Ensure all tables exist before accepting writes.
	if err := c.EnsureTables(ctx); err != nil {
		c.closeILP()
		pool.Close()
		return nil, fmt.Errorf("ensure tables: %w", err)
	}

	return c, nil
}

// Close closes the QuestDB connection pool and the ILP sender.
func (c *Client) Close() {
	c.closeILP()
	c.pool.Close()
}

// closeILP closes the ILP sender if it exists, logging any error.
func (c *Client) closeILP() {
	if c.ilpSender != nil {
		if err := c.ilpSender.Close(); err != nil {
			slog.Error("close ILP sender", "err", err)
		}
	}
}

// Ping checks the QuestDB connection.
func (c *Client) Ping(ctx context.Context) error {
	return c.pool.Ping(ctx)
}

// ---- Table Auto-Creation ----

// EnsureTables creates all required tables if they do not exist.
// Tables use WAL mode with deduplication on timestamp for idempotent ingestion.
func (c *Client) EnsureTables(ctx context.Context) error {
	for _, ddl := range createTableStatements {
		if _, err := c.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("execute DDL: %w", err)
		}
	}

	slog.Info("QuestDB tables ensured", "count", len(createTableStatements))
	return nil
}

// createTableStatements holds the DDL for all warm-tier tables.
// All tables include tenant_id SYMBOL for tenant isolation and use
// WAL mode with daily partitioning.
var createTableStatements = []string{
	`CREATE TABLE IF NOT EXISTS cpu_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		total_usage_pct DOUBLE,
		per_core_pct STRING,
		load_avg_1 DOUBLE,
		load_avg_5 DOUBLE,
		load_avg_15 DOUBLE,
		frequency_mhz DOUBLE,
		context_switches LONG
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS memory_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		total_bytes LONG,
		used_bytes LONG,
		available_bytes LONG,
		cached_bytes LONG,
		swap_total_bytes LONG,
		swap_used_bytes LONG
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS disk_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		device SYMBOL,
		mount_point SYMBOL,
		total_bytes LONG,
		used_bytes LONG,
		read_bytes_per_sec LONG,
		write_bytes_per_sec LONG,
		iops_read LONG,
		iops_write LONG
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS network_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		"interface" SYMBOL,
		rx_bytes_per_sec LONG,
		tx_bytes_per_sec LONG,
		rx_packets LONG,
		tx_packets LONG,
		errors LONG
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS process_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		pid LONG,
		name SYMBOL,
		cpu_usage_pct DOUBLE,
		memory_bytes LONG,
		threads LONG
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS aggregated_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		name SYMBOL,
		labels STRING,
		window LONG,
		agg_type SYMBOL,
		value DOUBLE
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS spans (
		timestamp TIMESTAMP,
		trace_id SYMBOL,
		span_id SYMBOL,
		parent_span_id SYMBOL,
		name SYMBOL,
		kind SYMBOL,
		service_name SYMBOL,
		tenant_id SYMBOL,
		end_time TIMESTAMP,
		duration LONG,
		status SYMBOL,
		status_code SYMBOL,
		status_message STRING
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,
}

// ---- Metric Operations (PG INSERT — fallback path) ----

// InsertMetric inserts a single metric into QuestDB.
func (c *Client) InsertMetric(ctx context.Context, m *models.Metric) error {
	query := `INSERT INTO metrics (agent_id, name, labels, value, type, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := c.pool.Exec(ctx, query,
		m.AgentID, m.Name, m.Labels, m.Value, m.Type, m.Timestamp)
	if err != nil {
		return fmt.Errorf("insert metric: %w", err)
	}
	return nil
}

// InsertMetricBatch inserts a batch of metrics into QuestDB via PG INSERT.
// This is the fallback path when ILP is unavailable.
func (c *Client) InsertMetricBatch(ctx context.Context, batch *models.MetricBatch, tenant string) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Insert CPU metrics
	for _, cpu := range batch.CPU {
		query := `INSERT INTO cpu_metrics (agent_id, tenant_id, timestamp, total_usage_pct, load_avg_1, load_avg_5, load_avg_15, frequency_mhz, context_switches)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
		_, err := tx.Exec(ctx, query,
			cpu.AgentID, tenant, cpu.Timestamp, cpu.TotalUsagePct, cpu.LoadAvg1m, cpu.LoadAvg5m, cpu.LoadAvg15m, cpu.FrequencyMHz, cpu.ContextSwitches)
		if err != nil {
			return fmt.Errorf("insert cpu: %w", err)
		}
	}

	// Insert memory metrics
	for _, mem := range batch.Memory {
		query := `INSERT INTO memory_metrics (agent_id, tenant_id, timestamp, total_bytes, used_bytes, available_bytes, cached_bytes, swap_total_bytes, swap_used_bytes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
		_, err := tx.Exec(ctx, query,
			mem.AgentID, tenant, mem.Timestamp, mem.TotalBytes, mem.UsedBytes, mem.AvailableBytes, mem.CachedBytes, mem.SwapTotalBytes, mem.SwapUsedBytes)
		if err != nil {
			return fmt.Errorf("insert memory: %w", err)
		}
	}

	// Insert disk metrics
	for _, disk := range batch.Disk {
		query := `INSERT INTO disk_metrics (agent_id, tenant_id, timestamp, device, mount_point, total_bytes, used_bytes, read_bytes_per_sec, write_bytes_per_sec, iops_read, iops_write)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
		_, err := tx.Exec(ctx, query,
			disk.AgentID, tenant, disk.Timestamp, disk.Device, disk.MountPoint, disk.TotalBytes, disk.UsedBytes, disk.ReadBytesPerSec, disk.WriteBytesPerSec, disk.IOPSRead, disk.IOPSWrite)
		if err != nil {
			return fmt.Errorf("insert disk: %w", err)
		}
	}

	// Insert network metrics
	for _, net := range batch.Network {
		query := `INSERT INTO network_metrics (agent_id, tenant_id, timestamp, "interface", rx_bytes_per_sec, tx_bytes_per_sec, rx_packets, tx_packets, errors)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
		_, err := tx.Exec(ctx, query,
			net.AgentID, tenant, net.Timestamp, net.Interface, net.RxBytesPerSec, net.TxBytesPerSec, net.RxPackets, net.TxPackets, net.Errors)
		if err != nil {
			return fmt.Errorf("insert network: %w", err)
		}
	}

	// Insert process metrics
	for _, proc := range batch.Processes {
		query := `INSERT INTO process_metrics (agent_id, tenant_id, timestamp, pid, name, cpu_usage_pct, memory_bytes, threads)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
		_, err := tx.Exec(ctx, query,
			proc.AgentID, tenant, proc.Timestamp, proc.PID, proc.Name, proc.CPUUsagePct, proc.MemoryBytes, proc.Threads)
		if err != nil {
			return fmt.Errorf("insert process: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// InsertMetricBatchILP inserts a batch of metrics via the ILP high-throughput path.
// Falls back to PG INSERT (InsertMetricBatch) if the ILP sender is nil or a write fails.
// The tenant parameter is included as a tag in every ILP line for tenant isolation.
func (c *Client) InsertMetricBatchILP(batch *models.MetricBatch, tenant string) error {
	if c.ilpSender == nil {
		// No ILP sender configured — fall back to PG INSERT.
		return c.InsertMetricBatch(context.Background(), batch, tenant)
	}

	// CPU metrics.
	for i := range batch.CPU {
		cpu := &batch.CPU[i]
		tags := map[string]string{
			"agent_id":  cpu.AgentID,
			"tenant_id": tenant,
		}
		fields := map[string]any{
			"total_usage_pct":  cpu.TotalUsagePct,
			"load_avg_1":       cpu.LoadAvg1m,
			"load_avg_5":       cpu.LoadAvg5m,
			"load_avg_15":      cpu.LoadAvg15m,
			"frequency_mhz":    cpu.FrequencyMHz,
			"context_switches": cpu.ContextSwitches,
		}
		if len(cpu.PerCorePct) > 0 {
			fields["per_core_pct"] = formatFloatSlice(cpu.PerCorePct)
		}
		if err := c.ilpSender.SendMetric("cpu_metrics", tags, fields, cpu.Timestamp); err != nil {
			slog.Warn("ILP send cpu failed, falling back to PG", "err", err, "agent_id", cpu.AgentID)
			return c.InsertMetricBatch(context.Background(), batch, tenant)
		}
	}

	// Memory metrics.
	for i := range batch.Memory {
		mem := &batch.Memory[i]
		tags := map[string]string{
			"agent_id":  mem.AgentID,
			"tenant_id": tenant,
		}
		fields := map[string]any{
			"total_bytes":      mem.TotalBytes,
			"used_bytes":       mem.UsedBytes,
			"available_bytes":  mem.AvailableBytes,
			"cached_bytes":     mem.CachedBytes,
			"swap_total_bytes": mem.SwapTotalBytes,
			"swap_used_bytes":  mem.SwapUsedBytes,
		}
		if err := c.ilpSender.SendMetric("memory_metrics", tags, fields, mem.Timestamp); err != nil {
			slog.Warn("ILP send memory failed, falling back to PG", "err", err, "agent_id", mem.AgentID)
			return c.InsertMetricBatch(context.Background(), batch, tenant)
		}
	}

	// Disk metrics.
	for i := range batch.Disk {
		disk := &batch.Disk[i]
		tags := map[string]string{
			"agent_id":   disk.AgentID,
			"tenant_id":  tenant,
			"device":     disk.Device,
			"mount_point": disk.MountPoint,
		}
		fields := map[string]any{
			"total_bytes":        disk.TotalBytes,
			"used_bytes":         disk.UsedBytes,
			"read_bytes_per_sec": disk.ReadBytesPerSec,
			"write_bytes_per_sec": disk.WriteBytesPerSec,
			"iops_read":          disk.IOPSRead,
			"iops_write":         disk.IOPSWrite,
		}
		if err := c.ilpSender.SendMetric("disk_metrics", tags, fields, disk.Timestamp); err != nil {
			slog.Warn("ILP send disk failed, falling back to PG", "err", err, "agent_id", disk.AgentID)
			return c.InsertMetricBatch(context.Background(), batch, tenant)
		}
	}

	// Network metrics.
	for i := range batch.Network {
		net := &batch.Network[i]
		tags := map[string]string{
			"agent_id":   net.AgentID,
			"tenant_id":  tenant,
			"interface":  net.Interface,
		}
		fields := map[string]any{
			"rx_bytes_per_sec": net.RxBytesPerSec,
			"tx_bytes_per_sec": net.TxBytesPerSec,
			"rx_packets":       net.RxPackets,
			"tx_packets":       net.TxPackets,
			"errors":           net.Errors,
		}
		if err := c.ilpSender.SendMetric("network_metrics", tags, fields, net.Timestamp); err != nil {
			slog.Warn("ILP send network failed, falling back to PG", "err", err, "agent_id", net.AgentID)
			return c.InsertMetricBatch(context.Background(), batch, tenant)
		}
	}

	// Process metrics.
	for i := range batch.Processes {
		proc := &batch.Processes[i]
		tags := map[string]string{
			"agent_id":  proc.AgentID,
			"tenant_id": tenant,
			"name":      proc.Name,
		}
		fields := map[string]any{
			"pid":            proc.PID,
			"cpu_usage_pct":  proc.CPUUsagePct,
			"memory_bytes":   proc.MemoryBytes,
			"threads":        proc.Threads,
		}
		if err := c.ilpSender.SendMetric("process_metrics", tags, fields, proc.Timestamp); err != nil {
			slog.Warn("ILP send process failed, falling back to PG", "err", err, "agent_id", proc.AgentID)
			return c.InsertMetricBatch(context.Background(), batch, tenant)
		}
	}

	// Flush all buffered ILP data.
	if err := c.ilpSender.Flush(); err != nil {
		slog.Warn("ILP flush failed, falling back to PG", "err", err)
		return c.InsertMetricBatch(context.Background(), batch, tenant)
	}

	return nil
}

// formatFloatSlice converts a float slice to a bracket-delimited string
// for storage as a string field in ILP (e.g., "[0.5,0.3,0.8]").
func formatFloatSlice(vals []float64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(',')
		}
		// 4 decimal places is sufficient for per-core percentages.
		_, _ = fmt.Fprintf(&b, "%.4f", v)
	}
	b.WriteByte(']')
	return b.String()
}

// ---- Query Operations ----

// QueryMetrics queries metrics within a time range.
func (c *Client) QueryMetrics(ctx context.Context, agentID string, metricName string, start, end time.Time) ([]models.Metric, error) {
	query := `SELECT agent_id, name, labels, value, type, timestamp
		FROM metrics
		WHERE agent_id = $1 AND name = $2 AND timestamp >= $3 AND timestamp <= $4
		ORDER BY timestamp DESC`

	rows, err := c.pool.Query(ctx, query, agentID, metricName, start, end)
	if err != nil {
		return nil, fmt.Errorf("query metrics: %w", err)
	}
	defer rows.Close()

	var metrics []models.Metric
	for rows.Next() {
		var m models.Metric
		if err := rows.Scan(&m.AgentID, &m.Name, &m.Labels, &m.Value, &m.Type, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("scan metric: %w", err)
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

// QueryAggregatedMetrics queries aggregated metrics.
func (c *Client) QueryAggregatedMetrics(ctx context.Context, agentID string, metricName string, window time.Duration, start, end time.Time) ([]models.AggregatedMetric, error) {
	query := `SELECT agent_id, name, labels, window, agg_type, value, timestamp
		FROM aggregated_metrics
		WHERE agent_id = $1 AND name = $2 AND window = $3 AND timestamp >= $4 AND timestamp <= $5
		ORDER BY timestamp DESC`

	rows, err := c.pool.Query(ctx, query, agentID, metricName, window, start, end)
	if err != nil {
		return nil, fmt.Errorf("query aggregated: %w", err)
	}
	defer rows.Close()

	var metrics []models.AggregatedMetric
	for rows.Next() {
		var m models.AggregatedMetric
		if err := rows.Scan(&m.AgentID, &m.Name, &m.Labels, &m.Window, &m.AggType, &m.Value, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("scan aggregated: %w", err)
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

// ---- Trace Operations ----

// InsertSpan inserts a trace span into QuestDB.
func (c *Client) InsertSpan(ctx context.Context, span *models.Span) error {
	query := `INSERT INTO spans (trace_id, span_id, parent_span_id, name, kind, service_name, start_time, end_time, duration, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := c.pool.Exec(ctx, query,
		span.TraceID, span.SpanID, span.ParentSpanID, span.Name, span.Kind,
		span.ServiceName, span.StartTime, span.EndTime, span.Duration, span.Status)
	if err != nil {
		return fmt.Errorf("insert span: %w", err)
	}
	return nil
}

// QueryTraces queries traces within a time range.
func (c *Client) QueryTraces(ctx context.Context, service string, start, end time.Time, limit int) ([]models.Trace, error) {
	query := `SELECT DISTINCT trace_id
		FROM spans
		WHERE service_name = $1 AND start_time >= $2 AND start_time <= $3
		ORDER BY start_time DESC
		LIMIT $4`

	rows, err := c.pool.Query(ctx, query, service, start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("query traces: %w", err)
	}
	defer rows.Close()

	var traceIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan trace id: %w", err)
		}
		traceIDs = append(traceIDs, id)
	}

	// Fetch spans for each trace.
	var traces []models.Trace
	for _, traceID := range traceIDs {
		trace, err := c.getTrace(ctx, traceID)
		if err != nil {
			continue
		}
		traces = append(traces, *trace)
	}
	return traces, nil
}

func (c *Client) getTrace(ctx context.Context, traceID string) (*models.Trace, error) {
	query := `SELECT trace_id, span_id, parent_span_id, name, kind, service_name, start_time, end_time, duration, status
		FROM spans
		WHERE trace_id = $1
		ORDER BY start_time`

	rows, err := c.pool.Query(ctx, query, traceID)
	if err != nil {
		return nil, fmt.Errorf("query spans: %w", err)
	}
	defer rows.Close()

	var spans []models.Span
	for rows.Next() {
		var s models.Span
		if err := rows.Scan(&s.TraceID, &s.SpanID, &s.ParentSpanID, &s.Name, &s.Kind,
			&s.ServiceName, &s.StartTime, &s.EndTime, &s.Duration, &s.Status); err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		spans = append(spans, s)
	}

	if len(spans) == 0 {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}

	return &models.Trace{
		TraceID: traceID,
		Spans:   spans,
	}, nil
}
