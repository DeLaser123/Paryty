/**
 * NodeDetailPanel — Slide-out panel for selected topology node.
 *
 * Shows node type icon, name, status badge, metrics summary,
 * and connections list. Animated entry/exit with framer-motion.
 *
 * @module components/topology/NodeDetailPanel
 */

import { memo, useMemo } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { X, Server, Box, Cpu, GitBranch } from 'lucide-react';
import type { TopologyNode } from '../../types/topology';
import { useTopologyStore } from '../../stores/topologyStore';

/** Props for NodeDetailPanel. */
interface NodeDetailPanelProps {
  /** The selected node to display details for. */
  node: TopologyNode;
  /** Callback when the panel close button is clicked. */
  onClose: () => void;
}

/** Map node type to icon component. */
function getNodeIcon(type: TopologyNode['type']): React.ReactNode {
  switch (type) {
    case 'host':
      return <Server size={16} />;
    case 'container':
      return <Box size={16} />;
    case 'service':
      return <GitBranch size={16} />;
    case 'process':
      return <Cpu size={16} />;
    default:
      return <Server size={16} />;
  }
}

/** Map node status to badge class. */
function getStatusBadgeClass(status: TopologyNode['status']): string {
  switch (status) {
    case 'healthy':
      return 'badge-valid';
    case 'degraded':
      return 'badge-warning';
    case 'unhealthy':
      return 'badge-warning';
    default:
      return 'badge-pending';
  }
}

/**
 * Slide-out detail panel for a selected topology node.
 *
 * Uses container card pattern from the design system.
 * Animated with framer-motion slide-in from right.
 */
export const NodeDetailPanel = memo(function NodeDetailPanel({
  node,
  onClose,
}: NodeDetailPanelProps) {
  const topology = useTopologyStore((s) => s.topology);

  // Find connected edges
  const connections = useMemo(() => {
    if (!topology) return [];
    return topology.edges
      .filter((e) => e.sourceId === node.id || e.targetId === node.id)
      .map((e) => {
        const otherId = e.sourceId === node.id ? e.targetId : e.sourceId;
        const otherNode = topology.nodes.find((n) => n.id === otherId);
        return {
          edge: e,
          otherNode,
        };
      });
  }, [topology, node.id]);

  return (
    <AnimatePresence>
      <motion.div
        className="detail-panel aef-container-card"
        initial={{ x: '100%', opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        exit={{ x: '100%', opacity: 0 }}
        transition={{ duration: 0.2, ease: [0.2, 0, 0, 1] }}
        data-testid="node-detail-panel"
      >
        {/* Header */}
        <div className="aef-container-card__header">
          <div className="aef-container-card__icon">
            {getNodeIcon(node.type)}
          </div>
          <span className="aef-container-card__title aef-truncate">{node.name}</span>
          <span className={`aef-badge ${getStatusBadgeClass(node.status)}`}>
            {node.status}
          </span>
          <button
            className="aef-modal-close"
            onClick={onClose}
            aria-label="Close panel"
            data-testid="node-detail-close"
          >
            <X size={16} />
          </button>
        </div>

        {/* Body */}
        <div className="aef-container-card__body aef-scroll">
          {/* Node info */}
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Type</span>
            <span className="aef-stat-module__value">{node.type}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">ID</span>
            <span className="aef-stat-module__value aef-truncate">{node.id}</span>
          </div>

          {/* Metrics */}
          {node.cpuUsage !== undefined && (
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">CPU</span>
              <span className="aef-stat-module__value">{node.cpuUsage.toFixed(1)}%</span>
            </div>
          )}
          {node.memoryUsage !== undefined && (
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Memory</span>
              <span className="aef-stat-module__value">{node.memoryUsage.toFixed(1)}%</span>
            </div>
          )}

          {/* Connections */}
          {connections.length > 0 && (
            <>
              <div className="aef-stat-module__label" style={{ marginTop: 'var(--aef-space-2)' }}>
                Connections ({connections.length})
              </div>
              {connections.map((conn) => (
                <div key={conn.edge.id} className="aef-stat-module">
                  <span className="aef-stat-module__label aef-truncate">
                    {conn.otherNode?.name ?? conn.edge.targetId}
                  </span>
                  <span className="aef-stat-module__value">{conn.edge.type}</span>
                </div>
              ))}
            </>
          )}

          {/* Labels */}
          {Object.keys(node.labels).length > 0 && (
            <>
              <div className="aef-stat-module__label" style={{ marginTop: 'var(--aef-space-2)' }}>
                Labels
              </div>
              {Object.entries(node.labels).map(([key, value]) => (
                <div key={key} className="aef-stat-module">
                  <span className="aef-stat-module__label">{key}</span>
                  <span className="aef-stat-module__value aef-truncate">{value}</span>
                </div>
              ))}
            </>
          )}
        </div>
      </motion.div>
    </AnimatePresence>
  );
});
