// Package security implements RBAC authorization, audit logging, TLS
// configuration, and secrets management for the Paryty platform.
package security

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// =============================================================================
// Role-Permission Mapping
// =============================================================================

// RolePermissions defines the default permissions for each role.
var RolePermissions = map[string]map[string]bool{
	"admin": {
		"tenants:read":      true,
		"tenants:write":     true,
		"users:read":        true,
		"users:write":       true,
		"twins:read":        true,
		"twins:write":       true,
		"api_keys:read":     true,
		"api_keys:write":    true,
		"audit:read":        true,
		"settings:read":     true,
		"settings:write":    true,
	},
	"operator": {
		"twins:read":         true,
		"twins:write":        true,
		"alerts:acknowledge": true,
		"metrics:read":       true,
		"topology:read":      true,
	},
	"viewer": {
		"twins:read":    true,
		"metrics:read":  true,
		"topology:read": true,
	},
}

// =============================================================================
// Gin HTTP Middleware
// =============================================================================

// RequireRole returns a Gin middleware that checks the authenticated user's
// role against a list of permitted roles. Roles are read from the Gin context
// key CtxUserRole (set by the JWT middleware).
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}

	return func(c *gin.Context) {
		userRole, ok := c.Get(string(plan.CtxUserRole))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "No role context found.",
			})
			return
		}

		roleStr, ok := userRole.(string)
		if !ok || !allowed[roleStr] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "FORBIDDEN",
				"message": "You do not have the required role for this action.",
			})
			return
		}

		c.Next()
	}
}

// RequirePermission returns a Gin middleware that checks whether the
// authenticated user has a specific permission. It merges the user's role
// defaults with any per-user permission overrides stored in the JWT claims.
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, ok := c.Get(string(plan.CtxUserRole))
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "UNAUTHENTICATED",
				"message": "No role context found.",
			})
			return
		}

		roleStr := userRole.(string)
		rolePerms, ok := RolePermissions[roleStr]
		if !ok || !rolePerms[permission] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":      "FORBIDDEN",
				"message":    "You do not have permission for this action.",
				"permission": permission,
			})
			return
		}

		c.Next()
	}
}

// =============================================================================
// gRPC Interceptors
// =============================================================================

// GrpcRequireRole returns a unary gRPC interceptor that checks the user's role.
func GrpcRequireRole(roles ...string) grpc.UnaryServerInterceptor {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}

	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		role, ok := ctx.Value(plan.CtxUserRole).(string)
		if !ok || !allowed[role] {
			return nil, status.Error(codes.PermissionDenied, "insufficient role")
		}
		return handler(ctx, req)
	}
}

// GrpcRequirePermission returns a unary gRPC interceptor that checks a specific permission.
func GrpcRequirePermission(permission string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		role, ok := ctx.Value(plan.CtxUserRole).(string)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "no role context")
		}

		rolePerms, ok := RolePermissions[role]
		if !ok || !rolePerms[permission] {
			return nil, status.Errorf(codes.PermissionDenied, "missing permission: %s", permission)
		}

		return handler(ctx, req)
	}
}
