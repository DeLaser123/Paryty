import { describe, it, expect } from 'vitest';
import {
  lttbDownsample,
  aggregateMetrics,
  parseMetrics,
  type MetricSample,
} from '../../engine/processing/metrics';
import {
  computeLayout,
  computeClusteredLayout,
  computePositionUpdate,
  type LayoutInput,
} from '../../engine/processing/layout';
import {
  isProcessingRequest,
  isProcessingResponse,
} from '../../engine/processing/protocol';
import type { MetricDataPoint } from '../../types/metric';

// ─── LTTB downsampling ──────────────────────────────────────────────────────

describe('lttbDownsample', () => {
  const series = (n: number): MetricDataPoint[] =>
    Array.from({ length: n }, (_, i) => ({
      timestamp: new Date(i * 1000).toISOString(),
      value: Math.sin(i / 5) * 100,
    }));

  it('returns the input unchanged when already at or below threshold', () => {
    const data = series(10);
    expect(lttbDownsample(data, 10)).toBe(data);
    expect(lttbDownsample(data, 50)).toBe(data);
  });

  it('returns the input unchanged when threshold is too small for buckets', () => {
    const data = series(100);
    expect(lttbDownsample(data, 2)).toBe(data);
  });

  it('downsamples to the requested point count', () => {
    const data = series(1000);
    const result = lttbDownsample(data, 100);
    expect(result.length).toBe(100);
  });

  it('always retains the first and last points', () => {
    const data = series(500);
    const result = lttbDownsample(data, 50);
    expect(result[0]).toBe(data[0]);
    expect(result[result.length - 1]).toBe(data[data.length - 1]);
  });
});

// ─── Aggregation ────────────────────────────────────────────────────────────

describe('aggregateMetrics', () => {
  it('returns an empty array for no samples', () => {
    expect(aggregateMetrics([])).toEqual([]);
  });

  it('groups samples by their label set', () => {
    const samples: MetricSample[] = [
      { timestamp: 0, value: 1, labels: { host: 'a' } },
      { timestamp: 0, value: 3, labels: { host: 'a' } },
      { timestamp: 0, value: 9, labels: { host: 'b' } },
    ];
    const result = aggregateMetrics(samples);
    expect(result.length).toBe(2);

    const a = result.find((r) => r.labels.host === 'a');
    expect(a).toBeDefined();
    expect(a!.count).toBe(2);
    expect(a!.sum).toBe(4);
    expect(a!.min).toBe(1);
    expect(a!.max).toBe(3);
    expect(a!.avg).toBe(2);
  });

  it('computes nearest-rank percentiles over sorted values', () => {
    const samples: MetricSample[] = [10, 20, 30, 40, 50].map((value) => ({
      timestamp: 0,
      value,
      labels: {},
    }));
    const [agg] = aggregateMetrics(samples);
    expect(agg.p50).toBe(30);
    expect(agg.p90).toBe(50);
    expect(agg.p99).toBe(50);
  });
});

// ─── Parsing ────────────────────────────────────────────────────────────────

describe('parseMetrics', () => {
  it('parses a valid JSON array of samples', () => {
    const samples: MetricSample[] = [{ timestamp: 1, value: 2, labels: { a: 'b' } }];
    expect(parseMetrics(JSON.stringify(samples), 'json')).toEqual(samples);
  });

  it('returns an empty array for malformed JSON', () => {
    expect(parseMetrics('{not json', 'json')).toEqual([]);
  });

  it('parses line-protocol lines into samples with the measurement name', () => {
    const raw = 'cpu,host=a 42 1700000000';
    const [sample] = parseMetrics(raw, 'line');
    expect(sample.value).toBe(42);
    expect(sample.timestamp).toBe(1700000000);
    expect(sample.labels.__name__).toBe('cpu');
    expect(sample.labels.host).toBe('a');
  });

  it('skips comment and malformed lines', () => {
    const raw = ['# a comment', 'incomplete', 'mem 7 1700000001'].join('\n');
    const result = parseMetrics(raw, 'line');
    expect(result.length).toBe(1);
    expect(result[0].labels.__name__).toBe('mem');
    expect(result[0].value).toBe(7);
  });
});

// ─── Layout ─────────────────────────────────────────────────────────────────

describe('computeLayout', () => {
  const input = (count: number): LayoutInput => ({
    nodes: Array.from({ length: count }, (_, i) => ({
      id: `n${i}`,
      type: 'service',
      status: 'healthy',
    })),
    edges: Array.from({ length: count - 1 }, (_, i) => ({
      source: `n${i}`,
      target: `n${i + 1}`,
      type: 'depends',
    })),
    width: 800,
    height: 600,
    iterations: 30,
  });

  it('returns a position for every input node', () => {
    const result = computeLayout(input(20));
    expect(result.nodes.length).toBe(20);
    for (const node of result.nodes) {
      expect(Number.isFinite(node.x)).toBe(true);
      expect(Number.isFinite(node.y)).toBe(true);
    }
  });

  it('preserves node type and status through layout', () => {
    const result = computeLayout(input(5));
    expect(result.nodes.every((n) => n.type === 'service')).toBe(true);
    expect(result.nodes.every((n) => n.status === 'healthy')).toBe(true);
  });

  it('is deterministic when initial positions are provided', () => {
    // Layout seeds missing coordinates with Math.random(); supplying explicit
    // positions makes the pure force math fully reproducible.
    const seeded = (): LayoutInput => ({
      nodes: Array.from({ length: 8 }, (_, i) => ({
        id: `n${i}`,
        type: 'service',
        status: 'healthy',
        x: 100 + i * 40,
        y: 100 + i * 30,
      })),
      edges: Array.from({ length: 7 }, (_, i) => ({
        source: `n${i}`,
        target: `n${i + 1}`,
        type: 'depends',
      })),
      width: 800,
      height: 600,
      iterations: 30,
    });
    const a = computeLayout(seeded());
    const b = computeLayout(seeded());
    expect(a.nodes).toEqual(b.nodes);
  });
});

describe('computeClusteredLayout', () => {
  it('produces positions and clusters grouped by type', () => {
    const result = computeClusteredLayout({
      nodes: [
        { id: 'a', type: 'service', status: 'healthy' },
        { id: 'b', type: 'service', status: 'healthy' },
        { id: 'c', type: 'database', status: 'healthy' },
      ],
      edges: [],
      config: { groupBy: 'type', width: 800, height: 600 },
    });
    expect(result.nodes.length).toBe(3);
    expect(result.clusters).toBeDefined();
    expect(result.clusters!.length).toBe(2);
  });
});

describe('computePositionUpdate', () => {
  it('moves dragged nodes toward their targets', () => {
    const result = computePositionUpdate({
      currentPositions: [
        { id: 'a', x: 0, y: 0 },
        { id: 'b', x: 100, y: 100 },
      ],
      movedNodes: [{ id: 'a', targetX: 50, targetY: 50 }],
      iterations: 5,
    });
    const a = result.nodes.find((n) => n.id === 'a');
    expect(a).toBeDefined();
    expect(a!.x).toBeCloseTo(50);
    expect(a!.y).toBeCloseTo(50);
  });
});

// ─── Protocol type guards ───────────────────────────────────────────────────

describe('processing protocol guards', () => {
  it('accepts valid requests', () => {
    expect(
      isProcessingRequest({ kind: 'aggregate', id: '1', samples: [] }),
    ).toBe(true);
    expect(
      isProcessingRequest({ kind: 'layout', id: '2', input: {} }),
    ).toBe(true);
  });

  it('rejects non-requests', () => {
    expect(isProcessingRequest(null)).toBe(false);
    expect(isProcessingRequest({ kind: 'nope' })).toBe(false);
    expect(isProcessingRequest({})).toBe(false);
  });

  it('accepts valid responses with a string id', () => {
    expect(isProcessingResponse({ kind: 'aggregate', id: '1', result: [] })).toBe(
      true,
    );
    expect(isProcessingResponse({ kind: 'error', id: '1', message: 'x' })).toBe(
      true,
    );
  });

  it('rejects responses missing a string id', () => {
    expect(isProcessingResponse({ kind: 'aggregate', result: [] })).toBe(false);
    expect(isProcessingResponse({ kind: 'aggregate', id: 5 })).toBe(false);
    expect(isProcessingResponse(undefined)).toBe(false);
  });
});
