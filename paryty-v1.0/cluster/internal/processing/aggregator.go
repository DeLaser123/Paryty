// Package processing implements the data processing pipeline.
// It includes aggregator, correlator, and enricher services.
package processing

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// Aggregator aggregates metrics over time windows.
type Aggregator struct {
	logger *zap.Logger
}

// NewAggregator creates a new aggregator.
func NewAggregator(logger *zap.Logger) *Aggregator {
	return &Aggregator{
		logger: logger,
	}
}

// Aggregate aggregates a batch of metrics over a time window.
func (a *Aggregator) Aggregate(ctx context.Context, batch *models.MetricBatch, window time.Duration) ([]models.AggregatedMetric, error) {
	var aggregated []models.AggregatedMetric

	// Aggregate CPU metrics
	if len(batch.CPU) > 0 {
		agg := a.aggregateCPU(batch.CPU, window)
		aggregated = append(aggregated, agg...)
	}

	// Aggregate memory metrics
	if len(batch.Memory) > 0 {
		agg := a.aggregateMemory(batch.Memory, window)
		aggregated = append(aggregated, agg...)
	}

	// Aggregate disk metrics
	if len(batch.Disk) > 0 {
		agg := a.aggregateDisk(batch.Disk, window)
		aggregated = append(aggregated, agg...)
	}

	// Aggregate network metrics
	if len(batch.Network) > 0 {
		agg := a.aggregateNetwork(batch.Network, window)
		aggregated = append(aggregated, agg...)
	}

	a.logger.Debug("Aggregated metrics",
		zap.String("agent_id", batch.AgentID),
		zap.Duration("window", window),
		zap.Int("count", len(aggregated)),
	)

	return aggregated, nil
}

func (a *Aggregator) aggregateCPU(metrics []models.CPUMetrics, window time.Duration) []models.AggregatedMetric {
	if len(metrics) == 0 {
		return nil
	}

	agentID := metrics[0].AgentID
	values := make([]float64, len(metrics))
	for i, m := range metrics {
		values[i] = m.TotalUsagePct
	}

	return []models.AggregatedMetric{
		{
			AgentID:   agentID,
			Name:      "cpu.usage_percent",
			Window:    window,
			AggType:   models.AggregationAvg,
			Value:     avg(values),
			Timestamp: time.Now(),
		},
		{
			AgentID:   agentID,
			Name:      "cpu.usage_percent",
			Window:    window,
			AggType:   models.AggregationMax,
			Value:     max(values),
			Timestamp: time.Now(),
		},
		{
			AgentID:   agentID,
			Name:      "cpu.usage_percent",
			Window:    window,
			AggType:   models.AggregationMin,
			Value:     min(values),
			Timestamp: time.Now(),
		},
		{
			AgentID:   agentID,
			Name:      "cpu.usage_percent",
			Window:    window,
			AggType:   models.AggregationP50,
			Value:     percentile(values, 50),
			Timestamp: time.Now(),
		},
		{
			AgentID:   agentID,
			Name:      "cpu.usage_percent",
			Window:    window,
			AggType:   models.AggregationP90,
			Value:     percentile(values, 90),
			Timestamp: time.Now(),
		},
		{
			AgentID:   agentID,
			Name:      "cpu.usage_percent",
			Window:    window,
			AggType:   models.AggregationP99,
			Value:     percentile(values, 99),
			Timestamp: time.Now(),
		},
	}
}

func (a *Aggregator) aggregateMemory(metrics []models.MemoryMetrics, window time.Duration) []models.AggregatedMetric {
	if len(metrics) == 0 {
		return nil
	}

	agentID := metrics[0].AgentID
	values := make([]float64, len(metrics))
	for i, m := range metrics {
		if m.TotalBytes > 0 {
			values[i] = float64(m.UsedBytes) / float64(m.TotalBytes) * 100
		}
	}

	return []models.AggregatedMetric{
		{
			AgentID:   agentID,
			Name:      "memory.usage_percent",
			Window:    window,
			AggType:   models.AggregationAvg,
			Value:     avg(values),
			Timestamp: time.Now(),
		},
	}
}

func (a *Aggregator) aggregateDisk(metrics []models.DiskMetrics, window time.Duration) []models.AggregatedMetric {
	if len(metrics) == 0 {
		return nil
	}

	agentID := metrics[0].AgentID
	readValues := make([]float64, len(metrics))
	writeValues := make([]float64, len(metrics))
	for i, m := range metrics {
		readValues[i] = float64(m.ReadBytesPerSec)
		writeValues[i] = float64(m.WriteBytesPerSec)
	}

	return []models.AggregatedMetric{
		{
			AgentID:   agentID,
			Name:      "disk.read_bytes_per_sec",
			Window:    window,
			AggType:   models.AggregationAvg,
			Value:     avg(readValues),
			Timestamp: time.Now(),
		},
		{
			AgentID:   agentID,
			Name:      "disk.write_bytes_per_sec",
			Window:    window,
			AggType:   models.AggregationAvg,
			Value:     avg(writeValues),
			Timestamp: time.Now(),
		},
	}
}

func (a *Aggregator) aggregateNetwork(metrics []models.NetworkMetrics, window time.Duration) []models.AggregatedMetric {
	if len(metrics) == 0 {
		return nil
	}

	agentID := metrics[0].AgentID
	rxValues := make([]float64, len(metrics))
	txValues := make([]float64, len(metrics))
	for i, m := range metrics {
		rxValues[i] = float64(m.RxBytesPerSec)
		txValues[i] = float64(m.TxBytesPerSec)
	}

	return []models.AggregatedMetric{
		{
			AgentID:   agentID,
			Name:      "network.rx_bytes_per_sec",
			Window:    window,
			AggType:   models.AggregationAvg,
			Value:     avg(rxValues),
			Timestamp: time.Now(),
		},
		{
			AgentID:   agentID,
			Name:      "network.tx_bytes_per_sec",
			Window:    window,
			AggType:   models.AggregationAvg,
			Value:     avg(txValues),
			Timestamp: time.Now(),
		},
	}
}

// Helper functions for aggregation

func avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func min(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	index := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}

	fraction := index - float64(lower)
	return sorted[lower]*(1-fraction) + sorted[upper]*fraction
}
