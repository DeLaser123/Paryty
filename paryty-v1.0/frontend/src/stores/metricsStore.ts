import { create } from 'zustand';
import type { MetricSeries, AggregatedMetric, MetricDataPoint } from '../types/metric';
import type { TimeRange } from '../types/common';
import { RingBuffer } from '../engine/ringBuffer';

// ─── Downsampling Resolution ───────────────────────────────────

export type DownsampleResolution = 'raw' | '1m' | '5m' | '1h' | '1d';

// ─── LTTB Downsampling ─────────────────────────────────────────

/**
 * Largest-Triangle-Three-Buckets (LTTB) downsampling algorithm.
 * Preserves visual shape of time series while reducing point count.
 *
 * @param data - Sorted array of data points (must have at least 3 points)
 * @param threshold - Target number of output points
 * @returns Downsampled array of data points
 */
export function lttbDownsample(
  data: MetricDataPoint[],
  threshold: number,
): MetricDataPoint[] {
  if (data.length <= threshold || threshold < 3) {
    return data;
  }

  const sampled: MetricDataPoint[] = [data[0]]; // Always keep first point
  const bucketSize = (data.length - 2) / (threshold - 2);

  let prevIndex = 0;

  for (let i = 1; i < threshold - 1; i++) {
    const bucketStart = Math.floor((i - 1) * bucketSize) + 1;
    const bucketEnd = Math.min(Math.floor(i * bucketSize) + 1, data.length - 1);
    const nextBucketStart = Math.floor(i * bucketSize) + 1;
    const nextBucketEnd = Math.min(
      Math.floor((i + 1) * bucketSize) + 1,
      data.length - 1,
    );

    // Calculate average of next bucket (for the triangle's third point)
    let avgTimestamp = 0;
    let avgValue = 0;
    const nextBucketLen = nextBucketEnd - nextBucketStart;
    for (let j = nextBucketStart; j < nextBucketEnd; j++) {
      avgTimestamp += new Date(data[j].timestamp).getTime();
      avgValue += data[j].value;
    }
    if (nextBucketLen > 0) {
      avgTimestamp /= nextBucketLen;
      avgValue /= nextBucketLen;
    }

    // Find the point in current bucket that forms the largest triangle
    // with the previous selected point and the average of the next bucket
    const prevTimestamp = new Date(data[prevIndex].timestamp).getTime();
    const prevValue = data[prevIndex].value;

    let maxArea = -1;
    let maxIndex = bucketStart;

    for (let j = bucketStart; j < bucketEnd; j++) {
      const curTimestamp = new Date(data[j].timestamp).getTime();
      const curValue = data[j].value;

      // Triangle area using cross product
      const area = Math.abs(
        (prevTimestamp - avgTimestamp) * (curValue - prevValue) -
        (prevTimestamp - curTimestamp) * (avgValue - prevValue),
      );

      if (area > maxArea) {
        maxArea = area;
        maxIndex = j;
      }
    }

    sampled.push(data[maxIndex]);
    prevIndex = maxIndex;
  }

  sampled.push(data[data.length - 1]); // Always keep last point
  return sampled;
}

// ─── Derived Metric Helpers ────────────────────────────────────

/**
 * Calculate the per-second rate of change for a metric series.
 * Returns a new series with rate values. First point has rate 0.
 */
export function rate(series: MetricSeries): MetricSeries {
  const points = series.points;
  if (points.length < 2) return { ...series, points: [] };

  const ratePoints: MetricDataPoint[] = [];
  for (let i = 1; i < points.length; i++) {
    const dt =
      (new Date(points[i].timestamp).getTime() -
        new Date(points[i - 1].timestamp).getTime()) /
      1000;
    if (dt > 0) {
      ratePoints.push({
        timestamp: points[i].timestamp,
        value: (points[i].value - points[i - 1].value) / dt,
      });
    }
  }

  return {
    name: `rate(${series.name})`,
    labels: series.labels,
    points: ratePoints,
  };
}

/**
 * Calculate the increase (delta) over a time window for a metric series.
 * Returns a new series with cumulative increase values.
 */
export function increase(series: MetricSeries): MetricSeries {
  const points = series.points;
  if (points.length < 2) return { ...series, points: [] };

  const increasePoints: MetricDataPoint[] = [];
  for (let i = 1; i < points.length; i++) {
    const delta = points[i].value - points[i - 1].value;
    increasePoints.push({
      timestamp: points[i].timestamp,
      value: delta >= 0 ? delta : 0, // Counter resets → 0
    });
  }

  return {
    name: `increase(${series.name})`,
    labels: series.labels,
    points: increasePoints,
  };
}

// ─── Max Points ────────────────────────────────────────────────

const MAX_POINTS = 10000;

/** Maximum number of distinct metric names stored (evicts oldest by lastUpdated). */
const MAX_SERIES_NAMES = 50;

// ─── Store Interface ───────────────────────────────────────────

interface MetricsState {
  // Data
  series: MetricSeries[];
  aggregated: AggregatedMetric[];
  selectedMetric: string | null;
  timeRange: TimeRange;
  refreshInterval: number; // ms
  isLoading: boolean;
  error: string | null;

  /** Tracks last-updated timestamp per metric name for eviction ordering. */
  seriesLastUpdated: Record<string, number>;

  // Streaming
  isStreaming: boolean;
  streamingAgentId: string | null;
  startStream: (agentId: string) => void;
  stopStream: () => void;

  // Downsampling
  autoDownsample: boolean;
  resolution: DownsampleResolution;
  setResolution: (res: DownsampleResolution) => void;
  setAutoDownsample: (enabled: boolean) => void;

  // Comparison
  comparedMetrics: string[];
  compareMode: boolean;
  toggleCompare: (metricName: string) => void;
  clearComparison: () => void;

  // Actions
  setSeries: (series: MetricSeries[]) => void;
  appendSeries: (series: MetricSeries[]) => void;
  setAggregated: (metrics: AggregatedMetric[]) => void;
  selectMetric: (name: string | null) => void;
  setTimeRange: (range: TimeRange) => void;
  setRefreshInterval: (interval: number) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  clear: () => void;

  // Computed
  selectedSeries: () => MetricSeries[];
  comparedSeries: () => MetricSeries[];
}

const DEFAULT_TIME_RANGE: TimeRange = {
  start: new Date(Date.now() - 3600000).toISOString(),
  end: new Date().toISOString(),
};

export const useMetricsStore = create<MetricsState>()((set, get) => ({
  series: [],
  aggregated: [],
  selectedMetric: null,
  timeRange: DEFAULT_TIME_RANGE,
  refreshInterval: 10000,
  isLoading: false,
  error: null,
  seriesLastUpdated: {},

  // Streaming defaults
  isStreaming: false,
  streamingAgentId: null,
  startStream: (agentId) =>
    set({ isStreaming: true, streamingAgentId: agentId }),
  stopStream: () =>
    set({ isStreaming: false, streamingAgentId: null }),

  // Downsampling defaults
  autoDownsample: true,
  resolution: 'raw' as DownsampleResolution,
  setResolution: (resolution) => set({ resolution }),
  setAutoDownsample: (autoDownsample) => set({ autoDownsample }),

  // Comparison defaults
  comparedMetrics: [],
  compareMode: false,
  toggleCompare: (metricName) =>
    set((state) => {
      const idx = state.comparedMetrics.indexOf(metricName);
      if (idx === -1) {
        return {
          comparedMetrics: [...state.comparedMetrics, metricName],
          compareMode: true,
        };
      }
      const next = state.comparedMetrics.filter((n) => n !== metricName);
      return {
        comparedMetrics: next,
        compareMode: next.length > 0,
      };
    }),
  clearComparison: () => set({ comparedMetrics: [], compareMode: false }),

  // Existing actions (preserved, with enhanced point cap + LTTB)
  setSeries: (series) => set({ series, error: null }),

  appendSeries: (newSeries) =>
    set((state) => {
      const now = Date.now();
      const merged = new Map(state.series.map((s) => [s.name, s]));
      const lastUpdated = { ...state.seriesLastUpdated };

      for (const s of newSeries) {
        lastUpdated[s.name] = now;
        const existing = merged.get(s.name);
        if (existing) {
          // Use RingBuffer for zero-alloc point merging
          const ring = new RingBuffer<MetricDataPoint>(MAX_POINTS);
          ring.pushMany(existing.points);
          ring.pushMany(s.points);
          merged.set(s.name, { ...s, points: ring.toArray() });
        } else {
          const points =
            s.points.length > MAX_POINTS
              ? lttbDownsample(s.points, MAX_POINTS)
              : s.points;
          merged.set(s.name, { ...s, points });
        }
      }

      let seriesList = Array.from(merged.values());

      // Evict oldest series when over the name cap
      if (seriesList.length > MAX_SERIES_NAMES) {
        const sorted = seriesList
          .map((s) => ({ name: s.name, ts: lastUpdated[s.name] ?? 0 }))
          .sort((a, b) => a.ts - b.ts);
        const evictCount = seriesList.length - MAX_SERIES_NAMES;
        const evictNames = new Set(sorted.slice(0, evictCount).map((e) => e.name));
        for (const name of evictNames) {
          delete lastUpdated[name];
        }
        seriesList = seriesList.filter((s) => !evictNames.has(s.name));
      }

      return { series: seriesList, seriesLastUpdated: lastUpdated };
    }),

  setAggregated: (aggregated) => set({ aggregated }),

  selectMetric: (name) => set({ selectedMetric: name }),

  setTimeRange: (range) => set({ timeRange: range }),

  setRefreshInterval: (interval) => set({ refreshInterval: interval }),

  setLoading: (loading) => set({ isLoading: loading }),

  setError: (error) => set({ error }),

  clear: () =>
    set({
      series: [],
      aggregated: [],
      error: null,
      comparedMetrics: [],
      compareMode: false,
      seriesLastUpdated: {},
    }),

  selectedSeries: () => {
    const { series, selectedMetric, autoDownsample, resolution } = get();
    let result: MetricSeries[];
    if (!selectedMetric) {
      result = series;
    } else {
      result = series.filter((s) => s.name === selectedMetric);
    }
    // Apply auto-downsampling if enabled and resolution is not raw
    if (autoDownsample && resolution !== 'raw') {
      result = result.map((s) => ({
        ...s,
        points: lttbDownsample(s.points, getTargetPoints(resolution)),
      }));
    }
    return result;
  },

  comparedSeries: () => {
    const { series, comparedMetrics } = get();
    if (comparedMetrics.length === 0) return [];
    return series.filter((s) => comparedMetrics.includes(s.name));
  },
}));

// ─── Resolution → Target Points ────────────────────────────────

function getTargetPoints(res: DownsampleResolution): number {
  switch (res) {
    case '1m':
      return 500;
    case '5m':
      return 200;
    case '1h':
      return 100;
    case '1d':
      return 50;
    default:
      return MAX_POINTS;
  }
}
