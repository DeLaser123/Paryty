/**
 * Processing Worker — the single heavy-compute thread.
 *
 * Hosts every CPU-bound algorithm behind one worker so the application runs
 * exactly one processing thread: force-directed layout, LTTB downsampling,
 * metric aggregation, and metric parsing. Each handler delegates to a pure
 * module in `engine/processing/*` that the main-thread fallback shares, so the
 * algorithms have a single implementation.
 *
 * @module engine/processing/processingWorker
 */

import {
  computeLayout,
  computeClusteredLayout,
  computePositionUpdate,
} from './layout';
import { lttbDownsample, aggregateMetrics, parseMetrics } from './metrics';
import {
  isProcessingRequest,
  type ProcessingRequest,
  type ProcessingResponse,
} from './protocol';

/** Posts a typed response back to the UI thread. */
function reply(response: ProcessingResponse): void {
  self.postMessage(response);
}

/** Routes one validated request to its pure algorithm and replies. */
function handle(request: ProcessingRequest): void {
  switch (request.kind) {
    case 'layout':
      reply({ kind: 'layout', id: request.id, result: computeLayout(request.input) });
      return;
    case 'clusteredLayout':
      reply({
        kind: 'layout',
        id: request.id,
        result: computeClusteredLayout(request.input),
      });
      return;
    case 'positionUpdate':
      reply({
        kind: 'layout',
        id: request.id,
        result: computePositionUpdate(request.input),
      });
      return;
    case 'downsample':
      reply({
        kind: 'downsample',
        id: request.id,
        data: lttbDownsample([...request.data], request.threshold),
      });
      return;
    case 'aggregate':
      reply({
        kind: 'aggregate',
        id: request.id,
        result: aggregateMetrics([...request.samples]),
      });
      return;
    case 'parse':
      reply({
        kind: 'parse',
        id: request.id,
        samples: parseMetrics(request.raw, request.format),
      });
      return;
  }
}

self.onmessage = (event: MessageEvent<unknown>): void => {
  const data = event.data;
  if (!isProcessingRequest(data)) return;
  try {
    handle(data);
  } catch (err) {
    reply({ kind: 'error', id: data.id, message: (err as Error).message });
  }
};
