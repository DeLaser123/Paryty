package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AlertSeverity represents the severity level of an alert
type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertStatus represents the current status of an alert
type AlertStatus string

const (
	AlertStatusFiring   AlertStatus = "firing"
	AlertStatusResolved AlertStatus = "resolved"
	AlertStatusPending  AlertStatus = "pending"
)

// Alert represents a monitoring alert
type Alert struct {
	Name        string        `json:"name"`
	Severity    AlertSeverity `json:"severity"`
	Status      AlertStatus   `json:"status"`
	Message     string        `json:"message"`
	Value       float64       `json:"value"`
	Threshold   float64       `json:"threshold"`
	StartedAt   time.Time     `json:"started_at"`
	ResolvedAt  *time.Time    `json:"resolved_at,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// AlertRule defines a rule for triggering alerts
type AlertRule struct {
	Name        string
	Severity    AlertSeverity
	Condition   func(metrics *SelfMonitoringMetrics) bool
	Message     func(metrics *SelfMonitoringMetrics) string
	Threshold   float64
	Duration    time.Duration // How long condition must be true before firing
}

// SelfMonitoringMetrics holds current system metrics
type SelfMonitoringMetrics struct {
	IngestionRate     float64 // metrics per second
	ProcessingLag     time.Duration
	QueryLatency      time.Duration
	StorageHealth     map[string]bool
	ActiveAgents      int
	ErrorRate         float64
	LastUpdated       time.Time
	// Audit log metrics
	FailedAuthRate    float64 // failed auth attempts per minute
	AuditWriteErrors  int64   // number of audit log write failures
	AdminActionRate   float64 // admin actions per minute (unusual if >50)
}

// AlertManager manages alert rules and active alerts
type AlertManager struct {
	rules       []AlertRule
	activeAlerts map[string]*Alert
	mu          sync.RWMutex
	logger      *slog.Logger
	webhookURL  string
}

// AlertManagerConfig configures the AlertManager
type AlertManagerConfig struct {
	Logger     *slog.Logger
	WebhookURL string
}

// NewAlertManager creates a new AlertManager
func NewAlertManager(cfg AlertManagerConfig) *AlertManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &AlertManager{
		rules:        make([]AlertRule, 0),
		activeAlerts: make(map[string]*Alert),
		logger:       cfg.Logger,
		webhookURL:   cfg.WebhookURL,
	}
}

// AddRule adds an alert rule
func (am *AlertManager) AddRule(rule AlertRule) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.rules = append(am.rules, rule)
}

// Evaluate evaluates all rules against current metrics
func (am *AlertManager) Evaluate(ctx context.Context, metrics *SelfMonitoringMetrics) []Alert {
	am.mu.Lock()
	defer am.mu.Unlock()

	var newAlerts []Alert

	for _, rule := range am.rules {
		if rule.Condition(metrics) {
			alert, exists := am.activeAlerts[rule.Name]
			if !exists {
				// New alert
				alert = &Alert{
					Name:      rule.Name,
					Severity:  rule.Severity,
					Status:    AlertStatusPending,
					Message:   rule.Message(metrics),
					Value:     metrics.IngestionRate,
					Threshold: rule.Threshold,
					StartedAt: time.Now(),
				}
				am.activeAlerts[rule.Name] = alert
				am.logger.Warn("alert condition detected",
					"alert", rule.Name,
					"severity", rule.Severity,
					"message", alert.Message,
				)
			} else if alert.Status == AlertStatusPending && time.Since(alert.StartedAt) >= rule.Duration {
				// Alert has been pending long enough, fire it
				alert.Status = AlertStatusFiring
				alert.Message = rule.Message(metrics)
				newAlerts = append(newAlerts, *alert)
				am.logger.Error("alert fired",
					"alert", rule.Name,
					"severity", rule.Severity,
					"message", alert.Message,
				)

				// Send webhook if configured
				if am.webhookURL != "" {
					go am.sendWebhook(ctx, *alert)
				}
			}
		} else {
			// Condition is false, resolve any pending/firing alert
			if alert, exists := am.activeAlerts[rule.Name]; exists {
				if alert.Status == AlertStatusFiring || alert.Status == AlertStatusPending {
					now := time.Now()
					alert.Status = AlertStatusResolved
					alert.ResolvedAt = &now
					am.logger.Info("alert resolved",
						"alert", rule.Name,
						"duration", time.Since(alert.StartedAt),
					)
					delete(am.activeAlerts, rule.Name)
				}
			}
		}
	}

	return newAlerts
}

// GetActiveAlerts returns all active alerts
func (am *AlertManager) GetActiveAlerts() []Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	alerts := make([]Alert, 0, len(am.activeAlerts))
	for _, alert := range am.activeAlerts {
		alerts = append(alerts, *alert)
	}
	return alerts
}

// sendWebhook sends an alert notification via webhook
func (am *AlertManager) sendWebhook(ctx context.Context, alert Alert) {
	// Implementation would send HTTP POST to webhook URL
	am.logger.Info("sending alert webhook",
		"alert", alert.Name,
		"webhook", am.webhookURL,
	)
}

// DefaultAlertRules returns the default set of alert rules for Paryty
func DefaultAlertRules() []AlertRule {
	return []AlertRule{
		{
			Name:     "low_ingestion_rate",
			Severity: AlertSeverityCritical,
			Condition: func(m *SelfMonitoringMetrics) bool {
				return m.IngestionRate < 100 // Less than 100 metrics/sec
			},
			Message: func(m *SelfMonitoringMetrics) string {
				return fmt.Sprintf("Ingestion rate dropped to %.2f metrics/sec (threshold: 100)", m.IngestionRate)
			},
			Threshold: 100,
			Duration:  1 * time.Minute,
		},
		{
			Name:     "high_processing_lag",
			Severity: AlertSeverityWarning,
			Condition: func(m *SelfMonitoringMetrics) bool {
				return m.ProcessingLag > 5*time.Second
			},
			Message: func(m *SelfMonitoringMetrics) string {
				return fmt.Sprintf("Processing lag is %v (threshold: 5s)", m.ProcessingLag)
			},
			Threshold: 5,
			Duration:  30 * time.Second,
		},
		{
			Name:     "high_query_latency",
			Severity: AlertSeverityWarning,
			Condition: func(m *SelfMonitoringMetrics) bool {
				return m.QueryLatency > 1*time.Second
			},
			Message: func(m *SelfMonitoringMetrics) string {
				return fmt.Sprintf("Query latency is %v (threshold: 1s)", m.QueryLatency)
			},
			Threshold: 1000,
			Duration:  1 * time.Minute,
		},
		{
			Name:     "storage_unreachable",
			Severity: AlertSeverityCritical,
			Condition: func(m *SelfMonitoringMetrics) bool {
				for _, healthy := range m.StorageHealth {
					if !healthy {
						return true
					}
				}
				return false
			},
			Message: func(m *SelfMonitoringMetrics) string {
				unhealthy := make([]string, 0)
				for name, healthy := range m.StorageHealth {
					if !healthy {
						unhealthy = append(unhealthy, name)
					}
				}
				return fmt.Sprintf("Storage backends unreachable: %v", unhealthy)
			},
			Threshold: 0,
			Duration:  10 * time.Second,
		},
		{
			Name:     "high_error_rate",
			Severity: AlertSeverityWarning,
			Condition: func(m *SelfMonitoringMetrics) bool {
				return m.ErrorRate > 0.01 // More than 1% errors
			},
			Message: func(m *SelfMonitoringMetrics) string {
				return fmt.Sprintf("Error rate is %.2f%% (threshold: 1%%)", m.ErrorRate*100)
			},
			Threshold: 0.01,
			Duration:  2 * time.Minute,
		},
		// Audit log security alerts
		{
			Name:     "high_failed_auth_rate",
			Severity: AlertSeverityCritical,
			Condition: func(m *SelfMonitoringMetrics) bool {
				return m.FailedAuthRate > 10 // More than 10 failed auths per minute
			},
			Message: func(m *SelfMonitoringMetrics) string {
				return fmt.Sprintf("Failed authentication rate is %.0f/min (threshold: 10). Possible brute-force attack.", m.FailedAuthRate)
			},
			Threshold: 10,
			Duration:  30 * time.Second,
		},
		{
			Name:     "audit_write_failures",
			Severity: AlertSeverityCritical,
			Condition: func(m *SelfMonitoringMetrics) bool {
				return m.AuditWriteErrors > 0
			},
			Message: func(m *SelfMonitoringMetrics) string {
				return fmt.Sprintf("Audit log write failures detected: %d events lost. SOC2 compliance at risk.", m.AuditWriteErrors)
			},
			Threshold: 1,
			Duration:  0, // Immediate
		},
		{
			Name:     "unusual_admin_activity",
			Severity: AlertSeverityWarning,
			Condition: func(m *SelfMonitoringMetrics) bool {
				return m.AdminActionRate > 50 // More than 50 admin actions per minute is unusual
			},
			Message: func(m *SelfMonitoringMetrics) string {
				return fmt.Sprintf("Unusual admin activity rate: %.0f actions/min (threshold: 50). Possible credential compromise.", m.AdminActionRate)
			},
			Threshold: 50,
			Duration:  1 * time.Minute,
		},
	}
}
