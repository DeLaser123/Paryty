// Package config provides centralized configuration for all Paryty Cluster services.
//
// Configuration is loaded from a YAML file with environment variable overrides.
// Every cmd/* binary imports this package instead of using ad-hoc getEnv() helpers.
package config

import (
	"errors"
	"fmt"
	"os"
	"runtime"
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
	DefaultPort              = 50051
	DefaultPoolSize          = 10
	DefaultMaxConns          = 20
	DefaultTTL               = "5m"
	DefaultMaxBatchesPerMin  = 10000
	DefaultDesiredIntervalMs = 10000
	DefaultSamplingRate      = 1.0
	DefaultMaxConnections    = 10000
	DefaultRegion            = "us-east-1"
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
	Processing  ProcessingConfig  `yaml:"processing"`
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
	Addr        string `yaml:"addr"`
	ILPAddr     string `yaml:"ilp_addr"`
	Database    string `yaml:"database"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	MaxConns    int    `yaml:"max_conns"`
	SSLMode     string `yaml:"sslmode"`
	SSLRootCert string `yaml:"sslrootcert"`
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

// PipelineConfig holds the processing pipeline configuration.
type PipelineConfig struct {
	ConsumerGroup       string        `yaml:"consumer_group"`
	InputTopics         []string      `yaml:"input_topics"`
	BatchSize           int           `yaml:"batch_size"`
	BatchTimeout        time.Duration `yaml:"batch_timeout"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
}

// AggregatorPipelineConfig holds aggregator-specific configuration.
type AggregatorPipelineConfig struct {
	WindowSizes      DurationSlice `yaml:"window_sizes"`
	GracePeriod      time.Duration `yaml:"grace_period"`
	SnapshotInterval time.Duration `yaml:"snapshot_interval"`
	TopNSize         int           `yaml:"top_n_size"`
}

// DurationSlice is a []time.Duration that supports flexible parsing
// including "d" (days) suffix in YAML.
type DurationSlice []time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for DurationSlice.
func (ds *DurationSlice) UnmarshalYAML(value func(interface{}) error) error {
	var raw []string
	if err := value(&raw); err != nil {
		return err
	}
	*ds = make(DurationSlice, 0, len(raw))
	for _, s := range raw {
		d, err := parseFlexibleDuration(s)
		if err != nil {
			return fmt.Errorf("parse window_size %q: %w", s, err)
		}
		*ds = append(*ds, d)
	}
	return nil
}

// CorrelatorPipelineConfig holds correlator-specific configuration.
type CorrelatorPipelineConfig struct {
	StaleNodeTimeout      time.Duration `yaml:"stale_node_timeout"`
	GraphSnapshotInterval time.Duration `yaml:"graph_snapshot_interval"`
	EventBufferSize       int           `yaml:"event_buffer_size"`
	CorrelationWindow     time.Duration `yaml:"correlation_window"`
}

// EnricherPipelineConfig holds enricher-specific configuration.
type EnricherPipelineConfig struct {
	StandardLabelKeys  []string `yaml:"standard_label_keys"`
	MaxLabelsPerMetric int      `yaml:"max_labels_per_metric"`
	LabelPrefix        string   `yaml:"label_prefix"`
}

// DownsamplingRule defines a retention and downsampling policy.
type DownsamplingRule struct {
	SourceWindow  string `yaml:"source_window"`
	TargetWindow  string `yaml:"target_window"`
	RetentionDays int    `yaml:"retention_days"`
}

// DownsamplerConfig holds downsampler configuration.
type DownsamplerConfig struct {
	Rules       []DownsamplingRule `yaml:"rules"`
	RunInterval time.Duration      `yaml:"run_interval"`
}

// MemoryConfig configures memory budget and circuit breaker for the pipeline.
// These settings control bounded resource usage and prevent OOM conditions
// in high-throughput scenarios.
type MemoryConfig struct {
	// MaxRAMBytes is the maximum allowed process memory in bytes.
	// The circuit breaker opens when memory exceeds 95% of this limit.
	// Default: 1 GB (1 << 30).
	MaxRAMBytes int64 `yaml:"max_ram_bytes"`

	// GoroutinePoolSize is the maximum number of concurrent worker goroutines.
	// Defaults to runtime.NumCPU() * 2, capped at 64.
	GoroutinePoolSize int `yaml:"goroutine_pool_size"`

	// WindowBufferCapacity is the maximum number of values per aggregation window.
	// Reduced from 10K to 256 to save memory (256 values × ~16 bytes = ~4KB per window).
	// Default: 256.
	WindowBufferCapacity int `yaml:"window_buffer_capacity"`

	// EventBufferSize is the ring buffer capacity for correlator events.
	// Sized for 30s correlation window (~1K events).
	// Default: 1000.
	EventBufferSize int `yaml:"event_buffer_size"`

	// MaxBufferedRecords is the maximum number of records buffered in the producer.
	// Prevents unbounded memory growth when downstream is slow.
	// Default: 1000.
	MaxBufferedRecords int `yaml:"max_buffered_records"`

	// GraphChangesCap is the capacity of the graph changes channel.
	// Caps topology change events to prevent memory spikes during high churn.
	// Default: 5000.
	GraphChangesCap int `yaml:"graph_changes_cap"`

	// CircuitBreakerEnabled controls whether the memory circuit breaker is active.
	// When enabled, the pipeline rejects new messages when memory exceeds 95% of MaxRAMBytes.
	// Default: true (set in YAML, defaults to false in Go zero-value).
	CircuitBreakerEnabled bool `yaml:"circuit_breaker_enabled"`

	// CheckInterval is how often the memory monitor checks process memory.
	// Default: 10s.
	CheckInterval time.Duration `yaml:"check_interval"`
}

// ProcessingConfig holds all processing pipeline configuration.
type ProcessingConfig struct {
	Pipeline    PipelineConfig           `yaml:"pipeline"`
	Aggregator  AggregatorPipelineConfig  `yaml:"aggregator"`
	Correlator  CorrelatorPipelineConfig  `yaml:"correlator"`
	Enricher    EnricherPipelineConfig    `yaml:"enricher"`
	Downsampler DownsamplerConfig         `yaml:"downsampler"`
	Memory      MemoryConfig              `yaml:"memory"`
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

// parseFlexibleDuration parses a duration string, adding support for
// "d" (days) suffix which Go's time.ParseDuration does not handle.
// Examples: "1d" -> 24h, "7d" -> 168h, "30s" -> 30s.
func parseFlexibleDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		days, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("parse duration %q: invalid day value", s)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}
	return time.ParseDuration(s)
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
		Addr:        c.Cluster.Storage.Warm.Addr,
		ILPAddr:     c.Cluster.Storage.Warm.ILPAddr,
		Database:    c.Cluster.Storage.Warm.Database,
		Username:    c.Cluster.Storage.Warm.Username,
		Password:    c.Cluster.Storage.Warm.Password,
		MaxConns:    c.Cluster.Storage.Warm.MaxConns,
		SSLMode:     c.Cluster.Storage.Warm.SSLMode,
		SSLRootCert: c.Cluster.Storage.Warm.SSLRootCert,
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

	// Pipeline defaults.
	if cfg.Cluster.Processing.Pipeline.ConsumerGroup == "" {
		cfg.Cluster.Processing.Pipeline.ConsumerGroup = "paryty-pipeline"
	}
	if cfg.Cluster.Processing.Pipeline.BatchSize == 0 {
		cfg.Cluster.Processing.Pipeline.BatchSize = 100
	}
	if cfg.Cluster.Processing.Pipeline.BatchTimeout == 0 {
		cfg.Cluster.Processing.Pipeline.BatchTimeout = time.Second
	}
	if cfg.Cluster.Processing.Pipeline.HealthCheckInterval == 0 {
		cfg.Cluster.Processing.Pipeline.HealthCheckInterval = 10 * time.Second
	}

	// Aggregator defaults.
	if len(cfg.Cluster.Processing.Aggregator.WindowSizes) == 0 {
		cfg.Cluster.Processing.Aggregator.WindowSizes = []time.Duration{
			time.Minute,
			5 * time.Minute,
			time.Hour,
			24 * time.Hour,
		}
	}
	if cfg.Cluster.Processing.Aggregator.GracePeriod == 0 {
		cfg.Cluster.Processing.Aggregator.GracePeriod = 30 * time.Second
	}
	if cfg.Cluster.Processing.Aggregator.SnapshotInterval == 0 {
		cfg.Cluster.Processing.Aggregator.SnapshotInterval = 30 * time.Second
	}
	if cfg.Cluster.Processing.Aggregator.TopNSize == 0 {
		cfg.Cluster.Processing.Aggregator.TopNSize = 10
	}

	// Correlator defaults.
	if cfg.Cluster.Processing.Correlator.StaleNodeTimeout == 0 {
		cfg.Cluster.Processing.Correlator.StaleNodeTimeout = 5 * time.Minute
	}
	if cfg.Cluster.Processing.Correlator.GraphSnapshotInterval == 0 {
		cfg.Cluster.Processing.Correlator.GraphSnapshotInterval = 60 * time.Second
	}
	if cfg.Cluster.Processing.Correlator.EventBufferSize == 0 {
		cfg.Cluster.Processing.Correlator.EventBufferSize = 1000
	}
	if cfg.Cluster.Processing.Correlator.CorrelationWindow == 0 {
		cfg.Cluster.Processing.Correlator.CorrelationWindow = 30 * time.Second
	}

	// Enricher defaults.
	if len(cfg.Cluster.Processing.Enricher.StandardLabelKeys) == 0 {
		cfg.Cluster.Processing.Enricher.StandardLabelKeys = []string{
			"env", "region", "team", "service", "version",
		}
	}
	if cfg.Cluster.Processing.Enricher.MaxLabelsPerMetric == 0 {
		cfg.Cluster.Processing.Enricher.MaxLabelsPerMetric = 20
	}
	if cfg.Cluster.Processing.Enricher.LabelPrefix == "" {
		cfg.Cluster.Processing.Enricher.LabelPrefix = "agent."
	}

	// Downsampler defaults.
	if cfg.Cluster.Processing.Downsampler.RunInterval == 0 {
		cfg.Cluster.Processing.Downsampler.RunInterval = time.Hour
	}
	if len(cfg.Cluster.Processing.Downsampler.Rules) == 0 {
		cfg.Cluster.Processing.Downsampler.Rules = []DownsamplingRule{
			{SourceWindow: "1m", TargetWindow: "5m", RetentionDays: 7},
			{SourceWindow: "5m", TargetWindow: "1h", RetentionDays: 30},
			{SourceWindow: "1h", TargetWindow: "1d", RetentionDays: 90},
		}
	}

	// Memory budget defaults.
	// MaxRAMBytes: 1 GB — conservative default for pipeline.exe process memory.
	// Allows room for OS and other processes while providing enough headroom
	// for burst traffic.
	if cfg.Cluster.Processing.Memory.MaxRAMBytes == 0 {
		cfg.Cluster.Processing.Memory.MaxRAMBytes = 1 << 30 // 1 GB
	}
	// GoroutinePoolSize: CPU * 2, capped at 64.
	// Balances parallelism with context-switching overhead. The cap prevents
	// runaway goroutine creation on high-core machines.
	if cfg.Cluster.Processing.Memory.GoroutinePoolSize == 0 {
		cfg.Cluster.Processing.Memory.GoroutinePoolSize = runtime.NumCPU() * 2
		if cfg.Cluster.Processing.Memory.GoroutinePoolSize > 64 {
			cfg.Cluster.Processing.Memory.GoroutinePoolSize = 64
		}
	}
	// WindowBufferCapacity: 256 values per window.
	// 256 × ~16 bytes = ~4KB per window. With 4 window sizes × N agents,
	// this bounds total window memory predictably.
	if cfg.Cluster.Processing.Memory.WindowBufferCapacity == 0 {
		cfg.Cluster.Processing.Memory.WindowBufferCapacity = 256
	}
	// EventBufferSize: 1000 events for correlator ring buffer.
	// Sized for 30s correlation window — ~33 events/sec is typical load.
	if cfg.Cluster.Processing.Memory.EventBufferSize == 0 {
		cfg.Cluster.Processing.Memory.EventBufferSize = 1000
	}
	// MaxBufferedRecords: 1000 records in producer buffer.
	// Prevents unbounded memory growth when Redpanda is slow or unavailable.
	if cfg.Cluster.Processing.Memory.MaxBufferedRecords == 0 {
		cfg.Cluster.Processing.Memory.MaxBufferedRecords = 1000
	}
	// GraphChangesCap: 5000 graph change events.
	// Caps topology change channel to prevent memory spikes during service churn.
	if cfg.Cluster.Processing.Memory.GraphChangesCap == 0 {
		cfg.Cluster.Processing.Memory.GraphChangesCap = 5000
	}
	// CheckInterval: 10 seconds between memory checks.
	// Frequent enough to catch memory spikes, infrequent to avoid overhead.
	if cfg.Cluster.Processing.Memory.CheckInterval == 0 {
		cfg.Cluster.Processing.Memory.CheckInterval = 10 * time.Second
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

	if v := os.Getenv("PARYTY_QUESTDB_USERNAME"); v != "" {
		cfg.Cluster.Storage.Warm.Username = v
	}

	if v := os.Getenv("PARYTY_QUESTDB_PASSWORD"); v != "" {
		cfg.Cluster.Storage.Warm.Password = v
	}

	if v := os.Getenv("PARYTY_SEAWEEDFS_ENDPOINT"); v != "" {
		cfg.Cluster.Storage.Cold.Endpoint = v
	}

	if v := os.Getenv("PARYTY_SEAWEEDFS_ACCESS_KEY"); v != "" {
		cfg.Cluster.Storage.Cold.AccessKey = v
	}

	if v := os.Getenv("PARYTY_SEAWEEDFS_SECRET_KEY"); v != "" {
		cfg.Cluster.Storage.Cold.SecretKey = v
	}

	if v := os.Getenv("PARYTY_SEAWEEDFS_USE_SSL"); v != "" {
		cfg.Cluster.Storage.Cold.UseSSL = strings.EqualFold(v, "true") || v == "1"
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
