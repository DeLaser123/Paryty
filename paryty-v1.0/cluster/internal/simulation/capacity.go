package simulation

import (
	"context"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"
)

// CapacityPlanner analyzes current resource usage trends and projects
// future capacity needs. It generates scaling recommendations based on
// linear extrapolation of historical metrics.
//
// V2.0 Migration: Replaces the Python CapacityPlanner microservice. The Python
// version used scikit-learn for trend analysis; the Go version uses analytical
// linear regression for the same results without the ML dependency.
type CapacityPlanner struct {
	store  StorageReader
	logger *zap.Logger
}

// NewCapacityPlanner creates a new capacity planner.
func NewCapacityPlanner(store StorageReader, logger *zap.Logger) *CapacityPlanner {
	return &CapacityPlanner{
		store:  store,
		logger: logger,
	}
}

// PlanCapacity analyzes current usage trends and projects future needs.
// It queries historical metrics for each resource type, fits a linear trend,
// and projects when capacity will be exhausted.
func (cp *CapacityPlanner) PlanCapacity(ctx context.Context, tenantID string) (*CapacityReport, error) {
	cp.logger.Info("Starting capacity planning analysis",
		zap.String("tenant_id", tenantID),
	)

	agents, err := cp.store.GetAllAgentStates(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get agent states: %w", err)
	}

	if len(agents) == 0 {
		return &CapacityReport{
			GeneratedAt:     time.Now(),
			CurrentUsage:    make(map[string]float64),
			ProjectedUsage:  make(map[string]float64),
			DaysUntilFull:   make(map[string]int),
			Recommendations: nil,
		}, nil
	}

	// Query historical metrics for trend analysis.
	end := time.Now()
	start := end.Add(-7 * 24 * time.Hour) // 7 days of history

	resources := []string{"cpu.usage_percent", "memory.usage_percent", "disk.usage_percent", "network.rx_bytes_per_sec"}
	currentUsage := make(map[string]float64)
	projectedUsage := make(map[string]float64)
	daysUntilFull := make(map[string]int)
	var recommendations []CapacityRecommendation
	var totalCost float64

	for _, resource := range resources {
		resourceKey := resourceToKey(resource)

		// Collect all data points for this resource across agents.
		var allPoints []metricPoint
		var latestValue float64

		for _, agent := range agents {
			pts, err := cp.store.QueryMetrics(ctx, tenantID, agent.ID, resource, start, end)
			if err != nil {
				continue
			}
			for _, p := range pts {
				allPoints = append(allPoints, metricPoint{
					timestamp: p.Timestamp,
					value:     p.Value,
				})
			}
			// Use the latest value from the first agent as representative.
			if len(pts) > 0 && latestValue == 0 {
				latestValue = pts[len(pts)-1].Value
			}
		}

		if len(allPoints) == 0 {
			continue
		}

		currentUsage[resourceKey] = latestValue

		// Fit a linear trend and project 30 days forward.
		slope, intercept := cp.linearRegression(allPoints)
		projected := slope*float64(30*24*3600) + intercept // 30 days in seconds from epoch
		projected = math.Max(0, math.Min(100, projected))
		projectedUsage[resourceKey] = projected

		// Calculate days until capacity exhaustion (100%).
		if slope > 0 {
			daysToFull := (100.0 - latestValue) / (slope * 86400) // slope is per-second
			if daysToFull > 0 {
				daysUntilFull[resourceKey] = int(daysToFull)
			}
		}

		// Generate recommendation if capacity is approaching limits.
		rec := cp.generateRecommendation(resourceKey, latestValue, projected, daysUntilFull[resourceKey], len(agents))
		if rec != nil {
			recommendations = append(recommendations, *rec)
			totalCost += rec.EstimatedCost
		}
	}

	report := &CapacityReport{
		GeneratedAt:     time.Now(),
		CurrentUsage:    currentUsage,
		ProjectedUsage:  projectedUsage,
		DaysUntilFull:   daysUntilFull,
		Recommendations: recommendations,
		CostProjection:  totalCost,
	}

	cp.logger.Info("Capacity planning completed",
		zap.String("tenant_id", tenantID),
		zap.Int("agents", len(agents)),
		zap.Int("recommendations", len(recommendations)),
		zap.Float64("cost_projection", totalCost),
	)

	return report, nil
}

// GenerateCapacityReport generates a formatted capacity report.
func (cp *CapacityPlanner) GenerateCapacityReport(ctx context.Context, tenantID string) (*CapacityReport, error) {
	return cp.PlanCapacity(ctx, tenantID)
}

// metricPoint is an internal timestamped value for regression.
type metricPoint struct {
	timestamp time.Time
	value     float64
}

// linearRegression computes a simple linear regression (y = mx + b) on
// the given data points where x is seconds since epoch.
func (cp *CapacityPlanner) linearRegression(points []metricPoint) (slope, intercept float64) {
	n := float64(len(points))
	if n == 0 {
		return 0, 0
	}

	// Use the first timestamp as the origin to avoid large number issues.
	origin := points[0].timestamp

	var sumX, sumY, sumXY, sumX2 float64
	for _, p := range points {
		x := p.timestamp.Sub(origin).Seconds()
		y := p.value
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denom := n*sumX2 - sumX*sumX
	if denom == 0 {
		return 0, sumY / n
	}

	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n

	return slope, intercept
}

// generateRecommendation creates a capacity recommendation for a single resource.
func (cp *CapacityPlanner) generateRecommendation(
	resource string,
	current float64,
	projected float64,
	daysToFull int,
	agentCount int,
) *CapacityRecommendation {
	// Only generate recommendations for resources under pressure.
	if projected < 80 && daysToFull > 90 {
		return nil
	}

	var urgency string
	switch {
	case daysToFull <= 7:
		urgency = "critical"
	case daysToFull <= 30:
		urgency = "high"
	case daysToFull <= 90:
		urgency = "medium"
	default:
		urgency = "low"
	}

	var currentCap, recommendedCap string
	var estimatedCost float64

	switch resource {
	case "cpu":
		currentCap = fmt.Sprintf("%d cores", agentCount*4)
		recommendedCap = fmt.Sprintf("%d cores", agentCount*8)
		estimatedCost = float64(agentCount) * 50.0 // $50/month per agent for CPU
	case "memory":
		currentCap = fmt.Sprintf("%d GB", agentCount*16)
		recommendedCap = fmt.Sprintf("%d GB", agentCount*32)
		estimatedCost = float64(agentCount) * 40.0
	case "disk":
		currentCap = fmt.Sprintf("%d GB", agentCount*100)
		recommendedCap = fmt.Sprintf("%d GB", agentCount*200)
		estimatedCost = float64(agentCount) * 10.0
	case "network":
		currentCap = fmt.Sprintf("%.0f Mbps", float64(agentCount)*1000)
		recommendedCap = fmt.Sprintf("%.0f Mbps", float64(agentCount)*2000)
		estimatedCost = float64(agentCount) * 30.0
	}

	return &CapacityRecommendation{
		Resource:            resource,
		CurrentCapacity:     currentCap,
		RecommendedCapacity: recommendedCap,
		Urgency:             urgency,
		Reason: fmt.Sprintf(
			"Current %s utilization at %.1f%%, projected to reach %.1f%% in 30 days. Estimated exhaustion in %d days.",
			resource, current, projected, daysToFull,
		),
		EstimatedCost: estimatedCost,
	}
}

// resourceToKey converts a metric name to a short resource key.
func resourceToKey(metricName string) string {
	switch {
	case metricName == "cpu.usage_percent":
		return "cpu"
	case metricName == "memory.usage_percent":
		return "memory"
	case metricName == "disk.usage_percent":
		return "disk"
	case metricName == "network.rx_bytes_per_sec":
		return "network"
	default:
		return metricName
	}
}
