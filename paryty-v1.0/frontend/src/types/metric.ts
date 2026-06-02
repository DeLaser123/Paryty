import type { Labels, Timestamp, UUID } from './common';

// CPU Metrics
export interface CpuMetrics {
  totalUsagePercent: number;
  perCoreUsage: number[];
  loadAverage1m: number;
  loadAverage5m: number;
  loadAverage15m: number;
  contextSwitches: number;
  interrupts: number;
}

// Memory Metrics
export interface MemoryMetrics {
  totalBytes: number;
  usedBytes: number;
  availableBytes: number;
  usagePercent: number;
  swapTotalBytes: number;
  swapUsedBytes: number;
  buffersBytes: number;
  cachedBytes: number;
}

// Disk Metrics
export interface DiskMetrics {
  device: string;
  mountPoint: string;
  totalBytes: number;
  usedBytes: number;
  usagePercent: number;
  readBytesPerSec: number;
  writeBytesPerSec: number;
  readOpsPerSec: number;
  writeOpsPerSec: number;
  ioWaitPercent: number;
}

// Network Metrics
export interface NetworkMetrics {
  interface: string;
  rxBytesPerSec: number;
  txBytesPerSec: number;
  rxPacketsPerSec: number;
  txPacketsPerSec: number;
  rxErrors: number;
  txErrors: number;
  rxDropped: number;
  txDropped: number;
  tcpEstablished: number;
  tcpTimeWait: number;
  tcpCloseWait: number;
}

// Process Info
export interface ProcessInfo {
  pid: number;
  ppid: number;
  name: string;
  cmdline: string;
  cpuPercent: number;
  memoryRssBytes: number;
  memoryVszBytes: number;
  threadCount: number;
  fdCount: number;
  state: string;
  containerId?: string;
}

// Container Metrics
export interface ContainerMetrics {
  containerId: string;
  name: string;
  runtime: string;
  image: string;
  state: string;
  cpuPercent: number;
  memoryUsageBytes: number;
  memoryLimitBytes: number;
  networkRxBytesPerSec: number;
  networkTxBytesPerSec: number;
}

// Metric Batch
export interface MetricBatch {
  batchId: UUID;
  agentId: string;
  hostname: string;
  timestamp: Timestamp;
  cpu?: CpuMetrics;
  memory?: MemoryMetrics;
  disks: DiskMetrics[];
  networks: NetworkMetrics[];
  processes: ProcessInfo[];
  containers: ContainerMetrics[];
  labels: Labels;
}

// Aggregated Metric
export interface AggregatedMetric {
  name: string;
  labels: Labels;
  timestamp: Timestamp;
  value: number;
  min: number;
  max: number;
  avg: number;
  count: number;
  p50: number;
  p90: number;
  p99: number;
}

// Metric Query
export interface MetricQuery {
  name: string;
  labels?: Labels;
  startTime: Timestamp;
  endTime: Timestamp;
  step: string;
  aggregation?: 'avg' | 'sum' | 'min' | 'max' | 'count';
}

// Metric Data Point
export interface MetricDataPoint {
  timestamp: Timestamp;
  value: number;
}

// Metric Series
export interface MetricSeries {
  name: string;
  labels: Labels;
  points: MetricDataPoint[];
}
