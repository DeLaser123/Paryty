/**
 * Render capability detection.
 *
 * Determines whether the current environment can run the PixiJS renderer on a
 * dedicated worker thread via {@link OffscreenCanvas}. When unsupported, the
 * caller falls back to main-thread rendering so the dashboard stays functional
 * on every browser.
 *
 * The 3-thread topology (UI / processing / render) depends on three runtime
 * features being present together:
 *   1. `Worker`                          — to spawn the render and processing threads.
 *   2. `OffscreenCanvas` + `transferControlToOffscreen` — to hand the canvas to the render worker.
 *   3. (preferred) `crossOriginIsolated` + `SharedArrayBuffer` — for zero-copy hot-path transport.
 *
 * @module engine/capabilities
 */

/** Individual runtime feature flags relevant to worker rendering. */
export interface RenderCapabilities {
  /** `Worker` constructor is available (can spawn threads). */
  readonly worker: boolean;
  /** `OffscreenCanvas` constructor is available. */
  readonly offscreenCanvas: boolean;
  /** `HTMLCanvasElement.prototype.transferControlToOffscreen` exists. */
  readonly transferControl: boolean;
  /** Page is cross-origin isolated (required for `SharedArrayBuffer`). */
  readonly crossOriginIsolated: boolean;
  /** `SharedArrayBuffer` constructor is available (zero-copy ring transport). */
  readonly sharedArrayBuffer: boolean;
}

/** The selected rendering execution mode. */
export type RenderMode = 'worker' | 'main';

/**
 * Probes the current environment for render-worker capabilities.
 *
 * Pure with respect to its inputs (reads only global feature flags) and safe
 * to call in any environment, including Node/jsdom test runners where the
 * features are absent.
 */
export function detectRenderCapabilities(): RenderCapabilities {
  const hasWorker = typeof Worker !== 'undefined';
  const hasOffscreen = typeof OffscreenCanvas !== 'undefined';
  const hasTransferControl =
    typeof HTMLCanvasElement !== 'undefined' &&
    typeof HTMLCanvasElement.prototype.transferControlToOffscreen === 'function';
  const isIsolated =
    typeof globalThis !== 'undefined' &&
    (globalThis as { crossOriginIsolated?: boolean }).crossOriginIsolated === true;
  const hasSab = typeof SharedArrayBuffer !== 'undefined';

  return {
    worker: hasWorker,
    offscreenCanvas: hasOffscreen,
    transferControl: hasTransferControl,
    crossOriginIsolated: isIsolated,
    sharedArrayBuffer: hasSab,
  };
}

/**
 * Returns true when the environment can render on a dedicated worker thread.
 *
 * Requires `Worker`, `OffscreenCanvas`, and `transferControlToOffscreen`.
 * `SharedArrayBuffer` is preferred (for zero-copy transport) but not required —
 * the protocol falls back to transferable `ArrayBuffer`s when it is absent.
 */
export function canUseRenderWorker(
  caps: RenderCapabilities = detectRenderCapabilities(),
): boolean {
  return caps.worker && caps.offscreenCanvas && caps.transferControl;
}

/**
 * Selects the rendering mode for the current environment.
 *
 * @returns `'worker'` when {@link canUseRenderWorker} holds, otherwise `'main'`.
 */
export function selectRenderMode(
  caps: RenderCapabilities = detectRenderCapabilities(),
): RenderMode {
  return canUseRenderWorker(caps) ? 'worker' : 'main';
}
