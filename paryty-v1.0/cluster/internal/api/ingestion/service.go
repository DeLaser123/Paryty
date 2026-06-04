// Package api provides the gRPC and REST API services for the Paryty cluster.
package api

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
)

// StoreBackend abstracts the storage layer for testability.
type StoreBackend interface {
	SetAgentState(ctx context.Context, tenant string, agent *models.AgentInfo) error
	GetAgentState(ctx context.Context, tenant, agentID string) (*models.AgentInfo, error)
	GetAllAgentStates(ctx context.Context, tenant string) ([]models.AgentInfo, error)
	StoreMetricWarmCold(ctx context.Context, tenant string, batch *models.MetricBatch) error
	SetLatestMetrics(ctx context.Context, tenant, agentID string, batch *models.MetricBatch) error
	StoreNetworkEvents(ctx context.Context, tenant string, batch *pb.NetworkEventBatch) error
}

// StreamBackend abstracts the stream layer for testability.
type StreamBackend interface {
	Producer() *stream.Producer
}

// IngestionService handles agent registration and metric ingestion.
type IngestionService struct {
	store  StoreBackend
	stream StreamBackend
	logger *slog.Logger
	agents sync.Map // map[string]*models.AgentInfo — keys are "tenant:agentID"
}

// NewIngestionService creates a new ingestion service.
func NewIngestionService(store *storage.Store, engine *stream.StreamEngine, logger *slog.Logger) *IngestionService {
	return &IngestionService{
		store:  store,
		stream: engine,
		logger: logger,
	}
}

// agentCacheKey returns the tenant-scoped cache key for an agent.
func agentCacheKey(tenant, agentID string) string {
	return tenant + ":" + agentID
}

// validateBatchDomain validates a domain MetricBatch.
// Rejects nil batches, batches with no metric types, and batches
// where all timestamps are zero.
func validateBatchDomain(batch *models.MetricBatch) error {
	if batch == nil {
		return ErrNilBatch
	}

	hasMetrics := len(batch.CPU) > 0 ||
		len(batch.Memory) > 0 ||
		len(batch.Disk) > 0 ||
		len(batch.Network) > 0 ||
		len(batch.Processes) > 0
	if !hasMetrics {
		return ErrNoMetricTypes
	}

	if batch.Timestamp.IsZero() {
		allZero := true
		for _, m := range batch.CPU {
			if !m.Timestamp.IsZero() {
				allZero = false
				break
			}
		}
		if allZero {
			for _, m := range batch.Memory {
				if !m.Timestamp.IsZero() {
					allZero = false
					break
				}
			}
		}
		if allZero {
			for _, m := range batch.Disk {
				if !m.Timestamp.IsZero() {
					allZero = false
					break
				}
			}
		}
		if allZero {
			for _, m := range batch.Network {
				if !m.Timestamp.IsZero() {
					allZero = false
					break
				}
			}
		}
		if allZero {
			for _, m := range batch.Processes {
				if !m.Timestamp.IsZero() {
					allZero = false
					break
				}
			}
		}
		if allZero {
			return ErrAllTimestampsZero
		}
	}

	return nil
}

// Sentinel errors for batch validation.
var (
	ErrNilBatch          = &BatchValidationError{Message: "batch must not be nil"}
	ErrNoMetricTypes     = &BatchValidationError{Message: "batch must contain at least one metric type"}
	ErrAllTimestampsZero = &BatchValidationError{Message: "batch timestamps must not all be zero"}
)

// BatchValidationError is returned when a metric batch fails validation.
type BatchValidationError struct {
	Message string
}

func (e *BatchValidationError) Error() string {
	return e.Message
}

// RegisterAgent registers a new agent.
func (s *IngestionService) RegisterAgent(ctx context.Context, tenant string, req *models.AgentRegistration) (*models.AgentInfo, error) {
	agent := &models.AgentInfo{
		ID:            req.AgentID,
		Hostname:      req.Hostname,
		IPAddress:     req.IPAddress,
		OS:            req.OS,
		Arch:          req.Arch,
		AgentVersion:  req.AgentVersion,
		Labels:        req.Labels,
		Status:        models.AgentStatusOnline,
		RegisteredAt:  time.Now(),
		LastHeartbeat: time.Now(),
	}

	// Persist to hot store
	if err := s.store.SetAgentState(ctx, tenant, agent); err != nil {
		slog.Error("failed to persist agent state",
			"error", err,
			"tenant", tenant,
			"agent_id", agent.ID,
		)
		return nil, err
	}

	// Cache in memory with tenant-scoped key
	s.agents.Store(agentCacheKey(tenant, agent.ID), agent)

	slog.Info("Agent registered",
		"tenant", tenant,
		"agent_id", agent.ID,
		"hostname", agent.Hostname,
		"ip", agent.IPAddress,
	)

	return agent, nil
}

// Heartbeat handles agent heartbeat.
func (s *IngestionService) Heartbeat(ctx context.Context, tenant, agentID string) error {
	key := agentCacheKey(tenant, agentID)
	if agent, ok := s.agents.Load(key); ok {
		a := agent.(*models.AgentInfo)
		a.LastHeartbeat = time.Now()
		a.Status = models.AgentStatusOnline

		// Persist updated state to hot store
		if err := s.store.SetAgentState(ctx, tenant, a); err != nil {
			slog.Error("failed to persist agent heartbeat",
				"error", err,
				"tenant", tenant,
				"agent_id", agentID,
			)
		}

		return nil
	}
	return ErrAgentNotFound
}

// ErrAgentNotFound is returned when a heartbeat targets an unknown agent.
var ErrAgentNotFound = &AgentError{Message: "agent not found"}

// AgentError is a domain error for agent operations.
type AgentError struct {
	Message string
}

func (e *AgentError) Error() string {
	return e.Message
}

// SendBatch processes a batch of metrics from an agent.
func (s *IngestionService) SendBatch(ctx context.Context, tenant, agentID string, batch *models.MetricBatch) error {
	// Validate
	if err := validateBatchDomain(batch); err != nil {
		return err
	}

	// Update agent heartbeat in cache
	if agent, ok := s.agents.Load(agentCacheKey(tenant, agentID)); ok {
		agent.(*models.AgentInfo).LastHeartbeat = time.Now()
	}

	// Store to warm + cold tiers (non-blocking hot store handled below)
	if err := s.store.StoreMetricWarmCold(ctx, tenant, batch); err != nil {
		return err
	}

	// Publish to tenant-scoped stream for processing
	if err := s.stream.Producer().PublishTenant(ctx, stream.TopicMetricsRaw(tenant), tenant, agentID, batch); err != nil {
		slog.Error("stream publish failed, attempting DLQ",
			"error", err,
			"tenant", tenant,
			"agent_id", agentID,
		)

		// DLQ is best-effort
		_ = s.stream.Producer().PublishTenant(ctx, stream.TopicDLQ(tenant), tenant, agentID, batch)

		return err
	}

	// Hot store write — non-blocking (cache hint only)
	go func() {
		if err := s.store.SetLatestMetrics(ctx, tenant, agentID, batch); err != nil {
			slog.Error("hot store async write failed",
				"error", err,
				"tenant", tenant,
				"agent_id", agentID,
			)
		}
	}()

	slog.Debug("Batch processed",
		"tenant", tenant,
		"agent_id", agentID,
		"cpu_metrics", len(batch.CPU),
		"memory_metrics", len(batch.Memory),
	)

	return nil
}

// StoreNetworkEvents stores a batch of eBPF network events in the warm tier
// and publishes them to the tenant-scoped network events topic on Redpanda.
func (s *IngestionService) StoreNetworkEvents(ctx context.Context, tenant string, batch *pb.NetworkEventBatch) error {
	agentID := batch.GetAgentId()

	// Store to warm tier (QuestDB).
	if err := s.store.StoreNetworkEvents(ctx, tenant, batch); err != nil {
		return err
	}

	// Publish to tenant-scoped network events stream.
	if err := s.stream.Producer().PublishTenant(ctx, stream.TopicNetworkEvents(tenant), tenant, agentID, batch); err != nil {
		slog.Error("network event stream publish failed, attempting DLQ",
			"error", err,
			"tenant", tenant,
			"agent_id", agentID,
		)

		// DLQ is best-effort.
		_ = s.stream.Producer().PublishTenant(ctx, stream.TopicDLQ(tenant), tenant, agentID, batch)

		return err
	}

	slog.Debug("Network events processed",
		"tenant", tenant,
		"agent_id", agentID,
		"event_count", len(batch.GetEvents()),
	)

	return nil
}

// GetAgent returns agent info.
func (s *IngestionService) GetAgent(ctx context.Context, tenant, agentID string) (*models.AgentInfo, error) {
	key := agentCacheKey(tenant, agentID)
	if agent, ok := s.agents.Load(key); ok {
		return agent.(*models.AgentInfo), nil
	}

	// Try hot storage
	agent, err := s.store.GetAgentState(ctx, tenant, agentID)
	if err != nil {
		return nil, err
	}

	s.agents.Store(key, agent)
	return agent, nil
}

// ListAgents returns all registered agents for a tenant.
func (s *IngestionService) ListAgents(ctx context.Context, tenant string) ([]models.AgentInfo, error) {
	// Try hot storage first
	agents, err := s.store.GetAllAgentStates(ctx, tenant)
	if err != nil {
		// Fall back to in-memory cache, filtered by tenant
		var result []models.AgentInfo
		prefix := tenant + ":"
		s.agents.Range(func(key, value interface{}) bool {
			if k, ok := key.(string); ok && len(k) > len(prefix) && k[:len(prefix)] == prefix {
				result = append(result, *value.(*models.AgentInfo))
			}
			return true
		})
		return result, nil
	}
	return agents, nil
}
