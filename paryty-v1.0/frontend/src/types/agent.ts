// Agent domain types — mirror the cluster's Go models (snake_case wire format).
// Source of truth: cluster/internal/models/agent.go and topology.go.

import type { Labels, Timestamp } from './common';

/** Connection lifecycle state of an agent. */
export type AgentStatus = 'online' | 'offline' | 'connecting' | 'error';

/** Overall health classification shared by agents and topology nodes. */
export type HealthStatus = 'healthy' | 'degraded' | 'unhealthy' | 'unknown';

/** Edge agent lifecycle status (Dual Reality) */
export type EdgeAgentLifecycleStatus =
  | 'unconfigured' | 'active' | 'lost' | 'rogue' | 'retired' | 'blacklisted'
  | 'pending' | 'deployed' | 'inactive'; // backward compat

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

/** Agent management info from `GET /api/v1/agents/all`. */
export interface AgentManagementInfo {
  agent_id: string;
  name: string;
  hostname: string;
  status: string; // pending | deployed | inactive | unconfigured | active | lost | rogue | retired | blacklisted
  os: string;
  arch: string;
  cloud_provider: string;
  location: string;
  assigned_twin: string | null;
  first_seen: string;
  last_seen: string;
  /** Timestamp when the agent was paired with a cluster agent. */
  paired_at: string | null;
  /** Timestamp when the agent was retired. */
  retired_at: string | null;
  /** Timestamp when the agent was blacklisted. */
  blacklisted_at: string | null;
  /** Reason for blacklisting, if applicable. */
  blacklist_reason: string | null;
  /** Identity token for agent authentication. */
  identity_token: string | null;
}

/** Pairing status response from backend */
export interface AgentPairingStatus {
  agent_id: string;
  edge_status: EdgeAgentLifecycleStatus;
  cluster_agent_id: string | null;
  cluster_agent_name: string | null;
  cluster_agent_status: string | null;
  is_paired: boolean;
  paired_at: string | null;
  os: string;
  arch: string;
  hostname: string;
  retired_at: string | null;
  blacklisted_at: string | null;
  blacklist_reason: string | null;
}

/** Response from `POST /api/v1/agents`. */
export interface CreateAgentResponse {
  agent_id: string;
  name: string;
  status: string;
}
