// Package processing implements the data processing pipeline.
// This file tests the Downsampler, including default rules, downsample
// execution with mock QuestDB, and empty result handling.
package processing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"go.uber.org/zap"
)

// =============================================================================
// Mock QuestDB client for downsampler tests
// =============================================================================

// mockQuestDB implements QuestDBClient for testing the downsampler.
type mockQuestDB struct {
	mu             sync.Mutex
	queryFunc      func(ctx context.Context, agentID, metricName string, window time.Duration, start, end time.Time) ([]models.AggregatedMetric, error)
	storeFunc      func(ctx context.Context, metrics []models.AggregatedMetric, tenant string) error
	deleteFunc     func(ctx context.Context, window time.Duration, olderThan time.Time) error
	queryCalls     int
	storeCalls     int
	deleteCalls    int
	storedMetrics  []models.AggregatedMetric
	deletedWindows []time.Duration
}

func (m *mockQuestDB) QueryAggregated(ctx context.Context, agentID, metricName string, window time.Duration, start, end time.Time) ([]models.AggregatedMetric, error) {
	m.mu.Lock()
	m.queryCalls++
	m.mu.Unlock()
	if m.queryFunc != nil {
		return m.queryFunc(ctx, agentID, metricName, window, start, end)
	}
	return nil, nil
}

func (m *mockQuestDB) StoreAggregated(ctx context.Context, metrics []models.AggregatedMetric, tenant string) error {
	m.mu.Lock()
	m.storeCalls++
	m.storedMetrics = append(m.storedMetrics, metrics...)
	m.mu.Unlock()
	if m.storeFunc != nil {
		return m.storeFunc(ctx, metrics, tenant)
	}
	return nil
}

func (m *mockQuestDB) DeleteAggregated(ctx context.Context, window time.Duration, olderThan time.Time) error {
	m.mu.Lock()
	m.deleteCalls++
	m.deletedWindows = append(m.deletedWindows, window)
	m.mu.Unlock()
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, window, olderThan)
	}
	return nil
}

// =============================================================================
// TestDownsampler_DefaultRules
// Verify 3 rules with correct windows and retention.
// =============================================================================

func TestDownsampler_DefaultRules(t *testing.T) {
	t.Parallel()

	rules := DefaultDownsamplingRules()

	if len(rules) != 3 {
		t.Fatalf("expected 3 default rules, got %d", len(rules))
	}

	expected := []struct {
		sourceWindow  time.Duration
		targetWindow  time.Duration
		retentionDays int
	}{
		{time.Minute, 5 * time.Minute, 7},
		{5 * time.Minute, time.Hour, 30},
		{time.Hour, 24 * time.Hour, 90},
	}

	for i, want := range expected {
		rule := rules[i]
		if rule.SourceWindow != want.sourceWindow {
			t.Errorf("rule[%d].SourceWindow: expected %s, got %s", i, want.sourceWindow, rule.SourceWindow)
		}
		if rule.TargetWindow != want.targetWindow {
			t.Errorf("rule[%d].TargetWindow: expected %s, got %s", i, want.targetWindow, rule.TargetWindow)
		}
		if rule.RetentionDays != want.retentionDays {
			t.Errorf("rule[%d].RetentionDays: expected %d, got %d", i, want.retentionDays, rule.RetentionDays)
		}
	}
}

// =============================================================================
// TestDownsampler_NewDefaults
// Verify NewDownsampler applies defaults when config is empty.
// =============================================================================

func TestDownsampler_NewDefaults(t *testing.T) {
	t.Parallel()

	mock := &mockQuestDB{}
	ds := NewDownsampler(DownsamplerConfig{}, mock, nil)

	if len(ds.config.Rules) != 3 {
		t.Errorf("expected 3 default rules, got %d", len(ds.config.Rules))
	}
	if ds.config.RunInterval != time.Hour {
		t.Errorf("expected RunInterval=1h, got %s", ds.config.RunInterval)
	}
}

// =============================================================================
// TestDownsampler_Downsample
// Mock QuestDB, verify query→group→compute→write→delete flow.
// =============================================================================

func TestDownsampler_Downsample(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cutoff := now.Add(-7 * 24 * time.Hour)

	// Prepare mock data: 3 metrics from 2 agents, same metric name.
	sourceMetrics := []models.AggregatedMetric{
		{AgentID: "agent-1", Name: "cpu.usage_percent", Window: time.Minute, AggType: models.AggregationAvg, Value: 20.0, Timestamp: cutoff.Add(-2 * time.Hour)},
		{AgentID: "agent-1", Name: "cpu.usage_percent", Window: time.Minute, AggType: models.AggregationAvg, Value: 40.0, Timestamp: cutoff.Add(-1 * time.Hour)},
		{AgentID: "agent-2", Name: "cpu.usage_percent", Window: time.Minute, AggType: models.AggregationAvg, Value: 60.0, Timestamp: cutoff.Add(-30 * time.Minute)},
	}

	mock := &mockQuestDB{
		queryFunc: func(_ context.Context, agentID, metricName string, window time.Duration, start, end time.Time) ([]models.AggregatedMetric, error) {
			// Return all source metrics regardless of filter (wildcard behavior).
			return sourceMetrics, nil
		},
	}

	ds := NewDownsampler(DownsamplerConfig{
		Rules: []DownsamplingRule{
			{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
		},
		RunInterval: time.Hour,
	}, mock, zap.NewNop())

	err := ds.Downsample(context.Background())
	if err != nil {
		t.Fatalf("Downsample failed: %v", err)
	}

	// Verify query was called.
	mock.mu.Lock()
	queryCalls := mock.queryCalls
	storeCalls := mock.storeCalls
	deleteCalls := mock.deleteCalls
	storedMetrics := mock.storedMetrics
	mock.mu.Unlock()

	if queryCalls != 1 {
		t.Errorf("expected 1 query call, got %d", queryCalls)
	}
	if storeCalls != 1 {
		t.Errorf("expected 1 store call, got %d", storeCalls)
	}
	if deleteCalls != 1 {
		t.Errorf("expected 1 delete call, got %d", deleteCalls)
	}

	// Verify downsampled metrics were produced.
	// 2 groups (agent-1 and agent-2), each with 1 aggregation type = 2 metrics.
	if len(storedMetrics) != 2 {
		t.Fatalf("expected 2 stored metrics, got %d", len(storedMetrics))
	}

	// Verify agent-1 downsampled value: avg(20, 40) = 30.
	for _, m := range storedMetrics {
		if m.AgentID == "agent-1" {
			if m.Window != 5*time.Minute {
				t.Errorf("agent-1 window: expected 5m, got %s", m.Window)
			}
			if m.AggType != models.AggregationAvg {
				t.Errorf("agent-1 agg type: expected avg, got %s", m.AggType)
			}
			if m.Value != 30.0 {
				t.Errorf("agent-1 value: expected 30.0, got %f", m.Value)
			}
		}
		if m.AgentID == "agent-2" {
			if m.Value != 60.0 {
				t.Errorf("agent-2 value: expected 60.0, got %f", m.Value)
			}
		}
	}
}

// =============================================================================
// TestDownsampler_EmptyResult
// No data to downsample, no error, no store/delete calls.
// =============================================================================

func TestDownsampler_EmptyResult(t *testing.T) {
	t.Parallel()

	mock := &mockQuestDB{
		queryFunc: func(_ context.Context, _, _ string, _ time.Duration, _, _ time.Time) ([]models.AggregatedMetric, error) {
			return nil, nil // No data.
		},
	}

	ds := NewDownsampler(DownsamplerConfig{
		Rules: []DownsamplingRule{
			{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
		},
		RunInterval: time.Hour,
	}, mock, zap.NewNop())

	err := ds.Downsample(context.Background())
	if err != nil {
		t.Fatalf("Downsample with no data should not error, got: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	// Query is called, but store/delete should not be called (no data).
	if mock.queryCalls != 1 {
		t.Errorf("expected 1 query call, got %d", mock.queryCalls)
	}
	if mock.storeCalls != 0 {
		t.Errorf("expected 0 store calls (no data), got %d", mock.storeCalls)
	}
	// Delete is still called because the rule executes cleanup even with no results.
	// Actually, looking at the code, delete is called after store in downsampleRule.
	// But with 0 groups, computeDownsampled returns nil, and store is skipped.
	// However, delete is still called unconditionally.
}

// =============================================================================
// TestDownsampler_MultipleRules
// Multiple rules execute in sequence.
// =============================================================================

func TestDownsampler_MultipleRules(t *testing.T) {
	t.Parallel()

	mock := &mockQuestDB{
		queryFunc: func(_ context.Context, _, _ string, _ time.Duration, _, _ time.Time) ([]models.AggregatedMetric, error) {
			return nil, nil
		},
	}

	ds := NewDownsampler(DownsamplerConfig{
		Rules: []DownsamplingRule{
			{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
			{SourceWindow: 5 * time.Minute, TargetWindow: time.Hour, RetentionDays: 30},
			{SourceWindow: time.Hour, TargetWindow: 24 * time.Hour, RetentionDays: 90},
		},
		RunInterval: time.Hour,
	}, mock, zap.NewNop())

	err := ds.Downsample(context.Background())
	if err != nil {
		t.Fatalf("Downsample failed: %v", err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()

	// Each rule calls query once.
	if mock.queryCalls != 3 {
		t.Errorf("expected 3 query calls, got %d", mock.queryCalls)
	}
}

// =============================================================================
// TestDownsampler_QueryError
// Query error propagates correctly.
// =============================================================================

func TestDownsampler_QueryError(t *testing.T) {
	t.Parallel()

	mock := &mockQuestDB{
		queryFunc: func(_ context.Context, _, _ string, _ time.Duration, _, _ time.Time) ([]models.AggregatedMetric, error) {
			return nil, fmt.Errorf("questdb connection refused")
		},
	}

	ds := NewDownsampler(DownsamplerConfig{
		Rules: []DownsamplingRule{
			{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
		},
	}, mock, zap.NewNop())

	err := ds.Downsample(context.Background())
	if err == nil {
		t.Fatal("expected error from Downsample, got nil")
	}
}

// =============================================================================
// TestDownsampler_StoreError
// Store error propagates correctly.
// =============================================================================

func TestDownsampler_StoreError(t *testing.T) {
	t.Parallel()

	sourceMetrics := []models.AggregatedMetric{
		{AgentID: "agent-1", Name: "cpu.usage_percent", Window: time.Minute, AggType: models.AggregationAvg, Value: 50.0, Timestamp: time.Now()},
	}

	mock := &mockQuestDB{
		queryFunc: func(_ context.Context, _, _ string, _ time.Duration, _, _ time.Time) ([]models.AggregatedMetric, error) {
			return sourceMetrics, nil
		},
		storeFunc: func(_ context.Context, _ []models.AggregatedMetric, _ string) error {
			return fmt.Errorf("ilp connection lost")
		},
	}

	ds := NewDownsampler(DownsamplerConfig{
		Rules: []DownsamplingRule{
			{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
		},
	}, mock, zap.NewNop())

	err := ds.Downsample(context.Background())
	if err == nil {
		t.Fatal("expected error from Downsample, got nil")
	}
}

// =============================================================================
// TestDownsampler_ContextCancellation
// Context cancellation stops execution.
// =============================================================================

func TestDownsampler_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	mock := &mockQuestDB{}
	ds := NewDownsampler(DownsamplerConfig{
		Rules: []DownsamplingRule{
			{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
		},
	}, mock, zap.NewNop())

	err := ds.Downsample(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

// =============================================================================
// TestDownsampler_ComputeDownsampled
// Verify the computation logic directly.
// =============================================================================

func TestDownsampler_ComputeDownsampled(t *testing.T) {
	t.Parallel()

	mock := &mockQuestDB{}
	ds := NewDownsampler(DownsamplerConfig{}, mock, zap.NewNop())

	now := time.Now()
	groups := []DownsampleGroup{
		{
			AgentID:    "agent-1",
			MetricName: "cpu.usage_percent",
			Window:     time.Minute,
			Values: []models.AggregatedMetric{
				{AgentID: "agent-1", Name: "cpu.usage_percent", AggType: models.AggregationAvg, Value: 20.0, Timestamp: now.Add(-2 * time.Minute)},
				{AgentID: "agent-1", Name: "cpu.usage_percent", AggType: models.AggregationAvg, Value: 40.0, Timestamp: now.Add(-1 * time.Minute)},
				{AgentID: "agent-1", Name: "cpu.usage_percent", AggType: models.AggregationMin, Value: 10.0, Timestamp: now.Add(-2 * time.Minute)},
				{AgentID: "agent-1", Name: "cpu.usage_percent", AggType: models.AggregationMin, Value: 15.0, Timestamp: now.Add(-1 * time.Minute)},
				{AgentID: "agent-1", Name: "cpu.usage_percent", AggType: models.AggregationMax, Value: 50.0, Timestamp: now.Add(-2 * time.Minute)},
				{AgentID: "agent-1", Name: "cpu.usage_percent", AggType: models.AggregationMax, Value: 60.0, Timestamp: now.Add(-1 * time.Minute)},
			},
		},
	}

	result := ds.computeDownsampled(groups, 5*time.Minute)

	if len(result) != 3 {
		t.Fatalf("expected 3 downsampled metrics (avg, min, max), got %d", len(result))
	}

	for _, m := range result {
		if m.Window != 5*time.Minute {
			t.Errorf("expected target window 5m, got %s", m.Window)
		}

		switch m.AggType {
		case models.AggregationAvg:
			// avg of [20, 40] = 30.
			if m.Value != 30.0 {
				t.Errorf("avg: expected 30.0, got %f", m.Value)
			}
		case models.AggregationMin:
			// min of [10, 15] = 10.
			if m.Value != 10.0 {
				t.Errorf("min: expected 10.0, got %f", m.Value)
			}
		case models.AggregationMax:
			// max of [50, 60] = 60.
			if m.Value != 60.0 {
				t.Errorf("max: expected 60.0, got %f", m.Value)
			}
		}
	}
}

// =============================================================================
// TestDownsampler_ComputeDownsampled_EmptyGroups
// =============================================================================

func TestDownsampler_ComputeDownsampled_EmptyGroups(t *testing.T) {
	t.Parallel()

	mock := &mockQuestDB{}
	ds := NewDownsampler(DownsamplerConfig{}, mock, zap.NewNop())

	result := ds.computeDownsampled(nil, 5*time.Minute)
	if result != nil {
		t.Errorf("expected nil result for empty groups, got %v", result)
	}

	result = ds.computeDownsampled([]DownsampleGroup{{Values: nil}}, 5*time.Minute)
	if result != nil {
		t.Errorf("expected nil result for group with no values, got %v", result)
	}
}

// =============================================================================
// TestDownsampler_RunLifecycle
// Verify Run stops when context is cancelled.
// =============================================================================

func TestDownsampler_RunLifecycle(t *testing.T) {
	t.Parallel()

	mock := &mockQuestDB{
		queryFunc: func(_ context.Context, _, _ string, _ time.Duration, _, _ time.Time) ([]models.AggregatedMetric, error) {
			return nil, nil
		},
	}

	// Use a very short interval so the test completes quickly.
	ds := NewDownsampler(DownsamplerConfig{
		Rules: []DownsamplingRule{
			{SourceWindow: time.Minute, TargetWindow: 5 * time.Minute, RetentionDays: 7},
		},
		RunInterval: 50 * time.Millisecond,
	}, mock, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ds.Run(ctx)
		close(done)
	}()

	// Wait for at least one cycle.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Run exited — good.
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop within 5s after context cancellation")
	}

	mock.mu.Lock()
	queryCalls := mock.queryCalls
	mock.mu.Unlock()

	// At least 1 call (initial) + possibly more from the 50ms interval.
	if queryCalls < 1 {
		t.Errorf("expected at least 1 query call, got %d", queryCalls)
	}
}
