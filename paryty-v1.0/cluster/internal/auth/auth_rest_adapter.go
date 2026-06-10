package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthRESTAdapter bridges Gin REST handlers to the gRPC AuthHandler, providing
// frontend-compatible JSON responses for register, login, refresh, and logout.
type AuthRESTAdapter struct {
	handler *AuthHandler
	db      *pgxpool.Pool
	engine  *plan.PlanEngine
}

// NewAuthRESTAdapter creates a new AuthRESTAdapter.
func NewAuthRESTAdapter(handler *AuthHandler, db *pgxpool.Pool, engine *plan.PlanEngine) *AuthRESTAdapter {
	return &AuthRESTAdapter{
		handler: handler,
		db:      db,
		engine:  engine,
	}
}

// ── bind types ─────────────────────────────────────────────────────────────

type registerBind struct {
	Email      string `json:"email" binding:"required"`
	Password   string `json:"password" binding:"required"`
	Name       string `json:"name" binding:"required"`
	TenantName string `json:"tenantName"`
	PlanName   string `json:"planName"`
}

type loginBind struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type refreshBind struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

type logoutBind struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

// ── gin.HandlerFunc methods ─────────────────────────────────────────────────

// Register handles POST /api/v1/auth/register.
func (a *AuthRESTAdapter) Register(c *gin.Context) {
	var req registerBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	// Proto Name field maps to tenant name.
	tenantName := req.TenantName
	if tenantName == "" {
		tenantName = req.Name
	}

	planName := req.PlanName
	if planName == "" {
		planName = "basic"
	}

	protoReq := &parytyv1.RegisterRequest{
		Email:    req.Email,
		Password: req.Password,
		Name:     tenantName,
		PlanName: planName,
	}

	resp, err := a.handler.Register(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(grpcCodeToHTTP(err), gin.H{"message": status.Convert(err).Message()})
		return
	}

	now := time.Now().UTC()
	accessExpiresAt := now.Add(AccessTokenTTL)

	// Resolve stored tenant name from DB.
	storedTenantName := queryTenantName(c.Request.Context(), a.db, resp.TenantId)

	body := gin.H{
		"accessToken":  resp.AccessToken,
		"refreshToken": resp.RefreshToken,
		"expiresAt":    accessExpiresAt.Format(time.RFC3339),
		"user": gin.H{
			"id":          resp.UserId,
			"email":       req.Email,
			"name":        req.Name,
			"role":        "admin",
			"tenantId":    resp.TenantId,
			"permissions": buildPermissions("admin"),
			"createdAt":   now.Format(time.RFC3339),
		},
		"tenant": gin.H{
			"id":        resp.TenantId,
			"name":      storedTenantName,
			"planName":  planName,
			"createdAt": now.Format(time.RFC3339),
		},
	}

	if def, found := a.engine.GetPlan(planName); found {
		body["plan"] = gin.H{
			"planName":  planName,
			"features":  def.Features,
			"limits":    planLimitsToMap(def.Limits),
			"quotas":    planQuotasToMap(def.Quotas),
			"startedAt": now.Format(time.RFC3339),
		}
	}

	c.JSON(http.StatusCreated, body)
}

// Login handles POST /api/v1/auth/login.
func (a *AuthRESTAdapter) Login(c *gin.Context) {
	var req loginBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "email and password are required"})
		return
	}

	protoReq := &parytyv1.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	}

	resp, err := a.handler.Login(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(grpcCodeToHTTP(err), gin.H{"message": status.Convert(err).Message()})
		return
	}

	now := time.Now().UTC()
	accessExpiresAt := now.Add(AccessTokenTTL)

	storedTenantName := queryTenantName(c.Request.Context(), a.db, resp.User.TenantId)

	planName := "basic"
	if stored, err := a.engine.GetTenantPlan(c.Request.Context(), resp.User.TenantId); err == nil && stored != nil {
		planName = stored.PlanName
	}

	body := gin.H{
		"accessToken":  resp.AccessToken,
		"refreshToken": resp.RefreshToken,
		"expiresAt":    accessExpiresAt.Format(time.RFC3339),
		"user": gin.H{
			"id":          resp.User.UserId,
			"email":       resp.User.Email,
			"name":        resp.User.Name,
			"role":        resp.User.Role,
			"tenantId":    resp.User.TenantId,
			"permissions": buildPermissions(resp.User.Role),
			"createdAt":   safeTimestamp(resp.User.CreatedAt, now).Format(time.RFC3339),
		},
		"tenant": gin.H{
			"id":        resp.User.TenantId,
			"name":      storedTenantName,
			"planName":  planName,
			"createdAt": now.Format(time.RFC3339),
		},
	}

	if def, found := a.engine.GetPlan(planName); found {
		body["plan"] = gin.H{
			"planName":  planName,
			"features":  def.Features,
			"limits":    planLimitsToMap(def.Limits),
			"quotas":    planQuotasToMap(def.Quotas),
			"startedAt": now.Format(time.RFC3339),
		}
	}

	c.JSON(http.StatusOK, body)
}

// RefreshToken handles POST /api/v1/auth/refresh.
func (a *AuthRESTAdapter) RefreshToken(c *gin.Context) {
	var req refreshBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "refresh_token is required"})
		return
	}

	protoReq := &parytyv1.RefreshTokenRequest{
		RefreshToken: req.RefreshToken,
	}

	resp, err := a.handler.RefreshToken(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(grpcCodeToHTTP(err), gin.H{"message": status.Convert(err).Message()})
		return
	}

	now := time.Now().UTC()
	accessExpiresAt := now.Add(AccessTokenTTL)

	storedTenantName := queryTenantName(c.Request.Context(), a.db, resp.User.TenantId)

	planName := "basic"
	if stored, err := a.engine.GetTenantPlan(c.Request.Context(), resp.User.TenantId); err == nil && stored != nil {
		planName = stored.PlanName
	}

	body := gin.H{
		"accessToken":  resp.AccessToken,
		"refreshToken": resp.RefreshToken,
		"expiresAt":    accessExpiresAt.Format(time.RFC3339),
		"user": gin.H{
			"id":          resp.User.UserId,
			"email":       resp.User.Email,
			"name":        resp.User.Name,
			"role":        resp.User.Role,
			"tenantId":    resp.User.TenantId,
			"permissions": buildPermissions(resp.User.Role),
			"createdAt":   safeTimestamp(resp.User.CreatedAt, now).Format(time.RFC3339),
		},
		"tenant": gin.H{
			"id":        resp.User.TenantId,
			"name":      storedTenantName,
			"planName":  planName,
			"createdAt": now.Format(time.RFC3339),
		},
	}

	if def, found := a.engine.GetPlan(planName); found {
		body["plan"] = gin.H{
			"planName":  planName,
			"features":  def.Features,
			"limits":    planLimitsToMap(def.Limits),
			"quotas":    planQuotasToMap(def.Quotas),
			"startedAt": now.Format(time.RFC3339),
		}
	}

	c.JSON(http.StatusOK, body)
}

// Logout handles POST /api/v1/auth/logout.
func (a *AuthRESTAdapter) Logout(c *gin.Context) {
	var req logoutBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "refresh_token is required"})
		return
	}

	protoReq := &parytyv1.LogoutRequest{
		RefreshToken: req.RefreshToken,
	}

	_, err := a.handler.Logout(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(grpcCodeToHTTP(err), gin.H{"message": status.Convert(err).Message()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// =============================================================================
// helpers
// =============================================================================

// grpcCodeToHTTP maps gRPC status codes to HTTP status codes.
func grpcCodeToHTTP(err error) int {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// queryTenantName returns the tenant name from the tenants table.
// Falls back to tenantID if the query fails or db is nil.
func queryTenantName(ctx context.Context, db *pgxpool.Pool, tenantID string) string {
	if db == nil {
		return tenantID
	}
	var name string
	if err := db.QueryRow(ctx, `SELECT name FROM tenants WHERE tenant_id = $1`, tenantID).Scan(&name); err != nil {
		return tenantID
	}
	return name
}

// safeTimestamp converts a proto timestamp to time.Time, falling back to now
// if the timestamp is nil.
func safeTimestamp(ts *timestamppb.Timestamp, fallback time.Time) time.Time {
	if ts == nil {
		return fallback
	}
	return ts.AsTime()
}

// buildAdminPermissions returns the default admin permission set.
func buildAdminPermissions() map[string]bool {
	return map[string]bool{
		"tenants:read":  true,
		"tenants:write": true,
		"users:read":    true,
		"users:write":   true,
		"twins:read":    true,
		"twins:write":   true,
		"api_keys:read": true,
		"api_keys:write": true,
	}
}

// planLimitsToMap converts plan.LimitSet to a flat map for JSON serialization.
// Duplicated from cmd/query/main.go (not exported from main).
func planLimitsToMap(limits plan.LimitSet) map[string]interface{} {
	return map[string]interface{}{
		"agentsPerTwin":       limits.AgentsPerTwin,
		"dataRetentionDays":   limits.DataRetentionDays,
		"subUsers":            limits.SubUsers,
		"alertRulesPerTenant": limits.AlertRulesPerTenant,
	}
}

// planQuotasToMap converts plan.QuotaSet to a flat map for JSON serialization.
// Duplicated from cmd/query/main.go (not exported from main).
func planQuotasToMap(quotas plan.QuotaSet) map[string]interface{} {
	return map[string]interface{}{
		"ingestionBytesPerDay":   quotas.IngestionBytesPerDay,
		"queryRequestsPerMinute": quotas.QueryRequestsPerMinute,
	}
}
