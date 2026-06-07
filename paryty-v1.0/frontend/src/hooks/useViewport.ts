/**
 * useViewport — Hook exposing viewport state and controls.
 *
 * Returns current zoom, panX, panY and control functions
 * (zoomIn, zoomOut, fitToContent, setViewport).
 *
 * @module hooks/useViewport
 */

import { useCallback } from 'react';
import { useTopologyStore, type ViewportState } from '../stores/topologyStore';

/** Viewport controls returned by the hook. */
interface ViewportControls {
  /** Current zoom level. */
  zoom: number;
  /** Current pan X offset. */
  panX: number;
  /** Current pan Y offset. */
  panY: number;
  /** Increment zoom by 0.2 (max 5.0). */
  zoomIn: () => void;
  /** Decrement zoom by 0.2 (min 0.1). */
  zoomOut: () => void;
  /** Reset viewport to default (zoom 1, centered). */
  fitToContent: () => void;
  /** Set partial viewport state. */
  setViewport: (partial: Partial<ViewportState>) => void;
}

/**
 * Hook providing viewport state and navigation controls.
 *
 * Reads from and writes to the topologyStore viewport.
 * zoomIn/zoomOut increment by 0.2 within 0.1–5.0 bounds.
 */
export function useViewport(): ViewportControls {
  const viewport = useTopologyStore((s) => s.viewport);
  const storeSetViewport = useTopologyStore((s) => s.setViewport);

  const zoomIn = useCallback(() => {
    const newZoom = Math.min(viewport.zoom + 0.2, 5);
    storeSetViewport({ zoom: newZoom });
  }, [viewport.zoom, storeSetViewport]);

  const zoomOut = useCallback(() => {
    const newZoom = Math.max(viewport.zoom - 0.2, 0.1);
    storeSetViewport({ zoom: newZoom });
  }, [viewport.zoom, storeSetViewport]);

  const fitToContent = useCallback(() => {
    storeSetViewport({ zoom: 1, panX: 0, panY: 0 });
  }, [storeSetViewport]);

  return {
    zoom: viewport.zoom,
    panX: viewport.panX,
    panY: viewport.panY,
    zoomIn,
    zoomOut,
    fitToContent,
    setViewport: storeSetViewport,
  };
}
