/**
 * TopologyCanvas — Main topology visualization view.
 *
 * Mounts the TopologyRenderer into a container div with ResizeObserver.
 * Overlays TopologyControls, SearchOverlay, ClusterBreadcrumb,
 * NodeDetailPanel, and EdgeDetailPanel.
 *
 * @module components/topology/TopologyCanvas
 */

import { useRef, useEffect, useCallback } from 'react';
import { useTopology } from '../../hooks/useTopology';
import { useTopologyStore } from '../../stores/topologyStore';
import { TopologyRenderer } from '../../engine/renderer';
import { TopologyControls } from './TopologyControls';
import { SearchOverlay } from './SearchOverlay';
import { ClusterBreadcrumb } from './ClusterBreadcrumb';
import { NodeDetailPanel } from './NodeDetailPanel';
import { EdgeDetailPanel } from './EdgeDetailPanel';
import { useKeyboard } from '../../hooks/useKeyboard';

/**
 * Main topology view replacing the old TopologyView.
 *
 * Renders the PixiJS canvas with overlay controls for zoom, search,
 * cluster navigation, and detail panels.
 */
export function TopologyCanvas() {
  const containerRef = useRef<HTMLDivElement>(null);
  const rendererRef = useRef<TopologyRenderer | null>(null);
  const topology = useTopology();

  // Use selectors to avoid full-store re-renders
  const selectedNode = useTopologyStore((s) => s.selectedNode);
  const selectedEdge = useTopologyStore((s) => s.selectedEdge);
  const selectNode = useTopologyStore((s) => s.selectNode);
  const selectEdge = useTopologyStore((s) => s.selectEdge);
  const { isSearchOpen, setIsSearchOpen } = useKeyboard();

  // Mount renderer
  useEffect(() => {
    if (!containerRef.current) return;

    const renderer = new TopologyRenderer({
      container: containerRef.current,
      width: containerRef.current.clientWidth,
      height: containerRef.current.clientHeight,
    });
    rendererRef.current = renderer;

    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        renderer.resize(entry.contentRect.width, entry.contentRect.height);
      }
    });
    resizeObserver.observe(containerRef.current);

    return () => {
      resizeObserver.disconnect();
      renderer.destroy();
      rendererRef.current = null;
    };
  }, []);

  // Update renderer when topology data changes
  useEffect(() => {
    if (topology.topology && rendererRef.current) {
      rendererRef.current.setTopology(topology.topology);
    }
  }, [topology.topology]);

  // Deselect node/edge on canvas click
  const handleCanvasClick = useCallback(() => {
    selectNode(null);
    selectEdge(null);
  }, [selectNode, selectEdge]);

  const handleCloseNodePanel = useCallback(() => {
    selectNode(null);
  }, [selectNode]);

  const handleCloseEdgePanel = useCallback(() => {
    selectEdge(null);
  }, [selectEdge]);

  return (
    <div className="view-container">
      {/* Canvas area */}
      <div className="topology-canvas-area">
        <div
          ref={containerRef}
          className="topology-canvas"
          onClick={handleCanvasClick}
          data-testid="topology-canvas"
        />

        {/* Overlay controls */}
        <TopologyControls />

        {/* Cluster breadcrumb */}
        <ClusterBreadcrumb />

        {/* Search overlay (Ctrl+K) */}
        {isSearchOpen && (
          <SearchOverlay onClose={() => setIsSearchOpen(false)} />
        )}

        {/* Detail panels */}
        {selectedNode && (
          <NodeDetailPanel node={selectedNode} onClose={handleCloseNodePanel} />
        )}
        {selectedEdge && (
          <EdgeDetailPanel edge={selectedEdge} onClose={handleCloseEdgePanel} />
        )}

        {/* Loading / error states */}
        {topology.isLoading && (
          <div className="topology-canvas__status">
            <span className="aef-text-secondary">Loading topology…</span>
          </div>
        )}
        {topology.error && (
          <div className="topology-canvas__status topology-canvas__status--error">
            <span>{topology.error}</span>
          </div>
        )}
      </div>
    </div>
  );
}
