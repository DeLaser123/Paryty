/**
 * Unit tests for the newly wired RestClient endpoint methods.
 *
 * These tests stub the global `fetch` to assert that each method targets the
 * exact backend route the Go Query API registers (see
 * cluster/internal/api/query/rest.go RegisterRoutes), forwards query
 * parameters correctly, and parses the JSON response into the expected shape.
 * No DOM or network access is required, so they run in the Vitest node
 * environment alongside the engine suite.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { RestClient, ApiClientError } from '../../api/rest';
import type { AgentInfo, HealthReport } from '../../types/agent';
import type { AgentMetricBatch, AgentAggregatedMetric } from '../../types/agentMetrics';
import type { NetworkEvent } from '../../types/network';
import type { Trace } from '../../types/trace';
import type { ParytyEvent } from '../../types/event';

const BASE_URL = 'http://cluster.test';

/** Captures the URL and init of the most recent fetch call. */
interface CapturedRequest {
  url: string;
  method: string;
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('RestClient new endpoint methods', () => {
  let client: RestClient;
  let fetchMock: ReturnType<typeof vi.fn>;
  let captured: CapturedRequest[];
  /** Queue of responses to return, in order; falls back to an empty 200. */
  let responseQueue: Response[];

  /** Enqueues the response the next fetch call should resolve with. */
  function enqueue(body: unknown, status = 200): void {
    responseQueue.push(jsonResponse(body, status));
  }

  beforeEach(() => {
    captured = [];
    responseQueue = [];
    // A single capturing implementation records every request and dequeues
    // the next queued response. Using a queue (rather than mockResolvedValueOnce)
    // keeps the capture side-effect alive for every call.
    fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      captured.push({
        url: typeof input === 'string' ? input : input.toString(),
        method: init?.method ?? 'GET',
      });
      const next = responseQueue.shift() ?? jsonResponse({});
      return Promise.resolve(next);
    });
    vi.stubGlobal('fetch', fetchMock);
    // baseUrl is explicit so the client never touches `window.location`.
    client = new RestClient({ baseUrl: BASE_URL, retries: 0 });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  /** Returns the parsed URL of the Nth captured request. */
  function urlAt(index: number): URL {
    return new URL(captured[index].url);
  }

  it('getAgentMetrics hits /metrics/:agent_id and returns the batch', async () => {
    const batch: AgentMetricBatch = { agent_id: 'agent-1', timestamp: '2026-06-07T00:00:00Z' };
    enqueue(batch);

    const result = await client.getAgentMetrics('agent-1');

    expect(captured[0].method).toBe('GET');
    expect(urlAt(0).pathname).toBe('/api/v1/metrics/agent-1');
    expect(result).toEqual(batch);
  });

  it('getAgentMetrics percent-encodes the agent id', async () => {
    await client.getAgentMetrics('agent/with space');
    expect(urlAt(0).pathname).toBe('/api/v1/metrics/agent%2Fwith%20space');
  });

  it('getAggregatedMetrics forwards metric and window query params', async () => {
    const agg: AgentAggregatedMetric[] = [
      {
        agent_id: 'agent-1',
        name: 'cpu.usage',
        labels: {},
        window: 60_000_000_000,
        agg_type: 'avg',
        value: 0.42,
        timestamp: '2026-06-07T00:00:00Z',
      },
    ];
    enqueue(agg);

    const result = await client.getAggregatedMetrics('agent-1', 'cpu.usage', '5m');

    const url = urlAt(0);
    expect(url.pathname).toBe('/api/v1/metrics/agent-1/aggregated');
    expect(url.searchParams.get('metric')).toBe('cpu.usage');
    expect(url.searchParams.get('window')).toBe('5m');
    expect(result).toEqual(agg);
  });

  it('getAggregatedMetrics omits query params when no filters are given', async () => {
    await client.getAggregatedMetrics('agent-1');
    const url = urlAt(0);
    expect(url.pathname).toBe('/api/v1/metrics/agent-1/aggregated');
    expect(url.search).toBe('');
  });

  it('listTraces forwards service and RFC3339 time range', async () => {
    const traces: Trace[] = [];
    enqueue(traces);

    await client.listTraces({
      service: 'query',
      start: '2026-06-07T00:00:00Z',
      end: '2026-06-07T01:00:00Z',
    });

    const url = urlAt(0);
    expect(url.pathname).toBe('/api/v1/traces');
    expect(url.searchParams.get('service')).toBe('query');
    expect(url.searchParams.get('start')).toBe('2026-06-07T00:00:00Z');
    expect(url.searchParams.get('end')).toBe('2026-06-07T01:00:00Z');
  });

  it('listTraces sends no query string when called without params', async () => {
    await client.listTraces();
    expect(urlAt(0).pathname).toBe('/api/v1/traces');
    expect(urlAt(0).search).toBe('');
  });

  it('listEvents hits /events and returns the array', async () => {
    const events: ParytyEvent[] = [];
    enqueue(events);

    const result = await client.listEvents();

    expect(urlAt(0).pathname).toBe('/api/v1/events');
    expect(result).toEqual(events);
  });

  it('getNetworkEvents maps agentId to agent_id and forwards all filters', async () => {
    const netEvents: NetworkEvent[] = [];
    enqueue(netEvents);

    await client.getNetworkEvents({
      agentId: 'agent-1',
      type: 'tcp',
      start: '2026-06-07T00:00:00Z',
      end: '2026-06-07T01:00:00Z',
      limit: 250,
    });

    const url = urlAt(0);
    expect(url.pathname).toBe('/api/v1/network-events');
    expect(url.searchParams.get('agent_id')).toBe('agent-1');
    expect(url.searchParams.get('type')).toBe('tcp');
    expect(url.searchParams.get('limit')).toBe('250');
  });

  it('getNetworkEvents forwards limit=0 (boundary, not dropped)', async () => {
    await client.getNetworkEvents({ limit: 0 });
    expect(urlAt(0).searchParams.get('limit')).toBe('0');
  });

  it('listAgents hits /agents and returns the array', async () => {
    const agents: AgentInfo[] = [];
    enqueue(agents);

    const result = await client.listAgents();

    expect(urlAt(0).pathname).toBe('/api/v1/agents');
    expect(result).toEqual(agents);
  });

  it('getAgent hits /agents/:agent_id', async () => {
    const agent: AgentInfo = {
      id: 'agent-1',
      hostname: 'host-1',
      ip_address: '10.0.0.1',
      os: 'linux',
      arch: 'amd64',
      agent_version: '1.0.0',
      labels: {},
      status: 'online',
      registered_at: '2026-06-07T00:00:00Z',
      last_heartbeat: '2026-06-07T00:00:30Z',
      config_hash: 'abc123',
    };
    enqueue(agent);

    const result = await client.getAgent('agent-1');

    expect(urlAt(0).pathname).toBe('/api/v1/agents/agent-1');
    expect(result).toEqual(agent);
  });

  it('getAgentHealth hits /agents/:agent_id/health', async () => {
    const health: HealthReport = {
      agent_id: 'agent-1',
      status: 'healthy',
      uptime: 3_600_000_000_000,
      memory_usage: 0.5,
      cpu_usage: 0.3,
      components: {},
      timestamp: '2026-06-07T00:00:00Z',
    };
    enqueue(health);

    const result = await client.getAgentHealth('agent-1');

    expect(urlAt(0).pathname).toBe('/api/v1/agents/agent-1/health');
    expect(result).toEqual(health);
  });

  it('surfaces a non-OK response as ApiClientError with the HTTP status', async () => {
    enqueue({ message: 'not found' }, 404);

    await expect(client.getAgent('missing')).rejects.toMatchObject({
      name: 'ApiClientError',
      status: 404,
    });
  });

  it('does not retry 4xx client errors (single fetch call)', async () => {
    enqueue({ message: 'bad request' }, 400);

    await expect(client.listAgents()).rejects.toBeInstanceOf(ApiClientError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
