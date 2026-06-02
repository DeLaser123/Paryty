package paryty

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// SpanStatus represents the status of a span.
type SpanStatus string

const (
	SpanStatusOK    SpanStatus = "ok"
	SpanStatusError SpanStatus = "error"
)

// SpanKind indicates the type of span.
type SpanKind string

const (
	SpanKindInternal SpanKind = "internal"
	SpanKindServer   SpanKind = "server"
	SpanKindClient   SpanKind = "client"
	SpanKindProducer SpanKind = "producer"
	SpanKindConsumer SpanKind = "consumer"
)

// Span represents a single operation within a trace.
type Span struct {
	mu          sync.Mutex
	TraceID     string            `json:"trace_id"`
	SpanID      string            `json:"span_id"`
	ParentID    string            `json:"parent_id,omitempty"`
	Name        string            `json:"name"`
	Kind        SpanKind          `json:"kind"`
	Status      SpanStatus        `json:"status"`
	StatusMsg   string            `json:"status_message,omitempty"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Duration    time.Duration     `json:"duration"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Events      []SpanEvent       `json:"events,omitempty"`
	ServiceName string            `json:"service_name"`
}

// SpanEvent is a timestamped event within a span.
type SpanEvent struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Trace is a collection of spans representing a distributed operation.
type Trace struct {
	TraceID     string  `json:"trace_id"`
	Spans       []*Span `json:"spans"`
	ServiceName string  `json:"service_name"`
}

// Tracer creates and manages spans.
type Tracer struct {
	mu          sync.RWMutex
	serviceName string
	spans       []*Span
	maxSpans    int
}

// TracerOption is a functional option for Tracer.
type TracerOption func(*Tracer)

// WithMaxSpans sets the maximum number of spans to buffer.
func WithMaxSpans(n int) TracerOption {
	return func(t *Tracer) {
		t.maxSpans = n
	}
}

// NewTracer creates a new tracer.
func NewTracer(serviceName string, opts ...TracerOption) *Tracer {
	t := &Tracer{
		serviceName: serviceName,
		maxSpans:    10000,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// StartSpan begins a new span. If the context contains a parent span, it becomes a child.
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	span := &Span{
		SpanID:      generateID(),
		Name:        name,
		Kind:        SpanKindInternal,
		Status:      SpanStatusOK,
		StartTime:   time.Now(),
		Attributes:  make(map[string]string),
		ServiceName: t.serviceName,
	}

	// Check for parent span in context
	if parent := spanFromContext(ctx); parent != nil {
		span.TraceID = parent.TraceID
		span.ParentID = parent.SpanID
	} else {
		span.TraceID = generateID()
	}

	// Apply options
	for _, opt := range opts {
		opt(span)
	}

	// Store span in tracer
	t.mu.Lock()
	if len(t.spans) < t.maxSpans {
		t.spans = append(t.spans, span)
	}
	t.mu.Unlock()

	return contextWithSpan(ctx, span), span
}

// Flush returns all collected spans and clears the buffer.
func (t *Tracer) Flush() []*Span {
	t.mu.Lock()
	spans := t.spans
	t.spans = make([]*Span, 0, len(spans))
	t.mu.Unlock()
	return spans
}

// Spans returns a copy of all buffered spans.
func (t *Tracer) Spans() []*Span {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]*Span, len(t.spans))
	copy(result, t.spans)
	return result
}

// SpanOption is a functional option for Span.
type SpanOption func(*Span)

// WithSpanKind sets the span kind.
func WithSpanKind(kind SpanKind) SpanOption {
	return func(s *Span) { s.Kind = kind }
}

// WithSpanAttributes sets span attributes.
func WithSpanAttributes(attrs map[string]string) SpanOption {
	return func(s *Span) {
		for k, v := range attrs {
			s.Attributes[k] = v
		}
	}
}

// WithSpanAttribute sets a single span attribute.
func WithSpanAttribute(key, value string) SpanOption {
	return func(s *Span) { s.Attributes[key] = value }
}

// End completes the span.
func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndTime = time.Now()
	s.Duration = s.EndTime.Sub(s.StartTime)
}

// SetStatus sets the span status.
func (s *Span) SetStatus(status SpanStatus, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	s.StatusMsg = message
}

// SetError marks the span as errored.
func (s *Span) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = SpanStatusError
	s.StatusMsg = err.Error()
}

// AddEvent adds an event to the span.
func (s *Span) AddEvent(name string, attrs map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

// SetAttribute sets a single attribute on the span.
func (s *Span) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Attributes[key] = value
}

// Context key for span propagation.
type spanContextKey struct{}

// contextWithSpan stores a span in the context.
func contextWithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, spanContextKey{}, span)
}

// spanFromContext extracts a span from the context.
func spanFromContext(ctx context.Context) *Span {
	if span, ok := ctx.Value(spanContextKey{}).(*Span); ok {
		return span
	}
	return nil
}

// SpanFromContext extracts a span from the context (public API).
func SpanFromContext(ctx context.Context) *Span {
	return spanFromContext(ctx)
}

// generateID generates a random 16-byte hex ID.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// TraceBatch holds a batch of traces for reporting.
type TraceBatch struct {
	Traces    []*Trace  `json:"traces"`
	Timestamp time.Time `json:"timestamp"`
}

// ToTraces converts buffered spans into Trace objects grouped by TraceID.
func (t *Tracer) ToTraces() []*Trace {
	spans := t.Spans()
	traceMap := make(map[string][]*Span)
	for _, span := range spans {
		traceMap[span.TraceID] = append(traceMap[span.TraceID], span)
	}

	traces := make([]*Trace, 0, len(traceMap))
	for traceID, traceSpans := range traceMap {
		traces = append(traces, &Trace{
			TraceID:     traceID,
			Spans:       traceSpans,
			ServiceName: t.serviceName,
		})
	}
	return traces
}
