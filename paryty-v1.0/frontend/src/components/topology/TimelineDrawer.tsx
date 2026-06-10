/**
 * TimelineDrawer — Slide-up timeline strip for the Topology page.
 *
 * A full-width panel that slides up from the bottom of the topology
 * canvas. Max height 150px. Contains playback controls, a smooth
 * 60fps-locked scrubber, speed selector, and time readout.
 *
 * Toggle state lives in topologyStore.timelineDrawerOpen.
 *
 * @module components/topology/TimelineDrawer
 */

import { memo, useCallback, useRef, useEffect } from 'react';
import {
  Play,
  Pause,
  Square,
  X,
  ChevronUp,
} from 'lucide-react';
import clsx from 'clsx';
import { useTopologyStore } from '../../stores/topologyStore';
import { useTimelineStore } from '../../stores/timelineStore';
import type { TimelineSpeed } from '../../types/timeline';

const SPEEDS: TimelineSpeed[] = [0.25, 0.5, 1, 2, 4, 8, 16];

/** Format an ISO timestamp to HH:MM:SS. */
function fmtTime(ts: string): string {
  try {
    return new Date(ts).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  } catch {
    return '—';
  }
}

/**
 * Full-width slide-up timeline strip. Mounts always; CSS drives
 * the translate animation so React never unmounts and re-mounts
 * the scrubber during transitions (avoids input loss).
 */
export const TimelineDrawer = memo(function TimelineDrawer() {
  const open = useTopologyStore((s) => s.timelineDrawerOpen);
  const toggleDrawer = useTopologyStore((s) => s.toggleTimelineDrawer);

  const position = useTimelineStore((s) => s.position);
  const config = useTimelineStore((s) => s.config);
  const snapshots = useTimelineStore((s) => s.snapshots);
  const play = useTimelineStore((s) => s.play);
  const pause = useTimelineStore((s) => s.pause);
  const stop = useTimelineStore((s) => s.stop);
  const seekTo = useTimelineStore((s) => s.seekTo);
  const setSpeed = useTimelineStore((s) => s.setSpeed);

  const isPlaying = position.state === 'playing';
  const trackRef = useRef<HTMLDivElement>(null);

  // ─── RAF-locked pointer scrubbing ─────────────────────────────
  const rafRef = useRef<number | null>(null);
  const pendingProgressRef = useRef<number | null>(null);
  const isDraggingRef = useRef(false);

  const computeProgress = useCallback((clientX: number): number => {
    if (!trackRef.current) return 0;
    const rect = trackRef.current.getBoundingClientRect();
    return Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
  }, []);

  const flushSeek = useCallback(() => {
    if (pendingProgressRef.current !== null) {
      seekTo(pendingProgressRef.current);
      pendingProgressRef.current = null;
    }
    rafRef.current = null;
    if (isDraggingRef.current) {
      rafRef.current = requestAnimationFrame(flushSeek);
    }
  }, [seekTo]);

  const handlePointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      e.currentTarget.setPointerCapture(e.pointerId);
      isDraggingRef.current = true;
      pendingProgressRef.current = computeProgress(e.clientX);
      if (rafRef.current === null) {
        rafRef.current = requestAnimationFrame(flushSeek);
      }
    },
    [computeProgress, flushSeek],
  );

  const handlePointerMove = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (!isDraggingRef.current) return;
      pendingProgressRef.current = computeProgress(e.clientX);
    },
    [computeProgress],
  );

  const handlePointerUp = useCallback(() => {
    isDraggingRef.current = false;
  }, []);

  // Cancel any pending RAF on unmount
  useEffect(() => {
    return () => {
      if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    };
  }, []);

  // Snapshot marker positions
  const startMs = new Date(config.startTime).getTime();
  const endMs = new Date(config.endTime).getTime();
  const rangeMs = endMs > startMs ? endMs - startMs : 1;

  return (
    <div
      className={clsx('topology-timeline-drawer', open && 'topology-timeline-drawer--open')}
      data-testid="timeline-drawer"
      aria-hidden={!open}
    >
      {/* ── Header strip ────────────────────────────────────────── */}
      <div className="topology-timeline-drawer__header">
        <span className="topology-timeline-drawer__label">
          <ChevronUp size={11} />
          Timeline Replay
        </span>

        {/* Speed selector */}
        <div className="topology-timeline-drawer__speeds">
          {SPEEDS.map((s) => (
            <button
              key={s}
              className={clsx(
                'tld-speed-btn',
                position.speed === s && 'tld-speed-btn--active',
              )}
              onClick={() => setSpeed(s)}
              data-testid={`tld-speed-${s}`}
            >
              {s}x
            </button>
          ))}
        </div>

        {/* Close */}
        <button
          className="topology-timeline-drawer__close"
          onClick={toggleDrawer}
          aria-label="Close timeline drawer"
          data-testid="tld-close"
        >
          <X size={13} />
        </button>
      </div>

      {/* ── Scrubber row ─────────────────────────────────────────── */}
      <div className="topology-timeline-drawer__scrubber-row">
        {/* Playback controls */}
        <div className="topology-timeline-drawer__controls">
          <button
            className={clsx('tld-ctrl-btn', isPlaying && 'tld-ctrl-btn--active')}
            onClick={isPlaying ? pause : play}
            aria-label={isPlaying ? 'Pause' : 'Play'}
            data-testid="tld-play-pause"
          >
            {isPlaying ? <Pause size={13} /> : <Play size={13} />}
          </button>
          <button
            className="tld-ctrl-btn"
            onClick={stop}
            aria-label="Stop"
            data-testid="tld-stop"
          >
            <Square size={13} />
          </button>
        </div>

        {/* Track */}
        <div
          ref={trackRef}
          className="tld-track"
          role="slider"
          aria-label="Timeline position"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(position.progress * 100)}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerCancel={handlePointerUp}
          data-testid="tld-track"
        >
          {/* Fill */}
          <div
            className="tld-track__fill"
            style={{ width: `${position.progress * 100}%` }}
          />

          {/* Thumb */}
          <div
            className="tld-track__thumb"
            style={{ left: `${position.progress * 100}%` }}
          />

          {/* Snapshot markers */}
          {snapshots.map((snap) => {
            const pct =
              ((new Date(snap.timestamp).getTime() - startMs) / rangeMs) * 100;
            if (pct < 0 || pct > 100) return null;
            return (
              <div
                key={snap.id}
                className="tld-track__marker"
                style={{ left: `${pct}%` }}
                title={fmtTime(snap.timestamp)}
              />
            );
          })}
        </div>

        {/* Time readout */}
        <div className="topology-timeline-drawer__time">
          <span className="tld-time-current">{fmtTime(position.currentTime)}</span>
          <span className="tld-time-end">{fmtTime(config.endTime)}</span>
        </div>
      </div>
    </div>
  );
});
