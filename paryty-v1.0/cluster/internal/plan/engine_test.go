package plan

import (
	"testing"
)

func newTestEngine() *PlanEngine {
	cfg := &PlansConfig{
		Plans: map[string]PlanDefinition{
			"basic": {
				DisplayName: "Basic",
				MaxTwins:    2,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": false, "paryty_intel": false},
				Limits:      LimitSet{AgentsPerTwin: 10, SubUsers: 2, AlertRulesPerTenant: 5},
				Quotas:      QuotaSet{IngestionBytesPerDay: 1_000_000_000, QueryRequestsPerMinute: 60},
			},
			"pro": {
				DisplayName: "Pro",
				MaxTwins:    4,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": false},
				Limits:      LimitSet{AgentsPerTwin: 50, SubUsers: 10, AlertRulesPerTenant: 20},
				Quotas:      QuotaSet{IngestionBytesPerDay: 10_000_000_000, QueryRequestsPerMinute: 300},
			},
			"enterprise": {
				DisplayName: "Enterprise",
				MaxTwins:    10,
				Creatable:   false,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": true, "custom_retention": true},
				Limits:      LimitSet{AgentsPerTwin: 200, SubUsers: 100, AlertRulesPerTenant: 100},
				Quotas:      QuotaSet{IngestionBytesPerDay: 100_000_000_000, QueryRequestsPerMinute: 1000},
			},
		},
	}
	return NewPlanEngine(cfg, nil)
}

func TestPlanEngine_HasFeature(t *testing.T) {
	e := newTestEngine()

	if !e.HasFeature("basic", "topology_monitoring") {
		t.Error("basic should have topology_monitoring")
	}
	if e.HasFeature("basic", "paryty_intel") {
		t.Error("basic should NOT have paryty_intel")
	}
	if !e.HasFeature("pro", "metrics") {
		t.Error("pro should have metrics")
	}
	if e.HasFeature("nonexistent", "anything") {
		t.Error("unknown plan should return false")
	}
}

func TestPlanEngine_CheckLimit(t *testing.T) {
	e := newTestEngine()

	// Basic: max_twins=2
	if !e.CheckLimit("basic", "max_twins", 0) {
		t.Error("0 < 2 should be allowed")
	}
	if !e.CheckLimit("basic", "max_twins", 1) {
		t.Error("1 < 2 should be allowed")
	}
	if e.CheckLimit("basic", "max_twins", 2) {
		t.Error("2 >= 2 should NOT be allowed (strict less-than)")
	}
	if e.CheckLimit("basic", "max_twins", 5) {
		t.Error("5 >= 2 should NOT be allowed")
	}

	// Basic: agents_per_twin=10
	if !e.CheckLimit("basic", "agents_per_twin", 5) {
		t.Error("5 < 10 should be allowed")
	}
	if e.CheckLimit("basic", "agents_per_twin", 10) {
		t.Error("10 >= 10 should NOT be allowed (strict <)")
	}

	// Unknown plan
	if e.CheckLimit("nonexistent", "max_twins", 0) {
		t.Error("unknown plan should return false")
	}
}

func TestPlanEngine_CheckLimit_SubUsers(t *testing.T) {
	e := newTestEngine()

	// Basic: sub_users=2
	if !e.CheckLimit("basic", "sub_users", 1) {
		t.Error("1 < 2 should be allowed")
	}
	if e.CheckLimit("basic", "sub_users", 2) {
		t.Error("2 >= 2 should NOT be allowed")
	}
}

func TestPlanEngine_CheckLimit_UnknownLimit(t *testing.T) {
	e := newTestEngine()

	// Unknown limit key defaults to false (security: fail-closed).
	// New limits must be explicitly added to the switch before they work.
	if e.CheckLimit("basic", "future_limit_type", 1000) {
		t.Error("unknown limit should default to denied (fail-closed)")
	}
}

func TestPlanEngine_CheckQuota(t *testing.T) {
	e := newTestEngine()

	// Basic: ingestion_bytes_per_day=1_000_000_000
	if !e.CheckQuota("basic", "ingestion_bytes_per_day", 500_000_000) {
		t.Error("500M < 1B should be allowed")
	}
	if e.CheckQuota("basic", "ingestion_bytes_per_day", 1_000_000_000) {
		t.Error("1B >= 1B should NOT be allowed")
	}

	// Unknown quota key defaults to true
	if !e.CheckQuota("basic", "unknown_quota", 999999) {
		t.Error("unknown quota should default to allowed")
	}

	// Unknown plan
	if e.CheckQuota("nonexistent", "ingestion_bytes_per_day", 0) {
		t.Error("unknown plan should return false")
	}
}

func TestPlanEngine_GetPlan(t *testing.T) {
	e := newTestEngine()

	plan, ok := e.GetPlan("basic")
	if !ok {
		t.Fatal("expected basic plan to exist")
	}
	if plan.DisplayName != "Basic" {
		t.Errorf("expected 'Basic', got %q", plan.DisplayName)
	}

	_, ok = e.GetPlan("nonexistent")
	if ok {
		t.Error("nonexistent plan should not be found")
	}
}

func TestPlanEngine_ListCreatablePlans(t *testing.T) {
	e := newTestEngine()

	plans := e.ListCreatablePlans()
	if len(plans) != 2 {
		t.Errorf("expected 2 creatable plans (basic+pro), got %d", len(plans))
	}
}

func TestPlanEngine_GetMaxTwins(t *testing.T) {
	e := newTestEngine()

	if got := e.GetMaxTwins("basic"); got != 2 {
		t.Errorf("basic max_twins: expected 2, got %d", got)
	}
	if got := e.GetMaxTwins("pro"); got != 4 {
		t.Errorf("pro max_twins: expected 4, got %d", got)
	}
	if got := e.GetMaxTwins("nonexistent"); got != 0 {
		t.Errorf("unknown plan max_twins: expected 0, got %d", got)
	}
}

func TestPlanEngine_ValidatePlanName(t *testing.T) {
	e := newTestEngine()

	if !e.ValidatePlanName("basic") {
		t.Error("basic should be a valid creatable plan")
	}
	if !e.ValidatePlanName("pro") {
		t.Error("pro should be a valid creatable plan")
	}
	if e.ValidatePlanName("enterprise") {
		t.Error("enterprise should NOT be valid (not creatable)")
	}
	if e.ValidatePlanName("nonexistent") {
		t.Error("nonexistent should NOT be valid")
	}
}

func TestPlanEngine_PlanExists(t *testing.T) {
	e := newTestEngine()

	if !e.PlanExists("basic") {
		t.Error("basic should exist")
	}
	if !e.PlanExists("enterprise") {
		t.Error("enterprise should exist (even though not creatable)")
	}
	if e.PlanExists("nonexistent") {
		t.Error("nonexistent should not exist")
	}
}

func TestPlanEngine_ReloadConfig(t *testing.T) {
	e := newTestEngine()

	if e.GetMaxTwins("basic") != 2 {
		t.Fatal("expected initial max_twins=2")
	}

	newCfg := &PlansConfig{
		Plans: map[string]PlanDefinition{
			"basic": {
				DisplayName: "Basic",
				MaxTwins:    10,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true},
			},
		},
	}
	e.ReloadConfig(newCfg)

	if got := e.GetMaxTwins("basic"); got != 10 {
		t.Errorf("after reload, expected max_twins=10, got %d", got)
	}
}
