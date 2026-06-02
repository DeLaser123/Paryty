import type { Timestamp } from './common';
import type { TopologyNode, TopologyEdge } from './topology';

export type TimelineSpeed = 0.5 | 1 | 2 | 4 | 8 | 16;
export type TimelineState = 'playing' | 'paused' | 'stopped';

export interface TimelineSnapshot {
  timestamp: Timestamp;
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  metrics: Record<string, number>;
  alertCount: number;
  eventCount: number;
}

export interface TimelineConfig {
  startTime: Timestamp;
  endTime: Timestamp;
  speed: TimelineSpeed;
  stepMs: number;
}

export interface TimelinePosition {
  currentTime: Timestamp;
  progress: number; // 0-1
  state: TimelineState;
  speed: TimelineSpeed;
}
