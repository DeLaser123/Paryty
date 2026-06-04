// Package processing implements the data processing pipeline.
// This file implements the downsampler, which periodically queries aggregated
// metrics from warm storage, groups them by agent/metric/window, computes
// higher-level aggregations (e.g. 1m → 5m), writes the results back, and
// deletes the source data once it exceeds its retention period.
package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// DownsamplingRule defines a retention and downsampling policy.
// SourceWindow metrics older than retention are aggregated into TargetWindow
// entries, and the original source data is deleted.
type DownsamplingRule struct {
	// SourceWindow is the window size of the source aggregated metrics (e.g. 1m).
	SourceWindow time.Duration

	// TargetWindow is the window size of the downsampled output (e.g. 5m).
	TargetWindow time.Duration

	// RetentionDays is how many days to retain sourceWindow data before
	// downsampling and deleting it.
	RetentionDays int
}

// DownsampleGroup groups aggregated metrics by agent, metric name, and
// window for a single downsampling pass.
type DownsampleGroup struct {
	AgentID    string
	MetricName string
	Window     time.Duration
	Values     []models.AggregatedMetric
}

// DownsamplerConfig holds configuration for the downsampler.
type DownsamplerConfig struct {
	// Rules is the list of downsampling rules to apply.
	Rules []DownsamplingRule

	// RunInterval is how often the downsampler runs. Default: 1 hour.
	RunInterval time.Duration
}

// QuestDBClient defines the interface for QuestDB operations needed by the
// downsampler. The warm.Client satisfies this interface.
type QuestDBClient interface {
	// QueryAggregated queries aggregated metrics from warm storage for a
	// given window, filtered by agent/metric name and time range.
	QueryAggregated(ctx context.Context, agentID string, metricName string, window time.Duration, start, end time.Time) ([]models.AggregatedMetric, error)

	// StoreAggregated stores aggregated metrics in warm storage via ILP.
	StoreAggregated(ctx context.Context, metrics []models.AggregatedMetric, tenant string) error

	// DeleteAggregated deletes aggregated metrics older than the cutoff
	// from warm storage.
	DeleteAggregated(ctx context.Context, window time.Duration, olderThan time.Time) error
}

// Downsampler periodically aggregates fine-grained metrics into coarser
// windows and enforces retention policies. It runs at a configurable
// interval and applies all configured downsampling rules.
//
// Thread-safe. All public methods are safe for concurrent use.
type Downsampler struct {
	config  DownsamplerConfig
	questdb QuestDBClient
	logger  *zap.Logger
}

// NewDownsampler creates a new Downsampler with the given configuration,
// QuestDB client, and logger.
//
// Parameters:
//   - config: downsampling configuration. If Rules is nil, DefaultDownsamplingRules is used.
//   - questdb: QuestDBClient for warm storage operations.
//   - logger: structured logger. If nil, a no-op logger is used.
func NewDownsampler(config DownsamplerConfig, questdb QuestDBClient, logger *zap.Logger) *Downsampler {
	if logger == nil {
		logger = zap.NewNop()
	}
	if len(config.Rules) == 0 {
		config.Rules = DefaultDownsamplingRules()
	}
	if config.RunInterval == 0 {
		config.RunInterval = time.Hour
	}
	return &Downsampler{
		config:  config,
		questdb: questdb,
		logger:  logger,
	}
}

// DefaultDownsamplingRules returns the standard set of downsampling rules:
//   - 1m → 5m (7 days retention)
//   - 5m → 1h (30 days retention)
//   - 1h → 1d (90 days retention)
func DefaultDownsamplingRules() []DownsamplingRule {
	return []DownsamplingRule{
		{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
		{SourceWindow: 5 * time.Minute, TargetWindow: time.Hour, RetentionDays: 30},
		{SourceWindow: time.Hour, TargetWindow: 24 * time.Hour, RetentionDays: 90},
	}
}

// Run starts the downsampler loop, executing Downsample at RunInterval
// until ctx is cancelled. The first execution happens immediately.
func (d *Downsampler) Run(ctx context.Context) {
	d.logger.Info("Downsampler started",
		zap.Duration("run_interval", d.config.RunInterval),
		zap.Int("rules", len(d.config.Rules)),
	)

	// Run immediately on startup.
	if err := d.Downsample(ctx); err != nil {
		d.logger.Error("Initial downsample failed", zap.Error(err))
	}

	ticker := time.NewTicker(d.config.RunInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("Downsampler stopping")
			return
		case <-ticker.C:
			if err := d.Downsample(ctx); err != nil {
				d.logger.Error("Downsample cycle failed", zap.Error(err))
			}
		}
	}
}

// Downsample executes all downsampling rules in sequence.
//
// For each rule:
//  1. Compute the retention cutoff: now - rule.RetentionDays.
//  2. Query aggregated metrics with sourceWindow older than the cutoff.
//  3. Group by (agentID, metricName).
//  4. For each group, compute targetWindow aggregation (avg of avg values).
//  5. Write the downsampled metrics to warm storage.
//  6. Delete the source data older than the cutoff.
//
// Returns the first error encountered; remaining rules are still attempted.
func (d *Downsampler) Downsample(ctx context.Context) error {
	var firstErr error

	for _, rule := range d.config.Rules {
		if err := d.downsampleRule(ctx, rule); err != nil {
			d.logger.Error("Rule downsample failed",
				zap.Duration("source_window", rule.SourceWindow),
				zap.Duration("target_window", rule.TargetWindow),
				zap.Error(err),
			)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr == nil {
		d.logger.Debug("Downsample cycle completed successfully")
	}

	return firstErr
}

// downsampleRule executes a single downsampling rule.
func (d *Downsampler) downsampleRule(ctx context.Context, rule DownsamplingRule) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("downsampler: context cancelled: %w", err)
	}

	cutoff := time.Now().Add(-time.Duration(rule.RetentionDays) * 24 * time.Hour)

	d.logger.Debug("Running downsample rule",
		zap.Duration("source_window", rule.SourceWindow),
		zap.Duration("target_window", rule.TargetWindow),
		zap.Int("retention_days", rule.RetentionDays),
		zap.Time("cutoff", cutoff),
	)

	// Step 1: Query aggregated metrics older than the cutoff.
	// We query without agent/metric filters to get all matching data.
	// The questdb interface requires agentID and metricName, so we
	// query with empty strings (wildcard) if the client supports it,
	// otherwise we use a direct SQL approach.
	groups, err := d.queryForDownsampling(ctx, rule.SourceWindow, cutoff)
	if err != nil {
		return fmt.Errorf("query for downsampling (source=%s): %w", rule.SourceWindow, err)
	}

	if len(groups) == 0 {
		d.logger.Debug("No data to downsample",
			zap.Duration("source_window", rule.SourceWindow),
		)
		return nil
	}

	// Step 2–4: For each group, compute downsampled metrics and write.
	downsampled := d.computeDownsampled(groups, rule.TargetWindow)
	if len(downsampled) > 0 {
		if err := d.questdb.StoreAggregated(ctx, downsampled, ""); err != nil {
			return fmt.Errorf("store downsampled metrics (target=%s): %w", rule.TargetWindow, err)
		}
		d.logger.Info("Stored downsampled metrics",
			zap.Duration("source_window", rule.SourceWindow),
			zap.Duration("target_window", rule.TargetWindow),
			zap.Int("groups", len(groups)),
			zap.Int("metrics_written", len(downsampled)),
		)
	}

	// Step 5: Delete source data older than the cutoff.
	if err := d.questdb.DeleteAggregated(ctx, rule.SourceWindow, cutoff); err != nil {
		return fmt.Errorf("delete old aggregated metrics (source=%s): %w", rule.SourceWindow, err)
	}

	return nil
}

// queryForDownsampling queries aggregated metrics for a given source window
// that are older than the cutoff time. Returns groups of metrics organized
// by (agentID, metricName).
func (d *Downsampler) queryForDownsampling(ctx context.Context, sourceWindow time.Duration, cutoff time.Time) ([]DownsampleGroup, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("downsampler: context cancelled: %w", err)
	}

	// Query a wide time range from epoch to cutoff to get all old data.
	start := time.Time{} // zero time = epoch
	end := cutoff

	// We need to discover all agentID+metricName combinations.
	// Use empty agentID/metricName as wildcards.
	metrics, err := d.questdb.QueryAggregated(ctx, "", "", sourceWindow, start, end)
	if err != nil {
		return nil, fmt.Errorf("query aggregated metrics: %w", err)
	}

	// Group by (agentID, metricName).
	type groupKey struct {
		agentID    string
		metricName string
	}

	groupMap := make(map[groupKey]*DownsampleGroup)
	for i := range metrics {
		m := metrics[i]
		key := groupKey{agentID: m.AgentID, metricName: m.Name}
		if _, ok := groupMap[key]; !ok {
			groupMap[key] = &DownsampleGroup{
				AgentID:    m.AgentID,
				MetricName: m.Name,
				Window:     m.Window,
			}
		}
		groupMap[key].Values = append(groupMap[key].Values, m)
	}

	groups := make([]DownsampleGroup, 0, len(groupMap))
	for _, g := range groupMap {
		groups = append(groups, *g)
	}

	return groups, nil
}

// computeDownsampled computes downsampled metrics from grouped source data.
// For each group, it computes avg across all source values per aggregation type,
// using the targetWindow duration and the latest timestamp.
func (d *Downsampler) computeDownsampled(groups []DownsampleGroup, targetWindow time.Duration) []models.AggregatedMetric {
	var result []models.AggregatedMetric

	for _, group := range groups {
		if len(group.Values) == 0 {
			continue
		}

		// Collect values by aggregation type.
		type aggEntry struct {
			aggType   models.AggregationType
			values    []float64
			lastTS    time.Time
			agentID   string
			metricName string
		}

		aggMap := make(map[models.AggregationType]*aggEntry)
		for _, m := range group.Values {
			if _, ok := aggMap[m.AggType]; !ok {
				aggMap[m.AggType] = &aggEntry{
					aggType:    m.AggType,
					agentID:    m.AgentID,
					metricName: m.Name,
				}
			}
			entry := aggMap[m.AggType]
			entry.values = append(entry.values, m.Value)
			if m.Timestamp.After(entry.lastTS) {
				entry.lastTS = m.Timestamp
			}
		}

		// For each aggregation type, compute the downsampled value.
		for _, entry := range aggMap {
			var value float64
			switch entry.aggType {
			case models.AggregationAvg, models.AggregationP50, models.AggregationP90, models.AggregationP99:
				value = calcAvg(entry.values)
			case models.AggregationMin:
				value = calcMin(entry.values)
			case models.AggregationMax:
				value = calcMax(entry.values)
			case models.AggregationSum:
				value = calcSum(entry.values)
			case models.AggregationCount:
				value = calcSum(entry.values)
			default:
				value = calcAvg(entry.values)
			}

			result = append(result, models.AggregatedMetric{
				AgentID:   entry.agentID,
				Name:      entry.metricName,
				Window:    targetWindow,
				AggType:   entry.aggType,
				Value:     value,
				Timestamp: entry.lastTS,
			})
		}
	}

	return result
}
