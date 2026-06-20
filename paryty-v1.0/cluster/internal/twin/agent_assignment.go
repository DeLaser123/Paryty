package twin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
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

// userIDFromCtx extracts the user ID from the context.
// Returns empty string if not found.
func userIDFromCtx(ctx context.Context) string {
	v := ctx.Value(plan.CtxUserID)
	if v == nil {
		return ""
	}
	userID, ok := v.(string)
	if !ok {
		return ""
	}
	return userID
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

// AgentInfo holds detailed info for an agent.
type AgentInfo struct {
	AgentID       string  `json:"agent_id"`
	Name          string  `json:"name"`
	Hostname      string  `json:"hostname"`
	Status        string  `json:"status"`
	OS            string  `json:"os"`
	Arch          string  `json:"arch"`
	CloudProvider string  `json:"cloud_provider"`
	Location      string  `json:"location"`
	AssignedTwin  *string `json:"assigned_twin"`
	FirstSeen     string  `json:"first_seen"`
	LastSeen      string  `json:"last_seen"`
}

// CreateAgent registers a new agent for the tenant. Generates a unique agent ID.
// Returns the created agent info or an error.
func (a *AgentAssigner) CreateAgent(ctx context.Context, tenantID, name string) (*AgentInfo, error) {
	// Generate a unique agent ID.
	agentID := "agent-" + uuid.New().String()[:8]

	_, err := a.db.Exec(ctx, `
		INSERT INTO agent_registrations (agent_id, tenant_id, name, status)
		VALUES ($1, $2, $3, 'pending')
	`, agentID, tenantID, name)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	return &AgentInfo{
		AgentID: agentID,
		Name:    name,
		Status:  "pending",
	}, nil
}

// ListAllAgents returns all agents for a tenant with metadata and assignment info.
func (a *AgentAssigner) ListAllAgents(ctx context.Context, tenantID string) ([]AgentInfo, error) {
	rows, err := a.db.Query(ctx, `
		SELECT r.agent_id, r.name, r.hostname, r.status, r.os, r.arch,
		       r.cloud_provider, r.location, a.twin_id,
		       r.first_seen, r.last_seen
		FROM agent_registrations r
		LEFT JOIN agent_assignments a ON a.agent_id = r.agent_id
		WHERE r.tenant_id = $1
		ORDER BY r.last_seen DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list all agents: %w", err)
	}
	defer rows.Close()

	var agents []AgentInfo
	for rows.Next() {
		var info AgentInfo
		var twinID *string
		var firstSeen, lastSeen time.Time
		if err := rows.Scan(&info.AgentID, &info.Name, &info.Hostname, &info.Status,
			&info.OS, &info.Arch, &info.CloudProvider, &info.Location,
			&twinID, &firstSeen, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		info.AssignedTwin = twinID
		info.FirstSeen = firstSeen.Format(time.RFC3339)
		info.LastSeen = lastSeen.Format(time.RFC3339)
		agents = append(agents, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}
	return agents, nil
}

// DeleteAgent soft-deletes an agent by removing its registration and assignments.
func (a *AgentAssigner) DeleteAgent(ctx context.Context, agentID, tenantID string) error {
	// Remove assignment first.
	_, err := a.db.Exec(ctx, `DELETE FROM agent_assignments WHERE agent_id = $1`, agentID)
	if err != nil {
		return fmt.Errorf("delete agent assignment: %w", err)
	}

	// Remove registration.
	tag, err := a.db.Exec(ctx, `DELETE FROM agent_registrations WHERE agent_id = $1 AND tenant_id = $2`, agentID, tenantID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found", agentID)
	}
	return nil
}

// UpdateAgent renames an agent and marks it as deployed.
func (a *AgentAssigner) UpdateAgent(ctx context.Context, agentID, tenantID, name string) (*AgentInfo, error) {
	tag, err := a.db.Exec(ctx, `
		UPDATE agent_registrations
		SET name = $1, status = CASE WHEN status = 'pending' THEN 'deployed' ELSE status END
		WHERE agent_id = $2 AND tenant_id = $3
	`, name, agentID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("update agent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}

	// Return updated agent info.
	agents, err := a.ListAllAgents(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for i := range agents {
		if agents[i].AgentID == agentID {
			return &agents[i], nil
		}
	}
	return nil, fmt.Errorf("agent %s not found after update", agentID)
}

// ═══════════════════════════════════════════════════════════════════════
// Dual Reality Agent System — New Lifecycle Operations
// ═══════════════════════════════════════════════════════════════════════

// PairingStatus holds detailed pairing information for the smart modal.
type PairingStatus struct {
	AgentID           string  `json:"agent_id"`
	EdgeStatus        string  `json:"edge_status"`
	ClusterAgentID    *string `json:"cluster_agent_id"`
	ClusterAgentName  *string `json:"cluster_agent_name"`
	ClusterAgentStatus *string `json:"cluster_agent_status"`
	IsPaired          bool    `json:"is_paired"`
	PairedAt          *string `json:"paired_at"`
	OS                string  `json:"os"`
	Arch              string  `json:"arch"`
	Hostname          string  `json:"hostname"`
	RetiredAt         *string `json:"retired_at"`
	BlacklistedAt     *string `json:"blacklisted_at"`
	BlacklistReason   string  `json:"blacklist_reason"`
}

// PairAgent links an edge agent to a cluster agent (twin).
// Validates state transitions and enforces blacklist checks.
func (a *AgentAssigner) PairAgent(ctx context.Context, agentID, twinID, tenantID string) error {
	// Check blacklist.
	blacklisted, err := a.IsBlacklisted(ctx, tenantID, agentID)
	if err != nil {
		return fmt.Errorf("check blacklist: %w", err)
	}
	if blacklisted {
		return fmt.Errorf("agent %s is blacklisted and cannot be paired", agentID)
	}

	// Get current edge agent status.
	var currentStatus string
	err = a.db.QueryRow(ctx, `
		SELECT status FROM agent_registrations WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("get agent status: %w", err)
	}

	// Validate state transition.
	if err := CanTransitionEdge(EdgeAgentStatus(currentStatus), "pair"); err != nil {
		return err
	}

	// Verify twin exists and belongs to tenant.
	var twinExists bool
	err = a.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM paryty_twins WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL)
	`, twinID, tenantID).Scan(&twinExists)
	if err != nil {
		return fmt.Errorf("check twin existence: %w", err)
	}
	if !twinExists {
		return fmt.Errorf("twin %s not found in tenant %s", twinID, tenantID)
	}

	// Execute pairing in a transaction.
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Remove any existing assignment for this agent.
	_, err = tx.Exec(ctx, `DELETE FROM agent_assignments WHERE agent_id = $1`, agentID)
	if err != nil {
		return fmt.Errorf("clear existing assignment: %w", err)
	}

	// Create new assignment.
	_, err = tx.Exec(ctx, `
		INSERT INTO agent_assignments (agent_id, twin_id, tenant_id)
		VALUES ($1, $2, $3)
	`, agentID, twinID, tenantID)
	if err != nil {
		return fmt.Errorf("insert assignment: %w", err)
	}

	// Update edge agent status to active.
	_, err = tx.Exec(ctx, `
		UPDATE agent_registrations
		SET status = 'active', paired_at = now(), unpaired_at = NULL
		WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID)
	if err != nil {
		return fmt.Errorf("update agent status: %w", err)
	}

	// Update cluster agent (twin) status to active.
	_, err = tx.Exec(ctx, `
		UPDATE paryty_twins
		SET status = 'active', updated_at = now()
		WHERE id = $1 AND tenant_id = $2
	`, twinID, tenantID)
	if err != nil {
		return fmt.Errorf("update twin status: %w", err)
	}

	// Log state transition.
	actorID := userIDFromCtx(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO agent_status_log (agent_id, tenant_id, from_status, to_status, reason, actor_id)
		VALUES ($1, $2, $3, 'active', 'assigned to twin ' || $4, $5)
	`, agentID, tenantID, currentStatus, twinID, actorID)
	if err != nil {
		return fmt.Errorf("log transition: %w", err)
	}

	return tx.Commit(ctx)
}

// UnpairAgent unlinks an edge agent from its cluster agent.
// The edge agent becomes rogue, the cluster agent becomes unconfigured.
func (a *AgentAssigner) UnpairAgent(ctx context.Context, agentID, tenantID string) error {
	// Get current edge agent status.
	var currentStatus string
	err := a.db.QueryRow(ctx, `
		SELECT status FROM agent_registrations WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("get agent status: %w", err)
	}

	if err := CanTransitionEdge(EdgeAgentStatus(currentStatus), "unpair"); err != nil {
		return err
	}

	// Get the twin ID before removing the assignment.
	var twinID string
	err = a.db.QueryRow(ctx, `
		SELECT twin_id FROM agent_assignments WHERE agent_id = $1
	`, agentID).Scan(&twinID)
	if err != nil {
		return fmt.Errorf("agent %s is not paired to any cluster agent", agentID)
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Soft-delete assignment (set unpaired_at).
	_, err = tx.Exec(ctx, `
		DELETE FROM agent_assignments WHERE agent_id = $1
	`, agentID)
	if err != nil {
		return fmt.Errorf("delete assignment: %w", err)
	}

	// Update edge agent status to rogue.
	_, err = tx.Exec(ctx, `
		UPDATE agent_registrations
		SET status = 'rogue', unpaired_at = now()
		WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID)
	if err != nil {
		return fmt.Errorf("update agent status: %w", err)
	}

	// Update cluster agent (twin) status to unconfigured.
	_, err = tx.Exec(ctx, `
		UPDATE paryty_twins
		SET status = 'unconfigured', updated_at = now()
		WHERE id = $1 AND tenant_id = $2
	`, twinID, tenantID)
	if err != nil {
		return fmt.Errorf("update twin status: %w", err)
	}

	// Log state transition.
	actorID := userIDFromCtx(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO agent_status_log (agent_id, tenant_id, from_status, to_status, reason, actor_id)
		VALUES ($1, $2, 'active', 'rogue', 'unassigned from twin ' || $3, $4)
	`, agentID, tenantID, twinID, actorID)
	if err != nil {
		return fmt.Errorf("log transition: %w", err)
	}

	return tx.Commit(ctx)
}

// RetireAgent gracefully decommissions an edge agent.
func (a *AgentAssigner) RetireAgent(ctx context.Context, agentID, tenantID string) error {
	var currentStatus string
	err := a.db.QueryRow(ctx, `
		SELECT status FROM agent_registrations WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("get agent status: %w", err)
	}

	if err := CanTransitionEdge(EdgeAgentStatus(currentStatus), "retire"); err != nil {
		return err
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Remove assignment if paired.
	_, _ = tx.Exec(ctx, `DELETE FROM agent_assignments WHERE agent_id = $1`, agentID)

	// Update edge agent status.
	_, err = tx.Exec(ctx, `
		UPDATE agent_registrations
		SET status = 'retired', retired_at = now()
		WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID)
	if err != nil {
		return fmt.Errorf("update agent status: %w", err)
	}

	// Log transition.
	actorID := userIDFromCtx(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO agent_status_log (agent_id, tenant_id, from_status, to_status, reason, actor_id)
		VALUES ($1, $2, $3, 'retired', 'retired by operator', $4)
	`, agentID, tenantID, currentStatus, actorID)
	if err != nil {
		return fmt.Errorf("log transition: %w", err)
	}

	return tx.Commit(ctx)
}

// BlacklistAgent blocks an edge agent from ever registering again.
func (a *AgentAssigner) BlacklistAgent(ctx context.Context, agentID, tenantID, reason string) error {
	var currentStatus string
	err := a.db.QueryRow(ctx, `
		SELECT COALESCE(status, 'unknown') FROM agent_registrations WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID).Scan(&currentStatus)
	if err != nil {
		// Agent may not be registered yet — still allow blacklisting.
		currentStatus = "unknown"
	}

	// Allow blacklisting from any non-blacklisted state.
	if currentStatus == "blacklisted" {
		return fmt.Errorf("agent %s is already blacklisted", agentID)
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Insert into blacklist table.
	_, err = tx.Exec(ctx, `
		INSERT INTO agent_blacklist (tenant_id, edge_agent_id, reason)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, edge_agent_id) DO UPDATE SET reason = EXCLUDED.reason
	`, tenantID, agentID, reason)
	if err != nil {
		return fmt.Errorf("insert blacklist: %w", err)
	}

	// Remove assignment if paired.
	_, _ = tx.Exec(ctx, `DELETE FROM agent_assignments WHERE agent_id = $1`, agentID)

	// Update edge agent status if registered.
	_, _ = tx.Exec(ctx, `
		UPDATE agent_registrations
		SET status = 'blacklisted', blacklisted_at = now(), blacklist_reason = $3
		WHERE agent_id = $1 AND tenant_id = $2
	`, agentID, tenantID, reason)

	// Log transition.
	actorID := userIDFromCtx(ctx)
	_, _ = tx.Exec(ctx, `
		INSERT INTO agent_status_log (agent_id, tenant_id, from_status, to_status, reason, actor_id)
		VALUES ($1, $2, $3, 'blacklisted', $4, $5)
	`, agentID, tenantID, currentStatus, reason, actorID)

	return tx.Commit(ctx)
}

// UnregisterAgent completely removes an edge agent from the system.
// This erases all traces of client association for security.
func (a *AgentAssigner) UnregisterAgent(ctx context.Context, agentID, tenantID string) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Log before deletion.
	actorID := userIDFromCtx(ctx)
	_, _ = tx.Exec(ctx, `
		INSERT INTO agent_status_log (agent_id, tenant_id, from_status, to_status, reason, actor_id)
		VALUES ($1, $2, 'active', 'unregistered', 'unregistered by operator — all client data erased', $3)
	`, agentID, tenantID, actorID)

	// Remove assignment.
	_, _ = tx.Exec(ctx, `DELETE FROM agent_assignments WHERE agent_id = $1`, agentID)

	// Remove from blacklist.
	_, _ = tx.Exec(ctx, `DELETE FROM agent_blacklist WHERE edge_agent_id = $1 AND tenant_id = $2`, agentID, tenantID)

	// Remove registration entirely.
	tag, err := tx.Exec(ctx, `DELETE FROM agent_registrations WHERE agent_id = $1 AND tenant_id = $2`, agentID, tenantID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent %s not found", agentID)
	}

	return tx.Commit(ctx)
}

// IsBlacklisted checks if an edge agent is in the blacklist for a tenant.
func (a *AgentAssigner) IsBlacklisted(ctx context.Context, tenantID, agentID string) (bool, error) {
	var exists bool
	err := a.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM agent_blacklist WHERE tenant_id = $1 AND edge_agent_id = $2)
	`, tenantID, agentID).Scan(&exists)
	return exists, err
}

// GetAgentPairingStatus returns detailed pairing info for the smart modal.
func (a *AgentAssigner) GetAgentPairingStatus(ctx context.Context, agentID string) (*PairingStatus, error) {
	var ps PairingStatus
	var twinID, twinName, twinStatus *string
	var pairedAt *time.Time
	var retiredAt *time.Time
	var blacklistedAt *time.Time

	err := a.db.QueryRow(ctx, `
		SELECT r.agent_id, r.status, r.hostname, r.os, r.arch,
		       r.paired_at, r.retired_at, r.blacklisted_at, r.blacklist_reason,
		       a.twin_id, t.name, t.status
		FROM agent_registrations r
		LEFT JOIN agent_assignments a ON a.agent_id = r.agent_id
		LEFT JOIN paryty_twins t ON t.id = a.twin_id
		WHERE r.agent_id = $1
	`, agentID).Scan(
		&ps.AgentID, &ps.EdgeStatus, &ps.Hostname, &ps.OS, &ps.Arch,
		&pairedAt, &retiredAt, &blacklistedAt, &ps.BlacklistReason,
		&twinID, &twinName, &twinStatus,
	)
	if err != nil {
		return nil, fmt.Errorf("get pairing status: %w", err)
	}

	ps.ClusterAgentID = twinID
	ps.ClusterAgentName = twinName
	ps.ClusterAgentStatus = twinStatus
	ps.IsPaired = twinID != nil && *twinID != ""
	if pairedAt != nil {
		s := pairedAt.Format(time.RFC3339)
		ps.PairedAt = &s
	}
	if retiredAt != nil {
		s := retiredAt.Format(time.RFC3339)
		ps.RetiredAt = &s
	}
	if blacklistedAt != nil {
		s := blacklistedAt.Format(time.RFC3339)
		ps.BlacklistedAt = &s
	}

	return &ps, nil
}
