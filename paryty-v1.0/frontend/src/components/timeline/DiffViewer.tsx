/**
 * DiffViewer — Side-by-side comparison of two timeline snapshots.
 *
 * Shows "From" and "To" snapshots using container card pattern.
 * Highlights added/removed/changed nodes.
 *
 * @module components/timeline/DiffViewer
 */

import { memo, useMemo } from 'react';
import type { TimelineSnapshot } from '../../types/timeline';
import { DetachableCard } from '../common/DetachableCard';
import { GitBranch } from 'lucide-react';

/** Props for DiffViewer. */
interface DiffViewerProps {
  /** The source snapshot. */
  from: TimelineSnapshot;
  /** The target snapshot. */
  to: TimelineSnapshot;
}

/**
 * Computes differences between two snapshots.
 */
function computeDiff(from: TimelineSnapshot, to: TimelineSnapshot) {
  const fromNodeIds = new Set(from.nodes.map((n) => n.id));
  const toNodeIds = new Set(to.nodes.map((n) => n.id));

  const added = to.nodes.filter((n) => !fromNodeIds.has(n.id));
  const removed = from.nodes.filter((n) => !toNodeIds.has(n.id));
  const changed = to.nodes.filter((n) => {
    if (!fromNodeIds.has(n.id)) return false;
    const fromNode = from.nodes.find((fn) => fn.id === n.id);
    return fromNode !== undefined && fromNode.status !== n.status;
  });

  return { added, removed, changed };
}

/**
 * Side-by-side snapshot comparison view.
 *
 * Uses container card pattern for each side.
 * Highlights differences: added (green), removed (red), changed (orange).
 */
export const DiffViewer = memo(function DiffViewer({ from, to }: DiffViewerProps) {
  const diff = useMemo(() => computeDiff(from, to), [from, to]);

  return (
    <div className="diff-viewer" data-testid="diff-viewer">
      {/* Summary */}
      <div className="diff-viewer__summary">
        <span className="aef-badge badge-valid">+{diff.added.length} added</span>
        <span className="aef-badge badge-warning">{diff.changed.length} changed</span>
        <span className="aef-badge badge-pending">-{diff.removed.length} removed</span>
      </div>

      <div className="diff-viewer__panels">
        {/* From snapshot */}
        <DetachableCard
          title="From"
          icon={<GitBranch size={14} />}
          metaLabel={new Date(from.timestamp).toLocaleString()}
          className="diff-viewer__panel"
          testId="diff-from-panel"
        >
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Nodes</span>
            <span className="aef-stat-module__value">{from.nodes.length}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Edges</span>
            <span className="aef-stat-module__value">{from.edges.length}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Alerts</span>
            <span className="aef-stat-module__value">{from.alertCount}</span>
          </div>
          {diff.removed.map((node) => (
            <div key={node.id} className="aef-stat-module" style={{ opacity: 0.5 }}>
              <span className="aef-stat-module__label">-{node.name}</span>
              <span className="aef-stat-module__value">{node.status}</span>
            </div>
          ))}
        </DetachableCard>

        {/* To snapshot */}
        <DetachableCard
          title="To"
          icon={<GitBranch size={14} />}
          metaLabel={new Date(to.timestamp).toLocaleString()}
          className="diff-viewer__panel"
          testId="diff-to-panel"
        >
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Nodes</span>
            <span className="aef-stat-module__value">{to.nodes.length}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Edges</span>
            <span className="aef-stat-module__value">{to.edges.length}</span>
          </div>
          <div className="aef-stat-module">
            <span className="aef-stat-module__label">Alerts</span>
            <span className="aef-stat-module__value">{to.alertCount}</span>
          </div>
          {diff.added.map((node) => (
            <div key={node.id} className="aef-stat-module">
              <span className="aef-stat-module__label" style={{ color: 'var(--aef-status-live)' }}>+{node.name}</span>
              <span className="aef-stat-module__value">{node.status}</span>
            </div>
          ))}
          {diff.changed.map((node) => (
            <div key={node.id} className="aef-stat-module">
              <span className="aef-stat-module__label" style={{ color: 'var(--aef-status-warning)' }}>~{node.name}</span>
              <span className="aef-stat-module__value">{node.status}</span>
            </div>
          ))}
        </DetachableCard>
      </div>
    </div>
  );
});
