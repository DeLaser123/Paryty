package plan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTwinCounter implements TwinCounter for testing.
type mockTwinCounter struct {
	count int
	err   error
}

func (m *mockTwinCounter) CountTwins(ctx context.Context, tenantID string) (int, error) {
	return m.count, m.err
}

// mockAgentCounter implements AgentCounter for testing.
type mockAgentCounter struct {
	count int
	err   error
}

func (m *mockAgentCounter) CountAgentsForTwin(ctx context.Context, twinID string) (int, error) {
	return m.count, m.err
}

func TestRequireTwinQuota_AllowsWhenUnderLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a mock plan engine with a limit of 5 twins
	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"max_twins": 5,
			},
		},
	}

	// Create a mock counter that returns 3 twins (under limit)
	counter := &mockTwinCounter{count: 3}

	// Create middleware
	middleware := RequireTwinQuota(engine, counter)

	// Create test context
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)

	// Set tenant and plan context
	c.Set(string(CtxTenantID), "tenant-123")
	c.Set(string(CtxPlanName), "basic")

	// Call middleware
	middleware(c)

	// Should not abort
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, c.IsAborted())
}

func TestRequireTwinQuota_BlocksWhenAtLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a mock plan engine with a limit of 5 twins
	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"max_twins": 5,
			},
		},
	}

	// Create a mock counter that returns 5 twins (at limit)
	counter := &mockTwinCounter{count: 5}

	// Create middleware
	middleware := RequireTwinQuota(engine, counter)

	// Create test context
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)

	// Set tenant and plan context
	c.Set(string(CtxTenantID), "tenant-123")
	c.Set(string(CtxPlanName), "basic")

	// Call middleware
	middleware(c)

	// Should abort with 402 Payment Required
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.True(t, c.IsAborted())
}

func TestRequireTwinQuota_BlocksWhenOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a mock plan engine with a limit of 5 twins
	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"max_twins": 5,
			},
		},
	}

	// Create a mock counter that returns 7 twins (over limit)
	counter := &mockTwinCounter{count: 7}

	// Create middleware
	middleware := RequireTwinQuota(engine, counter)

	// Create test context
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)

	// Set tenant and plan context
	c.Set(string(CtxTenantID), "tenant-123")
	c.Set(string(CtxPlanName), "basic")

	// Call middleware
	middleware(c)

	// Should abort with 402 Payment Required
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.True(t, c.IsAborted())
}

func TestRequireTwinQuota_DifferentPlansDifferentLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a mock plan engine with different limits
	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"max_twins": 5,
			},
		},
		"pro": {
			Name: "pro",
			Limits: map[string]int{
				"max_twins": 20,
			},
		},
	}

	// Test basic plan with 5 twins (at limit)
	counter := &mockTwinCounter{count: 5}
	middleware := RequireTwinQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)
	c.Set(string(CtxTenantID), "tenant-123")
	c.Set(string(CtxPlanName), "basic")

	middleware(c)
	assert.Equal(t, http.StatusPaymentRequired, w.Code)

	// Test pro plan with 5 twins (under limit)
	counter = &mockTwinCounter{count: 5}
	middleware = RequireTwinQuota(engine, counter)

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)
	c.Set(string(CtxTenantID), "tenant-123")
	c.Set(string(CtxPlanName), "pro")

	middleware(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireTwinQuota_MissingTenantContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"max_twins": 5,
			},
		},
	}

	counter := &mockTwinCounter{count: 3}
	middleware := RequireTwinQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)

	// Don't set tenant context
	c.Set(string(CtxPlanName), "basic")

	middleware(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.True(t, c.IsAborted())
}

func TestRequireTwinQuota_MissingPlanContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"max_twins": 5,
			},
		},
	}

	counter := &mockTwinCounter{count: 3}
	middleware := RequireTwinQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)

	// Set tenant but not plan
	c.Set(string(CtxTenantID), "tenant-123")

	middleware(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.True(t, c.IsAborted())
}

func TestRequireTwinQuota_UnknownPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"max_twins": 5,
			},
		},
	}

	counter := &mockTwinCounter{count: 3}
	middleware := RequireTwinQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins", nil)
	c.Set(string(CtxTenantID), "tenant-123")
	c.Set(string(CtxPlanName), "unknown-plan")

	middleware(c)
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.True(t, c.IsAborted())
}

func TestRequireAgentQuota_AllowsWhenUnderLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"agents_per_twin": 10,
			},
		},
	}

	counter := &mockAgentCounter{count: 5}
	middleware := RequireAgentQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins/twin-123/agents", nil)
	c.Set(string(CtxPlanName), "basic")
	c.Params = gin.Params{{Key: "twin_id", Value: "twin-123"}}

	middleware(c)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, c.IsAborted())
}

func TestRequireAgentQuota_BlocksWhenAtLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"agents_per_twin": 10,
			},
		},
	}

	counter := &mockAgentCounter{count: 10}
	middleware := RequireAgentQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins/twin-123/agents", nil)
	c.Set(string(CtxPlanName), "basic")
	c.Params = gin.Params{{Key: "twin_id", Value: "twin-123"}}

	middleware(c)
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.True(t, c.IsAborted())
}

func TestRequireAgentQuota_MissingTwinID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"agents_per_twin": 10,
			},
		},
	}

	counter := &mockAgentCounter{count: 5}
	middleware := RequireAgentQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins//agents", nil)
	c.Set(string(CtxPlanName), "basic")

	middleware(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, c.IsAborted())
}

func TestRequireAgentQuota_UnknownPlan(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := NewPlanEngine(nil)
	engine.plans = map[string]*Plan{
		"basic": {
			Name: "basic",
			Limits: map[string]int{
				"agents_per_twin": 10,
			},
		},
	}

	counter := &mockAgentCounter{count: 5}
	middleware := RequireAgentQuota(engine, counter)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/twins/twin-123/agents", nil)
	c.Set(string(CtxPlanName), "unknown-plan")
	c.Params = gin.Params{{Key: "twin_id", Value: "twin-123"}}

	middleware(c)
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.True(t, c.IsAborted())
}
