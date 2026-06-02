package models

import (
	"time"
)

// TraceStatus represents the status of a trace span.
type TraceStatus string

const (
	TraceStatusOK    TraceStatus = "ok"
	TraceStatusError TraceStatus = "error"
	TraceStatusUnset TraceStatus = "unset"
)

// SpanKind represents the kind of span.
type SpanKind string

const (
	SpanKindInternal SpanKind = "internal"
	SpanKindServer   SpanKind = "server"
	SpanKindClient   SpanKind = "client"
	SpanKindProducer SpanKind = "producer"
	SpanKindConsumer SpanKind = "consumer"
)

// Trace represents a complete distributed trace.
type Trace struct {
	TraceID    string        `json:"trace_id" db:"trace_id"`
	RootSpanID string        `json:"root_span_id" db:"root_span_id"`
	Spans      []Span        `json:"spans" db:"spans"`
	StartTime  time.Time     `json:"start_time" db:"start_time"`
	EndTime    time.Time     `json:"end_time" db:"end_time"`
	Duration   time.Duration `json:"duration" db:"duration"`
	Services   []string      `json:"services" db:"services"`
	Status     TraceStatus   `json:"status" db:"status"`
}

// Span represents a single operation within a trace.
type Span struct {
	TraceID       string            `json:"trace_id" db:"trace_id"`
	SpanID        string            `json:"span_id" db:"span_id"`
	ParentSpanID  string            `json:"parent_span_id,omitempty" db:"parent_span_id"`
	Name          string            `json:"name" db:"name"`
	Kind          SpanKind          `json:"kind" db:"kind"`
	ServiceName   string            `json:"service_name" db:"service_name"`
	StartTime     time.Time         `json:"start_time" db:"start_time"`
	EndTime       time.Time         `json:"end_time" db:"end_time"`
	Duration      time.Duration     `json:"duration" db:"duration"`
	Status        TraceStatus       `json:"status" db:"status"`
	StatusCode    string            `json:"status_code,omitempty" db:"status_code"`
	StatusMessage string            `json:"status_message,omitempty" db:"status_message"`
	Attributes    map[string]string `json:"attributes" db:"attributes"`
	Events        []SpanEvent       `json:"events,omitempty" db:"events"`
	Links         []SpanLink        `json:"links,omitempty" db:"links"`
}

// SpanEvent represents an event within a span.
type SpanEvent struct {
	Name       string            `json:"name" db:"name"`
	Timestamp  time.Time         `json:"timestamp" db:"timestamp"`
	Attributes map[string]string `json:"attributes" db:"attributes"`
}

// SpanLink represents a link to another span.
type SpanLink struct {
	TraceID    string            `json:"trace_id" db:"trace_id"`
	SpanID     string            `json:"span_id" db:"span_id"`
	Attributes map[string]string `json:"attributes" db:"attributes"`
}

// BusinessTrace represents a business logic trace for custom instrumentation.
type BusinessTrace struct {
	ID          string            `json:"id" db:"id"`
	Name        string            `json:"name" db:"name"`
	Description string            `json:"description,omitempty" db:"description"`
	StartTime   time.Time         `json:"start_time" db:"start_time"`
	EndTime     time.Time         `json:"end_time" db:"end_time"`
	Duration    time.Duration     `json:"duration" db:"duration"`
	Status      TraceStatus       `json:"status" db:"status"`
	Metadata    map[string]string `json:"metadata" db:"metadata"`
	AgentID     string            `json:"agent_id" db:"agent_id"`
}
