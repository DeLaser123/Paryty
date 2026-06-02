package paryty

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType represents the type of a metric.
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
)

// Metric represents a base metric with common fields.
type Metric struct {
	Name        string            `json:"name"`
	Type        MetricType        `json:"type"`
	Description string            `json:"description,omitempty"`
	Unit        string            `json:"unit,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
}

// Counter is a monotonically increasing value.
type Counter struct {
	metric Metric
	value  atomic.Uint64
}

// NewCounter creates a new counter metric.
func NewCounter(name string, opts ...MetricOption) *Counter {
	m := applyOptions(name, MetricTypeCounter, opts)
	return &Counter{metric: m}
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.value.Add(1)
}

// Add increments the counter by the given value (must be positive).
func (c *Counter) Add(delta uint64) {
	c.value.Add(delta)
}

// Value returns the current counter value.
func (c *Counter) Value() uint64 {
	return c.value.Load()
}

// Snapshot returns a metric snapshot for reporting.
func (c *Counter) Snapshot() MetricSnapshot {
	return MetricSnapshot{
		Metric:    c.metric,
		Value:     float64(c.value.Load()),
		Timestamp: time.Now(),
	}
}

// Gauge is a value that can go up and down.
type Gauge struct {
	metric Metric
	mu     sync.RWMutex
	value  float64
}

// NewGauge creates a new gauge metric.
func NewGauge(name string, opts ...MetricOption) *Gauge {
	m := applyOptions(name, MetricTypeGauge, opts)
	return &Gauge{metric: m}
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(value float64) {
	g.mu.Lock()
	g.value = value
	g.mu.Unlock()
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	g.mu.Lock()
	g.value++
	g.mu.Unlock()
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	g.mu.Lock()
	g.value--
	g.mu.Unlock()
}

// Add adds the given value to the gauge (can be negative).
func (g *Gauge) Add(delta float64) {
	g.mu.Lock()
	g.value += delta
	g.mu.Unlock()
}

// Sub subtracts the given value from the gauge.
func (g *Gauge) Sub(delta float64) {
	g.mu.Lock()
	g.value -= delta
	g.mu.Unlock()
}

// Value returns the current gauge value.
func (g *Gauge) Value() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.value
}

// Snapshot returns a metric snapshot for reporting.
func (g *Gauge) Snapshot() MetricSnapshot {
	g.mu.RLock()
	v := g.value
	g.mu.RUnlock()
	return MetricSnapshot{
		Metric:    g.metric,
		Value:     v,
		Timestamp: time.Now(),
	}
}

// Histogram tracks the distribution of observed values.
type Histogram struct {
	metric    Metric
	mu        sync.Mutex
	count     uint64
	sum       float64
	min       float64
	max       float64
	buckets   []float64 // upper bounds
	bucketCnt []uint64  // counts per bucket
}

// DefaultBuckets are the default histogram bucket boundaries.
var DefaultBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
}

// NewHistogram creates a new histogram metric with the given bucket boundaries.
func NewHistogram(name string, opts ...MetricOption) *Histogram {
	m := applyOptions(name, MetricTypeHistogram, opts)
	buckets := DefaultBuckets
	for _, opt := range opts {
		if opt.buckets != nil {
			buckets = opt.buckets
		}
	}
	return &Histogram{
		metric:    m,
		min:       math.MaxFloat64,
		max:       -math.MaxFloat64,
		buckets:   buckets,
		bucketCnt: make([]uint64, len(buckets)+1), // +1 for +Inf bucket
	}
}

// Observe records a new observation.
func (h *Histogram) Observe(value float64) {
	h.mu.Lock()
	h.count++
	h.sum += value
	if value < h.min {
		h.min = value
	}
	if value > h.max {
		h.max = value
	}
	// Find the appropriate bucket
	placed := false
	for i, upper := range h.buckets {
		if value <= upper {
			h.bucketCnt[i]++
			placed = true
			break
		}
	}
	if !placed {
		h.bucketCnt[len(h.buckets)]++ // +Inf bucket
	}
	h.mu.Unlock()
}

// Count returns the number of observations.
func (h *Histogram) Count() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

// Sum returns the sum of all observations.
func (h *Histogram) Sum() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sum
}

// Mean returns the mean of all observations.
func (h *Histogram) Mean() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.sum / float64(h.count)
}

// Quantile estimates a quantile value from the histogram buckets.
func (h *Histogram) Quantile(q float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 || q < 0 || q > 1 {
		return 0
	}
	target := uint64(math.Ceil(q * float64(h.count)))
	var cumulative uint64
	for i, cnt := range h.bucketCnt {
		cumulative += cnt
		if cumulative >= target {
			if i < len(h.buckets) {
				return h.buckets[i]
			}
			return h.max
		}
	}
	return h.max
}

// Snapshot returns a metric snapshot for reporting.
func (h *Histogram) Snapshot() MetricSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	snap := MetricSnapshot{
		Metric: h.metric,
		Count:  h.count,
		Sum:    h.sum,
		Min:    h.min,
		Max:    h.max,
	}
	if h.count > 0 {
		snap.Mean = h.sum / float64(h.count)
	}
	// Compute quantiles from buckets
	snap.Quantiles = make(map[float64]float64)
	for _, q := range []float64{0.5, 0.9, 0.95, 0.99} {
		snap.Quantiles[q] = h.quantileLocked(q)
	}
	snap.Timestamp = time.Now()
	return snap
}

func (h *Histogram) quantileLocked(q float64) float64 {
	if h.count == 0 {
		return 0
	}
	target := uint64(math.Ceil(q * float64(h.count)))
	var cumulative uint64
	for i, cnt := range h.bucketCnt {
		cumulative += cnt
		if cumulative >= target {
			if i < len(h.buckets) {
				return h.buckets[i]
			}
			return h.max
		}
	}
	return h.max
}

// MetricSnapshot is a point-in-time view of a metric.
type MetricSnapshot struct {
	Metric    Metric
	Value     float64             // For counter and gauge
	Count     uint64              // For histogram
	Sum       float64             // For histogram
	Min       float64             // For histogram
	Max       float64             // For histogram
	Mean      float64             // For histogram
	Quantiles map[float64]float64 // For histogram (p50, p90, p99)
	Timestamp time.Time
}

// MetricOption is a functional option for metric creation.
type MetricOption struct {
	apply   func(*Metric)
	buckets []float64
}

// WithDescription sets the metric description.
func WithDescription(desc string) MetricOption {
	return MetricOption{
		apply: func(m *Metric) { m.Description = desc },
	}
}

// WithUnit sets the metric unit.
func WithUnit(unit string) MetricOption {
	return MetricOption{
		apply: func(m *Metric) { m.Unit = unit },
	}
}

// WithLabels sets the metric labels.
func WithLabels(labels map[string]string) MetricOption {
	return MetricOption{
		apply: func(m *Metric) { m.Labels = labels },
	}
}

// WithBuckets sets custom histogram bucket boundaries.
func WithBuckets(buckets []float64) MetricOption {
	return MetricOption{
		buckets: buckets,
	}
}

func applyOptions(name string, typ MetricType, opts []MetricOption) Metric {
	m := Metric{
		Name:      name,
		Type:      typ,
		Timestamp: time.Now(),
	}
	for _, opt := range opts {
		if opt.apply != nil {
			opt.apply(&m)
		}
	}
	return m
}

// Registry manages a collection of metrics.
type Registry struct {
	mu      sync.RWMutex
	metrics map[string]interface{} // Counter, Gauge, or Histogram
}

// NewRegistry creates a new metric registry.
func NewRegistry() *Registry {
	return &Registry{
		metrics: make(map[string]interface{}),
	}
}

// RegisterCounter registers a counter in the registry.
func (r *Registry) RegisterCounter(name string, opts ...MetricOption) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := NewCounter(name, opts...)
	r.metrics[name] = c
	return c
}

// RegisterGauge registers a gauge in the registry.
func (r *Registry) RegisterGauge(name string, opts ...MetricOption) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := NewGauge(name, opts...)
	r.metrics[name] = g
	return g
}

// RegisterHistogram registers a histogram in the registry.
func (r *Registry) RegisterHistogram(name string, opts ...MetricOption) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := NewHistogram(name, opts...)
	r.metrics[name] = h
	return h
}

// Snapshots returns snapshots of all registered metrics.
func (r *Registry) Snapshots() []MetricSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snaps := make([]MetricSnapshot, 0, len(r.metrics))
	for _, m := range r.metrics {
		switch v := m.(type) {
		case *Counter:
			snaps = append(snaps, v.Snapshot())
		case *Gauge:
			snaps = append(snaps, v.Snapshot())
		case *Histogram:
			snaps = append(snaps, v.Snapshot())
		}
	}
	return snaps
}

// String returns a human-readable representation of a metric snapshot.
func (s MetricSnapshot) String() string {
	switch s.Metric.Type {
	case MetricTypeCounter:
		return fmt.Sprintf("%s{counter}=%d", s.Metric.Name, uint64(s.Value))
	case MetricTypeGauge:
		return fmt.Sprintf("%s{gauge}=%.4f", s.Metric.Name, s.Value)
	case MetricTypeHistogram:
		return fmt.Sprintf("%s{histogram} count=%d sum=%.4f mean=%.4f", s.Metric.Name, s.Count, s.Sum, s.Mean)
	default:
		return fmt.Sprintf("%s=%.4f", s.Metric.Name, s.Value)
	}
}
