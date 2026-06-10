//go:build integration

package integration

import (
	"testing"

	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	"github.com/paryty/paryty-v1.0/cluster/internal/twin"
)

// =============================================================================
// Test Helpers
// =============================================================================

// newPlanEngine creates a PlanEngine with test plan definitions and no DB.
func newPlanEngine() *plan.PlanEngine {
	cfg := &plan.PlansConfig{
		Plans: map[string]plan.PlanDefinition{
			"basic": {
				DisplayName: "Basic",
				MaxTwins:    2,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": false, "paryty_intel": false},
				Limits:      plan.LimitSet{AgentsPerTwin: 10, SubUsers: 2, AlertRulesPerTenant: 5},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 1_000_000_000, QueryRequestsPerMinute: 60},
			},
			"pro": {
				DisplayName: "Pro",
				MaxTwins:    4,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": false},
				Limits:      plan.LimitSet{AgentsPerTwin: 50, SubUsers: 10, AlertRulesPerTenant: 20},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 10_000_000_000, QueryRequestsPerMinute: 300},
			},
			"pro_plus": {
				DisplayName: "Pro+",
				MaxTwins:    8,
				Creatable:   true,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": true, "custom_retention": false},
				Limits:      plan.LimitSet{AgentsPerTwin: 100, SubUsers: 25, AlertRulesPerTenant: 50},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 50_000_000_000, QueryRequestsPerMinute: 600},
			},
			"enterprise": {
				DisplayName: "Enterprise",
				MaxTwins:    10,
				Creatable:   false,
				Features:    map[string]bool{"topology_monitoring": true, "metrics": true, "paryty_intel": true, "custom_retention": true},
				Limits:      plan.LimitSet{AgentsPerTwin: 200, SubUsers: 100, AlertRulesPerTenant: 100},
				Quotas:      plan.QuotaSet{IngestionBytesPerDay: 100_000_000_000, QueryRequestsPerMinute: 1000},
			},
		},
	}
	return plan.NewPlanEngine(cfg, nil)
}

// =============================================================================
// AgentAssigner Registration Patterns (No DB — struct-level tests)
// =============================================================================

func TestAgentAssignment_StructFields(t *testing.T) {
	// Validate the AgentAssignment struct is well-formed.
	assign := twin.AgentAssignment{
		AgentID:  "agent-001",
		TwinID:   "twin-abc",
		TenantID: "tenant-xyz",
	}

	if assign.AgentID != "agent-001" {
		t.Errorf("AgentID: expected 'agent-001', got %q", assign.AgentID)
	}
	if assign.TwinID != "twin-abc" {
		t.Errorf("TwinID: expected 'twin-abc', got %q", assign.TwinID)
	}
	if assign.TenantID != "tenant-xyz" {
		t.Errorf("TenantID: expected 'tenant-xyz', got %q", assign.TenantID)
	}
	// CreatedAt defaults to zero value — fine for a struct literal.
}

func TestAgentAssignment_Equality(t *testing.T) {
	a := twin.AgentAssignment{
		AgentID:  "agent-001",
		TwinID:   "twin-abc",
		TenantID: "tenant-xyz",
	}
	b := twin.AgentAssignment{
		AgentID:  "agent-001",
		TwinID:   "twin-abc",
		TenantID: "tenant-xyz",
	}
	c := twin.AgentAssignment{
		AgentID:  "agent-002",
		TwinID:   "twin-abc",
		TenantID: "tenant-xyz",
	}

	if a != b {
		t.Error("identical assignments should be equal")
	}
	if a == c {
		t.Error("assignments with different AgentID should not be equal")
	}
}

// =============================================================================
// PlanEngine Feature Gating (via engine, no DB)
// =============================================================================

func TestPlanEngine_FeatureGating_BasicPlan(t *testing.T) {
	e := newPlanEngine()

	// Basic plan: topology_monitoring only (no metrics, no paryty_intel).
	if !e.HasFeature("basic", "topology_monitoring") {
		t.Error("basic should have topology_monitoring")
	}
	if e.HasFeature("basic", "metrics") {
		t.Error("basic should NOT have metrics")
	}
	if e.HasFeature("basic", "paryty_intel") {
		t.Error("basic should NOT have paryty_intel")
	}
}

func TestPlanEngine_FeatureGating_ProPlan(t *testing.T) {
	e := newPlanEngine()

	if !e.HasFeature("pro", "topology_monitoring") {
		t.Error("pro should have topology_monitoring")
	}
	if !e.HasFeature("pro", "metrics") {
		t.Error("pro should have metrics")
	}
	if e.HasFeature("pro", "paryty_intel") {
		t.Error("pro should NOT have paryty_intel")
	}
}

func TestPlanEngine_FeatureGating_ProPlusPlan(t *testing.T) {
	e := newPlanEngine()

	if !e.HasFeature("pro_plus", "topology_monitoring") {
		t.Error("pro_plus should have topology_monitoring")
	}
	if !e.HasFeature("pro_plus", "metrics") {
		t.Error("pro_plus should have metrics")
	}
	if !e.HasFeature("pro_plus", "paryty_intel") {
		t.Error("pro_plus should have paryty_intel")
	}
	if e.HasFeature("pro_plus", "custom_retention") {
		t.Error("pro_plus should NOT have custom_retention")
	}
}

func TestPlanEngine_FeatureGating_EnterprisePlan(t *testing.T) {
	e := newPlanEngine()

	if !e.HasFeature("enterprise", "paryty_intel") {
		t.Error("enterprise should have paryty_intel")
	}
	if !e.HasFeature("enterprise", "custom_retention") {
		t.Error("enterprise should have custom_retention")
	}
}

func TestPlanEngine_FeatureGating_UnknownPlan(t *testing.T) {
	e := newPlanEngine()

	if e.HasFeature("nonexistent", "topology_monitoring") {
		t.Error("unknown plan should return false for any feature")
	}
	if e.HasFeature("nonexistent", "anything") {
		t.Error("unknown plan should return false for any feature")
	}
}

// =============================================================================
// PlanEngine Twin Limit Logic
// =============================================================================

func TestPlanEngine_TwinLimit_Basic(t *testing.T) {
	e := newPlanEngine()

	// Basic: max_twins = 2.
	if max := e.GetMaxTwins("basic"); max != 2 {
		t.Fatalf("expected max_twins=2, got %d", max)
	}

	// CheckLimit with various current counts.
	if !e.CheckLimit("basic", "max_twins", 0) {
		t.Error("0 < 2 should be allowed")
	}
	if !e.CheckLimit("basic", "max_twins", 1) {
		t.Error("1 < 2 should be allowed")
	}
	if e.CheckLimit("basic", "max_twins", 2) {
		t.Error("2 >= 2 should NOT be allowed (strict less-than)")
	}
	if e.CheckLimit("basic", "max_twins", 100) {
		t.Error("100 >= 2 should NOT be allowed")
	}
}

func TestPlanEngine_TwinLimit_Pro(t *testing.T) {
	e := newPlanEngine()

	if max := e.GetMaxTwins("pro"); max != 4 {
		t.Fatalf("expected max_twins=4, got %d", max)
	}

	if !e.CheckLimit("pro", "max_twins", 3) {
		t.Error("3 < 4 should be allowed")
	}
	if e.CheckLimit("pro", "max_twins", 4) {
		t.Error("4 >= 4 should NOT be allowed")
	}
}

func TestPlanEngine_TwinLimit_ProPlus(t *testing.T) {
	e := newPlanEngine()

	if max := e.GetMaxTwins("pro_plus"); max != 8 {
		t.Fatalf("expected max_twins=8, got %d", max)
	}

	if !e.CheckLimit("pro_plus", "max_twins", 7) {
		t.Error("7 < 8 should be allowed")
	}
	if e.CheckLimit("pro_plus", "max_twins", 8) {
		t.Error("8 >= 8 should NOT be allowed")
	}
}

func TestPlanEngine_TwinLimit_Enterprise(t *testing.T) {
	e := newPlanEngine()

	if max := e.GetMaxTwins("enterprise"); max != 10 {
		t.Fatalf("expected max_twins=10, got %d", max)
	}
}

func TestPlanEngine_TwinLimit_UnknownPlan(t *testing.T) {
	e := newPlanEngine()

	if max := e.GetMaxTwins("nonexistent"); max != 0 {
		t.Errorf("unknown plan should have max_twins=0, got %d", max)
	}

	if e.CheckLimit("nonexistent", "max_twins", 0) {
		t.Error("unknown plan should return false for CheckLimit")
	}
}

// =============================================================================
// PlanEngine Agent Limit per Twin
// =============================================================================

func TestPlanEngine_AgentLimit(t *testing.T) {
	e := newPlanEngine()

	// Basic: agents_per_twin = 10
	if !e.CheckLimit("basic", "agents_per_twin", 5) {
		t.Error("5 < 10 should be allowed")
	}
	if e.CheckLimit("basic", "agents_per_twin", 10) {
		t.Error("10 >= 10 should NOT be allowed")
	}

	// Pro: agents_per_twin = 50
	if !e.CheckLimit("pro", "agents_per_twin", 49) {
		t.Error("49 < 50 should be allowed")
	}
	if e.CheckLimit("pro", "agents_per_twin", 50) {
		t.Error("50 >= 50 should NOT be allowed")
	}

	// Pro+: agents_per_twin = 100
	if !e.CheckLimit("pro_plus", "agents_per_twin", 99) {
		t.Error("99 < 100 should be allowed")
	}

	// Enterprise: agents_per_twin = 200
	if !e.CheckLimit("enterprise", "agents_per_twin", 199) {
		t.Error("199 < 200 should be allowed")
	}
}

// =============================================================================
// PlanEngine SubUsers Limit
// =============================================================================

func TestPlanEngine_SubUsersLimit(t *testing.T) {
	e := newPlanEngine()

	// Basic: sub_users = 2
	if !e.CheckLimit("basic", "sub_users", 1) {
		t.Error("1 < 2 should be allowed")
	}
	if e.CheckLimit("basic", "sub_users", 2) {
		t.Error("2 >= 2 should NOT be allowed")
	}

	// Pro: sub_users = 10
	if !e.CheckLimit("pro", "sub_users", 9) {
		t.Error("9 < 10 should be allowed")
	}
}

// =============================================================================
// Twin Config Struct Tests
// =============================================================================

func TestTwinConfig_DefaultValues(t *testing.T) {
	cfg := twin.TwinConfig{
		CollectionIntervalSeconds: 60,
		SamplingRate:              1.0,
	}

	if cfg.CollectionIntervalSeconds != 60 {
		t.Errorf("expected 60, got %d", cfg.CollectionIntervalSeconds)
	}
	if cfg.SamplingRate != 1.0 {
		t.Errorf("expected 1.0, got %f", cfg.SamplingRate)
	}
	if cfg.AgentLabels != nil {
		t.Error("AgentLabels should be nil by default")
	}
	if cfg.EnabledCollectors != nil {
		t.Error("EnabledCollectors should be nil by default")
	}
}

func TestTwinConfig_WithLabels(t *testing.T) {
	cfg := twin.TwinConfig{
		AgentLabels: map[string]string{
			"env":     "production",
			"region":  "us-east-1",
			"version": "1.0.0",
		},
	}

	if len(cfg.AgentLabels) != 3 {
		t.Errorf("expected 3 labels, got %d", len(cfg.AgentLabels))
	}
	if cfg.AgentLabels["env"] != "production" {
		t.Errorf("expected 'production', got %q", cfg.AgentLabels["env"])
	}
}

// =============================================================================
// Twin Struct Tests
// =============================================================================

func TestTwinStruct_Fields(t *testing.T) {
	tr := twin.Twin{
		ID:          "twin-abc",
		TenantID:    "tenant-xyz",
		Name:        "Production Cluster",
		Description: "Main production environment",
		Status:      "active",
		AgentCount:  5,
	}

	if tr.ID != "twin-abc" {
		t.Errorf("ID: expected 'twin-abc', got %q", tr.ID)
	}
	if tr.TenantID != "tenant-xyz" {
		t.Errorf("TenantID: expected 'tenant-xyz', got %q", tr.TenantID)
	}
	if tr.Name != "Production Cluster" {
		t.Errorf("Name: expected 'Production Cluster', got %q", tr.Name)
	}
	if tr.Description != "Main production environment" {
		t.Errorf("Description: expected 'Main production environment', got %q", tr.Description)
	}
	if tr.Status != "active" {
		t.Errorf("Status: expected 'active', got %q", tr.Status)
	}
	if tr.AgentCount != 5 {
		t.Errorf("AgentCount: expected 5, got %d", tr.AgentCount)
	}
}

func TestTwin_DeletedAt_NilByDefault(t *testing.T) {
	tr := twin.Twin{
		ID:       "twin-abc",
		TenantID: "tenant-xyz",
		Name:     "Test Twin",
		Status:   "active",
	}

	if tr.DeletedAt != nil {
		t.Error("DeletedAt should be nil by default")
	}
}

// =============================================================================
// NewAgentAssigner without DB (nil pool)
// =============================================================================

func TestNewAgentAssigner_NilDB(t *testing.T) {
	// Creating an AgentAssigner with nil DB should not panic.
	assigner := twin.NewAgentAssigner(nil)
	if assigner == nil {
		t.Error("NewAgentAssigner should not return nil")
	}
}
