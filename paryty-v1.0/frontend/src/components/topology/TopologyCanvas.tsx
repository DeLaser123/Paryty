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
import clsx from 'clsx';
import { useTopology } from '../../hooks/useTopology';
import { useTopologyStore } from '../../stores/topologyStore';
import type { HealthFilter } from '../../stores/topologyStore';
import { TopologyRenderer } from '../../engine/renderer';
import { MemoryBudget } from '../../engine/memoryBudget';
import { TopologyControls } from './TopologyControls';
import { SearchOverlay } from './SearchOverlay';
import { ClusterBreadcrumb } from './ClusterBreadcrumb';
import { NodeDetailPanel } from './NodeDetailPanel';
import { EdgeDetailPanel } from './EdgeDetailPanel';
import { useKeyboard } from '../../hooks/useKeyboard';

const HEALTH_FILTERS: { value: HealthFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'healthy', label: 'Healthy' },
  { value: 'degraded', label: 'Degraded' },
  { value: 'unhealthy', label: 'Unhealthy' },
];

/**
 * Main topology view replacing the old TopologyView.
 *
 * Renders the PixiJS canvas with overlay controls for zoom, search,
 * cluster navigation, and detail panels.
 */
export function TopologyCanvas() {
  const containerRef = useRef<HTMLDivElement>(null);
  const rendererRef = useRef<TopologyRenderer | null>(null);
  /**
   * StrictMode double-init guard.
   *
   * React StrictMode (dev only) double-invokes effects: mount → cleanup →
   * remount. Without this guard, two TopologyRenderers — and thus two
   * WorkerRenderBridges, two WebGL contexts, and two 60fps RAF loops — exist
   * simultaneously between the first cleanup and the second mount, doubling
   * GPU pressure. The boolean ref serialises construction: if an instance was
   * already created for this DOM node it is not recreated until the previous
   * one has fully been destroyed.
   */
  const rendererCreatedRef = useRef(false);

  const topology = useTopology();

  // Use selectors to avoid full-store re-renders
  const selectedNode = useTopologyStore((s) => s.selectedNode);
  const selectedEdge = useTopologyStore((s) => s.selectedEdge);
  const selectNode = useTopologyStore((s) => s.selectNode);
  const selectEdge = useTopologyStore((s) => s.selectEdge);
  const filterHealth = useTopologyStore((s) => s.filterHealth);
  const setFilterHealth = useTopologyStore((s) => s.setFilterHealth);
  const { isSearchOpen, setIsSearchOpen } = useKeyboard();

  // Mount renderer — enforced single-instance even under StrictMode
  useEffect(() => {
    if (!containerRef.current) return;
    // Guard: do not create a second renderer if one is already alive for this
    // DOM node (React StrictMode fires this effect twice in dev).
    if (rendererCreatedRef.current) return;
    rendererCreatedRef.current = true;

    const container = containerRef.current;

    const renderer = new TopologyRenderer({
      container,
      width: container.clientWidth,
      height: container.clientHeight,
    });
    rendererRef.current = renderer;

    // Main-thread MemoryBudget monitor.
    //
    // performance.memory is a Chrome-only API that Chrome explicitly disables
    // inside dedicated workers (since Chrome 77, security restriction). The
    // MemoryBudget instance that lives inside PixiTopologyApp inside the render
    // worker is therefore a silent no-op — its isAvailable check returns false
    // and no degradation ever fires. We create a second instance here on the
    // main thread (where the API is available), derive the budget level, and
    // forward it to the renderer via sendBudgetLevel so the worker's particle
    // system and effect manager actually respond to memory pressure.
    const mainThreadBudget = new MemoryBudget({
      onBudgetChange: (level) => renderer.sendBudgetLevel(level),
      onRecover: () => renderer.sendBudgetLevel('normal'),
    });
    mainThreadBudget.start();

    const resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        renderer.resize(entry.contentRect.width, entry.contentRect.height);
      }
    });
    resizeObserver.observe(container);

    // Handle WebGL context loss/restore — critical for GPU availability.
    // On context loss: destroy renderer and flag for recreation.
    // On context restore: trigger full reinitialization by resetting the ref.
    const canvas = container.querySelector('canvas');
    const onContextLost = () => {
      mainThreadBudget.stop();
      renderer.destroy();
      rendererCreatedRef.current = false;
    };
    const onContextRestored = () => {
      rendererCreatedRef.current = false;
    };
    if (canvas) {
      canvas.addEventListener('webglcontextlost', onContextLost);
      canvas.addEventListener('webglcontextrestored', onContextRestored);
    }

    return () => {
      if (canvas) {
        canvas.removeEventListener('webglcontextlost', onContextLost);
        canvas.removeEventListener('webglcontextrestored', onContextRestored);
      }
      rendererCreatedRef.current = false;
      resizeObserver.disconnect();
      mainThreadBudget.stop();
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
    <div className="view-container topology-view-container">
      {/* Canvas area — fills remaining space above the timeline drawer */}
      <div className="topology-canvas-area">
        <div
          ref={containerRef}
          className="topology-canvas"
          onClick={handleCanvasClick}
          data-testid="topology-canvas"
        />

        {/* Overlay controls */}
        <TopologyControls />

        {/* Health filter bar */}
        <div
          className="topology-health-filter"
          role="group"
          aria-label="Filter by health status"
          style={{
            position: 'absolute',
            top: 'var(--aef-space-3)',
            left: '50%',
            transform: 'translateX(-50%)',
            display: 'flex',
            gap: 'var(--aef-space-1)',
            background: 'var(--aef-surface)',
            borderRadius: 'var(--aef-radius-md)',
            padding: 'var(--aef-space-1)',
            border: '1px solid var(--aef-border)',
            zIndex: 10,
          }}
        >
          {HEALTH_FILTERS.map((f) => (
            <button
              key={f.value}
              className={clsx('aef-btn', filterHealth === f.value ? 'aef-btn-active' : 'aef-btn-inactive')}
              onClick={() => setFilterHealth(f.value)}
              style={{ padding: '2px 8px', fontSize: 11 }}
              data-testid={`health-filter-${f.value}`}
            >
              {f.label}
            </button>
          ))}
        </div>

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
