import { useRef, useEffect } from 'react';
import { useTopology } from '../hooks/useTopology';
import { useTopologyStore } from '../stores/topologyStore';
import { TopologyRenderer } from '../engine/renderer';

export default function TopologyView() {
  const containerRef = useRef<HTMLDivElement>(null);
  const rendererRef = useRef<TopologyRenderer | null>(null);
  const { topology, isLoading, error } = useTopology();
  const searchQuery = useTopologyStore((s) => s.searchQuery);
  const setSearchQuery = useTopologyStore((s) => s.setSearchQuery);
  const selectedNode = useTopologyStore((s) => s.selectedNode);

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
    };
  }, []);

  useEffect(() => {
    if (topology && rendererRef.current) {
      rendererRef.current.update(topology);
    }
  }, [topology]);

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Topology</h2>
        <div className="view-controls">
          <input
            type="text"
            placeholder="Search nodes..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
          {isLoading && <span className="loading-indicator">Loading...</span>}
          {error && <span className="error-indicator">{error}</span>}
        </div>
      </div>
      <div ref={containerRef} className="topology-canvas" />
      {selectedNode && (
        <div className="detail-panel aef-container-card">
          <div className="aef-container-card__header">
            <span className="aef-container-card__title aef-truncate">{selectedNode.name}</span>
          </div>
          <div className="aef-container-card__body aef-scroll-thin">
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Type</span>
              <span className="aef-stat-module__value">{selectedNode.type}</span>
            </div>
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Status</span>
              <span className="aef-stat-module__value">{selectedNode.status}</span>
            </div>
            <pre style={{ fontFamily: 'var(--aef-font-body)', fontSize: 10, color: 'var(--aef-text-secondary)', whiteSpace: 'pre-wrap' }}>
              {JSON.stringify(selectedNode.labels, null, 2)}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
}
