package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"go.uber.org/zap"
)

// SSEHandler handles Server-Sent Events.
type SSEHandler struct {
	store  *storage.Store
	logger *zap.Logger
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(store *storage.Store, logger *zap.Logger) *SSEHandler {
	return &SSEHandler{
		store:  store,
		logger: logger,
	}
}

// HandleTimeline handles SSE timeline replay.
func (h *SSEHandler) HandleTimeline(c *gin.Context) {
	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	startStr := c.DefaultQuery("start", time.Now().Add(-1*time.Hour).Format(time.RFC3339))
	endStr := c.DefaultQuery("end", time.Now().Format(time.RFC3339))
	speedStr := c.DefaultQuery("speed", "1")

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

	var speed float64
	fmt.Sscanf(speedStr, "%f", &speed)
	if speed <= 0 {
		speed = 1
	}

	ctx := c.Request.Context()
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	// Send events at the specified speed
	current := start
	ticker := time.NewTicker(time.Duration(float64(time.Second) / speed))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if current.After(end) {
				// Send end event
				fmt.Fprintf(c.Writer, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}

			// Send snapshot at current time
			event := h.generateSnapshot(ctx, current)
			data, _ := json.Marshal(event)
			fmt.Fprintf(c.Writer, "event: snapshot\ndata: %s\n\n", data)
			flusher.Flush()

			current = current.Add(time.Second)
		}
	}
}

// HandleMetricsStream handles SSE metrics streaming.
func (h *SSEHandler) HandleMetricsStream(c *gin.Context) {
	agentID := c.Param("agent_id")
	tenant := tenantFromRequest(c)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ctx := c.Request.Context()
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metrics, err := h.store.GetLatestMetrics(ctx, tenant, agentID)
			if err != nil {
				continue
			}
			data, _ := json.Marshal(metrics)
			fmt.Fprintf(c.Writer, "event: metrics\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (h *SSEHandler) generateSnapshot(ctx context.Context, t time.Time) map[string]interface{} {
	// Generate a snapshot of the system state at time t
	// In production, this would query historical data
	return map[string]interface{}{
		"timestamp": t,
		"status":    "ok",
	}
}
