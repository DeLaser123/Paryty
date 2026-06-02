import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { Topology, TopologyNode, TopologyEdge, TopologyDiff } from '../types/topology';

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

  // Computed
  filteredNodes: () => TopologyNode[];
  filteredEdges: () => TopologyEdge[];
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
  })),
);
