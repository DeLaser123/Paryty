# Phase 5 Hardened Specification — Frontend Visualization

**Version:** 1.0.0
**Status:** LOCKED — All architectural decisions finalized
**Target LOC:** ~12,000 (TypeScript/React)
**Estimated Effort:** 4-6 weeks for a senior frontend engineer

---

## Table of Contents

1. [Phase 5 Overview & Decisions](#1-phase-5-overview--decisions)
2. [Pre-Phase Setup](#2-pre-phase-setup)
3. [Layer 17: GPU Rendering Engine](#3-layer-17-gpu-rendering-engine)
4. [Layer 18: Real-Time Data Pipeline](#4-layer-18-real-time-data-pipeline)
5. [Layer 19: State Management & Stores](#5-layer-19-state-management--stores)
6. [Layer 20: UI Components & Views](#6-layer-20-ui-components--views)
7. [Theme & Design System](#7-theme--design-system)
8. [Verification Gates](#8-verification-gates)
9. [Performance Targets](#9-performance-targets)
10. [Contingency & Rollback](#10-contingency--rollback)
11. [Appendices](#11-appendices)

---

## 1. Phase 5 Overview & Decisions

### 1.1 What Phase 5 Delivers

Phase 5 transforms the Paryty frontend from a basic placeholder into a production-grade, GPU-accelerated observability dashboard. The digital twin topology visualization renders 15K+ nodes at 60fps using instanced rendering, with hierarchical clustering for readable layouts and particle effects for data flow visualization.

**Before Phase 5:** Basic PixiJS renderer with individual Graphics per node (scales to ~1K). Placeholder UI components. No real-time data pipeline.

**After Phase 5:** Production dashboard with instanced rendering (15K+ nodes), hierarchical clustering layout, particle data flow, real-time WebSocket/SSE pipeline, and a complete set of dashboard components.

### 1.2 Architectural Decisions (LOCKED)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Topology Rendering | **A — Instanced Rendering** | PIXI.ParticleContainer for 15K+ nodes at 60fps. Single draw call, minimal memory. |
| 2 | Layout Algorithm | **B — Hierarchical + Clustering** | Group by service/region/layer. d3-force within clusters. Structured, readable layouts. |
| 3 | Particle System | **A — PixiJS ParticleContainer** | GPU-accelerated particles along edges. Color = throughput, speed = latency. |
| 4 | Visual Theme | **Custom — Monochromatic** | Black and white theme with Geist Sans/Mono. IDE-style, monochromatic digital twin aesthetic. |

### 1.3 Visual Theme — CRITICAL IMPLEMENTATION NOTE

> **STOP GATE: Before implementing any styling, colors, fonts, or visual effects, the implementing agent MUST stop and request the user's preferred UI/UX rulebook.** The user has a custom styling rules file that will be placed in the repository. Do NOT assume any color palette, font sizing, spacing, or visual effects. Wait for the rulebook before writing any CSS or theme code.

**Known preferences (confirmed by user):**
- **Color palette:** Black and white (monochromatic)
- **Typography:** Geist Sans (Headings, Titles & Labels) + Geist Mono (UI Text, code/data)
- **Aesthetic:** IDE-style, digital twin, beautiful monochromatic UI/UX
- **Framework:** CSS variables for all theme tokens (enables future theme switching)

### 1.4 What Gets Built

| Layer | Component | LOC | Description |
|-------|-----------|-----|-------------|
| 17 | GPU Rendering Engine | ~5,000 | Instanced rendering, hierarchical clustering, particle system, visual effects |
| 18 | Real-Time Data Pipeline | ~3,000 | WebSocket enhancements, SSE timeline, Web Workers |
| 19 | State Management | ~2,000 | Enhanced Zustand stores with subscriptions and computed state |
| 20 | UI Components | ~2,000 | Dashboard, topology view, timeline replay, alerts panel |

### 1.5 Existing Code Assessment

**What exists (stubs/basic):**
- `pixiApp.ts` (188 lines): Basic PixiJS renderer with individual Graphics per node. No instancing, no clustering, no particles.
- `renderer.ts` (118 lines): Bridge between Zustand and PixiJS. Uses layout worker. No instanced rendering.
- `websocket.ts` (224 lines): Solid WebSocket client with auto-reconnect, heartbeat, subscriptions. Good foundation.
- `sse.ts` (108 lines): SSE client for timeline replay. Basic but functional.
- `sharedBuffer.ts` (4.1KB): SharedArrayBuffer ring buffer for zero-copy communication.
- Workers (3 files): `topologyLayout.worker.ts` (3.9KB), `metricProcessor.worker.ts` (3.7KB), `dataParser.worker.ts` (1KB).
- Stores (5 files): topologyStore (115 lines), metricsStore (84 lines), timelineStore (115 lines), alertsStore, settingsStore.
- Components (5 files): TopologyView, MetricsView, TimelineView, AlertView, Layout. All basic placeholders.

---

## 2. Pre-Phase Setup

### 2.1 Dependencies

**File:** `frontend/package.json` — Add/verify:

```json
{
  "dependencies": {
    "pixi.js": "^7.3.2",          // Already present
    "d3-force": "^3.0.0",          // Already present
    "zustand": "^4.5.2",           // Already present
    "recharts": "^2.12.7",         // Already present
    "date-fns": "^3.6.0",          // Already present
    "react": "^18.3.1",            // Already present
    "react-dom": "^18.3.1",        // Already present
    "react-router-dom": "^6.23.1", // Already present

    // New for Phase 5:
    "@pixi/particle-emitter": "^5.0.8",  // Particle effects
    "framer-motion": "^11.0.0",          // UI animations
    "clsx": "^2.1.0",                    // Conditional classnames
    "tailwind-merge": "^2.2.0"           // Tailwind class merging
  }
}
```

### 2.2 New File Structure

```
frontend/src/
├── engine/
│   ├── pixiApp.ts              # REWRITE — Instanced rendering
│   ├── renderer.ts             # REWRITE — Hierarchical clustering renderer
│   ├── sharedBuffer.ts         # KEEP — SharedArrayBuffer ring buffer
│   ├── instancing.ts           # NEW — ParticleContainer node renderer (~500 LOC)
│   ├── clustering.ts           # NEW — Hierarchical clustering logic (~400 LOC)
│   ├── particles.ts            # NEW — Edge particle system (~600 LOC)
│   ├── effects.ts              # NEW — Visual effects (glow, pulse, health) (~400 LOC)
│   ├── viewport.ts             # NEW — Zoom/pan/fit controls (~300 LOC)
│   ├── workers/
│   │   ├── topologyLayout.worker.ts  # ENHANCE — Clustering support
│   │   ├── metricProcessor.worker.ts # KEEP
│   │   └── dataParser.worker.ts      # ENHANCE — Protobuf support
│   └── textures/
│       ├── nodeAtlas.ts        # NEW — Pre-generated node sprite atlas (~200 LOC)
│       └── generateTextures.ts # NEW — Runtime texture generation (~200 LOC)
├── api/
│   ├── websocket.ts            # ENHANCE — Backpressure, deduplication
│   ├── sse.ts                  # ENHANCE — Speed control, pause/resume
│   └── rest.ts                 # ENHANCE — New endpoints for DB queries, snapshots
├── stores/
│   ├── topologyStore.ts        # ENHANCE — Clustering state, viewport
│   ├── metricsStore.ts         # ENHANCE — Real-time streaming, downsampling
│   ├── alertsStore.ts          # ENHANCE — Alert grouping, acknowledge
│   ├── timelineStore.ts        # ENHANCE — Snapshot-based replay, diff viewer
│   └── settingsStore.ts        # ENHANCE — Theme, layout preferences
├── components/
│   ├── layout/
│   │   ├── AppShell.tsx        # NEW — Main app shell with sidebar + content (~200 LOC)
│   │   ├── Sidebar.tsx         # NEW — Navigation sidebar (~150 LOC)
│   │   ├── Header.tsx          # NEW — Top bar with search, notifications (~150 LOC)
│   │   └── StatusBar.tsx       # NEW — Bottom status bar (~100 LOC)
│   ├── topology/
│   │   ├── TopologyCanvas.tsx  # NEW — Main topology canvas component (~300 LOC)
│   │   ├── NodeDetailPanel.tsx # NEW — Selected node detail panel (~200 LOC)
│   │   ├── EdgeDetailPanel.tsx # NEW — Selected edge detail panel (~150 LOC)
│   │   ├── TopologyControls.tsx# NEW — Zoom, fit, filter controls (~150 LOC)
│   │   ├── SearchOverlay.tsx   # NEW — Node search with autocomplete (~150 LOC)
│   │   └── ClusterBreadcrumb.tsx # NEW — Breadcrumb for drill-down (~100 LOC)
│   ├── metrics/
│   │   ├── MetricChart.tsx     # NEW — Time-series chart component (~300 LOC)
│   │   ├── MetricCards.tsx     # NEW — Summary metric cards (~200 LOC)
│   │   ├── Sparkline.tsx       # NEW — Inline sparkline component (~100 LOC)
│   │   └── TimeRangeSelector.tsx # NEW — Time range picker (~150 LOC)
│   ├── timeline/
│   │   ├── TimelineScrubber.tsx # NEW — Timeline scrubber with preview (~250 LOC)
│   │   ├── SpeedControls.tsx   # NEW — Playback speed controls (~100 LOC)
│   │   ├── DiffViewer.tsx      # NEW — Compare two points in time (~200 LOC)
│   │   └── SnapshotList.tsx    # NEW — List of available snapshots (~150 LOC)
│   └── alerts/
│       ├── AlertList.tsx       # NEW — Alert list with filtering (~200 LOC)
│       ├── AlertCard.tsx       # NEW — Individual alert card (~150 LOC)
│       └── AlertPanel.tsx      # NEW — Slide-over alert detail panel (~150 LOC)
├── hooks/
│   ├── useTopology.ts          # ENHANCE — Clustering, viewport
│   ├── useMetrics.ts           # ENHANCE — Real-time streaming
│   ├── useAlerts.ts            # ENHANCE — Grouping, acknowledge
│   ├── useTimeline.ts          # ENHANCE — Snapshot-based replay
│   ├── useWebSocket.ts         # ENHANCE — Backpressure
│   ├── useViewport.ts          # NEW — Zoom/pan state (~100 LOC)
│   └── useKeyboard.ts          # NEW — Keyboard shortcuts (~100 LOC)
├── types/
│   ├── topology.ts             # ENHANCE — Cluster types, viewport types
│   ├── metric.ts               # ENHANCE — Downsampled metric types
│   ├── timeline.ts             # ENHANCE — Snapshot types, diff types
│   ├── alert.ts                # ENHANCE — Alert group types
│   └── common.ts               # ENHANCE — Shared types
├── styles/
│   ├── tokens.css              # NEW — CSS custom properties (theme tokens)
│   ├── globals.css             # NEW — Global styles, resets
│   ├── typography.css          # NEW — Geist Sans/Mono font definitions
│   └── utilities.css           # NEW — Utility classes
└── index.css                   # UPDATE — Import new style files
```

---

## 3. Layer 17: GPU Rendering Engine

### 3.1 Overview

The GPU rendering engine is the core of the Paryty digital twin visualization. It uses PixiJS with instanced rendering to display 15K+ nodes at 60fps, hierarchical clustering for readable layouts, and particle effects for data flow visualization.

### 3.2 Instanced Node Renderer

**File:** `frontend/src/engine/instancing.ts` (~500 LOC)

```typescript
// Instanced rendering for 15K+ topology nodes
//
// Uses PIXI.ParticleContainer for maximum performance.
// All nodes share pre-generated sprite textures (node atlas).
// Labels are rendered separately (only for visible/hovered nodes).

import * as PIXI from 'pixi.js';

export interface InstancedNodeData {
  id: string;
  x: number;
  y: number;
  type: 'host' | 'container' | 'service' | 'process' | 'endpoint';
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  size: number;
  label: string;
  clusterId?: string;
}

export interface NodeTextureAtlas {
  // Pre-generated textures for each (type, status) combination
  getTexture(type: string, status: string): PIXI.Texture;
}

// InstancedNodeRenderer renders thousands of nodes using ParticleContainer.
//
// PERFORMANCE CHARACTERISTICS:
//   - 15K nodes: ~60fps on modern GPU
//   - 50K nodes: ~30fps (with LOD)
//   - Memory: ~2 bytes per node (position + texture index)
//
// ARCHITECTURE:
//   - ParticleContainer: single draw call for all nodes
//   - Pre-generated texture atlas: avoids per-frame texture creation
//   - Spatial index: only updates visible nodes on pan/zoom
export class InstancedNodeRenderer {
  private container: PIXI.ParticleContainer;
  private sprites: Map<string, PIXI.Sprite> = new Map();
  private textureAtlas: NodeTextureAtlas;
  private spatialIndex: SpatialIndex;
  private visibleNodes: Set<string> = new Set();

  constructor(
    parentContainer: PIXI.Container,
    textureAtlas: NodeTextureAtlas,
  ) {
    this.textureAtlas = textureAtlas;
    this.container = new PIXI.ParticleContainer(20000, {
      vertices: false,    // Don't need vertex updates
      position: true,     // Need position updates
      rotation: false,    // No rotation
      uvs: false,         // No UV updates
      tint: true,         // Need tint for status colors
    });
    parentContainer.addChild(this.container);
    this.spatialIndex = new SpatialIndex(100); // 100px grid cells
  }

  // Update all nodes. Called when topology changes.
  //
  // ALGORITHM:
  //   1. Remove sprites for nodes no longer in topology
  //   2. Create sprites for new nodes
  //   3. Update positions for moved nodes
  //   4. Update textures for status changes
  //   5. Update spatial index
  //
  // PERFORMANCE: O(n) for full update, O(delta) for incremental
  update(nodes: InstancedNodeData[]): void {
    const currentIds = new Set(nodes.map(n => n.id));

    // Remove stale sprites
    for (const [id, sprite] of this.sprites) {
      if (!currentIds.has(id)) {
        this.container.removeChild(sprite);
        sprite.destroy();
        this.sprites.delete(id);
        this.spatialIndex.remove(id);
      }
    }

    // Add/update sprites
    for (const node of nodes) {
      let sprite = this.sprites.get(node.id);
      if (!sprite) {
        sprite = new PIXI.Sprite(
          this.textureAtlas.getTexture(node.type, node.status)
        );
        sprite.anchor.set(0.5);
        sprite.interactive = true;
        sprite.on('pointerdown', () => this.onNodeClick?.(node.id));
        sprite.on('pointerover', () => this.onNodeHover?.(node.id));
        this.container.addChild(sprite);
        this.sprites.set(node.id, sprite);
      }

      sprite.x = node.x;
      sprite.y = node.y;
      sprite.width = node.size;
      sprite.height = node.size;
      sprite.texture = this.textureAtlas.getTexture(node.type, node.status);

      this.spatialIndex.update(node.id, node.x, node.y);
    }
  }

  // Get nodes visible in the current viewport.
  getVisibleNodes(viewport: Viewport): string[] {
    return this.spatialIndex.query(
      viewport.x, viewport.y,
      viewport.x + viewport.width,
      viewport.y + viewport.height
    );
  }

  // Event callbacks
  onNodeClick?: (id: string) => void;
  onNodeHover?: (id: string) => void;

  destroy(): void {
    this.sprites.forEach(s => s.destroy());
    this.sprites.clear();
    this.container.destroy({ children: true });
  }
}

// SpatialIndex provides O(1) lookups for visible nodes.
//
// Uses a grid-based spatial hash for efficient viewport queries.
// Grid cell size should be ~2x the average node size.
class SpatialIndex {
  private grid: Map<string, Set<string>> = new Map();
  private cellSize: number;
  private nodeCells: Map<string, string> = new Map(); // node → cell key

  constructor(cellSize: number) {
    this.cellSize = cellSize;
  }

  update(id: string, x: number, y: number): void {
    const cellKey = this.cellKey(x, y);
    const oldCell = this.nodeCells.get(id);
    if (oldCell === cellKey) return;

    // Remove from old cell
    if (oldCell) {
      this.grid.get(oldCell)?.delete(id);
    }

    // Add to new cell
    if (!this.grid.has(cellKey)) {
      this.grid.set(cellKey, new Set());
    }
    this.grid.get(cellKey)!.add(id);
    this.nodeCells.set(id, cellKey);
  }

  remove(id: string): void {
    const cell = this.nodeCells.get(id);
    if (cell) {
      this.grid.get(cell)?.delete(id);
      this.nodeCells.delete(id);
    }
  }

  query(x1: number, y1: number, x2: number, y2: number): string[] {
    const result: string[] = [];
    const cx1 = Math.floor(x1 / this.cellSize);
    const cy1 = Math.floor(y1 / this.cellSize);
    const cx2 = Math.floor(x2 / this.cellSize);
    const cy2 = Math.floor(y2 / this.cellSize);

    for (let cx = cx1; cx <= cx2; cx++) {
      for (let cy = cy1; cy <= cy2; cy++) {
        const cell = this.grid.get(`${cx},${cy}`);
        if (cell) {
          result.push(...cell);
        }
      }
    }
    return result;
  }

  private cellKey(x: number, y: number): string {
    return `${Math.floor(x / this.cellSize)},${Math.floor(y / this.cellSize)}`;
  }
}

interface Viewport {
  x: number;
  y: number;
  width: number;
  height: number;
}
```

### 3.3 Node Texture Atlas

**File:** `frontend/src/engine/textures/generateTextures.ts` (~200 LOC)

```typescript
// Runtime texture generation for the node sprite atlas.
//
// Generates textures for each (type, status) combination at startup.
// This avoids loading external assets and enables dynamic theming.

import * as PIXI from 'pixi.js';

export interface TextureConfig {
  size: number;          // Base texture size (64px)
  typeShapes: Record<string, 'circle' | 'square' | 'diamond' | 'hexagon'>;
  statusColors: Record<string, number>;  // Monochromatic: white, gray, dark gray
}

// DEFAULT TEXTURE CONFIG (Monochromatic theme)
//
// NOTE: Colors will be updated when user's UI/UX rulebook is provided.
// These are placeholder monochromatic values.
export const DEFAULT_TEXTURE_CONFIG: TextureConfig = {
  size: 64,
  typeShapes: {
    host: 'circle',
    container: 'square',
    service: 'hexagon',
    process: 'diamond',
    endpoint: 'circle',
  },
  statusColors: {
    healthy: 0xffffff,    // White
    degraded: 0x888888,   // Gray
    unhealthy: 0x333333,  // Dark gray
    unknown: 0x555555,    // Mid gray
  },
};

// Generate all textures for the atlas.
//
// Creates PIXI.Texture objects for each (type, status) pair.
// Textures are generated once at startup and reused for all nodes.
//
// RETURNS: Map of "type:status" → PIXI.Texture
export function generateTextureAtlas(
  config: TextureConfig = DEFAULT_TEXTURE_CONFIG
): Map<string, PIXI.Texture> {
  const textures = new Map<string, PIXI.Texture>();
  const app = new PIXI.Application({ width: 1, height: 1 }); // Dummy for renderer

  for (const [type, shape] of Object.entries(config.typeShapes)) {
    for (const [status, color] of Object.entries(config.statusColors)) {
      const graphics = new PIXI.Graphics();

      // Draw shape with status color
      graphics.beginFill(color, 0.9);
      graphics.lineStyle(2, 0xffffff, 0.3);

      const s = config.size;
      const half = s / 2;

      switch (shape) {
        case 'circle':
          graphics.drawCircle(half, half, half - 4);
          break;
        case 'square':
          graphics.drawRoundedRect(4, 4, s - 8, s - 8, 6);
          break;
        case 'diamond':
          graphics.moveTo(half, 4);
          graphics.lineTo(s - 4, half);
          graphics.lineTo(half, s - 4);
          graphics.lineTo(4, half);
          graphics.closePath();
          break;
        case 'hexagon':
          const r = half - 4;
          for (let i = 0; i < 6; i++) {
            const angle = (Math.PI / 3) * i - Math.PI / 2;
            const px = half + r * Math.cos(angle);
            const py = half + r * Math.sin(angle);
            if (i === 0) graphics.moveTo(px, py);
            else graphics.lineTo(px, py);
          }
          graphics.closePath();
          break;
      }
      graphics.endFill();

      // Add status indicator dot
      const indicatorColor = status === 'healthy' ? 0xffffff :
                             status === 'degraded' ? 0xaaaaaa :
                             status === 'unhealthy' ? 0x444444 : 0x666666;
      graphics.beginFill(indicatorColor, 1);
      graphics.drawCircle(s - 8, 8, 4);
      graphics.endFill();

      const texture = PIXI.Texture.from(graphics);
      textures.set(`${type}:${status}`, texture);
      graphics.destroy();
    }
  }

  app.destroy(true);
  return textures;
}
```

### 3.4 Hierarchical Clustering

**File:** `frontend/src/engine/clustering.ts` (~400 LOC)

```typescript
// Hierarchical clustering for topology layout.
//
// Groups nodes by service/region/layer, then arranges clusters
// in a structured layout with d3-force within each cluster.

import * as d3 from 'd3-force';
import type { InstancedNodeData } from './instancing';

export interface ClusterConfig {
  groupBy: 'service' | 'type' | 'region' | 'layer';
  clusterPadding: number;      // Space between clusters (100px)
  nodePadding: number;         // Space between nodes within cluster (30px)
  maxClusterSize: number;      // Max nodes per cluster before splitting (50)
  width: number;
  height: number;
}

export interface Cluster {
  id: string;
  label: string;
  nodes: InstancedNodeData[];
  x: number;  // Cluster center X
  y: number;  // Cluster center Y
  radius: number;  // Cluster radius
}

// HierarchicalLayout computes a two-level layout:
//   Level 1: Arrange clusters in a grid/circle
//   Level 2: Arrange nodes within each cluster using d3-force
//
// ALGORITHM:
//   1. Group nodes by groupBy field
//   2. Compute cluster sizes (proportional to node count)
//   3. Arrange clusters in a grid layout
//   4. Run d3-force simulation within each cluster
//   5. Map node positions to global coordinates
//
// PERFORMANCE: O(n log n) for n nodes
export class HierarchicalLayout {
  private config: ClusterConfig;
  private clusters: Cluster[] = [];
  private simulations: Map<string, d3.Simulation<InstancedNodeData, undefined>> = new Map();

  constructor(config: ClusterConfig) {
    this.config = config;
  }

  // Compute layout for all nodes.
  //
  // RETURNS: Map of nodeId → {x, y} positions
  compute(nodes: InstancedNodeData[], edges: EdgeData[]): Map<string, { x: number; y: number }> {
    // 1. Group nodes into clusters
    this.clusters = this.groupIntoClusters(nodes);

    // 2. Arrange clusters in grid
    this.arrangeClusters();

    // 3. Run d3-force within each cluster
    const positions = new Map<string, { x: number; y: number }>();

    for (const cluster of this.clusters) {
      const clusterPositions = this.layoutCluster(cluster, edges);
      for (const [id, pos] of clusterPositions) {
        positions.set(id, pos);
      }
    }

    return positions;
  }

  // Group nodes by the configured field.
  private groupIntoClusters(nodes: InstancedNodeData[]): Cluster[] {
    const groups = new Map<string, InstancedNodeData[]>();

    for (const node of nodes) {
      const key = this.getGroupKey(node);
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key)!.push(node);
    }

    return Array.from(groups.entries()).map(([key, clusterNodes]) => ({
      id: key,
      label: key,
      nodes: clusterNodes,
      x: 0,
      y: 0,
      radius: this.computeClusterRadius(clusterNodes.length),
    }));
  }

  // Get the group key for a node based on groupBy config.
  private getGroupKey(node: InstancedNodeData): string {
    switch (this.config.groupBy) {
      case 'service': return node.clusterId ?? node.type;
      case 'type': return node.type;
      case 'region': return node.clusterId ?? 'default';
      case 'layer': return node.type; // host > container > service > process
      default: return node.type;
    }
  }

  // Compute cluster radius based on node count.
  private computeClusterRadius(nodeCount: number): number {
    const baseRadius = 50;
    const perNode = 15;
    return baseRadius + nodeCount * perNode;
  }

  // Arrange clusters in a grid layout.
  private arrangeClusters(): void {
    const cols = Math.ceil(Math.sqrt(this.clusters.length));
    const cellWidth = this.config.width / cols;
    const cellHeight = this.config.height / Math.ceil(this.clusters.length / cols);

    this.clusters.forEach((cluster, i) => {
      const col = i % cols;
      const row = Math.floor(i / cols);
      cluster.x = cellWidth * (col + 0.5);
      cluster.y = cellHeight * (row + 0.5);
    });
  }

  // Run d3-force simulation within a single cluster.
  private layoutCluster(
    cluster: Cluster,
    edges: EdgeData[],
  ): Map<string, { x: number; y: number }> {
    const positions = new Map<string, { x: number; y: number }>();

    // Initialize node positions at cluster center
    for (const node of cluster.nodes) {
      node.x = cluster.x + (Math.random() - 0.5) * cluster.radius;
      node.y = cluster.y + (Math.random() - 0.5) * cluster.radius;
    }

    // Filter edges relevant to this cluster
    const nodeIds = new Set(cluster.nodes.map(n => n.id));
    const clusterEdges = edges.filter(e => nodeIds.has(e.source) && nodeIds.has(e.target));

    // Run d3-force simulation
    const simulation = d3.forceSimulation(cluster.nodes)
      .force('charge', d3.forceManyBody().strength(-100))
      .force('center', d3.forceCenter(cluster.x, cluster.y))
      .force('collision', d3.forceCollide().radius(d => (d as InstancedNodeData).size + 5))
      .force('link', d3.forceLink(clusterEdges)
        .id((d: any) => d.id)
        .distance(50)
      )
      .stop();

    // Run synchronously (in worker thread for large graphs)
    for (let i = 0; i < 100; i++) simulation.tick();

    // Extract positions
    for (const node of cluster.nodes) {
      // Constrain to cluster bounds
      const dx = (node.x ?? cluster.x) - cluster.x;
      const dy = (node.y ?? cluster.y) - cluster.y;
      const dist = Math.sqrt(dx * dx + dy * dy);
      if (dist > cluster.radius) {
        const scale = cluster.radius / dist;
        node.x = cluster.x + dx * scale;
        node.y = cluster.y + dy * scale;
      }
      positions.set(node.id, { x: node.x ?? 0, y: node.y ?? 0 });
    }

    this.simulations.set(cluster.id, simulation);
    return positions;
  }

  getClusters(): Cluster[] {
    return this.clusters;
  }

  destroy(): void {
    this.simulations.forEach(s => s.stop());
    this.simulations.clear();
  }
}

interface EdgeData {
  source: string;
  target: string;
  type: string;
}
```

### 3.5 Particle System for Data Flow

**File:** `frontend/src/engine/particles.ts` (~600 LOC)

```typescript
// Particle system for visualizing data flow along edges.
//
// Particles move from source node to target node along edges.
// Visual properties encode data characteristics:
//   - Color: throughput (white = low, gray = medium, dark = high)
//   - Speed: latency (faster = lower latency)
//   - Density: traffic volume (more particles = more traffic)

import * as PIXI from 'pixi.js';

export interface ParticleConfig {
  maxParticles: number;        // 10000 max particles
  particleSize: number;        // 3px
  baseSpeed: number;           // 2px per frame
  spawnRate: number;           // Particles per frame per edge
  lifetime: number;            // Frames before particle dies
  fadeLength: number;          // Frames to fade in/out
}

export interface EdgeParticleData {
  edgeId: string;
  sourceX: number;
  sourceY: number;
  targetX: number;
  targetY: number;
  throughput: number;    // 0.0 - 1.0 (normalized)
  latency: number;       // 0.0 - 1.0 (normalized, lower = faster)
  volume: number;        // 0.0 - 1.0 (normalized, controls density)
}

// ParticleSystem manages all edge particles.
//
// Uses PIXI.ParticleContainer for GPU-accelerated rendering.
// Each edge spawns particles proportional to its traffic volume.
// Particles move along the edge at a speed proportional to latency.
export class ParticleSystem {
  private container: PIXI.ParticleContainer;
  private particles: ParticleInstance[] = [];
  private edgeData: Map<string, EdgeParticleData> = new Map();
  private particleTexture: PIXI.Texture;
  private config: ParticleConfig;
  private frameCount = 0;

  constructor(parentContainer: PIXI.Container, config: Partial<ParticleConfig> = {}) {
    this.config = {
      maxParticles: 10000,
      particleSize: 3,
      baseSpeed: 2,
      spawnRate: 0.5,
      lifetime: 120,
      fadeLength: 10,
      ...config,
    };

    this.container = new PIXI.ParticleContainer(this.config.maxParticles, {
      position: true,
      alpha: true,
      tint: true,
    });
    parentContainer.addChild(this.container);

    // Generate particle texture (small white circle)
    this.particleTexture = this.generateParticleTexture();
  }

  // Update edge data (called when metrics change).
  updateEdges(edges: EdgeParticleData[]): void {
    this.edgeData.clear();
    for (const edge of edges) {
      this.edgeData.set(edge.edgeId, edge);
    }
  }

  // Update particles each frame.
  //
  // PROCESS:
  //   1. Spawn new particles for edges with traffic
  //   2. Update particle positions (move along edge)
  //   3. Update particle alpha (fade in/out)
  //   4. Remove dead particles
  update(): void {
    this.frameCount++;

    // Spawn new particles
    for (const edge of this.edgeData.values()) {
      if (edge.volume > 0.1 && this.particles.length < this.config.maxParticles) {
        // Spawn rate proportional to volume
        const spawnCount = Math.floor(edge.volume * this.config.spawnRate);
        for (let i = 0; i < spawnCount; i++) {
          this.spawnParticle(edge);
        }
      }
    }

    // Update existing particles
    for (let i = this.particles.length - 1; i >= 0; i--) {
      const p = this.particles[i];
      p.age++;

      if (p.age >= this.config.lifetime) {
        // Remove dead particle
        this.container.removeChild(p.sprite);
        p.sprite.destroy();
        this.particles.splice(i, 1);
        continue;
      }

      // Move along edge
      p.progress += p.speed;
      if (p.progress >= 1) {
        p.progress = 0; // Loop
        p.age = 0;
      }

      // Interpolate position
      p.sprite.x = p.sourceX + (p.targetX - p.sourceX) * p.progress;
      p.sprite.y = p.sourceY + (p.targetY - p.sourceY) * p.progress;

      // Fade in/out
      const fadeIn = Math.min(p.age / this.config.fadeLength, 1);
      const fadeOut = Math.min((this.config.lifetime - p.age) / this.config.fadeLength, 1);
      p.sprite.alpha = fadeIn * fadeOut * 0.6;
    }
  }

  // Spawn a single particle on an edge.
  private spawnParticle(edge: EdgeParticleData): void {
    const sprite = new PIXI.Sprite(this.particleTexture);
    sprite.anchor.set(0.5);
    sprite.width = this.config.particleSize;
    sprite.height = this.config.particleSize;

    // Tint based on throughput (monochromatic)
    const brightness = Math.floor(128 + edge.throughput * 127);
    sprite.tint = (brightness << 16) | (brightness << 8) | brightness;

    sprite.x = edge.sourceX;
    sprite.y = edge.sourceY;
    this.container.addChild(sprite);

    const particle: ParticleInstance = {
      sprite,
      sourceX: edge.sourceX,
      sourceY: edge.sourceY,
      targetX: edge.targetX,
      targetY: edge.targetY,
      progress: Math.random(), // Random starting position
      speed: this.config.baseSpeed * (1 - edge.latency * 0.5) / 100,
      age: 0,
    };

    this.particles.push(particle);
  }

  // Generate a small white circle texture for particles.
  private generateParticleTexture(): PIXI.Texture {
    const g = new PIXI.Graphics();
    g.beginFill(0xffffff, 1);
    g.drawCircle(4, 4, 4);
    g.endFill();
    const texture = PIXI.Texture.from(g);
    g.destroy();
    return texture;
  }

  destroy(): void {
    this.particles.forEach(p => p.sprite.destroy());
    this.particles = [];
    this.container.destroy({ children: true });
  }
}

interface ParticleInstance {
  sprite: PIXI.Sprite;
  sourceX: number;
  sourceY: number;
  targetX: number;
  targetY: number;
  progress: number;  // 0.0 - 1.0 along edge
  speed: number;     // Progress increment per frame
  age: number;       // Frames since spawn
}
```

### 3.6 Viewport Controller

**File:** `frontend/src/engine/viewport.ts` (~300 LOC)

```typescript
// Viewport controller for zoom, pan, and fit-to-content.
//
// Manages the camera position and zoom level for the topology canvas.
// Supports mouse wheel zoom, click-drag pan, and programmatic fit.

export interface ViewportState {
  x: number;          // Camera offset X
  y: number;          // Camera offset Y
  zoom: number;       // Zoom level (1.0 = 100%)
  width: number;      // Viewport width in pixels
  height: number;     // Viewport height in pixels
}

export class ViewportController {
  private state: ViewportState;
  private stage: PIXI.Container;
  private minZoom = 0.1;
  private maxZoom = 5.0;
  private isDragging = false;
  private dragStart = { x: 0, y: 0 };

  constructor(stage: PIXI.Container, width: number, height: number) {
    this.stage = stage;
    this.state = { x: 0, y: 0, zoom: 1, width, height };
  }

  // Attach event listeners for mouse/touch interaction.
  attachEvents(canvas: HTMLCanvasElement): void {
    canvas.addEventListener('wheel', this.onWheel.bind(this));
    canvas.addEventListener('pointerdown', this.onPointerDown.bind(this));
    canvas.addEventListener('pointermove', this.onPointerMove.bind(this));
    canvas.addEventListener('pointerup', this.onPointerUp.bind(this));
  }

  // Zoom to fit all content in the viewport.
  fitToContent(bounds: { x: number; y: number; width: number; height: number }): void {
    const padding = 50;
    const scaleX = (this.state.width - padding * 2) / bounds.width;
    const scaleY = (this.state.height - padding * 2) / bounds.height;
    this.state.zoom = Math.min(scaleX, scaleY, this.maxZoom);
    this.state.x = -bounds.x * this.state.zoom + (this.state.width - bounds.width * this.state.zoom) / 2;
    this.state.y = -bounds.y * this.state.zoom + (this.state.height - bounds.height * this.state.zoom) / 2;
    this.applyTransform();
  }

  // Zoom to a specific level, centered on a point.
  zoomTo(level: number, centerX: number, centerY: number): void {
    const oldZoom = this.state.zoom;
    this.state.zoom = Math.max(this.minZoom, Math.min(this.maxZoom, level));

    // Adjust offset to keep center point stable
    const scale = this.state.zoom / oldZoom;
    this.state.x = centerX - (centerX - this.state.x) * scale;
    this.state.y = centerY - (centerY - this.state.y) * scale;
    this.applyTransform();
  }

  getState(): ViewportState {
    return { ...this.state };
  }

  resize(width: number, height: number): void {
    this.state.width = width;
    this.state.height = height;
  }

  private onWheel(e: WheelEvent): void {
    e.preventDefault();
    const delta = e.deltaY > 0 ? 0.9 : 1.1;
    this.zoomTo(this.state.zoom * delta, e.offsetX, e.offsetY);
  }

  private onPointerDown(e: PointerEvent): void {
    if (e.button === 1 || (e.button === 0 && e.shiftKey)) {
      this.isDragging = true;
      this.dragStart = { x: e.clientX - this.state.x, y: e.clientY - this.state.y };
    }
  }

  private onPointerMove(e: PointerEvent): void {
    if (this.isDragging) {
      this.state.x = e.clientX - this.dragStart.x;
      this.state.y = e.clientY - this.dragStart.y;
      this.applyTransform();
    }
  }

  private onPointerUp(): void {
    this.isDragging = false;
  }

  private applyTransform(): void {
    this.stage.x = this.state.x;
    this.stage.y = this.state.y;
    this.stage.scale.set(this.state.zoom);
  }
}
```

### 3.7 Visual Effects

**File:** `frontend/src/engine/effects.ts` (~400 LOC)

```typescript
// Visual effects for the topology canvas.
//
// NOTE: All color values are monochromatic (black/white/gray).
// Will be updated when user's UI/UX rulebook is provided.

import * as PIXI from 'pixi.js';

// HealthGlow adds a subtle glow effect around nodes based on health status.
//
// Healthy: no glow (clean)
// Degraded: subtle gray glow
// Unhealthy: pulsing dark glow
export class HealthGlow {
  private glowFilters: Map<string, PIXI.Filter> = new Map();

  apply(node: PIXI.Sprite, status: string): void {
    // Monochromatic: use alpha/brightness instead of color
    switch (status) {
      case 'degraded':
        node.alpha = 0.8;
        break;
      case 'unhealthy':
        // Pulsing effect via ticker
        node.alpha = 0.6 + Math.sin(Date.now() / 200) * 0.3;
        break;
      default:
        node.alpha = 1.0;
    }
  }
}

// EdgeAnimation animates edges based on traffic.
//
// Low traffic: thin, dim line
// High traffic: thick, bright line with animated dash
export class EdgeAnimation {
  update(edge: PIXI.Graphics, throughput: number): void {
    const lineWidth = 1 + throughput * 3;       // 1-4px
    const alpha = 0.3 + throughput * 0.5;       // 0.3-0.8
    const brightness = Math.floor(128 + throughput * 127);
    const color = (brightness << 16) | (brightness << 8) | brightness;

    edge.clear();
    edge.lineStyle(lineWidth, color, alpha);
    // Edge line is drawn by the edge renderer
  }
}

// AlertPulse adds a pulsing ring effect around nodes with active alerts.
export class AlertPulse {
  private rings: Map<string, PIXI.Graphics> = new Map();

  addPulse(container: PIXI.Container, nodeId: string, x: number, y: number, size: number): void {
    if (this.rings.has(nodeId)) return;

    const ring = new PIXI.Graphics();
    ring.lineStyle(2, 0xffffff, 0.5);
    ring.drawCircle(x, y, size + 5);
    container.addChild(ring);
    this.rings.set(nodeId, ring);
  }

  removePulse(nodeId: string): void {
    const ring = this.rings.get(nodeId);
    if (ring) {
      ring.destroy();
      this.rings.delete(nodeId);
    }
  }

  update(): void {
    const t = Date.now() / 500;
    for (const [, ring] of this.rings) {
      ring.alpha = 0.3 + Math.sin(t) * 0.3;
      ring.scale.set(1 + Math.sin(t) * 0.1);
    }
  }
}
```

---

## 4. Layer 18: Real-Time Data Pipeline

### 4.1 WebSocket Manager Enhancement

**File:** `frontend/src/api/websocket.ts` — Enhance existing (add ~300 LOC):

```typescript
// ADDITIONS to existing WebSocketClient:

// Backpressure: drop old updates if consumer is behind.
// Tracks last processed message ID per channel.
// If messages arrive faster than processed, skip to latest.

// Deduplication: ignore duplicate messages.
// Uses a Set of recent message IDs (last 1000).
// Duplicate messages are silently dropped.

// Message batching: batch multiple updates into single state updates.
// Accumulates messages for 16ms (one frame) before dispatching.
// Reduces React re-renders by 10-100x during high traffic.

// ADD TO WebSocketClient class:

private processedIds: Map<string, string> = new Map(); // channel → last message ID
private recentIds: Set<string> = new Set(); // For deduplication
private batchBuffer: Map<string, unknown[]> = new Map(); // channel → pending messages
private batchTimer: ReturnType<typeof setTimeout> | null = null;

// Process incoming message with backpressure and deduplication
private processMessage(msg: WsIncomingMessage): void {
  const messageId = msg.id ?? `${msg.type}_${Date.now()}`;

  // Deduplication
  if (this.recentIds.has(messageId)) return;
  this.recentIds.add(messageId);
  if (this.recentIds.size > 1000) {
    // Remove oldest entries
    const iter = this.recentIds.values();
    for (let i = 0; i < 100; i++) iter.next();
  }

  // Batch for next frame
  const channel = msg.channel ?? msg.type;
  if (!this.batchBuffer.has(channel)) this.batchBuffer.set(channel, []);
  this.batchBuffer.get(channel)!.push(msg.data ?? msg);

  // Schedule batch flush
  if (!this.batchTimer) {
    this.batchTimer = setTimeout(() => this.flushBatch(), 16);
  }
}

private flushBatch(): void {
  this.batchTimer = null;
  for (const [channel, messages] of this.batchBuffer) {
    const handlers = this.messageHandlers.get(channel);
    if (handlers) {
      // Dispatch batch (last message wins for state, all for append)
      for (const handler of handlers) {
        handler(messages.length === 1 ? messages[0] : messages);
      }
    }
  }
  this.batchBuffer.clear();
}
```

### 4.2 SSE Timeline Enhancement

**File:** `frontend/src/api/sse.ts` — Enhance existing (add ~200 LOC):

```typescript
// ADDITIONS to existing SSEClient:

// Speed control: send speed parameter to server.
// The server adjusts the rate of SSE events based on speed.
// Supported speeds: 0.25x, 0.5x, 1x, 2x, 4x, 8x, 16x

// Pause/Resume: send pause/resume commands.
// When paused, server buffers events and resumes from pause point.

// Seek: send seek command to jump to a specific timestamp.
// Server replays from the nearest snapshot.

// ADD TO SSEClient class or create TimelineSSEClient:

export class TimelineSSEClient extends SSEClient {
  private speed: number = 1;
  private paused: boolean = false;

  setSpeed(speed: number): void {
    this.speed = speed;
    this.sendCommand({ type: 'speed', speed });
  }

  pause(): void {
    this.paused = true;
    this.sendCommand({ type: 'pause' });
  }

  resume(): void {
    this.paused = false;
    this.sendCommand({ type: 'resume' });
  }

  seekTo(timestamp: string): void {
    this.sendCommand({ type: 'seek', timestamp });
  }

  private sendCommand(cmd: unknown): void {
    // SSE is unidirectional, so we use a separate HTTP POST for commands
    fetch('/api/v1/timeline/command', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cmd),
    });
  }
}
```

### 4.3 Web Workers Enhancement

**File:** `frontend/src/engine/workers/topologyLayout.worker.ts` — Enhance:

```typescript
// ADD clustering support to existing layout worker.

// NEW MESSAGE TYPE: 'clustered-layout'
// Input: { nodes, edges, clusters, config }
// Output: { positions, clusterBounds }

// The worker:
// 1. Groups nodes into clusters
// 2. Arranges clusters in grid
// 3. Runs d3-force within each cluster
// 4. Returns global positions + cluster bounding boxes

// This keeps the main thread free for rendering.
```

---

## 5. Layer 19: State Management & Stores

### 5.1 Topology Store Enhancement

**File:** `frontend/src/stores/topologyStore.ts` — Enhance (add ~200 LOC):

```typescript
// ADDITIONS to existing topologyStore:

// Viewport state
viewport: ViewportState;
setViewport: (viewport: Partial<ViewportState>) => void;
zoomIn: () => void;
zoomOut: () => void;
fitToContent: () => void;

// Clustering state
clusters: Cluster[];
activeCluster: string | null;
drillInto: (clusterId: string) => void;
drillOut: () => void;

// Node interaction
hoverNode: (node: TopologyNode | null) => void;
hoveredNode: TopologyNode | null;

// Real-time updates
applyTopologyDiff: (diff: TopologyDiff) => void;
lastUpdateTime: number;
updateLatency: number; // ms since last update
```

### 5.2 Metrics Store Enhancement

**File:** `frontend/src/stores/metricsStore.ts` — Enhance (add ~200 LOC):

```typescript
// ADDITIONS to existing metricsStore:

// Real-time streaming
isStreaming: boolean;
streamingAgentId: string | null;
startStream: (agentId: string) => void;
stopStream: () => void;

// Downsampled views
autoDownsample: boolean;
resolution: 'raw' | '1m' | '5m' | '1h' | '1d';
setResolution: (res: string) => void;

// Multi-metric comparison
comparedMetrics: string[];
toggleCompare: (metricName: string) => void;
```

### 5.3 Timeline Store Enhancement

**File:** `frontend/src/stores/timelineStore.ts` — Enhance (add ~200 LOC):

```typescript
// ADDITIONS to existing timelineStore:

// Snapshot-based replay
availableSnapshots: TimelineSnapshot[];
selectedSnapshot: TimelineSnapshot | null;
loadSnapshot: (id: string) => void;

// Diff viewer
diffMode: boolean;
diffFrom: string | null;
diffTo: string | null;
enableDiff: (from: string, to: string) => void;
disableDiff: () => void;

// Export
exportSnapshot: (format: 'json' | 'pdf' | 'html') => void;
```

---

## 6. Layer 20: UI Components & Views

### 6.1 App Shell

**File:** `frontend/src/components/layout/AppShell.tsx` (~200 LOC)

```typescript
// Main application shell with sidebar navigation and content area.
//
// LAYOUT:
// ┌─────────────────────────────────────────────────┐
// │ Header (search, notifications, settings)        │
// ├──────┬──────────────────────────────────────────┤
// │      │                                          │
// │ Side │  Content Area                            │
// │ bar │  (Topology / Metrics / Timeline / Alerts) │
// │      │                                          │
// ├──────┴──────────────────────────────────────────┤
// │ Status Bar (connection state, agent count)      │
// └─────────────────────────────────────────────────┘

export function AppShell() {
  return (
    <div className="app-shell">
      <Header />
      <div className="app-body">
        <Sidebar />
        <main className="app-content">
          <Routes>
            <Route path="/" element={<TopologyCanvas />} />
            <Route path="/metrics" element={<MetricsView />} />
            <Route path="/timeline" element={<TimelineView />} />
            <Route path="/alerts" element={<AlertView />} />
          </Routes>
        </main>
      </div>
      <StatusBar />
    </div>
  );
}
```

### 6.2 Topology Canvas

**File:** `frontend/src/components/topology/TopologyCanvas.tsx` (~300 LOC)

```typescript
// Main topology visualization canvas.
//
// Integrates:
//   - InstancedNodeRenderer for 15K+ nodes
//   - HierarchicalLayout for clustered positioning
//   - ParticleSystem for data flow visualization
//   - ViewportController for zoom/pan
//   - Node/Edge detail panels

export function TopologyCanvas() {
  const containerRef = useRef<HTMLDivElement>(null);
  const rendererRef = useRef<TopologyRenderer | null>(null);
  const { topology, clusters, viewport } = useTopologyStore();

  useEffect(() => {
    if (!containerRef.current) return;

    const renderer = new TopologyRenderer({
      container: containerRef.current,
      width: containerRef.current.clientWidth,
      height: containerRef.current.clientHeight,
    });
    rendererRef.current = renderer;

    return () => renderer.destroy();
  }, []);

  useEffect(() => {
    if (topology && rendererRef.current) {
      rendererRef.current.update(topology, clusters);
    }
  }, [topology, clusters]);

  return (
    <div className="topology-canvas-container">
      <div ref={containerRef} className="topology-canvas" />
      <TopologyControls />
      <SearchOverlay />
      <ClusterBreadcrumb />
      <NodeDetailPanel />
      <EdgeDetailPanel />
    </div>
  );
}
```

### 6.3 Metric Chart

**File:** `frontend/src/components/metrics/MetricChart.tsx` (~300 LOC)

```typescript
// Time-series metric chart using Recharts.
//
// Features:
//   - Multi-series line chart
//   - Zoom/pan on time axis
//   - Tooltip with exact values
//   - Auto-downsampling for large datasets
//   - Real-time streaming updates

export function MetricChart({ metricName, agentId }: MetricChartProps) {
  const { series, timeRange, resolution } = useMetricsStore();
  const filteredSeries = series.filter(s => s.name === metricName);

  return (
    <ResponsiveContainer width="100%" height={300}>
      <LineChart data={filteredSeries}>
        <XAxis dataKey="timestamp" />
        <YAxis />
        <Tooltip />
        <Line
          type="monotone"
          dataKey="value"
          stroke="var(--color-primary)"
          dot={false}
          isAnimationActive={false}
        />
      </LineChart>
    </ResponsiveContainer>
  );
}
```

### 6.4 Timeline Scrubber

**File:** `frontend/src/components/timeline/TimelineScrubber.tsx` (~250 LOC)

```typescript
// Timeline scrubber for navigating historical data.
//
// Features:
//   - Draggable scrubber handle
//   - Snapshot markers on timeline
//   - Speed controls (0.25x - 16x)
//   - Current time display
//   - Play/Pause/Stop buttons

export function TimelineScrubber() {
  const { position, config, play, pause, stop, setSpeed, seekTo } = useTimelineStore();

  return (
    <div className="timeline-scrubber">
      <div className="scrubber-controls">
        <button onClick={play} disabled={position.state === 'playing'}>▶</button>
        <button onClick={pause} disabled={position.state !== 'playing'}>⏸</button>
        <button onClick={stop}>⏹</button>
        <SpeedControls speed={config.speed} onChange={setSpeed} />
      </div>
      <div className="scrubber-track">
        <input
          type="range"
          min={0} max={1} step={0.001}
          value={position.progress}
          onChange={e => seekTo(Number(e.target.value))}
        />
        <div className="scrubber-markers">
          {/* Snapshot markers rendered here */}
        </div>
      </div>
      <div className="scrubber-info">
        <span>{position.currentTime}</span>
        <span>{config.speed}x</span>
      </div>
    </div>
  );
}
```

---

## 7. Theme & Design System

### 7.1 CRITICAL: UI/UX Rulebook Gate

> **STOP GATE: Before implementing any styling, the implementing agent MUST request the user's UI/UX rulebook file.** The following is a STRUCTURAL placeholder only. All color values, font sizes, spacing, and visual effects must come from the user's rulebook.

### 7.2 CSS Token Structure (Placeholder)

**File:** `frontend/src/styles/tokens.css`:

```css
/*
 * THEME TOKENS — MONOCHROMATIC PLACEHOLDER
 *
 * !! DO NOT USE THESE VALUES DIRECTLY !!
 * !! WAIT FOR USER'S UI/UX RULEBOOK !!
 *
 * Structure is defined. Values will be provided by user.
 */

:root {
  /* === COLORS (Monochromatic) === */
  /* Background layers (darkest to lightest) */
  --color-bg-0: #000000;        /* Deepest background */
  --color-bg-1: #0a0a0a;        /* App background */
  --color-bg-2: #111111;        /* Card/panel background */
  --color-bg-3: #1a1a1a;        /* Elevated surface */
  --color-bg-4: #222222;        /* Hover/active surface */

  /* Foreground layers (dimmest to brightest) */
  --color-fg-0: #333333;        /* Disabled/invisible */
  --color-fg-1: #555555;        /* Muted text */
  --color-fg-2: #888888;        /* Secondary text */
  --color-fg-3: #bbbbbb;        /* Primary text */
  --color-fg-4: #ffffff;        /* Emphasized text */

  /* Accent (monochromatic — only white/gray/black) */
  --color-accent: #ffffff;      /* Primary accent */
  --color-accent-dim: #888888;  /* Dimmed accent */

  /* Status (monochromatic brightness) */
  --color-healthy: #ffffff;     /* Bright white */
  --color-degraded: #888888;    /* Mid gray */
  --color-unhealthy: #444444;   /* Dark gray */
  --color-unknown: #666666;     /* Unknown gray */

  /* === TYPOGRAPHY === */
  --font-sans: 'Geist Sans', -apple-system, BlinkMacSystemFont, sans-serif;
  --font-mono: 'Geist Mono', 'SF Mono', 'Fira Code', monospace;

  --text-xs: 0.75rem;      /* 12px */
  --text-sm: 0.875rem;     /* 14px */
  --text-base: 1rem;       /* 16px */
  --text-lg: 1.125rem;     /* 18px */
  --text-xl: 1.25rem;      /* 20px */
  --text-2xl: 1.5rem;      /* 24px */

  --font-normal: 400;
  --font-medium: 500;
  --font-semibold: 600;
  --font-bold: 700;

  /* === SPACING === */
  --space-1: 0.25rem;      /* 4px */
  --space-2: 0.5rem;       /* 8px */
  --space-3: 0.75rem;      /* 12px */
  --space-4: 1rem;         /* 16px */
  --space-5: 1.25rem;      /* 20px */
  --space-6: 1.5rem;       /* 24px */
  --space-8: 2rem;         /* 32px */

  /* === BORDERS === */
  --border-color: #222222;
  --border-width: 1px;
  --border-radius-sm: 4px;
  --border-radius-md: 6px;
  --border-radius-lg: 8px;

  /* === SHADOWS (subtle, monochromatic) === */
  --shadow-sm: 0 1px 2px rgba(255, 255, 255, 0.05);
  --shadow-md: 0 4px 6px rgba(255, 255, 255, 0.05);
  --shadow-lg: 0 10px 15px rgba(255, 255, 255, 0.05);

  /* === LAYOUT === */
  --sidebar-width: 240px;
  --header-height: 48px;
  --statusbar-height: 28px;
  --panel-width: 360px;
}
```

### 7.3 Typography

**File:** `frontend/src/styles/typography.css`:

```css
/* Geist Sans and Geist Mono font definitions */
/* Load from CDN or local assets */

@font-face {
  font-family: 'Geist Sans';
  src: url('/fonts/GeistSans-Regular.woff2') format('woff2');
  font-weight: 400;
  font-style: normal;
  font-display: swap;
}

@font-face {
  font-family: 'Geist Sans';
  src: url('/fonts/GeistSans-Medium.woff2') format('woff2');
  font-weight: 500;
  font-style: normal;
  font-display: swap;
}

/* ... additional weights ... */

@font-face {
  font-family: 'Geist Mono';
  src: url('/fonts/GeistMono-Regular.woff2') format('woff2');
  font-weight: 400;
  font-style: normal;
  font-display: swap;
}

/* ... additional weights ... */

body {
  font-family: var(--font-sans);
  font-size: var(--text-base);
  color: var(--color-fg-3);
  background-color: var(--color-bg-1);
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

code, pre, .mono {
  font-family: var(--font-mono);
}
```

---

## 8. Verification Gates

### Gate 1: Instanced Rendering Performance (Automated)

```typescript
// Test: 15K nodes render at 60fps
// 1. Generate 15,000 node objects
// 2. Render with InstancedNodeRenderer
// 3. Measure FPS over 10 seconds
// 4. Assert: average FPS >= 55

// Test: Node update latency
// 1. Render 15K nodes
// 2. Update 100 node positions
// 3. Measure update time
// 4. Assert: update time < 5ms

// Test: Memory usage
// 1. Render 15K nodes
// 2. Measure memory usage
// 3. Assert: < 200MB
```

### Gate 2: Clustering Layout (Automated)

```typescript
// Test: Clusters are non-overlapping
// 1. Create 10 clusters with 50 nodes each
// 2. Run HierarchicalLayout
// 3. Assert: no cluster bounding boxes overlap

// Test: Nodes within cluster are connected
// 1. Create cluster with connected nodes
// 2. Run layout
// 3. Assert: connected nodes are closer than unconnected

// Test: Drill-down navigation
// 1. Render full topology
// 2. Click on cluster
// 3. Assert: view zooms to cluster, shows internal nodes
// 4. Click breadcrumb
// 5. Assert: view returns to full topology
```

### Gate 3: Particle System (Automated)

```typescript
// Test: Particles spawn on active edges
// 1. Create edge with volume=0.5
// 2. Run particle update for 60 frames
// 3. Assert: particles.length > 0

// Test: Particles move along edge
// 1. Spawn particle on edge
// 2. Run 10 frames
// 3. Assert: particle.progress increased

// Test: Performance with 10K particles
// 1. Spawn 10,000 particles across 100 edges
// 2. Measure FPS
// 3. Assert: FPS >= 30
```

### Gate 4: WebSocket Backpressure (Automated)

```typescript
// Test: Duplicate messages are dropped
// 1. Send same message ID 10 times
// 2. Assert: handler called only once

// Test: Batch processing
// 1. Send 100 messages in 16ms
// 2. Assert: handler called once with batch of 100

// Test: Reconnection
// 1. Connect WebSocket
// 2. Kill connection
// 3. Assert: reconnects within 5 seconds
```

### Gate 5: Timeline Replay (Automated)

```typescript
// Test: Play/Pause/Stop
// 1. Start timeline replay
// 2. Assert: state === 'playing'
// 3. Pause
// 4. Assert: state === 'paused', progress unchanged
// 5. Stop
// 6. Assert: state === 'stopped', progress === 0

// Test: Seek
// 1. Set timeline range (1 hour)
// 2. Seek to 0.5
// 3. Assert: currentTime is at midpoint

// Test: Speed control
// 1. Set speed to 16x
// 2. Play for 1 second
// 3. Assert: advanced 16 seconds of timeline
```

### Gate 6: Visual Theme (Manual)

```
□ All text uses Geist Sans or Geist Mono
□ All colors are monochromatic (black/white/gray)
□ No colored accents (no blue, green, red — only gray brightness)
□ Status indicators use brightness, not hue
□ Dark background with light text
□ High contrast for readability
□ Canvas background matches app background
□ Node shapes are distinguishable by type
□ Edge particles are visible but not distracting
□ Alert pulses are noticeable but not overwhelming
□ All spacing follows the token system
□ All font sizes follow the token system
```

---

## 9. Performance Targets

### 9.1 Rendering Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| FPS (1K nodes) | >= 60 | Average FPS over 10 seconds |
| FPS (15K nodes) | >= 55 | Average FPS over 10 seconds |
| FPS (50K nodes) | >= 30 | With LOD enabled |
| Node update latency | < 5ms | Time to update 100 nodes |
| Layout computation | < 500ms | 1K nodes, 100 iterations |
| Memory (15K nodes) | < 200 MB | Including textures and particles |

### 9.2 Data Pipeline Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| WebSocket message latency | < 50ms | Server → React render |
| Batch processing | < 16ms | 100 messages per batch |
| Reconnection time | < 5s | After connection loss |
| SSE timeline throughput | > 100 events/sec | At 1x speed |

### 9.3 User Interaction Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| Zoom response | < 16ms | Mouse wheel to render |
| Pan response | < 16ms | Drag to render |
| Node click → detail panel | < 100ms | Click to panel visible |
| Search results | < 200ms | Typing to results shown |
| Timeline seek | < 500ms | Drag to topology updated |

---

## 10. Contingency & Rollback

### 10.1 Rollback Strategy

**Scenario 1: Instanced rendering doesn't scale**
- Fall back to current individual Graphics rendering
- Add viewport culling (only render visible nodes)
- Reduce node detail (simpler shapes)

**Scenario 2: Clustering layout is too slow**
- Run layout in Web Worker (already planned)
- Cache layout results (only recompute on topology change)
- Use simple grid layout as fallback

**Scenario 3: Particle system causes frame drops**
- Reduce max particles (10K → 5K → 2K)
- Reduce spawn rate
- Disable particles for low-end devices

**Scenario 4: WebSocket backpressure drops important messages**
- Add priority levels (topology > metrics > particles)
- Ensure topology updates are never dropped
- Buffer critical messages separately

### 10.2 Graceful Degradation

| Feature | Full | Degraded | Minimal |
|---------|------|----------|---------|
| Topology nodes | 15K instanced | 5K individual | 1K with culling |
| Particles | Full particle system | Edge glow only | No effects |
| Layout | Hierarchical clustering | d3-force only | Grid layout |
| Real-time | WebSocket + batching | WebSocket basic | REST polling |
| Timeline | SSE with speed control | SSE basic | Manual refresh |

---

## 11. Appendices

### Appendix A: Files to Create/Modify

| File | Action | LOC | Description |
|------|--------|-----|-------------|
| `frontend/src/engine/instancing.ts` | CREATE | ~500 | Instanced node renderer |
| `frontend/src/engine/clustering.ts` | CREATE | ~400 | Hierarchical clustering layout |
| `frontend/src/engine/particles.ts` | CREATE | ~600 | Edge particle system |
| `frontend/src/engine/effects.ts` | CREATE | ~400 | Visual effects |
| `frontend/src/engine/viewport.ts` | CREATE | ~300 | Zoom/pan controller |
| `frontend/src/engine/textures/generateTextures.ts` | CREATE | ~200 | Runtime texture generation |
| `frontend/src/engine/textures/nodeAtlas.ts` | CREATE | ~200 | Texture atlas manager |
| `frontend/src/engine/pixiApp.ts` | REWRITE | ~300 | Updated for instancing |
| `frontend/src/engine/renderer.ts` | REWRITE | ~400 | Updated for clustering |
| `frontend/src/engine/workers/topologyLayout.worker.ts` | ENHANCE | +200 | Clustering support |
| `frontend/src/engine/workers/dataParser.worker.ts` | ENHANCE | +100 | Protobuf support |
| `frontend/src/api/websocket.ts` | ENHANCE | +300 | Backpressure, batching |
| `frontend/src/api/sse.ts` | ENHANCE | +200 | Speed control, seek |
| `frontend/src/api/rest.ts` | ENHANCE | +200 | New endpoints |
| `frontend/src/stores/topologyStore.ts` | ENHANCE | +200 | Viewport, clustering |
| `frontend/src/stores/metricsStore.ts` | ENHANCE | +200 | Streaming, downsample |
| `frontend/src/stores/timelineStore.ts` | ENHANCE | +200 | Snapshots, diff |
| `frontend/src/stores/alertsStore.ts` | ENHANCE | +150 | Grouping, acknowledge |
| `frontend/src/stores/settingsStore.ts` | ENHANCE | +100 | Theme preferences |
| `frontend/src/components/layout/AppShell.tsx` | CREATE | ~200 | Main app shell |
| `frontend/src/components/layout/Sidebar.tsx` | CREATE | ~150 | Navigation sidebar |
| `frontend/src/components/layout/Header.tsx` | CREATE | ~150 | Top bar |
| `frontend/src/components/layout/StatusBar.tsx` | CREATE | ~100 | Bottom status bar |
| `frontend/src/components/topology/TopologyCanvas.tsx` | CREATE | ~300 | Main canvas |
| `frontend/src/components/topology/NodeDetailPanel.tsx` | CREATE | ~200 | Node detail |
| `frontend/src/components/topology/EdgeDetailPanel.tsx` | CREATE | ~150 | Edge detail |
| `frontend/src/components/topology/TopologyControls.tsx` | CREATE | ~150 | Zoom/fit controls |
| `frontend/src/components/topology/SearchOverlay.tsx` | CREATE | ~150 | Node search |
| `frontend/src/components/topology/ClusterBreadcrumb.tsx` | CREATE | ~100 | Drill-down breadcrumb |
| `frontend/src/components/metrics/MetricChart.tsx` | CREATE | ~300 | Time-series chart |
| `frontend/src/components/metrics/MetricCards.tsx` | CREATE | ~200 | Summary cards |
| `frontend/src/components/metrics/Sparkline.tsx` | CREATE | ~100 | Inline sparkline |
| `frontend/src/components/metrics/TimeRangeSelector.tsx` | CREATE | ~150 | Time range picker |
| `frontend/src/components/timeline/TimelineScrubber.tsx` | CREATE | ~250 | Timeline scrubber |
| `frontend/src/components/timeline/SpeedControls.tsx` | CREATE | ~100 | Playback speed |
| `frontend/src/components/timeline/DiffViewer.tsx` | CREATE | ~200 | Diff comparison |
| `frontend/src/components/timeline/SnapshotList.tsx` | CREATE | ~150 | Snapshot list |
| `frontend/src/components/alerts/AlertList.tsx` | CREATE | ~200 | Alert list |
| `frontend/src/components/alerts/AlertCard.tsx` | CREATE | ~150 | Alert card |
| `frontend/src/components/alerts/AlertPanel.tsx` | CREATE | ~150 | Alert detail |
| `frontend/src/hooks/useViewport.ts` | CREATE | ~100 | Viewport hook |
| `frontend/src/hooks/useKeyboard.ts` | CREATE | ~100 | Keyboard shortcuts |
| `frontend/src/hooks/useTopology.ts` | ENHANCE | +100 | Clustering |
| `frontend/src/hooks/useMetrics.ts` | ENHANCE | +100 | Streaming |
| `frontend/src/hooks/useTimeline.ts` | ENHANCE | +100 | Snapshots |
| `frontend/src/styles/tokens.css` | CREATE | ~100 | Theme tokens |
| `frontend/src/styles/globals.css` | CREATE | ~100 | Global styles |
| `frontend/src/styles/typography.css` | CREATE | ~80 | Font definitions |
| `frontend/src/styles/utilities.css` | CREATE | ~100 | Utility classes |
| `frontend/src/App.tsx` | REWRITE | ~50 | Updated routing |
| `frontend/src/index.css` | UPDATE | ~20 | Import new styles |
| `frontend/package.json` | UPDATE | +10 | New dependencies |
| **TOTAL** | | **~10,400** | |

### Appendix B: Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| `Ctrl+K` | Open search overlay |
| `Ctrl+1-4` | Switch view (Topology/Metrics/Timeline/Alerts) |
| `+` / `-` | Zoom in/out |
| `0` | Fit to content |
| `Space` | Play/Pause timeline |
| `Escape` | Close panel / deselect |
| `F` | Focus selected node |
| `R` | Refresh data |

---

**END OF PHASE 5 HARDENED SPECIFICATION**
