/**
 * DigitalParyty — the client-side entity representing one Digital Paryty.
 *
 * A Digital Paryty is a live-telemetry-driven digital twin of one software
 * system owned by the client. Clients can own multiple Digital Parytys bounded
 * by their subscription tier.
 */

import type { EnabledAbility, AbilityId } from './ability';
import type { HealthStatus, AgentStatus } from './agent';

export interface DigitalParytyConfig {
  /** Agent IDs whose telemetry feeds this Digital Paryty. */
  agentIds: string[];
  /** Optional tenant label override. */
  tenantLabel?: string;
}

export interface DigitalParytySummary {
  /** Count of currently active traces (topology_observation). */
  activeTraces?: number;
  /** Forecast horizon in days (forecasting). */
  forecastHorizonDays?: number;
  /** ISO timestamp of the last Watif drill run. */
  lastDrillAt?: string;
  /** Active unacknowledged alert count. */
  activeAlerts?: number;
  /** Live node count from last topology snapshot. */
  nodeCount?: number;
}

export interface DigitalParyty {
  id: string;
  /** Human-readable name the client chose. */
  name: string;
  /** The software system being twinned (e.g. "Payment Gateway", "Auth Service"). */
  systemLabel: string;
  /** Current aggregate health status. */
  health: HealthStatus;
  /** Abilities the client has enabled on this Digital Paryty. */
  abilities: EnabledAbility[];
  /** Runtime configuration. */
  config: DigitalParytyConfig;
  /** Lightweight summary stats surfaced on the catalogue card. */
  summary: DigitalParytySummary;
  createdAt: string; // ISO 8601
  updatedAt: string; // ISO 8601
}

// ─── Creation wizard input ──────────────────────────────────────────────────

export interface CreateParytyDraft {
  name: string;
  systemLabel: string;
  agentIds: string[];
  selectedAbilities: AbilityId[];
}

// ─── Twin Detail Page Types (Task 13) ───────────────────────────────────────

export interface TwinConfig {
  /** Agent IDs whose telemetry feeds this Digital Paryty. */
  agentIds: string[];
  /** Optional tenant label override. */
  tenantLabel?: string;
}

export interface TwinDetails {
  id: string;
  tenantId: string;
  name: string;
  description: string;
  status: string;
  agentCount: number;
  config: TwinConfig;
  createdAt: string;
  updatedAt: string;
}

export interface TwinAgentInfo {
  agentId: string;
  hostname: string;
  status: AgentStatus;
  backlogBytes: number;
  lastHeartbeat: string;
  assigned: boolean;
  os: string;
  arch: string;
}
