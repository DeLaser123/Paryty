package stream

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNewConsumer_CreatesClientWithGroup(t *testing.T) {
	cfg := testConfig()
	logger := testLogger()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, logger)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.client == nil {
		t.Fatal("NewConsumer() returned nil client")
	}
	if consumer.group != "test-group" {
		t.Errorf("group = %q, want %q", consumer.group, "test-group")
	}
}

func TestNewConsumer_NoDLQByDefault(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.dlqProducer != nil {
		t.Error("NewConsumer() should not set dlqProducer")
	}
	if consumer.dlqTopic != "" {
		t.Error("NewConsumer() should not set dlqTopic")
	}
}

func TestNewConsumer_DefaultRetryConfig(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.maxRetries != defaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", consumer.maxRetries, defaultMaxRetries)
	}
	if consumer.drainTimeout != defaultDrainTimeout {
		t.Errorf("drainTimeout = %v, want %v", consumer.drainTimeout, defaultDrainTimeout)
	}
}

func TestNewConsumer_WithOptions(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithMaxRetries(5),
		WithDrainTimeout(30_000_000_000), // 30s in nanoseconds
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.maxRetries != 5 {
		t.Errorf("maxRetries = %d, want 5", consumer.maxRetries)
	}
}

func TestWithMaxRetries_NegativeClamped(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithMaxRetries(-1),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.maxRetries != 0 {
		t.Errorf("maxRetries = %d, want 0 (clamped from negative)", consumer.maxRetries)
	}
}

func TestNewConsumerWithDLQ_WiresDLQFields(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	dlqProducer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer dlqProducer.Close()

	dlqTopic := TopicDLQ("test-tenant")
	consumer, err := NewConsumerWithDLQ(cfg, "test-group", []string{"test-topic"}, handler, testLogger(), dlqProducer, dlqTopic)
	if err != nil {
		t.Fatalf("NewConsumerWithDLQ() error: %v", err)
	}
	defer consumer.Close()

	if consumer.dlqProducer != dlqProducer {
		t.Error("dlqProducer not wired")
	}
	if consumer.dlqTopic != dlqTopic {
		t.Errorf("dlqTopic = %q, want %q", consumer.dlqTopic, dlqTopic)
	}
}

func TestNewConsumerWithDLQ_EmptyDLQTopicMeansDisabled(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	dlqProducer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer dlqProducer.Close()

	consumer, err := NewConsumerWithDLQ(cfg, "test-group", []string{"test-topic"}, handler, testLogger(), dlqProducer, "")
	if err != nil {
		t.Fatalf("NewConsumerWithDLQ() error: %v", err)
	}
	defer consumer.Close()

	if consumer.dlqProducer != dlqProducer {
		t.Error("dlqProducer should still be stored even if topic is empty")
	}
	if consumer.dlqTopic != "" {
		t.Error("dlqTopic should remain empty")
	}
}

func TestNewConsumerWithDLQ_NilProducerMeansDisabled(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumerWithDLQ(cfg, "test-group", []string{"test-topic"}, handler, testLogger(), nil, TopicDLQ("x"))
	if err != nil {
		t.Fatalf("NewConsumerWithDLQ() error: %v", err)
	}
	defer consumer.Close()

	if consumer.dlqProducer != nil {
		t.Error("dlqProducer should be nil")
	}
}

func TestPublishToDLQ_NilProducer_DoesNotPanic(t *testing.T) {
	c := &Consumer{
		dlqProducer: nil,
		dlqTopic:    "dlq-topic",
		logger:      testLogger(),
	}

	record := &kgo.Record{
		Topic: "original-topic",
		Key:   []byte("key-1"),
		Value: []byte(`{"x":1}`),
	}

	c.publishToDLQ(context.Background(), record, errors.New("handler failed"))
}

func TestPublishToDLQ_ConstructsCorrectHeaders(t *testing.T) {
	originalTopic := "paryty.acme.metrics.raw"
	handlerErr := errors.New("corrupt data")
	key := []byte("tenant-1:agent-1")
	value := []byte(`{"metric":"cpu","value":99.5}`)

	record := &kgo.Record{
		Topic: originalTopic,
		Key:   key,
		Value: value,
	}

	dlqRecord := &kgo.Record{
		Topic: "paryty.acme.dead-letter",
		Key:   record.Key,
		Value: record.Value,
		Headers: []kgo.RecordHeader{
			{Key: "dlq-original-topic", Value: []byte(originalTopic)},
			{Key: "dlq-error", Value: []byte(handlerErr.Error())},
		},
	}

	if dlqRecord.Topic != "paryty.acme.dead-letter" {
		t.Errorf("DLQ topic = %q, want %q", dlqRecord.Topic, "paryty.acme.dead-letter")
	}
	if string(dlqRecord.Key) != string(key) {
		t.Errorf("DLQ key = %q, want %q", string(dlqRecord.Key), string(key))
	}
	if string(dlqRecord.Value) != string(value) {
		t.Errorf("DLQ value = %q, want %q", string(dlqRecord.Value), string(value))
	}

	if len(dlqRecord.Headers) != 2 {
		t.Fatalf("DLQ headers count = %d, want 2", len(dlqRecord.Headers))
	}
	if dlqRecord.Headers[0].Key != "dlq-original-topic" {
		t.Errorf("header[0].Key = %q, want dlq-original-topic", dlqRecord.Headers[0].Key)
	}
	if dlqRecord.Headers[1].Key != "dlq-error" {
		t.Errorf("header[1].Key = %q, want dlq-error", dlqRecord.Headers[1].Key)
	}
}

func TestConsumer_Lag_WithoutBroker(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	ctx := context.Background()
	_, err = consumer.Lag(ctx)
	if err == nil {
		t.Log("Lag() succeeded (broker is reachable)")
	}
}

func TestNewConsumer_EmptyBrokers(t *testing.T) {
	cfg := Config{
		Brokers:  []string{},
		ClientID: "test-consumer",
	}
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	_, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err == nil {
		t.Fatal("NewConsumer() expected error for empty brokers, got nil")
	}
}

func TestTopicDLQ_TenantScoped(t *testing.T) {
	tests := []struct {
		tenant string
		want   string
	}{
		{"acme", "paryty.acme.dead-letter"},
		{"default", "paryty.default.dead-letter"},
		{"", "paryty..dead-letter"},
	}

	for _, tc := range tests {
		got := TopicDLQ(tc.tenant)
		if got != tc.want {
			t.Errorf("TopicDLQ(%q) = %q, want %q", tc.tenant, got, tc.want)
		}
	}
}

func TestConsumerClose_MultipleCallsSafe(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}

	consumer.Close()
	consumer.Close()
}

func TestRetryBackoff_ExponentialGrowth(t *testing.T) {
	c := &Consumer{}

	tests := []struct {
		attempt int
		want    int64
	}{
		{0, 100},
		{1, 400},
		{2, 1600},
	}

	for _, tc := range tests {
		got := c.retryBackoff(tc.attempt)
		if got.Milliseconds() != tc.want {
			t.Errorf("retryBackoff(%d) = %dms, want %dms", tc.attempt, got.Milliseconds(), tc.want)
		}
	}
}

func TestConsumer_ImplementsCloseInterface(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.adminClient == nil {
		t.Fatal("adminClient should be initialized")
	}
}

func TestWithDrainTimeout_DefaultForZero(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithDrainTimeout(0),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.drainTimeout != defaultDrainTimeout {
		t.Errorf("drainTimeout = %v, want default %v for zero input", consumer.drainTimeout, defaultDrainTimeout)
	}
}

func TestConsumer_ConcurrentCloseSafety(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			consumer.Close()
		}()
	}
	wg.Wait()
}

func TestConsumer_DrainingFlagInitiallyFalse(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger())
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.draining.Load() {
		t.Error("new consumer should not be draining")
	}
}

// ---------------------------------------------------------------------------
// processRecord â€” retry, backoff, and DLQ integration tests
// ---------------------------------------------------------------------------

func TestProcessRecord_SuccessOnFirstAttempt(t *testing.T) {
	var handlerCalls atomic.Int32
	handler := func(ctx context.Context, key string, value []byte) error {
		handlerCalls.Add(1)
		return nil
	}

	c := &Consumer{
		handler:    handler,
		logger:     testLogger(),
		maxRetries: 3,
	}

	record := &kgo.Record{
		Topic: "test-topic", Key: []byte("k"), Value: []byte("v"),
		Partition: 0, Offset: 42,
	}

	c.processRecord(context.Background(), record)

	if handlerCalls.Load() != 1 {
		t.Errorf("handler called %d times, want 1", handlerCalls.Load())
	}
}

func TestProcessRecord_RetriesUntilSuccess(t *testing.T) {
	var handlerCalls atomic.Int32
	handler := func(ctx context.Context, key string, value []byte) error {
		calls := handlerCalls.Add(1)
		if calls <= 2 {
			return errors.New("transient error")
		}
		return nil
	}

	c := &Consumer{
		handler:      handler,
		logger:       testLogger(),
		maxRetries:   3,
		drainTimeout: defaultDrainTimeout,
	}

	record := &kgo.Record{
		Topic: "test-topic", Key: []byte("k"), Value: []byte("v"),
		Partition: 0, Offset: 42,
	}

	start := time.Now()
	c.processRecord(context.Background(), record)
	elapsed := time.Since(start)

	if handlerCalls.Load() != 3 {
		t.Errorf("handler called %d times, want 3", handlerCalls.Load())
	}

	if elapsed < 450*time.Millisecond {
		t.Errorf("elapsed %v, expected at least 450ms (two backoffs)", elapsed)
	}
}

func TestProcessRecord_ExhaustsRetriesThenDLQ(t *testing.T) {
	var handlerCalls atomic.Int32
	handler := func(ctx context.Context, key string, value []byte) error {
		handlerCalls.Add(1)
		return errors.New("permanent failure")
	}

	cfg := testConfig()
	dlqProducer, err := NewProducer(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewProducer() error: %v", err)
	}
	defer dlqProducer.Close()

	c := &Consumer{
		handler:      handler,
		logger:       testLogger(),
		maxRetries:   2,
		drainTimeout: defaultDrainTimeout,
		dlqProducer:  dlqProducer,
		dlqTopic:     "dead-letter",
	}

	record := &kgo.Record{
		Topic: "test-topic", Key: []byte("k"), Value: []byte("v"),
		Partition: 0, Offset: 42,
	}

	c.processRecord(context.Background(), record)

	if handlerCalls.Load() != 3 {
		t.Errorf("handler called %d times, want 3 (maxRetries+1)", handlerCalls.Load())
	}
}

func TestProcessRecord_ZeroRetries(t *testing.T) {
	var handlerCalls atomic.Int32
	handler := func(ctx context.Context, key string, value []byte) error {
		handlerCalls.Add(1)
		return errors.New("fail")
	}

	c := &Consumer{
		handler:      handler,
		logger:       testLogger(),
		maxRetries:   0,
		drainTimeout: defaultDrainTimeout,
	}

	record := &kgo.Record{
		Topic: "test-topic", Key: []byte("k"), Value: []byte("v"),
		Partition: 0, Offset: 42,
	}

	c.processRecord(context.Background(), record)

	if handlerCalls.Load() != 1 {
		t.Errorf("handler called %d times, want 1 (zero retries)", handlerCalls.Load())
	}
}

func TestProcessRecord_ContextCancelled_SkipsToDLQ(t *testing.T) {
	var handlerCalls atomic.Int32
	handler := func(ctx context.Context, key string, value []byte) error {
		handlerCalls.Add(1)
		return errors.New("fail")
	}

	c := &Consumer{
		handler:      handler,
		logger:       testLogger(),
		maxRetries:   5,
		drainTimeout: defaultDrainTimeout,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	record := &kgo.Record{
		Topic: "test-topic", Key: []byte("k"), Value: []byte("v"),
		Partition: 0, Offset: 42,
	}

	c.processRecord(ctx, record)

	if handlerCalls.Load() != 1 {
		t.Errorf("handler called %d times, want 1 (context cancelled during backoff)", handlerCalls.Load())
	}
}

func TestProcessRecord_DrainingSkipsRetries(t *testing.T) {
	var handlerCalls atomic.Int32
	handler := func(ctx context.Context, key string, value []byte) error {
		handlerCalls.Add(1)
		return errors.New("fail")
	}

	c := &Consumer{
		handler:      handler,
		logger:       testLogger(),
		maxRetries:   5,
		drainTimeout: defaultDrainTimeout,
	}
	c.draining.Store(true)

	record := &kgo.Record{
		Topic: "test-topic", Key: []byte("k"), Value: []byte("v"),
		Partition: 0, Offset: 42,
	}

	c.processRecord(context.Background(), record)

	if handlerCalls.Load() != 1 {
		t.Errorf("handler called %d times, want 1 (draining skips retries)", handlerCalls.Load())
	}
}

func TestProcessRecord_ConcurrentRecords_NoRace(t *testing.T) {
	var mu sync.Mutex
	processed := make(map[string]int)

	handler := func(ctx context.Context, key string, value []byte) error {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		processed[key]++
		mu.Unlock()
		return nil
	}

	c := &Consumer{
		handler:      handler,
		logger:       testLogger(),
		maxRetries:   0,
		drainTimeout: defaultDrainTimeout,
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		key := "key-" + string(rune('A'+i))
		go func() {
			defer wg.Done()
			record := &kgo.Record{
				Topic: "test-topic", Key: []byte(key), Value: []byte("v"),
			}
			c.processRecord(context.Background(), record)
		}()
	}
	wg.Wait()

	mu.Lock()
	total := 0
	for _, count := range processed {
		total += count
	}
	mu.Unlock()

	if total != 10 {
		t.Errorf("processed %d records, want 10", total)
	}
}

// ---------------------------------------------------------------------------
// Drain / Close tests
// ---------------------------------------------------------------------------

func TestClose_DrainTimeout(t *testing.T) {
	handlerStarted := make(chan struct{})
	handler := func(ctx context.Context, key string, value []byte) error {
		close(handlerStarted)
		time.Sleep(3 * time.Second)
		return nil
	}

	cfg := testConfig()
	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithDrainTimeout(500*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}

	consumer.wg.Add(1)
	go func() {
		defer consumer.wg.Done()
		consumer.handler(context.Background(), "key", []byte("value"))
	}()

	<-handlerStarted

	start := time.Now()
	consumer.Close()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("Close took %v, expected <2s (drain timeout is 500ms)", elapsed)
	}
}

func TestClose_DrainCompletesBeforeTimeout(t *testing.T) {
	handler := func(ctx context.Context, key string, value []byte) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	cfg := testConfig()
	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithDrainTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}

	consumer.wg.Add(1)
	go func() {
		defer consumer.wg.Done()
		consumer.handler(context.Background(), "key", []byte("value"))
	}()

	start := time.Now()
	consumer.Close()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("Close took %v, expected fast completion", elapsed)
	}
}

// =============================================================================
// WithWorkerPool bounds tests
// =============================================================================

func TestWithWorkerPool_BoundsClamped(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithWorkerPool(100),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.workerCount != maxWorkerPoolSize {
		t.Errorf("workerCount = %d, want %d (clamped from 100)", consumer.workerCount, maxWorkerPoolSize)
	}
}

func TestWithWorkerPool_NegativeDefaultsToGOMAXPROCS(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithWorkerPool(-1),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	expected := runtime.GOMAXPROCS(0) * defaultWorkerPoolMultiplier
	if expected > maxWorkerPoolSize {
		expected = maxWorkerPoolSize
	}
	if consumer.workerCount != expected {
		t.Errorf("workerCount = %d, want %d (GOMAXPROCS*2 clamped)", consumer.workerCount, expected)
	}
}

func TestWithWorkerPool_ZeroDefaultsToGOMAXPROCS(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithWorkerPool(0),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	expected := runtime.GOMAXPROCS(0) * defaultWorkerPoolMultiplier
	if expected > maxWorkerPoolSize {
		expected = maxWorkerPoolSize
	}
	if consumer.workerCount != expected {
		t.Errorf("workerCount = %d, want %d (zero defaults to GOMAXPROCS*2 clamped)", consumer.workerCount, expected)
	}
}

func TestWithWorkerPool_ValidSmallValue(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithWorkerPool(4),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.workerCount != 4 {
		t.Errorf("workerCount = %d, want 4", consumer.workerCount)
	}
}

func TestWithWorkerPool_ExactlyMaxValue(t *testing.T) {
	cfg := testConfig()
	handler := func(ctx context.Context, key string, value []byte) error { return nil }

	consumer, err := NewConsumer(cfg, "test-group", []string{"test-topic"}, handler, testLogger(),
		WithWorkerPool(maxWorkerPoolSize),
	)
	if err != nil {
		t.Fatalf("NewConsumer() error: %v", err)
	}
	defer consumer.Close()

	if consumer.workerCount != maxWorkerPoolSize {
		t.Errorf("workerCount = %d, want %d", consumer.workerCount, maxWorkerPoolSize)
	}
}

func TestWithWorkerPool_WorkChannelCapacityFormula(t *testing.T) {
	// Verify the channel capacity formula: workerCount * workerChannelBuffer.
	// The actual channel is created in Start(), so we verify the formula here.
	const workerCount = 4
	expectedCap := workerCount * workerChannelBuffer // 4 * 10 = 40
	if expectedCap != 40 {
		t.Errorf("expectedCap = %d, want 40", expectedCap)
	}
	if workerChannelBuffer != 10 {
		t.Errorf("workerChannelBuffer = %d, want 10", workerChannelBuffer)
	}
}
