package models

import (
	"time"
)

// NodeType represents the type of node in the topology graph.
type NodeType string

const (
	NodeTypeService   NodeType = "service"
	NodeTypeContainer NodeType = "container"
	NodeTypeProcess   NodeType = "process"
	NodeTypeHost      NodeType = "host"
	NodeTypeDatabase  NodeType = "database"
	NodeTypeCache     NodeType = "cache"
	NodeTypeQueue     NodeType = "queue"
)

// EdgeType represents the type of edge (connection) in the topology graph.
type EdgeType string

const (
	EdgeTypeHTTP     EdgeType = "http"
	EdgeTypeGRPC     EdgeType = "grpc"
	EdgeTypeTCP      EdgeType = "tcp"
	EdgeTypeDNS      EdgeType = "dns"
	EdgeTypeDatabase EdgeType = "database"
	EdgeTypeCache    EdgeType = "cache"
	EdgeTypeQueue    EdgeType = "queue"
)

// Topology represents the complete service topology.
type Topology struct {
	Nodes     []TopologyNode `json:"nodes" db:"nodes"`
	Edges     []TopologyEdge `json:"edges" db:"edges"`
	Timestamp time.Time      `json:"timestamp" db:"timestamp"`
	Version   uint64         `json:"version" db:"version"`
}

// TopologyNode represents a node in the topology graph.
type TopologyNode struct {
	ID        string            `json:"id" db:"id"`
	Name      string            `json:"name" db:"name"`
	Type      NodeType          `json:"type" db:"type"`
	AgentID   string            `json:"agent_id" db:"agent_id"`
	Labels    map[string]string `json:"labels" db:"labels"`
	Metadata  map[string]string `json:"metadata" db:"metadata"`
	Health    HealthStatus      `json:"health" db:"health"`
	LastSeen  time.Time         `json:"last_seen" db:"last_seen"`
	IPAddress string            `json:"ip_address,omitempty" db:"ip_address"`
	Port      uint32            `json:"port,omitempty" db:"port"`
}

// TopologyEdge represents an edge (connection) in the topology graph.
type TopologyEdge struct {
	ID       string            `json:"id" db:"id"`
	SourceID string            `json:"source_id" db:"source_id"`
	TargetID string            `json:"target_id" db:"target_id"`
	Type     EdgeType          `json:"type" db:"type"`
	Protocol string            `json:"protocol" db:"protocol"`
	Labels   map[string]string `json:"labels" db:"labels"`
	Metrics  EdgeMetrics       `json:"metrics" db:"metrics"`
	LastSeen time.Time         `json:"last_seen" db:"last_seen"`
}

// EdgeMetrics contains metrics for a topology edge.
type EdgeMetrics struct {
	RequestRate float64 `json:"request_rate" db:"request_rate"`
	ErrorRate   float64 `json:"error_rate" db:"error_rate"`
	LatencyP50  float64 `json:"latency_p50" db:"latency_p50"`
	LatencyP99  float64 `json:"latency_p99" db:"latency_p99"`
	BytesIn     uint64  `json:"bytes_in" db:"bytes_in"`
	BytesOut    uint64  `json:"bytes_out" db:"bytes_out"`
	ActiveConns uint64  `json:"active_connections" db:"active_connections"`
}

// HealthStatus represents the health status of a node.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// TopologyChange represents a change in the topology.
type TopologyChange struct {
	Type      string        `json:"type" db:"type"` // "node_added", "node_removed", "edge_added", "edge_removed"
	Node      *TopologyNode `json:"node,omitempty" db:"node"`
	Edge      *TopologyEdge `json:"edge,omitempty" db:"edge"`
	Timestamp time.Time     `json:"timestamp" db:"timestamp"`
}
