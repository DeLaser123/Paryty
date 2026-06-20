// Auto-reconnecting WebSocket client for real-time data
// Enhanced with dedup, batching, backpressure, priority channels, connection quality,
// and auth token injection.

export type WsMessageHandler = (data: unknown) => void;
export type WsStateChangeHandler = (state: WsState) => void;

export type WsState = 'connecting' | 'connected' | 'disconnected' | 'reconnecting';

export type WsChannelPriority = 'critical' | 'high' | 'normal' | 'low';

export interface WsConfig {
  url: string;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
  heartbeatInterval?: number;
  /** Maximum message IDs kept for deduplication (default: 1000) */
  deduplicationWindowSize?: number;
  /** Batch flush interval in ms (default: 16 — one frame) */
  batchIntervalMs?: number;
  /** Max batch size before forced flush (default: 500) */
  maxBatchSize?: number;
}

export interface WsSubscription {
  id: string;
  channel: string;
  handler: WsMessageHandler;
  priority: WsChannelPriority;
}

export interface WsConnectionQuality {
  /** Round-trip latency in ms (ping/pong) */
  latencyMs: number;
  /** Messages received per second */
  messagesPerSec: number;
  /** Ratio of dropped messages (0-1) */
  dropRate: number;
  /** Total messages received since connection */
  totalReceived: number;
  /** Total messages dropped due to backpressure */
  totalDropped: number;
  /** Current connection uptime in ms */
  uptimeMs: number;
}

/** Priority map: critical channels are never dropped */
const CHANNEL_PRIORITIES: Record<string, WsChannelPriority> = {
  topology: 'critical',
  'topology.diff': 'critical',
  alerts: 'high',
  'alerts.update': 'high',
  'intel.anomalies': 'high',
  'intel.forecasts': 'high',
  'intel.drift': 'high',
  metrics: 'normal',
  'metrics.batch': 'normal',
  events: 'low',
};

interface BatchedMessage {
  channel: string;
  data: unknown;
  id: string;
  receivedAt: number;
}

interface WsIncomingMessage {
  type: string;
  channel?: string;
  subscriptionId?: string;
  data?: unknown;
  id?: string;
  timestamp?: number;
}

/** Cap for aggressive (exponential backoff) reconnect phase. */
const MAX_AGGRESSIVE_RECONNECT = 10;
/** Interval for silent background reconnect after aggressive phase (ms). */
const SILENT_RECONNECT_INTERVAL = 60_000;

export class WebSocketClient {
  private ws: WebSocket | null = null;
  private state: WsState = 'disconnected';
  private subscriptions: Map<string, WsSubscription> = new Map();
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private stateHandlers: WsStateChangeHandler[] = [];
  private messageHandlers: Map<string, WsMessageHandler[]> = new Map();
  private url: string;
  private reconnectInterval: number;
  private maxReconnectAttempts: number;
  private heartbeatInterval: number;
  private intentionalClose = false;
  /** true once the aggressive backoff phase has been exhausted. */
  private silentReconnect = false;
  /** Whether a "giving up" warning has been logged (once per session). */
  private gaveUpLogged = false;
  /** Returns the current access token for WS auth query parameter. */
  private tokenGetter: (() => string | null) | null = null;

  // === Deduplication ===
  private recentMessageIds: Set<string> = new Set();
  private deduplicationWindowSize: number;

  // === Batching ===
  private batch: BatchedMessage[] = [];
  private batchTimer: ReturnType<typeof setTimeout> | null = null;
  private batchIntervalMs: number;
  private maxBatchSize: number;

  // === Backpressure ===
  /** Latest message ID seen per channel */
  private latestMessageIds: Map<string, string> = new Map();
  /** Latest message ID fully processed per channel */
  private processedMessageIds: Map<string, string> = new Map();

  // === Connection Quality ===
  private quality = {
    latencyMs: 0,
    messagesPerSec: 0,
    dropRate: 0,
    totalReceived: 0,
    totalDropped: 0,
    uptimeMs: 0,
    connectedAt: 0,
    pingSentAt: 0,
    messageTimestamps: [] as number[],
  };

  constructor(config: WsConfig) {
    this.url = config.url;
    this.reconnectInterval = config.reconnectInterval ?? 3000;
    this.maxReconnectAttempts = config.maxReconnectAttempts ?? 0; // 0 = unlimited
    this.heartbeatInterval = config.heartbeatInterval ?? 30000;
    this.deduplicationWindowSize = config.deduplicationWindowSize ?? 1000;
    this.batchIntervalMs = config.batchIntervalMs ?? 16;
    this.maxBatchSize = config.maxBatchSize ?? 500;
  }

  /**
   * Set a function that returns the current access token.
   * The token is appended as a query parameter to the WebSocket URL because
   * the browser WebSocket API cannot set custom headers. The backend's
   * GinJWTAuthFlexible middleware accepts the token from the "token" query
   * parameter when no Authorization header or httpOnly cookie is present.
   */
  setTokenGetter(fn: (() => string | null) | null): void {
    this.tokenGetter = fn;
  }

  /**
   * Build the WebSocket connection URL with auth token.
   * The browser WebSocket API cannot set custom headers, so the access
   * token is passed as a query parameter. The backend's GinJWTAuthFlexible
   * middleware accepts this for streaming endpoints (WebSocket, SSE).
   *
   * When no token is available (e.g. before login), connects without one —
   * the backend will reject with 401, triggering the reconnect backoff.
   */
  private buildUrl(): string {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const baseUrl = this.url.startsWith('ws') ? this.url : `${protocol}//${window.location.host}${this.url}`;

    const token = this.tokenGetter?.();
    if (token) {
      const separator = baseUrl.includes('?') ? '&' : '?';
      return `${baseUrl}${separator}token=${encodeURIComponent(token)}`;
    }
    return baseUrl;
  }

  connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN) return;

    this.intentionalClose = false;
    this.setState('connecting');

    try {
      const url = this.buildUrl();
      this.ws = new WebSocket(url);
    } catch {
      this.setState('disconnected');
      this.scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this.reconnectAttempts = 0;
      this.silentReconnect = false;
      this.gaveUpLogged = false;
      this.quality.connectedAt = Date.now();
      this.setState('connected');
      this.startHeartbeat();

      // Re-subscribe to all channels
      for (const sub of this.subscriptions.values()) {
        this.send({ type: 'subscribe', channel: sub.channel, id: sub.id });
      }
    };

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data) as WsIncomingMessage;
        if (msg.type === 'pong') {
          // Calculate latency from ping
          if (this.quality.pingSentAt > 0) {
            this.quality.latencyMs = Date.now() - this.quality.pingSentAt;
            this.quality.pingSentAt = 0;
          }
          return;
        }

        // Route through processing pipeline
        this.processMessage(msg);
      } catch {
        // Ignore malformed messages
      }
    };

    this.ws.onclose = () => {
      this.stopHeartbeat();
      this.flushBatch();
      if (!this.intentionalClose) {
        this.setState('reconnecting');
        this.scheduleReconnect();
      } else {
        this.setState('disconnected');
      }
    };

    this.ws.onerror = () => {
      this.ws?.close();
    };
  }

  disconnect(): void {
    this.intentionalClose = true;
    this.stopHeartbeat();
    this.flushBatch();
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.setState('disconnected');
  }

  subscribe(channel: string, handler: WsMessageHandler): string {
    const id = `sub_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
    const priority = CHANNEL_PRIORITIES[channel] ?? 'normal';
    const sub: WsSubscription = { id, channel, handler, priority };
    this.subscriptions.set(id, sub);

    // Also register in message handlers
    const handlers = this.messageHandlers.get(channel) ?? [];
    handlers.push(handler);
    this.messageHandlers.set(channel, handlers);

    // Send subscribe message if connected
    if (this.state === 'connected') {
      this.send({ type: 'subscribe', channel, id });
    }

    return id;
  }

  unsubscribe(subscriptionId: string): void {
    const sub = this.subscriptions.get(subscriptionId);
    if (sub) {
      this.subscriptions.delete(subscriptionId);
      // Also remove from messageHandlers to prevent leaked handlers
      const handlers = this.messageHandlers.get(sub.channel);
      if (handlers) {
        const idx = handlers.indexOf(sub.handler);
        if (idx !== -1) handlers.splice(idx, 1);
        if (handlers.length === 0) this.messageHandlers.delete(sub.channel);
      }
      if (this.state === 'connected') {
        this.send({ type: 'unsubscribe', id: subscriptionId });
      }
    }
  }

  onStateChange(handler: WsStateChangeHandler): () => void {
    this.stateHandlers.push(handler);
    return () => {
      const idx = this.stateHandlers.indexOf(handler);
      if (idx !== -1) this.stateHandlers.splice(idx, 1);
    };
  }

  getState(): WsState {
    return this.state;
  }

  /**
   * Returns current connection quality metrics.
   * Latency is measured via ping/pong round-trip.
   * Message rate is a rolling average over the last 5 seconds.
   * Drop rate is the ratio of dropped messages since connection.
   */
  getConnectionQuality(): WsConnectionQuality {
    const now = Date.now();
    const uptime = this.quality.connectedAt > 0 ? now - this.quality.connectedAt : 0;

    // Calculate rolling message rate (messages per second over last 5s)
    const windowMs = 5000;
    const cutoff = now - windowMs;
    // Prune old timestamps
    while (
      this.quality.messageTimestamps.length > 0 &&
      this.quality.messageTimestamps[0] < cutoff
    ) {
      this.quality.messageTimestamps.shift();
    }
    const messagesPerSec =
      this.quality.messageTimestamps.length / (windowMs / 1000);

    // Calculate drop rate
    const total = this.quality.totalReceived + this.quality.totalDropped;
    const dropRate = total > 0 ? this.quality.totalDropped / total : 0;

    this.quality.messagesPerSec = messagesPerSec;
    this.quality.dropRate = dropRate;
    this.quality.uptimeMs = uptime;

    return {
      latencyMs: this.quality.latencyMs,
      messagesPerSec,
      dropRate,
      totalReceived: this.quality.totalReceived,
      totalDropped: this.quality.totalDropped,
      uptimeMs: uptime,
    };
  }

  // ─── Private Methods ───────────────────────────────────────────

  /**
   * Processes an incoming message through dedup, backpressure, and batching.
   * Critical-priority channels are never dropped by backpressure.
   */
  private processMessage(msg: WsIncomingMessage): void {
    const channel = msg.channel ?? msg.type;
    const msgId = msg.id ?? `${channel}_${msg.timestamp ?? Date.now()}`;

    // 1. Deduplication — drop if already seen
    if (this.recentMessageIds.has(msgId)) {
      return;
    }
    this.recentMessageIds.add(msgId);
    // Evict oldest if over window size
    if (this.recentMessageIds.size > this.deduplicationWindowSize) {
      const first = this.recentMessageIds.values().next().value;
      if (first !== undefined) {
        this.recentMessageIds.delete(first);
      }
    }

    // Track quality
    this.quality.totalReceived++;
    this.quality.messageTimestamps.push(Date.now());

    // 2. Backpressure — check if we've fallen behind on this channel
    const priority = this.getChannelPriority(channel);
    this.latestMessageIds.set(channel, msgId);

    if (priority !== 'critical') {
      // For non-critical channels, if there's already a pending unprocessed message
      // in the batch for this channel, skip the old one (replace with new)
      const existingIdx = this.batch.findIndex((m) => m.channel === channel);
      if (existingIdx !== -1) {
        this.batch[existingIdx] = {
          channel,
          data: msg.data ?? msg,
          id: msgId,
          receivedAt: Date.now(),
        };
        this.quality.totalDropped++;
        return;
      }
    }

    // 3. Batch — accumulate for flush
    this.batch.push({
      channel,
      data: msg.data ?? msg,
      id: msgId,
      receivedAt: Date.now(),
    });

    // Flush immediately if batch is full
    if (this.batch.length >= this.maxBatchSize) {
      this.flushBatch();
    } else if (!this.batchTimer) {
      // Schedule flush at next frame boundary
      this.batchTimer = setTimeout(() => {
        this.flushBatch();
      }, this.batchIntervalMs);
    }
  }

  /**
   * Flushes all accumulated batched messages to their handlers.
   * Groups by channel for efficient dispatch.
   */
  private flushBatch(): void {
    if (this.batchTimer) {
      clearTimeout(this.batchTimer);
      this.batchTimer = null;
    }

    if (this.batch.length === 0) return;

    // Group messages by channel
    const byChannel = new Map<string, unknown[]>();
    for (const msg of this.batch) {
      const arr = byChannel.get(msg.channel) ?? [];
      arr.push(msg.data);
      byChannel.set(msg.channel, arr);

      // Mark as processed for backpressure tracking
      this.processedMessageIds.set(msg.channel, msg.id);
    }

    // Clear batch
    this.batch = [];

    // Dispatch grouped messages to subscription handlers
    for (const [channel, messages] of byChannel) {
      for (const sub of this.subscriptions.values()) {
        if (sub.channel === channel) {
          if (messages.length === 1) {
            sub.handler(messages[0]);
          } else {
            sub.handler(messages);
          }
        }
      }
    }
  }

  /**
   * Returns the priority level for a given channel.
   * Uses explicit map first, then falls back to 'normal'.
   */
  private getChannelPriority(channel: string): WsChannelPriority {
    return CHANNEL_PRIORITIES[channel] ?? 'normal';
  }

  private send(data: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    }
  }

  private setState(state: WsState): void {
    this.state = state;
    for (const handler of this.stateHandlers) {
      handler(state);
    }
  }

  private scheduleReconnect(): void {
    // Honor explicit max-attempts cap (if configured).
    if (this.maxReconnectAttempts > 0 && this.reconnectAttempts >= this.maxReconnectAttempts) {
      return;
    }

    // After exhausting aggressive backoff, switch to silent background
    // retry at a fixed long interval.  This prevents console spam from
    // repeated browser-level WebSocket error logs while still recovering
    // automatically when the backend becomes available.
    if (this.silentReconnect) {
      this.reconnectTimer = setTimeout(() => {
        this.connect();
      }, SILENT_RECONNECT_INTERVAL);
      return;
    }

    const delay = Math.min(
      this.reconnectInterval * Math.pow(1.5, this.reconnectAttempts),
      30000,
    );
    this.reconnectAttempts++;

    // Transition to silent phase after MAX_AGGRESSIVE_RECONNECT attempts.
    if (this.reconnectAttempts >= MAX_AGGRESSIVE_RECONNECT) {
      this.silentReconnect = true;
      if (!this.gaveUpLogged) {
        this.gaveUpLogged = true;
        console.warn(
          '[ws] Could not reach the real-time server after %d attempts. '
          + 'Will keep retrying silently every %ds in the background.',
          this.reconnectAttempts,
          SILENT_RECONNECT_INTERVAL / 1000,
        );
      }
    }

    this.reconnectTimer = setTimeout(() => {
      this.connect();
    }, delay);
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      this.quality.pingSentAt = Date.now();
      this.send({ type: 'ping' });
    }, this.heartbeatInterval);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }
}

// Default client instance
let defaultWsClient: WebSocketClient | undefined;

export function getWsClient(): WebSocketClient {
  if (!defaultWsClient) {
    defaultWsClient = new WebSocketClient({ url: '/ws' });
  }
  return defaultWsClient;
}

export function setWsClient(client: WebSocketClient): void {
  defaultWsClient = client;
}
