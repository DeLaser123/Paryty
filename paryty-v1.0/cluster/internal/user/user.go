// Package user implements sub-user management within a Paryty tenant.
// Users are scoped to a tenant and have a role (admin/operator/viewer)
// with optional per-user permission overrides stored as JSONB.
package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/auth"
)

// User represents a user account within a Paryty tenant.
type User struct {
	ID           string
	TenantID     string
	Email        string
	Name         string
	Role         string
	Permissions  map[string]bool
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserManager handles CRUD operations for tenant users.
type UserManager struct {
	db *pgxpool.Pool
}

// NewUserManager creates a new UserManager.
func NewUserManager(db *pgxpool.Pool) *UserManager {
	return &UserManager{db: db}
}

// CreateSubUser creates a new sub-user within a tenant. Only tenant admins
// should call this (enforced by RBAC middleware, not here).
func (m *UserManager) CreateSubUser(ctx context.Context, tenantID, email, password, name, role string, permissions map[string]bool) (*User, error) {
	// Validate password policy.
	if err := auth.ValidatePasswordPolicy(password); err != nil {
		return nil, fmt.Errorf("password policy: %w", err)
	}

	// Validate role.
	if !isValidRole(role) {
		return nil, fmt.Errorf("invalid role: %s (must be admin, operator, or viewer)", role)
	}

	// Hash password.
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// Serialize permissions to JSON.
	permsJSON, err := MarshalPermissions(permissions)
	if err != nil {
		return nil, fmt.Errorf("marshal permissions: %w", err)
	}

	user := &User{
		ID:          uuid.New().String(),
		TenantID:    tenantID,
		Email:       email,
		Name:        name,
		Role:        role,
		Permissions: permissions,
		IsActive:    true,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	_, err = m.db.Exec(ctx, `
		INSERT INTO users (id, tenant_id, email, password_hash, name, role, permissions, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true)
	`, user.ID, tenantID, email, passwordHash, name, role, permsJSON)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("a user with email %q already exists in this tenant", email)
		}
		return nil, fmt.Errorf("insert sub-user: %w", err)
	}

	return user, nil
}

// ListUsers returns all users in a tenant, ordered by creation time.
func (m *UserManager) ListUsers(ctx context.Context, tenantID string, pageToken string, pageSize int32) ([]User, string, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}

	// Count total for pagination.
	var totalCount int64
	err := m.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = $1`, tenantID).Scan(&totalCount)
	if err != nil {
		return nil, "", 0, fmt.Errorf("count users: %w", err)
	}

	// Query users with cursor pagination.
	rows, err := m.db.Query(ctx, `
		SELECT id, tenant_id, email, name, role, permissions, is_active, created_at, updated_at
		FROM users
		WHERE tenant_id = $1 AND ($2 = '' OR id > $2)
		ORDER BY id ASC
		LIMIT $3
	`, tenantID, pageToken, pageSize)
	if err != nil {
		return nil, "", 0, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []User
	lastID := ""
	for rows.Next() {
		var u User
		var permsJSON []byte
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &u.Role, &permsJSON, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, "", 0, fmt.Errorf("scan user: %w", err)
		}
		u.Permissions, _ = UnmarshalPermissions(permsJSON)
		users = append(users, u)
		lastID = u.ID
	}

	nextPageToken := ""
	if len(users) == int(pageSize) {
		nextPageToken = lastID
	}

	return users, nextPageToken, totalCount, nil
}

// GetUser returns a single user by ID, scoped to the given tenant.
func (m *UserManager) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	var u User
	var permsJSON []byte
	err := m.db.QueryRow(ctx, `
		SELECT id, tenant_id, email, name, role, permissions, is_active, created_at, updated_at
		FROM users
		WHERE id = $1 AND tenant_id = $2
	`, userID, tenantID).Scan(&u.ID, &u.TenantID, &u.Email, &u.Name, &u.Role, &permsJSON, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("user %s not found", userID)
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	u.Permissions, _ = UnmarshalPermissions(permsJSON)
	return &u, nil
}

// UpdateUser updates a user's name, role, and active status.
func (m *UserManager) UpdateUser(ctx context.Context, tenantID, userID, name, role string, isActive *bool) (*User, error) {
	if role != "" && !isValidRole(role) {
		return nil, fmt.Errorf("invalid role: %s", role)
	}

	_, err := m.db.Exec(ctx, `
		UPDATE users SET
			name = COALESCE(NULLIF($3, ''), name),
			role = COALESCE(NULLIF($4, ''), role),
			is_active = COALESCE($5, is_active),
			updated_at = now()
		WHERE id = $1 AND tenant_id = $2
	`, userID, tenantID, name, role, isActive)
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}

	return m.GetUser(ctx, tenantID, userID)
}

// DeleteUser soft-deletes a user by setting is_active = false.
// Hard deletes are not performed to preserve audit trail integrity.
func (m *UserManager) DeleteUser(ctx context.Context, tenantID, userID string) error {
	tag, err := m.db.Exec(ctx, `
		UPDATE users SET is_active = false, updated_at = now()
		WHERE id = $1 AND tenant_id = $2
	`, userID, tenantID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %s not found", userID)
	}
	return nil
}

// UpdatePermissions updates the per-user permission overrides (JSONB merge).
func (m *UserManager) UpdatePermissions(ctx context.Context, tenantID, userID string, permissions map[string]bool) (*User, error) {
	permsJSON, err := MarshalPermissions(permissions)
	if err != nil {
		return nil, fmt.Errorf("marshal permissions: %w", err)
	}

	_, err = m.db.Exec(ctx, `
		UPDATE users SET permissions = $3, updated_at = now()
		WHERE id = $1 AND tenant_id = $2
	`, userID, tenantID, permsJSON)
	if err != nil {
		return nil, fmt.Errorf("update permissions: %w", err)
	}

	return m.GetUser(ctx, tenantID, userID)
}

// CountUsers returns the number of active users in a tenant.
func (m *UserManager) CountUsers(ctx context.Context, tenantID string) (int, error) {
	var count int
	err := m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM users WHERE tenant_id = $1 AND is_active = true
	`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

// =============================================================================
// helpers
// =============================================================================

func isValidRole(role string) bool {
	switch role {
	case "admin", "operator", "viewer":
		return true
	}
	return false
}

// isUniqueViolation checks if a PostgreSQL error is a unique constraint violation
// by inspecting the SQLSTATE code (23505) via pgconn.PgError.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if pgErr2, ok := err.(*pgconn.PgError); ok {
		pgErr = pgErr2
	}
	return pgErr != nil && pgErr.Code == "23505"
}
