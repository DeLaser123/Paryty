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
