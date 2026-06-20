/**
 * ForecastCards — Summary cards for key metric forecasts.
 *
 * Displays metric icon, current value, predicted trend with confidence,
 * and a sparkline mini-chart using white line + opacity area fill.
 *
 * @module components/intel/ForecastCards
 */

import { memo, useRef, useEffect } from 'react';
import { TrendingUp, TrendingDown, Minus, Cpu, HardDrive, Activity, Network } from 'lucide-react';
import type { ForecastSeries, KeyMetric } from '../../types/intel';
import { KEY_METRIC_LABELS } from '../../types/intel';
import { DetachableCard } from '../common/DetachableCard';

// ─── Per-metric icons (lucide-react, design system compliant) ────

const METRIC_ICONS: Record<KeyMetric, React.ReactNode> = {
  cpu_usage_percent: <Cpu size={16} />,
  memory_usage_percent: <HardDrive size={16} />,
  disk_usage_percent: <Activity size={16} />,
  network_io_bytes: <Network size={16} />,
};

// ─── Sparkline ───────────────────────────────────────────────────

interface SparklineProps {
  points: number[];
  upper: number[];
  lower: number[];
  width?: number;
  height?: number;
}

/**
 * Canvas-based sparkline with white line + opacity area fill.
 * Upper/lower bounds rendered as a subtle confidence band.
 */
const Sparkline = memo(function Sparkline({
  points,
  upper,
  lower,
  width = 120,
  height = 36,
}: SparklineProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || points.length < 2) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, width, height);

    const allVals = [...points, ...upper, ...lower];
    const min = Math.min(...allVals);
    const max = Math.max(...allVals);
    const range = max - min || 1;
    const stepX = width / (points.length - 1);
    const padY = 2;

    const mapY = (v: number) => height - padY - ((v - min) / range) * (height - padY * 2);

    // Confidence band (upper → lower fill)
    ctx.beginPath();
    for (let i = 0; i < upper.length; i++) {
      const x = i * stepX;
      const y = mapY(upper[i]);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    for (let i = lower.length - 1; i >= 0; i--) {
      ctx.lineTo(i * stepX, mapY(lower[i]));
    }
    ctx.closePath();
    ctx.fillStyle = 'rgba(255, 255, 255, 0.06)';
    ctx.fill();

    // Area fill under the main line
    ctx.beginPath();
    ctx.moveTo(0, height);
    for (let i = 0; i < points.length; i++) {
      ctx.lineTo(i * stepX, mapY(points[i]));
    }
    ctx.lineTo(width, height);
    ctx.closePath();
    const gradient = ctx.createLinearGradient(0, 0, 0, height);
    gradient.addColorStop(0, 'rgba(255, 255, 255, 0.18)');
    gradient.addColorStop(1, 'rgba(255, 255, 255, 0.01)');
    ctx.fillStyle = gradient;
    ctx.fill();

    // Main line — white
    ctx.beginPath();
    for (let i = 0; i < points.length; i++) {
      const x = i * stepX;
      const y = mapY(points[i]);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.85)';
    ctx.lineWidth = 1.5;
    ctx.stroke();
  }, [points, upper, lower, width, height]);

  return (
    <canvas
      ref={canvasRef}
      style={{ width: '100%', height }}
      aria-hidden="true"
    />
  );
});

// ─── Trend Color ─────────────────────────────────────────────────

function getTrendColor(value: number): string {
  if (value >= 90) return 'var(--aef-node-critical)';
  if (value >= 70) return 'var(--aef-status-warning)';
  return 'var(--aef-status-live)';
}

// ─── Single Card ─────────────────────────────────────────────────

interface ForecastCardProps {
  metricName: string;
  series: ForecastSeries | undefined;
}

/** Derive a human-readable label from a metric name. */
function metricLabel(name: string): string {
  const known = KEY_METRIC_LABELS[name as KeyMetric];
  if (known) return known;
  // Fallback: snake_case → Title Case
  return name.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

const ForecastCard = memo(function ForecastCard({
  metricName,
  series,
}: ForecastCardProps) {
  const label = metricLabel(metricName);
  const icon = METRIC_ICONS[metricName as KeyMetric] ?? <Activity size={16} />;

  if (!series || series.points.length === 0) {
    return (
      <DetachableCard
        title={label}
        icon={icon}
        testId={`forecast-card-${metricName}`}
      >
        <div className="aef-counter aef-counter-neutral">
          <div className="aef-counter__body">
            <span className="aef-counter__label">No data</span>
            <span className="aef-counter__value">—</span>
          </div>
        </div>
      </DetachableCard>
    );
  }

  const firstPoint = series.points[0];
  const lastPoint = series.points[series.points.length - 1];
  const currentValue = firstPoint.value;
  const predictedValue = lastPoint.value;
  const trendColor = getTrendColor(predictedValue);
  const unit = metricName !== 'network_io_bytes' ? '%' : '';

  // Trend direction
  const delta = predictedValue - currentValue;
  const trendIcon =
    Math.abs(delta) < 0.5 ? (
      <Minus size={14} />
    ) : delta > 0 ? (
      <TrendingUp size={14} />
    ) : (
      <TrendingDown size={14} />
    );

  // Sparkline data
  const sparkValues = series.points.map((p) => p.value);
  const sparkUpper = series.points.map((p) => p.upperBound);
  const sparkLower = series.points.map((p) => p.lowerBound);

  return (
    <DetachableCard
      title={label}
      icon={icon}
      headerAction={
        <span className="forecast-card__trend" style={{ color: trendColor }}>
          {trendIcon}
        </span>
      }
      testId={`forecast-card-${metricName}`}
    >
      <div className="aef-counter aef-counter-neutral">
        <div className="aef-counter__body">
          <span className="aef-counter__label">Current</span>
          <span className="aef-counter__value">{Math.round(currentValue)}{unit}</span>
        </div>
      </div>
      <div className="forecast-card__sparkline">
        <Sparkline points={sparkValues} upper={sparkUpper} lower={sparkLower} />
      </div>
    </DetachableCard>
  );
});

// ─── Card Grid ───────────────────────────────────────────────────

interface ForecastCardsProps {
  forecasts: Map<string, ForecastSeries>;
  metricNames: string[];
}

/**
 * Grid of forecast summary cards for dynamically discovered metrics.
 */
export const ForecastCards = memo(function ForecastCards({
  forecasts,
  metricNames,
}: ForecastCardsProps) {
  return (
    <div className="forecast-cards" data-testid="forecast-cards">
      {metricNames.map((metric) => (
        <ForecastCard
          key={metric}
          metricName={metric}
          series={forecasts.get(metric)}
        />
      ))}
    </div>
  );
});
