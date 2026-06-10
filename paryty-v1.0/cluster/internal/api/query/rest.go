package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/auth"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/controlplane"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/twin"
	"go.uber.org/zap"
)

// QueryService handles query API requests.
type QueryService struct {
	store          *storage.Store
	logger         *zap.Logger
	apiKeyManager  *controlplane.APIKeyManager // nil if control plane not configured
	agentAssigner  *twin.AgentAssigner         // nil if control plane not configured
	authHandler    interface{}                  // Phase 8: auth.AuthHandler (interface to avoid import cycles)
	twinAPI        TwinAPI                     // Phase 8: TwinService for agent/twin management
	db             *pgxpool.Pool       // Phase 8: PostgreSQL control plane pool
	planEngine     *plan.PlanEngine    // Phase 8: Plan engine for plan operations
}

// TwinAPI defines the twin management operations needed by the REST layer.
// Implemented by auth.TwinHandler to avoid circular imports.
type TwinAPI interface {
	CreateTwin(ctx context.Context, tenantID, name, description string) (map[string]interface{}, error)
	ListTwins(ctx context.Context, tenantID string) ([]map[string]interface{}, error)
	GetTwin(ctx context.Context, tenantID, twinID string) (map[string]interface{}, error)
	UpdateTwin(ctx context.Context, tenantID, twinID, name, description string) (map[string]interface{}, error)
	DeleteTwin(ctx context.Context, tenantID, twinID string) error
	ListTwinAgents(ctx context.Context, twinID string) ([]map[string]interface{}, error)
	AssignAgentToTwin(ctx context.Context, tenantID, twinID, agentID string) error
	AcceptBacklog(ctx context.Context, agentID string) error
	RejectBacklog(ctx context.Context, agentID string) error
}

// NewQueryService creates a new query service.
func NewQueryService(store *storage.Store, apiKeyManager *controlplane.APIKeyManager, agentAssigner *twin.AgentAssigner, logger *zap.Logger) *QueryService {
	return &QueryService{
		store:         store,
		logger:        logger,
		apiKeyManager: apiKeyManager,
		agentAssigner: agentAssigner,
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

// SetAuthHandler attaches an AuthHandler for Phase 8 auth routes.
// Called during wiring in main.go after Phase 8 initialization.
func (s *QueryService) SetAuthHandler(authHandler interface{}) {
	s.authHandler = authHandler
}

// SetTwinAPI attaches a TwinAPI implementation for Phase 8 twin management routes.
func (s *QueryService) SetTwinAPI(api TwinAPI) {
	s.twinAPI = api
}

// SetDB attaches a PostgreSQL pool for user/tenant management.
func (s *QueryService) SetDB(db *pgxpool.Pool) {
	s.db = db
}

// SetPlanEngine attaches a PlanEngine for plan management.
func (s *QueryService) SetPlanEngine(pe *plan.PlanEngine) {
	s.planEngine = pe
}

// HealthCheck returns service health status.
func (s *QueryService) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "paryty-query",
		"version": "1.0.0",
	})
}

// â”€â”€â”€ Frontend-compatible response types â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
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
//
// Query parameters:
//
//	include_processes=true  â€” include individual process nodes (default: false).
//	                          Process nodes are omitted by default because a
//	                          single Windows/Linux host reports hundreds of
//	                          processes; rendering them all as flat graph nodes
//	                          saturates the WebGL renderer and freezes the
//	                          browser. The topology drill-down path (via
//	                          /topology/cluster/:cluster_id) is the correct
//	                          entry point for per-host process inspection.
func (s *QueryService) GetTopology(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	includeProcesses := strings.EqualFold(c.DefaultQuery("include_processes", "false"), "true")

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
		ft := toFrontendTopology(topo)
		if !includeProcesses {
			ft = filterOutProcessNodes(ft)
		}
		c.JSON(http.StatusOK, ft)
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
		topo, err := s.store.GetTopology(ctx, t)
		if err != nil || topo == nil {
			continue
		}
		ft := toFrontendTopology(topo)
		if !includeProcesses {
			ft = filterOutProcessNodes(ft)
		}
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

// filterOutProcessNodes removes process-type nodes and any edges that connect
// to them from a FrontendTopology.
//
// This is the structural fix for the browser-freeze bug: the Windows agent
// reports hundreds of OS processes as individual topology nodes. Rendering them
// as a flat graph saturates the PixiJS WebGL renderer (15K sprite pool, 10K
// particle cap). The correct UX is to show processes only on the
// per-host drill-down view (/topology/cluster/:cluster_id with
// include_processes=true), not in the global topology view.
//
// Edges whose source or target was a process node are also removed so the
// remaining graph remains consistent (no dangling endpoints).
func filterOutProcessNodes(ft *FrontendTopology) *FrontendTopology {
	if ft == nil {
		return nil
	}

	processIDs := make(map[string]bool)
	filteredNodes := make([]FrontendTopologyNode, 0, len(ft.Nodes))
	for _, n := range ft.Nodes {
		if n.Type == string(models.NodeTypeProcess) {
			processIDs[n.ID] = true
			continue
		}
		filteredNodes = append(filteredNodes, n)
	}

	// If no process nodes were present, return unchanged to avoid allocation.
	if len(processIDs) == 0 {
		return ft
	}

	filteredEdges := make([]FrontendTopologyEdge, 0, len(ft.Edges))
	for _, e := range ft.Edges {
		if processIDs[e.SourceID] || processIDs[e.TargetID] {
			continue
		}
		filteredEdges = append(filteredEdges, e)
	}

	return &FrontendTopology{
		Nodes:     filteredNodes,
		Edges:     filteredEdges,
		Timestamp: ft.Timestamp,
		Version:   ft.Version,
	}
}

// GetClusterTopology returns topology filtered to nodes belonging to a specific cluster.
// Matches nodes whose labels contain "cluster" or "cluster_id" matching the URL parameter.
//
// Pass include_processes=true to include OS process nodes (default: false, same
// rationale as GetTopology). This endpoint is the correct drill-down path for
// per-host process inspection.
func (s *QueryService) GetClusterTopology(c *gin.Context) {
	clusterID := c.Param("cluster_id")
	includeProcesses := strings.EqualFold(c.DefaultQuery("include_processes", "false"), "true")

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
		// Honour include_processes filter
		if !includeProcesses && node.Type == string(models.NodeTypeProcess) {
			continue
		}
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

	// Query metrics across agents Ã— metric names
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
		// Skip the "default" tenant â€” it's a fallback bucket, not a real tenant.
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
//   - agent_id (optional) â€” filter by agent
//   - type (optional) â€” "tcp", "dns", or "http"
//   - start (optional) â€” RFC3339 timestamp, default 1 hour ago
//   - end (optional) â€” RFC3339 timestamp, default now
//   - limit (optional) â€” default 100, max 1000
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

// â”€â”€â”€ Timeline Snapshot Handlers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

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

// â”€â”€ Phase 8: Twin Management REST Routes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// RegisterTwinRoutes registers twin management routes on an auth-protected group.
// Called from main.go with the authd router group (JWT + plan middleware applied).
func (s *QueryService) RegisterTwinRoutes(authGroup *gin.RouterGroup, writeMW, createMW gin.HandlerFunc) {
	authGroup.GET("/twins", s.ListTwins)
	authGroup.POST("/twins", createMW, s.CreateTwin)
	authGroup.GET("/twins/:twin_id", s.GetTwin)
	authGroup.PUT("/twins/:twin_id", writeMW, s.UpdateTwin)
	authGroup.DELETE("/twins/:twin_id", writeMW, s.DeleteTwin)
	authGroup.GET("/twins/:twin_id/agents", s.ListTwinAgents)
	authGroup.POST("/twins/:twin_id/agents", writeMW, s.AssignAgentToTwin)
	authGroup.POST("/twins/:twin_id/backlogs/:agent_id/accept", s.AcceptBacklog)
	authGroup.POST("/twins/:twin_id/backlogs/:agent_id/reject", s.RejectBacklog)
	authGroup.GET("/agents/unassigned", s.ListUnassignedAgents)
}

// tenantFromJWT extracts the tenant ID from the Gin context populated by JWT middleware.
func tenantFromJWT(c *gin.Context) (string, bool) {
	v, ok := c.Get(string(plan.CtxTenantID))
	if !ok {
		return "", false
	}
	tenantID, ok := v.(string)
	return tenantID, ok && tenantID != ""
}

// CreateTwin handles POST /api/v1/twins
func (s *QueryService) CreateTwin(c *gin.Context) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHENTICATED", "message": "Missing tenant context"})
		return
	}
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	twin, err := s.twinAPI.CreateTwin(c.Request.Context(), tenantID, req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": twin})
}

// ListTwins handles GET /api/v1/twins
func (s *QueryService) ListTwins(c *gin.Context) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHENTICATED", "message": "Missing tenant context"})
		return
	}
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	twins, err := s.twinAPI.ListTwins(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	if twins == nil {
		twins = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"data": twins})
}

// GetTwin handles GET /api/v1/twins/:twin_id
func (s *QueryService) GetTwin(c *gin.Context) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHENTICATED", "message": "Missing tenant context"})
		return
	}
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	twinID := c.Param("twin_id")
	twin, err := s.twinAPI.GetTwin(c.Request.Context(), tenantID, twinID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": twin})
}

// UpdateTwin handles PUT /api/v1/twins/:twin_id
func (s *QueryService) UpdateTwin(c *gin.Context) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHENTICATED", "message": "Missing tenant context"})
		return
	}
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	twinID := c.Param("twin_id")
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	twin, err := s.twinAPI.UpdateTwin(c.Request.Context(), tenantID, twinID, req.Name, req.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": twin})
}

// DeleteTwin handles DELETE /api/v1/twins/:twin_id
func (s *QueryService) DeleteTwin(c *gin.Context) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHENTICATED", "message": "Missing tenant context"})
		return
	}
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	twinID := c.Param("twin_id")
	if err := s.twinAPI.DeleteTwin(c.Request.Context(), tenantID, twinID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": true}})
}

// ListTwinAgents handles GET /api/v1/twins/:twin_id/agents
func (s *QueryService) ListTwinAgents(c *gin.Context) {
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	twinID := c.Param("twin_id")
	agents, err := s.twinAPI.ListTwinAgents(c.Request.Context(), twinID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	if agents == nil {
		agents = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"data": agents})
}

// AssignAgentToTwin handles POST /api/v1/twins/:twin_id/agents
func (s *QueryService) AssignAgentToTwin(c *gin.Context) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHENTICATED", "message": "Missing tenant context"})
		return
	}
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	twinID := c.Param("twin_id")
	var req struct {
		AgentID string `json:"agent_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	if err := s.twinAPI.AssignAgentToTwin(c.Request.Context(), tenantID, twinID, req.AgentID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"accepted": true}})
}

// AcceptBacklog handles POST /api/v1/twins/:twin_id/backlogs/:agent_id/accept
func (s *QueryService) AcceptBacklog(c *gin.Context) {
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	agentID := c.Param("agent_id")
	if err := s.twinAPI.AcceptBacklog(c.Request.Context(), agentID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"accepted": true}})
}

// RejectBacklog handles POST /api/v1/twins/:twin_id/backlogs/:agent_id/reject
func (s *QueryService) RejectBacklog(c *gin.Context) {
	if s.twinAPI == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "NOT_IMPLEMENTED", "message": "Twin service requires PostgreSQL control plane"})
		return
	}

	agentID := c.Param("agent_id")
	if err := s.twinAPI.RejectBacklog(c.Request.Context(), agentID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": true}})
}

// ListUnassignedAgents handles GET /api/v1/agents/unassigned
func (s *QueryService) ListUnassignedAgents(c *gin.Context) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Missing tenant context"})
		return
	}
	if s.agentAssigner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "Agent management requires PostgreSQL control plane"})
		return
	}

	agents, err := s.agentAssigner.ListUnassignedAgents(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if agents == nil {
		agents = []twin.UnassignedAgentInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"data": agents})
}

// â”€â”€ API Key Management Routes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// RegisterApiKeyRoutes registers API key management endpoints on the
// authenticated route group. Routes return 503 if the control plane is
// not configured (no DATABASE_URL / APIKeyManager).
func (s *QueryService) RegisterApiKeyRoutes(authGroup *gin.RouterGroup, writeMW gin.HandlerFunc) {
	authGroup.GET("/api-keys", s.ListApiKeys)
	authGroup.POST("/api-keys", writeMW, s.CreateApiKey)
	authGroup.DELETE("/api-keys/:key_id", writeMW, s.DeleteApiKey)
}

// ListApiKeys handles GET /api/v1/api-keys
func (s *QueryService) ListApiKeys(c *gin.Context) {
	if s.apiKeyManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "API key management requires PostgreSQL control plane"})
		return
	}

	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Missing tenant context"})
		return
	}

	keys, err := s.apiKeyManager.ListKeys(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	// Map to frontend-friendly shape
	type apiKeyResp struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Prefix    string `json:"prefix"`
		CreatedAt string `json:"createdAt"`
	}
	resp := make([]apiKeyResp, 0, len(keys))
	for _, k := range keys {
		if k.RevokedAt != nil {
			continue
		}
		resp = append(resp, apiKeyResp{
			ID:        k.KeyID,
			Name:      k.Name,
			Prefix:    k.KeyPrefix,
			CreatedAt: k.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, resp)
}

// CreateApiKey handles POST /api/v1/api-keys
func (s *QueryService) CreateApiKey(c *gin.Context) {
	if s.apiKeyManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "API key management requires PostgreSQL control plane"})
		return
	}

	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Missing tenant context"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	rawKey, keyID, err := s.apiKeyManager.GenerateKey(c.Request.Context(), tenantID, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":        keyID,
		"name":      req.Name,
		"key":       rawKey,
		"prefix":    rawKey[:16],
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// DeleteApiKey handles DELETE /api/v1/api-keys/:key_id
func (s *QueryService) DeleteApiKey(c *gin.Context) {
	if s.apiKeyManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "API key management requires PostgreSQL control plane"})
		return
	}

	keyID := c.Param("key_id")
	if err := s.apiKeyManager.RevokeKey(c.Request.Context(), keyID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": true}})
}

// ── Phase 8: User Management Handlers ────────────────────────────────

// dbGuard returns true if the control plane DB is available.
// Returns 503 and false otherwise.
func (s *QueryService) dbGuard(c *gin.Context) bool {
	if s.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "User management requires PostgreSQL control plane"})
		return false
	}
	return true
}

// requireTenant extracts tenant ID from JWT context or aborts with 401.
func (s *QueryService) requireTenant(c *gin.Context) (string, bool) {
	tenantID, ok := tenantFromJWT(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "UNAUTHENTICATED", "message": "Missing tenant context"})
		return "", false
	}
	return tenantID, true
}

// userResponse is the JSON shape returned for user objects.
type userResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	IsActive  bool   `json:"isActive"`
	CreatedAt string `json:"createdAt"`
}

// ListUsers handles GET /api/v1/users — list all users in the tenant.
func (s *QueryService) ListUsers(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, email, name, role, is_active, created_at
		FROM users
		WHERE tenant_id = $1
		ORDER BY created_at
	`, tenantID)
	if err != nil {
		s.logger.Error("Failed to list users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	defer rows.Close()

	users := make([]userResponse, 0)
	for rows.Next() {
		var u userResponse
		var dbTenantID string
		var createdAt time.Time
		if err := rows.Scan(&u.ID, &dbTenantID, &u.Email, &u.Name, &u.Role, &u.IsActive, &createdAt); err != nil {
			s.logger.Error("Failed to scan user row", zap.Error(err))
			continue
		}
		u.CreatedAt = createdAt.Format(time.RFC3339)
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		s.logger.Error("Row iteration error", zap.Error(err))
	}

	c.JSON(http.StatusOK, gin.H{"data": users})
}

// CreateUser handles POST /api/v1/users — create a sub-user in the tenant.
func (s *QueryService) CreateUser(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	var req struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
		Name     string `json:"name" binding:"required"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	// Validate password policy.
	if err := auth.ValidatePasswordPolicy(req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	// Default role to "viewer".
	if req.Role == "" {
		req.Role = "viewer"
	}
	validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
	if !validRoles[req.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": "Invalid role: must be admin, operator, or viewer"})
		return
	}

	// Hash password.
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": "Failed to hash password"})
		return
	}

	userID := uuid.New().String()
	now := time.Now().UTC()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err = s.db.Exec(ctx, `
		INSERT INTO users (id, tenant_id, email, password_hash, name, role, permissions, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, '{}', true, $7, $7)
	`, userID, tenantID, req.Email, passwordHash, req.Name, req.Role, now)
	if err != nil {
		// Check for duplicate email within tenant.
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "ALREADY_EXISTS", "message": "A user with this email already exists in the tenant"})
			return
		}
		s.logger.Error("Failed to create user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"data": userResponse{
			ID:        userID,
			Email:     req.Email,
			Name:      req.Name,
			Role:      req.Role,
			IsActive:  true,
			CreatedAt: now.Format(time.RFC3339),
		},
	})
}

// GetUser handles GET /api/v1/users/:user_id — get a single user.
func (s *QueryService) GetUser(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	userID := c.Param("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	var u userResponse
	var createdAt time.Time
	err := s.db.QueryRow(ctx, `
		SELECT id, email, name, role, is_active, created_at
		FROM users
		WHERE id = $1 AND tenant_id = $2
	`, userID, tenantID).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.IsActive, &createdAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "User not found"})
			return
		}
		s.logger.Error("Failed to get user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}

	u.CreatedAt = createdAt.Format(time.RFC3339)
	c.JSON(http.StatusOK, gin.H{"data": u})
}

// UpdateUser handles PUT /api/v1/users/:user_id — update user fields.
func (s *QueryService) UpdateUser(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	userID := c.Param("user_id")

	var req struct {
		Name     *string `json:"name"`
		Role     *string `json:"role"`
		IsActive *bool   `json:"isActive"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	// Validate role if provided.
	if req.Role != nil {
		validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
		if !validRoles[*req.Role] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": "Invalid role: must be admin, operator, or viewer"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// Build dynamic update.
	setClauses := make([]string, 0)
	args := make([]interface{}, 0)
	argIdx := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Role != nil {
		setClauses = append(setClauses, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, *req.Role)
		argIdx++
	}
	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *req.IsActive)
		argIdx++
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": "No fields to update"})
		return
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now().UTC())
	argIdx++

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d AND tenant_id = $%d", strings.Join(setClauses, ", "), argIdx, argIdx+1)
	args = append(args, userID, tenantID)

	tag, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		s.logger.Error("Failed to update user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "User not found"})
		return
	}

	// Return updated user.
	var u userResponse
	var createdAt time.Time
	err = s.db.QueryRow(ctx, `
		SELECT id, email, name, role, is_active, created_at
		FROM users
		WHERE id = $1 AND tenant_id = $2
	`, userID, tenantID).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.IsActive, &createdAt)
	if err != nil {
		s.logger.Error("Failed to fetch updated user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}

	u.CreatedAt = createdAt.Format(time.RFC3339)
	c.JSON(http.StatusOK, gin.H{"data": u})
}

// DeleteUser handles DELETE /api/v1/users/:user_id — soft-delete (deactivate) a user.
func (s *QueryService) DeleteUser(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	userID := c.Param("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tag, err := s.db.Exec(ctx, `
		UPDATE users SET is_active = false, updated_at = $1
		WHERE id = $2 AND tenant_id = $3
	`, time.Now().UTC(), userID, tenantID)
	if err != nil {
		s.logger.Error("Failed to deactivate user", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": true}})
}
// â”€â”€ Phase 8: Tenant Management Handlers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// tenantResponse is the JSON shape returned for tenant objects.
type tenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// GetTenant handles GET /api/v1/tenant â€” get current tenant details.
func (s *QueryService) GetTenant(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	var t tenantResponse
	var createdAt time.Time
	err := s.db.QueryRow(ctx, `
		SELECT tenant_id, name, status, created_at
		FROM tenants
		WHERE tenant_id = $1
	`, tenantID).Scan(&t.ID, &t.Name, &t.Status, &createdAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Tenant not found"})
			return
		}
		s.logger.Error("Failed to get tenant", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}

	t.CreatedAt = createdAt.Format(time.RFC3339)
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// UpdateTenant handles PUT /api/v1/tenant â€” update tenant name.
func (s *QueryService) UpdateTenant(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tag, err := s.db.Exec(ctx, `
		UPDATE tenants SET name = $1 WHERE tenant_id = $2
	`, req.Name, tenantID)
	if err != nil {
		s.logger.Error("Failed to update tenant", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND", "message": "Tenant not found"})
		return
	}

	// Return updated tenant.
	var t tenantResponse
	var createdAt time.Time
	err = s.db.QueryRow(ctx, `
		SELECT tenant_id, name, status, created_at
		FROM tenants
		WHERE tenant_id = $1
	`, tenantID).Scan(&t.ID, &t.Name, &t.Status, &createdAt)
	if err != nil {
		s.logger.Error("Failed to fetch updated tenant", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}

	t.CreatedAt = createdAt.Format(time.RFC3339)
	c.JSON(http.StatusOK, gin.H{"data": t})
}

// â”€â”€ Phase 8: Plan Management Handlers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// GetTenantPlan handles GET /api/v1/tenant/plan â€” get current tenant's plan.
func (s *QueryService) GetTenantPlan(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	if s.planEngine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "Plan engine not configured"})
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	stored, err := s.planEngine.GetTenantPlan(ctx, tenantID)
	if err != nil {
		s.logger.Error("Failed to get tenant plan", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"planName":  stored.PlanName,
			"features":  stored.Features,
			"limits":    stored.Limits,
			"quotas":    stored.Quotas,
			"startedAt": stored.StartedAt,
		},
	})
}

// ChangePlan handles POST /api/v1/tenant/plan/change â€” request a plan change.
func (s *QueryService) ChangePlan(c *gin.Context) {
	if !s.dbGuard(c) {
		return
	}
	if s.planEngine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "Plan engine not configured"})
		return
	}
	tenantID, ok := s.requireTenant(c)
	if !ok {
		return
	}

	var req struct {
		PlanName string `json:"planName" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": err.Error()})
		return
	}

	// Validate plan exists (allow non-creatable plans for admin assignment).
	if !s.planEngine.PlanExists(req.PlanName) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "INVALID_ARGUMENT", "message": "Unknown plan: " + req.PlanName})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.planEngine.AssignPlan(ctx, tenantID, req.PlanName); err != nil {
		s.logger.Error("Failed to assign plan", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "INTERNAL", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"planName": req.PlanName,
			"message":  "plan updated",
		},
	})
}

// â”€â”€ Phase 8: Admin Route Registration â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// RegisterAdminRoutes registers user, tenant, and plan management routes on
// the authenticated route group. Called from main.go after JWT middleware setup.
func (s *QueryService) RegisterAdminRoutes(authGroup *gin.RouterGroup, userWriteMW, userReadMW, tenantWriteMW gin.HandlerFunc) {
	// User management
	authGroup.GET("/users", userReadMW, s.ListUsers)
	authGroup.POST("/users", userWriteMW, s.CreateUser)
	authGroup.GET("/users/:user_id", userReadMW, s.GetUser)
	authGroup.PUT("/users/:user_id", userWriteMW, s.UpdateUser)
	authGroup.DELETE("/users/:user_id", userWriteMW, s.DeleteUser)

	// Tenant management
	authGroup.GET("/tenant", userReadMW, s.GetTenant)
	authGroup.PUT("/tenant", tenantWriteMW, s.UpdateTenant)

	// Plan management
	authGroup.GET("/tenant/plan", userReadMW, s.GetTenantPlan)
	authGroup.POST("/tenant/plan/change", tenantWriteMW, s.ChangePlan)
}
