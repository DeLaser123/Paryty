// Package hot — connection_ops.go implements agent connection tracking in Dragonfly.
//
// Connections are stored as JSON with a 30-second TTL. Agents must refresh their
// heartbeat within the TTL window or the key expires automatically, providing
// implicit disconnect detection without requiring explicit cleanup.
package hot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// connectionTTL is the TTL for connection keys. Agents must refresh
	// their heartbeat within this window or the key expires automatically.
	connectionTTL = 30 * time.Second

	// defaultDisconnectThreshold is the default duration after which an agent
	// is considered disconnected if no heartbeat has been received.
	defaultDisconnectThreshold = 60 * time.Second

	// connectionScanCount is the COUNT hint for SCAN iterations over connection keys.
	connectionScanCount = 100
)

// ConnectionInfo represents an active agent connection stored in Dragonfly.
type ConnectionInfo struct {
	AgentID       string    `json:"agent_id"`
	TenantID      string    `json:"tenant_id"`
	SessionID     string    `json:"session_id"`
	RemoteAddr    string    `json:"remote_addr"`
	ConnectedAt   time.Time `json:"connected_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Capabilities  []string  `json:"capabilities"`
}

// ConnectionOps provides operations for tracking agent connections in Dragonfly.
// All operations are tenant-scoped to maintain strict tenant isolation.
type ConnectionOps struct {
	client *Client
	logger *zap.Logger
}

// NewConnectionOps creates a new ConnectionOps backed by the given Dragonfly client.
func NewConnectionOps(client *Client, logger *zap.Logger) *ConnectionOps {
	return &ConnectionOps{
		client: client,
		logger: logger,
	}
}

// ---- Key Generation ----

// connectionKey returns the Dragonfly key for an agent's connection.
// Format: paryty:{tenant}:connections:{agent_id}
func connectionKey(tenant, agentID string) string {
	return fmt.Sprintf("paryty:%s:connections:%s", tenant, agentID)
}

// connectionPattern returns the SCAN pattern for all connection keys of a tenant.
func connectionPattern(tenant string) string {
	return fmt.Sprintf("paryty:%s:connections:*", tenant)
}

// ---- Connection Operations ----

// TrackConnection stores a new connection in Dragonfly with a 30-second TTL.
// If the agent already has a connection, it is overwritten (idempotent).
// The tenant field on info is always set to the provided tenant to prevent
// cross-tenant data corruption.
func (co *ConnectionOps) TrackConnection(ctx context.Context, tenant string, info *ConnectionInfo) error {
	if tenant == "" {
		return fmt.Errorf("tenant is required")
	}
	if info == nil {
		return fmt.Errorf("connection info is required")
	}
	if info.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}

	// Enforce tenant isolation: always stamp the caller's tenant.
	info.TenantID = tenant

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal connection info: %w", err)
	}

	key := connectionKey(tenant, info.AgentID)
	if err := co.client.rdb.Set(ctx, key, data, connectionTTL).Err(); err != nil {
		return fmt.Errorf("set connection %s: %w", key, err)
	}

	co.logger.Info("tracked connection",
		zap.String("tenant", tenant),
		zap.String("agent_id", info.AgentID),
		zap.String("session_id", info.SessionID),
	)

	return nil
}

// RefreshHeartbeat updates the last_heartbeat timestamp for an agent and
// extends the TTL to 30 seconds. Uses GET + SET to read the current info,
// update the heartbeat, and write it back atomically-enough for our use case.
//
// Returns an error if the connection does not exist (key expired or never tracked).
func (co *ConnectionOps) RefreshHeartbeat(ctx context.Context, tenant, agentID string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is required")
	}
	if agentID == "" {
		return fmt.Errorf("agent_id is required")
	}

	key := connectionKey(tenant, agentID)

	// Read current connection info.
	data, err := co.client.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("connection not found for agent %s (key expired or never tracked): %w", agentID, err)
		}
		return fmt.Errorf("get connection %s: %w", key, err)
	}

	var info ConnectionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("unmarshal connection info: %w", err)
	}

	// Update heartbeat and write back.
	now := time.Now().UTC()
	info.LastHeartbeat = now

	updated, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal connection info: %w", err)
	}

	if err := co.client.rdb.Set(ctx, key, updated, connectionTTL).Err(); err != nil {
		return fmt.Errorf("set connection %s: %w", key, err)
	}

	co.logger.Debug("refreshed heartbeat",
		zap.String("tenant", tenant),
		zap.String("agent_id", agentID),
		zap.Time("last_heartbeat", now),
	)

	return nil
}

// DetectDisconnectedAgents scans all connection keys for a tenant and returns
// agents whose last_heartbeat is older than the given threshold.
//
// If threshold is zero or negative, defaultDisconnectThreshold (60s) is used.
// Uses SCAN (not KEYS) to avoid blocking Dragonfly on large key spaces.
// Keys that expire between SCAN and GET are silently skipped (expected race).
func (co *ConnectionOps) DetectDisconnectedAgents(ctx context.Context, tenant string, threshold time.Duration) ([]ConnectionInfo, error) {
	if tenant == "" {
		return nil, fmt.Errorf("tenant is required")
	}
	if threshold <= 0 {
		threshold = defaultDisconnectThreshold
	}

	pattern := connectionPattern(tenant)
	cutoff := time.Now().UTC().Add(-threshold)
	var disconnected []ConnectionInfo

	var cursor uint64
	for {
		keys, nextCursor, err := co.client.rdb.Scan(ctx, cursor, pattern, connectionScanCount).Result()
		if err != nil {
			return nil, fmt.Errorf("scan connections: %w", err)
		}

		for _, key := range keys {
			data, err := co.client.rdb.Get(ctx, key).Bytes()
			if err != nil {
				if err == redis.Nil {
					// Key expired between SCAN and GET — expected race.
					continue
				}
				co.logger.Warn("failed to get connection key during disconnect detection",
					zap.String("key", key),
					zap.Error(err),
				)
				continue
			}

			var info ConnectionInfo
			if err := json.Unmarshal(data, &info); err != nil {
				co.logger.Warn("failed to unmarshal connection info",
					zap.String("key", key),
					zap.Error(err),
				)
				continue
			}

			if info.LastHeartbeat.Before(cutoff) {
				disconnected = append(disconnected, info)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	co.logger.Info("detected disconnected agents",
		zap.String("tenant", tenant),
		zap.Duration("threshold", threshold),
		zap.Int("count", len(disconnected)),
	)

	return disconnected, nil
}

// GetActiveConnections returns all active (non-expired) connections for a tenant.
// Uses SCAN (not KEYS) to avoid blocking Dragonfly on large key spaces.
func (co *ConnectionOps) GetActiveConnections(ctx context.Context, tenant string) ([]ConnectionInfo, error) {
	if tenant == "" {
		return nil, fmt.Errorf("tenant is required")
	}

	pattern := connectionPattern(tenant)
	var connections []ConnectionInfo

	var cursor uint64
	for {
		keys, nextCursor, err := co.client.rdb.Scan(ctx, cursor, pattern, connectionScanCount).Result()
		if err != nil {
			return nil, fmt.Errorf("scan connections: %w", err)
		}

		// Batch-fetch values with a pipeline to reduce round trips.
		if len(keys) > 0 {
			pipe := co.client.rdb.Pipeline()
			cmds := make([]*redis.StringCmd, len(keys))
			for i, key := range keys {
				cmds[i] = pipe.Get(ctx, key)
			}
			if _, err := pipe.Exec(ctx); err != nil {
				// Pipeline may return errors for individual keys that expired.
				// We handle per-key errors below.
				_ = err
			}

			for i, cmd := range cmds {
				data, err := cmd.Bytes()
				if err != nil {
					if err == redis.Nil {
						// Key expired between SCAN and pipeline execution.
						continue
					}
					co.logger.Warn("failed to get connection key",
						zap.String("key", keys[i]),
						zap.Error(err),
					)
					continue
				}

				var info ConnectionInfo
				if err := json.Unmarshal(data, &info); err != nil {
					co.logger.Warn("failed to unmarshal connection info",
						zap.String("key", keys[i]),
						zap.Error(err),
					)
					continue
				}

				connections = append(connections, info)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return connections, nil
}

// RemoveConnection deletes the connection key for an agent.
// Used on graceful disconnect. Idempotent: no error if the key does not exist.
func (co *ConnectionOps) RemoveConnection(ctx context.Context, tenant, agentID string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is required")
	}
	if agentID == "" {
		return fmt.Errorf("agent_id is required")
	}

	key := connectionKey(tenant, agentID)
	if err := co.client.rdb.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("del connection %s: %w", key, err)
	}

	co.logger.Info("removed connection",
		zap.String("tenant", tenant),
		zap.String("agent_id", agentID),
	)

	return nil
}
