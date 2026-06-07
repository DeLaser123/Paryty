/**
 * Memory Budget System — AAA Game Engine Pattern
 *
 * Monitors JS heap size via performance.memory (Chrome) and triggers
 * progressive degradation when limits are exceeded. Recovery restores
 * normal operation when memory drops below the safe threshold.
 *
 * Tiers:
 *   SOFT  (200 MB): disable particles, reduce glow, purge cache, evict metrics
 *   HARD  (280 MB): destroy all particles, clear effects, cap forecasts at 100pts
 *   SAFE  (150 MB): restore normal operation
 */

// ─── Budget Thresholds ─────────────────────────────────────────

/** Soft limit — begin graceful degradation (200 MB). */
const SOFT_LIMIT_BYTES = 200 * 1024 * 1024;

/** Hard limit — aggressive emergency measures (280 MB). */
const HARD_LIMIT_BYTES = 280 * 1024 * 1024;

/** Safe threshold — restore normal operation (150 MB). */
const SAFE_THRESHOLD_BYTES = 150 * 1024 * 1024;

/** Monitoring interval in milliseconds (5s). */
const MONITOR_INTERVAL_MS = 5000;

// ─── Budget Level ──────────────────────────────────────────────

export type BudgetLevel = 'normal' | 'soft' | 'hard';

// ─── Callbacks ─────────────────────────────────────────────────

export interface MemoryBudgetCallbacks {
  /** Called when entering SOFT or HARD budget. */
  onBudgetChange?: (level: BudgetLevel) => void;
  /** Called when recovering from SOFT/HARD back to normal. */
  onRecover?: () => void;
}

// ─── MemoryBudget ──────────────────────────────────────────────

/**
 * Monitors JS heap usage and triggers tiered degradation callbacks.
 *
 * Usage:
 * ```ts
 * const budget = new MemoryBudget({
 *   onBudgetChange: (level) => { ... degrade effects ... },
 *   onRecover: () => { ... restore effects ... },
 * });
 * budget.start();
 * // In render loop: budget.update()
 * ```
 */
export class MemoryBudget {
  private callbacks: MemoryBudgetCallbacks;
  private currentLevel: BudgetLevel = 'normal';
  private intervalId: ReturnType<typeof setInterval> | null = null;
  private started = false;

  constructor(callbacks: MemoryBudgetCallbacks = {}) {
    this.callbacks = callbacks;
  }

  /** Returns true if performance.memory API is available. */
  get isAvailable(): boolean {
    return typeof performance !== 'undefined' && 'memory' in performance;
  }

  /** Current JS heap size in bytes, or 0 if unavailable. */
  get usedHeapBytes(): number {
    if (!this.isAvailable) return 0;
    return (performance as unknown as { memory: { usedJSHeapSize: number } })
      .memory.usedJSHeapSize;
  }

  /** Current budget level. */
  get level(): BudgetLevel {
    return this.currentLevel;
  }

  /**
   * Starts periodic monitoring (every 5s).
   * Safe to call multiple times — subsequent calls are no-ops.
   */
  start(): void {
    if (this.started || !this.isAvailable) return;
    this.started = true;
    this.intervalId = setInterval(() => this.check(), MONITOR_INTERVAL_MS);
  }

  /**
   * Stops periodic monitoring.
   */
  stop(): void {
    if (this.intervalId !== null) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }
    this.started = false;
  }

  /**
   * Runs a single memory check. Called automatically by the interval,
   * but can also be called manually in the render loop for more
   * responsive degradation.
   */
  update(): void {
    if (!this.isAvailable) return;
    this.check();
  }

  /**
   * Computes the budget level from the current heap size
   * and fires callbacks on transitions.
   */
  private check(): void {
    const bytes = this.usedHeapBytes;
    if (bytes <= 0) return;

    const prevLevel = this.currentLevel;

    if (bytes >= HARD_LIMIT_BYTES) {
      this.currentLevel = 'hard';
    } else if (bytes >= SOFT_LIMIT_BYTES) {
      this.currentLevel = 'soft';
    } else if (bytes <= SAFE_THRESHOLD_BYTES) {
      this.currentLevel = 'normal';
    }
    // Between SAFE and SOFT: maintain current level (hysteresis)

    if (this.currentLevel !== prevLevel) {
      if (this.currentLevel === 'normal' && prevLevel !== 'normal') {
        this.callbacks.onRecover?.();
      } else if (this.currentLevel !== 'normal') {
        this.callbacks.onBudgetChange?.(this.currentLevel);
      }
    }
  }

  /**
   * Forces a level change. Useful for manual triggers or testing.
   */
  setLevel(level: BudgetLevel): void {
    if (level !== this.currentLevel) {
      this.currentLevel = level;
      if (level === 'normal') {
        this.callbacks.onRecover?.();
      } else {
        this.callbacks.onBudgetChange?.(level);
      }
    }
  }

  /** Destroys the monitor and clears the interval. */
  destroy(): void {
    this.stop();
  }
}
