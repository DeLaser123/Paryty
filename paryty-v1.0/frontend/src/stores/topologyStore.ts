import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Topology, TopologyNode, TopologyEdge, TopologyDiff } from '../types/topology';

// ─── Viewport ──────────────────────────────────────────────────

export interface ViewportState {
  zoom: number;
  panX: number;
  panY: number;
}

// ─── Layout Mode ───────────────────────────────────────────────

export type LayoutMode = 'force' | 'hierarchical' | 'radial';

// ─── Cluster Navigation ────────────────────────────────────────

export interface ClusterBreadcrumb {
  id: string;
  label: string;
}

// ─── Store Interface ───────────────────────────────────────────

interface TopologyState {
  // Data
  topology: Topology | null;
  selectedNode: TopologyNode | null;
  selectedEdge: TopologyEdge | null;
  hoveredNode: TopologyNode | null;
  searchQuery: string;
  filterTypes: string[];
  isLoading: boolean;
  error: string | null;
  version: string | null;

  // Viewport
  viewport: ViewportState;
  setViewport: (partial: Partial<ViewportState>) => void;

  // Cluster Navigation
  activeClusterId: string | null;
  clusterPath: string[];
  expandedClusters: Set<string>;
  enterCluster: (clusterId: string) => void;
  exitCluster: () => void;
  toggleExpandCluster: (clusterId: string) => void;

  // Layout State
  layoutMode: LayoutMode;
  layoutRunning: boolean;
  setLayoutMode: (mode: LayoutMode) => void;
  setLayoutRunning: (running: boolean) => void;

  // Actions
  setTopology: (topology: Topology) => void;
  applyDiff: (diff: TopologyDiff) => void;
  selectNode: (node: TopologyNode | null) => void;
  selectEdge: (edge: TopologyEdge | null) => void;
  hoverNode: (node: TopologyNode | null) => void;
  setSearchQuery: (query: string) => void;
  setFilterTypes: (types: string[]) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  zoomToNode: (nodeId: string) => void;

  // Computed
  filteredNodes: () => TopologyNode[];
  filteredEdges: () => TopologyEdge[];
  currentClusterNodes: () => TopologyNode[];
  clusterBreadcrumbs: () => ClusterBreadcrumb[];
}

export const useTopologyStore = create<TopologyState>()(
  subscribeWithSelector((set, get) => ({
    topology: null,
    selectedNode: null,
    selectedEdge: null,
    hoveredNode: null,
    searchQuery: '',
    filterTypes: [],
    isLoading: false,
    error: null,
    version: null,

    // Viewport defaults
    viewport: { zoom: 1, panX: 0, panY: 0 },
    setViewport: (partial) =>
      set((state) => ({
        viewport: { ...state.viewport, ...partial },
      })),

    // Cluster Navigation defaults
    activeClusterId: null,
    clusterPath: [],
    expandedClusters: new Set<string>(),

    enterCluster: (clusterId) =>
      set((state) => ({
        activeClusterId: clusterId,
        clusterPath: [...state.clusterPath, clusterId],
      })),

    exitCluster: () =>
      set((state) => {
        const newPath = state.clusterPath.slice(0, -1);
        return {
          activeClusterId: newPath.length > 0 ? newPath[newPath.length - 1] : null,
          clusterPath: newPath,
        };
      }),

    toggleExpandCluster: (clusterId) =>
      set((state) => {
        const next = new Set(state.expandedClusters);
        if (next.has(clusterId)) {
          next.delete(clusterId);
        } else {
          next.add(clusterId);
        }
        return { expandedClusters: next };
      }),

    // Layout State defaults
    layoutMode: 'force',
    layoutRunning: false,
    setLayoutMode: (mode) => set({ layoutMode: mode }),
    setLayoutRunning: (running) => set({ layoutRunning: running }),

    // Existing actions (preserved)
    setTopology: (topology) =>
      set({ topology, version: topology.version, error: null }),

    applyDiff: (diff) =>
      set((state) => {
        if (!state.topology) return state;
        const nodeMap = new Map(state.topology.nodes.map((n) => [n.id, n]));
        const edgeMap = new Map(state.topology.edges.map((e) => [e.id, e]));

        for (const id of diff.removedNodes) nodeMap.delete(id);
        for (const node of diff.addedNodes) nodeMap.set(node.id, node);
        for (const node of diff.updatedNodes) nodeMap.set(node.id, node);

        for (const id of diff.removedEdges) edgeMap.delete(id);
        for (const edge of diff.addedEdges) edgeMap.set(edge.id, edge);
        for (const edge of diff.updatedEdges) edgeMap.set(edge.id, edge);

        return {
          topology: {
            nodes: Array.from(nodeMap.values()),
            edges: Array.from(edgeMap.values()),
            timestamp: diff.timestamp,
            version: diff.newVersion,
          },
          version: diff.newVersion,
        };
      }),

    selectNode: (node) => set({ selectedNode: node, selectedEdge: null }),
    selectEdge: (edge) => set({ selectedEdge: edge, selectedNode: null }),
    hoverNode: (node) => set({ hoveredNode: node }),
    setSearchQuery: (query) => set({ searchQuery: query }),
    setFilterTypes: (types) => set({ filterTypes: types }),
    setLoading: (loading) => set({ isLoading: loading }),
    setError: (error) => set({ error }),

    /**
     * Zoom and pan the viewport to center on a specific node.
     * Sets zoom to 2x and pans to node position.
     */
    zoomToNode: (nodeId) => {
      const { topology } = get();
      if (!topology) return;
      const node = topology.nodes.find((n) => n.id === nodeId);
      if (!node) return;
      // Node positions are derived from layout — zoom to 2x centered on origin
      // Actual position lookup happens in the renderer via metadata
      const meta = node.metadata as Record<string, unknown>;
      const x = (typeof meta['x'] === 'number' ? meta['x'] : 0);
      const y = (typeof meta['y'] === 'number' ? meta['y'] : 0);
      set({
        viewport: { zoom: 2, panX: -x * 2, panY: -y * 2 },
        selectedNode: node,
      });
    },

    // Existing computed (preserved)
    filteredNodes: () => {
      const { topology, searchQuery, filterTypes } = get();
      if (!topology) return [];
      let nodes = topology.nodes;
      if (searchQuery) {
        const q = searchQuery.toLowerCase();
        nodes = nodes.filter(
          (n) =>
            n.name.toLowerCase().includes(q) ||
            n.id.toLowerCase().includes(q) ||
            Object.values(n.labels).some((v) => v.toLowerCase().includes(q)),
        );
      }
      if (filterTypes.length > 0) {
        nodes = nodes.filter((n) => filterTypes.includes(n.type));
      }
      return nodes;
    },

    filteredEdges: () => {
      const { topology, filterTypes } = get();
      if (!topology) return [];
      if (filterTypes.length === 0) return topology.edges;
      const nodeIds = new Set(
        get()
          .filteredNodes()
          .map((n) => n.id),
      );
      return topology.edges.filter(
        (e) => nodeIds.has(e.sourceId) || nodeIds.has(e.targetId),
      );
    },

    /**
     * Returns nodes filtered by the active cluster.
     * When no cluster is active, returns all filtered nodes.
     */
    currentClusterNodes: () => {
      const { activeClusterId, topology } = get();
      const baseNodes = get().filteredNodes();
      if (!activeClusterId || !topology) return baseNodes;

      // Find edges that represent containment
      const containedIds = new Set<string>();
      for (const edge of topology.edges) {
        if (edge.type === 'contains' && edge.sourceId === activeClusterId) {
          containedIds.add(edge.targetId);
        }
      }
      return baseNodes.filter(
        (n) => containedIds.has(n.id) || n.id === activeClusterId,
      );
    },

    /**
     * Returns breadcrumb labels for the current cluster navigation path.
     * Maps cluster IDs to their names from the topology data.
     */
    clusterBreadcrumbs: () => {
      const { clusterPath, topology } = get();
      if (!topology) return clusterPath.map((id) => ({ id, label: id }));
      const nodeMap = new Map(topology.nodes.map((n) => [n.id, n]));
      return clusterPath.map((id) => ({
        id,
        label: nodeMap.get(id)?.name ?? id,
      }));
    },
  })),
);
