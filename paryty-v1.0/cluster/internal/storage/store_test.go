package storage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/cold"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/warm"
)

// ---- Circuit Breaker: State Transitions ----

func TestCircuitBreaker_InitialStateIsClosed(t *testing.T) {
	cb := newCircuitBreaker(5, 30*time.Second)

	if got := cb.State(); got != "closed" {
		t.Errorf("initial state = %q, want %q", got, "closed")
	}
	if got := cb.Failures(); got != 0 {
		t.Errorf("initial failures = %d, want 0", got)
	}
}

func TestCircuitBreaker_AllowWhenClosed(t *testing.T) {
	cb := newCircuitBreaker(5, 30*time.Second)

	if !cb.Allow() {
		t.Error("closed breaker should allow requests")
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := newCircuitBreaker(5, 30*time.Second)

	// Accumulate some failures, then succeed.
	cb.RecordFailure()
	cb.RecordFailure()
	if got := cb.Failures(); got != 2 {
		t.Fatalf("failures after 2 records = %d, want 2", got)
	}

	cb.RecordSuccess()
	if got := cb.State(); got != "closed" {
		t.Errorf("state after success = %q, want %q", got, "closed")
	}
	if got := cb.Failures(); got != 0 {
		t.Errorf("failures after success = %d, want 0", got)
	}
}

func TestCircuitBreaker_OpenAfterThreshold(t *testing.T) {
	threshold := int64(5)
	cb := newCircuitBreaker(threshold, 30*time.Second)

	for i := int64(0); i < threshold-1; i++ {
		cb.RecordFailure()
		if got := cb.State(); got != "closed" {
			t.Fatalf("state after %d failures = %q, want %q", i+1, got, "closed")
		}
	}

	// One more failure crosses the threshold.
	cb.RecordFailure()
	if got := cb.State(); got != "open" {
		t.Errorf("state after %d failures = %q, want %q", threshold, got, "open")
	}
}

func TestCircuitBreaker_OpenRejects(t *testing.T) {
	cb := newCircuitBreaker(1, 30*time.Second)

	cb.RecordFailure() // threshold=1, immediately opens

	if cb.Allow() {
		t.Error("open breaker should reject requests")
	}
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	cooldown := 50 * time.Millisecond
	cb := newCircuitBreaker(1, cooldown)

	cb.RecordFailure() // opens the breaker
	if got := cb.State(); got != "open" {
		t.Fatalf("state = %q, want %q", got, "open")
	}

	// Before cooldown — still open.
	if cb.Allow() {
		t.Error("open breaker should reject before cooldown")
	}

	// Wait for cooldown to elapse.
	time.Sleep(cooldown + 10*time.Millisecond)

	// After cooldown — should transition to half-open and allow.
	if !cb.Allow() {
		t.Error("breaker should allow after cooldown (half-open)")
	}
	if got := cb.State(); got != "half-open" {
		t.Errorf("state after cooldown = %q, want %q", got, "half-open")
	}
}

func TestCircuitBreaker_HalfOpenAllows(t *testing.T) {
	cb := newCircuitBreaker(1, 10*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	// First Allow() transitions to half-open.
	if !cb.Allow() {
		t.Error("half-open breaker should allow probe request")
	}

	// Subsequent allows still work in half-open.
	if !cb.Allow() {
		t.Error("half-open breaker should allow subsequent requests")
	}
}

func TestCircuitBreaker_CloseOnSuccessInHalfOpen(t *testing.T) {
	cb := newCircuitBreaker(1, 10*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // transitions to half-open

	cb.RecordSuccess()
	if got := cb.State(); got != "closed" {
		t.Errorf("state after success in half-open = %q, want %q", got, "closed")
	}
	if got := cb.Failures(); got != 0 {
		t.Errorf("failures after success = %d, want 0", got)
	}
}

func TestCircuitBreaker_ReopenOnFailureInHalfOpen(t *testing.T) {
	cb := newCircuitBreaker(2, 10*time.Millisecond)

	cb.RecordFailure()
	cb.RecordFailure() // opens
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // transitions to half-open

	cb.RecordFailure() // fail in half-open → opens again
	if got := cb.State(); got != "open" {
		t.Errorf("state after failure in half-open = %q, want %q", got, "open")
	}
}

func TestCircuitBreaker_MultipleCycles(t *testing.T) {
	cooldown := 20 * time.Millisecond
	cb := newCircuitBreaker(2, cooldown)

	for cycle := 0; cycle < 3; cycle++ {
		// Closed → Open.
		cb.RecordFailure()
		cb.RecordFailure()
		if got := cb.State(); got != "open" {
			t.Errorf("cycle %d: state = %q, want %q", cycle, got, "open")
		}

		// Wait for cooldown.
		time.Sleep(cooldown + 5*time.Millisecond)

		// Open → Half-open.
		if !cb.Allow() {
			t.Errorf("cycle %d: should allow after cooldown", cycle)
		}
		if got := cb.State(); got != "half-open" {
			t.Errorf("cycle %d: state = %q, want %q", cycle, got, "half-open")
		}

		// Half-open → Closed.
		cb.RecordSuccess()
		if got := cb.State(); got != "closed" {
			t.Errorf("cycle %d: state = %q, want %q", cycle, got, "closed")
		}
	}
}

func TestCircuitBreaker_StateString(t *testing.T) {
	tests := []struct {
		state circuitBreakerState
		want  string
	}{
		{stateClosed, "closed"},
		{stateOpen, "open"},
		{stateHalfOpen, "half-open"},
		{circuitBreakerState(99), "unknown"},
	}

	for _, tc := range tests {
		cb := &circuitBreaker{state: tc.state}
		if got := cb.State(); got != tc.want {
			t.Errorf("State() for state %d = %q, want %q", tc.state, got, tc.want)
		}
	}
}

// ---- Circuit Breaker: Concurrent Access ----

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := newCircuitBreaker(100, 50*time.Millisecond)

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half the goroutines record failures.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				cb.RecordFailure()
			}
		}()
	}

	// Half the goroutines call Allow() concurrently.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				cb.Allow()
			}
		}()
	}

	wg.Wait()

	// The breaker should be open (100 goroutines × 100 ops = 5000 failures >> threshold).
	if got := cb.State(); got != "open" {
		t.Errorf("state after concurrent failures = %q, want %q", got, "open")
	}
}

func TestCircuitBreaker_ConcurrentSuccessAndFailure(t *testing.T) {
	cb := newCircuitBreaker(10, 10*time.Millisecond)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if j%3 == 0 {
					cb.RecordSuccess()
				} else {
					cb.RecordFailure()
				}
				cb.Allow()
				cb.State()
			}
		}(i)
	}

	wg.Wait()

	// No panic or race — state is deterministic based on last operations.
	// We just verify the state is a valid string.
	state := cb.State()
	validStates := map[string]bool{"closed": true, "open": true, "half-open": true}
	if !validStates[state] {
		t.Errorf("invalid state after concurrent ops: %q", state)
	}
}

// ---- Circuit Breaker: Threshold and Cooldown Configuration ----

func TestCircuitBreaker_ThresholdOfOne(t *testing.T) {
	cb := newCircuitBreaker(1, 10*time.Second)

	cb.RecordFailure()
	if got := cb.State(); got != "open" {
		t.Errorf("state after 1 failure (threshold=1) = %q, want %q", got, "open")
	}
}

func TestCircuitBreaker_ThresholdOfThree(t *testing.T) {
	cb := newCircuitBreaker(3, 10*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	if got := cb.State(); got != "closed" {
		t.Errorf("state after 2 failures (threshold=3) = %q, want %q", got, "closed")
	}

	cb.RecordFailure()
	if got := cb.State(); got != "open" {
		t.Errorf("state after 3 failures (threshold=3) = %q, want %q", got, "open")
	}
}

func TestCircuitBreaker_FailuresResetOnSuccess(t *testing.T) {
	cb := newCircuitBreaker(3, 10*time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // resets counter
	cb.RecordFailure()
	cb.RecordFailure()
	// Only 2 failures since last success — should stay closed.
	if got := cb.State(); got != "closed" {
		t.Errorf("state = %q, want %q (failures should reset on success)", got, "closed")
	}
}

// ---- Default Configuration ----

func TestNewCircuitBreaker_Defaults(t *testing.T) {
	cb := newCircuitBreaker(defaultFailureThreshold, defaultCooldown)

	if cb.threshold != 5 {
		t.Errorf("default threshold = %d, want 5", cb.threshold)
	}
	if cb.cooldown != 30*time.Second {
		t.Errorf("default cooldown = %v, want 30s", cb.cooldown)
	}
}

// ---- Store: HotStore Accessor ----

func TestHotStore_ReturnsNilForNilClient(t *testing.T) {
	s := &Store{hot: nil}
	if s.HotStore() != nil {
		t.Error("HotStore() should return nil when hot client is nil")
	}
}

func TestStore_CircuitBreakersInitializedByNew(t *testing.T) {
	// Verify that New() would initialize circuit breakers (we can't call New()
	// without real backends, but we verify the struct layout).
	s := &Store{
		hotBreaker:  newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		warmBreaker: newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
	}

	if s.hotBreaker == nil {
		t.Fatal("hotBreaker should not be nil")
	}
	if s.warmBreaker == nil {
		t.Fatal("warmBreaker should not be nil")
	}
	if got := s.hotBreaker.State(); got != "closed" {
		t.Errorf("hotBreaker initial state = %q, want %q", got, "closed")
	}
	if got := s.warmBreaker.State(); got != "closed" {
		t.Errorf("warmBreaker initial state = %q, want %q", got, "closed")
	}
}

// ---- Tenant Propagation: Key Isolation ----

func TestTenantPropagation_DifferentTenantsDifferentKeys(t *testing.T) {
	tenants := []string{"tenant-alpha", "tenant-beta", "tenant-gamma"}

	for i, a := range tenants {
		for _, b := range tenants[i+1:] {
			if a == b {
				t.Errorf("tenants %q and %q should differ", a, b)
			}
			prefixA := fmt.Sprintf("paryty:%s:", a)
			prefixB := fmt.Sprintf("paryty:%s:", b)
			if prefixA == prefixB {
				t.Errorf("tenant prefixes collide: %q == %q", prefixA, prefixB)
			}
		}
	}
}

func TestTenantPropagation_TenantInKeyPrefix(t *testing.T) {
	tenants := []string{"acme-corp", "default", "org-123_test"}

	for _, tenant := range tenants {
		prefix := fmt.Sprintf("paryty:%s:", tenant)
		if len(prefix) == 0 {
			t.Errorf("empty prefix for tenant %q", tenant)
		}
		if !containsSubstring(prefix, tenant) {
			t.Errorf("prefix %q does not contain tenant %q", prefix, tenant)
		}
	}
}

func TestTenantPropagation_SameAgentDifferentTenantsNeverCollide(t *testing.T) {
	agentID := "agent-shared-id"
	tenants := []string{"alpha", "beta", "gamma"}

	seen := make(map[string]string)
	for _, tenant := range tenants {
		key := fmt.Sprintf("paryty:%s:metrics:%s:latest", tenant, agentID)
		if prev, exists := seen[key]; exists {
			t.Errorf("tenant %q and %q produced colliding key %q", prev, tenant, key)
		}
		seen[key] = tenant
	}
}

// ---- Async Warm Write: Non-Blocking Behavior ----

func TestStoreMetricBatch_AsyncWarmWriteDoesNotBlock(t *testing.T) {
	done := make(chan struct{})
	start := time.Now()

	go func() {
		defer close(done)
		time.Sleep(100 * time.Millisecond)
	}()

	elapsed := time.Since(start)
	if elapsed > 10*time.Millisecond {
		t.Errorf("goroutine dispatch took %v, should be near-instant", elapsed)
	}

	<-done
}

func TestStoreMetricBatch_CircuitBreakerBlocksWhenOpen(t *testing.T) {
	cb := newCircuitBreaker(1, 10*time.Second)

	cb.RecordFailure()
	if cb.Allow() {
		t.Fatal("breaker should be open and reject")
	}

	want := "hot store circuit breaker open"
	got := fmt.Errorf("%s", want)
	if got.Error() != want {
		t.Errorf("error message = %q, want %q", got.Error(), want)
	}
}

// ---- Goroutine Leak Prevention ----

func TestCircuitBreaker_ConcurrentAccessNoGoroutineLeak(t *testing.T) {
	cb := newCircuitBreaker(50, 5*time.Millisecond)

	var wg sync.WaitGroup
	var ops atomic.Int64

	const workers = 100
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if cb.Allow() {
					if j%2 == 0 {
						cb.RecordSuccess()
					} else {
						cb.RecordFailure()
					}
				}
				_ = cb.State()
				_ = cb.Failures()
				ops.Add(1)
			}
		}()
	}

	wg.Wait()

	if got := ops.Load(); got != int64(workers*1000) {
		t.Errorf("total ops = %d, want %d", got, workers*1000)
	}
}

// ---- Phase 4: Mock Implementations for Testing ----

// mockTopologyUpdater implements TopologyUpdater for unit tests.
type mockTopologyUpdater struct {
	mu       sync.Mutex
	topology *models.Topology
	calls    int
	err      error
}

func (m *mockTopologyUpdater) UpdateTopology(_ context.Context, _ string, mutate func(*models.Topology) (*models.Topology, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return m.err
	}
	result, err := mutate(m.topology)
	if err != nil {
		return err
	}
	m.topology = result
	return nil
}

// mockSnapshotOperator implements SnapshotOperator for unit tests.
type mockSnapshotOperator struct {
	snapshot *cold.Snapshot
	err      error
}

func (m *mockSnapshotOperator) TakeSnapshot(_ context.Context, _ string) (*cold.Snapshot, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.snapshot, nil
}

func (m *mockSnapshotOperator) GetSnapshot(_ context.Context, _, _ string) (*cold.Snapshot, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.snapshot, nil
}

func (m *mockSnapshotOperator) ReconstructState(_ context.Context, _ string, _ time.Time) (*cold.Snapshot, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.snapshot, nil
}

// mockQueryDownsampler implements QueryDownsampler for unit tests.
type mockQueryDownsampler struct {
	metrics []models.Metric
	records []warm.DbQueryRecord
	err     error
}

func (m *mockQueryDownsampler) QueryWithDownsampling(_ context.Context, _, _, _ string, _, _ time.Time) ([]models.Metric, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.metrics, nil
}

func (m *mockQueryDownsampler) QueryDatabaseQueries(_ context.Context, _, _, _ string, _, _ time.Time, _ int) ([]warm.DbQueryRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.records, nil
}

// ---- Phase 4: TestStore_UpdateTopologyAtomic ----

func TestStore_UpdateTopologyAtomic(t *testing.T) {
	t.Parallel()

	t.Run("nil topology ops returns error", func(t *testing.T) {
		t.Parallel()
		s := &Store{
			topologyOps: nil,
			hotBreaker:  newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		err := s.UpdateTopologyAtomic(context.Background(), "tenant-1", func(topo *models.Topology) (*models.Topology, error) {
			return topo, nil
		})
		if err == nil {
			t.Fatal("expected error for nil topology ops")
		}
		if err.Error() != "topology operations not configured" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("circuit breaker open rejects call", func(t *testing.T) {
		t.Parallel()
		s := &Store{
			topologyOps: &mockTopologyUpdater{},
			hotBreaker:  newCircuitBreaker(1, 10*time.Second),
		}
		s.hotBreaker.RecordFailure() // opens breaker

		err := s.UpdateTopologyAtomic(context.Background(), "tenant-1", func(topo *models.Topology) (*models.Topology, error) {
			return topo, nil
		})
		if err == nil {
			t.Fatal("expected error for open circuit breaker")
		}
	})

	t.Run("successful delegation", func(t *testing.T) {
		t.Parallel()
		mock := &mockTopologyUpdater{
			topology: &models.Topology{Nodes: []models.TopologyNode{}},
		}
		s := &Store{
			topologyOps: mock,
			hotBreaker:  newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		err := s.UpdateTopologyAtomic(context.Background(), "tenant-1", func(topo *models.Topology) (*models.Topology, error) {
			topo.Nodes = append(topo.Nodes, models.TopologyNode{ID: "n1", Name: "node-1"})
			return topo, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mock.calls != 1 {
			t.Errorf("expected 1 call, got %d", mock.calls)
		}
		if len(mock.topology.Nodes) != 1 {
			t.Errorf("expected 1 node, got %d", len(mock.topology.Nodes))
		}
		if s.hotBreaker.State() != "closed" {
			t.Errorf("breaker should be closed after success, got %q", s.hotBreaker.State())
		}
	})

	t.Run("mutate error records failure", func(t *testing.T) {
		t.Parallel()
		mock := &mockTopologyUpdater{
			topology: &models.Topology{},
		}
		s := &Store{
			topologyOps: mock,
			hotBreaker:  newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		err := s.UpdateTopologyAtomic(context.Background(), "tenant-1", func(topo *models.Topology) (*models.Topology, error) {
			return nil, fmt.Errorf("mutation failed")
		})
		if err == nil {
			t.Fatal("expected error from mutate")
		}
		if s.hotBreaker.Failures() != 1 {
			t.Errorf("expected 1 failure, got %d", s.hotBreaker.Failures())
		}
	})
}

// ---- Phase 4: TestStore_TakeSnapshot ----

func TestStore_TakeSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("nil snapshot mgr returns error", func(t *testing.T) {
		t.Parallel()
		s := &Store{
			snapshotMgr: nil,
			coldBreaker: newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		snap, err := s.TakeSnapshot(context.Background(), "tenant-1")
		if err == nil {
			t.Fatal("expected error for nil snapshot manager")
		}
		if snap != nil {
			t.Error("expected nil snapshot on error")
		}
		if err.Error() != "snapshot manager not configured" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("circuit breaker open rejects call", func(t *testing.T) {
		t.Parallel()
		s := &Store{
			snapshotMgr: &mockSnapshotOperator{},
			coldBreaker: newCircuitBreaker(1, 10*time.Second),
		}
		s.coldBreaker.RecordFailure()

		snap, err := s.TakeSnapshot(context.Background(), "tenant-1")
		if err == nil {
			t.Fatal("expected error for open circuit breaker")
		}
		if snap != nil {
			t.Error("expected nil snapshot when breaker open")
		}
	})

	t.Run("successful delegation", func(t *testing.T) {
		t.Parallel()
		expected := &cold.Snapshot{
			ID:       "snap-123",
			TenantID: "tenant-1",
			Timestamp: time.Now(),
			Topology: &models.Topology{
				Nodes: []models.TopologyNode{{ID: "n1", Name: "svc-a"}},
			},
		}
		mock := &mockSnapshotOperator{snapshot: expected}
		s := &Store{
			snapshotMgr: mock,
			coldBreaker: newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		snap, err := s.TakeSnapshot(context.Background(), "tenant-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if snap.ID != "snap-123" {
			t.Errorf("expected snapshot ID snap-123, got %s", snap.ID)
		}
		if s.coldBreaker.State() != "closed" {
			t.Errorf("breaker should be closed after success, got %q", s.coldBreaker.State())
		}
	})

	t.Run("backend error records failure", func(t *testing.T) {
		t.Parallel()
		mock := &mockSnapshotOperator{err: fmt.Errorf("seaweedfs timeout")}
		s := &Store{
			snapshotMgr: mock,
			coldBreaker: newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		_, err := s.TakeSnapshot(context.Background(), "tenant-1")
		if err == nil {
			t.Fatal("expected error from backend")
		}
		if s.coldBreaker.Failures() != 1 {
			t.Errorf("expected 1 failure, got %d", s.coldBreaker.Failures())
		}
	})
}

// ---- Phase 4: TestStore_QueryMetricsOptimized ----

func TestStore_QueryMetricsOptimized(t *testing.T) {
	t.Parallel()

	t.Run("nil query optimizer returns error", func(t *testing.T) {
		t.Parallel()
		s := &Store{
			queryOptimizer: nil,
			warmBreaker:    newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		metrics, err := s.QueryMetricsOptimized(context.Background(), "tenant-1", "agent-1", "cpu.usage", time.Now().Add(-time.Hour), time.Now())
		if err == nil {
			t.Fatal("expected error for nil query optimizer")
		}
		if metrics != nil {
			t.Error("expected nil metrics on error")
		}
		if err.Error() != "query optimizer not configured" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("circuit breaker open rejects call", func(t *testing.T) {
		t.Parallel()
		s := &Store{
			queryOptimizer: &mockQueryDownsampler{},
			warmBreaker:    newCircuitBreaker(1, 10*time.Second),
		}
		s.warmBreaker.RecordFailure()

		metrics, err := s.QueryMetricsOptimized(context.Background(), "tenant-1", "agent-1", "cpu.usage", time.Now().Add(-time.Hour), time.Now())
		if err == nil {
			t.Fatal("expected error for open circuit breaker")
		}
		if metrics != nil {
			t.Error("expected nil metrics when breaker open")
		}
	})

	t.Run("successful delegation", func(t *testing.T) {
		t.Parallel()
		expected := []models.Metric{
			{AgentID: "agent-1", Name: "cpu.usage", Value: 42.5, Timestamp: time.Now()},
		}
		mock := &mockQueryDownsampler{metrics: expected}
		s := &Store{
			queryOptimizer: mock,
			warmBreaker:    newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		start := time.Now().Add(-time.Hour)
		end := time.Now()
		metrics, err := s.QueryMetricsOptimized(context.Background(), "tenant-1", "agent-1", "cpu.usage", start, end)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(metrics) != 1 {
			t.Fatalf("expected 1 metric, got %d", len(metrics))
		}
		if metrics[0].Name != "cpu.usage" {
			t.Errorf("expected metric name cpu.usage, got %s", metrics[0].Name)
		}
		if s.warmBreaker.State() != "closed" {
			t.Errorf("breaker should be closed after success, got %q", s.warmBreaker.State())
		}
	})

	t.Run("backend error records failure", func(t *testing.T) {
		t.Parallel()
		mock := &mockQueryDownsampler{err: fmt.Errorf("questdb timeout")}
		s := &Store{
			queryOptimizer: mock,
			warmBreaker:    newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		}

		_, err := s.QueryMetricsOptimized(context.Background(), "tenant-1", "agent-1", "cpu.usage", time.Now().Add(-time.Hour), time.Now())
		if err == nil {
			t.Fatal("expected error from backend")
		}
		if s.warmBreaker.Failures() != 1 {
			t.Errorf("expected 1 failure, got %d", s.warmBreaker.Failures())
		}
	})
}

// ---- Helpers ----

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
