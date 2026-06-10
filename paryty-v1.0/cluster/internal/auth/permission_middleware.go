package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
)

// RequirePermission returns a Gin middleware that checks the request context
// for the given permission key. If the user doesn't have it, returns 403.
//
// The middleware expects the user's permission set to be injected into the
// Gin context under plan.CtxPermissions by GinJWTAuth (or equivalent).
//
// Usage:
//
//	authGroup.POST("/twins", auth.RequirePermission("twins:write"), handler.CreateTwin)
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		permsVal, ok := c.Get(string(plan.CtxPermissions))
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "insufficient permissions",
				"details": "missing: " + permission,
			})
			return
		}

		permissions, ok := permsVal.(map[string]bool)
		if !ok || permissions == nil || len(permissions) == 0 {
			// Fallback: derive permissions from role (set by GinJWTAuth).
			if roleVal, rok := c.Get(string(plan.CtxUserRole)); rok {
				if role, rok2 := roleVal.(string); rok2 && role != "" {
					permissions = buildPermissions(role)
				}
			}
		}
		if permissions == nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "insufficient permissions",
				"details": "missing: " + permission,
			})
			return
		}

		if !permissions[permission] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "insufficient permissions",
				"details": "missing: " + permission,
			})
			return
		}
	}
}
