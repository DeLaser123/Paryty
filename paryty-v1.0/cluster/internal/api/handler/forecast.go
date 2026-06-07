// Package handler implements HTTP API handlers for the Paryty V2.0 features.
// Handlers use the standard net/http package and can be mounted on any router
// (Gin, gorilla/mux, chi, etc.) via adapter functions.
//
// V2.0 Migration: Replaces the Python FastAPI route handlers with Go
// net/http handlers. The request/response DTOs match the TypeScript frontend
// expectations for camelCase JSON fields.
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/intelligence"
	"go.uber.org/zap"
)

// ForecastHandler handles HTTP requests for forecasting operations.
//
// V2.0 Migration: Replaces the Python /api/v1/forecasts/ FastAPI routes.
type ForecastHandler struct {
	client intelligence.ForecastClient
	logger *zap.Logger
}

// NewForecastHandler creates a new forecast handler.
func NewForecastHandler(client intelligence.ForecastClient, logger *zap.Logger) *ForecastHandler {
	return &ForecastHandler{
		client: client,
		logger: logger,
	}
}

// forecastRequest is the JSON request body for single metric forecasts.
type forecastRequest struct {
	ServiceID       string  `json:"serviceId"`
	MetricName      string  `json:"metricName"`
	HorizonMinutes  int     `json:"horizonMinutes"`
	ConfidenceLevel float64 `json:"confidenceLevel"`
}

// forecastBatchRequest is the JSON request body for batch forecasts.
type forecastBatchRequest struct {
	Forecasts []forecastRequest `json:"forecasts"`
}

// HandleGetForecast handles GET /api/v1/forecasts/:metric.
// Query parameters: service_id, horizon (minutes), confidence
func (h *ForecastHandler) HandleGetForecast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	metric := r.PathValue("metric")
	if metric == "" {
		writeError(w, http.StatusBadRequest, "metric parameter is required")
		return
	}

	serviceID := r.URL.Query().Get("service_id")
	if serviceID == "" {
		serviceID = "default"
	}

	horizonMinutes := 60
	if h := r.URL.Query().Get("horizon"); h != "" {
		if parsed, err := time.ParseDuration(h + "m"); err == nil {
			horizonMinutes = int(parsed.Minutes())
		}
	}

	confidence := 0.95
	if c := r.URL.Query().Get("confidence"); c != "" {
		// Parse confidence from query parameter.
		var parsed float64
		if _, err := parseFloat(c, &parsed); err == nil && parsed > 0 && parsed <= 1 {
			confidence = parsed
		}
	}

	req := &intelligence.ForecastRequest{
		ServiceID:       serviceID,
		MetricName:      metric,
		Horizon:         time.Duration(horizonMinutes) * time.Minute,
		ConfidenceLevel: confidence,
	}

	resp, err := h.client.ForecastMetric(r.Context(), req)
	if err != nil {
		h.logger.Error("Forecast failed",
			zap.String("metric", metric),
			zap.String("service_id", serviceID),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "forecast generation failed")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleGetForecastBatch handles POST /api/v1/forecasts/batch.
func (h *ForecastHandler) HandleGetForecastBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req forecastBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if len(req.Forecasts) == 0 {
		writeError(w, http.StatusBadRequest, "forecasts array must not be empty")
		return
	}

	batchReq := &intelligence.ForecastBatchRequest{}
	for _, f := range req.Forecasts {
		batchReq.Requests = append(batchReq.Requests, intelligence.ForecastRequest{
			ServiceID:       f.ServiceID,
			MetricName:      f.MetricName,
			Horizon:         time.Duration(f.HorizonMinutes) * time.Minute,
			ConfidenceLevel: f.ConfidenceLevel,
		})
	}

	resp, err := h.client.ForecastBatch(r.Context(), batchReq)
	if err != nil {
		h.logger.Error("Batch forecast failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "batch forecast failed")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleGetModelAccuracy handles GET /api/v1/forecasts/accuracy.
func (h *ForecastHandler) HandleGetModelAccuracy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serviceID := r.URL.Query().Get("service_id")
	metricName := r.URL.Query().Get("metric")

	req := &intelligence.ModelAccuracyRequest{
		ServiceID:  serviceID,
		MetricName: metricName,
	}

	resp, err := h.client.GetModelAccuracy(r.Context(), req)
	if err != nil {
		h.logger.Error("Get model accuracy failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to get model accuracy")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleRetrainModels handles POST /api/v1/forecasts/retrain.
func (h *ForecastHandler) HandleRetrainModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		ServiceID  string `json:"serviceId"`
		MetricName string `json:"metricName"`
		Force      bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	req := &intelligence.RetrainRequest{
		ServiceID:  body.ServiceID,
		MetricName: body.MetricName,
		Force:      body.Force,
	}

	resp, err := h.client.RetrainModels(r.Context(), req)
	if err != nil {
		h.logger.Error("Retrain models failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "retrain request failed")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
