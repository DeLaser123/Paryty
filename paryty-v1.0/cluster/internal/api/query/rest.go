package api

import (
	"context"
	"net/http"
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

		// Alerts
		api.GET("/alerts", s.GetAlerts)
		api.GET("/alerts/rules", s.GetAlertRules)
		api.POST("/alerts/:alert_id/acknowledge", s.AcknowledgeAlert)

		// Agents
		api.GET("/agents", s.ListAgents)
		api.GET("/agents/:agent_id", s.GetAgent)
		api.GET("/agents/:agent_id/health", s.GetAgentHealth)
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

// GetTopology returns the current topology.
func (s *QueryService) GetTopology(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	topo, err := s.store.GetTopology(ctx, tenant)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, topo)
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
func (s *QueryService) QueryMetrics(c *gin.Context) {
	var query struct {
		AgentID  string   `json:"agent_id"`
		Names    []string `json:"names"`
		Start    string   `json:"start"`
		End      string   `json:"end"`
		Interval string   `json:"interval"`
	}
	if err := c.ShouldBindJSON(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	batch, err := s.store.GetLatestMetrics(ctx, tenant, query.AgentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert batch to series format expected by frontend
	var series []gin.H
	if batch != nil {
		for _, cpu := range batch.CPU {
			series = append(series, gin.H{"name": "cpu.usage_percent", "agent_id": cpu.AgentID, "points": []gin.H{{"timestamp": cpu.Timestamp, "value": cpu.TotalUsagePct}}})
		}
		for _, mem := range batch.Memory {
			usedPct := float64(mem.UsedBytes) / float64(mem.TotalBytes) * 100
			series = append(series, gin.H{"name": "memory.usage_percent", "agent_id": mem.AgentID, "points": []gin.H{{"timestamp": mem.Timestamp, "value": usedPct}}})
		}
	}

	c.JSON(http.StatusOK, series)
}

// GetMetricNames returns available metric names.
func (s *QueryService) GetMetricNames(c *gin.Context) {
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
func (s *QueryService) QueryEventsPost(c *gin.Context) {
	c.JSON(http.StatusOK, []models.Event{})
}

// GetAlertRules returns alert rules.
func (s *QueryService) GetAlertRules(c *gin.Context) {
	c.JSON(http.StatusOK, []models.AlertRule{})
}

// AcknowledgeAlert acknowledges an alert.
func (s *QueryService) AcknowledgeAlert(c *gin.Context) {
	alertID := c.Param("alert_id")
	s.logger.Info("Alert acknowledged", zap.String("alert_id", alertID))
	c.JSON(http.StatusOK, gin.H{"status": "acknowledged", "alert_id": alertID})
}

// ListAgents returns all agents.
func (s *QueryService) ListAgents(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	tenant := tenantFromRequest(c)
	agents, err := s.store.GetAllAgentStates(ctx, tenant)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, agents)
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
