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
	PhysicalCores   int32     `json:"physical_cores" db:"physical_cores"`
	LogicalCores    int32     `json:"logical_cores" db:"logical_cores"`
	ModelName       string    `json:"model_name" db:"model_name"`
	VendorID        string    `json:"vendor_id" db:"vendor_id"`
}

// MemoryMetrics contains memory-related metrics for an agent.
type MemoryMetrics struct {
	AgentID        string               `json:"agent_id" db:"agent_id"`
	Timestamp      time.Time            `json:"timestamp" db:"timestamp"`
	TotalBytes     uint64               `json:"total_bytes" db:"total_bytes"`
	UsedBytes      uint64               `json:"used_bytes" db:"used_bytes"`
	FreeBytes      uint64               `json:"free_bytes" db:"free_bytes"`
	AvailableBytes uint64               `json:"available_bytes" db:"available_bytes"`
	CachedBytes    uint64               `json:"cached_bytes" db:"cached_bytes"`
	BufferBytes    uint64               `json:"buffer_bytes" db:"buffer_bytes"`
	SwapTotalBytes uint64               `json:"swap_total_bytes" db:"swap_total_bytes"`
	SwapUsedBytes  uint64               `json:"swap_used_bytes" db:"swap_used_bytes"`
	UsagePercent   float64              `json:"usage_percent" db:"usage_percent"`
	Pressure       *MemoryPressure      `json:"pressure,omitempty" db:"pressure"`
	TopProcesses   []ProcessMemoryEntry `json:"top_processes,omitempty" db:"top_processes"`
}

// MemoryPressure contains PSI (Pressure Stall Information) data.
type MemoryPressure struct {
	Some10  float64 `json:"some_10"`
	Some60  float64 `json:"some_60"`
	Some300 float64 `json:"some_300"`
	Full10  float64 `json:"full_10"`
	Full60  float64 `json:"full_60"`
	Full300 float64 `json:"full_300"`
}

// ProcessMemoryEntry identifies one of the top N processes by RSS.
type ProcessMemoryEntry struct {
	PID      uint32 `json:"pid"`
	Name     string `json:"name"`
	RSSBytes uint64 `json:"rss_bytes"`
	VSZBytes uint64 `json:"vsz_bytes"`
}

// DiskMetrics contains disk-related metrics for an agent.
type DiskMetrics struct {
	AgentID          string    `json:"agent_id" db:"agent_id"`
	Timestamp        time.Time `json:"timestamp" db:"timestamp"`
	Device           string    `json:"device" db:"device"`
	MountPoint       string    `json:"mount_point" db:"mount_point"`
	FilesystemType   string    `json:"filesystem_type" db:"filesystem_type"`
	TotalBytes       uint64    `json:"total_bytes" db:"total_bytes"`
	UsedBytes        uint64    `json:"used_bytes" db:"used_bytes"`
	FreeBytes        uint64    `json:"free_bytes" db:"free_bytes"`
	ReadBytesPerSec  uint64    `json:"read_bytes_per_sec" db:"read_bytes_per_sec"`
	WriteBytesPerSec uint64    `json:"write_bytes_per_sec" db:"write_bytes_per_sec"`
	IOPSRead         uint64    `json:"iops_read" db:"iops_read"`
	IOPSWrite        uint64    `json:"iops_write" db:"iops_write"`
	IOLatencyMs      float64   `json:"io_latency_ms" db:"io_latency_ms"`
	QueueDepth       float64   `json:"queue_depth" db:"queue_depth"`
	IsSSD            bool      `json:"is_ssd" db:"is_ssd"`
	UtilizationPct   float64   `json:"utilization_pct" db:"utilization_pct"`
}

// NetworkMetrics contains network-related metrics for an agent.
type NetworkMetrics struct {
	AgentID        string    `json:"agent_id" db:"agent_id"`
	Timestamp      time.Time `json:"timestamp" db:"timestamp"`
	Interface      string    `json:"interface" db:"interface"`
	RxBytesPerSec  uint64    `json:"rx_bytes_per_sec" db:"rx_bytes_per_sec"`
	TxBytesPerSec  uint64    `json:"tx_bytes_per_sec" db:"tx_bytes_per_sec"`
	RxPackets      uint64    `json:"rx_packets" db:"rx_packets"`
	TxPackets      uint64    `json:"tx_packets" db:"tx_packets"`
	RxDropped      int64     `json:"rx_dropped" db:"rx_dropped"`
	TxDropped      int64     `json:"tx_dropped" db:"tx_dropped"`
	Errors         uint64    `json:"errors" db:"errors"`
	EstimatedRTTMs float64   `json:"estimated_rtt_ms" db:"estimated_rtt_ms"`
	TotalRxBytes   int64     `json:"total_rx_bytes" db:"total_rx_bytes"`
	TotalTxBytes   int64     `json:"total_tx_bytes" db:"total_tx_bytes"`
	TotalRxPackets int64     `json:"total_rx_packets" db:"total_rx_packets"`
	TotalTxPackets int64     `json:"total_tx_packets" db:"total_tx_packets"`
	SpeedMbps      int64     `json:"speed_mbps" db:"speed_mbps"`
	IsUp           bool      `json:"is_up" db:"is_up"`
	TCPStats       *TCPStats `json:"tcp_stats,omitempty" db:"tcp_stats"`
}

// TCPStats contains TCP connection state metrics.
type TCPStats struct {
	Established     int32 `json:"established"`
	TimeWait        int32 `json:"time_wait"`
	CloseWait       int32 `json:"close_wait"`
	Listen          int32 `json:"listen"`
	RetransmitCount int64 `json:"retransmit_count"`
}

// ProcessMetrics contains per-process metrics.
type ProcessMetrics struct {
	AgentID          string    `json:"agent_id" db:"agent_id"`
	Timestamp        time.Time `json:"timestamp" db:"timestamp"`
	PID              uint32    `json:"pid" db:"pid"`
	ParentPID        uint32    `json:"parent_pid" db:"parent_pid"`
	Name             string    `json:"name" db:"name"`
	CommandLine      string    `json:"command_line" db:"command_line"`
	CPUUsagePct      float64   `json:"cpu_usage_percent" db:"cpu_usage_percent"`
	MemoryBytes      uint64    `json:"memory_bytes" db:"memory_bytes"`
	VszBytes         uint64    `json:"vsz_bytes" db:"vsz_bytes"`
	Status           string    `json:"status" db:"status"`
	Threads          uint32    `json:"threads" db:"threads"`
	FdCount          uint32    `json:"fd_count" db:"fd_count"`
	ContainerID      string    `json:"container_id" db:"container_id"`
	Exe              string    `json:"exe" db:"exe"`
	DiskReadBytes    int64     `json:"disk_read_bytes" db:"disk_read_bytes"`
	DiskWrittenBytes int64     `json:"disk_written_bytes" db:"disk_written_bytes"`
	UserID           string    `json:"user_id" db:"user_id"`
	StartedAt        time.Time `json:"started_at" db:"started_at"`
}

// MetricBatch is a batch of metrics from a single agent.
type MetricBatch struct {
	AgentID    string             `json:"agent_id" db:"agent_id"`
	Timestamp  time.Time          `json:"timestamp" db:"timestamp"`
	CPU        []CPUMetrics       `json:"cpu,omitempty" db:"cpu"`
	Memory     []MemoryMetrics    `json:"memory,omitempty" db:"memory"`
	Disk       []DiskMetrics      `json:"disk,omitempty" db:"disk"`
	Network    []NetworkMetrics   `json:"network,omitempty" db:"network"`
	Processes  []ProcessMetrics   `json:"processes,omitempty" db:"processes"`
	Containers []ContainerMetrics `json:"containers,omitempty" db:"containers"`
}

// ContainerMetrics contains per-container data.
type ContainerMetrics struct {
	AgentID          string    `json:"agent_id" db:"agent_id"`
	Timestamp        time.Time `json:"timestamp" db:"timestamp"`
	ContainerID      string    `json:"container_id" db:"container_id"`
	Runtime          string    `json:"runtime" db:"runtime"`
	Name             string    `json:"name" db:"name"`
	Image            string    `json:"image" db:"image"`
	Status           string    `json:"status" db:"status"`
	CgroupVersion    string    `json:"cgroup_version" db:"cgroup_version"`
	PIDs             []int32   `json:"pids" db:"pids"`
	MemoryLimitBytes int64     `json:"memory_limit_bytes" db:"memory_limit_bytes"`
	CPUQuota         float64   `json:"cpu_quota" db:"cpu_quota"`
	CPUShares        int64     `json:"cpu_shares" db:"cpu_shares"`
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
