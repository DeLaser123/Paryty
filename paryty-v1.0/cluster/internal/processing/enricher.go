// Package processing implements the data processing pipeline.
// This file implements the enricher stage, which injects agent metadata,
// label propagation, service name resolution, and processing context
// into metrics, aggregated metrics, and topology changes.
package processing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	defaultMaxLabelsPerMetric = 20
	defaultLabelPrefix        = "agent."
	defaultAgentCacheSize     = 1024
	defaultAgentCacheTTL      = 5 * time.Minute

	// pipelineVersion is the version tag added to every enriched metric.
	pipelineVersion = "1.0.0"

	// agentStateKeyFmt is the Dragonfly key format for agent state lookups.
	agentStateKeyFmt = "paryty:agent:%s:state"
)

// EnricherConfig holds tunable parameters for the Enricher.
type EnricherConfig struct {
	// StandardLabelKeys are the agent label keys that are always included
	// in enrichment (highest priority). Default: env, region, team, service, version.
	StandardLabelKeys []string

	// MaxLabelsPerMetric is the maximum number of labels applied to a single
	// metric. Standard labels have priority; excess labels are dropped. Default: 20.
	MaxLabelsPerMetric int

	// LabelPrefix is the prefix applied to agent-sourced labels. Default: "agent."
	LabelPrefix string
}

// DefaultEnricherConfig returns an EnricherConfig with production defaults.
func DefaultEnricherConfig() EnricherConfig {
	return EnricherConfig{
		StandardLabelKeys:  []string{"env", "region", "team", "service", "version"},
		MaxLabelsPerMetric: defaultMaxLabelsPerMetric,
		LabelPrefix:        defaultLabelPrefix,
	}
}

// AgentCache is an LRU cache with TTL for agent state lookups.
// Cache hits return immediately; cache misses query Dragonfly and populate
// the cache. Unknown agents return a minimal AgentInfo with just the ID.
//
// Thread-safe. All public methods are safe for concurrent use.
type AgentCache struct {
	cache     *lru.Cache[string, *models.AgentInfo]
	dragonfly DragonflyClient
	ttl       time.Duration
	mu        sync.RWMutex // protects ttls map
	ttls      map[string]time.Time
}

// NewAgentCache creates a new AgentCache backed by an LRU of the given size
// and a Dragonfly client for cache misses. Entries expire after ttl.
func NewAgentCache(size int, ttl time.Duration, dragonfly DragonflyClient) *AgentCache {
	cache, _ := lru.New[string, *models.AgentInfo](size)
	return &AgentCache{
		cache:     cache,
		dragonfly: dragonfly,
		ttl:       ttl,
		ttls:      make(map[string]time.Time),
	}
}

// Get retrieves agent info by ID. Returns cached data on hit; queries
// Dragonfly on miss. Returns a minimal AgentInfo when the agent is unknown.
func (ac *AgentCache) Get(ctx context.Context, agentID string) (*models.AgentInfo, error) {
	// Check LRU cache first.
	if info, ok := ac.cache.Get(agentID); ok {
		ac.mu.RLock()
		expiry, exists := ac.ttls[agentID]
		ac.mu.RUnlock()
		if exists && time.Now().Before(expiry) {
			return info, nil
		}
		// Entry expired — fall through to Dragonfly lookup.
	}

	// Cache miss or expired — query Dragonfly.
	if ac.dragonfly == nil {
		return ac.minimalAgentInfo(agentID), nil
	}

	key := fmt.Sprintf(agentStateKeyFmt, agentID)
	data, err := ac.dragonfly.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ac.minimalAgentInfo(agentID), nil
		}
		return nil, fmt.Errorf("get agent state from dragonfly (key=%s): %w", key, err)
	}

	var info models.AgentInfo
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		return nil, fmt.Errorf("unmarshal agent state (key=%s): %w", key, err)
	}

	// Populate cache with fresh TTL.
	ac.cache.Add(agentID, &info)
	ac.mu.Lock()
	ac.ttls[agentID] = time.Now().Add(ac.ttl)
	ac.mu.Unlock()

	return &info, nil
}

// minimalAgentInfo returns a minimal AgentInfo for unknown agents.
func (ac *AgentCache) minimalAgentInfo(agentID string) *models.AgentInfo {
	return &models.AgentInfo{
		ID:     agentID,
		Labels: make(map[string]string),
	}
}

// Enricher enriches metrics, aggregated metrics, and topology changes with
// agent metadata, label propagation, service name resolution, and processing
// context. Agent state is cached via an LRU with TTL.
//
// Thread-safe. All public methods are safe for concurrent use.
type Enricher struct {
	config     EnricherConfig
	agentCache *AgentCache
	logger     *zap.Logger
}

// NewEnricher creates a new enricher with the given configuration and
// Dragonfly client for agent state lookups. If logger is nil, a no-op
// logger is used. dragonfly may be nil for testing.
func NewEnricher(config EnricherConfig, dragonfly DragonflyClient, logger *zap.Logger) *Enricher {
	if logger == nil {
		logger = zap.NewNop()
	}

	if config.MaxLabelsPerMetric == 0 {
		config.MaxLabelsPerMetric = defaultMaxLabelsPerMetric
	}
	if config.LabelPrefix == "" {
		config.LabelPrefix = defaultLabelPrefix
	}
	if len(config.StandardLabelKeys) == 0 {
		config.StandardLabelKeys = []string{"env", "region", "team", "service", "version"}
	}

	cache := NewAgentCache(defaultAgentCacheSize, defaultAgentCacheTTL, dragonfly)

	return &Enricher{
		config:     config,
		agentCache: cache,
		logger:     logger,
	}
}

// EnrichBatch enriches a metric batch with agent labels and processing metadata.
//
// Steps:
//  1. Look up agent info from cache (or Dragonfly).
//  2. Inject standard labels (env, region, team, etc.) with prefix.
//  3. Propagate service name from correlation result.
//  4. Add processing metadata (pipeline_version, enriched_at).
//  5. Enforce MaxLabelsPerMetric limit.
//  6. Enrich all metric types in batch (stamp AgentID).
func (e *Enricher) EnrichBatch(ctx context.Context, batch *models.MetricBatch, correlationResult *CorrelationResult) (*models.MetricBatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("enricher: context cancelled: %w", err)
	}

	// Step 1: Look up agent info from cache.
	agentInfo, err := e.getAgentInfo(ctx, batch.AgentID)
	if err != nil {
		return nil, fmt.Errorf("enricher: get agent info for batch: %w", err)
	}

	// Steps 2–5: Build merged labels with cardinality enforcement.
	extraLabels := make(map[string]string)
	if correlationResult != nil {
		for _, serviceName := range correlationResult.ProcessServices {
			if serviceName != "" {
				extraLabels["service.name"] = serviceName
				break // Use first non-empty service name.
			}
		}
	}
	extraLabels["pipeline.version"] = pipelineVersion
	extraLabels["pipeline.enriched_at"] = time.Now().UTC().Format(time.RFC3339)

	labels := e.buildLabels(agentInfo, extraLabels)

	// Step 6: Enrich all metric types — stamp AgentID.
	enriched := *batch

	for i := range enriched.CPU {
		enriched.CPU[i].AgentID = agentInfo.ID
	}
	for i := range enriched.Memory {
		enriched.Memory[i].AgentID = agentInfo.ID
	}
	for i := range enriched.Disk {
		enriched.Disk[i].AgentID = agentInfo.ID
	}
	for i := range enriched.Network {
		enriched.Network[i].AgentID = agentInfo.ID
	}
	for i := range enriched.Processes {
		enriched.Processes[i].AgentID = agentInfo.ID
	}
	for i := range enriched.Containers {
		enriched.Containers[i].AgentID = agentInfo.ID
	}

	e.logger.Debug("enriched batch",
		zap.String("agent_id", batch.AgentID),
		zap.String("hostname", agentInfo.Hostname),
		zap.Int("labels_applied", len(labels)),
	)

	return &enriched, nil
}

// EnrichAggregated enriches aggregated metrics with agent labels.
// Each metric's Labels map is populated with the agent's merged labels,
// subject to the MaxLabelsPerMetric cardinality limit.
//
// The input slice is never mutated; a new slice with deep-copied Labels
// maps is returned.
func (e *Enricher) EnrichAggregated(ctx context.Context, metrics []models.AggregatedMetric, agentID string) ([]models.AggregatedMetric, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("enricher: context cancelled: %w", err)
	}

	agentInfo, err := e.getAgentInfo(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("enricher: get agent info for aggregated metrics: %w", err)
	}

	labels := e.buildLabels(agentInfo, nil)

	result := make([]models.AggregatedMetric, len(metrics))
	for i, m := range metrics {
		enriched := m
		// Always create a new map to avoid mutating the input Labels.
		newLabels := make(map[string]string, len(m.Labels)+len(labels))
		for k, v := range m.Labels {
			newLabels[k] = v
		}
		for k, v := range labels {
			newLabels[k] = v
		}
		enriched.Labels = newLabels
		result[i] = enriched
	}

	return result, nil
}

// EnrichTopology enriches a slice of graph changes with agent labels.
// Labels are applied to deep copies of the Node or Edge within each change,
// so the original changes are never mutated.
func (e *Enricher) EnrichTopology(ctx context.Context, changes []GraphChange, agentID string) ([]GraphChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("enricher: context cancelled: %w", err)
	}

	agentInfo, err := e.getAgentInfo(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("enricher: get agent info for topology changes: %w", err)
	}

	labels := e.buildLabels(agentInfo, nil)

	result := make([]GraphChange, len(changes))
	for i, change := range changes {
		enriched := change

		if enriched.Node != nil {
			// Deep-copy the node to avoid mutating the original.
			nodeCopy := *enriched.Node
			newLabels := make(map[string]string, len(nodeCopy.Labels)+len(labels))
			for k, v := range nodeCopy.Labels {
				newLabels[k] = v
			}
			for k, v := range labels {
				newLabels[k] = v
			}
			nodeCopy.Labels = newLabels
			enriched.Node = &nodeCopy
		}

		if enriched.Edge != nil {
			// Deep-copy the edge to avoid mutating the original.
			edgeCopy := *enriched.Edge
			newLabels := make(map[string]string, len(edgeCopy.Labels)+len(labels))
			for k, v := range edgeCopy.Labels {
				newLabels[k] = v
			}
			for k, v := range labels {
				newLabels[k] = v
			}
			edgeCopy.Labels = newLabels
			enriched.Edge = &edgeCopy
		}

		result[i] = enriched
	}

	return result, nil
}

// EnrichTopologyNode enriches a single topology node with agent metadata.
// This is the original single-node enrichment method retained for backward
// compatibility with topology node-level enrichment.
func (e *Enricher) EnrichTopologyNode(ctx context.Context, node *models.TopologyNode, agentInfo *models.AgentInfo) *models.TopologyNode {
	enriched := *node

	if enriched.Labels == nil {
		enriched.Labels = make(map[string]string)
	}
	if enriched.Metadata == nil {
		enriched.Metadata = make(map[string]string)
	}

	// Add agent metadata.
	enriched.Labels["agent_id"] = agentInfo.ID
	enriched.Labels["hostname"] = agentInfo.Hostname

	// Add host metadata.
	enriched.Metadata["os"] = agentInfo.OS
	enriched.Metadata["arch"] = agentInfo.Arch
	enriched.Metadata["agent_version"] = agentInfo.AgentVersion

	// Add environment labels.
	for k, v := range agentInfo.Labels {
		enriched.Labels[k] = v
	}

	return &enriched
}

// EnrichMetric enriches a single metric with the given labels.
func (e *Enricher) EnrichMetric(ctx context.Context, metric *models.Metric, labels map[string]string) *models.Metric {
	enriched := *metric

	if enriched.Labels == nil {
		enriched.Labels = make(map[string]string)
	}

	// Merge labels.
	for k, v := range labels {
		enriched.Labels[k] = v
	}

	return &enriched
}

// EnrichSpan enriches a trace span with service metadata.
func (e *Enricher) EnrichSpan(ctx context.Context, span *models.Span, agentInfo *models.AgentInfo) *models.Span {
	enriched := *span

	if enriched.Attributes == nil {
		enriched.Attributes = make(map[string]string)
	}

	// Add agent metadata.
	enriched.Attributes["agent.id"] = agentInfo.ID
	enriched.Attributes["agent.hostname"] = agentInfo.Hostname
	enriched.Attributes["agent.os"] = agentInfo.OS
	enriched.Attributes["agent.arch"] = agentInfo.Arch

	// Add environment labels.
	for k, v := range agentInfo.Labels {
		enriched.Attributes["label."+k] = v
	}

	return &enriched
}

// EnrichAlert enriches an alert with agent metadata.
func (e *Enricher) EnrichAlert(ctx context.Context, alert *models.Alert, agentInfo *models.AgentInfo) *models.Alert {
	enriched := *alert

	if enriched.Labels == nil {
		enriched.Labels = make(map[string]string)
	}

	// Add agent metadata.
	enriched.Labels["agent_id"] = agentInfo.ID
	enriched.Labels["hostname"] = agentInfo.Hostname

	// Add environment labels.
	for k, v := range agentInfo.Labels {
		enriched.Labels[k] = v
	}

	return &enriched
}

// ─── internal helpers ─────────────────────────────────────────────────────

// getAgentInfo retrieves agent info from the LRU cache or Dragonfly.
func (e *Enricher) getAgentInfo(ctx context.Context, agentID string) (*models.AgentInfo, error) {
	return e.agentCache.Get(ctx, agentID)
}

// buildLabels merges standard agent labels, extra labels (processing metadata,
// correlation metadata), and remaining agent labels into a single map, applying
// the LabelPrefix to agent-sourced keys and enforcing MaxLabelsPerMetric.
//
// Priority order (highest first):
//  1. Standard labels from agent registration (always included).
//  2. Extra labels (processing metadata, correlation metadata).
//  3. All remaining agent labels.
//
// Labels are added in priority order until MaxLabelsPerMetric is reached.
func (e *Enricher) buildLabels(agentInfo *models.AgentInfo, extraLabels map[string]string) map[string]string {
	prefix := e.config.LabelPrefix
	maxLabels := e.config.MaxLabelsPerMetric

	agentLabels := make(map[string]string)
	if agentInfo != nil && agentInfo.Labels != nil {
		agentLabels = agentInfo.Labels
	}

	result := make(map[string]string)

	// Track which keys are standard for priority enforcement.
	standardSet := make(map[string]bool, len(e.config.StandardLabelKeys))
	for _, key := range e.config.StandardLabelKeys {
		standardSet[key] = true
	}

	// Priority 1: Standard agent labels (always included, highest priority).
	for _, key := range e.config.StandardLabelKeys {
		if val, ok := agentLabels[key]; ok {
			result[prefix+key] = val
		}
	}

	// Priority 2: Extra labels (processing metadata, correlation).
	for k, v := range extraLabels {
		if len(result) >= maxLabels {
			break
		}
		result[k] = v
	}

	// Priority 3: Remaining agent labels (non-standard, lowest priority).
	for k, v := range agentLabels {
		if standardSet[k] {
			continue // Already added at priority 1.
		}
		if len(result) >= maxLabels {
			break
		}
		result[prefix+k] = v
	}

	return result
}
