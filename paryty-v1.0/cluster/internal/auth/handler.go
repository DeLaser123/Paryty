package auth

import (
	"context"
	"errors"
	"net/mail"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthHandler implements the AuthService gRPC server defined in auth.proto.
// It handles user registration, login, token refresh, logout, and token validation.
type AuthHandler struct {
	parytyv1.UnimplementedAuthServiceServer
	tm        *TokenManager
	db        *pgxpool.Pool
	engine    *plan.PlanEngine
	loginRate *loginRateLimiter
	logger    *zap.Logger
}

// loginRateLimiter provides per-email brute-force protection for login.
// The attempts map is bounded: when it reaches maxTrackedKeys, expired
// entries are pruned; if still full, the new attempt is allowed but not
// tracked (fail-open for availability — bcrypt cost still throttles).
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginWindow
}

type loginWindow struct {
	failures     int
	windowEndsAt time.Time
	blockedUntil time.Time
}

const (
	// maxLoginFailures is how many failed attempts are permitted within a
	// window before the key is blocked. The Nth failure is still processed;
	// the (N+1)th attempt is rejected.
	maxLoginFailures = 5
	loginWindowSize  = 15 * time.Minute
	loginBlockTime   = 15 * time.Minute

	// maxTrackedKeys bounds the rate limiter map to prevent memory
	// exhaustion from attackers spraying random emails.
	maxTrackedKeys = 100_000
)

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		attempts: make(map[string]*loginWindow),
	}
}

// allow returns true if a login attempt is permitted for the given key (email).
// It counts the attempt as a failure up front; recordSuccess clears the state
// when the login succeeds.
func (l *loginRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	w, tracked := l.attempts[key]

	// Expired window or expired block → start fresh.
	if tracked && now.After(w.windowEndsAt) && now.After(w.blockedUntil) {
		delete(l.attempts, key)
		tracked = false
	}

	if !tracked {
		if len(l.attempts) >= maxTrackedKeys {
			l.pruneExpiredLocked(now)
		}
		if len(l.attempts) >= maxTrackedKeys {
			// Map still full after pruning: allow without tracking rather
			// than denying service to every untracked legitimate user.
			return true
		}
		l.attempts[key] = &loginWindow{
			failures:     1,
			windowEndsAt: now.Add(loginWindowSize),
		}
		return true
	}

	// Currently blocked.
	if now.Before(w.blockedUntil) {
		return false
	}

	w.failures++
	if w.failures > maxLoginFailures {
		w.blockedUntil = now.Add(loginBlockTime)
		return false
	}
	return true
}

// pruneExpiredLocked removes entries whose window and block have both passed.
// Caller must hold l.mu.
func (l *loginRateLimiter) pruneExpiredLocked(now time.Time) {
	for key, w := range l.attempts {
		if now.After(w.windowEndsAt) && now.After(w.blockedUntil) {
			delete(l.attempts, key)
		}
	}
}

// recordSuccess clears rate limit state for a key after successful login.
func (l *loginRateLimiter) recordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// NewAuthHandler creates a new AuthHandler. A nil logger is replaced with
// zap.NewNop() so callers without logging configured remain safe.
func NewAuthHandler(tm *TokenManager, db *pgxpool.Pool, engine *plan.PlanEngine, logger *zap.Logger) *AuthHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AuthHandler{
		tm:        tm,
		db:        db,
		engine:    engine,
		loginRate: newLoginRateLimiter(),
		logger:    logger,
	}
}

// Register creates a new tenant + admin user account and returns access + refresh tokens.
func (h *AuthHandler) Register(ctx context.Context, req *parytyv1.RegisterRequest) (*parytyv1.RegisterResponse, error) {
	// Validate email.
	if req.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return nil, status.Error(codes.InvalidArgument, "email is not a valid address")
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
	// Tenant + user already exist; on failure we log loudly and continue —
	// the user can still log in and the plan can be assigned later.
	if err := h.engine.AssignPlan(ctx, tenantID, planName); err != nil {
		h.logger.Error("plan assignment failed during registration; tenant has no plan row",
			zap.String("tenant_id", tenantID),
			zap.String("plan_name", planName),
			zap.Error(err))
	}

	// Generate tokens.
	accessToken, refreshToken, _, refreshExpiresAt, err := h.tm.GeneratePair(
		userID, tenantID, planName, "admin", buildPermissions("admin"))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate tokens: %v", err)
	}

	// Store refresh token hash.
	if err := h.storeRefreshToken(ctx, userID, refreshToken, refreshExpiresAt); err != nil {
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
		if errors.Is(err, pgx.ErrNoRows) {
			// Burn the same bcrypt cost as a real verification so response
			// timing does not reveal whether the email exists.
			_ = VerifyPassword(req.Password, dummyBcryptHash)
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

	// Build permission map from the single role→permissions source of truth.
	permissions := buildPermissions(role)

	// Generate tokens.
	accessToken, refreshToken, _, refreshExpiresAt, err := h.tm.GeneratePair(
		userID, tenantID, planName, role, permissions)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate tokens: %v", err)
	}

	// Store refresh token hash.
	if err := h.storeRefreshToken(ctx, userID, refreshToken, refreshExpiresAt); err != nil {
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
		}
		return nil, status.Errorf(codes.Internal, "lookup refresh token: %v", err)
	}

	// Reuse detection: a revoked token being presented again indicates the
	// token was stolen (the legitimate client already rotated it). Revoke
	// the user's entire refresh-token family to force re-authentication.
	if revokedAt != nil {
		if _, revokeErr := h.db.Exec(ctx, `
			UPDATE refresh_tokens SET revoked_at = now()
			WHERE user_id = $1 AND revoked_at IS NULL
		`, userID); revokeErr != nil {
			h.logger.Error("failed to revoke token family after reuse detection",
				zap.String("user_id", userID), zap.Error(revokeErr))
		} else {
			h.logger.Warn("refresh token reuse detected; revoked all sessions for user",
				zap.String("user_id", userID))
		}
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
	if err := h.storeRefreshToken(ctx, userID, refreshToken, refreshExpiresAt); err != nil {
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

// dummyBcryptHash is a valid bcrypt hash (of a random throwaway value,
// generated once at startup with the production cost factor) used to
// equalize response timing when a login email does not exist, preventing
// user enumeration via timing analysis. Generating it at init guarantees
// it is well-formed; a malformed constant would make bcrypt return
// immediately and silently defeat the defense.
var dummyBcryptHash = func() string {
	h, err := HashPassword(generateTokenID())
	if err != nil {
		// HashPassword only fails on >72-byte input; a 32-char hex token
		// cannot trigger that. Fail loudly if the impossible happens.
		panic("auth: failed to generate timing-equalization hash: " + err.Error())
	}
	return h
}()

// storeRefreshToken hashes and persists a refresh token, and opportunistically
// deletes the user's expired or long-revoked tokens so the table does not
// grow without bound.
func (h *AuthHandler) storeRefreshToken(ctx context.Context, userID, refreshToken string, expiresAt time.Time) error {
	refreshHash := HashRefreshToken(refreshToken)
	if _, err := h.db.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, refreshHash, expiresAt); err != nil {
		return err
	}

	// Best-effort cleanup; failure must not fail the auth flow.
	// Revoked tokens are retained 24h so reuse detection still fires.
	if _, err := h.db.Exec(ctx, `
		DELETE FROM refresh_tokens
		WHERE user_id = $1
		  AND (expires_at < now() OR revoked_at < now() - interval '24 hours')
	`, userID); err != nil {
		h.logger.Warn("refresh token cleanup failed", zap.String("user_id", userID), zap.Error(err))
	}
	return nil
}

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

// isUniqueViolation checks if a PostgreSQL error is a unique constraint
// violation by inspecting the SQLSTATE code (23505). errors.As unwraps any
// wrapping pgx applies.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
