// Type-safe REST API client for the Paryty Cluster Query API

import type { ApiError } from '../types/common';
import type { MetricQuery, MetricSeries } from '../types/metric';
import type { Topology } from '../types/topology';
import type { Trace, TraceQuery } from '../types/trace';
import type { ParytyEvent, EventQuery } from '../types/event';
import type { Alert, AlertRule } from '../types/alert';

export interface RestConfig {
  baseUrl: string;
  timeout?: number;
  retries?: number;
}

export class RestClient {
  private baseUrl: string;
  private timeout: number;
  private retries: number;

  constructor(config: RestConfig) {
    this.baseUrl = config.baseUrl || '';
    this.timeout = config.timeout || 10000;
    this.retries = config.retries ?? 2;
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    params?: Record<string, string>,
  ): Promise<T> {
    const url = new URL(path, this.baseUrl || window.location.origin);
    if (params) {
      Object.entries(params).forEach(([key, value]) => {
        url.searchParams.set(key, value);
      });
    }

    let lastError: Error | undefined;
    for (let attempt = 0; attempt <= this.retries; attempt++) {
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), this.timeout);

        const response = await fetch(url.toString(), {
          method,
          headers: {
            'Content-Type': 'application/json',
            'Accept': 'application/json',
          },
          body: body ? JSON.stringify(body) : undefined,
          signal: controller.signal,
        });

        clearTimeout(timeoutId);

        if (!response.ok) {
          const errorBody = await response.json().catch(() => ({})) as Partial<ApiError>;
          throw new ApiClientError(
            errorBody.message || `HTTP ${response.status}`,
            response.status,
            errorBody.code,
          );
        }

        return (await response.json()) as T;
      } catch (error) {
        lastError = error as Error;
        if (error instanceof ApiClientError && error.status < 500) {
          throw error; // Don't retry client errors
        }
        if (attempt < this.retries) {
          await sleep(Math.pow(2, attempt) * 1000);
        }
      }
    }
    throw lastError;
  }

  // Topology
  async getTopology(): Promise<Topology> {
    return this.request<Topology>('GET', '/api/v1/topology');
  }

  // Metrics
  async queryMetrics(query: MetricQuery): Promise<MetricSeries[]> {
    return this.request<MetricSeries[]>('POST', '/api/v1/metrics/query', query);
  }

  async getMetricNames(): Promise<string[]> {
    return this.request<string[]>('GET', '/api/v1/metrics/names');
  }

  // Traces
  async queryTraces(query: TraceQuery): Promise<Trace[]> {
    return this.request<Trace[]>('POST', '/api/v1/traces/query', query);
  }

  async getTrace(traceId: string): Promise<Trace> {
    return this.request<Trace>('GET', `/api/v1/traces/${traceId}`);
  }

  // Events
  async queryEvents(query: EventQuery): Promise<ParytyEvent[]> {
    return this.request<ParytyEvent[]>('POST', '/api/v1/events/query', query);
  }

  // Alerts
  async getAlerts(): Promise<Alert[]> {
    return this.request<Alert[]>('GET', '/api/v1/alerts');
  }

  async getAlertRules(): Promise<AlertRule[]> {
    return this.request<AlertRule[]>('GET', '/api/v1/alerts/rules');
  }

  async acknowledgeAlert(alertId: string): Promise<void> {
    await this.request<void>('POST', `/api/v1/alerts/${alertId}/acknowledge`);
  }

  // Health
  async getHealth(): Promise<{ status: string; version: string }> {
    return this.request<{ status: string; version: string }>('GET', '/api/v1/health');
  }
}

export class ApiClientError extends Error {
  constructor(
    message: string,
    public status: number,
    public code?: string,
  ) {
    super(message);
    this.name = 'ApiClientError';
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// Default client instance
let defaultClient: RestClient | undefined;

export function getRestClient(): RestClient {
  if (!defaultClient) {
    defaultClient = new RestClient({ baseUrl: '' });
  }
  return defaultClient;
}

export function setRestClient(client: RestClient): void {
  defaultClient = client;
}
