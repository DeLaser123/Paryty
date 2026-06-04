// Package processing implements the data processing pipeline.
// It includes aggregator, correlator, and enricher services.
package processing

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// AggregatorConfig holds configuration for the windowed aggregation engine.
type AggregatorConfig struct {
	// WindowSizes are the tumbling window durations applied to every incoming metric.
	// Default: 1m, 5m, 1h, 1d.
	WindowSizes []time.Duration

	// GracePeriod is how long after a window closes to still accept late data.
	// Default: 30s.
	GracePeriod time.Duration

	// SnapshotInterval controls how often window state is persisted to Dragonfly.
	// Default: 30s.
	SnapshotInterval time.Duration

	// TopNSize is the number of top entries tracked per category.
	// Default: 10.
	TopNSize int

	// TimeFunc returns the current time. Defaults to time.Now.
	// Inject a custom function for deterministic testing.
	TimeFunc func() time.Time
}

// applyDefaults fills zero-value fields with production defaults.
func (c *AggregatorConfig) applyDefaults() {
	if len(c.WindowSizes) == 0 {
		c.WindowSizes = []time.Duration{Window1Min, Window5Min, Window1Hr, Window1Day}
	}
	if c.GracePeriod == 0 {
		c.GracePeriod = 30 * time.Second
	}
	if c.SnapshotInterval == 0 {
		c.SnapshotInterval = 30 * time.Second
	}
	if c.TopNSize == 0 {
		c.TopNSize = 10
	}
	if c.TimeFunc == nil {
		c.TimeFunc = time.Now
	}
}

// AggregatorHealth reports the current operational status of the aggregator.
type AggregatorHealth struct {
	// WindowsOpen is the number of active tumbling windows in memory.
	WindowsOpen int `json:"windows_open"`

	// ValuesProcessed is the total number of metric values processed since start.
	ValuesProcessed int64 `json:"values_processed"`

	// TopNSize is the configured per-category Top-N limit.
	TopNSize int `json:"top_n_size"`
}

// Aggregator is a stateful, windowed aggregation engine that replaces the
// previous stateless batch processor.
//
// It accepts metric batches, distributes extracted values into tumbling windows
// managed by WindowStateManager, tracks Top-N process rankings via TopNTracker,
// and produces AggregatedMetric results when windows close.
//
// Thread-safe. All public methods are safe for concurrent use.
type Aggregator struct {
	config     AggregatorConfig
	windows    *WindowStateManager
	topn       *TopNTracker
	logger     *zap.Logger
	valuesProc atomic.Int64
	started    atomic.Bool
}

// NewAggregator creates a new windowed aggregation engine.
//
// Parameters:
//   - config: aggregation configuration (defaults applied to zero-value fields).
//   - dragonfly: DragonflyClient for window snapshot persistence. May be nil.
//   - logger: structured logger. If nil, a no-op logger is used.
//
// Returns an error if the configuration is invalid.
func NewAggregator(config AggregatorConfig, dragonfly DragonflyClient, logger *zap.Logger) (*Aggregator, error) {
	config.applyDefaults()

	if logger == nil {
		logger = zap.NewNop()
	}

	wmCfg := WindowManagerConfig{
		GracePeriod:      config.GracePeriod,
		WindowSizes:      config.WindowSizes,
		SnapshotInterval: config.SnapshotInterval,
		Dragonfly:        dragonfly,
		Logger:           slog.Default(),
		TimeFunc:         config.TimeFunc,
	}

	windows := NewWindowStateManager(wmCfg)

	categories := []string{"cpu.process", "memory.process"}
	topn := NewTopNTracker(config.TopNSize, categories, logger)

	agg := &Aggregator{
		config:  config,
		windows: windows,
		topn:    topn,
		logger:  logger,
	}

	return agg, nil
}

// Start launches the window manager background goroutine that periodically
// checks for closed windows, snapshots state, and purges stale windows.
// Must be called before Stop. Safe to call multiple times.
func (a *Aggregator) Start(ctx context.Context) {
	if a.started.Swap(true) {
		return // already started
	}
	a.windows.Start(ctx)
}

// Stop gracefully shuts down the window manager background goroutine.
// If Start was never called, Stop is a no-op. Safe to call multiple times.
func (a *Aggregator) Stop() {
	if !a.started.Load() {
		return
	}
	a.windows.Stop()
}

// Health returns the current operational status of the aggregator.
func (a *Aggregator) Health() AggregatorHealth {
	return AggregatorHealth{
		WindowsOpen:     a.windows.WindowCount(),
		ValuesProcessed: a.valuesProc.Load(),
		TopNSize:        a.config.TopNSize,
	}
}

// Snapshot persists the current window state to Dragonfly for crash recovery.
// Delegates to the underlying WindowStateManager.
func (a *Aggregator) Snapshot(ctx context.Context) error {
	return a.windows.Snapshot(ctx)
}

// Restore loads window state from a previous snapshot in Dragonfly.
// Delegates to the underlying WindowStateManager.
func (a *Aggregator) Restore(ctx context.Context) error {
	return a.windows.Restore(ctx)
}

// Process is the main entry point for the aggregation pipeline stage.
//
// It performs the following steps:
//  1. Extracts all numeric metric values from the batch.
//  2. Adds values to all applicable tumbling windows.
//  3. Updates Top-N trackers for each process in the batch.
//  4. Checks for windows that have closed (window end + grace period elapsed).
//  5. Aggregates closed windows into AggregatedMetric results.
//  6. Collects any Top-N ranking changes.
//
// Returns aggregated metrics from closed windows, Top-N changes, and any error.
// An empty (nil, nil, nil) result is valid — it means no windows closed during
// this call.
func (a *Aggregator) Process(ctx context.Context, batch *models.MetricBatch, timestamp time.Time) ([]models.AggregatedMetric, []TopNResult, error) {
	// Step 1: Extract all numeric metric values from the batch.
	values := a.extractMetricValues(batch)

	// Step 2: Add values to all applicable tumbling windows.
	for name, value := range values {
		if err := a.windows.AddValue(ctx, batch.AgentID, name, value, timestamp); err != nil {
			return nil, nil, fmt.Errorf("add value to window (agent=%s, metric=%s): %w", batch.AgentID, name, err)
		}
		a.valuesProc.Add(1)
	}

	// Step 3: Update Top-N trackers for each process.
	for _, proc := range batch.Processes {
		a.topn.Update("cpu.process", proc.Name, proc.AgentID, proc.CPUUsagePct, proc.Timestamp)
		a.topn.Update("memory.process", proc.Name, proc.AgentID, float64(proc.MemoryBytes), proc.Timestamp)
	}

	// Step 4: Check for closed windows.
	closedWindows := a.windows.GetClosedWindows(ctx)

	// Step 5: Aggregate closed windows into AggregatedMetric results.
	var allMetrics []models.AggregatedMetric
	for i := range closedWindows {
		metrics := a.aggregateWindow(&closedWindows[i])
		allMetrics = append(allMetrics, metrics...)
	}

	// Purge closed windows from memory after aggregation.
	a.windows.PurgeClosed()

	// Step 6: Collect Top-N ranking changes.
	topnChanges := a.collectTopNChanges()

	a.logger.Debug("processed metric batch",
		zap.String("agent_id", batch.AgentID),
		zap.Int("extracted_values", len(values)),
		zap.Int("closed_windows", len(closedWindows)),
		zap.Int("aggregated_metrics", len(allMetrics)),
		zap.Int("topn_changes", len(topnChanges)),
	)

	return allMetrics, topnChanges, nil
}

// aggregateWindow computes all aggregation types for a single closed window.
// For each closed window it produces one AggregatedMetric per aggregation type:
// avg, min, max, p50, p90, p99, count, and sum.
//
// Returns nil if the window contains no values.
func (a *Aggregator) aggregateWindow(window *ClosedWindow) []models.AggregatedMetric {
	if len(window.Values) == 0 {
		return nil
	}

	type aggEntry struct {
		aggType models.AggregationType
		value   float64
	}

	entries := []aggEntry{
		{models.AggregationAvg, calcAvg(window.Values)},
		{models.AggregationMin, calcMin(window.Values)},
		{models.AggregationMax, calcMax(window.Values)},
		{models.AggregationP50, calcPercentile(window.Values, 50)},
		{models.AggregationP90, calcPercentile(window.Values, 90)},
		{models.AggregationP99, calcPercentile(window.Values, 99)},
		{models.AggregationCount, float64(len(window.Values))},
		{models.AggregationSum, calcSum(window.Values)},
	}

	metrics := make([]models.AggregatedMetric, 0, len(entries))
	for _, e := range entries {
		metrics = append(metrics, models.AggregatedMetric{
			AgentID:   window.Key.AgentID,
			Name:      window.Key.MetricName,
			Window:    window.Key.WindowSize,
			AggType:   e.aggType,
			Value:     e.value,
			Timestamp: window.LastSeen,
		})
	}

	return metrics
}

// extractMetricValues flattens a MetricBatch into a metric-name → value map.
// Each metric category contributes one or more named values:
//
//   - CPU:    cpu.usage_percent
//   - Memory: memory.usage_percent, memory.used_bytes, memory.available_bytes
//   - Disk:   disk.read_bytes_per_sec, disk.write_bytes_per_sec
//   - Network: network.rx_bytes_per_sec, network.tx_bytes_per_sec
//   - Process: process.cpu_percent, process.memory_bytes
//
// When a category has multiple entries, the last entry's value is retained
// in the flat map. Per-process tracking is handled separately by TopNTracker.
func (a *Aggregator) extractMetricValues(batch *models.MetricBatch) map[string]float64 {
	values := make(map[string]float64)

	// CPU metrics.
	for _, cpu := range batch.CPU {
		values["cpu.usage_percent"] = cpu.TotalUsagePct
	}

	// Memory metrics.
	for _, mem := range batch.Memory {
		values["memory.usage_percent"] = mem.UsagePercent
		values["memory.used_bytes"] = float64(mem.UsedBytes)
		values["memory.available_bytes"] = float64(mem.AvailableBytes)
	}

	// Disk metrics.
	for _, disk := range batch.Disk {
		values["disk.read_bytes_per_sec"] = float64(disk.ReadBytesPerSec)
		values["disk.write_bytes_per_sec"] = float64(disk.WriteBytesPerSec)
	}

	// Network metrics.
	for _, net := range batch.Network {
		values["network.rx_bytes_per_sec"] = float64(net.RxBytesPerSec)
		values["network.tx_bytes_per_sec"] = float64(net.TxBytesPerSec)
	}

	// Process metrics (last entry per name in flat map; per-process tracking via TopN).
	for _, proc := range batch.Processes {
		values["process.cpu_percent"] = proc.CPUUsagePct
		values["process.memory_bytes"] = float64(proc.MemoryBytes)
	}

	return values
}

// collectTopNChanges gathers all Top-N ranking changes since the last call
// to collectTopNChanges (via Process). Returns nil if no entries changed.
func (a *Aggregator) collectTopNChanges() []TopNResult {
	changed := a.topn.GetChanged()
	if len(changed) == 0 {
		return nil
	}

	var results []TopNResult
	for _, entries := range changed {
		results = append(results, entries...)
	}
	return results
}
