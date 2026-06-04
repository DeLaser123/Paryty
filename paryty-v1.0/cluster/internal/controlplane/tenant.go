package controlplane

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// TenantManager provides CRUD operations for tenants backed by PostgreSQL.
// All methods are safe for concurrent use (pgxpool handles connection pooling).
type TenantManager struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

// NewTenantManager creates a TenantManager wired to the given pool and logger.
// Panics if logger is nil.
func NewTenantManager(pool *pgxpool.Pool, logger *zap.Logger) *TenantManager {
	if logger == nil {
		panic("controlplane.TenantManager: logger must not be nil")
	}
	return &TenantManager{
		pool:   pool,
		logger: logger.Named("tenant-manager"),
	}
}

// CreateTenant inserts a new tenant with status "active" and returns the
// generated UUID. The name must not be empty.
func (m *TenantManager) CreateTenant(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("create tenant: name must not be empty")
	}

	var id string
	err := m.pool.QueryRow(ctx,
		`INSERT INTO tenants (name) VALUES ($1) RETURNING tenant_id`,
		name,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create tenant %q: %w", name, err)
	}

	m.logger.Info("tenant created",
		zap.String("tenant_id", id),
		zap.String("name", name),
	)
	return id, nil
}

// GetTenant retrieves a tenant by ID. Returns an error if the tenant does
// not exist.
func (m *TenantManager) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("get tenant: tenant_id must not be empty")
	}

	t := &Tenant{}
	err := m.pool.QueryRow(ctx,
		`SELECT tenant_id, name, status, created_at
		 FROM tenants WHERE tenant_id = $1`,
		tenantID,
	).Scan(&t.ID, &t.Name, &t.Status, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get tenant %s: %w", tenantID, err)
	}
	return t, nil
}

// ListTenants returns all tenants ordered by creation time (newest first).
// An empty slice (not nil) is returned when no tenants exist.
func (m *TenantManager) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := m.pool.Query(ctx,
		`SELECT tenant_id, name, status, created_at
		 FROM tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	tenants := make([]Tenant, 0)
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	return tenants, nil
}

// UpdateStatus changes a tenant's status. Valid values: active, suspended, deleted.
// Returns an error if the tenant does not exist.
func (m *TenantManager) UpdateStatus(ctx context.Context, tenantID, status string) error {
	if tenantID == "" {
		return fmt.Errorf("update status: tenant_id must not be empty")
	}
	switch status {
	case "active", "suspended", "deleted":
		// valid
	default:
		return fmt.Errorf("update status: invalid status %q (must be active, suspended, or deleted)", status)
	}

	tag, err := m.pool.Exec(ctx,
		`UPDATE tenants SET status = $1 WHERE tenant_id = $2`,
		status, tenantID,
	)
	if err != nil {
		return fmt.Errorf("update tenant %s status to %s: %w", tenantID, status, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update status: tenant %s not found", tenantID)
	}

	m.logger.Info("tenant status updated",
		zap.String("tenant_id", tenantID),
		zap.String("new_status", status),
	)
	return nil
}
