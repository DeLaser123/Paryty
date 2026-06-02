import { useRef, useEffect } from 'react';
import { useTopology } from '../hooks/useTopology';
import { TopologyRenderer } from '../engine/renderer';

export default function TopologyView() {
  const containerRef = useRef<HTMLDivElement>(null);
  const rendererRef = useRef<TopologyRenderer | null>(null);
  const topology = useTopology();

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
    if (topology.topology && rendererRef.current) {
      rendererRef.current.update(topology.topology);
    }
  }, [topology.topology]);

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Topology</h2>
        <div className="view-controls">
          <input
            type="text"
            placeholder="Search nodes..."
            value={topology.searchQuery}
            onChange={(e) => topology.setSearchQuery(e.target.value)}
          />
          {topology.isLoading && <span className="loading-indicator">Loading...</span>}
          {topology.error && <span className="error-indicator">{topology.error}</span>}
        </div>
      </div>
      <div ref={containerRef} className="topology-canvas" />
      {topology.selectedNode && (
        <div className="detail-panel">
          <h3>{topology.selectedNode.name}</h3>
          <p>Type: {topology.selectedNode.type}</p>
          <p>Status: {topology.selectedNode.status}</p>
          <pre>{JSON.stringify(topology.selectedNode.labels, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}
