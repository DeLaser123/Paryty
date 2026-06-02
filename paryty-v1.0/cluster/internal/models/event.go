package models

import (
	"time"
)

// EventSeverity represents the severity level of an event.
type EventSeverity string

const (
	EventSeverityInfo     EventSeverity = "info"
	EventSeverityWarning  EventSeverity = "warning"
	EventSeverityError    EventSeverity = "error"
	EventSeverityCritical EventSeverity = "critical"
)

// EventCategory represents the category of an event.
type EventCategory string

const (
	EventCategorySystem     EventCategory = "system"
	EventCategoryNetwork    EventCategory = "network"
	EventCategorySecurity   EventCategory = "security"
	EventCategoryDeployment EventCategory = "deployment"
	EventCategoryCustom     EventCategory = "custom"
)

// Event represents a system event.
type Event struct {
	ID          string            `json:"id" db:"id"`
	AgentID     string            `json:"agent_id" db:"agent_id"`
	Source      string            `json:"source" db:"source"`
	Category    EventCategory     `json:"category" db:"category"`
	Severity    EventSeverity     `json:"severity" db:"severity"`
	Title       string            `json:"title" db:"title"`
	Description string            `json:"description" db:"description"`
	Labels      map[string]string `json:"labels" db:"labels"`
	Timestamp   time.Time         `json:"timestamp" db:"timestamp"`
	Duration    time.Duration     `json:"duration,omitempty" db:"duration"`
	RelatedIDs  []string          `json:"related_ids,omitempty" db:"related_ids"`
}

// NetworkEvent represents a network-level event observed via eBPF.
type NetworkEvent struct {
	AgentID   string      `json:"agent_id" db:"agent_id"`
	Timestamp time.Time   `json:"timestamp" db:"timestamp"`
	TCP       []TCPEvent  `json:"tcp,omitempty" db:"tcp"`
	DNS       []DNSEvent  `json:"dns,omitempty" db:"dns"`
	HTTP      []HTTPEvent `json:"http,omitempty" db:"http"`
	DB        []DBEvent   `json:"db,omitempty" db:"db"`
}

// TCPEvent represents a TCP connection event.
type TCPEvent struct {
	SrcIP     string `json:"src_ip" db:"src_ip"`
	DstIP     string `json:"dst_ip" db:"dst_ip"`
	SrcPort   uint32 `json:"src_port" db:"src_port"`
	DstPort   uint32 `json:"dst_port" db:"dst_port"`
	State     string `json:"state" db:"state"`
	BytesSent uint64 `json:"bytes_sent" db:"bytes_sent"`
	BytesRecv uint64 `json:"bytes_received" db:"bytes_received"`
}

// DNSEvent represents a DNS query event.
type DNSEvent struct {
	Query     string `json:"query" db:"query"`
	Response  string `json:"response" db:"response"`
	LatencyMs uint64 `json:"latency_ms" db:"latency_ms"`
	RCode     uint32 `json:"rcode" db:"rcode"`
}

// HTTPEvent represents an HTTP request/response event.
type HTTPEvent struct {
	Method    string `json:"method" db:"method"`
	Path      string `json:"path" db:"path"`
	Status    uint32 `json:"status" db:"status"`
	LatencyMs uint64 `json:"latency_ms" db:"latency_ms"`
	Host      string `json:"host" db:"host"`
}

// DBEvent represents a database query event.
type DBEvent struct {
	Database     string `json:"database" db:"database"`
	Query        string `json:"query" db:"query"`
	LatencyMs    uint64 `json:"latency_ms" db:"latency_ms"`
	RowsAffected uint64 `json:"rows_affected" db:"rows_affected"`
}
