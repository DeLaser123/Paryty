/**
 * Renderer Bridge — connects Zustand topologyStore to PixiTopologyApp
 *
 * Responsibilities:
 *   - Full topology updates (setTopology) with hierarchical layout
 *   - Incremental diff updates (applyDiff)
 *   - Node selection and viewport navigation
 *   - Cluster drill-down/exit
 *   - Preserves node type/status metadata through layout (fixes metadata loss bug)
 *   - Syncs viewport state changes back to the Zustand store
 */

import { type NodeRenderData, type EdgeRenderData } from './pixiApp';
import { createRenderBridge, type RenderBridge } from './renderBridge';
import { getProcessingClient, type ProcessingClient } from './processing/processingClient';
import { HierarchicalLayout, type LayoutNode, type LayoutEdge } from './clustering';
import type { Topology, TopologyNode, TopologyEdge, TopologyDiff } from '../types/topology';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Configuration for creating the renderer. */
export interface RendererConfig {
  /** Parent DOM element to mount the canvas into. */
  container: HTMLElement;
  /** Canvas width in pixels. */
  width: number;
  /** Canvas height in pixels. */
  height: number;
}

/** Stored metadata for a node (preserved through layout). */
interface NodeMeta {
  type: string;
  status: string;
  name: string;
  size: number;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Default node sizes by type (in world units). */
const NODE_SIZES: Record<string, number> = {
  host: 25,
  container: 18,
  service: 20,
  process: 12,
};

/** Default node size for unknown types. */
const DEFAULT_NODE_SIZE = 15;

// ---------------------------------------------------------------------------
// Topology Renderer
// ---------------------------------------------------------------------------

/**
 * Bridge between Zustand topology store and PixiJS rendering engine.
 *
 * Usage:
 * ```ts
 * const renderer = new TopologyRenderer({ container, width, height });
 * renderer.setTopology(topology);         // Full update
 * renderer.applyDiff(diff);               // Incremental update
 * renderer.selectNode('node-1');          // Highlight
 * renderer.zoomToNode('node-1');          // Navigate
 * renderer.resize(1200, 800);             // Container resize
 * ```
 */
export class TopologyRenderer {
  private bridge: RenderBridge;
  private container: HTMLElement;  /** Shared heavy-compute thread (force layout runs here). */
  private readonly processing: ProcessingClient = getProcessingClient();
  /**
   * Monotonic token incremented on every layout request. Async results from a
   * superseded request (stale generation) are discarded so a slow layout can
   * never overwrite a newer one.
   */
  private layoutGeneration = 0;

  /** Hierarchical layout engine. */
  private hierarchicalLayout: HierarchicalLayout;

  /** Stored node metadata (preserved through layout). */
  private nodeMeta: Map<string, NodeMeta> = new Map();

  /** Current edge data (for incremental updates). */
  private currentEdges: Map<string, TopologyEdge> = new Map();

  /** Current topology reference. */
  private currentTopology: Topology | null = null;

  /**
   * SHA-1-style fingerprint of the current topology structure (sorted node
   * IDs + edge pairs). Layout recomputation is skipped when this matches the
   * incoming topology — i.e. when only metric/status metadata changed but no
   * nodes or edges were added/removed. This prevents the 5-second polling
   * interval from triggering an expensive layout pass on every poll cycle.
   */
  private structureFingerprint: string = '';

  /** Current cluster breadcrumb for drill-down navigation. */
  private clusterStack: string[] = [];

  /** Flag indicating the renderer has been destroyed. */
  private disposed = false;

  constructor(config: RendererConfig) {
    this.container = config.container;
    this.hierarchicalLayout = new HierarchicalLayout({
      groupBy: 'type',
      width: config.width,
      height: config.height,
    });

    // Select the render backend: OffscreenCanvas worker ("render thread") when
    // supported, falling back to inline main-thread rendering otherwise.
    this.bridge = createRenderBridge({
      width: config.width,
      height: config.height,
    });

    this.bridge.mount(this.container);

    // Sync viewport changes back to the store
    this.bridge.onViewportChange((_state) => {
      // Viewport state is available for store sync if needed
    });
  }

  /**
   * Sets a complete topology. Runs hierarchical layout and renders all nodes/edges.
   *
   * Layout recomputation is skipped when the topology structure (node IDs and
   * edge connections) has not changed since the last call — i.e. when the REST
   * poll returned the same graph with only metric/status deltas. In that case
   * only the metadata map is updated and re-sent to the renderer so health
   * status colours stay current without paying the layout cost.
   *
   * @param topology - Full topology data
   */
  setTopology(topology: Topology): void {
    if (this.disposed) return;

    const incomingFingerprint = TopologyRenderer.computeStructureFingerprint(topology);
    const structureUnchanged = incomingFingerprint === this.structureFingerprint;

    this.currentTopology = topology;

    // Always refresh the metadata map (status, name, size can change on every poll)
    for (const node of topology.nodes) {
      this.nodeMeta.set(node.id, {
        type: node.type,
        status: node.status,
        name: node.name,
        size: this.getNodeSize(node),
      });
    }

    if (structureUnchanged && this.structureFingerprint !== '') {
      // Structure identical — push only metadata-refreshed render data so
      // node colours and labels stay current, but skip edge redraw entirely
      // (edges don't change on status-only polls) and don't run layout.
      this.reEmitMetadataOnly();
      return;
    }

    this.structureFingerprint = incomingFingerprint;

    // Build layout data preserving type/status
    const layoutNodes: LayoutNode[] = topology.nodes.map((n) => ({
      id: n.id,
      type: n.type,
      labels: n.labels,
      size: this.getNodeSize(n),
    }));

    const layoutEdges: LayoutEdge[] = topology.edges.map((e) => ({
      source: e.sourceId,
      target: e.targetId,
      type: e.type,
    }));

    // Run hierarchical layout
    this.runLayout(layoutNodes, layoutEdges, topology.edges);
  }

  /**
   * Backward-compatible alias for setTopology().
   * @deprecated Use setTopology() instead.
   */
  update(topology: Topology): void {
    this.setTopology(topology);
  }

  /**
   * Applies an incremental topology diff. Updates only changed nodes/edges.
   *
   * @param diff - Topology diff containing added/removed/updated nodes and edges
   */
  applyDiff(diff: TopologyDiff): void {
    if (this.disposed || !this.currentTopology) return;

    // Update stored topology
    const nodeMap = new Map(this.currentTopology.nodes.map((n) => [n.id, n]));
    const edgeMap = new Map(this.currentTopology.edges.map((e) => [e.id, e]));

    for (const id of diff.removedNodes) {
      nodeMap.delete(id);
      this.nodeMeta.delete(id);
    }
    for (const node of diff.addedNodes) {
      nodeMap.set(node.id, node);
      this.nodeMeta.set(node.id, {
        type: node.type,
        status: node.status,
        name: node.name,
        size: this.getNodeSize(node),
      });
    }
    for (const node of diff.updatedNodes) {
      nodeMap.set(node.id, node);
      this.nodeMeta.set(node.id, {
        type: node.type,
        status: node.status,
        name: node.name,
        size: this.getNodeSize(node),
      });
    }

    for (const id of diff.removedEdges) {
      edgeMap.delete(id);
      this.currentEdges.delete(id);
    }
    for (const edge of diff.addedEdges) {
      edgeMap.set(edge.id, edge);
      this.currentEdges.set(edge.id, edge);
    }
    for (const edge of diff.updatedEdges) {
      edgeMap.set(edge.id, edge);
      this.currentEdges.set(edge.id, edge);
    }

    this.currentTopology = {
      nodes: Array.from(nodeMap.values()),
      edges: Array.from(edgeMap.values()),
      timestamp: diff.timestamp,
      version: diff.newVersion,
    };

    // Re-run full layout with updated topology
    this.setTopology(this.currentTopology);
  }

  /**
   * Highlights a specific node. Delegates to PixiTopologyApp.
   *
   * @param id - Node ID to select, or null to deselect
   */
  selectNode(id: string | null): void {
    this.bridge.selectNode(id);
  }

  /**
   * Zooms the viewport to center on a specific node.
   *
   * @param id - Node ID to zoom to
   */
  zoomToNode(id: string): void {
    this.bridge.zoomToNode(id);
  }

  /**
   * Drills into a cluster. Pushes the current cluster onto the breadcrumb
   * stack and re-renders only the nodes in that cluster.
   *
   * @param clusterId - Cluster identifier to drill into
   */
  enterCluster(clusterId: string): void {
    this.clusterStack.push(clusterId);
    // Re-render filtered view
    if (this.currentTopology) {
      this.setTopology(this.currentTopology);
    }
  }

  /**
   * Drills out of the current cluster. Pops the breadcrumb stack
   * and re-renders the parent view.
   */
  exitCluster(): void {
    if (this.clusterStack.length === 0) return;
    this.clusterStack.pop();
    // Re-render parent view
    if (this.currentTopology) {
      this.setTopology(this.currentTopology);
    }
  }

  /**
   * Handles container resize. Updates both the PIXI renderer and layout dimensions.
   *
   * @param width - New width in pixels
   * @param height - New height in pixels
   */
  resize(width: number, height: number): void {
    if (this.disposed) return;

    this.bridge.resize(width, height);

    // Update layout dimensions
    this.hierarchicalLayout = new HierarchicalLayout({
      groupBy: 'type',
      width,
      height,
    });

    // Invalidate fingerprint — canvas dimensions changed so layout must rerun.
    this.structureFingerprint = '';

    // Re-run layout if topology is loaded
    if (this.currentTopology) {
      this.setTopology(this.currentTopology);
    }
  }

  /**
   * Forwards the current memory budget level to the render bridge.
   *
   * The main thread calls this after its own MemoryBudget sensor fires.
   * The bridge implementation propagates it into the renderer (inline or
   * worker) so the particle system and effect manager apply degradation even
   * when performance.memory is unavailable in the worker context.
   */
  sendBudgetLevel(level: 'normal' | 'soft' | 'hard'): void {
    if (!this.disposed) {
      this.bridge.sendBudgetLevel(level);
    }
  }

  /**
   * Destroys the renderer and frees all resources.
   */
  destroy(): void {
    if (this.disposed) return;
    this.disposed = true;

    // Supersede any in-flight layout so a late result is ignored.
    this.layoutGeneration++;
    this.bridge.destroy();
    this.nodeMeta.clear();
    this.currentEdges.clear();
    this.structureFingerprint = '';
  }

  // ---------------------------------------------------------------------------
  // Private — Structure fingerprint
  // ---------------------------------------------------------------------------

  /**
   * Computes a deterministic string fingerprint of the topology structure
   * (node IDs and edge source→target pairs), ignoring metric/status values.
   *
   * Sorted before joining so insertion order does not affect equality.
   * Pure function — takes the topology as argument to make it testable.
   */
  static computeStructureFingerprint(topology: Topology): string {
    const nodeIds = topology.nodes.map((n) => n.id).sort().join(',');
    const edgePairs = topology.edges
      .map((e) => `${e.sourceId}>${e.targetId}`)
      .sort()
      .join(',');
    return `${nodeIds}|${edgePairs}`;
  }

  /**
   * Re-sends node metadata (status, label, type, size) with positions preserved.
   * Called when structure is unchanged so health glow colors stay current
   * without rerunning layout or redrawing edges (Task 12).
   *
   * Edges are skipped entirely — they don't change on status-only polls.
   */
  private reEmitMetadataOnly(): void {
    if (this.nodeMeta.size === 0) return;

    // Build node data from stored metadata. Positions are 0,0 placeholders;
    // the renderer preserves last-computed positions for unchanged node IDs.
    const nodeData = Array.from(this.nodeMeta.entries()).map(([id, meta]) => ({
      id,
      x: -0,  // negative zero signals "use existing position"
      y: -0,
      type: meta.type,
      status: meta.status,
      label: meta.name,
      size: meta.size,
    }));

    // Skip edges entirely — they don't change on status-only polls
    this.bridge.updateNodes(nodeData);
    // NO updateEdges call — single Graphics is rebuilt only on structure change
  }

  // ---------------------------------------------------------------------------
  // Private — Layout
  // ---------------------------------------------------------------------------

  /**
   * Runs the layout algorithm. Uses the synchronous HierarchicalLayout for
   * small topologies (≤50 nodes) and offloads larger ones to the shared
   * processing thread, falling back to the synchronous layout on failure.
   */
  private runLayout(
    nodes: LayoutNode[],
    edges: LayoutEdge[],
    topologyEdges: TopologyEdge[],
  ): void {
    // For small topologies, run layout synchronously
    if (nodes.length <= 50) {
      this.applyPositions(this.computeSyncPositions(nodes, edges), topologyEdges);
      return;
    }

    // For larger topologies, offload to the processing thread.
    const generation = ++this.layoutGeneration;
    const { width, height } = this.bridge.getViewSize();

    this.processing
      .layout({
        nodes: nodes.map((n) => ({
          id: n.id,
          type: n.type,
          status: this.nodeMeta.get(n.id)?.status ?? 'unknown',
        })),
        edges: edges.map((e) => ({ source: e.source, target: e.target, type: e.type })),
        width,
        height,
        iterations: 100,
      })
      .then((result) => {
        // Ignore results from a superseded request or after disposal.
        if (this.disposed || generation !== this.layoutGeneration) return;
        this.applyPositions(result.nodes, topologyEdges);
      })
      .catch(() => {
        if (this.disposed || generation !== this.layoutGeneration) return;
        // Fallback: synchronous layout.
        this.applyPositions(this.computeSyncPositions(nodes, edges), topologyEdges);
      });
  }

  /** Computes positions synchronously via the hierarchical layout engine. */
  private computeSyncPositions(
    nodes: LayoutNode[],
    edges: LayoutEdge[],
  ): { id: string; x: number; y: number; type: string; status: string }[] {
    const positions = this.hierarchicalLayout.compute(nodes, edges);
    return nodes.map((n) => ({
      id: n.id,
      x: positions.get(n.id)?.x ?? 0,
      y: positions.get(n.id)?.y ?? 0,
      type: n.type,
      status: this.nodeMeta.get(n.id)?.status ?? 'unknown',
    }));
  }

  /**
   * Applies computed positions to the renderer.
   * Preserves type/status metadata from stored node data (fixes metadata loss bug).
   */
  private applyPositions(
    positions: { id: string; x: number; y: number; type?: string; status?: string }[],
    topologyEdges: TopologyEdge[],
  ): void {
    // Build position map
    const posMap = new Map(positions.map((p) => [p.id, p]));

    // Build node render data preserving all metadata
    const nodeData: NodeRenderData[] = [];
    for (const [id, meta] of this.nodeMeta) {
      const pos = posMap.get(id);
      nodeData.push({
        id,
        x: pos?.x ?? 0,
        y: pos?.y ?? 0,
        type: meta.type,
        status: meta.status,
        label: meta.name,
        size: meta.size,
      });
    }

    // Build edge render data
    const edgeData: EdgeRenderData[] = topologyEdges.map((e) => {
      const srcPos = posMap.get(e.sourceId);
      const tgtPos = posMap.get(e.targetId);
      return {
        id: e.id,
        sourceX: srcPos?.x ?? 0,
        sourceY: srcPos?.y ?? 0,
        targetX: tgtPos?.x ?? 0,
        targetY: tgtPos?.y ?? 0,
        type: e.type,
        throughput: e.bytesPerSec ? Math.min(1, e.bytesPerSec / 1_000_000) : 0,
      };
    });

    // Render
    this.bridge.updateNodes(nodeData);
    this.bridge.updateEdges(edgeData);

    // Store current edges for incremental updates
    for (const edge of topologyEdges) {
      this.currentEdges.set(edge.id, edge);
    }
  }

  /**
   * Computes node size from TopologyNode type.
   */
  private getNodeSize(node: TopologyNode): number {
    return NODE_SIZES[node.type] ?? DEFAULT_NODE_SIZE;
  }
}
