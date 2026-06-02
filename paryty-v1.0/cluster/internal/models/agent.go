package models

import (
	"time"
)

// AgentStatus represents the current status of an agent.
type AgentStatus string

const (
	AgentStatusOnline     AgentStatus = "online"
	AgentStatusOffline    AgentStatus = "offline"
	AgentStatusConnecting AgentStatus = "connecting"
	AgentStatusError      AgentStatus = "error"
)

// AgentInfo contains information about a registered agent.
type AgentInfo struct {
	ID            string            `json:"id" db:"id"`
	Hostname      string            `json:"hostname" db:"hostname"`
	IPAddress     string            `json:"ip_address" db:"ip_address"`
	OS            string            `json:"os" db:"os"`
	Arch          string            `json:"arch" db:"arch"`
	AgentVersion  string            `json:"agent_version" db:"agent_version"`
	Labels        map[string]string `json:"labels" db:"labels"`
	Status        AgentStatus       `json:"status" db:"status"`
	RegisteredAt  time.Time         `json:"registered_at" db:"registered_at"`
	LastHeartbeat time.Time         `json:"last_heartbeat" db:"last_heartbeat"`
	LastError     string            `json:"last_error,omitempty" db:"last_error"`
	ConfigHash    string            `json:"config_hash" db:"config_hash"`
}

// AgentRegistration is the request sent by an agent to register with the cluster.
type AgentRegistration struct {
	AgentID      string            `json:"agent_id" db:"agent_id"`
	Hostname     string            `json:"hostname" db:"hostname"`
	IPAddress    string            `json:"ip_address" db:"ip_address"`
	OS           string            `json:"os" db:"os"`
	Arch         string            `json:"arch" db:"arch"`
	AgentVersion string            `json:"agent_version" db:"agent_version"`
	Labels       map[string]string `json:"labels" db:"labels"`
	Capabilities []string          `json:"capabilities" db:"capabilities"`
}

// AgentConfig is the configuration pushed to an agent by the cluster.
type AgentConfig struct {
	AgentID            string            `json:"agent_id" db:"agent_id"`
	ConfigVersion      string            `json:"config_version" db:"config_version"`
	CollectionInterval time.Duration     `json:"collection_interval" db:"collection_interval"`
	EnabledCollectors  []string          `json:"enabled_collectors" db:"enabled_collectors"`
	SamplingRate       float64           `json:"sampling_rate" db:"sampling_rate"`
	CompressionEnabled bool              `json:"compression_enabled" db:"compression_enabled"`
	FlowControl        FlowControlConfig `json:"flow_control" db:"flow_control"`
}

// FlowControlConfig contains flow control settings for an agent.
type FlowControlConfig struct {
	MaxQueueSize   int     `json:"max_queue_size" db:"max_queue_size"`
	MaxBatchSize   int     `json:"max_batch_size" db:"max_batch_size"`
	SamplingRate   float64 `json:"sampling_rate" db:"sampling_rate"`
	BackpressureOn bool    `json:"backpressure_on" db:"backpressure_on"`
}

// HealthReport is a health report sent by an agent.
type HealthReport struct {
	AgentID     string            `json:"agent_id" db:"agent_id"`
	Status      HealthStatus      `json:"status" db:"status"`
	Uptime      time.Duration     `json:"uptime" db:"uptime"`
	MemoryUsage float64           `json:"memory_usage" db:"memory_usage"`
	CPUUsage    float64           `json:"cpu_usage" db:"cpu_usage"`
	Components  map[string]string `json:"components" db:"components"`
	Timestamp   time.Time         `json:"timestamp" db:"timestamp"`
}
