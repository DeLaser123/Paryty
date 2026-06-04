// Package warm implements the warm storage tier using QuestDB (time-series database).
// This tier stores historical data for time-range queries and analysis.
//
// Data ingestion uses ILP (InfluxDB Line Protocol) for high-throughput writes
// with PG INSERT as a fallback. Tables are auto-created via EnsureTables on startup.
package warm

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
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
	pool      *pgxpool.Pool
	cfg       Config
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

// Pool returns the underlying pgxpool.Pool for direct queries.
func (c *Client) Pool() *pgxpool.Pool {
	return c.pool
}

// ---- Table Auto-Creation ----

// EnsureTables creates all required tables if they do not exist.
// Tables use WAL mode with deduplication on timestamp for idempotent ingestion.
// After creation, runs column migrations for tables that predate new fields.
func (c *Client) EnsureTables(ctx context.Context) error {
	for _, ddl := range createTableStatements {
		if _, err := c.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("execute DDL: %w", err)
		}
	}

	// Migrate existing tables: add columns that were added after initial creation.
	// Errors (e.g., column already exists) are silently ignored.
	for _, alter := range migrateColumnStatements {
		_, _ = c.pool.Exec(ctx, alter)
	}

	slog.Info("QuestDB tables ensured", "count", len(createTableStatements))
	return nil
}

// migrateColumnStatements adds columns that were added after initial table creation.
// Each ALTER TABLE is idempotent — if the column already exists, the error is ignored.
var migrateColumnStatements = []string{
	// memory_metrics
	`ALTER TABLE memory_metrics ADD COLUMN free_bytes LONG`,
	`ALTER TABLE memory_metrics ADD COLUMN buffer_bytes LONG`,
	`ALTER TABLE memory_metrics ADD COLUMN usage_percent DOUBLE`,
	// disk_metrics
	`ALTER TABLE disk_metrics ADD COLUMN filesystem_type SYMBOL`,
	`ALTER TABLE disk_metrics ADD COLUMN free_bytes LONG`,
	`ALTER TABLE disk_metrics ADD COLUMN io_latency_ms DOUBLE`,
	`ALTER TABLE disk_metrics ADD COLUMN queue_depth DOUBLE`,
	// network_metrics
	`ALTER TABLE network_metrics ADD COLUMN rx_dropped LONG`,
	`ALTER TABLE network_metrics ADD COLUMN tx_dropped LONG`,
	`ALTER TABLE network_metrics ADD COLUMN estimated_rtt_ms DOUBLE`,
	`ALTER TABLE network_metrics ADD COLUMN total_rx_packets LONG`,
	`ALTER TABLE network_metrics ADD COLUMN total_tx_packets LONG`,
	// process_metrics
	`ALTER TABLE process_metrics ADD COLUMN parent_pid LONG`,
	`ALTER TABLE process_metrics ADD COLUMN command_line STRING`,
	`ALTER TABLE process_metrics ADD COLUMN vsz_bytes LONG`,
	`ALTER TABLE process_metrics ADD COLUMN status SYMBOL`,
	`ALTER TABLE process_metrics ADD COLUMN fd_count LONG`,
	`ALTER TABLE process_metrics ADD COLUMN container_id SYMBOL`,
	`ALTER TABLE process_metrics ADD COLUMN started_at TIMESTAMP`,
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
		context_switches LONG,
		physical_cores INT,
		logical_cores INT,
		model_name STRING,
		vendor_id STRING
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS memory_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		total_bytes LONG,
		used_bytes LONG,
		free_bytes LONG,
		available_bytes LONG,
		cached_bytes LONG,
		buffer_bytes LONG,
		swap_total_bytes LONG,
		swap_used_bytes LONG,
		usage_percent DOUBLE,
		pressure_some_avg10 DOUBLE,
		pressure_some_avg60 DOUBLE,
		pressure_some_avg300 DOUBLE,
		pressure_full_avg10 DOUBLE,
		pressure_full_avg60 DOUBLE,
		pressure_full_avg300 DOUBLE
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS disk_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		device SYMBOL,
		mount_point SYMBOL,
		filesystem_type SYMBOL,
		total_bytes LONG,
		used_bytes LONG,
		free_bytes LONG,
		read_bytes_per_sec LONG,
		write_bytes_per_sec LONG,
		iops_read LONG,
		iops_write LONG,
		io_latency_ms DOUBLE,
		queue_depth DOUBLE,
		is_ssd BOOLEAN,
		utilization_pct DOUBLE
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
		rx_dropped LONG,
		tx_dropped LONG,
		errors LONG,
		estimated_rtt_ms DOUBLE,
		total_rx_bytes LONG,
		total_tx_bytes LONG,
		total_rx_packets LONG,
		total_tx_packets LONG,
		speed_mbps LONG,
		is_up BOOLEAN,
		tcp_established INT,
		tcp_time_wait INT,
		tcp_listen INT,
		tcp_retransmit_count LONG
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS process_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		pid LONG,
		parent_pid LONG,
		name SYMBOL,
		command_line STRING,
		cpu_usage_pct DOUBLE,
		memory_bytes LONG,
		vsz_bytes LONG,
		status SYMBOL,
		threads LONG,
		fd_count LONG,
		container_id SYMBOL,
		exe STRING,
		disk_read_bytes LONG,
		disk_written_bytes LONG,
		user_id STRING
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS container_metrics (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		container_id SYMBOL,
		runtime SYMBOL,
		name SYMBOL,
		image SYMBOL,
		status SYMBOL,
		cgroup_version SYMBOL,
		memory_limit_bytes LONG,
		cpu_quota DOUBLE,
		cpu_shares LONG
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

	// ---- Network Event Tables (eBPF) ----

	`CREATE TABLE IF NOT EXISTS tcp_events (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		event_type SYMBOL,
		source_ip STRING,
		source_port INT,
		destination_ip STRING,
		destination_port INT,
		state SYMBOL,
		bytes_sent LONG,
		bytes_received LONG,
		pid INT,
		process_name STRING
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS dns_events (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		query_name STRING,
		query_type SYMBOL,
		latency_ms DOUBLE,
		pid INT
	) TIMESTAMP(timestamp) PARTITION BY DAY WAL`,

	`CREATE TABLE IF NOT EXISTS http_events (
		timestamp TIMESTAMP,
		agent_id SYMBOL,
		tenant_id SYMBOL,
		method SYMBOL,
		path STRING,
		status_code INT,
		latency_ms DOUBLE,
		source_ip STRING,
		destination_ip STRING,
		destination_port INT,
		host STRING,
		pid INT
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
		query := `INSERT INTO cpu_metrics (agent_id, tenant_id, timestamp, total_usage_pct, load_avg_1, load_avg_5, load_avg_15, frequency_mhz, context_switches, physical_cores, logical_cores, model_name, vendor_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
		_, err := tx.Exec(ctx, query,
			cpu.AgentID, tenant, cpu.Timestamp, cpu.TotalUsagePct, cpu.LoadAvg1m, cpu.LoadAvg5m, cpu.LoadAvg15m, cpu.FrequencyMHz, cpu.ContextSwitches, cpu.PhysicalCores, cpu.LogicalCores, cpu.ModelName, cpu.VendorID)
		if err != nil {
			return fmt.Errorf("insert cpu: %w", err)
		}
	}

	// Insert memory metrics
	for _, mem := range batch.Memory {
		var pressureSomeAvg10, pressureSomeAvg60, pressureSomeAvg300 float64
		var pressureFullAvg10, pressureFullAvg60, pressureFullAvg300 float64
		if mem.Pressure != nil {
			pressureSomeAvg10 = mem.Pressure.Some10
			pressureSomeAvg60 = mem.Pressure.Some60
			pressureSomeAvg300 = mem.Pressure.Some300
			pressureFullAvg10 = mem.Pressure.Full10
			pressureFullAvg60 = mem.Pressure.Full60
			pressureFullAvg300 = mem.Pressure.Full300
		}
		query := `INSERT INTO memory_metrics (agent_id, tenant_id, timestamp, total_bytes, used_bytes, free_bytes, available_bytes, cached_bytes, buffer_bytes, swap_total_bytes, swap_used_bytes, usage_percent, pressure_some_avg10, pressure_some_avg60, pressure_some_avg300, pressure_full_avg10, pressure_full_avg60, pressure_full_avg300)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`
		_, err := tx.Exec(ctx, query,
			mem.AgentID, tenant, mem.Timestamp, mem.TotalBytes, mem.UsedBytes, mem.FreeBytes, mem.AvailableBytes, mem.CachedBytes, mem.BufferBytes, mem.SwapTotalBytes, mem.SwapUsedBytes, mem.UsagePercent, pressureSomeAvg10, pressureSomeAvg60, pressureSomeAvg300, pressureFullAvg10, pressureFullAvg60, pressureFullAvg300)
		if err != nil {
			return fmt.Errorf("insert memory: %w", err)
		}
	}

	// Insert disk metrics
	for _, disk := range batch.Disk {
		query := `INSERT INTO disk_metrics (agent_id, tenant_id, timestamp, device, mount_point, filesystem_type, total_bytes, used_bytes, free_bytes, read_bytes_per_sec, write_bytes_per_sec, iops_read, iops_write, io_latency_ms, queue_depth, is_ssd, utilization_pct)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`
		_, err := tx.Exec(ctx, query,
			disk.AgentID, tenant, disk.Timestamp, disk.Device, disk.MountPoint, disk.FilesystemType, disk.TotalBytes, disk.UsedBytes, disk.FreeBytes, disk.ReadBytesPerSec, disk.WriteBytesPerSec, disk.IOPSRead, disk.IOPSWrite, disk.IOLatencyMs, disk.QueueDepth, disk.IsSSD, disk.UtilizationPct)
		if err != nil {
			return fmt.Errorf("insert disk: %w", err)
		}
	}

	// Insert network metrics
	for _, net := range batch.Network {
		var tcpEstablished, tcpTimeWait, tcpListen int32
		var tcpRetransmitCount int64
		if net.TCPStats != nil {
			tcpEstablished = net.TCPStats.Established
			tcpTimeWait = net.TCPStats.TimeWait
			tcpListen = net.TCPStats.Listen
			tcpRetransmitCount = net.TCPStats.RetransmitCount
		}
		query := `INSERT INTO network_metrics (agent_id, tenant_id, timestamp, "interface", rx_bytes_per_sec, tx_bytes_per_sec, rx_packets, tx_packets, rx_dropped, tx_dropped, errors, estimated_rtt_ms, total_rx_bytes, total_tx_bytes, total_rx_packets, total_tx_packets, speed_mbps, is_up, tcp_established, tcp_time_wait, tcp_listen, tcp_retransmit_count)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`
		_, err := tx.Exec(ctx, query,
			net.AgentID, tenant, net.Timestamp, net.Interface, net.RxBytesPerSec, net.TxBytesPerSec, net.RxPackets, net.TxPackets, net.RxDropped, net.TxDropped, net.Errors, net.EstimatedRTTMs, net.TotalRxBytes, net.TotalTxBytes, net.TotalRxPackets, net.TotalTxPackets, net.SpeedMbps, net.IsUp, tcpEstablished, tcpTimeWait, tcpListen, tcpRetransmitCount)
		if err != nil {
			return fmt.Errorf("insert network: %w", err)
		}
	}

	// Insert process metrics
	for _, proc := range batch.Processes {
		query := `INSERT INTO process_metrics (agent_id, tenant_id, timestamp, pid, parent_pid, name, command_line, cpu_usage_pct, memory_bytes, vsz_bytes, status, threads, fd_count, container_id, exe, disk_read_bytes, disk_written_bytes, user_id, started_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`
		_, err := tx.Exec(ctx, query,
			proc.AgentID, tenant, proc.Timestamp, proc.PID, proc.ParentPID, proc.Name, proc.CommandLine, proc.CPUUsagePct, proc.MemoryBytes, proc.VszBytes, proc.Status, proc.Threads, proc.FdCount, proc.ContainerID, proc.Exe, proc.DiskReadBytes, proc.DiskWrittenBytes, proc.UserID, proc.StartedAt)
		if err != nil {
			return fmt.Errorf("insert process: %w", err)
		}
	}

	// Insert container metrics
	for _, ctr := range batch.Containers {
		query := `INSERT INTO container_metrics (agent_id, tenant_id, timestamp, container_id, runtime, name, image, status, cgroup_version, memory_limit_bytes, cpu_quota, cpu_shares)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
		_, err := tx.Exec(ctx, query,
			ctr.AgentID, tenant, ctr.Timestamp, ctr.ContainerID, ctr.Runtime, ctr.Name, ctr.Image, ctr.Status, ctr.CgroupVersion, ctr.MemoryLimitBytes, ctr.CPUQuota, ctr.CPUShares)
		if err != nil {
			return fmt.Errorf("insert container: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// InsertMetricBatchILP inserts a batch of metrics into QuestDB.
// BUGFIX: ILP (InfluxDB Line Protocol) over TCP has persistent connection issues
// on Windows ("wsasend: connection aborted") that cause data loss for large batches.
// Using PG INSERT as the primary path for reliability. ILP can be re-enabled once
// the Windows TCP/ILP handler issue is resolved.
func (c *Client) InsertMetricBatchILP(batch *models.MetricBatch, tenant string) error {
	return c.InsertMetricBatch(context.Background(), batch, tenant)
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

// ---- Network Event Operations (PG INSERT) ----

// tcpEventTypeToString converts a proto TcpEventType enum to a human-readable string.
func tcpEventTypeToString(t pb.TcpEventType) string {
	switch t {
	case pb.TcpEventType_TCP_EVENT_TYPE_CONNECT:
		return "connect"
	case pb.TcpEventType_TCP_EVENT_TYPE_ACCEPT:
		return "accept"
	case pb.TcpEventType_TCP_EVENT_TYPE_CLOSE:
		return "close"
	case pb.TcpEventType_TCP_EVENT_TYPE_RESET:
		return "reset"
	default:
		return "unspecified"
	}
}

// tcpStateToString converts a proto TcpState enum to a human-readable string.
func tcpStateToString(s pb.TcpState) string {
	switch s {
	case pb.TcpState_TCP_STATE_ESTABLISHED:
		return "ESTABLISHED"
	case pb.TcpState_TCP_STATE_SYN_SENT:
		return "SYN_SENT"
	case pb.TcpState_TCP_STATE_SYN_RECEIVED:
		return "SYN_RECEIVED"
	case pb.TcpState_TCP_STATE_FIN_WAIT_1:
		return "FIN_WAIT_1"
	case pb.TcpState_TCP_STATE_FIN_WAIT_2:
		return "FIN_WAIT_2"
	case pb.TcpState_TCP_STATE_TIME_WAIT:
		return "TIME_WAIT"
	case pb.TcpState_TCP_STATE_CLOSE_WAIT:
		return "CLOSE_WAIT"
	case pb.TcpState_TCP_STATE_LAST_ACK:
		return "LAST_ACK"
	case pb.TcpState_TCP_STATE_CLOSING:
		return "CLOSING"
	case pb.TcpState_TCP_STATE_LISTEN:
		return "LISTEN"
	default:
		return "UNSPECIFIED"
	}
}

// eventTimestamp extracts a time.Time from a NetworkEvent's timestamp field.
// Falls back to time.Now() if the timestamp is nil.
func eventTimestamp(ev *pb.NetworkEvent) time.Time {
	if ev.GetTimestamp() != nil {
		return ev.GetTimestamp().AsTime()
	}
	return time.Now()
}

// InsertNetworkEvents inserts a batch of network events into the appropriate
// QuestDB tables (tcp_events, dns_events, http_events) via PG INSERT.
//
// Each event's oneof variant determines which table receives the row.
// DbQueryEvent is not stored (no table defined); unrecognized variants are skipped.
func (c *Client) InsertNetworkEvents(batch *pb.NetworkEventBatch, tenant string) error {
	ctx := context.Background()
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	agentID := batch.GetAgentId()
	events := batch.GetEvents()

	for _, ev := range events {
		ts := eventTimestamp(ev)

		switch e := ev.GetEvent().(type) {
		case *pb.NetworkEvent_TcpConnection:
			tcp := e.TcpConnection
			query := `INSERT INTO tcp_events
				(timestamp, agent_id, tenant_id, event_type, source_ip, source_port,
				 destination_ip, destination_port, state, bytes_sent, bytes_received,
				 pid, process_name)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
			if _, err := tx.Exec(ctx, query,
				ts, agentID, tenant,
				tcpEventTypeToString(tcp.GetType()),
				tcp.GetSourceIp(), tcp.GetSourcePort(),
				tcp.GetDestinationIp(), tcp.GetDestinationPort(),
				tcpStateToString(tcp.GetState()),
				tcp.GetBytesSent(), tcp.GetBytesReceived(),
				tcp.GetPid(), tcp.GetProcessName(),
			); err != nil {
				return fmt.Errorf("insert tcp event: %w", err)
			}

		case *pb.NetworkEvent_DnsQuery:
			dns := e.DnsQuery
			query := `INSERT INTO dns_events
				(timestamp, agent_id, tenant_id, query_name, query_type, latency_ms, pid)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`
			if _, err := tx.Exec(ctx, query,
				ts, agentID, tenant,
				dns.GetQueryName(), dns.GetQueryType(),
				dns.GetLatencyMs(), dns.GetPid(),
			); err != nil {
				return fmt.Errorf("insert dns event: %w", err)
			}

		case *pb.NetworkEvent_HttpRequest:
			http := e.HttpRequest
			query := `INSERT INTO http_events
				(timestamp, agent_id, tenant_id, method, path, status_code,
				 latency_ms, source_ip, destination_ip, destination_port, host, pid)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
			if _, err := tx.Exec(ctx, query,
				ts, agentID, tenant,
				http.GetMethod(), http.GetPath(), http.GetStatusCode(),
				http.GetLatencyMs(),
				http.GetSourceIp(), http.GetDestinationIp(), http.GetDestinationPort(),
				http.GetHost(), http.GetPid(),
			); err != nil {
				return fmt.Errorf("insert http event: %w", err)
			}

		default:
			// DbQueryEvent or unrecognized variant — skip.
		}
	}

	return tx.Commit(ctx)
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

// ---- Network Event Query Operations ----

// queryTCPClause builds a dynamic WHERE clause for agent_id filtering.
// Returns the base query and args slice.
func queryTCPClause(agentID string, start, end time.Time, limit int) (string, []any) {
	if agentID != "" {
		return `SELECT timestamp, agent_id, event_type, source_ip, source_port,
			destination_ip, destination_port, state, bytes_sent, bytes_received,
			pid, process_name
		FROM tcp_events
		WHERE agent_id = $1 AND timestamp >= $2 AND timestamp <= $3
		ORDER BY timestamp DESC
		LIMIT $4`,
			[]any{agentID, start, end, limit}
	}
	return `SELECT timestamp, agent_id, event_type, source_ip, source_port,
		destination_ip, destination_port, state, bytes_sent, bytes_received,
		pid, process_name
	FROM tcp_events
	WHERE timestamp >= $1 AND timestamp <= $2
	ORDER BY timestamp DESC
	LIMIT $3`,
		[]any{start, end, limit}
}

// QueryTCPEvents queries TCP network events from QuestDB.
func (c *Client) QueryTCPEvents(ctx context.Context, agentID string, start, end time.Time, limit int) ([]map[string]interface{}, error) {
	query, args := queryTCPClause(agentID, start, end, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tcp events: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var ts time.Time
		var agentIDVal string
		var eventType sql.NullString
		var srcIP, dstIP, state, processName sql.NullString
		var srcPort, dstPort, pid sql.NullInt32
		var bytesSent, bytesRecv sql.NullInt64

		if err := rows.Scan(
			&ts, &agentIDVal, &eventType,
			&srcIP, &srcPort, &dstIP, &dstPort,
			&state, &bytesSent, &bytesRecv,
			&pid, &processName,
		); err != nil {
			return nil, fmt.Errorf("scan tcp event: %w", err)
		}
		results = append(results, map[string]interface{}{
			"type":             "tcp",
			"timestamp":        ts.UnixMilli(),
			"agent_id":         agentIDVal,
			"event_type":       nullStringVal(eventType),
			"source_ip":        nullStringVal(srcIP),
			"source_port":      nullInt32Val(srcPort),
			"destination_ip":   nullStringVal(dstIP),
			"destination_port": nullInt32Val(dstPort),
			"state":            nullStringVal(state),
			"bytes_sent":       nullInt64Val(bytesSent),
			"bytes_received":   nullInt64Val(bytesRecv),
			"pid":              nullInt32Val(pid),
			"process_name":     nullStringVal(processName),
		})
	}
	return results, rows.Err()
}

// queryDNSClause builds a dynamic WHERE clause for agent_id filtering.
func queryDNSClause(agentID string, start, end time.Time, limit int) (string, []any) {
	if agentID != "" {
		return `SELECT timestamp, agent_id, query_name, query_type, latency_ms, pid
		FROM dns_events
		WHERE agent_id = $1 AND timestamp >= $2 AND timestamp <= $3
		ORDER BY timestamp DESC
		LIMIT $4`,
			[]any{agentID, start, end, limit}
	}
	return `SELECT timestamp, agent_id, query_name, query_type, latency_ms, pid
	FROM dns_events
	WHERE timestamp >= $1 AND timestamp <= $2
	ORDER BY timestamp DESC
	LIMIT $3`,
		[]any{start, end, limit}
}

// QueryDNSEvents queries DNS network events from QuestDB.
func (c *Client) QueryDNSEvents(ctx context.Context, agentID string, start, end time.Time, limit int) ([]map[string]interface{}, error) {
	query, args := queryDNSClause(agentID, start, end, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query dns events: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var ts time.Time
		var agentIDVal string
		var queryName, queryType sql.NullString
		var latencyMs sql.NullFloat64
		var pid sql.NullInt32

		if err := rows.Scan(
			&ts, &agentIDVal, &queryName, &queryType,
			&latencyMs, &pid,
		); err != nil {
			return nil, fmt.Errorf("scan dns event: %w", err)
		}
		results = append(results, map[string]interface{}{
			"type":        "dns",
			"timestamp":   ts.UnixMilli(),
			"agent_id":    agentIDVal,
			"query_name":  nullStringVal(queryName),
			"query_type":  nullStringVal(queryType),
			"latency_ms":  nullFloat64Val(latencyMs),
			"pid":         nullInt32Val(pid),
		})
	}
	return results, rows.Err()
}

// queryHTTPClause builds a dynamic WHERE clause for agent_id filtering.
func queryHTTPClause(agentID string, start, end time.Time, limit int) (string, []any) {
	if agentID != "" {
		return `SELECT timestamp, agent_id, method, path, status_code, latency_ms,
			source_ip, destination_ip, destination_port, host, pid
		FROM http_events
		WHERE agent_id = $1 AND timestamp >= $2 AND timestamp <= $3
		ORDER BY timestamp DESC
		LIMIT $4`,
			[]any{agentID, start, end, limit}
	}
	return `SELECT timestamp, agent_id, method, path, status_code, latency_ms,
		source_ip, destination_ip, destination_port, host, pid
	FROM http_events
	WHERE timestamp >= $1 AND timestamp <= $2
	ORDER BY timestamp DESC
	LIMIT $3`,
		[]any{start, end, limit}
}

// QueryHTTPEvents queries HTTP network events from QuestDB.
func (c *Client) QueryHTTPEvents(ctx context.Context, agentID string, start, end time.Time, limit int) ([]map[string]interface{}, error) {
	query, args := queryHTTPClause(agentID, start, end, limit)

	rows, err := c.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query http events: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var ts time.Time
		var agentIDVal string
		var method, path, srcIP, dstIP, host sql.NullString
		var statusCode, dstPort, pid sql.NullInt32
		var latencyMs sql.NullFloat64

		if err := rows.Scan(
			&ts, &agentIDVal, &method, &path, &statusCode, &latencyMs,
			&srcIP, &dstIP, &dstPort, &host, &pid,
		); err != nil {
			return nil, fmt.Errorf("scan http event: %w", err)
		}
		results = append(results, map[string]interface{}{
			"type":             "http",
			"timestamp":        ts.UnixMilli(),
			"agent_id":         agentIDVal,
			"method":           nullStringVal(method),
			"path":             nullStringVal(path),
			"status_code":      nullInt32Val(statusCode),
			"latency_ms":       nullFloat64Val(latencyMs),
			"source_ip":        nullStringVal(srcIP),
			"destination_ip":   nullStringVal(dstIP),
			"destination_port": nullInt32Val(dstPort),
			"host":             nullStringVal(host),
			"pid":              nullInt32Val(pid),
		})
	}
	return results, rows.Err()
}

// QueryNetworkEvents queries network events from the appropriate QuestDB table(s).
// If eventType is empty, all three tables are queried and results are merged.
func (c *Client) QueryNetworkEvents(ctx context.Context, agentID, eventType string, start, end time.Time, limit int) ([]map[string]interface{}, error) {
	switch eventType {
	case "tcp":
		return c.QueryTCPEvents(ctx, agentID, start, end, limit)
	case "dns":
		return c.QueryDNSEvents(ctx, agentID, start, end, limit)
	case "http":
		return c.QueryHTTPEvents(ctx, agentID, start, end, limit)
	case "":
		// Query all tables and merge results.
		var all []map[string]interface{}

		tcpEvents, err := c.QueryTCPEvents(ctx, agentID, start, end, limit)
		if err != nil {
			slog.Warn("query tcp events failed", "error", err)
		} else {
			all = append(all, tcpEvents...)
		}

		dnsEvents, err := c.QueryDNSEvents(ctx, agentID, start, end, limit)
		if err != nil {
			slog.Warn("query dns events failed", "error", err)
		} else {
			all = append(all, dnsEvents...)
		}

		httpEvents, err := c.QueryHTTPEvents(ctx, agentID, start, end, limit)
		if err != nil {
			slog.Warn("query http events failed", "error", err)
		} else {
			all = append(all, httpEvents...)
		}

		return all, nil
	default:
		return nil, fmt.Errorf("unknown event type: %s (valid: tcp, dns, http)", eventType)
	}
}

// ---- Nullable helper functions ----

// nullStringVal extracts the string value from a sql.NullString, returning "" if invalid.
func nullStringVal(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// nullInt32Val extracts the int32 value from a sql.NullInt32, returning 0 if invalid.
func nullInt32Val(ni sql.NullInt32) int32 {
	if ni.Valid {
		return ni.Int32
	}
	return 0
}

// nullInt64Val extracts the int64 value from a sql.NullInt64, returning 0 if invalid.
func nullInt64Val(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

// nullFloat64Val extracts the float64 value from a sql.NullFloat64, returning 0 if invalid.
func nullFloat64Val(nf sql.NullFloat64) float64 {
	if nf.Valid {
		return nf.Float64
	}
	return 0
}
