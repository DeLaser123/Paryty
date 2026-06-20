// Package processing implements the data processing pipeline.
// It includes aggregator, correlator, and enricher services.
package processing

import (
	"math"
	"sort"
	"strings"
	"sync"
)

// calcAvg computes the arithmetic mean of a slice of float64.
// Returns 0 for empty slices.
func calcAvg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return calcSum(values) / float64(len(values))
}

// calcSum computes the sum of a slice of float64.
func calcSum(values []float64) float64 {
	var total float64
	for _, v := range values {
		total += v
	}
	return total
}

// calcMin returns the minimum value in a slice of float64.
// Returns 0 for empty slices.
func calcMin(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

// calcMax returns the maximum value in a slice of float64.
// Returns 0 for empty slices.
func calcMax(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// calcPercentile computes the p-th percentile using linear interpolation.
// p must be in [0, 100]. Returns 0 for empty slices.
// Uses the nearest-rank method.
func calcPercentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	if p == 0 {
		return sorted[0]
	}
	if p == 100 {
		return sorted[len(sorted)-1]
	}

	rank := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}

	fraction := rank - float64(lower)
	return sorted[lower] + fraction*(sorted[upper]-sorted[lower])
}

// calcPercentileSorted computes the p-th percentile from an already-sorted
// slice using linear interpolation. This avoids redundant sorting when
// multiple percentiles are needed from the same data.
// p must be in [0, 100]. Returns 0 for empty slices.
func calcPercentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}

	if p == 0 {
		return sorted[0]
	}
	if p == 100 {
		return sorted[len(sorted)-1]
	}

	rank := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}

	fraction := rank - float64(lower)
	return sorted[lower] + fraction*(sorted[upper]-sorted[lower])
}

// calcMedian computes the 50th percentile.
func calcMedian(values []float64) float64 {
	return calcPercentile(values, 50)
}

// calcStddev computes the sample standard deviation.
// Returns 0 for slices with fewer than 2 elements.
func calcStddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := calcAvg(values)
	var sumSq float64
	for _, v := range values {
		diff := v - mean
		sumSq += diff * diff
	}
	return math.Sqrt(sumSq / float64(len(values)-1))
}

// RingBuffer is a fixed-size circular buffer.
//
// Thread-safe. When full, oldest entries are overwritten.
// Used for buffering network events in the correlator.
type RingBuffer struct {
	data  []interface{}
	size  int
	head  int
	tail  int
	count int
	mu    sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the given capacity.
//
// Parameters:
//   - size: maximum number of items the buffer can hold.
//
// Returns an initialized RingBuffer.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]interface{}, size),
		size: size,
	}
}

// Push adds an item to the ring buffer.
// If the buffer is full, the oldest item is overwritten.
func (rb *RingBuffer) Push(item interface{}) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.data[rb.head] = item
	rb.head = (rb.head + 1) % rb.size

	if rb.count == rb.size {
		// Buffer full — overwrite oldest by advancing tail.
		rb.tail = (rb.tail + 1) % rb.size
	} else {
		rb.count++
	}
}

// GetAll returns a copy of all items in the buffer, oldest first.
func (rb *RingBuffer) GetAll() []interface{} {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return nil
	}

	result := make([]interface{}, rb.count)
	for i := 0; i < rb.count; i++ {
		idx := (rb.tail + i) % rb.size
		result[i] = rb.data[idx]
	}
	return result
}

// Len returns the current number of items in the buffer.
func (rb *RingBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Clear removes all items from the buffer.
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.tail = 0
	rb.count = 0
	// Clear references to allow GC.
	for i := range rb.data {
		rb.data[i] = nil
	}
}

// DrainAll returns all items in the buffer and clears it atomically.
// Returns nil if the buffer is empty. The returned slice contains items
// in order from oldest to newest, identical to GetAll ordering.
//
// Thread-safe. The write lock is held for both read and clear to prevent
// races with concurrent Push calls.
func (rb *RingBuffer) DrainAll() []interface{} {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if rb.count == 0 {
		return nil
	}
	result := make([]interface{}, rb.count)
	for i := 0; i < rb.count; i++ {
		idx := (rb.tail + i) % rb.size
		result[i] = rb.data[idx]
		rb.data[idx] = nil
	}
	rb.head = 0
	rb.tail = 0
	rb.count = 0
	return result
}

// extractTwinFromTopic extracts the 12-char twin ID prefix from a twin-scoped
// topic name. Returns empty string if the topic is not twin-scoped.
//
// Format: paryty.<tenant>.twins.<twin_id_prefix>.<suffix>
func extractTwinFromTopic(topic string) string {
	parts := strings.Split(topic, ".")
	if len(parts) >= 4 && parts[2] == "twins" {
		return parts[3]
	}
	return ""
}
