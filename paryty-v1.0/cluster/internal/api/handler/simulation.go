package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/simulation"
	"go.uber.org/zap"
)

// SimulationHandler handles HTTP requests for simulation operations.
//
// V2.0 Migration: Replaces the Python /api/v1/simulations/ FastAPI routes.
type SimulationHandler struct {
	engine simulation.SimulationEngine
	logger *zap.Logger
}

// NewSimulationHandler creates a new simulation handler.
func NewSimulationHandler(engine simulation.SimulationEngine, logger *zap.Logger) *SimulationHandler {
	return &SimulationHandler{
		engine: engine,
		logger: logger,
	}
}

// runScenarioRequest is the JSON request body for running a scenario.
type runScenarioRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Changes     []simulation.InfraChange    `json:"changes"`
	Traffic     simulation.TrafficPattern   `json:"traffic"`
	DurationMin int                  `json:"durationMinutes"`
	Metrics     []string             `json:"metrics"`
	Tags        []string             `json:"tags"`
	TenantID    string               `json:"tenantId"`
}

// HandleRunScenario handles POST /api/v1/simulations/run.
func (h *SimulationHandler) HandleRunScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req runScenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.TenantID == "" {
		tenantID, err := tenantFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "missing tenant context")
			return
		}
		req.TenantID = tenantID
	}

	scenario := simulation.WhatIfScenario{
		Name:        req.Name,
		Description: req.Description,
		Changes:     req.Changes,
		Traffic:     req.Traffic,
		Duration:    time.Duration(req.DurationMin) * time.Minute,
		Metrics:     req.Metrics,
		Tags:        req.Tags,
		TenantID:    req.TenantID,
	}

	if scenario.Duration == 0 {
		scenario.Duration = 5 * time.Minute
	}

	resultCh, err := h.engine.RunScenario(r.Context(), scenario)
	if err != nil {
		h.logger.Error("Failed to start scenario", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to start scenario: "+err.Error())
		return
	}

	// Return immediately with the scenario ID.
	// The client can poll GET /api/v1/simulations/:id for results.
	select {
	case result := <-resultCh:
		writeJSON(w, http.StatusOK, result)
	default:
		// Scenario is running — return accepted status.
		writeJSON(w, http.StatusAccepted, map[string]string{
			"scenario_id": scenario.ID,
			"status":      "running",
			"message":     "Scenario started. Poll GET /api/v1/simulations/" + scenario.ID + " for results.",
		})
	}
}

// HandleListScenarios handles GET /api/v1/simulations.
func (h *SimulationHandler) HandleListScenarios(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	scenarios, err := h.engine.ListScenarios(r.Context(), tenantID)
	if err != nil {
		h.logger.Error("Failed to list scenarios",
			zap.String("tenant_id", tenantID),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "failed to list scenarios")
		return
	}

	if scenarios == nil {
		scenarios = []simulation.WhatIfScenario{}
	}

	writeJSON(w, http.StatusOK, scenarios)
}

// HandleGetScenarioStatus handles GET /api/v1/simulations/:id.
func (h *SimulationHandler) HandleGetScenarioStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	scenarioID := r.PathValue("id")
	if scenarioID == "" {
		writeError(w, http.StatusBadRequest, "scenario id is required")
		return
	}

	result, err := h.engine.GetScenarioStatus(r.Context(), scenarioID)
	if err != nil {
		h.logger.Warn("Scenario not found",
			zap.String("scenario_id", scenarioID),
			zap.Error(err),
		)
		writeError(w, http.StatusNotFound, "scenario not found")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleCancelScenario handles POST /api/v1/simulations/:id/cancel.
func (h *SimulationHandler) HandleCancelScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	scenarioID := r.PathValue("id")
	if scenarioID == "" {
		writeError(w, http.StatusBadRequest, "scenario id is required")
		return
	}

	if err := h.engine.CancelScenario(r.Context(), scenarioID); err != nil {
		h.logger.Error("Failed to cancel scenario",
			zap.String("scenario_id", scenarioID),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "failed to cancel scenario: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"scenario_id": scenarioID,
		"status":      "canceled",
	})
}
