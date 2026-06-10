/**
 * PixiJS Topology Application — GPU-accelerated topology visualization (v8)
 *
 * Container hierarchy (PixiJS v8 with viewport culling):
 *   stage > [edgeGfx (single Graphics), particleContainer (ParticleContainer),
 *            nodeContainer (ParticleContainer), effectContainer, labelContainer]
 *
 * Key v8 optimizations:
 *   - cullable: true on node/particle/label containers (automatic viewport culling)
 *   - ParticleContainer for nodes (batched draw calls, lightweight Particle objects)
 *   - ParticleContainer for particles (single draw call for all particles)
 *   - Single shared GraphicsGeometry for ALL edges (1 draw call instead of 30K)
 *   - BitmapText for labels (shared GPU texture, no per-label rasterization)
 *   - Zoom-based LOD (hide labels/particles at far zoom)
 *   - Smart effects (skip healthy nodes, only track changing edges)
 *
 * Backward-compatible with existing PixiAppConfig, NodeRenderData, EdgeRenderData interfaces.
 */

import * as PIXI from 'pixi.js';
import { TextureAtlas } from './textures/nodeAtlas';
import { InstancedNodeRenderer, type InstancedNodeData } from './instancing';
import { ViewportController, type ViewportState } from './viewport';
import { SemanticParticleSystem } from './particles';
import { EffectManager, type AlertData } from './effects';
import { EventIngest } from './eventIngest';
import { EventPathResolver } from './eventPathResolver';
import { useParticleStore } from '../stores/particleStore';
import { subscribeTransportMode } from './transportSubscription';
import { type BudgetLevel } from './memoryBudget';
import type { Topology } from '../types/topology';
import type { TransportMode } from '../types/event';

// ---------------------------------------------------------------------------
// Public Interfaces (backward-compatible)
// ---------------------------------------------------------------------------

/** Configuration for creating the PixiJS topology application. */
export interface PixiAppConfig {
  width: number;
  height: number;
  backgroundColor?: number;
  antialias?: boolean;
  resolution?: number;
  /**
   * Optional OffscreenCanvas. When provided, the app renders into it and runs
   * in worker-safe mode: no DOM event wiring, no `window`/`document` access,
   * and pointer interaction is driven via {@link PixiTopologyApp.forwardPointer}
   * instead of the canvas's federated DOM events.
   */
  view?: OffscreenCanvas;
}

/** Data required to render a single node (backward-compatible). */
export interface NodeRenderData {
  id: string;
  x: number;
  y: number;
  type: string;
  status: string;
  label: string;
  size: number;
}

/** Data required to render a single edge (backward-compatible). */
export interface EdgeRenderData {
  id: string;
  sourceX: number;
  sourceY: number;
  targetX: number;
  targetY: number;
  type: string;
  throughput?: number;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Design system background color. */
const BG_COLOR = 0x000000;

/** Design system text primary color. */
const TEXT_PRIMARY = 0xffffff;

/** FPS display update interval (frames). */
const FPS_UPDATE_INTERVAL = 30;

/** Label font stack. */
const LABEL_FONT_FAMILY = 'Inter, system-ui, -apple-system, sans-serif';

/** Label font size (BitmapText). */
const LABEL_FONT_SIZE = 11;

/** BitmapFont name registered at startup. */
const BITMAP_FONT_NAME = 'ParytyTopology';

/** Minimum world-space radius for worker-mode pointer hit-testing. */
const MIN_HIT_RADIUS = 8;

// ─── Zoom LOD Thresholds ───────────────────────────────────────

/** Below this zoom, show only cluster boundary circles. */
const ZOOM_CLUSTER_ONLY = 0.15;
/** Below this zoom, hide labels and particles. */
const ZOOM_NO_LABELS = 0.4;

// ─── Effects Budget ────────────────────────────────────────────

/** Only track this many unhealthy nodes for per-frame alpha pulsing. */
const MAX_TRACKED_UNHEALTHY = 200;

/** Edge line width range (Task 4 — baked into single Graphics stroke). */
const EDGE_WIDTH_MIN = 1;
const EDGE_WIDTH_MAX = 4;

/** Edge alpha range. */
const EDGE_ALPHA_MIN = 0.3;
const EDGE_ALPHA_MAX = 0.8;

/** Unhealthy pulse range. */
const UNHEALTHY_ALPHA_MIN = 0.6;
const UNHEALTHY_ALPHA_MAX = 0.9;

/** Pulse speed for unhealthy node alpha oscillation. */
const PULSE_SPEED = 0.05;

// ---------------------------------------------------------------------------
// PixiTopologyApp
// ---------------------------------------------------------------------------

/**
 * Main PixiJS application for topology rendering.
 *
 * Orchestrates container layers, node rendering, edge drawing,
 * particle animation, viewport control, and visual effects.
 */
export class PixiTopologyApp {
  /** Underlying PIXI.Application. */
  app!: PIXI.Application;

  /** Single shared Graphics object for ALL edges (1 draw call). */
  readonly edgeGfx: PIXI.Graphics;
  /** ParticleContainer for event-driven particles (cullable, batched). */
  readonly particleContainer: PIXI.ParticleContainer;
  /** Container for instanced node sprites (cullable, Task 2). */
  readonly nodeContainer: PIXI.Container;
  /** Container layer for visual effects (pulses, glows). */
  readonly effectContainer: PIXI.Container;
  /** Container layer for text labels (cullable). */
  readonly labelContainer: PIXI.Container;

  /** Texture atlas (lazy-initialized on first render). */
  private textureAtlas: TextureAtlas | null = null;
  /** Instanced node renderer (lazy-initialized). */
  private nodeRenderer: InstancedNodeRenderer | null = null;
  /** Viewport controller. */
  private viewportController: ViewportController | null = null;
  /** Semantic particle system (event-driven, replaces old edge-based system). */
  private semanticParticleSystem: SemanticParticleSystem | null = null;
  /** Visual effects manager. */
  private effectManager: EffectManager | null = null;
  /** Event ingest pipeline (polling REST API → RingBuffer). */
  private eventIngest: EventIngest | null = null;
  /** Event path resolver (topology traversal for multi-hop paths). */
  private pathResolver: EventPathResolver | null = null;

  /** Whether glow effects are throttled (soft/hard budget). */
  private glowThrottled = false;

  /** Per-frame alpha writes only for unhealthy/degraded nodes (Task 6). */
  private unhealthyNodes: Map<string, { sprite: PIXI.Sprite; status: string; pulsePhase: number }> = new Map();

  /** Cached topology for particle path resolution (updated each updateNodes/updateEdges). */
  private topologyCache: Topology | null = null;
  /** Cached node positions for particle path resolution. */
  private nodePositions: Map<string, { x: number; y: number }> = new Map();
  /** Previous visible count for debouncing particle store updates. */
  private prevVisibleCount = -1;

  /** Active label BitmapText objects keyed by node ID. */
  private labels: Map<string, PIXI.BitmapText> = new Map();

  /** Stored node data for re-rendering (preserves type/status metadata). */
  private nodeDataMap: Map<string, NodeRenderData> = new Map();

  /** Last known zoom level for LOD transitions. */
  private currentZoom = 1.0;
  /** Whether labels are currently hidden (LOD). */
  private labelsHidden = false;
  /** Whether particles are currently hidden (LOD). */
  private particlesHidden = false;

  /** Edge opacity multiplier from transport degradation (CSS --aef-edge-opacity). */
  private edgeOpacityMultiplier = 1.0;

  /** Animation state. */
  private running = false;
  private destroyed = false;
  private rafId = 0;

  /** FPS tracking. */
  private fpsCounter = 0;
  private fpsTimer = 0;
  private currentFps = 0;

  /** Callbacks for node interactions. */
  private clickCallback: ((id: string) => void) | null = null;
  private hoverCallback: ((id: string) => void) | null = null;

  /** Unsubscribe handle for the particle-store transport-mode subscription. */
  private transportUnsub: (() => void) | null = null;

  /**
   * True when rendering into an OffscreenCanvas inside a worker. In this mode
   * the app must not touch `window`/`document`, must not attach DOM event
   * listeners, and receives pointer input via {@link forwardPointer}.
   */
  private readonly workerMode: boolean;

  constructor(config: PixiAppConfig) {
    this.workerMode = config.view !== undefined;
    const inWorkerMode = this.workerMode;

    // Create PIXI Application and init asynchronously.
    // In v8, Application is created then init() sets up the renderer.
    this.app = new PIXI.Application();
    void this.app.init({
      canvas: inWorkerMode ? (config.view as unknown as HTMLCanvasElement) : undefined,
      width: config.width,
      height: config.height,
      backgroundColor: config.backgroundColor ?? BG_COLOR,
      antialias: config.antialias ?? true,
      resolution:
        config.resolution ??
        (typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1),
      autoDensity: !inWorkerMode,
    });

    // Create container hierarchy
    // Single Graphics for all edges — rebuilt on structure change only
    this.edgeGfx = new PIXI.Graphics();

    // ParticleContainer for particles — cullable, batched (1 draw call)
    this.particleContainer = new PIXI.ParticleContainer({
      dynamicProperties: { position: true, alpha: true },
    });
    this.particleContainer.cullable = true;

    // Standard containers for nodes, effects and labels
    this.nodeContainer = new PIXI.Container();
    this.nodeContainer.cullable = true;
    this.effectContainer = new PIXI.Container();
    this.labelContainer = new PIXI.Container();
    this.labelContainer.cullable = true;

    const stage = this.app.stage;
    stage.addChild(this.edgeGfx);
    stage.addChild(this.particleContainer);
    stage.addChild(this.nodeContainer);
    stage.addChild(this.effectContainer);
    stage.addChild(this.labelContainer);

    // Register BitmapFont once at startup (shared GPU texture for all labels)
    if (typeof window !== 'undefined') {
      PIXI.BitmapFont.install({
        name: BITMAP_FONT_NAME,
        style: {
          fontFamily: LABEL_FONT_FAMILY,
          fontSize: LABEL_FONT_SIZE,
          fill: TEXT_PRIMARY,
        },
      });
    }

    // Initialize subsystems
    this.initializeSubsystems(config);

    // Set up interaction and lifecycle handlers
    this.setupInteraction();
    this.setupWebGLRecovery();
    this.startAnimation();
  }

  /**
   * Initializes texture atlas, renderers, viewport, particles, and effects.
   */
  private initializeSubsystems(config: PixiAppConfig): void {
    const renderer = this.app.renderer as PIXI.Renderer;

    // Texture atlas (lazy — generates on first use)
    this.textureAtlas = new TextureAtlas(renderer);

    // Instanced node renderer (ParticleContainer-based, pre-allocates 15K sprite pool)
    this.nodeRenderer = new InstancedNodeRenderer(
      this.nodeContainer,
      this.textureAtlas,
    );

    // Viewport controller
    this.viewportController = new ViewportController(
      this.app.stage,
      config.width,
      config.height,
    );
    // DOM event wiring only on the main thread. In worker mode the host
    // forwards pointer input via forwardPointer() into the same input methods.
    if (!this.workerMode) {
      this.viewportController.attachEvents(this.view);
    }

    // Effect manager
    this.effectManager = new EffectManager(this.effectContainer);

    // Event path resolver (topology traversal for multi-hop paths)
    this.pathResolver = new EventPathResolver();

    // Semantic particle system (event-driven, replaces old ParticleSystem)
    this.semanticParticleSystem = new SemanticParticleSystem(
      this.particleContainer,
      this.textureAtlas.getParticleTexture(),
    );

    // Event ingest pipeline (tri-layer: WS → REST → SSE)
    this.eventIngest = new EventIngest({
      restIntervalMs: 5000,
      queueCapacity: 500,
      graceWindowMs: 5000,
      restFailureThreshold: 3,
    });
    this.eventIngest.start();

    // Subscribe to transport mode changes for visual degradation.
    // Capture the unsubscribe handle so destroy() can detach it. A discarded
    // handle here pins this entire app (renderer, texture atlas, sprite pool)
    // in the module-level particle store forever — a multi-GB leak that
    // compounds across StrictMode double-mounts and route navigations.
    this.transportUnsub = subscribeTransportMode((mode) =>
      this.applyTransportDegradation(mode),
    );

    // Push updated viewport bounds to the particle system on every pan/zoom.
    // This keeps the incremental visibleParticleCount accurate without
    // requiring an O(n) scan every frame.
    this.viewportController?.onChange((state) => {
      if (!this.semanticParticleSystem || !this.viewportController) return;
      const bounds = this.viewportController.getVisibleBounds();
      this.semanticParticleSystem.setViewportBounds(
        bounds.minX, bounds.minY, bounds.maxX, bounds.maxY,
      );
      void state; // bounds computed from the controller, not from the callback arg
    });
  }

  /**
   * Sets up node click and hover event delegation from InstancedNodeRenderer.
   */
  private setupInteraction(): void {
    if (!this.nodeRenderer) return;

    this.nodeRenderer.onNodeClick = (id: string) => {
      if (this.clickCallback) this.clickCallback(id);
    };

    this.nodeRenderer.onNodeHover = (id: string) => {
      if (this.hoverCallback) this.hoverCallback(id);
    };
  }

  /**
   * Sets up WebGL context loss/restored event handlers.
   */
  private setupWebGLRecovery(): void {
    // OffscreenCanvas does not emit the DOM 'webglcontextlost' event the same
    // way and is not wired for DOM listeners in worker mode.
    if (this.workerMode) return;
    const canvas = this.view;

    canvas.addEventListener('webglcontextlost', (e) => {
      e.preventDefault();
      this.running = false;
    });

    canvas.addEventListener('webglcontextrestored', () => {
      this.running = true;
      this.startAnimation();
    });
  }

  /**
   * Starts the animation loop using requestAnimationFrame.
   */
  private startAnimation(): void {
    if (this.running || this.destroyed) return;
    this.running = true;
    this.fpsTimer = performance.now();
    this.rafId = requestAnimationFrame(() => this.animate());
  }

  /**
   * Per-frame animation loop.
   */
  private animate(): void {
    if (!this.running || this.destroyed) return;

    // Zoom-based LOD (Task 9)
    const zoom = this.viewportController?.currentZoom ?? 1.0;
    this.applyZoomLOD(zoom);

    // Update effects (only unhealthy nodes + alert pulses)
    this.effectManager?.update();

    // Update per-frame unhealthy node pulsers (Task 6 — lightweight, only tracked nodes)
    this.updateUnhealthyPulse();

    // Update semantic particles (skip when throttled by frame budget)
    if (
      !this.effectManager?.isThrottled &&
      this.semanticParticleSystem &&
      this.eventIngest &&
      this.pathResolver &&
      this.topologyCache
    ) {
      this.semanticParticleSystem.update(
        this.eventIngest,
        this.pathResolver,
        this.topologyCache,
        this.nodePositions,
      );

      // Viewport-culled particle count for StatusBar.
      //
      // The count is maintained incrementally inside SemanticParticleSystem
      // as particles move each frame — O(1) read here instead of an O(n)
      // scan. The viewport bounds are pushed to the particle system on every
      // viewport change via the onChange callback registered in
      // initializeSubsystems, so visibleParticleCount is always current.
      const visibleCount = this.semanticParticleSystem.visibleParticleCount;
      if (visibleCount !== this.prevVisibleCount) {
        this.prevVisibleCount = visibleCount;
        useParticleStore.getState().setVisualParticleCount(visibleCount);
      }
    }

    // FPS tracking
    this.fpsCounter++;
    if (this.fpsCounter >= FPS_UPDATE_INTERVAL) {
      const now = performance.now();
      this.currentFps = Math.round(
        (this.fpsCounter * 1000) / (now - this.fpsTimer),
      );
      this.fpsCounter = 0;
      this.fpsTimer = now;
    }

    // Schedule next frame
    this.rafId = requestAnimationFrame(() => this.animate());
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /** Returns the canvas element used by the PIXI renderer. */
  get view(): HTMLCanvasElement {
    return this.app.view as unknown as HTMLCanvasElement;
  }

  /** Returns the current FPS. */
  get fps(): number {
    return this.currentFps;
  }

  /** Returns the viewport controller (or null if not initialized). */
  get viewport(): ViewportController | null {
    return this.viewportController;
  }

  /**
   * Handles container resize. Updates renderer, viewport, and re-renders edges.
   *
   * @param width - New width in pixels
   * @param height - New height in pixels
   */
  resize(width: number, height: number): void {
    this.app.renderer.resize(width, height);
    this.viewportController?.resize(width, height);
  }

  /**
   * Updates all node sprites. Delegates to InstancedNodeRenderer.
   * Also updates labels and health glow effects.
   *
   * @param nodes - Current node render data
   */
  updateNodes(nodes: NodeRenderData[]): void {
    // Build instanced node data for the renderer
    const instancedData: InstancedNodeData[] = nodes.map((n) => ({
      id: n.id,
      x: n.x,
      y: n.y,
      type: n.type,
      status: n.status,
      size: n.size,
      label: n.label,
    }));

    // Update instanced renderer
    this.nodeRenderer?.update(instancedData);

    // Update labels
    this.updateLabels(nodes);

    // Update health glow effects
    this.updateHealthGlow(nodes);

    // Store node data for re-rendering (fixes metadata loss bug)
    for (const node of nodes) {
      this.nodeDataMap.set(node.id, node);
    }

    // Purge stale entries from nodeDataMap to prevent unbounded memory growth
    const currentNodeIds = new Set(nodes.map((n) => n.id));
    for (const id of this.nodeDataMap.keys()) {
      if (!currentNodeIds.has(id)) {
        this.nodeDataMap.delete(id);
      }
    }

    // Build node positions map for particle path resolution
    this.nodePositions.clear();
    for (const node of nodes) {
      this.nodePositions.set(node.id, { x: node.x, y: node.y });
    }
  }

  /**
   * Manages label BitmapText objects: adds new, updates positions, removes stale.
   * All labels share a single GPU texture via BitmapFont (Task 5).
   */
  private updateLabels(nodes: NodeRenderData[]): void {
    const currentIds = new Set(nodes.map((n) => n.id));

    // Remove stale labels
    for (const [id, label] of this.labels) {
      if (!currentIds.has(id)) {
        label.destroy();
        this.labels.delete(id);
      }
    }

    // Add/update labels
    for (const node of nodes) {
      let label = this.labels.get(node.id);

      if (!label) {
        label = new PIXI.BitmapText({
          text: node.label,
          style: { fontFamily: BITMAP_FONT_NAME, fontSize: LABEL_FONT_SIZE },
        });
        label.anchor = { x: 0.5, y: 0 };
        this.labelContainer.addChild(label);
        this.labels.set(node.id, label);
      }

      // Position label below the node
      label.x = node.x;
      label.y = node.y + node.size + 4;
      label.visible = true;
    }
  }

  /**
   * Synchronizes health glow: tracks only unhealthy/degraded nodes for per-frame pulsing (Task 6).
   * Healthy nodes are set once at registration and never touched per-frame.
   */
  private updateHealthGlow(nodes: NodeRenderData[]): void {
    // Skip glow updates when throttled by memory budget
    if (this.glowThrottled) return;

    const currentNodeIds = new Set(nodes.map((n) => n.id));

    // Remove stale unhealthy entries
    for (const id of this.unhealthyNodes.keys()) {
      if (!currentNodeIds.has(id)) {
        this.unhealthyNodes.delete(id);
      }
    }

    // Track only unhealthy/degraded nodes (healthy nodes are set once and skipped per-frame)
    for (const node of nodes) {
      if (node.status === 'healthy' || node.status === 'unknown') {
        this.unhealthyNodes.delete(node.id);
        continue;
      }

      // Cap tracked unhealthy nodes to prevent unbounded per-frame work
      if (this.unhealthyNodes.size >= MAX_TRACKED_UNHEALTHY && !this.unhealthyNodes.has(node.id)) {
        continue;
      }

      const sprite = this.nodeRenderer?.getNodeSprite(node.id);
      if (sprite) {
        this.unhealthyNodes.set(node.id, {
          sprite,
          status: node.status,
          pulsePhase: Math.random() * Math.PI * 2,
        });
      }
    }
  }

  /**
   * Per-frame alpha pulsing for unhealthy/degraded nodes only (Task 6).
   * Healthy nodes are set once and never touched — saves 45K+ alpha writes per frame.
   */
  private updateUnhealthyPulse(): void {
    for (const [, node] of this.unhealthyNodes) {
      if (node.status === 'unhealthy') {
        node.pulsePhase += PULSE_SPEED;
        const pulse = (Math.sin(node.pulsePhase) + 1) / 2;
        node.sprite.alpha = UNHEALTHY_ALPHA_MIN +
          (UNHEALTHY_ALPHA_MAX - UNHEALTHY_ALPHA_MIN) * pulse;
      } else {
        // degraded
        node.sprite.alpha = 0.8;
      }
    }
  }

  /**
   * Zoom-based Level of Detail (Task 9).
   * Hides labels/particles at far zoom, shows everything at close zoom.
   */
  private applyZoomLOD(zoom: number): void {
    if (zoom === this.currentZoom) return;
    this.currentZoom = zoom;

    if (zoom < ZOOM_CLUSTER_ONLY) {
      // Cluster-only: hide everything, show cluster boundaries
      if (!this.labelsHidden) {
        this.labelContainer.visible = false;
        this.labelsHidden = true;
      }
      if (!this.particlesHidden) {
        this.particleContainer.visible = false;
        this.particlesHidden = true;
      }
      this.nodeContainer.visible = false;
      this.edgeGfx.visible = false;
    } else if (zoom < ZOOM_NO_LABELS) {
      // Mid zoom: nodes + edges visible, labels + particles hidden
      this.nodeContainer.visible = true;
      this.edgeGfx.visible = true;
      if (!this.labelsHidden) {
        this.labelContainer.visible = false;
        this.labelsHidden = true;
      }
      if (!this.particlesHidden) {
        this.particleContainer.visible = false;
        this.particlesHidden = true;
      }
    } else {
      // Full detail
      this.nodeContainer.visible = true;
      this.edgeGfx.visible = true;
      if (this.labelsHidden) {
        this.labelContainer.visible = true;
        this.labelsHidden = false;
      }
      if (this.particlesHidden) {
        this.particleContainer.visible = true;
        this.particlesHidden = false;
      }
    }
  }

  /**
   * Updates all edges as a single batched GraphicsGeometry (Task 4).
   * All edges are drawn into ONE Graphics object — 1 draw call regardless of edge count.
   * Rebuild only on structure change; skip entirely on status-only polls.
   *
   * @param edges - Current edge render data
   */
  updateEdges(edges: EdgeRenderData[]): void {
    // Rebuild the entire edge geometry as a single moveTo/lineTo batch
    this.edgeGfx.clear();

    for (const edge of edges) {
      const throughput = edge.throughput ?? 0;
      const t = Math.max(0, Math.min(1, throughput));
      const lineWidth = EDGE_WIDTH_MIN + (EDGE_WIDTH_MAX - EDGE_WIDTH_MIN) * t;
      const color = getEdgeColor(edge.type);
      const alpha = (EDGE_ALPHA_MIN + (EDGE_ALPHA_MAX - EDGE_ALPHA_MIN) * t) *
        this.edgeOpacityMultiplier;

      this.edgeGfx.moveTo(edge.sourceX, edge.sourceY);
      this.edgeGfx.lineTo(edge.targetX, edge.targetY);
      this.edgeGfx.stroke({ width: lineWidth, color, alpha });
    }
  }

  /**
   * Highlights a selected node by ID.
   *
   * @param id - Node ID to select (or null to deselect)
   */
  selectNode(id: string | null): void {
    // Node selection is handled at the effect layer

    if (id) {
      const pos = this.nodeRenderer?.getNodePosition(id);
      if (pos && this.effectManager) {
        // Add selection indicator as an alert pulse
        this.effectManager.alertPulse.addPulse({
          nodeId: id,
          worldX: pos.x,
          worldY: pos.y,
          severity: 'info',
        });
      }
    }
  }

  /**
   * Zooms the viewport to center on a specific node.
   *
   * @param id - Node ID to zoom to
   */
  zoomToNode(id: string): void {
    const pos = this.nodeRenderer?.getNodePosition(id);
    if (pos && this.viewportController) {
      this.viewportController.zoomTo(2.0, pos.x, pos.y);
    }
  }

  /**
   * Sets the viewport to a specific state.
   *
   * @param state - Target viewport state
   */
  setViewport(state: ViewportState): void {
    if (this.viewportController) {
      this.viewportController.zoomTo(state.zoom, state.x, state.y);
    }
  }

  /**
   * Adds an alert pulse effect at a node's location.
   *
   * @param alert - Alert data
   */
  addAlert(alert: AlertData): void {
    this.effectManager?.alertPulse.addPulse(alert);
  }

  /**
   * Registers a callback for node click events.
   */
  onNodeClick(callback: (id: string) => void): void {
    this.clickCallback = callback;
  }

  /**
   * Registers a callback for node hover events.
   */
  onNodeHover(callback: (id: string) => void): void {
    this.hoverCallback = callback;
  }

  /**
   * Forwards a pointer interaction in worker mode.
   *
   * On the main thread, PixiJS federated events and the viewport's DOM
   * listeners handle interaction directly. In an OffscreenCanvas worker there
   * is no DOM, so the host thread forwards canvas-relative pointer coordinates
   * here. Pan/zoom is delegated to the same {@link ViewportController} input
   * methods the DOM handlers use, and node click/hover is resolved by a
   * geometric hit test against cached node positions (the federated event
   * system is unavailable off the main thread).
   *
   * @param kind - Interaction kind.
   * @param screenX - Canvas-relative X in CSS pixels.
   * @param screenY - Canvas-relative Y in CSS pixels.
   * @param opts - Wheel delta and button/modifier state where applicable.
   * @returns The hovered node id (or null) for `move`, otherwise null.
   */
  forwardPointer(
    kind: 'move' | 'down' | 'up' | 'wheel',
    screenX: number,
    screenY: number,
    opts?: { deltaY?: number; button?: number; shiftKey?: boolean },
  ): string | null {
    const vp = this.viewportController;
    if (!vp) return null;

    switch (kind) {
      case 'wheel':
        vp.inputWheel(screenX, screenY, opts?.deltaY ?? 0);
        return null;
      case 'down': {
        const started = vp.inputPointerDown(
          screenX,
          screenY,
          opts?.button ?? 0,
          opts?.shiftKey ?? false,
        );
        // A plain (non-pan) left press selects the node under the cursor.
        if (!started && (opts?.button ?? 0) === 0) {
          const hit = this.hitTestNode(screenX, screenY);
          if (hit && this.clickCallback) this.clickCallback(hit);
          return hit;
        }
        return null;
      }
      case 'move': {
        vp.inputPointerMove(screenX, screenY);
        const hit = this.hitTestNode(screenX, screenY);
        if (hit && this.hoverCallback) this.hoverCallback(hit);
        return hit;
      }
      case 'up':
        vp.inputPointerUp();
        return null;
    }
  }

  /**
   * Resolves the node under a canvas-relative screen point, if any.
   *
   * Converts the screen point to world space via the viewport, then returns the
   * nearest node whose world-space radius contains the point. Used for worker
   * mode where PixiJS federated hit-testing is unavailable.
   *
   * @param screenX - Canvas-relative X in CSS pixels.
   * @param screenY - Canvas-relative Y in CSS pixels.
   * @returns The hit node id, or null when the point hits empty space.
   */
  private hitTestNode(screenX: number, screenY: number): string | null {
    const vp = this.viewportController;
    if (!vp || this.nodeDataMap.size === 0) return null;

    const world = vp.screenToWorld(screenX, screenY);
    let nearestId: string | null = null;
    let nearestDistSq = Infinity;

    for (const node of this.nodeDataMap.values()) {
      const dx = node.x - world.x;
      const dy = node.y - world.y;
      const radius = Math.max(node.size, MIN_HIT_RADIUS);
      const distSq = dx * dx + dy * dy;
      if (distSq <= radius * radius && distSq < nearestDistSq) {
        nearestDistSq = distSq;
        nearestId = node.id;
      }
    }
    return nearestId;
  }

  /**
   * Sets the current topology for particle path resolution.
   * Called by external code when topology data is fetched from the API.
   *
   * @param topology - Full topology graph (nodes + edges)
   */
  setTopology(topology: Topology): void {
    this.topologyCache = topology;
  }

  // ---------------------------------------------------------------------------
  // Memory Budget Handlers
  // ---------------------------------------------------------------------------

  /**
   * Applies progressive degradation based on memory budget level.
   * SOFT: disable particles, throttle glow updates.
   * HARD: destroy all particles, clear effects entirely.
   *
   * Public so the render-worker dispatch loop can forward budget-level
   * commands sent by the main thread, which owns the MemoryBudget sensor
   * (performance.memory is unavailable inside dedicated workers).
   */
  applyBudget(level: BudgetLevel): void {
    // Delegate particle throttling to SemanticParticleSystem
    this.semanticParticleSystem?.applyBudgetLevel(level);
    // Delegate polling throttling to EventIngest
    this.eventIngest?.setBudgetLevel(level);

    if (level === 'soft') {
      this.glowThrottled = true;
    } else if (level === 'hard') {
      this.glowThrottled = true;
      this.effectManager?.clear();
    }

    // Update particle store active flag
    useParticleStore.getState().setParticlesActive(level !== 'hard');
  }

  /**
   * Restores normal operation when memory drops below safe threshold.
   * Public so the render-worker dispatch can accept a 'normal' budget level
   * from the main-thread monitor.
   */
  recoverBudget(): void {
    this.glowThrottled = false;
    // Restore particle system to normal operation
    this.semanticParticleSystem?.applyBudgetLevel('normal');
    this.eventIngest?.setBudgetLevel('normal');

    // If the particle system was destroyed, re-create it
    if (!this.semanticParticleSystem && this.textureAtlas) {
      this.semanticParticleSystem = new SemanticParticleSystem(
        this.particleContainer,
        this.textureAtlas.getParticleTexture(),
      );
    }

    // Resume event ingest if it was paused
    if (this.eventIngest && !this.eventIngest.isActive) {
      this.eventIngest.resume();
    }

    // Update particle store active flag
    useParticleStore.getState().setParticlesActive(true);
  }

  /**
   * Applies transport degradation to PixiJS subsystems.
   * Reads CSS variables from #root computed style and propagates multipliers.
   */
  private applyTransportDegradation(mode: TransportMode): void {
    // CSS-variable-driven degradation is a main-thread concern; the worker has
    // no document. Multipliers still arrive on the main thread and the worker
    // receives them through explicit commands when needed.
    if (typeof document === 'undefined') return;
    const root = document.getElementById('root');
    if (!root) return;
    const style = getComputedStyle(root);

    const particleOpacity =
      parseFloat(style.getPropertyValue('--aef-particle-opacity').trim()) || 0.85;
    const edgeOpacity =
      parseFloat(style.getPropertyValue('--aef-edge-opacity').trim()) || 1.0;
    const glowOpacity =
      parseFloat(style.getPropertyValue('--aef-glow-opacity').trim()) || 1.0;

    this.semanticParticleSystem?.setDegradationMultiplier(particleOpacity);
    this.edgeOpacityMultiplier = edgeOpacity;
    // Glow degradation not needed — healthy nodes are static, unhealthy use local pulsing
    void glowOpacity;

    void mode; // Reserved for future mode-specific behavior
  }

  /**
   * Destroys the application and frees all GPU resources.
   */
  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.running = false;

    cancelAnimationFrame(this.rafId);

    // Detach the particle-store subscription so this instance can be GC'd.
    this.transportUnsub?.();
    this.transportUnsub = null;

    this.eventIngest?.stop();
    this.viewportController?.detachEvents();
    this.nodeRenderer?.destroy();
    this.semanticParticleSystem?.destroy();
    this.effectManager?.destroy();
    this.textureAtlas?.dispose();

    this.edgeGfx.destroy();
    this.particleContainer.destroy({ children: true });
    this.nodeContainer.destroy({ children: true });
    this.effectContainer.destroy({ children: true });
    this.labelContainer.destroy({ children: true });

    for (const label of this.labels.values()) {
      label.destroy();
    }
    this.labels.clear();
    this.nodeDataMap.clear();
    this.unhealthyNodes.clear();

    this.app.destroy(true, { children: true, texture: true });
  }
}

// ---------------------------------------------------------------------------
// Color Utilities (design system tokens)
// ---------------------------------------------------------------------------

/**
 * Returns the primary color for a node based on type and status.
 * Status overrides type color when unhealthy/degraded.
 */
export function getNodeColor(type: string, status: string): number {
  if (status === 'unhealthy') return 0xff4444;
  if (status === 'degraded') return 0xfb923c;

  switch (type) {
    case 'host': return 0x3b82f6;
    case 'container': return 0x4ade80;
    case 'service': return 0x8b5cf6;
    case 'process': return 0x06b6d4;
    default: return 0xffffff;
  }
}

/**
 * Returns the design system status color.
 */
export function getStatusColor(status: string): number {
  switch (status) {
    case 'healthy': return 0x4ade80;
    case 'degraded': return 0xfb923c;
    case 'unhealthy': return 0xff4444;
    default: return 0x656565;
  }
}

/**
 * Returns the edge line color by edge type.
 */
export function getEdgeColor(type: string): number {
  switch (type) {
    case 'network': return 0x3b82f6;
    case 'dependency': return 0xfb923c;
    case 'contains': return 0x8b5cf6;
    case 'calls': return 0x06b6d4;
    default: return 0x656565;
  }
}
