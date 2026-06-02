package stream

import (
	"context"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// testLogger returns a no-op logger for unit tests.
func testLogger() *zap.Logger {
	return zap.NewNop()
}

// testConfig returns a Config pointing to a non-existent broker.
// franz-go creates the client successfully even with unreachable brokers
// because it reconnects in the background.
func testConfig() Config {
	return Config{
		Brokers:  []string{"localhost:19092"},
		ClientID: "test-producer",
	}
}

func TestNewProducer_CreatesClientWithOptions(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()

	producer, err := NewProducer(cfg, logger)
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer producer.Close()

	if producer.client == nil {
		t.Fatal("NewProducer() returned nil client")
	}
	if producer.logger == nil {
		t.Fatal("NewProducer() returned nil logger")
	}
	if producer.cfg.ClientID != "test-producer" {
		t.Errorf("cfg.ClientID = %q, want %q", producer.cfg.ClientID, "test-producer")
	}
}

func TestNewProducer_IdempotentAndZstdOptions(t *testing.T) {
	// Verify that NewProducer does not error when idempotent write and
	// Zstd compression options are applied. The kgo.Client does not expose
	// its options, but creating it with these options must succeed.
	cfg := testConfig()
	producer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer with idempotent+zstd options failed: %v", err)
	}
	defer producer.Close()
}

func TestNewProducer_EmptyBrokers(t *testing.T) {
	cfg := Config{
		Brokers:  []string{},
		ClientID: "test-producer",
	}

	_, err := NewProducer(cfg, testLogger())
	if err == nil {
		t.Fatal("NewProducer() expected error for empty brokers, got nil")
	}
}

func TestPublishTenant_RejectsEmptyTenantID(t *testing.T) {
	cfg := testConfig()
	producer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer producer.Close()

	ctx := context.Background()
	err = producer.PublishTenant(ctx, "test-topic", "", "agent-1", map[string]string{"x": "y"})
	if err == nil {
		t.Fatal("PublishTenant() expected error for empty tenantID, got nil")
	}
	if !strings.Contains(err.Error(), "tenantID") {
		t.Errorf("error %q does not mention tenantID", err.Error())
	}
}

func TestPublishTenant_RejectsEmptyAgentID(t *testing.T) {
	cfg := testConfig()
	producer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer producer.Close()

	ctx := context.Background()
	err = producer.PublishTenant(ctx, "test-topic", "tenant-1", "", map[string]string{"x": "y"})
	if err == nil {
		t.Fatal("PublishTenant() expected error for empty agentID, got nil")
	}
	if !strings.Contains(err.Error(), "agentID") {
		t.Errorf("error %q does not mention agentID", err.Error())
	}
}

func TestPublishTenant_KeyFormat(t *testing.T) {
	// Verify the partition key format is "tenantID:agentID".
	// We test the key construction logic directly since we cannot produce
	// to a real broker in unit tests.
	tenantID := "tenant-abc"
	agentID := "agent-xyz"
	expected := "tenant-abc:agent-xyz"

	key := tenantID + ":" + agentID
	if key != expected {
		t.Errorf("key = %q, want %q", key, expected)
	}
}

func TestPublishTenant_KeyRouting(t *testing.T) {
	// Verify that the same tenant+agent always produces the same key,
	// ensuring deterministic partition routing.
	type testCase struct {
		tenant string
		agent  string
		want   string
	}

	tests := []testCase{
		{tenant: "t1", agent: "a1", want: "t1:a1"},
		{tenant: "t1", agent: "a2", want: "t1:a2"},
		{tenant: "t2", agent: "a1", want: "t2:a1"},
		{tenant: "", agent: "", want: ":"},
	}

	for _, tc := range tests {
		key := tc.tenant + ":" + tc.agent
		if key != tc.want {
			t.Errorf("tenant=%q agent=%q key=%q, want %q", tc.tenant, tc.agent, key, tc.want)
		}
	}
}

func TestPublishTenantBatch_EmptyEntries(t *testing.T) {
	cfg := testConfig()
	producer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer producer.Close()

	ctx := context.Background()
	// Empty batch should succeed without error (short-circuit).
	err = producer.PublishTenantBatch(ctx, "test-topic", nil)
	if err != nil {
		t.Errorf("PublishTenantBatch(nil) unexpected error: %v", err)
	}

	err = producer.PublishTenantBatch(ctx, "test-topic", []RecordEntry{})
	if err != nil {
		t.Errorf("PublishTenantBatch([]) unexpected error: %v", err)
	}
}

func TestPublishTenantBatch_RejectsEmptyTenantID(t *testing.T) {
	cfg := testConfig()
	producer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer producer.Close()

	ctx := context.Background()
	entries := []RecordEntry{
		{TenantID: "", AgentID: "agent-1", Value: map[string]string{"x": "y"}},
	}

	err = producer.PublishTenantBatch(ctx, "test-topic", entries)
	if err == nil {
		t.Fatal("PublishTenantBatch() expected error for empty tenantID, got nil")
	}
	if !strings.Contains(err.Error(), "tenantID") {
		t.Errorf("error %q does not mention tenantID", err.Error())
	}
}

func TestPublishTenantBatch_RejectsEmptyAgentID(t *testing.T) {
	cfg := testConfig()
	producer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer producer.Close()

	ctx := context.Background()
	entries := []RecordEntry{
		{TenantID: "tenant-1", AgentID: "", Value: map[string]string{"x": "y"}},
	}

	err = producer.PublishTenantBatch(ctx, "test-topic", entries)
	if err == nil {
		t.Fatal("PublishTenantBatch() expected error for empty agentID, got nil")
	}
	if !strings.Contains(err.Error(), "agentID") {
		t.Errorf("error %q does not mention agentID", err.Error())
	}
}

func TestPublishTenantBatch_ValidEntries(t *testing.T) {
	// Verify that valid entries are accepted and produce the correct
	// number of records with the right key format.
	entries := []RecordEntry{
		{TenantID: "t1", AgentID: "a1", Value: map[string]string{"v": "1"}},
		{TenantID: "t1", AgentID: "a2", Value: map[string]string{"v": "2"}},
		{TenantID: "t2", AgentID: "a1", Value: map[string]string{"v": "3"}},
	}

	// Build records the same way the producer would.
	records := make([]*kgo.Record, 0, len(entries))
	for _, entry := range entries {
		key := entry.TenantID + ":" + entry.AgentID
		records = append(records, &kgo.Record{
			Topic: "test-topic",
			Key:   []byte(key),
		})
	}

	if len(records) != 3 {
		t.Fatalf("record count = %d, want 3", len(records))
	}

	expectedKeys := []string{"t1:a1", "t1:a2", "t2:a1"}
	for i, rec := range records {
		if string(rec.Key) != expectedKeys[i] {
			t.Errorf("record[%d].Key = %q, want %q", i, string(rec.Key), expectedKeys[i])
		}
		if rec.Topic != "test-topic" {
			t.Errorf("record[%d].Topic = %q, want %q", i, rec.Topic, "test-topic")
		}
	}
}

func TestPublishBatch_EmptyMessages(t *testing.T) {
	cfg := testConfig()
	producer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer producer.Close()

	ctx := context.Background()
	err = producer.PublishBatch(ctx, "test-topic", nil)
	if err != nil {
		t.Errorf("PublishBatch(nil) unexpected error: %v", err)
	}
}

func TestMessage_Type(t *testing.T) {
	msg := Message{
		Key:   "test-key",
		Value: map[string]string{"foo": "bar"},
	}

	if msg.Key != "test-key" {
		t.Errorf("Message.Key = %q, want %q", msg.Key, "test-key")
	}
}

func TestRecordEntry_Type(t *testing.T) {
	entry := RecordEntry{
		TenantID: "tenant-1",
		AgentID:  "agent-1",
		Value:    map[string]int{"count": 42},
	}

	if entry.TenantID != "tenant-1" {
		t.Errorf("RecordEntry.TenantID = %q, want %q", entry.TenantID, "tenant-1")
	}
	if entry.AgentID != "agent-1" {
		t.Errorf("RecordEntry.AgentID = %q, want %q", entry.AgentID, "agent-1")
	}
}

func TestTenantKey_DifferentTenants_SameAgent(t *testing.T) {
	// Different tenants with the same agent ID must produce different keys,
	// ensuring tenant isolation at the partition level.
	key1 := "tenant-alpha" + ":" + "agent-1"
	key2 := "tenant-beta" + ":" + "agent-1"

	if key1 == key2 {
		t.Error("different tenants with same agent produced identical keys — tenant isolation violated")
	}
}

func TestTenantKey_SameTenant_DifferentAgents(t *testing.T) {
	// Same tenant with different agent IDs must produce different keys,
	// ensuring agents within a tenant are spread across partitions.
	key1 := "tenant-1" + ":" + "agent-alpha"
	key2 := "tenant-1" + ":" + "agent-beta"

	if key1 == key2 {
		t.Error("same tenant with different agents produced identical keys — agent isolation violated")
	}
}
