/**
 * MetricCards — Summary metric cards row.
 *
 * Displays CPU, Memory, Disk, Network cards using the counter card pattern
 * from the design system. Each card has icon, label, current value, and trend.
 *
 * @module components/metrics/MetricCards
 */

import { memo, useMemo } from 'react';
import { Cpu, HardDrive, Wifi, MemoryStick } from 'lucide-react';
import clsx from 'clsx';
import { useMetricsStore } from '../../stores/metricsStore';
import { Sparkline } from './Sparkline';

/** Metric card configuration. */
interface MetricCardConfig {
  /** Display label. */
  label: string;
  /** Metric name in the store. */
  metricName: string;
  /** lucide-react icon. */
  icon: React.ReactNode;
}

/** Default metric cards configuration. */
const METRIC_CARDS: MetricCardConfig[] = [
  { label: 'CPU',     metricName: 'cpu_usage',    icon: <Cpu size={16} /> },
  { label: 'Memory',  metricName: 'memory_usage', icon: <MemoryStick size={16} /> },
  { label: 'Disk',    metricName: 'disk_io',      icon: <HardDrive size={16} /> },
  { label: 'Network', metricName: 'network_io',   icon: <Wifi size={16} /> },
];

/**
 * Get counter card variant based on value thresholds.
 * - neutral: < 60%
 * - active: 60-80%
 * - variant-b (critical): > 80%
 */
function getVariant(value: number): 'neutral' | 'active' | 'variant-b' {
  if (value > 80) return 'variant-b';
  if (value > 60) return 'active';
  return 'neutral';
}

/**
 * Row of summary metric cards using the counter card pattern.
 *
 * Shows CPU, Memory, Disk, Network with current values
 * and inline sparklines.
 */
export const MetricCards = memo(function MetricCards() {
  const series = useMetricsStore((s) => s.series);
  const selectMetric = useMetricsStore((s) => s.selectMetric);

  // Get latest value and sparkline data for each metric
  const metricData = useMemo(() => {
    return METRIC_CARDS.map((config) => {
      const metricSeries = series.filter((s) => s.name === config.metricName);
      const latestPoint = metricSeries.length > 0
        ? metricSeries[0].points[metricSeries[0].points.length - 1]
        : null;
      const sparkData = metricSeries.length > 0
        ? metricSeries[0].points.slice(-20).map((p) => p.value)
        : [];
      return {
        ...config,
        value: latestPoint?.value ?? 0,
        sparkData,
      };
    });
  }, [series]);

  return (
    <div className="metric-cards-row" data-testid="metric-cards">
      {metricData.map((card) => {
        const variant = getVariant(card.value);
        return (
          <button
            key={card.metricName}
            className={clsx('aef-counter', `aef-counter-${variant}`)}
            onClick={() => selectMetric(card.metricName)}
            data-testid={`metric-card-${card.metricName}`}
          >
            <div className="aef-counter__icon">{card.icon}</div>
            <div className="aef-counter__body">
              <span className="aef-counter__label">{card.label}</span>
              <span className="aef-counter__value">
                {card.value > 0 ? `${card.value.toFixed(1)}%` : '—'}
              </span>
            </div>
            {card.sparkData.length > 1 && (
              <Sparkline data={card.sparkData} />
            )}
          </button>
        );
      })}
    </div>
  );
});
