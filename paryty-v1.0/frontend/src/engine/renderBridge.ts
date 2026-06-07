/**
 * Render Bridge — abstracts *where* the PixiJS renderer runs.
 *
 * {@link TopologyRenderer} (main-thread orchestrator: layout, diffing, cluster
 * navigation) talks to one of two interchangeable backends through the
 * {@link RenderBridge} interface:
 *
 *   - {@link WorkerRenderBridge}: hosts the renderer in an OffscreenCanvas
 *     worker (the "render thread"). The main thread is freed of all WebGL work;
 *     pointer input is captured on the DOM canvas here and forwarded to the
 *     worker as protocol commands.
 *   - {@link MainThreadRenderBridge}: hosts the renderer inline (current
 *     behavior). Used as a capability fallback when OffscreenCanvas or worker
 *     module support is unavailable, preserving identical behavior.
 *
 * {@link createRenderBridge} selects between them via {@link selectRenderMode}.
 *
 * @module engine/renderBridge
 */

import { PixiTopologyApp, type NodeRenderData, type EdgeRenderData } from './pixiApp';
import { selectRenderMode } from './capabilities';
import {
  isRenderEvent,
  type RenderCommand,
  type RenderEvent,
} from './workers/renderProtocol';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Viewport transform snapshot forwarded to the UI thread for store sync. */
export interface ViewportSnapshot {
  x: number;
  y: number;
  zoom: number;
}

/** Backend-agnostic contract the {@link TopologyRenderer} drives. */
export interface RenderBridge {
  /** Inserts the render surface into the DOM container. */
  mount(container: HTMLElement): void;
  /** Current backing-canvas pixel dimensions (used for layout sizing). */
  getViewSize(): { width: number; height: number };
  /** Replaces all node render data (positions baked in). */
  updateNodes(nodes: NodeRenderData[]): void;
  /** Replaces all edge render data (endpoint coordinates baked in). */
  updateEdges(edges: EdgeRenderData[]): void;
  /** Highlights a node, or clears selection when `id` is null. */
  selectNode(id: string | null): void;
  /** Zooms and centers the viewport on a node. */
  zoomToNode(id: string): void;
  /** Resizes the render surface. */
  resize(width: number, height: number): void;
  /** Registers a viewport-change listener (pan/zoom) for store sync. */
  onViewportChange(callback: (state: ViewportSnapshot) => void): void;
  /** Registers a node-click listener. */
  onNodeClick(callback: (id: string) => void): void;
  /** Registers a node-hover listener. */
  onNodeHover(callback: (id: string) => void): void;
  /** Tears down the renderer and frees all resources. */
  destroy(): void;
}

/** Construction parameters shared by both bridge implementations. */
export interface RenderBridgeConfig {
  width: number;
  height: number;
}

// ---------------------------------------------------------------------------
// Main-thread bridge (fallback) — thin pass-through to PixiTopologyApp
// ---------------------------------------------------------------------------

/**
 * Hosts the renderer inline on the main thread. Behavior is identical to the
 * pre-worker architecture, so this is the safe capability fallback.
 */
export class MainThreadRenderBridge implements RenderBridge {
  private readonly app: PixiTopologyApp;

  constructor(config: RenderBridgeConfig) {
    this.app = new PixiTopologyApp({ width: config.width, height: config.height });
  }

  mount(container: HTMLElement): void {
    container.appendChild(this.app.view);
  }

  getViewSize(): { width: number; height: number } {
    const view = this.app.view;
    return { width: view.width, height: view.height };
  }

  updateNodes(nodes: NodeRenderData[]): void {
    this.app.updateNodes(nodes);
  }

  updateEdges(edges: EdgeRenderData[]): void {
    this.app.updateEdges(edges);
  }

  selectNode(id: string | null): void {
    this.app.selectNode(id);
  }

  zoomToNode(id: string): void {
    this.app.zoomToNode(id);
  }

  resize(width: number, height: number): void {
    this.app.resize(width, height);
  }

  onViewportChange(callback: (state: ViewportSnapshot) => void): void {
    this.app.viewport?.onChange((state) =>
      callback({ x: state.x, y: state.y, zoom: state.zoom }),
    );
  }

  onNodeClick(callback: (id: string) => void): void {
    this.app.onNodeClick(callback);
  }

  onNodeHover(callback: (id: string) => void): void {
    this.app.onNodeHover(callback);
  }

  destroy(): void {
    this.app.destroy();
  }
}

// ---------------------------------------------------------------------------
// Worker bridge — renderer runs in an OffscreenCanvas worker
// ---------------------------------------------------------------------------

/**
 * Hosts the renderer in an OffscreenCanvas worker. This bridge owns a DOM
 * `<canvas>`, transfers its drawing surface to the worker, captures pointer
 * input on the main thread, and forwards it as protocol commands.
 */
export class WorkerRenderBridge implements RenderBridge {
  private readonly worker: Worker;
  private readonly canvas: HTMLCanvasElement;
  private destroyed = false;

  private viewportCallback: ((state: ViewportSnapshot) => void) | null = null;
  private clickCallback: ((id: string) => void) | null = null;
  private hoverCallback: ((id: string) => void) | null = null;

  /** Bound DOM listeners, retained so they can be detached on destroy. */
  private readonly onWheel: (e: WheelEvent) => void;
  private readonly onPointerDown: (e: PointerEvent) => void;
  private readonly onPointerMove: (e: PointerEvent) => void;
  private readonly onPointerUp: (e: PointerEvent) => void;

  constructor(config: RenderBridgeConfig) {
    this.canvas = document.createElement('canvas');
    this.canvas.width = config.width;
    this.canvas.height = config.height;
    this.canvas.style.width = '100%';
    this.canvas.style.height = '100%';
    this.canvas.style.display = 'block';

    this.worker = new Worker(new URL('./workers/renderWorker.ts', import.meta.url), {
      type: 'module',
    });
    this.worker.onmessage = (event: MessageEvent<unknown>) => {
      if (isRenderEvent(event.data)) this.handleEvent(event.data);
    };

    const offscreen = this.canvas.transferControlToOffscreen();
    const resolution = window.devicePixelRatio || 1;
    this.send(
      {
        type: 'init',
        canvas: offscreen,
        width: config.width,
        height: config.height,
        resolution,
      },
      [offscreen],
    );

    // Pointer capture happens on the main thread; coordinates are forwarded.
    this.onWheel = (e) => {
      e.preventDefault();
      const { x, y } = this.toCanvasCoords(e.clientX, e.clientY);
      this.send({ type: 'pointer', kind: 'wheel', x, y, deltaY: e.deltaY });
    };
    this.onPointerDown = (e) => {
      const { x, y } = this.toCanvasCoords(e.clientX, e.clientY);
      this.canvas.setPointerCapture(e.pointerId);
      this.send({
        type: 'pointer',
        kind: 'down',
        x,
        y,
        button: e.button,
        shiftKey: e.shiftKey,
      });
    };
    this.onPointerMove = (e) => {
      const { x, y } = this.toCanvasCoords(e.clientX, e.clientY);
      this.send({ type: 'pointer', kind: 'move', x, y });
    };
    this.onPointerUp = (e) => {
      const { x, y } = this.toCanvasCoords(e.clientX, e.clientY);
      this.send({ type: 'pointer', kind: 'up', x, y });
    };

    this.canvas.addEventListener('wheel', this.onWheel, { passive: false });
    this.canvas.addEventListener('pointerdown', this.onPointerDown);
    this.canvas.addEventListener('pointermove', this.onPointerMove);
    this.canvas.addEventListener('pointerup', this.onPointerUp);
    this.canvas.addEventListener('pointerleave', this.onPointerUp);
  }

  mount(container: HTMLElement): void {
    container.appendChild(this.canvas);
  }

  getViewSize(): { width: number; height: number } {
    return { width: this.canvas.width, height: this.canvas.height };
  }

  updateNodes(nodes: NodeRenderData[]): void {
    this.send({ type: 'updateNodes', nodes });
  }

  updateEdges(edges: EdgeRenderData[]): void {
    this.send({ type: 'updateEdges', edges });
  }

  selectNode(id: string | null): void {
    this.send({ type: 'selectNode', id });
  }

  zoomToNode(id: string): void {
    this.send({ type: 'zoomToNode', id });
  }

  resize(width: number, height: number): void {
    // The DOM canvas keeps its CSS size (100%); the backing store is owned by
    // the worker, so only the worker is told to resize its render target.
    this.send({ type: 'resize', width, height });
  }

  onViewportChange(callback: (state: ViewportSnapshot) => void): void {
    this.viewportCallback = callback;
  }

  onNodeClick(callback: (id: string) => void): void {
    this.clickCallback = callback;
  }

  onNodeHover(callback: (id: string) => void): void {
    this.hoverCallback = callback;
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;

    this.canvas.removeEventListener('wheel', this.onWheel);
    this.canvas.removeEventListener('pointerdown', this.onPointerDown);
    this.canvas.removeEventListener('pointermove', this.onPointerMove);
    this.canvas.removeEventListener('pointerup', this.onPointerUp);
    this.canvas.removeEventListener('pointerleave', this.onPointerUp);

    this.send({ type: 'destroy' });
    this.worker.terminate();
    this.canvas.remove();
  }

  /** Converts viewport (client) coordinates to canvas-relative CSS pixels. */
  private toCanvasCoords(clientX: number, clientY: number): { x: number; y: number } {
    const rect = this.canvas.getBoundingClientRect();
    return { x: clientX - rect.left, y: clientY - rect.top };
  }

  /** Routes a worker event to the registered UI-thread listeners. */
  private handleEvent(event: RenderEvent): void {
    switch (event.type) {
      case 'nodeClick':
        this.clickCallback?.(event.id);
        return;
      case 'nodeHover':
        if (event.id !== null) this.hoverCallback?.(event.id);
        return;
      case 'viewportChange':
        this.viewportCallback?.({ x: event.x, y: event.y, zoom: event.zoom });
        return;
      // ready/fps/particleCount/error are not yet surfaced to the UI here.
      case 'ready':
      case 'fps':
      case 'particleCount':
      case 'error':
        return;
    }
  }

  /** Posts a command to the worker, with an optional transfer list. */
  private send(command: RenderCommand, transfer?: Transferable[]): void {
    if (this.destroyed && command.type !== 'destroy') return;
    if (transfer && transfer.length > 0) {
      this.worker.postMessage(command, transfer);
    } else {
      this.worker.postMessage(command);
    }
  }
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

/**
 * Creates the best available render bridge for the current environment.
 *
 * Uses {@link selectRenderMode} to pick the OffscreenCanvas worker when the
 * browser supports it, falling back to inline main-thread rendering otherwise.
 */
export function createRenderBridge(config: RenderBridgeConfig): RenderBridge {
  return selectRenderMode() === 'worker'
    ? new WorkerRenderBridge(config)
    : new MainThreadRenderBridge(config);
}
