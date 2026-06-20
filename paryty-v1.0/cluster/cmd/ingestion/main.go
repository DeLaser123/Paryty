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

	"github.com/jackc/pgx/v5/pgxpool"
	api "github.com/paryty/paryty-v1.0/cluster/internal/api/ingestion"
	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/controlplane"
	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/security"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"github.com/paryty/paryty-v1.0/cluster/internal/twin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip" // Register Gzip decompressor for incoming agent requests.
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// noOpCommandDispatcher is a CommandDispatcher that always returns false,
// causing commands to be queued in the database for heartbeat delivery.
// Real-time command dispatch via active gRPC streams will be implemented
// when the StreamMetrics bidirectional stream tracks connected agents.
type noOpCommandDispatcher struct{}

func (n *noOpCommandDispatcher) DispatchCommand(ctx context.Context, agentID string, commandType int32, payload string) bool {
	// Always return false: commands are queued in agent_commands and
	// delivered on the next heartbeat via CommandManager.DequeueCommands.
	return false
}

// Compile-time interface check.
var _ twin.CommandDispatcher = (*noOpCommandDispatcher)(nil)

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

	// Create rate limiter from configuration
	rateLimiter := api.NewRateLimiter(cfg.Cluster.RateLimit.MaxBatchesPerMinute)
	rateLimiter.StartCleanup(ctx)
	logger.Info("Rate limiter initialized",
		zap.Int("max_batches_per_minute", cfg.Cluster.RateLimit.MaxBatchesPerMinute),
	)

	// Phase 8: Initialize control plane PostgreSQL pool for API key validation
	// and agent registration tracking.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	var apiKeyManager *controlplane.APIKeyManager
	var agentAssigner *twin.AgentAssigner
	var backlogManager *twin.BacklogManager
	var commandManager *twin.CommandManager
	var cpPool *pgxpool.Pool

	if dbURL != "" {
		var err error
		cpPool, err = pgxpool.New(ctx, dbURL)
		if err != nil {
			logger.Fatal("Failed to create control plane pool", zap.Error(err))
		}
		defer cpPool.Close()

		if err := controlplane.EnsureTables(ctx, cpPool); err != nil {
			logger.Fatal("Failed to ensure control plane tables", zap.Error(err))
		}
		if err := controlplane.EnsurePhase8Tables(ctx, cpPool); err != nil {
			logger.Fatal("Failed to ensure phase 8 control plane tables", zap.Error(err))
		}

		apiKeyManager = controlplane.NewAPIKeyManager(cpPool, logger)
		agentAssigner = twin.NewAgentAssigner(cpPool)
		backlogManager = twin.NewBacklogManager(cpPool)
		commandManager = twin.NewCommandManager(cpPool, &noOpCommandDispatcher{})
		logger.Info("Control plane PostgreSQL pool initialized for API key auth + agent tracking + command dispatch")
	} else {
		logger.Warn("DATABASE_URL not set — API key auth and agent tracking disabled")
	}

	// Create ingestion service (after agentAssigner is initialized)
	var routingCache *twin.RoutingCache
	if agentAssigner != nil {
		rdb := store.HotStore().RDB()
		routingCache = twin.NewRoutingCache(rdb, agentAssigner, slog.Default())
	}
	ingestionSvc := api.NewIngestionService(store, streamEngine, slog.Default(), routingCache)
	logger.Info("Ingestion service created")

	// Create gRPC server with auth interceptors (Phase 8).
	var grpcOpts []grpc.ServerOption

	// Check if mTLS is required
	if security.RequireMTLS() {
		logger.Info("mTLS required — loading TLS configuration")
		tlsConfig, err := security.LoadTLSFromEnv()
		if err != nil {
			logger.Fatal("Failed to load TLS configuration", zap.Error(err))
		}
		if tlsConfig == nil {
			logger.Fatal("mTLS required but TLS configuration is nil")
		}
		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(tlsConfig)))
		logger.Info("mTLS enabled for gRPC server")
	} else {
		logger.Warn("mTLS disabled — set PARYTY_TLS_CERT_FILE and PARYTY_TLS_CA_FILE to enable")
	}

	if apiKeyManager != nil {
		// Chain auth interceptor with RLS tenant interceptor
		var unaryInterceptors []grpc.UnaryServerInterceptor
		unaryInterceptors = append(unaryInterceptors, api.AuthInterceptor(apiKeyManager))

		// Add RLS tenant interceptor if database is available
		if cpPool != nil {
			unaryInterceptors = append(unaryInterceptors, api.RLSTenantInterceptor(cpPool))
			logger.Info("RLS tenant interceptor enabled")
		}

		grpcOpts = append(grpcOpts,
			grpc.ChainUnaryInterceptor(unaryInterceptors...),
			grpc.StreamInterceptor(api.StreamAuthInterceptor(apiKeyManager)),
		)
		logger.Info("API key auth interceptors enabled")
	}
	grpcServer := grpc.NewServer(grpcOpts...)

	// Register health check service with periodic readiness updates
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	healthChecker := api.NewHealthChecker(store, streamEngine, slog.Default())
	healthChecker.StartPeriodicCheck(ctx, 10*time.Second, healthServer)
	logger.Info("Health checker started (periodic readiness every 10s)")

	// Create gRPC adapter and register the IngestionService
	ingestionGRPC := api.NewIngestionGRPCAdapter(ingestionSvc, logger, rateLimiter, agentAssigner, backlogManager, commandManager)
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
