/**
 * TimelineScrubber — Horizontal timeline scrubber with playback controls.
 *
 * Provides a draggable track, snapshot markers, play/pause/stop buttons,
 * current time display, and speed indicator.
 *
 * @module components/timeline/TimelineScrubber
 */

import { memo, useCallback, useRef } from 'react';
import { Play, Pause, Square } from 'lucide-react';
import clsx from 'clsx';
import { useTimelineStore } from '../../stores/timelineStore';

/**
 * Horizontal timeline scrubber with playback controls.
 *
 * Uses the progress bar pattern (aef-progress-track/fill) for the track.
 */
export const TimelineScrubber = memo(function TimelineScrubber() {
  const position = useTimelineStore((s) => s.position);
  const config = useTimelineStore((s) => s.config);
  const snapshots = useTimelineStore((s) => s.snapshots);
  const play = useTimelineStore((s) => s.play);
  const pause = useTimelineStore((s) => s.pause);
  const stop = useTimelineStore((s) => s.stop);
  const seekTo = useTimelineStore((s) => s.seekTo);
  const trackRef = useRef<HTMLDivElement>(null);

  const isPlaying = position.state === 'playing';

  const handleTrackClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (!trackRef.current) return;
      const rect = trackRef.current.getBoundingClientRect();
      const progress = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
      seekTo(progress);
    },
    [seekTo],
  );

  const formatTime = useCallback((ts: string) => {
    try {
      return new Date(ts).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      });
    } catch {
      return ts;
    }
  }, []);

  return (
    <div className="timeline-scrubber" data-testid="timeline-scrubber">
      {/* Playback controls */}
      <div className="timeline-scrubber__controls">
        <button
          className={clsx('aef-btn', isPlaying ? 'aef-btn-active' : 'aef-btn-inactive')}
          onClick={isPlaying ? pause : play}
          aria-label={isPlaying ? 'Pause' : 'Play'}
          data-testid="timeline-play-pause"
        >
          {isPlaying ? <Pause size={14} /> : <Play size={14} />}
        </button>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={stop}
          aria-label="Stop"
          data-testid="timeline-stop"
        >
          <Square size={14} />
        </button>
      </div>

      {/* Track */}
      <div className="timeline-scrubber__track-area">
        <div
          ref={trackRef}
          className="aef-progress-track"
          onClick={handleTrackClick}
          role="slider"
          aria-label="Timeline position"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(position.progress * 100)}
          data-testid="timeline-track"
          style={{ cursor: 'pointer' }}
        >
          {/* Progress fill */}
          <div
            className="aef-progress-fill"
            style={{ width: `${position.progress * 100}%` }}
          />

          {/* Snapshot markers */}
          {snapshots.map((snap) => {
            const start = new Date(config.startTime).getTime();
            const end = new Date(config.endTime).getTime();
            const snapTime = new Date(snap.timestamp).getTime();
            const pct = end > start ? ((snapTime - start) / (end - start)) * 100 : 0;
            return (
              <div
                key={snap.id}
                className="timeline-scrubber__marker"
                style={{ left: `${pct}%` }}
                title={snap.timestamp}
              />
            );
          })}
        </div>
      </div>

      {/* Time + speed display */}
      <div className="timeline-scrubber__info">
        <span className="aef-text-sm aef-text-secondary">
          {formatTime(position.currentTime)}
        </span>
        <span className="aef-text-xs aef-text-secondary">
          {position.speed}x
        </span>
      </div>
    </div>
  );
});
