package handler

import (
	"encoding/json"
	"net/http"

	"github.com/paryty/paryty-v1.0/cluster/internal/intelligence"
	"go.uber.org/zap"
)

// AnomalyHandler handles HTTP requests for anomaly detection operations.
//
// V2.0 Migration: Replaces the Python /api/v1/anomalies/ FastAPI routes.
type AnomalyHandler struct {
	client intelligence.AnomalyClient
	logger *zap.Logger
}

// NewAnomalyHandler creates a new anomaly handler.
func NewAnomalyHandler(client intelligence.AnomalyClient, logger *zap.Logger) *AnomalyHandler {
	return &AnomalyHandler{
		client: client,
		logger: logger,
	}
}

// detectRequest is the JSON request body for anomaly detection.
type detectRequest struct {
	ServiceID   string  `json:"serviceId"`
	MetricName  string  `json:"metricName"`
	WindowMinutes int   `json:"windowMinutes"`
	Sensitivity   float64 `json:"sensitivity"`
}

// HandleDetectAnomalies handles POST /api/v1/anomalies/detect.
func (h *AnomalyHandler) HandleDetectAnomalies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req detectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Sensitivity == 0 {
		req.Sensitivity = 0.5
	}

	detectReq := &intelligence.AnomalyDetectionRequest{
		ServiceID:   req.ServiceID,
		MetricName:  req.MetricName,
		Sensitivity: req.Sensitivity,
	}

	resp, err := h.client.DetectAnomalies(r.Context(), detectReq)
	if err != nil {
		h.logger.Error("Anomaly detection failed",
			zap.String("service_id", req.ServiceID),
			zap.String("metric", req.MetricName),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "anomaly detection failed")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleGetAnomalies handles GET /api/v1/anomalies.
// This returns recent anomalies from the cache or triggers a new detection.
func (h *AnomalyHandler) HandleGetAnomalies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serviceID := r.URL.Query().Get("service_id")
	metricName := r.URL.Query().Get("metric")

	detectReq := &intelligence.AnomalyDetectionRequest{
		ServiceID:   serviceID,
		MetricName:  metricName,
		Sensitivity: 0.5,
	}

	resp, err := h.client.DetectAnomalies(r.Context(), detectReq)
	if err != nil {
		h.logger.Error("Get anomalies failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to get anomalies")
		return
	}

	writeJSON(w, http.StatusOK, resp.Anomalies)
}

// HandleExplainAnomaly handles GET /api/v1/anomalies/:id/explain.
func (h *AnomalyHandler) HandleExplainAnomaly(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	anomalyID := r.PathValue("id")
	if anomalyID == "" {
		writeError(w, http.StatusBadRequest, "anomaly id is required")
		return
	}

	req := &intelligence.ExplainAnomalyRequest{
		AgentID: anomalyID,
	}

	resp, err := h.client.ExplainAnomaly(r.Context(), req)
	if err != nil {
		h.logger.Error("Explain anomaly failed",
			zap.String("anomaly_id", anomalyID),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "failed to explain anomaly")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleGetDetectionStatus handles GET /api/v1/anomalies/status.
func (h *AnomalyHandler) HandleGetDetectionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	resp, err := h.client.GetDetectionStatus(r.Context())
	if err != nil {
		h.logger.Error("Get detection status failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to get detection status")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
