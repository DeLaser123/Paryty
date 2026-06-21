// Package main implements the Paryty Query Service.
//
// The Query Service provides REST, WebSocket, and SSE APIs for accessing
// observability data stored in the Paryty Cluster.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	api "github.com/paryty/paryty-v1.0/cluster/internal/api/query"
	"github.com/paryty/paryty-v1.0/cluster/internal/auth"
	"github.com/paryty/paryty-v1.0/cluster/internal/config"
	"github.com/paryty/paryty-v1.0/cluster/internal/controlplane"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"github.com/paryty/paryty-v1.0/cluster/internal/security"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"github.com/paryty/paryty-v1.0/cluster/internal/twin"
	"go.uber.org/zap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
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
// Delegates to the canonical security.RolePermissions map to avoid drift.
func buildPermissionsFromRole(role string) map[string]bool {
	if perms, ok := security.RolePermissions[role]; ok {
		out := make(map[string]bool, len(perms))
		for k, v := range perms {
			out[k] = v
		}
		return out
	}
	return map[string]bool{}
}

// loadEventConsumerTenants returns the tenant IDs whose Redpanda event
// topics should be consumed for WebSocket fanout. Active tenants come from
// the control plane when PostgreSQL is configured; otherwise the
// PARYTY_WS_TENANTS env var (comma-separated) is used, defaulting to
// "default" for single-tenant development setups.
func loadEventConsumerTenants(ctx context.Context, cpPool *pgxpool.Pool, logger *zap.Logger) []string {
	if cpPool != nil {
		rows, err := cpPool.Query(ctx, `SELECT tenant_id FROM tenants WHERE status = 'active'`)
		if err == nil {
			defer rows.Close()
			var tenants []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil && id != "" {
					tenants = append(tenants, id)
				}
			}
			if rows.Err() == nil && len(tenants) > 0 {
				return tenants
			}
		} else {
			logger.Warn("Failed to enumerate tenants from control plane; falling back to PARYTY_WS_TENANTS", zap.Error(err))
		}
	}

	if v := os.Getenv("PARYTY_WS_TENANTS"); v != "" {
		var tenants []string
		for _, t := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				tenants = append(tenants, trimmed)
			}
		}
		if len(tenants) > 0 {
			return tenants
		}
	}

	return []string{"default"}
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

// initOTelMeter initializes the OpenTelemetry meter with a Prometheus exporter
// for Paryty self-monitoring (dogfooding principle). Returns the meter and a
// shutdown function. On failure, returns a no-op meter from the default provider.
func initOTelMeter(logger *zap.Logger) (metric.Meter, func()) {
	exporter, err := prometheus.New()
	if err != nil {
		logger.Warn("Failed to create OTel prometheus exporter", zap.Error(err))
		return otel.Meter("paryty"), func() {}
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)
	return otel.Meter("paryty.cluster.query"), func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			logger.Warn("OTel meter provider shutdown error", zap.Error(err))
		}
	}
}

// otelMiddleware records golden signal metrics (latency, traffic, errors) for
// every HTTP request processed by the query service.
func otelMiddleware(queryDuration metric.Float64Histogram, queryRequests metric.Int64Counter, queryErrors metric.Int64Counter) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := float64(time.Since(start).Milliseconds())
		queryDuration.Record(c.Request.Context(), duration)
		queryRequests.Add(c.Request.Context(), 1)
		if c.Writer.Status() >= 400 {
			queryErrors.Add(c.Request.Context(), 1)
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
	tlsCertFileFlag := flag.String("tls-cert", "", "Path to TLS certificate PEM file")
	tlsKeyFileFlag := flag.String("tls-key", "", "Path to TLS private key PEM file")
	flag.Parse()

	// TLS env vars act as defaults when CLI flags are not set
	tlsCertFile := *tlsCertFileFlag
	tlsKeyFile := *tlsKeyFileFlag
	if tlsCertFile == "" {
		tlsCertFile = os.Getenv("PARYTY_TLS_CERT_FILE")
	}
	if tlsKeyFile == "" {
		tlsKeyFile = os.Getenv("PARYTY_TLS_KEY_FILE")
	}

	isDevMode := os.Getenv("PARYTY_DEV_MODE") == "true"

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	// ── TLS Configuration ────────────────────────────────────────────────
	// Production deployments MUST use TLS. Dev mode allows plaintext.
	// Set PARYTY_REQUIRE_TLS=true to enforce at startup (fatal if certs missing).
	requireTLS := os.Getenv("PARYTY_REQUIRE_TLS") == "true" ||
		os.Getenv("PARYTY_DEV_MODE") != "true"

	if requireTLS && (tlsCertFile == "" || tlsKeyFile == "") {
		logger.Fatal("TLS is required in production mode. Provide --tls-cert and --tls-key flags, " +
			"or set PARYTY_TLS_CERT_FILE and PARYTY_TLS_KEY_FILE environment variables. " +
			"Override with PARYTY_REQUIRE_TLS=false for development only.")
	}

	var httpTLSConfig *tls.Config
	if tlsCertFile != "" && tlsKeyFile != "" {
		cert, certErr := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
		if certErr != nil {
			logger.Fatal("Failed to load TLS certificate", zap.Error(certErr))
		}
		httpTLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		}
	}

	// Create TLS config for Dragonfly client connections.
	// Production enforces TLS 1.3; dev mode allows plaintext.
	var dragonflyTLS *tls.Config
	if !isDevMode {
		dragonflyTLS = &tls.Config{MinVersion: tls.VersionTLS13}
	}

	// Determine PostgreSQL SSL mode: production requires encryption;
	// dev mode disables it for local setups. Environment override wins.
	dbSSLMode := "require"
	if isDevMode {
		dbSSLMode = "disable"
	}
	if v := os.Getenv("PARYTY_DB_SSLMODE"); v != "" {
		dbSSLMode = v
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.String("path", configPath), zap.Error(err))
	}
	if err := cfg.Validate(); err != nil {
		logger.Fatal("Invalid configuration", zap.Error(err))
	}

	logger.Info("Starting Paryty Query Service", zap.String("port", *queryPort))

	// Initialize OpenTelemetry meter with Prometheus exporter for self-monitoring.
	meter, otelShutdown := initOTelMeter(logger)
	defer otelShutdown()

	queryDuration, _ := meter.Float64Histogram("paryty.cluster.query.duration_ms",
		metric.WithDescription("Query request duration in milliseconds"))
	queryRequests, _ := meter.Int64Counter("paryty.cluster.query.requests.total",
		metric.WithDescription("Total query requests"))
	queryErrors, _ := meter.Int64Counter("paryty.cluster.query.errors.total",
		metric.WithDescription("Total query errors"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize storage tier orchestrator
	storageCfg := cfg.ToStorageConfig()
	storageCfg.Warm.SSLMode = dbSSLMode
	store, err := storage.New(ctx, storageCfg)
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
			poolCfg.MaxConns = int32(runtime.NumCPU() * 4)
			poolCfg.MinConns = int32(runtime.NumCPU())
			poolCfg.MaxConnLifetime = 1 * time.Hour
			poolCfg.MaxConnIdleTime = 30 * time.Minute
			poolCfg.HealthCheckPeriod = 30 * time.Second
			cpPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
			if err != nil {
				logger.Warn("Failed to create control plane pool, API key management disabled",
					zap.Error(err),
				)
				cpPool = nil
			} else {
				defer cpPool.Close()
				// Run versioned migrations before idempotent DDL.
				if err := controlplane.RunMigrations(ctx, cpPool, "migrations"); err != nil {
					logger.Warn("Migration runner error (non-fatal)", zap.Error(err))
				}
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

	// ── Read Replica PostgreSQL Pool ────────────────────────────────────
	// Offloads read-heavy queries (GET topology, metrics, traces) from the
	// primary control plane pool. Optional; falls back to primary if unset.
	var cpReplicaPool *pgxpool.Pool
	if replicaDSN := os.Getenv("DATABASE_REPLICA_URL"); replicaDSN != "" {
		replicaCfg, cfgErr := pgxpool.ParseConfig(replicaDSN)
		if cfgErr != nil {
			logger.Warn("Failed to parse DATABASE_REPLICA_URL, read replica disabled",
				zap.Error(cfgErr),
			)
		} else {
			// Replica pool uses the same RLS GUC initialization as primary.
			replicaCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
				_, err := conn.Exec(ctx, "SELECT set_config('app.current_tenant_id', '', false)")
				return err
			}
			replicaCfg.MaxConns = int32(runtime.NumCPU() * 8)
			replicaCfg.MinConns = int32(runtime.NumCPU())
			replicaCfg.MaxConnLifetime = 1 * time.Hour
			replicaCfg.MaxConnIdleTime = 30 * time.Minute
			replicaCfg.HealthCheckPeriod = 30 * time.Second
			cpReplicaPool, err = pgxpool.NewWithConfig(ctx, replicaCfg)
			if err != nil {
				logger.Warn("Failed to create read replica pool",
					zap.Error(err),
				)
				cpReplicaPool = nil
			} else {
				defer cpReplicaPool.Close()
				logger.Info("Read replica PostgreSQL pool connected")
			}
		}
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
	// Production deployments MUST set PARYTY_JWT_SECRET. The insecure
	// development fallback is only used when PARYTY_DEV_MODE=true — a
	// missing secret in production is a fatal misconfiguration, not a
	// warning, because every access token would be forgeable.
	jwtSecret := os.Getenv("PARYTY_JWT_SECRET")
	if jwtSecret == "" {
		if os.Getenv("PARYTY_DEV_MODE") != "true" {
			logger.Fatal("PARYTY_JWT_SECRET is required in production (set PARYTY_DEV_MODE=true for local development)")
		}
		jwtSecret = "paryty-dev-jwt-secret-change-in-production-min-32-bytes!!"
		logger.Warn("DEV MODE: using built-in JWT secret — never run this configuration in production")
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
		Addr:      cfg.Cluster.Storage.Hot.Addr,
		TTL:       cfg.TTLDuration(),
		TLSConfig: dragonflyTLS,
	}
	queryCache := api.NewQueryCache(cacheCfg, logger)
	defer queryCache.Close()
	logger.Info("Query cache initialized")

	// Create real query service with storage backend
	queryService := api.NewQueryService(store, apiKeyManager, agentAssigner, logger, auditLogger)

	// Wire control plane DB and plan engine into the query service for admin routes.
	queryService.SetDB(cpPool)
	queryService.SetReadPool(cpReplicaPool)
	queryService.SetPlanEngine(planEngine)

	// Parse the shared origin allow-list once — it governs both HTTP CORS
	// and WebSocket upgrade origin checks. PARYTY_CORS_ORIGINS is
	// comma-separated; empty means allow-all without credentials (dev).
	var corsOrigins []string
	if v := os.Getenv("PARYTY_CORS_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				corsOrigins = append(corsOrigins, trimmed)
			}
		}
	}

	// Create WebSocket handler. Origin allow-list mirrors HTTP CORS config.
	wsHandler := api.NewWebSocketHandler(store, logger, corsOrigins)

	// Create SSE handler
	sseHandler := api.NewSSEHandler(store, logger, cfg.Cluster.Stream.Brokers)

	// Set Gin mode
	gin.SetMode(gin.ReleaseMode)

	// Create Gin router
	router := gin.New()
	// Disable trusted proxy detection — ClientIP() will use net.RemoteAddr
	// instead of trusting X-Forwarded-For headers, preventing IP spoofing
	// that would bypass rate limiting. Deployments behind a reverse proxy
	// must configure specific trusted proxies instead.
	router.SetTrustedProxies(nil)
	router.Use(security.SecurityHeaders())
	router.Use(gin.Recovery())
	router.Use(otelMiddleware(queryDuration, queryRequests, queryErrors))
	// Phase 8 middleware chain: SecurityHeaders → Recovery → CORS → RequestID → AuditBegin
	router.Use(security.CORS(corsOrigins...))
	router.Use(security.RequestID())
	router.Use(security.AuditBegin(auditLogger))

	// Health check endpoints (public — liveness probes cannot authenticate).
	healthHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "paryty-query",
			"cache":   "connected",
		})
	}
	router.GET("/health", healthHandler)
	router.GET("/api/v1/health", healthHandler)

	// Prometheus metrics endpoint for self-monitoring (dogfooding).
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Agent binary downloads — public route, authenticated via API key query parameter.
	// Agents don't have JWT tokens; the download handler validates the API key internally.
	router.GET("/api/v1/agents/download/:platform", queryService.DownloadAgentBinary)

	// â”€â”€ Phase 8: Register Auth Routes (PostgreSQL-backed) â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

	// Public auth routes (no JWT required).
	public := router.Group("/api/v1/auth")

	if cpPool != nil {
		// Obtain the Dragonfly Redis client for distributed rate limiting
		// across multiple query-service pods. Falls back to in-memory rate
		// limiting when Dragonfly is unreachable or not configured.
		var redisClientForAuth *redis.Client
		if store != nil {
			redisClientForAuth = store.HotStore().RDB()
		}
		authHandler := auth.NewAuthHandler(tokenManager, cpPool, planEngine, logger, auditLogger, redisClientForAuth)
		cookieSecure := os.Getenv("PARYTY_DEV_MODE") != "true"
		// Production safeguard: if cookieSecure is false (dev mode), warn loudly.
		// Secure cookies are silently rejected by browsers over HTTP, so dev mode
		// must disable Secure. Production deployments MUST NOT set PARYTY_DEV_MODE=true.
		if !cookieSecure {
			logger.Warn("PARYTY_DEV_MODE=true — auth cookies sent WITHOUT Secure flag. DO NOT use in production.")
		}
		authAdapter := auth.NewAuthRESTAdapter(authHandler, cpPool, planEngine, cookieSecure)

		public.POST("/register", authAdapter.Register)
		public.POST("/login", authAdapter.Login)
		public.POST("/refresh", authAdapter.RefreshToken)
		public.POST("/logout", authAdapter.Logout)
		public.POST("/change-password", authAdapter.ChangePassword)
		public.POST("/verify-email", authAdapter.VerifyEmail)
		public.POST("/forgot-password", authAdapter.ForgotPassword)
		public.POST("/reset-password", authAdapter.ResetPassword)
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
		public.POST("/change-password", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "PostgreSQL control plane not configured"})
		})
		public.POST("/verify-email", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "PostgreSQL control plane not configured"})
		})
		public.POST("/forgot-password", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "PostgreSQL control plane not configured"})
		})
		public.POST("/reset-password", func(c *gin.Context) {
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
	authd.Use(queryService.RLSTenantMiddleware())
	authd.Use(security.RateLimit(security.DefaultRateLimitConfig()))

	// Distributed per-tenant rate limiter backed by Dragonfly (Redis-compatible).
	// Supplements the in-memory token bucket with a distributed sliding-window
	// counter for multi-pod deployments.
	redisAddr := os.Getenv("PARYTY_DRAGONFLY_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	tenantRL := security.NewTenantRateLimiter(redisAddr, 100, 1*time.Minute, dragonflyTLS)
	authd.Use(tenantRL.Middleware())

	// Register query data routes (rest.go) on the JWT-protected group.
	// Tenant scope for every handler is derived from JWT claims.
	queryService.RegisterRoutes(authd)
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
		// Create TopicManager for twin-scoped topic lifecycle.
		topicMgr, tmErr := stream.NewTopicManager(cfg.ToStreamConfig(), logger)
		if tmErr != nil {
			logger.Warn("Failed to create TopicManager for twin topics", zap.Error(tmErr))
		}

		// Create RoutingCache for hot-path assignment lookups.
		var routingCache *twin.RoutingCache
		if agentAssigner != nil {
			rdb := store.HotStore().RDB()
			routingCache = twin.NewRoutingCache(rdb, agentAssigner, slog.Default())
		}

		twinHandler := auth.NewTwinHandler(cpPool, nil, topicMgr, routingCache, auditLogger) // nil dispatcher for now
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

	// Wire WebSocket endpoint. GinJWTAuthFlexible accepts the token as a
	// query parameter because the browser WebSocket API cannot set headers.
	router.GET("/ws", auth.GinJWTAuthFlexible(tokenManager), wsHandler.HandleWebSocket)

	// Wire SSE endpoints — same flexible auth (EventSource cannot set headers).
	// RLS middleware ensures tenant isolation for SSE streams.
	sseAuth := auth.GinJWTAuthFlexible(tokenManager)
	sseRLS := composeMiddleware(sseAuth, queryService.RLSTenantMiddleware())
	router.GET("/api/v1/timeline/replay", sseRLS, sseHandler.HandleTimeline)
	router.GET("/api/v1/metrics/:agent_id/stream", sseRLS, sseHandler.HandleMetricsStream)
	router.GET("/api/v1/events/stream", sseRLS, sseHandler.HandleEventStream)

	// Wire intelligence API handlers (Phase 6) — JWT-protected and plan-gated:
	// intelligence results are tenant-confidential and a paid-plan feature.
	intelCfg := api.NewIntelConfigFromEnv()
	intelHandlers, err := api.NewIntelHandlers(intelCfg, logger)
	if err != nil {
		logger.Warn("Failed to initialize intelligence handlers (intelligence service may not be running)",
			zap.Error(err),
			zap.String("address", intelCfg.Address),
		)
		// Non-fatal: the rest of the query service works without intelligence.
	} else {
		intelGroup := router.Group("/api/v1")
		intelGroup.Use(auth.GinJWTAuth(tokenManager))
		intelGroup.Use(plan.GinPlanInfoInjector(planEngine))
		intelGroup.Use(queryService.RLSTenantMiddleware())
		intelGroup.Use(plan.GinFeatureGate(planEngine, "paryty_intel"))
		intelHandlers.RegisterRoutes(intelGroup)
		defer intelHandlers.Close()
		logger.Info("Intelligence API routes registered (JWT + paryty_intel feature gate)",
			zap.String("address", intelCfg.Address),
		)
	}

	// â”€â”€ AuditEnd middleware: finalizes audit events started by AuditBegin â”€â”€
	// Registered AFTER all route handlers. Gin post-handler middleware runs in
	// reverse registration order: AuditBegin captures start â†’ handler runs â†’
	// AuditEnd finalizes â†’ AuditBegin (skips if already finalized).
	router.Use(security.AuditEnd(auditLogger))

	logger.Info("All query routes registered")

	// Start Redpanda event consumers for WebSocket fanout.
	// Tenants are enumerated from the control plane when available;
	// otherwise PARYTY_WS_TENANTS (comma-separated) provides the list.
	tenants := loadEventConsumerTenants(ctx, cpPool, logger)
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
		Addr:         ":" + *queryPort,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if httpTLSConfig != nil {
		srv.TLSConfig = httpTLSConfig
	}

	// Start server in background
	go func() {
		if httpTLSConfig != nil {
			logger.Info("Starting HTTPS server", zap.String("addr", srv.Addr))
			if err := srv.ListenAndServeTLS(tlsCertFile, tlsKeyFile); err != nil && err != http.ErrServerClosed {
				logger.Fatal("Failed to start HTTPS server", zap.Error(err))
			}
		} else {
			logger.Warn("Starting HTTP server WITHOUT TLS — dev mode only. Use --tls-cert and --tls-key for production.")
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Fatal("Failed to start server", zap.Error(err))
			}
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
