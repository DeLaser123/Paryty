package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// CacheConfig contains configuration for the query cache.
type CacheConfig struct {
	Addr      string        `yaml:"addr" json:"addr"`
	Password  string        `yaml:"password" json:"password"`
	DB        int           `yaml:"db" json:"db"`
	TTL       time.Duration `yaml:"ttl" json:"ttl"`
	TLSConfig *tls.Config   `yaml:"-" json:"-"` // TLS config for encrypted Dragonfly connections
}

// QueryCache provides caching for query results.
type QueryCache struct {
	rdb    *redis.Client
	logger *zap.Logger
	ttl    time.Duration
}

// NewQueryCache creates a new query cache.
// If cfg.TLSConfig is non-nil, the Dragonfly connection uses TLS.
func NewQueryCache(cfg CacheConfig, logger *zap.Logger) *QueryCache {
	rdb := redis.NewClient(&redis.Options{
		Addr:      cfg.Addr,
		Password:  cfg.Password,
		DB:        cfg.DB,
		TLSConfig: cfg.TLSConfig,
	})

	return &QueryCache{
		rdb:    rdb,
		logger: logger,
		ttl:    cfg.TTL,
	}
}

// Close closes the cache connection.
func (c *QueryCache) Close() error {
	return c.rdb.Close()
}

// GetTopology gets or sets topology in cache.
func (c *QueryCache) GetTopology(ctx context.Context, getter func() (*models.Topology, error)) (*models.Topology, error) {
	key := "cache:topology:current"

	// Try cache
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == nil {
		var topo models.Topology
		if err := json.Unmarshal(data, &topo); err == nil {
			return &topo, nil
		}
	}

	// Cache miss, get from source
	topo, err := getter()
	if err != nil {
		return nil, err
	}

	// Store in cache
	data, _ = json.Marshal(topo)
	c.rdb.Set(ctx, key, data, c.ttl)

	return topo, nil
}

// GetMetrics gets or sets metrics in cache.
func (c *QueryCache) GetMetrics(ctx context.Context, agentID string, getter func() (*models.MetricBatch, error)) (*models.MetricBatch, error) {
	key := fmt.Sprintf("cache:metrics:%s", agentID)

	// Try cache
	data, err := c.rdb.Get(ctx, key).Bytes()
	if err == nil {
		var batch models.MetricBatch
		if err := json.Unmarshal(data, &batch); err == nil {
			return &batch, nil
		}
	}

	// Cache miss
	batch, err := getter()
	if err != nil {
		return nil, err
	}

	// Store in cache
	data, _ = json.Marshal(batch)
	c.rdb.Set(ctx, key, data, c.ttl)

	return batch, nil
}

// Invalidate invalidates cache entries.
func (c *QueryCache) Invalidate(ctx context.Context, keys ...string) error {
	return c.rdb.Del(ctx, keys...).Err()
}

// InvalidateTopology invalidates topology cache.
func (c *QueryCache) InvalidateTopology(ctx context.Context) error {
	return c.Invalidate(ctx, "cache:topology:current")
}

// InvalidateMetrics invalidates metrics cache for an agent.
func (c *QueryCache) InvalidateMetrics(ctx context.Context, agentID string) error {
	return c.Invalidate(ctx, fmt.Sprintf("cache:metrics:%s", agentID))
}
