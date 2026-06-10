/**
 * TimelineDrawerBar — Retractable timeline control bar for the Topology page.
 *
 * Sits between the main content area and the StatusBar, expanding upward
 * from a collapsed state into a full timeline control strip (max 150px).
 * Toggle state lives in topologyStore.timelineDrawerOpen.
 *
 * Always mounted so the scrubber and playback state persist across
 * open/close transitions — CSS drives the slide-up animation.
 *
 * @module components/layout/TimelineDrawerBar
 */

import { memo, useCallback, useRef, useEffect, useLayoutEffect, useState } from 'react';
import {
  Play,
  Pause,
  Square,
  ChevronDown,
  SkipForward,
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
 * Full-width retractable timeline bar.
 * Mounts always; CSS drives the slide-up animation.
 */
export const TimelineDrawerBar = memo(function TimelineDrawerBar() {
  const open = useTopologyStore((s) => s.timelineDrawerOpen);

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

  // ─── Speed drop-up ────────────────────────────────────────────
  const [speedOpen, setSpeedOpen] = useState(false);
  const speedDropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!speedOpen) return;
    const handle = (e: MouseEvent) => {
      if (speedDropdownRef.current && !speedDropdownRef.current.contains(e.target as Node)) {
        setSpeedOpen(false);
      }
    };
    document.addEventListener('mousedown', handle);
    return () => document.removeEventListener('mousedown', handle);
  }, [speedOpen]);

  // ─── Past / present detection ──────────────────────────────────
  const isAtPresent = position.progress >= 0.995;

  useLayoutEffect(() => {
    const layout = document.querySelector('.app-layout');
    if (!layout) return;
    if (!isAtPresent && open) {
      layout.classList.add('app-layout--viewing-past');
    } else {
      layout.classList.remove('app-layout--viewing-past');
    }
    return () => { layout.classList.remove('app-layout--viewing-past'); };
  }, [isAtPresent, open]);

  const goToPresent = useCallback(() => {
    seekTo(1);
  }, [seekTo]);

  // ─── RAF-locked pointer scrubbing ─────────────────────────────
  const rafRef = useRef<number | null>(null);
  const pendingProgressRef = useRef<number | null>(null);
  const isDraggingRef = useRef(false);
  const [isDragging, setIsDragging] = useState(false);

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
      setIsDragging(true);
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
    setIsDragging(false);
  }, []);

  useEffect(() => {
    return () => {
      if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    };
  }, []);

  const startMs = new Date(config.startTime).getTime();
  const endMs = new Date(config.endTime).getTime();
  const rangeMs = endMs > startMs ? endMs - startMs : 1;

  return (
    <div
      className={clsx('timeline-drawer-bar', open && 'timeline-drawer-bar--open')}
      data-testid="timeline-drawer-bar"
      aria-hidden={!open}
    >
      <div className="timeline-drawer-bar__inner">
        {/* ── Header row ──────────────────────────────────── */}
        <div className="timeline-drawer-bar__header">
          <span className="timeline-drawer-bar__label">
            <ChevronDown
              size={11}
              className={clsx('timeline-drawer-bar__label-chevron', open && 'timeline-drawer-bar__label-chevron--open')}
            />
            Timeline Replay
          </span>

          {/* Speed selector — single trigger with drop-up menu */}
          <div className="timeline-drawer-bar__speed-dropdown" ref={speedDropdownRef}>
            <button
              className="timeline-drawer-bar__speed-trigger"
              onClick={() => setSpeedOpen((p) => !p)}
              data-testid="tdb-speed-trigger"
            >
              <span>{position.speed}x</span>
              <ChevronUp
                size={10}
                className={clsx(
                  'timeline-drawer-bar__speed-chevron',
                  speedOpen && 'timeline-drawer-bar__speed-chevron--open',
                )}
              />
            </button>
            {speedOpen && (
              <div className="timeline-drawer-bar__speed-menu" data-testid="tdb-speed-menu">
                {SPEEDS.map((s) => (
                  <button
                    key={s}
                    className={clsx(
                      'timeline-drawer-bar__speed-item',
                      position.speed === s && 'timeline-drawer-bar__speed-item--active',
                    )}
                    onClick={() => { setSpeed(s); setSpeedOpen(false); }}
                    data-testid={`tdb-speed-${s}`}
                  >
                    {s}x
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* ── Scrubber row ────────────────────────────────── */}
        <div className="timeline-drawer-bar__scrubber-row">
          {/* Playback controls */}
          <div className="timeline-drawer-bar__controls">
            <button
              className={clsx('timeline-drawer-bar__ctrl-btn', isPlaying && 'timeline-drawer-bar__ctrl-btn--active')}
              onClick={isPlaying ? pause : play}
              aria-label={isPlaying ? 'Pause' : 'Play'}
              data-testid="tdb-play-pause"
            >
              {isPlaying ? <Pause size={13} /> : <Play size={13} />}
            </button>
            <button
              className="timeline-drawer-bar__ctrl-btn"
              onClick={stop}
              aria-label="Stop"
              data-testid="tdb-stop"
            >
              <Square size={13} />
            </button>
          </div>

          {/* Track */}
          <div
            ref={trackRef}
            className={clsx(
              'timeline-drawer-bar__track',
              isDragging && 'timeline-drawer-bar__track--dragging',
            )}
            role="slider"
            aria-label="Timeline position"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(position.progress * 100)}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerUp}
            onPointerCancel={handlePointerUp}
            data-testid="tdb-track"
          >
            {/* Fill — white for played portion */}
            <div
              className="timeline-drawer-bar__track-fill"
              style={{ width: `${position.progress * 100}%` }}
            />

            {/* Ball thumb */}
            <div
              className={clsx(
                'timeline-drawer-bar__track-thumb',
                isDragging && 'timeline-drawer-bar__track-thumb--dragging',
              )}
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
                  className="timeline-drawer-bar__track-marker"
                  style={{ left: `${pct}%` }}
                  title={fmtTime(snap.timestamp)}
                />
              );
            })}
          </div>

          {/* Time readout + go-to-present button */}
          <div className="timeline-drawer-bar__time">
            <span className="timeline-drawer-bar__time-current">{fmtTime(position.currentTime)}</span>
            <span className="timeline-drawer-bar__time-sep">/</span>
            <span className="timeline-drawer-bar__time-end">{fmtTime(config.endTime)}</span>
            <button
              className={clsx(
                'timeline-drawer-bar__present-btn',
                isAtPresent && 'timeline-drawer-bar__present-btn--at-present',
              )}
              onClick={goToPresent}
              aria-label="Go to present time"
              title="Go to present"
              data-testid="tdb-present"
            >
              <SkipForward size={11} />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
});
