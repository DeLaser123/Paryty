// Package processing implements the data processing pipeline.
// It includes aggregator, correlator, and enricher services.
package processing

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// =============================================================================
// TestTopN_BasicRanking
// N=5, send 10 processes with different CPU values, verify top 5 returned
// in correct order.
// =============================================================================

func TestTopN_BasicRanking(t *testing.T) {
	tracker := NewTopNTracker(5, []string{"cpu.process"}, zap.NewNop())

	// Add 10 processes with values 10..100.
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("proc-%d", i*10)
		tracker.Update("cpu.process", name, "agent-1", float64(i*10), time.Now())
	}

	results := tracker.GetTopN("cpu.process")
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	// Verify descending order: 100, 90, 80, 70, 60.
	expectedValues := []float64{100, 90, 80, 70, 60}
	for i, want := range expectedValues {
		if results[i].Value != want {
			t.Errorf("rank %d: expected value %f, got %f", i+1, want, results[i].Value)
		}
		if results[i].Rank != i+1 {
			t.Errorf("rank %d: expected rank %d, got %d", i+1, i+1, results[i].Rank)
		}
		if results[i].Category != "cpu.process" {
			t.Errorf("rank %d: expected category 'cpu.process', got %q", i+1, results[i].Category)
		}
	}
}

// =============================================================================
// TestTopN_UpdateExisting
// N=3, add A=10 B=20 C=30, update A to 100, verify A=100 C=30 B=20
// =============================================================================

func TestTopN_UpdateExisting(t *testing.T) {
	tracker := NewTopNTracker(3, []string{"cpu.process"}, zap.NewNop())
	now := time.Now()

	tracker.Update("cpu.process", "A", "agent-1", 10, now)
	tracker.Update("cpu.process", "B", "agent-1", 20, now)
	tracker.Update("cpu.process", "C", "agent-1", 30, now)

	// Update A to 100.
	tracker.Update("cpu.process", "A", "agent-1", 100, now)

	results := tracker.GetTopN("cpu.process")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Expected order: A=100, C=30, B=20.
	expectedNames := []string{"A", "C", "B"}
	expectedValues := []float64{100, 30, 20}
	for i, wantName := range expectedNames {
		if results[i].Name != wantName {
			t.Errorf("rank %d: expected name %q, got %q", i+1, wantName, results[i].Name)
		}
		if results[i].Value != expectedValues[i] {
			t.Errorf("rank %d: expected value %f, got %f", i+1, expectedValues[i], results[i].Value)
		}
	}
}

// =============================================================================
// TestTopN_NewEntryBeatsOld
// N=2, add A=10 B=20, add C=30 → A evicted. Update A to 100 → B evicted.
// =============================================================================

func TestTopN_NewEntryBeatsOld(t *testing.T) {
	tracker := NewTopNTracker(2, []string{"cpu.process"}, zap.NewNop())
	now := time.Now()

	tracker.Update("cpu.process", "A", "agent-1", 10, now)
	tracker.Update("cpu.process", "B", "agent-1", 20, now)

	// C=30 beats A=10 (the minimum). A should be evicted.
	tracker.Update("cpu.process", "C", "agent-1", 30, now)

	results := tracker.GetTopN("cpu.process")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Should contain B=20 and C=30.
	names := map[string]float64{}
	for _, r := range results {
		names[r.Name] = r.Value
	}
	if _, ok := names["A"]; ok {
		t.Error("A should have been evicted")
	}
	if v, ok := names["B"]; !ok || v != 20 {
		t.Errorf("expected B=20, got %v", v)
	}
	if v, ok := names["C"]; !ok || v != 30 {
		t.Errorf("expected C=30, got %v", v)
	}

	// Now update A to 100 — A re-enters and evicts B=20 (new minimum).
	tracker.Update("cpu.process", "A", "agent-1", 100, now)

	results = tracker.GetTopN("cpu.process")
	if len(results) != 2 {
		t.Fatalf("expected 2 results after re-entry, got %d", len(results))
	}

	names = map[string]float64{}
	for _, r := range results {
		names[r.Name] = r.Value
	}
	if v, ok := names["A"]; !ok || v != 100 {
		t.Errorf("expected A=100, got %v", v)
	}
	if v, ok := names["C"]; !ok || v != 30 {
		t.Errorf("expected C=30, got %v", v)
	}
	if _, ok := names["B"]; ok {
		t.Error("B should have been evicted")
	}
}

// =============================================================================
// TestTopN_GetChanged
// Add entries, verify GetChanged returns them. Call again, verify empty.
// Add more, verify new changes.
// =============================================================================

func TestTopN_GetChanged(t *testing.T) {
	tracker := NewTopNTracker(5, []string{"cpu.process"}, zap.NewNop())
	now := time.Now()

	// No changes yet.
	changed := tracker.GetChanged()
	if changed != nil {
		t.Fatalf("expected nil before any updates, got %v", changed)
	}

	// Add two entries.
	tracker.Update("cpu.process", "A", "agent-1", 10, now)
	tracker.Update("cpu.process", "B", "agent-1", 20, now)

	changed = tracker.GetChanged()
	if changed == nil {
		t.Fatal("expected changes, got nil")
	}
	if len(changed["cpu.process"]) != 2 {
		t.Errorf("expected 2 changed entries, got %d", len(changed["cpu.process"]))
	}

	// Call again — should be empty.
	changed = tracker.GetChanged()
	if changed != nil {
		t.Fatalf("expected nil after consuming changes, got %v", changed)
	}

	// Update an existing entry.
	tracker.Update("cpu.process", "A", "agent-1", 50, now)

	changed = tracker.GetChanged()
	if changed == nil {
		t.Fatal("expected changes after update, got nil")
	}
	if len(changed["cpu.process"]) != 1 {
		t.Errorf("expected 1 changed entry, got %d", len(changed["cpu.process"]))
	}
	if changed["cpu.process"][0].Name != "A" || changed["cpu.process"][0].Value != 50 {
		t.Errorf("expected changed entry A=50, got %+v", changed["cpu.process"][0])
	}
}

// =============================================================================
// TestTopN_MultipleCategories
// Verify independent tracking across categories (cpu.process vs memory.process).
// =============================================================================

func TestTopN_MultipleCategories(t *testing.T) {
	categories := []string{"cpu.process", "memory.process"}
	tracker := NewTopNTracker(3, categories, zap.NewNop())
	now := time.Now()

	// Add entries to cpu.process.
	tracker.Update("cpu.process", "nginx", "agent-1", 80, now)
	tracker.Update("cpu.process", "postgres", "agent-1", 60, now)
	tracker.Update("cpu.process", "redis", "agent-1", 40, now)

	// Add entries to memory.process.
	tracker.Update("memory.process", "java-app", "agent-1", 512, now)
	tracker.Update("memory.process", "postgres", "agent-1", 256, now)
	tracker.Update("memory.process", "redis", "agent-1", 128, now)

	// Verify cpu.process results.
	cpuResults := tracker.GetTopN("cpu.process")
	if len(cpuResults) != 3 {
		t.Fatalf("expected 3 cpu results, got %d", len(cpuResults))
	}
	if cpuResults[0].Name != "nginx" || cpuResults[0].Value != 80 {
		t.Errorf("cpu rank 1: expected nginx=80, got %s=%f", cpuResults[0].Name, cpuResults[0].Value)
	}

	// Verify memory.process results.
	memResults := tracker.GetTopN("memory.process")
	if len(memResults) != 3 {
		t.Fatalf("expected 3 memory results, got %d", len(memResults))
	}
	if memResults[0].Name != "java-app" || memResults[0].Value != 512 {
		t.Errorf("memory rank 1: expected java-app=512, got %s=%f", memResults[0].Name, memResults[0].Value)
	}

	// Verify categories are independent.
	if cpuResults[0].Category != "cpu.process" {
		t.Errorf("expected category 'cpu.process', got %q", cpuResults[0].Category)
	}
	if memResults[0].Category != "memory.process" {
		t.Errorf("expected category 'memory.process', got %q", memResults[0].Category)
	}

	// Verify GetChanged returns both categories.
	changed := tracker.GetChanged()
	if changed == nil {
		t.Fatal("expected changes, got nil")
	}
	if len(changed) != 2 {
		t.Errorf("expected 2 categories in changes, got %d", len(changed))
	}
	if len(changed["cpu.process"]) != 3 {
		t.Errorf("expected 3 cpu changes, got %d", len(changed["cpu.process"]))
	}
	if len(changed["memory.process"]) != 3 {
		t.Errorf("expected 3 memory changes, got %d", len(changed["memory.process"]))
	}
}

// =============================================================================
// TestTopN_EmptyCategory
// GetTopN on unused category returns empty.
// =============================================================================

func TestTopN_EmptyCategory(t *testing.T) {
	tracker := NewTopNTracker(5, []string{"cpu.process", "memory.process"}, zap.NewNop())

	// No updates — should return nil.
	results := tracker.GetTopN("cpu.process")
	if results != nil {
		t.Errorf("expected nil for empty category, got %v", results)
	}

	results = tracker.GetTopN("memory.process")
	if results != nil {
		t.Errorf("expected nil for empty category, got %v", results)
	}

	// Unknown category — should return nil.
	results = tracker.GetTopN("disk.process")
	if results != nil {
		t.Errorf("expected nil for unknown category, got %v", results)
	}
}

// =============================================================================
// TestTopN_ConcurrentUpdates
// Goroutine safety with race detector.
// =============================================================================

func TestTopN_ConcurrentUpdates(t *testing.T) {
	categories := []string{"cpu.process", "memory.process", "disk.process"}
	tracker := NewTopNTracker(10, categories, zap.NewNop())

	const goroutines = 20
	const updatesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * len(categories))

	// Launch concurrent writers across all categories.
	for _, cat := range categories {
		for g := 0; g < goroutines; g++ {
			go func(category string, id int) {
				defer wg.Done()
				for i := 0; i < updatesPerGoroutine; i++ {
					name := fmt.Sprintf("proc-%d", id*1000+i)
					value := float64(id*1000 + i)
					tracker.Update(category, name, fmt.Sprintf("agent-%d", id), value, time.Now())
				}
			}(cat, g)
		}
	}

	// Launch concurrent readers while writers are active.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for i := 0; i < 100; i++ {
			for _, cat := range categories {
				_ = tracker.GetTopN(cat)
			}
			_ = tracker.GetChanged()
		}
	}()

	wg.Wait()
	<-readerDone

	// Verify each category has at most 10 entries (the N limit).
	for _, cat := range categories {
		results := tracker.GetTopN(cat)
		if len(results) > 10 {
			t.Errorf("category %s: expected at most 10 results, got %d", cat, len(results))
		}
		if len(results) == 0 {
			t.Errorf("category %s: expected at least 1 result, got 0", cat)
		}

		// Verify descending order.
		for i := 1; i < len(results); i++ {
			if results[i].Value > results[i-1].Value {
				t.Errorf("category %s: rank %d value %f > rank %d value %f",
					cat, i+1, results[i].Value, i, results[i-1].Value)
			}
		}
	}
}

// =============================================================================
// TestTopN_SetWindow
// Verify window duration is propagated to results.
// =============================================================================

func TestTopN_SetWindow(t *testing.T) {
	tracker := NewTopNTracker(3, []string{"cpu.process"}, zap.NewNop())

	tracker.SetWindow(5 * time.Minute)
	tracker.Update("cpu.process", "proc-1", "agent-1", 50, time.Now())

	results := tracker.GetTopN("cpu.process")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Window != 5*time.Minute {
		t.Errorf("expected window 5m, got %v", results[0].Window)
	}
}

// =============================================================================
// TestTopN_UpdateAllEntries
// N=3, fill to capacity, then update each entry to a higher value.
// Verify all entries are still tracked correctly.
// =============================================================================

func TestTopN_UpdateAllEntries(t *testing.T) {
	tracker := NewTopNTracker(3, []string{"cpu.process"}, zap.NewNop())
	now := time.Now()

	tracker.Update("cpu.process", "A", "agent-1", 10, now)
	tracker.Update("cpu.process", "B", "agent-1", 20, now)
	tracker.Update("cpu.process", "C", "agent-1", 30, now)

	// Update all entries to higher values.
	tracker.Update("cpu.process", "A", "agent-1", 100, now)
	tracker.Update("cpu.process", "B", "agent-1", 200, now)
	tracker.Update("cpu.process", "C", "agent-1", 300, now)

	results := tracker.GetTopN("cpu.process")
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Expected: C=300, B=200, A=100.
	expected := []struct {
		name  string
		value float64
	}{
		{"C", 300},
		{"B", 200},
		{"A", 100},
	}

	for i, want := range expected {
		if results[i].Name != want.name || results[i].Value != want.value {
			t.Errorf("rank %d: expected %s=%f, got %s=%f",
				i+1, want.name, want.value, results[i].Name, results[i].Value)
		}
	}
}

// =============================================================================
// TestTopN_NilLogger
// Verify tracker works with nil logger (defaults to nop).
// =============================================================================

func TestTopN_NilLogger(t *testing.T) {
	tracker := NewTopNTracker(3, []string{"cpu.process"}, nil)

	tracker.Update("cpu.process", "proc-1", "agent-1", 50, time.Now())

	results := tracker.GetTopN("cpu.process")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Value != 50 {
		t.Errorf("expected value 50, got %f", results[0].Value)
	}
}
