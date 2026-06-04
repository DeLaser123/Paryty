// Package warm — Production-grade ILP writer for QuestDB high-throughput ingestion.
//
// ILPWriter wraps the lower-level ILPSender with production features:
//   - Background periodic flush (configurable interval)
//   - Line-count-based auto-flush (configurable buffer size)
//   - Observability metrics (rows written, errors, flush count, latency)
//   - PG INSERT fallback on ILP failure (critical for Windows TCP reliability)
//   - Graceful shutdown with idempotent Close
//
// Usage:
//
//	writer, err := warm.NewILPWriter("localhost:9009", logger,
//	    warm.WithBufferSize(200),
//	    warm.WithFlushInterval(2*time.Second),
//	    warm.WithFallback(client.InsertMetricBatch),
//	)
//	defer writer.Close()
//
//	err = writer.WriteMetricBatch(batch, "tenant-a")
package warm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

const (
	// defaultILPBufSize is the maximum number of buffered lines before auto-flush.
	// 100 lines balances latency (~100ms of data at typical ingest rates) and
	// throughput (amortizes per-flush TCP overhead).
	defaultILPBufSize = 100

	// defaultILPFlushInterval is the maximum time between periodic flushes.
	// 1 second ensures data reaches QuestDB within 1s even under low load.
	defaultILPFlushInterval = 1 * time.Second
)

// Sentinel errors for ILP writer operations.
var (
	// ErrILPWriterClosed is returned when a write operation is attempted on a closed writer.
	ErrILPWriterClosed = fmt.Errorf("ILP writer is closed")
)

// ---- Types ----

// ILPMetric represents a single metric line for ILP ingestion.
// Fields use float64 for simple numeric metrics. For mixed-type fields
// (strings, bools, integers), use WriteMetricBatch which handles all
// Go types via the underlying ILPSender.
type ILPMetric struct {
	// Table is the QuestDB table name (e.g., "cpu_metrics", "memory_metrics").
	Table string

	// Tags are key-value pairs mapped to SYMBOL columns in QuestDB.
	// Tags with empty values are silently skipped (prevents invalid ILP lines).
	Tags map[string]string

	// Fields are numeric field values. Keys must match QuestDB column names.
	Fields map[string]float64

	// Timestamp is the metric collection time.
	Timestamp time.Time
}

// ILPMetrics contains observability counters for the ILP writer.
// All fields are protected by the writer's mutex and are safe to read
// concurrently via the Metrics() method.
type ILPMetrics struct {
	// RowsWritten is the total number of metric lines successfully sent to the ILP sender.
	RowsWritten int64

	// WriteErrors is the total number of failed write/flush operations.
	WriteErrors int64

	// FlushCount is the total number of flush operations (both manual and automatic).
	FlushCount int64

	// ReconnectCount is reserved for tracking ILP sender reconnections.
	// Currently always 0; the ILPSender handles reconnections transparently.
	ReconnectCount int64

	// LastFlushTime is when the most recent flush completed.
	LastFlushTime time.Time

	// AvgFlushLatencyMs is the rolling average flush latency in milliseconds.
	AvgFlushLatencyMs float64
}

// ILPOption configures the ILPWriter. Pass options to NewILPWriter.
type ILPOption func(*ILPWriter)

// WithBufferSize sets the maximum number of buffered lines before auto-flush.
// Default is 100. Lower values reduce latency; higher values improve throughput.
// Values <= 0 are silently ignored (keep default).
func WithBufferSize(n int) ILPOption {
	return func(w *ILPWriter) {
		if n > 0 {
			w.bufSize = n
		}
	}
}

// WithFlushInterval sets the interval for periodic background flush.
// Default is 1 second. The background goroutine flushes at this interval
// even if the buffer hasn't reached bufSize.
// Values <= 0 are silently ignored (keep default).
func WithFlushInterval(d time.Duration) ILPOption {
	return func(w *ILPWriter) {
		if d > 0 {
			w.flushInterval = d
		}
	}
}

// WithFallback sets the PG INSERT fallback function called when ILP writes fail.
// This is critical for Windows environments where TCP/ILP has known connection
// issues ("wsasend: connection aborted"). The fallback receives the original
// MetricBatch and tenant, and should perform a PG INSERT.
func WithFallback(fn func(*models.MetricBatch, string) error) ILPOption {
	return func(w *ILPWriter) {
		w.fallbackFn = fn
	}
}

// ---- ILPWriter ----

// ILPWriter wraps ILPSender with production features for QuestDB ILP ingestion.
//
// Thread-safety: all public methods are safe for concurrent use from multiple
// goroutines. Internal locking strategy:
//   - w.mu protects metrics counters, lineCount, and closed flag
//   - ILPSender has its own mutexes for buffer and connection
//   - Locks are never held across sender method calls (no deadlock risk)
type ILPWriter struct {
	sender        *ILPSender
	addr          string
	logger        *zap.Logger
	mu            sync.Mutex
	metrics       ILPMetrics
	bufSize       int
	flushInterval time.Duration
	closed        bool
	ticker        *time.Ticker
	done          chan struct{}
	fallbackFn    func(batch *models.MetricBatch, tenant string) error

	// lineCount tracks lines sent to the sender since the last writer-level flush.
	// Used to trigger auto-flush when count reaches bufSize.
	lineCount int

	// flushLatencySum accumulates flush durations for computing AvgFlushLatencyMs.
	flushLatencySum time.Duration
}

// NewILPWriter creates a new ILPWriter connected to the given QuestDB ILP address.
// The addr should be in "host:port" format. If no port is present, 9009 is used.
//
// A background goroutine is started that flushes buffered data at the configured
// flush interval. The goroutine is stopped when Close() is called.
//
// Returns an error if the logger is nil or the initial ILP connection fails.
func NewILPWriter(addr string, logger *zap.Logger, opts ...ILPOption) (*ILPWriter, error) {
	if logger == nil {
		return nil, fmt.Errorf("ILP writer logger must not be nil")
	}

	sender, err := NewILPSender(addr)
	if err != nil {
		return nil, fmt.Errorf("create ILP sender for writer: %w", err)
	}

	w := &ILPWriter{
		sender:        sender,
		addr:          addr,
		logger:        logger,
		bufSize:       defaultILPBufSize,
		flushInterval: defaultILPFlushInterval,
		done:          make(chan struct{}),
	}

	for _, opt := range opts {
		opt(w)
	}

	// Start background flush goroutine.
	w.ticker = time.NewTicker(w.flushInterval)
	go w.runFlushLoop()

	logger.Info("ILP writer started",
		zap.String("addr", addr),
		zap.Int("buf_size", w.bufSize),
		zap.Duration("flush_interval", w.flushInterval),
		zap.Bool("fallback_enabled", w.fallbackFn != nil),
	)

	return w, nil
}

// ---- Public Methods ----

// WriteMetric writes a single metric as an ILP line.
// The fields map uses float64 values for all field data. For mixed-type fields
// (strings, booleans, integers), use WriteMetricBatch instead.
//
// Returns ErrILPWriterClosed if the writer has been closed.
// Returns an error if the context is cancelled or the ILP send fails.
func (w *ILPWriter) WriteMetric(
	ctx context.Context,
	table string,
	tags map[string]string,
	fields map[string]float64,
	timestamp time.Time,
) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrILPWriterClosed
	}
	w.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	anyFields := float64MapToAny(fields)
	return w.sendLine(table, tags, anyFields, timestamp)
}

// WriteBatch buffers all metrics and flushes once at the end.
// Each metric is sent to the ILPSender's internal buffer. After all metrics
// are buffered, a single Flush() sends everything to QuestDB.
//
// Returns an error if any metric fails to send or the final flush fails.
// The context is checked between individual metrics for cancellation.
func (w *ILPWriter) WriteBatch(ctx context.Context, metrics []ILPMetric) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrILPWriterClosed
	}
	w.mu.Unlock()

	for i := range metrics {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled at metric %d/%d: %w", i+1, len(metrics), err)
		}

		anyFields := float64MapToAny(metrics[i].Fields)
		if err := w.sendLine(metrics[i].Table, metrics[i].Tags, anyFields, metrics[i].Timestamp); err != nil {
			return fmt.Errorf("write metric %d/%d to %s: %w", i+1, len(metrics), metrics[i].Table, err)
		}
	}

	// Flush once after all metrics are buffered.
	return w.Flush()
}

// WriteMetricBatch converts a MetricBatch to ILP lines and writes them.
// Supports all metric types: CPU, memory, disk, network, process, container.
//
// The conversion maps SYMBOL columns to ILP tags and all other columns to
// ILP fields, matching the QuestDB table schemas exactly.
//
// If ILP write or flush fails and a fallback function is configured (via
// WithFallback), the fallback is called with the original batch and tenant.
// This is the primary mechanism for handling Windows TCP/ILP reliability issues.
//
// Note: On partial ILP failure with successful fallback, some rows may exist
// in both QuestDB (via ILP) and the fallback store. This is an accepted
// trade-off for the Windows TCP workaround — QuestDB tables don't enforce
// unique constraints on regular columns.
func (w *ILPWriter) WriteMetricBatch(batch *models.MetricBatch, tenant string) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return ErrILPWriterClosed
	}
	w.mu.Unlock()

	// Send all metric lines without flushing (let the sender buffer them).
	if err := w.sendBatchLines(batch, tenant); err != nil {
		return w.handleBatchError(batch, tenant, err, "ILP write")
	}

	// Flush all buffered lines to QuestDB.
	if err := w.Flush(); err != nil {
		return w.handleBatchError(batch, tenant, err, "ILP flush")
	}

	return nil
}

// Flush forces a flush of all buffered ILP data to QuestDB.
// This flushes both the ILPWriter's line count tracking and the
// underlying ILPSender's byte buffer.
//
// Flush is safe to call from multiple goroutines concurrently.
// The sender's Flush is internally synchronized.
func (w *ILPWriter) Flush() error {
	start := time.Now()
	err := w.sender.Flush()
	elapsed := time.Since(start)

	w.mu.Lock()
	w.metrics.FlushCount++
	w.metrics.LastFlushTime = time.Now()
	w.flushLatencySum += elapsed
	if w.metrics.FlushCount > 0 {
		w.metrics.AvgFlushLatencyMs = float64(w.flushLatencySum.Milliseconds()) / float64(w.metrics.FlushCount)
	}
	w.lineCount = 0
	if err != nil {
		w.metrics.WriteErrors++
	}
	w.mu.Unlock()

	if err != nil {
		return fmt.Errorf("flush ILP: %w", err)
	}
	return nil
}

// Metrics returns a point-in-time snapshot of the ILP writer metrics.
// Safe to call from any goroutine.
func (w *ILPWriter) Metrics() ILPMetrics {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.metrics
}

// Close stops the background flush goroutine, performs a final flush,
// and closes the underlying ILP sender's TCP connection.
//
// Close is idempotent — subsequent calls return nil without side effects.
// After Close returns, all write methods return ErrILPWriterClosed.
func (w *ILPWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	w.mu.Unlock()

	// Stop the background flush goroutine.
	// Order: stop ticker first (prevents new ticks), then signal done.
	if w.ticker != nil {
		w.ticker.Stop()
	}
	close(w.done)

	// Capture final metrics for logging before any errors.
	w.mu.Lock()
	rowsWritten := w.metrics.RowsWritten
	writeErrors := w.metrics.WriteErrors
	flushCount := w.metrics.FlushCount
	w.mu.Unlock()

	// Final flush and close sender. Continue cleanup even if flush fails.
	var errs []error
	if err := w.Flush(); err != nil {
		errs = append(errs, fmt.Errorf("final flush: %w", err))
	}
	if err := w.sender.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close sender: %w", err))
	}

	w.logger.Info("ILP writer closed",
		zap.String("addr", w.addr),
		zap.Int64("rows_written", rowsWritten),
		zap.Int64("write_errors", writeErrors),
		zap.Int64("flush_count", flushCount),
	)

	if len(errs) > 0 {
		return fmt.Errorf("close ILP writer: %v", errs)
	}
	return nil
}

// ---- Internal Methods ----

// runFlushLoop is the background goroutine that flushes buffered data at
// the configured interval. It exits when the done channel is closed.
func (w *ILPWriter) runFlushLoop() {
	for {
		select {
		case <-w.ticker.C:
			if err := w.Flush(); err != nil {
				w.logger.Warn("background flush failed",
					zap.String("addr", w.addr),
					zap.Error(err),
				)
			}
		case <-w.done:
			return
		}
	}
}

// sendLine sends a single ILP line to the sender and tracks metrics.
// Increments RowsWritten on success, WriteErrors on failure.
// May trigger auto-flush when the line count reaches bufSize.
//
// Locking: w.mu is acquired briefly for counter updates, never held
// across sender.SendMetric or Flush calls.
func (w *ILPWriter) sendLine(table string, tags map[string]string, fields map[string]any, ts time.Time) error {
	if err := w.sender.SendMetric(table, tags, fields, ts); err != nil {
		w.mu.Lock()
		w.metrics.WriteErrors++
		w.mu.Unlock()
		return fmt.Errorf("send ILP line to %s: %w", table, err)
	}

	w.mu.Lock()
	w.metrics.RowsWritten++
	w.lineCount++
	count := w.lineCount
	limit := w.bufSize
	w.mu.Unlock()

	// Auto-flush when the line count reaches the buffer size limit.
	if count >= limit {
		return w.Flush()
	}
	return nil
}

// sendBatchLines sends all metric lines from a MetricBatch to the ILP sender
// without performing a final flush. The caller is responsible for calling Flush.
//
// Returns on the first error encountered. Successfully-sent lines before
// the error remain in the sender's buffer (or were already auto-flushed).
func (w *ILPWriter) sendBatchLines(batch *models.MetricBatch, tenant string) error {
	// CPU metrics — SYMBOL columns: agent_id, tenant_id
	for i := range batch.CPU {
		cpu := batch.CPU[i]
		if err := w.sendLine("cpu_metrics", cpuILPTags(cpu.AgentID, tenant), cpuILPFields(cpu), cpu.Timestamp); err != nil {
			return err
		}
	}

	// Memory metrics — SYMBOL columns: agent_id, tenant_id
	for i := range batch.Memory {
		mem := batch.Memory[i]
		if err := w.sendLine("memory_metrics", basicILPTags(mem.AgentID, tenant), memILPFields(mem), mem.Timestamp); err != nil {
			return err
		}
	}

	// Disk metrics — SYMBOL columns: agent_id, tenant_id, device, mount_point, filesystem_type
	for i := range batch.Disk {
		d := batch.Disk[i]
		if err := w.sendLine("disk_metrics", diskILPTags(d, tenant), diskILPFields(d), d.Timestamp); err != nil {
			return err
		}
	}

	// Network metrics — SYMBOL columns: agent_id, tenant_id, interface
	for i := range batch.Network {
		n := batch.Network[i]
		if err := w.sendLine("network_metrics", netILPTags(n, tenant), netILPFields(n), n.Timestamp); err != nil {
			return err
		}
	}

	// Process metrics — SYMBOL columns: agent_id, tenant_id, name, status, container_id
	for i := range batch.Processes {
		p := batch.Processes[i]
		if err := w.sendLine("process_metrics", procILPTags(p, tenant), procILPFields(p), p.Timestamp); err != nil {
			return err
		}
	}

	// Container metrics — SYMBOL columns: agent_id, tenant_id, container_id, runtime,
	// name, image, status, cgroup_version
	for i := range batch.Containers {
		c := batch.Containers[i]
		if err := w.sendLine("container_metrics", containerILPTags(c, tenant), containerILPFields(c), c.Timestamp); err != nil {
			return err
		}
	}

	return nil
}

// handleBatchError logs the ILP error and falls back to PG INSERT if configured.
// This is the central error-handling path for WriteMetricBatch.
func (w *ILPWriter) handleBatchError(batch *models.MetricBatch, tenant string, err error, op string) error {
	w.logger.Warn("ILP operation failed, checking for PG fallback",
		zap.String("operation", op),
		zap.String("agent_id", batch.AgentID),
		zap.String("tenant", tenant),
		zap.Error(err),
	)

	if w.fallbackFn != nil {
		w.logger.Info("falling back to PG INSERT",
			zap.String("agent_id", batch.AgentID),
			zap.String("tenant", tenant),
		)
		if fbErr := w.fallbackFn(batch, tenant); fbErr != nil {
			// Both ILP and fallback failed — return the fallback error
			// as it's the most recent failure context.
			return fmt.Errorf("ILP failed (%w), PG fallback also failed: %w", err, fbErr)
		}
		return nil
	}

	return err
}

// ---- ILP Line Formatting ----

// float64MapToAny converts a map[string]float64 to map[string]any
// for compatibility with the ILPSender's SendMetric signature.
func float64MapToAny(m map[string]float64) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// formatILPMetric formats an ILPMetric as an ILP line string.
// This is a pure function suitable for unit testing.
func formatILPMetric(m ILPMetric) string {
	return formatILPLine(m.Table, m.Tags, float64MapToAny(m.Fields), m.Timestamp)
}

// ---- MetricBatch → ILP Conversion ----
//
// These functions convert typed metric structs from the models package into
// ILP tag and field maps. The mapping follows QuestDB conventions:
//   - Tags → SYMBOL columns (low-cardinality identifiers)
//   - Fields → all other column types (DOUBLE, LONG, INT, STRING, BOOLEAN, TIMESTAMP)
//
// Empty tag values are skipped to prevent invalid ILP lines (the existing
// writeSortedTags function in ilp.go handles this).
//
// All fields are included even when zero-valued to maintain idempotency:
// same input always produces the same ILP line.

// cpuILPTags returns the tag set for a CPU metric row.
// cpu_metrics SYMBOL columns: agent_id, tenant_id.
func cpuILPTags(agentID, tenant string) map[string]string {
	return map[string]string{
		"agent_id":  agentID,
		"tenant_id": tenant,
	}
}

// cpuILPFields converts a CPUMetrics struct to an ILP field map.
func cpuILPFields(m models.CPUMetrics) map[string]any {
	return map[string]any{
		"total_usage_pct":  m.TotalUsagePct,
		"per_core_pct":     formatFloatSlice(m.PerCorePct),
		"load_avg_1":       m.LoadAvg1m,
		"load_avg_5":       m.LoadAvg5m,
		"load_avg_15":      m.LoadAvg15m,
		"frequency_mhz":    m.FrequencyMHz,
		"context_switches": m.ContextSwitches,
		"physical_cores":   m.PhysicalCores,
		"logical_cores":    m.LogicalCores,
		"model_name":       m.ModelName,
		"vendor_id":        m.VendorID,
	}
}

// basicILPTags returns the minimal tag set (agent_id + tenant_id).
// Used for metric tables with only these two SYMBOL columns.
func basicILPTags(agentID, tenant string) map[string]string {
	return map[string]string{
		"agent_id":  agentID,
		"tenant_id": tenant,
	}
}

// memILPFields converts a MemoryMetrics struct to an ILP field map.
// PSI pressure fields default to 0 when Pressure is nil.
func memILPFields(m models.MemoryMetrics) map[string]any {
	f := map[string]any{
		"total_bytes":      m.TotalBytes,
		"used_bytes":       m.UsedBytes,
		"free_bytes":       m.FreeBytes,
		"available_bytes":  m.AvailableBytes,
		"cached_bytes":     m.CachedBytes,
		"buffer_bytes":     m.BufferBytes,
		"swap_total_bytes": m.SwapTotalBytes,
		"swap_used_bytes":  m.SwapUsedBytes,
		"usage_percent":    m.UsagePercent,
	}

	// PSI pressure fields — default to 0 when not present.
	var some10, some60, some300, full10, full60, full300 float64
	if m.Pressure != nil {
		some10 = m.Pressure.Some10
		some60 = m.Pressure.Some60
		some300 = m.Pressure.Some300
		full10 = m.Pressure.Full10
		full60 = m.Pressure.Full60
		full300 = m.Pressure.Full300
	}
	f["pressure_some_avg10"] = some10
	f["pressure_some_avg60"] = some60
	f["pressure_some_avg300"] = some300
	f["pressure_full_avg10"] = full10
	f["pressure_full_avg60"] = full60
	f["pressure_full_avg300"] = full300

	return f
}

// diskILPTags returns tags for a disk metric row.
// disk_metrics SYMBOL columns: agent_id, tenant_id, device, mount_point, filesystem_type.
func diskILPTags(m models.DiskMetrics, tenant string) map[string]string {
	tags := map[string]string{
		"agent_id":  m.AgentID,
		"tenant_id": tenant,
	}
	if m.Device != "" {
		tags["device"] = m.Device
	}
	if m.MountPoint != "" {
		tags["mount_point"] = m.MountPoint
	}
	if m.FilesystemType != "" {
		tags["filesystem_type"] = m.FilesystemType
	}
	return tags
}

// diskILPFields converts a DiskMetrics struct to an ILP field map.
func diskILPFields(m models.DiskMetrics) map[string]any {
	return map[string]any{
		"total_bytes":         m.TotalBytes,
		"used_bytes":          m.UsedBytes,
		"free_bytes":          m.FreeBytes,
		"read_bytes_per_sec":  m.ReadBytesPerSec,
		"write_bytes_per_sec": m.WriteBytesPerSec,
		"iops_read":           m.IOPSRead,
		"iops_write":          m.IOPSWrite,
		"io_latency_ms":       m.IOLatencyMs,
		"queue_depth":         m.QueueDepth,
		"is_ssd":              m.IsSSD,
		"utilization_pct":     m.UtilizationPct,
	}
}

// netILPTags returns tags for a network metric row.
// network_metrics SYMBOL columns: agent_id, tenant_id, interface.
func netILPTags(m models.NetworkMetrics, tenant string) map[string]string {
	tags := map[string]string{
		"agent_id":  m.AgentID,
		"tenant_id": tenant,
	}
	if m.Interface != "" {
		tags["interface"] = m.Interface
	}
	return tags
}

// netILPFields converts a NetworkMetrics struct to an ILP field map.
// TCP stats fields default to 0 when TCPStats is nil.
func netILPFields(m models.NetworkMetrics) map[string]any {
	f := map[string]any{
		"rx_bytes_per_sec": m.RxBytesPerSec,
		"tx_bytes_per_sec": m.TxBytesPerSec,
		"rx_packets":       m.RxPackets,
		"tx_packets":       m.TxPackets,
		"rx_dropped":       m.RxDropped,
		"tx_dropped":       m.TxDropped,
		"errors":           m.Errors,
		"estimated_rtt_ms": m.EstimatedRTTMs,
		"total_rx_bytes":   m.TotalRxBytes,
		"total_tx_bytes":   m.TotalTxBytes,
		"total_rx_packets": m.TotalRxPackets,
		"total_tx_packets": m.TotalTxPackets,
		"speed_mbps":       m.SpeedMbps,
		"is_up":            m.IsUp,
	}

	// TCP stats — default to 0 when not present.
	var established, timeWait, listen int32
	var retransmitCount int64
	if m.TCPStats != nil {
		established = m.TCPStats.Established
		timeWait = m.TCPStats.TimeWait
		listen = m.TCPStats.Listen
		retransmitCount = m.TCPStats.RetransmitCount
	}
	f["tcp_established"] = established
	f["tcp_time_wait"] = timeWait
	f["tcp_listen"] = listen
	f["tcp_retransmit_count"] = retransmitCount

	return f
}

// procILPTags returns tags for a process metric row.
// process_metrics SYMBOL columns: agent_id, tenant_id, name, status, container_id.
func procILPTags(m models.ProcessMetrics, tenant string) map[string]string {
	tags := map[string]string{
		"agent_id":  m.AgentID,
		"tenant_id": tenant,
	}
	if m.Name != "" {
		tags["name"] = m.Name
	}
	if m.Status != "" {
		tags["status"] = m.Status
	}
	if m.ContainerID != "" {
		tags["container_id"] = m.ContainerID
	}
	return tags
}

// procILPFields converts a ProcessMetrics struct to an ILP field map.
func procILPFields(m models.ProcessMetrics) map[string]any {
	return map[string]any{
		"pid":                m.PID,
		"parent_pid":         m.ParentPID,
		"command_line":       m.CommandLine,
		"cpu_usage_pct":      m.CPUUsagePct,
		"memory_bytes":       m.MemoryBytes,
		"vsz_bytes":          m.VszBytes,
		"threads":            m.Threads,
		"fd_count":           m.FdCount,
		"exe":                m.Exe,
		"disk_read_bytes":    m.DiskReadBytes,
		"disk_written_bytes": m.DiskWrittenBytes,
		"user_id":            m.UserID,
		"started_at":         m.StartedAt,
	}
}

// containerILPTags returns tags for a container metric row.
// container_metrics SYMBOL columns: agent_id, tenant_id, container_id, runtime,
// name, image, status, cgroup_version.
func containerILPTags(m models.ContainerMetrics, tenant string) map[string]string {
	tags := map[string]string{
		"agent_id":  m.AgentID,
		"tenant_id": tenant,
	}
	if m.ContainerID != "" {
		tags["container_id"] = m.ContainerID
	}
	if m.Runtime != "" {
		tags["runtime"] = m.Runtime
	}
	if m.Name != "" {
		tags["name"] = m.Name
	}
	if m.Image != "" {
		tags["image"] = m.Image
	}
	if m.Status != "" {
		tags["status"] = m.Status
	}
	if m.CgroupVersion != "" {
		tags["cgroup_version"] = m.CgroupVersion
	}
	return tags
}

// containerILPFields converts a ContainerMetrics struct to an ILP field map.
func containerILPFields(m models.ContainerMetrics) map[string]any {
	return map[string]any{
		"memory_limit_bytes": m.MemoryLimitBytes,
		"cpu_quota":          m.CPUQuota,
		"cpu_shares":         m.CPUShares,
	}
}
