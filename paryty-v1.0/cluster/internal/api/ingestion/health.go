// Health checks for the ingestion service.
//
// Provides liveness (process alive) and readiness (subsystems reachable)
// checks that drive the gRPC health serving status.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// pinger is satisfied by any subsystem that can verify connectivity.
type pinger interface {
	Ping(ctx context.Context) error
}

// hotStorePinger is satisfied by types that expose a hot-tier sub-pinger.
type hotStorePinger interface {
	HotStore() pinger
	Ping(ctx context.Context) error
}

// healthStatusUpdater is the subset of health.Server used to update serving status.
// Accepting an interface makes the periodic checker testable without a real gRPC server.
type healthStatusUpdater interface {
	SetServingStatus(service string, servingStatus grpc_health_v1.HealthCheckResponse_ServingStatus)
}

// HealthChecker performs liveness and readiness checks against storage
// and stream subsystems. It is safe for concurrent use.
type HealthChecker struct {
	hotStore     pinger
	store        pinger
	streamPinger pinger
	logger       *slog.Logger
}

// NewHealthChecker creates a HealthChecker wired to the given storage
// orchestrator and stream engine.
func NewHealthChecker(store *storage.Store, stream *stream.StreamEngine, logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		hotStore:     store.HotStore(),
		store:        store,
		streamPinger: stream,
		logger:       logger,
	}
}

// LivenessCheck always returns nil. If the process can respond to the
// health RPC, it is alive by definition.
func (hc *HealthChecker) LivenessCheck(_ context.Context) error {
	return nil
}

// ReadinessCheck verifies that every subsystem required for ingestion
// is reachable: hot store (Dragonfly), warm store (QuestDB), and
// stream engine (Redpanda). An aggregated error is returned if any
// check fails.
func (hc *HealthChecker) ReadinessCheck(ctx context.Context) error {
	var errs []error

	if err := hc.hotStore.Ping(ctx); err != nil {
		errs = append(errs, fmt.Errorf("hot store: %w", err))
	}

	if err := hc.store.Ping(ctx); err != nil {
		errs = append(errs, fmt.Errorf("store: %w", err))
	}

	if err := hc.streamPinger.Ping(ctx); err != nil {
		errs = append(errs, fmt.Errorf("stream: %w", err))
	}

	return errors.Join(errs...)
}

// StartPeriodicCheck launches a background goroutine that calls
// ReadinessCheck every interval and updates the gRPC health serving
// status for "paryty.ingestion" to SERVING or NOT_SERVING accordingly.
// The goroutine exits when ctx is cancelled.
func (hc *HealthChecker) StartPeriodicCheck(ctx context.Context, interval time.Duration, healthServer healthStatusUpdater) {
	const serviceName = "paryty.ingestion"

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Run an immediate check so the status is accurate from the start.
		hc.updateStatus(ctx, healthServer, serviceName)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hc.updateStatus(ctx, healthServer, serviceName)
			}
		}
	}()
}

// updateStatus runs a readiness check and sets the serving status accordingly.
func (hc *HealthChecker) updateStatus(ctx context.Context, healthServer healthStatusUpdater, serviceName string) {
	if err := hc.ReadinessCheck(ctx); err != nil {
		healthServer.SetServingStatus(serviceName, grpc_health_v1.HealthCheckResponse_NOT_SERVING)
		hc.logger.Warn("readiness check failed",
			slog.String("service", serviceName),
			slog.Any("error", err),
		)
		return
	}

	healthServer.SetServingStatus(serviceName, grpc_health_v1.HealthCheckResponse_SERVING)
	hc.logger.Debug("readiness check passed",
		slog.String("service", serviceName),
	)
}
