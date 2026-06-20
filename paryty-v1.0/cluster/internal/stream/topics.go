package stream

import (
	"context"
	"fmt"
	"strings"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/zap"
)

// DefaultTenant is the tenant identifier used when no tenant context is available.
const DefaultTenant = "default"

// TopicManager manages Redpanda topics.
type TopicManager struct {
	client *kadm.Client
	logger *zap.Logger
	cfg    Config
}

// NewTopicManager creates a new topic manager.
func NewTopicManager(cfg Config, logger *zap.Logger) (*TopicManager, error) {
	client, err := kgo.NewClient(kgo.SeedBrokers(cfg.Brokers...))
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}

	return &TopicManager{
		client: kadm.NewClient(client),
		logger: logger,
		cfg:    cfg,
	}, nil
}

// Close closes the topic manager.
func (m *TopicManager) Close() {
	m.client.Close()
}

// Client returns the underlying kadm.Client for direct admin operations.
// Used by twin lifecycle management for topic creation/deletion.
func (m *TopicManager) Client() *kadm.Client {
	return m.client
}

// CreateTopic creates a topic with the given configuration.
func (m *TopicManager) CreateTopic(ctx context.Context, topic string, partitions int32, replication int16) error {
	_, err := m.client.CreateTopic(ctx, partitions, replication, nil, topic)
	if err != nil {
		return fmt.Errorf("create topic %s: %w", topic, err)
	}

	m.logger.Info("Topic created",
		zap.String("topic", topic),
		zap.Int32("partitions", partitions),
		zap.Int16("replication", replication),
	)

	return nil
}

// DeleteTopic deletes a topic.
func (m *TopicManager) DeleteTopic(ctx context.Context, topic string) error {
	_, err := m.client.DeleteTopics(ctx, topic)
	if err != nil {
		return fmt.Errorf("delete topic %s: %w", topic, err)
	}

	m.logger.Info("Topic deleted", zap.String("topic", topic))
	return nil
}

// ListTopics lists all topics.
func (m *TopicManager) ListTopics(ctx context.Context) ([]string, error) {
	topics, err := m.client.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}

	var names []string
	for name := range topics {
		names = append(names, name)
	}
	return names, nil
}

// EnsureTopic creates a topic if it doesn't exist.
func (m *TopicManager) EnsureTopic(ctx context.Context, topic string, partitions int32, replication int16) error {
	exists, err := m.client.ListTopics(ctx)
	if err != nil {
		return fmt.Errorf("list topics: %w", err)
	}

	if _, ok := exists[topic]; ok {
		return nil
	}

	return m.CreateTopic(ctx, topic, partitions, replication)
}

// topicConfig defines partition and replication settings for a topic.
type topicConfig struct {
	name        string
	partitions  int32
	replication int16
}

// TopicForTenant builds a fully qualified topic name for the given tenant: paryty.<tenant>.<topic>.
func TopicForTenant(tenant, topic string) string {
	return fmt.Sprintf("paryty.%s.%s", tenant, topic)
}

// Tenant-scoped topic name functions.
// Each function produces a namespaced topic name: paryty.<tenant>.<topic>.

func TopicMetricsRaw(tenant string) string      { return fmt.Sprintf("paryty.%s.metrics.raw", tenant) }
func TopicMetricsAgg(tenant string) string      { return fmt.Sprintf("paryty.%s.metrics.aggregated", tenant) }
func TopicTraces(tenant string) string          { return fmt.Sprintf("paryty.%s.traces", tenant) }
func TopicEvents(tenant string) string          { return fmt.Sprintf("paryty.%s.events", tenant) }
func TopicNetworkEvents(tenant string) string   { return fmt.Sprintf("paryty.%s.network.events", tenant) }
func TopicTopologyChanges(tenant string) string { return fmt.Sprintf("paryty.%s.topology.changes", tenant) }
func TopicAlerts(tenant string) string          { return fmt.Sprintf("paryty.%s.alerts", tenant) }
func TopicDLQ(tenant string) string             { return fmt.Sprintf("paryty.%s.dead-letter", tenant) }

// Tenant-scoped topic name functions for intelligence topics (Phase 6).
func TopicForecasts(tenant string) string     { return fmt.Sprintf("paryty.%s.forecasts", tenant) }
func TopicAnomalies(tenant string) string     { return fmt.Sprintf("paryty.%s.anomalies", tenant) }
func TopicSimulations(tenant string) string   { return fmt.Sprintf("paryty.%s.simulations", tenant) }
func TopicTimelineEvents(tenant string) string { return fmt.Sprintf("paryty.%s.timeline.events", tenant) }

// Topic name constants for pipeline stage output topics.
const (
	// TopicMetricsEnriched is the topic suffix for enriched metrics output.
	TopicMetricsEnriched = "metrics.enriched"
	// TopicCorrelations is the topic suffix for correlation events.
	TopicCorrelations = "correlations"
	// TopicDependencyGraph is the topic suffix for dependency graph updates.
	TopicDependencyGraph = "dependency.graph"
)

// Topic name constants for intelligence topics (Phase 6).
const (
	// TopicForecasts is the topic suffix for forecast results.
	TopicForecastsSuffix = "forecasts"
	// TopicAnomalies is the topic suffix for anomaly detection results.
	TopicAnomaliesSuffix = "anomalies"
	// TopicSimulations is the topic suffix for simulation results.
	TopicSimulationsSuffix = "simulations"
	// TopicTimelineEvents is the topic suffix for timeline events.
	TopicTimelineEventsSuffix = "timeline.events"
)

// Default-tenant convenience functions. Use these when tenant context is not yet available.
func DefaultTopicMetricsRaw() string      { return TopicMetricsRaw(DefaultTenant) }
func DefaultTopicMetricsAgg() string      { return TopicMetricsAgg(DefaultTenant) }
func DefaultTopicTraces() string          { return TopicTraces(DefaultTenant) }
func DefaultTopicEvents() string          { return TopicEvents(DefaultTenant) }
func DefaultTopicNetworkEvents() string   { return TopicNetworkEvents(DefaultTenant) }
func DefaultTopicTopologyChanges() string { return TopicTopologyChanges(DefaultTenant) }
func DefaultTopicAlerts() string          { return TopicAlerts(DefaultTenant) }
func DefaultTopicDLQ() string             { return TopicDLQ(DefaultTenant) }

// DefaultTopicMetricsEnriched returns the enriched metrics topic for the default tenant.
func DefaultTopicMetricsEnriched() string { return TopicForTenant(DefaultTenant, TopicMetricsEnriched) }

// DefaultTopicCorrelations returns the correlations topic for the default tenant.
func DefaultTopicCorrelations() string { return TopicForTenant(DefaultTenant, TopicCorrelations) }

// DefaultTopicDependencyGraph returns the dependency graph topic for the default tenant.
func DefaultTopicDependencyGraph() string { return TopicForTenant(DefaultTenant, TopicDependencyGraph) }

// DefaultTopicForecasts returns the forecasts topic for the default tenant.
func DefaultTopicForecasts() string { return TopicForecasts(DefaultTenant) }

// DefaultTopicAnomalies returns the anomalies topic for the default tenant.
func DefaultTopicAnomalies() string { return TopicAnomalies(DefaultTenant) }

// DefaultTopicSimulations returns the simulations topic for the default tenant.
func DefaultTopicSimulations() string { return TopicSimulations(DefaultTenant) }

// DefaultTopicTimelineEvents returns the timeline events topic for the default tenant.
func DefaultTopicTimelineEvents() string { return TopicTimelineEvents(DefaultTenant) }

// requiredTopics returns the topic configurations for a tenant.
func requiredTopics(tenant string) []topicConfig {
	return []topicConfig{
		{TopicMetricsRaw(tenant), 12, 1},
		{TopicMetricsAgg(tenant), 6, 1},
		{TopicTraces(tenant), 12, 1},
		{TopicEvents(tenant), 6, 1},
		{TopicNetworkEvents(tenant), 6, 1},
		{TopicTopologyChanges(tenant), 3, 1},
		{TopicAlerts(tenant), 3, 1},
		{TopicDLQ(tenant), 3, 1},
		{TopicForTenant(tenant, TopicMetricsEnriched), 12, 1},
		{TopicForTenant(tenant, TopicCorrelations), 6, 1},
		{TopicForTenant(tenant, TopicDependencyGraph), 3, 1},
		// Phase 6: Intelligence topics.
		{TopicForecasts(tenant), 3, 1},
		{TopicAnomalies(tenant), 3, 1},
		{TopicSimulations(tenant), 3, 1},
		{TopicTimelineEvents(tenant), 3, 1},
	}
}

// InitializeTopics creates all required topics for a given tenant.
// Uses a single ListTopics call to avoid redundant broker round-trips.
func InitializeTopics(ctx context.Context, admin *kadm.Client, tenant string) error {
	topics := requiredTopics(tenant)

	existing, err := admin.ListTopics(ctx)
	if err != nil {
		return fmt.Errorf("list topics: %w", err)
	}

	for _, tc := range topics {
		if _, ok := existing[tc.name]; ok {
			continue
		}
		_, err := admin.CreateTopic(ctx, tc.partitions, tc.replication, nil, tc.name)
		if err != nil {
			return fmt.Errorf("create topic %s: %w", tc.name, err)
		}
	}

	return nil
}

// ═══════════════════════════════════════════════════════════════════════
// Twin-Scoped Topic Naming (Enterprise Tenant Isolation)
// ═══════════════════════════════════════════════════════════════════════

// TopicForTwin builds a fully qualified twin-scoped topic name:
// paryty.<tenant>.twins.<twin_id_prefix>.<suffix>
//
// The twinID is truncated to 12 characters to keep topic names manageable
// while maintaining uniqueness (UUIDs are 36 chars, 12-char prefix gives
// ~2.8e18 unique values — more than sufficient).
func TopicForTwin(tenant, twinID, suffix string) string {
	prefix := twinID
	if len(twinID) > 12 {
		prefix = twinID[:12]
	}
	return fmt.Sprintf("paryty.%s.twins.%s.%s", tenant, prefix, suffix)
}

// Twin-scoped topic name functions.

func TopicTwinMetricsRaw(tenant, twinID string) string {
	return TopicForTwin(tenant, twinID, "metrics.raw")
}

func TopicTwinMetricsAgg(tenant, twinID string) string {
	return TopicForTwin(tenant, twinID, "metrics.aggregated")
}

func TopicTwinTraces(tenant, twinID string) string {
	return TopicForTwin(tenant, twinID, "traces")
}

func TopicTwinEvents(tenant, twinID string) string {
	return TopicForTwin(tenant, twinID, "events")
}

func TopicTwinNetworkEvents(tenant, twinID string) string {
	return TopicForTwin(tenant, twinID, "network.events")
}

// TopicOrphan returns the topic name for unassigned agent data.
// Data written here is accepted but not twin-isolated.
func TopicOrphan(tenant string) string {
	return fmt.Sprintf("paryty.%s.unassigned.orphan", tenant)
}

// twinRequiredTopics returns the topic configurations for a specific twin.
// Only the 5 core data-producing topics are created per twin.
// Pipeline-internal topics remain tenant-scoped to avoid topic explosion.
func twinRequiredTopics(tenant, twinID string) []topicConfig {
	return []topicConfig{
		{TopicTwinMetricsRaw(tenant, twinID), 6, 1},
		{TopicTwinMetricsAgg(tenant, twinID), 3, 1},
		{TopicTwinTraces(tenant, twinID), 6, 1},
		{TopicTwinEvents(tenant, twinID), 3, 1},
		{TopicTwinNetworkEvents(tenant, twinID), 3, 1},
	}
}

// InitializeTwinTopics creates all required topics for a specific twin.
// Idempotent — skips topics that already exist.
func InitializeTwinTopics(ctx context.Context, admin *kadm.Client, tenant, twinID string) error {
	topics := twinRequiredTopics(tenant, twinID)

	existing, err := admin.ListTopics(ctx)
	if err != nil {
		return fmt.Errorf("list topics: %w", err)
	}

	for _, tc := range topics {
		if _, ok := existing[tc.name]; ok {
			continue
		}
		_, err := admin.CreateTopic(ctx, tc.partitions, tc.replication, nil, tc.name)
		if err != nil {
			return fmt.Errorf("create twin topic %s: %w", tc.name, err)
		}
	}

	return nil
}

// DeleteTwinTopics deletes all topics associated with a specific twin.
// Best-effort — logs errors but does not fail if individual deletions fail.
func DeleteTwinTopics(ctx context.Context, admin *kadm.Client, tenant, twinID string) error {
	topics := twinRequiredTopics(tenant, twinID)
	topicNames := make([]string, 0, len(topics))
	for _, tc := range topics {
		topicNames = append(topicNames, tc.name)
	}

	if len(topicNames) == 0 {
		return nil
	}

	_, err := admin.DeleteTopics(ctx, topicNames...)
	if err != nil {
		if !strings.Contains(err.Error(), "UNKNOWN_TOPIC") {
			return fmt.Errorf("delete twin topics: %w", err)
		}
	}

	return nil
}

// IsTwinTopic returns true if the given topic name matches the twin-scoped
// naming pattern: paryty.<tenant>.twins.<twin_prefix>.<suffix>.
func IsTwinTopic(topic string) bool {
	parts := strings.Split(topic, ".")
	return len(parts) >= 4 && parts[2] == "twins"
}

// ExtractTwinPrefix extracts the 12-char twin ID prefix from a twin-scoped
// topic name. Returns empty string if the topic is not twin-scoped.
func ExtractTwinPrefix(topic string) string {
	parts := strings.Split(topic, ".")
	if len(parts) >= 4 && parts[2] == "twins" {
		return parts[3]
	}
	return ""
}
