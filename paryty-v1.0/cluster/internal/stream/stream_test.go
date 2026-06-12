package stream

import (
	"context"
	"net"
	"testing"
	"time"
)

// requireBroker skips the test when no live Redpanda broker is reachable.
// Broker round-trip tests are environment-dependent: they exercise real
// topic creation/deletion and belong to verification Gate 4 (integration,
// infrastructure running), not Gate 3 (unit, no infrastructure). Without
// this guard `go test ./...` fails on machines where Redpanda is down.
func requireBroker(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping broker-dependent test in short mode")
	}
	broker := testConfig().Brokers[0]
	conn, err := net.DialTimeout("tcp", broker, 2*time.Second)
	if err != nil {
		t.Skipf("Skipping: Redpanda broker %s not reachable (%v) — run with infrastructure for Gate 4", broker, err)
	}
	_ = conn.Close()
}

func TestNewTopicManager(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	tm, err := NewTopicManager(cfg, logger)
	if err != nil {
		t.Fatalf("NewTopicManager() error: %v", err)
	}
	defer tm.Close()
}

func TestTopicForTenant(t *testing.T) {
	tests := []struct {
		tenant string
		topic  string
		want   string
	}{
		{"acme", "metrics.raw", "paryty.acme.metrics.raw"},
		{"default", "traces", "paryty.default.traces"},
		{"", "events", "paryty..events"},
	}

	for _, tt := range tests {
		got := TopicForTenant(tt.tenant, tt.topic)
		if got != tt.want {
			t.Errorf("TopicForTenant(%q, %q) = %q, want %q", tt.tenant, tt.topic, got, tt.want)
		}
	}
}

func TestTenantScopedTopicNames(t *testing.T) {
	tenant := "acme"

	tests := []struct {
		fn   func(string) string
		want string
	}{
		{TopicMetricsRaw, "paryty.acme.metrics.raw"},
		{TopicMetricsAgg, "paryty.acme.metrics.aggregated"},
		{TopicTraces, "paryty.acme.traces"},
		{TopicEvents, "paryty.acme.events"},
		{TopicNetworkEvents, "paryty.acme.network.events"},
		{TopicTopologyChanges, "paryty.acme.topology.changes"},
		{TopicAlerts, "paryty.acme.alerts"},
		{TopicDLQ, "paryty.acme.dead-letter"},
		{TopicForecasts, "paryty.acme.forecasts"},
		{TopicAnomalies, "paryty.acme.anomalies"},
		{TopicSimulations, "paryty.acme.simulations"},
		{TopicTimelineEvents, "paryty.acme.timeline.events"},
	}

	for _, tt := range tests {
		got := tt.fn(tenant)
		if got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestDefaultTenantTopicNames(t *testing.T) {
	tests := []struct {
		fn   func() string
		want string
	}{
		{DefaultTopicMetricsRaw, "paryty.default.metrics.raw"},
		{DefaultTopicTraces, "paryty.default.traces"},
		{DefaultTopicForecasts, "paryty.default.forecasts"},
		{DefaultTopicAnomalies, "paryty.default.anomalies"},
		{DefaultTopicSimulations, "paryty.default.simulations"},
		{DefaultTopicTimelineEvents, "paryty.default.timeline.events"},
	}

	for _, tt := range tests {
		got := tt.fn()
		if got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestNewStreamEngine(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	engine, err := NewStreamEngine(cfg, logger)
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	if engine.Producer() == nil {
		t.Error("Producer() returned nil")
	}

	if engine.Topics() == nil {
		t.Error("Topics() returned nil")
	}
}

func TestNewStreamEngine_InvalidBroker(t *testing.T) {
	cfg := Config{
		Brokers:  []string{"invalid:99999"},
		ClientID: "test",
	}
	logger := testLogger()

	_, err := NewStreamEngine(cfg, logger)
	if err != nil {
		t.Logf("Expected behavior: NewStreamEngine with invalid broker: %v", err)
	}
}

func TestTopicManager_CreateAndDelete(t *testing.T) {
	requireBroker(t)

	cfg := testConfig()
	logger := testLogger()

	tm, err := NewTopicManager(cfg, logger)
	if err != nil {
		t.Fatalf("NewTopicManager() error: %v", err)
	}
	defer tm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	topicName := "paryty.test.integration.create-delete"

	err = tm.CreateTopic(ctx, topicName, 1, 1)
	if err != nil {
		t.Fatalf("CreateTopic() error: %v", err)
	}

	topics, err := tm.ListTopics(ctx)
	if err != nil {
		t.Fatalf("ListTopics() error: %v", err)
	}

	found := false
	for _, name := range topics {
		if name == topicName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Topic %s not found after creation", topicName)
	}

	err = tm.DeleteTopic(ctx, topicName)
	if err != nil {
		t.Fatalf("DeleteTopic() error: %v", err)
	}
}

func TestTopicManager_EnsureTopic(t *testing.T) {
	requireBroker(t)

	cfg := testConfig()
	logger := testLogger()

	tm, err := NewTopicManager(cfg, logger)
	if err != nil {
		t.Fatalf("NewTopicManager() error: %v", err)
	}
	defer tm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	topicName := "paryty.test.integration.ensure"

	err = tm.EnsureTopic(ctx, topicName, 1, 1)
	if err != nil {
		t.Fatalf("EnsureTopic() first call error: %v", err)
	}

	err = tm.EnsureTopic(ctx, topicName, 1, 1)
	if err != nil {
		t.Fatalf("EnsureTopic() second call error: %v", err)
	}

	tm.DeleteTopic(ctx, topicName) //nolint:errcheck
}

func TestStreamEngine_Producer(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	engine, err := NewStreamEngine(cfg, logger)
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	producer := engine.Producer()
	if producer == nil {
		t.Fatal("Producer() returned nil")
	}
}

func TestTopicManager_Close(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	tm, err := NewTopicManager(cfg, logger)
	if err != nil {
		t.Fatalf("NewTopicManager() error: %v", err)
	}
	tm.Close()
}

func TestStreamEngine_Close(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	engine, err := NewStreamEngine(cfg, logger)
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	engine.Close()
}

func TestStreamEngine_TopicsAccessor(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	engine, err := NewStreamEngine(cfg, logger)
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	if engine.Topics() != engine.topics {
		t.Error("Topics() accessor does not return internal topic manager")
	}
}

func TestInitializeTopics_TenantScopedTopicNames(t *testing.T) {
	topics := requiredTopics("acme")

	expected := map[string]bool{
		"paryty.acme.metrics.raw":        false,
		"paryty.acme.metrics.aggregated": false,
		"paryty.acme.traces":             false,
		"paryty.acme.events":             false,
		"paryty.acme.network.events":     false,
		"paryty.acme.topology.changes":   false,
		"paryty.acme.alerts":             false,
		"paryty.acme.dead-letter":        false,
		"paryty.acme.metrics.enriched":   false,
		"paryty.acme.correlations":       false,
		"paryty.acme.dependency.graph":   false,
		// Phase 6: Intelligence topics.
		"paryty.acme.forecasts":       false,
		"paryty.acme.anomalies":       false,
		"paryty.acme.simulations":     false,
		"paryty.acme.timeline.events": false,
	}

	for _, tc := range topics {
		if _, ok := expected[tc.name]; !ok {
			t.Errorf("unexpected topic: %s", tc.name)
		}
		expected[tc.name] = true
	}

	for name, seen := range expected {
		if !seen {
			t.Errorf("missing expected topic: %s", name)
		}
	}
}

func TestStreamEngine_NewConsumer_PassesOptions(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := engine.NewConsumer("test-group", []string{"test-topic"}, handler,
		WithMaxRetries(10),
	)
	if err != nil {
		t.Fatalf("NewConsumer() with options error: %v", err)
	}
	defer consumer.Close()

	if consumer.maxRetries != 10 {
		t.Errorf("maxRetries = %d, want 10 (option not forwarded)", consumer.maxRetries)
	}
	if engine.consumer != consumer {
		t.Error("engine.consumer does not match returned consumer")
	}
}

func TestStreamEngine_Ping_WithoutBroker(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = engine.Ping(ctx)
	if err == nil {
		t.Log("Ping succeeded (broker might be running)")
	} else {
		t.Logf("Ping failed as expected without broker: %v", err)
	}
}

func TestStreamEngine_Lag_NoConsumer(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	ctx := context.Background()
	lag, err := engine.Lag(ctx)
	if err != nil {
		t.Fatalf("Lag() error: %v", err)
	}

	if len(lag) != 0 {
		t.Errorf("Lag() returned %d entries, want 0 (no consumer)", len(lag))
	}
}
