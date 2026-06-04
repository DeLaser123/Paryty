// Package storage provides a unified interface to the tiered storage system.
// Data flows: Hot (Dragonfly) → Warm (QuestDB) → Cold (SeaweedFS)
package storage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/cold"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/hot"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/warm"
)

// Tier represents a storage tier.
type Tier string

const (
	TierHot  Tier = "hot"
	TierWarm Tier = "warm"
	TierCold Tier = "cold"
)

// Circuit breaker defaults.
const (
	defaultFailureThreshold = 5
	defaultCooldown         = 30 * time.Second
)

// ---- Circuit Breaker ----

// circuitBreakerState represents the state of a circuit breaker.
type circuitBreakerState int32

const (
	// stateClosed means requests pass through normally.
	stateClosed circuitBreakerState = iota
	// stateOpen means requests are rejected immediately.
	stateOpen
	// stateHalfOpen means a single probe request is allowed through.
	stateHalfOpen
)

// circuitBreaker implements a simple circuit breaker pattern per storage tier.
//
// Closed → Open: after threshold consecutive failures.
// Open → HalfOpen: after cooldown period elapses.
// HalfOpen → Closed: on success.
// HalfOpen → Open: on failure.
type circuitBreaker struct {
	mu          sync.Mutex
	failures    int64
	threshold   int64
	cooldown    time.Duration
	lastFailure time.Time
	state       circuitBreakerState
}

// newCircuitBreaker creates a circuit breaker with the given threshold and cooldown.
func newCircuitBreaker(threshold int64, cooldown time.Duration) *circuitBreaker {
	return &circuitBreaker{
		threshold: threshold,
		cooldown:  cooldown,
		state:     stateClosed,
	}
}

// Allow reports whether a request should be allowed through the circuit breaker.
// Returns true for closed and half-open states; returns false for open state
// unless the cooldown period has elapsed, in which case it transitions to
// half-open and returns true.
func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case stateClosed:
		return true
	case stateOpen:
		if time.Since(cb.lastFailure) >= cb.cooldown {
			cb.state = stateHalfOpen
			return true
		}
		return false
	case stateHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess records a successful operation.
// Resets failure count and transitions to closed state.
func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = stateClosed
}

// RecordFailure records a failed operation.
// Increments failure count and transitions to open state if threshold is reached.
func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()
	if cb.failures >= cb.threshold {
		cb.state = stateOpen
	}
}

// State returns the current circuit breaker state as a human-readable string.
func (cb *circuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case stateClosed:
		return "closed"
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Failures returns the current consecutive failure count.
func (cb *circuitBreaker) Failures() int64 {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}

// ---- Storage Orchestrator ----

// Config contains configuration for the storage orchestrator.
type Config struct {
	Hot  hot.Config  `yaml:"hot" json:"hot"`
	Warm warm.Config `yaml:"warm" json:"warm"`
	Cold cold.Config `yaml:"cold" json:"cold"`
}

// Store is the unified storage interface with per-tier circuit breakers.
type Store struct {
	hot         *hot.Client
	warm        *warm.Client
	cold        *cold.Client
	cfg         Config
	hotBreaker  *circuitBreaker
	warmBreaker *circuitBreaker
}

// New creates a new storage orchestrator with circuit breakers for hot and warm tiers.
func New(ctx context.Context, cfg Config) (*Store, error) {
	hotClient := hot.New(cfg.Hot)

	warmClient, err := warm.New(ctx, cfg.Warm)
	if err != nil {
		return nil, fmt.Errorf("create warm store: %w", err)
	}

	coldClient, err := cold.New(ctx, cfg.Cold)
	if err != nil {
		return nil, fmt.Errorf("create cold store: %w", err)
	}

	return &Store{
		hot:         hotClient,
		warm:        warmClient,
		cold:        coldClient,
		cfg:         cfg,
		hotBreaker:  newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
		warmBreaker: newCircuitBreaker(defaultFailureThreshold, defaultCooldown),
	}, nil
}

// HotStore returns the hot tier client for health checks and direct access.
func (s *Store) HotStore() *hot.Client {
	return s.hot
}

// WarmStore returns the warm tier client for direct access.
func (s *Store) WarmStore() *warm.Client {
	return s.warm
}

// ColdStore returns the cold tier client for direct access.
func (s *Store) ColdStore() *cold.Client {
	return s.cold
}

// Close closes all storage connections.
func (s *Store) Close() error {
	s.hot.Close()
	s.warm.Close()
	return s.cold.Close()
}

// Ping checks all storage connections.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.hot.Ping(ctx); err != nil {
		return fmt.Errorf("hot ping: %w", err)
	}
	if err := s.warm.Ping(ctx); err != nil {
		return fmt.Errorf("warm ping: %w", err)
	}
	return nil
}

// ---- Topology Operations (Hot Tier) ----

// SetTopology stores the current topology in hot storage.
func (s *Store) SetTopology(ctx context.Context, tenant string, topo *models.Topology) error {
	return s.hot.SetTopology(ctx, tenant, topo)
}

// GetTopology retrieves the current topology from hot storage.
func (s *Store) GetTopology(ctx context.Context, tenant string) (*models.Topology, error) {
	return s.hot.GetTopology(ctx, tenant)
}

// ---- Metrics Operations (Multi-Tier) ----

// StoreMetricBatch stores a metric batch across storage tiers.
//
// Hot store write is non-blocking (async via goroutine) for low-latency ingestion.
// Warm store write is non-blocking (async via goroutine).
// Cold store write is synchronous for archival guarantees.
//
// Both hot and warm tiers are protected by circuit breakers.
func (s *Store) StoreMetricBatch(ctx context.Context, tenant string, batch *models.MetricBatch) error {
	// Hot store — non-blocking async write.
	go func() {
		if err := s.hot.SetLatestMetrics(ctx, tenant, batch.AgentID, batch); err != nil {
			slog.Error("hot store async write failed",
				"error", err,
				"tenant", tenant,
				"agent_id", batch.AgentID,
			)
		}
	}()

	// Warm + cold — delegate to StoreMetricWarmCold.
	return s.StoreMetricWarmCold(ctx, tenant, batch)
}

// StoreMetricWarmCold stores a metric batch in the warm and cold tiers only.
// The hot tier is not written; callers that need hot-store writes should
// invoke SetLatestMetrics directly (typically in a background goroutine).
//
// Warm store write is non-blocking (async via goroutine).
// Cold store write is synchronous for archival guarantees.
//
// Both tiers are protected by circuit breakers.
func (s *Store) StoreMetricWarmCold(ctx context.Context, tenant string, batch *models.MetricBatch) error {
	// Warm store — synchronous write with dedicated context to prevent
	// ILP connection issues from concurrent goroutines and context cancellation.
	if s.warmBreaker.Allow() {
		if err := s.warm.InsertMetricBatchILP(batch, tenant); err != nil {
			s.warmBreaker.RecordFailure()
			slog.Error("warm store write failed",
				"error", err,
				"tenant", tenant,
				"agent_id", batch.AgentID,
			)
		} else {
			s.warmBreaker.RecordSuccess()
		}
	} else {
		slog.Warn("warm store circuit breaker open, skipping write",
			"tenant", tenant,
			"agent_id", batch.AgentID,
		)
	}

	// Cold store — best-effort archival. Warm store (QuestDB) is the primary
	// data store; a cold store failure must not reject the batch.
	if err := s.cold.StoreMetricBatch(ctx, batch); err != nil {
		slog.Warn("cold store write failed (non-fatal, warm store already written)",
			"error", err,
			"tenant", tenant,
			"agent_id", batch.AgentID,
		)
	}

	return nil
}

// GetLatestMetrics retrieves the latest metrics from hot storage.
// Falls back to warm storage (QuestDB) if hot tier is unavailable or empty.
func (s *Store) GetLatestMetrics(ctx context.Context, tenant, agentID string) (*models.MetricBatch, error) {
	batch, err := s.hot.GetLatestMetrics(ctx, tenant, agentID)
	if err == nil && batch != nil {
		return batch, nil
	}
	if err != nil {
		slog.Warn("hot tier get latest metrics failed, falling back to warm", "error", err, "agent_id", agentID)
	}
	// Fallback: query QuestDB for recent metrics and assemble a batch.
	return s.getLatestMetricsFromWarm(ctx, tenant, agentID)
}

// SetLatestMetrics writes the latest metrics to hot storage.
func (s *Store) SetLatestMetrics(ctx context.Context, tenant, agentID string, batch *models.MetricBatch) error {
	return s.hot.SetLatestMetrics(ctx, tenant, agentID, batch)
}

// getLatestMetricsFromWarm queries QuestDB for the most recent metrics for an agent.
// This is the fallback path when the hot tier is unavailable.
func (s *Store) getLatestMetricsFromWarm(ctx context.Context, tenant, agentID string) (*models.MetricBatch, error) {
	batch := &models.MetricBatch{
		AgentID: agentID,
	}

	// Query the latest CPU metric.
	cpuQuery := `SELECT timestamp, total_usage_pct, per_core_pct, load_avg_1, load_avg_5, load_avg_15,
		frequency_mhz, context_switches, physical_cores, logical_cores, model_name, vendor_id
		FROM cpu_metrics WHERE agent_id = $1 ORDER BY timestamp DESC LIMIT 1`
	var cpu models.CPUMetrics
	var perCorePctStr string
	err := s.warm.Pool().QueryRow(ctx, cpuQuery, agentID).Scan(
		&cpu.Timestamp, &cpu.TotalUsagePct, &perCorePctStr,
		&cpu.LoadAvg1m, &cpu.LoadAvg5m, &cpu.LoadAvg15m,
		&cpu.FrequencyMHz, &cpu.ContextSwitches,
		&cpu.PhysicalCores, &cpu.LogicalCores, &cpu.ModelName, &cpu.VendorID,
	)
	if err == nil {
		cpu.AgentID = agentID
		batch.CPU = []models.CPUMetrics{cpu}
		batch.Timestamp = cpu.Timestamp
	} else {
		slog.Warn("warm fallback: no cpu metrics", "error", err, "agent_id", agentID)
	}

	// Query the latest memory metric.
	memQuery := `SELECT timestamp, total_bytes, used_bytes, available_bytes, cached_bytes,
		swap_total_bytes, swap_used_bytes,
		pressure_some_avg10, pressure_some_avg60, pressure_some_avg300,
		pressure_full_avg10, pressure_full_avg60, pressure_full_avg300
		FROM memory_metrics WHERE agent_id = $1 ORDER BY timestamp DESC LIMIT 1`
	var mem models.MemoryMetrics
	var pSome10, pSome60, pSome300, pFull10, pFull60, pFull300 float64
	err = s.warm.Pool().QueryRow(ctx, memQuery, agentID).Scan(
		&mem.Timestamp, &mem.TotalBytes, &mem.UsedBytes, &mem.AvailableBytes, &mem.CachedBytes,
		&mem.SwapTotalBytes, &mem.SwapUsedBytes,
		&pSome10, &pSome60, &pSome300, &pFull10, &pFull60, &pFull300,
	)
	if err == nil {
		mem.AgentID = agentID
		mem.Pressure = &models.MemoryPressure{
			Some10: pSome10, Some60: pSome60, Some300: pSome300,
			Full10: pFull10, Full60: pFull60, Full300: pFull300,
		}
		batch.Memory = []models.MemoryMetrics{mem}
	} else {
		slog.Warn("warm fallback: no memory metrics", "error", err, "agent_id", agentID)
	}

	return batch, nil
}

// getAllAgentsFromWarm queries QuestDB for distinct agent IDs and returns AgentInfo stubs.
func (s *Store) getAllAgentsFromWarm(ctx context.Context) ([]models.AgentInfo, error) {
	query := `SELECT DISTINCT agent_id FROM cpu_metrics ORDER BY agent_id`
	rows, err := s.warm.Pool().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query agents from warm: %w", err)
	}
	defer rows.Close()

	var agents []models.AgentInfo
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			continue
		}
		agents = append(agents, models.AgentInfo{
			ID:     agentID,
			Status: models.AgentStatusOnline,
		})
	}
	return agents, rows.Err()
}

// QueryMetrics queries metrics from warm storage within a time range.
func (s *Store) QueryMetrics(ctx context.Context, agentID string, metricName string, start, end time.Time) ([]models.Metric, error) {
	return s.warm.QueryMetrics(ctx, agentID, metricName, start, end)
}

// ---- Alert Operations (Hot Tier) ----

// SetActiveAlerts stores active alerts in hot storage.
func (s *Store) SetActiveAlerts(ctx context.Context, tenant string, alerts []models.Alert) error {
	return s.hot.SetActiveAlerts(ctx, tenant, alerts)
}

// GetActiveAlerts retrieves active alerts from hot storage.
func (s *Store) GetActiveAlerts(ctx context.Context, tenant string) ([]models.Alert, error) {
	return s.hot.GetActiveAlerts(ctx, tenant)
}

// ---- Agent Operations (Hot Tier) ----

// SetAgentState stores agent state in hot storage.
func (s *Store) SetAgentState(ctx context.Context, tenant string, agent *models.AgentInfo) error {
	return s.hot.SetAgentState(ctx, tenant, agent)
}

// GetAgentState retrieves agent state from hot storage.
func (s *Store) GetAgentState(ctx context.Context, tenant, agentID string) (*models.AgentInfo, error) {
	return s.hot.GetAgentState(ctx, tenant, agentID)
}

// GetAllAgentStates retrieves all agent states from hot storage.
// Falls back to warm storage (QuestDB) if hot tier is unavailable or empty.
func (s *Store) GetAllAgentStates(ctx context.Context, tenant string) ([]models.AgentInfo, error) {
	agents, err := s.hot.GetAllAgentStates(ctx, tenant)
	if err == nil && len(agents) > 0 {
		return agents, nil
	}
	if err != nil {
		slog.Warn("hot tier get all agents failed, falling back to warm", "error", err)
	}
	// Fallback: query QuestDB for distinct agent IDs.
	return s.getAllAgentsFromWarm(ctx)
}

// ---- Health Operations (Hot Tier) ----

// SetHealthReport stores a health report in hot storage.
func (s *Store) SetHealthReport(ctx context.Context, tenant string, report *models.HealthReport) error {
	return s.hot.SetHealthReport(ctx, tenant, report)
}

// GetHealthReport retrieves a health report from hot storage.
func (s *Store) GetHealthReport(ctx context.Context, tenant, agentID string) (*models.HealthReport, error) {
	return s.hot.GetHealthReport(ctx, tenant, agentID)
}

// ---- Trace Operations (Multi-Tier) ----

// StoreSpan stores a trace span in warm storage.
func (s *Store) StoreSpan(ctx context.Context, span *models.Span) error {
	return s.warm.InsertSpan(ctx, span)
}

// QueryTraces queries traces from warm storage.
func (s *Store) QueryTraces(ctx context.Context, service string, start, end time.Time, limit int) ([]models.Trace, error) {
	return s.warm.QueryTraces(ctx, service, start, end, limit)
}

// StoreTrace stores a trace in cold storage for archival.
func (s *Store) StoreTrace(ctx context.Context, trace *models.Trace) error {
	return s.cold.StoreTrace(ctx, trace)
}

// ---- Event Operations (Multi-Tier) ----

// StoreEvents stores events in cold storage.
func (s *Store) StoreEvents(ctx context.Context, events []models.Event) error {
	return s.cold.StoreEvents(ctx, events)
}

// ---- Network Event Operations (Multi-Tier) ----

// StoreNetworkEvents stores a batch of eBPF network events in the warm tier.
// Events are written to QuestDB tables (tcp_events, dns_events, http_events)
// via PG INSERT with circuit breaker protection.
func (s *Store) StoreNetworkEvents(ctx context.Context, tenant string, batch *pb.NetworkEventBatch) error {
	if !s.warmBreaker.Allow() {
		slog.Warn("warm store circuit breaker open, skipping network event write",
			"tenant", tenant,
			"agent_id", batch.GetAgentId(),
		)
		return nil
	}

	if err := s.warm.InsertNetworkEvents(batch, tenant); err != nil {
		s.warmBreaker.RecordFailure()
		slog.Error("warm store network event write failed",
			"error", err,
			"tenant", tenant,
			"agent_id", batch.GetAgentId(),
		)
		return err
	}

	s.warmBreaker.RecordSuccess()
	return nil
}

// ---- Aggregation Operations (Warm Tier) ----

// QueryAggregatedMetrics queries aggregated metrics from warm storage.
func (s *Store) QueryAggregatedMetrics(ctx context.Context, agentID string, metricName string, window time.Duration, start, end time.Time) ([]models.AggregatedMetric, error) {
	return s.warm.QueryAggregatedMetrics(ctx, agentID, metricName, window, start, end)
}

// ---- Network Event Query Operations (Warm Tier) ----

// QueryNetworkEvents queries network events from warm storage (QuestDB).
// Delegates to the warm client with circuit breaker protection.
// If eventType is empty, all event types (tcp, dns, http) are queried and merged.
func (s *Store) QueryNetworkEvents(ctx context.Context, agentID, eventType string, start, end time.Time, limit int) ([]map[string]interface{}, error) {
	if !s.warmBreaker.Allow() {
		return nil, fmt.Errorf("warm store circuit breaker open")
	}

	events, err := s.warm.QueryNetworkEvents(ctx, agentID, eventType, start, end, limit)
	if err != nil {
		s.warmBreaker.RecordFailure()
		slog.Error("warm store network event query failed",
			"error", err,
			"agent_id", agentID,
			"event_type", eventType,
		)
		return nil, err
	}

	s.warmBreaker.RecordSuccess()
	return events, nil
}
