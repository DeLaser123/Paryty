// Raw per-agent metric batch types — mirror the cluster's Go models exactly
// (snake_case wire format) for `GET /api/v1/metrics/:agent_id` and the
// pre-aggregated `GET /api/v1/metrics/:agent_id/aggregated`.
// Source of truth: cluster/internal/models/metric.go.

import type { Labels, Timestamp } from './common';

/** CPU collector sample. */
export interface CpuMetricSample {
  agent_id: string;
  timestamp: Timestamp;
  total_usage_percent: number;
  per_core_percent: number[];
  load_average_1m: number;
  load_average_5m: number;
  load_average_15m: number;
  frequency_mhz: number;
  context_switches: number;
  physical_cores: number;
  logical_cores: number;
  model_name: string;
  vendor_id: string;
}

/** PSI (Pressure Stall Information) for memory. */
export interface MemoryPressure {
  some_10: number;
  some_60: number;
  some_300: number;
  full_10: number;
  full_60: number;
  full_300: number;
}

/** One of the top processes by resident memory. */
export interface ProcessMemoryEntry {
  pid: number;
  name: string;
  rss_bytes: number;
  vsz_bytes: number;
}

/** Memory collector sample. */
export interface MemoryMetricSample {
  agent_id: string;
  timestamp: Timestamp;
  total_bytes: number;
  used_bytes: number;
  free_bytes: number;
  available_bytes: number;
  cached_bytes: number;
  buffer_bytes: number;
  swap_total_bytes: number;
  swap_used_bytes: number;
  usage_percent: number;
  pressure?: MemoryPressure;
  top_processes?: ProcessMemoryEntry[];
}

/** Disk collector sample. */
export interface DiskMetricSample {
  agent_id: string;
  timestamp: Timestamp;
  device: string;
  mount_point: string;
  filesystem_type: string;
  total_bytes: number;
  used_bytes: number;
  free_bytes: number;
  read_bytes_per_sec: number;
  write_bytes_per_sec: number;
  iops_read: number;
  iops_write: number;
  io_latency_ms: number;
  queue_depth: number;
  is_ssd: boolean;
  utilization_pct: number;
}

/** TCP connection-state counts. */
export interface TcpStats {
  established: number;
  time_wait: number;
  close_wait: number;
  listen: number;
  retransmit_count: number;
}

/** Network collector sample. */
export interface NetworkMetricSample {
  agent_id: string;
  timestamp: Timestamp;
  interface: string;
  rx_bytes_per_sec: number;
  tx_bytes_per_sec: number;
  rx_packets: number;
  tx_packets: number;
  rx_dropped: number;
  tx_dropped: number;
  errors: number;
  estimated_rtt_ms: number;
  total_rx_bytes: number;
  total_tx_bytes: number;
  total_rx_packets: number;
  total_tx_packets: number;
  speed_mbps: number;
  is_up: boolean;
  tcp_stats?: TcpStats;
}

/** Per-process collector sample. */
export interface ProcessMetricSample {
  agent_id: string;
  timestamp: Timestamp;
  pid: number;
  parent_pid: number;
  name: string;
  command_line: string;
  cpu_usage_percent: number;
  memory_bytes: number;
  vsz_bytes: number;
  status: string;
  threads: number;
  fd_count: number;
  container_id: string;
  exe: string;
  disk_read_bytes: number;
  disk_written_bytes: number;
  user_id: string;
  started_at: Timestamp;
}

/** Per-container collector sample. */
export interface ContainerMetricSample {
  agent_id: string;
  timestamp: Timestamp;
  container_id: string;
  runtime: string;
  name: string;
  image: string;
  status: string;
  cgroup_version: string;
  pids: number[];
  memory_limit_bytes: number;
  cpu_quota: number;
  cpu_shares: number;
}

/** Latest raw metric batch for one agent (`GET /api/v1/metrics/:agent_id`). */
export interface AgentMetricBatch {
  agent_id: string;
  timestamp: Timestamp;
  cpu?: CpuMetricSample[];
  memory?: MemoryMetricSample[];
  disk?: DiskMetricSample[];
  network?: NetworkMetricSample[];
  processes?: ProcessMetricSample[];
  containers?: ContainerMetricSample[];
}

/** How a pre-aggregated metric was reduced over its window. */
export type AggregationType =
  | 'avg'
  | 'sum'
  | 'min'
  | 'max'
  | 'p50'
  | 'p90'
  | 'p99'
  | 'count';

/**
 * Pre-aggregated metric point (`GET /api/v1/metrics/:agent_id/aggregated`).
 *
 * `window` is a Go `time.Duration` serialized as nanoseconds.
 */
export interface AgentAggregatedMetric {
  agent_id: string;
  name: string;
  labels: Labels;
  window: number;
  agg_type: AggregationType;
  value: number;
  timestamp: Timestamp;
}
