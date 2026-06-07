/**
 * Pure metric processing algorithms (DOM-free, thread-agnostic).
 *
 * These functions are imported by BOTH the processing worker and the
 * main-thread synchronous fallback, so heavy metric work has exactly one
 * implementation regardless of where it runs. Keeping them free of any
 * `self`/`window`/`document` reference is what allows that dual use.
 *
 * @module engine/processing/metrics
 */

import type { MetricDataPoint } from '../../types/metric';

// ─── LTTB downsampling ──────────────────────────────────────────────────────

/** Minimum points below which LTTB is a no-op (needs first/last + ≥1 bucket). */
const LTTB_MIN_THRESHOLD = 3;

/**
 * Largest-Triangle-Three-Buckets downsampling.
 *
 * Reduces a dense time series to `threshold` points while preserving the
 * visual shape of the curve (peaks and troughs survive). Returns the input
 * unchanged when it is already at or below `threshold`, or when `threshold`
 * is too small to form buckets.
 *
 * @param data - Time-ordered points.
 * @param threshold - Target point count.
 * @returns The downsampled series (first and last points always retained).
 */
export function lttbDownsample(
  data: MetricDataPoint[],
  threshold: number,
): MetricDataPoint[] {
  if (data.length <= threshold || threshold < LTTB_MIN_THRESHOLD) {
    return data;
  }

  const sampled: MetricDataPoint[] = [data[0]];
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

    const prevTimestamp = new Date(data[prevIndex].timestamp).getTime();
    const prevValue = data[prevIndex].value;

    let maxArea = -1;
    let maxIndex = bucketStart;

    for (let j = bucketStart; j < bucketEnd; j++) {
      const curTimestamp = new Date(data[j].timestamp).getTime();
      const curValue = data[j].value;

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

  sampled.push(data[data.length - 1]);
  return sampled;
}

// ─── Aggregation ────────────────────────────────────────────────────────────

/** A single timestamped sample with its label set, prior to aggregation. */
export interface MetricSample {
  timestamp: number;
  value: number;
  labels: Record<string, string>;
}

/** Summary statistics for one label group. */
export interface AggregatedMetric {
  name: string;
  labels: Record<string, string>;
  count: number;
  sum: number;
  min: number;
  max: number;
  avg: number;
  p50: number;
  p90: number;
  p99: number;
}

/**
 * Returns the value at percentile `p` (0..1) from a pre-sorted ascending array.
 *
 * Uses the nearest-rank method, clamped to valid indices.
 */
function percentile(sorted: number[], p: number): number {
  const idx = Math.ceil(p * sorted.length) - 1;
  return sorted[Math.max(0, Math.min(idx, sorted.length - 1))];
}

/**
 * Groups samples by their label set and computes summary statistics per group.
 *
 * @param samples - Raw samples to aggregate.
 * @returns One {@link AggregatedMetric} per distinct label set.
 */
export function aggregateMetrics(samples: MetricSample[]): AggregatedMetric[] {
  if (samples.length === 0) return [];

  const groups = new Map<string, MetricSample[]>();
  for (const sample of samples) {
    const key = JSON.stringify(sample.labels);
    const group = groups.get(key) ?? [];
    group.push(sample);
    groups.set(key, group);
  }

  const results: AggregatedMetric[] = [];
  for (const [key, group] of groups) {
    const values = group.map((m) => m.value).sort((a, b) => a - b);
    const sum = values.reduce((a, b) => a + b, 0);
    const count = values.length;

    results.push({
      name: '',
      labels: JSON.parse(key) as Record<string, string>,
      count,
      sum,
      min: values[0],
      max: values[count - 1],
      avg: sum / count,
      p50: percentile(values, 0.5),
      p90: percentile(values, 0.9),
      p99: percentile(values, 0.99),
    });
  }

  return results;
}

// ─── Parsing ────────────────────────────────────────────────────────────────

/** Wire format accepted by {@link parseMetrics}. */
export type MetricFormat = 'json' | 'line';

/**
 * Parses raw metric text into samples.
 *
 * - `json`: expects a JSON array of {@link MetricSample}; returns `[]` on
 *   malformed input.
 * - `line`: InfluxDB line protocol; skips comment (`#`) and malformed lines,
 *   parsing `measurement,tag=v value timestamp` into a sample whose labels
 *   include the measurement name under `__name__`.
 *
 * @param raw - Raw metric payload.
 * @param format - Wire format of `raw`.
 * @returns Parsed samples (best-effort; never throws).
 */
export function parseMetrics(raw: string, format: MetricFormat): MetricSample[] {
  if (format === 'json') {
    try {
      return JSON.parse(raw) as MetricSample[];
    } catch {
      return [];
    }
  }

  const lines = raw.split('\n').filter((l) => l.trim() && !l.startsWith('#'));
  const metrics: MetricSample[] = [];

  for (const line of lines) {
    try {
      const parts = line.split(' ');
      if (parts.length < 2) continue;
      const [measurement, valueStr, timestampStr] = parts;
      const [name, labelStr] = measurement.split(',');
      const labels: Record<string, string> = {};
      if (labelStr) {
        for (const pair of labelStr.split(',')) {
          const [k, v] = pair.split('=');
          if (k && v) labels[k] = v;
        }
      }
      metrics.push({
        timestamp: parseInt(timestampStr, 10) || Date.now(),
        value: parseFloat(valueStr) || 0,
        labels: { ...labels, __name__: name },
      });
    } catch {
      // Skip malformed lines.
    }
  }

  return metrics;
}
