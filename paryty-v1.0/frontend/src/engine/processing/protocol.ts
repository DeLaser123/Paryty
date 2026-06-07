/**
 * Processing-worker message protocol.
 *
 * Defines the typed request/response contract between the UI thread and the
 * single shared processing worker (the "heavy compute thread"). One worker
 * serves every CPU-bound task — force-directed layout, LTTB downsampling,
 * metric aggregation, and metric parsing — so the application runs exactly one
 * processing thread regardless of how many features are active.
 *
 * Every request carries a string `id` the client uses to correlate the
 * matching response; the worker echoes it back unchanged.
 *
 * @module engine/processing/protocol
 */

import type {
  LayoutInput,
  ClusteredLayoutInput,
  PositionUpdateInput,
  LayoutResult,
} from './layout';
import type {
  MetricSample,
  AggregatedMetric,
  MetricFormat,
} from './metrics';
import type { MetricDataPoint } from '../../types/metric';

// ─── UI → Processing requests ───────────────────────────────────────────────

/** Full force-directed layout. */
export interface LayoutRequest {
  readonly kind: 'layout';
  readonly id: string;
  readonly input: LayoutInput;
}

/** Hierarchical (grouped) layout. */
export interface ClusteredLayoutRequest {
  readonly kind: 'clusteredLayout';
  readonly id: string;
  readonly input: ClusteredLayoutInput;
}

/** Incremental settle around dragged nodes. */
export interface PositionUpdateRequest {
  readonly kind: 'positionUpdate';
  readonly id: string;
  readonly input: PositionUpdateInput;
}

/** LTTB downsampling of a time series. */
export interface DownsampleRequest {
  readonly kind: 'downsample';
  readonly id: string;
  readonly data: readonly MetricDataPoint[];
  readonly threshold: number;
}

/** Aggregate samples into per-label summary statistics. */
export interface AggregateRequest {
  readonly kind: 'aggregate';
  readonly id: string;
  readonly samples: readonly MetricSample[];
}

/** Parse raw metric text into samples. */
export interface ParseRequest {
  readonly kind: 'parse';
  readonly id: string;
  readonly raw: string;
  readonly format: MetricFormat;
}

/** Union of all UI → processing-worker requests. */
export type ProcessingRequest =
  | LayoutRequest
  | ClusteredLayoutRequest
  | PositionUpdateRequest
  | DownsampleRequest
  | AggregateRequest
  | ParseRequest;

// ─── Processing → UI responses ──────────────────────────────────────────────

/** Successful layout / clustered-layout / position-update result. */
export interface LayoutResponse {
  readonly kind: 'layout';
  readonly id: string;
  readonly result: LayoutResult;
}

/** Successful downsample result. */
export interface DownsampleResponse {
  readonly kind: 'downsample';
  readonly id: string;
  readonly data: MetricDataPoint[];
}

/** Successful aggregation result. */
export interface AggregateResponse {
  readonly kind: 'aggregate';
  readonly id: string;
  readonly result: AggregatedMetric[];
}

/** Successful parse result. */
export interface ParseResponse {
  readonly kind: 'parse';
  readonly id: string;
  readonly samples: MetricSample[];
}

/** A request failed; carries the originating id and an error message. */
export interface ErrorResponse {
  readonly kind: 'error';
  readonly id: string;
  readonly message: string;
}

/** Union of all processing-worker → UI responses. */
export type ProcessingResponse =
  | LayoutResponse
  | DownsampleResponse
  | AggregateResponse
  | ParseResponse
  | ErrorResponse;

// ─── Type guards ────────────────────────────────────────────────────────────

/** Narrows an unknown message to a {@link ProcessingRequest}. */
export function isProcessingRequest(value: unknown): value is ProcessingRequest {
  if (typeof value !== 'object' || value === null) return false;
  const kind = (value as { kind?: unknown }).kind;
  return (
    kind === 'layout' ||
    kind === 'clusteredLayout' ||
    kind === 'positionUpdate' ||
    kind === 'downsample' ||
    kind === 'aggregate' ||
    kind === 'parse'
  );
}

/** Narrows an unknown message to a {@link ProcessingResponse}. */
export function isProcessingResponse(value: unknown): value is ProcessingResponse {
  if (typeof value !== 'object' || value === null) return false;
  const kind = (value as { kind?: unknown }).kind;
  const id = (value as { id?: unknown }).id;
  if (typeof id !== 'string') return false;
  return (
    kind === 'layout' ||
    kind === 'downsample' ||
    kind === 'aggregate' ||
    kind === 'parse' ||
    kind === 'error'
  );
}
