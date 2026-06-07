package timeline

import (
	"context"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/cold"
	"go.uber.org/zap"
)

// ReplayEngine streams historical system state as a sequence of frames,
// enabling time-travel debugging of infrastructure changes.
//
// V2.0 Migration: Replaces the Python ReplayEngine class. The Python version
// used gRPC streaming; V2.0 returns a Go channel for in-process consumption
// or can be wrapped in an SSE handler for HTTP streaming.
type ReplayEngine struct {
	store  SnapshotStore
	logger *zap.Logger
}

// NewReplayEngine creates a new replay engine.
func NewReplayEngine(store SnapshotStore, logger *zap.Logger) *ReplayEngine {
	return &ReplayEngine{
		store:  store,
		logger: logger,
	}
}

// ReplayConfig configures a timeline replay session.
//
// V2.0 Migration: Replaces the Python ReplayConfig dataclass. The ResumeCh
// field replaces the Python threading Event used for pause/resume.
type ReplayConfig struct {
	// StartTime is the beginning of the replay window.
	StartTime time.Time `json:"start_time"`
	// EndTime is the end of the replay window.
	EndTime time.Time `json:"end_time"`
	// Speed is the playback speed multiplier (0.25, 0.5, 1.0, 2.0, 4.0, 8.0, 16.0).
	Speed float64 `json:"speed"`
	// TenantID is the tenant whose data to replay.
	TenantID string `json:"tenant_id"`
	// ResumeCh is a channel that controls pause/resume. Sending true resumes,
	// sending false pauses. Nil means no pause/resume control.
	ResumeCh <-chan bool `json:"-"`
}

// ReplayFrame represents a single point-in-time snapshot during replay.
//
// V2.0 Migration: Replaces the Python ReplayFrame dataclass. The Progress
// field replaces the Python progress_callback mechanism.
type ReplayFrame struct {
	// Timestamp is the time this frame represents.
	Timestamp time.Time `json:"timestamp"`
	// Topology is the topology state at this point.
	Topology *models.Topology `json:"topology"`
	// Alerts are active at this point.
	Alerts []models.Alert `json:"alerts"`
	// Progress is the replay progress (0.0 to 1.0).
	Progress float64 `json:"progress"`
}

// Replay starts streaming historical state as a channel of ReplayFrame values.
// The channel is closed when the replay reaches EndTime or the context is canceled.
// Speed controls the rate of frame delivery: 1.0 means real-time, 2.0 means 2x speed, etc.
func (re *ReplayEngine) Replay(ctx context.Context, config ReplayConfig) (<-chan ReplayFrame, error) {
	if config.Speed <= 0 {
		config.Speed = 1.0
	}
	if config.Speed > 16.0 {
		config.Speed = 16.0
	}
	if config.Speed < 0.25 {
		config.Speed = 0.25
	}
	if config.TenantID == "" {
		config.TenantID = "default"
	}
	if config.StartTime.After(config.EndTime) {
		return nil, fmt.Errorf("start time %v is after end time %v", config.StartTime, config.EndTime)
	}

	totalDuration := config.EndTime.Sub(config.StartTime)
	re.logger.Info("Starting timeline replay",
		zap.String("tenant", config.TenantID),
		zap.Time("start", config.StartTime),
		zap.Time("end", config.EndTime),
		zap.Float64("speed", config.Speed),
		zap.Duration("duration", totalDuration),
	)

	// Load the initial snapshot nearest to StartTime.
	initialSnap, err := re.store.ReconstructState(ctx, config.TenantID, config.StartTime)
	if err != nil {
		return nil, fmt.Errorf("load initial snapshot: %w", err)
	}

	ch := make(chan ReplayFrame, 1)
	go re.streamFrames(ctx, config, initialSnap, totalDuration, ch)

	return ch, nil
}

// streamFrames generates and sends replay frames until the end time or context cancellation.
func (re *ReplayEngine) streamFrames(
	ctx context.Context,
	config ReplayConfig,
	initialSnap *cold.Snapshot,
	totalDuration time.Duration,
	ch chan<- ReplayFrame,
) {
	defer close(ch)

	// Calculate the interval between frames based on speed.
	// At 1x speed, we emit one frame per simulated second.
	frameInterval := time.Duration(float64(time.Second) / config.Speed)
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	currentTime := config.StartTime
	var lastSnap *cold.Snapshot = initialSnap

	// Send the initial frame.
	ch <- ReplayFrame{
		Timestamp: currentTime,
		Topology:  initialSnap.Topology,
		Alerts:    initialSnap.Alerts,
		Progress:  0,
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check for pause/resume.
			if config.ResumeCh != nil {
				select {
				case paused := <-config.ResumeCh:
					if !paused {
						// Paused — block until resumed.
						re.waitForResume(ctx, config.ResumeCh)
					}
				default:
					// Not paused, continue.
				}
			}

			currentTime = currentTime.Add(time.Second)
			if currentTime.After(config.EndTime) {
				// Send final frame.
				progress := 1.0
				ch <- ReplayFrame{
					Timestamp: currentTime,
					Topology:  lastSnap.Topology,
					Alerts:    lastSnap.Alerts,
					Progress:  progress,
				}
				return
			}

			// Attempt to reconstruct state at the current time.
			snap, err := re.store.ReconstructState(ctx, config.TenantID, currentTime)
			if err != nil {
				re.logger.Warn("Failed to reconstruct state, using last known",
					zap.Time("time", currentTime),
					zap.Error(err),
				)
				snap = lastSnap
			}
			lastSnap = snap

			progress := currentTime.Sub(config.StartTime).Seconds() / totalDuration.Seconds()
			ch <- ReplayFrame{
				Timestamp: currentTime,
				Topology:  snap.Topology,
				Alerts:    snap.Alerts,
				Progress:  progress,
			}
		}
	}
}

// waitForResume blocks until a resume signal is received or context is canceled.
func (re *ReplayEngine) waitForResume(ctx context.Context, resumeCh <-chan bool) {
	for {
		select {
		case <-ctx.Done():
			return
		case resumed := <-resumeCh:
			if resumed {
				return
			}
		}
	}
}
