// Package config provides centralized configuration for all Paryty Cluster services.
//
// Configuration is loaded from a YAML file with environment variable overrides.
// Every cmd/* binary imports this package instead of using ad-hoc getEnv() helpers.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/cold"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/hot"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/warm"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"gopkg.in/yaml.v3"
)

// Defaults for configuration fields when YAML omits them.
const (
	DefaultPort               = 50051
	DefaultPoolSize           = 10
	DefaultMaxConns           = 20
	DefaultTTL                = "5m"
	DefaultMaxBatchesPerMin   = 10000
	DefaultDesiredIntervalMs  = 10000
	DefaultSamplingRate       = 1.0
	DefaultMaxConnections     = 10000
	DefaultRegion             = "us-east-1"
)

// Config is the top-level configuration mapped to the cluster YAML schema.
type Config struct {
	Cluster ClusterConfig `yaml:"cluster"`
}

// ClusterConfig holds all cluster-level settings.
type ClusterConfig struct {
	Name        string            `yaml:"name"`
	Tenant      string            `yaml:"tenant"`
	Ingestion   IngestionConfig   `yaml:"ingestion"`
	Stream      StreamConfig      `yaml:"stream"`
	Storage     StorageConfig     `yaml:"storage"`
	RateLimit   RateLimitConfig   `yaml:"rate_limit"`
	FlowControl FlowControlConfig `yaml:"flow_control"`
}

// IngestionConfig configures the gRPC ingestion service.
type IngestionConfig struct {
	Port           int  `yaml:"port"`
	TLSEnabled     bool `yaml:"tls"`
	MaxConnections int  `yaml:"max_connections"`
}

// StreamConfig configures the Redpanda stream engine.
type StreamConfig struct {
	Brokers  []string      `yaml:"brokers"`
	ClientID string        `yaml:"client_id"`
	Topics   []TopicConfig `yaml:"topics"`
}

// TopicConfig describes a single Redpanda topic.
type TopicConfig struct {
	Name       string `yaml:"name"`
	Partitions int    `yaml:"partitions"`
	Retention  string `yaml:"retention"`
}

// StorageConfig groups the three storage tiers.
type StorageConfig struct {
	Hot  HotConfig  `yaml:"hot"`
	Warm WarmConfig `yaml:"warm"`
	Cold ColdConfig `yaml:"cold"`
}

// HotConfig configures the Dragonfly (hot) tier.
type HotConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	PoolSize int    `yaml:"pool_size"`
	TTL      string `yaml:"ttl"`
}

// WarmConfig configures the QuestDB (warm) tier.
type WarmConfig struct {
	Addr     string `yaml:"addr"`
	ILPAddr  string `yaml:"ilp_addr"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	MaxConns int    `yaml:"max_conns"`
}

// ColdConfig configures the SeaweedFS (cold) tier.
type ColdConfig struct {
	Endpoint  string `yaml:"endpoint"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	UseSSL    bool   `yaml:"use_ssl"`
	Region    string `yaml:"region"`
	Bucket    string `yaml:"bucket"`
}

// RateLimitConfig configures rate limiting for the ingestion pipeline.
type RateLimitConfig struct {
	MaxBatchesPerMinute int `yaml:"max_batches_per_minute"`
}

// FlowControlConfig configures flow control parameters.
type FlowControlConfig struct {
	DesiredIntervalMs int     `yaml:"desired_interval_ms"`
	SamplingRate      float64 `yaml:"sampling_rate"`
}

// Load reads a YAML file at path, applies defaults, and then applies
// environment variable overrides. It returns a fully populated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	return cfg, nil
}

// Validate checks that the configuration has all required fields populated
// with sensible values.
func (c *Config) Validate() error {
	var errs []error

	if c.Cluster.Ingestion.Port <= 0 {
		errs = append(errs, errors.New("ingestion.port must be > 0"))
	}
	if len(c.Cluster.Stream.Brokers) == 0 {
		errs = append(errs, errors.New("stream.brokers must have at least 1 entry"))
	}
	if c.Cluster.Storage.Hot.Addr == "" {
		errs = append(errs, errors.New("storage.hot.addr must not be empty"))
	}

	return errors.Join(errs...)
}

// TTLDuration parses the hot storage TTL string into a time.Duration.
// Returns 5 minutes if the TTL string is empty or unparseable.
func (c *Config) TTLDuration() time.Duration {
	d, err := time.ParseDuration(c.Cluster.Storage.Hot.TTL)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// ToHotConfig converts to the hot storage package Config.
// The TTL string from YAML is parsed into a time.Duration.
func (c *Config) ToHotConfig() hot.Config {
	return hot.Config{
		Addr:     c.Cluster.Storage.Hot.Addr,
		Password: c.Cluster.Storage.Hot.Password,
		DB:       c.Cluster.Storage.Hot.DB,
		PoolSize: c.Cluster.Storage.Hot.PoolSize,
		TTL:      c.TTLDuration(),
	}
}

// ToWarmConfig converts to the warm storage package Config.
func (c *Config) ToWarmConfig() warm.Config {
	return warm.Config{
		Addr:     c.Cluster.Storage.Warm.Addr,
		ILPAddr:  c.Cluster.Storage.Warm.ILPAddr,
		Database: c.Cluster.Storage.Warm.Database,
		Username: c.Cluster.Storage.Warm.Username,
		Password: c.Cluster.Storage.Warm.Password,
		MaxConns: c.Cluster.Storage.Warm.MaxConns,
	}
}

// ToColdConfig converts to the cold storage package Config.
func (c *Config) ToColdConfig() cold.Config {
	return cold.Config{
		Endpoint:  c.Cluster.Storage.Cold.Endpoint,
		AccessKey: c.Cluster.Storage.Cold.AccessKey,
		SecretKey: c.Cluster.Storage.Cold.SecretKey,
		UseSSL:    c.Cluster.Storage.Cold.UseSSL,
		Region:    c.Cluster.Storage.Cold.Region,
	}
}

// ToStorageConfig converts to the storage orchestrator Config,
// composing all three tier configs.
func (c *Config) ToStorageConfig() storage.Config {
	return storage.Config{
		Hot:  c.ToHotConfig(),
		Warm: c.ToWarmConfig(),
		Cold: c.ToColdConfig(),
	}
}

// ToStreamConfig converts to the stream package Config.
func (c *Config) ToStreamConfig() stream.Config {
	return stream.Config{
		Brokers:  c.Cluster.Stream.Brokers,
		ClientID: c.Cluster.Stream.ClientID,
	}
}

// ILPAddr returns the QuestDB ILP ingestion address from the warm config.
// This is not part of the warm.Config struct but is needed for ILP writes.
func (c *Config) ILPAddr() string {
	return c.Cluster.Storage.Warm.ILPAddr
}

// applyDefaults populates zero-valued fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.Cluster.Ingestion.Port == 0 {
		cfg.Cluster.Ingestion.Port = DefaultPort
	}
	if cfg.Cluster.Ingestion.MaxConnections == 0 {
		cfg.Cluster.Ingestion.MaxConnections = DefaultMaxConnections
	}
	if cfg.Cluster.Storage.Hot.PoolSize == 0 {
		cfg.Cluster.Storage.Hot.PoolSize = DefaultPoolSize
	}
	if cfg.Cluster.Storage.Hot.TTL == "" {
		cfg.Cluster.Storage.Hot.TTL = DefaultTTL
	}
	if cfg.Cluster.Storage.Warm.MaxConns == 0 {
		cfg.Cluster.Storage.Warm.MaxConns = DefaultMaxConns
	}
	if cfg.Cluster.Storage.Cold.Region == "" {
		cfg.Cluster.Storage.Cold.Region = DefaultRegion
	}
	if cfg.Cluster.RateLimit.MaxBatchesPerMinute == 0 {
		cfg.Cluster.RateLimit.MaxBatchesPerMinute = DefaultMaxBatchesPerMin
	}
	if cfg.Cluster.FlowControl.DesiredIntervalMs == 0 {
		cfg.Cluster.FlowControl.DesiredIntervalMs = DefaultDesiredIntervalMs
	}
	if cfg.Cluster.FlowControl.SamplingRate == 0 {
		cfg.Cluster.FlowControl.SamplingRate = DefaultSamplingRate
	}
}

// applyEnvOverrides reads PARYTY_* environment variables and overrides
// the corresponding config fields. Only connection-critical fields are
// overridable to keep the surface area small.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("PARYTY_INGESTION_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Cluster.Ingestion.Port = port
		}
	}

	if v := os.Getenv("PARYTY_REDPANDA_BROKERS"); v != "" {
		cfg.Cluster.Stream.Brokers = splitAndTrim(v)
	}

	if v := os.Getenv("PARYTY_DRAGONFLY_ADDR"); v != "" {
		cfg.Cluster.Storage.Hot.Addr = v
	}

	if v := os.Getenv("PARYTY_QUESTDB_ADDR"); v != "" {
		cfg.Cluster.Storage.Warm.Addr = v
	}

	if v := os.Getenv("PARYTY_QUESTDB_ILP_ADDR"); v != "" {
		cfg.Cluster.Storage.Warm.ILPAddr = v
	}

	if v := os.Getenv("PARYTY_SEAWEEDFS_ENDPOINT"); v != "" {
		cfg.Cluster.Storage.Cold.Endpoint = v
	}
}

// splitAndTrim splits a comma-separated string and trims whitespace from each element.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
