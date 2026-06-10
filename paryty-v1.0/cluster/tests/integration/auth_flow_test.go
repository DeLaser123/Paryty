//go:build integration

// Package integration contains integration tests for the Paryty Cluster.
// These tests exercise cross-component flows and validate roundtrip
// correctness of auth, twin, and plan subsystems without requiring
// a live database connection.
package integration

import (
	"testing"

	"github.com/paryty/paryty-v1.0/cluster/internal/auth"
)

// =============================================================================
// TokenManager: GeneratePair → ValidateAccess Roundtrip
// =============================================================================

func TestTokenManager_GeneratePair_ValidateAccess_Roundtrip(t *testing.T) {
	// Use a well-known secret for deterministic testing.
	secret := []byte("paryty-integration-test-secret-32b!!")
	tm, err := auth.NewTokenManager(secret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	accessToken, refreshToken, accessExp, refreshExp, err := tm.GeneratePair(
		"user-123",
		"tenant-abc",
		"pro",
		"admin",
		map[string]bool{
			"twins:read":  true,
			"twins:write": true,
			"users:read":  true,
			"users:write": true,
		},
	)
	if err != nil {
		t.Fatalf("GeneratePair: %v", err)
	}

	// Verify tokens are non-empty.
	if accessToken == "" {
		t.Error("accessToken must not be empty")
	}
	if refreshToken == "" {
		t.Error("refreshToken must not be empty")
	}
	if accessExp.IsZero() {
		t.Error("accessExpiresAt must not be zero")
	}
	if refreshExp.IsZero() {
		t.Error("refreshExpiresAt must not be zero")
	}

	// ValidateAccess roundtrip.
	claims, err := tm.ValidateAccess(accessToken)
	if err != nil {
		t.Fatalf("ValidateAccess: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("Subject: expected 'user-123', got %q", claims.Subject)
	}
	if claims.TenantID != "tenant-abc" {
		t.Errorf("TenantID: expected 'tenant-abc', got %q", claims.TenantID)
	}
	if claims.PlanName != "pro" {
		t.Errorf("PlanName: expected 'pro', got %q", claims.PlanName)
	}
	if claims.Role != "admin" {
		t.Errorf("Role: expected 'admin', got %q", claims.Role)
	}
	if !claims.Permissions["twins:read"] {
		t.Error("expected twins:read permission")
	}
	if !claims.Permissions["twins:write"] {
		t.Error("expected twins:write permission")
	}
	if !claims.Permissions["users:read"] {
		t.Error("expected users:read permission")
	}
	if !claims.Permissions["users:write"] {
		t.Error("expected users:write permission")
	}
}

// =============================================================================
// TokenManager: Different Roles/Permissions
// =============================================================================

func TestTokenManager_GeneratePair_DifferentRoles(t *testing.T) {
	secret := []byte("paryty-integration-test-secret-32b!!")
	tm, err := auth.NewTokenManager(secret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	tests := []struct {
		name        string
		role        string
		permissions map[string]bool
		wantPerms   []string
		unwanted    []string
	}{
		{
			name: "admin",
			role: "admin",
			permissions: map[string]bool{
				"tenants:read":  true,
				"tenants:write": true,
				"users:read":    true,
				"users:write":   true,
				"twins:read":    true,
				"twins:write":   true,
			},
			wantPerms: []string{"tenants:read", "tenants:write", "users:read", "users:write", "twins:read", "twins:write"},
			unwanted:  []string{"alerts:acknowledge"},
		},
		{
			name: "operator",
			role: "operator",
			permissions: map[string]bool{
				"twins:read":         true,
				"twins:write":        true,
				"alerts:acknowledge": true,
			},
			wantPerms: []string{"twins:read", "twins:write", "alerts:acknowledge"},
			unwanted:  []string{"tenants:read", "tenants:write", "users:read", "users:write"},
		},
		{
			name: "viewer",
			role: "viewer",
			permissions: map[string]bool{
				"twins:read": true,
			},
			wantPerms: []string{"twins:read"},
			unwanted:  []string{"twins:write", "tenants:read", "alerts:acknowledge"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accessToken, _, _, _, err := tm.GeneratePair(
				"user-123", "tenant-abc", "pro", tt.role, tt.permissions,
			)
			if err != nil {
				t.Fatalf("GeneratePair: %v", err)
			}

			claims, err := tm.ValidateAccess(accessToken)
			if err != nil {
				t.Fatalf("ValidateAccess: %v", err)
			}

			if claims.Role != tt.role {
				t.Errorf("Role: expected %q, got %q", tt.role, claims.Role)
			}

			for _, p := range tt.wantPerms {
				if !claims.Permissions[p] {
					t.Errorf("expected permission %q to be enabled", p)
				}
			}
			for _, p := range tt.unwanted {
				if claims.Permissions[p] {
					t.Errorf("permission %q should NOT be enabled for role %q", p, tt.role)
				}
			}
		})
	}
}

// =============================================================================
// TokenManager: ValidateAccess with Invalid Tokens
// =============================================================================

func TestTokenManager_ValidateAccess_InvalidTokens(t *testing.T) {
	secret := []byte("paryty-integration-test-secret-32b!!")
	tm, err := auth.NewTokenManager(secret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"empty token", "", true},
		{"malformed", "not-a-jwt", true},
		{"wrong signature", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", true},
		{"random garbage", "xyz.abc.123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tm.ValidateAccess(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAccess error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// TokenManager: ValidateAccess with Different Secret → Must Fail
// =============================================================================

func TestTokenManager_ValidateAccess_DifferentSecret(t *testing.T) {
	tm1, _ := auth.NewTokenManager([]byte("paryty-integration-test-secret-32b!!"))
	tm2, _ := auth.NewTokenManager([]byte("a-different-secret-key-32bytes!!!!!!"))

	accessToken, _, _, _, err := tm1.GeneratePair("user-123", "tenant-abc", "pro", "admin", nil)
	if err != nil {
		t.Fatalf("GeneratePair: %v", err)
	}

	_, err = tm2.ValidateAccess(accessToken)
	if err == nil {
		t.Error("expected validation to fail with a different secret")
	}
}

// =============================================================================
// HashRefreshToken → VerifyRefreshToken Roundtrip
// =============================================================================

func TestHashRefreshToken_VerifyRefreshToken_Roundtrip(t *testing.T) {
	rawToken := "abc123def456-random-refresh-token-value"
	hash := auth.HashRefreshToken(rawToken)

	if hash == "" {
		t.Error("hash must not be empty")
	}
	if hash == rawToken {
		t.Error("hash must differ from raw token")
	}

	// Valid roundtrip.
	if !auth.VerifyRefreshToken(rawToken, hash) {
		t.Error("VerifyRefreshToken should return true for matching token and hash")
	}

	// Wrong token.
	if auth.VerifyRefreshToken("wrong-token", hash) {
		t.Error("VerifyRefreshToken should return false for non-matching token")
	}

	// Wrong hash.
	if auth.VerifyRefreshToken(rawToken, "deadbeef") {
		t.Error("VerifyRefreshToken should return false for non-matching hash")
	}

	// Empty token.
	if auth.VerifyRefreshToken("", hash) {
		t.Error("VerifyRefreshToken should return false for empty token")
	}

	// Invalid hex hash.
	if auth.VerifyRefreshToken(rawToken, "not-valid-hex-zzz") {
		t.Error("VerifyRefreshToken should return false for invalid hex hash")
	}
}

// =============================================================================
// TokenManager: Secret Validation
// =============================================================================

func TestTokenManager_NewTokenManager_SecretTooShort(t *testing.T) {
	_, err := auth.NewTokenManager([]byte("short"))
	if err == nil {
		t.Error("expected error for secret shorter than 32 bytes")
	}

	// 40-byte secret for safety margin.
	_, err = auth.NewTokenManager([]byte("this-is-exactly-40-bytes-long-my-key!!!"))
	if err != nil {
		t.Errorf("40-byte secret should be valid: %v", err)
	}
}

// =============================================================================
// HashPassword → VerifyPassword Roundtrip
// =============================================================================

func TestHashPassword_VerifyPassword_Roundtrip(t *testing.T) {
	password := "SecureP@ss1"

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Error("hash must not be empty")
	}
	if hash == password {
		t.Error("hash must differ from plaintext password")
	}

	// Verify correct password.
	if err := auth.VerifyPassword(password, hash); err != nil {
		t.Errorf("VerifyPassword should succeed: %v", err)
	}

	// Verify wrong password.
	if err := auth.VerifyPassword("WrongP@ss1", hash); err == nil {
		t.Error("VerifyPassword should fail for wrong password")
	}

	// Verify empty password.
	if err := auth.VerifyPassword("", hash); err == nil {
		t.Error("VerifyPassword should fail for empty password")
	}
}

func TestHashPassword_MaxLength(t *testing.T) {
	// Create a password longer than 72 bytes.
	longPassword := ""
	for i := 0; i < 73; i++ {
		longPassword += "a"
	}
	_, err := auth.HashPassword(longPassword)
	if err == nil {
		t.Error("expected error for password exceeding max length")
	}
}

// =============================================================================
// Password Policy Validation
// =============================================================================

func TestValidatePasswordPolicy_Success(t *testing.T) {
	validPasswords := []string{
		"SecureP@ss1",           // meets all requirements
		"C0mpl3x!Pass",          // mixed case, digits, special
		"MyP@ssw0rd",            // standard pattern
		"Abcdef1!",              // minimum viable
	}

	for _, pw := range validPasswords {
		t.Run(pw, func(t *testing.T) {
			if err := auth.ValidatePasswordPolicy(pw); err != nil {
				t.Errorf("expected %q to be valid: %v", pw, err)
			}
		})
	}
}

func TestValidatePasswordPolicy_Failures(t *testing.T) {
	tests := []struct {
		password string
		wantMsg  string
	}{
		{"short1!", "too short"},
		{"NoDigits!", "digit"},
		{"nodigitsspecial?", "uppercase"},
		{"NOLOWERCASE1!", "lowercase"},
		{"NoSpecial1", "special character"},
		{"", "at least 8"},
		{"NoSpec1", "special character"},
	}

	for _, tt := range tests {
		t.Run(tt.password, func(t *testing.T) {
			err := auth.ValidatePasswordPolicy(tt.password)
			if err == nil {
				t.Errorf("expected %q to fail with message containing %q", tt.password, tt.wantMsg)
			}
		})
	}
}

func TestValidatePasswordPolicy_TooLong(t *testing.T) {
	longPassword := ""
	for i := 0; i < 73; i++ {
		longPassword += "A"
	}
	longPassword += "a1!" // Now it has all character categories but exceeds length.

	err := auth.ValidatePasswordPolicy(longPassword)
	if err == nil {
		t.Error("expected error for password exceeding max length")
	}
}

// =============================================================================
// TokenManager: GeneratePair Uniqueness
// =============================================================================

func TestTokenManager_GeneratePair_Uniqueness(t *testing.T) {
	secret := []byte("paryty-integration-test-secret-32b!!")
	tm, err := auth.NewTokenManager(secret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}

	accessTokens := make(map[string]bool)
	refreshTokens := make(map[string]bool)
	n := 20

	for i := 0; i < n; i++ {
		access, refresh, _, _, err := tm.GeneratePair("user-123", "tenant-abc", "pro", "admin", nil)
		if err != nil {
			t.Fatalf("iteration %d: GeneratePair: %v", i, err)
		}
		if accessTokens[access] {
			t.Errorf("iteration %d: duplicate access token", i)
		}
		if refreshTokens[refresh] {
			t.Errorf("iteration %d: duplicate refresh token", i)
		}
		accessTokens[access] = true
		refreshTokens[refresh] = true
	}
}
