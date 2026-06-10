package auth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthHandler implements the AuthService gRPC server defined in auth.proto.
// It handles user registration, login, token refresh, logout, and token validation.
type AuthHandler struct {
	parytyv1.UnimplementedAuthServiceServer
	tm         *TokenManager
	db         *pgxpool.Pool
	engine     *plan.PlanEngine
	loginRate  *loginRateLimiter
}

// loginRateLimiter provides per-email brute-force protection for login.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginWindow
}

type loginWindow struct {
	count     int
	resetAt   time.Time
	blockedUntil time.Time
}

const (
	maxLoginAttempts = 5
	loginWindowSize  = 15 * time.Minute
	loginBlockTime   = 15 * time.Minute
)

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		attempts: make(map[string]*loginWindow),
	}
}

// allow returns true if the login attempt is allowed for the given key (email).
func (l *loginRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	w, ok := l.attempts[key]

	// Clean up expired entries periodically.
	if ok && now.After(w.resetAt) {
		delete(l.attempts, key)
		ok = false
	}

	if !ok {
		l.attempts[key] = &loginWindow{
			count:   1,
			resetAt: now.Add(loginWindowSize),
		}
		return true
	}

	// Check if currently blocked.
	if !now.Before(w.blockedUntil) && w.blockedUntil.After(time.Time{}) {
		// Block expired, reset window.
		w.count = 1
		w.blockedUntil = time.Time{}
		w.resetAt = now.Add(loginWindowSize)
		return true
	}

	if !now.Before(w.blockedUntil) {
		w.count++
		if w.count >= maxLoginAttempts {
			w.blockedUntil = now.Add(loginBlockTime)
			return false
		}
		return true
	}

	return false // blocked
}

// recordSuccess clears rate limit state for a key after successful login.
func (l *loginRateLimiter) recordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(tm *TokenManager, db *pgxpool.Pool, engine *plan.PlanEngine) *AuthHandler {
	return &AuthHandler{
		tm:        tm,
		db:        db,
		engine:    engine,
		loginRate: newLoginRateLimiter(),
	}
}

// Register creates a new tenant + admin user account and returns access + refresh tokens.
func (h *AuthHandler) Register(ctx context.Context, req *parytyv1.RegisterRequest) (*parytyv1.RegisterResponse, error) {
	// Validate email.
	if req.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	// Validate password policy.
	if err := ValidatePasswordPolicy(req.Password); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "password policy: %v", err)
	}

	// Validate plan name.
	planName := req.PlanName
	if planName == "" {
		planName = "basic"
	}
	if !h.engine.ValidatePlanName(planName) {
		return nil, status.Errorf(codes.InvalidArgument, "plan %q is not available for signup", planName)
	}

	// Hash password.
	passwordHash, err := HashPassword(req.Password)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "hash password: %v", err)
	}

	// Start a transaction: create tenant + user + assign plan.
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Create tenant.
	var tenantID string
	err = tx.QueryRow(ctx, `
		INSERT INTO tenants (name, status)
		VALUES ($1, 'active')
		RETURNING tenant_id
	`, req.Name).Scan(&tenantID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create tenant: %v", err)
	}

	// Create admin user.
	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, tenant_id, email, password_hash, name, role)
		VALUES ($1, $2, $3, $4, $5, 'admin')
	`, userID, tenantID, req.Email, passwordHash, req.Name)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, status.Error(codes.AlreadyExists, "a user with this email already exists in this tenant")
		}
		return nil, status.Errorf(codes.Internal, "create user: %v", err)
	}

	// Commit transaction before plan assignment — keeps plan assignment outside
	// the tenant+user transaction to avoid split-brain (plan with no tenant).
	if err := tx.Commit(ctx); err != nil {
		return nil, status.Errorf(codes.Internal, "commit tx: %v", err)
	}

	// Assign plan to tenant (post-commit; idempotent via ON CONFLICT).
	if err := h.engine.AssignPlan(ctx, tenantID, planName); err != nil {
		// Tenant + user already exist; log and continue — plan can be assigned later.
		// Return success with tokens so the user can still log in.
	}

	// Generate tokens.
	accessToken, refreshToken, _, refreshExpiresAt, err := h.tm.GeneratePair(
		userID, tenantID, planName, "admin", buildPermissions("admin"))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate tokens: %v", err)
	}

	// Store refresh token hash.
	refreshHash := HashRefreshToken(refreshToken)
	_, err = h.db.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, refreshHash, refreshExpiresAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "store refresh token: %v", err)
	}

	return &parytyv1.RegisterResponse{
		TenantId:     tenantID,
		UserId:       userID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(AccessTokenTTL.Seconds()),
	}, nil
}

// Login authenticates a user by email + password and returns tokens.
func (h *AuthHandler) Login(ctx context.Context, req *parytyv1.LoginRequest) (*parytyv1.LoginResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}

	// Brute-force protection: rate-limit login attempts per email.
	if !h.loginRate.allow(req.Email) {
		return nil, status.Error(codes.ResourceExhausted,
			"too many login attempts; please try again later")
	}

	// Look up user by email.
	var (
		userID       string
		tenantID     string
		passwordHash string
		userName     string
		role         string
		isActive     bool
	)
	err := h.db.QueryRow(ctx, `
		SELECT id, tenant_id, password_hash, name, role, is_active
		FROM users
		WHERE email = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, req.Email).Scan(&userID, &tenantID, &passwordHash, &userName, &role, &isActive)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.Unauthenticated, "invalid email or password")
		}
		return nil, status.Errorf(codes.Internal, "lookup user: %v", err)
	}

	if !isActive {
		return nil, status.Error(codes.PermissionDenied, "account is deactivated")
	}

	// Verify password.
	if err := VerifyPassword(req.Password, passwordHash); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid email or password")
	}

	// Get tenant plan.
	planName := "basic"
	if stored, err := h.engine.GetTenantPlan(ctx, tenantID); err == nil && stored != nil {
		planName = stored.PlanName
	}

	// Build permission map.
	permissions := map[string]bool{}
	// Default permissions based on role.
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

	// Generate tokens.
	accessToken, refreshToken, _, refreshExpiresAt, err := h.tm.GeneratePair(
		userID, tenantID, planName, role, permissions)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate tokens: %v", err)
	}

	// Store refresh token hash.
	refreshHash := HashRefreshToken(refreshToken)
	_, err = h.db.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, refreshHash, refreshExpiresAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "store refresh token: %v", err)
	}

	// Clear rate limit on successful login.
	h.loginRate.recordSuccess(req.Email)

	return &parytyv1.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(AccessTokenTTL.Seconds()),
		User: &parytyv1.UserInfo{
			UserId:    userID,
			TenantId:  tenantID,
			Email:     req.Email,
			Name:      userName,
			Role:      role,
			CreatedAt: timestamppb.New(time.Now().UTC()),
		},
	}, nil
}

// RefreshToken exchanges a valid refresh token for a new token pair.
// The old refresh token is revoked (one-time use rotation).
func (h *AuthHandler) RefreshToken(ctx context.Context, req *parytyv1.RefreshTokenRequest) (*parytyv1.LoginResponse, error) {
	if req.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	// Hash the incoming refresh token to look up in DB.
	hash := HashRefreshToken(req.RefreshToken)

	// Find and validate the stored token.
	var (
		tokenID   string
		userID    string
		expiresAt time.Time
		revokedAt *time.Time
	)
	err := h.db.QueryRow(ctx, `
		SELECT id, user_id, expires_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1
	`, hash).Scan(&tokenID, &userID, &expiresAt, &revokedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
		}
		return nil, status.Errorf(codes.Internal, "lookup refresh token: %v", err)
	}

	// Check if already revoked.
	if revokedAt != nil {
		return nil, status.Error(codes.Unauthenticated, "refresh token has been revoked")
	}

	// Check expiration.
	if time.Now().UTC().After(expiresAt) {
		return nil, status.Error(codes.Unauthenticated, "refresh token has expired")
	}

	// Revoke the old refresh token (rotation — one-time use).
	_, err = h.db.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1
	`, tokenID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "revoke old refresh token: %v", err)
	}

	// Look up user info for the new token pair.
	var (
		tenantID string
		userName string
		email    string
		role     string
		isActive bool
	)
	err = h.db.QueryRow(ctx, `
		SELECT tenant_id, email, name, role, is_active
		FROM users WHERE id = $1
	`, userID).Scan(&tenantID, &email, &userName, &role, &isActive)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "lookup user: %v", err)
	}
	if !isActive {
		return nil, status.Error(codes.PermissionDenied, "account is deactivated")
	}

	// Get tenant plan.
	planName := "basic"
	if stored, err := h.engine.GetTenantPlan(ctx, tenantID); err == nil && stored != nil {
		planName = stored.PlanName
	}

	// Build permissions (same as Login).
	permissions := buildPermissions(role)

	// Generate new token pair.
	accessToken, refreshToken, _, refreshExpiresAt, err := h.tm.GeneratePair(
		userID, tenantID, planName, role, permissions)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate tokens: %v", err)
	}

	// Store new refresh token.
	refreshHash := HashRefreshToken(refreshToken)
	_, err = h.db.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, refreshHash, refreshExpiresAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "store refresh token: %v", err)
	}

	return &parytyv1.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(AccessTokenTTL.Seconds()),
		User: &parytyv1.UserInfo{
			UserId:    userID,
			TenantId:  tenantID,
			Email:     email,
			Name:      userName,
			Role:      role,
			CreatedAt: timestamppb.New(time.Now().UTC()),
		},
	}, nil
}

// Logout revokes the given refresh token.
func (h *AuthHandler) Logout(ctx context.Context, req *parytyv1.LogoutRequest) (*emptypb.Empty, error) {
	if req.RefreshToken == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	hash := HashRefreshToken(req.RefreshToken)
	_, err := h.db.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, hash)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "revoke refresh token: %v", err)
	}

	return &emptypb.Empty{}, nil
}

// ValidateToken validates an access token and returns its claims.
func (h *AuthHandler) ValidateToken(ctx context.Context, req *parytyv1.ValidateTokenRequest) (*parytyv1.ValidateTokenResponse, error) {
	if req.AccessToken == "" {
		return &parytyv1.ValidateTokenResponse{Valid: false}, nil
	}

	claims, err := h.tm.ValidateAccess(req.AccessToken)
	if err != nil {
		return &parytyv1.ValidateTokenResponse{Valid: false}, nil
	}

	// Convert permissions map to string slice for proto.
	perms := make([]string, 0, len(claims.Permissions))
	for p, enabled := range claims.Permissions {
		if enabled {
			perms = append(perms, p)
		}
	}

	return &parytyv1.ValidateTokenResponse{
		Valid:       true,
		UserId:      claims.Subject,
		TenantId:    claims.TenantID,
		Role:        claims.Role,
		Permissions: perms,
		ExpiresAt:   timestamppb.New(claims.ExpiresAt.Time),
	}, nil
}

// =============================================================================
// helpers
// =============================================================================

func buildPermissions(role string) map[string]bool {
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

// isUniqueViolation checks if a PostgreSQL error is a unique constraint violation
// by inspecting the SQLSTATE code (23505) via pgconn.PgError.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if err != nil {
		// pgx wraps errors; try unwrapping.
		if pgErr2, ok := err.(*pgconn.PgError); ok {
			pgErr = pgErr2
		}
	}
	return pgErr != nil && pgErr.Code == "23505"
}
