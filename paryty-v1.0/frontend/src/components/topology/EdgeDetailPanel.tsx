/**
 * EdgeDetailPanel — Slide-out panel for selected topology edge.
 *
 * Shows source→target, throughput, latency, protocol, bytes/sec.
 * Uses stat module pattern from the design system.
 *
 * @module components/topology/EdgeDetailPanel
 */

import { memo, useMemo } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { X, ArrowRight, Zap, Clock, Network } from 'lucide-react';
import type { TopologyEdge } from '../../types/topology';
import { useTopologyStore } from '../../stores/topologyStore';

/** Props for EdgeDetailPanel. */
interface EdgeDetailPanelProps {
  /** The selected edge to display details for. */
  edge: TopologyEdge;
  /** Callback when the panel close button is clicked. */
  onClose: () => void;
}

/**
 * Slide-out detail panel for a selected topology edge.
 *
 * Uses container card pattern from the design system.
 * Animated with framer-motion slide-in from right.
 */
export const EdgeDetailPanel = memo(function EdgeDetailPanel({
  edge,
  onClose,
}: EdgeDetailPanelProps) {
  const topology = useTopologyStore((s) => s.topology);

  const sourceNode = useMemo(
    () => topology?.nodes.find((n) => n.id === edge.sourceId),
    [topology, edge.sourceId],
  );

  const targetNode = useMemo(
    () => topology?.nodes.find((n) => n.id === edge.targetId),
    [topology, edge.targetId],
  );

  return (
    <AnimatePresence>
      <motion.div
        className="detail-panel aef-container-card"
        initial={{ x: '100%', opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        exit={{ x: '100%', opacity: 0 }}
        transition={{ duration: 0.2, ease: [0.2, 0, 0, 1] }}
        data-testid="edge-detail-panel"
      >
        {/* Header */}
        <div className="aef-container-card__header">
          <div className="aef-container-card__icon">
            <Network size={16} />
          </div>
          <span className="aef-container-card__title aef-truncate">
            {sourceNode?.name ?? edge.sourceId}
            <ArrowRight size={12} style={{ margin: '0 4px', display: 'inline' }} />
            {targetNode?.name ?? edge.targetId}
          </span>
          <button
            className="aef-modal-close"
            onClick={onClose}
            aria-label="Close panel"
            data-testid="edge-detail-close"
          >
            <X size={16} />
          </button>
        </div>

        {/* Body */}
        <div className="aef-container-card__body aef-scroll">
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Type</span>
            <span className="aef-stat-module__value">{edge.type}</span>
          </div>

          {edge.protocol && (
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">
                <Zap size={12} style={{ display: 'inline', marginRight: 4 }} />
                Protocol
              </span>
              <span className="aef-stat-module__value">{edge.protocol}</span>
            </div>
          )}

          {edge.latencyMs !== undefined && (
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">
                <Clock size={12} style={{ display: 'inline', marginRight: 4 }} />
                Latency
              </span>
              <span className="aef-stat-module__value">{edge.latencyMs.toFixed(1)} ms</span>
            </div>
          )}

          {edge.bytesPerSec !== undefined && (
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Throughput</span>
              <span className="aef-stat-module__value">{formatBytes(edge.bytesPerSec)}/s</span>
            </div>
          )}

          {edge.errorRate !== undefined && (
            <div className="aef-stat-module">
              <span className="aef-stat-module__label">Error Rate</span>
              <span className="aef-stat-module__value">{(edge.errorRate * 100).toFixed(2)}%</span>
            </div>
          )}

          {/* Metadata */}
          {Object.keys(edge.metadata).length > 0 && (
            <>
              <div className="aef-stat-module__label" style={{ marginTop: 'var(--aef-space-2)' }}>
                Metadata
              </div>
              {Object.entries(edge.metadata).map(([key, value]) => (
                <div key={key} className="aef-stat-module">
                  <span className="aef-stat-module__label">{key}</span>
                  <span className="aef-stat-module__value aef-truncate">{String(value)}</span>
                </div>
              ))}
            </>
          )}
        </div>
      </motion.div>
    </AnimatePresence>
  );
});

/** Format bytes to human-readable string. */
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`;
}
