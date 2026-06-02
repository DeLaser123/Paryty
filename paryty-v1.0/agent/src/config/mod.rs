#![allow(dead_code)]

//! Configuration module for Paryty Agent
//!
//! Handles loading and parsing of agent configuration from YAML files
//! and environment variables.

use anyhow::{Context, Result};
use serde::Deserialize;
use std::time::Duration;

/// Main agent configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct Config {
    pub agent: AgentConfig,
    pub layers: LayersConfig,
    pub communication: CommunicationConfig,
    pub logging: LoggingConfig,
}

/// Agent-level configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct AgentConfig {
    pub id: String,
    pub cluster_endpoint: String,
    pub api_key: String,
    pub self_metrics: SelfMetricsConfig,
}

/// Self-metrics configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct SelfMetricsConfig {
    pub enabled: bool,
    pub port: u16,
}

/// Collection layers configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct LayersConfig {
    pub metal: MetalConfig,
    pub ebpf: EbpfConfig,
    pub supervisor: SupervisorConfig,
}

/// Metal scraper configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct MetalConfig {
    pub enabled: bool,
    pub interval: String,
    pub cpu_per_core: bool,
    pub cpu_per_process: bool,
    pub memory_rss: bool,
    pub disk_io: bool,
    pub network_io: bool,
    pub process_tree: bool,
    pub container_detection: bool,
}

/// eBPF observer configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct EbpfConfig {
    pub enabled: bool,
    pub tcp_connections: bool,
    pub dns_resolution: bool,
    pub http_inspection: bool,
    pub db_inspection: bool,
    pub exclude_ports: Vec<u16>,
    pub exclude_ips: Vec<String>,
}

/// Supervisor configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct SupervisorConfig {
    pub enabled: bool,
    pub health_checks: Vec<HealthCheckConfig>,
    pub log_tailing: Vec<LogTailingConfig>,
}

/// Health check configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct HealthCheckConfig {
    pub name: String,
    pub target: String,
    pub interval: String,
}

/// Log tailing configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct LogTailingConfig {
    pub name: String,
    pub path: String,
    pub pattern: String,
}

/// Communication configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct CommunicationConfig {
    pub protocol: String,
    pub tls: TlsConfig,
    pub compression: String,
    pub edge_buffer: EdgeBufferConfig,
    pub flow_control: FlowControlConfig,
}

/// TLS configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct TlsConfig {
    pub enabled: bool,
}

/// Edge buffer configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct EdgeBufferConfig {
    pub enabled: bool,
    pub max_size_mb: u64,
    pub retention_hours: u64,
    /// Optional path to a SQLite database file for persistent edge buffering.
    /// When `None`, the buffer operates in memory-only mode.
    pub sqlite_path: Option<String>,
}

/// Flow control configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct FlowControlConfig {
    pub backpressure_enabled: bool,
    pub adaptive_sampling: bool,
    pub priority_queues: Vec<PriorityQueueConfig>,
}

/// Priority queue configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct PriorityQueueConfig {
    pub name: String,
    pub topics: Vec<String>,
    pub priority: u8,
}

/// Logging configuration
#[derive(Debug, Deserialize, Clone)]
#[serde(deny_unknown_fields)]
pub struct LoggingConfig {
    pub level: String,
    pub format: String,
    pub output: String,
}

/// Parse a human-readable duration string into a [`Duration`].
///
/// Supports the format `<number><unit>` where unit is one of:
/// - `s` (seconds)
/// - `m` (minutes)
/// - `h` (hours)
///
/// Returns an error if the format is invalid or the value is zero.
fn parse_duration(s: &str) -> Result<Duration> {
    let s = s.trim();
    if s.is_empty() {
        anyhow::bail!("duration string must not be empty");
    }

    let (num_str, unit) = s.split_at(s.len() - 1);

    let value: u64 =
        num_str.parse().with_context(|| format!("invalid duration number in '{}'", s))?;

    if value == 0 {
        anyhow::bail!("duration must be greater than zero, got '{}'", s);
    }

    let duration = match unit {
        "s" => Duration::from_secs(value),
        "m" => Duration::from_secs(value * 60),
        "h" => Duration::from_secs(value * 3600),
        _ => anyhow::bail!("unknown duration unit '{}' in '{}'; expected s, m, or h", unit, s),
    };

    Ok(duration)
}

/// Validate a fully-loaded [`Config`].
///
/// Checks:
/// - `cluster_endpoint` is non-empty
/// - All `interval` strings parse as valid durations
/// - `edge_buffer.max_size_mb > 0` when edge buffer is enabled
fn validate(config: &Config) -> Result<()> {
    // Cluster endpoint must not be empty
    if config.agent.cluster_endpoint.trim().is_empty() {
        anyhow::bail!("agent.cluster_endpoint must not be empty");
    }

    // Warn if API key looks auto-generated (UUID-shaped)
    if config.agent.api_key.len() == 36 && config.agent.api_key.contains('-') {
        tracing::warn!(
            api_key_prefix = &config.agent.api_key[..8],
            "API key appears to be an auto-generated UUID; ensure this is intentional"
        );
    }

    // Validate metal interval
    parse_duration(&config.layers.metal.interval)
        .context("layers.metal.interval is not a valid duration")?;

    // Validate health check intervals
    for hc in &config.layers.supervisor.health_checks {
        parse_duration(&hc.interval)
            .context(format!("health check '{}' has an invalid interval", hc.name))?;
    }

    // Validate edge buffer constraints when enabled
    if config.communication.edge_buffer.enabled && config.communication.edge_buffer.max_size_mb == 0
    {
        anyhow::bail!(
            "communication.edge_buffer.max_size_mb must be greater than 0 when edge buffer is enabled"
        );
    }

    Ok(())
}

/// Load configuration from file and environment.
///
/// 1. Reads YAML from the path in `PARYTY_AGENT_CONFIG` (default: `configs/agent/agent.yaml`).
/// 2. Applies environment variable overrides (`PARYTY_API_KEY`, `PARYTY_CLUSTER_ENDPOINT`).
/// 3. Auto-generates an agent ID when set to `"auto"`.
/// 4. Validates the merged configuration.
pub fn load() -> Result<Config> {
    // Try to load from environment variable first
    let config_path = std::env::var("PARYTY_AGENT_CONFIG")
        .unwrap_or_else(|_| "configs/agent/agent.yaml".to_string());

    load_from_path(&config_path)
}

/// Load configuration from a specific file path, applying env overrides and validation.
pub fn load_from_path(config_path: &str) -> Result<Config> {
    // Load configuration file
    let config_str = std::fs::read_to_string(config_path)
        .context(format!("Failed to read config file: {}", config_path))?;

    load_from_str(&config_str)
}

/// Parse a configuration from a YAML string, apply env overrides, and validate.
pub fn load_from_str(yaml: &str) -> Result<Config> {
    // Parse YAML — deny_unknown_fields is enforced by serde attribute
    let mut config: Config = serde_yaml::from_str(yaml).context("Failed to parse config YAML")?;

    // Override with environment variables
    if let Ok(api_key) = std::env::var("PARYTY_API_KEY") {
        config.agent.api_key = api_key;
    }

    if let Ok(endpoint) = std::env::var("PARYTY_CLUSTER_ENDPOINT") {
        config.agent.cluster_endpoint = endpoint;
    }

    // Generate agent ID if set to "auto"
    if config.agent.id == "auto" {
        config.agent.id = uuid::Uuid::new_v4().to_string();
    }

    // Validate the merged configuration
    validate(&config)?;

    Ok(config)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A minimal but complete valid YAML configuration.
    const VALID_YAML: &str = r#"
agent:
  id: "test-agent-001"
  cluster_endpoint: "grpc://cluster.example.com:443"
  api_key: "secret-key-123"
  self_metrics:
    enabled: true
    port: 9090
layers:
  metal:
    enabled: true
    interval: "10s"
    cpu_per_core: true
    cpu_per_process: true
    memory_rss: true
    disk_io: true
    network_io: true
    process_tree: true
    container_detection: true
  ebpf:
    enabled: true
    tcp_connections: true
    dns_resolution: true
    http_inspection: true
    db_inspection: true
    exclude_ports: [22, 53]
    exclude_ips: ["127.0.0.1"]
  supervisor:
    enabled: false
    health_checks: []
    log_tailing: []
communication:
  protocol: grpc
  tls:
    enabled: true
  compression: zstd
  edge_buffer:
    enabled: true
    max_size_mb: 100
    retention_hours: 24
  flow_control:
    backpressure_enabled: true
    adaptive_sampling: true
    priority_queues: []
logging:
  level: info
  format: json
  output: stdout
"#;

    /// Helper: parse and validate a YAML string, returning the config or error.
    fn try_parse(yaml: &str) -> Result<Config> {
        let config: Config = serde_yaml::from_str(yaml).context("parse")?;
        // Skip env overrides and auto-ID for unit tests
        validate(&config)?;
        Ok(config)
    }

    #[test]
    fn test_load_valid_config() {
        let config = try_parse(VALID_YAML).expect("valid YAML should parse");
        assert_eq!(config.agent.id, "test-agent-001");
        assert_eq!(config.agent.cluster_endpoint, "grpc://cluster.example.com:443");
        assert_eq!(config.layers.metal.interval, "10s");
        assert!(config.communication.edge_buffer.enabled);
        assert_eq!(config.communication.edge_buffer.max_size_mb, 100);
        assert_eq!(config.logging.level, "info");
    }

    #[test]
    fn test_env_override() {
        // Ensure a clean environment for this test
        std::env::remove_var("PARYTY_API_KEY");
        std::env::remove_var("PARYTY_CLUSTER_ENDPOINT");

        let endpoint_key = "PARYTY_CLUSTER_ENDPOINT";
        let test_endpoint = "grpc://override.example.com:9999";

        // Set the override
        std::env::set_var(endpoint_key, test_endpoint);

        // load_from_str applies env overrides + validation
        let config = load_from_str(VALID_YAML).expect("should load with env override");

        assert_eq!(config.agent.cluster_endpoint, test_endpoint);

        // Cleanup
        std::env::remove_var(endpoint_key);
    }

    #[test]
    fn test_deny_unknown_fields() {
        let yaml_with_unknown = r#"
agent:
  id: "test-agent"
  cluster_endpoint: "grpc://cluster.example.com:443"
  api_key: "key"
  self_metrics:
    enabled: true
    port: 9090
layers:
  metal:
    enabled: true
    interval: "10s"
    cpu_per_core: true
    cpu_per_process: true
    memory_rss: true
    disk_io: true
    network_io: true
    process_tree: true
    container_detection: true
    this_field_does_not_exist: true
  ebpf:
    enabled: false
    tcp_connections: false
    dns_resolution: false
    http_inspection: false
    db_inspection: false
    exclude_ports: []
    exclude_ips: []
  supervisor:
    enabled: false
    health_checks: []
    log_tailing: []
communication:
  protocol: grpc
  tls:
    enabled: false
  compression: zstd
  edge_buffer:
    enabled: false
    max_size_mb: 0
    retention_hours: 0
  flow_control:
    backpressure_enabled: false
    adaptive_sampling: false
    priority_queues: []
logging:
  level: info
  format: json
  output: stdout
"#;

        let result = try_parse(yaml_with_unknown);
        assert!(result.is_err(), "YAML with unknown field should fail to parse");
        // anyhow's Debug format includes the full error chain
        let err_msg = format!("{:?}", result.unwrap_err());
        assert!(
            err_msg.contains("unknown field"),
            "error chain should mention 'unknown field', got: {}",
            err_msg
        );
    }

    #[test]
    fn test_validation_failure_empty_endpoint() {
        let yaml_empty_endpoint = r#"
agent:
  id: "test-agent"
  cluster_endpoint: ""
  api_key: "key"
  self_metrics:
    enabled: true
    port: 9090
layers:
  metal:
    enabled: true
    interval: "10s"
    cpu_per_core: true
    cpu_per_process: true
    memory_rss: true
    disk_io: true
    network_io: true
    process_tree: true
    container_detection: true
  ebpf:
    enabled: false
    tcp_connections: false
    dns_resolution: false
    http_inspection: false
    db_inspection: false
    exclude_ports: []
    exclude_ips: []
  supervisor:
    enabled: false
    health_checks: []
    log_tailing: []
communication:
  protocol: grpc
  tls:
    enabled: false
  compression: zstd
  edge_buffer:
    enabled: false
    max_size_mb: 0
    retention_hours: 0
  flow_control:
    backpressure_enabled: false
    adaptive_sampling: false
    priority_queues: []
logging:
  level: info
  format: json
  output: stdout
"#;

        let result = try_parse(yaml_empty_endpoint);
        assert!(result.is_err(), "empty endpoint should be rejected");
        let err_msg = format!("{}", result.unwrap_err());
        assert!(
            err_msg.contains("cluster_endpoint"),
            "error should mention cluster_endpoint, got: {}",
            err_msg
        );
    }

    #[test]
    fn test_validation_failure_zero_edge_buffer_size() {
        let yaml_zero_buffer = r#"
agent:
  id: "test-agent"
  cluster_endpoint: "grpc://cluster.example.com:443"
  api_key: "key"
  self_metrics:
    enabled: true
    port: 9090
layers:
  metal:
    enabled: true
    interval: "10s"
    cpu_per_core: true
    cpu_per_process: true
    memory_rss: true
    disk_io: true
    network_io: true
    process_tree: true
    container_detection: true
  ebpf:
    enabled: false
    tcp_connections: false
    dns_resolution: false
    http_inspection: false
    db_inspection: false
    exclude_ports: []
    exclude_ips: []
  supervisor:
    enabled: false
    health_checks: []
    log_tailing: []
communication:
  protocol: grpc
  tls:
    enabled: false
  compression: zstd
  edge_buffer:
    enabled: true
    max_size_mb: 0
    retention_hours: 24
  flow_control:
    backpressure_enabled: false
    adaptive_sampling: false
    priority_queues: []
logging:
  level: info
  format: json
  output: stdout
"#;

        let result = try_parse(yaml_zero_buffer);
        assert!(result.is_err(), "enabled edge buffer with max_size_mb=0 should be rejected");
        let err_msg = format!("{}", result.unwrap_err());
        assert!(
            err_msg.contains("max_size_mb"),
            "error should mention max_size_mb, got: {}",
            err_msg
        );
    }

    #[test]
    fn test_sqlite_path_optional() {
        // Config without sqlite_path should still parse
        let config = try_parse(VALID_YAML).expect("should parse without sqlite_path");
        assert!(
            config.communication.edge_buffer.sqlite_path.is_none(),
            "sqlite_path should default to None when omitted"
        );
    }

    #[test]
    fn test_sqlite_path_present() {
        let yaml = VALID_YAML.replace(
            "    retention_hours: 24",
            "    retention_hours: 24\n    sqlite_path: \"/var/lib/paryty/edge.db\"",
        );
        let config = try_parse(&yaml).expect("should parse with sqlite_path");
        assert_eq!(
            config.communication.edge_buffer.sqlite_path.as_deref(),
            Some("/var/lib/paryty/edge.db")
        );
    }

    #[test]
    fn test_parse_duration_valid() {
        assert_eq!(parse_duration("5s").unwrap(), Duration::from_secs(5));
        assert_eq!(parse_duration("1m").unwrap(), Duration::from_secs(60));
        assert_eq!(parse_duration("2h").unwrap(), Duration::from_secs(7200));
    }

    #[test]
    fn test_parse_duration_invalid() {
        assert!(parse_duration("").is_err());
        assert!(parse_duration("0s").is_err());
        assert!(parse_duration("abc").is_err());
        assert!(parse_duration("5x").is_err());
    }

    #[test]
    fn test_validation_failure_invalid_interval() {
        let yaml_bad_interval = r#"
agent:
  id: "test-agent"
  cluster_endpoint: "grpc://cluster.example.com:443"
  api_key: "key"
  self_metrics:
    enabled: true
    port: 9090
layers:
  metal:
    enabled: true
    interval: "not-a-duration"
    cpu_per_core: true
    cpu_per_process: true
    memory_rss: true
    disk_io: true
    network_io: true
    process_tree: true
    container_detection: true
  ebpf:
    enabled: false
    tcp_connections: false
    dns_resolution: false
    http_inspection: false
    db_inspection: false
    exclude_ports: []
    exclude_ips: []
  supervisor:
    enabled: false
    health_checks: []
    log_tailing: []
communication:
  protocol: grpc
  tls:
    enabled: false
  compression: zstd
  edge_buffer:
    enabled: false
    max_size_mb: 0
    retention_hours: 0
  flow_control:
    backpressure_enabled: false
    adaptive_sampling: false
    priority_queues: []
logging:
  level: info
  format: json
  output: stdout
"#;

        let result = try_parse(yaml_bad_interval);
        assert!(result.is_err(), "invalid interval should be rejected");
        let err_msg = format!("{}", result.unwrap_err());
        assert!(err_msg.contains("interval"), "error should mention interval, got: {}", err_msg);
    }
}
