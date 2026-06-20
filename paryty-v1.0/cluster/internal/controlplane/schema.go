// Package controlplane implements the multi-tenant control plane for the
// Paryty cluster. It manages tenants, API keys, and the PostgreSQL schema
// required for tenant isolation and authentication.
package controlplane

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tenant represents a Paryty tenant (client account).
type Tenant struct {
	ID        string `json:"id"`         // UUID
	Name      string `json:"name"`       // Human-readable tenant name.
	Status    string `json:"status"`     // active, suspended, deleted
	CreatedAt string `json:"created_at"` // ISO-8601 timestamp.
}

// APIKey represents an API key associated with a tenant.
type APIKey struct {
	KeyID     string  `json:"key_id"`     // UUID
	TenantID  string  `json:"tenant_id"`  // FK -> tenants.tenant_id
	Name      string  `json:"name"`       // Human-readable key name.
	KeyHash   string  `json:"-"`          // bcrypt hash, never serialized.
	KeyPrefix string  `json:"key_prefix"` // first 16 chars for display / O(1) lookup.
	CreatedAt string  `json:"created_at"` // ISO-8601 timestamp.
	RevokedAt *string `json:"revoked_at"` // nil if active.
}

// createControlPlaneTables holds the DDL statements for auto-creation of
// the control plane schema. All statements are idempotent.
var createControlPlaneTables = []string{
	`CREATE TABLE IF NOT EXISTS tenants (
		tenant_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE IF NOT EXISTS api_keys (
		key_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		tenant_id UUID NOT NULL REFERENCES tenants(tenant_id),
		name TEXT NOT NULL DEFAULT '',
		key_hash TEXT NOT NULL,
		key_prefix TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		revoked_at TIMESTAMPTZ,
		UNIQUE(key_hash)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_hash
		ON api_keys(key_hash) WHERE revoked_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_tenant
		ON api_keys(tenant_id)`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_prefix
		ON api_keys(key_prefix) WHERE revoked_at IS NULL`,
}

// EnsureTables creates all control plane tables and indexes if they do not
// already exist. It is safe to call on every startup -- all DDL statements
// use IF NOT EXISTS for idempotency.
func EnsureTables(ctx context.Context, pool *pgxpool.Pool) error {
	for _, ddl := range createControlPlaneTables {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("execute control plane DDL: %w", err)
		}
	}
	return nil
}

// =============================================================================
// Phase 8: Multi-Tenant SaaS Platform Schema
// =============================================================================

var createPhase8Tables = []string{
	// users — web-authenticated user accounts within a tenant.
	`CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
		email TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'operator', 'viewer')),
		permissions JSONB NOT NULL DEFAULT '{}',
		is_active BOOLEAN NOT NULL DEFAULT true,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE(tenant_id, email)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id)`,
	`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,

	// refresh_tokens — persisted refresh tokens for JWT rotation.
	`CREATE TABLE IF NOT EXISTS refresh_tokens (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL,
		device_info TEXT,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		revoked_at TIMESTAMPTZ,
		UNIQUE(token_hash)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id)`,
	`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens(token_hash) WHERE revoked_at IS NULL`,

	// tenant_plans — plan assignment with JSONB snapshot of plan features/limits.
	`CREATE TABLE IF NOT EXISTS tenant_plans (
		tenant_id UUID PRIMARY KEY REFERENCES tenants(tenant_id) ON DELETE CASCADE,
		plan_name TEXT NOT NULL,
		features JSONB NOT NULL DEFAULT '{}',
		limits JSONB NOT NULL DEFAULT '{}',
		quotas JSONB NOT NULL DEFAULT '{}',
		started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_tenant_plans_name ON tenant_plans(plan_name)`,

	// paryty_twins — digital twin definitions per tenant.
	`CREATE TABLE IF NOT EXISTS paryty_twins (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		description TEXT,
		status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'degraded', 'inactive', 'deleted')),
		twin_config JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		deleted_at TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_paryty_twins_tenant ON paryty_twins(tenant_id) WHERE deleted_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_paryty_twins_status ON paryty_twins(status) WHERE deleted_at IS NULL`,

	// agent_assignments — maps agents to their digital twin (composite PK).
	`CREATE TABLE IF NOT EXISTS agent_assignments (
		agent_id TEXT NOT NULL,
		twin_id UUID NOT NULL REFERENCES paryty_twins(id) ON DELETE CASCADE,
		tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
		client_id TEXT NOT NULL DEFAULT '',
		topic_prefix TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (agent_id, twin_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_assignments_twin ON agent_assignments(twin_id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_assignments_tenant ON agent_assignments(tenant_id)`,

	// agent_registrations — records every agent that registers, regardless
	// of twin assignment. Used to surface unassigned agents on the dashboard.
	`CREATE TABLE IF NOT EXISTS agent_registrations (
		agent_id TEXT PRIMARY KEY,
		tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
		hostname TEXT NOT NULL DEFAULT '',
		client_id TEXT NOT NULL DEFAULT '',
		first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
		last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_registrations_tenant ON agent_registrations(tenant_id)`,

	// ALTER statements for agent metadata columns (idempotent).
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS os TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS arch TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS cloud_provider TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'deployed', 'inactive'))`,

	// agent_backlogs — tracks historical backlog per agent for operator approval.
	`CREATE TABLE IF NOT EXISTS agent_backlogs (
		agent_id TEXT PRIMARY KEY,
		twin_id UUID NOT NULL REFERENCES paryty_twins(id) ON DELETE CASCADE,
		backlog_bytes BIGINT NOT NULL DEFAULT 0,
		backlog_since_epoch BIGINT NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'uploading', 'completed', 'rejected', 'deleted')),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_backlogs_twin ON agent_backlogs(twin_id)`,

	// agent_commands — pending commands to deliver to agents via heartbeat.
	`CREATE TABLE IF NOT EXISTS agent_commands (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		agent_id TEXT NOT NULL,
		command_type INT NOT NULL,
		payload TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		delivered_at TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_commands_agent ON agent_commands(agent_id, delivered_at)`,

	// ALTER statements for column additions on existing tables (idempotent).
	`ALTER TABLE agent_assignments ADD COLUMN IF NOT EXISTS client_id TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_assignments ADD COLUMN IF NOT EXISTS topic_prefix TEXT NOT NULL DEFAULT ''`,

	// ═══════════════════════════════════════════════════════════════════════
	// Dual Reality Agent System — Schema Extensions
	// ═══════════════════════════════════════════════════════════════════════

	// Extend agent_registrations with Dual Reality lifecycle columns.
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS paired_at TIMESTAMPTZ`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS unpaired_at TIMESTAMPTZ`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS retired_at TIMESTAMPTZ`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS blacklisted_at TIMESTAMPTZ`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS blacklist_reason TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS identity_token TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agent_registrations ADD COLUMN IF NOT EXISTS identity_token_hash TEXT NOT NULL DEFAULT ''`,

	// Extend agent_assignments with unpair tracking.
	`ALTER TABLE agent_assignments ADD COLUMN IF NOT EXISTS unpaired_at TIMESTAMPTZ`,

	// Extend edge agent status CHECK constraint to include Dual Reality states.
	// Uses DO block to handle constraint recreation idempotently.
	`DO $$
	BEGIN
		ALTER TABLE agent_registrations DROP CONSTRAINT IF EXISTS agent_registrations_status_check;
		ALTER TABLE agent_registrations ADD CONSTRAINT agent_registrations_status_check
			CHECK (status IN ('pending', 'deployed', 'inactive', 'unconfigured', 'active', 'lost', 'rogue', 'retired', 'blacklisted'));
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,

	// Extend cluster agent (twin) status CHECK constraint.
	`DO $$
	BEGIN
		ALTER TABLE paryty_twins DROP CONSTRAINT IF EXISTS paryty_twins_status_check;
		ALTER TABLE paryty_twins ADD CONSTRAINT paryty_twins_status_check
			CHECK (status IN ('pending', 'active', 'degraded', 'inactive', 'deleted', 'unconfigured'));
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,

	// agent_blacklist — tracks edge agents blocked from registering to a tenant.
	`CREATE TABLE IF NOT EXISTS agent_blacklist (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
		edge_agent_id TEXT NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE(tenant_id, edge_agent_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_blacklist_tenant ON agent_blacklist(tenant_id)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_blacklist_edge ON agent_blacklist(edge_agent_id)`,

	// agent_status_log — immutable audit trail for agent state transitions.
	`CREATE TABLE IF NOT EXISTS agent_status_log (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		agent_id TEXT NOT NULL,
		tenant_id UUID NOT NULL,
		from_status TEXT,
		to_status TEXT NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		actor_id UUID,
		metadata JSONB NOT NULL DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_status_log_agent ON agent_status_log(agent_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_status_log_tenant ON agent_status_log(tenant_id, created_at DESC)`,

	// RLS for new Dual Reality tables.
	`DO $$
	BEGIN
		ALTER TABLE agent_blacklist ENABLE ROW LEVEL SECURITY;
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,
	`DO $$
	BEGIN
		ALTER TABLE agent_status_log ENABLE ROW LEVEL SECURITY;
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,
	`DO $$
	BEGIN
		DROP POLICY IF EXISTS tenant_isolation_agent_blacklist ON agent_blacklist;
		CREATE POLICY tenant_isolation_agent_blacklist ON agent_blacklist
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
	END $$`,
	`DO $$
	BEGIN
		DROP POLICY IF EXISTS tenant_isolation_agent_status_log ON agent_status_log;
		CREATE POLICY tenant_isolation_agent_status_log ON agent_status_log
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
	END $$`,

	// audit_log — immutable audit trail for all security-relevant actions.
	`CREATE TABLE IF NOT EXISTS audit_log (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		tenant_id UUID NOT NULL,
		user_id UUID,
		action TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		resource_id TEXT,
		details JSONB NOT NULL DEFAULT '{}',
		ip_address TEXT,
		user_agent TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		hash TEXT -- SHA-256 hash chain for tamper detection
	)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_log_tenant ON audit_log(tenant_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_log_user ON audit_log(user_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON audit_log(resource_type, resource_id)`,

	// ── Row-Level Security ─────────────────────────────────────────────────

	// Enable RLS on tenant-scoped tables (idempotent via DO block).
	`DO $$
	BEGIN
		ALTER TABLE paryty_twins ENABLE ROW LEVEL SECURITY;
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,
	`DO $$
	BEGIN
		ALTER TABLE agent_assignments ENABLE ROW LEVEL SECURITY;
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,
	`DO $$
	BEGIN
		ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,
	`DO $$
	BEGIN
		ALTER TABLE agent_registrations ENABLE ROW LEVEL SECURITY;
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,

	// Create RLS policies: tenant_id must match the session variable.
	// Uses DROP IF EXISTS + CREATE for idempotency.
	`DO $$
	BEGIN
		DROP POLICY IF EXISTS tenant_isolation_paryty_twins ON paryty_twins;
		CREATE POLICY tenant_isolation_paryty_twins ON paryty_twins
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
	END $$`,
	`DO $$
	BEGIN
		DROP POLICY IF EXISTS tenant_isolation_agent_assignments ON agent_assignments;
		CREATE POLICY tenant_isolation_agent_assignments ON agent_assignments
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
	END $$`,
	`DO $$
	BEGIN
		DROP POLICY IF EXISTS tenant_isolation_api_keys ON api_keys;
		CREATE POLICY tenant_isolation_api_keys ON api_keys
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
	END $$`,
	`DO $$
	BEGIN
		DROP POLICY IF EXISTS tenant_isolation_agent_registrations ON agent_registrations;
		CREATE POLICY tenant_isolation_agent_registrations ON agent_registrations
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
	END $$`,

	// Enable RLS on audit_log
	`DO $$
	BEGIN
		ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
	EXCEPTION WHEN duplicate_object THEN NULL;
	END $$`,

	// Create RLS policy for audit_log
	`DO $$
	BEGIN
		DROP POLICY IF EXISTS tenant_isolation_audit_log ON audit_log;
		CREATE POLICY tenant_isolation_audit_log ON audit_log
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid);
	END $$`,
}

// EnsurePhase8Tables creates all Phase 8 multi-tenancy tables and indexes if
// they do not already exist. Safe to call on every startup.
func EnsurePhase8Tables(ctx context.Context, pool *pgxpool.Pool) error {
	for _, ddl := range createPhase8Tables {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("execute Phase 8 DDL: %w", err)
		}
	}
	return nil
}

// EnsureRLSPolicies ensures that Row-Level Security policies are properly
// configured on all tenant-scoped tables. This function is idempotent and
// safe to call on every startup.
//
// RLS policies enforce tenant isolation at the database level, providing
// defense-in-depth against cross-tenant data leaks.
func EnsureRLSPolicies(ctx context.Context, pool *pgxpool.Pool) error {
	// Enable RLS on all tenant-scoped tables
	enableRLSStatements := []string{
		`ALTER TABLE paryty_twins ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE agent_assignments ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE agent_registrations ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY`,
	}

	for _, stmt := range enableRLSStatements {
		// Use DO blocks to handle cases where RLS is already enabled
		doStmt := fmt.Sprintf(`DO $$
BEGIN
	%s;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$`, stmt)
		if _, err := pool.Exec(ctx, doStmt); err != nil {
			return fmt.Errorf("enable RLS: %w", err)
		}
	}

	// Create RLS policies for each table
	policyStatements := []string{
		`DROP POLICY IF EXISTS tenant_isolation_paryty_twins ON paryty_twins`,
		`CREATE POLICY tenant_isolation_paryty_twins ON paryty_twins
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid)`,

		`DROP POLICY IF EXISTS tenant_isolation_agent_assignments ON agent_assignments`,
		`CREATE POLICY tenant_isolation_agent_assignments ON agent_assignments
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid)`,

		`DROP POLICY IF EXISTS tenant_isolation_api_keys ON api_keys`,
		`CREATE POLICY tenant_isolation_api_keys ON api_keys
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid)`,

		`DROP POLICY IF EXISTS tenant_isolation_agent_registrations ON agent_registrations`,
		`CREATE POLICY tenant_isolation_agent_registrations ON agent_registrations
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid)`,

		`DROP POLICY IF EXISTS tenant_isolation_audit_log ON audit_log`,
		`CREATE POLICY tenant_isolation_audit_log ON audit_log
			USING (tenant_id = current_setting('app.current_tenant_id')::uuid)`,
	}

	for _, stmt := range policyStatements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("create RLS policy: %w", err)
		}
	}

	return nil
}
