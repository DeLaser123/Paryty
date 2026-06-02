package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalYAML is the smallest valid configuration that passes validation.
const minimalYAML = `
cluster:
  name: "test-cluster"
  tenant: "test-tenant"
  ingestion:
    port: 50051
  stream:
    brokers:
      - "localhost:9092"
    client_id: "test-client"
  storage:
    hot:
      addr: "localhost:6379"
    warm:
      addr: "localhost:8812"
      ilp_addr: "localhost:9009"
      database: "paryty"
      username: "admin"
      password: "quest"
      max_conns: 20
    cold:
      endpoint: "localhost:8333"
      access_key: "minioadmin"
      secret_key: "minioadmin"
      use_ssl: false
      region: "us-east-1"
      bucket: "paryty-cold"
  rate_limit:
    max_batches_per_minute: 10000
  flow_control:
    desired_interval_ms: 10000
    sampling_rate: 1.0
`

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cluster.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	return path
}

func TestLoad_ParsesMinimalConfig(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cluster.Name != "test-cluster" {
		t.Errorf("Name = %q, want %q", cfg.Cluster.Name, "test-cluster")
	}
	if cfg.Cluster.Tenant != "test-tenant" {
		t.Errorf("Tenant = %q, want %q", cfg.Cluster.Tenant, "test-tenant")
	}
	if cfg.Cluster.Ingestion.Port != 50051 {
		t.Errorf("Ingestion.Port = %d, want 50051", cfg.Cluster.Ingestion.Port)
	}
	if cfg.Cluster.Stream.ClientID != "test-client" {
		t.Errorf("Stream.ClientID = %q, want %q", cfg.Cluster.Stream.ClientID, "test-client")
	}
	if cfg.Cluster.Storage.Hot.Addr != "localhost:6379" {
		t.Errorf("Storage.Hot.Addr = %q, want %q", cfg.Cluster.Storage.Hot.Addr, "localhost:6379")
	}
	if cfg.Cluster.Storage.Warm.ILPAddr != "localhost:9009" {
		t.Errorf("Storage.Warm.ILPAddr = %q, want %q", cfg.Cluster.Storage.Warm.ILPAddr, "localhost:9009")
	}
	if cfg.Cluster.Storage.Cold.Bucket != "paryty-cold" {
		t.Errorf("Storage.Cold.Bucket = %q, want %q", cfg.Cluster.Storage.Cold.Bucket, "paryty-cold")
	}
	if cfg.Cluster.FlowControl.SamplingRate != 1.0 {
		t.Errorf("FlowControl.SamplingRate = %f, want 1.0", cfg.Cluster.FlowControl.SamplingRate)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)

	t.Setenv("PARYTY_INGESTION_PORT", "9999")
	t.Setenv("PARYTY_REDPANDA_BROKERS", "broker-a:9092,broker-b:9093")
	t.Setenv("PARYTY_DRAGONFLY_ADDR", "dragonfly.prod:6380")
	t.Setenv("PARYTY_QUESTDB_ADDR", "questdb.prod:8812")
	t.Setenv("PARYTY_QUESTDB_ILP_ADDR", "questdb.prod:9010")
	t.Setenv("PARYTY_SEAWEEDFS_ENDPOINT", "seaweed.prod:8333")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cluster.Ingestion.Port != 9999 {
		t.Errorf("Ingestion.Port = %d, want 9999 (env override)", cfg.Cluster.Ingestion.Port)
	}
	if len(cfg.Cluster.Stream.Brokers) != 2 || cfg.Cluster.Stream.Brokers[0] != "broker-a:9092" {
		t.Errorf("Stream.Brokers = %v, want [broker-a:9092 broker-b:9093]", cfg.Cluster.Stream.Brokers)
	}
	if cfg.Cluster.Storage.Hot.Addr != "dragonfly.prod:6380" {
		t.Errorf("Hot.Addr = %q, want %q", cfg.Cluster.Storage.Hot.Addr, "dragonfly.prod:6380")
	}
	if cfg.Cluster.Storage.Warm.Addr != "questdb.prod:8812" {
		t.Errorf("Warm.Addr = %q, want %q", cfg.Cluster.Storage.Warm.Addr, "questdb.prod:8812")
	}
	if cfg.Cluster.Storage.Warm.ILPAddr != "questdb.prod:9010" {
		t.Errorf("Warm.ILPAddr = %q, want %q", cfg.Cluster.Storage.Warm.ILPAddr, "questdb.prod:9010")
	}
	if cfg.Cluster.Storage.Cold.Endpoint != "seaweed.prod:8333" {
		t.Errorf("Cold.Endpoint = %q, want %q", cfg.Cluster.Storage.Cold.Endpoint, "seaweed.prod:8333")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	// Minimal YAML that omits optional fields to verify defaults are applied.
	yamlContent := `
cluster:
  ingestion: {}
  stream:
    brokers:
      - "localhost:9092"
  storage:
    hot:
      addr: "localhost:6379"
`
	path := writeTempYAML(t, yamlContent)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cluster.Ingestion.Port != DefaultPort {
		t.Errorf("Ingestion.Port = %d, want default %d", cfg.Cluster.Ingestion.Port, DefaultPort)
	}
	if cfg.Cluster.Ingestion.MaxConnections != DefaultMaxConnections {
		t.Errorf("Ingestion.MaxConnections = %d, want default %d", cfg.Cluster.Ingestion.MaxConnections, DefaultMaxConnections)
	}
	if cfg.Cluster.Storage.Hot.PoolSize != DefaultPoolSize {
		t.Errorf("Hot.PoolSize = %d, want default %d", cfg.Cluster.Storage.Hot.PoolSize, DefaultPoolSize)
	}
	if cfg.Cluster.Storage.Hot.TTL != DefaultTTL {
		t.Errorf("Hot.TTL = %q, want default %q", cfg.Cluster.Storage.Hot.TTL, DefaultTTL)
	}
	if cfg.Cluster.Storage.Warm.MaxConns != DefaultMaxConns {
		t.Errorf("Warm.MaxConns = %d, want default %d", cfg.Cluster.Storage.Warm.MaxConns, DefaultMaxConns)
	}
	if cfg.Cluster.Storage.Cold.Region != DefaultRegion {
		t.Errorf("Cold.Region = %q, want default %q", cfg.Cluster.Storage.Cold.Region, DefaultRegion)
	}
	if cfg.Cluster.RateLimit.MaxBatchesPerMinute != DefaultMaxBatchesPerMin {
		t.Errorf("RateLimit.MaxBatchesPerMinute = %d, want default %d", cfg.Cluster.RateLimit.MaxBatchesPerMinute, DefaultMaxBatchesPerMin)
	}
	if cfg.Cluster.FlowControl.DesiredIntervalMs != DefaultDesiredIntervalMs {
		t.Errorf("FlowControl.DesiredIntervalMs = %d, want default %d", cfg.Cluster.FlowControl.DesiredIntervalMs, DefaultDesiredIntervalMs)
	}
	if cfg.Cluster.FlowControl.SamplingRate != DefaultSamplingRate {
		t.Errorf("FlowControl.SamplingRate = %f, want default %f", cfg.Cluster.FlowControl.SamplingRate, DefaultSamplingRate)
	}
}

func TestValidate_MissingPort(t *testing.T) {
	yamlContent := `
cluster:
  ingestion:
    port: 0
  stream:
    brokers:
      - "localhost:9092"
  storage:
    hot:
      addr: "localhost:6379"
`
	// Need to bypass applyDefaults so port stays 0.
	// We test Validate() directly after unmarshal without defaults.
	path := writeTempYAML(t, yamlContent)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Defaults will have set port to 50051, so we need to test raw validation.
	// Reset port to 0 to test validation catches it.
	cfg.Cluster.Ingestion.Port = 0
	err = cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for port=0, got nil")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("Validate() error %q does not mention port", err.Error())
	}
}

func TestValidate_ZeroBrokers(t *testing.T) {
	cfg := &Config{
		Cluster: ClusterConfig{
			Ingestion: IngestionConfig{Port: 50051},
			Stream:    StreamConfig{Brokers: nil},
			Storage:   StorageConfig{Hot: HotConfig{Addr: "localhost:6379"}},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for zero brokers, got nil")
	}
	if !strings.Contains(err.Error(), "brokers") {
		t.Errorf("Validate() error %q does not mention brokers", err.Error())
	}
}

func TestValidate_MissingHotAddr(t *testing.T) {
	cfg := &Config{
		Cluster: ClusterConfig{
			Ingestion: IngestionConfig{Port: 50051},
			Stream:    StreamConfig{Brokers: []string{"localhost:9092"}},
			Storage:   StorageConfig{Hot: HotConfig{Addr: ""}},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for empty hot addr, got nil")
	}
	if !strings.Contains(err.Error(), "hot.addr") {
		t.Errorf("Validate() error %q does not mention hot.addr", err.Error())
	}
}

func TestValidate_AllErrors(t *testing.T) {
	cfg := &Config{} // Everything zero-valued.

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected multiple errors for empty config, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "port") {
		t.Errorf("expected 'port' in error: %s", errStr)
	}
	if !strings.Contains(errStr, "brokers") {
		t.Errorf("expected 'brokers' in error: %s", errStr)
	}
	if !strings.Contains(errStr, "hot.addr") {
		t.Errorf("expected 'hot.addr' in error: %s", errStr)
	}
}

func TestValidate_PassesWithValidConfig(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestToHotConfig(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	hotCfg := cfg.ToHotConfig()
	if hotCfg.Addr != "localhost:6379" {
		t.Errorf("ToHotConfig().Addr = %q, want %q", hotCfg.Addr, "localhost:6379")
	}
	if hotCfg.PoolSize != 10 {
		t.Errorf("ToHotConfig().PoolSize = %d, want 10", hotCfg.PoolSize)
	}
	if hotCfg.DB != 0 {
		t.Errorf("ToHotConfig().DB = %d, want 0", hotCfg.DB)
	}
}

func TestToWarmConfig(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	warmCfg := cfg.ToWarmConfig()
	if warmCfg.Addr != "localhost:8812" {
		t.Errorf("ToWarmConfig().Addr = %q, want %q", warmCfg.Addr, "localhost:8812")
	}
	if warmCfg.Database != "paryty" {
		t.Errorf("ToWarmConfig().Database = %q, want %q", warmCfg.Database, "paryty")
	}
	if warmCfg.Username != "admin" {
		t.Errorf("ToWarmConfig().Username = %q, want %q", warmCfg.Username, "admin")
	}
	if warmCfg.Password != "quest" {
		t.Errorf("ToWarmConfig().Password = %q, want %q", warmCfg.Password, "quest")
	}
	if warmCfg.MaxConns != 20 {
		t.Errorf("ToWarmConfig().MaxConns = %d, want 20", warmCfg.MaxConns)
	}
}

func TestToStreamConfig(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	streamCfg := cfg.ToStreamConfig()
	if len(streamCfg.Brokers) != 1 || streamCfg.Brokers[0] != "localhost:9092" {
		t.Errorf("ToStreamConfig().Brokers = %v, want [localhost:9092]", streamCfg.Brokers)
	}
	if streamCfg.ClientID != "test-client" {
		t.Errorf("ToStreamConfig().ClientID = %q, want %q", streamCfg.ClientID, "test-client")
	}
}

func TestToColdConfig(t *testing.T) {
	path := writeTempYAML(t, minimalYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	coldCfg := cfg.ToColdConfig()
	if coldCfg.Endpoint != "localhost:8333" {
		t.Errorf("ToColdConfig().Endpoint = %q, want %q", coldCfg.Endpoint, "localhost:8333")
	}
	if coldCfg.AccessKey != "minioadmin" {
		t.Errorf("ToColdConfig().AccessKey = %q, want %q", coldCfg.AccessKey, "minioadmin")
	}
	if coldCfg.UseSSL {
		t.Errorf("ToColdConfig().UseSSL = true, want false")
	}
	if coldCfg.Region != "us-east-1" {
		t.Errorf("ToColdConfig().Region = %q, want %q", coldCfg.Region, "us-east-1")
	}
}

func TestTTLDuration(t *testing.T) {
	tests := []struct {
		name string
		ttl  string
		want int64 // seconds
	}{
		{name: "5 minutes", ttl: "5m", want: 300},
		{name: "1 hour", ttl: "1h", want: 3600},
		{name: "empty falls back to 5m", ttl: "", want: 300},
		{name: "invalid falls back to 5m", ttl: "bogus", want: 300},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Cluster.Storage.Hot.TTL = tc.ttl
			d := cfg.TTLDuration()
			if d.Seconds() != float64(tc.want) {
				t.Errorf("TTLDuration() = %v, want %ds", d, tc.want)
			}
		})
	}
}

func TestILPAddr(t *testing.T) {
	cfg := &Config{}
	cfg.Cluster.Storage.Warm.ILPAddr = "localhost:9009"

	if got := cfg.ILPAddr(); got != "localhost:9009" {
		t.Errorf("ILPAddr() = %q, want %q", got, "localhost:9009")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/cluster.yaml")
	if err == nil {
		t.Fatal("Load() expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	// Unclosed flow mapping is a real YAML syntax error.
	path := writeTempYAML(t, "{key: value, nested: [unclosed")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected error for invalid YAML, got nil")
	}
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{input: "a,b,c", want: []string{"a", "b", "c"}},
		{input: " a , b , c ", want: []string{"a", "b", "c"}},
		{input: "single", want: []string{"single"}},
		{input: "a,,b", want: []string{"a", "b"}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := splitAndTrim(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("splitAndTrim(%q) = %v (len %d), want %v (len %d)", tc.input, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("splitAndTrim(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}
