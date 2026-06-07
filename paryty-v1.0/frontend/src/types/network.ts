// Network event types — mirror the cluster's `GET /api/v1/network-events` contract.
// The cluster returns a heterogeneous array discriminated by a `type` field
// ("tcp" | "dns" | "http"); remaining fields vary per event kind, so the
// payload is modeled as an open record keyed off that discriminator.
// Source of truth: cluster/internal/api/query/rest.go (QueryNetworkEvents).

import type { Timestamp } from './common';

/** Discriminator for the kind of captured network event. */
export type NetworkEventType = 'tcp' | 'dns' | 'http';

/**
 * A single captured network event.
 *
 * Only `type` is guaranteed; `agent_id` and `timestamp` are present on most
 * events. Kind-specific fields (ports, hostnames, status codes, …) are
 * accessed through the index signature.
 */
export interface NetworkEvent {
  type: NetworkEventType;
  agent_id?: string;
  timestamp?: Timestamp;
  [key: string]: unknown;
}

/** Query parameters for `GET /api/v1/network-events`. */
export interface NetworkEventQuery {
  agentId?: string;
  type?: NetworkEventType;
  start?: Timestamp;
  end?: Timestamp;
  limit?: number;
}
