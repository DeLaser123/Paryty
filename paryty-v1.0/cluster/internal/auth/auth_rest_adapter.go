package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthRESTAdapter bridges Gin REST handlers to the gRPC AuthHandler, providing
// frontend-compatible JSON responses for register, login, refresh, and logout.
type AuthRESTAdapter struct {
	handler     *AuthHandler
	db          *pgxpool.Pool
	engine      *plan.PlanEngine
	cookieSecure bool // false in dev (HTTP), true in production (HTTPS)
}

// NewAuthRESTAdapter creates a new AuthRESTAdapter.
// cookieSecure controls the Secure flag on auth cookies — pass false for local
// development over HTTP (where Secure cookies are silently rejected by browsers)
// and true for production deployments behind HTTPS.
func NewAuthRESTAdapter(handler *AuthHandler, db *pgxpool.Pool, engine *plan.PlanEngine, cookieSecure bool) *AuthRESTAdapter {
	return &AuthRESTAdapter{
		handler:      handler,
		db:           db,
		engine:       engine,
		cookieSecure: cookieSecure,
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

type changePasswordBind struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

type verifyEmailBind struct {
	Token string `json:"token" binding:"required"`
}

type forgotPasswordBind struct {
	Email string `json:"email" binding:"required,email"`
}

type resetPasswordBind struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

// ── gin.HandlerFunc methods ─────────────────────────────────────────────────

// Register handles POST /api/v1/auth/register.
func (a *AuthRESTAdapter) Register(c *gin.Context) {
	// Distributed rate limit (Dragonfly-backed) takes precedence and works
	// across multiple pods. Falls through to in-memory when Dragonfly is
	// unreachable (fail-open).
	clientIP := c.ClientIP()
	if a.handler.distributedRegisterRL != nil {
		key := RegisterKey(clientIP)
		if allowed, retryAfter := a.handler.distributedRegisterRL.Allow(c.Request.Context(), key, maxRegisterAttempts, registerWindowSize); !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"message": fmt.Sprintf("too many registration attempts; try again in %.0f seconds", retryAfter.Seconds()),
			})
			return
		}
	}

	// In-memory rate limit as secondary backstop.
	// Rate limit: 5 registrations per hour per IP.
	if a.handler.registerRate != nil && !a.handler.registerRate.allow(clientIP) {
		c.JSON(http.StatusTooManyRequests, gin.H{"message": "too many registration attempts"})
		return
	}

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

	// Set httpOnly cookie for refresh token (XSS protection).
	// Matches Login behavior: refresh token travels only via httpOnly cookie,
	// never in the response body.
	refreshExpiresAt := now.Add(RefreshTokenTTL)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "paryty_refresh_token",
		Value:    resp.RefreshToken,
		Path:     "/api/v1/auth",
		Expires:  refreshExpiresAt,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})

	// Set httpOnly cookie for access token (used by WebSocket/SSE to avoid
	// exposing tokens in URL query parameters).
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "paryty_access_token",
		Value:    resp.AccessToken,
		Path:     "/",
		Expires:  accessExpiresAt,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})

	// Remove refresh token from response body (now in httpOnly cookie).
	delete(body, "refreshToken")

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

	// Set httpOnly cookie for refresh token (XSS protection).
	// The refresh token is NOT included in the response body when using cookies.
	refreshExpiresAt := now.Add(RefreshTokenTTL)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "paryty_refresh_token",
		Value:    resp.RefreshToken,
		Path:     "/api/v1/auth",
		Expires:  refreshExpiresAt,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})

	// Set httpOnly cookie for access token (used by WebSocket/SSE to avoid
	// exposing tokens in URL query parameters, which are logged by proxies
	// and visible in browser history).
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "paryty_access_token",
		Value:    resp.AccessToken,
		Path:     "/",
		Expires:  accessExpiresAt,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})

	// Remove refresh token from response body (now in httpOnly cookie).
	delete(body, "refreshToken")

	c.JSON(http.StatusOK, body)
}

// RefreshToken handles POST /api/v1/auth/refresh.
func (a *AuthRESTAdapter) RefreshToken(c *gin.Context) {
	var req refreshBind
	
	// Try to get refresh token from httpOnly cookie first, then from request body.
	cookie, err := c.Cookie("paryty_refresh_token")
	if err == nil && cookie != "" {
		req.RefreshToken = cookie
	} else if err := c.ShouldBindJSON(&req); err != nil {
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

	// Set httpOnly cookie for new refresh token.
	refreshExpiresAt := now.Add(RefreshTokenTTL)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "paryty_refresh_token",
		Value:    resp.RefreshToken,
		Path:     "/api/v1/auth",
		Expires:  refreshExpiresAt,
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})

	// Remove refresh token from response body.
	delete(body, "refreshToken")

	c.JSON(http.StatusOK, body)
}

// Logout handles POST /api/v1/auth/logout.
func (a *AuthRESTAdapter) Logout(c *gin.Context) {
	var req logoutBind
	
	// Try to get refresh token from httpOnly cookie first, then from request body.
	cookie, err := c.Cookie("paryty_refresh_token")
	if err == nil && cookie != "" {
		req.RefreshToken = cookie
	} else if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "refresh_token is required"})
		return
	}

	protoReq := &parytyv1.LogoutRequest{
		RefreshToken: req.RefreshToken,
	}

	_, err = a.handler.Logout(c.Request.Context(), protoReq)
	if err != nil {
		c.JSON(grpcCodeToHTTP(err), gin.H{"message": status.Convert(err).Message()})
		return
	}

	// Clear the httpOnly cookie.
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "paryty_refresh_token",
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1, // Delete cookie
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	})

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

// ChangePassword handles POST /api/v1/auth/change-password.
func (a *AuthRESTAdapter) ChangePassword(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "authentication required"})
		return
	}

	var req changePasswordBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}

	// Validate new password against policy.
	if err := ValidatePasswordPolicy(req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	// Fetch current password hash from DB.
	var currentHash string
	err := a.db.QueryRow(c.Request.Context(),
		`SELECT password_hash FROM paryty_users WHERE id = $1`, userID,
	).Scan(&currentHash)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to verify current password"})
		return
	}

	// Verify current password.
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(req.CurrentPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "current password is incorrect"})
		return
	}

	// Hash new password and update DB.
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to process new password"})
		return
	}

	_, err = a.db.Exec(c.Request.Context(),
		`UPDATE paryty_users SET password_hash = $1, updated_at = NOW() WHERE id = $2`,
		string(newHash), userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to update password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password changed successfully"})
}

// VerifyEmail handles POST /api/v1/auth/verify-email.
// Validates the email verification token and marks the user's email as verified.
func (a *AuthRESTAdapter) VerifyEmail(c *gin.Context) {
	var req verifyEmailBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "token required"})
		return
	}

	tokenHash := HashRefreshToken(req.Token)
	var userID string
	err := a.db.QueryRow(c.Request.Context(), `
		SELECT user_id FROM email_verifications
		WHERE token_hash = $1 AND expires_at > NOW()
	`, tokenHash).Scan(&userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid or expired verification token"})
		return
	}

	// Mark user as verified.
	_, err = a.db.Exec(c.Request.Context(), `
		UPDATE users SET email_verified = TRUE, updated_at = NOW() WHERE id = $1
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to verify email"})
		return
	}

	// Delete used token (single-use).
	_, _ = a.db.Exec(c.Request.Context(), `DELETE FROM email_verifications WHERE token_hash = $1`, tokenHash)

	c.JSON(http.StatusOK, gin.H{"message": "email verified successfully"})
}

// ForgotPassword handles POST /api/v1/auth/forgot-password.
// Always returns success to prevent email enumeration attacks.
func (a *AuthRESTAdapter) ForgotPassword(c *gin.Context) {
	var req forgotPasswordBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "valid email required"})
		return
	}

	// Always return success to prevent email enumeration.
	defer c.JSON(http.StatusOK, gin.H{
		"message": "If an account with that email exists, a reset link has been sent.",
	})

	// Look up user by email.
	var userID string
	err := a.db.QueryRow(c.Request.Context(), `
		SELECT id FROM users WHERE email = $1
	`, req.Email).Scan(&userID)
	if err != nil {
		return // User not found — don't reveal.
	}

	// Generate reset token.
	resetToken := generateTokenID()
	resetHash := HashRefreshToken(resetToken)
	_, err = a.db.Exec(c.Request.Context(), `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '1 hour')
	`, userID, resetHash)
	if err != nil {
		return // Non-fatal.
	}

	// TODO(paryty#auth): Send email with reset link containing resetToken.
	// In production, this would be sent via SendGrid/AWS SES.
}

// ResetPassword handles POST /api/v1/auth/reset-password.
// Validates the password reset token, updates the password, and revokes all
// existing refresh tokens to force re-authentication.
func (a *AuthRESTAdapter) ResetPassword(c *gin.Context) {
	var req resetPasswordBind
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "token and newPassword required"})
		return
	}

	// Validate password policy.
	if err := ValidatePasswordPolicy(req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	tokenHash := HashRefreshToken(req.Token)
	var userID string
	err := a.db.QueryRow(c.Request.Context(), `
		SELECT user_id FROM password_reset_tokens
		WHERE token_hash = $1 AND expires_at > NOW()
	`, tokenHash).Scan(&userID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid or expired reset token"})
		return
	}

	// Hash new password.
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to process password"})
		return
	}

	// Update password.
	_, err = a.db.Exec(c.Request.Context(), `
		UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2
	`, string(newHash), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to update password"})
		return
	}

	// Revoke all refresh tokens (force re-authentication).
	_, _ = a.db.Exec(c.Request.Context(), `
		UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)

	// Delete used token (single-use).
	_, _ = a.db.Exec(c.Request.Context(), `DELETE FROM password_reset_tokens WHERE token_hash = $1`, tokenHash)

	c.JSON(http.StatusOK, gin.H{"message": "password reset successfully"})
}
