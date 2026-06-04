// gRPC adapter for IngestionService.
// Bridges proto-generated gRPC interface to the domain-focused IngestionService.
//
// Hardened with:
//   - Input validation on all RPC methods (InvalidArgument)
//   - Per-agent token-bucket rate limiting (ResourceExhausted)
//   - Correlation ID propagation from gRPC metadata
package api

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// gRPC metadata keys.
const (
	tenantMetadataKey      = "x-tenant-id"
	correlationMetadataKey = "x-correlation-id"
)

// maxFutureSkew is the maximum allowed clock skew for timestamp validation.
// Timestamps more than maxFutureSkew in the future are rejected.
const maxFutureSkew = 5 * time.Minute

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys from other packages.
type contextKey string

const (
	ctxKeyTenant        contextKey = "tenant"
	ctxKeyCorrelationID contextKey = "correlation_id"
)

// TenantFromContext extracts the tenant stored in ctx.
// Returns "default" if no tenant is set.
func TenantFromContext(ctx context.Context) string {
	if t, ok := ctx.Value(ctxKeyTenant).(string); ok && t != "" {
		return t
	}
	return "default"
}

// CorrelationIDFromContext extracts the correlation ID stored in ctx.
// Returns empty string if not set.
func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(ctxKeyCorrelationID).(string); ok {
		return id
	}
	return ""
}

// IngestionGRPCAdapter wraps IngestionService to implement the gRPC IngestionServiceServer interface.
type IngestionGRPCAdapter struct {
	pb.UnimplementedIngestionServiceServer
	svc         *IngestionService
	logger      *zap.Logger
	rateLimiter *RateLimiter
}

// NewIngestionGRPCAdapter creates a new gRPC adapter for the ingestion service.
// If rateLimiter is nil, rate limiting is disabled.
func NewIngestionGRPCAdapter(svc *IngestionService, logger *zap.Logger, rateLimiter *RateLimiter) *IngestionGRPCAdapter {
	return &IngestionGRPCAdapter{
		svc:         svc,
		logger:      logger,
		rateLimiter: rateLimiter,
	}
}

// enrichContext extracts tenant and correlation ID from gRPC metadata
// and stores them in the returned context. Also logs the correlation ID.
func (a *IngestionGRPCAdapter) enrichContext(ctx context.Context) context.Context {
	tenant := "default"
	correlationID := ""

	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if vals := md.Get(tenantMetadataKey); len(vals) > 0 && vals[0] != "" {
			tenant = vals[0]
		}
		if vals := md.Get(correlationMetadataKey); len(vals) > 0 && vals[0] != "" {
			correlationID = vals[0]
		}
	}

	if correlationID == "" {
		correlationID = uuid.New().String()
	}

	ctx = context.WithValue(ctx, ctxKeyTenant, tenant)
	ctx = context.WithValue(ctx, ctxKeyCorrelationID, correlationID)

	return ctx
}

// isRateLimited checks if the agent has exceeded its rate limit.
// Returns a gRPC status error if rate-limited, nil otherwise.
func (a *IngestionGRPCAdapter) isRateLimited(agentID string) error {
	if a.rateLimiter == nil {
		return nil
	}
	if !a.rateLimiter.Allow(agentID) {
		return status.Errorf(codes.ResourceExhausted,
			"rate limit exceeded for agent %s", agentID)
	}
	return nil
}

// validateBatch validates a MetricBatch request.
// Returns a gRPC InvalidArgument error if validation fails.
func validateBatch(req *pb.MetricBatch) error {
	if strings.TrimSpace(req.AgentId) == "" {
		return status.Error(codes.InvalidArgument, "agent_id must not be empty")
	}

	if req.Timestamp == nil {
		return status.Error(codes.InvalidArgument, "timestamp must not be nil")
	}

	ts := req.Timestamp.AsTime()
	if ts.IsZero() {
		return status.Error(codes.InvalidArgument, "timestamp must not be zero")
	}

	now := time.Now()
	if ts.After(now.Add(maxFutureSkew)) {
		return status.Errorf(codes.InvalidArgument,
			"timestamp %v is more than %v in the future (server time: %v)",
			ts, maxFutureSkew, now)
	}

	if !hasMetricType(req) {
		return status.Error(codes.InvalidArgument,
			"batch must contain at least one metric type (cpu, memory, disk, network, or processes)")
	}

	return nil
}

// hasMetricType returns true if the batch contains at least one metric type.
func hasMetricType(req *pb.MetricBatch) bool {
	return req.Cpu != nil ||
		req.Memory != nil ||
		len(req.Disks) > 0 ||
		len(req.Interfaces) > 0 ||
		len(req.Processes) > 0
}

// validateNetworkEventBatch validates a NetworkEventBatch request.
// Returns a gRPC InvalidArgument error if validation fails.
func validateNetworkEventBatch(batch *pb.NetworkEventBatch) error {
	if strings.TrimSpace(batch.AgentId) == "" {
		return status.Error(codes.InvalidArgument, "agent_id must not be empty")
	}

	if len(batch.Events) == 0 {
		return status.Error(codes.InvalidArgument, "events must not be empty")
	}

	return nil
}

// RegisterAgent handles agent registration via gRPC.
func (a *IngestionGRPCAdapter) RegisterAgent(ctx context.Context, req *pb.AgentRegistration) (*pb.AgentRegistrationResponse, error) {
	// Validate input
	if strings.TrimSpace(req.AgentId) == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id must not be empty")
	}
	if strings.TrimSpace(req.Hostname) == "" {
		return nil, status.Error(codes.InvalidArgument, "hostname must not be empty")
	}

	// Enrich context with tenant + correlation ID
	ctx = a.enrichContext(ctx)
	correlationID := CorrelationIDFromContext(ctx)
	tenant := TenantFromContext(ctx)

	a.logger.Info("RegisterAgent request",
		zap.String("correlation_id", correlationID),
		zap.String("tenant", tenant),
		zap.String("agent_id", req.AgentId),
	)

	// Convert proto request to domain model
	domainReq := &models.AgentRegistration{
		AgentID:      req.AgentId,
		Hostname:     req.Hostname,
		IPAddress:    firstOrEmpty(req.IpAddresses),
		AgentVersion: req.Version,
		Labels:       labelsFromProto(req.Labels),
		OS:           "",
		Arch:         "",
	}
	if req.Capabilities != nil {
		domainReq.OS = req.Capabilities.Os
		domainReq.Arch = req.Capabilities.Arch
	}

	// Call domain service
	agent, err := a.svc.RegisterAgent(ctx, tenant, domainReq)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "register agent: %v", err)
	}

	// Convert domain response to proto
	return &pb.AgentRegistrationResponse{
		SessionId:  agent.ID + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		ServerTime: timestamppb.Now(),
		Config: &pb.AgentConfig{
			CollectionIntervalMs: 10000,
			SamplingRate:         1.0,
			MetalEnabled:         true,
			EbpfEnabled:          true,
			SupervisorEnabled:    true,
			Compression:          "zstd",
			MaxBatchSize:         100,
			MaxBatchAgeMs:        10000,
		},
	}, nil
}

// SendBatch handles a single metric batch via gRPC.
func (a *IngestionGRPCAdapter) SendBatch(ctx context.Context, req *pb.MetricBatch) (*pb.SendBatchResponse, error) {
	// Validate input
	if err := validateBatch(req); err != nil {
		return nil, err
	}

	// Enrich context with tenant + correlation ID
	ctx = a.enrichContext(ctx)
	correlationID := CorrelationIDFromContext(ctx)
	tenant := TenantFromContext(ctx)

	a.logger.Debug("SendBatch request",
		zap.String("correlation_id", correlationID),
		zap.String("tenant", tenant),
		zap.String("agent_id", req.AgentId),
	)

	// Rate limit check
	if err := a.isRateLimited(req.AgentId); err != nil {
		return nil, err
	}

	// Convert proto MetricBatch to domain model
	batch := metricBatchFromProto(req)

	// Call domain service
	if err := a.svc.SendBatch(ctx, tenant, req.AgentId, batch); err != nil {
		return &pb.SendBatchResponse{
			Accepted: false,
			Error: &pb.Error{
				Code:    "INGESTION_ERROR",
				Message: err.Error(),
			},
			ServerTime: timestamppb.Now(),
		}, nil
	}

	return &pb.SendBatchResponse{
		Accepted:   true,
		BatchId:    fmt.Sprintf("batch-%d", time.Now().UnixNano()),
		ServerTime: timestamppb.Now(),
	}, nil
}

// Heartbeat handles agent heartbeat via gRPC.
func (a *IngestionGRPCAdapter) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	// Validate input
	if strings.TrimSpace(req.AgentId) == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id must not be empty")
	}

	// Enrich context
	ctx = a.enrichContext(ctx)
	correlationID := CorrelationIDFromContext(ctx)
	tenant := TenantFromContext(ctx)

	a.logger.Debug("Heartbeat request",
		zap.String("correlation_id", correlationID),
		zap.String("tenant", tenant),
		zap.String("agent_id", req.AgentId),
	)

	if err := a.svc.Heartbeat(ctx, tenant, req.AgentId); err != nil {
		return nil, status.Errorf(codes.NotFound, "heartbeat: %v", err)
	}

	return &pb.HeartbeatResponse{
		ServerTime:      timestamppb.Now(),
		ContinueSending: true,
	}, nil
}

// StreamMetrics handles bidirectional streaming for real-time metrics.
func (a *IngestionGRPCAdapter) StreamMetrics(stream grpc.BidiStreamingServer[pb.AgentToCluster, pb.ClusterToAgent]) error {
	ctx := a.enrichContext(stream.Context())
	correlationID := CorrelationIDFromContext(ctx)
	tenant := TenantFromContext(ctx)

	a.logger.Info("StreamMetrics connection opened",
		zap.String("correlation_id", correlationID),
		zap.String("tenant", tenant),
	)

	// Send an initial FlowControl message immediately after accepting the stream.
	// This is required to break a gRPC protocol deadlock: gRPC-Go only sends
	// HTTP/2 response headers when the server calls Send() for the first time.
	// Tonic (Rust) clients wait for response headers before allowing the caller
	// to send messages. Without this initial message, both sides block forever.
	if err := stream.Send(&pb.ClusterToAgent{
		Message: &pb.ClusterToAgent_FlowControl{
			FlowControl: &pb.FlowControl{
				DesiredIntervalMs: 10000,
				SamplingRate:      1.0,
				Pause:             false,
			},
		},
	}); err != nil {
		return status.Errorf(codes.Internal, "send initial flow control: %v", err)
	}

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			a.logger.Info("StreamMetrics connection closed by client",
				zap.String("correlation_id", correlationID),
			)
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Internal, "receive error: %v", err)
		}

		switch m := msg.Message.(type) {
		case *pb.AgentToCluster_Metrics:
			// Validate the streamed message
			if valErr := validateBatch(m.Metrics); valErr != nil {
				a.logger.Warn("Invalid streamed metrics",
					zap.String("correlation_id", correlationID),
					zap.Error(valErr),
				)
				// Continue the stream — don't terminate on a single bad message.
				continue
			}

			// Rate limit check
			if rateErr := a.isRateLimited(m.Metrics.AgentId); rateErr != nil {
				a.logger.Warn("Stream rate limited",
					zap.String("correlation_id", correlationID),
					zap.String("agent_id", m.Metrics.AgentId),
				)
				continue
			}

			batch := metricBatchFromProto(m.Metrics)
			if err := a.svc.SendBatch(ctx, tenant, m.Metrics.AgentId, batch); err != nil {
				a.logger.Error("Failed to process streamed metrics",
					zap.String("correlation_id", correlationID),
					zap.Error(err),
				)
			}

		case *pb.AgentToCluster_Heartbeat:
			if strings.TrimSpace(m.Heartbeat.AgentId) == "" {
				a.logger.Warn("Heartbeat with empty agent_id in stream",
					zap.String("correlation_id", correlationID),
				)
				continue
			}

			if err := a.svc.Heartbeat(ctx, tenant, m.Heartbeat.AgentId); err != nil {
				a.logger.Warn("Heartbeat failed in stream",
					zap.String("correlation_id", correlationID),
					zap.Error(err),
				)
			}
		}

		// Send flow control response
		if err := stream.Send(&pb.ClusterToAgent{
			Message: &pb.ClusterToAgent_FlowControl{
				FlowControl: &pb.FlowControl{
					DesiredIntervalMs: 10000,
					SamplingRate:      1.0,
					Pause:             false,
				},
			},
		}); err != nil {
			return status.Errorf(codes.Internal, "send flow control: %v", err)
		}
	}
}

// ReportNetworkEvents handles client-streaming network events.
// Each received batch is validated, stored in QuestDB, and published to Redpanda.
func (a *IngestionGRPCAdapter) ReportNetworkEvents(stream grpc.ClientStreamingServer[pb.NetworkEventBatch, pb.NetworkEventResponse]) error {
	ctx := a.enrichContext(stream.Context())
	correlationID := CorrelationIDFromContext(ctx)
	tenant := TenantFromContext(ctx)

	var accepted int64
	var rejected int64

	for {
		batch, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.NetworkEventResponse{
				AcceptedCount: accepted,
				RejectedCount: rejected,
			})
		}
		if err != nil {
			return status.Errorf(codes.Internal, "receive error: %v", err)
		}

		// Validate the batch.
		if valErr := validateNetworkEventBatch(batch); valErr != nil {
			a.logger.Warn("Invalid network event batch",
				zap.String("correlation_id", correlationID),
				zap.Error(valErr),
			)
			rejected += int64(len(batch.Events))
			continue
		}

		// Rate limit check.
		if rateErr := a.isRateLimited(batch.AgentId); rateErr != nil {
			a.logger.Warn("Network event rate limited",
				zap.String("correlation_id", correlationID),
				zap.String("agent_id", batch.AgentId),
			)
			rejected += int64(len(batch.Events))
			continue
		}

		a.logger.Debug("Processing network events",
			zap.String("correlation_id", correlationID),
			zap.String("tenant", tenant),
			zap.String("agent_id", batch.AgentId),
			zap.Int("event_count", len(batch.Events)),
		)

		// Store to QuestDB and publish to Redpanda.
		if err := a.svc.StoreNetworkEvents(ctx, tenant, batch); err != nil {
			a.logger.Error("Failed to store network events",
				zap.String("correlation_id", correlationID),
				zap.String("tenant", tenant),
				zap.String("agent_id", batch.AgentId),
				zap.Error(err),
			)
			rejected += int64(len(batch.Events))
			continue
		}

		accepted += int64(len(batch.Events))
	}
}

// --- Proto-to-domain conversion helpers ---

func metricBatchFromProto(pbBatch *pb.MetricBatch) *models.MetricBatch {
	batch := &models.MetricBatch{
		AgentID:   pbBatch.AgentId,
		Timestamp: timeFromProto(pbBatch.Timestamp),
	}

	if pbBatch.Cpu != nil {
		batch.CPU = []models.CPUMetrics{{
			AgentID:         pbBatch.AgentId,
			Timestamp:       timeFromProto(pbBatch.Timestamp),
			TotalUsagePct:   pbBatch.Cpu.TotalUsagePercent,
			PerCorePct:      pbBatch.Cpu.PerCorePercent,
			LoadAvg1m:       pbBatch.Cpu.LoadAverage_1M,
			LoadAvg5m:       pbBatch.Cpu.LoadAverage_5M,
			LoadAvg15m:      pbBatch.Cpu.LoadAverage_15M,
			FrequencyMHz:    pbBatch.Cpu.FrequencyMhz,
			ContextSwitches: uint64(pbBatch.Cpu.ContextSwitches),
			PhysicalCores:   pbBatch.Cpu.PhysicalCores,
			LogicalCores:    pbBatch.Cpu.LogicalCores,
			ModelName:       pbBatch.Cpu.ModelName,
			VendorID:        pbBatch.Cpu.VendorId,
		}}
	}

	if pbBatch.Memory != nil {
		mem := models.MemoryMetrics{
			AgentID:        pbBatch.AgentId,
			Timestamp:      timeFromProto(pbBatch.Timestamp),
			TotalBytes:     uint64(pbBatch.Memory.TotalBytes),
			UsedBytes:      uint64(pbBatch.Memory.UsedBytes),
			FreeBytes:      uint64(pbBatch.Memory.FreeBytes),
			AvailableBytes: uint64(pbBatch.Memory.AvailableBytes),
			CachedBytes:    uint64(pbBatch.Memory.CachedBytes),
			BufferBytes:    uint64(pbBatch.Memory.BufferBytes),
			SwapTotalBytes: uint64(pbBatch.Memory.SwapTotalBytes),
			SwapUsedBytes:  uint64(pbBatch.Memory.SwapUsedBytes),
			UsagePercent:   pbBatch.Memory.UsagePercent,
		}
		if pbBatch.Memory.Pressure != nil {
			mem.Pressure = &models.MemoryPressure{
				Some10:  pbBatch.Memory.Pressure.Some_10,
				Some60:  pbBatch.Memory.Pressure.Some_60,
				Some300: pbBatch.Memory.Pressure.Some_300,
				Full10:  pbBatch.Memory.Pressure.Full_10,
				Full60:  pbBatch.Memory.Pressure.Full_60,
				Full300: pbBatch.Memory.Pressure.Full_300,
			}
		}
		for _, tp := range pbBatch.Memory.TopProcesses {
			mem.TopProcesses = append(mem.TopProcesses, models.ProcessMemoryEntry{
				PID:      uint32(tp.Pid),
				Name:     tp.Name,
				RSSBytes: uint64(tp.RssBytes),
				VSZBytes: uint64(tp.VszBytes),
			})
		}
		batch.Memory = []models.MemoryMetrics{mem}
	}

	for _, d := range pbBatch.Disks {
		batch.Disk = append(batch.Disk, models.DiskMetrics{
			AgentID:          pbBatch.AgentId,
			Timestamp:        timeFromProto(pbBatch.Timestamp),
			Device:           d.DeviceName,
			MountPoint:       d.MountPoint,
			FilesystemType:   d.FilesystemType,
			TotalBytes:       uint64(d.TotalBytes),
			UsedBytes:        uint64(d.UsedBytes),
			FreeBytes:        uint64(d.FreeBytes),
			ReadBytesPerSec:  uint64(d.ReadBytesPerSec),
			WriteBytesPerSec: uint64(d.WriteBytesPerSec),
			IOPSRead:         uint64(d.ReadOpsPerSec),
			IOPSWrite:        uint64(d.WriteOpsPerSec),
			IOLatencyMs:      d.IoLatencyMs,
			QueueDepth:       d.QueueDepth,
			IsSSD:            d.IsSsd,
			UtilizationPct:   d.UtilizationPct,
		})
	}

	for _, n := range pbBatch.Interfaces {
		net := models.NetworkMetrics{
			AgentID:        pbBatch.AgentId,
			Timestamp:      timeFromProto(pbBatch.Timestamp),
			Interface:      n.InterfaceName,
			RxBytesPerSec:  uint64(n.RxBytesPerSec),
			TxBytesPerSec:  uint64(n.TxBytesPerSec),
			RxPackets:      uint64(n.RxPacketsPerSec),
			TxPackets:      uint64(n.TxPacketsPerSec),
			RxDropped:      n.RxDropped,
			TxDropped:      n.TxDropped,
			Errors:         uint64(n.RxErrors + n.TxErrors),
			EstimatedRTTMs: n.EstimatedRttMs,
			TotalRxBytes:   n.TotalRxBytes,
			TotalTxBytes:   n.TotalTxBytes,
			TotalRxPackets: n.TotalRxPackets,
			TotalTxPackets: n.TotalTxPackets,
			SpeedMbps:      n.SpeedMbps,
			IsUp:           n.IsUp,
		}
		if n.TcpStats != nil {
			net.TCPStats = &models.TCPStats{
				Established:     n.TcpStats.Established,
				TimeWait:        n.TcpStats.TimeWait,
				CloseWait:       n.TcpStats.CloseWait,
				Listen:          n.TcpStats.Listen,
				RetransmitCount: n.TcpStats.RetransmitCount,
			}
		}
		batch.Network = append(batch.Network, net)
	}

	for _, p := range pbBatch.Processes {
		batch.Processes = append(batch.Processes, models.ProcessMetrics{
			AgentID:          pbBatch.AgentId,
			Timestamp:        timeFromProto(pbBatch.Timestamp),
			PID:              uint32(p.Pid),
			ParentPID:        uint32(p.ParentPid),
			Name:             p.Name,
			CommandLine:      p.CommandLine,
			CPUUsagePct:      p.CpuUsagePercent,
			MemoryBytes:      uint64(p.RssBytes),
			VszBytes:         uint64(p.VszBytes),
			Status:           p.Status,
			Threads:          uint32(p.ThreadCount),
			FdCount:          uint32(p.FdCount),
			ContainerID:      p.ContainerId,
			Exe:              p.Exe,
			DiskReadBytes:    p.DiskReadBytes,
			DiskWrittenBytes: p.DiskWrittenBytes,
			UserID:           p.UserId,
			StartedAt:        timeFromProto(p.StartedAt),
		})
	}

	for _, c := range pbBatch.Containers {
		batch.Containers = append(batch.Containers, models.ContainerMetrics{
			AgentID:          pbBatch.AgentId,
			Timestamp:        timeFromProto(pbBatch.Timestamp),
			ContainerID:      c.ContainerId,
			Runtime:          c.Runtime,
			Name:             c.Name,
			Image:            c.Image,
			Status:           c.Status,
			CgroupVersion:    c.CgroupVersion,
			PIDs:             c.Pids,
			MemoryLimitBytes: c.MemoryLimitBytes,
			CPUQuota:         c.CpuQuota,
			CPUShares:        c.CpuShares,
		})
	}

	return batch
}

func timeFromProto(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Now()
	}
	return ts.AsTime()
}

func labelsFromProto(l *pb.Labels) map[string]string {
	if l == nil {
		return nil
	}
	return l.Entries
}

func firstOrEmpty(ss []string) string {
	if len(ss) > 0 {
		return ss[0]
	}
	return ""
}
