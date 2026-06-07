package simulation

import (
	"context"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"
)

// DegradationSimulator models gradual performance degradation scenarios.
// It predicts how system metrics change over time as resources are exhausted.
//
// V2.0 Migration: Replaces the Python DegradationSimulator class. The Python
// version used numpy for curve fitting; the Go version uses analytical models
// (exponential saturation curves) for the same results without the numpy dependency.
type DegradationSimulator struct {
	logger *zap.Logger
}

// NewDegradationSimulator creates a new degradation simulator.
func NewDegradationSimulator(logger *zap.Logger) *DegradationSimulator {
	return &DegradationSimulator{
		logger: logger,
	}
}

// DegradationConfig configures a degradation simulation.
type DegradationConfig struct {
	// Type is the degradation type to simulate.
	Type DegradationType `json:"type"`
	// TargetAgent is the agent/service to degrade.
	TargetAgent string `json:"target_agent"`
	// Duration is how long the degradation simulation runs.
	Duration time.Duration `json:"duration"`
	// StartValue is the initial metric value.
	StartValue float64 `json:"start_value"`
	// EndValue is the final metric value (e.g., 100 for full saturation).
	EndValue float64 `json:"end_value"`
	// Points is the number of data points to generate.
	Points int `json:"points"`
}

// DegradationResult contains the predicted metric values over time.
type DegradationResult struct {
	// Config is the configuration used.
	Config DegradationConfig `json:"config"`
	// TimeSeries contains the predicted metric values.
	TimeSeries []DegradationPoint `json:"time_series"`
	// TimeToThreshold maps threshold percentages to the time they'll be reached.
	TimeToThreshold map[string]time.Duration `json:"time_to_threshold"`
	// Recommendations contains mitigation suggestions.
	Recommendations []string `json:"recommendations"`
}

// DegradationPoint is a single data point in the degradation time series.
type DegradationPoint struct {
	// Timestamp is the simulated time.
	Timestamp time.Time `json:"timestamp"`
	// Value is the predicted metric value.
	Value float64 `json:"value"`
	// Status indicates if the value is in a danger zone.
	Status string `json:"status"`
}

// SimulateDegradation models gradual performance degradation over time.
// It uses exponential saturation curves to predict when thresholds will be hit.
func (ds *DegradationSimulator) SimulateDegradation(ctx context.Context, config DegradationConfig) (*DegradationResult, error) {
	if config.Duration <= 0 {
		return nil, fmt.Errorf("degradation duration must be positive")
	}
	if config.Points <= 0 {
		config.Points = 100
	}

	ds.logger.Info("Starting degradation simulation",
		zap.String("type", string(config.Type)),
		zap.String("target", config.TargetAgent),
		zap.Duration("duration", config.Duration),
	)

	result := &DegradationResult{
		Config:          config,
		TimeToThreshold: make(map[string]time.Duration),
	}

	// Generate time series based on degradation type.
	switch config.Type {
	case DegradationTypeCPUSaturation:
		result.TimeSeries = ds.simulateCPUSaturation(config)
	case DegradationTypeMemoryLeak:
		result.TimeSeries = ds.simulateMemoryLeak(config)
	case DegradationTypeDiskFill:
		result.TimeSeries = ds.simulateDiskFill(config)
	case DegradationTypeNetworkCongestion:
		result.TimeSeries = ds.simulateNetworkCongestion(config)
	default:
		return nil, fmt.Errorf("unknown degradation type: %s", config.Type)
	}

	// Compute time-to-threshold for standard thresholds.
	thresholds := []float64{50, 75, 90, 95, 99}
	for _, threshold := range thresholds {
		for _, pt := range result.TimeSeries {
			if pt.Value >= threshold {
				elapsed := pt.Timestamp.Sub(result.TimeSeries[0].Timestamp)
				label := fmt.Sprintf("%.0f%%", threshold)
				result.TimeToThreshold[label] = elapsed
				break
			}
		}
	}

	// Generate recommendations.
	result.Recommendations = ds.generateDegradationRecommendations(config.Type, result)

	ds.logger.Info("Degradation simulation completed",
		zap.String("type", string(config.Type)),
		zap.Int("points", len(result.TimeSeries)),
		zap.Any("thresholds", result.TimeToThreshold),
	)

	return result, nil
}

// simulateCPUSaturation models CPU utilization increase using an exponential
// saturation curve: value(t) = end - (end - start) * e^(-k*t)
// where k controls the rate of saturation.
func (ds *DegradationSimulator) simulateCPUSaturation(config DegradationConfig) []DegradationPoint {
	k := 3.0 / config.Duration.Seconds() // Reach 95% of target by 1/3 of duration
	return ds.generateCurve(config, k, "exponential_saturation")
}

// simulateMemoryLeak models memory utilization increase using a linear-with-
// acceleration curve: value(t) = start + (end - start) * (t/T)^1.5
// Memory leaks accelerate as fragmentation increases.
func (ds *DegradationSimulator) simulateMemoryLeak(config DegradationConfig) []DegradationPoint {
	points := make([]DegradationPoint, config.Points)
	interval := config.Duration / time.Duration(config.Points)
	startTime := time.Now()

	for i := 0; i < config.Points; i++ {
		t := float64(i) / float64(config.Points-1) // Normalized time [0, 1]
		// Power curve with exponent 1.5 for acceleration.
		progress := math.Pow(t, 1.5)
		value := config.StartValue + (config.EndValue-config.StartValue)*progress

		points[i] = DegradationPoint{
			Timestamp: startTime.Add(time.Duration(i) * interval),
			Value:     math.Min(value, config.EndValue),
			Status:    ds.valueToStatus(value, config.Type),
		}
	}

	return points
}

// simulateDiskFill models disk utilization increase using a logarithmic curve
// that slows as disk fills: value(t) = start + (end - start) * log(1 + k*t) / log(1 + k*T)
func (ds *DegradationSimulator) simulateDiskFill(config DegradationConfig) []DegradationPoint {
	points := make([]DegradationPoint, config.Points)
	interval := config.Duration / time.Duration(config.Points)
	startTime := time.Now()
	k := 10.0 // Controls curve shape

	for i := 0; i < config.Points; i++ {
		t := float64(i) / float64(config.Points-1)
		progress := math.Log(1+k*t) / math.Log(1+k)
		value := config.StartValue + (config.EndValue-config.StartValue)*progress

		points[i] = DegradationPoint{
			Timestamp: startTime.Add(time.Duration(i) * interval),
			Value:     math.Min(value, config.EndValue),
			Status:    ds.valueToStatus(value, config.Type),
		}
	}

	return points
}

// simulateNetworkCongestion models network degradation using a sigmoid curve
// that shows a sharp transition from normal to congested:
// value(t) = start + (end - start) / (1 + e^(-k*(t - midpoint)))
func (ds *DegradationSimulator) simulateNetworkCongestion(config DegradationConfig) []DegradationPoint {
	points := make([]DegradationPoint, config.Points)
	interval := config.Duration / time.Duration(config.Points)
	startTime := time.Now()
	k := 10.0 // Controls steepness of transition
	midpoint := 0.5

	for i := 0; i < config.Points; i++ {
		t := float64(i) / float64(config.Points-1)
		sigmoid := 1.0 / (1.0 + math.Exp(-k*(t-midpoint)))
		value := config.StartValue + (config.EndValue-config.StartValue)*sigmoid

		points[i] = DegradationPoint{
			Timestamp: startTime.Add(time.Duration(i) * interval),
			Value:     math.Min(value, config.EndValue),
			Status:    ds.valueToStatus(value, config.Type),
		}
	}

	return points
}

// generateCurve creates an exponential saturation curve.
func (ds *DegradationSimulator) generateCurve(config DegradationConfig, k float64, curveType string) []DegradationPoint {
	points := make([]DegradationPoint, config.Points)
	interval := config.Duration / time.Duration(config.Points)
	startTime := time.Now()

	for i := 0; i < config.Points; i++ {
		t := float64(i) / float64(config.Points-1)
		var progress float64

		switch curveType {
		case "exponential_saturation":
			// value(t) = 1 - e^(-k*t)
			progress = 1.0 - math.Exp(-k*config.Duration.Seconds()*t)
		default:
			progress = t
		}

		value := config.StartValue + (config.EndValue-config.StartValue)*progress

		points[i] = DegradationPoint{
			Timestamp: startTime.Add(time.Duration(i) * interval),
			Value:     math.Min(value, config.EndValue),
			Status:    ds.valueToStatus(value, config.Type),
		}
	}

	return points
}

// valueToStatus converts a metric value to a human-readable status.
func (ds *DegradationSimulator) valueToStatus(value float64, _ DegradationType) string {
	switch {
	case value >= 99:
		return "critical"
	case value >= 95:
		return "danger"
	case value >= 90:
		return "warning"
	case value >= 75:
		return "elevated"
	default:
		return "normal"
	}
}

// generateDegradationRecommendations produces mitigation suggestions.
func (ds *DegradationSimulator) generateDegradationRecommendations(
	dtype DegradationType,
	result *DegradationResult,
) []string {
	var recs []string

	if t, ok := result.TimeToThreshold["90%"]; ok {
		recs = append(recs, fmt.Sprintf(
			"Resource will reach 90%% utilization in %s — plan intervention before then",
			t.Round(time.Minute),
		))
	}

	switch dtype {
	case DegradationTypeCPUSaturation:
		recs = append(recs, "Scale out horizontally by adding more service replicas")
		recs = append(recs, "Profile hot code paths for CPU optimization")
		recs = append(recs, "Consider request rate limiting to cap CPU demand")
	case DegradationTypeMemoryLeak:
		recs = append(recs, "Identify and fix the memory leak in the application code")
		recs = append(recs, "Implement rolling restarts as a temporary mitigation")
		recs = append(recs, "Set memory limits in container configuration to trigger OOM before host exhaustion")
	case DegradationTypeDiskFill:
		recs = append(recs, "Implement log rotation and cleanup policies")
		recs = append(recs, "Move cold data to object storage (SeaweedFS)")
		recs = append(recs, "Set up disk usage alerts at 80% threshold")
	case DegradationTypeNetworkCongestion:
		recs = append(recs, "Implement circuit breakers to prevent cascading failures")
		recs = append(recs, "Add connection pooling and request batching")
		recs = append(recs, "Consider deploying closer to data sources to reduce cross-region traffic")
	}

	return recs
}
