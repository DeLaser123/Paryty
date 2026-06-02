import type { Labels, Timestamp, UUID } from './common';

// Node in the topology graph
export interface TopologyNode {
  id: string;
  name: string;
  type: 'host' | 'container' | 'service' | 'process';
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  labels: Labels;
  metadata: Record<string, unknown>;
  cpuUsage?: number;
  memoryUsage?: number;
  lastSeen: Timestamp;
}

// Edge in the topology graph
export interface TopologyEdge {
  id: string;
  sourceId: string;
  targetId: string;
  type: 'network' | 'dependency' | 'contains' | 'calls';
  protocol?: string;
  latencyMs?: number;
  bytesPerSec?: number;
  errorRate?: number;
  metadata: Record<string, unknown>;
}

// Complete topology
export interface Topology {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  timestamp: Timestamp;
  version: UUID;
}

// Topology diff for incremental updates
export interface TopologyDiff {
  addedNodes: TopologyNode[];
  removedNodes: string[];
  updatedNodes: TopologyNode[];
  addedEdges: TopologyEdge[];
  removedEdges: string[];
  updatedEdges: TopologyEdge[];
  timestamp: Timestamp;
  baseVersion: UUID;
  newVersion: UUID;
}
