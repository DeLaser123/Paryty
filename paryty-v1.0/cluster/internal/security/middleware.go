package security

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"github.com/redis/go-redis/v9"
)

// =============================================================================
// Request ID Middleware
// =============================================================================

const (
	// HeaderRequestID is the HTTP header for request correlation IDs.
	HeaderRequestID = "X-Request-ID"

	// CtxRequestID is the Gin context key for the request ID.
	CtxRequestID = "paryty:request_id"
)

// RequestID returns a Gin middleware that assigns a unique request ID to
// each incoming request. If the client sends an X-Request-ID header, that
// value is used; otherwise, a new UUID v4 is generated.
//
// The request ID is set on:
//   - The Gin context (accessible via c.GetString(CtxRequestID))
//   - The response header (X-Request-ID)
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(HeaderRequestID)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		c.Set(CtxRequestID, requestID)
		c.Header(HeaderRequestID, requestID)
		c.Next()
	}
}

// =============================================================================
// Audit Middleware (Gin)
// =============================================================================

const (
	// ctxAuditMeta is the Gin context key for audit metadata set by AuditBegin.
	ctxAuditMeta = "paryty:audit_meta"

	// ctxAuditFinalized is set to true after AuditEnd (or AuditBegin fallback)
	// finalizes the audit event, preventing double-logging.
	ctxAuditFinalized = "paryty:audit_finalized"
)

// auditMetadata captures request metadata at the start of processing and
// is finalized after the handler completes.
type auditMetadata struct {
	StartTime  time.Time
	Duration   time.Duration
	Method     string
	Path       string
	StatusCode int
	IPAddress  string
	UserAgent  string
}

// AuditBegin returns a Gin middleware that captures request metadata at the
// start of a request and stores it in the Gin context. The actual audit event
// logging is deferred to AuditEnd (or falls back to the post-handler in
// AuditBegin if AuditEnd is not registered).
//
// This middleware should be placed AFTER the JWT auth middleware so that
// user/tenant IDs are available in the context.
//
// Usage (paired with AuditEnd):
//
//	router.Use(security.AuditBegin(logger))
//	// ... routes ...
//	router.Use(security.AuditEnd(logger))
func AuditBegin(logger *AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Capture start time.
		start := time.Now()

		// Store audit metadata in context for AuditEnd to finalize.
		auditMeta := &auditMetadata{
			StartTime: start,
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
			IPAddress: c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
		}
		c.Set(ctxAuditMeta, auditMeta)
		c.Set(ctxAuditFinalized, false)

		c.Next()

		// Fallback: if AuditEnd has not finalized (not registered),
		// finalize here so audit events are never silently lost.
		if finalized, _ := c.Get(ctxAuditFinalized); !finalized.(bool) {
			auditMeta.Duration = time.Since(start)
			auditMeta.StatusCode = c.Writer.Status()

			tenantID, _ := c.Get(string(plan.CtxTenantID))
			userID, _ := c.Get(string(plan.CtxUserID))

			tenantStr, _ := tenantID.(string)
			userStr, _ := userID.(string)

			logger.Log(&AuditEvent{
				TenantID:     tenantStr,
				UserID:       userStr,
				Action:       auditMeta.Method,
				ResourceType: "http_request",
				ResourceID:   auditMeta.Path,
				Details: map[string]interface{}{
					"status_code": auditMeta.StatusCode,
					"duration_ms": auditMeta.Duration.Milliseconds(),
					"method":      auditMeta.Method,
					"path":        auditMeta.Path,
				},
				IPAddress: auditMeta.IPAddress,
				UserAgent: auditMeta.UserAgent,
			})

			c.Set(ctxAuditFinalized, true)
		}
	}
}

// AuditEnd returns a Gin middleware that finalizes audit events started by
// AuditBegin. It reads the metadata stored by AuditBegin, computes the
// duration and status code, then logs the audit event.
//
// AuditEnd should be registered AFTER all route handlers via router.Use():
//
//	router.Use(security.AuditBegin(logger))
//	// ... routes ...
//	router.Use(security.AuditEnd(logger))
//
// Gin middleware execution order for post-handlers is reverse registration:
// AuditBegin.pre → handler → AuditEnd.post → AuditBegin.post (skips if
// AuditEnd already finalized).
//
// If AuditEnd is not registered, AuditBegin's own post-handler acts as a
// fallback so audit events are never silently lost.
func AuditEnd(logger *AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Read metadata set by AuditBegin.
		metaVal, ok := c.Get(ctxAuditMeta)
		if !ok {
			return // AuditBegin not registered; nothing to finalize.
		}
		auditMeta, ok := metaVal.(*auditMetadata)
		if !ok {
			return
		}

		// Guard against double-finalization (e.g., if AuditBegin fallback already ran).
		if finalized, _ := c.Get(ctxAuditFinalized); finalized.(bool) {
			return
		}

		auditMeta.Duration = time.Since(auditMeta.StartTime)
		auditMeta.StatusCode = c.Writer.Status()

		tenantID, _ := c.Get(string(plan.CtxTenantID))
		userID, _ := c.Get(string(plan.CtxUserID))

		tenantStr, _ := tenantID.(string)
		userStr, _ := userID.(string)

		logger.Log(&AuditEvent{
			TenantID:     tenantStr,
			UserID:       userStr,
			Action:       auditMeta.Method,
			ResourceType: "http_request",
			ResourceID:   auditMeta.Path,
			Details: map[string]interface{}{
				"status_code": auditMeta.StatusCode,
				"duration_ms": auditMeta.Duration.Milliseconds(),
				"method":      auditMeta.Method,
				"path":        auditMeta.Path,
			},
			IPAddress: auditMeta.IPAddress,
			UserAgent: auditMeta.UserAgent,
		})

		c.Set(ctxAuditFinalized, true)
	}
}

// =============================================================================
// CORS Middleware
// =============================================================================

// CORS returns a Gin middleware that sets CORS headers. With no arguments
// (or "*") every origin is allowed WITHOUT credentials — the wildcard +
// credentials combination is both rejected by browsers and an OWASP
// misconfiguration. Pass explicit origins (e.g. from PARYTY_CORS_ORIGINS)
// to enable credentialed cross-origin requests in production.
func CORS(allowedOrigins ...string) gin.HandlerFunc {
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}
	wildcard := allowedOrigins[0] == "*"

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		allowOrigin := ""
		if wildcard {
			allowOrigin = "*"
		} else {
			for _, o := range allowedOrigins {
				if o == origin {
					allowOrigin = origin
					break
				}
			}
		}

		if allowOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowOrigin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
			c.Header("Access-Control-Expose-Headers", "X-Request-ID")
			c.Header("Access-Control-Max-Age", "86400")
			// Credentials are only safe with an explicit origin match.
			if !wildcard {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// =============================================================================
// Security Headers Middleware
// =============================================================================

// SecurityHeaders returns a Gin middleware that sets hardened HTTP security
// headers on every response. These headers mitigate common attack classes
// including clickjacking, MIME-type sniffing, XSS, and information leakage.
//
// This middleware should be registered FIRST in the middleware chain so that
// security headers are present on all responses, including error responses
// from later middleware.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "0")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; "+
			"img-src 'self' data: blob:; connect-src 'self' wss: https:; "+
			"font-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		c.Next()
	}
}

// =============================================================================
// Rate Limit Middleware (token bucket)
// =============================================================================

// RateLimitConfig configures the token-bucket rate limiter.
type RateLimitConfig struct {
	// MaxRequestsPerMinute is the sustained request rate per tenant.
	MaxRequestsPerMinute int
	// BurstSize is the maximum burst above the sustained rate.
	BurstSize int
}

// DefaultRateLimitConfig returns sensible defaults for the rate limiter.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		MaxRequestsPerMinute: 600, // 10 req/s sustained
		BurstSize:            100, // allow bursts up to 100
	}
}

// RateLimit returns a Gin middleware that enforces per-tenant token-bucket
// rate limiting. The tenant ID is read from the plan.CtxTenantID context
// key (set by the JWT auth middleware). If no tenant context is present,
// the global "anonymous" bucket is used.
//
// The current implementation is an in-memory token bucket backed by sync.Map.
// TODO: Replace with Dragonfly/Redis-backed distributed rate limiter for
// multi-pod deployments. Key: ratelimit:{tenantID}:{endpoint}
func RateLimit(cfg RateLimitConfig) gin.HandlerFunc {
	limiter := newTokenBucketLimiter(cfg.MaxRequestsPerMinute, cfg.BurstSize)
	return func(c *gin.Context) {
		// Determine tenant. Fall back to "anonymous" if no auth context.
		tenantID := "anonymous"
		if v, ok := c.Get("paryty:tenant_id"); ok {
			if tid, ok := v.(string); ok && tid != "" {
				tenantID = tid
			}
		}

		// Use endpoint path as the rate limit scope key.
		endpoint := c.Request.URL.Path
		key := "ratelimit:" + tenantID + ":" + endpoint

		retryAfter, ok := limiter.tryConsume(key)
		if !ok {
			c.AbortWithStatusJSON(429, gin.H{
				"message": "rate limit exceeded",
				"details": gin.H{
					"retry_after_seconds": int(retryAfter.Seconds()),
				},
			})
			return
		}

		c.Next()
	}
}

// tokenBucketLimiter is an in-memory token bucket rate limiter.
// Each bucket is keyed by tenant+endpoint and holds the available tokens
// plus the timestamp of the last refill.
type tokenBucketLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64 // max tokens (burst size)
	buckets map[string]*bucketState
}

type bucketState struct {
	tokens   float64
	lastFill time.Time
	lastSeen time.Time
}

func newTokenBucketLimiter(maxPerMinute, burst int) *tokenBucketLimiter {
	if maxPerMinute <= 0 {
		maxPerMinute = 600
	}
	if burst <= 0 {
		burst = max(maxPerMinute/6, 10)
	}
	return &tokenBucketLimiter{
		rate:    float64(maxPerMinute) / 60.0, // convert to per-second
		burst:   float64(burst),
		buckets: make(map[string]*bucketState),
	}
}

// tryConsume attempts to consume 1 token from the bucket for the given key.
// Returns (retryAfter, false) if no token is available; (0, true) on success.
func (l *tokenBucketLimiter) tryConsume(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Lazy eviction: periodically remove stale buckets to prevent memory leak.
	// Runs inline every 1000 calls to avoid background goroutine.
	if len(l.buckets) > 1000 {
		staleThreshold := now.Add(-10 * time.Minute)
		for k, b := range l.buckets {
			if b.lastSeen.Before(staleThreshold) {
				delete(l.buckets, k)
			}
		}
	}

	bucket, ok := l.buckets[key]
	if !ok {
		// New bucket: start full.
		bucket = &bucketState{
			tokens:   l.burst,
			lastFill: now,
			lastSeen: now,
		}
		l.buckets[key] = bucket
	} else {
		// Refill tokens based on elapsed time.
		elapsed := now.Sub(bucket.lastFill).Seconds()
		bucket.tokens = min(l.burst, bucket.tokens+elapsed*l.rate)
		bucket.lastFill = now
		bucket.lastSeen = now
	}

	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return 0, true
	}

	// No token available. Calculate how long until next token is available.
	retryAfter := time.Duration((1.0-bucket.tokens)/l.rate*1000) * time.Millisecond
	if retryAfter < 100*time.Millisecond {
		retryAfter = 100 * time.Millisecond
	}
	return retryAfter, false
}

// =============================================================================
// Distributed Rate Limit Middleware (Dragonfly/Redis)
// =============================================================================

// TenantRateLimiter enforces per-tenant request rate limits using Dragonfly.
type TenantRateLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
}

// NewTenantRateLimiter creates a distributed rate limiter backed by Dragonfly.
// The redisAddr should point to the Dragonfly instance (Redis-compatible).
// limit is the maximum number of requests per tenant within the given window.
// tlsCfg, when non-nil, enables TLS for the Dragonfly connection.
func NewTenantRateLimiter(redisAddr string, limit int, window time.Duration, tlsCfg *tls.Config) *TenantRateLimiter {
	client := redis.NewClient(&redis.Options{Addr: redisAddr, TLSConfig: tlsCfg})
	return &TenantRateLimiter{client: client, limit: limit, window: window}
}

// Middleware returns a Gin middleware that enforces per-tenant rate limits
// using a sliding-window counter stored in Dragonfly. The tenant ID is read
// from plan.CtxTenantID (set by the JWT auth middleware). Requests without
// a tenant context pass through uncounted (fail-open for health probes etc.).
//
// Rate limit exceeded responses use HTTP 429 with a structured error body.
func (rl *TenantRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, ok := c.Get(string(plan.CtxTenantID))
		if !ok {
			c.Next()
			return
		}
		key := fmt.Sprintf("ratelimit:%s:%d", tenant, time.Now().UnixMilli()/rl.window.Milliseconds())
		count, err := rl.client.Incr(c.Request.Context(), key).Result()
		if err != nil {
			c.Next() // fail-open on Dragonfly errors
			return
		}
		if count == 1 {
			rl.client.Expire(c.Request.Context(), key, rl.window) //nolint:errcheck
		}
		if int(count) > rl.limit {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "RATE_LIMITED",
				"message": fmt.Sprintf("rate limit exceeded: %d requests per %s", rl.limit, rl.window),
			})
			return
		}
		c.Next()
	}
}
