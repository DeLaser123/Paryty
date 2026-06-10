// Package stream implements the Redpanda (Kafka-compatible) stream engine.
// This is the central nervous system for data flow in the Paryty cluster.
package stream

import (
	"context"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/pool"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// Config contains configuration for the stream engine.
type Config struct {
	Brokers  []string `yaml:"brokers" json:"brokers"`
	ClientID string   `yaml:"client_id" json:"client_id"`
}

// Producer is the Redpanda producer.
type Producer struct {
	client *kgo.Client
	logger *zap.Logger
	cfg    Config
}

// NewProducer creates a new Redpanda producer with idempotent writes,
// Zstd batch compression, bounded buffering, and delivery timeouts.
func NewProducer(cfg Config, logger *zap.Logger) (*Producer, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(5 * time.Millisecond),
		kgo.RecordPartitioner(kgo.StickyPartitioner()),
		// Idempotent writes are enabled by default in franz-go (broker-level
		// deduplication of retried messages). Do NOT call
		// DisableIdempotentWrite() — it is strictly a win for correctness.
		//
		// Zstd batch compression for throughput and storage efficiency.
		kgo.ProducerBatchCompression(kgo.ZstdCompression()),
		// Delivery timeout for the entire produce request lifecycle.
		kgo.ProduceRequestTimeout(30 * time.Second),
		// Bound the in-memory record buffer to prevent unbounded memory growth.
		// Reduced from 10,000 to 1,000 — 1K records at ~1 KB each = 1 MB buffer.
		// With 5ms linger time the producer flushes frequently enough.
		// Saves up to 9 MB of buffered record memory under backpressure.
		kgo.MaxBufferedRecords(1000),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}

	logger.Info("Producer created",
		zap.String("client_id", cfg.ClientID),
		zap.Int("brokers", len(cfg.Brokers)),
		zap.Bool("idempotent", true),
		zap.String("compression", "zstd"),
		zap.Int("max_buffered_records", 1000),
	)

	return &Producer{
		client: client,
		logger: logger,
		cfg:    cfg,
	}, nil
}

// Close closes the producer, flushing any buffered records.
func (p *Producer) Close() {
	p.client.Close()
}

// Publish publishes a single message to a topic with an arbitrary key.
// This method is kept for backward compatibility with callers that do not
// have tenant context. Prefer PublishTenant for tenant-scoped writes.
func (p *Producer) Publish(ctx context.Context, topic string, key string, value interface{}) error {
	data, err := pool.PooledJSONMarshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}

	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
	}

	if err := p.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("produce: %w", err)
	}

	p.logger.Debug("Published message",
		zap.String("topic", topic),
		zap.String("key", key),
		zap.Int("size", len(data)),
	)

	return nil
}

// PublishTenant publishes a message with a tenant-scoped partition key.
// The record key is formatted as "tenantID:agentID", ensuring all records
// for the same tenant+agent pair are routed to the same partition and
// processed in order.
func (p *Producer) PublishTenant(ctx context.Context, topic, tenantID, agentID string, value interface{}) error {
	if tenantID == "" {
		return fmt.Errorf("tenantID must not be empty")
	}
	if agentID == "" {
		return fmt.Errorf("agentID must not be empty")
	}

	data, err := pool.PooledJSONMarshal(value)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}

	key := tenantID + ":" + agentID
	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
	}

	if err := p.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("produce tenant=%s agent=%s: %w", tenantID, agentID, err)
	}

	p.logger.Debug("Published tenant message",
		zap.String("topic", topic),
		zap.String("tenant_id", tenantID),
		zap.String("agent_id", agentID),
		zap.Int("size", len(data)),
	)

	return nil
}

// RecordEntry is a single entry for batch publishing with tenant context.
type RecordEntry struct {
	TenantID string
	AgentID  string
	Value    interface{}
}

// PublishTenantBatch publishes a batch of tenant-scoped records to a topic
// in a single ProduceSync call. Each record key is "tenantID:agentID".
// This is more efficient than calling PublishTenant in a loop because it
// amortizes network round-trips and benefits from Zstd batch compression.
func (p *Producer) PublishTenantBatch(ctx context.Context, topic string, entries []RecordEntry) error {
	if len(entries) == 0 {
		return nil
	}

	records := make([]*kgo.Record, 0, len(entries))
	for i, entry := range entries {
		if entry.TenantID == "" {
			return fmt.Errorf("entry[%d]: tenantID must not be empty", i)
		}
		if entry.AgentID == "" {
			return fmt.Errorf("entry[%d]: agentID must not be empty", i)
		}

		data, err := pool.PooledJSONMarshal(entry.Value)
		if err != nil {
			return fmt.Errorf("entry[%d] marshal value: %w", i, err)
		}

		key := entry.TenantID + ":" + entry.AgentID
		records = append(records, &kgo.Record{
			Topic: topic,
			Key:   []byte(key),
			Value: data,
		})
	}

	results := p.client.ProduceSync(ctx, records...)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("produce tenant batch: %w", err)
	}

	p.logger.Debug("Published tenant batch",
		zap.String("topic", topic),
		zap.Int("count", len(records)),
	)

	return nil
}

// PublishBatch publishes a batch of messages to a topic using the provided
// keys directly. Retained for backward compatibility.
func (p *Producer) PublishBatch(ctx context.Context, topic string, messages []Message) error {
	if len(messages) == 0 {
		return nil
	}

	records := make([]*kgo.Record, 0, len(messages))
	for _, msg := range messages {
		data, err := pool.PooledJSONMarshal(msg.Value)
		if err != nil {
			return fmt.Errorf("marshal value: %w", err)
		}
		records = append(records, &kgo.Record{
			Topic: topic,
			Key:   []byte(msg.Key),
			Value: data,
		})
	}

	results := p.client.ProduceSync(ctx, records...)
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("produce batch: %w", err)
	}

	p.logger.Debug("Published batch",
		zap.String("topic", topic),
		zap.Int("count", len(records)),
	)

	return nil
}

// Message represents a message to be published.
type Message struct {
	Key   string
	Value interface{}
}
