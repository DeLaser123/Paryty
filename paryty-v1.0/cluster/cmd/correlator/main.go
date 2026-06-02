// Package main implements the Paryty Correlator service.
//
// The Correlator consumes aggregated metrics and network events from Redpanda,
// builds dependency graphs between services, detects topology changes,
// and publishes topology updates to the topology.changes topic.
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

	logger.Info("Starting Paryty Correlator",
		zap.Strings("brokers", cfg.Cluster.Stream.Brokers),
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

	// Create correlator
	correlator := processing.NewCorrelator(logger)

	// Create producer for publishing topology changes
	producer := streamEngine.Producer()

	// Network events buffer for correlation
	// In production this would be a sliding window cache
	networkEventsBuf := make([]models.NetworkEvent, 0, 1000)

	// Create consumer handler for aggregated metrics + network events
	handler := func(ctx context.Context, key string, value []byte) error {
		// Try to unmarshal as MetricBatch first
		var batch models.MetricBatch
		if err := json.Unmarshal(value, &batch); err == nil {
			// Correlate with buffered network events
			result, err := correlator.Correlate(ctx, &batch, networkEventsBuf)
			if err != nil {
				logger.Error("Correlation failed",
					zap.String("agent_id", batch.AgentID),
					zap.Error(err),
				)
				return err
			}

			// Publish topology changes
			for _, change := range result.TopologyChanges {
				if err := producer.Publish(ctx, stream.DefaultTopicTopologyChanges(), batch.AgentID, change); err != nil {
					logger.Error("Failed to publish topology change", zap.Error(err))
					return err
				}
			}

			if len(result.TopologyChanges) > 0 {
				logger.Info("Published topology changes",
					zap.String("agent_id", batch.AgentID),
					zap.Int("changes", len(result.TopologyChanges)),
					zap.Int("dependencies", len(result.Dependencies)),
				)
			}

			return nil
		}

		// Try to unmarshal as NetworkEvent
		var netEvent models.NetworkEvent
		if err := json.Unmarshal(value, &netEvent); err == nil {
			// Buffer network events for correlation
			networkEventsBuf = append(networkEventsBuf, netEvent)
			// Keep buffer bounded
			if len(networkEventsBuf) > 1000 {
				networkEventsBuf = networkEventsBuf[len(networkEventsBuf)-500:]
			}
			return nil
		}

		logger.Warn("Unknown message format", zap.Int("size", len(value)))
		return nil
	}

	// Create consumer for both topics
	consumer, err := streamEngine.NewConsumer(
		"paryty-correlator-group",
		[]string{stream.DefaultTopicMetricsAgg(), stream.DefaultTopicNetworkEvents()},
		handler,
	)
	if err != nil {
		logger.Fatal("Failed to create consumer", zap.Error(err))
	}
	defer consumer.Close()

	// Start consuming
	consumer.Start(ctx)
	logger.Info("Correlator consuming from topics",
		zap.String("topic_metrics", stream.DefaultTopicMetricsAgg()),
		zap.String("topic_network", stream.DefaultTopicNetworkEvents()),
	)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down correlator...")
	cancel()
	logger.Info("Correlator stopped")
}
