package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Gin HTTP Middleware
// =============================================================================

// GinJWTAuth returns a Gin middleware that validates the Bearer access token
// from the Authorization header. On success, it injects user identity into the
// Gin context under the standard plan context keys.
//
// Injected context keys:
//   - CtxTenantID    → tenant_id from JWT
//   - CtxPlanName    → plan_name from JWT
//   - CtxUserID      → sub (user_id) from JWT
//   - CtxUserRole    → role from JWT
//   - CtxPermissions → permissions map from JWT
func GinJWTAuth(tm *TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "Authorization header is required.",
			})
			return
		}

		// Accept "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "Authorization header must be 'Bearer <token>'.",
			})
			return
		}

		tokenStr := strings.TrimSpace(parts[1])
		if tokenStr == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "Token must not be empty.",
			})
			return
		}

		claims, err := tm.ValidateAccess(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "Invalid or expired token.",
			})
			return
		}

		// Inject claims into context for downstream middleware/handlers.
		c.Set(string(plan.CtxTenantID), claims.TenantID)
		c.Set(string(plan.CtxPlanName), claims.PlanName)
		c.Set(string(plan.CtxUserID), claims.Subject)
		c.Set(string(plan.CtxUserRole), claims.Role)
		c.Set(string(plan.CtxPermissions), claims.Permissions)

		c.Next()
	}
}

// GinOptionalAuth is like GinJWTAuth but does NOT abort if no token is present.
// If a valid token IS present, the claims are injected. If not, the request
// continues anonymously. This is useful for public endpoints that have
// optional personalization.
func GinOptionalAuth(tm *TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.Next()
			return
		}

		tokenStr := strings.TrimSpace(parts[1])
		if tokenStr == "" {
			c.Next()
			return
		}

		claims, err := tm.ValidateAccess(tokenStr)
		if err != nil {
			c.Next()
			return
		}

		c.Set(string(plan.CtxTenantID), claims.TenantID)
		c.Set(string(plan.CtxPlanName), claims.PlanName)
		c.Set(string(plan.CtxUserID), claims.Subject)
		c.Set(string(plan.CtxUserRole), claims.Role)
		c.Set(string(plan.CtxPermissions), claims.Permissions)

		c.Next()
	}
}

// =============================================================================
// gRPC Interceptors
// =============================================================================

// GrpcJWTAuth returns a unary gRPC interceptor that validates the Bearer
// access token from gRPC metadata ("authorization" key). On success, it
// injects claims into the context.
func GrpcJWTAuth(tm *TokenManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		authValues := md.Get("authorization")
		if len(authValues) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization metadata")
		}

		authHeader := authValues[0]
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return nil, status.Error(codes.Unauthenticated, "authorization must be 'Bearer <token>'")
		}

		claims, err := tm.ValidateAccess(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}

		// Inject claims into context.
		ctx = context.WithValue(ctx, plan.CtxTenantID, claims.TenantID)
		ctx = context.WithValue(ctx, plan.CtxPlanName, claims.PlanName)
		ctx = context.WithValue(ctx, plan.CtxUserID, claims.Subject)
		ctx = context.WithValue(ctx, plan.CtxUserRole, claims.Role)
		ctx = context.WithValue(ctx, plan.CtxPermissions, claims.Permissions)

		return handler(ctx, req)
	}
}
