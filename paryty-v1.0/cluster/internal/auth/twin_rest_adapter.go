package auth

import (
	"context"

	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/twin"
)

// TwinRESTAdapter adapts TwinHandler to the TwinAPI interface expected by the
// query REST layer. It converts between REST-friendly maps and gRPC proto types.
type TwinRESTAdapter struct {
	handler  *TwinHandler
	tm       *twin.TwinManager
	assigner *twin.AgentAssigner
}

// NewTwinRESTAdapter creates a new TwinRESTAdapter.
func NewTwinRESTAdapter(handler *TwinHandler) *TwinRESTAdapter {
	return &TwinRESTAdapter{
		handler:  handler,
		tm:       handler.tm,
		assigner: handler.assigner,
	}
}

// CreateTwin adapts TwinHandler.CreateTwin for the REST API.
func (a *TwinRESTAdapter) CreateTwin(ctx context.Context, tenantID, name, description string) (map[string]interface{}, error) {
	req := &parytyv1.CreateTwinRequest{
		Name:        name,
		Description: description,
	}
	// Inject tenant into context for the gRPC handler.
	ctx = context.WithValue(ctx, plan.CtxTenantID, tenantID)
	info, err := a.handler.CreateTwin(ctx, req)
	if err != nil {
		return nil, err
	}
	return twinInfoToMap(info), nil
}

// ListTwins adapts TwinHandler.ListTwins for the REST API.
func (a *TwinRESTAdapter) ListTwins(ctx context.Context, tenantID string) ([]map[string]interface{}, error) {
	ctx = context.WithValue(ctx, plan.CtxTenantID, tenantID)
	resp, err := a.handler.ListTwins(ctx, &parytyv1.ListTwinsRequest{})
	if err != nil {
		return nil, err
	}
	twins := make([]map[string]interface{}, 0, len(resp.Twins))
	for _, t := range resp.Twins {
		twins = append(twins, twinInfoToMap(t))
	}
	return twins, nil
}

// GetTwin adapts TwinHandler.GetTwin for the REST API.
func (a *TwinRESTAdapter) GetTwin(ctx context.Context, tenantID, twinID string) (map[string]interface{}, error) {
	ctx = context.WithValue(ctx, plan.CtxTenantID, tenantID)
	info, err := a.handler.GetTwin(ctx, &parytyv1.GetTwinRequest{TwinId: twinID})
	if err != nil {
		return nil, err
	}
	return twinInfoToMap(info), nil
}

// UpdateTwin adapts TwinHandler.UpdateTwin for the REST API.
func (a *TwinRESTAdapter) UpdateTwin(ctx context.Context, tenantID, twinID, name, description string) (map[string]interface{}, error) {
	ctx = context.WithValue(ctx, plan.CtxTenantID, tenantID)
	req := &parytyv1.UpdateTwinRequest{
		TwinId:      twinID,
		Name:        name,
		Description: description,
	}
	info, err := a.handler.UpdateTwin(ctx, req)
	if err != nil {
		return nil, err
	}
	return twinInfoToMap(info), nil
}

// DeleteTwin adapts TwinHandler.DeleteTwin for the REST API.
func (a *TwinRESTAdapter) DeleteTwin(ctx context.Context, tenantID, twinID string) error {
	ctx = context.WithValue(ctx, plan.CtxTenantID, tenantID)
	_, err := a.handler.DeleteTwin(ctx, &parytyv1.DeleteTwinRequest{TwinId: twinID})
	return err
}

// ListTwinAgents adapts TwinHandler.ListTwinAgents for the REST API.
func (a *TwinRESTAdapter) ListTwinAgents(ctx context.Context, twinID string) ([]map[string]interface{}, error) {
	resp, err := a.handler.ListTwinAgents(ctx, &parytyv1.ListTwinAgentsRequest{TwinId: twinID})
	if err != nil {
		return nil, err
	}
	agents := make([]map[string]interface{}, 0, len(resp.Agents))
	for _, ag := range resp.Agents {
		agents = append(agents, twinAgentInfoToMap(ag))
	}
	return agents, nil
}

// AssignAgentToTwin adapts TwinHandler.AssignAgentToTwin for the REST API.
func (a *TwinRESTAdapter) AssignAgentToTwin(ctx context.Context, tenantID, twinID, agentID string) error {
	ctx = context.WithValue(ctx, plan.CtxTenantID, tenantID)
	_, err := a.handler.AssignAgentToTwin(ctx, &parytyv1.AssignAgentToTwinRequest{
		TwinId:  twinID,
		AgentId: agentID,
	})
	return err
}

// AcceptBacklog adapts TwinHandler.AcceptBacklog for the REST API.
func (a *TwinRESTAdapter) AcceptBacklog(ctx context.Context, agentID string) error {
	_, err := a.handler.AcceptBacklog(ctx, &parytyv1.AcceptBacklogRequest{
		AgentId: agentID,
	})
	return err
}

// RejectBacklog adapts TwinHandler.RejectBacklog for the REST API.
func (a *TwinRESTAdapter) RejectBacklog(ctx context.Context, agentID string) error {
	_, err := a.handler.RejectBacklog(ctx, &parytyv1.RejectBacklogRequest{
		AgentId: agentID,
	})
	return err
}

// ── helpers ────────────────────────────────────────────────────────────────

func twinInfoToMap(t *parytyv1.TwinInfo) map[string]interface{} {
	m := map[string]interface{}{
		"id":          t.Id,
		"tenantId":    t.TenantId,
		"name":        t.Name,
		"description": t.Description,
		"status":      t.Status,
		"agentCount":  t.AgentCount,
	}
	if t.CreatedAt != nil {
		m["createdAt"] = t.CreatedAt.AsTime().UTC().Format("2006-01-02T15:04:05Z")
	}
	if t.UpdatedAt != nil {
		m["updatedAt"] = t.UpdatedAt.AsTime().UTC().Format("2006-01-02T15:04:05Z")
	}
	if t.Config != nil {
		m["config"] = map[string]interface{}{
			"agentLabels":              t.Config.AgentLabels,
			"enabledCollectors":        t.Config.EnabledCollectors,
			"collectionIntervalSeconds": t.Config.CollectionIntervalSeconds,
			"samplingRate":             t.Config.SamplingRate,
		}
	}
	return m
}

func twinAgentInfoToMap(a *parytyv1.TwinAgentInfo) map[string]interface{} {
	m := map[string]interface{}{
		"agentId":      a.AgentId,
		"hostname":     a.Hostname,
		"status":       a.Status,
		"backlogBytes": a.BacklogBytes,
		"assigned":     a.Assigned,
		"os":           a.Os,
		"arch":         a.Arch,
	}
	if a.LastHeartbeat != nil {
		m["lastHeartbeat"] = a.LastHeartbeat.AsTime().UTC().Format("2006-01-02T15:04:05Z")
	}
	return m
}
