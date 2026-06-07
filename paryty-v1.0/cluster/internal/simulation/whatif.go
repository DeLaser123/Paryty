package simulation

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// WhatIfRunner executes what-if scenario analyses by capturing a baseline
// topology, applying hypothetical changes, and predicting the resulting
// system behavior using linear extrapolation models.
//
// V2.0 Migration: Replaces the Python WhatIfAnalyzer class. The Python version
// used Celery tasks for distributed execution; V2.0 runs analysis in-process
// with bounded goroutines for lower latency and simpler debugging.
type WhatIfRunner struct {
	store  StorageReader
	logger *zap.Logger
}

// NewWhatIfRunner creates a new what-if runner.
func NewWhatIfRunner(store StorageReader, logger *zap.Logger) *WhatIfRunner {
	return &WhatIfRunner{
		store:  store,
		logger: logger,
	}
}

// RunScenario executes a full what-if analysis pipeline:
//  1. Capture baseline topology and metrics
//  2. Apply hypothetical infrastructure changes
//  3. Predict resulting metrics using linear models
//  4. Detect anomalies and find breaking points
//  5. Generate actionable recommendations
func (r *WhatIfRunner) RunScenario(ctx context.Context, scenario WhatIfScenario) (*SimulationResult, error) {
	// Step 1: Capture baseline.
	baseline, err := r.captureBaseline(ctx, scenario.TenantID)
	if err != nil {
		return nil, fmt.Errorf("capture baseline: %w", err)
	}

	// Step 2: Apply changes to get modified topology.
	modified, err := r.applyChanges(baseline.topology, scenario.Changes)
	if err != nil {
		return nil, fmt.Errorf("apply changes: %w", err)
	}

	// Step 3: Predict metrics under the modified topology.
	predicted, err := r.predictMetrics(ctx, baseline, modified, scenario)
	if err != nil {
		return nil, fmt.Errorf("predict metrics: %w", err)
	}

	// Step 4: Detect anomalies in predicted metrics.
	anomalies := r.detectAnomalies(predicted)

	// Step 5: Find breaking point.
	breakingPoint := r.findBreakingPoint(predicted)

	// Step 6: Generate recommendations.
	recommendations := r.generateRecommendations(scenario, predicted, anomalies, breakingPoint)

	// Build result.
	result := &SimulationResult{
		ScenarioID: scenario.ID,
		Status:     ScenarioStatusComplete,
		StartTime:  time.Now(),
		Metrics:    predicted,
		Summary: SimulationSummary{
			TotalRequests:   int64(scenario.Traffic.RPS * scenario.Duration.Seconds()),
			Recommendations: recommendations,
		},
		BreakingPoint: breakingPoint,
	}

	// Populate summary from predicted metrics.
	r.populateSummary(result, predicted, scenario)

	return result, nil
}

// baseline contains the captured system state before modifications.
type baseline struct {
	topology  *models.Topology
	agents    []models.AgentInfo
	alerts    []models.Alert
	metrics   map[string][]MetricPoint
	nodeCount int
	edgeCount int
}

// captureBaseline retrieves the current topology and recent metrics.
func (r *WhatIfRunner) captureBaseline(ctx context.Context, tenantID string) (*baseline, error) {
	topology, err := r.store.GetTopology(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get topology: %w", err)
	}

	agents, err := r.store.GetAllAgentStates(ctx, tenantID)
	if err != nil {
		r.logger.Warn("Failed to get agent states, continuing with empty list",
			zap.String("tenant_id", tenantID),
			zap.Error(err),
		)
		agents = nil
	}

	alerts, err := r.store.GetActiveAlerts(ctx, tenantID)
	if err != nil {
		r.logger.Warn("Failed to get active alerts, continuing with empty list",
			zap.String("tenant_id", tenantID),
			zap.Error(err),
		)
		alerts = nil
	}

	metrics := make(map[string][]MetricPoint)

	// Query recent metrics for each agent to establish the baseline.
	metricNames := []string{"cpu.usage_percent", "memory.usage_percent", "disk.usage_percent", "network.rx_bytes_per_sec"}
	end := time.Now()
	start := end.Add(-1 * time.Hour)

	for _, agent := range agents {
		for _, metricName := range metricNames {
			pts, err := r.store.QueryMetrics(ctx, agent.ID, metricName, start, end)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s.%s", agent.ID, metricName)
			for _, p := range pts {
				metrics[key] = append(metrics[key], MetricPoint{
					Timestamp: p.Timestamp,
					Value:     p.Value,
				})
			}
		}
	}

	nodeCount := 0
	edgeCount := 0
	if topology != nil {
		nodeCount = len(topology.Nodes)
		edgeCount = len(topology.Edges)
	}

	return &baseline{
		topology:  topology,
		agents:    agents,
		alerts:    alerts,
		metrics:   metrics,
		nodeCount: nodeCount,
		edgeCount: edgeCount,
	}, nil
}

// applyChanges creates a modified copy of the topology by applying
// infrastructure changes. The original topology is not mutated.
func (r *WhatIfRunner) applyChanges(topology *models.Topology, changes []InfraChange) (*models.Topology, error) {
	if topology == nil {
		return nil, fmt.Errorf("topology is nil, cannot apply changes")
	}

	// Deep copy the topology.
	modified := r.deepCopyTopology(topology)

	for _, change := range changes {
		switch change.Type {
		case ChangeTypeHost:
			if err := r.applyHostChange(modified, change); err != nil {
				return nil, fmt.Errorf("apply host change: %w", err)
			}
		case ChangeTypeContainer:
			if err := r.applyContainerChange(modified, change); err != nil {
				return nil, fmt.Errorf("apply container change: %w", err)
			}
		case ChangeTypeService:
			if err := r.applyServiceChange(modified, change); err != nil {
				return nil, fmt.Errorf("apply service change: %w", err)
			}
		case ChangeTypeNetwork:
			if err := r.applyNetworkChange(modified, change); err != nil {
				return nil, fmt.Errorf("apply network change: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown change type: %s", change.Type)
		}
	}

	return modified, nil
}

// deepCopyTopology creates a deep copy of the topology.
func (r *WhatIfRunner) deepCopyTopology(src *models.Topology) *models.Topology {
	dst := &models.Topology{
		Timestamp: src.Timestamp,
		Version:   src.Version,
	}

	if src.Nodes != nil {
		dst.Nodes = make([]models.TopologyNode, len(src.Nodes))
		copy(dst.Nodes, src.Nodes)
		for i := range dst.Nodes {
			if dst.Nodes[i].Labels != nil {
				labels := make(map[string]string, len(dst.Nodes[i].Labels))
				for k, v := range dst.Nodes[i].Labels {
					labels[k] = v
				}
				dst.Nodes[i].Labels = labels
			}
			if dst.Nodes[i].Metadata != nil {
				meta := make(map[string]string, len(dst.Nodes[i].Metadata))
				for k, v := range dst.Nodes[i].Metadata {
					meta[k] = v
				}
				dst.Nodes[i].Metadata = meta
			}
		}
	}

	if src.Edges != nil {
		dst.Edges = make([]models.TopologyEdge, len(src.Edges))
		copy(dst.Edges, src.Edges)
		for i := range dst.Edges {
			if dst.Edges[i].Labels != nil {
				labels := make(map[string]string, len(dst.Edges[i].Labels))
				for k, v := range dst.Edges[i].Labels {
					labels[k] = v
				}
				dst.Edges[i].Labels = labels
			}
		}
	}

	return dst
}

// applyHostChange adds or removes host nodes from the topology.
func (r *WhatIfRunner) applyHostChange(topology *models.Topology, change InfraChange) error {
	switch change.Action {
	case "add":
		for i := 0; i < change.Count; i++ {
			nodeID := fmt.Sprintf("sim-host-%s-%d", change.Target, i)
			topology.Nodes = append(topology.Nodes, models.TopologyNode{
				ID:        nodeID,
				Name:      fmt.Sprintf("simulated-host-%d", i),
				Type:      models.NodeTypeHost,
				Health:    models.HealthStatusHealthy,
				LastSeen:  time.Now(),
				Labels:    map[string]string{"source": "simulation"},
				Metadata:  map[string]string{},
			})
		}
	case "remove":
		removed := 0
		var remaining []models.TopologyNode
		for _, node := range topology.Nodes {
			if removed < change.Count && (node.ID == change.Target || node.Name == change.Target) {
				removed++
				continue
			}
			remaining = append(remaining, node)
		}
		topology.Nodes = remaining
		// Remove edges referencing removed nodes.
		r.pruneOrphanedEdges(topology)
	case "scale":
		// Scale changes are handled as a multiplier on the target count.
		var scaled []models.TopologyNode
		for _, node := range topology.Nodes {
			if node.ID == change.Target || node.Name == change.Target {
				for i := 0; i < change.Count; i++ {
					clone := node
					clone.ID = fmt.Sprintf("%s-scaled-%d", node.ID, i)
					scaled = append(scaled, clone)
				}
			} else {
				scaled = append(scaled, node)
			}
		}
		topology.Nodes = scaled
	default:
		return fmt.Errorf("unknown host action: %s", change.Action)
	}
	return nil
}

// applyContainerChange adds or removes container nodes.
func (r *WhatIfRunner) applyContainerChange(topology *models.Topology, change InfraChange) error {
	switch change.Action {
	case "add":
		for i := 0; i < change.Count; i++ {
			nodeID := fmt.Sprintf("sim-container-%s-%d", change.Target, i)
			topology.Nodes = append(topology.Nodes, models.TopologyNode{
				ID:       nodeID,
				Name:     fmt.Sprintf("simulated-container-%d", i),
				Type:     models.NodeTypeContainer,
				Health:   models.HealthStatusHealthy,
				LastSeen: time.Now(),
				Labels:   map[string]string{"source": "simulation"},
				Metadata: map[string]string{},
			})
		}
	case "remove":
		removed := 0
		var remaining []models.TopologyNode
		for _, node := range topology.Nodes {
			if removed < change.Count && node.Type == models.NodeTypeContainer &&
				(node.ID == change.Target || node.Name == change.Target) {
				removed++
				continue
			}
			remaining = append(remaining, node)
		}
		topology.Nodes = remaining
		r.pruneOrphanedEdges(topology)
	default:
		return fmt.Errorf("unknown container action: %s", change.Action)
	}
	return nil
}

// applyServiceChange adds or removes service nodes.
func (r *WhatIfRunner) applyServiceChange(topology *models.Topology, change InfraChange) error {
	switch change.Action {
	case "add":
		for i := 0; i < change.Count; i++ {
			nodeID := fmt.Sprintf("sim-svc-%s-%d", change.Target, i)
			topology.Nodes = append(topology.Nodes, models.TopologyNode{
				ID:       nodeID,
				Name:     fmt.Sprintf("simulated-service-%d", i),
				Type:     models.NodeTypeService,
				Health:   models.HealthStatusHealthy,
				LastSeen: time.Now(),
				Labels:   map[string]string{"source": "simulation"},
				Metadata: map[string]string{},
			})
		}
	case "remove":
		removed := 0
		var remaining []models.TopologyNode
		for _, node := range topology.Nodes {
			if removed < change.Count && node.Type == models.NodeTypeService &&
				(node.ID == change.Target || node.Name == change.Target) {
				removed++
				continue
			}
			remaining = append(remaining, node)
		}
		topology.Nodes = remaining
		r.pruneOrphanedEdges(topology)
	default:
		return fmt.Errorf("unknown service action: %s", change.Action)
	}
	return nil
}

// applyNetworkChange modifies network edges.
func (r *WhatIfRunner) applyNetworkChange(topology *models.Topology, change InfraChange) error {
	switch change.Action {
	case "add":
		latency := 10.0
		if v, ok := change.Config["latency_ms"]; ok {
			fmt.Sscanf(v, "%f", &latency)
		}
		topology.Edges = append(topology.Edges, models.TopologyEdge{
			ID:       fmt.Sprintf("sim-edge-%s", change.Target),
			SourceID: change.Target,
			TargetID: fmt.Sprintf("%s-dest", change.Target),
			Type:     models.EdgeTypeHTTP,
			Protocol: "http",
			Labels:   map[string]string{"source": "simulation"},
			Metrics: models.EdgeMetrics{
				LatencyP50: latency,
			},
			LastSeen: time.Now(),
		})
	case "modify":
		latency := 50.0
		if v, ok := change.Config["latency_ms"]; ok {
			fmt.Sscanf(v, "%f", &latency)
		}
		for i := range topology.Edges {
			if topology.Edges[i].SourceID == change.Target || topology.Edges[i].TargetID == change.Target {
				topology.Edges[i].Metrics.LatencyP50 = latency
			}
		}
	case "remove":
		var remaining []models.TopologyEdge
		for _, edge := range topology.Edges {
			if edge.SourceID == change.Target && edge.TargetID == fmt.Sprintf("%s-dest", change.Target) {
				continue
			}
			remaining = append(remaining, edge)
		}
		topology.Edges = remaining
	default:
		return fmt.Errorf("unknown network action: %s", change.Action)
	}
	return nil
}

// pruneOrphanedEdges removes edges that reference non-existent nodes.
func (r *WhatIfRunner) pruneOrphanedEdges(topology *models.Topology) {
	nodeIDs := make(map[string]bool, len(topology.Nodes))
	for _, node := range topology.Nodes {
		nodeIDs[node.ID] = true
	}

	var valid []models.TopologyEdge
	for _, edge := range topology.Edges {
		if nodeIDs[edge.SourceID] && nodeIDs[edge.TargetID] {
			valid = append(valid, edge)
		}
	}
	topology.Edges = valid
}

// predictMetrics predicts the system metrics under the modified topology
// using a linear adjustment model.
func (r *WhatIfRunner) predictMetrics(ctx context.Context, bl *baseline, modified *models.Topology, scenario WhatIfScenario) (map[string][]MetricPoint, error) {
	predicted := make(map[string][]MetricPoint)

	// Compute the adjustment factor from the topology change.
	adj := r.computeAdjustment(bl, modified)

	for key, points := range bl.metrics {
		if len(points) == 0 {
			continue
		}

		// Find the applicable adjustment factor for this metric.
		factor := 1.0
		switch {
		case containsMetric(key, "cpu"):
			factor = adj.cpuFactor
		case containsMetric(key, "memory"):
			factor = adj.memFactor
		case containsMetric(key, "disk"):
			factor = adj.diskFactor
		case containsMetric(key, "network"):
			factor = adj.netFactor
		}

		// Apply the traffic pattern scaling.
		if scenario.Traffic.RPS > 0 {
			baselineRPS := float64(len(bl.agents)) * 10.0 // assume 10 RPS per agent baseline
			if baselineRPS > 0 {
				trafficFactor := scenario.Traffic.RPS / baselineRPS
				factor *= trafficFactor
			}
		}

		var adjusted []MetricPoint
		for _, pt := range points {
			newVal := pt.Value * factor
			// Clamp to [0, 100] for percentage metrics.
			if isPercentMetric(key) {
				newVal = math.Max(0, math.Min(100, newVal))
			}
			adjusted = append(adjusted, MetricPoint{
				Timestamp: pt.Timestamp,
				Value:     newVal,
			})
		}

		predicted[key] = adjusted
	}

	return predicted, nil
}

// adjustment holds the computed adjustment factors for each metric category.
type adjustment struct {
	cpuFactor  float64
	memFactor  float64
	diskFactor float64
	netFactor  float64
}

// computeAdjustment computes the linear adjustment factors based on the
// ratio of nodes between baseline and modified topology.
//
// The model: adding resources decreases per-resource utilization (factor < 1),
// removing resources increases per-resource utilization (factor > 1).
func (r *WhatIfRunner) computeAdjustment(bl *baseline, modified *models.Topology) adjustment {
	adj := adjustment{
		cpuFactor:  1.0,
		memFactor:  1.0,
		diskFactor: 1.0,
		netFactor:  1.0,
	}

	if bl.nodeCount == 0 {
		return adj
	}

	// Count nodes by type in modified topology.
	modHosts := countNodesByType(modified, models.NodeTypeHost)
	modSvcs := countNodesByType(modified, models.NodeTypeService)
	modContainers := countNodesByType(modified, models.NodeTypeContainer)

	baseHosts := countNodesByType(bl.topology, models.NodeTypeHost)
	baseSvcs := countNodesByType(bl.topology, models.NodeTypeService)
	baseContainers := countNodesByType(bl.topology, models.NodeTypeContainer)

	// CPU factor: inversely proportional to host/service count changes.
	if baseHosts > 0 {
		adj.cpuFactor = float64(baseHosts) / float64(maxInt(modHosts, 1))
	}
	if baseSvcs > 0 {
		svcAdj := float64(baseSvcs) / float64(maxInt(modSvcs, 1))
		adj.cpuFactor = (adj.cpuFactor + svcAdj) / 2.0
	}

	// Memory factor: inversely proportional to container count.
	if baseContainers > 0 {
		adj.memFactor = float64(baseContainers) / float64(maxInt(modContainers, 1))
	}

	// Network factor: proportional to edge count ratio.
	baseEdges := len(bl.topology.Edges)
	modEdges := len(modified.Edges)
	if baseEdges > 0 {
		adj.netFactor = float64(modEdges) / float64(baseEdges)
	}

	// Disk factor: scales with host count.
	if baseHosts > 0 {
		adj.diskFactor = float64(baseHosts) / float64(maxInt(modHosts, 1))
	}

	return adj
}

// detectAnomalies checks predicted metrics for utilization above 95%.
func (r *WhatIfRunner) detectAnomalies(predicted map[string][]MetricPoint) []string {
	var anomalies []string

	for key, points := range predicted {
		if len(points) == 0 {
			continue
		}

		// Check the latest point.
		latest := points[len(points)-1]
		if isPercentMetric(key) && latest.Value > 95.0 {
			anomalies = append(anomalies, fmt.Sprintf("%s at %.1f%% (threshold: 95%%)", key, latest.Value))
		}
	}

	return anomalies
}

// findBreakingPoint checks if any predicted metric exceeds 99% utilization.
func (r *WhatIfRunner) findBreakingPoint(predicted map[string][]MetricPoint) *BreakingPoint {
	for key, points := range predicted {
		if len(points) == 0 {
			continue
		}

		latest := points[len(points)-1]
		if isPercentMetric(key) && latest.Value > 99.0 {
			bottleneck := "unknown"
			switch {
			case containsMetric(key, "cpu"):
				bottleneck = "cpu"
			case containsMetric(key, "memory"):
				bottleneck = "memory"
			case containsMetric(key, "disk"):
				bottleneck = "disk"
			case containsMetric(key, "network"):
				bottleneck = "network"
			}

			return &BreakingPoint{
				RPS:      0,
				ErrorRate: (latest.Value - 99.0) / 100.0,
				Bottleneck: bottleneck,
				Recommendations: []string{
					fmt.Sprintf("Scale out %s resources to reduce utilization below 99%%", bottleneck),
					fmt.Sprintf("Current %s utilization: %.1f%%", bottleneck, latest.Value),
				},
			}
		}
	}

	return nil
}

// generateRecommendations produces actionable suggestions based on analysis results.
func (r *WhatIfRunner) generateRecommendations(
	scenario WhatIfScenario,
	predicted map[string][]MetricPoint,
	anomalies []string,
	bp *BreakingPoint,
) []string {
	var recs []string

	if bp != nil {
		recs = append(recs, fmt.Sprintf("CRITICAL: System breaks at current scale. Bottleneck: %s", bp.Bottleneck))
		recs = append(recs, bp.Recommendations...)
	}

	if len(anomalies) > 0 {
		recs = append(recs, fmt.Sprintf("WARNING: %d metric(s) exceed 95%% utilization", len(anomalies)))
	}

	// Check for adding/removing resources.
	hasAdds := false
	hasRemoves := false
	for _, ch := range scenario.Changes {
		switch ch.Action {
		case "add", "scale":
			hasAdds = true
		case "remove":
			hasRemoves = true
		}
	}

	if hasRemoves && len(anomalies) > 0 {
		recs = append(recs, "Consider a more gradual removal schedule to avoid overloading remaining resources")
	}

	if hasAdds && len(anomalies) == 0 && bp == nil {
		recs = append(recs, "Resources were added and all metrics remain within safe bounds — scaling looks sufficient")
	}

	if len(anomalies) == 0 && bp == nil {
		recs = append(recs, "All predicted metrics are within safe bounds")
	}

	return recs
}

// populateSummary fills in the simulation summary from predicted metrics.
func (r *WhatIfRunner) populateSummary(result *SimulationResult, predicted map[string][]MetricPoint, scenario WhatIfScenario) {
	var maxCPU, maxMemory float64

	if pts, ok := predicted["cpu.usage_percent"]; ok {
		for _, p := range pts {
			if p.Value > maxCPU {
				maxCPU = p.Value
			}
		}
	}
	// Also try agent-keyed format.
	for key, pts := range predicted {
		if containsMetric(key, "cpu") && containsMetric(key, "usage_percent") {
			for _, p := range pts {
				if p.Value > maxCPU {
					maxCPU = p.Value
				}
			}
		}
		if containsMetric(key, "memory") && containsMetric(key, "usage_percent") {
			for _, p := range pts {
				if p.Value > maxMemory {
					maxMemory = p.Value
				}
			}
		}
	}

	if pts, ok := predicted["memory.usage_percent"]; ok {
		for _, p := range pts {
			if p.Value > maxMemory {
				maxMemory = p.Value
			}
		}
	}

	result.Summary.MaxCPU = maxCPU
	result.Summary.MaxMemory = maxMemory

	// Compute success rate: assume success degrades with high utilization.
	successRate := 1.0
	if maxCPU > 90 {
		successRate -= (maxCPU - 90) / 100
	}
	if maxMemory > 90 {
		successRate -= (maxMemory - 90) / 100
	}
	result.Summary.SuccessRate = math.Max(0, math.Min(1, successRate))

	// Estimate latency based on utilization.
	baseLatency := 50 * time.Millisecond
	if maxCPU > 80 {
		extra := time.Duration((maxCPU - 80) * float64(time.Millisecond) * 2)
		baseLatency += extra
	}
	result.Summary.AvgLatency = baseLatency
	result.Summary.P99Latency = baseLatency * 3

	// Rough cost estimate based on node count.
	nodeCount := 0
	for key := range predicted {
		if containsMetric(key, "cpu") {
			nodeCount++
		}
	}
	if nodeCount == 0 {
		nodeCount = 1
	}
	result.Summary.CostEstimate = float64(nodeCount) * 150.0 // $150/month per node estimate
}

// Helper functions.

func containsMetric(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func isPercentMetric(key string) bool {
	return containsMetric(key, "usage_percent") || containsMetric(key, "utilization")
}

func countNodesByType(topology *models.Topology, nodeType models.NodeType) int {
	if topology == nil {
		return 0
	}
	count := 0
	for _, n := range topology.Nodes {
		if n.Type == nodeType {
			count++
		}
	}
	return count
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
