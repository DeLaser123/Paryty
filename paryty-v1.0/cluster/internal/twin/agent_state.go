package twin

import "fmt"

// ─── Edge Agent State Machine ───────────────────────────────────────────────

// EdgeAgentStatus represents the lifecycle status of an edge agent.
type EdgeAgentStatus string

const (
	EdgeStatusUnconfigured EdgeAgentStatus = "unconfigured"
	EdgeStatusActive       EdgeAgentStatus = "active"
	EdgeStatusLost         EdgeAgentStatus = "lost"
	EdgeStatusRogue        EdgeAgentStatus = "rogue"
	EdgeStatusRetired      EdgeAgentStatus = "retired"
	EdgeStatusBlacklisted  EdgeAgentStatus = "blacklisted"
	// Legacy statuses (backward compatibility).
	EdgeStatusPending  EdgeAgentStatus = "pending"
	EdgeStatusDeployed EdgeAgentStatus = "deployed"
	EdgeStatusInactive EdgeAgentStatus = "inactive"
)

// AgentStateTransition defines a valid state transition for edge agents.
type AgentStateTransition struct {
	From   EdgeAgentStatus
	Action string
	To     EdgeAgentStatus
}

// edgeValidTransitions is the complete state machine for edge agents.
var edgeValidTransitions = []AgentStateTransition{
	// Pairing flow — from unconfigured/legacy states.
	{From: EdgeStatusUnconfigured, Action: "pair", To: EdgeStatusActive},
	{From: EdgeStatusPending, Action: "pair", To: EdgeStatusActive},
	{From: EdgeStatusRogue, Action: "pair", To: EdgeStatusActive},
	// Unpairing — active agent loses twin association.
	{From: EdgeStatusActive, Action: "unpair", To: EdgeStatusRogue},
	// Heartbeat lifecycle.
	{From: EdgeStatusActive, Action: "heartbeat_timeout", To: EdgeStatusLost},
	{From: EdgeStatusLost, Action: "heartbeat_resume", To: EdgeStatusActive},
	// Retirement — graceful decommission from any non-terminal state.
	{From: EdgeStatusActive, Action: "retire", To: EdgeStatusRetired},
	{From: EdgeStatusLost, Action: "retire", To: EdgeStatusRetired},
	{From: EdgeStatusRogue, Action: "retire", To: EdgeStatusRetired},
	{From: EdgeStatusUnconfigured, Action: "retire", To: EdgeStatusRetired},
	// Blacklisting — security action from any non-terminal state.
	{From: EdgeStatusActive, Action: "blacklist", To: EdgeStatusBlacklisted},
	{From: EdgeStatusLost, Action: "blacklist", To: EdgeStatusBlacklisted},
	{From: EdgeStatusRogue, Action: "blacklist", To: EdgeStatusBlacklisted},
	// Note: Unregister removes the agent entirely — handled outside the state machine.
}

// CanTransitionEdge checks if an edge agent state transition is valid.
// Returns nil if valid, or an error describing why the transition is disallowed.
func CanTransitionEdge(current EdgeAgentStatus, action string) error {
	for _, t := range edgeValidTransitions {
		if t.From == current && t.Action == action {
			return nil
		}
	}
	return fmt.Errorf("invalid edge agent transition: %s + %s", current, action)
}

// ─── Cluster Agent (Twin) State Machine ─────────────────────────────────────

// ClusterAgentStatus represents the lifecycle status of a cluster agent (twin).
type ClusterAgentStatus string

const (
	ClusterStatusPending      ClusterAgentStatus = "pending"
	ClusterStatusActive       ClusterAgentStatus = "active"
	ClusterStatusDegraded     ClusterAgentStatus = "degraded"
	ClusterStatusInactive     ClusterAgentStatus = "inactive"
	ClusterStatusDeleted      ClusterAgentStatus = "deleted"
	ClusterStatusUnconfigured ClusterAgentStatus = "unconfigured"
)

// ClusterStateTransition defines a valid state transition for cluster agents.
type ClusterStateTransition struct {
	From   ClusterAgentStatus
	Action string
	To     ClusterAgentStatus
}

// clusterValidTransitions is the complete state machine for cluster agents.
var clusterValidTransitions = []ClusterStateTransition{
	// Initial activation.
	{From: ClusterStatusPending, Action: "activate", To: ClusterStatusActive},
	// Degradation and recovery.
	{From: ClusterStatusActive, Action: "degrade", To: ClusterStatusDegraded},
	{From: ClusterStatusDegraded, Action: "activate", To: ClusterStatusActive},
	// Unconfigure — edge agent unpaired, cluster agent becomes idle.
	{From: ClusterStatusActive, Action: "unconfigure", To: ClusterStatusUnconfigured},
	{From: ClusterStatusDegraded, Action: "unconfigure", To: ClusterStatusUnconfigured},
	// Reactivation from unconfigured state.
	{From: ClusterStatusUnconfigured, Action: "activate", To: ClusterStatusActive},
}

// CanTransitionCluster checks if a cluster agent state transition is valid.
// Returns nil if valid, or an error describing why the transition is disallowed.
func CanTransitionCluster(current ClusterAgentStatus, action string) error {
	for _, t := range clusterValidTransitions {
		if t.From == current && t.Action == action {
			return nil
		}
	}
	return fmt.Errorf("invalid cluster agent transition: %s + %s", current, action)
}
