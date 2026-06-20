// Type-safe REST API client for the Paryty Cluster Query API
// Enhanced with request cancellation, LRU caching, auth token injection,
// and automatic token refresh on 401 responses.

import type { ApiError } from '../types/common';
import type { MetricQuery, MetricSeries } from '../types/metric';
import type { Topology, TopologyNode } from '../types/topology';
import type { Trace, TraceQuery } from '../types/trace';
import type { ParytyEvent, EventQuery } from '../types/event';
import type { Alert, AlertRule } from '../types/alert';
import type { TimelineSnapshot } from '../types/timeline';
import type { AgentInfo, HealthReport, AgentPairingStatus, AgentManagementInfo } from '../types/agent';
import type { NetworkEvent, NetworkEventQuery } from '../types/network';
import type { AgentMetricBatch, AgentAggregatedMetric } from '../types/agentMetrics';
import type {
  ForecastQuery,
  ForecastSeries,
  ModelInfo,
  Anomaly,
  AnomalyDetectionStatus,
} from '../types/intel';

export interface RestConfig {
  baseUrl: string;
  timeout?: number;
  retries?: number;
}

// ─── Request Cancellation ───────────────────────────────────────

export interface RequestOptions {
  /** AbortSignal for request cancellation */
  signal?: AbortSignal;
  /** Override default timeout for this request */
  timeout?: number;
  /** Whether this request should skip auth token injection. */
  skipAuth?: boolean;
}

// ─── LRU Cache ─────────────────────────────────────────────────

interface CacheEntry<T> {
  data: T;
  expiresAt: number;
  sizeBytes: number;
}

/** Size-aware LRU budget (5 MB). */
const LRU_MAX_BYTES = 5 * 1024 * 1024;

/**
 * Size-aware LRU cache with TTL support.
 * Evicts entries by age when byte budget is exceeded.
 * Tracks entry size to bound total memory consumption.
 */
class LRUCache {
  private cache: Map<string, CacheEntry<unknown>> = new Map();
  private maxBytes: number;
  private currentBytes = 0;

  constructor(maxBytes: number = LRU_MAX_BYTES) {
    this.maxBytes = maxBytes;
  }

  /** Estimates the JSON byte size of a value. */
  private estimateSize(data: unknown): number {
    try {
      return new TextEncoder().encode(JSON.stringify(data)).length;
    } catch {
      return 0;
    }
  }

  get<T>(key: string): T | null {
    const entry = this.cache.get(key);
    if (!entry) return null;
    if (Date.now() > entry.expiresAt) {
      this.currentBytes -= entry.sizeBytes;
      this.cache.delete(key);
      return null;
    }
    // Move to end (most recently used)
    this.cache.delete(key);
    this.cache.set(key, entry);
    return entry.data as T;
  }

  set<T>(key: string, data: T, ttlMs: number): void {
    const sizeBytes = this.estimateSize(data);

    // Evict oldest entries until under budget
    while (this.cache.size > 0 && this.currentBytes + sizeBytes > this.maxBytes) {
      const firstKey = this.cache.keys().next().value;
      if (firstKey === undefined) break;
      const old = this.cache.get(firstKey);
      if (old) {
        this.currentBytes -= old.sizeBytes;
      }
      this.cache.delete(firstKey);
    }

    this.cache.set(key, { data, expiresAt: Date.now() + ttlMs, sizeBytes });
    this.currentBytes += sizeBytes;
  }

  invalidate(key: string): void {
    const entry = this.cache.get(key);
    if (entry) {
      this.currentBytes -= entry.sizeBytes;
    }
    this.cache.delete(key);
  }

  clear(): void {
    this.cache.clear();
    this.currentBytes = 0;
  }

  /** Returns the current estimated byte size of all cached entries. */
  get currentSizeBytes(): number {
    return this.currentBytes;
  }
}

/** Cache TTLs in milliseconds */
const CACHE_TTL = {
  topology: 5000,
  metricNames: 30000,
  forecast: 30000,
  modelAccuracy: 60000,
} as const;

// ─── Snapshot Types ─────────────────────────────────────────────

export interface SnapshotParams {
  startTime?: string;
  endTime?: string;
  limit?: number;
}

export interface SnapshotDiff {
  fromId: string;
  toId: string;
  addedNodes: TopologyNode[];
  removedNodes: string[];
  addedEdges: string[];
  removedEdges: string[];
  metricChanges: Record<string, { from: number; to: number }>;
}

// ─── Error Sanitization ────────────────────────────────────────

/**
 * Strip HTML tags from a string to prevent XSS when displaying
 * backend error messages in the UI.
 */
function sanitizeErrorMessage(message: string): string {
  return message.replace(/<[^>]*>/g, '');
}

// ─── RestClient ─────────────────────────────────────────────────

export class RestClient {
  private baseUrl: string;
  private timeout: number;
  private retries: number;
  private cache: LRUCache;
  /** Returns the current access token, or null if not authenticated. */
  private tokenGetter: (() => string | null) | null = null;
  /** Called on 401 — attempts token refresh, returns true on success. */
  private authRefreshCallback: (() => Promise<boolean>) | null = null;

  constructor(config: RestConfig) {
    this.baseUrl = config.baseUrl || '';
    this.timeout = config.timeout || 10000;
    this.retries = config.retries ?? 2;
    this.cache = new LRUCache();
  }

  /** Set a function that returns the current access token. */
  setTokenGetter(fn: (() => string | null) | null): void {
    this.tokenGetter = fn;
  }

  /** Set a callback that attempts to refresh the auth token. Returns true on success. */
  setAuthRefreshCallback(fn: (() => Promise<boolean>) | null): void {
    this.authRefreshCallback = fn;
  }

  /**
   * Core request method with auth injection, 401 retry, and exponential backoff.
   * @internal
   */
  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    params?: Record<string, string>,
    options?: RequestOptions,
  ): Promise<T> {
    const url = new URL(path, this.baseUrl || window.location.origin);
    if (params) {
      Object.entries(params).forEach(([key, value]) => {
        url.searchParams.set(key, value);
      });
    }

    const externalSignal = options?.signal;
    const requestTimeout = options?.timeout ?? this.timeout;

    let lastError: Error | undefined;
    for (let attempt = 0; attempt <= this.retries; attempt++) {
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), requestTimeout);

        if (externalSignal?.aborted) {
          throw new DOMException('Aborted', 'AbortError');
        }

        const onExternalAbort = (): void => controller.abort();
        externalSignal?.addEventListener('abort', onExternalAbort);

        // Build headers with optional auth token
        const headers: Record<string, string> = {
          'Content-Type': 'application/json',
          'Accept': 'application/json',
        };
        if (!options?.skipAuth && this.tokenGetter) {
          const token = this.tokenGetter();
          if (token) {
            headers['Authorization'] = `Bearer ${token}`;
          }
        }

        const response = await fetch(url.toString(), {
          method,
          headers,
          body: body ? JSON.stringify(body) : undefined,
          signal: controller.signal,
          credentials: 'include',  // Include httpOnly cookies for refresh token
        });

        clearTimeout(timeoutId);
        externalSignal?.removeEventListener('abort', onExternalAbort);

        if (!response.ok) {
          const errorBody = await response.json().catch(() => ({})) as Partial<ApiError>;

          // 401 handling: attempt token refresh and retry
          if (response.status === 401 && this.authRefreshCallback && !options?.skipAuth) {
            const refreshed = await this.authRefreshCallback();
            if (refreshed) {
              // Retry the request once with the new token — do not count as an attempt
              continue;
            }
          }

          throw new ApiClientError(
            sanitizeErrorMessage(errorBody.message || `HTTP ${response.status}`),
            response.status,
            errorBody.code,
          );
        }

        return (await response.json()) as T;
      } catch (error) {
        lastError = error as Error;
        if (error instanceof ApiClientError && error.status < 500) {
          throw error; // Don't retry client errors (except 401 handled above)
        }
        if (error instanceof DOMException && error.name === 'AbortError') {
          throw error; // Don't retry aborted requests
        }
        if (attempt < this.retries) {
          await sleep(Math.pow(2, attempt) * 1000);
        }
      }
    }
    throw lastError;
  }

  // ─── Generic HTTP Methods ────────────────────────────────────

  /**
   * Perform a generic GET request to an arbitrary API path.
   * @param path - API path (e.g. '/api/v1/me')
   * @param options - Optional request options
   */
  async get<T>(path: string, options?: RequestOptions): Promise<T> {
    return this.request<T>('GET', path, undefined, undefined, options);
  }

  /**
   * Perform a generic POST request to an arbitrary API path.
   * @param path - API path (e.g. '/api/v1/auth/login')
   * @param body - Request body
   * @param options - Optional request options
   */
  async post<T>(path: string, body?: unknown, options?: RequestOptions): Promise<T> {
    return this.request<T>('POST', path, body, undefined, options);
  }

  /**
   * Perform a generic PUT request to an arbitrary API path.
   * @param path - API path
   * @param body - Request body
   * @param options - Optional request options
   */
  async put<T>(path: string, body?: unknown, options?: RequestOptions): Promise<T> {
    return this.request<T>('PUT', path, body, undefined, options);
  }

  /**
   * Perform a generic DELETE request to an arbitrary API path.
   * @param path - API path
   * @param options - Optional request options
   */
  async delete<T>(path: string, options?: RequestOptions): Promise<T> {
    return this.request<T>('DELETE', path, undefined, undefined, options);
  }

  // ─── Topology (cached, 5s TTL) ─────────────────────────────

  /**
   * Fetch the complete topology graph.
   * Cached for 5 seconds.
   */
  async getTopology(options?: RequestOptions): Promise<Topology> {
    const cacheKey = 'topology:all';
    const cached = this.cache.get<Topology>(cacheKey);
    if (cached) return cached;

    const topology = await this.request<Topology>('GET', '/api/v1/topology', undefined, undefined, options);
    this.cache.set(cacheKey, topology, CACHE_TTL.topology);
    return topology;
  }

  /**
   * Fetch topology for a specific cluster.
   * @param clusterId - The cluster identifier
   */
  async getClusterTopology(clusterId: string, options?: RequestOptions): Promise<Topology> {
    const cacheKey = `topology:cluster:${clusterId}`;
    const cached = this.cache.get<Topology>(cacheKey);
    if (cached) return cached;

    const topology = await this.request<Topology>(
      'GET',
      `/api/v1/topology/cluster/${encodeURIComponent(clusterId)}`,
      undefined,
      undefined,
      options,
    );
    this.cache.set(cacheKey, topology, CACHE_TTL.topology);
    return topology;
  }

  /**
   * Search topology nodes by query string.
   * @param query - Search query
   */
  async searchNodes(query: string, options?: RequestOptions): Promise<TopologyNode[]> {
    return this.request<TopologyNode[]>(
      'GET',
      '/api/v1/topology/search',
      undefined,
      { q: query },
      options,
    );
  }

  // ─── Metrics ───────────────────────────────────────────────

  async queryMetrics(query: MetricQuery, options?: RequestOptions): Promise<MetricSeries[]> {
    return this.request<MetricSeries[]>('POST', '/api/v1/metrics/query', query, undefined, options);
  }

  /**
   * Fetch available metric names.
   * Cached for 30 seconds.
   */
  async getMetricNames(options?: RequestOptions): Promise<string[]> {
    const cacheKey = 'metrics:names';
    const cached = this.cache.get<string[]>(cacheKey);
    if (cached) return cached;

    const names = await this.request<string[]>('GET', '/api/v1/metrics/names', undefined, undefined, options);
    this.cache.set(cacheKey, names, CACHE_TTL.metricNames);
    return names;
  }

  /**
   * Fetch the latest raw metric batch for a single agent.
   * @param agentId - The agent identifier
   */
  async getAgentMetrics(agentId: string, options?: RequestOptions): Promise<AgentMetricBatch> {
    return this.request<AgentMetricBatch>(
      'GET',
      `/api/v1/metrics/${encodeURIComponent(agentId)}`,
      undefined,
      undefined,
      options,
    );
  }

  /**
   * Fetch pre-aggregated metrics for an agent over a rolling window.
   * @param agentId - The agent identifier
   * @param metricName - Optional metric name filter
   * @param window - Aggregation window (Go duration string, e.g. "1m"); defaults to "1m"
   */
  async getAggregatedMetrics(
    agentId: string,
    metricName?: string,
    window?: string,
    options?: RequestOptions,
  ): Promise<AgentAggregatedMetric[]> {
    const params: Record<string, string> = {};
    if (metricName) params['metric'] = metricName;
    if (window) params['window'] = window;
    return this.request<AgentAggregatedMetric[]>(
      'GET',
      `/api/v1/metrics/${encodeURIComponent(agentId)}/aggregated`,
      undefined,
      Object.keys(params).length > 0 ? params : undefined,
      options,
    );
  }

  // ─── Traces ────────────────────────────────────────────────

  async queryTraces(query: TraceQuery, options?: RequestOptions): Promise<Trace[]> {
    return this.request<Trace[]>('POST', '/api/v1/traces/query', query, undefined, options);
  }

  /**
   * List traces via GET with optional service/time filtering.
   * @param params - Optional service name and RFC3339 time range
   */
  async listTraces(
    params?: { service?: string; start?: string; end?: string },
    options?: RequestOptions,
  ): Promise<Trace[]> {
    const queryParams: Record<string, string> = {};
    if (params?.service) queryParams['service'] = params.service;
    if (params?.start) queryParams['start'] = params.start;
    if (params?.end) queryParams['end'] = params.end;
    return this.request<Trace[]>(
      'GET',
      '/api/v1/traces',
      undefined,
      Object.keys(queryParams).length > 0 ? queryParams : undefined,
      options,
    );
  }

  async getTrace(traceId: string, options?: RequestOptions): Promise<Trace> {
    return this.request<Trace>('GET', `/api/v1/traces/${encodeURIComponent(traceId)}`, undefined, undefined, options);
  }

  // ─── Events ────────────────────────────────────────────────

  async queryEvents(query: EventQuery, options?: RequestOptions): Promise<ParytyEvent[]> {
    return this.request<ParytyEvent[]>('POST', '/api/v1/events/query', query, undefined, options);
  }

  /** List events via GET. */
  async listEvents(options?: RequestOptions): Promise<ParytyEvent[]> {
    return this.request<ParytyEvent[]>('GET', '/api/v1/events', undefined, undefined, options);
  }

  // ─── Network Events ────────────────────────────────────────

  /**
   * Query captured network events (TCP/DNS/HTTP).
   * @param query - Optional agent, type, time range, and limit filters
   */
  async getNetworkEvents(
    query?: NetworkEventQuery,
    options?: RequestOptions,
  ): Promise<NetworkEvent[]> {
    const params: Record<string, string> = {};
    if (query?.agentId) params['agent_id'] = query.agentId;
    if (query?.type) params['type'] = query.type;
    if (query?.start) params['start'] = query.start;
    if (query?.end) params['end'] = query.end;
    if (query?.limit !== undefined) params['limit'] = String(query.limit);
    return this.request<NetworkEvent[]>(
      'GET',
      '/api/v1/network-events',
      undefined,
      Object.keys(params).length > 0 ? params : undefined,
      options,
    );
  }

  // ─── Agents ────────────────────────────────────────────────

  /** List all registered agents. */
  async listAgents(options?: RequestOptions): Promise<AgentInfo[]> {
    return this.request<AgentInfo[]>('GET', '/api/v1/agents', undefined, undefined, options);
  }

  /**
   * Fetch a single agent's registration metadata.
   * @param agentId - The agent identifier
   */
  async getAgent(agentId: string, options?: RequestOptions): Promise<AgentInfo> {
    return this.request<AgentInfo>(
      'GET',
      `/api/v1/agents/${encodeURIComponent(agentId)}`,
      undefined,
      undefined,
      options,
    );
  }

  /**
   * Fetch an agent's latest health report.
   * @param agentId - The agent identifier
   */
  async getAgentHealth(agentId: string, options?: RequestOptions): Promise<HealthReport> {
    return this.request<HealthReport>(
      'GET',
      `/api/v1/agents/${encodeURIComponent(agentId)}/health`,
      undefined,
      undefined,
      options,
    );
  }

  /**
   * Rename an agent.
   * @param agentId - The agent identifier
   * @param name - New display name
   */
  async updateAgentName(agentId: string, name: string, options?: RequestOptions): Promise<AgentManagementInfo> {
    // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
    const resp = await this.request<{ data: AgentManagementInfo }>(
      'PATCH',
      `/api/v1/agents/${encodeURIComponent(agentId)}`,
      { name },
      undefined,
      options,
    );
    return resp.data;
  }

  /**
   * Assign an agent to a digital twin.
   * @param twinId - The twin to assign to
   * @param agentId - The agent to assign
   */
  async assignAgentToTwin(twinId: string, agentId: string, options?: RequestOptions): Promise<{ accepted: boolean }> {
    // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
    const resp = await this.request<{ data: { accepted: boolean } }>(
      'POST',
      `/api/v1/twins/${encodeURIComponent(twinId)}/agents`,
      { agent_id: agentId },
      undefined,
      options,
    );
    return resp.data;
  }

  // ─── Dual Reality Agent Management ──────────────────────────

  /**
   * Pair an edge agent with a cluster agent.
   * @param agentId - The edge agent identifier
   * @param twinId - The cluster agent / twin identifier to pair with
   */
  async pairAgent(agentId: string, twinId: string, options?: RequestOptions): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/agents/${encodeURIComponent(agentId)}/pair`,
      { twin_id: twinId },
      undefined,
      options,
    );
  }

  /**
   * Unpair an edge agent from its cluster agent.
   * The edge agent becomes 'rogue' and the cluster agent becomes 'unconfigured'.
   * @param agentId - The edge agent identifier
   */
  async unpairAgent(agentId: string, options?: RequestOptions): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/agents/${encodeURIComponent(agentId)}/unpair`,
      undefined,
      undefined,
      options,
    );
  }

  /**
   * Retire an edge agent (graceful decommission).
   * @param agentId - The edge agent identifier
   */
  async retireAgent(agentId: string, options?: RequestOptions): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/agents/${encodeURIComponent(agentId)}/retire`,
      undefined,
      undefined,
      options,
    );
  }

  /**
   * Blacklist an edge agent.
   * @param agentId - The edge agent identifier
   * @param reason - Reason for blacklisting
   */
  async blacklistAgent(agentId: string, reason: string, options?: RequestOptions): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/agents/${encodeURIComponent(agentId)}/blacklist`,
      { reason },
      undefined,
      options,
    );
  }

  /**
   * Unregister an edge agent — completely erases client data.
   * @param agentId - The edge agent identifier
   */
  async unregisterAgent(agentId: string, options?: RequestOptions): Promise<void> {
    await this.request<void>(
      'POST',
      `/api/v1/agents/${encodeURIComponent(agentId)}/unregister`,
      undefined,
      undefined,
      options,
    );
  }

  /**
   * Get the pairing status of an edge agent.
   * @param agentId - The edge agent identifier
   */
  async getAgentPairingStatus(agentId: string, options?: RequestOptions): Promise<AgentPairingStatus> {
    // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
    const resp = await this.request<{ data: AgentPairingStatus }>(
      'GET',
      `/api/v1/agents/${encodeURIComponent(agentId)}/pairing-status`,
      undefined,
      undefined,
      options,
    );
    return resp.data;
  }

  // ─── Alerts ────────────────────────────────────────────────

  async getAlerts(options?: RequestOptions): Promise<Alert[]> {
    return this.request<Alert[]>('GET', '/api/v1/alerts', undefined, undefined, options);
  }

  async getAlertRules(options?: RequestOptions): Promise<AlertRule[]> {
    return this.request<AlertRule[]>('GET', '/api/v1/alerts/rules', undefined, undefined, options);
  }

  async acknowledgeAlert(alertId: string, options?: RequestOptions): Promise<void> {
    await this.request<void>('POST', `/api/v1/alerts/${encodeURIComponent(alertId)}/acknowledge`, undefined, undefined, options);
  }

  // ─── Timeline Snapshots ────────────────────────────────────

  /**
   * Fetch available timeline snapshots with optional time filtering.
   * @param params - Optional query parameters
   */
  async getSnapshots(params?: SnapshotParams, options?: RequestOptions): Promise<TimelineSnapshot[]> {
    const queryParams: Record<string, string> = {};
    if (params?.startTime) queryParams['start'] = params.startTime;
    if (params?.endTime) queryParams['end'] = params.endTime;
    if (params?.limit) queryParams['limit'] = String(params.limit);

    return this.request<TimelineSnapshot[]>(
      'GET',
      '/api/v1/timeline/snapshots',
      undefined,
      Object.keys(queryParams).length > 0 ? queryParams : undefined,
      options,
    );
  }

  /**
   * Fetch a single timeline snapshot by ID.
   * @param id - Snapshot identifier
   */
  async getSnapshot(id: string, options?: RequestOptions): Promise<TimelineSnapshot> {
    return this.request<TimelineSnapshot>(
      'GET',
      `/api/v1/timeline/snapshots/${encodeURIComponent(id)}`,
      undefined,
      undefined,
      options,
    );
  }

  /**
   * Compute the diff between two timeline snapshots.
   * @param fromId - Source snapshot ID
   * @param toId - Target snapshot ID
   */
  async getSnapshotDiff(fromId: string, toId: string, options?: RequestOptions): Promise<SnapshotDiff> {
    return this.request<SnapshotDiff>(
      'GET',
      '/api/v1/timeline/snapshots/diff',
      undefined,
      { from: fromId, to: toId },
      options,
    );
  }

  // ─── Intelligence ──────────────────────────────────────────

  /**
   * Fetch a single metric forecast.
   * Cached for 30 seconds.
   */
  async getForecast(query: ForecastQuery, options?: RequestOptions): Promise<ForecastSeries> {
    const cacheKey = `forecast:${query.metricName}:${query.horizonSeconds}`;
    const cached = this.cache.get<ForecastSeries>(cacheKey);
    if (cached) return cached;

    const series = await this.request<ForecastSeries>(
      'POST',
      '/api/v1/intel/forecast',
      query,
      undefined,
      options,
    );
    this.cache.set(cacheKey, series, CACHE_TTL.forecast);
    return series;
  }

  /**
   * Fetch forecasts for multiple metrics in a single batch request.
   * Cached per-metric for 30 seconds.
   */
  async getForecastBatch(
    queries: ForecastQuery[],
    options?: RequestOptions,
  ): Promise<ForecastSeries[]> {
    // Check cache for each query
    const uncached: ForecastQuery[] = [];
    const results: ForecastSeries[] = [];
    const cacheKeys: string[] = [];

    for (const q of queries) {
      const key = `forecast:${q.metricName}:${q.horizonSeconds}`;
      cacheKeys.push(key);
      const cached = this.cache.get<ForecastSeries>(key);
      if (cached) {
        results.push(cached);
      } else {
        uncached.push(q);
      }
    }

    if (uncached.length > 0) {
      const fresh = await this.request<ForecastSeries[]>(
        'POST',
        '/api/v1/intel/forecast/batch',
        { queries: uncached },
        undefined,
        options,
      );
      for (const s of fresh) {
        const key = `forecast:${s.metricName}:${queries[0]?.horizonSeconds ?? 604800}`;
        this.cache.set(key, s, CACHE_TTL.forecast);
        results.push(s);
      }
    }

    return results;
  }

  /**
   * Fetch model accuracy metrics.
   * Cached for 60 seconds.
   */
  async getModelAccuracy(
    metricName?: string,
    options?: RequestOptions,
  ): Promise<Record<string, ModelInfo>> {
    const cacheKey = `modelAccuracy:${metricName ?? 'all'}`;
    const cached = this.cache.get<Record<string, ModelInfo>>(cacheKey);
    if (cached) return cached;

    const params: Record<string, string> | undefined = metricName
      ? { metric: metricName }
      : undefined;
    const accuracy = await this.request<Record<string, ModelInfo>>(
      'GET',
      '/api/v1/intel/models/accuracy',
      undefined,
      params,
      options,
    );
    this.cache.set(cacheKey, accuracy, CACHE_TTL.modelAccuracy);
    return accuracy;
  }

  /**
   * Trigger model retraining.
   */
  async retrainModels(
    metricName?: string,
    force?: boolean,
    options?: RequestOptions,
  ): Promise<{ success: boolean; message: string }> {
    const result = await this.request<{ success: boolean; message: string }>(
      'POST',
      '/api/v1/intel/models/retrain',
      { metricName, force },
      undefined,
      options,
    );
    // Invalidate model accuracy cache on retrain
    this.cache.invalidate(`modelAccuracy:${metricName ?? 'all'}`);
    return result;
  }

  /**
   * Detect anomalies in a given time series.
   */
  async detectAnomalies(
    agentId: string,
    metricName: string,
    values: number[],
    timestamps: number[],
    options?: RequestOptions,
  ): Promise<{ anomalies: Anomaly[]; overallScore: number }> {
    return this.request<{ anomalies: Anomaly[]; overallScore: number }>(
      'POST',
      '/api/v1/intel/anomalies/detect',
      { agentId, metricName, values, timestamps },
      undefined,
      options,
    );
  }

  /**
   * Fetch anomaly detection subsystem status.
   */
  async getAnomalyStatus(options?: RequestOptions): Promise<AnomalyDetectionStatus> {
    const cacheKey = 'anomalyStatus';
    const cached = this.cache.get<AnomalyDetectionStatus>(cacheKey);
    if (cached) return cached;

    const status = await this.request<AnomalyDetectionStatus>(
      'GET',
      '/api/v1/intel/anomalies/status',
      undefined,
      undefined,
      options,
    );
    this.cache.set(cacheKey, status, 30000);
    return status;
  }

  /**
   * Get detailed explanation for a specific anomaly.
   */
  async explainAnomaly(
    agentId: string,
    metricName: string,
    timestamp: number,
    options?: RequestOptions,
  ): Promise<{ anomaly: Anomaly; similarIncidents: string[]; recommendations: string[] }> {
    return this.request<{
      anomaly: Anomaly;
      similarIncidents: string[];
      recommendations: string[];
    }>(
      'POST',
      '/api/v1/intel/anomalies/explain',
      { agentId, metricName, timestamp },
      undefined,
      options,
    );
  }

  // ─── Health ────────────────────────────────────────────────

  async getHealth(options?: RequestOptions): Promise<{ status: string; version: string }> {
    return this.request<{ status: string; version: string }>(
      'GET',
      '/api/v1/health',
      undefined,
      undefined,
      options,
    );
  }

  // ─── Cache Management ──────────────────────────────────────

  /**
   * Invalidate a specific cache entry.
   * @param key - Cache key to invalidate
   */
  invalidateCache(key: string): void {
    this.cache.invalidate(key);
  }

  /**
   * Clear the entire response cache.
   */
  clearCache(): void {
    this.cache.clear();
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
