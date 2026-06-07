/**
 * PixiJS Topology Application — GPU-accelerated topology visualization
 *
 * Replaces individual per-node Graphics with InstancedNodeRenderer (sprite batching).
 * Container hierarchy:
 *   stage > [edgeContainer, particleContainer, nodeContainer, effectContainer, labelContainer]
 *
 * Integrates:
 *   - ViewportController (pan/zoom)
 *   - InstancedNodeRenderer (15K+ nodes at 60fps)
 *   - ParticleSystem (edge flow animation)
 *   - EffectManager (health glow, edge animation, alert pulses)
 *
 * Backward-compatible with existing PixiAppConfig, NodeRenderData, EdgeRenderData interfaces.
 */

import * as PIXI from 'pixi.js';
import { TextureAtlas } from './textures/nodeAtlas';
import { InstancedNodeRenderer, type InstancedNodeData } from './instancing';
import { ViewportController, type ViewportState } from './viewport';
import { SemanticParticleSystem } from './particles';
import { EffectManager, type AlertData } from './effects';
import { MemoryBudget, type BudgetLevel } from './memoryBudget';
import { EventIngest } from './eventIngest';
import { EventPathResolver } from './eventPathResolver';
import { useParticleStore } from '../stores/particleStore';
import { subscribeTransportMode } from './transportSubscription';
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

/** Label font size. */
const LABEL_FONT_SIZE = 11;

/** Minimum world-space radius for worker-mode pointer hit-testing. */
const MIN_HIT_RADIUS = 8;

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
  readonly app: PIXI.Application;

  /** Container layer for edge lines. */
  readonly edgeContainer: PIXI.Container;
  /** Container layer for edge particles. */
  readonly particleContainer: PIXI.Container;
  /** Container layer for instanced node sprites. */
  readonly nodeContainer: PIXI.Container;
  /** Container layer for visual effects (pulses, glows). */
  readonly effectContainer: PIXI.Container;
  /** Container layer for text labels. */
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

  /** Memory budget monitor for progressive degradation. */
  readonly memoryBudget: MemoryBudget;
  /** Whether glow effects are throttled (soft/hard budget). */
  private glowThrottled = false;

  /** Cached topology for particle path resolution (updated each updateNodes/updateEdges). */
  private topologyCache: Topology | null = null;
  /** Cached node positions for particle path resolution. */
  private nodePositions: Map<string, { x: number; y: number }> = new Map();
  /** Previous visible count for debouncing particle store updates. */
  private prevVisibleCount = -1;

  /** Active edge Graphics keyed by edge ID. */
  private edgeGraphics: Map<string, PIXI.Graphics> = new Map();
  /** Active label Text objects keyed by node ID. */
  private labels: Map<string, PIXI.Text> = new Map();

  /** Stored node data for re-rendering (preserves type/status metadata). */
  private nodeDataMap: Map<string, NodeRenderData> = new Map();

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

    // Create PIXI Application. In worker mode, render into the transferred
    // OffscreenCanvas and disable autoDensity (it mutates canvas.style, which
    // OffscreenCanvas lacks).
    this.app = new PIXI.Application({
      view: config.view as unknown as PIXI.ICanvas | undefined,
      width: config.width,
      height: config.height,
      backgroundColor: config.backgroundColor ?? BG_COLOR,
      antialias: config.antialias ?? true,
      resolution:
        config.resolution ??
        (typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1),
      autoDensity: !this.workerMode,
    });

    // Create container hierarchy
    this.edgeContainer = new PIXI.Container();
    this.particleContainer = new PIXI.Container();
    this.nodeContainer = new PIXI.Container();
    this.effectContainer = new PIXI.Container();
    this.labelContainer = new PIXI.Container();

    const stage = this.app.stage;
    stage.addChild(this.edgeContainer);
    stage.addChild(this.particleContainer);
    stage.addChild(this.nodeContainer);
    stage.addChild(this.effectContainer);
    stage.addChild(this.labelContainer);

    // Initialize subsystems
    this.initializeSubsystems(config);

    // Memory budget for progressive degradation
    this.memoryBudget = new MemoryBudget({
      onBudgetChange: (level: BudgetLevel) => this.applyBudget(level),
      onRecover: () => this.recoverBudget(),
    });
    this.memoryBudget.start();

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

    // Instanced node renderer (pre-allocates 15K sprite pool)
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

    // Memory budget check (per-frame for responsive degradation)
    this.memoryBudget.update();

    // Update effects
    this.effectManager?.update();

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

      // Viewport-culled particle counting for StatusBar
      const vp = this.viewportController;
      if (vp) {
        const bounds = vp.getVisibleBounds();
        if (bounds) {
          const visibleCount = this.semanticParticleSystem.countInViewport(
            bounds.minX,
            bounds.minY,
            bounds.maxX,
            bounds.maxY,
          );
          if (visibleCount !== this.prevVisibleCount) {
            this.prevVisibleCount = visibleCount;
            useParticleStore.getState().setVisualParticleCount(visibleCount);
          }
        }
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
   * Manages label Text objects: adds new, updates positions, removes stale.
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
        label = new PIXI.Text(node.label, {
          fontSize: LABEL_FONT_SIZE,
          fill: TEXT_PRIMARY,
          fontFamily: LABEL_FONT_FAMILY,
        });
        label.anchor.set(0.5, 0);
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
   * Synchronizes HealthGlow effect state with current nodes.
   */
  private updateHealthGlow(nodes: NodeRenderData[]): void {
    const glow = this.effectManager?.healthGlow;
    if (!glow) return;

    const currentNodeIds = new Set(nodes.map((n) => n.id));

    // Remove stale entries that are no longer in the current node set.
    // Without this, HealthGlow.nodes grows unbounded because addNode()
    // is called on every update but removeNode() was never called.
    for (const id of glow.getTrackedIds()) {
      if (!currentNodeIds.has(id)) {
        glow.removeNode(id);
      }
    }

    // Skip glow updates when throttled by memory budget
    if (this.glowThrottled) return;

    // Update glow for nodes that have active sprites
    for (const node of nodes) {
      const sprite = this.nodeRenderer?.getNodePosition(node.id);
      if (sprite) {
        glow.addNode(node.id, {} as PIXI.Sprite, node.status);
      }
    }
  }

  /**
   * Updates all edge graphics and edge particle data.
   *
   * @param edges - Current edge render data
   */
  updateEdges(edges: EdgeRenderData[]): void {
    const currentIds = new Set(edges.map((e) => e.id));

    // Remove stale edges
    for (const [id, graphic] of this.edgeGraphics) {
      if (!currentIds.has(id)) {
        this.effectManager?.edgeAnimation.removeEdge(id);
        graphic.destroy();
        this.edgeGraphics.delete(id);
      }
    }

    for (const edge of edges) {
      let graphic = this.edgeGraphics.get(edge.id);

      if (!graphic) {
        graphic = new PIXI.Graphics();
        this.edgeContainer.addChild(graphic);
        this.edgeGraphics.set(edge.id, graphic);
      }

      // Draw edge line
      graphic.clear();
      const throughput = edge.throughput ?? 0;
      const lineWidth = this.effectManager?.edgeAnimation.getLineWidth(edge.id) ?? 1;
      const color = getEdgeColor(edge.type);

      graphic.lineStyle(lineWidth, color, 0.6);
      graphic.moveTo(edge.sourceX, edge.sourceY);
      graphic.lineTo(edge.targetX, edge.targetY);

      // Track in effect manager
      this.effectManager?.edgeAnimation.addEdge(edge.id, graphic, throughput);
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
   */
  private applyBudget(level: BudgetLevel): void {
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
   */
  private recoverBudget(): void {
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
    this.effectManager?.setDegradationMultiplier({ edgeOpacity, glowOpacity });

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
    // Without this, the store's subscriber set pins the whole app forever.
    this.transportUnsub?.();
    this.transportUnsub = null;

    this.memoryBudget.destroy();

    this.eventIngest?.stop();
    this.viewportController?.detachEvents();
    this.nodeRenderer?.destroy();
    this.semanticParticleSystem?.destroy();
    this.effectManager?.destroy();
    this.textureAtlas?.dispose();

    this.edgeContainer.destroy({ children: true });
    this.particleContainer.destroy({ children: true });
    this.nodeContainer.destroy({ children: true });
    this.effectContainer.destroy({ children: true });
    this.labelContainer.destroy({ children: true });

    for (const label of this.labels.values()) {
      label.destroy();
    }
    this.labels.clear();
    this.edgeGraphics.clear();
    this.nodeDataMap.clear();

    this.app.destroy(true, { children: true, texture: true, baseTexture: true });
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
