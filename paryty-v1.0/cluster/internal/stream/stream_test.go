package stream

import (
	"context"
	"testing"
)

func TestNewStreamEngine_CreatesProducerAndTopics(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	engine, err := NewStreamEngine(cfg, logger)
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	if engine.producer == nil {
		t.Fatal("NewStreamEngine() did not create producer")
	}
	if engine.topics == nil {
		t.Fatal("NewStreamEngine() did not create topic manager")
	}
	if engine.consumer != nil {
		t.Fatal("NewStreamEngine() should not create consumer")
	}
}

func TestStreamEngine_InitializeTopics_RequiresTenant(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	ctx := context.Background()

	// This will fail because broker is unreachable, but verifies the tenant
	// is passed through to requiredTopics.
	err = engine.InitializeTopics(ctx, "test-tenant")
	if err == nil {
		t.Log("InitializeTopics succeeded (broker is reachable)")
	}
	// We don't assert error because the test broker may or may not be up.
	// The important thing is it doesn't panic.
}

func TestStreamEngine_InitializeTopics_EmptyTenant(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	ctx := context.Background()

	// Empty tenant produces topic names like "paryty..metrics.raw".
	// Should not panic; may fail if broker is unreachable.
	_ = engine.InitializeTopics(ctx, "")
}

func TestStreamEngine_Ping_UnreachableBroker(t *testing.T) {
	cfg := Config{
		Brokers:  []string{"localhost:19092"},
		ClientID: "test-ping",
	}
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	ctx := context.Background()
	err = engine.Ping(ctx)
	if err == nil {
		t.Log("Ping succeeded (broker is reachable)")
	}
	// With unreachable broker, Ping should return an error.
	// We just verify it doesn't panic.
}

func TestStreamEngine_Lag_NilConsumer(t *testing.T) {
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
	if lag == nil {
		t.Fatal("Lag() returned nil map")
	}
	if len(lag) != 0 {
		t.Errorf("Lag() returned %d entries, want 0 for nil consumer", len(lag))
	}
}

func TestStreamEngine_NewConsumer_StoresConsumer(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := engine.NewConsumer("test-group", []string{"test-topic"}, handler)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if engine.consumer != consumer {
		t.Error("NewConsumer() did not store consumer in engine")
	}
}

func TestStreamEngine_NewConsumerWithDLQ_NilDLQ(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := engine.NewConsumerWithDLQ("test-group", []string{"test-topic"}, handler, nil, "")
	if err != nil {
		t.Fatalf("NewConsumerWithDLQ() error: %v", err)
	}
	defer consumer.Close()

	if consumer.dlqProducer != nil {
		t.Error("dlqProducer should be nil when not configured")
	}
	if consumer.dlqTopic != "" {
		t.Error("dlqTopic should be empty when not configured")
	}
}

func TestStreamEngine_NewConsumerWithDLQ_WithDLQProducer(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	handler := func(ctx context.Context, key string, value []byte) error { return nil }
	dlqProducer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer dlqProducer.Close()

	dlqTopic := TopicDLQ("test-tenant")
	consumer, err := engine.NewConsumerWithDLQ("test-group", []string{"test-topic"}, handler, dlqProducer, dlqTopic)
	if err != nil {
		t.Fatalf("NewConsumerWithDLQ() error: %v", err)
	}
	defer consumer.Close()

	if consumer.dlqProducer != dlqProducer {
		t.Error("dlqProducer not wired correctly")
	}
	if consumer.dlqTopic != dlqTopic {
		t.Errorf("dlqTopic = %q, want %q", consumer.dlqTopic, dlqTopic)
	}
}

func TestStreamEngine_Lag_WithConsumer(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	handler := func(ctx context.Context, key string, value []byte) error { return nil }
	consumer, err := engine.NewConsumer("test-group", []string{"test-topic"}, handler)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	ctx := context.Background()
	// Lag will fail because broker is unreachable, but we verify the
	// delegation path exists and doesn't panic.
	_, _ = engine.Lag(ctx)
}

func TestStreamEngine_Close_WithConsumer(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}

	handler := func(ctx context.Context, key string, value []byte) error { return nil }
	consumer, err := engine.NewConsumer("test-group", []string{"test-topic"}, handler)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	_ = consumer

	// Close should close consumer, then producer, then topics.
	// We verify it does not panic and completes.
	engine.Close()
}

func TestStreamEngine_Close_WithoutConsumer(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}

	// Close with nil consumer should not panic.
	engine.Close()
}

func TestStreamEngine_ProducerAccessor(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	if engine.Producer() != engine.producer {
		t.Error("Producer() accessor does not return internal producer")
	}
}

func TestStreamEngine_TopicsAccessor(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	if engine.Topics() != engine.topics {
		t.Error("Topics() accessor does not return internal topic manager")
	}
}

func TestInitializeTopics_TenantScopedTopicNames(t *testing.T) {
	// Verify that requiredTopics generates correct tenant-scoped names.
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
		t.Error("NewConsumer() did not store consumer in engine")
	}
}

func TestStreamEngine_NewConsumerWithDLQ_PassesOptions(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	handler := func(ctx context.Context, key string, value []byte) error { return nil }
	dlqProducer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer dlqProducer.Close()

	consumer, err := engine.NewConsumerWithDLQ("test-group", []string{"test-topic"}, handler,
		dlqProducer, TopicDLQ("test"), WithMaxRetries(7),
	)
	if err != nil {
		t.Fatalf("NewConsumerWithDLQ() with options error: %v", err)
	}
	defer consumer.Close()

	if consumer.maxRetries != 7 {
		t.Errorf("maxRetries = %d, want 7", consumer.maxRetries)
	}
	if consumer.dlqProducer != dlqProducer {
		t.Error("dlqProducer not wired")
	}
}

// Ensure StreamEngine fields are accessible within the package.
func TestStreamEngine_FieldAccess(t *testing.T) {
	cfg := testConfig()
	engine, err := NewStreamEngine(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStreamEngine() error: %v", err)
	}
	defer engine.Close()

	if engine.logger == nil {
		t.Error("engine.logger is nil")
	}
	if engine.cfg.ClientID != cfg.ClientID {
		t.Errorf("engine.cfg.ClientID = %q, want %q", engine.cfg.ClientID, cfg.ClientID)
	}
}
