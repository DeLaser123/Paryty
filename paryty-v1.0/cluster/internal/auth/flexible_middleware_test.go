package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
)

// =============================================================================
// GinJWTAuthFlexible regression tests
//
// This middleware exists exclusively for browser streaming APIs (WebSocket,
// EventSource/SSE) that cannot set Authorization headers. It must:
//   - accept a valid Bearer header (same as GinJWTAuth)
//   - accept a valid "token" query parameter when no header is present
//   - reject requests with neither, or with invalid tokens
// =============================================================================

func flexTestRouter(t *testing.T, tm *TokenManager) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/stream", GinJWTAuthFlexible(tm), func(c *gin.Context) {
		tenant, _ := c.Get(string(plan.CtxTenantID))
		c.JSON(http.StatusOK, gin.H{"tenant": tenant})
	})
	return r
}

func flexTestToken(t *testing.T, tm *TokenManager) string {
	t.Helper()
	access, _, _, _, err := tm.GeneratePair("user-1", "tenant-1", "pro", "viewer", map[string]bool{"twins:read": true})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return access
}

func newFlexTokenManager(t *testing.T) *TokenManager {
	t.Helper()
	tm, err := NewTokenManager([]byte("test-secret-which-is-at-least-32-bytes!!"))
	if err != nil {
		t.Fatalf("new token manager: %v", err)
	}
	return tm
}

func TestGinJWTAuthFlexible_BearerHeader(t *testing.T) {
	tm := newFlexTokenManager(t)
	r := flexTestRouter(t, tm)

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Authorization", "Bearer "+flexTestToken(t, tm))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with Bearer header, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGinJWTAuthFlexible_QueryParam(t *testing.T) {
	tm := newFlexTokenManager(t)
	r := flexTestRouter(t, tm)

	req := httptest.NewRequest(http.MethodGet, "/stream?token="+flexTestToken(t, tm), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with token query param, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGinJWTAuthFlexible_NoToken_Rejected(t *testing.T) {
	tm := newFlexTokenManager(t)
	r := flexTestRouter(t, tm)

	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without any token, got %d", rec.Code)
	}
}

func TestGinJWTAuthFlexible_InvalidQueryToken_Rejected(t *testing.T) {
	tm := newFlexTokenManager(t)
	r := flexTestRouter(t, tm)

	req := httptest.NewRequest(http.MethodGet, "/stream?token=not-a-jwt", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with invalid query token, got %d", rec.Code)
	}
}

func TestGinJWTAuthFlexible_HeaderTakesPrecedence(t *testing.T) {
	// An invalid header must be rejected even if a valid query token exists:
	// silently falling back would mask client bugs and create ambiguity.
	tm := newFlexTokenManager(t)
	r := flexTestRouter(t, tm)

	req := httptest.NewRequest(http.MethodGet, "/stream?token="+flexTestToken(t, tm), nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when header is invalid, got %d", rec.Code)
	}
}

func TestGinJWTAuthFlexible_ClaimsInjected(t *testing.T) {
	tm := newFlexTokenManager(t)
	r := flexTestRouter(t, tm)

	req := httptest.NewRequest(http.MethodGet, "/stream?token="+flexTestToken(t, tm), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != `{"tenant":"tenant-1"}` {
		t.Fatalf("expected tenant claim injection, got %s", body)
	}
}
