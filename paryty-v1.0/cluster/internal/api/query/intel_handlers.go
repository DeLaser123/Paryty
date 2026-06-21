// Package api provides Gin-compatible HTTP handlers for the Intelligence Layer.
//
// These handlers bridge the frontend's /api/v1/intel/* routes to the
// intelligence clients (forecasting + anomaly detection). They call the
// intelligence clients directly rather than wrapping the net/http handlers
// from handler/ to avoid r.PathValue() incompatibilities with Gin.
package api

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/intelligence"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IntelHandlers holds the intelligence API handlers for Gin routing.
type IntelHandlers struct {
	forecastClient intelligence.ForecastClient
	anomalyClient  intelligence.AnomalyClient
	logger         *zap.Logger
}

// IntelConfig configures the intelligence client connections.
type IntelConfig struct {
	Address string
	Timeout time.Duration
}

// NewIntelConfigFromEnv creates IntelConfig from environment variables.
func NewIntelConfigFromEnv() IntelConfig {
	addr := os.Getenv("PARYTY_INTELLIGENCE_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}
	return IntelConfig{
		Address: addr,
		Timeout: 30 * time.Second,
	}
}

// NewIntelHandlers creates intelligence clients and returns Gin handlers.
func NewIntelHandlers(cfg IntelConfig, logger *zap.Logger) (*IntelHandlers, error) {
	forecastClient, err := intelligence.NewForecastClient(
		intelligence.ForecastClientConfig{
			Address: cfg.Address,
			Timeout: cfg.Timeout,
		},
		logger,
	)
	if err != nil {
		return nil, err
	}

	anomalyClient, err := intelligence.NewAnomalyClient(
		intelligence.AnomalyClientConfig{
			Address: cfg.Address,
			Timeout: cfg.Timeout,
		},
		logger,
	)
	if err != nil {
		forecastClient.Close()
		return nil, err
	}

	return &IntelHandlers{
		forecastClient: forecastClient,
		anomalyClient:  anomalyClient,
		logger:         logger,
	}, nil
}

// Close closes the underlying gRPC connections.
func (h *IntelHandlers) Close() {
	if h.forecastClient != nil {
		h.forecastClient.Close()
	}
	if h.anomalyClient != nil {
		h.anomalyClient.Close()
	}
}

// RegisterRoutes registers all intelligence API routes on the given Gin
// router group. The group MUST already be rooted at /api/v1 and protected
// by JWT middleware — intelligence results are tenant-confidential and the
// /intel feature is plan-gated upstream.
func (h *IntelHandlers) RegisterRoutes(rg *gin.RouterGroup) {
	intel := rg.Group("/intel")
	{
		// Forecasting
		intel.POST("/forecast", h.handleForecast)
		intel.POST("/forecast/batch", h.handleForecastBatch)
		intel.GET("/models/accuracy", h.handleModelAccuracy)
		intel.POST("/models/retrain", h.handleRetrainModels)

		// Anomaly detection
		intel.POST("/anomalies/detect", h.handleDetectAnomalies)
		intel.GET("/anomalies/status", h.handleAnomalyStatus)
		intel.POST("/anomalies/explain", h.handleExplainAnomaly)
	}
}

// ─── Forecast Handlers ────────────────────────────────────────────

// forecastRequest matches the frontend's ForecastQuery TypeScript type.
type forecastRequest struct {
	MetricName     string  `json:"metricName"`
	HorizonSeconds int     `json:"horizonSeconds"`
	ServiceID      string  `json:"serviceId"`
	Confidence     float64 `json:"confidence"`
}

func (h *IntelHandlers) handleForecast(c *gin.Context) {
	var req forecastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if req.MetricName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "metricName is required"})
		return
	}

	tenantID := c.GetString("tenant_id")

	if req.HorizonSeconds == 0 {
		req.HorizonSeconds = 86400 // default 1 day
	}
	if req.Confidence == 0 {
		req.Confidence = 0.95
	}
	if req.ServiceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serviceId is required"})
		return
	}

	freq := &intelligence.ForecastRequest{
		TenantID:        tenantID,
		ServiceID:       req.ServiceID,
		MetricName:      req.MetricName,
		Horizon:         time.Duration(req.HorizonSeconds) * time.Second,
		ConfidenceLevel: req.Confidence,
	}

	resp, err := h.forecastClient.ForecastMetric(c.Request.Context(), freq)
	if err != nil {
		h.logger.Error("Forecast failed", zap.String("metric", req.MetricName), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "forecast generation failed"})
		return
	}

	// Transform backend ForecastResponse into the frontend's ForecastSeries shape.
	points := make([]gin.H, 0, len(resp.Points))
	for _, p := range resp.Points {
		points = append(points, gin.H{
			"timestamp":  p.Timestamp,
			"value":      p.PredictedValue,
			"lowerBound": p.LowerBound,
			"upperBound": p.UpperBound,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"metricName": resp.MetricName,
		"agentId":    resp.ServiceID,
		"tenantId":   tenantID,
		"points":     points,
		"modelInfo": gin.H{
			"bestModel":       resp.Model,
			"weights":         gin.H{resp.Model: 1.0},
			"accuracy":        gin.H{resp.Model: 0.0},
			"lastTrained":     resp.GeneratedAt,
			"trainingSamples": 0,
		},
		"overallConfidence": resp.ConfidenceScore,
	})
}

// forecastBatchRequest matches the frontend's batch forecast call.
type forecastBatchRequest struct {
	Queries []forecastRequest `json:"queries"`
}

func (h *IntelHandlers) handleForecastBatch(c *gin.Context) {
	var req forecastBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if len(req.Queries) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "queries array must not be empty"})
		return
	}

	tenantID := c.GetString("tenant_id")

	batchReq := &intelligence.ForecastBatchRequest{}
	for _, q := range req.Queries {
		if q.HorizonSeconds == 0 {
			q.HorizonSeconds = 86400
		}
		if q.Confidence == 0 {
			q.Confidence = 0.95
		}
		if q.ServiceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "serviceId is required for all queries"})
			return
		}
		batchReq.Requests = append(batchReq.Requests, intelligence.ForecastRequest{
			TenantID:        tenantID,
			ServiceID:       q.ServiceID,
			MetricName:      q.MetricName,
			Horizon:         time.Duration(q.HorizonSeconds) * time.Second,
			ConfidenceLevel: q.Confidence,
		})
	}

	resp, err := h.forecastClient.ForecastBatch(c.Request.Context(), batchReq)
	if err != nil {
		h.logger.Error("Batch forecast failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "batch forecast failed"})
		return
	}

	// Transform each forecast response into the frontend's ForecastSeries shape.
	results := make([]gin.H, 0, len(resp.Results))
	for _, r := range resp.Results {
		points := make([]gin.H, 0, len(r.Points))
		for _, p := range r.Points {
			points = append(points, gin.H{
				"timestamp":  p.Timestamp,
				"value":      p.PredictedValue,
				"lowerBound": p.LowerBound,
				"upperBound": p.UpperBound,
			})
		}
		results = append(results, gin.H{
			"metricName": r.MetricName,
			"agentId":    r.ServiceID,
			"tenantId":   tenantID,
			"points":     points,
			"modelInfo": gin.H{
				"bestModel":       r.Model,
				"weights":         gin.H{r.Model: 1.0},
				"accuracy":        gin.H{r.Model: 0.0},
				"lastTrained":     r.GeneratedAt,
				"trainingSamples": 0,
			},
			"overallConfidence": r.ConfidenceScore,
		})
	}

	c.JSON(http.StatusOK, results)
}

func (h *IntelHandlers) handleModelAccuracy(c *gin.Context) {
	metricName := c.Query("metric")
	serviceID := c.Query("service_id")
	tenantID := c.GetString("tenant_id")

	_ = serviceID // reserved for future use
	req := &intelligence.ModelAccuracyRequest{
		TenantID:   tenantID,
		MetricName: metricName,
	}

	resp, err := h.forecastClient.GetModelAccuracy(c.Request.Context(), req)
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			c.JSON(http.StatusOK, gin.H{})
			return
		}
		h.logger.Error("Get model accuracy failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get model accuracy"})
		return
	}

	// Transform backend response into the Record<string, ModelInfo> shape
	// that the frontend expects. Each metric gets its own ModelInfo entry.
	result := gin.H{}
	if resp.Models != nil {
		for metric, modelResp := range resp.Models {
			result[metric] = gin.H{
				"bestModel":       modelResp.Model,
				"weights":         gin.H{modelResp.Model: 1.0},
				"accuracy":        gin.H{modelResp.Model: modelResp.ConfidenceScore},
				"lastTrained":     modelResp.GeneratedAt,
				"trainingSamples": 0,
			}
		}
	}
	if len(result) == 0 {
		c.JSON(http.StatusAccepted, gin.H{
			"status":  "untrained",
			"message": "No forecasting models trained yet. Models require at least 7 days of historical data.",
			"models":  gin.H{},
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

type retrainRequest struct {
	ServiceID  string `json:"serviceId"`
	MetricName string `json:"metricName"`
	Force      bool   `json:"force"`
}

func (h *IntelHandlers) handleRetrainModels(c *gin.Context) {
	var req retrainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")

	resp, err := h.forecastClient.RetrainModels(c.Request.Context(), &intelligence.RetrainRequest{
		TenantID:    tenantID,
		MetricName: req.MetricName,
		Force:      req.Force,
	})
	if err != nil {
		h.logger.Error("Retrain models failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "retrain request failed"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ─── Anomaly Handlers ─────────────────────────────────────────────

type detectAnomaliesRequest struct {
	AgentId     string    `json:"agentId"`
	MetricName  string    `json:"metricName"`
	Values      []float64 `json:"values"`
	Timestamps  []int64   `json:"timestamps"`
	WindowMin   int       `json:"windowMinutes"`
	Sensitivity float64   `json:"sensitivity"`
}

func (h *IntelHandlers) handleDetectAnomalies(c *gin.Context) {
	var req detectAnomaliesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")

	if req.WindowMin == 0 {
		req.WindowMin = 60
	}
	if req.Sensitivity == 0 {
		req.Sensitivity = 0.5
	}
	if req.AgentId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agentId is required"})
		return
	}

	detectReq := &intelligence.AnomalyDetectionRequest{
		TenantID:    tenantID,
		ServiceID:   req.AgentId,
		MetricName:  req.MetricName,
		Sensitivity: req.Sensitivity,
		Values:      req.Values,
		Timestamps:  req.Timestamps,
	}

	resp, err := h.anomalyClient.DetectAnomalies(c.Request.Context(), detectReq)
	if err != nil {
		h.logger.Error("Anomaly detection failed",
			zap.String("agent_id", req.AgentId),
			zap.String("metric", req.MetricName),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "anomaly detection failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"anomalies":    resp.Anomalies,
		"overallScore": 1.0 - float64(len(resp.Anomalies))*0.1,
	})
}

func (h *IntelHandlers) handleAnomalyStatus(c *gin.Context) {
	resp, err := h.anomalyClient.GetDetectionStatus(c.Request.Context())
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			c.JSON(http.StatusOK, gin.H{"models": gin.H{}})
			return
		}
		h.logger.Error("Get detection status failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get detection status"})
		return
	}

	// Transform raw gRPC response to frontend AnomalyDetectionStatus shape (camelCase)
	models := gin.H{}
	for name, status := range resp.Models {
		models[name] = gin.H{
			"name":        status.Name,
			"trained":     status.Trained,
			"accuracy":    status.Accuracy,
			"lastUpdated": status.LastUpdated,
		}
	}
	// Provide defaults if no models reported
	if len(models) == 0 {
		models["isolation_forest"] = gin.H{
			"name": "Isolation Forest", "trained": false, "accuracy": 0.0, "lastUpdated": "",
		}
		models["statistical_zscore"] = gin.H{
			"name": "Statistical Z-Score", "trained": false, "accuracy": 0.0, "lastUpdated": "",
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"models":                models,
		"lastTraining":          resp.LastTraining,
		"anomaliesDetected24h": resp.AnomaliesDetected24h,
		"falsePositiveRate":     resp.FalsePositiveRate,
	})
}

type explainAnomalyRequest struct {
	AgentId    string `json:"agentId"`
	MetricName string `json:"metricName"`
	Timestamp  int64  `json:"timestamp"`
}

func (h *IntelHandlers) handleExplainAnomaly(c *gin.Context) {
	var req explainAnomalyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	tenantID := c.GetString("tenant_id")

	// Build an anomaly ID from the request parameters.
	anomalyID := req.MetricName + ":" + req.AgentId
	if req.Timestamp > 0 {
		anomalyID += ":" + time.Unix(req.Timestamp, 0).Format(time.RFC3339)
	}

	resp, err := h.anomalyClient.ExplainAnomaly(c.Request.Context(), &intelligence.ExplainAnomalyRequest{
		AgentID:    req.AgentId,
		TenantID:   tenantID,
		MetricName: req.MetricName,
		Timestamp:  req.Timestamp,
	})
	if err != nil {
		h.logger.Error("Explain anomaly failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to explain anomaly"})
		return
	}

	// Use response anomaly or build a fallback
	var anomaly intelligence.Anomaly
	if resp.Anomaly != nil {
		anomaly = *resp.Anomaly
	} else {
		anomaly = intelligence.Anomaly{
			ID:         anomalyID,
			MetricName: req.MetricName,
			ServiceID:  req.AgentId,
			Timestamp:  time.Unix(req.Timestamp, 0),
			Severity:   "medium",
		}
	}

	// Use response data for similar incidents and recommendations
	similarIncidents := resp.SimilarIncidents
	if similarIncidents == nil {
		similarIncidents = []string{}
	}
	recommendations := resp.RecommendedActions
	if recommendations == nil {
		recommendations = []string{}
	}

	c.JSON(http.StatusOK, gin.H{
		"anomaly":          anomaly,
		"similarIncidents": similarIncidents,
		"recommendations":  recommendations,
	})
}
