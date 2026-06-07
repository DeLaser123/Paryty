/**
 * Instanced Node Renderer for GPU Rendering Engine
 *
 * Uses PIXI.ParticleContainer for batch-optimized rendering of 15K+ nodes.
 * Includes a grid-based SpatialIndex for O(1) hit testing and viewport queries.
 *
 * Performance targets:
 *   - 15K nodes at ≥55fps
 *   - O(1) spatial queries via grid hash
 *   - Zero allocations in render loop (object pooling)
 */

import * as PIXI from 'pixi.js';
import { TextureAtlas } from './textures/nodeAtlas';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Data required to render a single instanced node. */
export interface InstancedNodeData {
  id: string;
  x: number;
  y: number;
  type: string;
  status: string;
  size: number;
  label: string;
  clusterId?: string;
}

/** Rectangle in world coordinates. */
interface WorldRect {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

/** Wrapper associating a pooled sprite with its node ID. */
interface PooledSprite {
  sprite: PIXI.Sprite;
  nodeId: string;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Initial sprite pool size (allocated at construction). */
const INITIAL_POOL = 500;

/** Maximum allowed sprite pool size. */
const MAX_POOL = 15_000;

/** Number of sprites to allocate when pool is exhausted. */
const POOL_BATCH_SIZE = 100;

/** Idle count threshold before shrinking the pool. */
const SHRINK_IDLE_THRESHOLD = 500;

/** Idle duration in ms before shrinking (60s). */
const SHRINK_IDLE_MS = 60_000;

/** Grid cell size for spatial index (pixels). */
const GRID_CELL_SIZE = 100;

/** Pre-allocated sprite pool multiplier (10% headroom). */
const POOL_HEADROOM = 1.1;

// ---------------------------------------------------------------------------
// Spatial Index
// ---------------------------------------------------------------------------

/**
 * Grid-based spatial hash for O(1) viewport queries and hit testing.
 *
 * Divides world space into fixed-size cells. Each cell stores a set of node IDs.
 * Queries enumerate only cells that overlap the query rectangle.
 */
export class SpatialIndex {
  private cellSize: number;
  /** Map from "cellX:cellY" → Set of node IDs. */
  private grid: Map<string, Set<string>> = new Map();
  /** Map from node ID → { cellKey, x, y }. */
  private nodePositions: Map<string, { cellKey: string; x: number; y: number }> = new Map();

  constructor(cellSize: number = GRID_CELL_SIZE) {
    this.cellSize = cellSize;
  }

  /** Computes the grid cell key for a world coordinate. */
  private cellKey(x: number, y: number): string {
    const cx = Math.floor(x / this.cellSize);
    const cy = Math.floor(y / this.cellSize);
    return `${cx}:${cy}`;
  }

  /**
   * Inserts or updates a node's position in the spatial index.
   * If the node moved to a different cell, removes it from the old cell first.
   *
   * @param id - Node identifier
   * @param x - World X coordinate
   * @param y - World Y coordinate
   */
  update(id: string, x: number, y: number): void {
    const newKey = this.cellKey(x, y);
    const existing = this.nodePositions.get(id);

    if (existing && existing.cellKey !== newKey) {
      // Node moved to a different cell — remove from old cell
      const oldCell = this.grid.get(existing.cellKey);
      if (oldCell) {
        oldCell.delete(id);
        if (oldCell.size === 0) {
          this.grid.delete(existing.cellKey);
        }
      }
    }

    // Add to new cell
    let cell = this.grid.get(newKey);
    if (!cell) {
      cell = new Set();
      this.grid.set(newKey, cell);
    }
    cell.add(id);

    this.nodePositions.set(id, { cellKey: newKey, x, y });
  }

  /**
   * Removes a node from the spatial index.
   *
   * @param id - Node identifier to remove
   */
  remove(id: string): void {
    const entry = this.nodePositions.get(id);
    if (!entry) return;

    const cell = this.grid.get(entry.cellKey);
    if (cell) {
      cell.delete(id);
      if (cell.size === 0) {
        this.grid.delete(entry.cellKey);
      }
    }

    this.nodePositions.delete(id);
  }

  /**
   * Queries all node IDs within a rectangular world region.
   * Returns only cells that overlap the query rectangle.
   *
   * @param x1 - Left edge (world coords)
   * @param y1 - Top edge (world coords)
   * @param x2 - Right edge (world coords)
   * @param y2 - Bottom edge (world coords)
   * @returns Array of node IDs within the rectangle
   */
  query(x1: number, y1: number, x2: number, y2: number): string[] {
    const results: string[] = [];

    const minCx = Math.floor(x1 / this.cellSize);
    const maxCx = Math.floor(x2 / this.cellSize);
    const minCy = Math.floor(y1 / this.cellSize);
    const maxCy = Math.floor(y2 / this.cellSize);

    for (let cx = minCx; cx <= maxCx; cx++) {
      for (let cy = minCy; cy <= maxCy; cy++) {
        const cell = this.grid.get(`${cx}:${cy}`);
        if (cell) {
          for (const id of cell) {
            results.push(id);
          }
        }
      }
    }

    return results;
  }

  /**
   * Gets the stored world position for a node.
   *
   * @param id - Node identifier
   * @returns World coordinates or null if node not found
   */
  getPosition(id: string): { x: number; y: number } | null {
    const entry = this.nodePositions.get(id);
    if (!entry) return null;
    return { x: entry.x, y: entry.y };
  }

  /** Returns the total number of indexed nodes. */
  get size(): number {
    return this.nodePositions.size;
  }
}

// ---------------------------------------------------------------------------
// Instanced Node Renderer
// ---------------------------------------------------------------------------

/**
 * High-performance node renderer using PIXI.ParticleContainer.
 *
 * Pre-allocates a sprite pool. On each frame update, reuses sprites
 * from the pool rather than creating/destroying them. Delegates hit
 * testing and viewport culling to a SpatialIndex.
 */
export class InstancedNodeRenderer {
  /** Container holding all node sprites (added to parent). */
  private container: PIXI.Container;
  /** Texture atlas for (type, status) lookups. */
  private textureAtlas: TextureAtlas;
  /** Spatial index for hit testing and culling. */
  private spatialIndex: SpatialIndex;

  /** Map from node ID → active sprite wrapper. */
  private activeSprites: Map<string, PooledSprite> = new Map();
  /** Pool of available sprites for reuse. */
  private spritePool: PIXI.Sprite[] = [];

  /** Pre-allocated pool size. */
  private poolSize: number;

  /** Timestamp of last pool shrink check. */
  private lastShrinkCheck = 0;

  /** Number of sprites currently idle in the pool. */
  private idleCount = 0;

  /** Callback fired when a node is clicked. */
  onNodeClick: ((id: string) => void) | null = null;
  /** Callback fired when a node is hovered. */
  onNodeHover: ((id: string) => void) | null = null;

  constructor(
    parentContainer: PIXI.Container,
    textureAtlas: TextureAtlas,
    maxNodes?: number,
  ) {
    this.textureAtlas = textureAtlas;
    this.spatialIndex = new SpatialIndex(GRID_CELL_SIZE);
    this.poolSize = Math.ceil((maxNodes ?? MAX_POOL) * POOL_HEADROOM);

    // Use Container for full type safety. ParticleContainer would be preferred
    // for 15K+ nodes but its type is not exposed in pixi.js v7 typings.
    this.container = new PIXI.Container();
    parentContainer.addChild(this.container);

    // Pre-allocate initial pool (small, grows on demand)
    this.preAllocateSprites(INITIAL_POOL);
  }

  /**
   * Pre-allocates sprites into the pool. These are NOT added to the container
   * until they are needed (to keep the container's child count minimal).
   */
  private preAllocateSprites(count: number): void {
    const defaultTexture = this.textureAtlas.getTexture('host', 'unknown');
    for (let i = 0; i < count; i++) {
      const sprite = new PIXI.Sprite(defaultTexture);
      sprite.anchor.set(0.5);
      sprite.visible = false;
      this.spritePool.push(sprite);
      this.idleCount++;
    }
  }

  /**
   * Acquires a sprite from the pool or creates a new one if the pool is empty.
   * Grows the pool in batches when exhausted (up to MAX_POOL).
   */
  private acquireSprite(): PIXI.Sprite {
    let sprite = this.spritePool.pop();
    if (sprite) {
      this.idleCount--;
    } else {
      // Pool exhausted — grow in batches (up to max)
      if (this.spritePool.length + this.activeSprites.size < this.poolSize) {
        const batchSize = Math.min(
          POOL_BATCH_SIZE,
          this.poolSize - this.spritePool.length - this.activeSprites.size,
        );
        this.preAllocateSprites(batchSize);
        sprite = this.spritePool.pop();
        if (sprite) this.idleCount--;
      }
      if (!sprite) {
        // Pool at capacity — create a one-off (rare path)
        const defaultTexture = this.textureAtlas.getTexture('host', 'unknown');
        sprite = new PIXI.Sprite(defaultTexture);
        sprite.anchor.set(0.5);
      }
    }
    sprite.visible = true;
    return sprite;
  }

  /**
   * Returns a sprite to the pool for reuse. Removes it from the container.
   * Shrinks the pool when idle count exceeds threshold after cooldown period.
   */
  private releaseSprite(sprite: PIXI.Sprite): void {
    sprite.visible = false;
    this.container.removeChild(sprite);
    this.spritePool.push(sprite);
    this.idleCount++;

    // Shrink pool if too many idle sprites have been idle for too long
    const now = performance.now();
    if (
      this.idleCount > SHRINK_IDLE_THRESHOLD &&
      now - this.lastShrinkCheck > SHRINK_IDLE_MS
    ) {
      this.lastShrinkCheck = now;
      // Release half of the excess idle sprites
      const excess = this.idleCount - INITIAL_POOL;
      const toRelease = Math.floor(excess / 2);
      for (let i = 0; i < toRelease; i++) {
        const stale = this.spritePool.pop();
        if (stale) {
          stale.destroy();
          this.idleCount--;
        }
      }
    }
  }

  /**
   * Batch-updates all node sprites to match the provided node data.
   *
   * - Removes sprites for nodes no longer in the data set
   * - Adds sprites for new nodes (from pool)
   * - Updates positions and textures for existing nodes
   * - Maintains spatial index in sync with rendered positions
   *
   * @param nodes - Array of current node render data
   */
  update(nodes: InstancedNodeData[]): void {
    const incomingIds = new Set<string>();

    for (const node of nodes) {
      incomingIds.add(node.id);
      const existing = this.activeSprites.get(node.id);

      if (existing) {
        // Update existing sprite
        const sprite = existing.sprite;
        sprite.x = node.x;
        sprite.y = node.y;
        sprite.width = node.size * 2;
        sprite.height = node.size * 2;
        sprite.tint = 0xffffff;

        // Update texture if type/status changed
        const expectedTexture = this.textureAtlas.getTexture(node.type, node.status);
        if (sprite.texture !== expectedTexture) {
          sprite.texture = expectedTexture;
        }

        // Update spatial index
        this.spatialIndex.update(node.id, node.x, node.y);
      } else {
        // Acquire sprite from pool and add to container
        const sprite = this.acquireSprite();
        sprite.x = node.x;
        sprite.y = node.y;
        sprite.width = node.size * 2;
        sprite.height = node.size * 2;
        sprite.tint = 0xffffff;
        sprite.texture = this.textureAtlas.getTexture(node.type, node.status);
        sprite.eventMode = 'static';
        sprite.cursor = 'pointer';

        this.container.addChild(sprite);
        this.activeSprites.set(node.id, { sprite, nodeId: node.id });
        this.spatialIndex.update(node.id, node.x, node.y);
      }
    }

    // Remove stale nodes
    for (const [id, pooled] of this.activeSprites) {
      if (!incomingIds.has(id)) {
        this.releaseSprite(pooled.sprite);
        this.activeSprites.delete(id);
        this.spatialIndex.remove(id);
      }
    }
  }

  /**
   * Hit test: finds the node under a world coordinate.
   * Uses spatial index to narrow candidates, then checks circle containment.
   *
   * @param worldX - X in world coordinates
   * @param worldY - Y in world coordinates
   * @returns Node ID at the point, or null if no node found
   */
  getNodeAtPoint(worldX: number, worldY: number): string | null {
    // Query a small region around the point
    const padding = 50;
    const candidates = this.spatialIndex.query(
      worldX - padding,
      worldY - padding,
      worldX + padding,
      worldY + padding,
    );

    let closestId: string | null = null;
    let closestDist = Infinity;

    for (const id of candidates) {
      const pos = this.spatialIndex.getPosition(id);
      if (!pos) continue;

      const dx = worldX - pos.x;
      const dy = worldY - pos.y;
      const dist = Math.sqrt(dx * dx + dy * dy);

      // Use the sprite's visual size as the hit radius
      const pooled = this.activeSprites.get(id);
      const nodeRadius = pooled ? pooled.sprite.width / 2 : 15;

      if (dist <= nodeRadius && dist < closestDist) {
        closestDist = dist;
        closestId = id;
      }
    }

    return closestId;
  }

  /**
   * Returns IDs of all nodes visible within a viewport rectangle.
   *
   * @param viewport - World-space rectangle to query
   * @returns Array of visible node IDs
   */
  getVisibleNodes(viewport: WorldRect): string[] {
    return this.spatialIndex.query(viewport.x1, viewport.y1, viewport.x2, viewport.y2);
  }

  /**
   * Gets the world position of a node from the spatial index.
   *
   * @param id - Node identifier
   * @returns World coordinates or null
   */
  getNodePosition(id: string): { x: number; y: number } | null {
    return this.spatialIndex.getPosition(id);
  }

  /** Returns the number of currently rendered (active) nodes. */
  get activeNodeCount(): number {
    return this.activeSprites.size;
  }

  /**
   * Destroys all sprites, clears pools, and removes the container.
   * Call this when the renderer is no longer needed.
   */
  destroy(): void {
    for (const [, pooled] of this.activeSprites) {
      pooled.sprite.destroy();
    }
    this.activeSprites.clear();

    for (const sprite of this.spritePool) {
      sprite.destroy();
    }
    this.spritePool = [];

    this.container.destroy({ children: true });
  }
}
