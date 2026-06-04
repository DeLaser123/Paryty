// Package main implements the Paryty Query Service.
//
// The Query Service provides REST, WebSocket, and SSE APIs for accessing
// observability data stored in the Paryty Cluster.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	api "github.com/paryty/paryty-v1.0/cluster/internal/api/query"
	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"go.uber.org/zap"
)

func main() {
	// Determine config path: flag > env > default
	configPath := "configs/cluster/cluster.yaml"
	if v := os.Getenv("PARYTY_CONFIG_PATH"); v != "" {
		configPath = v
	}
	flag.StringVar(&configPath, "config", configPath, "Path to YAML configuration file")
	queryPort := flag.String("port", "8080", "Query service HTTP port")
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

	logger.Info("Starting Paryty Query Service", zap.String("port", *queryPort))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize storage tier orchestrator
	store, err := storage.New(ctx, cfg.ToStorageConfig())
	if err != nil {
		logger.Fatal("Failed to initialize storage", zap.Error(err))
	}
	defer store.Close()
	logger.Info("Storage initialized")

	// Initialize query cache (backed by Dragonfly)
	cacheCfg := api.CacheConfig{
		Addr: cfg.Cluster.Storage.Hot.Addr,
		TTL:  cfg.TTLDuration(),
	}
	queryCache := api.NewQueryCache(cacheCfg, logger)
	defer queryCache.Close()
	logger.Info("Query cache initialized")

	// Create real query service with storage backend
	queryService := api.NewQueryService(store, logger)

	// Create WebSocket handler
	wsHandler := api.NewWebSocketHandler(store, logger)

	// Create SSE handler
	sseHandler := api.NewSSEHandler(store, logger)

	// Set Gin mode
	gin.SetMode(gin.ReleaseMode)

	// Create Gin router
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "paryty-query",
			"cache":   "connected",
		})
	})

	// Register real query routes (rest.go: RegisterRoutes)
	root := router.Group("")
	queryService.RegisterRoutes(root)

	// Wire WebSocket endpoint
	router.GET("/ws", wsHandler.HandleWebSocket)

	// Wire SSE endpoints
	router.GET("/api/v1/timeline/replay", sseHandler.HandleTimeline)
	router.GET("/api/v1/metrics/:agent_id/stream", sseHandler.HandleMetricsStream)

	logger.Info("All query routes registered")

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", *queryPort),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		logger.Info("Query service listening", zap.String("address", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down query service...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Query service stopped")
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
