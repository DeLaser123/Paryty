package warm

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// ---- Test Helpers ----

// testSender creates an ILPSender connected to an in-memory pipe.
// Writes succeed without a real QuestDB instance. A background goroutine
// drains the pipe to prevent write blocking.
func testSender(t *testing.T) *ILPSender {
	t.Helper()

	server, client := net.Pipe()

	// Drain the server side so writes don't block on synchronous pipe.
	go func() {
		buf := make([]byte, 64*1024)
		for {
			if _, err := server.Read(buf); err != nil {
				return
			}
		}
	}()

	t.Cleanup(func() {
		client.Close()
		server.Close()
	})

	return &ILPSender{
		addr:     "test:9009",
		conn:     client,
		bufLimit: 1024 * 1024, // 1 MB — prevents sender-level auto-flush in tests
	}
}

// testWriter creates an ILPWriter with a pipe-based sender for testing.
// The writer uses a large bufSize to prevent writer-level auto-flush.
func testWriter(t *testing.T, opts ...ILPOption) *ILPWriter {
	t.Helper()

	sender := testSender(t)

	w := &ILPWriter{
		sender:        sender,
		addr:          "test:9009",
		logger:        zap.NewNop(),
		bufSize:       10000, // Large — prevents auto-flush during tests
		flushInterval: 1 * time.Hour, // Long — prevents background flush during tests
		done:          make(chan struct{}),
	}

	for _, opt := range opts {
		opt(w)
	}

	// Note: ticker and runFlushLoop are NOT started for test writers.
	// Tests control flush explicitly.
	return w
}

// testWriterNoConn creates an ILPWriter whose sender has nil conn and bufLimit=0.
// Every SendMetric call will fail (ILP connection is closed).
// Useful for testing fallback behavior.
func testWriterNoConn(t *testing.T, opts ...ILPOption) *ILPWriter {
	t.Helper()

	sender := &ILPSender{
		addr:     "test:9009",
		conn:     nil,      // No connection — all writes fail
		bufLimit: 0,        // Triggers immediate flush attempt on every SendMetric
	}

	w := &ILPWriter{
		sender:        sender,
		addr:          "test:9009",
		logger:        zap.NewNop(),
		bufSize:       defaultILPBufSize,
		flushInterval: defaultILPFlushInterval,
		done:          make(chan struct{}),
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// ---- ILPMetric Formatting Tests ----

func TestILPMetric_FormatILPLine(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name   string
		metric ILPMetric
		want   string
	}{
		{
			name: "basic cpu metric",
			metric: ILPMetric{
				Table:     "cpu_metrics",
				Tags:      map[string]string{"agent_id": "agent-1", "tenant_id": "tenant-a"},
				Fields:    map[string]float64{"total_usage_pct": 50.5, "load_avg_1": 1.5},
				Timestamp: ts,
			},
			// Tags sorted: agent_id < tenant_id. Fields sorted: load_avg_1 < total_usage_pct.
			want: "cpu_metrics,agent_id=agent-1,tenant_id=tenant-a load_avg_1=1.5,total_usage_pct=50.5 1736937000000000000\n",
		},
		{
			name: "single field",
			metric: ILPMetric{
				Table:     "test_table",
				Tags:      map[string]string{"k": "v"},
				Fields:    map[string]float64{"value": 42.0},
				Timestamp: ts,
			},
			want: "test_table,k=v value=42 1736937000000000000\n",
		},
		{
			name: "empty tags",
			metric: ILPMetric{
				Table:     "events",
				Tags:      map[string]string{},
				Fields:    map[string]float64{"count": 10.0},
				Timestamp: ts,
			},
			want: "events count=10 1736937000000000000\n",
		},
		{
			name: "multiple fields sorted alphabetically",
			metric: ILPMetric{
				Table:     "metrics",
				Tags:      map[string]string{"id": "x"},
				Fields:    map[string]float64{"z_field": 1.0, "a_field": 2.0, "m_field": 3.0},
				Timestamp: ts,
			},
			want: "metrics,id=x a_field=2,m_field=3,z_field=1 1736937000000000000\n",
		},
		{
			name: "fractional values",
			metric: ILPMetric{
				Table:     "cpu_metrics",
				Tags:      map[string]string{"agent_id": "a1", "tenant_id": "t1"},
				Fields:    map[string]float64{"p50": 0.123456, "p99": 99.99},
				Timestamp: ts,
			},
			want: "cpu_metrics,agent_id=a1,tenant_id=t1 p50=0.123456,p99=99.99 1736937000000000000\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatILPMetric(tc.metric)
			if got != tc.want {
				t.Errorf("formatILPMetric() =\n  %q\nwant:\n  %q", got, tc.want)
			}
		})
	}
}

func TestILPMetric_FormatILPLine_Idempotent(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	m := ILPMetric{
		Table:     "cpu_metrics",
		Tags:      map[string]string{"agent_id": "a1", "tenant_id": "t1"},
		Fields:    map[string]float64{"value": 42.0, "count": 100.0},
		Timestamp: ts,
	}

	// Format 10 times — output must be identical every time.
	for i := 0; i < 10; i++ {
		got := formatILPMetric(m)
		want := formatILPMetric(m)
		if got != want {
			t.Fatalf("iteration %d: non-deterministic output:\n  %q\n  %q", i, got, want)
		}
	}
}

// ---- WriteBatch Tests ----

func TestILPWriter_WriteBatch(t *testing.T) {
	w := testWriter(t)

	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	metrics := []ILPMetric{
		{
			Table:     "cpu_metrics",
			Tags:      map[string]string{"agent_id": "a1", "tenant_id": "t1"},
			Fields:    map[string]float64{"total_usage_pct": 50.0},
			Timestamp: ts,
		},
		{
			Table:     "cpu_metrics",
			Tags:      map[string]string{"agent_id": "a1", "tenant_id": "t1"},
			Fields:    map[string]float64{"total_usage_pct": 60.0},
			Timestamp: ts.Add(time.Minute),
		},
		{
			Table:     "memory_metrics",
			Tags:      map[string]string{"agent_id": "a1", "tenant_id": "t1"},
			Fields:    map[string]float64{"usage_percent": 75.5},
			Timestamp: ts.Add(2 * time.Minute),
		},
	}

	err := w.WriteBatch(context.Background(), metrics)
	if err != nil {
		t.Fatalf("WriteBatch error: %v", err)
	}

	m := w.Metrics()
	if m.RowsWritten != 3 {
		t.Errorf("RowsWritten = %d, want 3", m.RowsWritten)
	}
	if m.WriteErrors != 0 {
		t.Errorf("WriteErrors = %d, want 0", m.WriteErrors)
	}
	if m.FlushCount < 1 {
		t.Errorf("FlushCount = %d, want >= 1", m.FlushCount)
	}
}

func TestILPWriter_WriteBatch_EmptySlice(t *testing.T) {
	w := testWriter(t)

	err := w.WriteBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("WriteBatch with nil slice: %v", err)
	}

	m := w.Metrics()
	if m.RowsWritten != 0 {
		t.Errorf("RowsWritten = %d, want 0", m.RowsWritten)
	}
}

func TestILPWriter_WriteBatch_ContextCancelled(t *testing.T) {
	w := testWriter(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	metrics := []ILPMetric{
		{Table: "t", Tags: map[string]string{"k": "v"}, Fields: map[string]float64{"f": 1.0}, Timestamp: time.Now()},
	}

	err := w.WriteBatch(ctx, metrics)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---- Flush Tests ----

func TestILPWriter_Flush(t *testing.T) {
	w := testWriter(t)

	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	// Write a single metric (buffered in sender, no auto-flush due to large bufSize).
	err := w.WriteMetric(context.Background(), "test_table",
		map[string]string{"k": "v"},
		map[string]float64{"val": 1.0},
		ts,
	)
	if err != nil {
		t.Fatalf("WriteMetric error: %v", err)
	}

	// Manual flush.
	beforeFlush := time.Now()
	err = w.Flush()
	if err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	m := w.Metrics()
	if m.FlushCount != 1 {
		t.Errorf("FlushCount = %d, want 1", m.FlushCount)
	}
	if m.LastFlushTime.Before(beforeFlush) {
		t.Errorf("LastFlushTime %v should be >= %v", m.LastFlushTime, beforeFlush)
	}
	if m.AvgFlushLatencyMs < 0 {
		t.Errorf("AvgFlushLatencyMs = %f, want >= 0", m.AvgFlushLatencyMs)
	}
	// lineCount should be reset after flush.
	if w.lineCount != 0 {
		t.Errorf("lineCount = %d, want 0 after flush", w.lineCount)
	}
}

func TestILPWriter_Flush_Multiple(t *testing.T) {
	w := testWriter(t)

	for i := 0; i < 5; i++ {
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush %d error: %v", i, err)
		}
	}

	m := w.Metrics()
	if m.FlushCount != 5 {
		t.Errorf("FlushCount = %d, want 5", m.FlushCount)
	}
}

// ---- Metrics Tests ----

func TestILPWriter_Metrics(t *testing.T) {
	w := testWriter(t)

	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	// Initial metrics should be zero.
	m := w.Metrics()
	if m.RowsWritten != 0 || m.WriteErrors != 0 || m.FlushCount != 0 {
		t.Errorf("initial metrics not zero: %+v", m)
	}

	// Write 5 metrics.
	for i := 0; i < 5; i++ {
		err := w.WriteMetric(context.Background(), "test",
			map[string]string{"id": "a1"},
			map[string]float64{"v": float64(i)},
			ts.Add(time.Duration(i)*time.Second),
		)
		if err != nil {
			t.Fatalf("WriteMetric %d error: %v", i, err)
		}
	}

	m = w.Metrics()
	if m.RowsWritten != 5 {
		t.Errorf("RowsWritten = %d, want 5", m.RowsWritten)
	}
	if m.WriteErrors != 0 {
		t.Errorf("WriteErrors = %d, want 0", m.WriteErrors)
	}

	// Flush and verify.
	_ = w.Flush()
	m = w.Metrics()
	if m.FlushCount != 1 {
		t.Errorf("FlushCount = %d, want 1", m.FlushCount)
	}
	if m.AvgFlushLatencyMs < 0 {
		t.Errorf("AvgFlushLatencyMs = %f, want >= 0", m.AvgFlushLatencyMs)
	}
	if m.LastFlushTime.IsZero() {
		t.Error("LastFlushTime should be set after flush")
	}
}

func TestILPWriter_Metrics_ConcurrentSafety(t *testing.T) {
	w := testWriter(t)

	// Verify that calling Metrics() concurrently doesn't race.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = w.Metrics()
		}
	}()

	for i := 0; i < 100; i++ {
		_ = w.WriteMetric(context.Background(), "t", map[string]string{"k": "v"}, map[string]float64{"f": float64(i)}, time.Now())
	}

	<-done
}

// ---- Fallback Tests ----

func TestILPWriter_Fallback(t *testing.T) {
	var fallbackCalled bool
	var receivedBatch *models.MetricBatch
	var receivedTenant string

	fallback := func(batch *models.MetricBatch, tenant string) error {
		fallbackCalled = true
		receivedBatch = batch
		receivedTenant = tenant
		return nil
	}

	w := testWriterNoConn(t, WithFallback(fallback))

	batch := &models.MetricBatch{
		AgentID: "agent-1",
		Timestamp: time.Now(),
		CPU: []models.CPUMetrics{
			{
				AgentID:       "agent-1",
				Timestamp:     time.Now(),
				TotalUsagePct: 50.0,
				LoadAvg1m:     1.5,
			},
		},
	}

	err := w.WriteMetricBatch(batch, "tenant-a")
	if err != nil {
		t.Fatalf("WriteMetricBatch returned error: %v", err)
	}

	if !fallbackCalled {
		t.Error("fallback function was not called")
	}
	if receivedBatch != batch {
		t.Error("fallback received wrong batch pointer")
	}
	if receivedTenant != "tenant-a" {
		t.Errorf("fallback tenant = %q, want %q", receivedTenant, "tenant-a")
	}

	m := w.Metrics()
	if m.WriteErrors == 0 {
		t.Error("WriteErrors should be > 0 after ILP failure")
	}
}

func TestILPWriter_Fallback_PropagatesError(t *testing.T) {
	fallbackErr := fmt.Errorf("PG insert failed")

	w := testWriterNoConn(t, WithFallback(func(batch *models.MetricBatch, tenant string) error {
		return fallbackErr
	}))

	batch := &models.MetricBatch{
		AgentID:   "agent-1",
		Timestamp: time.Now(),
		Memory: []models.MemoryMetrics{
			{AgentID: "agent-1", Timestamp: time.Now(), TotalBytes: 1024},
		},
	}

	err := w.WriteMetricBatch(batch, "t")
	if err == nil {
		t.Fatal("expected error when both ILP and fallback fail")
	}

	// The error should contain context about both failures.
	if err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

func TestILPWriter_Fallback_NilFallback(t *testing.T) {
	// Without a fallback, ILP errors should be returned directly.
	w := testWriterNoConn(t) // No WithFallback

	batch := &models.MetricBatch{
		AgentID:   "agent-1",
		Timestamp: time.Now(),
		CPU: []models.CPUMetrics{
			{AgentID: "agent-1", Timestamp: time.Now()},
		},
	}

	err := w.WriteMetricBatch(batch, "t")
	if err == nil {
		t.Fatal("expected error when ILP fails without fallback")
	}
}

// ---- Write After Close Tests ----

func TestILPWriter_WriteAfterClose(t *testing.T) {
	w := testWriter(t)

	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// All write methods should return ErrILPWriterClosed.
	err := w.WriteMetric(context.Background(), "t", nil, nil, time.Now())
	if err != ErrILPWriterClosed {
		t.Errorf("WriteMetric after close: got %v, want ErrILPWriterClosed", err)
	}

	err = w.WriteBatch(context.Background(), []ILPMetric{{Table: "t"}})
	if err != ErrILPWriterClosed {
		t.Errorf("WriteBatch after close: got %v, want ErrILPWriterClosed", err)
	}

	err = w.WriteMetricBatch(&models.MetricBatch{}, "t")
	if err != ErrILPWriterClosed {
		t.Errorf("WriteMetricBatch after close: got %v, want ErrILPWriterClosed", err)
	}
}

func TestILPWriter_CloseIdempotent(t *testing.T) {
	w := testWriter(t)

	for i := 0; i < 5; i++ {
		err := w.Close()
		if err != nil {
			t.Fatalf("Close call %d error: %v", i, err)
		}
	}
}

// ---- Options Tests ----

func TestILPWriter_Options(t *testing.T) {
	w := testWriter(t,
		WithBufferSize(500),
		WithFlushInterval(5*time.Second),
	)

	if w.bufSize != 500 {
		t.Errorf("bufSize = %d, want 500", w.bufSize)
	}
	if w.flushInterval != 5*time.Second {
		t.Errorf("flushInterval = %v, want 5s", w.flushInterval)
	}
	if w.fallbackFn != nil {
		t.Error("fallbackFn should be nil when not set")
	}
}

func TestILPWriter_Options_WithFallback(t *testing.T) {
	fb := func(batch *models.MetricBatch, tenant string) error { return nil }
	w := testWriter(t, WithFallback(fb))

	if w.fallbackFn == nil {
		t.Error("fallbackFn should be set")
	}
}

func TestILPWriter_Options_InvalidValuesIgnored(t *testing.T) {
	w := testWriter(t,
		WithBufferSize(-1),
		WithFlushInterval(0),
	)

	if w.bufSize != 10000 { // testWriter default
		t.Errorf("bufSize = %d, want 10000 (default should be preserved)", w.bufSize)
	}
	if w.flushInterval != 1*time.Hour { // testWriter default
		t.Errorf("flushInterval = %v, want 1h (default should be preserved)", w.flushInterval)
	}
}

// ---- Batch Conversion Tests ----

func TestILPWriter_MetricBatchConversion_CPU(t *testing.T) {
	w := testWriter(t)

	batch := &models.MetricBatch{
		AgentID:   "agent-42",
		Timestamp: time.Now(),
		CPU: []models.CPUMetrics{
			{
				AgentID:         "agent-42",
				Timestamp:       time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
				TotalUsagePct:   75.5,
				PerCorePct:      []float64{50.0, 80.0, 90.0},
				LoadAvg1m:       2.5,
				LoadAvg5m:       1.8,
				LoadAvg15m:      1.2,
				FrequencyMHz:    3600.0,
				ContextSwitches: 123456,
				PhysicalCores:   4,
				LogicalCores:    8,
				ModelName:       "AMD Ryzen 7",
				VendorID:        "AuthenticAMD",
			},
		},
	}

	err := w.WriteMetricBatch(batch, "tenant-1")
	if err != nil {
		t.Fatalf("WriteMetricBatch error: %v", err)
	}

	m := w.Metrics()
	if m.RowsWritten != 1 {
		t.Errorf("RowsWritten = %d, want 1", m.RowsWritten)
	}
}

func TestILPWriter_MetricBatchConversion_AllTypes(t *testing.T) {
	w := testWriter(t)
	ts := time.Now()

	batch := &models.MetricBatch{
		AgentID:   "agent-all",
		Timestamp: ts,
		CPU: []models.CPUMetrics{
			{AgentID: "agent-all", Timestamp: ts, TotalUsagePct: 50.0},
		},
		Memory: []models.MemoryMetrics{
			{
				AgentID: "agent-all", Timestamp: ts,
				TotalBytes: 16384, UsedBytes: 8192, FreeBytes: 8192,
				AvailableBytes: 12288, UsagePercent: 50.0,
				Pressure: &models.MemoryPressure{
					Some10: 0.5, Some60: 0.3, Some300: 0.1,
					Full10: 0.2, Full60: 0.1, Full300: 0.05,
				},
			},
		},
		Disk: []models.DiskMetrics{
			{
				AgentID: "agent-all", Timestamp: ts,
				Device: "/dev/sda1", MountPoint: "/", FilesystemType: "ext4",
				TotalBytes: 1073741824, UsedBytes: 536870912, FreeBytes: 536870912,
				IsSSD: true, UtilizationPct: 50.0,
			},
		},
		Network: []models.NetworkMetrics{
			{
				AgentID: "agent-all", Timestamp: ts,
				Interface: "eth0", RxBytesPerSec: 1000, TxBytesPerSec: 500,
				TCPStats: &models.TCPStats{Established: 10, TimeWait: 2, Listen: 1, RetransmitCount: 3},
			},
		},
		Processes: []models.ProcessMetrics{
			{
				AgentID: "agent-all", Timestamp: ts, PID: 1234, Name: "nginx",
				CPUUsagePct: 5.0, MemoryBytes: 102400, Status: "running",
			},
		},
		Containers: []models.ContainerMetrics{
			{
				AgentID: "agent-all", Timestamp: ts,
				ContainerID: "abc123", Runtime: "docker", Name: "web",
				Image: "nginx:latest", Status: "running", CgroupVersion: "v2",
				MemoryLimitBytes: 536870912, CPUQuota: 2.0, CPUShares: 1024,
			},
		},
	}

	err := w.WriteMetricBatch(batch, "tenant-all")
	if err != nil {
		t.Fatalf("WriteMetricBatch error: %v", err)
	}

	m := w.Metrics()
	// 1 CPU + 1 Memory + 1 Disk + 1 Network + 1 Process + 1 Container = 6
	wantRows := int64(6)
	if m.RowsWritten != wantRows {
		t.Errorf("RowsWritten = %d, want %d", m.RowsWritten, wantRows)
	}
}

func TestILPWriter_MetricBatchConversion_EmptyBatch(t *testing.T) {
	w := testWriter(t)

	batch := &models.MetricBatch{
		AgentID:   "agent-empty",
		Timestamp: time.Now(),
		// All slices are nil.
	}

	err := w.WriteMetricBatch(batch, "tenant-1")
	if err != nil {
		t.Fatalf("WriteMetricBatch with empty batch: %v", err)
	}

	m := w.Metrics()
	if m.RowsWritten != 0 {
		t.Errorf("RowsWritten = %d, want 0", m.RowsWritten)
	}
}

// ---- float64MapToAny Tests ----

func TestFloat64MapToAny(t *testing.T) {
	t.Run("normal map", func(t *testing.T) {
		input := map[string]float64{"a": 1.0, "b": 2.5}
		result := float64MapToAny(input)

		if len(result) != 2 {
			t.Fatalf("len = %d, want 2", len(result))
		}
		if result["a"] != 1.0 {
			t.Errorf("a = %v, want 1.0", result["a"])
		}
		if result["b"] != 2.5 {
			t.Errorf("b = %v, want 2.5", result["b"])
		}
	})

	t.Run("nil map", func(t *testing.T) {
		result := float64MapToAny(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		result := float64MapToAny(map[string]float64{})
		if len(result) != 0 {
			t.Errorf("expected empty map, got %v", result)
		}
	})
}

// ---- ILP Tag/Field Conversion Tests ----

func TestCpuILPTags(t *testing.T) {
	tags := cpuILPTags("agent-1", "tenant-a")
	if tags["agent_id"] != "agent-1" {
		t.Errorf("agent_id = %q, want %q", tags["agent_id"], "agent-1")
	}
	if tags["tenant_id"] != "tenant-a" {
		t.Errorf("tenant_id = %q, want %q", tags["tenant_id"], "tenant-a")
	}
}

func TestCpuILPFields(t *testing.T) {
	cpu := models.CPUMetrics{
		TotalUsagePct:   75.5,
		PerCorePct:      []float64{50.0, 80.0},
		LoadAvg1m:       2.5,
		LoadAvg5m:       1.8,
		LoadAvg15m:      1.2,
		FrequencyMHz:    3600.0,
		ContextSwitches: 123456,
		PhysicalCores:   4,
		LogicalCores:    8,
		ModelName:       "TestCPU",
		VendorID:        "TestVendor",
	}

	fields := cpuILPFields(cpu)

	if fields["total_usage_pct"] != 75.5 {
		t.Errorf("total_usage_pct = %v, want 75.5", fields["total_usage_pct"])
	}
	if fields["context_switches"] != uint64(123456) {
		t.Errorf("context_switches = %v, want 123456", fields["context_switches"])
	}
	if fields["physical_cores"] != int32(4) {
		t.Errorf("physical_cores = %v, want 4", fields["physical_cores"])
	}
	if fields["model_name"] != "TestCPU" {
		t.Errorf("model_name = %v, want TestCPU", fields["model_name"])
	}
	// per_core_pct should be formatted as a string.
	expected := "[50.0000,80.0000]"
	if fields["per_core_pct"] != expected {
		t.Errorf("per_core_pct = %q, want %q", fields["per_core_pct"], expected)
	}
}

func TestMemILPFields_NilPressure(t *testing.T) {
	mem := models.MemoryMetrics{
		TotalBytes: 1024,
		UsedBytes:  512,
	}

	fields := memILPFields(mem)

	if fields["total_bytes"] != uint64(1024) {
		t.Errorf("total_bytes = %v, want 1024", fields["total_bytes"])
	}
	// Pressure fields should default to 0.
	if fields["pressure_some_avg10"] != float64(0) {
		t.Errorf("pressure_some_avg10 = %v, want 0", fields["pressure_some_avg10"])
	}
}

func TestNetILPFields_NilTCPStats(t *testing.T) {
	net := models.NetworkMetrics{
		RxBytesPerSec: 1000,
	}

	fields := netILPFields(net)

	if fields["rx_bytes_per_sec"] != uint64(1000) {
		t.Errorf("rx_bytes_per_sec = %v, want 1000", fields["rx_bytes_per_sec"])
	}
	// TCP stats should default to 0.
	if fields["tcp_established"] != int32(0) {
		t.Errorf("tcp_established = %v, want 0", fields["tcp_established"])
	}
}

func TestDiskILPTags_WithDevice(t *testing.T) {
	disk := models.DiskMetrics{
		AgentID:        "a1",
		Device:         "/dev/sda1",
		MountPoint:     "/",
		FilesystemType: "ext4",
	}

	tags := diskILPTags(disk, "tenant-1")

	if tags["device"] != "/dev/sda1" {
		t.Errorf("device = %q, want %q", tags["device"], "/dev/sda1")
	}
	if tags["mount_point"] != "/" {
		t.Errorf("mount_point = %q, want %q", tags["mount_point"], "/")
	}
	if tags["filesystem_type"] != "ext4" {
		t.Errorf("filesystem_type = %q, want %q", tags["filesystem_type"], "ext4")
	}
}

func TestDiskILPTags_EmptyDevice(t *testing.T) {
	disk := models.DiskMetrics{AgentID: "a1"}
	tags := diskILPTags(disk, "t")

	// Empty string fields should NOT be in tags.
	if _, ok := tags["device"]; ok {
		t.Error("empty device should not be in tags")
	}
}

func TestContainerILPTags(t *testing.T) {
	ctr := models.ContainerMetrics{
		AgentID:       "a1",
		ContainerID:   "abc123",
		Runtime:       "docker",
		Name:          "web",
		Image:         "nginx:latest",
		Status:        "running",
		CgroupVersion: "v2",
	}

	tags := containerILPTags(ctr, "t1")

	if tags["container_id"] != "abc123" {
		t.Errorf("container_id = %q", tags["container_id"])
	}
	if tags["runtime"] != "docker" {
		t.Errorf("runtime = %q", tags["runtime"])
	}
	if tags["name"] != "web" {
		t.Errorf("name = %q", tags["name"])
	}
	if tags["image"] != "nginx:latest" {
		t.Errorf("image = %q", tags["image"])
	}
}

func TestProcILPTags(t *testing.T) {
	proc := models.ProcessMetrics{
		AgentID:     "a1",
		Name:        "nginx",
		Status:      "running",
		ContainerID: "ctr-1",
	}

	tags := procILPTags(proc, "t1")

	if tags["name"] != "nginx" {
		t.Errorf("name = %q", tags["name"])
	}
	if tags["status"] != "running" {
		t.Errorf("status = %q", tags["status"])
	}
	if tags["container_id"] != "ctr-1" {
		t.Errorf("container_id = %q", tags["container_id"])
	}
}
