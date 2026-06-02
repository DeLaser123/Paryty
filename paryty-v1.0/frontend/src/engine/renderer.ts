// Renderer bridge between Zustand stores and PixiJS engine

import { PixiTopologyApp, type NodeRenderData } from './pixiApp';
import type { TopologyNode, TopologyEdge } from '../types/topology';

export interface RendererConfig {
  container: HTMLElement;
  width: number;
  height: number;
}

export class TopologyRenderer {
  private app: PixiTopologyApp;
  private container: HTMLElement;
  private layoutWorker: Worker | null = null;
  private pendingLayout: boolean = false;

  constructor(config: RendererConfig) {
    this.container = config.container;
    this.app = new PixiTopologyApp({
      width: config.width,
      height: config.height,
    });
    this.container.appendChild(this.app.view);
  }

  update(topology: { nodes: TopologyNode[]; edges: TopologyEdge[] }): void {
    const nodeData: NodeRenderData[] = topology.nodes.map((n) => ({
      id: n.id,
      x: 0, // Will be set by layout
      y: 0,
      type: n.type,
      status: n.status,
      label: n.name,
      size: getNodeSize(n),
    }));

    // Run layout in worker
    this.runLayout(nodeData, topology.edges);
  }

  private runLayout(nodes: NodeRenderData[], edges: TopologyEdge[]): void {
    if (this.pendingLayout) return;
    this.pendingLayout = true;

    try {
      if (!this.layoutWorker) {
        this.layoutWorker = new Worker(
          new URL('./workers/topologyLayout.worker.ts', import.meta.url),
          { type: 'module' },
        );
        this.layoutWorker.onmessage = (event) => {
          this.pendingLayout = false;
          if (event.data.type === 'result') {
            const positions = event.data.data.nodes as { id: string; x: number; y: number }[];
            this.applyPositions(positions);
          }
        };
      }

      const width = this.app.view instanceof HTMLCanvasElement ? this.app.view.width : 800;
      const height = this.app.view instanceof HTMLCanvasElement ? this.app.view.height : 600;

      this.layoutWorker.postMessage({
        type: 'layout',
        id: 'topology',
        data: {
          nodes: nodes.map((n) => ({ id: n.id, type: n.type })),
          edges: edges.map((e) => ({ source: e.sourceId, target: e.targetId, type: e.type })),
          width,
          height,
          iterations: 100,
        },
      });
    } catch {
      this.pendingLayout = false;
      // Fallback: render without layout
      this.app.updateNodes(nodes);
    }
  }

  private applyPositions(positions: { id: string; x: number; y: number }[]): void {
    const posMap = new Map(positions.map((p) => [p.id, p]));
    const nodes = Array.from(this.app.nodes.keys()).map((id) => {
      const pos = posMap.get(id);
      return {
        id,
        x: pos?.x ?? 0,
        y: pos?.y ?? 0,
        type: 'unknown',
        status: 'unknown',
        label: id,
        size: 15,
      };
    });
    this.app.updateNodes(nodes);
  }

  resize(width: number, height: number): void {
    this.app.resize(width, height);
  }

  destroy(): void {
    this.layoutWorker?.terminate();
    this.app.destroy();
  }
}

function getNodeSize(node: TopologyNode): number {
  switch (node.type) {
    case 'host': return 25;
    case 'container': return 18;
    case 'service': return 20;
    case 'process': return 12;
    default: return 15;
  }
}
