package processing

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"go.uber.org/zap"
)

// newTestLogger creates a development logger for tests.
func newTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// =============================================================================
// calcAvg tests
// =============================================================================

func TestCalcAvg(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{name: "empty slice", values: nil, expected: 0},
		{name: "single element", values: []float64{42.0}, expected: 42.0},
		{name: "multiple elements", values: []float64{10, 20, 30, 40, 50}, expected: 30.0},
		{name: "negative values", values: []float64{-10, -20, -30}, expected: -20.0},
		{name: "mixed positive and negative", values: []float64{-10, 10}, expected: 0.0},
		{name: "decimal values", values: []float64{1.5, 2.5, 3.5}, expected: 2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcAvg(tt.values)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcAvg(%v) = %f, want %f", tt.values, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// calcSum tests
// =============================================================================

func TestCalcSum(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{name: "empty slice", values: nil, expected: 0},
		{name: "single element", values: []float64{42.0}, expected: 42.0},
		{name: "multiple elements", values: []float64{1, 2, 3, 4, 5}, expected: 15.0},
		{name: "negative values", values: []float64{-1, -2, -3}, expected: -6.0},
		{name: "mixed positive and negative", values: []float64{10, -5, 3}, expected: 8.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcSum(tt.values)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcSum(%v) = %f, want %f", tt.values, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// calcMin tests
// =============================================================================

func TestCalcMin(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{name: "empty slice", values: nil, expected: 0},
		{name: "single element", values: []float64{42.0}, expected: 42.0},
		{name: "multiple elements", values: []float64{5, 3, 1, 4, 2}, expected: 1.0},
		{name: "negative values", values: []float64{-5, -3, -1}, expected: -5.0},
		{name: "mixed positive and negative", values: []float64{5, -3, 1}, expected: -3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcMin(tt.values)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcMin(%v) = %f, want %f", tt.values, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// calcMax tests
// =============================================================================

func TestCalcMax(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{name: "empty slice", values: nil, expected: 0},
		{name: "single element", values: []float64{42.0}, expected: 42.0},
		{name: "multiple elements", values: []float64{5, 3, 1, 4, 2}, expected: 5.0},
		{name: "negative values", values: []float64{-5, -3, -1}, expected: -1.0},
		{name: "mixed positive and negative", values: []float64{-5, 3, -1}, expected: 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcMax(tt.values)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcMax(%v) = %f, want %f", tt.values, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// calcPercentile tests
// =============================================================================

func TestCalcPercentile(t *testing.T) {
	tenValues := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}

	tests := []struct {
		name     string
		values   []float64
		p        float64
		expected float64
	}{
		{name: "empty slice", values: nil, p: 50, expected: 0},
		{name: "single element p50", values: []float64{42.0}, p: 50, expected: 42.0},
		{name: "p0 returns minimum", values: tenValues, p: 0, expected: 10.0},
		{name: "p100 returns maximum", values: tenValues, p: 100, expected: 100.0},
		{name: "p50 median odd count", values: tenValues, p: 50, expected: 55.0},
		{name: "p90 with 10 values", values: tenValues, p: 90, expected: 91.0},
		{name: "p below 0 clamped to 0", values: tenValues, p: -10, expected: 10.0},
		{name: "p above 100 clamped to 100", values: tenValues, p: 150, expected: 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcPercentile(tt.values, tt.p)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcPercentile(%v, %f) = %f, want %f", tt.values, tt.p, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// calcMedian tests
// =============================================================================

func TestCalcMedian(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{name: "empty slice", values: nil, expected: 0},
		{name: "odd count", values: []float64{1, 2, 3, 4, 5}, expected: 3.0},
		{name: "even count", values: []float64{1, 2, 3, 4}, expected: 2.5},
		{name: "single element", values: []float64{42.0}, expected: 42.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcMedian(tt.values)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcMedian(%v) = %f, want %f", tt.values, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// calcStddev tests
// =============================================================================

func TestCalcStddev(t *testing.T) {
	// Dataset: [2, 4, 4, 4, 5, 5, 7, 9]
	// Mean = 40/8 = 5.0
	// Sum of squared deviations = (3^2 + 1^2 + 1^2 + 1^2 + 0 + 0 + 2^2 + 4^2) = 9+1+1+1+0+0+4+16 = 32
	// Sample stddev = sqrt(32 / (8-1)) = sqrt(32/7) ≈ 2.138090
	expectedKnown := math.Sqrt(32.0 / 7.0)

	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{name: "empty slice", values: nil, expected: 0},
		{name: "single element", values: []float64{42.0}, expected: 0},
		{name: "two elements", values: []float64{0, 10}, expected: 7.0710678118654755},
		{name: "known dataset", values: []float64{2, 4, 4, 4, 5, 5, 7, 9}, expected: expectedKnown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcStddev(tt.values)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcStddev(%v) = %f, want %f", tt.values, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// RingBuffer tests
// =============================================================================

func TestRingBuffer_PushWithinCapacity(t *testing.T) {
	rb := NewRingBuffer(5)

	// Push 3 items (within capacity of 5).
	rb.Push("a")
	rb.Push("b")
	rb.Push("c")

	if got := rb.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}

	result := rb.GetAll()
	if len(result) != 3 {
		t.Fatalf("GetAll() returned %d items, want 3", len(result))
	}

	expected := []interface{}{"a", "b", "c"}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("GetAll()[%d] = %v, want %v", i, result[i], want)
		}
	}
}

func TestRingBuffer_PushBeyondCapacity(t *testing.T) {
	rb := NewRingBuffer(3)

	// Push 5 items into capacity-3 buffer.
	rb.Push("a")
	rb.Push("b")
	rb.Push("c")
	rb.Push("d") // overwrites "a"
	rb.Push("e") // overwrites "b"

	if got := rb.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}

	result := rb.GetAll()
	if len(result) != 3 {
		t.Fatalf("GetAll() returned %d items, want 3", len(result))
	}

	// Oldest remaining should be "c", "d", "e".
	expected := []interface{}{"c", "d", "e"}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("GetAll()[%d] = %v, want %v", i, result[i], want)
		}
	}
}

func TestRingBuffer_LenLifecycle(t *testing.T) {
	rb := NewRingBuffer(3)

	if got := rb.Len(); got != 0 {
		t.Fatalf("empty buffer Len() = %d, want 0", got)
	}

	rb.Push("a")
	if got := rb.Len(); got != 1 {
		t.Fatalf("after 1 push Len() = %d, want 1", got)
	}

	rb.Push("b")
	rb.Push("c")
	if got := rb.Len(); got != 3 {
		t.Fatalf("after 3 pushes Len() = %d, want 3", got)
	}

	// Push beyond capacity — Len should stay at capacity.
	rb.Push("d")
	if got := rb.Len(); got != 3 {
		t.Fatalf("after overflow push Len() = %d, want 3", got)
	}

	rb.Push("e")
	if got := rb.Len(); got != 3 {
		t.Fatalf("after second overflow push Len() = %d, want 3", got)
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer(5)

	for i := 0; i < 5; i++ {
		rb.Push(fmt.Sprintf("item-%d", i))
	}

	if got := rb.Len(); got != 5 {
		t.Fatalf("before Clear Len() = %d, want 5", got)
	}

	result := rb.GetAll()
	if len(result) != 5 {
		t.Fatalf("before Clear GetAll() = %d items, want 5", len(result))
	}

	rb.Clear()

	if got := rb.Len(); got != 0 {
		t.Fatalf("after Clear Len() = %d, want 0", got)
	}

	result = rb.GetAll()
	if result != nil {
		t.Fatalf("after Clear GetAll() = %v, want nil", result)
	}

	// Verify we can push again after clear.
	rb.Push("new-item")
	if got := rb.Len(); got != 1 {
		t.Fatalf("after re-push Len() = %d, want 1", got)
	}

	result = rb.GetAll()
	if len(result) != 1 || result[0] != "new-item" {
		t.Errorf("after re-push GetAll() = %v, want [new-item]", result)
	}
}

func TestRingBuffer_EmptyGetAll(t *testing.T) {
	rb := NewRingBuffer(5)

	result := rb.GetAll()
	if result != nil {
		t.Errorf("GetAll() on empty buffer = %v, want nil", result)
	}
}

func TestRingBuffer_ConcurrentPushGetAll(t *testing.T) {
	rb := NewRingBuffer(100)
	const goroutines = 20
	const itemsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	// Launch concurrent writers.
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < itemsPerGoroutine; i++ {
				rb.Push(fmt.Sprintf("g%d-i%d", id, i))
			}
		}(g)
	}

	// Launch concurrent readers while writers are active.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for i := 0; i < 100; i++ {
			_ = rb.GetAll()
			_ = rb.Len()
		}
	}()

	wg.Wait()
	<-readerDone

	// After all writes, buffer should be at capacity.
	if got := rb.Len(); got != 100 {
		t.Fatalf("after concurrent writes Len() = %d, want 100", got)
	}

	result := rb.GetAll()
	if len(result) != 100 {
		t.Fatalf("after concurrent writes GetAll() returned %d items, want 100", len(result))
	}

	// Verify no nil entries (all slots should have values).
	for i, item := range result {
		if item == nil {
			t.Errorf("GetAll()[%d] is nil, expected non-nil", i)
		}
	}
}

func TestRingBuffer_ConcurrentPushClear(t *testing.T) {
	rb := NewRingBuffer(50)
	const goroutines = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				rb.Push(fmt.Sprintf("g%d-i%d", id, i))
			}
		}(g)
	}

	// Clear during concurrent writes — should not panic.
	rb.Clear()

	wg.Wait()

	// Buffer state is non-deterministic after concurrent access with clear,
	// but Len() should always be ≤ capacity.
	if got := rb.Len(); got > 50 {
		t.Fatalf("Len() = %d exceeds capacity of 50", got)
	}
}

func TestRingBuffer_SingleCapacity(t *testing.T) {
	rb := NewRingBuffer(1)

	rb.Push("first")
	if got := rb.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}
	result := rb.GetAll()
	if len(result) != 1 || result[0] != "first" {
		t.Errorf("GetAll() = %v, want [first]", result)
	}

	rb.Push("second")
	if got := rb.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}
	result = rb.GetAll()
	if len(result) != 1 || result[0] != "second" {
		t.Errorf("GetAll() = %v, want [second]", result)
	}
}

// =============================================================================
// TestCalcPercentile_With256Values
// Verify percentile accuracy with the max window buffer capacity (256 values).
// =============================================================================

func TestCalcPercentile_With256Values(t *testing.T) {
	t.Parallel()

	// Build a sorted sequence [0, 1, 2, ..., 255].
	values := make([]float64, 256)
	for i := range values {
		values[i] = float64(i)
	}

	tests := []struct {
		name     string
		p        float64
		expected float64
	}{
		{"p50", 50, 127.5},
		{"p90", 90, 229.5},
		{"p95", 95, 242.25},
		{"p99", 99, 252.45},
		{"p0", 0, 0},
		{"p100", 100, 255},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := calcPercentile(values, tt.p)
			if math.Abs(got-tt.expected) > 0.01 {
				t.Errorf("calcPercentile(0..255, %f) = %f, want %f", tt.p, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// TestCalcPercentileSorted
// Verify in-place percentile calculation on pre-sorted data.
// =============================================================================

func TestCalcPercentileSorted(t *testing.T) {
	t.Parallel()

	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	tests := []struct {
		name     string
		p        float64
		expected float64
	}{
		{"p0 returns minimum", 0, 1.0},
		{"p50 median", 50, 5.5},
		{"p90", 90, 9.1},
		{"p100 returns maximum", 100, 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := calcPercentileSorted(sorted, tt.p)
			if math.Abs(got-tt.expected) > 0.0001 {
				t.Errorf("calcPercentileSorted(sorted, %f) = %f, want %f", tt.p, got, tt.expected)
			}
		})
	}
}

func TestCalcPercentileSorted_Empty(t *testing.T) {
	t.Parallel()

	got := calcPercentileSorted(nil, 50)
	if got != 0 {
		t.Errorf("calcPercentileSorted(nil, 50) = %f, want 0", got)
	}

	got = calcPercentileSorted([]float64{}, 50)
	if got != 0 {
		t.Errorf("calcPercentileSorted(empty, 50) = %f, want 0", got)
	}
}

func TestCalcPercentileSorted_MatchesCalcPercentile(t *testing.T) {
	t.Parallel()

	// Both functions should produce identical results for sorted input.
	sorted := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	percentiles := []float64{0, 10, 25, 50, 75, 90, 95, 99, 100}

	for _, p := range percentiles {
		fromUnsorted := calcPercentile(sorted, p)
		fromSorted := calcPercentileSorted(sorted, p)
		if math.Abs(fromUnsorted-fromSorted) > 0.0001 {
			t.Errorf("p=%f: calcPercentile=%f, calcPercentileSorted=%f", p, fromUnsorted, fromSorted)
		}
	}
}

// =============================================================================
// TestRingBuffer_DrainAll
// Verify atomic drain-and-clear behavior.
// =============================================================================

func TestRingBuffer_DrainAll(t *testing.T) {
	t.Parallel()

	rb := NewRingBuffer(5)

	rb.Push("a")
	rb.Push("b")
	rb.Push("c")

	result := rb.DrainAll()
	if len(result) != 3 {
		t.Fatalf("DrainAll() returned %d items, want 3", len(result))
	}
	expected := []interface{}{"a", "b", "c"}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("DrainAll()[%d] = %v, want %v", i, result[i], want)
		}
	}

	// After drain, buffer should be empty.
	if got := rb.Len(); got != 0 {
		t.Errorf("Len() after DrainAll = %d, want 0", got)
	}
	if rb.GetAll() != nil {
		t.Error("GetAll() after DrainAll should return nil")
	}

	// Verify we can push again after drain.
	rb.Push("new")
	if got := rb.Len(); got != 1 {
		t.Errorf("Len() after re-push = %d, want 1", got)
	}
}

func TestRingBuffer_DrainAll_Empty(t *testing.T) {
	t.Parallel()

	rb := NewRingBuffer(5)

	result := rb.DrainAll()
	if result != nil {
		t.Errorf("DrainAll() on empty buffer = %v, want nil", result)
	}
}

func TestRingBuffer_DrainAll_WithOverflow(t *testing.T) {
	t.Parallel()

	rb := NewRingBuffer(3)

	rb.Push("a")
	rb.Push("b")
	rb.Push("c")
	rb.Push("d") // overwrites "a"
	rb.Push("e") // overwrites "b"

	result := rb.DrainAll()
	if len(result) != 3 {
		t.Fatalf("DrainAll() returned %d items, want 3", len(result))
	}

	// Should contain the 3 most recent items.
	expected := []interface{}{"c", "d", "e"}
	for i, want := range expected {
		if result[i] != want {
			t.Errorf("DrainAll()[%d] = %v, want %v", i, result[i], want)
		}
	}

	// Buffer should be empty.
	if got := rb.Len(); got != 0 {
		t.Errorf("Len() after DrainAll = %d, want 0", got)
	}
}

func TestRingBuffer_DrainAll_Concurrent(t *testing.T) {
	t.Parallel()

	rb := NewRingBuffer(100)

	// Fill the buffer.
	for i := 0; i < 100; i++ {
		rb.Push(i)
	}

	// Concurrent DrainAll from multiple goroutines — only one should get items.
	const goroutines = 10
	results := make(chan []interface{}, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			results <- rb.DrainAll()
		}()
	}
	wg.Wait()
	close(results)

	// Exactly one goroutine should have received 100 items; the rest get nil.
	nonNilCount := 0
	for r := range results {
		if r != nil {
			nonNilCount++
			if len(r) != 100 {
				t.Errorf("DrainAll() returned %d items, want 100", len(r))
			}
		}
	}
	if nonNilCount != 1 {
		t.Errorf("expected exactly 1 non-nil DrainAll result, got %d", nonNilCount)
	}
}

// =============================================================================
// TestRingBuffer_AtDefaultEventBufferSize
// Verify RingBuffer works correctly at the defaultEventBufferSize (1000).
// =============================================================================

func TestRingBuffer_AtDefaultEventBufferSize(t *testing.T) {
	t.Parallel()

	const capacity = 1000 // defaultEventBufferSize
	rb := NewRingBuffer(capacity)

	// Push 1500 items — 500 should be evicted.
	for i := 0; i < 1500; i++ {
		rb.Push(i)
	}

	if got := rb.Len(); got != capacity {
		t.Fatalf("Len() = %d, want %d", got, capacity)
	}

	result := rb.GetAll()
	if len(result) != capacity {
		t.Fatalf("GetAll() returned %d items, want %d", len(result), capacity)
	}

	// First item should be 500 (oldest 0-499 evicted).
	if result[0] != 500 {
		t.Errorf("GetAll()[0] = %v, want 500", result[0])
	}
	// Last item should be 1499.
	if result[capacity-1] != 1499 {
		t.Errorf("GetAll()[%d] = %v, want 1499", capacity-1, result[capacity-1])
	}
}

