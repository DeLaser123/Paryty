/**
 * ForecastCards — Summary cards for key metric forecasts.
 *
 * Displays current value, 7-day predicted trend, confidence %,
 * and a sparkline mini-chart for each key metric.
 *
 * @module components/intel/ForecastCards
 */

import { memo, useRef, useEffect } from 'react';
import { TrendingUp, TrendingDown, Minus } from 'lucide-react';
import type { ForecastSeries, KeyMetric } from '../../types/intel';
import { KEY_METRICS, KEY_METRIC_LABELS } from '../../types/intel';

// ─── Sparkline ───────────────────────────────────────────────────

interface SparklineProps {
  points: number[];
  color: string;
  width?: number;
  height?: number;
}

/**
 * Tiny canvas-based sparkline rendered with zero dependencies.
 * Uses requestAnimationFrame-safe synchronous drawing.
 */
const Sparkline = memo(function Sparkline({
  points,
  color,
  width = 80,
  height = 28,
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

    const min = Math.min(...points);
    const max = Math.max(...points);
    const range = max - min || 1;
    const stepX = width / (points.length - 1);

    // Draw filled area
    ctx.beginPath();
    ctx.moveTo(0, height);
    for (let i = 0; i < points.length; i++) {
      const x = i * stepX;
      const y = height - ((points[i] - min) / range) * (height - 4) - 2;
      ctx.lineTo(x, y);
    }
    ctx.lineTo(width, height);
    ctx.closePath();
    ctx.fillStyle = `${color}20`;
    ctx.fill();

    // Draw line
    ctx.beginPath();
    for (let i = 0; i < points.length; i++) {
      const x = i * stepX;
      const y = height - ((points[i] - min) / range) * (height - 4) - 2;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.5;
    ctx.stroke();
  }, [points, color, width, height]);

  return (
    <canvas
      ref={canvasRef}
      style={{ width, height }}
      aria-hidden="true"
    />
  );
});

// ─── Card Status Color ───────────────────────────────────────────

function getStatusColor(value: number): string {
  if (value >= 90) return 'var(--aef-node-critical)';
  if (value >= 70) return 'var(--aef-status-warning)';
  return 'var(--aef-status-live)';
}

function getStatusClass(value: number): string {
  if (value >= 90) return 'forecast-card--critical';
  if (value >= 70) return 'forecast-card--warning';
  return 'forecast-card--healthy';
}

// ─── Single Card ─────────────────────────────────────────────────

interface ForecastCardProps {
  metricName: KeyMetric;
  series: ForecastSeries | undefined;
}

const ForecastCard = memo(function ForecastCard({
  metricName,
  series,
}: ForecastCardProps) {
  const label = KEY_METRIC_LABELS[metricName];

  if (!series || series.points.length === 0) {
    return (
      <div className="forecast-card forecast-card--empty" data-testid={`forecast-card-${metricName}`}>
        <div className="forecast-card__header">
          <span className="forecast-card__label">{label}</span>
        </div>
        <div className="forecast-card__empty-msg">No forecast data</div>
      </div>
    );
  }

  const firstPoint = series.points[0];
  const lastPoint = series.points[series.points.length - 1];
  const currentValue = firstPoint.value;
  const predictedValue = lastPoint.value;
  const confidence = Math.round(series.overallConfidence * 100);
  const statusClass = getStatusClass(predictedValue);
  const statusColor = getStatusColor(predictedValue);

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

  // Sparkline data — combine value + bounds for a richer mini-chart
  const sparkValues = series.points.map((p) => p.value);
  const sparkUpper = series.points.map((p) => p.upperBound);
  const sparkLower = series.points.map((p) => p.lowerBound);

  // Predicted range text
  const rangeLow = Math.round(lastPoint.lowerBound);
  const rangeHigh = Math.round(lastPoint.upperBound);

  return (
    <div
      className={`forecast-card ${statusClass}`}
      data-testid={`forecast-card-${metricName}`}
    >
      <div className="forecast-card__header">
        <span className="forecast-card__label">{label}</span>
        <span className="forecast-card__confidence" style={{ color: statusColor }}>
          {confidence}%
        </span>
      </div>

      <div className="forecast-card__value-row">
        <span className="forecast-card__current" style={{ color: statusColor }}>
          {Math.round(currentValue)}
          {metricName !== 'network_io_bytes' ? '%' : ''}
        </span>
        <span className="forecast-card__trend" style={{ color: statusColor }}>
          {trendIcon}
        </span>
      </div>

      <div className="forecast-card__prediction">
        <span className="forecast-card__predicted-label">Predicted</span>
        <span className="forecast-card__predicted-value">
          {Math.round(predictedValue)}
          {metricName !== 'network_io_bytes' ? '%' : ''}{' '}
          <span className="forecast-card__range">
            ({rangeLow}–{rangeHigh})
          </span>
        </span>
      </div>

      <div className="forecast-card__sparkline">
        <Sparkline points={sparkValues} color={statusColor} />
        <Sparkline points={sparkUpper} color="rgba(255,255,255,0.1)" />
        <Sparkline points={sparkLower} color="rgba(255,255,255,0.1)" />
      </div>

      <div className="forecast-card__footer">
        <span className="forecast-card__model">
          Best: {series.modelInfo.bestModel}
        </span>
      </div>
    </div>
  );
});

// ─── Card Grid ───────────────────────────────────────────────────

interface ForecastCardsProps {
  forecasts: Map<string, ForecastSeries>;
}

/**
 * Grid of forecast summary cards for all key metrics.
 */
export const ForecastCards = memo(function ForecastCards({
  forecasts,
}: ForecastCardsProps) {
  return (
    <div className="forecast-cards" data-testid="forecast-cards">
      {KEY_METRICS.map((metric) => (
        <ForecastCard
          key={metric}
          metricName={metric}
          series={forecasts.get(metric)}
        />
      ))}
    </div>
  );
});
