import { useEffect } from 'react';
import { useMetrics } from '../hooks/useMetrics';
import { useMetricsStore } from '../stores/metricsStore';
import { getProcessingClient } from '../engine/processing/processingClient';
import { ParytySelect } from './common/ParytySelect';
import type { MetricSample } from '../engine/processing/metrics';
import type { AggregatedMetric } from '../types/metric';

export default function MetricsView() {
  const { selectedMetric, selectMetric, timeRange, setTimeRange } = useMetricsStore();
  const metrics = useMetrics(selectedMetric ?? undefined);

  // Aggregate raw metrics on the shared processing thread when data changes.
  useEffect(() => {
    if (metrics.series.length === 0) return;

    let cancelled = false;
    const samples: MetricSample[] = metrics.series
      .flatMap((s) => s.points)
      .map((p) => ({ timestamp: 0, value: p.value, labels: {} }));

    getProcessingClient()
      .aggregate(samples)
      .then((result) => {
        if (cancelled || result.length === 0) return;
        // Map the processing-thread statistics onto the store's metric shape.
        const aggregated: AggregatedMetric[] = result.map((m) => ({
          name: m.name,
          labels: m.labels,
          timestamp: new Date().toISOString(),
          value: m.avg,
          min: m.min,
          max: m.max,
          avg: m.avg,
          count: m.count,
          p50: m.p50,
          p90: m.p90,
          p99: m.p99,
        }));
        useMetricsStore.getState().setAggregated(aggregated);
      })
      .catch(() => {
        /* aggregation is best-effort; ignore failures */
      });

    return () => {
      cancelled = true;
    };
  }, [metrics.series]);

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Metrics</h2>
        <div className="view-controls">
          <ParytySelect
            options={[
              { label: 'Select metric...', value: '' },
              { label: 'CPU Usage', value: 'cpu_usage' },
              { label: 'Memory Usage', value: 'memory_usage' },
              { label: 'Disk I/O', value: 'disk_io' },
              { label: 'Network I/O', value: 'network_io' },
            ]}
            value={selectedMetric ?? ''}
            onChange={(val) => selectMetric(val || null)}
            placeholder="Select metric..."
          />
          <input
            type="datetime-local"
            value={timeRange.start.slice(0, 16)}
            onChange={(e) => setTimeRange({ ...timeRange, start: e.target.value })}
          />
          <input
            type="datetime-local"
            value={timeRange.end.slice(0, 16)}
            onChange={(e) => setTimeRange({ ...timeRange, end: e.target.value })}
          />
        </div>
      </div>
      <div className="metrics-content">
        {metrics.isLoading && <div className="loading">Loading metrics...</div>}
        {metrics.error && <div className="error">{metrics.error}</div>}
        {metrics.series.length === 0 && !metrics.isLoading && (
          <div className="empty-state">
            <p>Select a metric to view data</p>
          </div>
        )}
        {metrics.series.length > 0 && (
          <div className="metric-chart-placeholder">
            <h3>{selectedMetric}</h3>
            <p>{metrics.series.length} series, {metrics.series.reduce((a, s) => a + s.points.length, 0)} data points</p>
            {/* Chart will be rendered here with recharts or custom canvas */}
          </div>
        )}
      </div>
    </div>
  );
}
