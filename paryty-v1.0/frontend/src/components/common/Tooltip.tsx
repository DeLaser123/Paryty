/**
 * Tooltip — Global portal-rendered tooltip that follows the cursor.
 *
 * Rendered via createPortal to document.body so it always appears above
 * every other element (including future modals, drawers, overlays).
 * Positioned at the mouse cursor with a small offset.
 *
 * @module components/common/Tooltip
 */

import { useState, useRef, useCallback, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

interface TooltipProps {
  /** Content to show in the tooltip (usually the full label). */
  label: string;
  /** The element that triggers the tooltip on hover. */
  children: ReactNode;
}

interface CursorPos {
  x: number;
  y: number;
}

const OFFSET_X = 12; // px to the right of cursor
const OFFSET_Y = 16; // px below cursor (negated when flipped above)
const FLIP_THRESHOLD = 60; // flip above if cursor is within this many px of viewport bottom

/**
 * Global tooltip rendered to document.body at the mouse cursor.
 *
 * Delays 400ms before showing. Uses createPortal to escape any
 * parent overflow/stacking contexts.
 */
export function Tooltip({ label, children }: TooltipProps) {
  const [visible, setVisible] = useState(false);
  const [pos, setPos] = useState<CursorPos>({ x: 0, y: 0 });
  const [above, setAbove] = useState(true);
  const timerRef = useRef<ReturnType<typeof setTimeout>>();
  const wrapperRef = useRef<HTMLSpanElement>(null);

  const trackCursor = useCallback((e: React.MouseEvent) => {
    setPos({ x: e.clientX, y: e.clientY });
  }, []);

  const show = useCallback(() => {
    timerRef.current = setTimeout(() => {
      // Flip above if cursor is near the bottom edge
      if (wrapperRef.current) {
        const rect = wrapperRef.current.getBoundingClientRect();
        setAbove(window.innerHeight - rect.bottom < FLIP_THRESHOLD);
      }
      setVisible(true);
    }, 400);
  }, []);

  const hide = useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    setVisible(false);
  }, []);

  return (
    <>
      <span
        ref={wrapperRef}
        className="aef-tooltip-anchor"
        onMouseEnter={show}
        onMouseMove={trackCursor}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
      >
        {children}
      </span>
      {visible &&
        createPortal(
          <span
            className="aef-tooltip"
            data-tooltip-above={above}
            role="tooltip"
            style={{
              position: 'fixed',
              left: pos.x + OFFSET_X,
              top: above ? pos.y - OFFSET_Y : pos.y + OFFSET_Y,
            }}
          >
            {label}
          </span>,
          document.body,
        )}
    </>
  );
}
