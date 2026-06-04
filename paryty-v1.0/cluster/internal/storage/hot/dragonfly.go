// Package hot implements the hot storage tier using Dragonfly (Redis-compatible).
// This tier stores the most recent data (last 5 minutes) for real-time queries.
package hot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
)

const (
	// defaultTTL is the fallback TTL when Config.TTL is zero.
	defaultTTL = 5 * time.Minute

	// scanCount is the COUNT hint for SCAN iterations.
	scanCount = 100
)

// ---- Tenant-scoped key generation ----
//
// Every key is namespaced as paryty:<tenant>:<domain>:<id> to guarantee
// strict tenant isolation in shared Dragonfly instances.

// metricsKey returns the key for the latest metrics of an agent.
func metricsKey(tenant, agentID string) string {
	return fmt.Sprintf("paryty:%s:metrics:%s:latest", tenant, agentID)
}

// agentStateKey returns the key for an agent's registration state.
func agentStateKey(tenant, agentID string) string {
	return fmt.Sprintf("paryty:%s:agent:%s", tenant, agentID)
}

// topologyKey returns the key for the current topology graph.
func topologyKey(tenant string) string {
	return fmt.Sprintf("paryty:%s:topology:current", tenant)
}

// alertsKey returns the key for active alerts.
func alertsKey(tenant string) string {
	return fmt.Sprintf("paryty:%s:alerts:active", tenant)
}

// healthKey returns the key for an agent's health report.
func healthKey(tenant, agentID string) string {
	return fmt.Sprintf("paryty:%s:health:%s", tenant, agentID)
}

// agentStatePattern returns the SCAN pattern for all agent keys of a tenant.
func agentStatePattern(tenant string) string {
	return fmt.Sprintf("paryty:%s:agent:*", tenant)
}

// Config contains configuration for the Dragonfly client.
type Config struct {
	Addr     string        `yaml:"addr" json:"addr"`
	Password string        `yaml:"password" json:"password"`
	DB       int           `yaml:"db" json:"db"`
	PoolSize int           `yaml:"pool_size" json:"pool_size"`
	TTL      time.Duration `yaml:"ttl" json:"ttl"`
}

// Client is the Dragonfly hot storage client.
type Client struct {
	rdb *redis.Client
	cfg Config
}

// New creates a new Dragonfly client.
// If cfg.TTL is zero, it defaults to 5 minutes.
func New(cfg Config) *Client {
	if cfg.TTL == 0 {
		cfg.TTL = defaultTTL
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})

	return &Client{
		rdb: rdb,
		cfg: cfg,
	}
}

// Close closes the Dragonfly connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// RDB returns the underlying redis.Client for direct access.
// Used by Phase 4 components (AlertOps, SnapshotManager) that need
// the raw redis.Cmdable interface.
func (c *Client) RDB() *redis.Client {
	return c.rdb
}

// Ping checks the Dragonfly connection and logs connection pool statistics.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return err
	}

	stats := c.rdb.PoolStats()
	slog.Info("dragonfly pool stats",
		"hits", stats.Hits,
		"misses", stats.Misses,
		"timeouts", stats.Timeouts,
		"total_conns", stats.TotalConns,
		"idle_conns", stats.IdleConns,
		"stale_conns", stats.StaleConns,
	)

	return nil
}

// ---- Topology Operations ----

// SetTopology stores the current topology for a tenant.
func (c *Client) SetTopology(ctx context.Context, tenant string, topo *models.Topology) error {
	data, err := json.Marshal(topo)
	if err != nil {
		return fmt.Errorf("marshal topology: %w", err)
	}
	return c.rdb.Set(ctx, topologyKey(tenant), data, c.cfg.TTL).Err()
}

// GetTopology retrieves the current topology for a tenant.
func (c *Client) GetTopology(ctx context.Context, tenant string) (*models.Topology, error) {
	data, err := c.rdb.Get(ctx, topologyKey(tenant)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get topology: %w", err)
	}
	var topo models.Topology
	if err := json.Unmarshal(data, &topo); err != nil {
		return nil, fmt.Errorf("unmarshal topology: %w", err)
	}
	return &topo, nil
}

// ---- Metrics Operations ----

// SetLatestMetrics stores the latest metrics for a tenant's agent.
func (c *Client) SetLatestMetrics(ctx context.Context, tenant, agentID string, batch *models.MetricBatch) error {
	data, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("marshal metrics: %w", err)
	}
	return c.rdb.Set(ctx, metricsKey(tenant, agentID), data, c.cfg.TTL).Err()
}

// GetLatestMetrics retrieves the latest metrics for a tenant's agent.
func (c *Client) GetLatestMetrics(ctx context.Context, tenant, agentID string) (*models.MetricBatch, error) {
	data, err := c.rdb.Get(ctx, metricsKey(tenant, agentID)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get metrics: %w", err)
	}
	var batch models.MetricBatch
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, fmt.Errorf("unmarshal metrics: %w", err)
	}
	return &batch, nil
}

// SetLatestMetricsBatch stores metrics for multiple agents in a single MSET.
// The batches map keys are agent IDs; values are *models.MetricBatch.
// This is more efficient than individual Set calls for bulk updates.
func (c *Client) SetLatestMetricsBatch(ctx context.Context, tenant string, batches map[string]*models.MetricBatch) error {
	if len(batches) == 0 {
		return nil
	}

	// Build key-value pairs for MSet: [key1, val1, key2, val2, ...]
	args := make([]interface{}, 0, len(batches)*2)
	for agentID, batch := range batches {
		data, err := json.Marshal(batch)
		if err != nil {
			return fmt.Errorf("marshal metrics for agent %s: %w", agentID, err)
		}
		args = append(args, metricsKey(tenant, agentID), data)
	}

	if err := c.rdb.MSet(ctx, args...).Err(); err != nil {
		return fmt.Errorf("mset metrics: %w", err)
	}

	// MSet does not support per-key TTL, so we must set TTL on each key.
	// Use a pipeline to batch the EXPIRE commands.
	pipe := c.rdb.Pipeline()
	for agentID := range batches {
		pipe.Expire(ctx, metricsKey(tenant, agentID), c.cfg.TTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("expire metrics: %w", err)
	}

	return nil
}

// ---- Alert Operations ----

// SetActiveAlerts stores the current active alerts for a tenant.
func (c *Client) SetActiveAlerts(ctx context.Context, tenant string, alerts []models.Alert) error {
	data, err := json.Marshal(alerts)
	if err != nil {
		return fmt.Errorf("marshal alerts: %w", err)
	}
	return c.rdb.Set(ctx, alertsKey(tenant), data, c.cfg.TTL).Err()
}

// GetActiveAlerts retrieves the current active alerts for a tenant.
func (c *Client) GetActiveAlerts(ctx context.Context, tenant string) ([]models.Alert, error) {
	data, err := c.rdb.Get(ctx, alertsKey(tenant)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get alerts: %w", err)
	}
	var alerts []models.Alert
	if err := json.Unmarshal(data, &alerts); err != nil {
		return nil, fmt.Errorf("unmarshal alerts: %w", err)
	}
	return alerts, nil
}

// ---- Agent State Operations ----

// SetAgentState stores the state of an agent for a tenant.
func (c *Client) SetAgentState(ctx context.Context, tenant string, agent *models.AgentInfo) error {
	data, err := json.Marshal(agent)
	if err != nil {
		return fmt.Errorf("marshal agent: %w", err)
	}
	return c.rdb.Set(ctx, agentStateKey(tenant, agent.ID), data, c.cfg.TTL).Err()
}

// GetAgentState retrieves the state of an agent for a tenant.
func (c *Client) GetAgentState(ctx context.Context, tenant, agentID string) (*models.AgentInfo, error) {
	data, err := c.rdb.Get(ctx, agentStateKey(tenant, agentID)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	var agent models.AgentInfo
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("unmarshal agent: %w", err)
	}
	return &agent, nil
}

// GetAllAgentStates retrieves all agent states for a tenant using SCAN
// instead of KEYS to avoid blocking Dragonfly on large key spaces.
func (c *Client) GetAllAgentStates(ctx context.Context, tenant string) ([]models.AgentInfo, error) {
	pattern := agentStatePattern(tenant)
	var agents []models.AgentInfo

	var cursor uint64
	for {
		keys, nextCursor, err := c.rdb.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return nil, fmt.Errorf("scan agents: %w", err)
		}

		for _, key := range keys {
			data, err := c.rdb.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			var agent models.AgentInfo
			if err := json.Unmarshal(data, &agent); err != nil {
				continue
			}
			agents = append(agents, agent)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return agents, nil
}

// ---- Health Operations ----

// SetHealthReport stores a health report for a tenant's agent.
func (c *Client) SetHealthReport(ctx context.Context, tenant string, report *models.HealthReport) error {
	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal health: %w", err)
	}
	return c.rdb.Set(ctx, healthKey(tenant, report.AgentID), data, c.cfg.TTL).Err()
}

// GetHealthReport retrieves a health report for a tenant's agent.
func (c *Client) GetHealthReport(ctx context.Context, tenant, agentID string) (*models.HealthReport, error) {
	data, err := c.rdb.Get(ctx, healthKey(tenant, agentID)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("get health: %w", err)
	}
	var report models.HealthReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("unmarshal health: %w", err)
	}
	return &report, nil
}
