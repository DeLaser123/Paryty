package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/paryty/paryty-v1.0/cluster/internal/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func newTestAdapter() *IngestionGRPCAdapter {
	svc := &IngestionService{
		agents: sync.Map{},
	}
	return NewIngestionGRPCAdapter(svc, newTestLogger(), nil, nil, nil, nil)
}

func newTestAdapterWithRateLimit(maxPerMinute int) (*IngestionGRPCAdapter, *RateLimiter) {
	svc := &IngestionService{
		agents: sync.Map{},
	}
	rl := NewRateLimiter(maxPerMinute)
	adapter := NewIngestionGRPCAdapter(svc, newTestLogger(), rl, nil, nil, nil)
	return adapter, rl
}

// --- Context helper tests ---

func TestTenantFromContext_Default(t *testing.T) {
	ctx := context.Background()
	if got := TenantFromContext(ctx); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestTenantFromContext_Set(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyTenant, "acme-corp")
	if got := TenantFromContext(ctx); got != "acme-corp" {
		t.Errorf("expected 'acme-corp', got %q", got)
	}
}

func TestCorrelationIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	if got := CorrelationIDFromContext(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestCorrelationIDFromContext_Set(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyCorrelationID, "abc-123")
	if got := CorrelationIDFromContext(ctx); got != "abc-123" {
		t.Errorf("expected 'abc-123', got %q", got)
	}
}

// --- enrichContext tests ---

func TestEnrichContext_WithMetadata(t *testing.T) {
	adapter := newTestAdapter()

	md := metadata.Pairs(
		twinMetadataKey, "twin-1",
		correlationMetadataKey, "corr-42",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	enriched := adapter.enrichContext(ctx)

	// enrichContext no longer sets tenant (tenant is owned by AuthInterceptor).
	if got := TenantFromContext(enriched); got != "" {
		t.Errorf("expected tenant '' (not set by enrichContext), got %q", got)
	}
	if got := TwinIDFromContext(enriched); got != "twin-1" {
		t.Errorf("expected twin 'twin-1', got %q", got)
	}
	if got := CorrelationIDFromContext(enriched); got != "corr-42" {
		t.Errorf("expected correlation 'corr-42', got %q", got)
	}
}

func TestEnrichContext_NoMetadata_GeneratesCorrelationID(t *testing.T) {
	adapter := newTestAdapter()
	ctx := context.Background()

	enriched := adapter.enrichContext(ctx)

	if got := TenantFromContext(enriched); got != "" {
		t.Errorf("expected tenant '', got %q", got)
	}
	corrID := CorrelationIDFromContext(enriched)
	if corrID == "" {
		t.Error("expected non-empty correlation ID to be generated")
	}
	// UUID v4 format: 8-4-4-4-12
	if len(corrID) != 36 {
		t.Errorf("expected UUID format (36 chars), got %d chars: %q", len(corrID), corrID)
	}
}

func TestEnrichContext_EmptyMetadataValues_DefaultsApplied(t *testing.T) {
	adapter := newTestAdapter()

	// Empty twin_id should result in empty twin context value.
	md := metadata.Pairs(twinMetadataKey, "")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	enriched := adapter.enrichContext(ctx)
	// Tenant is not set by enrichContext — AuthInterceptor owns it.
	if got := TenantFromContext(enriched); got != "" {
		t.Errorf("expected tenant '', got %q", got)
	}
	if got := TwinIDFromContext(enriched); got != "" {
		t.Errorf("expected empty twin ID for empty value, got %q", got)
	}
}

// --- Validation tests ---

func TestValidateBatch_Valid(t *testing.T) {
	req := &pb.MetricBatch{
		AgentId:   "agent-1",
		Timestamp: timestamppb.Now(),
		Cpu: &pb.CpuMetric{
			TotalUsagePercent: 50.0,
		},
	}
	if err := validateBatch(req); err != nil {
		t.Errorf("expected no error for valid batch, got %v", err)
	}
}

func TestValidateBatch_EmptyAgentID(t *testing.T) {
	req := &pb.MetricBatch{
		AgentId:   "",
		Timestamp: timestamppb.Now(),
		Cpu:       &pb.CpuMetric{TotalUsagePercent: 50.0},
	}
	err := validateBatch(req)
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
	if !strings.Contains(st.Message(), "agent_id") {
		t.Errorf("error message should mention agent_id, got %q", st.Message())
	}
}

func TestValidateBatch_WhitespaceAgentID(t *testing.T) {
	req := &pb.MetricBatch{
		AgentId:   "   ",
		Timestamp: timestamppb.Now(),
		Cpu:       &pb.CpuMetric{TotalUsagePercent: 50.0},
	}
	err := validateBatch(req)
	if err == nil {
		t.Fatal("expected error for whitespace-only agent_id")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestValidateBatch_NilTimestamp(t *testing.T) {
	req := &pb.MetricBatch{
		AgentId:   "agent-1",
		Timestamp: nil,
		Cpu:       &pb.CpuMetric{TotalUsagePercent: 50.0},
	}
	err := validateBatch(req)
	if err == nil {
		t.Fatal("expected error for nil timestamp")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
	if !strings.Contains(st.Message(), "timestamp") {
		t.Errorf("error message should mention timestamp, got %q", st.Message())
	}
}

func TestValidateBatch_FutureTimestamp(t *testing.T) {
	future := time.Now().Add(10 * time.Minute)
	req := &pb.MetricBatch{
		AgentId:   "agent-1",
		Timestamp: timestamppb.New(future),
		Cpu:       &pb.CpuMetric{TotalUsagePercent: 50.0},
	}
	err := validateBatch(req)
	if err == nil {
		t.Fatal("expected error for future timestamp")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
	if !strings.Contains(st.Message(), "future") {
		t.Errorf("error message should mention 'future', got %q", st.Message())
	}
}

func TestValidateBatch_FutureTimestampWithinSkew(t *testing.T) {
	// 3 minutes in the future — within the 5-minute skew tolerance
	future := time.Now().Add(3 * time.Minute)
	req := &pb.MetricBatch{
		AgentId:   "agent-1",
		Timestamp: timestamppb.New(future),
		Cpu:       &pb.CpuMetric{TotalUsagePercent: 50.0},
	}
	if err := validateBatch(req); err != nil {
		t.Errorf("expected no error for timestamp within skew tolerance, got %v", err)
	}
}

func TestValidateBatch_NoMetrics(t *testing.T) {
	req := &pb.MetricBatch{
		AgentId:   "agent-1",
		Timestamp: timestamppb.Now(),
		// No cpu, memory, disks, interfaces, or processes
	}
	err := validateBatch(req)
	if err == nil {
		t.Fatal("expected error for batch with no metrics")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
	if !strings.Contains(st.Message(), "at least one metric type") {
		t.Errorf("error message should mention metric types, got %q", st.Message())
	}
}

func TestValidateBatch_HasMetricType(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*pb.MetricBatch)
		want   bool
	}{
		{
			name:   "cpu only",
			modify: func(b *pb.MetricBatch) { b.Cpu = &pb.CpuMetric{TotalUsagePercent: 50} },
			want:   true,
		},
		{
			name:   "memory only",
			modify: func(b *pb.MetricBatch) { b.Memory = &pb.MemoryMetric{TotalBytes: 1000} },
			want:   true,
		},
		{
			name:   "disk only",
			modify: func(b *pb.MetricBatch) { b.Disks = []*pb.DiskMetric{{DeviceName: "sda1"}} },
			want:   true,
		},
		{
			name:   "network only",
			modify: func(b *pb.MetricBatch) { b.Interfaces = []*pb.NetworkMetric{{InterfaceName: "eth0"}} },
			want:   true,
		},
		{
			name:   "processes only",
			modify: func(b *pb.MetricBatch) { b.Processes = []*pb.ProcessMetric{{Pid: 1}} },
			want:   true,
		},
		{
			name:   "none",
			modify: func(b *pb.MetricBatch) {},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch := &pb.MetricBatch{}
			tt.modify(batch)
			if got := hasMetricType(batch); got != tt.want {
				t.Errorf("hasMetricType() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- RegisterAgent validation tests ---

func TestRegisterAgent_EmptyAgentID(t *testing.T) {
	adapter := newTestAdapter()
	req := &pb.AgentRegistration{
		AgentId:  "",
		Hostname: "host-1",
	}
	_, err := adapter.RegisterAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestRegisterAgent_EmptyHostname(t *testing.T) {
	adapter := newTestAdapter()
	req := &pb.AgentRegistration{
		AgentId:  "agent-1",
		Hostname: "",
	}
	_, err := adapter.RegisterAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty hostname")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestRegisterAgent_WhitespaceHostname(t *testing.T) {
	adapter := newTestAdapter()
	req := &pb.AgentRegistration{
		AgentId:  "agent-1",
		Hostname: "   ",
	}
	_, err := adapter.RegisterAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for whitespace hostname")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// --- Heartbeat validation tests ---

func TestHeartbeat_EmptyAgentID(t *testing.T) {
	adapter := newTestAdapter()
	req := &pb.HeartbeatRequest{
		AgentId: "",
	}
	_, err := adapter.Heartbeat(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// --- Adapter construction tests ---

func TestNewIngestionGRPCAdapter(t *testing.T) {
	svc := &IngestionService{agents: sync.Map{}}
	logger := newTestLogger()
	rl := NewRateLimiter(100)

	adapter := NewIngestionGRPCAdapter(svc, logger, rl, nil, nil, nil)

	if adapter.svc == nil {
		t.Fatal("adapter service is nil")
	}
	if adapter.logger == nil {
		t.Fatal("adapter logger is nil")
	}
	if adapter.rateLimiter == nil {
		t.Fatal("adapter rate limiter is nil")
	}
	if adapter.agentAssigner != nil {
		t.Fatal("expected nil agent assigner")
	}
}

func TestNewIngestionGRPCAdapter_NilRateLimiter(t *testing.T) {
	svc := &IngestionService{agents: sync.Map{}}
	adapter := NewIngestionGRPCAdapter(svc, newTestLogger(), nil, nil, nil, nil)

	if adapter.rateLimiter != nil {
		t.Fatal("expected nil rate limiter")
	}

	// isRateLimited should return nil when rate limiter is nil
	if err := adapter.isRateLimited("any-agent"); err != nil {
		t.Errorf("expected nil error with nil rate limiter, got %v", err)
	}
}

// --- Rate limiter integration tests ---

func TestSendBatch_RateLimitExceeded(t *testing.T) {
	adapter, rl := newTestAdapterWithRateLimit(2) // 2 per minute

	// Pre-exhaust tokens directly via the rate limiter so we never
	// reach the nil store/stream inside IngestionService.
	for i := 0; i < 2; i++ {
		if !rl.Allow("agent-1") {
			t.Fatalf("token %d should be allowed", i+1)
		}
	}

	// Next request through the adapter should be rate limited
	req := &pb.MetricBatch{
		AgentId:   "agent-1",
		Timestamp: timestamppb.Now(),
		Cpu:       &pb.CpuMetric{TotalUsagePercent: 90.0},
	}
	_, err := adapter.SendBatch(context.Background(), req)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.ResourceExhausted {
		t.Errorf("expected ResourceExhausted, got %v", st.Code())
	}
	if !strings.Contains(st.Message(), "agent-1") {
		t.Errorf("error should mention agent ID, got %q", st.Message())
	}
}

func TestSendBatch_DifferentAgentsNotCrossLimited(t *testing.T) {
	_, rl := newTestAdapterWithRateLimit(1) // 1 per minute per agent

	// Pre-exhaust agent-1's token directly
	if !rl.Allow("agent-1") {
		t.Fatal("agent-1 first token should be allowed")
	}

	// agent-1 should now be exhausted
	if rl.Allow("agent-1") {
		t.Error("agent-1 should be exhausted after using its token")
	}

	// agent-2 should still have its own separate bucket — not affected by agent-1
	if !rl.Allow("agent-2") {
		t.Error("agent-2 should NOT be affected by agent-1's rate limit")
	}
}

// --- Correlation ID propagation tests ---

func TestSendBatch_CorrelationIDFromMetadata(t *testing.T) {
	adapter := newTestAdapter()

	md := metadata.Pairs(
		correlationMetadataKey, "test-corr-id-123",
		twinMetadataKey, "twin-z",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	// Test enrichment directly — enrichContext doesn't touch the service layer.
	enriched := adapter.enrichContext(ctx)

	// enrichContext no longer reads tenant from metadata (tenant owned by AuthInterceptor).
	if got := TenantFromContext(enriched); got != "" {
		t.Errorf("expected tenant '' (not set by enrichContext), got %q", got)
	}
	if got := TwinIDFromContext(enriched); got != "twin-z" {
		t.Errorf("expected twin 'twin-z', got %q", got)
	}
	if got := CorrelationIDFromContext(enriched); got != "test-corr-id-123" {
		t.Errorf("expected correlation 'test-corr-id-123', got %q", got)
	}
}

func TestSendBatch_CorrelationIDGenerated(t *testing.T) {
	adapter := newTestAdapter()

	// No correlation ID in metadata — should be auto-generated
	ctx := context.Background()

	enriched := adapter.enrichContext(ctx)

	if got := TenantFromContext(enriched); got != "" {
		t.Errorf("expected tenant '', got %q", got)
	}
	corrID := CorrelationIDFromContext(enriched)
	if corrID == "" {
		t.Error("expected auto-generated correlation ID")
	}
	if len(corrID) != 36 {
		t.Errorf("expected UUID format (36 chars), got %d chars: %q", len(corrID), corrID)
	}
}

// --- Rate limiter unit tests ---

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(5)

	for i := 0; i < 5; i++ {
		if !rl.Allow("agent-1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 6th should be rejected
	if rl.Allow("agent-1") {
		t.Error("6th request should be rejected")
	}
}

func TestRateLimiter_DifferentAgents(t *testing.T) {
	rl := NewRateLimiter(2)

	if !rl.Allow("a1") {
		t.Error("a1 first request should be allowed")
	}
	if !rl.Allow("a2") {
		t.Error("a2 first request should be allowed")
	}
	if !rl.Allow("a1") {
		t.Error("a1 second request should be allowed")
	}
	if !rl.Allow("a2") {
		t.Error("a2 second request should be allowed")
	}
	if rl.Allow("a1") {
		t.Error("a1 third request should be rejected")
	}
	if rl.Allow("a2") {
		t.Error("a2 third request should be rejected")
	}
}

func TestRateLimiter_ZeroMax_DefaultsToSafeValue(t *testing.T) {
	rl := NewRateLimiter(0)

	// Should use default of 10000, not panic
	if !rl.Allow("agent-1") {
		t.Error("first request should be allowed with default limit")
	}
}

func TestRateLimiter_NegativeMax_DefaultsToSafeValue(t *testing.T) {
	rl := NewRateLimiter(-1)

	if !rl.Allow("agent-1") {
		t.Error("first request should be allowed with default limit")
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(1000)

	var wg sync.WaitGroup
	allowed := make(chan bool, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow("agent-concurrent")
		}()
	}

	wg.Wait()
	close(allowed)

	allowedCount := 0
	for a := range allowed {
		if a {
			allowedCount++
		}
	}

	// Exactly 1000 should be allowed initially, but we only sent 200
	if allowedCount != 200 {
		t.Errorf("expected 200 allowed (within limit), got %d", allowedCount)
	}
}

func TestRateLimiter_RefillOverTime(t *testing.T) {
	// 60 per minute = 1 per second
	rl := NewRateLimiter(60)

	// Exhaust initial tokens
	for i := 0; i < 60; i++ {
		if !rl.Allow("agent-1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// Should be rate limited now
	if rl.Allow("agent-1") {
		t.Error("should be rate limited after exhausting tokens")
	}

	// Manipulate the bucket to simulate time passing
	bucket := rl.getOrCreateBucket("agent-1")
	bucket.mu.Lock()
	bucket.lastRefill = time.Now().Add(-2 * time.Second) // pretend 2 seconds passed
	bucket.mu.Unlock()

	// Should have refilled ~2 tokens (60/min = 1/sec)
	if !rl.Allow("agent-1") {
		t.Error("should be allowed after refill")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewRateLimiter(100)

	// Create some buckets
	rl.Allow("old-agent")
	rl.Allow("new-agent")

	// Make old-agent stale
	if v, ok := rl.buckets.Load("old-agent"); ok {
		bucket := v.(*tokenBucket)
		bucket.mu.Lock()
		bucket.lastAccess = time.Now().Add(-15 * time.Minute) // older than staleThreshold
		bucket.mu.Unlock()
	}

	rl.cleanup()

	if _, ok := rl.buckets.Load("old-agent"); ok {
		t.Error("old-agent should have been cleaned up")
	}
	if _, ok := rl.buckets.Load("new-agent"); !ok {
		t.Error("new-agent should NOT have been cleaned up")
	}
}

func TestRateLimiter_StartCleanup(t *testing.T) {
	rl := NewRateLimiter(100)
	ctx, cancel := context.WithCancel(context.Background())

	// Should not panic
	rl.StartCleanup(ctx)

	// Cancel to stop the cleanup goroutine
	cancel()

	// Give the goroutine time to exit
	time.Sleep(50 * time.Millisecond)
}

// --- Helper function tests (preserved from original) ---

func TestMetricBatchFromProto(t *testing.T) {
	now := timestamppb.Now()

	pbBatch := &pb.MetricBatch{
		AgentId:   "test-agent-1",
		Timestamp: now,
		Cpu: &pb.CpuMetric{
			TotalUsagePercent: 75.5,
			PerCorePercent:    []float64{80.0, 70.0},
			LoadAverage_1M:    1.5,
			LoadAverage_5M:    1.2,
			LoadAverage_15M:   1.0,
			FrequencyMhz:      3000.0,
			ContextSwitches:   12345,
		},
		Memory: &pb.MemoryMetric{
			TotalBytes:     16000000000,
			UsedBytes:      8000000000,
			AvailableBytes: 8000000000,
			CachedBytes:    2000000000,
			SwapTotalBytes: 4000000000,
			SwapUsedBytes:  1000000000,
		},
		Disks: []*pb.DiskMetric{
			{
				DeviceName:       "sda1",
				MountPoint:       "/",
				TotalBytes:       100000000000,
				UsedBytes:        50000000000,
				ReadBytesPerSec:  1000000,
				WriteBytesPerSec: 500000,
			},
		},
		Interfaces: []*pb.NetworkMetric{
			{
				InterfaceName: "eth0",
				RxBytesPerSec: 5000000,
				TxBytesPerSec: 2000000,
			},
		},
		Processes: []*pb.ProcessMetric{
			{
				Pid:             1234,
				Name:            "nginx",
				CpuUsagePercent: 15.0,
				RssBytes:        100000000,
				ThreadCount:     4,
			},
		},
	}

	batch := metricBatchFromProto(pbBatch)

	if batch.AgentID != "test-agent-1" {
		t.Errorf("expected agent_id=test-agent-1, got %s", batch.AgentID)
	}

	if len(batch.CPU) != 1 {
		t.Fatalf("expected 1 CPU metric, got %d", len(batch.CPU))
	}
	if batch.CPU[0].TotalUsagePct != 75.5 {
		t.Errorf("expected CPU usage=75.5, got %f", batch.CPU[0].TotalUsagePct)
	}
	if batch.CPU[0].FrequencyMHz != 3000.0 {
		t.Errorf("expected frequency=3000, got %f", batch.CPU[0].FrequencyMHz)
	}

	if len(batch.Memory) != 1 {
		t.Fatalf("expected 1 memory metric, got %d", len(batch.Memory))
	}
	if batch.Memory[0].TotalBytes != 16000000000 {
		t.Errorf("expected total_bytes=16000000000, got %d", batch.Memory[0].TotalBytes)
	}

	if len(batch.Disk) != 1 {
		t.Fatalf("expected 1 disk metric, got %d", len(batch.Disk))
	}
	if batch.Disk[0].Device != "sda1" {
		t.Errorf("expected device=sda1, got %s", batch.Disk[0].Device)
	}

	if len(batch.Network) != 1 {
		t.Fatalf("expected 1 network metric, got %d", len(batch.Network))
	}
	if batch.Network[0].Interface != "eth0" {
		t.Errorf("expected interface=eth0, got %s", batch.Network[0].Interface)
	}

	if len(batch.Processes) != 1 {
		t.Fatalf("expected 1 process metric, got %d", len(batch.Processes))
	}
	if batch.Processes[0].Name != "nginx" {
		t.Errorf("expected process name=nginx, got %s", batch.Processes[0].Name)
	}
	if batch.Processes[0].PID != 1234 {
		t.Errorf("expected PID=1234, got %d", batch.Processes[0].PID)
	}
}

func TestTimeFromProto(t *testing.T) {
	tests := []struct {
		name string
		ts   *timestamppb.Timestamp
	}{
		{name: "nil timestamp", ts: nil},
		{name: "valid timestamp", ts: timestamppb.Now()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := timeFromProto(tt.ts)
			if time.Since(result) > time.Second {
				t.Errorf("expected recent time, got %v", result)
			}
		})
	}
}

func TestLabelsFromProto(t *testing.T) {
	tests := []struct {
		name string
		l    *pb.Labels
		want map[string]string
	}{
		{name: "nil labels", l: nil, want: nil},
		{
			name: "valid labels",
			l:    &pb.Labels{Entries: map[string]string{"env": "prod", "region": "us-east-1"}},
			want: map[string]string{"env": "prod", "region": "us-east-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labelsFromProto(tt.l)
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("expected %s=%s, got %s", k, v, got[k])
				}
			}
		})
	}
}

func TestFirstOrEmpty(t *testing.T) {
	tests := []struct {
		name string
		ss   []string
		want string
	}{
		{name: "nil", ss: nil, want: ""},
		{name: "empty", ss: []string{}, want: ""},
		{name: "single", ss: []string{"first"}, want: "first"},
		{name: "multiple", ss: []string{"first", "second"}, want: "first"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstOrEmpty(tt.ss); got != tt.want {
				t.Errorf("firstOrEmpty(%v) = %q, want %q", tt.ss, got, tt.want)
			}
		})
	}
}

// --- Table-driven validation tests ---

func TestSendBatch_Validation_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		req      *pb.MetricBatch
		wantCode codes.Code
	}{
		{
			name: "empty agent_id",
			req: &pb.MetricBatch{
				AgentId:   "",
				Timestamp: timestamppb.Now(),
				Cpu:       &pb.CpuMetric{TotalUsagePercent: 50},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "nil timestamp",
			req: &pb.MetricBatch{
				AgentId:   "agent-1",
				Timestamp: nil,
				Cpu:       &pb.CpuMetric{TotalUsagePercent: 50},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "future timestamp beyond skew",
			req: &pb.MetricBatch{
				AgentId:   "agent-1",
				Timestamp: timestamppb.New(time.Now().Add(10 * time.Minute)),
				Cpu:       &pb.CpuMetric{TotalUsagePercent: 50},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "no metrics",
			req: &pb.MetricBatch{
				AgentId:   "agent-1",
				Timestamp: timestamppb.Now(),
			},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newTestAdapter()
			_, err := adapter.SendBatch(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			st, ok := status.FromError(err)
			if !ok {
				t.Fatalf("expected gRPC status error, got %T", err)
			}
			if st.Code() != tt.wantCode {
				t.Errorf("expected code %v, got %v", tt.wantCode, st.Code())
			}
		})
	}
}

// --- Benchmarks ---

func BenchmarkRateLimiter_Allow(b *testing.B) {
	rl := NewRateLimiter(b.N * 2) // ensure we never hit the limit

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow(fmt.Sprintf("agent-%d", i%100))
	}
}

func BenchmarkRateLimiter_AllowSameAgent(b *testing.B) {
	rl := NewRateLimiter(b.N * 2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow("agent-1")
	}
}

func BenchmarkValidateBatch(b *testing.B) {
	req := &pb.MetricBatch{
		AgentId:   "agent-1",
		Timestamp: timestamppb.Now(),
		Cpu:       &pb.CpuMetric{TotalUsagePercent: 50},
		Memory:    &pb.MemoryMetric{TotalBytes: 1000},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateBatch(req)
	}
}
