/**
 * Processing Client — main-thread facade for the single processing worker.
 *
 * Owns one {@link Worker} instance (the heavy-compute thread) and exposes a
 * promise-based API for every CPU-bound task. Requests are correlated to
 * responses by a monotonic id. When Web Workers are unavailable (or fail to
 * construct), every method transparently falls back to the same pure algorithm
 * modules executed synchronously, so callers never branch on capability.
 *
 * A module-level singleton ({@link getProcessingClient}) ensures the whole app
 * shares exactly one processing thread.
 *
 * @module engine/processing/processingClient
 */

import {
  computeLayout,
  computeClusteredLayout,
  computePositionUpdate,
  type LayoutInput,
  type ClusteredLayoutInput,
  type PositionUpdateInput,
  type LayoutResult,
} from './layout';
import {
  lttbDownsample,
  aggregateMetrics,
  parseMetrics,
  type MetricSample,
  type AggregatedMetric,
  type MetricFormat,
} from './metrics';
import {
  isProcessingResponse,
  type ProcessingRequest,
  type ProcessingResponse,
} from './protocol';
import type { MetricDataPoint } from '../../types/metric';

/** Resolver/rejecter pair for an in-flight request. */
interface Pending {
  resolve: (value: ProcessingResponse) => void;
  reject: (err: Error) => void;
}

/**
 * Promise-based client over the single processing worker, with a synchronous
 * pure-module fallback when workers are unavailable.
 */
export class ProcessingClient {
  private worker: Worker | null = null;
  private workerFailed = false;
  private nextId = 1;
  private readonly pending = new Map<string, Pending>();

  /**
   * Lazily constructs the worker. Returns false (and latches failure) when
   * workers are unsupported or construction throws, signalling sync fallback.
   */
  private ensureWorker(): boolean {
    if (this.worker) return true;
    if (this.workerFailed || typeof Worker === 'undefined') return false;

    try {
      this.worker = new Worker(new URL('./processingWorker.ts', import.meta.url), {
        type: 'module',
      });
      this.worker.onmessage = (event: MessageEvent<unknown>) => {
        if (isProcessingResponse(event.data)) this.settle(event.data);
      };
      this.worker.onerror = () => {
        // Reject everything in flight; future calls use the sync fallback.
        this.workerFailed = true;
        for (const [, p] of this.pending) {
          p.reject(new Error('processing worker failed'));
        }
        this.pending.clear();
        this.worker?.terminate();
        this.worker = null;
      };
      return true;
    } catch {
      this.workerFailed = true;
      return false;
    }
  }

  /** Resolves the pending promise that matches a worker response id. */
  private settle(response: ProcessingResponse): void {
    const pending = this.pending.get(response.id);
    if (!pending) return;
    this.pending.delete(response.id);
    if (response.kind === 'error') {
      pending.reject(new Error(response.message));
    } else {
      pending.resolve(response);
    }
  }

  /** Posts a request and returns a promise for its correlated response. */
  private send(build: (id: string) => ProcessingRequest): Promise<ProcessingResponse> {
    const id = String(this.nextId++);
    return new Promise<ProcessingResponse>((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      // Non-null: ensureWorker() returned true before this is called.
      this.worker!.postMessage(build(id));
    });
  }

  // ─── Layout ───────────────────────────────────────────────────────────────

  /** Runs a full force-directed layout (worker when available). */
  async layout(input: LayoutInput): Promise<LayoutResult> {
    if (!this.ensureWorker()) return computeLayout(input);
    const res = await this.send((id) => ({ kind: 'layout', id, input }));
    return (res as Extract<ProcessingResponse, { kind: 'layout' }>).result;
  }

  /** Runs a hierarchical (grouped) layout (worker when available). */
  async clusteredLayout(input: ClusteredLayoutInput): Promise<LayoutResult> {
    if (!this.ensureWorker()) return computeClusteredLayout(input);
    const res = await this.send((id) => ({ kind: 'clusteredLayout', id, input }));
    return (res as Extract<ProcessingResponse, { kind: 'layout' }>).result;
  }

  /** Settles non-moved nodes around dragged nodes (worker when available). */
  async positionUpdate(input: PositionUpdateInput): Promise<LayoutResult> {
    if (!this.ensureWorker()) return computePositionUpdate(input);
    const res = await this.send((id) => ({ kind: 'positionUpdate', id, input }));
    return (res as Extract<ProcessingResponse, { kind: 'layout' }>).result;
  }

  // ─── Metrics ──────────────────────────────────────────────────────────────

  /** LTTB-downsamples a time series (worker when available). */
  async downsample(
    data: readonly MetricDataPoint[],
    threshold: number,
  ): Promise<MetricDataPoint[]> {
    if (!this.ensureWorker()) return lttbDownsample([...data], threshold);
    const res = await this.send((id) => ({ kind: 'downsample', id, data, threshold }));
    return (res as Extract<ProcessingResponse, { kind: 'downsample' }>).data;
  }

  /** Aggregates samples into per-label summary statistics. */
  async aggregate(samples: readonly MetricSample[]): Promise<AggregatedMetric[]> {
    if (!this.ensureWorker()) return aggregateMetrics([...samples]);
    const res = await this.send((id) => ({ kind: 'aggregate', id, samples }));
    return (res as Extract<ProcessingResponse, { kind: 'aggregate' }>).result;
  }

  /** Parses raw metric text into samples. */
  async parse(raw: string, format: MetricFormat): Promise<MetricSample[]> {
    if (!this.ensureWorker()) return parseMetrics(raw, format);
    const res = await this.send((id) => ({ kind: 'parse', id, raw, format }));
    return (res as Extract<ProcessingResponse, { kind: 'parse' }>).samples;
  }

  /** Terminates the worker and rejects any in-flight requests. */
  destroy(): void {
    for (const [, p] of this.pending) {
      p.reject(new Error('ProcessingClient destroyed'));
    }
    this.pending.clear();
    this.worker?.terminate();
    this.worker = null;
  }
}

// ─── Singleton ──────────────────────────────────────────────────────────────

let instance: ProcessingClient | null = null;

/** Returns the shared processing client (one worker for the whole app). */
export function getProcessingClient(): ProcessingClient {
  if (!instance) instance = new ProcessingClient();
  return instance;
}
