/**
 * TimeRangeSelector — Button group for time range selection.
 *
 * Provides 1h, 6h, 24h, 7d, 30d range options.
 * Uses the button group pattern (aef-btn-active/inactive).
 *
 * @module components/metrics/TimeRangeSelector
 */

import { memo, useCallback } from 'react';
import clsx from 'clsx';
import { useMetricsStore } from '../../stores/metricsStore';

/** Time range option. */
interface TimeRangeOption {
  /** Display label. */
  label: string;
  /** Duration in milliseconds. */
  ms: number;
}

/** Available time range options. */
const TIME_RANGES: TimeRangeOption[] = [
  { label: '1h',  ms: 3600_000 },
  { label: '6h',  ms: 21600_000 },
  { label: '24h', ms: 86400_000 },
  { label: '7d',  ms: 604800_000 },
  { label: '30d', ms: 2592000_000 },
];

/**
 * Button group for selecting metrics time range.
 *
 * Uses the design system button group pattern.
 * Integrates with metricsStore timeRange.
 */
export const TimeRangeSelector = memo(function TimeRangeSelector() {
  const timeRange = useMetricsStore((s) => s.timeRange);
  const setTimeRange = useMetricsStore((s) => s.setTimeRange);

  /** Current duration in ms. */
  const currentDuration = new Date(timeRange.end).getTime() - new Date(timeRange.start).getTime();

  const handleSelect = useCallback(
    (ms: number) => {
      const end = new Date().toISOString();
      const start = new Date(Date.now() - ms).toISOString();
      setTimeRange({ start, end });
    },
    [setTimeRange],
  );

  return (
    <div className="time-range-selector" data-testid="time-range-selector">
      {TIME_RANGES.map((range) => {
        const isActive = Math.abs(currentDuration - range.ms) < 60_000; // 1 min tolerance
        return (
          <button
            key={range.label}
            className={clsx(
              'aef-btn',
              isActive ? 'aef-btn-active' : 'aef-btn-inactive',
            )}
            onClick={() => handleSelect(range.ms)}
            data-testid={`time-range-${range.label}`}
          >
            {range.label}
          </button>
        );
      })}
    </div>
  );
});
