package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// newQuotaTestEngine creates a PlanEngine with free/pro/business/enterprise
// plans for quota boundary testing. This mirrors the production plans.yaml
// structure but with test-specific values.
func newQuotaTestEngine() *PlanEngine {
	cfg := &PlansConfig{
		Plans: map[string]PlanDefinition{
			"free": {
				DisplayName: "Free",
				MaxTwins:    1,
				Creatable:   true,
				Features:    map[string]bool{"forecasting": false, "topology_monitoring": true},
				Limits:      LimitSet{AgentsPerTwin: 3, SubUsers: 1, AlertRulesPerTenant: 3},
				Quotas:      QuotaSet{IngestionBytesPerDay: 500_000_000, QueryRequestsPerMinute: 30},
			},
			"pro": {
				DisplayName: "Pro",
				MaxTwins:    5,
				Creatable:   true,
				Features:    map[string]bool{"forecasting": true, "topology_monitoring": true, "metrics": true},
				Limits:      LimitSet{AgentsPerTwin: 50, SubUsers: 10, AlertRulesPerTenant: 20},
				Quotas:      QuotaSet{IngestionBytesPerDay: 10_000_000_000, QueryRequestsPerMinute: 300},
			},
			"business": {
				DisplayName: "Business",
				MaxTwins:    25,
				Creatable:   true,
				Features:    map[string]bool{"forecasting": true, "topology_monitoring": true, "metrics": true, "paryty_intel": true},
				Limits:      LimitSet{AgentsPerTwin: 100, SubUsers: 50, AlertRulesPerTenant: 50},
				Quotas:      QuotaSet{IngestionBytesPerDay: 50_000_000_000, QueryRequestsPerMinute: 600},
			},
			"enterprise": {
				DisplayName: "Enterprise",
				MaxTwins:    -1, // Unlimited sentinel
				Creatable:   false,
				Features:    map[string]bool{"forecasting": true, "topology_monitoring": true, "metrics": true, "paryty_intel": true, "custom_retention": true},
				Limits:      LimitSet{AgentsPerTwin: 500, SubUsers: 200, AlertRulesPerTenant: 200},
				Quotas:      QuotaSet{IngestionBytesPerDay: 500_000_000_000, QueryRequestsPerMinute: 5000},
			},
		},
	}
	return NewPlanEngine(cfg, nil)
}

// TestQuotaLimitsPerPlan verifies quota limits are correctly defined for each plan.
func TestQuotaLimitsPerPlan(t *testing.T) {
	engine := newQuotaTestEngine()

	// Free plan: max 1 twin
	free, ok := engine.GetPlan("free")
	assert.True(t, ok, "free plan must exist")
	assert.Equal(t, 1, free.MaxTwins, "free plan must allow max 1 twin")

	// Pro plan: max 5 twins
	pro, ok := engine.GetPlan("pro")
	assert.True(t, ok, "pro plan must exist")
	assert.Equal(t, 5, pro.MaxTwins, "pro plan must allow max 5 twins")

	// Business plan: max 25 twins
	business, ok := engine.GetPlan("business")
	assert.True(t, ok, "business plan must exist")
	assert.Equal(t, 25, business.MaxTwins, "business plan must allow max 25 twins")

	// Enterprise plan: unlimited (-1 sentinel)
	enterprise, ok := engine.GetPlan("enterprise")
	assert.True(t, ok, "enterprise plan must exist")
	assert.Equal(t, -1, enterprise.MaxTwins, "enterprise plan must use -1 for unlimited twins")
}

// TestPlanFeatureGating verifies feature flags per plan.
func TestPlanFeatureGating(t *testing.T) {
	engine := newQuotaTestEngine()

	// Free plan: no forecasting
	free, ok := engine.GetPlan("free")
	assert.True(t, ok)
	assert.False(t, free.Features["forecasting"], "free plan must not have forecasting")

	// Pro plan: has forecasting
	pro, ok := engine.GetPlan("pro")
	assert.True(t, ok)
	assert.True(t, pro.Features["forecasting"], "pro plan must have forecasting")

	// Business plan: has forecasting and paryty_intel
	business, ok := engine.GetPlan("business")
	assert.True(t, ok)
	assert.True(t, business.Features["forecasting"], "business plan must have forecasting")
	assert.True(t, business.Features["paryty_intel"], "business plan must have paryty_intel")

	// Enterprise plan: has all features
	enterprise, ok := engine.GetPlan("enterprise")
	assert.True(t, ok)
	assert.True(t, enterprise.Features["forecasting"], "enterprise plan must have forecasting")
	assert.True(t, enterprise.Features["custom_retention"], "enterprise plan must have custom_retention")
}

// TestCreatablePlans verifies only non-enterprise plans are creatable via self-service.
func TestCreatablePlans(t *testing.T) {
	engine := newQuotaTestEngine()

	// Use ListAllPlans to get plan names (ListCreatablePlans returns []PlanDefinition without names)
	allPlans := engine.ListAllPlans()
	creatablePlanNames := make([]string, 0)
	nonCreatablePlanNames := make([]string, 0)
	for _, np := range allPlans {
		if np.Def.Creatable {
			creatablePlanNames = append(creatablePlanNames, np.Name)
		} else {
			nonCreatablePlanNames = append(nonCreatablePlanNames, np.Name)
		}
	}

	// Enterprise must NOT be creatable
	assert.Contains(t, nonCreatablePlanNames, "enterprise", "enterprise must not be self-service creatable")

	// At least 3 plans should be creatable (free, pro, business)
	assert.GreaterOrEqual(t, len(creatablePlanNames), 3,
		"at least 3 plans should be creatable via self-service")

	// Verify none of the creatable plans are enterprise
	for _, name := range creatablePlanNames {
		assert.NotEqual(t, "enterprise", name,
			"enterprise should not appear in creatable plans")
	}
}

// TestQuotaEnforcementBoundary verifies that CheckLimit enforces boundaries correctly
// for each plan tier, including the strict-less-than semantics.
func TestQuotaEnforcementBoundary(t *testing.T) {
	engine := newQuotaTestEngine()

	testCases := []struct {
		name      string
		plan      string
		limit     string
		current   int
		allowed   bool
	}{
		// Free plan: max_twins=1
		{"free_0_twins_allowed", "free", "max_twins", 0, true},
		{"free_1_twin_at_limit", "free", "max_twins", 1, false},
		{"free_2_twins_over_limit", "free", "max_twins", 2, false},

		// Pro plan: max_twins=5
		{"pro_4_twins_allowed", "pro", "max_twins", 4, true},
		{"pro_5_twins_at_limit", "pro", "max_twins", 5, false},
		{"pro_6_twins_over_limit", "pro", "max_twins", 6, false},

		// Business plan: max_twins=25
		{"biz_24_twins_allowed", "business", "max_twins", 24, true},
		{"biz_25_twins_at_limit", "business", "max_twins", 25, false},

		// Enterprise plan: max_twins=-1 (unlimited sentinel)
		// Note: CheckLimit uses current < plan.MaxTwins, so -1 blocks all.
		// Unlimited plans bypass CheckLimit at the middleware level.
		{"enterprise_0_twins_blocked_by_sentinel", "enterprise", "max_twins", 0, false},

		// Free plan: agents_per_twin=3
		{"free_2_agents_allowed", "free", "agents_per_twin", 2, true},
		{"free_3_agents_at_limit", "free", "agents_per_twin", 3, false},

		// Unknown plan defaults to denied
		{"unknown_plan_denied", "nonexistent", "max_twins", 0, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := engine.CheckLimit(tc.plan, tc.limit, tc.current)
			assert.Equal(t, tc.allowed, result,
				"plan=%s limit=%s current=%d", tc.plan, tc.limit, tc.current)
		})
	}
}

// TestPlanQuotaThroughputLimits verifies per-plan throughput quotas.
func TestPlanQuotaThroughputLimits(t *testing.T) {
	engine := newQuotaTestEngine()

	// Free plan: 500MB/day ingestion
	assert.True(t, engine.CheckQuota("free", "ingestion_bytes_per_day", 499_999_999))
	assert.False(t, engine.CheckQuota("free", "ingestion_bytes_per_day", 500_000_000))

	// Pro plan: 10GB/day ingestion
	assert.True(t, engine.CheckQuota("pro", "ingestion_bytes_per_day", 9_999_999_999))
	assert.False(t, engine.CheckQuota("pro", "ingestion_bytes_per_day", 10_000_000_000))
}

// TestPlanExistsBoundary verifies PlanExists for known and unknown plans.
func TestPlanExistsBoundary(t *testing.T) {
	engine := newQuotaTestEngine()

	assert.True(t, engine.PlanExists("free"))
	assert.True(t, engine.PlanExists("pro"))
	assert.True(t, engine.PlanExists("business"))
	assert.True(t, engine.PlanExists("enterprise"))
	assert.False(t, engine.PlanExists("nonexistent"))
	assert.False(t, engine.PlanExists(""))
}
