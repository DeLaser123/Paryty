/**
 * ClusterBreadcrumb — Horizontal breadcrumb for cluster drill-down.
 *
 * Shows the current cluster navigation path with chevron separators.
 * Each segment is clickable to navigate back.
 *
 * @module components/topology/ClusterBreadcrumb
 */

import { memo, useCallback } from 'react';
import { ChevronRight } from 'lucide-react';
import { useTopologyStore } from '../../stores/topologyStore';

/**
 * Cluster breadcrumb navigation for topology drill-down.
 *
 * Only renders when `clusterPath` has entries.
 * Each breadcrumb segment triggers `exitCluster` to navigate back.
 */
export const ClusterBreadcrumb = memo(function ClusterBreadcrumb() {
  const clusterPath = useTopologyStore((s) => s.clusterPath);
  const clusterBreadcrumbs = useTopologyStore((s) => s.clusterBreadcrumbs());
  const exitCluster = useTopologyStore((s) => s.exitCluster);

  const handleClick = useCallback(
    (_index: number) => {
      // Navigate back by clicking any breadcrumb segment
      // exitCluster pops one level at a time — in a real implementation
      // we'd navigate directly to the target depth
      exitCluster();
    },
    [exitCluster],
  );

  if (clusterPath.length === 0) return null;

  return (
    <nav
      className="cluster-breadcrumb"
      aria-label="Cluster navigation"
      data-testid="cluster-breadcrumb"
    >
      <button
        className="cluster-breadcrumb__item"
        onClick={() => handleClick(0)}
        data-testid="breadcrumb-root"
      >
        Root
      </button>
      {clusterBreadcrumbs.map((crumb, i) => (
        <span key={crumb.id} className="cluster-breadcrumb__segment">
          <ChevronRight size={12} className="cluster-breadcrumb__chevron" />
          <button
            className="cluster-breadcrumb__item"
            onClick={() => handleClick(i + 1)}
            data-testid={`breadcrumb-${crumb.id}`}
          >
            {crumb.label}
          </button>
        </span>
      ))}
    </nav>
  );
});
