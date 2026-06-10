package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ginTestCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	return c, w
}

// =============================================================================
// RequireRole tests
// =============================================================================

func TestRequireRole_Allowed(t *testing.T) {
	c, w := ginTestCtx()
	c.Set(string(plan.CtxUserRole), "admin")

	RequireRole("admin", "operator")(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireRole_Denied(t *testing.T) {
	c, w := ginTestCtx()
	c.Set(string(plan.CtxUserRole), "viewer")

	RequireRole("admin", "operator")(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireRole_NoRoleContext(t *testing.T) {
	c, w := ginTestCtx()
	// No role set

	RequireRole("admin")(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireRole_SingleRole(t *testing.T) {
	c, w := ginTestCtx()
	c.Set(string(plan.CtxUserRole), "admin")

	RequireRole("admin")(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// =============================================================================
// RequirePermission tests
// =============================================================================

func TestRequirePermission_Granted(t *testing.T) {
	c, w := ginTestCtx()
	c.Set(string(plan.CtxUserRole), "admin")

	RequirePermission("twins:read")(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequirePermission_Denied(t *testing.T) {
	c, w := ginTestCtx()
	c.Set(string(plan.CtxUserRole), "viewer")

	RequirePermission("twins:write")(c)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequirePermission_NoRoleContext(t *testing.T) {
	c, w := ginTestCtx()

	RequirePermission("twins:read")(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequirePermission_OperatorCanAcknowledgeAlerts(t *testing.T) {
	c, w := ginTestCtx()
	c.Set(string(plan.CtxUserRole), "operator")

	RequirePermission("alerts:acknowledge")(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// =============================================================================
// GrpcRequireRole tests
// =============================================================================

func TestGrpcRequireRole_Allowed(t *testing.T) {
	ctx := context.WithValue(context.Background(), plan.CtxUserRole, "admin")

	interceptor := GrpcRequireRole("admin", "operator")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGrpcRequireRole_Denied(t *testing.T) {
	ctx := context.WithValue(context.Background(), plan.CtxUserRole, "viewer")

	interceptor := GrpcRequireRole("admin")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", st.Code())
	}
}

func TestGrpcRequireRole_NoContext(t *testing.T) {
	ctx := context.Background()

	interceptor := GrpcRequireRole("admin")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// GrpcRequirePermission tests
// =============================================================================

func TestGrpcRequirePermission_Granted(t *testing.T) {
	ctx := context.WithValue(context.Background(), plan.CtxUserRole, "operator")

	interceptor := GrpcRequirePermission("alerts:acknowledge")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGrpcRequirePermission_Denied(t *testing.T) {
	ctx := context.WithValue(context.Background(), plan.CtxUserRole, "viewer")

	interceptor := GrpcRequirePermission("users:write")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", st.Code())
	}
}

func TestGrpcRequirePermission_NoContext(t *testing.T) {
	ctx := context.Background()

	interceptor := GrpcRequirePermission("anything")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", st.Code())
	}
}
