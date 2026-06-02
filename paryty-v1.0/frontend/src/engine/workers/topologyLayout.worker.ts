// Topology layout computation Web Worker
// Uses d3-force for graph layout without blocking the main thread

interface Node {
  id: string;
  x?: number;
  y?: number;
  vx?: number;
  vy?: number;
  fx?: number | null;
  fy?: number | null;
  type: string;
}

interface Edge {
  source: string;
  target: string;
  type: string;
}

interface LayoutRequest {
  nodes: Node[];
  edges: Edge[];
  width: number;
  height: number;
  iterations: number;
}

interface LayoutResult {
  nodes: { id: string; x: number; y: number }[];
}

// Simplified force simulation (avoids d3-force dependency in worker)
// For production, import d3-force-3d in the worker build
self.onmessage = (event: MessageEvent<{ type: string; id: string; data: LayoutRequest }>) => {
  const { type, id, data } = event.data;

  if (type === 'layout') {
    const result = computeLayout(data);
    self.postMessage({ type: 'result', id, data: result });
  }
};

function computeLayout(request: LayoutRequest): LayoutResult {
  const { nodes, edges, width, height, iterations } = request;

  // Initialize positions
  for (const node of nodes) {
    if (node.x === undefined) node.x = width / 2 + (Math.random() - 0.5) * width * 0.5;
    if (node.y === undefined) node.y = height / 2 + (Math.random() - 0.5) * height * 0.5;
    node.vx = 0;
    node.vy = 0;
  }

  const nodeMap = new Map(nodes.map((n) => [n.id, n]));

  // Force simulation
  for (let i = 0; i < iterations; i++) {
    const alpha = 1 - i / iterations;
    const alphaMin = 0.001;
    const effectiveAlpha = Math.max(alpha, alphaMin);

    // Repulsion (all pairs)
    for (let a = 0; a < nodes.length; a++) {
      for (let b = a + 1; b < nodes.length; b++) {
        const na = nodes[a];
        const nb = nodes[b];
        let dx = (nb.x ?? 0) - (na.x ?? 0);
        let dy = (nb.y ?? 0) - (na.y ?? 0);
        let dist = Math.sqrt(dx * dx + dy * dy) || 1;
        const repulsion = 500 * effectiveAlpha / (dist * dist);
        const fx = (dx / dist) * repulsion;
        const fy = (dy / dist) * repulsion;
        na.vx = (na.vx ?? 0) - fx;
        na.vy = (na.vy ?? 0) - fy;
        nb.vx = (nb.vx ?? 0) + fx;
        nb.vy = (nb.vy ?? 0) + fy;
      }
    }

    // Attraction (edges)
    for (const edge of edges) {
      const source = nodeMap.get(edge.source);
      const target = nodeMap.get(edge.target);
      if (!source || !target) continue;
      let dx = (target.x ?? 0) - (source.x ?? 0);
      let dy = (target.y ?? 0) - (source.y ?? 0);
      const dist = Math.sqrt(dx * dx + dy * dy) || 1;
      const attraction = (dist - 100) * 0.01 * effectiveAlpha;
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
      node.vx = (node.vx ?? 0) + dx * 0.01 * effectiveAlpha;
      node.vy = (node.vy ?? 0) + dy * 0.01 * effectiveAlpha;
    }

    // Apply velocities with damping
    for (const node of nodes) {
      if (node.fx != null) {
        node.x = node.fx;
        node.vx = 0;
      } else {
        node.vx = (node.vx ?? 0) * 0.6;
        node.x = (node.x ?? 0) + (node.vx ?? 0);
      }
      if (node.fy != null) {
        node.y = node.fy;
        node.vy = 0;
      } else {
        node.vy = (node.vy ?? 0) * 0.6;
        node.y = (node.y ?? 0) + (node.vy ?? 0);
      }
      // Keep within bounds
      node.x = Math.max(50, Math.min(width - 50, node.x ?? 0));
      node.y = Math.max(50, Math.min(height - 50, node.y ?? 0));
    }
  }

  return {
    nodes: nodes.map((n) => ({
      id: n.id,
      x: n.x ?? 0,
      y: n.y ?? 0,
    })),
  };
}
