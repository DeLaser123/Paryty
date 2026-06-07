/**
 * Pure topology layout algorithms (DOM-free, thread-agnostic).
 *
 * Force-directed graph layout extracted verbatim from the original layout
 * worker so the heavy simulation has a single implementation that both the
 * processing worker and any synchronous fallback can import. Contains no
 * `self`/`window` reference.
 *
 * Performance:
 *   - Barnes-Hut O(n log n) repulsion above {@link BARNES_HUT_THRESHOLD}
 *   - QuadTree spatial approximation
 *   - Alpha decay for convergence control
 *
 * @module engine/processing/layout
 */

// ─── Types ──────────────────────────────────────────────────────────────────

/** A mutable simulation node (positions/velocities filled during layout). */
export interface WorkerNode {
  id: string;
  x?: number;
  y?: number;
  vx?: number;
  vy?: number;
  fx?: number | null;
  fy?: number | null;
  type: string;
  status?: string;
  size?: number;
}

/** A simulation edge by endpoint node ids. */
export interface WorkerEdge {
  source: string;
  target: string;
  type: string;
}

/** Input for a full force-directed layout. */
export interface LayoutInput {
  nodes: WorkerNode[];
  edges: WorkerEdge[];
  width: number;
  height: number;
  iterations: number;
}

/** Input for a hierarchical (grouped) layout. */
export interface ClusteredLayoutInput {
  nodes: WorkerNode[];
  edges: WorkerEdge[];
  config: {
    groupBy: string;
    width: number;
    height: number;
  };
}

/** Input for an incremental position settle around dragged nodes. */
export interface PositionUpdateInput {
  currentPositions: { id: string; x: number; y: number }[];
  movedNodes: { id: string; targetX: number; targetY: number }[];
  iterations: number;
}

/** Computed layout output: resolved node positions and optional clusters. */
export interface LayoutResult {
  nodes: { id: string; x: number; y: number; type: string; status: string }[];
  clusters?: {
    id: string;
    label: string;
    nodes: string[];
    x: number;
    y: number;
    radius: number;
  }[];
}

// ─── Constants ──────────────────────────────────────────────────────────────

/** Threshold above which Barnes-Hut replaces all-pairs repulsion. */
const BARNES_HUT_THRESHOLD = 500;

/** Barnes-Hut theta parameter (accuracy/speed trade-off). */
const BH_THETA = 0.8;

/** Damping factor for velocity decay. */
const DAMPING = 0.6;

/** Default repulsion strength. */
const REPULSION_STRENGTH = 500;

/** Default attraction strength (spring). */
const ATTRACTION_STRENGTH = 0.01;

/** Default spring rest length. */
const SPRING_REST_LENGTH = 100;

/** Center gravity strength. */
const CENTER_GRAVITY = 0.01;

// ─── Barnes-Hut QuadTree ────────────────────────────────────────────────────

interface QuadTreeNode {
  cx: number;
  cy: number;
  mass: number;
  size: number;
  isLeaf: boolean;
  nw?: QuadTreeNode;
  ne?: QuadTreeNode;
  sw?: QuadTreeNode;
  se?: QuadTreeNode;
  nodeIds?: string[];
}

function buildQuadTree(
  nodes: WorkerNode[],
  centerX: number,
  centerY: number,
  size: number,
  depth: number = 0,
): QuadTreeNode | null {
  if (nodes.length === 0) return null;

  const node: QuadTreeNode = {
    cx: centerX,
    cy: centerY,
    mass: nodes.length,
    size,
    isLeaf: nodes.length <= 1 || depth > 12,
  };

  if (node.isLeaf) {
    // Compute center of mass
    let sumX = 0;
    let sumY = 0;
    for (const n of nodes) {
      sumX += n.x ?? 0;
      sumY += n.y ?? 0;
    }
    node.cx = sumX / nodes.length;
    node.cy = sumY / nodes.length;
    node.nodeIds = nodes.map((n) => n.id);
    return node;
  }

  // Partition into quadrants
  const half = size / 2;
  const nw: WorkerNode[] = [];
  const ne: WorkerNode[] = [];
  const sw: WorkerNode[] = [];
  const se: WorkerNode[] = [];

  for (const n of nodes) {
    const nx = n.x ?? 0;
    const ny = n.y ?? 0;
    if (nx < centerX && ny < centerY) nw.push(n);
    else if (nx >= centerX && ny < centerY) ne.push(n);
    else if (nx < centerX && ny >= centerY) sw.push(n);
    else se.push(n);
  }

  // Compute center of mass for this node
  let sumX = 0;
  let sumY = 0;
  for (const n of nodes) {
    sumX += n.x ?? 0;
    sumY += n.y ?? 0;
  }
  node.cx = sumX / nodes.length;
  node.cy = sumY / nodes.length;

  node.nw = buildQuadTree(nw, centerX - half / 2, centerY - half / 2, half, depth + 1) ?? undefined;
  node.ne = buildQuadTree(ne, centerX + half / 2, centerY - half / 2, half, depth + 1) ?? undefined;
  node.sw = buildQuadTree(sw, centerX - half / 2, centerY + half / 2, half, depth + 1) ?? undefined;
  node.se = buildQuadTree(se, centerX + half / 2, centerY + half / 2, half, depth + 1) ?? undefined;

  return node;
}

function barnesHutForce(
  node: WorkerNode,
  tree: QuadTreeNode | null,
  strength: number,
): { fx: number; fy: number } {
  if (!tree) return { fx: 0, fy: 0 };

  let fx = 0;
  let fy = 0;

  const nx = node.x ?? 0;
  const ny = node.y ?? 0;

  // If this is a leaf with only our node, skip
  if (tree.isLeaf && tree.nodeIds?.length === 1 && tree.nodeIds[0] === node.id) {
    return { fx: 0, fy: 0 };
  }

  const dx = tree.cx - nx;
  const dy = tree.cy - ny;
  const distSq = dx * dx + dy * dy || 1;
  const dist = Math.sqrt(distSq);

  // Barnes-Hut criterion: s/d < theta
  if (tree.isLeaf || tree.size / dist < BH_THETA) {
    // Treat as single body
    const repulsion = strength * tree.mass / distSq;
    fx = (dx / dist) * repulsion;
    fy = (dy / dist) * repulsion;
  } else {
    // Recurse into children
    if (tree.nw) {
      const f = barnesHutForce(node, tree.nw, strength);
      fx += f.fx;
      fy += f.fy;
    }
    if (tree.ne) {
      const f = barnesHutForce(node, tree.ne, strength);
      fx += f.fx;
      fy += f.fy;
    }
    if (tree.sw) {
      const f = barnesHutForce(node, tree.sw, strength);
      fx += f.fx;
      fy += f.fy;
    }
    if (tree.se) {
      const f = barnesHutForce(node, tree.se, strength);
      fx += f.fx;
      fy += f.fy;
    }
  }

  return { fx, fy };
}

// ─── Force Simulation ───────────────────────────────────────────────────────

/** Runs a full force-directed layout over the graph. */
export function computeLayout(request: LayoutInput): LayoutResult {
  const { nodes, edges, width, height, iterations } = request;
  const useBarnesHut = nodes.length > BARNES_HUT_THRESHOLD;

  // Initialize positions
  for (const node of nodes) {
    if (node.x === undefined) node.x = width / 2 + (Math.random() - 0.5) * width * 0.5;
    if (node.y === undefined) node.y = height / 2 + (Math.random() - 0.5) * height * 0.5;
    node.vx = 0;
    node.vy = 0;
  }

  const nodeMap = new Map(nodes.map((n) => [n.id, n]));

  for (let i = 0; i < iterations; i++) {
    const alpha = 1 - i / iterations;
    const effectiveAlpha = Math.max(alpha, 0.001);

    // Repulsion
    if (useBarnesHut) {
      // Barnes-Hut O(n log n) approximation
      const tree = buildQuadTree(nodes, width / 2, height / 2, Math.max(width, height));
      for (const node of nodes) {
        const force = barnesHutForce(node, tree, REPULSION_STRENGTH * effectiveAlpha);
        node.vx = (node.vx ?? 0) + force.fx;
        node.vy = (node.vy ?? 0) + force.fy;
      }
    } else {
      // All-pairs O(n²) repulsion (faster for small graphs)
      for (let a = 0; a < nodes.length; a++) {
        for (let b = a + 1; b < nodes.length; b++) {
          const na = nodes[a];
          const nb = nodes[b];
          const dx = (nb.x ?? 0) - (na.x ?? 0);
          const dy = (nb.y ?? 0) - (na.y ?? 0);
          const dist = Math.sqrt(dx * dx + dy * dy) || 1;
          const repulsion = REPULSION_STRENGTH * effectiveAlpha / (dist * dist);
          const fx = (dx / dist) * repulsion;
          const fy = (dy / dist) * repulsion;
          na.vx = (na.vx ?? 0) - fx;
          na.vy = (na.vy ?? 0) - fy;
          nb.vx = (nb.vx ?? 0) + fx;
          nb.vy = (nb.vy ?? 0) + fy;
        }
      }
    }

    // Attraction (edges)
    for (const edge of edges) {
      const source = nodeMap.get(edge.source);
      const target = nodeMap.get(edge.target);
      if (!source || !target) continue;
      const dx = (target.x ?? 0) - (source.x ?? 0);
      const dy = (target.y ?? 0) - (source.y ?? 0);
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const attraction = (dist - SPRING_REST_LENGTH) * ATTRACTION_STRENGTH * effectiveAlpha;
      const fx = (dx / dist) * attraction;
      const fy = (dy / dist) * attraction;
      source.vx = (source.vx ?? 0) + fx;
      source.vy = (source.vy ?? 0) + fy;
      target.vx = (target.vx ?? 0) - fx;
      target.vy = (target.vy ?? 0) - fy;
    }

    // Center gravity
    for (const node of nodes) {
      const dx = width / 2 - (node.x ?? 0);
      const dy = height / 2 - (node.y ?? 0);
      node.vx = (node.vx ?? 0) + dx * CENTER_GRAVITY * effectiveAlpha;
      node.vy = (node.vy ?? 0) + dy * CENTER_GRAVITY * effectiveAlpha;
    }

    // Apply velocities with damping
    for (const node of nodes) {
      if (node.fx != null) {
        node.x = node.fx;
        node.vx = 0;
      } else {
        node.vx = (node.vx ?? 0) * DAMPING;
        node.x = (node.x ?? 0) + (node.vx ?? 0);
      }
      if (node.fy != null) {
        node.y = node.fy;
        node.vy = 0;
      } else {
        node.vy = (node.vy ?? 0) * DAMPING;
        node.y = (node.y ?? 0) + (node.vy ?? 0);
      }
      // Bounds
      node.x = Math.max(50, Math.min(width - 50, node.x ?? 0));
      node.y = Math.max(50, Math.min(height - 50, node.y ?? 0));
    }
  }

  return {
    nodes: nodes.map((n) => ({
      id: n.id,
      x: n.x ?? 0,
      y: n.y ?? 0,
      type: n.type,
      status: n.status ?? 'unknown',
    })),
  };
}

// ─── Clustered Layout ───────────────────────────────────────────────────────

/** Runs a hierarchical layout: group → grid → per-cluster force simulation. */
export function computeClusteredLayout(data: ClusteredLayoutInput): LayoutResult {
  const { nodes, edges, config } = data;
  const { groupBy, width, height } = config;

  // Step 1: Group nodes
  const groups = new Map<string, WorkerNode[]>();
  for (const node of nodes) {
    const key = groupBy === 'type' ? node.type : (groupBy);
    let group = groups.get(key);
    if (!group) {
      group = [];
      groups.set(key, group);
    }
    group.push(node);
  }

  // Step 2: Arrange cluster centers in a grid
  const clusterList: {
    id: string;
    nodes: WorkerNode[];
    x: number;
    y: number;
    radius: number;
  }[] = [];

  const entries = Array.from(groups.entries());
  const cols = Math.ceil(Math.sqrt(entries.length));
  const cellW = width / cols;
  const cellH = height / Math.ceil(entries.length / cols);

  for (let i = 0; i < entries.length; i++) {
    const [id, groupNodes] = entries[i];
    const col = i % cols;
    const row = Math.floor(i / cols);
    const cx = cellW * (col + 0.5);
    const cy = cellH * (row + 0.5);
    const radius = Math.max(60, Math.sqrt(groupNodes.length * 900 / Math.PI));

    clusterList.push({ id, nodes: groupNodes, x: cx, y: cy, radius });
  }

  // Step 3: Run force simulation within each cluster
  for (const cluster of clusterList) {
    // Initialize positions around cluster center
    for (const node of cluster.nodes) {
      node.x = cluster.x + (Math.random() - 0.5) * cluster.radius;
      node.y = cluster.y + (Math.random() - 0.5) * cluster.radius;
      node.vx = 0;
      node.vy = 0;
    }

    const clusterNodeMap = new Map(cluster.nodes.map((n) => [n.id, n]));
    const clusterEdges = edges.filter(
      (e) => clusterNodeMap.has(e.source) && clusterNodeMap.has(e.target),
    );

    // Run simulation (fewer iterations for within-cluster)
    const clusterIter = 50;
    for (let i = 0; i < clusterIter; i++) {
      const alpha = Math.max(1 - i / clusterIter, 0.001);

      // Repulsion within cluster
      for (let a = 0; a < cluster.nodes.length; a++) {
        for (let b = a + 1; b < cluster.nodes.length; b++) {
          const na = cluster.nodes[a];
          const nb = cluster.nodes[b];
          const dx = (nb.x ?? 0) - (na.x ?? 0);
          const dy = (nb.y ?? 0) - (na.y ?? 0);
          const dist = Math.sqrt(dx * dx + dy * dy) || 1;
          const repulsion = 200 * alpha / (dist * dist);
          const fx = (dx / dist) * repulsion;
          const fy = (dy / dist) * repulsion;
          na.vx = (na.vx ?? 0) - fx;
          na.vy = (na.vy ?? 0) - fy;
          nb.vx = (nb.vx ?? 0) + fx;
          nb.vy = (nb.vy ?? 0) + fy;
        }
      }

      // Attraction (edges within cluster)
      for (const edge of clusterEdges) {
        const source = clusterNodeMap.get(edge.source);
        const target = clusterNodeMap.get(edge.target);
        if (!source || !target) continue;
        const dx = (target.x ?? 0) - (source.x ?? 0);
        const dy = (target.y ?? 0) - (source.y ?? 0);
        const dist = Math.sqrt(dx * dx + dy * dy) || 1;
        const attraction = (dist - 80) * 0.01 * alpha;
        const fx = (dx / dist) * attraction;
        const fy = (dy / dist) * attraction;
        source.vx = (source.vx ?? 0) + fx;
        source.vy = (source.vy ?? 0) + fy;
        target.vx = (target.vx ?? 0) - fx;
        target.vy = (target.vy ?? 0) - fy;
      }

      // Attract to cluster center
      for (const node of cluster.nodes) {
        const dx = cluster.x - (node.x ?? 0);
        const dy = cluster.y - (node.y ?? 0);
        node.vx = (node.vx ?? 0) + dx * 0.02 * alpha;
        node.vy = (node.vy ?? 0) + dy * 0.02 * alpha;
      }

      // Apply velocities
      for (const node of cluster.nodes) {
        node.vx = (node.vx ?? 0) * DAMPING;
        node.vy = (node.vy ?? 0) * DAMPING;
        node.x = (node.x ?? 0) + (node.vx ?? 0);
        node.y = (node.y ?? 0) + (node.vy ?? 0);

        // Constrain to cluster radius
        const dx = (node.x ?? 0) - cluster.x;
        const dy = (node.y ?? 0) - cluster.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist > cluster.radius) {
          const scale = cluster.radius / dist;
          node.x = cluster.x + dx * scale;
          node.y = cluster.y + dy * scale;
        }
      }
    }
  }

  // Collect all positions
  return {
    nodes: nodes.map((n) => ({
      id: n.id,
      x: n.x ?? 0,
      y: n.y ?? 0,
      type: n.type,
      status: n.status ?? 'unknown',
    })),
    clusters: clusterList.map((c) => ({
      id: c.id,
      label: c.id,
      nodes: c.nodes.map((n) => n.id),
      x: c.x,
      y: c.y,
      radius: c.radius,
    })),
  };
}

// ─── Incremental Position Update ────────────────────────────────────────────

/** Settles non-moved nodes around dragged nodes over a few light iterations. */
export function computePositionUpdate(data: PositionUpdateInput): LayoutResult {
  const { currentPositions, movedNodes, iterations } = data;

  const nodes: WorkerNode[] = currentPositions.map((p) => ({
    id: p.id,
    x: p.x,
    y: p.y,
    vx: 0,
    vy: 0,
    type: 'unknown',
  }));

  const nodeMap = new Map(nodes.map((n) => [n.id, n]));
  const movedIds = new Set(movedNodes.map((m) => m.id));

  // Set target positions for moved nodes
  for (const moved of movedNodes) {
    const node = nodeMap.get(moved.id);
    if (node) {
      node.fx = moved.targetX;
      node.fy = moved.targetY;
    }
  }

  // Run a few iterations to settle non-moved nodes around moved ones
  for (let i = 0; i < iterations; i++) {
    const alpha = Math.max(1 - i / iterations, 0.001);

    // Light repulsion between all nodes
    for (let a = 0; a < nodes.length; a++) {
      for (let b = a + 1; b < nodes.length; b++) {
        const na = nodes[a];
        const nb = nodes[b];
        const dx = (nb.x ?? 0) - (na.x ?? 0);
        const dy = (nb.y ?? 0) - (na.y ?? 0);
        const dist = Math.sqrt(dx * dx + dy * dy) || 1;
        if (dist > 100) continue; // Only repel nearby nodes
        const repulsion = 100 * alpha / (dist * dist);
        const fx = (dx / dist) * repulsion;
        const fy = (dy / dist) * repulsion;
        if (!movedIds.has(na.id)) {
          na.vx = (na.vx ?? 0) - fx;
          na.vy = (na.vy ?? 0) - fy;
        }
        if (!movedIds.has(nb.id)) {
          nb.vx = (nb.vx ?? 0) + fx;
          nb.vy = (nb.vy ?? 0) + fy;
        }
      }
    }

    // Apply velocities
    for (const node of nodes) {
      if (node.fx != null) {
        node.x = node.fx;
        node.vx = 0;
      } else {
        node.vx = (node.vx ?? 0) * DAMPING;
        node.x = (node.x ?? 0) + (node.vx ?? 0);
      }
      if (node.fy != null) {
        node.y = node.fy;
        node.vy = 0;
      } else {
        node.vy = (node.vy ?? 0) * DAMPING;
        node.y = (node.y ?? 0) + (node.vy ?? 0);
      }
    }
  }

  return {
    nodes: nodes.map((n) => ({
      id: n.id,
      x: n.x ?? 0,
      y: n.y ?? 0,
      type: n.type,
      status: n.status ?? 'unknown',
    })),
  };
}
