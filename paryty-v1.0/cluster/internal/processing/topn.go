// Package processing implements the data processing pipeline.
// It includes aggregator, correlator, and enricher services.
package processing

import (
	"container/heap"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TopNResult is a ranked entry in a Top-N result set.
// It is JSON-tagged for direct serialization to API responses.
type TopNResult struct {
	Rank      int           `json:"rank"`
	AgentID   string        `json:"agent_id"`
	Name      string        `json:"name"`
	Category  string        `json:"category"`
	Value     float64       `json:"value"`
	Window    time.Duration `json:"window"`
	Timestamp time.Time     `json:"timestamp"`
}

// TopNEntry is a single entry tracked by the Top-N heap.
// It stores the metric identity, current value, and timestamp.
type TopNEntry struct {
	Name      string
	AgentID   string
	Value     float64
	Timestamp time.Time
}

// minHeap implements container/heap.Interface for a slice of TopNEntry.
// It maintains a min-heap ordered by Value so that the smallest entry
// is always at index 0 for O(1) eviction checks.
//
// The heap also maintains an external index map so that existing entries
// can be found and updated in O(log N) time.
type minHeap struct {
	entries []TopNEntry
	index   map[string]int // name → position in entries
}

// Len returns the number of entries in the heap.
func (h *minHeap) Len() int { return len(h.entries) }

// Less compares two entries by Value for min-heap ordering.
func (h *minHeap) Less(i, j int) bool {
	return h.entries[i].Value < h.entries[j].Value
}

// Swap exchanges two entries and updates the index map.
func (h *minHeap) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	h.index[h.entries[i].Name] = i
	h.index[h.entries[j].Name] = j
}

// Push appends an entry and updates the index map.
// Called by container/heap.Push.
func (h *minHeap) Push(x interface{}) {
	entry := x.(TopNEntry)
	h.index[entry.Name] = len(h.entries)
	h.entries = append(h.entries, entry)
}

// Pop removes and returns the last entry (after heap reordering, this is
// the entry that container/heap moves to the end).
// Called by container/heap.Pop.
func (h *minHeap) Pop() interface{} {
	old := h.entries
	n := len(old)
	entry := old[n-1]
	h.entries = old[:n-1]
	delete(h.index, entry.Name)
	return entry
}

// update modifies the value of an existing entry at index i and
// re-establishes the heap invariant.
func (h *minHeap) update(i int, entry TopNEntry) {
	h.entries[i] = entry
	heap.Fix(h, i)
}

// categoryTracker tracks the Top-N entries for a single metric category
// (e.g., "cpu.process", "memory.process").
type categoryTracker struct {
	category string
	heap     minHeap
	maxSize  int
	dirty    map[string]bool // names changed since last GetChanged
}

// newCategoryTracker creates a category tracker for the given category with
// capacity for maxSize entries.
func newCategoryTracker(category string, maxSize int) *categoryTracker {
	return &categoryTracker{
		category: category,
		heap: minHeap{
			entries: make([]TopNEntry, 0, maxSize),
			index:   make(map[string]int, maxSize),
		},
		maxSize: maxSize,
		dirty:   make(map[string]bool),
	}
}

// update adds or updates an entry in the category tracker.
//
// Algorithm:
//  1. If the entry already exists (same name), update its value and fix the heap.
//  2. If the heap is not full, push the new entry.
//  3. If the heap is full and the new value exceeds the minimum, replace the minimum.
//  4. Otherwise, the new value is too small — ignore it.
func (ct *categoryTracker) update(name, agentID string, value float64, timestamp time.Time) {
	// Case 1: Entry already exists — update in place.
	if idx, ok := ct.heap.index[name]; ok {
		ct.heap.update(idx, TopNEntry{
			Name:      name,
			AgentID:   agentID,
			Value:     value,
			Timestamp: timestamp,
		})
		ct.dirty[name] = true
		return
	}

	entry := TopNEntry{
		Name:      name,
		AgentID:   agentID,
		Value:     value,
		Timestamp: timestamp,
	}

	// Case 2: Heap not full — just push.
	if ct.heap.Len() < ct.maxSize {
		heap.Push(&ct.heap, entry)
		ct.dirty[name] = true
		return
	}

	// Case 3: Heap full — replace minimum if new value is larger.
	if value > ct.heap.entries[0].Value {
		// Remove the old minimum from the dirty set (it's being evicted).
		evicted := ct.heap.entries[0].Name
		delete(ct.dirty, evicted)

		// Replace the root with the new entry and fix the heap.
		ct.heap.entries[0] = entry
		ct.heap.index[name] = 0
		// Remove evicted entry from index.
		delete(ct.heap.index, evicted)
		heap.Fix(&ct.heap, 0)
		ct.dirty[name] = true
	}
	// Case 4: New value is smaller than the minimum — drop it.
}

// getTopN returns the current top N entries sorted by value descending.
// The result includes rank, category, and window metadata.
func (ct *categoryTracker) getTopN(window time.Duration) []TopNResult {
	if ct.heap.Len() == 0 {
		return nil
	}

	// Copy entries to avoid mutating the heap during sort.
	sorted := make([]TopNEntry, ct.heap.Len())
	copy(sorted, ct.heap.entries)

	// Sort descending by value.
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	results := make([]TopNResult, len(sorted))
	for i, e := range sorted {
		results[i] = TopNResult{
			Rank:      i + 1,
			AgentID:   e.AgentID,
			Name:      e.Name,
			Category:  ct.category,
			Value:     e.Value,
			Window:    window,
			Timestamp: e.Timestamp,
		}
	}

	return results
}

// getChanged returns entries that were modified since the last call.
// After building the result, the dirty set is cleared.
func (ct *categoryTracker) getChanged(window time.Duration) []TopNResult {
	if len(ct.dirty) == 0 {
		return nil
	}

	var results []TopNResult
	for i, e := range ct.heap.entries {
		if ct.dirty[e.Name] {
			results = append(results, TopNResult{
				Rank:      i + 1,
				AgentID:   e.AgentID,
				Name:      e.Name,
				Category:  ct.category,
				Value:     e.Value,
				Window:    window,
				Timestamp: e.Timestamp,
			})
		}
	}

	// Sort descending by value for consistent ranking.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Value > results[j].Value
	})

	// Re-assign ranks after sort.
	for i := range results {
		results[i].Rank = i + 1
	}

	// Clear dirty set.
	ct.dirty = make(map[string]bool)

	return results
}

// TopNTracker manages Top-N rankings across multiple metric categories.
//
// It is used by the Aggregator to track the top N processes by CPU, memory,
// disk, and network usage. Each category is tracked independently.
//
// Thread-safe. All public methods are safe for concurrent use.
type TopNTracker struct {
	n        int
	trackers map[string]*categoryTracker
	mu       sync.RWMutex
	logger   *zap.Logger
	window   time.Duration
}

// NewTopNTracker creates a Top-N tracker that ranks the top n entries
// per category. Categories must be specified upfront (e.g., "cpu.process",
// "memory.process").
//
// The tracker defaults to a 1-minute window for result metadata.
func NewTopNTracker(n int, categories []string, logger *zap.Logger) *TopNTracker {
	if logger == nil {
		logger = zap.NewNop()
	}

	trackers := make(map[string]*categoryTracker, len(categories))
	for _, cat := range categories {
		trackers[cat] = newCategoryTracker(cat, n)
	}

	return &TopNTracker{
		n:        n,
		trackers: trackers,
		logger:   logger,
		window:   time.Minute,
	}
}

// SetWindow sets the window duration reported in TopNResult.Window.
func (t *TopNTracker) SetWindow(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.window = d
}

// Update adds or updates an entry in the specified category.
//
// If an entry with the same name already exists, its value is updated.
// If the category is full and the new value exceeds the current minimum,
// the minimum entry is evicted and replaced.
//
// This method is safe for concurrent use.
func (t *TopNTracker) Update(category, name, agentID string, value float64, timestamp time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tracker, ok := t.trackers[category]
	if !ok {
		t.logger.Warn("Update for unknown category",
			zap.String("category", category),
		)
		return
	}

	tracker.update(name, agentID, value, timestamp)
}

// GetTopN returns the current top N entries for the given category,
// sorted by value in descending order.
//
// Returns nil if the category has no entries or does not exist.
// This method is safe for concurrent use.
func (t *TopNTracker) GetTopN(category string) []TopNResult {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tracker, ok := t.trackers[category]
	if !ok {
		return nil
	}

	return tracker.getTopN(t.window)
}

// GetChanged returns all entries that were modified since the last call
// to GetChanged, grouped by category. The dirty set is cleared after
// this call.
//
// Returns nil if no entries have changed.
// This method is safe for concurrent use.
func (t *TopNTracker) GetChanged() map[string][]TopNResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[string][]TopNResult)
	for cat, tracker := range t.trackers {
		changed := tracker.getChanged(t.window)
		if len(changed) > 0 {
			result[cat] = changed
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}
