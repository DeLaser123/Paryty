// Package processing implements the data processing pipeline.
// It includes aggregator, correlator, and enricher services.
package processing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/redis/go-redis/v9"
)

// Predefined window sizes used by the tumbling window state manager.
var (
	Window1Min = 1 * time.Minute
	Window5Min = 5 * time.Minute
	Window1Hr  = 1 * time.Hour
	Window1Day = 24 * time.Hour
)

// defaultWindowSizes are the tumbling window durations applied to every incoming metric.
var defaultWindowSizes = []time.Duration{Window1Min, Window5Min, Window1Hr, Window1Day}

// maxWindowBufferCapacity is the maximum number of values stored per window.
// When exceeded, oldest values are evicted to maintain bounded memory.
const maxWindowBufferCapacity = 10000

// snapshotKey is the Dragonfly key for persisted window state.
const snapshotKey = "paryty:pipeline:window:snapshot"

// WindowKey uniquely identifies a single tumbling window for a specific agent,
// metric name, window size, and aligned window start time.
type WindowKey struct {
	TenantID    string        `json:"tenant_id"`
	AgentID    string        `json:"agent_id"`
	MetricName string        `json:"metric_name"`
	WindowSize time.Duration `json:"window_size"`
	WindowStart time.Time    `json:"window_start"`
}

// String returns a human-readable representation of the window key.
func (k WindowKey) String() string {
	return fmt.Sprintf("%s/%s/%s/%s",
		k.AgentID, k.MetricName, k.WindowSize, k.WindowStart.Format(time.RFC3339))
}

// WindowBuffer holds raw metric values for a single tumbling window.
// It maintains running statistics (count, sum, min, max) for O(1) access
// and retains raw values for percentile calculations.
//
// Thread-safe: all reads and writes go through mu.
type WindowBuffer struct {
	Key       WindowKey `json:"key"`
	Values    []float64 `json:"values"`
	Count     int       `json:"count"`
	Sum       float64   `json:"sum"`
	Min       float64   `json:"min"`
	Max       float64   `json:"max"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Closed    bool      `json:"closed"`
	mu        sync.RWMutex `json:"-"`
}

// newWindowBuffer creates a new WindowBuffer for the given key.
func newWindowBuffer(key WindowKey) *WindowBuffer {
	return &WindowBuffer{
		Key:    key,
		Values: make([]float64, 0, 64),
	}
}

// AddValue appends a value to the buffer, updating running statistics.
// If the buffer exceeds maxWindowBufferCapacity, the oldest value is evicted.
func (wb *WindowBuffer) AddValue(value float64, ts time.Time) {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	// Evict oldest if at capacity.
	if len(wb.Values) >= maxWindowBufferCapacity {
		wb.Values = wb.Values[1:]
	}

	wb.Values = append(wb.Values, value)
	wb.Count++
	wb.Sum += value

	if wb.Count == 1 {
		wb.Min = value
		wb.Max = value
		wb.FirstSeen = ts
	} else {
		if value < wb.Min {
			wb.Min = value
		}
		if value > wb.Max {
			wb.Max = value
		}
	}

	wb.LastSeen = ts
}

// Snapshot returns a frozen copy of the buffer data for aggregation.
// The returned slices are independent of the buffer.
func (wb *WindowBuffer) Snapshot() (values []float64, count int, sum, min, max float64, firstSeen, lastSeen time.Time) {
	wb.mu.RLock()
	defer wb.mu.RUnlock()

	values = make([]float64, len(wb.Values))
	copy(values, wb.Values)
	return values, wb.Count, wb.Sum, wb.Min, wb.Max, wb.FirstSeen, wb.LastSeen
}

// markClosed sets the window as closed. No further values should be added.
func (wb *WindowBuffer) markClosed() {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	wb.Closed = true
}

// isClosed returns whether the window is closed.
func (wb *WindowBuffer) isClosed() bool {
	wb.mu.RLock()
	defer wb.mu.RUnlock()
	return wb.Closed
}

// WindowState is a serializable snapshot of a WindowBuffer for persistence.
type WindowState struct {
	Key       WindowKey `json:"key"`
	Values    []float64 `json:"values"`
	Count     int       `json:"count"`
	Sum       float64   `json:"sum"`
	Min       float64   `json:"min"`
	Max       float64   `json:"max"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// DragonflyClient defines the interface for Dragonfly persistence operations
// needed by the window manager. The hot.Client satisfies this interface.
type DragonflyClient interface {
	// Set stores a value with the given key and TTL.
	// If ttl is zero, the key persists until evicted.
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Get retrieves the value for the given key.
	// Returns redis.Nil error if the key does not exist.
	Get(ctx context.Context, key string) (string, error)
}

// RedisAdapter adapts a *redis.Client to satisfy DragonflyClient.
// The hot.Client uses *redis.Client internally; this adapter exposes
// the low-level Set/Get needed for snapshot persistence.
type RedisAdapter struct {
	rdb *redis.Client
}

// NewRedisAdapter creates a RedisAdapter wrapping the given redis client.
func NewRedisAdapter(rdb *redis.Client) *RedisAdapter {
	return &RedisAdapter{rdb: rdb}
}

// Set stores value with key and TTL using the underlying redis client.
func (a *RedisAdapter) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return a.rdb.Set(ctx, key, value, ttl).Err()
}

// Get retrieves the string value for key using the underlying redis client.
func (a *RedisAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.rdb.Get(ctx, key).Result()
}

// WindowManagerConfig holds configuration for creating a WindowStateManager.
type WindowManagerConfig struct {
	// GracePeriod is how long after a window closes to still accept late data.
	// Default: 5 minutes.
	GracePeriod time.Duration

	// WindowSizes are the tumbling window durations to maintain.
	// Default: 1m, 5m, 1h, 1d.
	WindowSizes []time.Duration

	// SnapshotInterval controls how often window state is persisted to Dragonfly.
	// Default: 30 seconds.
	SnapshotInterval time.Duration

	// Dragonfly is the DragonflyClient for snapshot persistence.
	// If nil, snapshot/restore are no-ops.
	Dragonfly DragonflyClient

	// Logger is the structured logger. If nil, a default logger is used.
	Logger *slog.Logger

	// TimeFunc returns the current time. Defaults to time.Now.
	// Inject a custom function for deterministic testing.
	TimeFunc func() time.Time
}

func (c *WindowManagerConfig) applyDefaults() {
	if c.GracePeriod == 0 {
		c.GracePeriod = 5 * time.Minute
	}
	if len(c.WindowSizes) == 0 {
		c.WindowSizes = defaultWindowSizes
	}
	if c.SnapshotInterval == 0 {
		c.SnapshotInterval = 30 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.TimeFunc == nil {
		c.TimeFunc = time.Now
	}
}

// WindowStateManager manages all tumbling windows for the aggregation pipeline.
//
// It accepts raw metric values and routes them to all applicable windows
// (1m, 5m, 1h, 1d). Windows are automatically closed after their duration
// plus a grace period for late data. State can be snapshotted to Dragonfly
// for crash recovery.
//
// Thread-safe. All public methods are safe for concurrent use.
type WindowStateManager struct {
	windows          sync.Map // map[WindowKey]*WindowBuffer
	gracePeriod      time.Duration
	windowSizes      []time.Duration
	logger           *slog.Logger
	snapshotInterval time.Duration
	dragonfly        DragonflyClient
	timeFunc         func() time.Time
	stopCh           chan struct{}
	doneCh           chan struct{}
}

// NewWindowStateManager creates a new window state manager with the given config.
func NewWindowStateManager(cfg WindowManagerConfig) *WindowStateManager {
	cfg.applyDefaults()

	return &WindowStateManager{
		gracePeriod:      cfg.GracePeriod,
		windowSizes:      cfg.WindowSizes,
		logger:           cfg.Logger,
		snapshotInterval: cfg.SnapshotInterval,
		dragonfly:        cfg.Dragonfly,
		timeFunc:         cfg.TimeFunc,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
	}
}

// alignToWindow aligns a timestamp to the start of its tumbling window.
// The formula: aligned = (unix_seconds / window_seconds) * window_seconds.
func alignToWindow(ts time.Time, windowSize time.Duration) time.Time {
	unix := ts.Unix()
	windowSecs := int64(windowSize / time.Second)
	aligned := (unix / windowSecs) * windowSecs
	return time.Unix(aligned, 0).UTC()
}

// windowEnd returns the end time of a window (exclusive).
func windowEnd(windowStart time.Time, windowSize time.Duration) time.Time {
	return windowStart.Add(windowSize)
}

// AddValue adds a metric value to all applicable tumbling windows.
// For each configured window size, the value is placed into the window
// that contains the given timestamp.
//
// Returns an error only if the context is cancelled.
func (wm *WindowStateManager) AddValue(ctx context.Context, agentID, metricName string, value float64, ts time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	for _, ws := range wm.windowSizes {
		wsStart := alignToWindow(ts, ws)
		key := WindowKey{
			AgentID:     agentID,
			MetricName:  metricName,
			WindowSize:  ws,
			WindowStart: wsStart,
		}

		buf := wm.getOrCreateBuffer(key)
		if buf.isClosed() {
			// Window already closed — this is late data beyond grace period.
			continue
		}
		buf.AddValue(value, ts)
	}

	return nil
}

// getOrCreateBuffer returns the buffer for the given key, creating it if necessary.
func (wm *WindowStateManager) getOrCreateBuffer(key WindowKey) *WindowBuffer {
	if v, ok := wm.windows.Load(key); ok {
		return v.(*WindowBuffer)
	}

	buf := newWindowBuffer(key)
	actual, loaded := wm.windows.LoadOrStore(key, buf)
	if loaded {
		// Another goroutine created it first; use the existing one.
		return actual.(*WindowBuffer)
	}
	return buf
}

// ClosedWindow contains the aggregated data from a closed window.
type ClosedWindow struct {
	Key       WindowKey
	Values    []float64
	Count     int
	Sum       float64
	Min       float64
	Max       float64
	Avg       float64
	FirstSeen time.Time
	LastSeen  time.Time
}

// GetClosedWindows identifies and marks windows whose close time (window end + grace period)
// has passed. Returns the data from those windows.
//
// A window is considered closed when: now >= windowEnd(windowStart, windowSize) + gracePeriod.
//
// This method does NOT remove windows from the map — call PurgeClosed after
// aggregation is complete.
func (wm *WindowStateManager) GetClosedWindows(ctx context.Context) []ClosedWindow {
	now := wm.timeFunc()
	var closed []ClosedWindow

	wm.windows.Range(func(key, value interface{}) bool {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		wk := key.(WindowKey)
		buf := value.(*WindowBuffer)

		if buf.isClosed() {
			return true // already closed, skip
		}

		end := windowEnd(wk.WindowStart, wk.WindowSize)
		closeAt := end.Add(wm.gracePeriod)

		if now.Before(closeAt) {
			return true // not yet closed
		}

		// Mark as closed and capture snapshot.
		buf.markClosed()
		values, count, sum, min, max, firstSeen, lastSeen := buf.Snapshot()

		avg := 0.0
		if count > 0 {
			avg = sum / float64(count)
		}

		closed = append(closed, ClosedWindow{
			Key:       wk,
			Values:    values,
			Count:     count,
			Sum:       sum,
			Min:       min,
			Max:       max,
			Avg:       avg,
			FirstSeen: firstSeen,
			LastSeen:  lastSeen,
		})

		return true
	})

	return closed
}

// PurgeClosed removes all closed windows from memory.
// Call this after aggregating closed windows returned by GetClosedWindows.
func (wm *WindowStateManager) PurgeClosed() int {
	count := 0
	wm.windows.Range(func(key, value interface{}) bool {
		buf := value.(*WindowBuffer)
		if buf.isClosed() {
			wm.windows.Delete(key)
			count++
		}
		return true
	})
	return count
}

// PurgeStale removes all windows whose window end is older than maxAge from now.
// This handles the case where windows accumulate without being closed
// (e.g., clock skew or missed close cycles).
func (wm *WindowStateManager) PurgeStale(maxAge time.Duration) int {
	now := wm.timeFunc()
	cutoff := now.Add(-maxAge)
	count := 0

	wm.windows.Range(func(key, value interface{}) bool {
		wk := key.(WindowKey)
		end := windowEnd(wk.WindowStart, wk.WindowSize)
		if end.Before(cutoff) {
			wm.windows.Delete(key)
			count++
		}
		return true
	})

	return count
}

// WindowCount returns the number of active (non-purged) windows.
func (wm *WindowStateManager) WindowCount() int {
	count := 0
	wm.windows.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// Snapshot serializes all open (non-closed) windows to JSON, compresses with Zstd,
// and stores the result in Dragonfly at key "paryty:pipeline:window:snapshot".
//
// If dragonfly is nil, this is a no-op.
func (wm *WindowStateManager) Snapshot(ctx context.Context) error {
	if wm.dragonfly == nil {
		return nil
	}

	states := wm.collectOpenStates()
	if len(states) == 0 {
		wm.logger.Debug("no open windows to snapshot")
		return nil
	}

	jsonData, err := json.Marshal(states)
	if err != nil {
		return fmt.Errorf("marshal window states: %w", err)
	}

	compressed, err := zstdCompress(jsonData)
	if err != nil {
		return fmt.Errorf("compress window snapshot: %w", err)
	}

	if err := wm.dragonfly.Set(ctx, snapshotKey, compressed, 0); err != nil {
		return fmt.Errorf("store window snapshot: %w", err)
	}

	wm.logger.Info("window state snapshotted",
		"windows", len(states),
		"json_bytes", len(jsonData),
		"compressed_bytes", len(compressed),
	)

	return nil
}

// collectOpenStates gathers serializable state from all non-closed windows.
func (wm *WindowStateManager) collectOpenStates() []WindowState {
	var states []WindowState

	wm.windows.Range(func(_, value interface{}) bool {
		buf := value.(*WindowBuffer)
		if buf.isClosed() {
			return true
		}

		values, count, sum, min, max, firstSeen, lastSeen := buf.Snapshot()
		states = append(states, WindowState{
			Key:       buf.Key,
			Values:    values,
			Count:     count,
			Sum:       sum,
			Min:       min,
			Max:       max,
			FirstSeen: firstSeen,
			LastSeen:  lastSeen,
		})
		return true
	})

	return states
}

// Restore loads window state from a previous snapshot in Dragonfly.
// If no snapshot exists, this is a non-fatal no-op (fresh start).
//
// Existing windows are NOT cleared — restored windows are merged in.
func (wm *WindowStateManager) Restore(ctx context.Context) error {
	if wm.dragonfly == nil {
		return nil
	}

	compressed, err := wm.dragonfly.Get(ctx, snapshotKey)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			wm.logger.Info("no window snapshot found, starting fresh")
			return nil
		}
		return fmt.Errorf("get window snapshot: %w", err)
	}

	jsonData, err := zstdDecompress([]byte(compressed))
	if err != nil {
		return fmt.Errorf("decompress window snapshot: %w", err)
	}

	var states []WindowState
	if err := json.Unmarshal(jsonData, &states); err != nil {
		return fmt.Errorf("unmarshal window states: %w", err)
	}

	restored := 0
	for _, state := range states {
		// Skip stale windows (older than 2x the window size).
		end := windowEnd(state.Key.WindowStart, state.Key.WindowSize)
		if wm.timeFunc().Sub(end) > 2*state.Key.WindowSize {
			continue
		}

		buf := newWindowBuffer(state.Key)
		buf.mu.Lock()
		buf.Values = state.Values
		buf.Count = state.Count
		buf.Sum = state.Sum
		buf.Min = state.Min
		buf.Max = state.Max
		buf.FirstSeen = state.FirstSeen
		buf.LastSeen = state.LastSeen
		buf.mu.Unlock()

		wm.windows.Store(state.Key, buf)
		restored++
	}

	wm.logger.Info("window state restored", "windows", restored, "total_states", len(states))
	return nil
}

// Start launches the background goroutine that:
//  1. Checks for closed windows every 1 second
//  2. Snapshots window state every snapshotInterval
//  3. Purges windows older than 24 hours
//
// The goroutine stops when ctx is cancelled or Stop() is called.
func (wm *WindowStateManager) Start(ctx context.Context) {
	go wm.run(ctx)
}

// run is the main loop for the background maintenance goroutine.
func (wm *WindowStateManager) run(ctx context.Context) {
	defer close(wm.doneCh)

	closeTicker := time.NewTicker(1 * time.Second)
	defer closeTicker.Stop()

	snapshotTicker := time.NewTicker(wm.snapshotInterval)
	defer snapshotTicker.Stop()

	purgeTicker := time.NewTicker(1 * time.Minute)
	defer purgeTicker.Stop()

	wm.logger.Info("window state manager started",
		"grace_period", wm.gracePeriod,
		"window_sizes", wm.windowSizes,
		"snapshot_interval", wm.snapshotInterval,
	)

	for {
		select {
		case <-ctx.Done():
			wm.logger.Info("window state manager stopping (context cancelled)")
			wm.performShutdown(ctx)
			return

		case <-wm.stopCh:
			wm.logger.Info("window state manager stopping (Stop called)")
			wm.performShutdown(ctx)
			return

		case <-closeTicker.C:
			wm.checkClosedWindows(ctx)

		case <-snapshotTicker.C:
			if err := wm.Snapshot(ctx); err != nil {
				wm.logger.Error("failed to snapshot window state", "error", err)
			}

		case <-purgeTicker.C:
			purged := wm.PurgeStale(24 * time.Hour)
			if purged > 0 {
				wm.logger.Info("purged stale windows", "count", purged)
			}
		}
	}
}

// checkClosedWindows logs newly closed windows for observability.
func (wm *WindowStateManager) checkClosedWindows(ctx context.Context) {
	closed := wm.GetClosedWindows(ctx)
	if len(closed) > 0 {
		wm.logger.Debug("detected closed windows", "count", len(closed))
	}
}

// performShutdown does a final snapshot and logs the shutdown.
func (wm *WindowStateManager) performShutdown(ctx context.Context) {
	snapCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := wm.Snapshot(snapCtx); err != nil {
		wm.logger.Error("failed to snapshot on shutdown", "error", err)
	}

	wm.logger.Info("window state manager stopped",
		"remaining_windows", wm.WindowCount())
}

// Stop gracefully shuts down the window state manager.
// It signals the background goroutine to stop and waits for it to finish.
func (wm *WindowStateManager) Stop() {
	select {
	case <-wm.stopCh:
		// Already stopped.
		return
	default:
		close(wm.stopCh)
	}
	<-wm.doneCh
}

// ---- Zstd compression helpers ----

// zstdCompress compresses data using Zstd.
func zstdCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	if _, err := enc.Write(data); err != nil {
		enc.Close()
		return nil, fmt.Errorf("zstd write: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("zstd close: %w", err)
	}
	return buf.Bytes(), nil
}

// zstdDecompress decompresses Zstd-compressed data.
func zstdDecompress(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer dec.Close()

	result, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("zstd read: %w", err)
	}
	return result, nil
}
