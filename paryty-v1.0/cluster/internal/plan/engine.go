package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PlanEngine is the runtime plan manager. It loads plan definitions from YAML
// and resolves effective plans for tenants by merging the definition with the
// tenant's stored snapshot (if any). All read operations are lock-free via
// sync.RWMutex.
type PlanEngine struct {
	mu     sync.RWMutex
	config *PlansConfig
	db     *pgxpool.Pool
}

// NewPlanEngine creates a PlanEngine with the given plan configuration.
// The db parameter may be nil if only in-memory plan operations are needed.
func NewPlanEngine(config *PlansConfig, db *pgxpool.Pool) *PlanEngine {
	return &PlanEngine{
		config: config,
		db:     db,
	}
}

// ReloadConfig atomically replaces the plan configuration. Safe for concurrent use.
func (e *PlanEngine) ReloadConfig(config *PlansConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

// HasFeature returns true if the named plan has the given feature enabled.
func (e *PlanEngine) HasFeature(planName, feature string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.config.Plans[planName]
	if !ok {
		return false
	}
	enabled, ok := plan.Features[feature]
	return ok && enabled
}

// CheckLimit returns true if current usage is within the plan's limit for the
// named resource. Returns false if the limit is exceeded or the plan is unknown.
func (e *PlanEngine) CheckLimit(planName, limit string, current int) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.config.Plans[planName]
	if !ok {
		return false
	}

	switch limit {
	case "agents_per_twin":
		return current < plan.Limits.AgentsPerTwin
	case "alert_rules_per_tenant":
		return current < plan.Limits.AlertRulesPerTenant
	case "sub_users":
		return current < plan.Limits.SubUsers
	case "max_twins":
		return current < plan.MaxTwins
	default:
		// Unknown limits default to denied (fail closed for security — new limits
		// must be explicitly added to the switch before they can be used).
		return false
	}
}

// CheckQuota returns true if current usage is within the plan's quota.
func (e *PlanEngine) CheckQuota(planName, quota string, current int64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.config.Plans[planName]
	if !ok {
		return false
	}

	switch quota {
	case "ingestion_bytes_per_day":
		return current < plan.Quotas.IngestionBytesPerDay
	default:
		return true
	}
}

// GetPlan returns the plan definition for the given name. The second return
// value is false if the plan does not exist.
func (e *PlanEngine) GetPlan(planName string) (PlanDefinition, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.config.Plans[planName]
	return plan, ok
}

// NamedPlan pairs a plan name (the YAML key) with its definition.
type NamedPlan struct {
	Name string
	Def  PlanDefinition
}

// ListCreatablePlans returns all plans that are available for self-signup.
func (e *PlanEngine) ListCreatablePlans() []PlanDefinition {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var plans []PlanDefinition
	for _, plan := range e.config.Plans {
		if plan.Creatable {
			plans = append(plans, plan)
		}
	}
	return plans
}

// ListAllPlans returns every defined plan with its YAML key name.
// Safe for concurrent use.
func (e *PlanEngine) ListAllPlans() []NamedPlan {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plans := make([]NamedPlan, 0, len(e.config.Plans))
	for name, def := range e.config.Plans {
		plans = append(plans, NamedPlan{Name: name, Def: def})
	}
	return plans
}

// GetMaxTwins returns the maximum number of twins allowed for a plan.
// Returns 0 if the plan is unknown.
func (e *PlanEngine) GetMaxTwins(planName string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.config.Plans[planName]
	if !ok {
		return 0
	}
	return plan.MaxTwins
}

// GetAgentsPerTwin returns the maximum number of agents allowed per twin
// for the given plan. Returns 0 if the plan is unknown.
func (e *PlanEngine) GetAgentsPerTwin(planName string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.config.Plans[planName]
	if !ok {
		return 0
	}
	return plan.Limits.AgentsPerTwin
}

// ValidatePlanName returns true if the plan name exists and is creatable
// (i.e., available for self-signup).
func (e *PlanEngine) ValidatePlanName(planName string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.config.Plans[planName]
	return ok && plan.Creatable
}

// PlanExists returns true if the plan name is defined (including non-creatable plans).
func (e *PlanEngine) PlanExists(planName string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	_, ok := e.config.Plans[planName]
	return ok
}

// =============================================================================
// Database Operations — Tenant Plan Assignment
// =============================================================================

// StoredTenantPlan is the DB representation of a tenant's plan assignment.
type StoredTenantPlan struct {
	TenantID  string           `json:"tenant_id"`
	PlanName  string           `json:"plan_name"`
	Features  *json.RawMessage `json:"features"`
	Limits    *json.RawMessage `json:"limits"`
	Quotas    *json.RawMessage `json:"quotas"`
	StartedAt string           `json:"started_at"`
	ExpiresAt *string          `json:"expires_at"`
}

// AssignPlan inserts or updates a tenant's plan assignment in PostgreSQL.
// It snapshots the current plan definition's features, limits, and quotas as JSONB.
func (e *PlanEngine) AssignPlan(ctx context.Context, tenantID, planName string) error {
	if e.db == nil {
		return fmt.Errorf("plan engine has no database connection")
	}

	plan, ok := e.GetPlan(planName)
	if !ok {
		return fmt.Errorf("unknown plan: %s", planName)
	}

	featuresJSON, err := json.Marshal(plan.Features)
	if err != nil {
		return fmt.Errorf("marshal plan features: %w", err)
	}
	limitsJSON, err := json.Marshal(plan.Limits)
	if err != nil {
		return fmt.Errorf("marshal plan limits: %w", err)
	}
	quotasJSON, err := json.Marshal(plan.Quotas)
	if err != nil {
		return fmt.Errorf("marshal plan quotas: %w", err)
	}

	_, err = e.db.Exec(ctx, `
		INSERT INTO tenant_plans (tenant_id, plan_name, features, limits, quotas)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id) DO UPDATE SET
			plan_name = EXCLUDED.plan_name,
			features = EXCLUDED.features,
			limits = EXCLUDED.limits,
			quotas = EXCLUDED.quotas,
			started_at = now()
	`, tenantID, planName, featuresJSON, limitsJSON, quotasJSON)
	if err != nil {
		return fmt.Errorf("assign plan %s to tenant %s: %w", planName, tenantID, err)
	}

	return nil
}

// GetTenantPlan retrieves the stored plan assignment for a tenant.
func (e *PlanEngine) GetTenantPlan(ctx context.Context, tenantID string) (*StoredTenantPlan, error) {
	if e.db == nil {
		return nil, fmt.Errorf("plan engine has no database connection")
	}

	var plan StoredTenantPlan
	err := e.db.QueryRow(ctx, `
		SELECT tenant_id, plan_name, features, limits, quotas,
		       to_char(started_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       CASE WHEN expires_at IS NOT NULL THEN to_char(expires_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"') END
		FROM tenant_plans
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&plan.TenantID, &plan.PlanName,
		&plan.Features, &plan.Limits, &plan.Quotas,
		&plan.StartedAt, &plan.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get tenant plan for %s: %w", tenantID, err)
	}

	return &plan, nil
}
