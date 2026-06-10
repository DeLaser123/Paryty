package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var testSecret = []byte("test-middleware-secret-thirty-two!!")

func newTestTM(t *testing.T) *TokenManager {
	t.Helper()
	tm, err := NewTokenManager(testSecret)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return tm
}

func ginTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	return c, w
}

// =============================================================================
// GinJWTAuth tests
// =============================================================================

func TestGinJWTAuth_ValidToken(t *testing.T) {
	tm := newTestTM(t)
	access, _, _, _, _ := tm.GeneratePair("user-1", "tenant-1", "pro", "admin", nil)

	c, w := ginTestContext()
	c.Request.Header.Set("Authorization", "Bearer "+access)

	GinJWTAuth(tm)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if v, ok := c.Get(string(plan.CtxTenantID)); !ok || v.(string) != "tenant-1" {
		t.Error("tenant not set in context")
	}
	if v, ok := c.Get(string(plan.CtxUserID)); !ok || v.(string) != "user-1" {
		t.Error("user ID not set in context")
	}
	if v, ok := c.Get(string(plan.CtxUserRole)); !ok || v.(string) != "admin" {
		t.Error("role not set in context")
	}
}

func TestGinJWTAuth_NoHeader(t *testing.T) {
	tm := newTestTM(t)
	c, w := ginTestContext()

	GinJWTAuth(tm)(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGinJWTAuth_InvalidBearerFormat(t *testing.T) {
	tm := newTestTM(t)
	c, w := ginTestContext()
	c.Request.Header.Set("Authorization", "Basic abc123")

	GinJWTAuth(tm)(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGinJWTAuth_EmptyToken(t *testing.T) {
	tm := newTestTM(t)
	c, w := ginTestContext()
	c.Request.Header.Set("Authorization", "Bearer ")

	GinJWTAuth(tm)(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGinJWTAuth_InvalidToken(t *testing.T) {
	tm := newTestTM(t)
	c, w := ginTestContext()
	c.Request.Header.Set("Authorization", "Bearer not.a.valid.jwt")

	GinJWTAuth(tm)(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGinJWTAuth_MalformedHeader(t *testing.T) {
	tm := newTestTM(t)
	c, w := ginTestContext()
	c.Request.Header.Set("Authorization", "BearerTokenWithoutSpace")

	GinJWTAuth(tm)(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// =============================================================================
// GinOptionalAuth tests
// =============================================================================

func TestGinOptionalAuth_NoToken_Passes(t *testing.T) {
	tm := newTestTM(t)
	c, w := ginTestContext()

	GinOptionalAuth(tm)(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for optional auth, got %d", w.Code)
	}
	// No claims should be set
	if _, ok := c.Get(string(plan.CtxUserID)); ok {
		t.Error("user ID should not be set without token")
	}
}

func TestGinOptionalAuth_ValidToken_SetsClaims(t *testing.T) {
	tm := newTestTM(t)
	access, _, _, _, _ := tm.GeneratePair("user-2", "tenant-2", "basic", "viewer", nil)

	c, w := ginTestContext()
	c.Request.Header.Set("Authorization", "Bearer "+access)

	GinOptionalAuth(tm)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if v, _ := c.Get(string(plan.CtxUserID)); v.(string) != "user-2" {
		t.Error("user ID not set")
	}
}

func TestGinOptionalAuth_BadToken_StillPasses(t *testing.T) {
	tm := newTestTM(t)
	c, w := ginTestContext()
	c.Request.Header.Set("Authorization", "Bearer invalid.token.here")

	GinOptionalAuth(tm)(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (optional skips bad tokens), got %d", w.Code)
	}
}

// =============================================================================
// GrpcJWTAuth tests
// =============================================================================

func TestGrpcJWTAuth_ValidToken(t *testing.T) {
	tm := newTestTM(t)
	access, _, _, _, _ := tm.GeneratePair("user-3", "tenant-3", "pro_plus", "operator", nil)

	md := metadata.Pairs("authorization", "Bearer "+access)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	interceptor := GrpcJWTAuth(tm)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test/Method"},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			// Verify claims injected
			if v, ok := ctx.Value(plan.CtxTenantID).(string); !ok || v != "tenant-3" {
				t.Errorf("tenant not in context: %v", v)
			}
			return "ok", nil
		})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGrpcJWTAuth_MissingMetadata(t *testing.T) {
	tm := newTestTM(t)
	ctx := context.Background()

	interceptor := GrpcJWTAuth(tm)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error for missing metadata")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", st.Code())
	}
}

func TestGrpcJWTAuth_NoAuthHeader(t *testing.T) {
	tm := newTestTM(t)
	md := metadata.Pairs("x-custom", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	interceptor := GrpcJWTAuth(tm)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error for missing authorization")
	}
}

func TestGrpcJWTAuth_InvalidToken(t *testing.T) {
	tm := newTestTM(t)
	md := metadata.Pairs("authorization", "Bearer invalid.token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	interceptor := GrpcJWTAuth(tm)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", st.Code())
	}
}

func TestGrpcJWTAuth_NonBearerFormat(t *testing.T) {
	tm := newTestTM(t)
	md := metadata.Pairs("authorization", "Basic abc123")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	interceptor := GrpcJWTAuth(tm)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error for non-Bearer format")
	}
}
