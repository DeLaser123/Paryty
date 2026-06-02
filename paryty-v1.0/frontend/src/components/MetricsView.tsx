import { useEffect, useRef } from 'react';
import { useMetrics } from '../hooks/useMetrics';
import { useMetricsStore } from '../stores/metricsStore';

export default function MetricsView() {
  const { selectedMetric, selectMetric, timeRange, setTimeRange } = useMetricsStore();
  const metrics = useMetrics(selectedMetric ?? undefined);
  const metricWorkerRef = useRef<Worker | null>(null);

  // Initialize metric processor worker
  useEffect(() => {
    metricWorkerRef.current = new Worker(
      new URL('../engine/workers/metricProcessor.worker.ts', import.meta.url),
      { type: 'module' },
    );
    metricWorkerRef.current.onmessage = (event) => {
      if (event.data.type === 'result') {
        // Aggregated results from worker - store for display
        const aggregated = event.data.data;
        if (aggregated && aggregated.length > 0) {
          useMetricsStore.getState().setAggregated(aggregated);
        }
      }
    };
    return () => metricWorkerRef.current?.terminate();
  }, []);

  // Send raw metrics to worker for aggregation when data changes
  useEffect(() => {
    if (metrics.series.length > 0 && metricWorkerRef.current) {
      const allPoints = metrics.series.flatMap((s) => s.points);
      metricWorkerRef.current.postMessage({
        type: 'aggregate',
        id: 'metrics-view',
        data: {
          metrics: allPoints.map((p) => ({
            timestamp: p.timestamp,
            value: p.value,
            labels: {},
          })),
          windowMs: 60000,
        },
      });
    }
  }, [metrics.series]);

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Metrics</h2>
        <div className="view-controls">
          <select
            value={selectedMetric ?? ''}
            onChange={(e) => selectMetric(e.target.value || null)}
          >
            <option value="">Select metric...</option>
            <option value="cpu_usage">CPU Usage</option>
            <option value="memory_usage">Memory Usage</option>
            <option value="disk_io">Disk I/O</option>
            <option value="network_io">Network I/O</option>
          </select>
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
