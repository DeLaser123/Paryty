package twin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"github.com/redis/go-redis/v9"
)

const (
	// routingCacheTTL is how long cached routing entries remain valid.
	routingCacheTTL = 5 * time.Minute

	// routingKeyPrefix is the Dragonfly hash key prefix for routing data.
	routingKeyPrefix = "paryty:routing"
)

// RoutingCache provides hot-path topic resolution for agent-to-twin routing.
// It uses Dragonfly (Redis-compatible) for sub-millisecond lookups with
// PostgreSQL fallback on cache misses.
type RoutingCache struct {
	rdb      *redis.Client
	assigner *AgentAssigner
	logger   *slog.Logger
}

// NewRoutingCache creates a new RoutingCache.
func NewRoutingCache(rdb *redis.Client, assigner *AgentAssigner, logger *slog.Logger) *RoutingCache {
	return &RoutingCache{
		rdb:      rdb,
		assigner: assigner,
		logger:   logger,
	}
}

// routingKey returns the Dragonfly hash key for a tenant's routing table.
func routingKey(tenant string) string {
	return fmt.Sprintf("%s:%s", routingKeyPrefix, tenant)
}

// ResolveTopic returns the Redpanda topic for an agent's data.
// If the agent is assigned to a twin, returns the twin-scoped topic.
// If unassigned, returns the orphan topic.
//
// Resolution order:
//  1. Dragonfly HGET (sub-ms)
//  2. PostgreSQL fallback on cache miss
//  3. Cache the result for future lookups
func (rc *RoutingCache) ResolveTopic(ctx context.Context, tenant, agentID string) string {
	key := routingKey(tenant)

	// Try Dragonfly cache first.
	twinID, err := rc.rdb.HGet(ctx, key, agentID).Result()
	if err == nil && twinID != "" {
		// Cache hit — return twin-scoped topic.
		return stream.TopicTwinMetricsRaw(tenant, twinID)
	}

	// Cache miss — fallback to PostgreSQL.
	assignment, err := rc.assigner.GetAgentAssignment(ctx, agentID)
	if err != nil {
		rc.logger.Warn("routing fallback failed, using orphan topic",
			"agent_id", agentID,
			"tenant", tenant,
			"error", err,
		)
		return stream.TopicOrphan(tenant)
	}

	if assignment == nil {
		// Agent is unassigned — cache empty string and return orphan topic.
		if cacheErr := rc.rdb.HSet(ctx, key, agentID, "").Err(); cacheErr != nil {
			rc.logger.Warn("failed to cache unassigned routing", "error", cacheErr)
		}
		rc.rdb.Expire(ctx, key, routingCacheTTL)
		return stream.TopicOrphan(tenant)
	}

	// Cache the twin_id for future lookups.
	if cacheErr := rc.rdb.HSet(ctx, key, agentID, assignment.TwinID).Err(); cacheErr != nil {
		rc.logger.Warn("failed to cache routing", "error", cacheErr)
	}
	rc.rdb.Expire(ctx, key, routingCacheTTL)

	return stream.TopicTwinMetricsRaw(tenant, assignment.TwinID)
}

// ResolveTwinID returns the twin ID for an agent, or empty string if unassigned.
// Uses the same caching logic as ResolveTopic but returns the twin ID directly.
func (rc *RoutingCache) ResolveTwinID(ctx context.Context, tenant, agentID string) string {
	key := routingKey(tenant)

	// Try Dragonfly cache first.
	twinID, err := rc.rdb.HGet(ctx, key, agentID).Result()
	if err == nil {
		return twinID // may be empty string for unassigned
	}

	// Cache miss — fallback to PostgreSQL.
	assignment, err := rc.assigner.GetAgentAssignment(ctx, agentID)
	if err != nil {
		rc.logger.Warn("routing fallback failed", "agent_id", agentID, "error", err)
		return ""
	}

	if assignment == nil {
		// Agent is unassigned — cache empty string.
		if cacheErr := rc.rdb.HSet(ctx, key, agentID, "").Err(); cacheErr != nil {
			rc.logger.Warn("failed to cache unassigned routing", "error", cacheErr)
		}
		rc.rdb.Expire(ctx, key, routingCacheTTL)
		return ""
	}

	// Cache the twin_id for future lookups.
	if cacheErr := rc.rdb.HSet(ctx, key, agentID, assignment.TwinID).Err(); cacheErr != nil {
		rc.logger.Warn("failed to cache routing", "error", cacheErr)
	}
	rc.rdb.Expire(ctx, key, routingCacheTTL)

	return assignment.TwinID
}

// InvalidateAgent removes a specific agent's routing cache entry.
// Called after assignment changes (assign, unassign, unpair).
func (rc *RoutingCache) InvalidateAgent(ctx context.Context, tenant, agentID string) {
	key := routingKey(tenant)
	if err := rc.rdb.HDel(ctx, key, agentID).Err(); err != nil {
		rc.logger.Warn("failed to invalidate routing cache",
			"agent_id", agentID,
			"error", err,
		)
	}
}

// InvalidateTenant removes all cached routing entries for a tenant.
// Called when a twin is deleted (affects multiple agents).
func (rc *RoutingCache) InvalidateTenant(ctx context.Context, tenant string) {
	key := routingKey(tenant)
	if err := rc.rdb.Del(ctx, key).Err(); err != nil {
		rc.logger.Warn("failed to invalidate tenant routing cache",
			"tenant", tenant,
			"error", err,
		)
	}
}
