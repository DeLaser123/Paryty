package twin

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BacklogInfo holds backlog metadata reported by an agent via heartbeat.
type BacklogInfo struct {
	AgentID            string
	TwinID             string
	BacklogBytes       int64
	BacklogSinceEpoch  int64
	Status             string
	UpdatedAt          time.Time
}

// BacklogManager handles backlog tracking and operator approval.
type BacklogManager struct {
	db *pgxpool.Pool
}

// NewBacklogManager creates a new BacklogManager.
func NewBacklogManager(db *pgxpool.Pool) *BacklogManager {
	return &BacklogManager{db: db}
}

// UpsertBacklog inserts or updates backlog metadata for an agent.
// Called from the heartbeat handler when an agent reports backlog info.
func (b *BacklogManager) UpsertBacklog(ctx context.Context, agentID, twinID string, backlogBytes, backlogSinceEpoch int64) error {
	_, err := b.db.Exec(ctx, `
		INSERT INTO agent_backlogs (agent_id, twin_id, backlog_bytes, backlog_since_epoch, status, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', now())
		ON CONFLICT (agent_id) DO UPDATE SET
			backlog_bytes = EXCLUDED.backlog_bytes,
			backlog_since_epoch = EXCLUDED.backlog_since_epoch,
			updated_at = now()
	`, agentID, twinID, backlogBytes, backlogSinceEpoch)
	if err != nil {
		return fmt.Errorf("upsert backlog: %w", err)
	}
	return nil
}

// GetBacklog returns backlog info for an agent.
func (b *BacklogManager) GetBacklog(ctx context.Context, agentID string) (*BacklogInfo, error) {
	var info BacklogInfo
	err := b.db.QueryRow(ctx, `
		SELECT agent_id, twin_id, backlog_bytes, backlog_since_epoch, status, updated_at
		FROM agent_backlogs
		WHERE agent_id = $1
	`, agentID).Scan(&info.AgentID, &info.TwinID, &info.BacklogBytes, &info.BacklogSinceEpoch, &info.Status, &info.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get backlog: %w", err)
	}
	return &info, nil
}

// AcceptBacklog marks a backlog as uploading (operator approved the upload).
func (b *BacklogManager) AcceptBacklog(ctx context.Context, agentID string) error {
	tag, err := b.db.Exec(ctx, `
		UPDATE agent_backlogs SET status = 'uploading', updated_at = now()
		WHERE agent_id = $1 AND status = 'pending'
	`, agentID)
	if err != nil {
		return fmt.Errorf("accept backlog: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no pending backlog for agent %s", agentID)
	}
	return nil
}

// RejectBacklog marks a backlog as rejected (operator declined the upload).
func (b *BacklogManager) RejectBacklog(ctx context.Context, agentID string) error {
	tag, err := b.db.Exec(ctx, `
		UPDATE agent_backlogs SET status = 'rejected', updated_at = now()
		WHERE agent_id = $1 AND status = 'pending'
	`, agentID)
	if err != nil {
		return fmt.Errorf("reject backlog: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no pending backlog for agent %s", agentID)
	}
	return nil
}

// ListBacklogsForTwin returns all backlog entries for agents assigned to a twin.
func (b *BacklogManager) ListBacklogsForTwin(ctx context.Context, twinID string) ([]BacklogInfo, error) {
	rows, err := b.db.Query(ctx, `
		SELECT agent_id, twin_id, backlog_bytes, backlog_since_epoch, status, updated_at
		FROM agent_backlogs
		WHERE twin_id = $1
		ORDER BY updated_at DESC
	`, twinID)
	if err != nil {
		return nil, fmt.Errorf("query backlogs for twin: %w", err)
	}
	defer rows.Close()

	var backlogs []BacklogInfo
	for rows.Next() {
		var info BacklogInfo
		if err := rows.Scan(&info.AgentID, &info.TwinID, &info.BacklogBytes, &info.BacklogSinceEpoch, &info.Status, &info.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan backlog: %w", err)
		}
		backlogs = append(backlogs, info)
	}

	return backlogs, nil
}
