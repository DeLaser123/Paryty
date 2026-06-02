package stream

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// StreamEngine is the main stream engine that manages producers, consumers, and topics.
type StreamEngine struct {
	producer *Producer
	consumer *Consumer
	topics   *TopicManager
	logger   *zap.Logger
	cfg      Config
}

// NewStreamEngine creates a new stream engine.
func NewStreamEngine(cfg Config, logger *zap.Logger) (*StreamEngine, error) {
	producer, err := NewProducer(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("create producer: %w", err)
	}

	topics, err := NewTopicManager(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("create topic manager: %w", err)
	}

	return &StreamEngine{
		producer: producer,
		topics:   topics,
		logger:   logger,
		cfg:      cfg,
	}, nil
}

// Close closes the stream engine in the correct order:
// consumer (stop polling), producer (flush buffered records), topic manager.
func (e *StreamEngine) Close() {
	if e.consumer != nil {
		e.consumer.Close()
	}
	e.producer.Close()
	e.topics.Close()
}

// InitializeTopics creates all required topics for the given tenant.
// Uses a single ListTopics call to avoid redundant broker round-trips.
func (e *StreamEngine) InitializeTopics(ctx context.Context, tenant string) error {
	for _, tc := range requiredTopics(tenant) {
		if err := e.topics.EnsureTopic(ctx, tc.name, tc.partitions, tc.replication); err != nil {
			return fmt.Errorf("ensure topic %s: %w", tc.name, err)
		}
	}

	e.logger.Info("All topics initialized", zap.String("tenant", tenant))
	return nil
}

// Producer returns the stream producer.
func (e *StreamEngine) Producer() *Producer {
	return e.producer
}

// Topics returns the topic manager.
func (e *StreamEngine) Topics() *TopicManager {
	return e.topics
}

// NewConsumer creates a new consumer for the given topics without DLQ support.
// The consumer is stored in the engine for health checks and lag reporting.
func (e *StreamEngine) NewConsumer(group string, topics []string, handler Handler, opts ...ConsumerOption) (*Consumer, error) {
	return e.NewConsumerWithDLQ(group, topics, handler, nil, "", opts...)
}

// NewConsumerWithDLQ creates a new consumer with optional DLQ support.
// If dlqProducer and dlqTopic are both provided, failed messages are
// forwarded to the tenant-scoped DLQ topic. The consumer is stored in
// the engine for health checks and lag reporting.
func (e *StreamEngine) NewConsumerWithDLQ(group string, topics []string, handler Handler, dlqProducer *Producer, dlqTopic string, opts ...ConsumerOption) (*Consumer, error) {
	consumer, err := NewConsumerWithDLQ(e.cfg, group, topics, handler, e.logger, dlqProducer, dlqTopic, opts...)
	if err != nil {
		return nil, err
	}
	e.consumer = consumer
	return consumer, nil
}

// Ping verifies connectivity to the Redpanda broker by issuing a metadata
// request via the topic manager's admin client. Returns an error if the
// broker is unreachable.
func (e *StreamEngine) Ping(ctx context.Context) error {
	_, err := e.topics.client.ListTopics(ctx)
	if err != nil {
		return fmt.Errorf("ping: stream broker unreachable: %w", err)
	}
	return nil
}

// Lag returns consumer lag per topic-partition. The map keys are formatted
// as "topic[partition]". Returns an empty map if no consumer has been created.
func (e *StreamEngine) Lag(ctx context.Context) (map[string]int64, error) {
	if e.consumer == nil {
		return map[string]int64{}, nil
	}
	return e.consumer.Lag(ctx)
}
