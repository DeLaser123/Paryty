/**
 * useKeyboard — Global keyboard shortcut handler.
 *
 * Handles Ctrl+K (search), Escape (close/deselect), +/- (zoom),
 * 0 (fit), Space (play/pause).
 *
 * @module hooks/useKeyboard
 */

import { useState, useEffect, useCallback } from 'react';
import { useTopologyStore } from '../stores/topologyStore';

/** Keyboard state returned by the hook. */
interface KeyboardState {
  /** Whether the search overlay is open. */
  isSearchOpen: boolean;
  /** Toggle the search overlay. */
  setIsSearchOpen: (open: boolean) => void;
}

/**
 * Hook for global keyboard shortcuts.
 *
 * Shortcuts:
 * - Ctrl+K / Cmd+K → Open search overlay
 * - Escape → Close search / deselect node
 * - + → Zoom in
 * - - → Zoom out
 * - 0 → Fit to content
 */
export function useKeyboard(): KeyboardState {
  const [isSearchOpen, setIsSearchOpen] = useState(false);
  const selectNode = useTopologyStore((s) => s.selectNode);
  const selectEdge = useTopologyStore((s) => s.selectEdge);
  const setViewport = useTopologyStore((s) => s.setViewport);
  const viewport = useTopologyStore((s) => s.viewport);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      // Ignore shortcuts when typing in an input
      const target = e.target as HTMLElement;
      if (
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' ||
        target.isContentEditable
      ) {
        // Only allow Escape in inputs
        if (e.key === 'Escape') {
          setIsSearchOpen(false);
        }
        return;
      }

      // Ctrl+K / Cmd+K → Search
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
        setIsSearchOpen((prev) => !prev);
        return;
      }

      // Escape → Close search or deselect
      if (e.key === 'Escape') {
        if (isSearchOpen) {
          setIsSearchOpen(false);
        } else {
          selectNode(null);
          selectEdge(null);
        }
        return;
      }

      // + → Zoom in
      if (e.key === '+' || e.key === '=') {
        e.preventDefault();
        setViewport({ zoom: Math.min(viewport.zoom + 0.2, 5) });
        return;
      }

      // - → Zoom out
      if (e.key === '-') {
        e.preventDefault();
        setViewport({ zoom: Math.max(viewport.zoom - 0.2, 0.1) });
        return;
      }

      // 0 → Fit to content
      if (e.key === '0' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        setViewport({ zoom: 1, panX: 0, panY: 0 });
        return;
      }
    },
    [isSearchOpen, selectNode, selectEdge, setViewport, viewport.zoom],
  );

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [handleKeyDown]);

  return {
    isSearchOpen,
    setIsSearchOpen,
  };
}
