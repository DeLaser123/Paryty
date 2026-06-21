// Package processing implements the data processing pipeline.
// This file implements the Pipeline orchestrator, which runs the aggregator,
// correlator, enricher, and downsampler as in-process stages within a single
// binary. Messages are consumed from Redpanda, routed by topic through the
// processing stages, and results are published and stored.
//
// Pipeline stages:
//
//	[Consumer goroutines] → [Aggregator] → [Correlator] → [Enricher] → [Producer goroutines]
package processing

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/pool"
	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/warm"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// PipelineStore defines the storage operations required by the Pipeline.
// *storage.Store satisfies this interface.
type PipelineStore interface {
	StoreMetricBatch(ctx context.Context, tenant string, batch *models.MetricBatch) error
	SetTopology(ctx context.Context, tenant string, topo *models.Topology) error
	StoreSpan(ctx context.Context, tenant string, span *models.Span) error
	StoreEvents(ctx context.Context, events []models.Event) error
	StoreAggregatedMetric(ctx context.Context, tenant string, m *models.AggregatedMetric) error
	StoreAnomaly(ctx context.Context, tenant string, anomaly *warm.AnomalyRecord) error
}

// PipelineProducer defines the publishing operations required by the Pipeline.
// *stream.Producer satisfies this interface.
type PipelineProducer interface {
	PublishTenant(ctx context.Context, topic, tenantID, agentID string, value interface{}) error
	Close()
}

// PipelineConsumer defines the consumer operations required by the Pipeline.
// *stream.Consumer satisfies this interface.
type PipelineConsumer interface {
	SetRecordHandler(rh stream.RecordHandler)
	Start(ctx context.Context)
	Close()
}

// Compile-time interface satisfaction checks.
var (
	_ PipelineStore    = (*storage.Store)(nil)
	_ PipelineProducer = (*stream.Producer)(nil)
	_ PipelineConsumer = (*stream.Consumer)(nil)
)

// Pipeline mode constants for inter-stage transport.
const (
	// PipelineModeSingle (default) runs all stages in-process with Go channels.
	PipelineModeSingle = "single"

	// PipelineModeDistributed uses Redpanda topics between pipeline stages,
	// enabling independent scaling of each stage. Requires the pipeline.proto
	// serialization protocol.
	PipelineModeDistributed = "distributed"
)

// GetPipelineMode reads PARYTY_PIPELINE_MODE from the environment.
// Returns "single" if unset or invalid. When set to "distributed", the
// pipeline serializes inter-stage messages via Protobuf and Redpanda
// instead of in-process Go channels.
func GetPipelineMode() string {
	mode := os.Getenv("PARYTY_PIPELINE_MODE")
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case PipelineModeDistributed:
		return PipelineModeDistributed
	case "", PipelineModeSingle:
		return PipelineModeSingle
	default:
		return PipelineModeSingle
	}
}

// PipelineConfig holds configuration for the processing pipeline.
type PipelineConfig struct {
	// InputTopics are the Redpanda topics to consume from.
	InputTopics []string

	// ConsumerGroup is the consumer group identifier.
	ConsumerGroup string

	// BatchSize is the maximum number of messages to batch before processing.
	BatchSize int

	// BatchTimeout is the maximum time to wait before processing a partial batch.
	BatchTimeout time.Duration
}

// PipelineHealth reports the current operational status of the pipeline.
type PipelineHealth struct {
	// Status is "healthy", "degraded", or "stopped".
	Status string `json:"status"`

	// Uptime is the duration since the pipeline started.
	Uptime time.Duration `json:"uptime"`

	// MessagesProcessed is the total number of messages processed since start.
	MessagesProcessed int64 `json:"messages_processed"`

	// ErrorsTotal is the total number of processing errors since start.
	ErrorsTotal int64 `json:"errors_total"`
}

// MemoryStats holds current memory monitoring statistics.
// Thread-safe: all fields are accessed via atomic operations or under the
// monitor's lock.
type MemoryStats struct {
	// CurrentAlloc is the current process memory allocation in bytes (from runtime.MemStats).
	CurrentAlloc int64

	// PeakAlloc is the highest allocation observed since monitoring started.
	PeakAlloc int64

	// BreakerTrips is the number of times the circuit breaker has tripped.
	BreakerTrips int64

	// LastCheckTime is when the last memory check was performed.
	LastCheckTime time.Time
}

// MemoryMonitor tracks process memory and manages the circuit breaker.
// When memory exceeds 95% of MaxRAMBytes, the breaker opens and rejects
// new messages until memory drops below 80% (the warning threshold).
//
// Thread-safe. Safe for concurrent use from multiple goroutines.
type MemoryMonitor struct {
	config      config.MemoryConfig
	logger      *zap.Logger
	breakerOpen atomic.Bool
	stats       MemoryStats
	mu          sync.RWMutex // protects stats
}

// NewMemoryMonitor creates a new memory monitor with the given configuration.
// Call Start() to begin periodic memory checks in a background goroutine.
func NewMemoryMonitor(cfg config.MemoryConfig, logger *zap.Logger) *MemoryMonitor {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MemoryMonitor{
		config: cfg,
		logger: logger,
	}
}

// Start begins periodic memory monitoring. It runs until ctx is cancelled.
// This method blocks and should be called in a dedicated goroutine.
func (m *MemoryMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	// Perform an initial check immediately on start.
	m.check()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check()
		}
	}
}

// check reads runtime memory stats and updates the circuit breaker state.
// The breaker uses two thresholds:
//   - 80% of MaxRAMBytes: warning (logged, breaker may close here)
//   - 95% of MaxRAMBytes: breaker opens (rejects new messages)
func (m *MemoryMonitor) check() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	currentAlloc := int64(memStats.Alloc)

	// Update stats under write lock.
	m.mu.Lock()
	m.stats.CurrentAlloc = currentAlloc
	if currentAlloc > m.stats.PeakAlloc {
		m.stats.PeakAlloc = currentAlloc
	}
	m.stats.LastCheckTime = time.Now()
	m.mu.Unlock()

	// Check thresholds against MaxRAMBytes.
	maxRAM := m.config.MaxRAMBytes
	warningThreshold := maxRAM * 80 / 100 // 80%
	breakerThreshold := maxRAM * 95 / 100 // 95%

	if currentAlloc > breakerThreshold {
		if !m.breakerOpen.Load() {
			m.breakerOpen.Store(true)
			m.mu.Lock()
			m.stats.BreakerTrips++
			trips := m.stats.BreakerTrips
			m.mu.Unlock()
			m.logger.Error("memory circuit breaker OPEN — rejecting new messages",
				zap.Int64("current_mb", currentAlloc/1024/1024),
				zap.Int64("limit_mb", maxRAM/1024/1024),
				zap.Int64("breaker_trips", trips),
			)
		}
	} else if currentAlloc < warningThreshold {
		if m.breakerOpen.Load() {
			m.breakerOpen.Store(false)
			m.logger.Info("memory circuit breaker CLOSED — resuming processing",
				zap.Int64("current_mb", currentAlloc/1024/1024),
				zap.Int64("limit_mb", maxRAM/1024/1024),
			)
		}
	}

	// Log warning when approaching the breaker threshold (80-95%).
	if currentAlloc > warningThreshold && currentAlloc <= breakerThreshold {
		m.logger.Warn("memory usage approaching limit",
			zap.Int64("current_mb", currentAlloc/1024/1024),
			zap.Int64("limit_mb", maxRAM/1024/1024),
			zap.Int("percent", int(currentAlloc*100/maxRAM)),
		)
	}
}

// IsBreakerOpen returns true if the memory circuit breaker is open.
// When open, the pipeline should reject new messages to prevent OOM.
func (m *MemoryMonitor) IsBreakerOpen() bool {
	return m.breakerOpen.Load()
}

// GetStats returns a snapshot of the current memory monitoring statistics.
func (m *MemoryMonitor) GetStats() MemoryStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
}

// PipelineMetrics holds the OpenTelemetry instruments for the pipeline's
// golden signals. If nil, metric recording is silently skipped (no-op).
// The zero-value is safe to use: all methods on nil instruments are no-ops.
type PipelineMetrics struct {
	// MessagesTotal is the counter for all messages ingested through the pipeline.
	// Golden signal: paryty.cluster.pipeline.messages.total
	MessagesTotal metric.Int64Counter

	// ErrorsTotal is the counter for processing errors.
	// Golden signal: paryty.cluster.pipeline.errors.total
	ErrorsTotal metric.Int64Counter

	// QueueDepth is a gauge for the current consumer queue depth.
	// Golden signal: paryty.cluster.pipeline.queue.depth
	QueueDepth metric.Int64Gauge

	// DeadLetterSize is a gauge for the dead letter queue size.
	// Golden signal: paryty.pipeline.dead_letter.size
	DeadLetterSize metric.Int64Gauge
}

// Pipeline is the single-binary orchestrator that runs aggregator, correlator,
// enricher, and downsampler as in-process stages. It consumes from Redpanda,
// routes messages by topic through the processing pipeline, and publishes
// results.
//
// Thread-safe. All public methods are safe for concurrent use.
type Pipeline struct {
	config        PipelineConfig
	aggregator    *Aggregator
	correlator    *Correlator
	enricher      *Enricher
	downsampler   *Downsampler
	store         PipelineStore
	producer      PipelineProducer
	consumer      PipelineConsumer
	dragonfly     DragonflyClient
	logger        *zap.Logger
	tenant        string
	memoryMonitor *MemoryMonitor
	pipelineMetrics *PipelineMetrics

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	startTime time.Time
	processed atomic.Int64
	errors    atomic.Int64
}

// NewPipeline creates a new Pipeline orchestrator.
//
// Parameters:
//   - config: pipeline configuration.
//   - aggregator: the windowed aggregation engine.
//   - correlator: the graph-based correlation engine.
//   - enricher: the label-propagating enricher.
//   - downsampler: the metrics downsampler.
//   - store: the unified storage orchestrator (implements PipelineStore).
//   - producer: the Redpanda producer for publishing results (implements PipelineProducer).
//   - consumer: the Redpanda consumer for consuming input messages (implements PipelineConsumer).
//   - dragonfly: DragonflyClient for graph snapshot persistence. May be nil.
//   - logger: structured logger. If nil, a no-op logger is used.
//   - tenant: the tenant identifier for storage and publishing operations.
//   - memConfig: memory budget configuration for the circuit breaker.
//   - metrics: optional OpenTelemetry instruments for golden-signal recording. May be nil.
func NewPipeline(
	config PipelineConfig,
	aggregator *Aggregator,
	correlator *Correlator,
	enricher *Enricher,
	downsampler *Downsampler,
	store PipelineStore,
	producer PipelineProducer,
	consumer PipelineConsumer,
	dragonfly DragonflyClient,
	logger *zap.Logger,
	tenant string,
	memConfig config.MemoryConfig,
	metrics *PipelineMetrics,
) *Pipeline {
	if logger == nil {
		logger = zap.NewNop()
	}
	if tenant == "" {
		tenant = "default"
	}

	return &Pipeline{
		config:          config,
		aggregator:      aggregator,
		correlator:      correlator,
		enricher:        enricher,
		downsampler:     downsampler,
		store:           store,
		producer:        producer,
		consumer:        consumer,
		dragonfly:       dragonfly,
		logger:          logger,
		tenant:          tenant,
		memoryMonitor:   NewMemoryMonitor(memConfig, logger),
		pipelineMetrics: metrics,
	}
}

// Start launches all pipeline goroutines:
//  1. Consumer: subscribe to input topics, route messages via processMessage.
//  2. Memory monitor: periodic memory checks with circuit breaker.
//  3. Window flush: periodic (1s) check for closed windows → aggregate → enrich → publish.
//  4. Downsampler: periodic (1h) downsampling.
//  5. Graph cleanup: periodic (1m) stale node removal.
//  6. Snapshot: periodic (30s windows, 60s graph) Dragonfly snapshots.
//
// Returns an error if the consumer cannot be started.
func (p *Pipeline) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.startTime = time.Now()

	p.logger.Info("Pipeline starting",
		zap.String("tenant", p.tenant),
		zap.Strings("topics", p.config.InputTopics),
		zap.String("consumer_group", p.config.ConsumerGroup),
	)

	// Start the memory monitor in a background goroutine.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.memoryMonitor.Start(p.ctx)
	}()

	// Start the aggregator's window manager.
	p.aggregator.Start(p.ctx)

	// Restore graph state from Dragonfly (non-fatal if no snapshot exists).
	if err := p.correlator.RestoreGraph(p.ctx, p.dragonfly); err != nil {
		p.logger.Warn("Failed to restore graph from Dragonfly", zap.Error(err))
	}

	// Set up topic-aware message routing on the consumer.
	p.consumer.SetRecordHandler(p.handleRecord)

	// Start consuming.
	p.consumer.Start(p.ctx)

	// Goroutine 2: Graph cleanup (1m interval).
	p.wg.Add(1)
	go p.runGraphCleanup()

	// Goroutine 3: Downsampler (at configured interval).
	p.wg.Add(1)
	go p.runDownsampler()

	// Goroutine 4: Snapshot persistence (30s windows, 60s graph).
	p.wg.Add(1)
	go p.runSnapshots()

	p.logger.Info("Pipeline started successfully")
	return nil
}

// Stop performs a graceful shutdown with a 30-second timeout:
//  1. Cancel the context to signal all goroutines.
//  2. Close the consumer (drain in-flight messages).
//  3. Stop the aggregator (final window flush).
//  4. Wait for all background goroutines to finish.
//  5. Perform final snapshots.
//  6. Flush and close the producer.
func (p *Pipeline) Stop() {
	p.logger.Info("Pipeline stopping...")

	// Cancel the pipeline context.
	if p.cancel != nil {
		p.cancel()
	}

	// Close the consumer (drain in-flight handlers).
	if p.consumer != nil {
		p.consumer.Close()
	}

	// Stop the aggregator (final window flush + snapshot).
	p.aggregator.Stop()

	// Wait for background goroutines with 30s timeout.
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("All pipeline goroutines stopped")
	case <-time.After(30 * time.Second):
		p.logger.Warn("Pipeline shutdown timeout exceeded")
	}

	// Flush and close the producer.
	if p.producer != nil {
		p.producer.Close()
	}

	// Log final memory stats.
	memStats := p.memoryMonitor.GetStats()
	p.logger.Info("Pipeline stopped",
		zap.Int64("messages_processed", p.processed.Load()),
		zap.Int64("errors_total", p.errors.Load()),
		zap.Duration("uptime", time.Since(p.startTime)),
		zap.Int64("peak_memory_mb", memStats.PeakAlloc/1024/1024),
		zap.Int64("breaker_trips", memStats.BreakerTrips),
	)
}

// Health returns the current operational status of the pipeline.
func (p *Pipeline) Health() PipelineHealth {
	uptime := time.Since(p.startTime)
	status := "healthy"
	if p.errors.Load() > 0 {
		errorRate := float64(p.errors.Load()) / float64(p.processed.Load()+1)
		if errorRate > 0.1 {
			status = "degraded"
		}
	}
	// If the circuit breaker is open, status is degraded.
	if p.memoryMonitor.IsBreakerOpen() {
		status = "degraded"
	}

	return PipelineHealth{
		Status:            status,
		Uptime:            uptime,
		MessagesProcessed: p.processed.Load(),
		ErrorsTotal:       p.errors.Load(),
	}
}

// MemoryMonitorRef returns the memory monitor for external inspection.
// Used by the query API to expose memory stats.
func (p *Pipeline) MemoryMonitorRef() *MemoryMonitor {
	return p.memoryMonitor
}

// handleRecord is the topic-aware record handler for the consumer.
// It routes messages by topic to the appropriate processing function.
// Golden-signal metrics are recorded via OTel when PipelineMetrics is configured.
func (p *Pipeline) handleRecord(ctx context.Context, record *kgo.Record) error {
	if err := p.processMessage(ctx, record.Topic, string(record.Key), record.Value); err != nil {
		p.errors.Add(1)
		// Record OTel error counter.
		if pm := p.pipelineMetrics; pm != nil {
			pm.ErrorsTotal.Add(ctx, 1)
		}
		return err
	}
	p.processed.Add(1)
	// Record OTel messages counter.
	if pm := p.pipelineMetrics; pm != nil {
		pm.MessagesTotal.Add(ctx, 1)
	}
	return nil
}

// processMessage routes a message to the appropriate processing function
// based on the topic.
//
// Routing:
//   - metrics.raw → parse MetricBatch → processMetricBatch
//   - network.events → parse NetworkEvent → correlator.BufferNetworkEvents
//   - traces → parse Span → enricher.EnrichSpan → store.StoreSpan
//   - events → parse Events → store.StoreEvents
//   - anomalies → parse AnomalyEvent → store.StoreAnomaly
//   - unknown → log warning and skip
//
// Tenant is extracted from the message key (format: "tenant_id:agent_id").
// Falls back to p.tenant if the key does not contain a tenant prefix.
func (p *Pipeline) processMessage(ctx context.Context, topic, key string, value []byte) error {
	// Check circuit breaker before processing.
	// When the breaker is open, reject messages to prevent OOM.
	if p.memoryMonitor.IsBreakerOpen() {
		return fmt.Errorf("memory circuit breaker open, rejecting message")
	}

	// Extract tenant from message key for multi-tenant routing.
	tenant := extractTenantFromKey(key)

	switch {
	case strings.HasSuffix(topic, "metrics.raw"):
		var batch models.MetricBatch
		if err := pool.PooledJSONUnmarshal(value, &batch); err != nil {
			p.logger.Warn("Failed to unmarshal MetricBatch",
				zap.String("topic", topic),
				zap.Error(err),
			)
			return nil // Skip malformed messages — do not send to DLQ.
		}
		return p.processMetricBatch(ctx, tenant, &batch)

	case strings.HasSuffix(topic, "network.events"):
		var event models.NetworkEvent
		if err := pool.PooledJSONUnmarshal(value, &event); err != nil {
			p.logger.Warn("Failed to unmarshal NetworkEvent",
				zap.String("topic", topic),
				zap.Error(err),
			)
			return nil
		}
		p.correlator.BufferNetworkEvents([]models.NetworkEvent{event})
		return nil

	case strings.HasSuffix(topic, "traces"):
		var span models.Span
		if err := pool.PooledJSONUnmarshal(value, &span); err != nil {
			p.logger.Warn("Failed to unmarshal Span",
				zap.String("topic", topic),
				zap.Error(err),
			)
			return nil
		}
		// Enrich span with agent metadata if available.
		agentInfo, err := p.enricher.agentCache.Get(ctx, span.ServiceName)
		if err != nil {
			p.logger.Warn("Failed to get agent info for span enrichment",
				zap.String("service_name", span.ServiceName),
				zap.Error(err),
			)
		}
		if agentInfo != nil {
			enriched := p.enricher.EnrichSpan(ctx, &span, agentInfo)
			span = *enriched
		}
		return p.store.StoreSpan(ctx, tenant, &span)

	case strings.HasSuffix(topic, "events"):
		var events []models.Event
		if err := pool.PooledJSONUnmarshal(value, &events); err != nil {
			// Try single event.
			var single models.Event
			if err2 := pool.PooledJSONUnmarshal(value, &single); err2 != nil {
				p.logger.Warn("Failed to unmarshal Events",
					zap.String("topic", topic),
					zap.Error(err),
				)
				return nil
			}
			events = []models.Event{single}
		}
		return p.store.StoreEvents(ctx, events)

	case strings.HasSuffix(topic, "anomalies"):
		var anomalyEvent parytyv1.AnomalyEvent
		if err := pool.PooledJSONUnmarshal(value, &anomalyEvent); err != nil {
			p.logger.Warn("Failed to unmarshal AnomalyEvent",
				zap.String("topic", topic),
				zap.Error(err),
			)
			return nil
		}

		// Convert proto AnomalyEvent to warm.AnomalyRecord.
		anomalyRecord := &warm.AnomalyRecord{
			ID:            anomalyEvent.Id,
			TenantID:      anomalyEvent.TenantId,
			AgentID:       anomalyEvent.AgentId,
			MetricName:    anomalyEvent.MetricName,
			Severity:      anomalyEvent.Severity,
			Score:         anomalyEvent.Score,
			ExpectedValue: anomalyEvent.ExpectedValue,
			ActualValue:   anomalyEvent.ActualValue,
			Description:   anomalyEvent.Description,
		}
		if anomalyEvent.Timestamp != nil {
			anomalyRecord.Timestamp = anomalyEvent.Timestamp.AsTime()
		}

		return p.store.StoreAnomaly(ctx, tenant, anomalyRecord)

	default:
		p.logger.Warn("Unknown topic, skipping message",
			zap.String("topic", topic),
			zap.String("key", key),
		)
		return nil
	}
}

// processMetricBatch orchestrates the full metric processing pipeline for a
// single MetricBatch:
//  1. Aggregator: process the batch through tumbling windows.
//  2. Correlator: correlate metrics with buffered network events.
//  3. Enricher: enrich the batch with agent metadata and correlation results.
//  4. Store: persist the enriched batch to storage tiers.
//  5. Publish: publish enriched metrics for downstream consumers.
//  6. If aggregated results from closed windows: enrich and publish.
//  7. If topology changes: enrich and publish.
func (p *Pipeline) processMetricBatch(ctx context.Context, tenant string, batch *models.MetricBatch) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("pipeline: context cancelled: %w", err)
	}

	now := batch.Timestamp
	if now.IsZero() {
		now = time.Now()
	}

	// Step 1: Aggregate — process through tumbling windows.
	aggregated, topnChanges, err := p.aggregator.Process(ctx, batch, now)
	if err != nil {
		return fmt.Errorf("pipeline: aggregator process: %w", err)
	}

	// Step 2: Correlate — drain buffered events and correlate.
	networkEvents := p.correlator.DrainBufferedEvents()
	correlationResult, err := p.correlator.Correlate(ctx, batch, networkEvents)
	if err != nil {
		return fmt.Errorf("pipeline: correlator correlate: %w", err)
	}

	// Step 3: Enrich — add agent labels and correlation metadata.
	enrichedBatch, err := p.enricher.EnrichBatch(ctx, batch, correlationResult)
	if err != nil {
		return fmt.Errorf("pipeline: enricher enrich batch: %w", err)
	}

	// Step 4: Store enriched metrics across storage tiers.
	if err := p.store.StoreMetricBatch(ctx, tenant, enrichedBatch); err != nil {
		return fmt.Errorf("pipeline: store metric batch: %w", err)
	}

	// Step 5: Publish enriched metrics for downstream consumers.
	if err := p.producer.PublishTenant(ctx,
		stream.DefaultTopicMetricsEnriched(),
		tenant, batch.AgentID, enrichedBatch); err != nil {
		return fmt.Errorf("pipeline: publish enriched metrics: %w", err)
	}

	// Step 6: Publish Top-N changes.
	for _, topn := range topnChanges {
		if err := p.producer.PublishTenant(ctx,
			stream.DefaultTopicMetricsAgg(),
			tenant, batch.AgentID, topn); err != nil {
			return fmt.Errorf("pipeline: publish topn change: %w", err)
		}
	}

	// Step 7: If aggregated results from closed windows, enrich and publish.
	if len(aggregated) > 0 {
		enrichedAgg, err := p.enricher.EnrichAggregated(ctx, aggregated, batch.AgentID)
		if err != nil {
			return fmt.Errorf("pipeline: enrich aggregated: %w", err)
		}

		for _, metric := range enrichedAgg {
			// Publish to Redpanda for downstream consumers.
			if err := p.producer.PublishTenant(ctx,
				stream.DefaultTopicMetricsAgg(),
				tenant, batch.AgentID, metric); err != nil {
				return fmt.Errorf("pipeline: publish aggregated metric: %w", err)
			}

			// Write to QuestDB aggregated_metrics table.
			if err := p.store.StoreAggregatedMetric(ctx, tenant, &metric); err != nil {
				p.logger.Warn("Failed to store aggregated metric",
					zap.String("agent_id", batch.AgentID),
					zap.Error(err),
				)
				// Non-fatal: continue processing remaining metrics.
			}
		}

		p.logger.Debug("Published aggregated metrics",
			zap.String("agent_id", batch.AgentID),
			zap.Int("count", len(aggregated)),
		)
	}

	// Step 8: If topology changes, enrich, publish, and store.
	if len(correlationResult.TopologyChanges) > 0 {
		enrichedChanges, err := p.enricher.EnrichTopology(ctx, correlationResult.TopologyChanges, batch.AgentID)
		if err != nil {
			return fmt.Errorf("pipeline: enrich topology: %w", err)
		}

		// Publish topology changes for downstream consumers.
		for _, change := range enrichedChanges {
			if err := p.producer.PublishTenant(ctx,
				stream.DefaultTopicTopologyChanges(),
				tenant, batch.AgentID, change); err != nil {
				return fmt.Errorf("pipeline: publish topology change: %w", err)
			}
		}

		// Store topology in hot tier.
		topo := p.buildTopology(correlationResult, enrichedChanges)
		if err := p.store.SetTopology(ctx, tenant, topo); err != nil {
			return fmt.Errorf("pipeline: set topology: %w", err)
		}
	}

	p.logger.Debug("Processed metric batch",
		zap.String("agent_id", batch.AgentID),
		zap.Int("aggregated", len(aggregated)),
		zap.Int("topn_changes", len(topnChanges)),
		zap.Int("dependencies", len(correlationResult.Dependencies)),
		zap.Int("topology_changes", len(correlationResult.TopologyChanges)),
	)

	return nil
}

// buildTopology constructs a models.Topology from the correlation result.
func (p *Pipeline) buildTopology(result *CorrelationResult, changes []GraphChange) *models.Topology {
	nodes := make([]models.TopologyNode, 0, len(result.ProcessServices))
	seen := make(map[string]bool)

	for _, serviceName := range result.ProcessServices {
		if seen[serviceName] || serviceName == "" {
			continue
		}
		seen[serviceName] = true
		nodes = append(nodes, models.TopologyNode{
			ID:       serviceName + ":" + result.AgentID,
			Name:     serviceName,
			Type:     models.NodeTypeService,
			AgentID:  result.AgentID,
			Labels:   make(map[string]string),
			Metadata: make(map[string]string),
			Health:   models.HealthStatusHealthy,
			LastSeen: result.Timestamp,
		})
	}

	edges := make([]models.TopologyEdge, 0, len(result.Dependencies))
	for i, dep := range result.Dependencies {
		edges = append(edges, models.TopologyEdge{
			ID:       fmt.Sprintf("%s:%s:%d", dep.SourceService, dep.TargetService, i),
			SourceID: dep.SourceService,
			TargetID: dep.TargetService,
			Type:     models.EdgeTypeTCP,
			Protocol: dep.Protocol,
			Labels:   make(map[string]string),
			LastSeen: result.Timestamp,
		})
	}

	return &models.Topology{
		Nodes:     nodes,
		Edges:     edges,
		Timestamp: result.Timestamp,
	}
}

// runGraphCleanup periodically removes stale nodes from the dependency graph.
func (p *Pipeline) runGraphCleanup() {
	defer p.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			nodesRemoved, edgesRemoved := p.correlator.CleanupStale(p.ctx)
			if nodesRemoved > 0 || edgesRemoved > 0 {
				p.logger.Info("Graph cleanup completed",
					zap.Int("nodes_removed", nodesRemoved),
					zap.Int("edges_removed", edgesRemoved),
				)
			}
		}
	}
}

// runDownsampler runs the downsampler at its configured interval.
func (p *Pipeline) runDownsampler() {
	defer p.wg.Done()
	p.downsampler.Run(p.ctx)
}

// runSnapshots periodically persists window state and graph state to Dragonfly.
func (p *Pipeline) runSnapshots() {
	defer p.wg.Done()

	windowTicker := time.NewTicker(30 * time.Second)
	defer windowTicker.Stop()

	graphTicker := time.NewTicker(60 * time.Second)
	defer graphTicker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			// Final snapshots on shutdown.
			snapCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := p.aggregator.Snapshot(snapCtx); err != nil {
				p.logger.Error("Final window snapshot failed", zap.Error(err))
			}
			if err := p.correlator.SnapshotGraph(snapCtx, p.dragonfly); err != nil {
				p.logger.Error("Final graph snapshot failed", zap.Error(err))
			}
			cancel()
			return

		case <-windowTicker.C:
			if err := p.aggregator.Snapshot(p.ctx); err != nil {
				p.logger.Error("Window snapshot failed", zap.Error(err))
			}

		case <-graphTicker.C:
			if err := p.correlator.SnapshotGraph(p.ctx, p.dragonfly); err != nil {
				p.logger.Error("Graph snapshot failed", zap.Error(err))
			}
		}
	}
}

// extractTenantFromTopic extracts tenant from topic name.
// "paryty.acme.metrics.raw" → "acme"
func extractTenantFromTopic(topic string) string {
	parts := strings.Split(topic, ".")
	if len(parts) >= 3 && parts[0] == "paryty" {
		return parts[1]
	}
	return "default"
}

// extractTenantFromKey extracts tenant from partition key.
// "tenant_id:agent_id" → "tenant_id"
func extractTenantFromKey(key string) string {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) >= 2 {
		return parts[0]
	}
	return "default"
}
