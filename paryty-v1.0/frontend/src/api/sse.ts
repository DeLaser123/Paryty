// SSE client for timeline replay
// Enhanced with TimelineSSEClient: speed control, pause/resume, seek, ring buffer

import type { TimelineSpeed } from '../types/timeline';

export type SseEventHandler = (event: MessageEvent) => void;

// ─── Auth token wiring ─────────────────────────────────────────

/**
 * Module-level access token getter for all SSE connections.
 *
 * Auth is handled via httpOnly cookie (paryty_access_token) sent automatically
 * by the browser for same-origin requests. No longer exposing tokens in URL
 * query parameters, which were logged by proxies and visible in browser history.
 */

export function setSseTokenGetter(_fn: (() => string | null) | null): void {
  // No-op: auth is handled via httpOnly cookie sent automatically by browser.
}

/** Returns the URL unchanged; auth is handled via httpOnly cookie. */
function withAuthToken(url: string): string {
  return url;
}

export interface SseConfig {
  url: string;
  withCredentials?: boolean;
}

export class SSEClient {
  protected source: EventSource | null = null;
  protected url: string;
  private withCredentials: boolean;
  private eventHandlers: Map<string, SseEventHandler[]> = new Map();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  protected connected = false;

  constructor(config: SseConfig) {
    this.url = config.url;
    this.withCredentials = config.withCredentials ?? false;
  }

  connect(): void {
    if (this.source) this.close();

    this.source = new EventSource(withAuthToken(this.url), {
      withCredentials: this.withCredentials,
    });

    this.source.onopen = () => {
      this.connected = true;
    };

    this.source.onmessage = (event) => {
      this.dispatch('message', event);
    };

    this.source.onerror = () => {
      this.connected = false;
      // EventSource auto-reconnects
    };

    // Register named event listeners
    for (const [eventName] of this.eventHandlers) {
      if (eventName !== 'message') {
        this.source.addEventListener(eventName, (event) => {
          this.dispatch(eventName, event as MessageEvent);
        });
      }
    }
  }

  on(eventName: string, handler: SseEventHandler): () => void {
    const handlers = this.eventHandlers.get(eventName) ?? [];
    handlers.push(handler);
    this.eventHandlers.set(eventName, handlers);

    // Add listener to existing source if connected
    if (this.source && eventName !== 'message') {
      this.source.addEventListener(eventName, (event) => {
        this.dispatch(eventName, event as MessageEvent);
      });
    }

    return () => {
      const h = this.eventHandlers.get(eventName);
      if (h) {
        const idx = h.indexOf(handler);
        if (idx !== -1) h.splice(idx, 1);
      }
    };
  }

  close(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.source?.close();
    this.source = null;
    this.connected = false;
  }

  isConnected(): boolean {
    return this.connected;
  }

  protected getHandlers(eventName: string): SseEventHandler[] | undefined {
    return this.eventHandlers.get(eventName);
  }

  private dispatch(eventName: string, event: MessageEvent): void {
    const handlers = this.eventHandlers.get(eventName);
    if (handlers) {
      for (const handler of handlers) {
        handler(event);
      }
    }
  }
}

// ─── TimelineSSEClient ─────────────────────────────────────────

export interface TimelineSSEConfig {
  baseUrl: string;
  startTime: string;
  endTime: string;
  speed?: TimelineSpeed;
  /** Ring buffer size for recent events (default: 100) */
  ringBufferSize?: number;
}

export interface TimelineSSEEvent {
  id: string;
  timestamp: string;
  type: string;
  data: unknown;
}

export type TimelineSSESpeed = 0.25 | 0.5 | 1 | 2 | 4 | 8 | 16;

/**
 * Specialized SSE client for timeline replay with speed control,
 * pause/resume, seek, and a ring buffer for recent events.
 *
 * The backend replay endpoint (`GET /api/v1/timeline/replay`) accepts
 * `start`, `end`, and `speed` as connect-time query parameters and emits
 * named `snapshot` events followed by a terminal `end` event. Because SSE
 * is unidirectional and the server exposes no command channel, speed and
 * seek changes are applied by reconnecting at the current position.
 */
export class TimelineSSEClient extends SSEClient {
  private baseUrl: string;
  private initialStartTime: string;
  private endTime: string;
  private currentSpeed: TimelineSSESpeed;
  private paused = false;
  private lastPosition: string;

  // Ring buffer for recent events
  private ringBuffer: TimelineSSEEvent[] = [];
  private ringBufferSize: number;

  constructor(config: TimelineSSEConfig) {
    const speed = config.speed ?? 1;
    const url = `${config.baseUrl}/api/v1/timeline/replay?start=${encodeURIComponent(config.startTime)}&end=${encodeURIComponent(config.endTime)}&speed=${speed}`;
    super({ url });
    this.baseUrl = config.baseUrl;
    this.initialStartTime = config.startTime;
    this.endTime = config.endTime;
    this.currentSpeed = speed as TimelineSSESpeed;
    this.lastPosition = config.startTime;
    this.ringBufferSize = config.ringBufferSize ?? 100;

    // Server emits named `snapshot` events; populate the ring buffer from them.
    this.on('snapshot', (event) => {
      this.addToRingBuffer(event);
    });
  }

  /**
   * Set replay speed. Speed is a connect-time parameter on the server, so
   * this reconnects the stream at the current position with the new speed.
   * @param speed - Playback multiplier (0.25x to 16x)
   */
  setSpeed(speed: TimelineSSESpeed): void {
    this.currentSpeed = speed;
    if (!this.paused) {
      this.close();
      this.reconnectAtPosition(this.lastPosition);
    }
  }

  /**
   * Pause the replay. Closes the SSE connection and buffers current position.
   */
  pause(): void {
    this.paused = true;
    this.close();
  }

  /**
   * Resume the replay from the last known position.
   * Reconnects the SSE stream at the saved position.
   */
  resume(): void {
    if (!this.paused) return;
    this.paused = false;
    this.reconnectAtPosition(this.lastPosition);
  }

  /**
   * Seek to a specific timestamp. Disconnects current stream
   * and reconnects at the new position.
   * @param timestamp - ISO 8601 timestamp to seek to
   */
  seekTo(timestamp: string): void {
    this.lastPosition = timestamp;
    this.paused = false;
    this.close();
    this.reconnectAtPosition(timestamp);
  }

  /**
   * Reset replay to the initial start time.
   * Clears ring buffer and reconnects from the beginning.
   */
  reset(): void {
    this.ringBuffer = [];
    this.paused = false;
    this.lastPosition = this.initialStartTime;
    this.close();
    this.reconnectAtPosition(this.initialStartTime);
  }

  /**
   * Returns the current replay speed.
   */
  getSpeed(): TimelineSSESpeed {
    return this.currentSpeed;
  }

  /**
   * Returns whether the replay is paused.
   */
  isPaused(): boolean {
    return this.paused;
  }

  /**
   * Returns a copy of the ring buffer containing recent events.
   * Most recent events are at the end of the array.
   */
  getRecentEvents(): TimelineSSEEvent[] {
    return [...this.ringBuffer];
  }

  /**
   * Returns the last known replay position.
   */
  getLastPosition(): string {
    return this.lastPosition;
  }

  // ─── Private ──────────────────────────────────────────────────

  /**
   * Reconnects the SSE stream at a specific timestamp position.
   */
  private reconnectAtPosition(timestamp: string): void {
    this.lastPosition = timestamp;
    this.url = `${this.baseUrl}/api/v1/timeline/replay?start=${encodeURIComponent(timestamp)}&end=${encodeURIComponent(this.endTime)}&speed=${this.currentSpeed}`;
    this.connect();
  }

  /**
   * Adds a message event to the ring buffer, evicting oldest if full.
   */
  private addToRingBuffer(event: MessageEvent): void {
    let parsed: unknown;
    try {
      parsed = JSON.parse(event.data);
    } catch {
      parsed = event.data;
    }

    const record = parsed as Record<string, unknown>;
    const entry: TimelineSSEEvent = {
      id: (record['id'] as string) ?? `evt_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`,
      timestamp: (record['timestamp'] as string) ?? new Date().toISOString(),
      type: (record['type'] as string) ?? 'unknown',
      data: record,
    };

    this.ringBuffer.push(entry);
    if (this.ringBuffer.length > this.ringBufferSize) {
      this.ringBuffer.splice(0, this.ringBuffer.length - this.ringBufferSize);
    }
  }
}

// Create a timeline SSE connection (backward compatible)
export function createTimelineSSE(
  baseUrl: string,
  startTime: string,
  endTime: string,
  speed: number,
): SSEClient {
  const url = `${baseUrl}/api/v1/timeline/replay?start=${encodeURIComponent(startTime)}&end=${encodeURIComponent(endTime)}&speed=${speed}`;
  return new SSEClient({ url });
}
