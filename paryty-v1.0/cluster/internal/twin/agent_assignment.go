package twin

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AgentAssignment maps an agent to its parent Paryty Twin.
type AgentAssignment struct {
	AgentID     string
	TwinID      string
	TenantID    string
	ClientID    string
	TopicPrefix string
	CreatedAt   time.Time
}

// AgentIdentity holds the routing identity for an agent.
type AgentIdentity struct {
	AgentID     string
	TwinID      string
	ClientID    string
	TopicPrefix string
	Assigned    bool
}

// AgentAssigner manages agent-to-twin assignments.
type AgentAssigner struct {
	db *pgxpool.Pool
}

// NewAgentAssigner creates a new AgentAssigner.
func NewAgentAssigner(db *pgxpool.Pool) *AgentAssigner {
	return &AgentAssigner{db: db}
}

// RegisterAgent assigns an agent to a twin. Uses upsert semantics: if the
// agent is already assigned to a different twin, the assignment is moved.
// An agent can only belong to one twin at a time.
func (a *AgentAssigner) RegisterAgent(ctx context.Context, agentID, twinID, tenantID, clientID, topicPrefix string) (*AgentAssignment, error) {
	// Verify the twin exists and belongs to the tenant.
	var twinExists bool
	err := a.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM paryty_twins WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL)
	`, twinID, tenantID).Scan(&twinExists)
	if err != nil {
		return nil, fmt.Errorf("check twin existence: %w", err)
	}
	if !twinExists {
		return nil, fmt.Errorf("twin %s not found in tenant %s", twinID, tenantID)
	}

	// Remove any existing assignment for this agent (one twin per agent).
	_, err = a.db.Exec(ctx, `
		DELETE FROM agent_assignments WHERE agent_id = $1
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("clear existing assignment: %w", err)
	}

	// Create new assignment.
	assignment := &AgentAssignment{
		AgentID:     agentID,
		TwinID:      twinID,
		TenantID:    tenantID,
		ClientID:    clientID,
		TopicPrefix: topicPrefix,
		CreatedAt:   time.Now().UTC(),
	}

	_, err = a.db.Exec(ctx, `
		INSERT INTO agent_assignments (agent_id, twin_id, tenant_id, client_id, topic_prefix)
		VALUES ($1, $2, $3, $4, $5)
	`, agentID, twinID, tenantID, clientID, topicPrefix)
	if err != nil {
		return nil, fmt.Errorf("insert agent assignment: %w", err)
	}

	return assignment, nil
}

// GetAgentAssignment returns the twin an agent is assigned to, or nil if unassigned.
func (a *AgentAssigner) GetAgentAssignment(ctx context.Context, agentID string) (*AgentAssignment, error) {
	var assign AgentAssignment
	err := a.db.QueryRow(ctx, `
		SELECT agent_id, twin_id, tenant_id, client_id, topic_prefix, created_at
		FROM agent_assignments
		WHERE agent_id = $1
	`, agentID).Scan(&assign.AgentID, &assign.TwinID, &assign.TenantID, &assign.ClientID, &assign.TopicPrefix, &assign.CreatedAt)
	if err != nil {
		return nil, nil // unassigned is not an error
	}
	return &assign, nil
}

// ListAgentsForTwin returns all agents assigned to a twin.
func (a *AgentAssigner) ListAgentsForTwin(ctx context.Context, twinID string) ([]AgentAssignment, error) {
	rows, err := a.db.Query(ctx, `
		SELECT agent_id, twin_id, tenant_id, created_at
		FROM agent_assignments
		WHERE twin_id = $1
		ORDER BY created_at ASC
	`, twinID)
	if err != nil {
		return nil, fmt.Errorf("query agents for twin: %w", err)
	}
	defer rows.Close()

	var agents []AgentAssignment
	for rows.Next() {
		var assign AgentAssignment
		if err := rows.Scan(&assign.AgentID, &assign.TwinID, &assign.TenantID, &assign.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan agent assignment: %w", err)
		}
		agents = append(agents, assign)
	}

	return agents, nil
}

// UnassignAgent removes an agent from its twin.
func (a *AgentAssigner) UnassignAgent(ctx context.Context, agentID string) error {
	_, err := a.db.Exec(ctx, `
		DELETE FROM agent_assignments WHERE agent_id = $1
	`, agentID)
	if err != nil {
		return fmt.Errorf("unassign agent: %w", err)
	}
	return nil
}

// GetAgentIdentity resolves the full identity (twin_id, client_id, topic_prefix)
// for an agent. Returns assigned=false if the agent has no assignment.
func (a *AgentAssigner) GetAgentIdentity(ctx context.Context, agentID string) (*AgentIdentity, error) {
	assign, err := a.GetAgentAssignment(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if assign == nil {
		return &AgentIdentity{
			AgentID:  agentID,
			Assigned: false,
		}, nil
	}
	return &AgentIdentity{
		AgentID:     assign.AgentID,
		TwinID:      assign.TwinID,
		ClientID:    assign.ClientID,
		TopicPrefix: assign.TopicPrefix,
		Assigned:    true,
	}, nil
}

// UpsertAgentRegistration records an agent registration in the registry.
// Called by the ingestion adapter every time an agent registers or heartbeats.
func (a *AgentAssigner) UpsertAgentRegistration(ctx context.Context, agentID, tenantID, hostname, clientID string) error {
	_, err := a.db.Exec(ctx, `
		INSERT INTO agent_registrations (agent_id, tenant_id, hostname, client_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (agent_id) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			client_id = EXCLUDED.client_id,
			last_seen = now()
	`, agentID, tenantID, hostname, clientID)
	return err
}

// UnassignedAgentInfo holds summary info for an unassigned agent.
type UnassignedAgentInfo struct {
	AgentID  string    `json:"agent_id"`
	Hostname string    `json:"hostname"`
	ClientID string    `json:"client_id"`
	LastSeen time.Time `json:"last_seen"`
}

// ListUnassignedAgents returns agents registered with the given tenant that
// are NOT currently assigned to any twin.
func (a *AgentAssigner) ListUnassignedAgents(ctx context.Context, tenantID string) ([]UnassignedAgentInfo, error) {
	rows, err := a.db.Query(ctx, `
		SELECT r.agent_id, r.hostname, r.client_id, r.last_seen
		FROM agent_registrations r
		LEFT JOIN agent_assignments a ON a.agent_id = r.agent_id
		WHERE r.tenant_id = $1 AND a.agent_id IS NULL
		ORDER BY r.last_seen DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list unassigned agents: %w", err)
	}
	defer rows.Close()

	var agents []UnassignedAgentInfo
	for rows.Next() {
		var info UnassignedAgentInfo
		if err := rows.Scan(&info.AgentID, &info.Hostname, &info.ClientID, &info.LastSeen); err != nil {
			return nil, fmt.Errorf("scan unassigned agent: %w", err)
		}
		agents = append(agents, info)
	}
	return agents, nil
}
