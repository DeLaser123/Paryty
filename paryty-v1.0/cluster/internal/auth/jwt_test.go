package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestNewTokenManager_ValidSecret(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("super-secret-key-at-least-32-bytes!!"))
	tm, err := NewTokenManager(secret)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tm == nil {
		t.Fatal("expected non-nil TokenManager")
	}
}

func TestNewTokenManager_ShortSecret(t *testing.T) {
	_, err := NewTokenManager([]byte("too-short"))
	if err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestNewTokenManager_Exact32Bytes(t *testing.T) {
	secret := make([]byte, 32)
	tm, err := NewTokenManager(secret)
	if err != nil {
		t.Fatalf("expected 32-byte secret to be accepted, got %v", err)
	}
	if tm == nil {
		t.Fatal("expected non-nil TokenManager")
	}
}

func TestGeneratePair_ProducesValidTokens(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("test-secret-that-is-32-bytes-long!"))
	tm, _ := NewTokenManager(secret)

	access, refresh, accessExp, refreshExp, err := tm.GeneratePair(
		"user-123", "tenant-abc", "pro", "admin", map[string]bool{"twins:read": true},
	)
	if err != nil {
		t.Fatalf("GeneratePair failed: %v", err)
	}
	if access == "" {
		t.Error("access token must not be empty")
	}
	if refresh == "" {
		t.Error("refresh token must not be empty")
	}
	if len(refresh) != 64 { // 32 bytes hex-encoded = 64 chars
		t.Errorf("refresh token hex length: expected 64, got %d", len(refresh))
	}

	// Verify timestamps are in the future
	now := time.Now().UTC()
	if !accessExp.After(now) {
		t.Error("access expiry should be in the future")
	}
	if !refreshExp.After(now) {
		t.Error("refresh expiry should be in the future")
	}
	if !refreshExp.After(accessExp) {
		t.Error("refresh expiry should be after access expiry")
	}

	// Verify TTLs
	if accessExp.Sub(now) > AccessTokenTTL+time.Second {
		t.Error("access expiry should be ~15 minutes")
	}
	if refreshExp.Sub(now) > RefreshTokenTTL+time.Second {
		t.Error("refresh expiry should be ~7 days")
	}
}

func TestGeneratePair_ValidateAccess_RoundTrip(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("test-secret-that-is-32-bytes-long!"))
	tm, _ := NewTokenManager(secret)

	access, _, _, _, err := tm.GeneratePair(
		"user-456", "tenant-xyz", "basic", "viewer", map[string]bool{"twins:read": true},
	)
	if err != nil {
		t.Fatalf("GeneratePair failed: %v", err)
	}

	claims, err := tm.ValidateAccess(access)
	if err != nil {
		t.Fatalf("ValidateAccess failed: %v", err)
	}

	if claims.TenantID != "tenant-xyz" {
		t.Errorf("tenant: expected 'tenant-xyz', got %q", claims.TenantID)
	}
	if claims.PlanName != "basic" {
		t.Errorf("plan: expected 'basic', got %q", claims.PlanName)
	}
	if claims.Role != "viewer" {
		t.Errorf("role: expected 'viewer', got %q", claims.Role)
	}
	if claims.Subject != "user-456" {
		t.Errorf("subject: expected 'user-456', got %q", claims.Subject)
	}
	if !claims.Permissions["twins:read"] {
		t.Error("expected twins:read permission to be true")
	}
}

func TestValidateAccess_InvalidToken(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("test-secret-that-is-32-bytes-long!"))
	tm, _ := NewTokenManager(secret)

	_, err := tm.ValidateAccess("not.a.valid.token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestValidateAccess_EmptyToken(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("test-secret-that-is-32-bytes-long!"))
	tm, _ := NewTokenManager(secret)

	_, err := tm.ValidateAccess("")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestValidateAccess_WrongSecret(t *testing.T) {
	secret1 := make([]byte, 32)
	copy(secret1, []byte("secret-number-one-32-bytes-long!!"))
	tm1, _ := NewTokenManager(secret1)

	access, _, _, _, _ := tm1.GeneratePair(
		"user-1", "tenant-1", "pro", "admin", map[string]bool{},
	)

	// Validate with different secret
	secret2 := make([]byte, 32)
	copy(secret2, []byte("secret-number-two-32-bytes-long!!"))
	tm2, _ := NewTokenManager(secret2)

	_, err := tm2.ValidateAccess(access)
	if err == nil {
		t.Fatal("expected error when validating with wrong secret")
	}
}

func TestValidateAccess_ExpiredToken(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("test-secret-that-is-32-bytes-long!"))
	tm, _ := NewTokenManager(secret)

	// Create an already-expired token
	now := time.Now().UTC().Add(-1 * time.Hour)
	claims := &AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			NotBefore: jwt.NewNumericDate(now),
		},
		TenantID: "tenant-1",
		PlanName: "basic",
		Role:     "viewer",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expiredToken, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign expired token: %v", err)
	}

	_, err = tm.ValidateAccess(expiredToken)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestHashRefreshToken(t *testing.T) {
	hash1 := HashRefreshToken("test-refresh-token-1")
	hash2 := HashRefreshToken("test-refresh-token-1")
	hash3 := HashRefreshToken("different-token")

	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different inputs should produce different hashes")
	}
	if len(hash1) != 64 { // SHA-256 = 32 bytes = 64 hex chars
		t.Errorf("expected 64-char hex hash, got %d", len(hash1))
	}
}

func TestVerifyRefreshToken(t *testing.T) {
	raw := "my-secret-refresh-token-value"
	hash := HashRefreshToken(raw)

	if !VerifyRefreshToken(raw, hash) {
		t.Error("expected verification to succeed")
	}
}

func TestVerifyRefreshToken_WrongToken(t *testing.T) {
	raw := "correct-token"
	hash := HashRefreshToken(raw)

	if VerifyRefreshToken("wrong-token", hash) {
		t.Error("expected verification to fail for wrong token")
	}
}

func TestVerifyRefreshToken_InvalidHash(t *testing.T) {
	if VerifyRefreshToken("anything", "not-valid-hex") {
		t.Error("expected verification to fail for invalid hash")
	}
}

func TestVerifyRefreshToken_EmptyValues(t *testing.T) {
	if VerifyRefreshToken("", "") {
		t.Error("expected verification to fail for empty values")
	}
}

func TestGeneratePair_MultiplePairsUnique(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("test-secret-that-is-32-bytes-long!"))
	tm, _ := NewTokenManager(secret)

	pairs := make(map[string]bool)
	for i := 0; i < 100; i++ {
		_, refresh, _, _, err := tm.GeneratePair(
			"user-1", "tenant-1", "pro", "admin", nil,
		)
		if err != nil {
			t.Fatalf("pair %d failed: %v", i, err)
		}
		if pairs[refresh] {
			t.Fatalf("duplicate refresh token generated at pair %d", i)
		}
		pairs[refresh] = true
	}
}

func TestValidateAccess_AlgorithmConfusion(t *testing.T) {
	secret := make([]byte, 32)
	copy(secret, []byte("test-secret-that-is-32-bytes-long!"))
	tm, _ := NewTokenManager(secret)

	// Create a token with "none" algorithm — should be rejected
	token := jwt.NewWithClaims(jwt.SigningMethodNone, &AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:  "user-1",
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
		TenantID: "tenant-1",
	})
	noneToken, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	_, err := tm.ValidateAccess(noneToken)
	if err == nil {
		t.Fatal("expected error for 'none' algorithm token")
	}
}
