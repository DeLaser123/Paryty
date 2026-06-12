package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
)

// =============================================================================
// requireTenant regression tests
//
// These tests pin the multi-tenant isolation contract introduced after the
// F12/F13 audit findings: tenant scope comes EXCLUSIVELY from JWT claims
// (injected into the Gin context by auth middleware). The client-supplied
// X-Tenant-ID header must never influence tenancy, and requests without an
// authenticated tenant must be rejected with 401.
// =============================================================================

func newTenantTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/topology", nil)
	return c, rec
}

func TestRequireTenant_NoClaims_Returns401(t *testing.T) {
	c, rec := newTenantTestContext(t)

	tenant, ok := requireTenant(c)
	if ok {
		t.Fatal("requireTenant must fail when no JWT claims are present")
	}
	if tenant != "" {
		t.Fatalf("expected empty tenant, got %q", tenant)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireTenant_JWTClaims_ReturnsTenant(t *testing.T) {
	c, _ := newTenantTestContext(t)
	c.Set(string(plan.CtxTenantID), "tenant-from-jwt")

	tenant, ok := requireTenant(c)
	if !ok {
		t.Fatal("requireTenant must succeed when JWT claims are present")
	}
	if tenant != "tenant-from-jwt" {
		t.Fatalf("expected tenant-from-jwt, got %q", tenant)
	}
}

func TestRequireTenant_HeaderIsNeverTrusted(t *testing.T) {
	// Regression for F13: a spoofed X-Tenant-ID header must not grant
	// access to another tenant's data.
	c, rec := newTenantTestContext(t)
	c.Request.Header.Set("X-Tenant-ID", "victim-tenant")

	tenant, ok := requireTenant(c)
	if ok || tenant != "" {
		t.Fatalf("X-Tenant-ID header must be ignored; got tenant=%q ok=%v", tenant, ok)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireTenant_HeaderCannotOverrideJWT(t *testing.T) {
	c, _ := newTenantTestContext(t)
	c.Set(string(plan.CtxTenantID), "own-tenant")
	c.Request.Header.Set("X-Tenant-ID", "victim-tenant")

	tenant, ok := requireTenant(c)
	if !ok {
		t.Fatal("requireTenant must succeed with JWT claims")
	}
	if tenant != "own-tenant" {
		t.Fatalf("JWT tenant must win over header; got %q", tenant)
	}
}

func TestRequireTenant_EmptyClaimRejected(t *testing.T) {
	c, rec := newTenantTestContext(t)
	c.Set(string(plan.CtxTenantID), "")

	if _, ok := requireTenant(c); ok {
		t.Fatal("empty tenant claim must be rejected")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
