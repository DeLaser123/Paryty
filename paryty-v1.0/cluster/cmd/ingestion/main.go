// Package main implements the Paryty Ingestion Service.
//
// The Ingestion Service receives data from Paryty Agents via gRPC
// and publishes it to the Stream Engine (Redpanda).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	api "github.com/paryty/paryty-v1.0/cluster/internal/api/ingestion"
	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // Register Gzip decompressor for incoming agent requests.
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
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

	logger.Info("Starting Paryty Ingestion Service",
		zap.Int("port", cfg.Cluster.Ingestion.Port),
		zap.Strings("brokers", cfg.Cluster.Stream.Brokers),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize storage tier orchestrator
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

	// Initialize topics
	if err := streamEngine.InitializeTopics(ctx, "default"); err != nil {
		logger.Warn("Failed to initialize topics (may already exist)", zap.Error(err))
	}

	// Create ingestion service
	ingestionSvc := api.NewIngestionService(store, streamEngine, slog.Default())
	logger.Info("Ingestion service created")

	// Create rate limiter from configuration
	rateLimiter := api.NewRateLimiter(cfg.Cluster.RateLimit.MaxBatchesPerMinute)
	rateLimiter.StartCleanup(ctx)
	logger.Info("Rate limiter initialized",
		zap.Int("max_batches_per_minute", cfg.Cluster.RateLimit.MaxBatchesPerMinute),
	)

	// Create gRPC server (Gzip decompression registered via blank import above).
	grpcServer := grpc.NewServer()

	// Register health check service with periodic readiness updates
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	healthChecker := api.NewHealthChecker(store, streamEngine, slog.Default())
	healthChecker.StartPeriodicCheck(ctx, 10*time.Second, healthServer)
	logger.Info("Health checker started (periodic readiness every 10s)")

	// Create gRPC adapter and register the IngestionService
	ingestionGRPC := api.NewIngestionGRPCAdapter(ingestionSvc, logger, rateLimiter)
	pb.RegisterIngestionServiceServer(grpcServer, ingestionGRPC)
	logger.Info("Ingestion gRPC service registered")

	// Start listening
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Cluster.Ingestion.Port))
	if err != nil {
		logger.Fatal("Failed to listen", zap.Error(err))
	}

	// Start gRPC server in background
	go func() {
		logger.Info("Ingestion service listening", zap.String("address", lis.Addr().String()))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("Failed to serve", zap.Error(err))
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down ingestion service...")
	grpcServer.GracefulStop()
	logger.Info("Ingestion service stopped")
}
