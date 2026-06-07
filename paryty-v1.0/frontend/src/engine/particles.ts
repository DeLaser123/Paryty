/**
 * Semantic Particle System for GPU Rendering Engine
 *
 * Each particle represents a single real ParytyEvent. Particles traverse
 * multi-hop paths through the topology and die at terminal nodes (no looping).
 * Color is the DEFINITIVE event type indicator via EVENT_TYPE_COLORS lookup.
 *
 * Performance target: 10K particles at ≥55fps
 * Pool sizing:    2K initial (lazy), 200 batch growth, 15K hard max
 *
 * Particle behavior:
 *   - Spawned from EventIngest drain (NOT random probability)
 *   - Multi-hop traversal with edge type priority
 *   - Tint = EVENT_TYPE_COLORS[category][severity] (full RGB, no brightness modulation)
 *   - Alpha modulated by lifetime fade only
 *   - Dead at terminal node (no loop back)
 */

import * as PIXI from 'pixi.js';
import type { Topology } from '../types/topology';
import type {
  SemanticParticleData,
  SemanticParticleConfig,
} from '../types/particle';
import {
  DEFAULT_SEMANTIC_PARTICLE_CONFIG,
  INITIAL_PARTICLE_POOL,
  PARTICLE_POOL_BATCH,
  MAX_PARTICLE_POOL,
  EVENT_TYPE_COLORS,
} from '../types/particle';
import type { EventIngest } from './eventIngest';
import type { EventPathResolver } from './eventPathResolver';
import type { BudgetLevel } from './memoryBudget';

// ─── Default Config Overrides for Particle System ──────────────

/** Internal defaults tuned for the particle system behaviour. */
const SYSTEM_DEFAULTS = {
  /** Base speed divisor to convert config speed to progress units. */
  SPEED_DIVISOR: 500,
  /** Base alpha multiplier for sprites. */
  BASE_ALPHA: 0.85,
};

// ─── SemanticParticleSystem ─────────────────────────────────────

/**
 * Manages animated semantic particles flowing along topology paths.
 *
 * Replaces the old edge-based decorative ParticleSystem. Each particle
 * is now backed by a real ParytyEvent with a resolved multi-hop path.
 *
 * Usage:
 * ```ts
 * const sps = new SemanticParticleSystem(container, texture);
 * // In render loop:
 * sps.update(eventIngest, pathResolver, topology, nodePositions);
 * ```
 */
export class SemanticParticleSystem {
  /** Container to add particle sprites to. */
  private container: PIXI.Container;
  /** Base texture for particle sprites. */
  private particleTexture: PIXI.Texture;
  /** Configuration (immutable reference, budget may override limits). */
  private config: SemanticParticleConfig;

  /** Active semantic particles. */
  private particles: SemanticParticleData[] = [];
  /** Inactive sprite pool for reuse. */
  private spritePool: PIXI.Sprite[] = [];
  /** Total sprites ever created (for tracking pool growth). */
  private totalSpritesCreated = 0;

  /** Time since last pool activity for shrink timer. */
  private lastPoolActivityTime: number = Date.now();

  /** Whether spawning is currently enabled. */
  private spawnEnabled = true;
  /** Effective max particles (may be reduced by budget). */
  private effectiveMaxParticles: number;
  /** Transport degradation alpha multiplier (1.0 = full, synced with CSS). */
  private degradationMultiplier: number = SYSTEM_DEFAULTS.BASE_ALPHA;

  constructor(
    container: PIXI.Container,
    particleTexture: PIXI.Texture,
    config: Partial<SemanticParticleConfig> = {},
  ) {
    this.container = container;
    this.particleTexture = particleTexture;
    this.config = { ...DEFAULT_SEMANTIC_PARTICLE_CONFIG, ...config };
    this.effectiveMaxParticles = this.config.maxParticles;
  }

  // ─── Public API ───────────────────────────────────────────────

  /**
   * Per-frame update. Drains events from ingest, resolves paths,
   * spawns new particles, moves existing ones, and culls dead.
   *
   * Call this once per animation frame.
   */
  update(
    eventIngest: EventIngest,
    pathResolver: EventPathResolver,
    topology: Topology,
    nodePositions: Map<string, { x: number; y: number }>,
  ): void {
    // Phase 1: Drain events and spawn new particles
    if (this.spawnEnabled) {
      this.spawnFromIngest(eventIngest, pathResolver, topology, nodePositions);
    }

    // Phase 2: Move all active particles
    this.moveParticles();

    // Phase 3: Cull dead particles
    this.cullDeadParticles();

    // Phase 4: Periodic pool shrink check
    this.maybeShrinkPool();

    // Update pool activity timestamp
    if (this.particles.length > 0) {
      this.lastPoolActivityTime = Date.now();
    }
  }

  /**
   * Applies memory budget level to control particle count and spawning.
   *
   *   normal: full capacity, spawning enabled
   *   soft:   half capacity, spawning enabled
   *   hard:   500 max, only error/critical survive, spawning disabled
   */
  applyBudgetLevel(level: BudgetLevel): void {
    switch (level) {
      case 'normal':
        this.effectiveMaxParticles = this.config.maxParticles;
        this.spawnEnabled = true;
        break;
      case 'soft':
        this.effectiveMaxParticles = Math.floor(
          this.config.maxParticles / 2,
        );
        this.spawnEnabled = true;
        break;
      case 'hard':
        this.effectiveMaxParticles = 500;
        this.cullNonCritical();
        this.spawnEnabled = false;
        break;
    }
  }

  /**
   * Sets the degradation multiplier for particle alpha.
   * Called by PixiTopologyApp when transport mode changes.
   *
   * @param multiplier - Alpha multiplier (0.0–1.0), typically from CSS --aef-particle-opacity
   */
  setDegradationMultiplier(multiplier: number): void {
    this.degradationMultiplier = multiplier;
  }

  /** Current number of active particles. */
  get activeParticleCount(): number {
    return this.particles.length;
  }

  /**
   * Counts particles whose sprite positions fall within the given
   * world-space bounding box. Used for viewport-culled StatusBar display.
   */
  countInViewport(
    minX: number,
    minY: number,
    maxX: number,
    maxY: number,
  ): number {
    let count = 0;
    for (let i = 0; i < this.particles.length; i++) {
      const s = this.particles[i].sprite;
      if (
        s.x >= minX &&
        s.x <= maxX &&
        s.y >= minY &&
        s.y <= maxY
      ) {
        count++;
      }
    }
    return count;
  }

  /**
   * Destroys all particles, sprites, and clears pools.
   */
  destroy(): void {
    for (const particle of this.particles) {
      if (particle.sprite && !particle.sprite.destroyed) {
        particle.sprite.destroy();
      }
    }
    this.particles = [];

    for (const sprite of this.spritePool) {
      if (!sprite.destroyed) {
        sprite.destroy();
      }
    }
    this.spritePool = [];
    this.totalSpritesCreated = 0;
  }

  // ─── Pool Management ──────────────────────────────────────────

  /**
   * Acquires a sprite from the pool. Grows the pool lazily
   * in batches if exhausted.
   */
  private acquireSprite(): PIXI.Sprite | null {
    if (this.spritePool.length > 0) {
      const sprite = this.spritePool.pop()!;
      sprite.visible = true;
      sprite.alpha = 1;
      return sprite;
    }

    // Pool exhausted — grow in batch
    if (
      this.totalSpritesCreated < MAX_PARTICLE_POOL
    ) {
      const batchSize = Math.min(
        PARTICLE_POOL_BATCH,
        MAX_PARTICLE_POOL - this.totalSpritesCreated,
      );
      for (let i = 0; i < batchSize; i++) {
        const sprite = new PIXI.Sprite(this.particleTexture);
        sprite.anchor.set(0.5);
        sprite.visible = false;
        sprite.width = this.config.particleSize;
        sprite.height = this.config.particleSize;
        this.spritePool.push(sprite);
        this.totalSpritesCreated++;
      }
      // Return one from the new batch
      const sprite = this.spritePool.pop()!;
      sprite.visible = true;
      return sprite;
    }

    return null; // Hard pool exhausted
  }

  /**
   * Returns a sprite to the inactive pool.
   */
  private releaseSprite(sprite: PIXI.Sprite): void {
    sprite.visible = false;
    this.container.removeChild(sprite);

    // Cap pool to MAX_PARTICLE_POOL
    if (this.spritePool.length < MAX_PARTICLE_POOL) {
      this.spritePool.push(sprite);
    } else {
      sprite.destroy();
    }
  }

  /**
   * Periodically shrinks the idle pool to free memory when
   * particles are inactive for 60s.
   */
  private maybeShrinkPool(): void {
    const SHRINK_IDLE_MS = 60_000;
    const SHRINK_TARGET = INITIAL_PARTICLE_POOL;

    if (
      this.particles.length === 0 &&
      this.spritePool.length > SHRINK_TARGET &&
      Date.now() - this.lastPoolActivityTime > SHRINK_IDLE_MS
    ) {
      while (this.spritePool.length > SHRINK_TARGET) {
        const sprite = this.spritePool.pop();
        if (sprite && !sprite.destroyed) {
          sprite.destroy();
          this.totalSpritesCreated--;
        }
      }
    }
  }

  // ─── Spawning ─────────────────────────────────────────────────

  /**
   * Drains events from EventIngest, resolves paths via EventPathResolver,
   * and spawns semantic particles.
   */
  private spawnFromIngest(
    eventIngest: EventIngest,
    pathResolver: EventPathResolver,
    topology: Topology,
    nodePositions: Map<string, { x: number; y: number }>,
  ): void {
    const availableSlots =
      this.effectiveMaxParticles - this.particles.length;
    if (availableSlots <= 0) return;

    const events = eventIngest.drain();
    if (events.length === 0) return;

    let spawned = 0;
    for (const event of events) {
      if (spawned >= availableSlots) break;

      // Resolve path
      const path = pathResolver.resolvePath(event, topology, nodePositions);
      if (path.length === 0) continue;

      // Acquire sprite
      const sprite = this.acquireSprite();
      if (!sprite) break;

      // Determine tint color
      const catColors = EVENT_TYPE_COLORS[event.category];
      const tint = catColors?.[event.severity] ?? 0x656565;

      sprite.tint = tint;
      sprite.alpha = 0; // Start invisible, fade in

      // Position at start of first segment
      sprite.x = path[0].fromX;
      sprite.y = path[0].fromY;

      this.container.addChild(sprite);

      const particle: SemanticParticleData = {
        eventId: event.id,
        eventCategory: event.category,
        eventSeverity: event.severity,
        path,
        currentPathIndex: 0,
        progress: 0,
        lifetime: this.config.lifetime,
        sprite,
      };

      this.particles.push(particle);
      spawned++;
    }
  }

  // ─── Movement ─────────────────────────────────────────────────

  /**
   * Advances all active particles along their multi-hop paths.
   * Updates position, alpha (fade), and handles segment transitions.
   */
  private moveParticles(): void {
    const baseSpeed = this.config.baseSpeed;
    const fadeLen = this.config.fadeLength;
    const maxLifetime = this.config.lifetime;

    for (const particle of this.particles) {
      const seg = particle.path[particle.currentPathIndex];
      if (!seg) continue;

      // Advance progress
      particle.progress += baseSpeed / SYSTEM_DEFAULTS.SPEED_DIVISOR;
      particle.lifetime--;

      if (particle.progress >= 1.0) {
        // Reached end of current segment
        if (particle.currentPathIndex < particle.path.length - 1) {
          // Advance to next segment
          particle.currentPathIndex++;
          particle.progress = 0;
        } else {
          // Terminal node reached — hold position until lifetime expires
          particle.progress = 1.0;
        }
      }

      // Position interpolation on current segment
      const currentSeg = particle.path[particle.currentPathIndex];
      if (currentSeg) {
        const t = Math.min(1, Math.max(0, particle.progress));
        particle.sprite.x =
          currentSeg.fromX + (currentSeg.toX - currentSeg.fromX) * t;
        particle.sprite.y =
          currentSeg.fromY + (currentSeg.toY - currentSeg.fromY) * t;
      }

      // Alpha: fade-in at start, fade-out near end of lifetime
      const age = maxLifetime - particle.lifetime;
      let alpha = 1.0;
      if (age < fadeLen) {
        // Fade in
        alpha = age / fadeLen;
      }
      if (particle.lifetime < fadeLen) {
        // Fade out
        alpha = Math.min(alpha, particle.lifetime / fadeLen);
      }
      // Terminal hold: also fade out at the very end
      if (
        particle.progress >= 1.0 &&
        particle.currentPathIndex >= particle.path.length - 1
      ) {
        alpha = Math.min(alpha, Math.max(0, particle.lifetime / fadeLen));
      }

      particle.sprite.alpha = alpha * this.degradationMultiplier;
    }
  }

  // ─── Culling ──────────────────────────────────────────────────

  /**
   * Removes particles whose lifetime has expired.
   * Uses in-place compaction (write index) for zero allocation.
   */
  private cullDeadParticles(): void {
    let writeIdx = 0;

    for (let i = 0; i < this.particles.length; i++) {
      const particle = this.particles[i];

      // Kill if lifetime expired
      if (particle.lifetime <= 0) {
        if (particle.sprite && !particle.sprite.destroyed) {
          this.releaseSprite(particle.sprite);
        }
        continue;
      }

      // Keep alive
      this.particles[writeIdx] = particle;
      writeIdx++;
    }

    this.particles.length = writeIdx;
  }

  /**
   * Removes all non-critical particles (used for HARD budget).
   * Only error and critical severity events survive.
   */
  private cullNonCritical(): void {
    let writeIdx = 0;

    for (let i = 0; i < this.particles.length; i++) {
      const p = this.particles[i];
      if (
        p.eventSeverity === 'error' ||
        p.eventSeverity === 'critical'
      ) {
        this.particles[writeIdx] = p;
        writeIdx++;
      } else {
        if (p.sprite && !p.sprite.destroyed) {
          this.releaseSprite(p.sprite);
        }
      }
    }

    this.particles.length = writeIdx;
  }
}

// ─── Backward-Compatible Re-exports ─────────────────────────────

/**
 * @deprecated Use SemanticParticleData from types/particle.ts instead.
 * Kept for backward compatibility during migration.
 */
export type { SemanticParticleData as ParticleData };

/**
 * @deprecated Use SemanticParticleConfig from types/particle.ts instead.
 * Kept for backward compatibility during migration.
 */
export type { SemanticParticleConfig as ParticleConfig };

/**
 * Edge particle data type retained for backward compatibility with
 * any code that still references it. The semantic particle system
 * no longer uses edge-based spawning.
 *
 * @deprecated Edge-based particle spawning has been replaced by
 *   event-based ingest. Use ParytyEvent + EventPathSegment instead.
 */
export interface EdgeParticleData {
  edgeId: string;
  sourceX: number;
  sourceY: number;
  targetX: number;
  targetY: number;
  throughput: number;
  latency: number;
  volume: number;
}
