/**
 * EventIngest Pipeline — Tri-Layer Event Feed for Semantic Particles
 *
 * Implements a WebSocket → REST → SSE state machine with graceful degradation:
 *   - WebSocket (primary): push events via 'events' channel subscription
 *   - REST (fallback): 5s polling when WS disconnects
 *   - SSE (tertiary): streaming fallback after 3 consecutive REST failures
 *   - 5-second grace window before promoting REST → WS
 *
 * Degradation is instant (ws→rest on disconnect). Promotion is gated.
 *
 * Features:
 *   - RingBuffer backpressure (capacity 500, silent overflow)
 *   - MemoryBudget integration (transport-aware throttling)
 *   - Cumulative totalEventsReceived counter via particleStore
 *   - Start/stop/pause/resume lifecycle (backward-compatible)
 */

import { RingBuffer } from './ringBuffer';
import { getRestClient } from '../api/rest';
import { getWsClient } from '../api/websocket';
import type { WsState } from '../api/websocket';
import { SSEClient } from '../api/sse';
import type { BudgetLevel } from './memoryBudget';
import type { ParytyEvent, TransportMode } from '../types/event';
import type { EventQuery } from '../types/event';
import { useParticleStore } from '../stores/particleStore';

// ─── Configuration ──────────────────────────────────────────────

export interface EventIngestConfig {
  /** Grace window in ms before promoting REST → WS (default: 5000). */
  graceWindowMs: number;
  /** REST polling interval in ms when in REST mode (default: 5000). */
  restIntervalMs: number;
  /** Number of consecutive REST failures before degrading to SSE (default: 3). */
  restFailureThreshold: number;
  /** RingBuffer capacity for the ingest queue (default: 500). */
  queueCapacity: number;
  /** SSE stream URL (default: '/api/v1/events/stream'). */
  sseUrl: string;
}

const DEFAULT_CONFIG: EventIngestConfig = {
  graceWindowMs: 5000,
  restIntervalMs: 5000,
  restFailureThreshold: 3,
  queueCapacity: 500,
  sseUrl: '/api/v1/events/stream',
};

/** Slow-poll interval when memory budget is SOFT. */
const SOFT_POLL_INTERVAL_MS = 10000;

/** Time window overlap in milliseconds to avoid gaps between polls. */
const POLL_OVERLAP_MS = 1000;

// ─── EventIngest ────────────────────────────────────────────────

export class EventIngest {
  /** Ring buffer feeding events to the particle system. */
  readonly ringBuffer: RingBuffer<ParytyEvent>;

  private config: EventIngestConfig;

  /** Current transport mode. */
  private _transportMode: TransportMode = 'ws';

  // WebSocket state
  private wsStateUnsubscriber: (() => void) | null = null;
  private wsSubscriptionId: string | null = null;

  // REST state
  private restTimerId: ReturnType<typeof setInterval> | null = null;
  private consecutiveRestFailures = 0;
  private lastPollTime: number = Date.now();
  private restIntervalMs: number;

  // SSE state
  private sseClient: SSEClient | null = null;
  private sseUnsubscriber: (() => void) | null = null;

  // Grace window state
  private graceTimerId: ReturnType<typeof setTimeout> | null = null;

  // Lifecycle
  private _started = false;
  private _stopped = false;

  /** Last seen event ID for deduplication. */
  private lastEventId: string | null = null;

  /** Total number of distinct events ingested (cumulative). */
  private _totalEventsReceived = 0;

  constructor(config: Partial<EventIngestConfig> = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.restIntervalMs = this.config.restIntervalMs;
    this.ringBuffer = new RingBuffer<ParytyEvent>(this.config.queueCapacity);
  }

  /** Current transport mode (read-only). */
  get transportMode(): TransportMode {
    return this._transportMode;
  }

  /** Cumulative total of distinct events received. */
  get totalEventsReceived(): number {
    return this._totalEventsReceived;
  }

  /** Whether the ingest pipeline is currently active (not stopped). */
  get isActive(): boolean {
    return this._started && !this._stopped;
  }

  // ─── Lifecycle ────────────────────────────────────────────────

  /**
   * Starts the tri-layer ingest pipeline.
   * Safe to call multiple times.
   */
  start(): void {
    if (this._started && !this._stopped) return;
    this._started = true;
    this._stopped = false;
    this.lastPollTime = Date.now();

    const wsClient = getWsClient();

    // Listen for WS state changes
    this.wsStateUnsubscriber = wsClient.onStateChange((state: WsState) => {
      this.onWsStateChange(state);
    });

    // Try WebSocket first
    if (wsClient.getState() === 'connected') {
      this.subscribeWs();
      this.transitionTo('ws');
    } else {
      // WS not connected — start REST immediately
      this.startRestPolling();
      this.transitionTo('rest');
    }
  }

  /** Stops the pipeline permanently. Cannot be resumed. */
  stop(): void {
    this._stopped = true;
    this._started = false;
    this.unsubscribeWs();
    this.stopRestPolling();
    this.closeSse();
    this.cancelGraceWindow();
    this.wsStateUnsubscriber?.();
    this.wsStateUnsubscriber = null;
    this.ringBuffer.clear();
    this.transitionTo('offline');
  }

  /** Pauses the pipeline without clearing the buffer. */
  pause(): void {
    this._started = false;
    this.unsubscribeWs();
    this.stopRestPolling();
    this.closeSse();
    this.cancelGraceWindow();
  }

  /** Resumes the pipeline after a pause. */
  resume(): void {
    if (this._stopped) return;
    this.start();
  }

  /**
   * Drains all available events from the ring buffer.
   * Called by SemanticParticleSystem each frame.
   *
   * @returns Array of events drained from the queue.
   */
  drain(): ParytyEvent[] {
    const events = this.ringBuffer.toArray();
    this.ringBuffer.clear();
    return events;
  }

  /**
   * Adjusts behavior based on memory budget level (transport-aware).
   *
   * | Budget | WS                    | REST                | SSE              |
   * |--------|-----------------------|---------------------|------------------|
   * | normal | Unthrottled           | Poll at 5s          | Closed           |
   * | soft   | Drop non-critical     | Poll at 10s         | Closed           |
   * | hard   | Unsubscribe events    | Stop, clear buffer  | Close, clear     |
   */
  setBudgetLevel(level: BudgetLevel): void {
    switch (level) {
      case 'normal':
        this.restIntervalMs = this.config.restIntervalMs;
        if (!this._started && !this._stopped) {
          this.resume();
        }
        break;
      case 'soft':
        this.restIntervalMs = SOFT_POLL_INTERVAL_MS;
        break;
      case 'hard':
        this.unsubscribeWs();
        this.stopRestPolling();
        this.closeSse();
        this.ringBuffer.clear();
        break;
    }
  }

  // ─── Transport Mode State Machine ─────────────────────────────

  /**
   * Central dispatcher: sets transport mode and propagates to particleStore.
   * Manages transport lifecycle based on mode.
   */
  private transitionTo(mode: TransportMode): void {
    if (this._transportMode === mode && mode !== 'offline') return;
    this._transportMode = mode;
    useParticleStore.getState().setTransportMode(mode);
  }

  // ─── WebSocket ────────────────────────────────────────────────

  /** Subscribes to the WebSocket 'events' channel. */
  private subscribeWs(): void {
    if (this.wsSubscriptionId) return;
    const wsClient = getWsClient();
    this.wsSubscriptionId = wsClient.subscribe('events', (data: unknown) => {
      this.onWsMessage(data);
    });
  }

  /** Unsubscribes from the WebSocket 'events' channel. */
  private unsubscribeWs(): void {
    if (this.wsSubscriptionId) {
      getWsClient().unsubscribe(this.wsSubscriptionId);
      this.wsSubscriptionId = null;
    }
  }

  /** Handles incoming WebSocket event messages. */
  private onWsMessage(data: unknown): void {
    try {
      const event = data as ParytyEvent;
      if (!event || !event.id) return;

      // Deduplication
      if (this.lastEventId && event.id <= this.lastEventId) return;

      this.ringBuffer.push(event);
      this._totalEventsReceived++;
      useParticleStore.getState().incrementTotalEventsReceived(1);
      this.lastEventId = event.id;

      // Reset REST failure counter — WS data arrived
      this.consecutiveRestFailures = 0;
    } catch {
      // Silently ignore malformed messages
    }
  }

  /** Handles WebSocket state changes. */
  private onWsStateChange(state: WsState): void {
    if (this._stopped) return;

    switch (state) {
      case 'connected':
        this.subscribeWs();
        if (this._transportMode === 'rest' || this._transportMode === 'sse') {
          this.startGraceWindow();
        }
        break;
      case 'disconnected':
      case 'reconnecting':
        this.cancelGraceWindow();
        if (this._transportMode === 'ws') {
          this.startRestPolling();
          this.transitionTo('rest');
        }
        break;
    }
  }

  // ─── REST Polling ─────────────────────────────────────────────

  /** Starts REST polling at the configured interval. */
  private startRestPolling(): void {
    if (this.restTimerId !== null) return;
    this.lastPollTime = Date.now();
    // Fire first poll immediately, then at interval
    this.pollRest();
    this.restTimerId = setInterval(() => this.pollRest(), this.restIntervalMs);
  }

  /** Stops REST polling. */
  private stopRestPolling(): void {
    if (this.restTimerId !== null) {
      clearInterval(this.restTimerId);
      this.restTimerId = null;
    }
  }

  /** Executes a single REST poll cycle. */
  private async pollRest(): Promise<void> {
    if (this._stopped) return;

    try {
      const now = Date.now();
      const startTime = new Date(
        this.lastPollTime - POLL_OVERLAP_MS,
      ).toISOString();
      const endTime = new Date(now).toISOString();

      const query: EventQuery = {
        startTime,
        endTime,
        limit: 500,
      };

      const restClient = getRestClient();
      const events = await restClient.queryEvents(query);

      if (events.length > 0) {
        let newEvents: ParytyEvent[];
        if (this.lastEventId) {
          const lastIdx = events.findIndex(
            (e) => e.id === this.lastEventId,
          );
          newEvents = lastIdx >= 0 ? events.slice(lastIdx + 1) : events;
        } else {
          newEvents = events;
        }

        if (newEvents.length > 0) {
          this.ringBuffer.pushMany(newEvents);
          this._totalEventsReceived += newEvents.length;
          useParticleStore
            .getState()
            .incrementTotalEventsReceived(newEvents.length);
          this.lastEventId = newEvents[newEvents.length - 1].id;
        }
      }

      this.lastPollTime = now;
      this.consecutiveRestFailures = 0;
    } catch {
      this.consecutiveRestFailures++;
      if (
        this.consecutiveRestFailures >= this.config.restFailureThreshold &&
        this._transportMode === 'rest'
      ) {
        this.stopRestPolling();
        this.startSse();
        this.transitionTo('sse');
      }
    }
  }

  // ─── SSE ──────────────────────────────────────────────────────

  /** Starts the SSE tertiary stream. */
  private startSse(): void {
    if (this.sseClient) return;

    this.sseClient = new SSEClient({ url: this.config.sseUrl });
    this.sseUnsubscriber = this.sseClient.on('event', (msg: MessageEvent) => {
      this.onSseMessage(msg);
    });
    this.sseClient.connect();
  }

  /** Closes the SSE stream. */
  private closeSse(): void {
    this.sseUnsubscriber?.();
    this.sseUnsubscriber = null;
    this.sseClient?.close();
    this.sseClient = null;
  }

  /** Handles an SSE event message. */
  private onSseMessage(msg: MessageEvent): void {
    try {
      const event: ParytyEvent = JSON.parse(msg.data);
      if (!event || !event.id) return;

      if (this.lastEventId && event.id <= this.lastEventId) return;

      this.ringBuffer.push(event);
      this._totalEventsReceived++;
      useParticleStore.getState().incrementTotalEventsReceived(1);
      this.lastEventId = event.id;
    } catch {
      // Silently ignore parse failures
    }
  }

  // ─── Grace Window ─────────────────────────────────────────────

  /**
   * Starts the 5-second grace window before promoting REST → WS.
   * On expiry, checks WS is still connected before committing.
   */
  private startGraceWindow(): void {
    this.cancelGraceWindow();
    this.graceTimerId = setTimeout(() => this.commitPromotion(), this.config.graceWindowMs);
  }

  /** Cancels the grace window timer. */
  private cancelGraceWindow(): void {
    if (this.graceTimerId !== null) {
      clearTimeout(this.graceTimerId);
      this.graceTimerId = null;
    }
  }

  /** Commits promotion from REST/SSE → WS if WS is still stable. */
  private commitPromotion(): void {
    this.graceTimerId = null;
    if (getWsClient().getState() === 'connected') {
      this.stopRestPolling();
      this.closeSse();
      this.transitionTo('ws');
    }
    // else: WS dropped during grace window — stay in current mode
  }
}
