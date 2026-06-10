package monitoring

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// =============================================================================
// HealthFallback — Independent Health Checker
// =============================================================================

// HealthFallback performs independent health checks that work even when
// the primary health system is unavailable. It uses direct TCP dials,
// HTTP endpoint checks, and ping-based latency measurements.
type HealthFallback struct {
	config HealthFallbackConfig
}

// HealthFallbackConfig configures the HealthFallback checker.
type HealthFallbackConfig struct {
	// DefaultTimeout is the timeout for each individual check. Default: 2s.
	DefaultTimeout time.Duration
	// MaxRetries is the number of retries for transient failures. Default: 2.
	MaxRetries int
	// RetryDelay is the delay between retries. Default: 500ms.
	RetryDelay time.Duration
}

// ServiceHealth represents the health status of a single service.
type ServiceHealth struct {
	Name        string        `json:"name"`
	Address     string        `json:"address"`
	Healthy     bool          `json:"healthy"`
	Latency     time.Duration `json:"latency"`
	Error       string        `json:"error,omitempty"`
	CheckedAt   time.Time     `json:"checked_at"`
}

// HealthReport is a structured health report for all services.
type HealthReport struct {
	Timestamp time.Time       `json:"timestamp"`
	Overall   bool            `json:"overall_healthy"`
	Services  []ServiceHealth `json:"services"`
}

// NewHealthFallback creates a new HealthFallback checker.
func NewHealthFallback(cfg HealthFallbackConfig) *HealthFallback {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 2 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 2
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 500 * time.Millisecond
	}
	return &HealthFallback{config: cfg}
}

// CheckTCP performs a TCP dial check on the given address.
// Returns latency on success, or an error.
func (h *HealthFallback) CheckTCP(ctx context.Context, address string) (time.Duration, error) {
	start := time.Now()

	var lastErr error
	for attempt := 0; attempt <= h.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(h.config.RetryDelay):
			}
		}

		dialer := net.Dialer{Timeout: h.config.DefaultTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			lastErr = err
			continue
		}
		conn.Close()
		return time.Since(start), nil
	}
	return 0, fmt.Errorf("TCP dial %s: %w (after %d retries)", address, lastErr, h.config.MaxRetries)
}

// CheckHTTP performs an HTTP GET health check on the given URL.
// Expects 2xx status code. Returns latency on success.
func (h *HealthFallback) CheckHTTP(ctx context.Context, url string) (time.Duration, error) {
	start := time.Now()

	var lastErr error
	for attempt := 0; attempt <= h.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(h.config.RetryDelay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, fmt.Errorf("create request for %s: %w", url, err)
		}

		client := &http.Client{Timeout: h.config.DefaultTimeout}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
			continue
		}
		return time.Since(start), nil
	}
	return 0, fmt.Errorf("HTTP GET %s: %w (after %d retries)", url, lastErr, h.config.MaxRetries)
}

// ServiceTarget defines a service to check.
type ServiceTarget struct {
	Name    string // e.g., "query-service"
	Address string // e.g., "localhost:8080" for TCP or "http://localhost:8080/health" for HTTP
	Mode    string // "tcp" or "http"
}

// CheckAll checks all configured services and returns a HealthReport.
func (h *HealthFallback) CheckAll(ctx context.Context, services []ServiceTarget) *HealthReport {
	report := &HealthReport{
		Timestamp: time.Now().UTC(),
		Overall:   true,
		Services:  make([]ServiceHealth, len(services)),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, svc := range services {
		i, svc := i, svc
		wg.Add(1)
		go func() {
			defer wg.Done()

			var latency time.Duration
			var err error
			checkedAt := time.Now().UTC()

			switch svc.Mode {
			case "http":
				latency, err = h.CheckHTTP(ctx, svc.Address)
			default: // "tcp" or empty
				latency, err = h.CheckTCP(ctx, svc.Address)
			}

			sh := ServiceHealth{
				Name:      svc.Name,
				Address:   svc.Address,
				Healthy:   err == nil,
				Latency:   latency,
				CheckedAt: checkedAt,
			}
			if err != nil {
				sh.Error = err.Error()
			}

			mu.Lock()
			report.Services[i] = sh
			if !sh.Healthy {
				report.Overall = false
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	return report
}

// StandardServices returns the default set of Paryty Cluster services.
func StandardServices() []ServiceTarget {
	return []ServiceTarget{
		{Name: "query-service", Address: "localhost:8080", Mode: "http"},
		{Name: "ingestion-grpc", Address: "localhost:9090", Mode: "tcp"},
		{Name: "dragonfly", Address: "localhost:6379", Mode: "tcp"},
		{Name: "questdb", Address: "localhost:8812", Mode: "tcp"},
		{Name: "redpanda", Address: "localhost:9092", Mode: "tcp"},
	}
}
