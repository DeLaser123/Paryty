package plan

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Context Keys
// =============================================================================

type contextKey string

const (
	// CtxTenantID is the context key for the authenticated tenant ID.
	CtxTenantID contextKey = "paryty:tenant_id"
	// CtxPlanName is the context key for the tenant's current plan name.
	CtxPlanName contextKey = "paryty:plan_name"
	// CtxUserID is the context key for the authenticated user ID.
	CtxUserID contextKey = "paryty:user_id"
	// CtxUserRole is the context key for the authenticated user's role.
	CtxUserRole contextKey = "paryty:user_role"
	// CtxPermissions is the context key for the authenticated user's permission set.
	CtxPermissions contextKey = "paryty:permissions"
)

// =============================================================================
// Gin HTTP Middleware
// =============================================================================

// GinFeatureGate returns a Gin middleware that checks whether the authenticated
// tenant's plan has the required feature enabled. If the feature is not enabled,
// the request is aborted with HTTP 402 Payment Required and a descriptive body.
//
// The middleware expects the tenant's plan name to be set in the Gin context
// under the key CtxPlanName. This should be injected by the JWT auth middleware.
func GinFeatureGate(engine *PlanEngine, feature string) gin.HandlerFunc {
	return func(c *gin.Context) {
		planName, ok := c.Get(string(CtxPlanName))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "No plan context found. Authentication required.",
			})
			return
		}

		planStr, ok := planName.(string)
		if !ok || !engine.HasFeature(planStr, feature) {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":   "FEATURE_NOT_AVAILABLE",
				"message": "This feature is not available on your current plan.",
				"feature": feature,
			})
			return
		}

		c.Next()
	}
}

// GinTwinLimitGate returns a Gin middleware that checks whether the tenant
// has reached their twin limit. The twinCounter callback should return the
// current number of twins for the authenticated tenant.
func GinTwinLimitGate(engine *PlanEngine, twinCounter func(c *gin.Context) int) gin.HandlerFunc {
	return func(c *gin.Context) {
		planName, ok := c.Get(string(CtxPlanName))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "No plan context found. Authentication required.",
			})
			return
		}

		planStr := planName.(string)
		maxTwins := engine.GetMaxTwins(planStr)
		current := twinCounter(c)

		if current >= maxTwins {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":      "LIMIT_REACHED",
				"message":    "You have reached the maximum number of Paryty Twins for your plan.",
				"current":    current,
				"max":        maxTwins,
				"upgrade_to_increase": true,
			})
			return
		}

		c.Next()
	}
}

// GinPlanInfoInjector is middleware that reads the tenant's plan from the
// database (or falls back to the JWT-injected plan name) and sets CtxPlanName
// in the Gin context.
func GinPlanInfoInjector(engine *PlanEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		// The JWT middleware should have already set the plan name.
		// If it hasn't, try to look up from the database using the tenant ID.
		if _, ok := c.Get(string(CtxPlanName)); !ok {
			tenantID, ok := c.Get(string(CtxTenantID))
			if !ok {
				c.Next()
				return
			}

			tenantStr, ok := tenantID.(string)
			if !ok {
				c.Next()
				return
			}

			// Try DB lookup as fallback.
			if engine.db != nil {
				stored, err := engine.GetTenantPlan(c.Request.Context(), tenantStr)
				if err == nil && stored != nil {
					c.Set(string(CtxPlanName), stored.PlanName)
				}
			}
		}

		c.Next()
	}
}

// =============================================================================
// gRPC Interceptors
// =============================================================================

// GrpcFeatureGate returns a unary gRPC interceptor that checks whether the
// authenticated tenant's plan has the required feature enabled.
//
// The interceptor expects the plan name to be set in the gRPC metadata/context
// under the key CtxPlanName. This should be injected by the JWT auth interceptor.
func GrpcFeatureGate(engine *PlanEngine, feature string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		planName, ok := ctx.Value(CtxPlanName).(string)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "no plan context in request")
		}

		if !engine.HasFeature(planName, feature) {
			return nil, status.Errorf(codes.PermissionDenied,
				"feature %q is not available on your current plan (%s)", feature, planName)
		}

		return handler(ctx, req)
	}
}

// GrpcTwinLimitGate returns a unary gRPC interceptor that checks the twin limit.
func GrpcTwinLimitGate(engine *PlanEngine, twinCounter func(ctx context.Context) int) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		planName, ok := ctx.Value(CtxPlanName).(string)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "no plan context in request")
		}

		maxTwins := engine.GetMaxTwins(planName)
		current := twinCounter(ctx)

		if current >= maxTwins {
			return nil, status.Errorf(codes.ResourceExhausted,
				"twin limit reached: %d/%d (plan: %s)", current, maxTwins, planName)
		}

		return handler(ctx, req)
	}
}
