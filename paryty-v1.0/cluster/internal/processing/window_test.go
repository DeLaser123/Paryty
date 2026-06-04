package processing

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testClock is a controllable clock for deterministic time-based tests.
type testClock struct {
	mu  sync.RWMutex
	now time.Time
}

// newTestClock creates a test clock initialized at the given time.
func newTestClock(initial time.Time) *testClock {
	return &testClock{now: initial}
}

// Now returns the current fake time.
func (tc *testClock) Now() time.Time {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.now
}

// Advance moves the clock forward by d.
func (tc *testClock) Advance(d time.Duration) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.now = tc.now.Add(d)
}

// Set sets the clock to a specific time.
func (tc *testClock) Set(t time.Time) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.now = t
}

// testDragonfly is an in-memory mock of DragonflyClient for testing.
type testDragonfly struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

// newTestDragonfly creates a new in-memory mock.
func newTestDragonfly() *testDragonfly {
	return &testDragonfly{
		data: make(map[string]interface{}),
	}
}

// Set stores a value in memory. Handles both string and []byte values.
func (td *testDragonfly) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.data[key] = value
	return nil
}

// Get retrieves a value from memory. Returns redis.Nil if the key does not exist.
func (td *testDragonfly) Get(_ context.Context, key string) (string, error) {
	td.mu.RLock()
	defer td.mu.RUnlock()
	v, ok := td.data[key]
	if !ok {
		return "", redis.Nil
	}
	// Convert stored value to string.
	switch val := v.(type) {
	case string:
		return val, nil
	case []byte:
		return string(val), nil
	default:
		return "", nil
	}
}

// nopLogger returns a no-op slog logger for tests.
func nopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// =============================================================================
// TestAlignToWindow
// =============================================================================

func TestAlignToWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ts         time.Time
		windowSize time.Duration
		expected   time.Time
	}{
		{
			name:       "1m window aligned",
			ts:         time.Date(2025, 6, 4, 10, 5, 30, 0, time.UTC),
			windowSize: 1 * time.Minute,
			expected:   time.Date(2025, 6, 4, 10, 5, 0, 0, time.UTC),
		},
		{
			name:       "1m window at boundary",
			ts:         time.Date(2025, 6, 4, 10, 5, 0, 0, time.UTC),
			windowSize: 1 * time.Minute,
			expected:   time.Date(2025, 6, 4, 10, 5, 0, 0, time.UTC),
		},
		{
			name:       "5m window aligned",
			ts:         time.Date(2025, 6, 4, 10, 7, 15, 0, time.UTC),
			windowSize: 5 * time.Minute,
			expected:   time.Date(2025, 6, 4, 10, 5, 0, 0, time.UTC),
		},
		{
			name:       "5m window at boundary",
			ts:         time.Date(2025, 6, 4, 10, 10, 0, 0, time.UTC),
			windowSize: 5 * time.Minute,
			expected:   time.Date(2025, 6, 4, 10, 10, 0, 0, time.UTC),
		},
		{
			name:       "1h window aligned",
			ts:         time.Date(2025, 6, 4, 10, 35, 45, 0, time.UTC),
			windowSize: 1 * time.Hour,
			expected:   time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC),
		},
		{
			name:       "1h window at boundary",
			ts:         time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC),
			windowSize: 1 * time.Hour,
			expected:   time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC),
		},
		{
			name:       "1d window aligned",
			ts:         time.Date(2025, 6, 4, 15, 30, 0, 0, time.UTC),
			windowSize: 24 * time.Hour,
			expected:   time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC),
		},
		{
			name:       "1d window at midnight",
			ts:         time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC),
			windowSize: 24 * time.Hour,
			expected:   time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := alignToWindow(tt.ts, tt.windowSize)
			if !got.Equal(tt.expected) {
				t.Errorf("alignToWindow(%s, %s) = %s, want %s",
					tt.ts, tt.windowSize, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// TestTumblingWindow_BasicAggregation
// =============================================================================

func TestTumblingWindow_BasicAggregation(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})
	ctx := context.Background()

	values := []float64{10.0, 20.0, 30.0, 40.0, 50.0}
	for i, v := range values {
		ts := startTime.Add(time.Duration(i) * 10 * time.Second)
		if err := wm.AddValue(ctx, "agent-1", "cpu.usage", v, ts); err != nil {
			t.Fatalf("AddValue failed: %v", err)
		}
	}

	if wm.WindowCount() != 1 {
		t.Fatalf("expected 1 window, got %d", wm.WindowCount())
	}

	// Advance past window end + grace period.
	clock.Set(startTime.Add(1*time.Minute + 31*time.Second))

	closed := wm.GetClosedWindows(ctx)
	if len(closed) != 1 {
		t.Fatalf("expected 1 closed window, got %d", len(closed))
	}

	cw := closed[0]
	if cw.Count != 5 {
		t.Errorf("expected count=5, got %d", cw.Count)
	}
	if cw.Sum != 150.0 {
		t.Errorf("expected sum=150.0, got %f", cw.Sum)
	}
	if cw.Min != 10.0 {
		t.Errorf("expected min=10.0, got %f", cw.Min)
	}
	if cw.Max != 50.0 {
		t.Errorf("expected max=50.0, got %f", cw.Max)
	}
	if cw.Avg != 30.0 {
		t.Errorf("expected avg=30.0, got %f", cw.Avg)
	}
	if cw.Key.AgentID != "agent-1" {
		t.Errorf("expected agent_id=agent-1, got %s", cw.Key.AgentID)
	}
	if cw.Key.MetricName != "cpu.usage" {
		t.Errorf("expected metric_name=cpu.usage, got %s", cw.Key.MetricName)
	}
}

// =============================================================================
// TestTumblingWindow_LateData
// =============================================================================

func TestTumblingWindow_LateData(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ts := startTime.Add(time.Duration(i) * 10 * time.Second)
		if err := wm.AddValue(ctx, "agent-1", "cpu.usage", float64(i+1)*10, ts); err != nil {
			t.Fatalf("AddValue failed: %v", err)
		}
	}

	// Advance to window end but still within grace period.
	clock.Set(startTime.Add(1*time.Minute + 15*time.Second))

	lateTs := startTime.Add(50 * time.Second)
	if err := wm.AddValue(ctx, "agent-1", "cpu.usage", 40.0, lateTs); err != nil {
		t.Fatalf("AddValue (late within grace) failed: %v", err)
	}

	closed := wm.GetClosedWindows(ctx)
	if len(closed) != 0 {
		t.Fatalf("expected 0 closed windows within grace, got %d", len(closed))
	}

	// Advance past grace period.
	clock.Set(startTime.Add(1*time.Minute + 31*time.Second))

	closed = wm.GetClosedWindows(ctx)
	if len(closed) != 1 {
		t.Fatalf("expected 1 closed window, got %d", len(closed))
	}

	if closed[0].Count != 4 {
		t.Errorf("expected count=4 (including late data), got %d", closed[0].Count)
	}
	if closed[0].Sum != 100.0 {
		t.Errorf("expected sum=100.0, got %f", closed[0].Sum)
	}

	// Try to add data AFTER grace period -- should be silently skipped.
	reallyLateTs := startTime.Add(20 * time.Second)
	if err := wm.AddValue(ctx, "agent-1", "cpu.usage", 999.0, reallyLateTs); err != nil {
		t.Fatalf("AddValue (after grace) failed: %v", err)
	}

	purged := wm.PurgeClosed()
	if purged != 1 {
		t.Fatalf("expected 1 purged, got %d", purged)
	}

	if wm.WindowCount() != 0 {
		t.Errorf("expected 0 windows after purge, got %d", wm.WindowCount())
	}
}

// =============================================================================
// TestTumblingWindow_MultipleWindows
// =============================================================================

func TestTumblingWindow_MultipleWindows(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute, 5 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})
	ctx := context.Background()

	// Send one metric per minute for 6 minutes (minutes 0 through 5).
	// 1m windows: [10:00,10:01), [10:01,10:02), ..., [10:05,10:06) => 6 windows
	// 5m windows: [10:00,10:05) gets minutes 0-4, [10:05,10:10) gets minute 5 => 2 windows
	// Total: 8 windows
	for min := 0; min < 6; min++ {
		ts := startTime.Add(time.Duration(min) * time.Minute)
		if err := wm.AddValue(ctx, "agent-1", "cpu.usage", float64(min+1)*10, ts); err != nil {
			t.Fatalf("AddValue at minute %d failed: %v", min, err)
		}
	}

	if wm.WindowCount() != 8 {
		t.Fatalf("expected 8 windows (6x1m + 2x5m), got %d", wm.WindowCount())
	}

	// Advance past all 1m windows + first 5m window.
	// 1m window [10:05,10:06) closes at 10:06:30.
	// 5m window [10:00,10:05) closes at 10:05:30.
	// 5m window [10:05,10:10) closes at 10:10:30 -- NOT yet closed at 10:06:31.
	clock.Set(startTime.Add(6*time.Minute + 31*time.Second))

	closed := wm.GetClosedWindows(ctx)

	// Expect 7 closed: 6x1m + 1x5m ([10:00,10:05)).
	// [10:05,10:10) is still open.
	if len(closed) != 7 {
		t.Fatalf("expected 7 closed windows, got %d", len(closed))
	}

	var oneMinWindows []ClosedWindow
	var fiveMinWindows []ClosedWindow
	for _, cw := range closed {
		switch cw.Key.WindowSize {
		case 1 * time.Minute:
			oneMinWindows = append(oneMinWindows, cw)
		case 5 * time.Minute:
			fiveMinWindows = append(fiveMinWindows, cw)
		}
	}

	if len(oneMinWindows) != 6 {
		t.Errorf("expected 6 one-minute windows, got %d", len(oneMinWindows))
	}
	if len(fiveMinWindows) != 1 {
		t.Errorf("expected 1 five-minute window, got %d", len(fiveMinWindows))
	}

	for _, cw := range oneMinWindows {
		if cw.Count != 1 {
			t.Errorf("1m window at %s: expected count=1, got %d", cw.Key.WindowStart, cw.Count)
		}
	}

	if len(fiveMinWindows) == 1 {
		fwm := fiveMinWindows[0]
		if fwm.Count != 5 {
			t.Errorf("5m window: expected count=5, got %d", fwm.Count)
		}
		if fwm.Sum != 150.0 {
			t.Errorf("5m window: expected sum=150.0, got %f", fwm.Sum)
		}
	}
}

// =============================================================================
// TestTumblingWindow_EmptyWindow
// =============================================================================

func TestTumblingWindow_EmptyWindow(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})
	ctx := context.Background()

	if err := wm.AddValue(ctx, "agent-1", "cpu.usage", 50.0, startTime); err != nil {
		t.Fatalf("AddValue failed: %v", err)
	}

	// Skip [10:01, 10:02) -- no data. Send at [10:02, 10:03).
	ts2 := startTime.Add(2 * time.Minute)
	if err := wm.AddValue(ctx, "agent-1", "cpu.usage", 75.0, ts2); err != nil {
		t.Fatalf("AddValue failed: %v", err)
	}

	if wm.WindowCount() != 2 {
		t.Fatalf("expected 2 windows, got %d", wm.WindowCount())
	}

	clock.Set(startTime.Add(3*time.Minute + 31*time.Second))

	closed := wm.GetClosedWindows(ctx)
	if len(closed) != 2 {
		t.Fatalf("expected 2 closed windows, got %d", len(closed))
	}

	for _, cw := range closed {
		if cw.Count == 0 {
			t.Errorf("window at %s is empty but should have data", cw.Key.WindowStart)
		}
	}

	// Verify the skipped window does NOT exist.
	for _, cw := range closed {
		if cw.Key.WindowStart.Equal(startTime.Add(1 * time.Minute)) {
			t.Error("found window for [10:01, 10:02) which should not exist")
		}
	}
}

// =============================================================================
// TestWindowStateManager_SnapshotRestore
// =============================================================================

func TestWindowStateManager_SnapshotRestore(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)
	mockDF := newTestDragonfly()

	ctx := context.Background()

	wm1 := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
		Dragonfly:   mockDF,
	})

	for i := 0; i < 3; i++ {
		ts := startTime.Add(time.Duration(i) * 10 * time.Second)
		if err := wm1.AddValue(ctx, "agent-1", "cpu.usage", float64(i+1)*10, ts); err != nil {
			t.Fatalf("AddValue failed: %v", err)
		}
	}

	if err := wm1.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	mockDF.mu.RLock()
	_, ok := mockDF.data[snapshotKey]
	mockDF.mu.RUnlock()
	if !ok {
		t.Fatal("snapshot not stored in Dragonfly")
	}

	// --- Phase 2: Create new manager, restore ---
	wm2 := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
		Dragonfly:   mockDF,
	})

	if err := wm2.Restore(ctx); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if wm2.WindowCount() != 1 {
		t.Fatalf("expected 1 restored window, got %d", wm2.WindowCount())
	}

	clock.Set(startTime.Add(1*time.Minute + 31*time.Second))

	closed := wm2.GetClosedWindows(ctx)
	if len(closed) != 1 {
		t.Fatalf("expected 1 closed window from restored state, got %d", len(closed))
	}

	cw := closed[0]
	if cw.Count != 3 {
		t.Errorf("expected count=3, got %d", cw.Count)
	}
	if cw.Sum != 60.0 {
		t.Errorf("expected sum=60.0, got %f", cw.Sum)
	}
	if cw.Min != 10.0 {
		t.Errorf("expected min=10.0, got %f", cw.Min)
	}
	if cw.Max != 30.0 {
		t.Errorf("expected max=30.0, got %f", cw.Max)
	}
}

// =============================================================================
// TestWindowStateManager_SnapshotRestore_NilDragonfly
// =============================================================================

func TestWindowStateManager_SnapshotRestore_NilDragonfly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		Logger:      nopLogger(),
		TimeFunc:    func() time.Time { return time.Now() },
	})

	if err := wm.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot with nil dragonfly should not error, got: %v", err)
	}

	if err := wm.Restore(ctx); err != nil {
		t.Fatalf("Restore with nil dragonfly should not error, got: %v", err)
	}
}

// =============================================================================
// TestWindowStateManager_SnapshotRestore_NoSnapshot
// =============================================================================

func TestWindowStateManager_SnapshotRestore_NoSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockDF := newTestDragonfly()

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		Logger:      nopLogger(),
		TimeFunc:    func() time.Time { return time.Now() },
		Dragonfly:   mockDF,
	})

	if err := wm.Restore(ctx); err != nil {
		t.Fatalf("Restore with no snapshot should not error, got: %v", err)
	}
}

// =============================================================================
// TestWindowStateManager_StartStop
// =============================================================================

func TestWindowStateManager_StartStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes:      []time.Duration{1 * time.Minute},
		GracePeriod:      30 * time.Second,
		SnapshotInterval: 100 * time.Millisecond,
		Logger:           nopLogger(),
		TimeFunc:         func() time.Time { return time.Now() },
	})

	wm.Start(ctx)

	now := time.Now()
	if err := wm.AddValue(ctx, "agent-1", "cpu.usage", 50.0, now); err != nil {
		t.Fatalf("AddValue failed: %v", err)
	}

	time.Sleep(250 * time.Millisecond)

	wm.Stop()
}

// =============================================================================
// TestWindowStateManager_StopIdempotent
// =============================================================================

func TestWindowStateManager_StopIdempotent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		Logger:      nopLogger(),
		TimeFunc:    func() time.Time { return time.Now() },
	})

	wm.Start(ctx)

	wm.Stop()
	wm.Stop()
}

// =============================================================================
// TestWindowStateManager_ContextCancellation
// =============================================================================

func TestWindowStateManager_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		Logger:      nopLogger(),
		TimeFunc:    func() time.Time { return time.Now() },
	})

	wm.Start(ctx)

	cancel()

	done := make(chan struct{})
	go func() {
		wm.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not complete within 5s after context cancellation")
	}
}

// =============================================================================
// TestWindowBuffer_BoundedCapacity
// =============================================================================

func TestWindowBuffer_BoundedCapacity(t *testing.T) {
	t.Parallel()

	key := WindowKey{
		AgentID:     "agent-1",
		MetricName:  "cpu.usage",
		WindowSize:  1 * time.Minute,
		WindowStart: time.Now(),
	}

	buf := newWindowBuffer(key)
	now := time.Now()

	excess := maxWindowBufferCapacity + 100
	for i := 0; i < excess; i++ {
		buf.AddValue(float64(i), now.Add(time.Duration(i)*time.Second))
	}

	buf.mu.RLock()
	valueCount := len(buf.Values)
	buf.mu.RUnlock()

	if valueCount != maxWindowBufferCapacity {
		t.Errorf("expected %d values in buffer, got %d", maxWindowBufferCapacity, valueCount)
	}

	if buf.Count != excess {
		t.Errorf("expected count=%d, got %d", excess, buf.Count)
	}

	if buf.Min != 0.0 {
		t.Errorf("expected min=0.0, got %f", buf.Min)
	}

	if buf.Max != float64(excess-1) {
		t.Errorf("expected max=%f, got %f", float64(excess-1), buf.Max)
	}
}

// =============================================================================
// TestWindowStateManager_ConcurrentAddValue
// =============================================================================

func TestWindowStateManager_ConcurrentAddValue(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})
	ctx := context.Background()

	const goroutines = 10
	const valuesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < valuesPerGoroutine; i++ {
				ts := startTime.Add(time.Duration(i) * time.Second)
				_ = wm.AddValue(ctx, "agent-1", "cpu.usage", float64(id*1000+i), ts)
			}
		}(g)
	}

	wg.Wait()

	// Only 60 of the 100 timestamps fit in [10:00, 10:01) (0..59s).
	// Seconds 60-99 fall into [10:01, 10:02).
	clock.Set(startTime.Add(1*time.Minute + 31*time.Second))

	closed := wm.GetClosedWindows(ctx)
	if len(closed) < 1 {
		t.Fatalf("expected at least 1 closed window, got %d", len(closed))
	}

	// Find the [10:00, 10:01) window.
	var target ClosedWindow
	found := false
	for _, cw := range closed {
		if cw.Key.WindowStart.Equal(startTime) {
			target = cw
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected window [10:00, 10:01) to be closed")
	}

	// Each goroutine adds values at seconds 0..59 for 60 values in the first window.
	expectedCount := goroutines * 60
	if target.Count != expectedCount {
		t.Errorf("expected count=%d, got %d", expectedCount, target.Count)
	}
}

// =============================================================================
// TestWindowStateManager_PurgeStale
// =============================================================================

func TestWindowStateManager_PurgeStale(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})
	ctx := context.Background()

	_ = wm.AddValue(ctx, "agent-1", "cpu.usage", 50.0, startTime)

	if wm.WindowCount() != 1 {
		t.Fatalf("expected 1 window, got %d", wm.WindowCount())
	}

	purged := wm.PurgeStale(24 * time.Hour)
	if purged != 0 {
		t.Errorf("expected 0 purged, got %d", purged)
	}

	clock.Set(startTime.Add(25 * time.Hour))

	purged = wm.PurgeStale(24 * time.Hour)
	if purged != 1 {
		t.Errorf("expected 1 purged, got %d", purged)
	}

	if wm.WindowCount() != 0 {
		t.Errorf("expected 0 windows after purge, got %d", wm.WindowCount())
	}
}

// =============================================================================
// TestWindowStateManager_WindowKeyString
// =============================================================================

func TestWindowStateManager_WindowKeyString(t *testing.T) {
	t.Parallel()

	key := WindowKey{
		AgentID:     "agent-1",
		MetricName:  "cpu.usage",
		WindowSize:  1 * time.Minute,
		WindowStart: time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC),
	}

	s := key.String()
	if s == "" {
		t.Error("WindowKey.String() should not return empty string")
	}
	if !strContains(s, "agent-1") || !strContains(s, "cpu.usage") {
		t.Errorf("WindowKey.String() = %q, missing expected components", s)
	}
}

// strContains is a simple substring check helper for tests.
func strContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// TestZstdCompressDecompress
// =============================================================================

func TestZstdCompressDecompress(t *testing.T) {
	t.Parallel()

	original := []byte(`[{"key":{"agent_id":"agent-1","metric_name":"cpu.usage","window_size":60000000000,"window_start":"2025-06-04T10:00:00Z"},"values":[10,20,30],"count":3,"sum":60,"min":10,"max":30}]`)

	compressed, err := zstdCompress(original)
	if err != nil {
		t.Fatalf("zstdCompress failed: %v", err)
	}

	if len(compressed) == 0 {
		t.Fatal("compressed data is empty")
	}

	decompressed, err := zstdDecompress(compressed)
	if err != nil {
		t.Fatalf("zstdDecompress failed: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("decompressed data doesn't match original.\noriginal:     %s\ndecompressed: %s", original, decompressed)
	}
}

// =============================================================================
// TestWindowStateManager_SnapshotEmpty
// =============================================================================

func TestWindowStateManager_SnapshotEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockDF := newTestDragonfly()

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: []time.Duration{1 * time.Minute},
		Logger:      nopLogger(),
		TimeFunc:    func() time.Time { return time.Now() },
		Dragonfly:   mockDF,
	})

	if err := wm.Snapshot(ctx); err != nil {
		t.Fatalf("Snapshot with no windows should not error, got: %v", err)
	}

	mockDF.mu.RLock()
	_, ok := mockDF.data[snapshotKey]
	mockDF.mu.RUnlock()
	if ok {
		t.Error("expected no data stored for empty snapshot")
	}
}

// =============================================================================
// TestWindowStateManager_AllWindowSizes
// =============================================================================

func TestWindowStateManager_AllWindowSizes(t *testing.T) {
	t.Parallel()

	startTime := time.Date(2025, 6, 4, 10, 0, 0, 0, time.UTC)
	clock := newTestClock(startTime)

	wm := NewWindowStateManager(WindowManagerConfig{
		WindowSizes: defaultWindowSizes,
		GracePeriod: 30 * time.Second,
		Logger:      nopLogger(),
		TimeFunc:    clock.Now,
	})
	ctx := context.Background()

	_ = wm.AddValue(ctx, "agent-1", "cpu.usage", 50.0, startTime)

	if wm.WindowCount() != 4 {
		t.Fatalf("expected 4 windows, got %d", wm.WindowCount())
	}

	// Advance past 1m window close.
	clock.Set(startTime.Add(1*time.Minute + 31*time.Second))

	closed := wm.GetClosedWindows(ctx)
	if len(closed) != 1 {
		t.Fatalf("expected 1 closed window (1m), got %d", len(closed))
	}
	if closed[0].Key.WindowSize != 1*time.Minute {
		t.Errorf("expected 1m window, got %s", closed[0].Key.WindowSize)
	}

	// Advance past 5m window close.
	clock.Set(startTime.Add(5*time.Minute + 31*time.Second))

	closed = wm.GetClosedWindows(ctx)
	if len(closed) != 1 {
		t.Fatalf("expected 1 newly closed window (5m), got %d", len(closed))
	}
	if closed[0].Key.WindowSize != 5*time.Minute {
		t.Errorf("expected 5m window, got %s", closed[0].Key.WindowSize)
	}
}
