package controlplane

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRLS_TenantIsolation_ParytyTwins verifies that Tenant A cannot read Tenant B's twins.
func TestRLS_TenantIsolation_ParytyTwins(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping RLS integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Ensure tables and RLS policies exist
	err = EnsureTables(ctx, pool)
	require.NoError(t, err)
	err = EnsurePhase8Tables(ctx, pool)
	require.NoError(t, err)
	err = EnsureRLSPolicies(ctx, pool)
	require.NoError(t, err)

	// Create two tenants
	tenantA := uuid.New().String()
	tenantB := uuid.New().String()

	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantA, "Tenant A")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantB, "Tenant B")
	require.NoError(t, err)

	// Create a twin for Tenant A
	twinA := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO paryty_twins (id, tenant_id, name, description, status) 
		VALUES ($1, $2, 'Twin A', 'Test twin for Tenant A', 'active')`, twinA, tenantA)
	require.NoError(t, err)

	// Create a twin for Tenant B
	twinB := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO paryty_twins (id, tenant_id, name, description, status) 
		VALUES ($1, $2, 'Twin B', 'Test twin for Tenant B', 'active')`, twinB, tenantB)
	require.NoError(t, err)

	// Set tenant context for Tenant A
	_, err = pool.Exec(ctx, "SET LOCAL app.current_tenant_id = $1", tenantA)
	require.NoError(t, err)

	// Tenant A should only see their own twin
	rows, err := pool.Query(ctx, "SELECT id FROM paryty_twins")
	require.NoError(t, err)
	defer rows.Close()

	var twinIDs []string
	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		require.NoError(t, err)
		twinIDs = append(twinIDs, id)
	}
	err = rows.Err()
	require.NoError(t, err)

	// Should only contain twinA, not twinB
	assert.Contains(t, twinIDs, twinA, "Tenant A should see their own twin")
	assert.NotContains(t, twinIDs, twinB, "Tenant A should not see Tenant B's twin")
}

// TestRLS_TenantIsolation_AuditLog verifies that Tenant A cannot read Tenant B's audit logs.
func TestRLS_TenantIsolation_AuditLog(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping RLS integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Ensure tables and RLS policies exist
	err = EnsureTables(ctx, pool)
	require.NoError(t, err)
	err = EnsurePhase8Tables(ctx, pool)
	require.NoError(t, err)
	err = EnsureRLSPolicies(ctx, pool)
	require.NoError(t, err)

	// Create two tenants
	tenantA := uuid.New().String()
	tenantB := uuid.New().String()

	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantA, "Tenant A")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantB, "Tenant B")
	require.NoError(t, err)

	// Create audit log entries for Tenant A
	auditA := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO audit_log (id, tenant_id, action, resource_type, details) 
		VALUES ($1, $2, 'test.action', 'test', '{"test": true}')`, auditA, tenantA)
	require.NoError(t, err)

	// Create audit log entries for Tenant B
	auditB := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO audit_log (id, tenant_id, action, resource_type, details) 
		VALUES ($1, $2, 'test.action', 'test', '{"test": true}')`, auditB, tenantB)
	require.NoError(t, err)

	// Set tenant context for Tenant A
	_, err = pool.Exec(ctx, "SET LOCAL app.current_tenant_id = $1", tenantA)
	require.NoError(t, err)

	// Tenant A should only see their own audit logs
	rows, err := pool.Query(ctx, "SELECT id FROM audit_log")
	require.NoError(t, err)
	defer rows.Close()

	var auditIDs []string
	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		require.NoError(t, err)
		auditIDs = append(auditIDs, id)
	}
	err = rows.Err()
	require.NoError(t, err)

	// Should only contain auditA, not auditB
	assert.Contains(t, auditIDs, auditA, "Tenant A should see their own audit log")
	assert.NotContains(t, auditIDs, auditB, "Tenant A should not see Tenant B's audit log")
}

// TestRLS_SuperuserBypass verifies that superuser can bypass RLS.
func TestRLS_SuperuserBypass(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping RLS integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Ensure tables and RLS policies exist
	err = EnsureTables(ctx, pool)
	require.NoError(t, err)
	err = EnsurePhase8Tables(ctx, pool)
	require.NoError(t, err)
	err = EnsureRLSPolicies(ctx, pool)
	require.NoError(t, err)

	// Create two tenants
	tenantA := uuid.New().String()
	tenantB := uuid.New().String()

	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantA, "Tenant A")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantB, "Tenant B")
	require.NoError(t, err)

	// Create twins for both tenants
	twinA := uuid.New().String()
	twinB := uuid.New().String()

	_, err = pool.Exec(ctx, `INSERT INTO paryty_twins (id, tenant_id, name, description, status) 
		VALUES ($1, $2, 'Twin A', 'Test twin for Tenant A', 'active')`, twinA, tenantA)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO paryty_twins (id, tenant_id, name, description, status) 
		VALUES ($1, $2, 'Twin B', 'Test twin for Tenant B', 'active')`, twinB, tenantB)
	require.NoError(t, err)

	// Don't set tenant context - this simulates superuser access
	// Superuser should see all twins
	rows, err := pool.Query(ctx, "SELECT id FROM paryty_twins")
	require.NoError(t, err)
	defer rows.Close()

	var twinIDs []string
	for rows.Next() {
		var id string
		err := rows.Scan(&id)
		require.NoError(t, err)
		twinIDs = append(twinIDs, id)
	}
	err = rows.Err()
	require.NoError(t, err)

	// Should contain both twins
	assert.Contains(t, twinIDs, twinA, "Superuser should see Tenant A's twin")
	assert.Contains(t, twinIDs, twinB, "Superuser should see Tenant B's twin")
}

// TestRLS_CrossTenantWritePrevention verifies that Tenant A cannot write to Tenant B's data.
func TestRLS_CrossTenantWritePrevention(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping RLS integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Ensure tables and RLS policies exist
	err = EnsureTables(ctx, pool)
	require.NoError(t, err)
	err = EnsurePhase8Tables(ctx, pool)
	require.NoError(t, err)
	err = EnsureRLSPolicies(ctx, pool)
	require.NoError(t, err)

	// Create two tenants
	tenantA := uuid.New().String()
	tenantB := uuid.New().String()

	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantA, "Tenant A")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantB, "Tenant B")
	require.NoError(t, err)

	// Create a twin for Tenant B
	twinB := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO paryty_twins (id, tenant_id, name, description, status) 
		VALUES ($1, $2, 'Twin B', 'Test twin for Tenant B', 'active')`, twinB, tenantB)
	require.NoError(t, err)

	// Set tenant context for Tenant A
	_, err = pool.Exec(ctx, "SET LOCAL app.current_tenant_id = $1", tenantA)
	require.NoError(t, err)

	// Tenant A should not be able to update Tenant B's twin
	_, err = pool.Exec(ctx, `UPDATE paryty_twins SET name = 'Hacked' WHERE id = $1`, twinB)
	// This should fail or affect 0 rows
	if err != nil {
		// Expected - RLS prevented the update
		assert.Contains(t, err.Error(), "permission denied", "RLS should prevent cross-tenant updates")
	} else {
		// If no error, verify the update didn't actually happen
		var name string
		err = pool.QueryRow(ctx, "SELECT name FROM paryty_twins WHERE id = $1", twinB).Scan(&name)
		require.NoError(t, err)
		assert.Equal(t, "Twin B", name, "Tenant B's twin should not be modified by Tenant A")
	}
}

// TestRLS_EnsureRLSPolicies_Idempotent verifies that EnsureRLSPolicies is idempotent.
func TestRLS_EnsureRLSPolicies_Idempotent(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping RLS integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Ensure tables exist
	err = EnsureTables(ctx, pool)
	require.NoError(t, err)
	err = EnsurePhase8Tables(ctx, pool)
	require.NoError(t, err)

	// Run EnsureRLSPolicies multiple times - should not fail
	for i := 0; i < 3; i++ {
		err = EnsureRLSPolicies(ctx, pool)
		assert.NoError(t, err, "EnsureRLSPolicies should be idempotent (run %d)", i+1)
	}
}
