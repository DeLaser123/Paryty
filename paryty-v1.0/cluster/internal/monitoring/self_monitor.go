// Package monitoring implements self-monitoring, health fallback checks,
// alert webhooks, and SLO tracking for the Paryty Cluster.
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// =============================================================================
// SelfMonitor
// =============================================================================

// HealthCheckFunc is a function that checks the health of a component and
// returns nil if healthy, or an error describing the issue.
type HealthCheckFunc func(ctx context.Context) error

// CheckResult holds the outcome of a single health check.
type CheckResult struct {
	Name      string        `json:"name"`
	Healthy   bool          `json:"healthy"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
}

// ResourceMetrics holds runtime resource usage snapshots.
type ResourceMetrics struct {
	GoroutineCount int           `json:"goroutine_count"`
	HeapAllocBytes uint64        `json:"heap_alloc_bytes"`
	HeapInUseBytes uint64        `json:"heap_in_use_bytes"`
	GCPauseNs      uint64        `json:"gc_pause_ns"`
	NumGC          uint32        `json:"num_gc"`
	Timestamp      time.Time     `json:"timestamp"`
}

// MetricReporter is called with CheckResult and ResourceMetrics for reporting
// to the ingestion pipeline or external monitoring systems.
type MetricReporter func(ctx context.Context, results []CheckResult, metrics ResourceMetrics)

// SelfMonitor periodically runs health checks and collects resource metrics,
// reporting them via the configured MetricReporter.
type SelfMonitor struct {
	checks       []namedCheck
	interval     time.Duration
	reporter     MetricReporter
	logger       *slog.Logger
	mu           sync.Mutex
	running      bool
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

type namedCheck struct {
	name  string
	check HealthCheckFunc
}

// SelfMonitorConfig configures the SelfMonitor.
type SelfMonitorConfig struct {
	// Interval is how often health checks run. Default: 30s.
	Interval time.Duration
	// Reporter is called with health check results and resource metrics.
	// If nil, results are logged only.
	Reporter MetricReporter
	// Logger for structured logging. Uses slog.Default() if nil.
	Logger *slog.Logger
}

// NewSelfMonitor creates a new SelfMonitor.
func NewSelfMonitor(cfg SelfMonitorConfig) *SelfMonitor {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &SelfMonitor{
		interval: cfg.Interval,
		reporter: cfg.Reporter,
		logger:   cfg.Logger,
		stopCh:   make(chan struct{}),
	}
}

// RegisterCheck adds a named health check. Must be called before Start.
func (m *SelfMonitor) RegisterCheck(name string, check HealthCheckFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks = append(m.checks, namedCheck{name: name, check: check})
}

// Start begins periodic health checking. Non-blocking; runs in background goroutines.
func (m *SelfMonitor) Start(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.loop(ctx)
}

// loop is the main monitoring loop.
func (m *SelfMonitor) loop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run immediately on start.
	m.runChecks(ctx)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("self-monitor stopping (context cancelled)")
			return
		case <-m.stopCh:
			m.logger.Info("self-monitor stopping (stop requested)")
			return
		case <-ticker.C:
			m.runChecks(ctx)
		}
	}
}

// Stop gracefully stops the self-monitor.
func (m *SelfMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	m.running = false
	close(m.stopCh)
	m.wg.Wait()
}

// runChecks executes all registered health checks and collects metrics.
func (m *SelfMonitor) runChecks(ctx context.Context) {
	var wg sync.WaitGroup
	results := make([]CheckResult, len(m.checks))

	for i, nc := range m.checks {
		i, nc := i, nc
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			err := nc.check(checkCtx)
			results[i] = CheckResult{
				Name:      nc.name,
				Healthy:   err == nil,
				Duration:  time.Since(start),
				Timestamp: start,
			}
			if err != nil {
				results[i].Error = err.Error()
			}
		}()
	}
	wg.Wait()

	metrics := collectResourceMetrics()

	// Log results.
	for _, r := range results {
		if r.Healthy {
			m.logger.Debug("health check passed",
				"check", r.Name,
				"duration_ms", r.Duration.Milliseconds(),
			)
		} else {
			m.logger.Warn("health check failed",
				"check", r.Name,
				"error", r.Error,
				"duration_ms", r.Duration.Milliseconds(),
			)
		}
	}

	m.logger.Debug("resource metrics",
		"goroutines", metrics.GoroutineCount,
		"heap_alloc_mb", float64(metrics.HeapAllocBytes)/(1024*1024),
		"gc_pause_us", float64(metrics.GCPauseNs)/1000,
	)

	// Report if a reporter is configured.
	if m.reporter != nil {
		m.reporter(ctx, results, metrics)
	}
}

// collectResourceMetrics captures current runtime metrics.
func collectResourceMetrics() ResourceMetrics {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return ResourceMetrics{
		GoroutineCount: runtime.NumGoroutine(),
		HeapAllocBytes: mem.HeapAlloc,
		HeapInUseBytes: mem.HeapInuse,
		GCPauseNs:      mem.PauseNs[(mem.NumGC+255)%256],
		NumGC:          mem.NumGC,
		Timestamp:      time.Now().UTC(),
	}
}

// =============================================================================
// Standard Health Checks
// =============================================================================

// HTTPHealthCheck creates a HealthCheckFunc that performs an HTTP GET to the
// given URL and expects a 2xx status code.
func HTTPHealthCheck(name, url string, timeout time.Duration) HealthCheckFunc {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("%s: create request: %w", name, err)
		}

		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("%s: HTTP GET %s: %w", name, url, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("%s: unexpected status %d from %s", name, resp.StatusCode, url)
		}
		return nil
	}
}

// =============================================================================
// Self-Monitoring Report Types (for ingestion pipeline)
// =============================================================================

// MonitoringReport is the structure sent to the ingestion pipeline.
type MonitoringReport struct {
	Source   string          `json:"source"`
	Checks   []CheckResult   `json:"checks"`
	Metrics  ResourceMetrics `json:"metrics"`
}

// ToJSON serializes the report to JSON bytes.
func (r *MonitoringReport) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}

// LoggingReporter returns a MetricReporter that logs results at the given level.
func LoggingReporter(logger *slog.Logger) MetricReporter {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, results []CheckResult, metrics ResourceMetrics) {
		report := MonitoringReport{
			Source:  "paryty-self-monitor",
			Checks:  results,
			Metrics: metrics,
		}
		data, err := report.ToJSON()
		if err != nil {
			logger.Error("failed to marshal monitoring report", "error", err)
			return
		}
		logger.Info("self-monitoring report", "data", string(data))
	}
}
