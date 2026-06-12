package processing

import "testing"

// =============================================================================
// allocDelta regression tests
//
// Pins the fix for the uint64 underflow in load-test memory accounting:
// when the heap SHRINKS between two MemStats.Alloc readings, naive
// subtraction wrapped to a near-2^64 value (observed as a bogus
// "17592186044415 MB" delta that failed the 2 GB budget assertion).
// =============================================================================

func TestAllocDelta_Growth(t *testing.T) {
	if got := allocDelta(1_000, 5_000); got != 4_000 {
		t.Fatalf("allocDelta(1000, 5000) = %d, want 4000", got)
	}
}

func TestAllocDelta_Shrink_ReturnsZero(t *testing.T) {
	// Heap shrank: GC reclaimed allocations made by earlier tests.
	// Must return 0, never underflow.
	if got := allocDelta(5_000, 1_000); got != 0 {
		t.Fatalf("allocDelta(5000, 1000) = %d, want 0 (underflow regression)", got)
	}
}

func TestAllocDelta_Equal(t *testing.T) {
	if got := allocDelta(42, 42); got != 0 {
		t.Fatalf("allocDelta(42, 42) = %d, want 0", got)
	}
}
