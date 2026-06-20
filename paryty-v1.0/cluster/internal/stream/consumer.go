package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

const (
	defaultMaxRetries   = 3
	defaultDrainTimeout = 10 * time.Second
	defaultRetryBase    = 100 * time.Millisecond
	dlqPublishTimeout   = 5 * time.Second

	// defaultWorkerPoolMultiplier is multiplied by GOMAXPROCS to compute
	// the default worker pool size when WithWorkerPool is not specified.
	defaultWorkerPoolMultiplier = 2

	// maxWorkerPoolSize caps the worker pool to prevent excessive goroutines.
	maxWorkerPoolSize = 64

	// workerChannelBuffer is the per-worker buffer depth for the work channel.
	// Total buffer = workerCount * workerChannelBuffer.
	workerChannelBuffer = 10
)

// hasRegexTopics returns true if any topic in the slice contains regex metacharacters.
func hasRegexTopics(topics []string) bool {
	for _, t := range topics {
		if strings.ContainsAny(t, ".*+?[](){}|^$\\") {
			return true
		}
	}
	return false
}

// Handler is a function that handles consumed messages.
type Handler func(ctx context.Context, key string, value []byte) error

// RecordHandler is a function that handles consumed records with topic context.
// Unlike Handler, RecordHandler receives the full kgo.Record including the
// Topic field, enabling topic-aware routing in the pipeline.
type RecordHandler func(ctx context.Context, record *kgo.Record) error

// ConsumerOption configures the Consumer.
type ConsumerOption func(*Consumer)

// WithMaxRetries sets the maximum number of retries for failed handlers.
// After maxRetries attempts the message is sent to the DLQ if configured,
// otherwise it is logged and dropped.
func WithMaxRetries(n int) ConsumerOption {
	return func(c *Consumer) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithDrainTimeout sets the timeout for graceful drain on Close.
// If in-flight handlers do not complete within this duration, the consumer
// force-closes. Default is 10 seconds.
func WithDrainTimeout(d time.Duration) ConsumerOption {
	return func(c *Consumer) {
		if d <= 0 {
			d = defaultDrainTimeout
		}
		c.drainTimeout = d
	}
}

// WithWorkerPool sets the number of worker goroutines for processing records.
// This replaces the previous goroutine-per-message pattern with a bounded pool,
// eliminating unbounded goroutine spawning during high-throughput bursts.
//
// The worker pool is bounded: n is clamped to [1, maxWorkerPoolSize].
// When n <= 0, it defaults to runtime.GOMAXPROCS(0) * 2.
// When not called, NewConsumerWithDLQ sets the default automatically.
//
// Memory impact: eliminates 2-8 KB per-message goroutine stacks during bursts.
// A pool of 64 workers uses a fixed ~512 KB regardless of burst size.
func WithWorkerPool(n int) ConsumerOption {
	return func(c *Consumer) {
		if n <= 0 {
			n = runtime.GOMAXPROCS(0) * defaultWorkerPoolMultiplier
		}
		if n > maxWorkerPoolSize {
			n = maxWorkerPoolSize
		}
		c.workerCount = n
	}
}

// Consumer is the Redpanda consumer with DLQ support, retry with backoff,
// lag monitoring, and graceful drain.
type Consumer struct {
	client        *kgo.Client
	handler       Handler
	recordHandler RecordHandler
	logger        *zap.Logger
	cfg           Config
	group         string
	wg            sync.WaitGroup
	cancel        context.CancelFunc
	draining      atomic.Bool
	dlqProducer   *Producer
	dlqTopic      string
	maxRetries    int
	drainTimeout  time.Duration
	adminClient   *kadm.Client

	// Worker pool fields — bounded set of goroutines that process records
	// from a shared channel, replacing the previous goroutine-per-message pattern.
	workCh         chan *kgo.Record
	workerCount    int
	workerWg       sync.WaitGroup
	workerCloseOnce sync.Once
}

// NewConsumer creates a new Redpanda consumer with optional configuration.
func NewConsumer(cfg Config, group string, topics []string, handler Handler, logger *zap.Logger, opts ...ConsumerOption) (*Consumer, error) {
	return NewConsumerWithDLQ(cfg, group, topics, handler, logger, nil, "", opts...)
}

// NewConsumerWithDLQ creates a new Redpanda consumer with optional DLQ support.
// If dlqProducer and dlqTopic are both set, failed messages are forwarded to
// the dead letter queue topic with original metadata headers after retries are
// exhausted. When DLQ is not configured, failed messages are logged and dropped.
//
// Additional ConsumerOption values can be provided to override defaults for
// max retries, drain timeout, and worker pool size.
//
// If any topic in the topics slice contains regex metacharacters (e.g., \. or .*)
// the consumer automatically enables regex subscription mode.
func NewConsumerWithDLQ(cfg Config, group string, topics []string, handler Handler, logger *zap.Logger, dlqProducer *Producer, dlqTopic string, opts ...ConsumerOption) (*Consumer, error) {
	kopts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
		kgo.AutoCommitInterval(1 * time.Second),
		kgo.AutoCommitMarks(),
		kgo.OnPartitionsAssigned(func(ctx context.Context, _ *kgo.Client, m map[string][]int32) {
			logger.Info("Partitions assigned", zap.Any("partitions", m))
		}),
		kgo.OnPartitionsRevoked(func(ctx context.Context, _ *kgo.Client, m map[string][]int32) {
			logger.Info("Partitions revoked", zap.Any("partitions", m))
		}),
		kgo.OnPartitionsLost(func(ctx context.Context, _ *kgo.Client, m map[string][]int32) {
			logger.Warn("Partitions lost", zap.Any("partitions", m))
		}),
	}

	// Enable regex subscription if topics contain regex patterns.
	if hasRegexTopics(topics) {
		kopts = append(kopts, kgo.ConsumeRegex())
		logger.Info("Regex subscription enabled for topics", zap.Strings("topics", topics))
	}

	client, err := kgo.NewClient(kopts...)
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}

	c := &Consumer{
		client:       client,
		handler:      handler,
		logger:       logger,
		cfg:          cfg,
		group:        group,
		dlqProducer:  dlqProducer,
		dlqTopic:     dlqTopic,
		maxRetries:   defaultMaxRetries,
		drainTimeout: defaultDrainTimeout,
		adminClient:  kadm.NewClient(client),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Set default worker pool size if WithWorkerPool was not called.
	if c.workerCount == 0 {
		c.workerCount = runtime.GOMAXPROCS(0) * defaultWorkerPoolMultiplier
		if c.workerCount > maxWorkerPoolSize {
			c.workerCount = maxWorkerPoolSize
		}
	}

	logger.Info("Consumer created",
		zap.String("group", group),
		zap.Strings("topics", topics),
		zap.Bool("dlq_enabled", dlqProducer != nil && dlqTopic != ""),
		zap.Int("max_retries", c.maxRetries),
		zap.Duration("drain_timeout", c.drainTimeout),
		zap.Int("worker_count", c.workerCount),
	)

	return c, nil
}

// SetRecordHandler sets a RecordHandler that receives the full kgo.Record
// including topic information, enabling topic-aware routing in the pipeline.
// When set, this handler takes precedence over the Handler provided at
// construction time. Both handlers share the same retry and DLQ semantics.
func (c *Consumer) SetRecordHandler(rh RecordHandler) {
	c.recordHandler = rh
}

// Close gracefully shuts down the consumer.
// It stops accepting new messages, waits for in-flight handlers to complete
// (up to the drain timeout), commits final marked offsets, and closes the
// underlying client.
func (c *Consumer) Close() {
	c.logger.Info("Consumer closing, starting drain")
	c.draining.Store(true)

	if c.cancel != nil {
		c.cancel()
	}

	// Wait for the poll loop to exit. This ensures no new records are sent
	// to workCh after this point.
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("Poll loop exited")
	case <-time.After(c.drainTimeout):
		c.logger.Warn("Drain timeout exceeded waiting for poll loop",
			zap.Duration("timeout", c.drainTimeout),
		)
	}

	// Close the work channel (once) so workers drain remaining records and exit.
	// Guard against nil workCh — Close may be called before Start.
	c.workerCloseOnce.Do(func() {
		if c.workCh != nil {
			close(c.workCh)
		}
	})

	// Wait for in-flight record handlers with drain timeout.
	workerDone := make(chan struct{})
	go func() {
		c.workerWg.Wait()
		close(workerDone)
	}()

	select {
	case <-workerDone:
		c.logger.Info("All in-flight handlers completed during drain")
	case <-time.After(c.drainTimeout):
		c.logger.Warn("Drain timeout exceeded for worker pool, forcing close",
			zap.Duration("timeout", c.drainTimeout),
		)
	}

	// Commit final marked offsets before closing.
	commitCtx, commitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer commitCancel()
	if err := c.client.CommitMarkedOffsets(commitCtx); err != nil {
		c.logger.Error("Failed to commit final offsets", zap.Error(err))
	} else {
		c.logger.Info("Final offsets committed")
	}

	c.client.Close()
	c.logger.Info("Consumer closed")
}

// Start starts consuming messages using a bounded worker pool.
// Records are dispatched to a fixed pool of worker goroutines via a buffered
// channel, replacing the previous goroutine-per-message pattern. This bounds
// memory usage during high-throughput bursts (each goroutine stack is 2-8 KB).
//
// Worker pool lifecycle:
//   - Workers start here and block on the work channel.
//   - Each record is sent to the channel (backpressure when full).
//   - On Close: context is cancelled → poll loop exits → channel is closed →
//     workers drain remaining records and exit.
func (c *Consumer) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)

	// Start the bounded worker pool.
	c.workCh = make(chan *kgo.Record, c.workerCount*workerChannelBuffer)
	for i := 0; i < c.workerCount; i++ {
		c.workerWg.Add(1)
		go func(workerID int) {
			defer c.workerWg.Done()
			c.logger.Debug("Worker started", zap.Int("worker_id", workerID))
			for record := range c.workCh {
				c.processRecord(ctx, record)
			}
			c.logger.Debug("Worker stopped", zap.Int("worker_id", workerID))
		}(i)
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.logger.Info("Consumer started",
			zap.Int("max_retries", c.maxRetries),
			zap.Duration("drain_timeout", c.drainTimeout),
			zap.Bool("dlq_enabled", c.dlqProducer != nil),
			zap.Int("worker_count", c.workerCount),
		)

		for {
			select {
			case <-ctx.Done():
				c.logger.Info("Consumer poll loop stopping")
				return
			default:
				if c.draining.Load() {
					return
				}

				fetches := c.client.PollFetches(ctx)
				if fetches.IsClientClosed() {
					return
				}

				// Dispatch to bounded worker pool via channel instead of
				// spawning a goroutine per record. The channel provides
				// automatic backpressure when all workers are busy.
				fetches.EachRecord(func(record *kgo.Record) {
					c.workCh <- record
				})

				if err := fetches.Err(); err != nil {
					c.logger.Error("Fetch error", zap.Error(err))
				}
			}
		}
	}()
}

// Lag returns the consumer group lag per topic-partition.
// The map key is "topic-partition" (e.g., "my-topic-0") and the value is
// the number of messages behind the high watermark. Returns an empty map
// if the group has no committed offsets.
func (c *Consumer) Lag(ctx context.Context) (map[string]int64, error) {
	lags, err := c.adminClient.Lag(ctx, c.group)
	if err != nil {
		return nil, fmt.Errorf("fetch consumer group lag: %w", err)
	}

	result := make(map[string]int64)
	groupLag, ok := lags[c.group]
	if !ok {
		return result, nil
	}

	for topic, partitionLags := range groupLag.Lag {
		for partition, memberLag := range partitionLags {
			key := topic + "-" + strconv.FormatInt(int64(partition), 10)
			result[key] = memberLag.Lag
		}
	}

	return result, nil
}

// processRecord handles a single consumed record with retry and DLQ support.
// Retries use exponential backoff (100ms, 400ms, 1600ms). After maxRetries
// attempts the record is sent to the DLQ if configured.
func (c *Consumer) processRecord(ctx context.Context, record *kgo.Record) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// If draining, skip retries and send straight to DLQ.
		if attempt > 0 && c.draining.Load() {
			c.logger.Warn("Draining, skipping remaining retries",
				zap.String("topic", record.Topic),
				zap.Int32("partition", record.Partition),
				zap.Int64("offset", record.Offset),
				zap.Int("attempt", attempt),
			)
			c.publishToDLQ(ctx, record, lastErr)
			return
		}

		// Exponential backoff between retries.
		if attempt > 0 {
			backoff := c.retryBackoff(attempt - 1)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				c.publishToDLQ(ctx, record, lastErr)
				return
			case <-timer.C:
			}
		}

		// Dispatch to record handler (topic-aware) or handler (key+value only).
		var err error
		if c.recordHandler != nil {
			err = c.recordHandler(ctx, record)
		} else {
			err = c.handler(ctx, string(record.Key), record.Value)
		}

		if err == nil {
			if attempt > 0 {
				c.logger.Info("Handler succeeded after retry",
					zap.String("topic", record.Topic),
					zap.Int32("partition", record.Partition),
					zap.Int64("offset", record.Offset),
					zap.Int("total_attempts", attempt+1),
				)
			}
			return
		}
		lastErr = err

		if attempt < c.maxRetries {
			c.logger.Warn("Handler error, will retry",
				zap.String("topic", record.Topic),
				zap.String("key", string(record.Key)),
				zap.Int32("partition", record.Partition),
				zap.Int64("offset", record.Offset),
				zap.Int("attempt", attempt+1),
				zap.Int("max_retries", c.maxRetries),
				zap.Error(err),
			)
		}
	}

	// All retries exhausted — send to DLQ.
	c.logger.Error("Max retries exceeded, sending to DLQ",
		zap.String("topic", record.Topic),
		zap.Int32("partition", record.Partition),
		zap.Int64("offset", record.Offset),
		zap.Int("max_retries", c.maxRetries),
		zap.Error(lastErr),
	)
	c.publishToDLQ(ctx, record, lastErr)
}

// retryBackoff calculates exponential backoff for the given attempt index.
// Returns 100ms, 400ms, 1600ms for attempts 0, 1, 2.
func (c *Consumer) retryBackoff(attempt int) time.Duration {
	backoff := defaultRetryBase
	for i := 0; i < attempt; i++ {
		backoff *= 4
	}
	return backoff
}

// publishToDLQ publishes a failed record to the dead letter queue.
// This is best-effort: if the DLQ publish itself fails, the error is logged
// but the consumer does not crash. The original topic, partition, offset,
// error message, and timestamp are preserved as record headers.
func (c *Consumer) publishToDLQ(ctx context.Context, record *kgo.Record, handlerErr error) {
	if c.dlqProducer == nil {
		c.logger.Warn("No DLQ producer configured, message dropped",
			zap.String("topic", record.Topic),
			zap.Int32("partition", record.Partition),
			zap.Int64("offset", record.Offset),
		)
		return
	}

	dlqRecord := &kgo.Record{
		Topic: c.dlqTopic,
		Key:   record.Key,
		Value: record.Value,
		Headers: []kgo.RecordHeader{
			{Key: "dlq-original-topic", Value: []byte(record.Topic)},
			{Key: "dlq-original-partition", Value: strconv.AppendInt(nil, int64(record.Partition), 10)},
			{Key: "dlq-original-offset", Value: strconv.AppendInt(nil, record.Offset, 10)},
			{Key: "dlq-error", Value: []byte(handlerErr.Error())},
			{Key: "dlq-timestamp", Value: []byte(time.Now().UTC().Format(time.RFC3339))},
		},
	}

	// Use background context — parent context may be cancelled during drain.
	dlqCtx, dlqCancel := context.WithTimeout(context.Background(), dlqPublishTimeout)
	defer dlqCancel()

	if err := c.dlqProducer.client.ProduceSync(dlqCtx, dlqRecord).FirstErr(); err != nil {
		c.logger.Error("Failed to publish to DLQ (best-effort, message lost)",
			zap.String("original_topic", record.Topic),
			zap.Int32("partition", record.Partition),
			zap.Int64("offset", record.Offset),
			zap.Error(err),
		)
	} else {
		c.logger.Info("Message published to DLQ",
			zap.String("original_topic", record.Topic),
			zap.Int32("partition", record.Partition),
			zap.Int64("offset", record.Offset),
			zap.String("dlq_topic", c.dlqTopic),
		)
	}
}

// ConsumeJSON consumes messages and unmarshals them as JSON.
func ConsumeJSON[T any](handler func(ctx context.Context, key string, value T) error) Handler {
	return func(ctx context.Context, key string, value []byte) error {
		var t T
		if err := json.Unmarshal(value, &t); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		return handler(ctx, key, t)
	}
}
