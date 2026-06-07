/**
 * Visual Effects for GPU Rendering Engine
 *
 * Manages dynamic visual effects applied to nodes and edges:
 *   - HealthGlow: alpha modulation per node based on health status
 *   - EdgeAnimation: line width and alpha based on throughput
 *   - AlertPulse: expanding ring effect around alerting nodes
 *   - EffectManager: orchestrates all effects per frame
 */

import * as PIXI from 'pixi.js';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Node effect state tracked by HealthGlow. */
interface GlowNode {
  sprite: PIXI.Sprite;
  status: string;
  pulsePhase: number;
}

/** Edge effect state tracked by EdgeAnimation. */
interface EdgeEffectData {
  graphics: PIXI.Graphics;
  throughput: number;
}

/** Alert pulse lifecycle state. */
interface PulseInstance {
  ring: PIXI.Graphics;
  lifetime: number;
  maxLifetime: number;
  worldX: number;
  worldY: number;
}

/** Data passed to AlertPulse.addPulse(). */
export interface AlertData {
  nodeId: string;
  worldX: number;
  worldY: number;
  severity: 'info' | 'warn' | 'crit';
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Frame counter speed for pulse phases. */
const PULSE_SPEED = 0.05;

/** HealthGlow alpha values by status. */
const GLOW_ALPHA: Record<string, number> = {
  healthy: 1.0,
  degraded: 0.8,
  unhealthy: 0.0, // Pulsing, so base is 0 (set dynamically)
  unknown: 0.6,
};

/** Unhealthy pulse range. */
const UNHEALTHY_ALPHA_MIN = 0.6;
const UNHEALTHY_ALPHA_MAX = 0.9;

/** Edge animation throughput → line width range. */
const EDGE_WIDTH_MIN = 1;
const EDGE_WIDTH_MAX = 4;

/** Edge animation throughput → alpha range. */
const EDGE_ALPHA_MIN = 0.3;
const EDGE_ALPHA_MAX = 0.8;

/** Alert pulse lifetime in frames. */
const PULSE_LIFETIME = 60;

/** Alert pulse max radius expansion in pixels. */
const PULSE_MAX_RADIUS = 80;

/** Alert ring initial radius. */
const PULSE_INITIAL_RADIUS = 10;

/** Alert severity colors. */
const ALERT_COLORS: Record<string, number> = {
  info: 0x3b82f6,
  warn: 0xfb923c,
  crit: 0xff4444,
};

/** Effect frame budget in milliseconds (4ms). */
const EFFECT_FRAME_BUDGET_MS = 4;

// ---------------------------------------------------------------------------
// HealthGlow
// ---------------------------------------------------------------------------

/**
 * Manages alpha modulation for node sprites based on health status.
 *
 * - Healthy: full alpha (1.0)
 * - Degraded: slightly dimmed (0.8)
 * - Unhealthy: pulsing alpha (0.6–0.9) to draw attention
 * - Unknown: dimmed (0.6)
 */
export class HealthGlow {
  private nodes: Map<string, GlowNode> = new Map();
  private globalAlphaMultiplier: number = 1.0;

  /**
   * Sets the global alpha multiplier for transport degradation.
   * Synced with CSS --aef-glow-opacity.
   */
  setGlobalAlphaMultiplier(m: number): void { this.globalAlphaMultiplier = m; }

  /**
   * Registers a node sprite for health glow tracking.
   *
   * @param id - Node identifier
   * @param sprite - The PIXI.Sprite to modulate
   * @param status - Health status string
   */
  addNode(id: string, sprite: PIXI.Sprite, status: string): void {
    this.nodes.set(id, {
      sprite,
      status,
      pulsePhase: Math.random() * Math.PI * 2, // Random start phase
    });
  }

  /**
   * Updates the status for an already-tracked node.
   *
   * @param id - Node identifier
   * @param status - New health status
   */
  updateStatus(id: string, status: string): void {
    const node = this.nodes.get(id);
    if (node) {
      node.status = status;
    }
  }

  /**
   * Removes a node from health glow tracking.
   *
   * @param id - Node identifier
   */
  removeNode(id: string): void {
    this.nodes.delete(id);
  }

  /**
   * Per-frame update. Modulates alpha for all tracked nodes.
   * Call once per requestAnimationFrame.
   *
   * @param frameCount - Global frame counter for animation timing
   */
  update(frameCount: number): void {
    for (const [, node] of this.nodes) {
      if (node.status === 'unhealthy') {
        // Pulsing alpha for unhealthy nodes
        node.pulsePhase += PULSE_SPEED;
        const pulse = (Math.sin(node.pulsePhase) + 1) / 2; // 0–1
        node.sprite.alpha = (UNHEALTHY_ALPHA_MIN +
          (UNHEALTHY_ALPHA_MAX - UNHEALTHY_ALPHA_MIN) * pulse) *
          this.globalAlphaMultiplier;
      } else {
        node.sprite.alpha =
          (GLOW_ALPHA[node.status] ?? GLOW_ALPHA.unknown) *
          this.globalAlphaMultiplier;
      }

      // Use frame count to occasionally update pulse even for non-unhealthy
      // (ensures smooth transitions when status changes)
      void frameCount;
    }
  }

  /** Removes all tracked nodes. */
  clear(): void {
    this.nodes.clear();
  }

  /** Returns the number of tracked nodes. */
  get count(): number {
    return this.nodes.size;
  }

  /**
   * Returns the set of currently tracked node IDs.
   * Used by PixiTopologyApp.updateHealthGlow to identify stale entries.
   */
  getTrackedIds(): Set<string> {
    return new Set(this.nodes.keys());
  }
}

// ---------------------------------------------------------------------------
// EdgeAnimation
// ---------------------------------------------------------------------------

/**
 * Manages edge visual properties based on throughput.
 *
 * - Line width: 1–4px (proportional to throughput)
 * - Alpha: 0.3–0.8 (proportional to throughput)
 * - Color: design system status colors for the edge tint
 */
export class EdgeAnimation {
  private edges: Map<string, EdgeEffectData> = new Map();
  private globalAlphaMultiplier: number = 1.0;

  /**
   * Sets the global alpha multiplier for transport degradation.
   * Synced with CSS --aef-edge-opacity.
   */
  setGlobalAlphaMultiplier(m: number): void { this.globalAlphaMultiplier = m; }

  /**
   * Registers an edge for animation tracking.
   *
   * @param id - Edge identifier
   * @param graphics - The PIXI.Graphics object for the edge line
   * @param throughput - Initial throughput (0–1)
   */
  addEdge(id: string, graphics: PIXI.Graphics, throughput: number): void {
    this.edges.set(id, { graphics, throughput });
  }

  /**
   * Updates the throughput for an already-tracked edge.
   *
   * @param id - Edge identifier
   * @param throughput - New throughput value (0–1)
   */
  updateThroughput(id: string, throughput: number): void {
    const edge = this.edges.get(id);
    if (edge) {
      edge.throughput = throughput;
    }
  }

  /**
   * Removes an edge from animation tracking.
   *
   * @param id - Edge identifier
   */
  removeEdge(id: string): void {
    this.edges.delete(id);
  }

  /**
   * Per-frame update. Applies line width and alpha to all tracked edges.
   */
  update(): void {
    for (const [, edge] of this.edges) {
      const t = Math.max(0, Math.min(1, edge.throughput));
      const alpha = (EDGE_ALPHA_MIN + (EDGE_ALPHA_MAX - EDGE_ALPHA_MIN) * t) *
        this.globalAlphaMultiplier;
      edge.graphics.alpha = alpha;
    }
  }

  /**
   * Gets the computed line width for an edge (for use during edge redraw).
   *
   * @param id - Edge identifier
   * @returns Line width in pixels
   */
  getLineWidth(id: string): number {
    const edge = this.edges.get(id);
    if (!edge) return EDGE_WIDTH_MIN;
    const t = Math.max(0, Math.min(1, edge.throughput));
    return EDGE_WIDTH_MIN + (EDGE_WIDTH_MAX - EDGE_WIDTH_MIN) * t;
  }

  /** Removes all tracked edges. */
  clear(): void {
    this.edges.clear();
  }

  /** Returns the number of tracked edges. */
  get count(): number {
    return this.edges.size;
  }
}

// ---------------------------------------------------------------------------
// AlertPulse
// ---------------------------------------------------------------------------

/**
 * Renders expanding ring effects around alerting nodes.
 *
 * Each pulse:
 *   1. Starts as a small ring at the alert location
 *   2. Expands outward over PULSE_LIFETIME frames
 *   3. Fades out as it expands
 *   4. Is removed from the container when expired
 */
export class AlertPulse {
  private container: PIXI.Container;
  private pulses: PulseInstance[] = [];
  private graphicsPool: PIXI.Graphics[] = [];

  constructor(container: PIXI.Container) {
    this.container = container;
  }

  /**
   * Creates a new alert pulse at the specified world position.
   *
   * @param alert - Alert data including position and severity
   */
  addPulse(alert: AlertData): void {
    const color = ALERT_COLORS[alert.severity] ?? ALERT_COLORS.info;
    const ring = this.acquireRing();

    ring.clear();
    ring.lineStyle(2, color, 1.0);
    ring.drawCircle(0, 0, PULSE_INITIAL_RADIUS);

    ring.x = alert.worldX;
    ring.y = alert.worldY;
    ring.scale.set(1);
    ring.alpha = 1.0;

    this.container.addChild(ring);

    this.pulses.push({
      ring,
      lifetime: PULSE_LIFETIME,
      maxLifetime: PULSE_LIFETIME,
      worldX: alert.worldX,
      worldY: alert.worldY,
    });
  }

  /**
   * Acquires a ring Graphics from the pool or creates a new one.
   */
  private acquireRing(): PIXI.Graphics {
    const ring = this.graphicsPool.pop();
    if (ring) {
      ring.visible = true;
      return ring;
    }
    return new PIXI.Graphics();
  }

  /**
   * Returns a ring to the pool for reuse.
   */
  private releaseRing(ring: PIXI.Graphics): void {
    ring.visible = false;
    this.container.removeChild(ring);
    ring.clear();
    if (this.graphicsPool.length < 200) {
      this.graphicsPool.push(ring);
    } else {
      ring.destroy();
    }
  }

  /**
   * Per-frame update. Expands and fades all active pulses.
   * Removes expired pulses.
   */
  update(): void {
    let writeIdx = 0;

    for (let i = 0; i < this.pulses.length; i++) {
      const pulse = this.pulses[i];
      pulse.lifetime--;

      if (pulse.lifetime <= 0) {
        this.releaseRing(pulse.ring);
        continue;
      }

      // Progress: 0 (new) → 1 (expired)
      const progress = 1 - pulse.lifetime / pulse.maxLifetime;

      // Scale: expand from initial radius to max radius
      const scale = 1 + progress * (PULSE_MAX_RADIUS / PULSE_INITIAL_RADIUS);
      pulse.ring.scale.set(scale);

      // Alpha: fade out as it expands
      pulse.ring.alpha = (1 - progress) * 0.7;

      this.pulses[writeIdx] = pulse;
      writeIdx++;
    }

    this.pulses.length = writeIdx;
  }

  /** Returns the number of active pulses. */
  get activePulseCount(): number {
    return this.pulses.length;
  }

  /** Removes and destroys all active pulses. */
  clear(): void {
    for (const pulse of this.pulses) {
      this.releaseRing(pulse.ring);
    }
    this.pulses = [];
  }

  /** Destroys all resources. */
  destroy(): void {
    this.clear();
    for (const ring of this.graphicsPool) {
      ring.destroy();
    }
    this.graphicsPool = [];
  }
}

// ---------------------------------------------------------------------------
// Effect Manager
// ---------------------------------------------------------------------------

/**
 * Orchestrates all visual effects. Call `update()` once per frame.
 *
 * Usage:
 * ```ts
 * const manager = new EffectManager(effectContainer);
 * manager.healthGlow.addNode(id, sprite, 'healthy');
 * manager.edgeAnimation.addEdge(id, graphics, 0.5);
 * // In render loop:
 * manager.update();
 * ```
 */
export class EffectManager {
  /** Health-based alpha glow on nodes. */
  readonly healthGlow: HealthGlow;
  /** Throughput-based edge line animation. */
  readonly edgeAnimation: EdgeAnimation;
  /** Expanding ring pulses for alerts. */
  readonly alertPulse: AlertPulse;

  /** Global frame counter. */
  private frameCount = 0;

  /** Time spent in the previous frame's effect update (ms). */
  private _lastFrameTimeMs = 0;

  /** Whether effects are currently throttled (frame budget exceeded). */
  private _isThrottled = false;

  constructor(effectContainer: PIXI.Container) {
    this.healthGlow = new HealthGlow();
    this.edgeAnimation = new EdgeAnimation();
    this.alertPulse = new AlertPulse(effectContainer);
  }

  /**
   * Per-frame update. Advances all effects by one frame.
   * Skips expensive effects when frame budget is exceeded.
   * Call inside requestAnimationFrame.
   */
  update(): void {
    const frameStart = performance.now();
    this.frameCount++;

    // Always update essential effects
    this.healthGlow.update(this.frameCount);
    this.edgeAnimation.update();

    // Skip alert pulses if last frame exceeded budget
    if (!this._isThrottled) {
      this.alertPulse.update();
    }

    this._lastFrameTimeMs = performance.now() - frameStart;
    this._isThrottled = this._lastFrameTimeMs > EFFECT_FRAME_BUDGET_MS;
  }

  /** Current frame count since creation. */
  get frame(): number {
    return this.frameCount;
  }

  /** Whether effects are currently throttled (frame budget exceeded). */
  get isThrottled(): boolean {
    return this._isThrottled;
  }

  /** Time spent in the previous frame's effect update (ms). */
  get lastFrameTimeMs(): number {
    return this._lastFrameTimeMs;
  }

  /**
   * Applies transport degradation multipliers to all effect subsystems.
   * Called by PixiTopologyApp when transport mode changes.
   *
   * @param m - Object containing edgeOpacity and glowOpacity multipliers
   */
  setDegradationMultiplier(m: { edgeOpacity: number; glowOpacity: number }): void {
    this.healthGlow.setGlobalAlphaMultiplier(m.glowOpacity);
    this.edgeAnimation.setGlobalAlphaMultiplier(m.edgeOpacity);
  }

  /**
   * Clears all effects without destroying resources.
   */
  clear(): void {
    this.healthGlow.clear();
    this.edgeAnimation.clear();
    this.alertPulse.clear();
    this.frameCount = 0;
  }

  /**
   * Destroys all effect resources.
   */
  destroy(): void {
    this.healthGlow.clear();
    this.edgeAnimation.clear();
    this.alertPulse.destroy();
  }
}
