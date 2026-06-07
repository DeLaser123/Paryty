package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"go.uber.org/zap"
)

// QueryService handles query API requests.
type QueryService struct {
	store  *storage.Store
	logger *zap.Logger
}

// NewQueryService creates a new query service.
func NewQueryService(store *storage.Store, logger *zap.Logger) *QueryService {
	return &QueryService{
		store:  store,
		logger: logger,
	}
}

// tenantFromRequest extracts the tenant from the X-Tenant-ID header.
// Returns "default" if the header is not set.
func tenantFromRequest(c *gin.Context) string {
	if t := c.GetHeader("X-Tenant-ID"); t != "" {
		return t
	}
	return "default"
}

// RegisterRoutes registers query API routes.
func (s *QueryService) RegisterRoutes(r *gin.RouterGroup) {
	api := r.Group("/api/v1")
	{
		// Health
		api.GET("/health", s.HealthCheck)

		// Topology
		api.GET("/topology", s.GetTopology)
		api.GET("/topology/cluster/:cluster_id", s.GetClusterTopology)
		api.GET("/topology/search", s.SearchNodes)

		// Metrics
		api.GET("/metrics/:agent_id", s.GetMetrics)
		api.GET("/metrics/:agent_id/aggregated", s.GetAggregatedMetrics)
		api.POST("/metrics/query", s.QueryMetrics)
		api.GET("/metrics/names", s.GetMetricNames)

		// Traces
		api.GET("/traces", s.QueryTraces)
		api.GET("/traces/:trace_id", s.GetTrace)
		api.POST("/traces/query", s.QueryTracesPost)

		// Events
		api.GET("/events", s.QueryEvents)
		api.POST("/events/query", s.QueryEventsPost)

		// Network Events
		api.GET("/network-events", s.QueryNetworkEvents)

		// Alerts
		api.GET("/alerts", s.GetAlerts)
		api.GET("/alerts/rules", s.GetAlertRules)
		api.POST("/alerts/:alert_id/acknowledge", s.AcknowledgeAlert)

		// Agents
		api.GET("/agents", s.ListAgents)
		api.GET("/agents/:agent_id", s.GetAgent)
		api.GET("/agents/:agent_id/health", s.GetAgentHealth)

		// Timeline Snapshots
		api.GET("/timeline/snapshots", s.ListTimelineSnapshots)
		api.GET("/timeline/snapshots/diff", s.GetSnapshotDiff)
		api.GET("/timeline/snapshots/:snapshot_id", s.GetTimelineSnapshot)
	}
}

// HealthCheck returns service health status.
func (s *QueryService) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "paryty-query",
		"version": "1.0.0",
	})
}

// ─── Frontend-compatible response types ─────────────────────────────
// These DTOs normalize Go model field names to match the TypeScript
// frontend's camelCase expectations (status, sourceId, targetId, lastSeen, etc.)

type FrontendTopology struct {
	Nodes     []FrontendTopologyNode `json:"nodes"`
	Edges     []FrontendTopologyEdge `json:"edges"`
	Timestamp time.Time              `json:"timestamp"`
	Version   string                 `json:"version"`
}

type FrontendTopologyNode struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Metadata    map[string]string `json:"metadata"`
	LastSeen    time.Time         `json:"lastSeen"`
	IPAddress   string            `json:"ip_address,omitempty"`
	Port        uint32            `json:"port,omitempty"`
}

type FrontendTopologyEdge struct {
	ID         string            `json:"id"`
	SourceID   string            `json:"sourceId"`
	TargetID   string            `json:"targetId"`
	Type       string            `json:"type"`
	Protocol   string            `json:"protocol"`
	Labels     map[string]string `json:"labels"`
	LatencyMs  float64           `json:"latencyMs"`
	BytesPerSec float64          `json:"bytesPerSec"`
	ErrorRate  float64           `json:"errorRate"`
	Metadata   map[string]string `json:"metadata"`
	LastSeen   time.Time         `json:"lastSeen"`
}

// toFrontendTopology converts a models.Topology to the frontend-compatible format.
func toFrontendTopology(topo *models.Topology) *FrontendTopology {
	if topo == nil {
		return nil
	}

	nodes := make([]FrontendTopologyNode, len(topo.Nodes))
	for i, n := range topo.Nodes {
		metadata := n.Metadata
		if metadata == nil {
			metadata = map[string]string{}
		}
		nodes[i] = FrontendTopologyNode{
			ID:        n.ID,
			Name:      n.Name,
			Type:      string(n.Type),
			Status:    string(n.Health),
			Labels:    n.Labels,
			Metadata:  metadata,
			LastSeen:  n.LastSeen,
			IPAddress: n.IPAddress,
			Port:      n.Port,
		}
	}

	edges := make([]FrontendTopologyEdge, len(topo.Edges))
	for i, e := range topo.Edges {
		latencyMs := e.Metrics.LatencyP50
		bytesPerSec := float64(e.Metrics.BytesIn + e.Metrics.BytesOut)
		edges[i] = FrontendTopologyEdge{
			ID:          e.ID,
			SourceID:    e.SourceID,
			TargetID:    e.TargetID,
			Type:        string(e.Type),
			Protocol:    e.Protocol,
			Labels:      e.Labels,
			LatencyMs:   latencyMs,
			BytesPerSec: bytesPerSec,
			ErrorRate:   e.Metrics.ErrorRate,
			Metadata:    e.Labels,
			LastSeen:    e.LastSeen,
		}
	}

	return &FrontendTopology{
		Nodes:     nodes,
		Edges:     edges,
		Timestamp: topo.Timestamp,
		Version:   fmt.Sprintf("%d", topo.Version),
	}
}

// GetTopology returns the current topology in frontend-compatible format.
// When no X-Tenant-ID header is set, aggregates topology from all active tenants.
func (s *QueryService) GetTopology(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)

	// If a specific tenant is requested, return its topology directly.
	if tenant != "default" {
		topo, err := s.store.GetTopology(ctx, tenant)
		if err != nil {
			c.JSON(http.StatusOK, &FrontendTopology{
				Nodes:     []FrontendTopologyNode{},
				Edges:     []FrontendTopologyEdge{},
				Timestamp: time.Now(),
				Version:   "0",
			})
			return
		}
		c.JSON(http.StatusOK, toFrontendTopology(topo))
		return
	}

	// Default tenant: aggregate topology from all active tenants.
	// First, discover active tenants by listing agents.
	activeTenants := s.discoverTenants(ctx)

	merged := &FrontendTopology{
		Nodes:     []FrontendTopologyNode{},
		Edges:     []FrontendTopologyEdge{},
		Timestamp: time.Now(),
		Version:   "0",
	}

	seenNodeIDs := make(map[string]bool)
	maxVersion := uint64(0)

	for _, t := range activeTenants {
		if t == "default" {
			continue
		}
		topo, err := s.store.GetTopology(ctx, t)
		if err != nil || topo == nil {
			continue
		}
		ft := toFrontendTopology(topo)
		for _, node := range ft.Nodes {
			if !seenNodeIDs[node.ID] {
				seenNodeIDs[node.ID] = true
				merged.Nodes = append(merged.Nodes, node)
			}
		}
		for _, edge := range ft.Edges {
			merged.Edges = append(merged.Edges, edge)
		}
		if topo.Version > maxVersion {
			maxVersion = topo.Version
		}
		if topo.Timestamp.After(merged.Timestamp) {
			merged.Timestamp = topo.Timestamp
		}
	}

	merged.Version = fmt.Sprintf("%d", maxVersion)
	c.JSON(http.StatusOK, merged)
}

// discoverTenants returns a list of active tenant IDs.
// In production this would scan DragonflyDB keys.
// For now returns known tenants including gai-tech.
func (s *QueryService) discoverTenants(_ context.Context) []string {
	return []string{"tenant1", "tenant-b", "gai-tech", "default"}
}

// GetClusterTopology returns topology filtered to nodes belonging to a specific cluster.
// Matches nodes whose labels contain "cluster" or "cluster_id" matching the URL parameter.
func (s *QueryService) GetClusterTopology(c *gin.Context) {
	clusterID := c.Param("cluster_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	topo, err := s.store.GetTopology(ctx, tenant)
	if err != nil || topo == nil {
		c.JSON(http.StatusOK, &FrontendTopology{
			Nodes:     []FrontendTopologyNode{},
			Edges:     []FrontendTopologyEdge{},
			Timestamp: time.Now(),
			Version:   "0",
		})
		return
	}

	ft := toFrontendTopology(topo)

	// Filter nodes: keep those with a cluster label matching clusterID
	filteredNodes := make([]FrontendTopologyNode, 0)
	nodeIDs := make(map[string]bool)
	for _, node := range ft.Nodes {
		matches := false
		if v, ok := node.Labels["cluster"]; ok && v == clusterID {
			matches = true
		} else if v, ok := node.Labels["cluster_id"]; ok && v == clusterID {
			matches = true
		} else if v, ok := node.Metadata["cluster"]; ok && v == clusterID {
			matches = true
		}
		if matches {
			filteredNodes = append(filteredNodes, node)
			nodeIDs[node.ID] = true
		}
	}

	// Filter edges: keep only those connecting filtered nodes
	filteredEdges := make([]FrontendTopologyEdge, 0)
	for _, edge := range ft.Edges {
		if nodeIDs[edge.SourceID] && nodeIDs[edge.TargetID] {
			filteredEdges = append(filteredEdges, edge)
		}
	}

	c.JSON(http.StatusOK, &FrontendTopology{
		Nodes:     filteredNodes,
		Edges:     filteredEdges,
		Timestamp: ft.Timestamp,
		Version:   ft.Version,
	})
}

// SearchNodes searches topology nodes by name or label substring match.
// Query parameter: ?q= search query.
func (s *QueryService) SearchNodes(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusOK, []FrontendTopologyNode{})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Aggregate across all tenants for search
	activeTenants := s.discoverTenants(ctx)

	seenIDs := make(map[string]bool)
	results := make([]FrontendTopologyNode, 0)
	lowerQuery := strings.ToLower(query)

	for _, tenant := range activeTenants {
		topo, err := s.store.GetTopology(ctx, tenant)
		if err != nil || topo == nil {
			continue
		}
		ft := toFrontendTopology(topo)
		for _, node := range ft.Nodes {
			if seenIDs[node.ID] {
				continue
			}
			// Match against name, type, and label values
			if strings.Contains(strings.ToLower(node.Name), lowerQuery) ||
				strings.Contains(strings.ToLower(node.Type), lowerQuery) ||
				matchesLabels(node.Labels, lowerQuery) ||
				matchesLabels(node.Metadata, lowerQuery) {
				seenIDs[node.ID] = true
				results = append(results, node)
			}
		}
	}

	if results == nil {
		results = []FrontendTopologyNode{}
	}

	c.JSON(http.StatusOK, results)
}

// matchesLabels checks if any label value contains the query (case-insensitive).
func matchesLabels(labels map[string]string, lowerQuery string) bool {
	for _, v := range labels {
		if strings.Contains(strings.ToLower(v), lowerQuery) {
			return true
		}
	}
	return false
}

// GetMetrics returns metrics for an agent.
func (s *QueryService) GetMetrics(c *gin.Context) {
	agentID := c.Param("agent_id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	batch, err := s.store.GetLatestMetrics(ctx, tenant, agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, batch)
}

// GetAggregatedMetrics returns aggregated metrics.
func (s *QueryService) GetAggregatedMetrics(c *gin.Context) {
	agentID := c.Param("agent_id")
	metricName := c.Query("metric")
	windowStr := c.DefaultQuery("window", "1m")

	window, err := time.ParseDuration(windowStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid window"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	end := time.Now()
	start := end.Add(-24 * time.Hour)

	metrics, err := s.store.QueryAggregatedMetrics(ctx, agentID, metricName, window, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

// QueryTraces queries traces.
func (s *QueryService) QueryTraces(c *gin.Context) {
	service := c.Query("service")
	startStr := c.DefaultQuery("start", time.Now().Add(-1*time.Hour).Format(time.RFC3339))
	endStr := c.DefaultQuery("end", time.Now().Format(time.RFC3339))
	limit := 100

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start time"})
		return
	}

	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end time"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	traces, err := s.store.QueryTraces(ctx, service, start, end, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, traces)
}

// GetTrace returns a specific trace.
func (s *QueryService) GetTrace(c *gin.Context) {
	traceID := c.Param("trace_id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	traces, err := s.store.QueryTraces(ctx, "", time.Time{}, time.Now(), 1)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for _, trace := range traces {
		if trace.TraceID == traceID {
			c.JSON(http.StatusOK, trace)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "trace not found"})
}

// QueryEvents queries events.
func (s *QueryService) QueryEvents(c *gin.Context) {
	c.JSON(http.StatusOK, []models.Event{})
}

// GetAlerts returns active alerts.
func (s *QueryService) GetAlerts(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	alerts, err := s.store.GetActiveAlerts(ctx, tenant)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, alerts)
}

// QueryMetrics handles POST /api/v1/metrics/query for flexible metric queries.
// Accepts the TypeScript MetricQuery shape: {name, labels?, startTime, endTime, step, aggregation?}.
// Queries QuestDB for time-series data within the requested range and returns
// MetricSeries[] with {name, labels, points: [{timestamp, value}]} shape.
func (s *QueryService) QueryMetrics(c *gin.Context) {
	var query struct {
		AgentID     string            `json:"agentId"`
		Name        string            `json:"name"`
		Names       []string          `json:"names"`
		Labels      map[string]string `json:"labels"`
		StartTime   string            `json:"startTime"`
		EndTime     string            `json:"endTime"`
		Step        string            `json:"step"`
		Aggregation string            `json:"aggregation"`
	}
	if err := c.ShouldBindJSON(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse time range (RFC3339 timestamps from TypeScript)
	start, err := time.Parse(time.RFC3339, query.StartTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid startTime: " + err.Error()})
		return
	}
	end, err := time.Parse(time.RFC3339, query.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid endTime: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)

	// Collect metric names to query: use Names array if provided, else single Name
	metricNames := query.Names
	if len(metricNames) == 0 && query.Name != "" {
		metricNames = []string{query.Name}
	}
	if len(metricNames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one metric name is required"})
		return
	}

	// Determine which agents to query
	var agentIDs []string
	if query.AgentID != "" {
		agentIDs = []string{query.AgentID}
	} else {
		// No specific agent: discover active agents from storage
		agents, err := s.store.GetAllAgentStates(ctx, tenant)
		if err != nil || len(agents) == 0 {
			// Fallback: try without agent filter (empty string, QuestDB exact match only)
			// Use known agent IDs from topology discovery
			agentIDs = s.discoverTenants(ctx)
			if len(agentIDs) == 0 {
				agentIDs = []string{"gai-tech"}
			}
		} else {
			for _, a := range agents {
				agentIDs = append(agentIDs, a.ID)
			}
		}
	}

	// Query metrics across agents × metric names
	type metricPoint struct {
		Timestamp time.Time `json:"timestamp"`
		Value     float64   `json:"value"`
	}

	type seriesKey struct {
		AgentID string
		Name    string
	}

	type seriesBuilder struct {
		AgentID string
		Name    string
		Labels  map[string]string
		Points  []metricPoint
	}

	seriesMap := make(map[seriesKey]*seriesBuilder)

	for _, aid := range agentIDs {
		for _, name := range metricNames {
			metrics, err := s.store.QueryMetrics(ctx, aid, name, start, end)
			if err != nil {
				s.logger.Warn("Failed to query metrics",
					zap.String("agent_id", aid),
					zap.String("metric_name", name),
					zap.Error(err),
				)
				continue
			}
			for _, m := range metrics {
				key := seriesKey{AgentID: m.AgentID, Name: m.Name}
				sb, ok := seriesMap[key]
				if !ok {
					labels := m.Labels
					if labels == nil {
						labels = map[string]string{}
					}
					sb = &seriesBuilder{
						AgentID: m.AgentID,
						Name:    m.Name,
						Labels:  labels,
						Points:  make([]metricPoint, 0),
					}
					seriesMap[key] = sb
				}
				sb.Points = append(sb.Points, metricPoint{Timestamp: m.Timestamp, Value: m.Value})
			}
		}
	}

	// Build result array in MetricSeries shape
	result := make([]gin.H, 0, len(seriesMap))
	for _, sb := range seriesMap {
		result = append(result, gin.H{
			"name":   sb.Name,
			"labels": sb.Labels,
			"points": sb.Points,
		})
	}

	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, result)
}

// GetMetricNames returns available metric names dynamically from QuestDB.
// Falls back to a default list if the query fails.
func (s *QueryService) GetMetricNames(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	names, err := s.store.GetDistinctMetricNames(ctx)
	if err != nil || len(names) == 0 {
		// Fallback to default metric names
		s.logger.Debug("Falling back to default metric names", zap.Error(err))
		c.JSON(http.StatusOK, []string{
			"cpu.usage_percent",
			"cpu.load_average_1m",
			"memory.usage_percent",
			"memory.used_bytes",
			"disk.usage_percent",
			"disk.read_bytes_per_sec",
			"disk.write_bytes_per_sec",
			"network.rx_bytes_per_sec",
			"network.tx_bytes_per_sec",
		})
		return
	}

	c.JSON(http.StatusOK, names)
}

// QueryTracesPost handles POST /api/v1/traces/query.
func (s *QueryService) QueryTracesPost(c *gin.Context) {
	var query struct {
		Service string `json:"service"`
		Start   string `json:"start"`
		End     string `json:"end"`
		Limit   int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	start, _ := time.Parse(time.RFC3339, query.Start)
	end, _ := time.Parse(time.RFC3339, query.End)
	if start.IsZero() {
		start = time.Now().Add(-1 * time.Hour)
	}
	if end.IsZero() {
		end = time.Now()
	}
	if query.Limit == 0 {
		query.Limit = 100
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	traces, err := s.store.QueryTraces(ctx, query.Service, start, end, query.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, traces)
}

// QueryEventsPost handles POST /api/v1/events/query.
// Accepts frontend EventQuery: {source?, category?, severity?, startTime, endTime, limit?}.
func (s *QueryService) QueryEventsPost(c *gin.Context) {
	var query struct {
		Source    string `json:"source"`
		Category  string `json:"category"`
		Severity  string `json:"severity"`
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
		Limit     int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse time range
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	if query.StartTime != "" {
		if parsed, err := time.Parse(time.RFC3339, query.StartTime); err == nil {
			start = parsed
		}
	}
	if query.EndTime != "" {
		if parsed, err := time.Parse(time.RFC3339, query.EndTime); err == nil {
			end = parsed
		}
	}
	if query.Limit <= 0 {
		query.Limit = 100
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	events, err := s.store.QueryEvents(ctx, "", query.Category, query.Severity, start, end, query.Limit)
	if err != nil {
		s.logger.Warn("Failed to query events", zap.Error(err))
		c.JSON(http.StatusOK, []gin.H{})
		return
	}

	// Transform to frontend ParytyEvent shape
	result := make([]gin.H, 0, len(events))
	for _, e := range events {
		labels := e.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		result = append(result, gin.H{
			"id":        e.ID,
			"source":    e.Source,
			"category":  e.Category,
			"severity":  e.Severity,
			"title":     e.Title,
			"message":   e.Description,
			"labels":    labels,
			"timestamp": e.Timestamp,
			"acknowledged": false,
		})
	}
	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, result)
}

// GetAlertRules returns configured alert rules.
// Returns default system rules when no rules are persisted in storage.
func (s *QueryService) GetAlertRules(c *gin.Context) {
	// Default system alert rules
	rules := []models.AlertRule{
		{
			ID:          "rule-cpu-high",
			Name:        "High CPU Usage",
			Description: "Alert when CPU usage exceeds threshold for sustained period",
			Severity:    models.AlertSeverityWarning,
			MetricName:  "cpu.usage_percent",
			Condition:   models.AlertConditionAbove,
			Threshold:   90.0,
			Duration:    5 * time.Minute,
			Enabled:     true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "rule-cpu-critical",
			Name:        "Critical CPU Usage",
			Description: "Alert when CPU usage is critically high",
			Severity:    models.AlertSeverityCritical,
			MetricName:  "cpu.usage_percent",
			Condition:   models.AlertConditionAbove,
			Threshold:   95.0,
			Duration:    1 * time.Minute,
			Enabled:     true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "rule-memory-high",
			Name:        "High Memory Usage",
			Description: "Alert when memory usage exceeds threshold",
			Severity:    models.AlertSeverityWarning,
			MetricName:  "memory.usage_percent",
			Condition:   models.AlertConditionAbove,
			Threshold:   85.0,
			Duration:    5 * time.Minute,
			Enabled:     true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "rule-disk-high",
			Name:        "High Disk Usage",
			Description: "Alert when disk usage exceeds threshold",
			Severity:    models.AlertSeverityWarning,
			MetricName:  "disk.usage_percent",
			Condition:   models.AlertConditionAbove,
			Threshold:   90.0,
			Duration:    10 * time.Minute,
			Enabled:     true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	c.JSON(http.StatusOK, rules)
}

// AcknowledgeAlert acknowledges an alert.
func (s *QueryService) AcknowledgeAlert(c *gin.Context) {
	alertID := c.Param("alert_id")
	s.logger.Info("Alert acknowledged", zap.String("alert_id", alertID))
	c.JSON(http.StatusOK, gin.H{"status": "acknowledged", "alert_id": alertID})
}

// ListAgents returns all agents.
// When no X-Tenant-ID header is set (default), aggregates agents across all active tenants.
func (s *QueryService) ListAgents(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)

	// If a specific tenant is requested, return its agents directly.
	if tenant != "default" {
		agents, err := s.store.GetAllAgentStates(ctx, tenant)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, agents)
		return
	}

	// Default: aggregate agents from all active tenants.
	activeTenants := s.discoverTenants(ctx)
	seenIDs := make(map[string]int) // maps agent ID to index in allAgents
	var allAgents []models.AgentInfo

	for _, t := range activeTenants {
		// Skip the "default" tenant — it's a fallback bucket, not a real tenant.
		// Including it would pull stubs from warm storage (QuestDB) that
		// overwrite real agent metadata from actual tenants.
		if t == "default" {
			continue
		}
		agents, err := s.store.GetAllAgentStates(ctx, t)
		if err != nil {
			s.logger.Debug("Failed to get agents for tenant",
				zap.String("tenant", t), zap.Error(err))
			continue
		}
		for _, a := range agents {
			if idx, exists := seenIDs[a.ID]; exists {
				// If the new agent has hostname/OS metadata but the existing
				// entry is a stub (empty hostname), replace it with the richer entry.
				if a.Hostname != "" && allAgents[idx].Hostname == "" {
					allAgents[idx] = a
				}
			} else {
				seenIDs[a.ID] = len(allAgents)
				allAgents = append(allAgents, a)
			}
		}
	}

	if allAgents == nil {
		allAgents = []models.AgentInfo{}
	}
	c.JSON(http.StatusOK, allAgents)
}

// GetAgent returns a specific agent.
func (s *QueryService) GetAgent(c *gin.Context) {
	agentID := c.Param("agent_id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	agent, err := s.store.GetAgentState(ctx, tenant, agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	c.JSON(http.StatusOK, agent)
}

// GetAgentHealth returns agent health.
func (s *QueryService) GetAgentHealth(c *gin.Context) {
	agentID := c.Param("agent_id")
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	health, err := s.store.GetHealthReport(ctx, tenant, agentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "health report not found"})
		return
	}

	c.JSON(http.StatusOK, health)
}

// QueryNetworkEvents handles GET /api/v1/network-events.
//
// Query parameters:
//   - agent_id (optional) — filter by agent
//   - type (optional) — "tcp", "dns", or "http"
//   - start (optional) — RFC3339 timestamp, default 1 hour ago
//   - end (optional) — RFC3339 timestamp, default now
//   - limit (optional) — default 100, max 1000
//
// Response: JSON array of network events with a "type" discriminator field.
func (s *QueryService) QueryNetworkEvents(c *gin.Context) {
	agentID := c.Query("agent_id")
	eventType := c.Query("type")

	// Validate event type.
	if eventType != "" && eventType != "tcp" && eventType != "dns" && eventType != "http" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid type (valid: tcp, dns, http)"})
		return
	}

	// Parse start time, default to 1 hour ago.
	startStr := c.DefaultQuery("start", time.Now().Add(-1*time.Hour).Format(time.RFC3339))
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start time"})
		return
	}

	// Parse end time, default to now.
	endStr := c.DefaultQuery("end", time.Now().Format(time.RFC3339))
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end time"})
		return
	}

	// Parse limit, default 100, max 1000.
	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	events, err := s.store.QueryNetworkEvents(ctx, agentID, eventType, start, end, limit)
	if err != nil {
		s.logger.Error("query network events failed",
			zap.Error(err),
			zap.String("agent_id", agentID),
			zap.String("type", eventType),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return empty array instead of null when no events match.
	if events == nil {
		events = []map[string]interface{}{}
	}

	c.JSON(http.StatusOK, events)
}

// ─── Timeline Snapshot Handlers ─────────────────────────────────────

// ListTimelineSnapshots handles GET /api/v1/timeline/snapshots.
// Query parameters: start, end (RFC3339), limit (default 50).
func (s *QueryService) ListTimelineSnapshots(c *gin.Context) {
	tenant := tenantFromRequest(c)

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()
	limit := 50

	if s := c.Query("start"); s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			start = parsed
		}
	}
	if e := c.Query("end"); e != "" {
		if parsed, err := time.Parse(time.RFC3339, e); err == nil {
			end = parsed
		}
	}
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	metas, err := s.store.ListSnapshots(ctx, tenant, start, end, limit)
	if err != nil {
		s.logger.Warn("Failed to list snapshots", zap.Error(err))
		c.JSON(http.StatusOK, []gin.H{})
		return
	}

	// Transform SnapshotMeta to frontend TimelineSnapshot shape
	result := make([]gin.H, 0, len(metas))
	for _, m := range metas {
		result = append(result, gin.H{
			"id":         m.ID,
			"timestamp":  m.Timestamp,
			"nodes":      []gin.H{},
			"edges":      []gin.H{},
			"metrics":    gin.H{},
			"alertCount": m.AlertCount,
			"eventCount": 0,
		})
	}

	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, result)
}

// GetTimelineSnapshot handles GET /api/v1/timeline/snapshots/:snapshot_id.
func (s *QueryService) GetTimelineSnapshot(c *gin.Context) {
	snapshotID := c.Param("snapshot_id")
	tenant := tenantFromRequest(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	snap, err := s.store.GetSnapshot(ctx, tenant, snapshotID)
	if err != nil {
		s.logger.Warn("Failed to get snapshot", zap.String("snapshot_id", snapshotID), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "snapshot not found"})
		return
	}

	// Transform to frontend TimelineSnapshot shape
	nodes := make([]gin.H, 0)
	edges := make([]gin.H, 0)
	if snap.Graph != nil {
		for _, n := range snap.Graph.Nodes {
			nodes = append(nodes, gin.H{
				"id":       n.ID,
				"name":     n.Name,
				"type":     string(n.Type),
				"status":   string(n.Health),
				"labels":   n.Labels,
				"lastSeen": n.LastSeen,
			})
		}
		for _, e := range snap.Graph.Edges {
			edges = append(edges, gin.H{
				"id":       e.ID,
				"sourceId": e.SourceID,
				"targetId": e.TargetID,
				"type":     string(e.Type),
				"protocol": e.Protocol,
				"lastSeen": e.LastSeen,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         snap.ID,
		"timestamp":  snap.Timestamp,
		"nodes":      nodes,
		"edges":      edges,
		"metrics":    gin.H{},
		"alertCount": snap.Metadata.AlertCount,
		"eventCount": 0,
	})
}

// GetSnapshotDiff handles GET /api/v1/timeline/snapshots/diff?from=&to=.
func (s *QueryService) GetSnapshotDiff(c *gin.Context) {
	fromID := c.Query("from")
	toID := c.Query("to")
	if fromID == "" || toID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to query parameters are required"})
		return
	}

	tenant := tenantFromRequest(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	fromSnap, err := s.store.GetSnapshot(ctx, tenant, fromID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "from snapshot not found"})
		return
	}
	toSnap, err := s.store.GetSnapshot(ctx, tenant, toID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "to snapshot not found"})
		return
	}

	// Compute diff between the two snapshots
	addedNodes := make([]gin.H, 0)
	removedNodes := make([]string, 0)
	addedEdges := make([]string, 0)
	removedEdges := make([]string, 0)
	metricChanges := gin.H{}

	fromNodes := make(map[string]bool)
	toNodes := make(map[string]bool)

	if fromSnap.Graph != nil {
		for _, n := range fromSnap.Graph.Nodes {
			fromNodes[n.ID] = true
		}
	}
	if toSnap.Graph != nil {
		for _, n := range toSnap.Graph.Nodes {
			toNodes[n.ID] = true
			if !fromNodes[n.ID] {
				addedNodes = append(addedNodes, gin.H{
					"id":     n.ID,
					"name":   n.Name,
					"type":   string(n.Type),
					"status": string(n.Health),
				})
			}
		}
	}
	for id := range fromNodes {
		if !toNodes[id] {
			removedNodes = append(removedNodes, id)
		}
	}

	fromEdges := make(map[string]bool)
	toEdges := make(map[string]bool)
	if fromSnap.Graph != nil {
		for _, e := range fromSnap.Graph.Edges {
			fromEdges[e.ID] = true
		}
	}
	if toSnap.Graph != nil {
		for _, e := range toSnap.Graph.Edges {
			toEdges[e.ID] = true
			if !fromEdges[e.ID] {
				addedEdges = append(addedEdges, e.ID)
			}
		}
	}
	for id := range fromEdges {
		if !toEdges[id] {
			removedEdges = append(removedEdges, id)
		}
	}

	if addedNodes == nil {
		addedNodes = []gin.H{}
	}
	if removedNodes == nil {
		removedNodes = []string{}
	}
	if addedEdges == nil {
		addedEdges = []string{}
	}
	if removedEdges == nil {
		removedEdges = []string{}
	}

	c.JSON(http.StatusOK, gin.H{
		"fromId":         fromID,
		"toId":           toID,
		"addedNodes":     addedNodes,
		"removedNodes":   removedNodes,
		"addedEdges":     addedEdges,
		"removedEdges":   removedEdges,
		"metricChanges":  metricChanges,
	})
}
