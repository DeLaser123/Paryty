import type { Labels, Timestamp, UUID } from './common';

export type AlertSeverity = 'info' | 'warning' | 'critical';
export type AlertState = 'pending' | 'firing' | 'resolved' | 'silenced';
export type AlertComparison = 'gt' | 'lt' | 'gte' | 'lte' | 'eq' | 'neq';

export interface AlertRule {
  id: UUID;
  name: string;
  description: string;
  metricName: string;
  comparison: AlertComparison;
  threshold: number;
  duration: string;
  severity: AlertSeverity;
  labels: Labels;
  annotations: Labels;
  enabled: boolean;
  createdAt: Timestamp;
  updatedAt: Timestamp;
}

export interface Alert {
  id: UUID;
  ruleId: UUID;
  ruleName: string;
  state: AlertState;
  severity: AlertSeverity;
  value: number;
  threshold: number;
  labels: Labels;
  annotations: Labels;
  startedAt: Timestamp;
  resolvedAt?: Timestamp;
  silencedUntil?: Timestamp;
  fingerprint: string;
}

export interface AlertSilence {
  id: UUID;
  alertFingerprint: string;
  reason: string;
  startsAt: Timestamp;
  endsAt: Timestamp;
  createdBy: string;
}
