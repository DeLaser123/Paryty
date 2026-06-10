package plan

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// TwinCounter provides the ability to count active twins for a tenant.
// Implementations include twin.TwinManager.
type TwinCounter interface {
	// CountTwins returns the number of active (non-deleted) twins for the
	// given tenant. Returns an error if the database is unavailable.
	CountTwins(ctx context.Context, tenantID string) (int, error)
}

// RequireTwinQuota returns a Gin middleware that checks whether the
// authenticated tenant has reached their max twin limit. If the limit
// is exceeded, the request is aborted with HTTP 402 Payment Required.
//
// The middleware expects:
//   - plan.CtxTenantID (set by JWT auth middleware)
//   - plan.CtxPlanName (set by JWT auth middleware or PlanInfoInjector)
//
// Usage:
//
//	authGroup.POST("/twins", plan.RequireTwinQuota(planEngine, twinMgr), handler.CreateTwin)
func RequireTwinQuota(engine *PlanEngine, twinCounter TwinCounter) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantVal, ok := c.Get(string(CtxTenantID))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "No tenant context found. Authentication required.",
			})
			return
		}
		tenantID, ok := tenantVal.(string)
		if !ok || tenantID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "Invalid tenant context.",
			})
			return
		}

		planVal, ok := c.Get(string(CtxPlanName))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "No plan context found. Authentication required.",
			})
			return
		}
		planName, ok := planVal.(string)
		if !ok || planName == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "Invalid plan context.",
			})
			return
		}

		maxTwins := engine.GetMaxTwins(planName)
		if maxTwins <= 0 {
			// Unknown plan or misconfigured — fail closed.
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":   "PLAN_UNKNOWN",
				"message": "Your plan does not support Paryty Twins.",
			})
			return
		}

		current, err := twinCounter.CountTwins(c.Request.Context(), tenantID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "INTERNAL",
				"message": "Failed to check twin quota.",
			})
			return
		}

		if current >= maxTwins {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":   "TWIN_LIMIT_REACHED",
				"message": "Twin quota exceeded. Upgrade your plan to create more twins.",
				"details": gin.H{
					"current": current,
					"limit":   maxTwins,
				},
			})
			return
		}
	}
}

// RequireAgentQuota returns a Gin middleware that checks whether the
// authenticated tenant has reached their max agents-per-twin limit.
//
// The middleware expects:
//   - plan.CtxTenantID (set by JWT auth middleware)
//   - plan.CtxPlanName (set by JWT auth middleware or PlanInfoInjector)
//   - :twin_id URL parameter for the target twin
//
// Usage:
//
//	authGroup.POST("/twins/:twin_id/agents", plan.RequireAgentQuota(planEngine, agentCounter), handler.AssignAgent)
func RequireAgentQuota(engine *PlanEngine, agentCounter AgentCounter) gin.HandlerFunc {
	return func(c *gin.Context) {
		planVal, ok := c.Get(string(CtxPlanName))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "No plan context found.",
			})
			return
		}
		planName, ok := planVal.(string)
		if !ok || planName == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "Invalid plan context.",
			})
			return
		}

		twinID := c.Param("twin_id")
		if twinID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "INVALID_ARGUMENT",
				"message": "Missing twin_id in URL.",
			})
			return
		}

		maxAgents := engine.GetAgentsPerTwin(planName)
		if maxAgents <= 0 {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":   "PLAN_UNKNOWN",
				"message": "Your plan does not support agent assignment.",
			})
			return
		}

		current, err := agentCounter.CountAgentsForTwin(c.Request.Context(), twinID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "INTERNAL",
				"message": "Failed to check agent quota.",
			})
			return
		}

		if current >= maxAgents {
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":   "AGENT_LIMIT_REACHED",
				"message": "Agent quota per twin exceeded. Upgrade your plan to assign more agents.",
				"details": gin.H{
					"current": current,
					"limit":   maxAgents,
				},
			})
			return
		}

		c.Next()
	}
}

// AgentCounter provides the ability to count agents assigned to a twin.
type AgentCounter interface {
	// CountAgentsForTwin returns the number of agents currently assigned
	// to the specified twin.
	CountAgentsForTwin(ctx context.Context, twinID string) (int, error)
}
