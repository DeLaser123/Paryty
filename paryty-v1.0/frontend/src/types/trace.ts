import type { Labels, Timestamp, UUID } from './common';

export type SpanKind = 'internal' | 'server' | 'client' | 'producer' | 'consumer';
export type SpanStatus = 'ok' | 'error' | 'unset';

export interface SpanEvent {
  name: string;
  timestamp: Timestamp;
  attributes: Labels;
}

export interface Span {
  traceId: UUID;
  spanId: UUID;
  parentSpanId?: UUID;
  name: string;
  kind: SpanKind;
  status: SpanStatus;
  statusMessage?: string;
  startTime: Timestamp;
  endTime: Timestamp;
  durationMs: number;
  attributes: Labels;
  events: SpanEvent[];
  serviceName: string;
}

export interface Trace {
  traceId: UUID;
  spans: Span[];
  rootSpan?: Span;
  serviceName: string;
  totalDurationMs: number;
}

export interface TraceQuery {
  serviceName?: string;
  operationName?: string;
  minDurationMs?: number;
  maxDurationMs?: number;
  startTime: Timestamp;
  endTime: Timestamp;
  attributes?: Labels;
  limit?: number;
}
