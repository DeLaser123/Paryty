// PixiJS application setup for GPU-accelerated topology rendering
// Basic placeholder - user will design brand identity later

import * as PIXI from 'pixi.js';

export interface PixiAppConfig {
  width: number;
  height: number;
  backgroundColor?: number;
  antialias?: boolean;
  resolution?: number;
}

export interface NodeRenderData {
  id: string;
  x: number;
  y: number;
  type: string;
  status: string;
  label: string;
  size: number;
}

export interface EdgeRenderData {
  id: string;
  sourceX: number;
  sourceY: number;
  targetX: number;
  targetY: number;
  type: string;
}

export class PixiTopologyApp {
  app: PIXI.Application;
  nodeContainer: PIXI.Container;
  edgeContainer: PIXI.Container;
  labelContainer: PIXI.Container;
  nodes: Map<string, PIXI.Graphics> = new Map();
  edges: Map<string, PIXI.Graphics> = new Map();
  labels: Map<string, PIXI.Text> = new Map();

  constructor(config: PixiAppConfig) {
    this.app = new PIXI.Application({
      width: config.width,
      height: config.height,
      backgroundColor: config.backgroundColor ?? 0x1a1a2e,
      antialias: config.antialias ?? true,
      resolution: config.resolution ?? (window.devicePixelRatio || 1),
      autoDensity: true,
    });

    this.edgeContainer = new PIXI.Container();
    this.nodeContainer = new PIXI.Container();
    this.labelContainer = new PIXI.Container();

    this.app.stage.addChild(this.edgeContainer);
    this.app.stage.addChild(this.nodeContainer);
    this.app.stage.addChild(this.labelContainer);
  }

  get view(): HTMLCanvasElement {
    return this.app.view as HTMLCanvasElement;
  }

  resize(width: number, height: number): void {
    this.app.renderer.resize(width, height);
  }

  updateNodes(nodes: NodeRenderData[]): void {
    // Remove stale nodes
    const currentIds = new Set(nodes.map((n) => n.id));
    for (const [id, graphic] of this.nodes) {
      if (!currentIds.has(id)) {
        graphic.destroy();
        this.nodes.delete(id);
        this.labels.get(id)?.destroy();
        this.labels.delete(id);
      }
    }

    for (const node of nodes) {
      let graphic = this.nodes.get(node.id);
      if (!graphic) {
        graphic = new PIXI.Graphics();
        this.nodeContainer.addChild(graphic);
        this.nodes.set(node.id, graphic);

        // Add label
        const label = new PIXI.Text(node.label, {
          fontSize: 11,
          fill: 0xffffff,
          fontFamily: 'monospace',
        });
        label.anchor.set(0.5, 0);
        this.labelContainer.addChild(label);
        this.labels.set(node.id, label);
      }

      // Draw node
      graphic.clear();
      const color = getNodeColor(node.type, node.status);
      const size = node.size || 20;

      graphic.beginFill(color, 0.9);
      graphic.lineStyle(1.5, 0xffffff, 0.3);
      graphic.drawCircle(node.x, node.y, size);
      graphic.endFill();

      // Status indicator
      const statusColor = getStatusColor(node.status);
      graphic.beginFill(statusColor, 1);
      graphic.drawCircle(node.x + size - 4, node.y - size + 4, 4);
      graphic.endFill();

      // Update label position
      const label = this.labels.get(node.id);
      if (label) {
        label.x = node.x;
        label.y = node.y + size + 4;
      }
    }
  }

  updateEdges(edges: EdgeRenderData[]): void {
    // Remove stale edges
    const currentIds = new Set(edges.map((e) => e.id));
    for (const [id, graphic] of this.edges) {
      if (!currentIds.has(id)) {
        graphic.destroy();
        this.edges.delete(id);
      }
    }

    for (const edge of edges) {
      let graphic = this.edges.get(edge.id);
      if (!graphic) {
        graphic = new PIXI.Graphics();
        this.edgeContainer.addChild(graphic);
        this.edges.set(edge.id, graphic);
      }

      graphic.clear();
      const color = getEdgeColor(edge.type);
      graphic.lineStyle(1, color, 0.6);
      graphic.moveTo(edge.sourceX, edge.sourceY);
      graphic.lineTo(edge.targetX, edge.targetY);
    }
  }

  destroy(): void {
    this.nodes.forEach((n) => n.destroy());
    this.edges.forEach((e) => e.destroy());
    this.labels.forEach((l) => l.destroy());
    this.app.destroy(true, { children: true, texture: true });
  }
}

function getNodeColor(type: string, status: string): number {
  if (status === 'unhealthy') return 0xe74c3c;
  if (status === 'degraded') return 0xf39c12;

  switch (type) {
    case 'host': return 0x3498db;
    case 'container': return 0x2ecc71;
    case 'service': return 0x9b59b6;
    case 'process': return 0x1abc9c;
    default: return 0x95a5a6;
  }
}

function getStatusColor(status: string): number {
  switch (status) {
    case 'healthy': return 0x2ecc71;
    case 'degraded': return 0xf39c12;
    case 'unhealthy': return 0xe74c3c;
    default: return 0x7f8c8d;
  }
}

function getEdgeColor(type: string): number {
  switch (type) {
    case 'network': return 0x3498db;
    case 'dependency': return 0xe67e22;
    case 'calls': return 0x1abc9c;
    default: return 0x7f8c8d;
  }
}
