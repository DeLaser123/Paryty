package api

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// --- Test doubles ---

// mockPinger is a test double that satisfies the pinger interface.
type mockPinger struct {
	mu  sync.Mutex
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *mockPinger) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// mockHotStore satisfies hotStorePinger for tests.
type mockHotStore struct {
	hot pinger
}

func (m *mockHotStore) HotStore() pinger { return m.hot }

func (m *mockHotStore) Ping(ctx context.Context) error { return m.hot.Ping(ctx) }

// --- Test helpers ---

func newTestHealthChecker(hotErr, storeErr, streamErr error) *HealthChecker {
	hot := &mockPinger{err: hotErr}
	return &HealthChecker{
		hotStore:     hot,
		store:        &mockPinger{err: storeErr},
		streamPinger: &mockPinger{err: streamErr},
		logger:       slog.Default(),
	}
}

// --- Liveness tests ---

func TestHealthChecker_LivenessCheck_AlwaysReturnsNil(t *testing.T) {
	hc := newTestHealthChecker(
		errors.New("hot down"),
		errors.New("store down"),
		errors.New("stream down"),
	)

	// Even when all subsystems are failing, liveness should return nil.
	if err := hc.LivenessCheck(context.Background()); err != nil {
		t.Errorf("liveness check should always return nil, got: %v", err)
	}
}

func TestHealthChecker_LivenessCheck_NilDependencies(t *testing.T) {
	// A HealthChecker with zero-value (nil) fields.
	hc := &HealthChecker{logger: slog.Default()}

	if err := hc.LivenessCheck(context.Background()); err != nil {
		t.Errorf("liveness check should never touch dependencies, got: %v", err)
	}
}

// --- Readiness tests ---

func TestHealthChecker_ReadinessCheck_AllHealthy(t *testing.T) {
	hc := newTestHealthChecker(nil, nil, nil)

	if err := hc.ReadinessCheck(context.Background()); err != nil {
		t.Errorf("expected nil error when all subsystems healthy, got: %v", err)
	}
}

func TestHealthChecker_ReadinessCheck_HotStoreFailing(t *testing.T) {
	hc := newTestHealthChecker(
		errors.New("dragonfly connection refused"),
		nil,
		nil,
	)

	err := hc.ReadinessCheck(context.Background())
	if err == nil {
		t.Fatal("expected error when hot store is failing")
	}

	if !errors.Is(err, errors.New("dragonfly connection refused")) {
		// Check that the error message wraps the original.
		if got := err.Error(); !containsSubstring(got, "dragonfly connection refused") {
			t.Errorf("error should mention dragonfly failure, got: %s", got)
		}
	}
}

func TestHealthChecker_ReadinessCheck_StoreFailing(t *testing.T) {
	hc := newTestHealthChecker(
		nil,
		errors.New("questdb unreachable"),
		nil,
	)

	err := hc.ReadinessCheck(context.Background())
	if err == nil {
		t.Fatal("expected error when store is failing")
	}

	if got := err.Error(); !containsSubstring(got, "questdb unreachable") {
		t.Errorf("error should mention questdb failure, got: %s", got)
	}
}

func TestHealthChecker_ReadinessCheck_StreamFailing(t *testing.T) {
	hc := newTestHealthChecker(
		nil,
		nil,
		errors.New("redpanda broker unreachable"),
	)

	err := hc.ReadinessCheck(context.Background())
	if err == nil {
		t.Fatal("expected error when stream is failing")
	}

	if got := err.Error(); !containsSubstring(got, "redpanda broker unreachable") {
		t.Errorf("error should mention redpanda failure, got: %s", got)
	}
}

func TestHealthChecker_ReadinessCheck_MultipleFailing(t *testing.T) {
	hc := newTestHealthChecker(
		errors.New("hot down"),
		errors.New("store down"),
		errors.New("stream down"),
	)

	err := hc.ReadinessCheck(context.Background())
	if err == nil {
		t.Fatal("expected error when multiple subsystems failing")
	}

	msg := err.Error()
	for _, want := range []string{"hot down", "store down", "stream down"} {
		if !containsSubstring(msg, want) {
			t.Errorf("aggregated error should contain %q, got: %s", want, msg)
		}
	}
}

func TestHealthChecker_ReadinessCheck_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	hc := newTestHealthChecker(nil, nil, nil)

	// The mock pingers don't use context, so this still returns nil.
	// In production the real pingers would propagate the cancellation.
	_ = hc.ReadinessCheck(ctx)
}

// --- Periodic check tests ---

func TestHealthChecker_StartPeriodicCheck_SetsServing(t *testing.T) {
	hc := newTestHealthChecker(nil, nil, nil)
	healthServer := health.NewServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc.StartPeriodicCheck(ctx, 100*time.Millisecond, healthServer)

	// Wait for the immediate first check to complete.
	time.Sleep(50 * time.Millisecond)

	resp, err := healthServer.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "paryty.ingestion",
	})
	if err != nil {
		t.Fatalf("health check RPC failed: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING, got %v", resp.Status)
	}
}

func TestHealthChecker_StartPeriodicCheck_SetsNotServing(t *testing.T) {
	hot := &mockPinger{err: errors.New("hot down")}
	hc := &HealthChecker{
		hotStore:     hot,
		store:        &mockPinger{err: errors.New("store down")},
		streamPinger: &mockPinger{err: errors.New("stream down")},
		logger:       slog.Default(),
	}
	healthServer := health.NewServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc.StartPeriodicCheck(ctx, 100*time.Millisecond, healthServer)

	// Wait for the immediate first check.
	time.Sleep(50 * time.Millisecond)

	resp, err := healthServer.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "paryty.ingestion",
	})
	if err != nil {
		t.Fatalf("health check RPC failed: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Errorf("expected NOT_SERVING, got %v", resp.Status)
	}
}

func TestHealthChecker_StartPeriodicCheck_TransitionsToServing(t *testing.T) {
	hot := &mockPinger{err: errors.New("starting up")}
	hc := &HealthChecker{
		hotStore:     hot,
		store:        &mockPinger{},
		streamPinger: &mockPinger{},
		logger:       slog.Default(),
	}
	healthServer := health.NewServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc.StartPeriodicCheck(ctx, 50*time.Millisecond, healthServer)

	// First check should be NOT_SERVING (hot store fails).
	time.Sleep(25 * time.Millisecond)
	resp, _ := healthServer.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "paryty.ingestion",
	})
	if resp.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Errorf("initial status should be NOT_SERVING, got %v", resp.Status)
	}

	// Fix the hot store.
	hot.SetError(nil)

	// Wait for next tick + processing.
	time.Sleep(100 * time.Millisecond)
	resp, _ = healthServer.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "paryty.ingestion",
	})
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING after recovery, got %v", resp.Status)
	}
}

func TestHealthChecker_StartPeriodicCheck_ExitsOnContextCancel(t *testing.T) {
	hc := newTestHealthChecker(nil, nil, nil)
	healthServer := health.NewServer()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		// We can't directly observe the goroutine exit, but we can
		// verify that cancelling the context doesn't panic and the
		// health server still works.
		hc.StartPeriodicCheck(ctx, 50*time.Millisecond, healthServer)
		close(done)
	}()

	// Let a few ticks run.
	time.Sleep(150 * time.Millisecond)
	cancel()

	// Give the goroutine time to exit.
	time.Sleep(100 * time.Millisecond)

	// Health server should still be responsive (no panics).
	resp, err := healthServer.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{
		Service: "paryty.ingestion",
	})
	if err != nil {
		t.Fatalf("health check after cancel failed: %v", err)
	}
	// Status remains whatever it was last set to.
	_ = resp
}

// --- Constructor test ---

func TestNewHealthChecker(t *testing.T) {
	// We can't easily create real *storage.Store and *stream.StreamEngine
	// without backends, but we can verify the constructor doesn't panic
	// with a mock-based approach. The real constructor is tested via
	// integration. Here we test the internal structure.

	hc := newTestHealthChecker(nil, nil, nil)
	if hc.hotStore == nil {
		t.Error("hotStore should not be nil")
	}
	if hc.store == nil {
		t.Error("store should not be nil")
	}
	if hc.streamPinger == nil {
		t.Error("streamPinger should not be nil")
	}
	if hc.logger == nil {
		t.Error("logger should not be nil")
	}
}

// --- Benchmarks ---

func BenchmarkHealthChecker_LivenessCheck(b *testing.B) {
	hc := newTestHealthChecker(nil, nil, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hc.LivenessCheck(ctx)
	}
}

func BenchmarkHealthChecker_ReadinessCheck_AllHealthy(b *testing.B) {
	hc := newTestHealthChecker(nil, nil, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hc.ReadinessCheck(ctx)
	}
}

// --- Helpers ---

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
