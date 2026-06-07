/**
 * ForecastChart — Detailed forecast visualization using Canvas API.
 *
 * Renders historical data as a solid line, forecast as a dashed line,
 * and confidence intervals as a shaded band. No external chart library.
 *
 * @module components/intel/ForecastChart
 */

import { memo, useRef, useEffect, useCallback, useState } from 'react';
import type { ForecastSeries } from '../../types/intel';
import { KEY_METRICS, KEY_METRIC_LABELS } from '../../types/intel';

// ─── Chart Constants ─────────────────────────────────────────────

const PADDING = { top: 24, right: 24, bottom: 40, left: 56 };
const GRID_COLOR = 'rgba(255, 255, 255, 0.06)';
const AXIS_COLOR = 'rgba(255, 255, 255, 0.15)';
const LABEL_COLOR = 'rgba(255, 255, 255, 0.4)';
const HISTORICAL_COLOR = '#4ade80';
const FORECAST_COLOR = '#60a5fa';
const CONFIDENCE_COLOR = 'rgba(96, 165, 250, 0.12)';
const CONFIDENCE_BORDER_COLOR = 'rgba(96, 165, 250, 0.25)';

// ─── Helpers ─────────────────────────────────────────────────────

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()} ${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
}

function formatValue(v: number): string {
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(1)}K`;
  return v.toFixed(1);
}

// ─── Tooltip State ───────────────────────────────────────────────

interface TooltipData {
  x: number;
  y: number;
  timestamp: string;
  value: number;
  lower: number;
  upper: number;
  isForecast: boolean;
}

// ─── Component ───────────────────────────────────────────────────

interface ForecastChartProps {
  series: ForecastSeries | undefined;
  selectedMetric: string;
  onMetricChange: (metric: string) => void;
}

/**
 * Canvas-based forecast chart with confidence bands.
 * Renders historical data (solid) + forecast (dashed) + CI (shaded).
 */
export const ForecastChart = memo(function ForecastChart({
  series,
  selectedMetric,
  onMetricChange,
}: ForecastChartProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [tooltip, setTooltip] = useState<TooltipData | null>(null);
  const [dimensions, setDimensions] = useState({ width: 800, height: 360 });
  const rafRef = useRef<number>(0);

  // Resize observer for responsive canvas
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) {
        const { width, height } = entry.contentRect;
        setDimensions({
          width: Math.max(400, Math.floor(width)),
          height: Math.max(240, Math.floor(height)),
        });
      }
    });

    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  // Canvas rendering
  const draw = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    const { width, height } = dimensions;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.scale(dpr, dpr);

    // Clear
    ctx.clearRect(0, 0, width, height);

    // Chart area
    const chartX = PADDING.left;
    const chartY = PADDING.top;
    const chartW = width - PADDING.left - PADDING.right;
    const chartH = height - PADDING.top - PADDING.bottom;

    if (!series || series.points.length === 0) {
      // Empty state
      ctx.fillStyle = LABEL_COLOR;
      ctx.font = '12px "Geist Mono", monospace';
      ctx.textAlign = 'center';
      ctx.fillText('No forecast data available', width / 2, height / 2);
      return;
    }

    const points = series.points;
    const allValues = points.flatMap((p) => [p.value, p.lowerBound, p.upperBound]);
    const minVal = Math.min(...allValues) * 0.9;
    const maxVal = Math.max(...allValues) * 1.1;
    const valRange = maxVal - minVal || 1;

    const minTime = new Date(points[0].timestamp).getTime();
    const maxTime = new Date(points[points.length - 1].timestamp).getTime();
    const timeRange = maxTime - minTime || 1;

    // Map data to canvas coordinates
    const mapX = (timestamp: string): number =>
      chartX + ((new Date(timestamp).getTime() - minTime) / timeRange) * chartW;

    const mapY = (value: number): number =>
      chartY + chartH - ((value - minVal) / valRange) * chartH;

    // ─── Grid Lines ──────────────────────────────────────────────

    const yTicks = 5;
    ctx.strokeStyle = GRID_COLOR;
    ctx.lineWidth = 1;
    ctx.fillStyle = LABEL_COLOR;
    ctx.font = '10px "Geist Mono", monospace';
    ctx.textAlign = 'right';

    for (let i = 0; i <= yTicks; i++) {
      const y = chartY + (chartH / yTicks) * i;
      const val = maxVal - (valRange / yTicks) * i;
      ctx.beginPath();
      ctx.moveTo(chartX, y);
      ctx.lineTo(chartX + chartW, y);
      ctx.stroke();
      ctx.fillText(formatValue(val), chartX - 8, y + 3);
    }

    // X-axis labels
    const xTicks = Math.min(8, points.length);
    ctx.textAlign = 'center';
    for (let i = 0; i < xTicks; i++) {
      const idx = Math.floor((i / (xTicks - 1)) * (points.length - 1));
      const p = points[idx];
      if (!p) continue;
      const x = mapX(p.timestamp);

      ctx.strokeStyle = GRID_COLOR;
      ctx.beginPath();
      ctx.moveTo(x, chartY);
      ctx.lineTo(x, chartY + chartH);
      ctx.stroke();

      ctx.fillStyle = LABEL_COLOR;
      ctx.fillText(formatTimestamp(p.timestamp), x, chartY + chartH + 16);
    }

    // ─── Confidence Band ─────────────────────────────────────────

    ctx.beginPath();
    for (let i = 0; i < points.length; i++) {
      const x = mapX(points[i].timestamp);
      const y = mapY(points[i].upperBound);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    for (let i = points.length - 1; i >= 0; i--) {
      const x = mapX(points[i].timestamp);
      const y = mapY(points[i].lowerBound);
      ctx.lineTo(x, y);
    }
    ctx.closePath();
    ctx.fillStyle = CONFIDENCE_COLOR;
    ctx.fill();

    // Confidence border (dashed)
    ctx.setLineDash([4, 4]);
    ctx.strokeStyle = CONFIDENCE_BORDER_COLOR;
    ctx.lineWidth = 1;

    ctx.beginPath();
    for (let i = 0; i < points.length; i++) {
      const x = mapX(points[i].timestamp);
      const y = mapY(points[i].upperBound);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.stroke();

    ctx.beginPath();
    for (let i = 0; i < points.length; i++) {
      const x = mapX(points[i].timestamp);
      const y = mapY(points[i].lowerBound);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.stroke();
    ctx.setLineDash([]);

    // ─── Historical Line (solid) ─────────────────────────────────

    // Assume first half is historical, second half is forecast
    const splitIdx = Math.floor(points.length / 2);

    ctx.strokeStyle = HISTORICAL_COLOR;
    ctx.lineWidth = 2;
    ctx.beginPath();
    for (let i = 0; i <= splitIdx; i++) {
      const x = mapX(points[i].timestamp);
      const y = mapY(points[i].value);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.stroke();

    // ─── Forecast Line (dashed) ──────────────────────────────────

    ctx.strokeStyle = FORECAST_COLOR;
    ctx.lineWidth = 2;
    ctx.setLineDash([6, 4]);
    ctx.beginPath();
    for (let i = splitIdx; i < points.length; i++) {
      const x = mapX(points[i].timestamp);
      const y = mapY(points[i].value);
      if (i === splitIdx) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.stroke();
    ctx.setLineDash([]);

    // ─── Transition Marker ───────────────────────────────────────

    const splitX = mapX(points[splitIdx].timestamp);
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.2)';
    ctx.lineWidth = 1;
    ctx.setLineDash([2, 3]);
    ctx.beginPath();
    ctx.moveTo(splitX, chartY);
    ctx.lineTo(splitX, chartY + chartH);
    ctx.stroke();
    ctx.setLineDash([]);

    ctx.fillStyle = LABEL_COLOR;
    ctx.font = '9px "Geist Mono", monospace';
    ctx.textAlign = 'center';
    ctx.fillText('← Historical | Forecast →', splitX, chartY - 8);

    // ─── Axes ────────────────────────────────────────────────────

    ctx.strokeStyle = AXIS_COLOR;
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(chartX, chartY);
    ctx.lineTo(chartX, chartY + chartH);
    ctx.lineTo(chartX + chartW, chartY + chartH);
    ctx.stroke();
  }, [series, dimensions]);

  // Render on data or size change
  useEffect(() => {
    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(rafRef.current);
  }, [draw]);

  // ─── Mouse Hover → Tooltip ─────────────────────────────────────

  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLCanvasElement>) => {
      if (!series || series.points.length === 0) {
        setTooltip(null);
        return;
      }

      const canvas = canvasRef.current;
      if (!canvas) return;

      const rect = canvas.getBoundingClientRect();
      const mouseX = e.clientX - rect.left;

      const chartX = PADDING.left;
      const chartW = dimensions.width - PADDING.left - PADDING.right;

      // Find nearest point
      const relX = (mouseX - chartX) / chartW;
      const idx = Math.round(relX * (series.points.length - 1));
      const clampedIdx = Math.max(0, Math.min(series.points.length - 1, idx));
      const point = series.points[clampedIdx];

      if (!point) {
        setTooltip(null);
        return;
      }

      const splitIdx = Math.floor(series.points.length / 2);
      const isForecast = clampedIdx > splitIdx;

      const allValues = series.points.flatMap((p) => [p.value, p.lowerBound, p.upperBound]);
      const minVal = Math.min(...allValues) * 0.9;
      const maxVal = Math.max(...allValues) * 1.1;
      const valRange = maxVal - minVal || 1;
      const chartH = dimensions.height - PADDING.top - PADDING.bottom;
      const pointY = PADDING.top + chartH - ((point.value - minVal) / valRange) * chartH;

      setTooltip({
        x: mouseX,
        y: pointY,
        timestamp: point.timestamp,
        value: point.value,
        lower: point.lowerBound,
        upper: point.upperBound,
        isForecast,
      });
    },
    [series, dimensions],
  );

  const handleMouseLeave = useCallback(() => {
    setTooltip(null);
  }, []);

  return (
    <div className="forecast-chart" data-testid="forecast-chart">
      <div className="forecast-chart__header">
        <h3 className="forecast-chart__title">Forecast Detail</h3>
        <select
          className="forecast-chart__selector"
          value={selectedMetric}
          onChange={(e) => onMetricChange(e.target.value)}
          data-testid="forecast-metric-selector"
        >
          {KEY_METRICS.map((m) => (
            <option key={m} value={m}>
              {KEY_METRIC_LABELS[m]}
            </option>
          ))}
        </select>
      </div>

      <div className="forecast-chart__canvas-wrapper" ref={containerRef}>
        <canvas
          ref={canvasRef}
          style={{ width: '100%', height: '100%' }}
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
          data-testid="forecast-canvas"
        />

        {tooltip && (
          <div
            className="forecast-chart__tooltip"
            style={{
              left: tooltip.x + 12,
              top: tooltip.y - 60,
            }}
            data-testid="forecast-tooltip"
          >
            <div className="forecast-chart__tooltip-time">
              {formatTimestamp(tooltip.timestamp)}
            </div>
            <div className="forecast-chart__tooltip-value">
              {tooltip.isForecast ? '🔮 Forecast' : '📊 Historical'}:{' '}
              {formatValue(tooltip.value)}
            </div>
            <div className="forecast-chart__tooltip-range">
              Range: {formatValue(tooltip.lower)} – {formatValue(tooltip.upper)}
            </div>
          </div>
        )}
      </div>

      <div className="forecast-chart__legend">
        <span className="forecast-chart__legend-item">
          <span className="forecast-chart__legend-swatch" style={{ background: HISTORICAL_COLOR }} />
          Historical
        </span>
        <span className="forecast-chart__legend-item">
          <span className="forecast-chart__legend-swatch" style={{ background: FORECAST_COLOR }} />
          Forecast
        </span>
        <span className="forecast-chart__legend-item">
          <span className="forecast-chart__legend-swatch" style={{ background: CONFIDENCE_COLOR }} />
          Confidence Interval
        </span>
      </div>
    </div>
  );
});
