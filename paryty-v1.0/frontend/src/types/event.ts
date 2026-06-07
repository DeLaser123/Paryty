import type { Labels, Timestamp, UUID } from './common';

export type EventSeverity = 'info' | 'warning' | 'error' | 'critical';
export type EventCategory = 'system' | 'application' | 'security' | 'audit';
export type TransportMode = 'ws' | 'rest' | 'sse' | 'offline';

export interface ParytyEvent {
  id: UUID;
  source: string;
  category: EventCategory;
  severity: EventSeverity;
  title: string;
  message: string;
  labels: Labels;
  timestamp: Timestamp;
  relatedEntityId?: string;
  relatedEntityType?: string;
  acknowledged: boolean;
  acknowledgedBy?: string;
  acknowledgedAt?: Timestamp;
}

export interface EventQuery {
  source?: string;
  category?: EventCategory;
  severity?: EventSeverity;
  startTime: Timestamp;
  endTime: Timestamp;
  labels?: Labels;
  limit?: number;
}
