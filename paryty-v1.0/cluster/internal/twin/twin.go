// Package twin implements Paryty Twin lifecycle management.
// A Paryty Twin is a digital representation of a customer's infrastructure
// environment. It groups agents, collectors, and configuration under a single
// entity that can be monitored, analyzed, and simulated.
package twin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Twin represents a Paryty Twin entity.
type Twin struct {
	ID          string
	TenantID    string
	Name        string
	Description string
	Status      string
	Config      TwinConfig
	AgentCount  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// TwinConfig holds the configuration for a Paryty Twin.
type TwinConfig struct {
	AgentLabels              map[string]string `json:"agent_labels"`
	EnabledCollectors        []string          `json:"enabled_collectors"`
	CollectionIntervalSeconds int32            `json:"collection_interval_seconds"`
	SamplingRate             float32           `json:"sampling_rate"`
	Abilities                []string          `json:"abilities,omitempty"`
}

// TwinManager handles CRUD operations for Paryty Twins.
type TwinManager struct {
	db *pgxpool.Pool
}

// NewTwinManager creates a new TwinManager.
func NewTwinManager(db *pgxpool.Pool) *TwinManager {
	return &TwinManager{db: db}
}

// CreateTwin creates a new Paryty Twin for a tenant.
func (m *TwinManager) CreateTwin(ctx context.Context, tenantID, name, description string, config TwinConfig, abilities []string) (*Twin, error) {
	if name == "" {
		return nil, fmt.Errorf("twin name is required")
	}

	// Check name uniqueness within tenant.
	var existingCount int
	err := m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM paryty_twins
		WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL
	`, tenantID, name).Scan(&existingCount)
	if err != nil {
		return nil, fmt.Errorf("check twin name uniqueness: %w", err)
	}
	if existingCount > 0 {
		return nil, fmt.Errorf("twin with name %q already exists", name)
	}

	// Store abilities in both config (for backward compat) and dedicated column.
	if len(abilities) > 0 {
		config.Abilities = abilities
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal twin config: %w", err)
	}

	abilitiesJSON, _ := json.Marshal(abilities)
	if abilitiesJSON == nil {
		abilitiesJSON = []byte("[]")
	}

	twin := &Twin{
		ID:          uuid.New().String(),
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		Status:      "pending",
		Config:      config,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	_, err = m.db.Exec(ctx, `
		INSERT INTO paryty_twins (id, tenant_id, name, description, status, twin_config, abilities)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6)
	`, twin.ID, tenantID, name, description, configJSON, abilitiesJSON)
	if err != nil {
		return nil, fmt.Errorf("insert twin: %w", err)
	}

	return twin, nil
}

// GetTwin retrieves a single twin by ID, scoped to tenant.
func (m *TwinManager) GetTwin(ctx context.Context, tenantID, twinID string) (*Twin, error) {
	var t Twin
	var configJSON []byte
	var deletedAt *time.Time

	err := m.db.QueryRow(ctx, `
		SELECT id, tenant_id, name, description, status, twin_config, created_at, updated_at, deleted_at
		FROM paryty_twins
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, twinID, tenantID).Scan(&t.ID, &t.TenantID, &t.Name, &t.Description, &t.Status, &configJSON, &t.CreatedAt, &t.UpdatedAt, &deletedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("twin %s not found", twinID)
		}
		return nil, fmt.Errorf("get twin: %w", err)
	}

	t.DeletedAt = deletedAt
	if err := json.Unmarshal(configJSON, &t.Config); err != nil {
		return nil, fmt.Errorf("unmarshal twin config: %w", err)
	}

	// Get agent count.
	_ = m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_assignments WHERE twin_id = $1
	`, twinID).Scan(&t.AgentCount)

	return &t, nil
}

// ListTwins returns all active twins for a tenant.
func (m *TwinManager) ListTwins(ctx context.Context, tenantID string, pageToken string, pageSize int32) ([]Twin, string, int64, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}

	var totalCount int64
	err := m.db.QueryRow(ctx, `SELECT COUNT(*) FROM paryty_twins WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID).Scan(&totalCount)
	if err != nil {
		return nil, "", 0, fmt.Errorf("count twins: %w", err)
	}

	rows, err := m.db.Query(ctx, `
		SELECT t.id, t.tenant_id, t.name, t.description, t.status, t.twin_config, t.created_at, t.updated_at,
		       COALESCE(a.cnt, 0) AS agent_count
		FROM paryty_twins t
		LEFT JOIN (
			SELECT twin_id, COUNT(*) AS cnt FROM agent_assignments GROUP BY twin_id
		) a ON a.twin_id = t.id
		WHERE t.tenant_id = $1 AND t.deleted_at IS NULL AND ($2 = '' OR t.id > $2::uuid)
		ORDER BY t.id ASC
		LIMIT $3
	`, tenantID, pageToken, pageSize)
	if err != nil {
		return nil, "", 0, fmt.Errorf("query twins: %w", err)
	}
	defer rows.Close()

	var twins []Twin
	lastID := ""
	for rows.Next() {
		var t Twin
		var configJSON []byte
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.Description, &t.Status, &configJSON, &t.CreatedAt, &t.UpdatedAt, &t.AgentCount); err != nil {
			return nil, "", 0, fmt.Errorf("scan twin: %w", err)
		}
		if err := json.Unmarshal(configJSON, &t.Config); err != nil {
			return nil, "", 0, fmt.Errorf("unmarshal twin config: %w", err)
		}
		twins = append(twins, t)
		lastID = t.ID
	}

	nextPageToken := ""
	if len(twins) == int(pageSize) {
		nextPageToken = lastID
	}

	return twins, nextPageToken, totalCount, nil
}

// UpdateTwin updates a twin's metadata and configuration.
func (m *TwinManager) UpdateTwin(ctx context.Context, tenantID, twinID, name, description string, config *TwinConfig) (*Twin, error) {
	var configJSON []byte
	if config != nil {
		var err error
		configJSON, err = json.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("marshal twin config: %w", err)
		}
	}

	_, err := m.db.Exec(ctx, `
		UPDATE paryty_twins SET
			name = COALESCE(NULLIF($3, ''), name),
			description = COALESCE(NULLIF($4, ''), description),
			twin_config = COALESCE($5, twin_config),
			updated_at = now()
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, twinID, tenantID, name, description, configJSON)
	if err != nil {
		return nil, fmt.Errorf("update twin: %w", err)
	}

	return m.GetTwin(ctx, tenantID, twinID)
}

// DeleteTwin soft-deletes a twin and dissociates all agents.
func (m *TwinManager) DeleteTwin(ctx context.Context, tenantID, twinID string) error {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Soft-delete the twin.
	tag, err := tx.Exec(ctx, `
		UPDATE paryty_twins SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, twinID, tenantID)
	if err != nil {
		return fmt.Errorf("soft-delete twin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("twin %s not found", twinID)
	}

	// Remove all agent assignments.
	_, err = tx.Exec(ctx, `
		DELETE FROM agent_assignments WHERE twin_id = $1
	`, twinID)
	if err != nil {
		return fmt.Errorf("delete agent assignments: %w", err)
	}

	return tx.Commit(ctx)
}

// CountTwins returns the number of active twins for a tenant.
func (m *TwinManager) CountTwins(ctx context.Context, tenantID string) (int, error) {
	var count int
	err := m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM paryty_twins
		WHERE tenant_id = $1 AND deleted_at IS NULL
	`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count twins: %w", err)
	}
	return count, nil
}

// CountAgentsForTwin returns the number of agents currently assigned to a twin.
func (m *TwinManager) CountAgentsForTwin(ctx context.Context, twinID string) (int, error) {
	var count int
	err := m.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_assignments
		WHERE twin_id = $1
	`, twinID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count agents for twin: %w", err)
	}
	return count, nil
}

// GetTwinConfigForAgent returns the twin configuration for agent auto-discovery.
// Called by the ingestion service when an agent registers with a twin_id.
func (m *TwinManager) GetTwinConfigForAgent(ctx context.Context, twinID, agentID string) (*TwinConfig, error) {
	var configJSON []byte
	err := m.db.QueryRow(ctx, `
		SELECT twin_config FROM paryty_twins
		WHERE id = $1 AND deleted_at IS NULL
	`, twinID).Scan(&configJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("twin %s not found", twinID)
		}
		return nil, fmt.Errorf("get twin config: %w", err)
	}

	var cfg TwinConfig
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal twin config: %w", err)
	}

	return &cfg, nil
}

// GetTwinAbilities returns the abilities for a twin from the dedicated abilities column.
func (m *TwinManager) GetTwinAbilities(ctx context.Context, twinID string) ([]string, error) {
	var abilitiesJSON []byte
	err := m.db.QueryRow(ctx, `
		SELECT COALESCE(abilities, '[]') FROM paryty_twins
		WHERE id = $1 AND deleted_at IS NULL
	`, twinID).Scan(&abilitiesJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return []string{}, nil
		}
		return nil, fmt.Errorf("get twin abilities: %w", err)
	}
	var abilities []string
	if err := json.Unmarshal(abilitiesJSON, &abilities); err != nil {
		return []string{}, nil
	}
	return abilities, nil
}
