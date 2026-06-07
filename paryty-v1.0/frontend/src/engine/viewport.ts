/**
 * Viewport Controller for GPU Rendering Engine
 *
 * Manages camera/viewport state: pan (drag), zoom (wheel), coordinate transforms.
 * Applies the transform to a PIXI.Container stage so that world-space objects
 * are rendered at the correct screen position.
 *
 * Zoom limits: 0.1x – 5.0x
 * Pan: middle-button drag, or shift + left-button drag
 * Zoom: mouse wheel, centered on cursor position
 */

import * as PIXI from 'pixi.js';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Immutable snapshot of the current viewport state. */
export interface ViewportState {
  /** World X coordinate at the center of the viewport. */
  x: number;
  /** World Y coordinate at the center of the viewport. */
  y: number;
  /** Current zoom level (1.0 = 100%). */
  zoom: number;
  /** Viewport width in screen pixels. */
  width: number;
  /** Viewport height in screen pixels. */
  height: number;
}

/** Axis-aligned bounding box in world coordinates. */
export interface WorldBounds {
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
}

/** Callback type for viewport state change notifications. */
export type ViewportChangeCallback = (state: ViewportState) => void;

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Minimum allowed zoom level. */
const MIN_ZOOM = 0.1;

/** Maximum allowed zoom level. */
const MAX_ZOOM = 5.0;

/** Zoom factor multiplier per wheel tick. */
const ZOOM_FACTOR = 0.1;

/** Default padding when fitting to content (world units). */
const FIT_PADDING = 80;

// ---------------------------------------------------------------------------
// Viewport Controller
// ---------------------------------------------------------------------------

/**
 * Manages pan/zoom transforms and applies them to a PIXI.Container.
 *
 * Usage:
 * ```ts
 * const viewport = new ViewportController(stage, width, height);
 * viewport.attachEvents(canvas);
 * viewport.on('change', (state) => console.log(state));
 * ```
 */
export class ViewportController {
  /** The PIXI stage container whose transform is modified. */
  private stage: PIXI.Container;

  /** Current zoom level. */
  private zoomLevel: number = 1.0;
  /** Current pivot X (world point at stage origin). */
  private pivotX: number = 0;
  /** Current pivot Y (world point at stage origin). */
  private pivotY: number = 0;
  /** Current stage position X. */
  private posX: number = 0;
  /** Current stage position Y. */
  private posY: number = 0;

  /** Viewport width in screen pixels. */
  private vpWidth: number;
  /** Viewport height in screen pixels. */
  private vpHeight: number;

  // Drag state
  private isDragging = false;
  private dragStartScreenX = 0;
  private dragStartScreenY = 0;
  private dragStartPivotX = 0;
  private dragStartPivotY = 0;

  // Event listeners (stored for cleanup)
  private boundWheel: ((e: WheelEvent) => void) | null = null;
  private boundPointerDown: ((e: PointerEvent) => void) | null = null;
  private boundPointerMove: ((e: PointerEvent) => void) | null = null;
  private boundPointerUp: ((e: PointerEvent) => void) | null = null;
  private attachedCanvas: HTMLCanvasElement | null = null;

  // Listeners
  private changeListeners: ViewportChangeCallback[] = [];

  constructor(stage: PIXI.Container, width: number, height: number) {
    this.stage = stage;
    this.vpWidth = width;
    this.vpHeight = height;

    // Initial pivot: center of viewport in world coords
    this.pivotX = width / 2;
    this.pivotY = height / 2;
    this.posX = width / 2;
    this.posY = height / 2;

    this.applyTransform();
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /**
   * Attaches mouse/pointer events to the canvas for pan and zoom.
   *
   * @param canvas - The HTML canvas element rendered by PIXI
   */
  attachEvents(canvas: HTMLCanvasElement): void {
    this.detachEvents();
    this.attachedCanvas = canvas;

    this.boundWheel = this.onWheel.bind(this);
    this.boundPointerDown = this.onPointerDown.bind(this);
    this.boundPointerMove = this.onPointerMove.bind(this);
    this.boundPointerUp = this.onPointerUp.bind(this);

    canvas.addEventListener('wheel', this.boundWheel, { passive: false });
    canvas.addEventListener('pointerdown', this.boundPointerDown);
    canvas.addEventListener('pointermove', this.boundPointerMove);
    canvas.addEventListener('pointerup', this.boundPointerUp);
    canvas.addEventListener('pointerleave', this.boundPointerUp);
  }

  /**
   * Detaches all event listeners. Safe to call multiple times.
   */
  detachEvents(): void {
    const canvas = this.attachedCanvas;
    if (!canvas) return;

    if (this.boundWheel) canvas.removeEventListener('wheel', this.boundWheel);
    if (this.boundPointerDown) canvas.removeEventListener('pointerdown', this.boundPointerDown);
    if (this.boundPointerMove) canvas.removeEventListener('pointermove', this.boundPointerMove);
    if (this.boundPointerUp) {
      canvas.removeEventListener('pointerup', this.boundPointerUp);
      canvas.removeEventListener('pointerleave', this.boundPointerUp);
    }

    this.boundWheel = null;
    this.boundPointerDown = null;
    this.boundPointerMove = null;
    this.boundPointerUp = null;
    this.attachedCanvas = null;
    this.isDragging = false;
  }

  /**
   * Zooms to a specific level centered on a world point.
   *
   * @param level - Target zoom level (clamped to MIN_ZOOM..MAX_ZOOM)
   * @param centerWorldX - World X to center zoom on
   * @param centerWorldY - World Y to center zoom on
   */
  zoomTo(level: number, centerWorldX: number, centerWorldY: number): void {
    const clamped = this.clampZoom(level);
    if (clamped === this.zoomLevel) return;

    this.zoomLevel = clamped;
    this.pivotX = centerWorldX;
    this.pivotY = centerWorldY;
    this.posX = this.vpWidth / 2 + (1 - this.zoomLevel) * this.pivotX;
    this.posY = this.vpHeight / 2 + (1 - this.zoomLevel) * this.pivotY;

    this.applyTransform();
    this.emitChange();
  }

  /**
   * Adjusts zoom and pan to fit all content within the viewport.
   *
   * @param bounds - World-space bounding box of all content
   * @param padding - Extra padding around the content (world units)
   */
  fitToContent(bounds: WorldBounds, padding: number = FIT_PADDING): void {
    const contentW = bounds.maxX - bounds.minX + padding * 2;
    const contentH = bounds.maxY - bounds.minY + padding * 2;

    if (contentW <= 0 || contentH <= 0) return;

    const scaleX = this.vpWidth / contentW;
    const scaleY = this.vpHeight / contentH;
    const targetZoom = this.clampZoom(Math.min(scaleX, scaleY));

    const centerX = (bounds.minX + bounds.maxX) / 2;
    const centerY = (bounds.minY + bounds.maxY) / 2;

    this.zoomLevel = targetZoom;
    this.pivotX = centerX;
    this.pivotY = centerY;
    this.posX = this.vpWidth / 2 + (1 - this.zoomLevel) * this.pivotX;
    this.posY = this.vpHeight / 2 + (1 - this.zoomLevel) * this.pivotY;

    this.applyTransform();
    this.emitChange();
  }

  /**
   * Converts screen coordinates to world coordinates.
   *
   * @param screenX - Screen X (canvas pixels)
   * @param screenY - Screen Y (canvas pixels)
   * @returns World coordinates
   */
  screenToWorld(screenX: number, screenY: number): { x: number; y: number } {
    return {
      x: (screenX - this.posX) / this.zoomLevel,
      y: (screenY - this.posY) / this.zoomLevel,
    };
  }

  /**
   * Converts world coordinates to screen coordinates.
   *
   * @param worldX - World X
   * @param worldY - World Y
   * @returns Screen coordinates (canvas pixels)
   */
  worldToScreen(worldX: number, worldY: number): { x: number; y: number } {
    return {
      x: worldX * this.zoomLevel + this.posX,
      y: worldY * this.zoomLevel + this.posY,
    };
  }

  /**
   * Handles container resize events. Updates viewport dimensions and
   * re-applies the transform to maintain the current view.
   *
   * @param width - New viewport width in screen pixels
   * @param height - New viewport height in screen pixels
   */
  resize(width: number, height: number): void {
    this.vpWidth = width;
    this.vpHeight = height;

    // Re-center: maintain the same world point at the center
    this.posX = this.vpWidth / 2 + (1 - this.zoomLevel) * this.pivotX;
    this.posY = this.vpHeight / 2 + (1 - this.zoomLevel) * this.pivotY;

    this.applyTransform();
    this.emitChange();
  }

  /**
   * Registers a callback for viewport state changes (pan/zoom/resize).
   *
   * @param callback - Function called with the new ViewportState
   */
  onChange(callback: ViewportChangeCallback): void {
    this.changeListeners.push(callback);
  }

  /**
   * Removes a previously registered change callback.
   *
   * @param callback - The callback to remove
   */
  offChange(callback: ViewportChangeCallback): void {
    const idx = this.changeListeners.indexOf(callback);
    if (idx >= 0) {
      this.changeListeners.splice(idx, 1);
    }
  }

  /**
   * Returns a snapshot of the current viewport state.
   *
   * @returns Current ViewportState
   */
  getState(): ViewportState {
    return {
      x: this.pivotX,
      y: this.pivotY,
      zoom: this.zoomLevel,
      width: this.vpWidth,
      height: this.vpHeight,
    };
  }

  /**
   * Gets the world-space bounds visible in the current viewport.
   *
   * @returns World-space rectangle
   */
  getVisibleBounds(): WorldBounds {
    const topLeft = this.screenToWorld(0, 0);
    const bottomRight = this.screenToWorld(this.vpWidth, this.vpHeight);
    return {
      minX: topLeft.x,
      minY: topLeft.y,
      maxX: bottomRight.x,
      maxY: bottomRight.y,
    };
  }

  /** Current zoom level. */
  get currentZoom(): number {
    return this.zoomLevel;
  }

  // ---------------------------------------------------------------------------
  // Public API — Coordinate-based input (DOM-independent)
  //
  // These methods carry the pan/zoom logic in terms of canvas-relative screen
  // pixels, with no dependency on DOM event objects. They are driven both by
  // the DOM event handlers below (main-thread host) and by pointer commands
  // forwarded from the UI thread when the renderer runs in an OffscreenCanvas
  // worker. Keeping a single code path guarantees identical behavior in both
  // execution modes.
  // ---------------------------------------------------------------------------

  /**
   * Applies a wheel-zoom centered on a canvas-relative screen point.
   *
   * @param screenX - Pointer X in canvas pixels.
   * @param screenY - Pointer Y in canvas pixels.
   * @param deltaY - Wheel delta; positive zooms out, negative zooms in.
   */
  inputWheel(screenX: number, screenY: number, deltaY: number): void {
    const delta = deltaY > 0 ? -ZOOM_FACTOR : ZOOM_FACTOR;
    const newZoom = this.clampZoom(this.zoomLevel * (1 + delta));
    if (newZoom === this.zoomLevel) return;

    // World point under cursor before zoom
    const worldBefore = this.screenToWorld(screenX, screenY);

    // Apply new zoom
    this.zoomLevel = newZoom;

    // After zoom, the same world point should remain under the cursor.
    this.posX = screenX - worldBefore.x * this.zoomLevel;
    this.posY = screenY - worldBefore.y * this.zoomLevel;

    // Update pivot for state tracking
    this.pivotX = (this.vpWidth / 2 - this.posX) / (1 - this.zoomLevel || 1);
    this.pivotY = (this.vpHeight / 2 - this.posY) / (1 - this.zoomLevel || 1);

    this.applyTransform();
    this.emitChange();
  }

  /**
   * Begins a pan drag when the button/modifier combination qualifies
   * (middle button, or Shift + left button).
   *
   * @param screenX - Pointer X in canvas pixels.
   * @param screenY - Pointer Y in canvas pixels.
   * @param button - Pointer button index (0 = left, 1 = middle).
   * @param shiftKey - Whether Shift is held.
   * @returns `true` if a drag started (host should capture the pointer).
   */
  inputPointerDown(
    screenX: number,
    screenY: number,
    button: number,
    shiftKey: boolean,
  ): boolean {
    const isMiddle = button === 1;
    const isShiftLeft = button === 0 && shiftKey;
    if (!isMiddle && !isShiftLeft) return false;

    this.isDragging = true;
    this.dragStartScreenX = screenX;
    this.dragStartScreenY = screenY;
    this.dragStartPivotX = this.pivotX;
    this.dragStartPivotY = this.pivotY;
    return true;
  }

  /**
   * Updates the pan during an active drag. No-op when not dragging.
   *
   * @param screenX - Pointer X in canvas pixels.
   * @param screenY - Pointer Y in canvas pixels.
   */
  inputPointerMove(screenX: number, screenY: number): void {
    if (!this.isDragging) return;

    const dx = screenX - this.dragStartScreenX;
    const dy = screenY - this.dragStartScreenY;

    // Move pivot by the drag delta in world units
    this.pivotX = this.dragStartPivotX - dx / this.zoomLevel;
    this.pivotY = this.dragStartPivotY - dy / this.zoomLevel;

    this.posX = this.vpWidth / 2 + (1 - this.zoomLevel) * this.pivotX;
    this.posY = this.vpHeight / 2 + (1 - this.zoomLevel) * this.pivotY;

    this.applyTransform();
    this.emitChange();
  }

  /** Ends an active drag. No-op when not dragging. */
  inputPointerUp(): void {
    if (!this.isDragging) return;
    this.isDragging = false;
  }

  // ---------------------------------------------------------------------------
  // Private — Event Handlers
  // ---------------------------------------------------------------------------

  /** Handles mouse wheel events for zoom. */
  private onWheel(e: WheelEvent): void {
    e.preventDefault();
    const rect = (e.target as HTMLCanvasElement).getBoundingClientRect();
    this.inputWheel(e.clientX - rect.left, e.clientY - rect.top, e.deltaY);
  }

  /** Handles pointer down for drag initiation. */
  private onPointerDown(e: PointerEvent): void {
    const rect = (e.target as HTMLCanvasElement).getBoundingClientRect();
    const started = this.inputPointerDown(
      e.clientX - rect.left,
      e.clientY - rect.top,
      e.button,
      e.shiftKey,
    );
    if (started) {
      (e.target as HTMLCanvasElement).setPointerCapture(e.pointerId);
    }
  }

  /** Handles pointer move for drag panning. */
  private onPointerMove(e: PointerEvent): void {
    if (!this.isDragging) return;
    const rect = (e.target as HTMLCanvasElement).getBoundingClientRect();
    this.inputPointerMove(e.clientX - rect.left, e.clientY - rect.top);
  }

  /** Handles pointer up / leave for drag termination. */
  private onPointerUp(_e: PointerEvent): void {
    this.inputPointerUp();
  }

  // ---------------------------------------------------------------------------
  // Private — Helpers
  // ---------------------------------------------------------------------------

  /** Applies the current pivot/zoom/position to the PIXI stage transform. */
  private applyTransform(): void {
    this.stage.pivot.set(this.pivotX, this.pivotY);
    this.stage.scale.set(this.zoomLevel, this.zoomLevel);
    this.stage.position.set(this.posX, this.posY);
  }

  /** Clamps a zoom value to the allowed range. */
  private clampZoom(zoom: number): number {
    return Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, zoom));
  }

  /** Notifies all registered change listeners. */
  private emitChange(): void {
    const state = this.getState();
    for (const listener of this.changeListeners) {
      listener(state);
    }
  }
}
