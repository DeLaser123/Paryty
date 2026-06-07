/**
 * SpeedControls — Speed selector for timeline replay.
 *
 * Provides 0.25x, 0.5x, 1x, 2x, 4x, 8x, 16x speed options.
 * Uses the button group pattern from the design system.
 *
 * @module components/timeline/SpeedControls
 */

import { memo } from 'react';
import clsx from 'clsx';
import { useTimelineStore } from '../../stores/timelineStore';
import type { TimelineSpeed } from '../../types/timeline';

/** Available speed options. */
const SPEEDS: TimelineSpeed[] = [0.25, 0.5, 1, 2, 4, 8, 16];

/**
 * Speed selector button group for timeline playback.
 *
 * Uses aef-btn-active/inactive pattern.
 * Integrates with timelineStore setSpeed.
 */
export const SpeedControls = memo(function SpeedControls() {
  const speed = useTimelineStore((s) => s.position.speed);
  const setSpeed = useTimelineStore((s) => s.setSpeed);

  return (
    <div className="speed-controls" data-testid="speed-controls">
      {SPEEDS.map((s) => (
        <button
          key={s}
          className={clsx(
            'aef-btn',
            speed === s ? 'aef-btn-active' : 'aef-btn-inactive',
          )}
          onClick={() => setSpeed(s)}
          data-testid={`speed-${s}`}
        >
          {s}x
        </button>
      ))}
    </div>
  );
});
