package twin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CommandDispatcher is an interface for dispatching commands to agents
// via their active gRPC stream. This is injected by the ingestion layer.
type CommandDispatcher interface {
	// DispatchCommand sends a command directly to an agent via its active stream.
	// Returns true if the command was delivered immediately, false if queued.
	DispatchCommand(ctx context.Context, agentID string, commandType int32, payload string) bool
}

// CommandRecord represents a queued command for an agent.
type CommandRecord struct {
	ID          string
	AgentID     string
	CommandType int32
	Payload     string
	CreatedAt   time.Time
	DeliveredAt *time.Time
}

// CommandManager manages the agent_commands table for deferred command delivery.
type CommandManager struct {
	db         *pgxpool.Pool
	dispatcher CommandDispatcher
}

// NewCommandManager creates a new CommandManager.
func NewCommandManager(db *pgxpool.Pool, dispatcher CommandDispatcher) *CommandManager {
	return &CommandManager{
		db:         db,
		dispatcher: dispatcher,
	}
}

// EnqueueCommand inserts a pending command for an agent. If a dispatcher is
// available and can deliver immediately, the command is NOT persisted.
// Otherwise, it's queued for the next heartbeat.
func (c *CommandManager) EnqueueCommand(ctx context.Context, agentID string, commandType int32, payload string) error {
	// Try immediate dispatch first.
	if c.dispatcher != nil && c.dispatcher.DispatchCommand(ctx, agentID, commandType, payload) {
		return nil
	}

	// Fall back to queued delivery via heartbeat.
	_, err := c.db.Exec(ctx, `
		INSERT INTO agent_commands (agent_id, command_type, payload)
		VALUES ($1, $2, $3)
	`, agentID, commandType, payload)
	if err != nil {
		return fmt.Errorf("enqueue command: %w", err)
	}
	return nil
}

// DequeueCommands returns all undelivered commands for an agent and marks them
// as delivered. Called by the heartbeat handler to include commands in the
// HeartbeatResponse.
func (c *CommandManager) DequeueCommands(ctx context.Context, agentID string) ([]CommandRecord, error) {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx, `
		SELECT id, agent_id, command_type, payload, created_at
		FROM agent_commands
		WHERE agent_id = $1 AND delivered_at IS NULL
		ORDER BY created_at ASC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("query pending commands: %w", err)
	}
	defer rows.Close()

	var commands []CommandRecord
	var ids []string
	for rows.Next() {
		var cmd CommandRecord
		if err := rows.Scan(&cmd.ID, &cmd.AgentID, &cmd.CommandType, &cmd.Payload, &cmd.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan command: %w", err)
		}
		commands = append(commands, cmd)
		ids = append(ids, cmd.ID)
	}

	// Mark all fetched commands as delivered.
	if len(ids) > 0 {
		_, err = tx.Exec(ctx, `
			UPDATE agent_commands SET delivered_at = now()
			WHERE id = ANY($1)
		`, ids)
		if err != nil {
			return nil, fmt.Errorf("mark commands delivered: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return commands, nil
}

// EnqueueIdentityCommand is a convenience wrapper that marshals identity
// assignment data as JSON and enqueues it as an ASSIGN_IDENTITY command.
func (c *CommandManager) EnqueueIdentityCommand(ctx context.Context, agentID, twinID, clientID, topicPrefix string) error {
	payload := struct {
		TwinID      string `json:"twin_id"`
		ClientID    string `json:"client_id"`
		TopicPrefix string `json:"topic_prefix"`
	}{
		TwinID:      twinID,
		ClientID:    clientID,
		TopicPrefix: topicPrefix,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal identity payload: %w", err)
	}
	return c.EnqueueCommand(ctx, agentID, 5, string(payloadJSON))
}
