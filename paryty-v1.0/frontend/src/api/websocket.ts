// Auto-reconnecting WebSocket client for real-time data

export type WsMessageHandler = (data: unknown) => void;
export type WsStateChangeHandler = (state: WsState) => void;

export type WsState = 'connecting' | 'connected' | 'disconnected' | 'reconnecting';

export interface WsConfig {
  url: string;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
  heartbeatInterval?: number;
}

export interface WsSubscription {
  id: string;
  channel: string;
  handler: WsMessageHandler;
}

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

  constructor(config: WsConfig) {
    this.url = config.url;
    this.reconnectInterval = config.reconnectInterval ?? 3000;
    this.maxReconnectAttempts = config.maxReconnectAttempts ?? 0; // 0 = unlimited
    this.heartbeatInterval = config.heartbeatInterval ?? 30000;
  }

  connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN) return;

    this.intentionalClose = false;
    this.setState('connecting');

    try {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url = this.url.startsWith('ws') ? this.url : `${protocol}//${window.location.host}${this.url}`;
      this.ws = new WebSocket(url);
    } catch {
      this.setState('disconnected');
      this.scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this.reconnectAttempts = 0;
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
        if (msg.type === 'pong') return; // heartbeat response

        // Dispatch to channel handlers
        const channel = msg.channel ?? msg.type;
        const handlers = this.messageHandlers.get(channel);
        if (handlers) {
          for (const handler of handlers) {
            handler(msg.data ?? msg);
          }
        }

        // Dispatch to subscription handler
        if (msg.subscriptionId) {
          const sub = this.subscriptions.get(msg.subscriptionId);
          sub?.handler(msg.data ?? msg);
        }
      } catch {
        // Ignore malformed messages
      }
    };

    this.ws.onclose = () => {
      this.stopHeartbeat();
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
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.setState('disconnected');
  }

  subscribe(channel: string, handler: WsMessageHandler): string {
    const id = `sub_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
    const sub: WsSubscription = { id, channel, handler };
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
    if (this.maxReconnectAttempts > 0 && this.reconnectAttempts >= this.maxReconnectAttempts) {
      return;
    }

    const delay = Math.min(
      this.reconnectInterval * Math.pow(1.5, this.reconnectAttempts),
      30000,
    );
    this.reconnectAttempts++;

    this.reconnectTimer = setTimeout(() => {
      this.connect();
    }, delay);
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
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

interface WsIncomingMessage {
  type: string;
  channel?: string;
  subscriptionId?: string;
  data?: unknown;
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
