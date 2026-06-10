package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// =============================================================================
// Alert Interface
// =============================================================================

// AlertSeverity represents the severity level of an alert.
type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// Alert represents a monitoring alert.
type Alert struct {
	Name      string        `json:"name"`
	Severity  AlertSeverity `json:"severity"`
	Timestamp time.Time     `json:"timestamp"`
	Message   string        `json:"message"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// AlertChannel is the interface for sending alerts.
type AlertChannel interface {
	// Send dispatches an alert. Returns an error if delivery fails.
	Send(ctx context.Context, alert Alert) error
}

// =============================================================================
// WebhookAlertChannel
// =============================================================================

// WebhookAlertChannel sends alerts by POSTing JSON to a webhook URL.
type WebhookAlertChannel struct {
	webhookURL string
	client     *http.Client
	logger     *slog.Logger
	// maxRetries is the maximum number of retry attempts with backoff.
	maxRetries int
}

// WebhookConfig configures the WebhookAlertChannel.
type WebhookConfig struct {
	// WebhookURL is the URL to POST alerts to.
	// If empty, reads from WEBHOOK_ALERT_URL env var.
	WebhookURL string
	// HTTPTimeout is the timeout for each attempt. Default: 5s.
	HTTPTimeout time.Duration
	// MaxRetries for exponential backoff. Default: 3.
	MaxRetries int
	// Logger for structured logging.
	Logger *slog.Logger
}

// NewWebhookAlertChannel creates a new WebhookAlertChannel.
func NewWebhookAlertChannel(cfg WebhookConfig) *WebhookAlertChannel {
	url := cfg.WebhookURL
	if url == "" {
		url = os.Getenv("WEBHOOK_ALERT_URL")
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 5 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &WebhookAlertChannel{
		webhookURL: url,
		client:     &http.Client{Timeout: cfg.HTTPTimeout},
		logger:     cfg.Logger,
		maxRetries: cfg.MaxRetries,
	}
}

// Send dispatches an alert to the configured webhook URL with exponential backoff.
func (w *WebhookAlertChannel) Send(ctx context.Context, alert Alert) error {
	if w.webhookURL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}

	body, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}

	var lastErr error
	backoff := 1 * time.Second

	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		if attempt > 0 {
			w.logger.Warn("retrying alert webhook",
				"alert", alert.Name,
				"attempt", attempt,
				"backoff", backoff,
			)
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.webhookURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create webhook request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := w.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("POST webhook: %w", err)
			continue
		}

		// Accept 2xx as success.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			w.logger.Debug("alert sent via webhook",
				"alert", alert.Name,
				"severity", alert.Severity,
			)
			return nil
		}

		resp.Body.Close()
		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return fmt.Errorf("webhook delivery failed after %d attempts: %w", w.maxRetries+1, lastErr)
}

// =============================================================================
// NoOpAlertChannel
// =============================================================================

// NoOpAlertChannel is an alert channel that discards all alerts.
// Useful for testing or when alerting is not configured.
type NoOpAlertChannel struct{}

// Send discards the alert.
func (n *NoOpAlertChannel) Send(_ context.Context, _ Alert) error {
	return nil
}

// =============================================================================
// Alert Helpers
// =============================================================================

// NewAlert creates a new Alert with the current timestamp.
func NewAlert(name string, severity AlertSeverity, message string, labels map[string]string) Alert {
	return Alert{
		Name:      name,
		Severity:  severity,
		Timestamp: time.Now().UTC(),
		Message:   message,
		Labels:    labels,
	}
}

// HealthDownAlert creates a "service down" alert.
func HealthDownAlert(serviceName, details string) Alert {
	return NewAlert(
		"service_down",
		AlertSeverityCritical,
		fmt.Sprintf("Service %s is unhealthy: %s", serviceName, details),
		map[string]string{"service": serviceName},
	)
}

// HighErrorRateAlert creates a "high error rate" alert.
func HighErrorRateAlert(component string, rate float64) Alert {
	return NewAlert(
		"high_error_rate",
		AlertSeverityWarning,
		fmt.Sprintf("High error rate in %s: %.2f%%", component, rate*100),
		map[string]string{"component": component},
	)
}

// SLOWarningAlert creates an SLO budget burn alert.
func SLOWarningAlert(sloName string, burnRate float64) Alert {
	return NewAlert(
		"slo_burn_rate_warning",
		AlertSeverityWarning,
		fmt.Sprintf("SLO %s error budget burning at %.2fx rate", sloName, burnRate),
		map[string]string{"slo": sloName},
	)
}
