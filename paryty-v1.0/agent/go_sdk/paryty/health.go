package paryty

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// HealthStatus represents the health status.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthCheckType indicates how the check is triggered.
type HealthCheckType string

const (
	HealthCheckTypeLive   HealthCheckType = "liveness"
	HealthCheckTypeReady  HealthCheckType = "readiness"
	HealthCheckTypeCustom HealthCheckType = "custom"
)

// HealthCheckFunc is a function that performs a health check.
type HealthCheckFunc func(ctx context.Context) HealthCheckResult

// HealthCheckResult is the result of a single health check.
type HealthCheckResult struct {
	Name      string            `json:"name"`
	Type      HealthCheckType   `json:"type"`
	Status    HealthStatus      `json:"status"`
	Message   string            `json:"message,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Duration  time.Duration     `json:"duration"`
}

// HealthReport is an aggregated report of all health checks.
type HealthReport struct {
	ServiceName string              `json:"service_name"`
	Status      HealthStatus        `json:"status"`
	Checks      []HealthCheckResult `json:"checks"`
	Timestamp   time.Time           `json:"timestamp"`
	Hostname    string              `json:"hostname,omitempty"`
}

// HealthChecker manages health checks and periodic reporting.
type HealthChecker struct {
	mu          sync.RWMutex
	serviceName string
	checks      []registeredCheck
	hostname    string
	interval    time.Duration
	stopCh      chan struct{}
	lastReport  *HealthReport
}

type registeredCheck struct {
	name     string
	checkTyp HealthCheckType
	fn       HealthCheckFunc
}

// HealthOption is a functional option for HealthChecker.
type HealthOption func(*HealthChecker)

// WithCheckInterval sets the periodic health check interval.
func WithCheckInterval(d time.Duration) HealthOption {
	return func(h *HealthChecker) { h.interval = d }
}

// WithHostname sets the hostname in health reports.
func WithHostname(hostname string) HealthOption {
	return func(h *HealthChecker) { h.hostname = hostname }
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(serviceName string, opts ...HealthOption) *HealthChecker {
	h := &HealthChecker{
		serviceName: serviceName,
		interval:    30 * time.Second,
		stopCh:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterCheck registers a named health check function.
func (h *HealthChecker) RegisterCheck(name string, checkType HealthCheckType, fn HealthCheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks = append(h.checks, registeredCheck{
		name:     name,
		checkTyp: checkType,
		fn:       fn,
	})
}

// RegisterLivenessCheck registers a liveness check.
func (h *HealthChecker) RegisterLivenessCheck(name string, fn HealthCheckFunc) {
	h.RegisterCheck(name, HealthCheckTypeLive, fn)
}

// RegisterReadinessCheck registers a readiness check.
func (h *HealthChecker) RegisterReadinessCheck(name string, fn HealthCheckFunc) {
	h.RegisterCheck(name, HealthCheckTypeReady, fn)
}

// Run executes all health checks and returns an aggregated report.
func (h *HealthChecker) Run(ctx context.Context) *HealthReport {
	h.mu.RLock()
	checks := make([]registeredCheck, len(h.checks))
	copy(checks, h.checks)
	h.mu.RUnlock()

	results := make([]HealthCheckResult, 0, len(checks))
	overallStatus := HealthStatusHealthy

	for _, check := range checks {
		start := time.Now()
		result := check.fn(ctx)
		result.Name = check.name
		result.Type = check.checkTyp
		result.Timestamp = start
		result.Duration = time.Since(start)
		results = append(results, result)

		// Aggregate status: worst status wins
		switch result.Status {
		case HealthStatusUnhealthy:
			overallStatus = HealthStatusUnhealthy
		case HealthStatusDegraded:
			if overallStatus != HealthStatusUnhealthy {
				overallStatus = HealthStatusDegraded
			}
		case HealthStatusUnknown:
			if overallStatus == HealthStatusHealthy {
				overallStatus = HealthStatusUnknown
			}
		}
	}

	report := &HealthReport{
		ServiceName: h.serviceName,
		Status:      overallStatus,
		Checks:      results,
		Timestamp:   time.Now(),
		Hostname:    h.hostname,
	}

	h.mu.Lock()
	h.lastReport = report
	h.mu.Unlock()

	return report
}

// LastReport returns the most recent health report.
func (h *HealthChecker) LastReport() *HealthReport {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastReport
}

// StartPeriodic starts periodic health check reporting.
// It calls the reportCallback with each health report.
func (h *HealthChecker) StartPeriodic(ctx context.Context, reportCallback func(*HealthReport)) {
	go func() {
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()

		// Run immediately on start
		report := h.Run(ctx)
		if reportCallback != nil {
			reportCallback(report)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-h.stopCh:
				return
			case <-ticker.C:
				report := h.Run(ctx)
				if reportCallback != nil {
					reportCallback(report)
				}
			}
		}
	}()
}

// Stop stops periodic health check reporting.
func (h *HealthChecker) Stop() {
	select {
	case <-h.stopCh:
		// Already stopped
	default:
		close(h.stopCh)
	}
}

// Convenience health check functions.

// TCPHealthCheck creates a health check that verifies TCP connectivity.
func TCPHealthCheck(addr string) HealthCheckFunc {
	return func(ctx context.Context) HealthCheckResult {
		start := time.Now()
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return HealthCheckResult{
				Status:  HealthStatusUnhealthy,
				Message: "TCP connection failed: " + err.Error(),
			}
		}
		conn.Close()
		return HealthCheckResult{
			Status:   HealthStatusHealthy,
			Duration: time.Since(start),
		}
	}
}

// HTTPHealthCheck creates a health check that verifies HTTP endpoint.
func HTTPHealthCheck(url string) HealthCheckFunc {
	return func(ctx context.Context) HealthCheckResult {
		start := time.Now()
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return HealthCheckResult{
				Status:  HealthStatusUnhealthy,
				Message: "failed to create request: " + err.Error(),
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return HealthCheckResult{
				Status:  HealthStatusUnhealthy,
				Message: "HTTP check failed: " + err.Error(),
			}
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return HealthCheckResult{
				Status:   HealthStatusHealthy,
				Duration: time.Since(start),
				Details:  map[string]string{"status_code": fmt.Sprintf("%d", resp.StatusCode)},
			}
		}
		status := HealthStatusDegraded
		if resp.StatusCode >= 500 {
			status = HealthStatusUnhealthy
		}
		return HealthCheckResult{
			Status:   status,
			Message:  fmt.Sprintf("HTTP status %d", resp.StatusCode),
			Duration: time.Since(start),
		}
	}
}
