package models

import (
	"time"
)

// AlertSeverity represents the severity level of an alert.
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityError    AlertSeverity = "error"
	AlertSeverityCritical AlertSeverity = "critical"
)

// AlertStatus represents the current status of an alert.
type AlertStatus string

const (
	AlertStatusPending  AlertStatus = "pending"
	AlertStatusFiring   AlertStatus = "firing"
	AlertStatusResolved AlertStatus = "resolved"
	AlertStatusSilenced AlertStatus = "silenced"
)

// Alert represents an alert triggered by the system.
type Alert struct {
	ID          string            `json:"id" db:"id"`
	Name        string            `json:"name" db:"name"`
	Description string            `json:"description" db:"description"`
	Severity    AlertSeverity     `json:"severity" db:"severity"`
	Status      AlertStatus       `json:"status" db:"status"`
	Source      string            `json:"source" db:"source"`
	AgentID     string            `json:"agent_id" db:"agent_id"`
	Labels      map[string]string `json:"labels" db:"labels"`
	Annotations map[string]string `json:"annotations" db:"annotations"`
	StartsAt    time.Time         `json:"starts_at" db:"starts_at"`
	EndsAt      *time.Time        `json:"ends_at,omitempty" db:"ends_at"`
	UpdatedAt   time.Time         `json:"updated_at" db:"updated_at"`
	Value       float64           `json:"value" db:"value"`
	Threshold   float64           `json:"threshold" db:"threshold"`
}

// AlertRule defines a rule for triggering alerts.
type AlertRule struct {
	ID          string            `json:"id" db:"id"`
	Name        string            `json:"name" db:"name"`
	Description string            `json:"description" db:"description"`
	Severity    AlertSeverity     `json:"severity" db:"severity"`
	MetricName  string            `json:"metric_name" db:"metric_name"`
	Condition   AlertCondition    `json:"condition" db:"condition"`
	Threshold   float64           `json:"threshold" db:"threshold"`
	Duration    time.Duration     `json:"duration" db:"duration"`
	Labels      map[string]string `json:"labels" db:"labels"`
	Enabled     bool              `json:"enabled" db:"enabled"`
	CreatedAt   time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at" db:"updated_at"`
}

// AlertCondition represents the condition for triggering an alert.
type AlertCondition string

const (
	AlertConditionAbove AlertCondition = "above"
	AlertConditionBelow AlertCondition = "below"
	AlertConditionEqual AlertCondition = "equal"
)

// Forecast represents a predicted future value for a metric.
type Forecast struct {
	AgentID    string    `json:"agent_id" db:"agent_id"`
	Metric     string    `json:"metric" db:"metric"`
	Current    float64   `json:"current" db:"current"`
	Predicted  float64   `json:"predicted" db:"predicted"`
	Lower      float64   `json:"lower" db:"lower"`
	Upper      float64   `json:"upper" db:"upper"`
	At         time.Time `json:"at" db:"at"`
	Confidence float64   `json:"confidence" db:"confidence"`
}
