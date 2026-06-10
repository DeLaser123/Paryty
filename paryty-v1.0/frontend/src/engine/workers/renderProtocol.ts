/**
 * Render-worker message protocol.
 *
 * Defines the typed, transferable-friendly contract between the UI thread and
 * the OffscreenCanvas render worker. The design separates two concerns:
 *
 *   - **Structure** (`setTopology`): node/edge identity and metadata. Sent only
 *     when the graph shape changes. Carried by structured clone.
 *   - **Motion** (`positions`): per-frame x/y coordinates. Sent every layout
 *     tick as a transferable `Float32Array` so the buffer's memory is *moved*
 *     to the worker rather than copied. This is what prevents the per-frame
 *     copy amplification that otherwise dominates heap churn on large graphs.
 *
 * Node identity for the motion channel is implied by index: the i-th node in
 * the most recent `setTopology.nodes` array corresponds to floats `[2i, 2i+1]`
 * in every subsequent `positions` buffer.
 *
 * @module engine/workers/renderProtocol
 */

import type { NodeRenderData, EdgeRenderData } from '../pixiApp';

// ─── Serializable graph shapes (structured-clone friendly) ──────────────────

/** Node metadata for the render worker (no live object references). */
export interface RenderNode {
  readonly id: string;
  readonly type: string;
  readonly status: string;
  readonly label: string;
  readonly size: number;
}

/** Edge metadata for the render worker (endpoints by node id). */
export interface RenderEdge {
  readonly id: string;
  readonly sourceId: string;
  readonly targetId: string;
  readonly type: string;
  readonly throughput: number;
}

// ─── Main → Render commands ─────────────────────────────────────────────────

/** Hand the transferred OffscreenCanvas to the worker and start rendering. */
export interface InitCommand {
  readonly type: 'init';
  readonly canvas: OffscreenCanvas;
  readonly width: number;
  readonly height: number;
  readonly resolution: number;
}

/** Resize the renderer surface. */
export interface ResizeCommand {
  readonly type: 'resize';
  readonly width: number;
  readonly height: number;
}

/** Replace the graph structure (identity + metadata). */
export interface SetTopologyCommand {
  readonly type: 'setTopology';
  readonly nodes: readonly RenderNode[];
  readonly edges: readonly RenderEdge[];
}

/**
 * Replace all node render data (positions already baked in).
 *
 * This is the structural update channel used by {@link TopologyRenderer}: it
 * fires when layout recomputes, not every frame, so structured-clone copy cost
 * is bounded. The per-frame zero-copy {@link PositionsCommand} is reserved for
 * the streaming-layout path.
 */
export interface UpdateNodesCommand {
  readonly type: 'updateNodes';
  readonly nodes: readonly NodeRenderData[];
}

/** Replace all edge render data (endpoint coordinates baked in). */
export interface UpdateEdgesCommand {
  readonly type: 'updateEdges';
  readonly edges: readonly EdgeRenderData[];
}

/** Select/highlight a node, or clear selection when `id` is null. */
export interface SelectNodeCommand {
  readonly type: 'selectNode';
  readonly id: string | null;
}

/** Zoom and center the viewport on a node. */
export interface ZoomToNodeCommand {
  readonly type: 'zoomToNode';
  readonly id: string;
}

/** Stream per-frame node positions as a transferable buffer. */
export interface PositionsCommand {
  readonly type: 'positions';
  /** Packed `[x0, y0, x1, y1, ...]` aligned to the latest `setTopology.nodes`. */
  readonly buffer: Float32Array;
}

/** Update the viewport transform. */
export interface ViewportCommand {
  readonly type: 'viewport';
  readonly x: number;
  readonly y: number;
  readonly zoom: number;
}

/** Forward a pointer interaction from the (still main-thread) DOM canvas host. */
export interface PointerCommand {
  readonly type: 'pointer';
  readonly kind: 'move' | 'down' | 'up' | 'wheel';
  /** Canvas-relative pointer X in CSS pixels. */
  readonly x: number;
  /** Canvas-relative pointer Y in CSS pixels. */
  readonly y: number;
  /** Wheel delta (only meaningful for `kind: 'wheel'`). */
  readonly deltaY?: number;
  /** Pointer button index (0 = left, 1 = middle); only for `kind: 'down'`. */
  readonly button?: number;
  /** Whether Shift was held; only for `kind: 'down'`. */
  readonly shiftKey?: boolean;
}

/** Tear down the renderer and release GPU resources. */
export interface DestroyCommand {
  readonly type: 'destroy';
}

/**
 * Notify the render worker of the current memory budget level.
 *
 * MemoryBudget relies on `performance.memory` which Chrome disables inside
 * dedicated workers. The main thread runs the monitor and broadcasts the
 * derived level here so the worker can apply degradation (particle culling,
 * glow throttling) without any sensor access of its own.
 */
export interface BudgetLevelCommand {
  readonly type: 'budgetLevel';
  readonly level: 'normal' | 'soft' | 'hard';
}

/** Union of all UI → render-worker commands. */
export type RenderCommand =
  | InitCommand
  | ResizeCommand
  | SetTopologyCommand
  | UpdateNodesCommand
  | UpdateEdgesCommand
  | SelectNodeCommand
  | ZoomToNodeCommand
  | PositionsCommand
  | ViewportCommand
  | PointerCommand
  | BudgetLevelCommand
  | DestroyCommand;

// ─── Render → Main events ───────────────────────────────────────────────────

/** Worker finished initialization and is rendering. */
export interface ReadyEvent {
  readonly type: 'ready';
}

/** Current frames-per-second sample. */
export interface FpsEvent {
  readonly type: 'fps';
  readonly value: number;
}

/** Viewport-culled visible particle count. */
export interface ParticleCountEvent {
  readonly type: 'particleCount';
  readonly value: number;
}

/** A node was clicked (hit-tested on the worker). */
export interface NodeClickEvent {
  readonly type: 'nodeClick';
  readonly id: string;
}

/** Hover target changed (`null` when leaving all nodes). */
export interface NodeHoverEvent {
  readonly type: 'nodeHover';
  readonly id: string | null;
}

/** The worker encountered a non-fatal error. */
export interface RenderErrorEvent {
  readonly type: 'error';
  readonly message: string;
}

/** The viewport transform changed (pan/zoom), for store sync on the UI thread. */
export interface ViewportChangeEvent {
  readonly type: 'viewportChange';
  readonly x: number;
  readonly y: number;
  readonly zoom: number;
}

/** Union of all render-worker → UI events. */
export type RenderEvent =
  | ReadyEvent
  | FpsEvent
  | ParticleCountEvent
  | NodeClickEvent
  | NodeHoverEvent
  | ViewportChangeEvent
  | RenderErrorEvent;

// ─── Position codec (zero-copy hot path) ────────────────────────────────────

/** A node's resolved screen-space position. */
export interface NodePosition {
  readonly x: number;
  readonly y: number;
}

/**
 * Packs node positions into a transferable `Float32Array` as `[x, y]` pairs.
 *
 * The returned buffer's ownership is intended to be transferred to the worker
 * (see {@link transferListFor}); do not read it on the sending thread after the
 * `postMessage` that transfers it.
 *
 * @param positions - Positions in node-index order (matches latest topology).
 * @returns A densely packed buffer of length `2 * positions.length`.
 */
export function encodePositions(positions: readonly NodePosition[]): Float32Array {
  const buffer = new Float32Array(positions.length * 2);
  for (let i = 0; i < positions.length; i++) {
    const base = i * 2;
    buffer[base] = positions[i].x;
    buffer[base + 1] = positions[i].y;
  }
  return buffer;
}

/**
 * Unpacks a position buffer produced by {@link encodePositions}.
 *
 * @param buffer - Packed `[x0, y0, x1, y1, ...]` floats (even length).
 * @returns One {@link NodePosition} per `[x, y]` pair.
 * @throws RangeError if the buffer length is odd (malformed frame).
 */
export function decodePositions(buffer: Float32Array): NodePosition[] {
  if (buffer.length % 2 !== 0) {
    throw new RangeError(
      `position buffer length must be even, got ${buffer.length}`,
    );
  }
  const count = buffer.length / 2;
  const positions: NodePosition[] = new Array(count);
  for (let i = 0; i < count; i++) {
    const base = i * 2;
    positions[i] = { x: buffer[base], y: buffer[base + 1] };
  }
  return positions;
}

/**
 * Builds a {@link PositionsCommand} and its transfer list for `postMessage`.
 *
 * Usage:
 * ```ts
 * const { message, transfer } = packPositionsCommand(positions);
 * worker.postMessage(message, transfer);
 * ```
 *
 * @returns The command plus the transfer list (the backing `ArrayBuffer`).
 */
export function packPositionsCommand(
  positions: readonly NodePosition[],
): { message: PositionsCommand; transfer: Transferable[] } {
  const buffer = encodePositions(positions);
  return {
    message: { type: 'positions', buffer },
    transfer: [buffer.buffer],
  };
}

/**
 * Returns the transfer list for any command that carries transferable memory,
 * or an empty array for plain commands. Centralizes transfer-list construction
 * so callers cannot forget to transfer a `positions` buffer (a silent copy).
 */
export function transferListFor(command: RenderCommand): Transferable[] {
  switch (command.type) {
    case 'init':
      return [command.canvas];
    case 'positions':
      return [command.buffer.buffer];
    default:
      return [];
  }
}

// ─── Type guards ────────────────────────────────────────────────────────────

/** Narrows an unknown message to a {@link RenderEvent}. */
export function isRenderEvent(value: unknown): value is RenderEvent {
  if (typeof value !== 'object' || value === null) return false;
  const type = (value as { type?: unknown }).type;
  return (
    type === 'ready' ||
    type === 'fps' ||
    type === 'particleCount' ||
    type === 'nodeClick' ||
    type === 'nodeHover' ||
    type === 'viewportChange' ||
    type === 'error'
  );
}

/** Narrows an unknown message to a {@link RenderCommand}. */
export function isRenderCommand(value: unknown): value is RenderCommand {
  if (typeof value !== 'object' || value === null) return false;
  const type = (value as { type?: unknown }).type;
  return (
    type === 'init' ||
    type === 'resize' ||
    type === 'setTopology' ||
    type === 'updateNodes' ||
    type === 'updateEdges' ||
    type === 'selectNode' ||
    type === 'zoomToNode' ||
    type === 'positions' ||
    type === 'viewport' ||
    type === 'pointer' ||
    type === 'destroy'
  );
}
