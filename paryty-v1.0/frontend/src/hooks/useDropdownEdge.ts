/**
 * useDropdownEdge — Viewport-edge detection for dropdown menus.
 *
 * Measures the `.aef-dropdown-menu` element inside the container ref
 * and returns edge-overflow signals for horizontal and both vertical directions.
 *
 * - `flipRight`: apply `.aef-dropdown-menu--flip` to flip horizontally.
 * - `flipUp`: apply `.aef-dropdown-menu--flip-up` when a downward-opening
 *   dropdown overflows the bottom of the viewport.
 * - `flipDown`: apply `.aef-dropdown-menu--flip-up` when an upward-opening
 *   dropdown overflows the top of the viewport.
 *
 * @module hooks/useDropdownEdge
 */

import { useLayoutEffect, useState, type RefObject } from 'react';

/** Result from useDropdownEdge — independent axis overflow signals. */
export interface DropdownEdgeResult {
  /** True when menu right edge extends past viewport right edge. */
  flipRight: boolean;
  /** True when menu bottom edge extends past viewport bottom edge
   *  (for dropdowns that open downward by default). */
  flipUp: boolean;
  /** True when menu top edge extends past viewport top edge
   *  (for dropdowns that open upward by default). */
  flipDown: boolean;
}

/**
 * Detects whether a dropdown menu anchored inside `containerRef`
 * overflows the right, top, or bottom edges of the viewport.
 *
 * @param containerRef - ref attached to the `.aef-dropdown` wrapper div
 * @param open - whether the dropdown is currently open
 * @returns `{ flipRight, flipUp, flipDown }` — apply the corresponding CSS
 *          modifier classes independently based on each signal.
 */
export function useDropdownEdge(
  containerRef: RefObject<HTMLElement | null>,
  open: boolean,
): DropdownEdgeResult {
  const [result, setResult] = useState<DropdownEdgeResult>({
    flipRight: false,
    flipUp: false,
    flipDown: false,
  });

  useLayoutEffect(() => {
    if (!open || !containerRef.current) {
      setResult({ flipRight: false, flipUp: false, flipDown: false });
      return;
    }
    const menu = containerRef.current.querySelector('.aef-dropdown-menu') as HTMLElement | null;
    if (!menu) {
      setResult({ flipRight: false, flipUp: false, flipDown: false });
      return;
    }
    const rect = menu.getBoundingClientRect();
    setResult({
      flipRight: rect.right > window.innerWidth - 4,
      flipUp: rect.bottom > window.innerHeight - 4,
      flipDown: rect.top < 4,
    });
  }, [open, containerRef]);

  return result;
}
