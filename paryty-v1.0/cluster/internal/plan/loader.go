// Package plan implements SaaS plan definitions, feature-gating, and
// quota/limit enforcement for the Paryty multi-tenant platform.
//
// Plan definitions live in a version-controlled YAML file (configs/cluster/plans.yaml).
// Tenant plan assignments are snapshotted as JSONB in the tenant_plans PostgreSQL table
// at signup time and can be explicitly synced to pick up definition changes.
package plan

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PlansConfig is the top-level structure of the plans YAML file.
type PlansConfig struct {
	Plans map[string]PlanDefinition `yaml:"plans"`
}

// PlanDefinition defines the capabilities, limits, and quotas for a single plan tier.
type PlanDefinition struct {
	DisplayName string            `yaml:"display_name"`
	MaxTwins    int               `yaml:"max_twins"`
	Creatable   bool              `yaml:"creatable"`
	Features    map[string]bool   `yaml:"features"`
	Limits      LimitSet          `yaml:"limits"`
	Quotas      QuotaSet          `yaml:"quotas"`
}

// FeatureSet is a map of feature flag → enabled state.
type FeatureSet map[string]bool

// LimitSet defines usage caps (count-based) for a plan tier.
type LimitSet struct {
	AgentsPerTwin       int    `yaml:"agents_per_twin"`
	DataRetentionDays   int    `yaml:"data_retention_days"`
	MetricsResolution   string `yaml:"metrics_resolution"`
	AlertRulesPerTenant int    `yaml:"alert_rules_per_tenant"`
	SubUsers            int    `yaml:"sub_users"`
}

// QuotaSet defines throughput caps (rate-based) for a plan tier.
type QuotaSet struct {
	IngestionBytesPerDay     int64 `yaml:"ingestion_bytes_per_day"`
	QueryRequestsPerMinute   int   `yaml:"query_requests_per_minute"`
}

// LoadPlans reads and parses the plans YAML configuration file.
// Returns an error if the file cannot be read or contains invalid YAML.
func LoadPlans(path string) (*PlansConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plans config %q: %w", path, err)
	}

	var cfg PlansConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse plans config %q: %w", path, err)
	}

	if len(cfg.Plans) == 0 {
		return nil, fmt.Errorf("plans config %q contains no plan definitions", path)
	}

	// Validate that every plan has at least the required fields.
	for name, plan := range cfg.Plans {
		if name == "" {
			return nil, fmt.Errorf("plans config %q contains a plan with an empty name", path)
		}
		if plan.DisplayName == "" {
			return nil, fmt.Errorf("plan %q is missing display_name", name)
		}
		if plan.MaxTwins <= 0 {
			return nil, fmt.Errorf("plan %q has invalid max_twins: %d", name, plan.MaxTwins)
		}
	}

	return &cfg, nil
}
