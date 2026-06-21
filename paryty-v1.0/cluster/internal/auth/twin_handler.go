package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"github.com/paryty/paryty-v1.0/cluster/internal/security"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"github.com/paryty/paryty-v1.0/cluster/internal/twin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TwinHandler implements the TwinServiceServer gRPC interface defined in
// auth.proto. It wraps the twin package managers for CRUD, identity
// resolution, agent assignment, and backlog operations.
type TwinHandler struct {
	parytyv1.UnimplementedTwinServiceServer
	tm        *twin.TwinManager
	assigner  *twin.AgentAssigner
	backlogs  *twin.BacklogManager
	commands  *twin.CommandManager
	topics    *stream.TopicManager
	routing   *twin.RoutingCache
	audit     *security.AuditLogger
}

// NewTwinHandler creates a new TwinHandler.
func NewTwinHandler(db *pgxpool.Pool, dispatcher twin.CommandDispatcher, topics *stream.TopicManager, routing *twin.RoutingCache, audit *security.AuditLogger) *TwinHandler {
	return &TwinHandler{
		tm:       twin.NewTwinManager(db),
		assigner: twin.NewAgentAssigner(db),
		backlogs: twin.NewBacklogManager(db),
		commands: twin.NewCommandManager(db, dispatcher),
		topics:   topics,
		routing:  routing,
		audit:    audit,
	}
}

// TwinManager returns the underlying twin.TwinManager.
// Useful for quota enforcement middleware that needs CountTwins.
func (h *TwinHandler) TwinManager() *twin.TwinManager {
	return h.tm
}

// tenantFromCtx extracts the tenant ID from the gRPC context. It is set by
// the GrpcJWTAuth interceptor. Returns an error if not found.
func tenantFromCtx(ctx context.Context) (string, error) {
	v := ctx.Value(plan.CtxTenantID)
	if v == nil {
		return "", status.Error(codes.Unauthenticated, "missing tenant context")
	}
	tenantID, ok := v.(string)
	if !ok || tenantID == "" {
		return "", status.Error(codes.Unauthenticated, "invalid tenant context")
	}
	return tenantID, nil
}

// CreateTwin creates a new Paryty Twin.
func (h *TwinHandler) CreateTwin(ctx context.Context, req *parytyv1.CreateTwinRequest) (*parytyv1.TwinInfo, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	cfg := twinConfigFromProto(req.Config)
	t, err := h.tm.CreateTwin(ctx, tenantID, req.Name, req.Description, cfg, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create twin: %v", err)
	}

	// Create twin-scoped Redpanda topics (best-effort).
	if h.topics != nil {
		if err := stream.InitializeTwinTopics(ctx, h.topics.Client(), tenantID, t.ID); err != nil {
			slog.Error("twin topic creation failed (best-effort)", "twin_id", t.ID, "error", err)
		}
	}

	// Audit log twin creation.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "twin.create", "twin", t.ID, map[string]interface{}{
			"twin_id": t.ID,
			"name":    req.Name,
		})
	}

	return twinToProto(t), nil
}

// ListTwins returns all twins for the authenticated tenant.
func (h *TwinHandler) ListTwins(ctx context.Context, req *parytyv1.ListTwinsRequest) (*parytyv1.ListTwinsResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	pageToken := ""
	pageSize := int32(50)
	if req.Pagination != nil {
		pageToken = req.Pagination.PageToken
		if req.Pagination.PageSize > 0 {
			pageSize = req.Pagination.PageSize
		}
	}

	twins, nextToken, totalCount, err := h.tm.ListTwins(ctx, tenantID, pageToken, pageSize)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list twins: %v", err)
	}

	protoTwins := make([]*parytyv1.TwinInfo, 0, len(twins))
	for i := range twins {
		protoTwins = append(protoTwins, twinToProto(&twins[i]))
	}

	return &parytyv1.ListTwinsResponse{
		Twins: protoTwins,
		Pagination: &parytyv1.PaginationResponse{
			NextPageToken: nextToken,
			TotalCount:    totalCount,
		},
	}, nil
}

// GetTwin returns details for a specific twin.
func (h *TwinHandler) GetTwin(ctx context.Context, req *parytyv1.GetTwinRequest) (*parytyv1.TwinInfo, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.TwinId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id is required")
	}

	t, err := h.tm.GetTwin(ctx, tenantID, req.TwinId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "get twin: %v", err)
	}
	return twinToProto(t), nil
}

// UpdateTwin updates twin metadata.
func (h *TwinHandler) UpdateTwin(ctx context.Context, req *parytyv1.UpdateTwinRequest) (*parytyv1.TwinInfo, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.TwinId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id is required")
	}

	var cfg *twin.TwinConfig
	if req.Config != nil {
		c := twinConfigFromProto(req.Config)
		cfg = &c
	}

	t, err := h.tm.UpdateTwin(ctx, tenantID, req.TwinId, req.Name, req.Description, cfg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update twin: %v", err)
	}

	// Audit log twin update.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "twin.update", "twin", req.TwinId, map[string]interface{}{
			"twin_id": req.TwinId,
			"name":    req.Name,
		})
	}

	return twinToProto(t), nil
}

// DeleteTwin soft-deletes a twin and disassociates all agents.
func (h *TwinHandler) DeleteTwin(ctx context.Context, req *parytyv1.DeleteTwinRequest) (*emptypb.Empty, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.TwinId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id is required")
	}

	if err := h.tm.DeleteTwin(ctx, tenantID, req.TwinId); err != nil {
		return nil, status.Errorf(codes.Internal, "delete twin: %v", err)
	}

	// Clean up twin-scoped Redpanda topics (best-effort).
	if h.topics != nil {
		if err := stream.DeleteTwinTopics(ctx, h.topics.Client(), tenantID, req.TwinId); err != nil {
			slog.Error("twin topic cleanup failed (best-effort)", "twin_id", req.TwinId, "error", err)
		}
	}

	// Invalidate entire tenant routing cache (twin deletion affects multiple agents).
	if h.routing != nil {
		h.routing.InvalidateTenant(ctx, tenantID)
	}

	// Audit log the twin deletion.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "twin.delete", "twin", req.TwinId, map[string]interface{}{
			"twin_id": req.TwinId,
		})
	}

	return &emptypb.Empty{}, nil
}

// GetTwinConfig returns the agent configuration for a twin.
// Called by agents during registration to discover their configuration.
func (h *TwinHandler) GetTwinConfig(ctx context.Context, req *parytyv1.GetTwinConfigRequest) (*parytyv1.TwinConfig, error) {
	if req.TwinId == "" || req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id and agent_id are required")
	}

	cfg, err := h.tm.GetTwinConfigForAgent(ctx, req.TwinId, req.AgentId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "get twin config: %v", err)
	}
	return twinConfigToProto(cfg), nil
}

// ResolveIdentity is called by agents at boot to discover their assigned
// twin/client identity. Returns unassigned if no assignment exists.
func (h *TwinHandler) ResolveIdentity(ctx context.Context, req *parytyv1.ResolveIdentityRequest) (*parytyv1.ResolveIdentityResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	identity, err := h.assigner.GetAgentIdentity(ctx, req.AgentId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve identity: %v", err)
	}

	return &parytyv1.ResolveIdentityResponse{
		Assigned:    identity.Assigned,
		TwinId:      identity.TwinID,
		ClientId:    identity.ClientID,
		TopicPrefix: identity.TopicPrefix,
	}, nil
}

// AssignAgentToTwin assigns an agent to a twin and pushes identity to the agent.
func (h *TwinHandler) AssignAgentToTwin(ctx context.Context, req *parytyv1.AssignAgentToTwinRequest) (*parytyv1.AssignAgentToTwinResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.TwinId == "" || req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id and agent_id are required")
	}

	// Look up the twin to validate it exists and belongs to the tenant.
	_, err = h.tm.GetTwin(ctx, tenantID, req.TwinId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "twin not found: %v", err)
	}

	// Derive client_id and topic_prefix from the assignment.
	clientID := fmt.Sprintf("client_%s", tenantID[:12])
	topicPrefix := fmt.Sprintf("clients.%s.twin_%s", clientID, req.TwinId[:8])

	assignment, err := h.assigner.RegisterAgent(ctx, req.AgentId, req.TwinId, tenantID, clientID, topicPrefix)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "assign agent: %v", err)
	}

	// Invalidate routing cache for this agent.
	if h.routing != nil {
		h.routing.InvalidateAgent(ctx, tenantID, req.AgentId)
	}

	// Audit log the assignment.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "twin.assign_agent", "twin", req.TwinId, map[string]interface{}{
			"agent_id": req.AgentId,
			"twin_id":  req.TwinId,
		})
	}

	// Push identity to the agent (via stream or queued command).
	if err := h.commands.EnqueueIdentityCommand(ctx, req.AgentId, assignment.TwinID, clientID, topicPrefix); err != nil {
		// Assignment succeeded even if command delivery fails — the agent
		// will pick up identity on next heartbeat or re-registration.
	}

	return &parytyv1.AssignAgentToTwinResponse{
		Accepted: true,
	}, nil
}

// ListTwinAgents returns all agents associated with a twin.
func (h *TwinHandler) ListTwinAgents(ctx context.Context, req *parytyv1.ListTwinAgentsRequest) (*parytyv1.ListTwinAgentsResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if req.TwinId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id is required")
	}

	// Verify twin belongs to the authenticated tenant before listing agents.
	if _, err := h.tm.GetTwin(ctx, tenantID, req.TwinId); err != nil {
		return nil, status.Errorf(codes.NotFound, "twin not found: %v", err)
	}

	assignments, err := h.assigner.ListAgentsForTwin(ctx, req.TwinId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list twin agents: %v", err)
	}

	agents := make([]*parytyv1.TwinAgentInfo, 0, len(assignments))
	for _, assign := range assignments {
		info := &parytyv1.TwinAgentInfo{
			AgentId:  assign.AgentID,
			Hostname: "", // populated from agents table in Task 5
			Status:   "unknown",
			Assigned: true,
			Os:       "", // populated from agents table in Task 5
			Arch:     "", // populated from agents table in Task 5
		}

		// Enrich with backlog info if present.
		if bl, err := h.backlogs.GetBacklog(ctx, assign.AgentID); err == nil {
			info.BacklogBytes = bl.BacklogBytes
		}

		agents = append(agents, info)
	}

	return &parytyv1.ListTwinAgentsResponse{
		Agents: agents,
	}, nil
}

// AcceptBacklog instructs the agent to begin uploading its historical backlog.
func (h *TwinHandler) AcceptBacklog(ctx context.Context, req *parytyv1.AcceptBacklogRequest) (*parytyv1.AcceptBacklogResponse, error) {
	if req.TwinId == "" || req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id and agent_id are required")
	}

	if err := h.backlogs.AcceptBacklog(ctx, req.AgentId); err != nil {
		return nil, status.Errorf(codes.Internal, "accept backlog: %v", err)
	}

	// Enqueue UPLOAD_BACKLOG command for the agent (type 6).
	if err := h.commands.EnqueueCommand(ctx, req.AgentId, 6, `{"action":"upload_backlog"}`); err != nil {
		// Backlog accepted; command delivery is best-effort.
	}

	return &parytyv1.AcceptBacklogResponse{
		Accepted: true,
	}, nil
}

// RejectBacklog instructs the agent to permanently delete its local backlog.
func (h *TwinHandler) RejectBacklog(ctx context.Context, req *parytyv1.RejectBacklogRequest) (*parytyv1.RejectBacklogResponse, error) {
	if req.TwinId == "" || req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "twin_id and agent_id are required")
	}

	if err := h.backlogs.RejectBacklog(ctx, req.AgentId); err != nil {
		return nil, status.Errorf(codes.Internal, "reject backlog: %v", err)
	}

	// Enqueue DELETE_BACKLOG command for the agent (type 7).
	if err := h.commands.EnqueueCommand(ctx, req.AgentId, 7, `{"action":"delete_backlog"}`); err != nil {
		// Backlog rejected; command delivery is best-effort.
	}

	return &parytyv1.RejectBacklogResponse{
		Deleted: true,
	}, nil
}

// ═══════════════════════════════════════════════════════════════════════
// Dual Reality Agent System — Lifecycle Management Handlers
// ═══════════════════════════════════════════════════════════════════════

// PairAgentToTwin assigns an edge agent's cluster agent to a Paryty Twin.
func (h *TwinHandler) PairAgentToTwin(ctx context.Context, req *parytyv1.PairAgentToTwinRequest) (*parytyv1.PairAgentToTwinResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.AgentId == "" || req.TwinId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id and twin_id are required")
	}

	if err := h.assigner.PairAgent(ctx, req.AgentId, req.TwinId, tenantID); err != nil {
		return &parytyv1.PairAgentToTwinResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Invalidate routing cache for this agent.
	if h.routing != nil {
		h.routing.InvalidateAgent(ctx, tenantID, req.AgentId)
	}

	// Push identity to the agent.
	clientID := fmt.Sprintf("client_%s", tenantID[:12])
	topicPrefix := fmt.Sprintf("clients.%s.twin_%s", clientID, req.TwinId[:8])
	_ = h.commands.EnqueueIdentityCommand(ctx, req.AgentId, req.TwinId, clientID, topicPrefix)

	// Audit log the pairing.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "agent.pair", "agent", req.AgentId, map[string]interface{}{
			"agent_id": req.AgentId,
			"twin_id":  req.TwinId,
		})
	}

	return &parytyv1.PairAgentToTwinResponse{Success: true}, nil
}

// UnpairAgent unlinks a cluster agent from its Paryty Twin.
func (h *TwinHandler) UnpairAgent(ctx context.Context, req *parytyv1.UnpairAgentRequest) (*parytyv1.UnpairAgentResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	if err := h.assigner.UnpairAgent(ctx, req.AgentId, tenantID); err != nil {
		return nil, status.Errorf(codes.Internal, "unpair agent: %v", err)
	}

	// Invalidate routing cache for this agent.
	if h.routing != nil {
		h.routing.InvalidateAgent(ctx, tenantID, req.AgentId)
	}

	// Notify the edge agent via command.
	_ = h.commands.EnqueueUnpairCommand(ctx, req.AgentId)

	// Audit log the unpairing.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "agent.unpair", "agent", req.AgentId, map[string]interface{}{
			"agent_id": req.AgentId,
		})
	}

	return &parytyv1.UnpairAgentResponse{Success: true}, nil
}

// RetireAgent gracefully decommissions an edge agent.
func (h *TwinHandler) RetireAgent(ctx context.Context, req *parytyv1.RetireAgentRequest) (*parytyv1.RetireAgentResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	if err := h.assigner.RetireAgent(ctx, req.AgentId, tenantID); err != nil {
		return nil, status.Errorf(codes.Internal, "retire agent: %v", err)
	}

	// Notify the edge agent to shut down.
	_ = h.commands.EnqueueRetireCommand(ctx, req.AgentId)

	// Audit log the retirement.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "agent.retire", "agent", req.AgentId, map[string]interface{}{
			"agent_id": req.AgentId,
		})
	}

	return &parytyv1.RetireAgentResponse{Success: true}, nil
}

// BlacklistAgent blocks an edge agent from ever registering again.
func (h *TwinHandler) BlacklistAgent(ctx context.Context, req *parytyv1.BlacklistAgentRequest) (*parytyv1.BlacklistAgentResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	reason := req.Reason
	if reason == "" {
		reason = "blacklisted by operator"
	}

	if err := h.assigner.BlacklistAgent(ctx, req.AgentId, tenantID, reason); err != nil {
		return nil, status.Errorf(codes.Internal, "blacklist agent: %v", err)
	}

	// Notify the edge agent.
	_ = h.commands.EnqueueBlacklistCommand(ctx, req.AgentId, reason)

	// Audit log the blacklisting.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "agent.blacklist", "agent", req.AgentId, map[string]interface{}{
			"agent_id": req.AgentId,
			"reason":   reason,
		})
	}

	return &parytyv1.BlacklistAgentResponse{Success: true}, nil
}

// UnregisterAgent completely removes an edge agent from the system.
func (h *TwinHandler) UnregisterAgent(ctx context.Context, req *parytyv1.UnregisterAgentRequest) (*emptypb.Empty, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	if err := h.assigner.UnregisterAgent(ctx, req.AgentId, tenantID); err != nil {
		return nil, status.Errorf(codes.Internal, "unregister agent: %v", err)
	}

	// Audit log the unregistration.
	if h.audit != nil {
		h.audit.LogAction(tenantID, "", "agent.unregister", "agent", req.AgentId, map[string]interface{}{
			"agent_id": req.AgentId,
		})
	}

	return &emptypb.Empty{}, nil
}

// GetAgentPairingStatus returns detailed pairing info for the smart modal.
func (h *TwinHandler) GetAgentPairingStatus(ctx context.Context, req *parytyv1.GetAgentPairingStatusRequest) (*parytyv1.AgentPairingStatusResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	ps, err := h.assigner.GetAgentPairingStatus(ctx, req.AgentId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "get pairing status: %v", err)
	}

	resp := &parytyv1.AgentPairingStatusResponse{
		AgentId:         ps.AgentID,
		EdgeStatus:      ps.EdgeStatus,
		IsPaired:        ps.IsPaired,
		Os:              ps.OS,
		Arch:            ps.Arch,
		Hostname:        ps.Hostname,
		BlacklistReason: ps.BlacklistReason,
	}
	if ps.ClusterAgentID != nil {
		resp.ClusterAgentId = *ps.ClusterAgentID
	}
	if ps.ClusterAgentName != nil {
		resp.ClusterAgentName = *ps.ClusterAgentName
	}
	if ps.ClusterAgentStatus != nil {
		resp.ClusterAgentStatus = *ps.ClusterAgentStatus
	}
	if ps.PairedAt != nil {
		resp.PairedAt = timestamppb.Now() // placeholder
	}
	if ps.RetiredAt != nil {
		resp.RetiredAt = timestamppb.Now()
	}
	if ps.BlacklistedAt != nil {
		resp.BlacklistedAt = timestamppb.Now()
	}

	return resp, nil
}

// CheckBlacklist checks if an edge agent is in the blacklist.
func (h *TwinHandler) CheckBlacklist(ctx context.Context, req *parytyv1.CheckBlacklistRequest) (*parytyv1.CheckBlacklistResponse, error) {
	tenantID, err := tenantFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	blacklisted, err := h.assigner.IsBlacklisted(ctx, tenantID, req.AgentId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "check blacklist: %v", err)
	}

	return &parytyv1.CheckBlacklistResponse{
		Blacklisted: blacklisted,
	}, nil
}

// ── conversion helpers ────────────────────────────────────────────────────

// twinToProto converts a twin.Twin to a protobuf TwinInfo.
func twinToProto(t *twin.Twin) *parytyv1.TwinInfo {
	return &parytyv1.TwinInfo{
		Id:          t.ID,
		TenantId:    t.TenantID,
		Name:        t.Name,
		Description: t.Description,
		Status:      t.Status,
		AgentCount:  int32(t.AgentCount),
		Config:      twinConfigToProto(&t.Config),
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
	}
}

// twinConfigFromProto converts a protobuf TwinConfig to twin.TwinConfig.
func twinConfigFromProto(cfg *parytyv1.TwinConfig) twin.TwinConfig {
	if cfg == nil {
		return twin.TwinConfig{}
	}
	return twin.TwinConfig{
		AgentLabels:               cfg.AgentLabels,
		EnabledCollectors:         cfg.EnabledCollectors,
		CollectionIntervalSeconds: cfg.CollectionIntervalSeconds,
		SamplingRate:              cfg.SamplingRate,
	}
}

// twinConfigToProto converts a twin.TwinConfig to a protobuf TwinConfig.
func twinConfigToProto(cfg *twin.TwinConfig) *parytyv1.TwinConfig {
	if cfg == nil {
		return &parytyv1.TwinConfig{}
	}
	return &parytyv1.TwinConfig{
		AgentLabels:               cfg.AgentLabels,
		EnabledCollectors:         cfg.EnabledCollectors,
		CollectionIntervalSeconds: cfg.CollectionIntervalSeconds,
		SamplingRate:              cfg.SamplingRate,
	}
}

