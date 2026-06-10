//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
)

// =============================================================================
// Test Helpers
// =============================================================================

// defaultPlanEngine returns a PlanEngine matching the test configuration from
// the plan package tests for consistency.
func defaultPlanEngine() *plan.PlanEngine {
	cfg := &plan.PlansConfig{
		Plans: map[string]plan.PlanDefinition{
			"basic": {
				DisplayName: "Basic",
				MaxTwins:    2,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": false, "paryty_intel": false, "custom_retention": false, "forecasting": false, "simulation": false, "sso": false},
				Limits:      plan.LimitSet{AgentsPerTwin: 10, SubUsers: 2, AlertRulesPerTenant: 5},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 1_000_000_000, QueryRequestsPerMinute: 60},
			},
			"pro": {
				DisplayName: "Pro",
				MaxTwins:    4,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": false, "custom_retention": false, "forecasting": false, "simulation": false, "sso": false},
				Limits:      plan.LimitSet{AgentsPerTwin: 50, SubUsers: 10, AlertRulesPerTenant: 20},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 10_000_000_000, QueryRequestsPerMinute: 300},
			},
			"pro_plus": {
				DisplayName: "Pro+",
				MaxTwins:    8,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": true, "custom_retention": false, "forecasting": true, "simulation": true, "sso": false},
				Limits:      plan.LimitSet{AgentsPerTwin: 100, SubUsers: 25, AlertRulesPerTenant: 50},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 50_000_000_000, QueryRequestsPerMinute: 600},
			},
			"enterprise": {
				DisplayName: "Enterprise",
				MaxTwins:    10,
				Creatable:   false,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": true, "custom_retention": true, "forecasting": true, "simulation": true, "sso": true},
				Limits:      plan.LimitSet{AgentsPerTwin: 200, SubUsers: 100, AlertRulesPerTenant: 100},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 100_000_000_000, QueryRequestsPerMinute: 1000},
			},
		},
	}
	return plan.NewPlanEngine(cfg, nil)
}

// =============================================================================
// Basic Plan Lacks Pro Features
// =============================================================================

func TestPlanGating_BasicLacksProFeatures(t *testing.T) {
	e := defaultPlanEngine()

	proFeatures := []string{"metrics", "paryty_intel", "forecasting", "simulation", "sso", "custom_retention"}
	for _, feature := range proFeatures {
		if e.HasFeature("basic", feature) {
			t.Errorf("basic plan should NOT have feature %q", feature)
		}
	}
}

func TestPlanGating_BasicHasTopologyMonitoring(t *testing.T) {
	e := defaultPlanEngine()

	if !e.HasFeature("basic", "topology_monitoring") {
		t.Error("basic plan should have topology_monitoring")
	}
}

// =============================================================================
// Pro Plan Has Intermediate Features
// =============================================================================

func TestPlanGating_ProHasMetricsButNotIntel(t *testing.T) {
	e := defaultPlanEngine()

	if !e.HasFeature("pro", "topology_monitoring") {
		t.Error("pro should have topology_monitoring")
	}
	if !e.HasFeature("pro", "metrics") {
		t.Error("pro should have metrics")
	}
	if e.HasFeature("pro", "paryty_intel") {
		t.Error("pro should NOT have paryty_intel")
	}
	if e.HasFeature("pro", "forecasting") {
		t.Error("pro should NOT have forecasting")
	}
	if e.HasFeature("pro", "simulation") {
		t.Error("pro should NOT have simulation")
	}
	if e.HasFeature("pro", "sso") {
		t.Error("pro should NOT have sso")
	}
}

// =============================================================================
// Pro+ Plan Has All Features (Except Enterprise Exclusives)
// =============================================================================

func TestPlanGating_ProPlusHasAllProFeatures(t *testing.T) {
	e := defaultPlanEngine()

	// Pro+ should have everything pro has plus more.
	if !e.HasFeature("pro_plus", "topology_monitoring") {
		t.Error("pro_plus should have topology_monitoring")
	}
	if !e.HasFeature("pro_plus", "metrics") {
		t.Error("pro_plus should have metrics")
	}
	if !e.HasFeature("pro_plus", "paryty_intel") {
		t.Error("pro_plus should have paryty_intel")
	}
	if !e.HasFeature("pro_plus", "forecasting") {
		t.Error("pro_plus should have forecasting")
	}
	if !e.HasFeature("pro_plus", "simulation") {
		t.Error("pro_plus should have simulation")
	}

	// Pro+ should NOT have enterprise-only features.
	if e.HasFeature("pro_plus", "custom_retention") {
		t.Error("pro_plus should NOT have custom_retention")
	}
	if e.HasFeature("pro_plus", "sso") {
		t.Error("pro_plus should NOT have sso")
	}
}

// =============================================================================
// Enterprise Is Not Creatable
// =============================================================================

func TestPlanGating_EnterpriseNotCreatable(t *testing.T) {
	e := defaultPlanEngine()

	if e.ValidatePlanName("enterprise") {
		t.Error("enterprise should NOT be valid for self-signup")
	}

	// But it should still exist.
	if !e.PlanExists("enterprise") {
		t.Error("enterprise plan should exist in definitions")
	}
}

func TestPlanGating_OnlyBasicAndProCreatable(t *testing.T) {
	e := defaultPlanEngine()

	creatable := e.ListCreatablePlans()

	planNames := make(map[string]bool)
	for _, p := range creatable {
		planNames[p.DisplayName] = true
	}

	if !planNames["Basic"] {
		t.Error("Basic should be creatable")
	}
	if !planNames["Pro"] {
		t.Error("Pro should be creatable")
	}
	if !planNames["Pro+"] {
		t.Error("Pro+ should be creatable")
	}
	if planNames["Enterprise"] {
		t.Error("Enterprise should NOT be creatable")
	}

	// Total should be 3.
	if len(creatable) != 3 {
		t.Errorf("expected 3 creatable plans, got %d", len(creatable))
	}
}

// =============================================================================
// CheckLimit for Various Limits
// =============================================================================

func TestPlanGating_CheckLimit_MaxTwins(t *testing.T) {
	e := defaultPlanEngine()

	tests := []struct {
		plan    string
		current int
		want    bool
	}{
		{"basic", 0, true},
		{"basic", 1, true},
		{"basic", 2, false},
		{"pro", 3, true},
		{"pro", 4, false},
		{"pro_plus", 7, true},
		{"pro_plus", 8, false},
		{"enterprise", 9, true},
		{"enterprise", 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.plan+"_"+fmt.Sprintf("%d", tt.current), func(t *testing.T) {
			got := e.CheckLimit(tt.plan, "max_twins", tt.current)
			if got != tt.want {
				t.Errorf("CheckLimit(%q, max_twins, %d) = %v, want %v", tt.plan, tt.current, got, tt.want)
			}
		})
	}
}

func TestPlanGating_CheckLimit_AgentsPerTwin(t *testing.T) {
	e := defaultPlanEngine()

	// Basic: 10, Pro: 50, Pro+: 100, Enterprise: 200
	if !e.CheckLimit("basic", "agents_per_twin", 0) {
		t.Error("0 < 10 for basic")
	}
	if e.CheckLimit("basic", "agents_per_twin", 10) {
		t.Error("10 >= 10 for basic")
	}
	if e.CheckLimit("basic", "agents_per_twin", 11) {
		t.Error("11 >= 10 for basic")
	}

	if !e.CheckLimit("pro", "agents_per_twin", 49) {
		t.Error("49 < 50 for pro")
	}
	if e.CheckLimit("pro", "agents_per_twin", 50) {
		t.Error("50 >= 50 for pro")
	}

	if !e.CheckLimit("pro_plus", "agents_per_twin", 99) {
		t.Error("99 < 100 for pro_plus")
	}

	if !e.CheckLimit("enterprise", "agents_per_twin", 199) {
		t.Error("199 < 200 for enterprise")
	}
}

func TestPlanGating_CheckLimit_SubUsers(t *testing.T) {
	e := defaultPlanEngine()

	// Basic: 2, Pro: 10, Pro+: 25, Enterprise: 100
	if !e.CheckLimit("basic", "sub_users", 1) {
		t.Error("1 < 2 for basic")
	}
	if e.CheckLimit("basic", "sub_users", 2) {
		t.Error("2 >= 2 for basic")
	}

	if !e.CheckLimit("pro", "sub_users", 9) {
		t.Error("9 < 10 for pro")
	}

	if !e.CheckLimit("pro_plus", "sub_users", 24) {
		t.Error("24 < 25 for pro_plus")
	}

	if !e.CheckLimit("enterprise", "sub_users", 99) {
		t.Error("99 < 100 for enterprise")
	}
}

func TestPlanGating_CheckLimit_AlertRulesPerTenant(t *testing.T) {
	e := defaultPlanEngine()

	// Basic: 5, Pro: 20, Pro+: 50, Enterprise: 100
	if !e.CheckLimit("basic", "alert_rules_per_tenant", 4) {
		t.Error("4 < 5 for basic")
	}
	if e.CheckLimit("basic", "alert_rules_per_tenant", 5) {
		t.Error("5 >= 5 for basic")
	}

	if !e.CheckLimit("pro", "alert_rules_per_tenant", 19) {
		t.Error("19 < 20 for pro")
	}
	if !e.CheckLimit("pro_plus", "alert_rules_per_tenant", 49) {
		t.Error("49 < 50 for pro_plus")
	}
	if !e.CheckLimit("enterprise", "alert_rules_per_tenant", 99) {
		t.Error("99 < 100 for enterprise")
	}
}

func TestPlanGating_CheckLimit_UnknownLimit(t *testing.T) {
	e := defaultPlanEngine()

	// Unknown limit keys default to false (security: fail-closed).
	// New limits must be explicitly added to the switch before they work.
	if e.CheckLimit("basic", "future_feature_xyz", 9999) {
		t.Error("unknown limit should default to denied (fail-closed)")
	}
}

func TestPlanGating_CheckLimit_UnknownPlan(t *testing.T) {
	e := defaultPlanEngine()

	if e.CheckLimit("nonexistent", "max_twins", 0) {
		t.Error("unknown plan should return false")
	}
	if e.CheckLimit("nonexistent", "agents_per_twin", 0) {
		t.Error("unknown plan should return false")
	}
}

// =============================================================================
// CheckQuota
// =============================================================================

func TestPlanGating_CheckQuota_IngestionBytesPerDay(t *testing.T) {
	e := defaultPlanEngine()

	tests := []struct {
		plan    string
		current int64
		want    bool
	}{
		{"basic", 500_000_000, true},
		{"basic", 999_999_999, true},
		{"basic", 1_000_000_000, false},
		{"basic", 2_000_000_000, false},
		{"pro", 9_999_999_999, true},
		{"pro", 10_000_000_000, false},
		{"pro_plus", 49_999_999_999, true},
		{"pro_plus", 50_000_000_000, false},
		{"enterprise", 99_999_999_999, true},
		{"enterprise", 100_000_000_000, false},
	}

	for _, tt := range tests {
		t.Run(tt.plan, func(t *testing.T) {
			got := e.CheckQuota(tt.plan, "ingestion_bytes_per_day", tt.current)
			if got != tt.want {
				t.Errorf("CheckQuota(%q, ingestion_bytes_per_day, %d) = %v, want %v", tt.plan, tt.current, got, tt.want)
			}
		})
	}
}

func TestPlanGating_CheckQuota_UnknownQuota(t *testing.T) {
	e := defaultPlanEngine()

	// Unknown quota keys default to true (forward-compat).
	if !e.CheckQuota("basic", "future_quota_metric", 999999) {
		t.Error("unknown quota should default to allowed")
	}
}

func TestPlanGating_CheckQuota_UnknownPlan(t *testing.T) {
	e := defaultPlanEngine()

	if e.CheckQuota("nonexistent", "ingestion_bytes_per_day", 0) {
		t.Error("unknown plan should return false for CheckQuota")
	}
}

// =============================================================================
// Plan Definition Structure Tests
// =============================================================================

func TestPlanGating_PlanDefinitions_AllHaveDisplayNames(t *testing.T) {
	e := defaultPlanEngine()

	expected := map[string]string{
		"basic":      "Basic",
		"pro":        "Pro",
		"pro_plus":   "Pro+",
		"enterprise": "Enterprise",
	}

	for name, expectedDisplay := range expected {
		plan, ok := e.GetPlan(name)
		if !ok {
			t.Errorf("plan %q should exist", name)
			continue
		}
		if plan.DisplayName != expectedDisplay {
			t.Errorf("plan %q: expected display name %q, got %q", name, expectedDisplay, plan.DisplayName)
		}
	}
}

func TestPlanGating_PlanDefinitions_AllHavePositiveMaxTwins(t *testing.T) {
	e := defaultPlanEngine()

	plans := []string{"basic", "pro", "pro_plus", "enterprise"}
	for _, name := range plans {
		plan, ok := e.GetPlan(name)
		if !ok {
			t.Errorf("plan %q should exist", name)
			continue
		}
		if plan.MaxTwins <= 0 {
			t.Errorf("plan %q: MaxTwins should be > 0, got %d", name, plan.MaxTwins)
		}
	}
}

// =============================================================================
// ReloadConfig then Re-Check Gating
// =============================================================================

func TestPlanGating_ReloadConfig_AffectsGating(t *testing.T) {
	e := defaultPlanEngine()

	if e.GetMaxTwins("basic") != 2 {
		t.Fatal("expected initial max_twins=2")
	}

	// Reload with different config.
	newCfg := &plan.PlansConfig{
		Plans: map[string]plan.PlanDefinition{
			"basic": {
				DisplayName: "Basic",
				MaxTwins:    5,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true},
				Limits:      plan.LimitSet{AgentsPerTwin: 10, SubUsers: 2, AlertRulesPerTenant: 5},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 1_000_000_000, QueryRequestsPerMinute: 60},
			},
		},
	}
	e.ReloadConfig(newCfg)

	if got := e.GetMaxTwins("basic"); got != 5 {
		t.Errorf("after reload, expected max_twins=5, got %d", got)
	}

	// Old "pro" should now be gone.
	if e.PlanExists("pro") {
		t.Error("pro plan should not exist after reload")
	}
	if e.HasFeature("pro", "metrics") {
		t.Error("pro metrics should not be available after reload")
	}
}
