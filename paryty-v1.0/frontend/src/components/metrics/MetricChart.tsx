/**
 * MetricChart — Time-series chart using Recharts.
 *
 * Renders a responsive line chart for metric data with monochromatic
 * styling using design system tokens.
 *
 * @module components/metrics/MetricChart
 */

import { memo, useMemo } from 'react';
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
} from 'recharts';
import { useMetricsStore } from '../../stores/metricsStore';

/** Props for MetricChart. */
interface MetricChartProps {
  /** Name of the metric to display. */
  metricName: string;
  /** Optional agent ID to filter by. */
  agentId?: string;
  /** Chart height in pixels. Default: 300. */
  height?: number;
}

/**
 * Time-series line chart for metric visualization.
 *
 * Uses Recharts LineChart with monochromatic styling.
 * Grid color: `var(--aef-border)`. Line color: `var(--aef-text-primary)`.
 * No animation for real-time performance.
 */
export const MetricChart = memo(function MetricChart({
  metricName,
  height = 300,
}: MetricChartProps) {
  const series = useMetricsStore((s) => s.series);
  const isLoading = useMetricsStore((s) => s.isLoading);

  // Transform series data for Recharts
  const chartData = useMemo(() => {
    const targetSeries = series.filter((s) => s.name === metricName);
    if (targetSeries.length === 0) return [];

    // Merge all points, sorted by timestamp
    const allPoints = targetSeries
      .flatMap((s) => s.points)
      .sort(
        (a, b) =>
          new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime(),
      );

    return allPoints.map((p) => ({
      time: new Date(p.timestamp).getTime(),
      value: p.value,
    }));
  }, [series, metricName]);

  if (isLoading) {
    return (
      <div className="aef-viz-well" data-testid="metric-chart-loading">
        <span className="aef-viz-well__label">Loading…</span>
      </div>
    );
  }

  if (chartData.length === 0) {
    return (
      <div className="aef-viz-well" data-testid="metric-chart-empty">
        <span className="aef-viz-well__label">No data for {metricName}</span>
      </div>
    );
  }

  return (
    <div className="metric-chart" data-testid="metric-chart">
      <ResponsiveContainer width="100%" height={height}>
        <LineChart data={chartData} margin={{ top: 8, right: 16, bottom: 8, left: 16 }}>
          <CartesianGrid
            strokeDasharray="3 3"
            stroke="var(--aef-border)"
            vertical={false}
          />
          <XAxis
            dataKey="time"
            type="number"
            domain={['dataMin', 'dataMax']}
            tickFormatter={(ts: number) => new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            stroke="var(--aef-text-secondary)"
            tick={{ fontSize: 10 }}
            axisLine={{ stroke: 'var(--aef-border)' }}
            tickLine={false}
          />
          <YAxis
            stroke="var(--aef-text-secondary)"
            tick={{ fontSize: 10 }}
            axisLine={{ stroke: 'var(--aef-border)' }}
            tickLine={false}
            width={40}
          />
          <Tooltip
            contentStyle={{
              background: 'var(--aef-surface-card)',
              border: '1px solid var(--aef-border)',
              borderRadius: 'var(--aef-radius-control)',
              fontSize: 11,
              color: 'var(--aef-text-primary)',
            }}
            labelFormatter={(ts: number) => new Date(ts).toLocaleString()}
          />
          <Line
            type="monotone"
            dataKey="value"
            stroke="var(--aef-text-primary)"
            strokeWidth={1.5}
            dot={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
});
