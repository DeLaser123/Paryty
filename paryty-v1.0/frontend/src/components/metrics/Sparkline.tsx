/**
 * Sparkline — Inline mini chart for metric trends.
 *
 * Tiny Recharts AreaChart with no axes, no labels — just the line.
 * Monochromatic: white line, dark fill.
 *
 * @module components/metrics/Sparkline
 */

import { memo, useMemo } from 'react';
import { AreaChart, Area, ResponsiveContainer } from 'recharts';

/** Props for Sparkline. */
interface SparklineProps {
  /** Array of numeric values to plot. */
  data: number[];
  /** Width in pixels. Default: 40. */
  width?: number;
  /** Height in pixels. Default: 16. */
  height?: number;
}

/**
 * Inline mini chart for displaying metric trends.
 *
 * Renders a tiny area chart with no axes or labels.
 * Line: white. Fill: dark with low opacity.
 */
export const Sparkline = memo(function Sparkline({
  data,
  width = 40,
  height = 16,
}: SparklineProps) {
  const chartData = useMemo(
    () => data.map((value, i) => ({ i, value })),
    [data],
  );

  return (
    <div className="sparkline" data-testid="sparkline">
      <ResponsiveContainer width={width} height={height}>
        <AreaChart data={chartData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
          <Area
            type="monotone"
            dataKey="value"
            stroke="var(--aef-text-primary)"
            fill="var(--aef-surface-mid)"
            strokeWidth={1}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
});
