// Package models defines the domain types for the Paryty cluster.
// These types match the proto definitions and include JSON/DB tags
// for serialization and database storage.
package models

import (
	"time"
)

// MetricType represents the type of metric being collected.
type MetricType string

const (
	MetricTypeCPU     MetricType = "cpu"
	MetricTypeMemory  MetricType = "memory"
	MetricTypeDisk    MetricType = "disk"
	MetricTypeNetwork MetricType = "network"
	MetricTypeProcess MetricType = "process"
	MetricTypeCustom  MetricType = "custom"
)

// AggregationType represents how metrics should be aggregated.
type AggregationType string

const (
	AggregationAvg   AggregationType = "avg"
	AggregationSum   AggregationType = "sum"
	AggregationMin   AggregationType = "min"
	AggregationMax   AggregationType = "max"
	AggregationP50   AggregationType = "p50"
	AggregationP90   AggregationType = "p90"
	AggregationP99   AggregationType = "p99"
	AggregationCount AggregationType = "count"
)

// Metric represents a single metric data point.
type Metric struct {
	// AgentID is the unique identifier of the agent that collected this metric.
	AgentID string `json:"agent_id" db:"agent_id"`

	// Name is the metric name (e.g., "cpu.usage_percent", "memory.used_bytes").
	Name string `json:"name" db:"name"`

	// Labels are key-value pairs for metric dimensions.
	Labels map[string]string `json:"labels" db:"labels"`

	// Value is the metric value.
	Value float64 `json:"value" db:"value"`

	// Type indicates the metric type for aggregation purposes.
	Type MetricType `json:"type" db:"type"`

	// Timestamp is when the metric was collected.
	Timestamp time.Time `json:"timestamp" db:"timestamp"`
}

// CPUMetrics contains CPU-related metrics for an agent.
type CPUMetrics struct {
	AgentID         string    `json:"agent_id" db:"agent_id"`
	Timestamp       time.Time `json:"timestamp" db:"timestamp"`
	TotalUsagePct   float64   `json:"total_usage_percent" db:"total_usage_percent"`
	PerCorePct      []float64 `json:"per_core_percent" db:"per_core_percent"`
	LoadAvg1m       float64   `json:"load_average_1m" db:"load_average_1m"`
	LoadAvg5m       float64   `json:"load_average_5m" db:"load_average_5m"`
	LoadAvg15m      float64   `json:"load_average_15m" db:"load_average_15m"`
	FrequencyMHz    float64   `json:"frequency_mhz" db:"frequency_mhz"`
	ContextSwitches uint64    `json:"context_switches" db:"context_switches"`
}

// MemoryMetrics contains memory-related metrics for an agent.
type MemoryMetrics struct {
	AgentID        string    `json:"agent_id" db:"agent_id"`
	Timestamp      time.Time `json:"timestamp" db:"timestamp"`
	TotalBytes     uint64    `json:"total_bytes" db:"total_bytes"`
	UsedBytes      uint64    `json:"used_bytes" db:"used_bytes"`
	AvailableBytes uint64    `json:"available_bytes" db:"available_bytes"`
	CachedBytes    uint64    `json:"cached_bytes" db:"cached_bytes"`
	SwapTotalBytes uint64    `json:"swap_total_bytes" db:"swap_total_bytes"`
	SwapUsedBytes  uint64    `json:"swap_used_bytes" db:"swap_used_bytes"`
}

// DiskMetrics contains disk-related metrics for an agent.
type DiskMetrics struct {
	AgentID          string    `json:"agent_id" db:"agent_id"`
	Timestamp        time.Time `json:"timestamp" db:"timestamp"`
	Device           string    `json:"device" db:"device"`
	MountPoint       string    `json:"mount_point" db:"mount_point"`
	TotalBytes       uint64    `json:"total_bytes" db:"total_bytes"`
	UsedBytes        uint64    `json:"used_bytes" db:"used_bytes"`
	ReadBytesPerSec  uint64    `json:"read_bytes_per_sec" db:"read_bytes_per_sec"`
	WriteBytesPerSec uint64    `json:"write_bytes_per_sec" db:"write_bytes_per_sec"`
	IOPSRead         uint64    `json:"iops_read" db:"iops_read"`
	IOPSWrite        uint64    `json:"iops_write" db:"iops_write"`
}

// NetworkMetrics contains network-related metrics for an agent.
type NetworkMetrics struct {
	AgentID       string    `json:"agent_id" db:"agent_id"`
	Timestamp     time.Time `json:"timestamp" db:"timestamp"`
	Interface     string    `json:"interface" db:"interface"`
	RxBytesPerSec uint64    `json:"rx_bytes_per_sec" db:"rx_bytes_per_sec"`
	TxBytesPerSec uint64    `json:"tx_bytes_per_sec" db:"tx_bytes_per_sec"`
	RxPackets     uint64    `json:"rx_packets" db:"rx_packets"`
	TxPackets     uint64    `json:"tx_packets" db:"tx_packets"`
	Errors        uint64    `json:"errors" db:"errors"`
}

// ProcessMetrics contains per-process metrics.
type ProcessMetrics struct {
	AgentID     string    `json:"agent_id" db:"agent_id"`
	Timestamp   time.Time `json:"timestamp" db:"timestamp"`
	PID         uint32    `json:"pid" db:"pid"`
	Name        string    `json:"name" db:"name"`
	CPUUsagePct float64   `json:"cpu_usage_percent" db:"cpu_usage_percent"`
	MemoryBytes uint64    `json:"memory_bytes" db:"memory_bytes"`
	Threads     uint32    `json:"threads" db:"threads"`
}

// MetricBatch is a batch of metrics from a single agent.
type MetricBatch struct {
	AgentID   string           `json:"agent_id" db:"agent_id"`
	Timestamp time.Time        `json:"timestamp" db:"timestamp"`
	CPU       []CPUMetrics     `json:"cpu,omitempty" db:"cpu"`
	Memory    []MemoryMetrics  `json:"memory,omitempty" db:"memory"`
	Disk      []DiskMetrics    `json:"disk,omitempty" db:"disk"`
	Network   []NetworkMetrics `json:"network,omitempty" db:"network"`
	Processes []ProcessMetrics `json:"processes,omitempty" db:"processes"`
}

// AggregatedMetric represents a pre-aggregated metric for faster queries.
type AggregatedMetric struct {
	AgentID   string            `json:"agent_id" db:"agent_id"`
	Name      string            `json:"name" db:"name"`
	Labels    map[string]string `json:"labels" db:"labels"`
	Window    time.Duration     `json:"window" db:"window"`
	AggType   AggregationType   `json:"agg_type" db:"agg_type"`
	Value     float64           `json:"value" db:"value"`
	Timestamp time.Time         `json:"timestamp" db:"timestamp"`
}
