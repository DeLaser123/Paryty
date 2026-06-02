// SSE client for timeline replay

export type SseEventHandler = (event: MessageEvent) => void;

export interface SseConfig {
  url: string;
  withCredentials?: boolean;
}

export class SSEClient {
  private source: EventSource | null = null;
  private url: string;
  private withCredentials: boolean;
  private eventHandlers: Map<string, SseEventHandler[]> = new Map();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private connected = false;

  constructor(config: SseConfig) {
    this.url = config.url;
    this.withCredentials = config.withCredentials ?? false;
  }

  connect(): void {
    if (this.source) this.close();

    this.source = new EventSource(this.url, {
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

  private dispatch(eventName: string, event: MessageEvent): void {
    const handlers = this.eventHandlers.get(eventName);
    if (handlers) {
      for (const handler of handlers) {
        handler(event);
      }
    }
  }
}

// Create a timeline SSE connection
export function createTimelineSSE(
  baseUrl: string,
  startTime: string,
  endTime: string,
  speed: number,
): SSEClient {
  const url = `${baseUrl}/api/v1/timeline/stream?start=${encodeURIComponent(startTime)}&end=${encodeURIComponent(endTime)}&speed=${speed}`;
  return new SSEClient({ url });
}
