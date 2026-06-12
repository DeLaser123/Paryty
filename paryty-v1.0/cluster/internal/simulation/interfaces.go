// Package simulation implements the Simulation Engine for Paryty V2.0 migration.
// It provides what-if scenario analysis, chaos engineering, load testing,
// bot detection, degradation simulation, and capacity planning.
//
// V2.0 Migration: This package replaces the Python-based simulation service
// with a native Go implementation for lower latency and tighter integration
// with the storage and streaming layers.
package simulation

import (
	"context"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
)

// ChangeType represents the type of infrastructure change in a what-if scenario.
//
// V2.0 Migration: Replaces the Python InfraChangeType enum with a Go string type
// for compile-time safety and JSON serialization compatibility.
type ChangeType string

const (
	// ChangeTypeHost represents changes to bare-metal or virtual hosts.
	ChangeTypeHost ChangeType = "host"
	// ChangeTypeContainer represents changes to container instances.
	ChangeTypeContainer ChangeType = "container"
	// ChangeTypeService represents changes to service replicas or configuration.
	ChangeTypeService ChangeType = "service"
	// ChangeTypeNetwork represents changes to network connectivity or latency.
	ChangeTypeNetwork ChangeType = "network"
)

// ScenarioStatus represents the lifecycle state of a simulation scenario.
//
// V2.0 Migration: Maps 1:1 to the Python ScenarioStatus enum. The Cancel
// transition is new in V2.0 and was not available in the Python implementation.
type ScenarioStatus string

const (
	// ScenarioStatusPending indicates the scenario is queued but not yet running.
	ScenarioStatusPending ScenarioStatus = "pending"
	// ScenarioStatusRunning indicates the scenario is actively executing.
	ScenarioStatusRunning ScenarioStatus = "running"
	// ScenarioStatusComplete indicates the scenario finished successfully.
	ScenarioStatusComplete ScenarioStatus = "complete"
	// ScenarioStatusFailed indicates the scenario encountered an error.
	ScenarioStatusFailed ScenarioStatus = "failed"
	// ScenarioStatusCanceled indicates the scenario was canceled by the user.
	ScenarioStatusCanceled ScenarioStatus = "canceled"
)

// TrafficType represents the source of traffic for simulation.
//
// V2.0 Migration: The Mirror type is new in V2.0 and enables shadow traffic
// mirroring from production, which the Python version did not support.
type TrafficType string

const (
	// TrafficTypeSimulated generates synthetic traffic based on the profile.
	TrafficTypeSimulated TrafficType = "simulated"
	// TrafficTypeReplay replays captured traffic from historical data.
	TrafficTypeReplay TrafficType = "replay"
	// TrafficTypeMirror mirrors live production traffic (V2.0 only).
	TrafficTypeMirror TrafficType = "mirror"
)

// DegradationType represents the type of performance degradation to simulate.
//
// V2.0 Migration: Consolidates the Python DegradationType and FailureMode
// enums into a single type. The NetworkCongestion type replaces the separate
// NetworkLatency and NetworkLoss Python types.
type DegradationType string

const (
	// DegradationTypeCPUSaturation simulates gradual CPU exhaustion.
	DegradationTypeCPUSaturation DegradationType = "cpu_saturation"
	// DegradationTypeMemoryLeak simulates gradual memory exhaustion.
	DegradationTypeMemoryLeak DegradationType = "memory_leak"
	// DegradationTypeDiskFill simulates gradual disk space exhaustion.
	DegradationTypeDiskFill DegradationType = "disk_fill"
	// DegradationTypeNetworkCongestion simulates increasing network latency and packet loss.
	DegradationTypeNetworkCongestion DegradationType = "network_congestion"
)

// WhatIfScenario defines a what-if simulation scenario.
//
// V2.0 Migration: Replaces the Python WhatIfScenario dataclass. The Tags field
// is new in V2.0 for scenario organization and filtering. The Changes field now
// uses typed InfraChange instead of Python's dict-based representation.
type WhatIfScenario struct {
	// ID is the unique identifier for this scenario.
	ID string `json:"id"`
	// Name is the human-readable name of the scenario.
	Name string `json:"name"`
	// Description explains what the scenario tests.
	Description string `json:"description"`
	// Changes defines infrastructure modifications to apply.
	Changes []InfraChange `json:"changes"`
	// Traffic defines the traffic pattern for the simulation.
	Traffic TrafficPattern `json:"traffic"`
	// Duration is how long the simulation should run.
	Duration time.Duration `json:"duration"`
	// Metrics lists the metric names to track during simulation.
	Metrics []string `json:"metrics"`
	// Tags are arbitrary labels for filtering and organization.
	Tags []string `json:"tags"`
	// TenantID is the tenant that owns this scenario.
	TenantID string `json:"tenant_id"`
	// CreatedAt is when the scenario was created.
	CreatedAt time.Time `json:"created_at"`
}

// InfraChange describes a single infrastructure modification.
//
// V2.0 Migration: Replaces Python's untyped dict with a strongly-typed struct.
// The Config field preserves arbitrary key-value configuration that was
// previously represented as a Python dict.
type InfraChange struct {
	// Type is the kind of infrastructure resource to modify.
	Type ChangeType `json:"type"`
	// Target identifies the specific resource (e.g., host ID, service name).
	Target string `json:"target"`
	// Action is the operation to perform (add, remove, scale, modify).
	Action string `json:"action"`
	// Count is the number of instances for add/remove/scale operations.
	Count int `json:"count"`
	// Config holds additional configuration for the change.
	Config map[string]string `json:"config,omitempty"`
}

// TrafficPattern defines how traffic should be generated for a simulation.
//
// V2.0 Migration: The Profile field replaces Python's traffic_config dict.
// V2.0 adds the Mirror source type for production traffic shadowing.
type TrafficPattern struct {
	// Type is the traffic source type.
	Type TrafficType `json:"type"`
	// RPS is the target requests per second.
	RPS float64 `json:"rps"`
	// Duration is how long to generate traffic.
	Duration time.Duration `json:"duration"`
	// Profile holds additional traffic shaping configuration.
	Profile map[string]string `json:"profile,omitempty"`
}

// SimulationResult contains the complete outcome of a simulation run.
//
// V2.0 Migration: Replaces the Python SimulationResult dataclass. Adds the
// BreakingPoint field which was computed separately in Python. The Summary
// is now computed inline rather than via a separate Python Celery task.
type SimulationResult struct {
	// ScenarioID is the ID of the scenario that produced this result.
	ScenarioID string `json:"scenario_id"`
	// Status is the final status of the simulation.
	Status ScenarioStatus `json:"status"`
	// StartTime is when the simulation began executing.
	StartTime time.Time `json:"start_time"`
	// EndTime is when the simulation finished (zero if still running).
	EndTime time.Time `json:"end_time"`
	// Metrics contains collected metric time series keyed by metric name.
	Metrics map[string][]MetricPoint `json:"metrics"`
	// Summary contains aggregated result metrics.
	Summary SimulationSummary `json:"summary"`
	// BreakingPoint identifies the system's capacity limits.
	BreakingPoint *BreakingPoint `json:"breaking_point,omitempty"`
	// Error contains the failure reason when Status is Failed.
	Error string `json:"error,omitempty"`
}

// MetricPoint represents a single metric observation during simulation.
//
// V2.0 Migration: New type not present in Python. Replaces the ad-hoc
// tuple representation (timestamp, value) used in the Python results.
type MetricPoint struct {
	// Timestamp is when the metric was observed.
	Timestamp time.Time `json:"timestamp"`
	// Value is the metric value at that point in time.
	Value float64 `json:"value"`
}

// SimulationSummary contains aggregated metrics from a simulation run.
//
// V2.0 Migration: Replaces the Python SimulationSummary dataclass. The
// CostEstimate field is new in V2.0 for cloud cost projections.
type SimulationSummary struct {
	// TotalRequests is the total number of requests sent during the simulation.
	TotalRequests int64 `json:"total_requests"`
	// SuccessRate is the fraction of successful responses (0.0 to 1.0).
	SuccessRate float64 `json:"success_rate"`
	// AvgLatency is the average response latency.
	AvgLatency time.Duration `json:"avg_latency"`
	// P99Latency is the 99th percentile response latency.
	P99Latency time.Duration `json:"p99_latency"`
	// MaxCPU is the peak CPU utilization observed (0.0 to 100.0).
	MaxCPU float64 `json:"max_cpu"`
	// MaxMemory is the peak memory utilization observed (0.0 to 100.0).
	MaxMemory float64 `json:"max_memory"`
	// CostEstimate is the projected monthly cost in USD.
	CostEstimate float64 `json:"cost_estimate"`
	// Recommendations contains actionable suggestions based on the results.
	Recommendations []string `json:"recommendations"`
}

// BreakingPoint identifies the system's capacity ceiling.
//
// V2.0 Migration: New type not present in Python. The Python version computed
// breaking points as a post-processing step; V2.0 computes them inline during
// simulation execution for faster feedback.
type BreakingPoint struct {
	// RPS is the requests-per-second at which the system broke.
	RPS float64 `json:"rps"`
	// Latency is the latency at the breaking point.
	Latency time.Duration `json:"latency"`
	// ErrorRate is the error rate at the breaking point (0.0 to 1.0).
	ErrorRate float64 `json:"error_rate"`
	// Bottleneck identifies the limiting resource (cpu, memory, network, disk).
	Bottleneck string `json:"bottleneck"`
	// Recommendations suggests how to increase capacity.
	Recommendations []string `json:"recommendations"`
}

// LoadTestConfig configures a k6-based load test.
//
// V2.0 Migration: Replaces the Python K6Config dataclass. Adds ScriptTemplate
// for custom k6 script generation instead of relying on a fixed Python template.
type LoadTestConfig struct {
	// TargetURL is the endpoint to test.
	TargetURL string `json:"target_url"`
	// VUs is the number of virtual users.
	VUs int `json:"vus"`
	// Duration is how long the test should run.
	Duration time.Duration `json:"duration"`
	// RampUp is the time to ramp from 0 to VUs.
	RampUp time.Duration `json:"ramp_up"`
	// Thresholds defines pass/fail criteria (e.g., "p99<500ms").
	Thresholds []string `json:"thresholds,omitempty"`
	// ScriptTemplate is an optional custom k6 script (empty = auto-generate).
	ScriptTemplate string `json:"script_template,omitempty"`
	// Tags are applied to results for filtering.
	Tags map[string]string `json:"tags,omitempty"`
}

// LoadTestResult contains the outcome of a k6 load test.
//
// V2.0 Migration: Replaces the Python K6Result dataclass. Adds the RawOutput
// field for preserving the full k6 JSON summary for debugging.
type LoadTestResult struct {
	// Config is the configuration used for the test.
	Config LoadTestConfig `json:"config"`
	// TotalRequests is the total number of HTTP requests made.
	TotalRequests int64 `json:"total_requests"`
	// SuccessRate is the fraction of successful responses (0.0 to 1.0).
	SuccessRate float64 `json:"success_rate"`
	// AvgLatency is the average response latency.
	AvgLatency time.Duration `json:"avg_latency"`
	// P50Latency is the median response latency.
	P50Latency time.Duration `json:"p50_latency"`
	// P95Latency is the 95th percentile response latency.
	P95Latency time.Duration `json:"p95_latency"`
	// P99Latency is the 99th percentile response latency.
	P99Latency time.Duration `json:"p99_latency"`
	// MaxLatency is the maximum observed latency.
	MaxLatency time.Duration `json:"max_latency"`
	// RequestsPerSec is the achieved throughput.
	RequestsPerSec float64 `json:"requests_per_sec"`
	// ErrorCount is the total number of failed requests.
	ErrorCount int64 `json:"error_count"`
	// DataReceived is the total bytes received.
	DataReceived int64 `json:"data_received"`
	// DataSent is the total bytes sent.
	DataSent int64 `json:"data_sent"`
	// Duration is the actual test duration.
	Duration time.Duration `json:"duration"`
	// Passed indicates whether all thresholds were met.
	Passed bool `json:"passed"`
	// RawOutput is the raw k6 JSON summary output.
	RawOutput []byte `json:"raw_output,omitempty"`
}

// ChaosExperiment defines a chaos engineering experiment via LitmusChaos.
//
// V2.0 Migration: Replaces the Python ChaosExperiment dataclass. The Engine
// field supports both LitmusChaos and custom chaos providers.
type ChaosExperiment struct {
	// Name is the human-readable experiment name.
	Name string `json:"name"`
	// Engine is the chaos engine to use (litmus, custom).
	Engine string `json:"engine"`
	// Target identifies the resource to attack.
	Target string `json:"target"`
	// Fault is the fault type to inject (pod-kill, cpu-stress, etc.).
	Fault string `json:"fault"`
	// Duration is how long the fault should be injected.
	Duration time.Duration `json:"duration"`
	// Intensity is the fault severity (0.0 to 1.0).
	Intensity float64 `json:"intensity"`
	// Labels are applied to the experiment for filtering.
	Labels map[string]string `json:"labels,omitempty"`
}

// ChaosResult contains the outcome of a chaos experiment.
//
// V2.0 Migration: Replaces the Python ChaosResult dataclass. The ProbeResults
// field is new in V2.0 for structured probe outcome tracking.
type ChaosResult struct {
	// Experiment is the experiment configuration that was executed.
	Experiment ChaosExperiment `json:"experiment"`
	// Status is the final experiment status.
	Status string `json:"status"`
	// StartTime is when the experiment began.
	StartTime time.Time `json:"start_time"`
	// EndTime is when the experiment ended.
	EndTime time.Time `json:"end_time"`
	// SteadyStateBefore contains metrics captured before fault injection.
	SteadyStateBefore map[string]float64 `json:"steady_state_before"`
	// SteadyStateAfter contains metrics captured after fault injection.
	SteadyStateAfter map[string]float64 `json:"steady_state_after"`
	// Recovered indicates whether the system returned to steady state.
	Recovered bool `json:"recovered"`
	// RecoveryTime is how long it took to recover (zero if not recovered).
	RecoveryTime time.Duration `json:"recovery_time"`
	// ProbeResults contains individual probe outcomes.
	ProbeResults []ProbeResult `json:"probe_results"`
}

// ProbeResult represents the outcome of a single chaos probe.
type ProbeResult struct {
	// Name is the probe identifier.
	Name string `json:"name"`
	// Passed indicates whether the probe succeeded.
	Passed bool `json:"passed"`
	// Message contains a human-readable description of the outcome.
	Message string `json:"message"`
}

// BotDetectionResult contains the analysis of potential bot traffic.
//
// V2.0 Migration: New type not present in the Python version. The Python
// version used a separate microservice for bot detection; V2.0 integrates
// it directly into the simulation engine.
type BotDetectionResult struct {
	// IsBot indicates whether bot traffic was detected.
	IsBot bool `json:"is_bot"`
	// Confidence is the detection confidence (0.0 to 1.0).
	Confidence float64 `json:"confidence"`
	// RequestRegularity measures how regular request intervals are (0.0 to 1.0).
	RequestRegularity float64 `json:"request_regularity"`
	// IPDiversity measures the ratio of unique IPs to total requests.
	IPDiversity float64 `json:"ip_diversity"`
	// SuspiciousUserAgents lists detected bot-like user agent strings.
	SuspiciousUserAgents []string `json:"suspicious_user_agents"`
	// AvgSessionDuration is the average session duration.
	AvgSessionDuration time.Duration `json:"avg_session_duration"`
	// RequestPattern describes the detected pattern.
	RequestPattern string `json:"request_pattern"`
	// Recommendation suggests mitigation actions.
	Recommendation string `json:"recommendation"`
}

// CapacityReport contains capacity planning analysis.
//
// V2.0 Migration: Replaces the Python CapacityReport dataclass. Adds the
// CostProjection field for multi-month cloud cost forecasting.
type CapacityReport struct {
	// GeneratedAt is when the report was generated.
	GeneratedAt time.Time `json:"generated_at"`
	// CurrentUsage maps resource names to current utilization (0.0 to 100.0).
	CurrentUsage map[string]float64 `json:"current_usage"`
	// ProjectedUsage maps resource names to projected utilization in 30 days.
	ProjectedUsage map[string]float64 `json:"projected_usage"`
	// DaysUntilFull maps resource names to estimated days until capacity exhaustion.
	DaysUntilFull map[string]int `json:"days_until_full"`
	// Recommendations contains scaling recommendations per resource.
	Recommendations []CapacityRecommendation `json:"recommendations"`
	// CostProjection is the projected monthly cost in USD.
	CostProjection float64 `json:"cost_projection"`
}

// CapacityRecommendation is a single scaling recommendation.
type CapacityRecommendation struct {
	// Resource is the resource type (cpu, memory, disk, network).
	Resource string `json:"resource"`
	// CurrentCapacity is the current provisioned capacity.
	CurrentCapacity string `json:"current_capacity"`
	// RecommendedCapacity is the suggested capacity.
	RecommendedCapacity string `json:"recommended_capacity"`
	// Urgency is the priority (low, medium, high, critical).
	Urgency string `json:"urgency"`
	// Reason explains why this change is recommended.
	Reason string `json:"reason"`
	// EstimatedCost is the estimated monthly cost in USD.
	EstimatedCost float64 `json:"estimated_cost"`
}

// StorageReader defines the storage operations needed by the simulation engine.
// This interface allows testing with mock implementations.
type StorageReader interface {
	// GetTopology retrieves the current topology from hot storage.
	GetTopology(ctx context.Context, tenant string) (*models.Topology, error)
	// GetLatestMetrics retrieves the latest metrics from hot storage.
	GetLatestMetrics(ctx context.Context, tenant, agentID string) (*models.MetricBatch, error)
	// GetAllAgentStates retrieves all agent states from hot storage.
	GetAllAgentStates(ctx context.Context, tenant string) ([]models.AgentInfo, error)
	// GetActiveAlerts retrieves active alerts from hot storage.
	GetActiveAlerts(ctx context.Context, tenant string) ([]models.Alert, error)
	// QueryMetrics queries metrics from warm storage within a time range,
	// scoped to the given tenant.
	QueryMetrics(ctx context.Context, tenant, agentID, metricName string, start, end time.Time) ([]models.Metric, error)
}

// SimulationEngine manages the lifecycle of what-if simulation scenarios.
//
// V2.0 Migration: Replaces the Python SimulationEngine ABC. The CancelScenario
// method is new in V2.0. RunScenario now returns a channel for real-time
// progress updates, replacing the Python polling-based approach.
type SimulationEngine interface {
	// RunScenario starts executing a scenario and returns a channel that
	// receives the final result. The context can be used to cancel execution.
	RunScenario(ctx context.Context, scenario WhatIfScenario) (<-chan *SimulationResult, error)

	// ListScenarios returns all known scenarios for a tenant.
	ListScenarios(ctx context.Context, tenantID string) ([]WhatIfScenario, error)

	// GetScenarioStatus returns the current status and partial results for a scenario.
	GetScenarioStatus(ctx context.Context, scenarioID string) (*SimulationResult, error)

	// CancelScenario stops a running scenario. Returns an error if the scenario
	// is not in a cancellable state.
	CancelScenario(ctx context.Context, scenarioID string) error
}
