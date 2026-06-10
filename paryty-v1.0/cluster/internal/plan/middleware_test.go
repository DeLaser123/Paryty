package plan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

func newTestPlanEngine() *PlanEngine {
	cfg := &PlansConfig{
		Plans: map[string]PlanDefinition{
			"basic": {
				DisplayName: "Basic",
				MaxTwins:    2,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "paryty_intel": false},
				Limits:      LimitSet{AgentsPerTwin: 10},
			},
			"pro": {
				DisplayName: "Pro",
				MaxTwins:    4,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "paryty_intel": true},
				Limits:      LimitSet{AgentsPerTwin: 50},
			},
		},
	}
	return NewPlanEngine(cfg, nil)
}

func TestGinFeatureGate_HasFeature(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()
	c.Set(string(CtxPlanName), "pro")

	GinFeatureGate(e, "paryty_intel")(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGinFeatureGate_MissingFeature(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()
	c.Set(string(CtxPlanName), "basic")

	GinFeatureGate(e, "paryty_intel")(c)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402, got %d", w.Code)
	}
}

func TestGinFeatureGate_NoPlanContext(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()
	// Plan name not set

	GinFeatureGate(e, "anything")(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGinTwinLimitGate_UnderLimit(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()
	c.Set(string(CtxPlanName), "basic")

	counter := func(c *gin.Context) int { return 1 } // 1 < 2 max

	GinTwinLimitGate(e, counter)(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGinTwinLimitGate_AtLimit(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()
	c.Set(string(CtxPlanName), "basic")

	counter := func(c *gin.Context) int { return 2 } // 2 >= 2 max

	GinTwinLimitGate(e, counter)(c)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402, got %d", w.Code)
	}
}

func TestGinTwinLimitGate_OverLimit(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()
	c.Set(string(CtxPlanName), "pro")

	counter := func(c *gin.Context) int { return 5 } // 5 >= 4 max

	GinTwinLimitGate(e, counter)(c)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("expected 402, got %d", w.Code)
	}
}

func TestGinTwinLimitGate_NoPlanContext(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()

	counter := func(c *gin.Context) int { return 0 }

	GinTwinLimitGate(e, counter)(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGinPlanInfoInjector_NoPlanSet_NoTenant(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()

	GinPlanInfoInjector(e)(c)

	// Should just pass through
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGinPlanInfoInjector_PlanAlreadySet(t *testing.T) {
	e := newTestPlanEngine()
	c, w := ginTestCtx()
	c.Set(string(CtxPlanName), "pro")

	GinPlanInfoInjector(e)(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	// Should still be "pro"
	if v, _ := c.Get(string(CtxPlanName)); v.(string) != "pro" {
		t.Errorf("plan name should still be 'pro', got %v", v)
	}
}

func TestGrpcFeatureGate_HasFeature(t *testing.T) {
	e := newTestPlanEngine()
	ctx := context.WithValue(context.Background(), CtxPlanName, "pro")

	interceptor := GrpcFeatureGate(e, "paryty_intel")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGrpcFeatureGate_MissingFeature(t *testing.T) {
	e := newTestPlanEngine()
	ctx := context.WithValue(context.Background(), CtxPlanName, "basic")

	interceptor := GrpcFeatureGate(e, "paryty_intel")
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

func TestGrpcFeatureGate_NoPlanContext(t *testing.T) {
	e := newTestPlanEngine()
	ctx := context.Background()

	interceptor := GrpcFeatureGate(e, "anything")
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

func TestGrpcTwinLimitGate_UnderLimit(t *testing.T) {
	e := newTestPlanEngine()
	ctx := context.WithValue(context.Background(), CtxPlanName, "basic")

	counter := func(ctx context.Context) int { return 0 }

	interceptor := GrpcTwinLimitGate(e, counter)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return "ok", nil
		})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestGrpcTwinLimitGate_AtLimit(t *testing.T) {
	e := newTestPlanEngine()
	ctx := context.WithValue(context.Background(), CtxPlanName, "pro")

	counter := func(ctx context.Context) int { return 4 } // max is 4

	interceptor := GrpcTwinLimitGate(e, counter)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{},
		func(ctx context.Context, req interface{}) (interface{}, error) {
			return nil, nil
		})

	if err == nil {
		t.Fatal("expected error for at-limit")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.ResourceExhausted {
		t.Errorf("expected ResourceExhausted, got %v", st.Code())
	}
}
