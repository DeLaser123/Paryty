package plan

import (
	"testing"
)

func TestLoadPlans_Success(t *testing.T) {
	cfg, err := LoadPlans("../../../configs/cluster/plans.yaml")
	if err != nil {
		t.Fatalf("LoadPlans failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("config must not be nil")
	}
	if len(cfg.Plans) < 4 {
		t.Errorf("expected at least 4 plans, got %d", len(cfg.Plans))
	}

	// Verify basic plan
	basic, ok := cfg.Plans["basic"]
	if !ok {
		t.Fatal("expected 'basic' plan")
	}
	if basic.DisplayName != "Basic" {
		t.Errorf("expected display_name 'Basic', got %q", basic.DisplayName)
	}
	if basic.MaxTwins != 2 {
		t.Errorf("expected max_twins 2, got %d", basic.MaxTwins)
	}
	if !basic.Creatable {
		t.Error("basic plan should be creatable")
	}
	if !basic.Features["topology_monitoring"] {
		t.Error("basic plan should have topology_monitoring")
	}
	if basic.Features["paryty_intel"] {
		t.Error("basic plan should NOT have paryty_intel")
	}
}

func TestLoadPlans_EnterpriseExists(t *testing.T) {
	cfg, err := LoadPlans("../../../configs/cluster/plans.yaml")
	if err != nil {
		t.Fatalf("LoadPlans failed: %v", err)
	}

	ent, ok := cfg.Plans["enterprise"]
	if !ok {
		t.Fatal("expected 'enterprise' plan")
	}
	if ent.Creatable {
		t.Error("enterprise plan should NOT be creatable")
	}
	if ent.MaxTwins != 10 {
		t.Errorf("expected enterprise max_twins 10, got %d", ent.MaxTwins)
	}
}

func TestLoadPlans_FileNotFound(t *testing.T) {
	_, err := LoadPlans("nonexistent/plans.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoadPlans_InvalidYAML(t *testing.T) {
	_, err := LoadPlans("../../../configs/cluster/cluster.yaml")
	// cluster.yaml is valid YAML but doesn't have a "plans" key.
	// The yaml.Unmarshal won't error, but Plans map will be empty.
	if err == nil {
		t.Fatal("expected error for YAML without plans key")
	}
}
