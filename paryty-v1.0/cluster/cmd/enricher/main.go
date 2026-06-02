// Package main implements the Paryty Enricher service.
//
// The Enricher consumes raw data from multiple Redpanda topics,
// adds metadata (service name, version, environment labels) from agent info,
// and publishes enriched data to the storage layer.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/processing"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"go.uber.org/zap"
)

func main() {
	// Determine config path: flag > env > default
	configPath := "configs/cluster/cluster.yaml"
	if v := os.Getenv("PARYTY_CONFIG_PATH"); v != "" {
		configPath = v
	}
	flag.StringVar(&configPath, "config", configPath, "Path to YAML configuration file")
	flag.Parse()

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.String("path", configPath), zap.Error(err))
	}
	if err := cfg.Validate(); err != nil {
		logger.Fatal("Invalid configuration", zap.Error(err))
	}

	// Tenant from config — used for all storage operations.
	tenant := cfg.Cluster.Tenant
	if tenant == "" {
		tenant = "default"
	}

	logger.Info("Starting Paryty Enricher",
		zap.String("tenant", tenant),
		zap.Strings("brokers", cfg.Cluster.Stream.Brokers),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize storage (for reading agent info and storing enriched data)
	store, err := storage.New(ctx, cfg.ToStorageConfig())
	if err != nil {
		logger.Fatal("Failed to initialize storage", zap.Error(err))
	}
	defer store.Close()
	logger.Info("Storage initialized")

	// Initialize stream engine
	streamEngine, err := stream.NewStreamEngine(cfg.ToStreamConfig(), logger)
	if err != nil {
		logger.Fatal("Failed to initialize stream engine", zap.Error(err))
	}
	defer streamEngine.Close()
	logger.Info("Stream engine initialized")

	// Create enricher
	enricher := processing.NewEnricher(logger)

	// Create producer for publishing enriched data
	producer := streamEngine.Producer()

	// Create consumer handler
	handler := func(ctx context.Context, key string, value []byte) error {
		// Try MetricBatch
		var batch models.MetricBatch
		if err := json.Unmarshal(value, &batch); err == nil {
			// Look up agent info for enrichment
			agentInfo, err := store.GetAgentState(ctx, tenant, batch.AgentID)
			if err != nil {
				// Agent info not found, skip enrichment but still forward
				logger.Debug("Agent info not found for enrichment",
					zap.String("tenant", tenant),
					zap.String("agent_id", batch.AgentID),
				)
				agentInfo = &models.AgentInfo{ID: batch.AgentID}
			}

			// Enrich the batch
			enriched, err := enricher.EnrichBatch(ctx, &batch, agentInfo)
			if err != nil {
				logger.Error("Enrichment failed", zap.String("agent_id", batch.AgentID), zap.Error(err))
				return err
			}

			// Store enriched metrics in storage tiers
			if err := store.StoreMetricBatch(ctx, tenant, enriched); err != nil {
				logger.Error("Failed to store enriched metrics",
					zap.String("tenant", tenant),
					zap.String("agent_id", batch.AgentID),
					zap.Error(err),
				)
				return err
			}

			// Re-publish enriched metrics for downstream consumers
			if err := producer.Publish(ctx, stream.DefaultTopicMetricsRaw(), batch.AgentID, enriched); err != nil {
				logger.Error("Failed to publish enriched metrics", zap.Error(err))
				return err
			}

			return nil
		}

		// Try Trace (Span)
		var span models.Span
		if err := json.Unmarshal(value, &span); err == nil {
			// Store span in warm storage
			if err := store.StoreSpan(ctx, &span); err != nil {
				logger.Error("Failed to store span", zap.Error(err))
				return err
			}
			return nil
		}

		// Try Event
		var events []models.Event
		if err := json.Unmarshal(value, &events); err == nil {
			// Store events in cold storage
			if err := store.StoreEvents(ctx, events); err != nil {
				logger.Error("Failed to store events", zap.Error(err))
				return err
			}
			return nil
		}

		logger.Warn("Unknown message format", zap.Int("size", len(value)))
		return nil
	}

	// Create consumer for all raw topics
	consumer, err := streamEngine.NewConsumer(
		"paryty-enricher-group",
		[]string{
			stream.DefaultTopicMetricsRaw(),
			stream.DefaultTopicTraces(),
			stream.DefaultTopicEvents(),
		},
		handler,
	)
	if err != nil {
		logger.Fatal("Failed to create consumer", zap.Error(err))
	}
	defer consumer.Close()

	// Start consuming
	consumer.Start(ctx)
	logger.Info("Enricher consuming from topics",
		zap.String("topic_metrics", stream.DefaultTopicMetricsRaw()),
		zap.String("topic_traces", stream.DefaultTopicTraces()),
		zap.String("topic_events", stream.DefaultTopicEvents()),
	)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down enricher...")
	cancel()
	logger.Info("Enricher stopped")
}
