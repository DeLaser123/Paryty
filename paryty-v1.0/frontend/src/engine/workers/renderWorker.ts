/**
 * Render Worker — hosts the PixiJS topology renderer on an OffscreenCanvas.
 *
 * This is the "render thread" of the two-worker architecture. The UI thread
 * transfers control of a DOM `<canvas>` here via `transferControlToOffscreen()`
 * and thereafter speaks only the {@link RenderCommand} protocol. All WebGL work,
 * the sprite pool, the texture atlas, and the per-frame animation loop live in
 * this thread, so a busy main thread (React reconciliation, layout, GC) can
 * never stall rendering — and vice versa.
 *
 * Interaction: PixiJS federated (DOM) events are unavailable off the main
 * thread, so the UI host forwards canvas-relative pointer coordinates as
 * {@link PointerCommand}s, which drive the same viewport input methods and a
 * geometric hit test ({@link PixiTopologyApp.forwardPointer}).
 *
 * @module engine/workers/renderWorker
 */

import { PixiTopologyApp } from '../pixiApp';
import { isRenderCommand, type RenderCommand, type RenderEvent } from './renderProtocol';

/** The renderer instance, created on the `init` command. */
let app: PixiTopologyApp | null = null;

/** Posts a typed event back to the UI thread. */
function emit(event: RenderEvent): void {
  // Single-argument post matches the options overload; render events never
  // carry transferable memory (the zero-copy buffers flow UI → worker only).
  self.postMessage(event);
}

/** Samples FPS and forwards it to the UI thread at a fixed cadence. */
let fpsInterval: ReturnType<typeof setInterval> | null = null;

/** Handles the one-time renderer construction. */
function handleInit(canvas: OffscreenCanvas, width: number, height: number, resolution: number): void {
  if (app) return; // idempotent — ignore duplicate init

  app = new PixiTopologyApp({ view: canvas, width, height, resolution });

  app.onNodeClick((id) => emit({ type: 'nodeClick', id }));
  app.onNodeHover((id) => emit({ type: 'nodeHover', id }));
  app.viewport?.onChange((state) =>
    emit({ type: 'viewportChange', x: state.x, y: state.y, zoom: state.zoom }),
  );

  // Stream FPS roughly once per second for the UI overlay.
  fpsInterval = setInterval(() => {
    if (app) emit({ type: 'fps', value: app.fps });
  }, 1000);

  emit({ type: 'ready' });
}

/** Routes a single validated command to the renderer. */
function dispatch(command: RenderCommand): void {
  switch (command.type) {
    case 'init':
      handleInit(command.canvas, command.width, command.height, command.resolution);
      return;
    case 'resize':
      app?.resize(command.width, command.height);
      return;
    case 'updateNodes':
      app?.updateNodes([...command.nodes]);
      return;
    case 'updateEdges':
      app?.updateEdges([...command.edges]);
      return;
    case 'selectNode':
      app?.selectNode(command.id);
      return;
    case 'zoomToNode':
      app?.zoomToNode(command.id);
      return;
    case 'pointer':
      app?.forwardPointer(command.kind, command.x, command.y, {
        deltaY: command.deltaY,
        button: command.button,
        shiftKey: command.shiftKey,
      });
      return;
    case 'destroy':
      if (fpsInterval !== null) {
        clearInterval(fpsInterval);
        fpsInterval = null;
      }
      app?.destroy();
      app = null;
      return;
    case 'budgetLevel':
      // Main thread owns the MemoryBudget sensor (performance.memory is
      // unavailable in workers). It broadcasts the derived level here so the
      // renderer can apply degradation (particle culling, glow throttling).
      app?.applyBudget(command.level);
      return;
    // 'setTopology', 'positions', and 'viewport' belong to the streaming-layout
    // path wired in a later increment; ignore them until then.
    case 'setTopology':
    case 'positions':
    case 'viewport':
      return;
  }
}

self.onmessage = (event: MessageEvent<unknown>): void => {
  const data = event.data;
  if (!isRenderCommand(data)) return;
  try {
    dispatch(data);
  } catch (err) {
    emit({ type: 'error', message: (err as Error).message });
  }
};
