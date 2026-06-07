// Agent domain types — mirror the cluster's Go models (snake_case wire format).
// Source of truth: cluster/internal/models/agent.go and topology.go.

import type { Labels, Timestamp } from './common';

/** Connection lifecycle state of an agent. */
export type AgentStatus = 'online' | 'offline' | 'connecting' | 'error';

/** Overall health classification shared by agents and topology nodes. */
export type HealthStatus = 'healthy' | 'degraded' | 'unhealthy' | 'unknown';

/**
 * Registered agent metadata.
 *
 * Field names match the cluster JSON contract exactly (`GET /api/v1/agents`,
 * `GET /api/v1/agents/:id`).
 */
export interface AgentInfo {
  id: string;
  hostname: string;
  ip_address: string;
  os: string;
  arch: string;
  agent_version: string;
  labels: Labels;
  status: AgentStatus;
  registered_at: Timestamp;
  last_heartbeat: Timestamp;
  last_error?: string;
  config_hash: string;
}

/**
 * Health report for a single agent (`GET /api/v1/agents/:id/health`).
 *
 * `uptime` is a Go `time.Duration` serialized as nanoseconds.
 */
export interface HealthReport {
  agent_id: string;
  status: HealthStatus;
  uptime: number;
  memory_usage: number;
  cpu_usage: number;
  components: Record<string, string>;
  timestamp: Timestamp;
}
