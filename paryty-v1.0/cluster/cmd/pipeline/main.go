// Package main implements the Paryty Pipeline service — the single binary
// that replaces the previous separate aggregator, correlator, and enricher
// services. It runs all processing stages as in-process goroutines:
//
//	[Consumer goroutines] → [Aggregator] → [Correlator] → [Enricher] → [Producer goroutines]
//
// Configuration is loaded from YAML (or defaults) with environment variable
// overrides. Graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/processing"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/cold"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/hot"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/warm"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"go.uber.org/zap"
)

func main() {
	// Determine config path: flag > env > default.
	configPath := "configs/cluster/cluster.yaml"
	if v := os.Getenv("PARYTY_CONFIG_PATH"); v != "" {
		configPath = v
	}
	flag.StringVar(&configPath, "config", configPath, "Path to YAML configuration file")
	flag.Parse()

	// Initialize logger.
	logLevel := os.Getenv("PARYTY_LOG_LEVEL")
	var logger *zap.Logger
	var err error
	if logLevel == "debug" {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	// Load configuration.
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration",
			zap.String("path", configPath), zap.Error(err))
	}
	if err := cfg.Validate(); err != nil {
		logger.Fatal("Invalid configuration", zap.Error(err))
	}

	// Tenant from config.
	tenant := cfg.Cluster.Tenant
	if tenant == "" {
		tenant = "default"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Info("Starting Paryty Pipeline",
		zap.String("tenant", tenant),
		zap.Strings("brokers", cfg.Cluster.Stream.Brokers),
	)

	// ── Initialize storage (Dragonfly, QuestDB, SeaweedFS) ────────────────
	store, err := storage.New(ctx, cfg.ToStorageConfig())
	if err != nil {
		logger.Fatal("Failed to initialize storage", zap.Error(err))
	}
	defer store.Close()
	logger.Info("Storage initialized")

	// ── Phase 4: Schema Migration ──────────────────────────────────────────
	schemaMgr := warm.NewSchemaManager(store.WarmStore().Pool(), logger)
	if err := schemaMgr.Migrate(ctx); err != nil {
		logger.Fatal("Schema migration failed", zap.Error(err))
	}
	logger.Info("Phase 4 schema migration completed")

	// ── Phase 4: Initialize Hot Store Operations ───────────────────────────
	hotClient := store.HotStore()
	topologyOps := hot.NewTopologyOps(hotClient, logger)
	logger.Info("Phase 4 hot store ops initialized")

	// ── Phase 4: Initialize Cold Store Operations ──────────────────────────
	snapshotConfig := cold.SnapshotConfig{
		Interval:      5 * time.Minute,
		Compression:   true,
		RetentionDays: 7,
	}
	snapshotMgr := cold.NewSnapshotManager(
		snapshotConfig,
		store.ColdStore(),
		hotClient,
		hotClient.RDB(),
		store.WarmStore().Pool(),
		logger,
	)
	eventLog := cold.NewEventLog(store.WarmStore().Pool(), logger)
	logger.Info("Phase 4 cold store ops initialized")

	// ── Phase 4: Initialize Warm Store Operations ──────────────────────────
	queryOptimizer := warm.NewQueryOptimizer(
		store.WarmStore().Pool(),
		hotClient.RDB(),
		slog.Default(),
	)
	retentionMgr := warm.NewRetentionManager(
		store.WarmStore().Pool(),
		nil, // Uses default retention rules
		nil, // Cold archiver disabled for now
		logger,
	)
	logger.Info("Phase 4 warm store ops initialized")

	// ── Phase 4: Attach to Store ───────────────────────────────────────────
	store.SetOptions(storage.StoreOptions{
		TopologyOps:    topologyOps,
		SnapshotMgr:    snapshotMgr,
		EventLog:       eventLog,
		QueryOptimizer: queryOptimizer,
		RetentionMgr:   retentionMgr,
	})
	logger.Info("Phase 4 components wired into store")

	// ── Initialize stream engine (Redpanda) ───────────────────────────────
	streamEngine, err := stream.NewStreamEngine(cfg.ToStreamConfig(), logger)
	if err != nil {
		logger.Fatal("Failed to initialize stream engine", zap.Error(err))
	}
	defer streamEngine.Close()
	logger.Info("Stream engine initialized")

	// ── Create Dragonfly adapter for enricher agent cache ─────────────────
	// Phase B optimization: reuse the existing hot store Redis client instead
	// of creating a second connection pool. This eliminates one full Redis
	// connection pool (default PoolSize=10 connections with internal buffers),
	// saving an estimated 5-15 MB of memory.
	dragonflyAdapter := processing.NewRedisAdapter(hotClient.RDB())

	// ── Create processing stages ──────────────────────────────────────────

	// ServiceMap — process-to-service resolver.
	serviceMap, err := processing.NewServiceMap(nil, logger)
	if err != nil {
		logger.Fatal("Failed to create service map", zap.Error(err))
	}

	// Aggregator — windowed aggregation engine.
	aggCfg := processing.AggregatorConfig{
		WindowSizes:      cfg.Cluster.Processing.Aggregator.WindowSizes,
		GracePeriod:      cfg.Cluster.Processing.Aggregator.GracePeriod,
		SnapshotInterval: cfg.Cluster.Processing.Aggregator.SnapshotInterval,
		TopNSize:         cfg.Cluster.Processing.Aggregator.TopNSize,
	}
	aggregator, err := processing.NewAggregator(aggCfg, dragonflyAdapter, logger)
	if err != nil {
		logger.Fatal("Failed to create aggregator", zap.Error(err))
	}
	logger.Info("Aggregator created",
		zap.Durations("window_sizes", aggCfg.WindowSizes),
		zap.Duration("grace_period", aggCfg.GracePeriod),
	)

	// Correlator — graph-based correlation engine.
	corrCfg := processing.CorrelatorConfig{
		StaleNodeTimeout:      cfg.Cluster.Processing.Correlator.StaleNodeTimeout,
		GraphSnapshotInterval: cfg.Cluster.Processing.Correlator.GraphSnapshotInterval,
		EventBufferSize:       cfg.Cluster.Processing.Correlator.EventBufferSize,
		CorrelationWindow:     cfg.Cluster.Processing.Correlator.CorrelationWindow,
	}
	correlator := processing.NewCorrelator(corrCfg, serviceMap, logger)
	logger.Info("Correlator created",
		zap.Duration("stale_timeout", corrCfg.StaleNodeTimeout),
		zap.Int("event_buffer_size", corrCfg.EventBufferSize),
	)

	// Enricher — label-propagating enricher.
	enricherCfg := processing.EnricherConfig{
		StandardLabelKeys:  cfg.Cluster.Processing.Enricher.StandardLabelKeys,
		MaxLabelsPerMetric: cfg.Cluster.Processing.Enricher.MaxLabelsPerMetric,
		LabelPrefix:        cfg.Cluster.Processing.Enricher.LabelPrefix,
	}
	enricher := processing.NewEnricher(enricherCfg, dragonflyAdapter, logger)
	logger.Info("Enricher created",
		zap.Strings("standard_labels", enricherCfg.StandardLabelKeys),
		zap.Int("max_labels", enricherCfg.MaxLabelsPerMetric),
	)

	// Downsampler — metrics downsampler with QuestDB adapter.
	downsamplerCfg := processing.DownsamplerConfig{
		Rules:       processing.DefaultDownsamplingRules(),
		RunInterval: cfg.Cluster.Processing.Downsampler.RunInterval,
	}
	downsampler := processing.NewDownsampler(
		downsamplerCfg,
		&warmQuestDBAdapter{warm: store.WarmStore()},
		logger,
	)
	logger.Info("Downsampler created",
		zap.Int("rules", len(downsamplerCfg.Rules)),
		zap.Duration("run_interval", downsamplerCfg.RunInterval),
	)

	// ── Create producer and consumer ──────────────────────────────────────
	producer := streamEngine.Producer()

	// Input topics for the pipeline.
	// Includes default tenant topics + twin-scoped topics (via regex) + orphan topic.
	inputTopics := cfg.Cluster.Processing.Pipeline.InputTopics
	if len(inputTopics) == 0 {
		inputTopics = []string{
			stream.DefaultTopicMetricsRaw(),
			stream.DefaultTopicNetworkEvents(),
			stream.DefaultTopicTraces(),
			stream.DefaultTopicEvents(),
			stream.TopicOrphan(stream.DefaultTenant), // orphan topic for unassigned agents
			// Regex pattern for twin-scoped topics: paryty.<tenant>.twins.*
			fmt.Sprintf("paryty\\.%s\\.twins\\..*", stream.DefaultTenant),
		}
	}

	// Consumer group from config.
	consumerGroup := cfg.Cluster.Processing.Pipeline.ConsumerGroup
	if consumerGroup == "" {
		consumerGroup = "paryty-pipeline"
	}

	consumer, err := streamEngine.NewConsumer(consumerGroup, inputTopics, nil)
	if err != nil {
		logger.Fatal("Failed to create consumer", zap.Error(err))
	}

	// ── Phase E: Memory budget configuration ──────────────────────────────
	memConfig := cfg.Cluster.Processing.Memory
	logger.Info("Memory budget configured",
		zap.Int64("max_ram_bytes", memConfig.MaxRAMBytes),
		zap.Int("goroutine_pool_size", memConfig.GoroutinePoolSize),
		zap.Int("window_buffer_capacity", memConfig.WindowBufferCapacity),
		zap.Bool("circuit_breaker_enabled", memConfig.CircuitBreakerEnabled),
	)

	// ── Create and start pipeline ─────────────────────────────────────────
	pipeline := processing.NewPipeline(
		processing.PipelineConfig{
			InputTopics:   inputTopics,
			ConsumerGroup: consumerGroup,
			BatchSize:     cfg.Cluster.Processing.Pipeline.BatchSize,
			BatchTimeout:  cfg.Cluster.Processing.Pipeline.BatchTimeout,
		},
		aggregator,
		correlator,
		enricher,
		downsampler,
		store,
		producer,
		consumer,
		dragonflyAdapter,
		logger,
		tenant,
		memConfig,
	)

	if err := pipeline.Start(ctx); err != nil {
		logger.Fatal("Failed to start pipeline", zap.Error(err))
	}

	// ── Wait for shutdown signal ──────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutdown signal received")
	pipeline.Stop()
	cancel()
	logger.Info("Pipeline exited")
}

// warmQuestDBAdapter adapts the warm.Client to the QuestDBClient interface
// used by the Downsampler. It wraps QueryAggregatedMetrics for querying,
// provides a simple StoreAggregated implementation, and stubs
// DeleteAggregated for future implementation.
type warmQuestDBAdapter struct {
	warm *warm.Client
}

// QueryAggregated queries aggregated metrics from QuestDB.
// Empty agentID or metricName act as wildcards (no filter).
// The empty tenant is intentional: the downsampler is an internal
// maintenance process that compacts data across ALL tenants.
func (a *warmQuestDBAdapter) QueryAggregated(ctx context.Context, agentID string, metricName string, window time.Duration, start, end time.Time) ([]models.AggregatedMetric, error) {
	return a.warm.QueryAggregatedMetrics(ctx, "", agentID, metricName, window, start, end)
}

// StoreAggregated stores aggregated metrics in QuestDB.
// This writes through the existing InsertMetricBatchILP path with an empty
// MetricBatch (only aggregated metrics populated).
func (a *warmQuestDBAdapter) StoreAggregated(ctx context.Context, metrics []models.AggregatedMetric, tenant string) error {
	// QuestDB already stores aggregated metrics via the pipeline's
	// store.StoreMetricBatch path. For the downsampler, we store
	// directly via the aggregated_metrics table INSERT.
	// This is a lightweight operation — each metric is a single row.
	for _, m := range metrics {
		query := `INSERT INTO aggregated_metrics
			(timestamp, agent_id, tenant_id, name, window, agg_type, value)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`
		_, err := a.warm.Pool().Exec(ctx, query,
			m.Timestamp, m.AgentID, tenant, m.Name, int64(m.Window), string(m.AggType), m.Value)
		if err != nil {
			return fmt.Errorf("store downsampled metric (agent=%s, name=%s): %w", m.AgentID, m.Name, err)
		}
	}
	return nil
}

// DeleteAggregated deletes aggregated metrics older than the cutoff.
// This is a placeholder — QuestDB uses SAMPLE BY and TTL for automatic
// data lifecycle management. Manual deletion is deferred to a future
// migration that adds a DELETE FROM aggregated_metrics WHERE ... query.
func (a *warmQuestDBAdapter) DeleteAggregated(_ context.Context, _ time.Duration, _ time.Time) error {
	// TODO: Implement QuestDB DELETE for aggregated metrics older than cutoff.
	// QuestDB handles data lifecycle via partition TTL; manual deletion
	// is only needed when TTL is not configured.
	return nil
}
