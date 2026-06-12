/**
 * SeekInput — Input field to jump to a specific timestamp in timeline replay.
 *
 * Allows users to enter a timestamp and jump directly to that point in time.
 * Supports ISO 8601 format and relative time expressions.
 *
 * @module components/timeline/SeekInput
 */

import { memo, useState, useCallback } from 'react';
import { Search } from 'lucide-react';
import { useTimelineStore } from '../../stores/timelineStore';

/**
 * Input field for seeking to a specific timestamp.
 *
 * Features:
 * - ISO 8601 timestamp input
 * - Relative time expressions (e.g., "-5m", "+1h")
 * - Validation and error display
 * - Integration with timelineStore seekToTimestamp
 */
export const SeekInput = memo(function SeekInput() {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const seekTo = useTimelineStore((s) => s.seekTo);
  const position = useTimelineStore((s) => s.position);
  const config = useTimelineStore((s) => s.config);
  
  // Convert ISO strings to timestamps for calculations
  const currentTimeMs = new Date(position.currentTime).getTime();
  const startTimeMs = new Date(config.startTime).getTime();
  const endTimeMs = new Date(config.endTime).getTime();

  const handleSeek = useCallback(() => {
    if (!value.trim()) {
      setError('Please enter a timestamp');
      return;
    }

    try {
      // Try parsing as ISO 8601
      const date = new Date(value);
      if (!isNaN(date.getTime())) {
        // Convert timestamp to progress (0-1)
        const totalDuration = endTimeMs - startTimeMs;
        if (totalDuration > 0) {
          const progress = Math.max(0, Math.min(1, (date.getTime() - startTimeMs) / totalDuration));
          seekTo(progress);
        }
        setError(null);
        return;
      }

      // Try parsing relative time (e.g., "-5m", "+1h")
      const relativeMatch = value.match(/^([+-])(\d+)([smhd])$/);
      if (relativeMatch) {
        const sign = relativeMatch[1] === '-' ? -1 : 1;
        const amount = parseInt(relativeMatch[2]);
        const unit = relativeMatch[3];

        let milliseconds = 0;

        switch (unit) {
          case 's':
            milliseconds = amount * 1000;
            break;
          case 'm':
            milliseconds = amount * 60 * 1000;
            break;
          case 'h':
            milliseconds = amount * 60 * 60 * 1000;
            break;
          case 'd':
            milliseconds = amount * 24 * 60 * 60 * 1000;
            break;
        }

        // Calculate new time relative to current time
        const targetTime = currentTimeMs + sign * milliseconds;
        const totalDuration = endTimeMs - startTimeMs;
        if (totalDuration > 0) {
          const progress = Math.max(0, Math.min(1, (targetTime - startTimeMs) / totalDuration));
          seekTo(progress);
        }
        setError(null);
        return;
      }

      setError('Invalid timestamp format. Use ISO 8601 or relative time (e.g., "-5m")');
    } catch (err) {
      setError('Failed to parse timestamp');
    }
  }, [value, seekTo, currentTimeMs, startTimeMs, endTimeMs]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') {
        handleSeek();
      }
    },
    [handleSeek],
  );

  return (
    <div className="seek-input" data-testid="seek-input">
      <div className="seek-input__field">
        <Search size={14} className="seek-input__icon" />
        <input
          type="text"
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            setError(null);
          }}
          onKeyDown={handleKeyDown}
          placeholder="Enter timestamp or relative time (e.g., -5m)"
          className="seek-input__input"
          aria-label="Seek to timestamp"
          data-testid="seek-input-field"
        />
        <button
          onClick={handleSeek}
          className="aef-btn aef-btn-inactive seek-input__button"
          data-testid="seek-input-button"
        >
          Go
        </button>
      </div>
      {error && (
        <div className="seek-input__error" data-testid="seek-input-error">
          {error}
        </div>
      )}
      <div className="seek-input__hints">
        <span className="aef-text-xs aef-text-secondary">
          Format: ISO 8601 or relative time (-5m, +1h, -1d)
        </span>
      </div>
    </div>
  );
});
