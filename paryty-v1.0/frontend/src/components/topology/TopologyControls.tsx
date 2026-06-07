/**
 * TopologyControls — Floating control bar for topology canvas.
 *
 * Provides zoom in/out, fit-to-content, and layout mode toggle.
 * Uses button group pattern (aef-btn-active/inactive).
 *
 * @module components/topology/TopologyControls
 */

import { memo, useCallback } from 'react';
import { ZoomIn, ZoomOut, Maximize2, Network, GitBranch, Circle } from 'lucide-react';
import clsx from 'clsx';
import { useTopologyStore, type LayoutMode } from '../../stores/topologyStore';

/** Available layout modes with their icons and labels. */
const LAYOUT_MODES: { mode: LayoutMode; icon: React.ReactNode; label: string }[] = [
  { mode: 'force',        icon: <Network size={14} />,  label: 'Force' },
  { mode: 'hierarchical', icon: <GitBranch size={14} />, label: 'Hierarchical' },
  { mode: 'radial',       icon: <Circle size={14} />,   label: 'Radial' },
];

/**
 * Floating control bar positioned at bottom-left of the topology canvas.
 *
 * Uses the design system button group pattern for zoom and layout controls.
 */
export const TopologyControls = memo(function TopologyControls() {
  const { viewport, setViewport, layoutMode, setLayoutMode } = useTopologyStore();

  const zoomIn = useCallback(() => {
    const newZoom = Math.min(viewport.zoom + 0.2, 5);
    setViewport({ zoom: newZoom });
  }, [viewport.zoom, setViewport]);

  const zoomOut = useCallback(() => {
    const newZoom = Math.max(viewport.zoom - 0.2, 0.1);
    setViewport({ zoom: newZoom });
  }, [viewport.zoom, setViewport]);

  const fitToContent = useCallback(() => {
    setViewport({ zoom: 1, panX: 0, panY: 0 });
  }, [setViewport]);

  return (
    <div className="topology-controls" data-testid="topology-controls">
      {/* Zoom controls */}
      <div className="topology-controls__group">
        <button
          className="aef-btn aef-btn-inactive"
          onClick={zoomIn}
          aria-label="Zoom in"
          data-testid="topology-zoom-in"
        >
          <ZoomIn size={14} />
        </button>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={zoomOut}
          aria-label="Zoom out"
          data-testid="topology-zoom-out"
        >
          <ZoomOut size={14} />
        </button>
        <button
          className="aef-btn aef-btn-inactive"
          onClick={fitToContent}
          aria-label="Fit to content"
          data-testid="topology-fit"
        >
          <Maximize2 size={14} />
        </button>
      </div>

      {/* Layout mode toggle */}
      <div className="topology-controls__group">
        {LAYOUT_MODES.map((lm) => (
          <button
            key={lm.mode}
            className={clsx(
              'aef-btn',
              layoutMode === lm.mode ? 'aef-btn-active' : 'aef-btn-inactive',
            )}
            onClick={() => setLayoutMode(lm.mode)}
            aria-label={`Layout: ${lm.label}`}
            data-testid={`topology-layout-${lm.mode}`}
          >
            {lm.icon}
          </button>
        ))}
      </div>
    </div>
  );
});
