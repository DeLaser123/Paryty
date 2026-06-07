/**
 * Semantic Particle Types — Event-to-Particle Rendering System
 *
 * Defines the color palette, path segment, particle data, and configuration
 * types for the semantic particle renderer where each particle represents
 * a single real ParytyEvent.
 *
 * Color is the DEFINITIVE event type indicator — operators identify event
 * categories purely by particle color at a glance.
 */

import type { ParytyEvent } from './event';

// ─── Event Type Color Palette ───────────────────────────────────

/**
 * Definitive color mapping: (category, severity) → hex color.
 *
 * Base hues per category:
 *   system      → blue family
 *   application → green family
 *   security    → purple family
 *   audit       → cyan family
 *
 * Saturation/lightness shift per severity:
 *   info     → lighter
 *   warning  → medium
 *   error    → saturated
 *   critical → darkest / most intense
 *
 * THIS IS THE ONLY LOOKUP. Particle tint = EVENT_TYPE_COLORS[category][severity].
 * No throughput/volume modulation of hue. Alpha modulated by lifetime fade only.
 */
export const EVENT_TYPE_COLORS: Record<
  string,
  Record<string, number>
> = {
  system: {
    info:     0x60a5fa, // blue-400
    warning:  0xf59e0b, // amber-500
    error:    0xef4444, // red-500
    critical: 0xec4899, // pink-500
  },
  application: {
    info:     0x4ade80, // green-400
    warning:  0xfacc15, // yellow-400
    error:    0xf97316, // orange-500
    critical: 0xdc2626, // red-600
  },
  security: {
    info:     0xa78bfa, // violet-400
    warning:  0xc084fc, // purple-400
    error:    0x7c3aed, // violet-600
    critical: 0x581c87, // purple-900
  },
  audit: {
    info:     0x22d3ee, // cyan-400
    warning:  0x2dd4bf, // teal-400
    error:    0x06b6d4, // cyan-500
    critical: 0x0e7490, // cyan-700
  },
};

/**
 * Looks up the particle tint color for an event.
 * Falls back to grey if category or severity is unknown.
 */
export function getEventParticleColor(event: ParytyEvent): number {
  const catColors = EVENT_TYPE_COLORS[event.category];
  if (!catColors) return 0x656565; // grey fallback
  return catColors[event.severity] ?? 0x656565;
}

// ─── Event Path Segment ─────────────────────────────────────────

/**
 * A single segment of a particle's multi-hop path through the topology.
 * Each segment represents traversal along one edge from a source node
 * to a target node.
 */
export interface EventPathSegment {
  /** Source node ID for this segment. */
  fromNodeId: string;
  /** Target node ID for this segment. */
  toNodeId: string;
  /** Edge ID connecting the two nodes. */
  edgeId: string;
  /** World-space X of the source node. */
  fromX: number;
  /** World-space Y of the source node. */
  fromY: number;
  /** World-space X of the target node. */
  toX: number;
  /** World-space Y of the target node. */
  toY: number;
}

// ─── Semantic Particle Data ─────────────────────────────────────

/**
 * One particle = one real ParytyEvent.
 *
 * Stores the event metadata (for color lookup), the resolved multi-hop
 * path through the topology, current traversal state, and the PIXI.Sprite
 * used for GPU rendering.
 */
export interface SemanticParticleData {
  /** Links back to the ParytyEvent in the ingest system. */
  eventId: string;
  /** Defines the particle color via EVENT_TYPE_COLORS. */
  eventCategory: string;
  /** Defines color intensity/brightness (drives which shade in the category). */
  eventSeverity: string;
  /** Ordered list of path segments the event traverses. */
  path: EventPathSegment[];
  /** Current segment index (0-based) the particle is on. */
  currentPathIndex: number;
  /** Progress within the current segment (0 = at from, 1 = at to). */
  progress: number;
  /** Remaining lifetime in frames (counts down each frame). */
  lifetime: number;
  /** The PIXI.Sprite rendering this particle. */
  sprite: import('pixi.js').Sprite;
}

// ─── Semantic Particle Configuration ────────────────────────────

/**
 * Immutable configuration for the semantic particle system.
 * All values have sensible defaults optimized for 60fps at 10K particles.
 */
export interface SemanticParticleConfig {
  /** Maximum total particles across all edges (default: 10_000). */
  maxParticles: number;
  /** Particle sprite size in pixels (default: 3). */
  particleSize: number;
  /** Base speed in progress units per frame at 60fps (default: 2). */
  baseSpeed: number;
  /** Particle lifetime in frames after reaching terminal (default: 60). */
  lifetime: number;
  /** Number of frames for fade-in and fade-out (default: 10). */
  fadeLength: number;
  /** Polling interval for event ingestion in ms (default: 2000). */
  ingestIntervalMs: number;
  /** RingBuffer capacity for the event ingest queue (default: 500). */
  ingestQueueCapacity: number;
}

/** Default semantic particle configuration. */
export const DEFAULT_SEMANTIC_PARTICLE_CONFIG: SemanticParticleConfig = {
  maxParticles: 10_000,
  particleSize: 3,
  baseSpeed: 2,
  lifetime: 60,
  fadeLength: 10,
  ingestIntervalMs: 2000,
  ingestQueueCapacity: 500,
};

// ─── Particle Pool Sizing ───────────────────────────────────────

/** Initial sprite pool size (allocated lazily on first spawn). */
export const INITIAL_PARTICLE_POOL = 2_000;

/** Batch growth when pool is exhausted. */
export const PARTICLE_POOL_BATCH = 200;

/** Hard max sprite pool size. */
export const MAX_PARTICLE_POOL = 15_000;
