/**
 * Hierarchical Clustering Layout for GPU Rendering Engine
 *
 * Groups nodes by a configurable field (service, type, region, layer),
 * arranges clusters in a grid, then runs d3-force simulation within
 * each cluster for optimal sub-graph layout.
 *
 * Algorithm:
 *   1. Group nodes by `groupBy` field (from labels or node type)
 *   2. Arrange cluster centers in a grid pattern
 *   3. Run d3-force within each cluster (many-body repulsion + collision)
 *   4. Constrain nodes to cluster radius bounds
 *   5. Return final positions
 */

import * as d3 from 'd3-force';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Configuration for the hierarchical layout algorithm. */
export interface ClusterConfig {
  /** Field to group nodes by. */
  groupBy: 'service' | 'type' | 'region' | 'layer';
  /** Padding between cluster bounding circles (world units). */
  clusterPadding: number;
  /** Minimum padding between nodes within a cluster (world units). */
  nodePadding: number;
  /** Maximum number of nodes per cluster before splitting. */
  maxClusterSize: number;
  /** Layout area width (world units). */
  width: number;
  /** Layout area height (world units). */
  height: number;
}

/** Input node for layout computation. */
export interface LayoutNode {
  id: string;
  type: string;
  labels: Record<string, string>;
  size: number;
}

/** Input edge for layout computation. */
export interface LayoutEdge {
  source: string;
  target: string;
  type: string;
}

/** Computed cluster with position and member nodes. */
export interface Cluster {
  id: string;
  label: string;
  nodes: string[];
  x: number;
  y: number;
  radius: number;
}

/** Internal node used during simulation (augments LayoutNode with d3-force fields). */
interface SimNode extends d3.SimulationNodeDatum {
  id: string;
  type: string;
  size: number;
  clusterId: string;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Default config values. */
const DEFAULT_CONFIG: ClusterConfig = {
  groupBy: 'type',
  clusterPadding: 100,
  nodePadding: 30,
  maxClusterSize: 50,
  width: 1200,
  height: 800,
};

/** Number of d3-force iterations within each cluster. */
const CLUSTER_ITERATIONS = 80;

// ---------------------------------------------------------------------------
// Hierarchical Layout
// ---------------------------------------------------------------------------

/**
 * Computes a hierarchical clustered layout for a set of topology nodes.
 *
 * Usage:
 * ```ts
 * const layout = new HierarchicalLayout(config);
 * const positions = layout.compute(nodes, edges);
 * const clusters = layout.getClusters();
 * ```
 */
export class HierarchicalLayout {
  private config: ClusterConfig;
  private clusters: Cluster[] = [];

  constructor(config: Partial<ClusterConfig> = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * Computes node positions using hierarchical clustering.
   *
   * @param nodes - Nodes to lay out
   * @param edges - Edges connecting nodes (used for within-cluster attraction)
   * @returns Map from node ID → { x, y } position
   */
  compute(nodes: LayoutNode[], edges: LayoutEdge[]): Map<string, { x: number; y: number }> {
    if (nodes.length === 0) {
      this.clusters = [];
      return new Map();
    }

    // Step 1: Group nodes
    const groups = this.groupNodes(nodes);

    // Step 2: Arrange cluster centers in grid
    this.clusters = this.arrangeClusterCenters(groups);

    // Step 3: Run d3-force within each cluster
    const allPositions = this.layoutWithinClusters(nodes, edges);

    return allPositions;
  }

  /**
   * Returns the computed clusters after `compute()` has been called.
   *
   * @returns Array of Cluster objects with positions and member lists
   */
  getClusters(): Cluster[] {
    return this.clusters;
  }

  /**
   * Returns the cluster a specific node belongs to.
   *
   * @param nodeId - Node identifier
   * @returns Cluster or undefined if not found
   */
  getClusterForNode(nodeId: string): Cluster | undefined {
    return this.clusters.find((c) => c.nodes.includes(nodeId));
  }

  // ---------------------------------------------------------------------------
  // Private — Grouping
  // ---------------------------------------------------------------------------

  /**
   * Groups nodes by the configured `groupBy` field.
   * Falls back to the node type if the label field is missing.
   */
  private groupNodes(
    nodes: LayoutNode[],
  ): Map<string, LayoutNode[]> {
    const groups = new Map<string, LayoutNode[]>();

    for (const node of nodes) {
      let groupKey: string;

      if (this.config.groupBy === 'type') {
        groupKey = node.type;
      } else {
        groupKey = node.labels[this.config.groupBy] ?? node.type;
      }

      let group = groups.get(groupKey);
      if (!group) {
        group = [];
        groups.set(groupKey, group);
      }
      group.push(node);
    }

    // Split oversized clusters
    const finalGroups = new Map<string, LayoutNode[]>();
    for (const [key, groupNodes] of groups) {
      if (groupNodes.length <= this.config.maxClusterSize) {
        finalGroups.set(key, groupNodes);
      } else {
        // Split into sub-clusters
        let partIdx = 0;
        for (let i = 0; i < groupNodes.length; i += this.config.maxClusterSize) {
          const chunk = groupNodes.slice(i, i + this.config.maxClusterSize);
          finalGroups.set(`${key}_part${partIdx}`, chunk);
          partIdx++;
        }
      }
    }

    return finalGroups;
  }

  // ---------------------------------------------------------------------------
  // Private — Grid Layout
  // ---------------------------------------------------------------------------

  /**
   * Arranges cluster centers in a grid pattern within the layout area.
   * Computes each cluster's bounding radius based on node count.
   */
  private arrangeClusterCenters(
    groups: Map<string, LayoutNode[]>,
  ): Cluster[] {
    const clusterList: Cluster[] = [];
    const entries = Array.from(groups.entries());

    // Compute cluster radii
    for (const [id, nodes] of entries) {
      const radius = this.estimateClusterRadius(nodes.length);
      clusterList.push({
        id,
        label: id,
        nodes: nodes.map((n) => n.id),
        x: 0,
        y: 0,
        radius,
      });
    }

    // Arrange in a grid
    const cols = Math.ceil(Math.sqrt(clusterList.length));
    const cellW = this.config.width / cols;
    const cellH = this.config.height / Math.ceil(clusterList.length / cols);

    for (let i = 0; i < clusterList.length; i++) {
      const col = i % cols;
      const row = Math.floor(i / cols);
      clusterList[i].x = cellW * (col + 0.5);
      clusterList[i].y = cellH * (row + 0.5);
    }

    return clusterList;
  }

  /**
   * Estimates the cluster radius based on the number of nodes.
   * Uses a simple area-based heuristic.
   */
  private estimateClusterRadius(nodeCount: number): number {
    const areaPerNode = (this.config.nodePadding * 2) ** 2;
    const totalArea = nodeCount * areaPerNode;
    return Math.max(60, Math.sqrt(totalArea / Math.PI) + this.config.clusterPadding / 2);
  }

  // ---------------------------------------------------------------------------
  // Private — Within-Cluster Layout
  // ---------------------------------------------------------------------------

  /**
   * Runs d3-force simulation within each cluster.
   * Positions nodes relative to the cluster center.
   */
  private layoutWithinClusters(
    nodes: LayoutNode[],
    edges: LayoutEdge[],
  ): Map<string, { x: number; y: number }> {
    const positions = new Map<string, { x: number; y: number }>();
    const nodeMap = new Map(nodes.map((n) => [n.id, n]));
    const clusterLookup = new Map<string, Cluster>();

    for (const cluster of this.clusters) {
      clusterLookup.set(cluster.id, cluster);
    }

    // Build cluster → node mapping
    const clusterNodeMap = new Map<string, SimNode[]>();
    for (const cluster of this.clusters) {
      const simNodes: SimNode[] = cluster.nodes.map((id) => {
        const node = nodeMap.get(id)!;
        return {
          id: node.id,
          type: node.type,
          size: node.size,
          clusterId: cluster.id,
          x: cluster.x + (Math.random() - 0.5) * cluster.radius,
          y: cluster.y + (Math.random() - 0.5) * cluster.radius,
        };
      });
      clusterNodeMap.set(cluster.id, simNodes);
    }

    // Build edge lookup for within-cluster links
    const nodeToCluster = new Map<string, string>();
    for (const cluster of this.clusters) {
      for (const nodeId of cluster.nodes) {
        nodeToCluster.set(nodeId, cluster.id);
      }
    }

    // Run simulation per cluster
    for (const cluster of this.clusters) {
      const simNodes = clusterNodeMap.get(cluster.id) ?? [];
      if (simNodes.length === 0) continue;

      // Filter edges within this cluster
      const simNodeIds = new Set(simNodes.map((n) => n.id));
      const simNodeIdx = new Map(simNodes.map((n, i) => [n.id, i]));
      const clusterEdges = edges
        .filter((e) => simNodeIds.has(e.source) && simNodeIds.has(e.target))
        .map((e) => ({
          source: simNodeIdx.get(e.source) as unknown as SimNode,
          target: simNodeIdx.get(e.target) as unknown as SimNode,
        }));

      // Create simulation
      const sim = d3.forceSimulation<SimNode>(simNodes)
        .force('charge', d3.forceManyBody<SimNode>().strength(-200))
        .force('collision', d3.forceCollide<SimNode>().radius((d) => d.size + this.config.nodePadding))
        .force('center', d3.forceCenter(cluster.x, cluster.y))
        .force('x', d3.forceX<SimNode>(cluster.x).strength(0.05))
        .force('y', d3.forceY<SimNode>(cluster.y).strength(0.05))
        .alphaDecay(0.02)
        .velocityDecay(0.4);

      if (clusterEdges.length > 0) {
        sim.force('link', d3.forceLink<SimNode, { source: SimNode; target: SimNode }>(clusterEdges)
          .id((d) => d.id)
          .distance(80)
          .strength(0.3),
        );
      }

      // Run simulation
      sim.tick(CLUSTER_ITERATIONS);
      sim.stop();

      // Constrain to cluster bounds and collect positions
      for (const simNode of simNodes) {
        const dx = (simNode.x ?? cluster.x) - cluster.x;
        const dy = (simNode.y ?? cluster.y) - cluster.y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        if (dist > cluster.radius) {
          // Clamp to cluster boundary
          const scale = cluster.radius / dist;
          simNode.x = cluster.x + dx * scale;
          simNode.y = cluster.y + dy * scale;
        }

        positions.set(simNode.id, {
          x: simNode.x ?? cluster.x,
          y: simNode.y ?? cluster.y,
        });
      }
    }

    return positions;
  }
}
