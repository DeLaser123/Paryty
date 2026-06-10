// Package auth implements JWT token management, password hashing, and
// authentication middleware for the Paryty multi-tenant platform.
//
// Token flow:
//  1. Login/Register → GeneratePair() returns access_token (15min) + refresh_token (7d)
//  2. API calls → Bearer access_token in Authorization header
//  3. Access expires → client sends refresh_token to /auth/refresh
//  4. Refresh → GeneratePair() again, old refresh_token revoked
//  5. Logout → refresh_token revoked, client discards both tokens
//
// Access tokens are NOT stored — they are validated purely via HMAC signature.
// Refresh tokens are hashed (SHA-256) and stored in PostgreSQL for revocation.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// =============================================================================
// Configuration
// =============================================================================

const (
	// AccessTokenTTL is the lifetime of an access token.
	AccessTokenTTL = 15 * time.Minute

	// RefreshTokenTTL is the lifetime of a refresh token.
	RefreshTokenTTL = 7 * 24 * time.Hour

	// RefreshTokenByteLen is the length of raw refresh token bytes before hex encoding.
	RefreshTokenByteLen = 32
)

// =============================================================================
// Claims
// =============================================================================

// AccessClaims represents the JWT claims embedded in an access token.
type AccessClaims struct {
	jwt.RegisteredClaims
	TenantID    string            `json:"tenant_id"`
	PlanName    string            `json:"plan_name"`
	Role        string            `json:"role"`
	Permissions map[string]bool   `json:"permissions"`
}

// =============================================================================
// TokenManager
// =============================================================================

// TokenManager creates and validates JWT access tokens and refresh tokens.
// Access tokens are HMAC-signed and validated without database lookups.
// Refresh tokens are opaque random strings, hashed with SHA-256 for storage.
type TokenManager struct {
	secret []byte // HMAC-SHA256 signing key (at least 32 bytes)
}

// NewTokenManager creates a TokenManager with the given HS256 secret.
// The secret must be at least 32 bytes (256 bits) for HS256 compliance.
func NewTokenManager(secret []byte) (*TokenManager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT secret must be at least 32 bytes (got %d)", len(secret))
	}
	return &TokenManager{secret: secret}, nil
}

// GeneratePair creates a new access + refresh token pair for the given user.
// Returns the raw tokens (not hashed). The caller is responsible for:
//   - Returning the raw tokens to the client (once!)
//   - Hashing the refresh token and storing it in the database
func (tm *TokenManager) GeneratePair(userID, tenantID, planName, role string, permissions map[string]bool) (accessToken, refreshToken string, accessExpiresAt time.Time, refreshExpiresAt time.Time, err error) {
	now := time.Now().UTC()
	accessExpiresAt = now.Add(AccessTokenTTL)
	refreshExpiresAt = now.Add(RefreshTokenTTL)

	// Access token: signed JWT
	claims := &AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExpiresAt),
			NotBefore: jwt.NewNumericDate(now),
			ID:        generateTokenID(),
		},
		TenantID:    tenantID,
		PlanName:    planName,
		Role:        role,
		Permissions: permissions,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err = token.SignedString(tm.secret)
	if err != nil {
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("sign access token: %w", err)
	}

	// Refresh token: cryptographically random opaque string
	refreshBytes := make([]byte, RefreshTokenByteLen)
	if _, err := rand.Read(refreshBytes); err != nil {
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshToken = hex.EncodeToString(refreshBytes)

	return accessToken, refreshToken, accessExpiresAt, refreshExpiresAt, nil
}

// ValidateAccess parses and validates an access token. Returns the embedded
// claims if the token is valid. Returns an error if the token is expired,
// malformed, or has an invalid signature.
func (tm *TokenManager) ValidateAccess(tokenString string) (*AccessClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AccessClaims{}, func(t *jwt.Token) (interface{}, error) {
		// Enforce signing algorithm.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return tm.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse access token: %w", err)
	}

	claims, ok := token.Claims.(*AccessClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid access token claims")
	}

	return claims, nil
}

// HashRefreshToken returns the SHA-256 hex hash of a refresh token for
// secure storage in PostgreSQL.
func HashRefreshToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// VerifyRefreshToken compares a raw refresh token against a stored hash
// using constant-time comparison to prevent timing attacks.
func VerifyRefreshToken(rawToken, storedHash string) bool {
	computed := sha256.Sum256([]byte(rawToken))
	expected, err := hex.DecodeString(storedHash)
	if err != nil {
		return false
	}
	return hmac.Equal(computed[:], expected)
}

// =============================================================================
// helpers
// =============================================================================

func generateTokenID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
