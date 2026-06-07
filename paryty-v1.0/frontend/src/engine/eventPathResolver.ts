/**
 * EventPathResolver — Multi-Hop Path Resolution for Semantic Particles
 *
 * Given a ParytyEvent and the current Topology, resolves a multi-hop path
 * that the particle will traverse through the topology graph.
 *
 * Algorithm:
 *   1. Match event to source node (via event.source or event.labels['node_id'])
 *   2. Find outgoing edges from the source node
 *   3. Traverse up to 3 hops, prioritizing edge types: calls > network > dependency > contains
 *   4. Build an ordered list of EventPathSegment with world-space coordinates
 *   5. If no source node found → generate a fallback single-segment path
 */

import type { ParytyEvent } from '../types/event';
import type { Topology, TopologyNode, TopologyEdge } from '../types/topology';
import type { EventPathSegment } from '../types/particle';

// ─── Constants ──────────────────────────────────────────────────

/** Maximum number of hops in a multi-hop path. */
const MAX_HOPS = 3;

/** Edge type priority for path traversal (lower = higher priority). */
const EDGE_TYPE_PRIORITY: Record<string, number> = {
  calls: 0,
  network: 1,
  dependency: 2,
  contains: 3,
};

// ─── EventPathResolver ──────────────────────────────────────────

export class EventPathResolver {
  /** Adjacency list cache: nodeId → outgoing TopologyEdge[] (rebuilt each resolve). */
  private adjacencyCache: Map<string, TopologyEdge[]> = new Map();

  /**
   * Builds a multi-hop path for an event given the current topology state.
   *
   * @param event - The ParytyEvent to resolve a path for.
   * @param topology - Current topology graph (nodes + edges).
   * @param nodePositions - World-space positions of all nodes (Map<nodeId, {x, y}>).
   * @returns An ordered list of path segments the particle will traverse.
   */
  resolvePath(
    event: ParytyEvent,
    topology: Topology,
    nodePositions: Map<string, { x: number; y: number }>,
  ): EventPathSegment[] {
    // Step 1: Build adjacency cache for this topology
    this.buildAdjacencyCache(topology);

    // Step 2: Find the source node
    const sourceNode = this.findSourceNode(event, topology);
    if (!sourceNode) {
      // Fallback: path from topology centroid to a random node
      return this.fallbackPath(topology, nodePositions);
    }

    // Step 3: Traverse the graph starting from the source node
    const segments = this.traverse(sourceNode, topology, nodePositions, event);

    // If traversal produces 0 segments (no outgoing edges), generate a
    // minimal single-segment path at the source node position
    if (segments.length === 0) {
      const pos = nodePositions.get(sourceNode.id);
      if (pos) {
        return [{
          fromNodeId: sourceNode.id,
          toNodeId: sourceNode.id,
          edgeId: `terminal-${sourceNode.id}`,
          fromX: pos.x,
          fromY: pos.y,
          toX: pos.x,
          toY: pos.y,
        }];
      }
    }

    return segments;
  }

  // ─── Private Helpers ──────────────────────────────────────────

  /**
   * Builds an adjacency list from topology edges.
   * Maps source node ID → array of outgoing edges.
   */
  private buildAdjacencyCache(topology: Topology): void {
    this.adjacencyCache.clear();
    for (const edge of topology.edges) {
      const existing = this.adjacencyCache.get(edge.sourceId);
      if (existing) {
        existing.push(edge);
      } else {
        this.adjacencyCache.set(edge.sourceId, [edge]);
      }
    }
  }

  /**
   * Finds the source node that best matches the event.
   * Priority: event.labels['node_id'] → event.source → node name match.
   */
  private findSourceNode(
    event: ParytyEvent,
    topology: Topology,
  ): TopologyNode | null {
    // Priority 1: Explicit node_id in labels
    const nodeId = event.labels?.['node_id'];
    if (nodeId) {
      const node = topology.nodes.find((n) => n.id === nodeId);
      if (node) return node;
    }

    // Priority 2: event.source matches node name or id
    if (event.source) {
      const node = topology.nodes.find(
        (n) => n.id === event.source || n.name === event.source,
      );
      if (node) return node;
    }

    // Priority 3: relatedEntityId / relatedEntityType match
    if (event.relatedEntityType === 'node' && event.relatedEntityId) {
      const node = topology.nodes.find((n) => n.id === event.relatedEntityId);
      if (node) return node;
    }

    return null;
  }

  /**
   * Traverses the topology graph starting from sourceNode.
   * Follows outgoing edges up to MAX_HOPS, prioritizing by edge type.
   * Builds a world-space path from the node positions map.
   */
  private traverse(
    sourceNode: TopologyNode,
    topology: Topology,
    nodePositions: Map<string, { x: number; y: number }>,
    _event: ParytyEvent,
  ): EventPathSegment[] {
    const segments: EventPathSegment[] = [];
    const visited = new Set<string>();
    visited.add(sourceNode.id);

    let currentNode = sourceNode;
    let hopCount = 0;

    while (hopCount < MAX_HOPS) {
      // Get outgoing edges from the current node
      const outgoing = this.adjacencyCache.get(currentNode.id);
      if (!outgoing || outgoing.length === 0) break;

      // Filter to unvisited targets and sort by edge type priority
      const candidates = outgoing
        .filter((e) => !visited.has(e.targetId))
        .sort(
          (a, b) =>
            (EDGE_TYPE_PRIORITY[a.type] ?? 99) -
            (EDGE_TYPE_PRIORITY[b.type] ?? 99),
        );

      if (candidates.length === 0) break;

      // Pick the first (highest priority) edge
      const nextEdge = candidates[0];
      visited.add(nextEdge.targetId);

      // Find target node
      const targetNode = topology.nodes.find((n) => n.id === nextEdge.targetId);
      if (!targetNode) break;

      // Get world-space positions
      const fromPos = nodePositions.get(currentNode.id);
      const toPos = nodePositions.get(targetNode.id);

      if (fromPos && toPos) {
        segments.push({
          fromNodeId: currentNode.id,
          toNodeId: targetNode.id,
          edgeId: nextEdge.id,
          fromX: fromPos.x,
          fromY: fromPos.y,
          toX: toPos.x,
          toY: toPos.y,
        });
      }

      // Advance to the target node
      currentNode = targetNode;
      hopCount++;
    }

    return segments;
  }

  /**
   * Generates a fallback path when the event cannot be matched to any node.
   * Creates a single-segment path near the topology centroid.
   */
  private fallbackPath(
    topology: Topology,
    nodePositions: Map<string, { x: number; y: number }>,
  ): EventPathSegment[] {
    // Compute topology centroid from node positions
    let cx = 0;
    let cy = 0;
    let count = 0;

    for (const node of topology.nodes) {
      const pos = nodePositions.get(node.id);
      if (pos) {
        cx += pos.x;
        cy += pos.y;
        count++;
      }
    }

    if (count === 0) {
      // No nodes at all — return a tiny segment at origin
      return [{
        fromNodeId: 'fallback',
        toNodeId: 'fallback',
        edgeId: 'fallback',
        fromX: 0,
        fromY: 0,
        toX: 0,
        toY: 0,
      }];
    }

    cx /= count;
    cy /= count;

    return [{
      fromNodeId: 'fallback',
      toNodeId: 'fallback',
      edgeId: 'fallback',
      fromX: cx,
      fromY: cy,
      toX: cx + 20,
      toY: cy + 20,
    }];
  }
}
