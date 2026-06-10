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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	api "github.com/paryty/paryty-v1.0/cluster/internal/api/query"
	"github.com/paryty/paryty-v1.0/cluster/internal/auth"
	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/controlplane"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"github.com/paryty/paryty-v1.0/cluster/internal/security"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/twin"
	"go.uber.org/zap"
)

// planLimitsToMap converts plan.LimitSet to a flat map for JSON serialization.
func planLimitsToMap(limits plan.LimitSet) map[string]interface{} {
	return map[string]interface{}{
		"agentsPerTwin":       limits.AgentsPerTwin,
		"dataRetentionDays":   limits.DataRetentionDays,
		"subUsers":            limits.SubUsers,
		"alertRulesPerTenant": limits.AlertRulesPerTenant,
	}
}

// planQuotasToMap converts plan.QuotaSet to a flat map for JSON serialization.
func planQuotasToMap(quotas plan.QuotaSet) map[string]interface{} {
	return map[string]interface{}{
		"ingestionBytesPerDay":   quotas.IngestionBytesPerDay,
		"queryRequestsPerMinute": quotas.QueryRequestsPerMinute,
	}
}

// buildPermissionsFromRole returns the default permission set for a role.
// Used as a fallback when JWT claims lack a permissions map.
func buildPermissionsFromRole(role string) map[string]bool {
	permissions := map[string]bool{}
	switch role {
	case "admin":
		permissions["tenants:read"] = true
		permissions["tenants:write"] = true
		permissions["users:read"] = true
		permissions["users:write"] = true
		permissions["twins:read"] = true
		permissions["twins:write"] = true
		permissions["api_keys:read"] = true
		permissions["api_keys:write"] = true
	case "operator":
		permissions["twins:read"] = true
		permissions["twins:write"] = true
		permissions["alerts:acknowledge"] = true
	case "viewer":
		permissions["twins:read"] = true
	}
	return permissions
}

// contextString safely extracts a string value from a Gin context.
func contextString(c *gin.Context, key string) string {
	v, ok := c.Get(key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// composeMiddleware chains multiple Gin middleware handlers into a single
// HandlerFunc. Each middleware is executed in order; if any abort the chain
// (via c.Abort()), subsequent middleware and the handler are skipped.
func composeMiddleware(mws ...gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, mw := range mws {
			mw(c)
			if c.IsAborted() {
				return
			}
		}
	}
}

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

	// â”€â”€ Phase 8: Initialize Multi-Tenant Platform â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

	// â”€â”€ Control Plane PostgreSQL Pool â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	// Shared control plane database for API keys, tenants, and twin
	// management. Falls back gracefully if DATABASE_URL is not set.
	var cpPool *pgxpool.Pool
	var planEngine *plan.PlanEngine
	var apiKeyManager *controlplane.APIKeyManager
	var agentAssigner *twin.AgentAssigner
	if cpDSN := os.Getenv("DATABASE_URL"); cpDSN != "" {
		poolCfg, cfgErr := pgxpool.ParseConfig(cpDSN)
		if cfgErr != nil {
			logger.Warn("Failed to parse control plane DSN, API key management disabled",
				zap.Error(cfgErr),
			)
		} else {
			// Initialize app.current_tenant_id GUC for Row-Level Security.
			poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "SELECT set_config('app.current_tenant_id', '', false)")
				return err
			}
			cpPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
			if err != nil {
				logger.Warn("Failed to create control plane pool, API key management disabled",
					zap.Error(err),
				)
				cpPool = nil
			} else {
				defer cpPool.Close()
				// Ensure control plane tables exist.
				if err := controlplane.EnsureTables(ctx, cpPool); err != nil {
					logger.Warn("Failed to ensure control plane tables", zap.Error(err))
				} else if err := controlplane.EnsurePhase8Tables(ctx, cpPool); err != nil {
					logger.Warn("Failed to ensure Phase 8 tables", zap.Error(err))
				} else {
					apiKeyManager = controlplane.NewAPIKeyManager(cpPool, logger)
					agentAssigner = twin.NewAgentAssigner(cpPool)
					logger.Info("Control plane PostgreSQL pool connected â€” API key + agent management enabled")
				}
			}
		}
	} else {
		logger.Warn("DATABASE_URL not set -- API key management disabled")
	}
	// Load plan definitions from YAML configuration.
	plansPath := "configs/cluster/plans.yaml"
	if v := os.Getenv("PARYTY_PLANS_PATH"); v != "" {
		plansPath = v
	}
	plansCfg, err := plan.LoadPlans(plansPath)
	if err != nil {
		logger.Warn("Failed to load plans config, using defaults", zap.Error(err))
		plansCfg = &plan.PlansConfig{
			Plans: map[string]plan.PlanDefinition{
				"basic": {
					DisplayName: "Basic",
					MaxTwins:    2,
					Creatable:   true,
					Features:    map[string]bool{"topology_monitoring": true},
				},
			},
		}
	}
	planEngine = plan.NewPlanEngine(plansCfg, cpPool)
	logger.Info("Plan engine initialized", zap.Int("plans", len(plansCfg.Plans)))

	// Initialize JWT token manager.
	jwtSecret := os.Getenv("PARYTY_JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "paryty-dev-jwt-secret-change-in-production-min-32-bytes!!"
		logger.Warn("Using default JWT secret â€” set PARYTY_JWT_SECRET in production!")
	}
	tokenManager, err := auth.NewTokenManager([]byte(jwtSecret))
	if err != nil {
		logger.Fatal("Failed to initialize token manager", zap.Error(err))
	}
	logger.Info("JWT token manager initialized")

	// Initialize audit logger (async, buffered).
	// cpPool may be nil â†’ audit logger will drop events gracefully.
	auditLogger := security.NewAuditLogger(cpPool, security.AuditLoggerConfig{
		BufferSize: 4096,
	})
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := auditLogger.Shutdown(shutdownCtx); err != nil {
			logger.Warn("Audit logger shutdown timeout", zap.Error(err))
		}
	}()
	logger.Info("Audit logger initialized")

	// Initialize query cache (backed by Dragonfly)
	cacheCfg := api.CacheConfig{
		Addr: cfg.Cluster.Storage.Hot.Addr,
		TTL:  cfg.TTLDuration(),
	}
	queryCache := api.NewQueryCache(cacheCfg, logger)
	defer queryCache.Close()
	logger.Info("Query cache initialized")

	// Create real query service with storage backend
	queryService := api.NewQueryService(store, apiKeyManager, agentAssigner, logger)

	// Wire control plane DB and plan engine into the query service for admin routes.
	queryService.SetDB(cpPool)
	queryService.SetPlanEngine(planEngine)

	// Create WebSocket handler
	wsHandler := api.NewWebSocketHandler(store, logger)

	// Create SSE handler
	sseHandler := api.NewSSEHandler(store, logger, cfg.Cluster.Stream.Brokers)

	// Set Gin mode
	gin.SetMode(gin.ReleaseMode)

	// Create Gin router
	router := gin.New()
	router.Use(gin.Recovery())
	// Phase 8 middleware chain: CORS â†’ RequestID â†’ AuditBegin
	router.Use(security.CORS())
	router.Use(security.RequestID())
	router.Use(security.AuditBegin(auditLogger))

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

	// â”€â”€ Phase 8: Register Auth Routes (PostgreSQL-backed) â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

	// Public auth routes (no JWT required).
	public := router.Group("/api/v1/auth")

	if cpPool != nil {
		authHandler := auth.NewAuthHandler(tokenManager, cpPool, planEngine)
		authAdapter := auth.NewAuthRESTAdapter(authHandler, cpPool, planEngine)

		public.POST("/register", authAdapter.Register)
		public.POST("/login", authAdapter.Login)
		public.POST("/refresh", authAdapter.RefreshToken)
		public.POST("/logout", authAdapter.Logout)
		logger.Info("Auth REST routes wired with PostgreSQL backend")
	} else {
		logger.Warn("No PostgreSQL control plane â€” auth routes not available")
		public.POST("/register", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "PostgreSQL control plane not configured"})
		})
		public.POST("/login", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "PostgreSQL control plane not configured"})
		})
		public.POST("/refresh", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "PostgreSQL control plane not configured"})
		})
		public.POST("/logout", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "PostgreSQL control plane not configured"})
		})
	}

	// Plan listing (public â€” read-only, no auth required).
	// Maps PlanEngine data to the frontend's camelCase Plan[] shape.
	router.GET("/api/v1/plans", func(c *gin.Context) {
		enginePlans := planEngine.ListAllPlans()
		resp := make([]gin.H, 0, len(enginePlans))
		for _, np := range enginePlans {
			resp = append(resp, gin.H{
				"name":        np.Name,
				"displayName": np.Def.DisplayName,
				"maxTwins":    np.Def.MaxTwins,
				"creatable":   np.Def.Creatable,
				"features":    np.Def.Features,
				"limits": gin.H{
					"agentsPerTwin":       np.Def.Limits.AgentsPerTwin,
					"dataRetentionDays":   np.Def.Limits.DataRetentionDays,
					"subUsers":            np.Def.Limits.SubUsers,
					"alertRulesPerTenant": np.Def.Limits.AlertRulesPerTenant,
					"metricsResolution":   np.Def.Limits.MetricsResolution,
				},
				"quotas": gin.H{
					"ingestionBytesPerDay":   np.Def.Quotas.IngestionBytesPerDay,
					"queryRequestsPerMinute": np.Def.Quotas.QueryRequestsPerMinute,
				},
			})
		}
		c.JSON(http.StatusOK, resp)
	})

	// Protected routes with JWT middleware.
	authd := router.Group("/api/v1")
	authd.Use(auth.GinJWTAuth(tokenManager))
	authd.Use(plan.GinPlanInfoInjector(planEngine))
	authd.Use(security.RateLimit(security.DefaultRateLimitConfig()))
	{
		authd.GET("/me", func(c *gin.Context) {
			uid := contextString(c, string(plan.CtxUserID))
			tid := contextString(c, string(plan.CtxTenantID))
			role := contextString(c, string(plan.CtxUserRole))
			pn := contextString(c, string(plan.CtxPlanName))

			// Extract permissions from JWT claims (injected by GinJWTAuth).
			var permissions map[string]bool
			if perms, ok := c.Get(string(plan.CtxPermissions)); ok {
				if p, ok := perms.(map[string]bool); ok {
					permissions = p
				}
			}
			if permissions == nil {
				// Fallback: derive permissions from role.
				permissions = buildPermissionsFromRole(role)
			}

			now := time.Now().UTC()

			// â”€â”€ Database-backed full profile â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
			if cpPool != nil {
				// Query users table for email, name, created_at.
				var email, name string
				var userCreatedAt time.Time
				err := cpPool.QueryRow(c.Request.Context(), `
					SELECT email, name, created_at
					FROM users WHERE id = $1
				`, uid).Scan(&email, &name, &userCreatedAt)
				if err != nil {
					if err == pgx.ErrNoRows {
						c.JSON(http.StatusNotFound, gin.H{"message": "user not found"})
						return
					}
					// Other DB errors: fall back to JWT-only below.
					email = ""
					name = uid
				}
				userCreatedAtStr := userCreatedAt.Format(time.RFC3339)
				if userCreatedAt.IsZero() {
					userCreatedAtStr = now.Format(time.RFC3339)
				}

				// Query tenants table for name and created_at.
				var tenantName string
				var tenantCreatedAt time.Time
				err = cpPool.QueryRow(c.Request.Context(), `
					SELECT name, created_at
					FROM tenants WHERE tenant_id = $1
				`, tid).Scan(&tenantName, &tenantCreatedAt)
				if err != nil {
					tenantName = tid
					tenantCreatedAt = now
				}
				tenantCreatedAtStr := tenantCreatedAt.Format(time.RFC3339)
				if tenantCreatedAt.IsZero() {
					tenantCreatedAtStr = now.Format(time.RFC3339)
				}

				// Query tenant_plans for started_at.
				planStartedAt := now.Format(time.RFC3339)
				if pn != "" {
					var dbStartedAt time.Time
					err := cpPool.QueryRow(c.Request.Context(), `
						SELECT started_at FROM tenant_plans WHERE tenant_id = $1
					`, tid).Scan(&dbStartedAt)
					if err == nil && !dbStartedAt.IsZero() {
						planStartedAt = dbStartedAt.Format(time.RFC3339)
					}
				}

				resp := gin.H{
					"user": gin.H{
						"id":          uid,
						"email":       email,
						"name":        name,
						"role":        role,
						"tenantId":    tid,
						"permissions": permissions,
						"createdAt":   userCreatedAtStr,
					},
					"tenant": gin.H{
						"id":        tid,
						"name":      tenantName,
						"planName":  pn,
						"createdAt": tenantCreatedAtStr,
					},
				}

				if pn != "" {
					if def, found := planEngine.GetPlan(pn); found {
						resp["plan"] = gin.H{
							"planName":  pn,
							"features":  def.Features,
							"limits":    planLimitsToMap(def.Limits),
							"quotas":    planQuotasToMap(def.Quotas),
							"startedAt": planStartedAt,
						}
					}
				}

				c.JSON(http.StatusOK, resp)
				return
			}

			// â”€â”€ No DB: fall back to JWT-only response â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
			resp := gin.H{
				"user": gin.H{
					"id":          uid,
					"email":       "",
					"name":        uid,
					"role":        role,
					"tenantId":    tid,
					"permissions": permissions,
					"createdAt":   now.Format(time.RFC3339),
				},
				"tenant": gin.H{
					"id":        tid,
					"name":      tid,
					"planName":  pn,
					"createdAt": now.Format(time.RFC3339),
				},
			}

			if pn != "" {
				if def, found := planEngine.GetPlan(pn); found {
					resp["plan"] = gin.H{
						"planName":  pn,
						"features":  def.Features,
						"limits":    planLimitsToMap(def.Limits),
						"quotas":    planQuotasToMap(def.Quotas),
						"startedAt": now.Format(time.RFC3339),
					}
				}
			}

			c.JSON(http.StatusOK, resp)
		})
	}

	// â”€â”€ Phase 8: Register Twin Management Routes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	// Wire TwinHandler + TwinRESTAdapter if control plane pool is available.
	var twinCreateMW gin.HandlerFunc
	if cpPool != nil {
		twinHandler := auth.NewTwinHandler(cpPool, nil) // nil dispatcher for now
		twinAdapter := auth.NewTwinRESTAdapter(twinHandler)
		queryService.SetTwinAPI(twinAdapter)
		logger.Info("TwinHandler + TwinRESTAdapter wired for twin management")

		// Compose quota + permission middleware for twin creation.
		twinCreateMW = composeMiddleware(
			plan.RequireTwinQuota(planEngine, twinHandler.TwinManager()),
			auth.RequirePermission("twins:write"),
		)
	} else {
		twinCreateMW = auth.RequirePermission("twins:write")
	}
	queryService.RegisterTwinRoutes(authd, auth.RequirePermission("twins:write"), twinCreateMW)
	queryService.RegisterApiKeyRoutes(authd, auth.RequirePermission("api_keys:write"))
	queryService.RegisterAdminRoutes(authd, auth.RequirePermission("users:write"), auth.RequirePermission("users:read"), auth.RequirePermission("tenants:write"))

	logger.Info("Phase 8 auth routes registered")

	// Wire WebSocket endpoint
	router.GET("/ws", wsHandler.HandleWebSocket)

	// Wire SSE endpoints
	router.GET("/api/v1/timeline/replay", sseHandler.HandleTimeline)
	router.GET("/api/v1/metrics/:agent_id/stream", sseHandler.HandleMetricsStream)
	router.GET("/api/v1/events/stream", sseHandler.HandleEventStream)

	// Wire intelligence API handlers (Phase 6)
	intelCfg := api.NewIntelConfigFromEnv()
	intelHandlers, err := api.NewIntelHandlers(intelCfg, logger)
	if err != nil {
		logger.Warn("Failed to initialize intelligence handlers (intelligence service may not be running)",
			zap.Error(err),
			zap.String("address", intelCfg.Address),
		)
		// Non-fatal: the rest of the query service works without intelligence.
	} else {
		intelHandlers.RegisterRoutes(root)
		defer intelHandlers.Close()
		logger.Info("Intelligence API routes registered",
			zap.String("address", intelCfg.Address),
		)
	}

	// â”€â”€ AuditEnd middleware: finalizes audit events started by AuditBegin â”€â”€
	// Registered AFTER all route handlers. Gin post-handler middleware runs in
	// reverse registration order: AuditBegin captures start â†’ handler runs â†’
	// AuditEnd finalizes â†’ AuditBegin (skips if already finalized).
	router.Use(security.AuditEnd(auditLogger))

	logger.Info("All query routes registered")

	// Start Redpanda event consumers for WebSocket fanout (all active tenants)
	tenants := []string{"default", "gai-tech", "tenant1", "tenant-b"}
	for _, t := range tenants {
		tenant := t // capture loop variable
		go func() {
			if err := wsHandler.StartEventConsumer(ctx, cfg.Cluster.Stream.Brokers, tenant); err != nil {
				logger.Warn("Event consumer stopped", zap.String("tenant", tenant), zap.Error(err))
			}
		}()
	}
	logger.Info("WebSocket event consumers started", zap.Int("tenants", len(tenants)))

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
