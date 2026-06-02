// Package main implements the Paryty Aggregator service.
//
// The Aggregator consumes raw metrics from Redpanda, aggregates them
// over time windows (avg, p50, p90, p99, min, max), and publishes
// aggregated results to the metrics.aggregated topic.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/processing"
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

	// Default aggregation window from flow control config
	window := time.Duration(cfg.Cluster.FlowControl.DesiredIntervalMs) * time.Millisecond

	logger.Info("Starting Paryty Aggregator",
		zap.Strings("brokers", cfg.Cluster.Stream.Brokers),
		zap.Duration("window", window),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize stream engine
	streamEngine, err := stream.NewStreamEngine(cfg.ToStreamConfig(), logger)
	if err != nil {
		logger.Fatal("Failed to initialize stream engine", zap.Error(err))
	}
	defer streamEngine.Close()
	logger.Info("Stream engine initialized")

	// Create aggregator
	aggregator := processing.NewAggregator(logger)

	// Create producer for publishing aggregated metrics
	producer := streamEngine.Producer()

	// Create consumer handler
	handler := func(ctx context.Context, key string, value []byte) error {
		var batch models.MetricBatch
		if err := json.Unmarshal(value, &batch); err != nil {
			logger.Warn("Failed to unmarshal metric batch", zap.Error(err))
			return nil // Skip malformed messages
		}

		// Aggregate the batch
		aggregated, err := aggregator.Aggregate(ctx, &batch, window)
		if err != nil {
			logger.Error("Aggregation failed",
				zap.String("agent_id", batch.AgentID),
				zap.Error(err),
			)
			return err
		}

		if len(aggregated) == 0 {
			return nil
		}

		// Publish aggregated metrics
		for _, metric := range aggregated {
			if err := producer.Publish(ctx, stream.DefaultTopicMetricsAgg(), batch.AgentID, metric); err != nil {
				logger.Error("Failed to publish aggregated metric",
					zap.String("agent_id", batch.AgentID),
					zap.Error(err),
				)
				return err
			}
		}

		logger.Debug("Published aggregated metrics",
			zap.String("agent_id", batch.AgentID),
			zap.Int("count", len(aggregated)),
		)

		return nil
	}

	// Create consumer
	consumer, err := streamEngine.NewConsumer(
		"paryty-aggregator-group",
		[]string{stream.DefaultTopicMetricsRaw()},
		handler,
	)
	if err != nil {
		logger.Fatal("Failed to create consumer", zap.Error(err))
	}
	defer consumer.Close()

	// Start consuming
	consumer.Start(ctx)
	logger.Info("Aggregator consuming from topic", zap.String("topic", stream.DefaultTopicMetricsRaw()))

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down aggregator...")
	cancel()
	logger.Info("Aggregator stopped")
}
