package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/timeline"
	"go.uber.org/zap"
)

// TimelineHandler handles HTTP requests for timeline operations.
//
// V2.0 Migration: Replaces the Python /api/v1/timeline/ FastAPI routes.
type TimelineHandler struct {
	snapshots *timeline.SnapshotManager
	replay    *timeline.ReplayEngine
	diffCalc  *timeline.DiffCalculator
	export    *timeline.ExportManager
	logger    *zap.Logger
}

// NewTimelineHandler creates a new timeline handler.
func NewTimelineHandler(
	snapshots *timeline.SnapshotManager,
	replay *timeline.ReplayEngine,
	diffCalc *timeline.DiffCalculator,
	export *timeline.ExportManager,
	logger *zap.Logger,
) *TimelineHandler {
	return &TimelineHandler{
		snapshots: snapshots,
		replay:    replay,
		diffCalc:  diffCalc,
		export:    export,
		logger:    logger,
	}
}

// HandleCreateSnapshot handles POST /api/v1/timeline/snapshots.
func (h *TimelineHandler) HandleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		tenant = "default"
	}

	snapshot, err := h.snapshots.CreateSnapshot(r.Context(), tenant)
	if err != nil {
		h.logger.Error("Failed to create snapshot",
			zap.String("tenant", tenant),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "failed to create snapshot")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"snapshot_id": snapshot.ID,
		"timestamp":   snapshot.Timestamp,
		"tenant":      tenant,
	})
}

// HandleListSnapshots handles GET /api/v1/timeline/snapshots.
func (h *TimelineHandler) HandleListSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		tenant = "default"
	}

	// Get the nearest snapshot to now to verify the store is working.
	snap, err := h.snapshots.GetNearestSnapshot(r.Context(), tenant, time.Now())
	if err != nil {
		h.logger.Warn("No snapshots found",
			zap.String("tenant", tenant),
			zap.Error(err),
		)
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	// Return the snapshot summary.
	type snapshotInfo struct {
		ID        string    `json:"id"`
		Timestamp time.Time `json:"timestamp"`
		Tenant    string    `json:"tenant"`
	}

	result := []snapshotInfo{
		{
			ID:        snap.ID,
			Timestamp: snap.Timestamp,
			Tenant:    tenant,
		},
	}

	writeJSON(w, http.StatusOK, result)
}

// HandleDiffSnapshots handles GET /api/v1/timeline/snapshots/diff.
// Query parameters: tenant_id, from (RFC3339), to (RFC3339)
func (h *TimelineHandler) HandleDiffSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		tenant = "default"
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	if fromStr == "" || toStr == "" {
		writeError(w, http.StatusBadRequest, "from and to query parameters are required (RFC3339 format)")
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from time format (use RFC3339)")
		return
	}

	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to time format (use RFC3339)")
		return
	}

	report, err := h.diffCalc.CalculateDiff(r.Context(), tenant, from, to)
	if err != nil {
		h.logger.Error("Failed to calculate diff",
			zap.String("tenant", tenant),
			zap.Time("from", from),
			zap.Time("to", to),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "failed to calculate diff")
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// HandleStartReplay handles GET /api/v1/timeline/replay.
// Streams replay frames as Server-Sent Events (SSE).
// Query parameters: tenant_id, start (RFC3339), end (RFC3339), speed (0.25-16.0)
func (h *TimelineHandler) HandleStartReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		tenant = "default"
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	speedStr := r.URL.Query().Get("speed")

	if startStr == "" || endStr == "" {
		writeError(w, http.StatusBadRequest, "start and end query parameters are required")
		return
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start time format")
		return
	}

	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid end time format")
		return
	}

	speed := 1.0
	if speedStr != "" {
		if _, err := parseFloat(speedStr, &speed); err != nil {
			speed = 1.0
		}
	}

	config := timeline.ReplayConfig{
		StartTime: start,
		EndTime:   end,
		Speed:     speed,
		TenantID:  tenant,
	}

	frameCh, err := h.replay.Replay(r.Context(), config)
	if err != nil {
		h.logger.Error("Failed to start replay", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to start replay")
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	for frame := range frameCh {
		data, err := json.Marshal(frame)
		if err != nil {
			h.logger.Error("Failed to marshal replay frame", zap.Error(err))
			continue
		}

		fmt.Fprintf(w, "event: frame\ndata: %s\n\n", data)
		flusher.Flush()
	}

	// Send end event.
	fmt.Fprintf(w, "event: end\ndata: {}\n\n")
	flusher.Flush()
}

// HandleExportReport handles GET /api/v1/timeline/export.
// Query parameters: tenant_id, format (json, html), from (RFC3339), to (RFC3339)
func (h *TimelineHandler) HandleExportReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tenant := r.URL.Query().Get("tenant_id")
	if tenant == "" {
		tenant = "default"
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	if fromStr == "" || toStr == "" {
		writeError(w, http.StatusBadRequest, "from and to query parameters are required")
		return
	}

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from time format")
		return
	}

	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to time format")
		return
	}

	switch format {
	case "json":
		data, err := h.export.ExportDiffJSON(r.Context(), tenant, from, to)
		if err != nil {
			h.logger.Error("Failed to export JSON", zap.Error(err))
			writeError(w, http.StatusInternalServerError, "export failed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"paryty-diff-%s.json\"", from.Format("20060102")))
		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck

	case "html":
		data, err := h.export.ExportHTML(r.Context(), tenant, from, to)
		if err != nil {
			h.logger.Error("Failed to export HTML", zap.Error(err))
			writeError(w, http.StatusInternalServerError, "export failed")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"paryty-timeline-%s.html\"", from.Format("20060102")))
		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck

	default:
		writeError(w, http.StatusBadRequest, "unsupported format (use json or html)")
	}
}
