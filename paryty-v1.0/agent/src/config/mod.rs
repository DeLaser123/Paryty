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
    /// Optional tenant ID for multi-tenant routing.
    /// When set, the agent sends `x-tenant-id` gRPC metadata.
    /// Can also be set via `PARYTY_TENANT_ID` environment variable.
    #[serde(default)]
    pub tenant_id: Option<String>,
    /// Assigned twin ID (set by cluster after registration).
    /// Fallback env var: `PARYTY_TWIN_ID` — gRPC assignment is primary.
    #[serde(default)]
    pub twin_id: Option<String>,
    /// Assigned client ID (set by cluster after registration).
    /// Fallback env var: `PARYTY_CLIENT_ID` — gRPC assignment is primary.
    #[serde(default)]
    pub client_id: Option<String>,
    /// Assigned cluster agent ID for auto-pairing on registration.
    #[serde(default)]
    pub cluster_agent_id: Option<String>,
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
    /// Ring buffer size in KB for eBPF event delivery (default: 256)
    #[serde(default = "default_ring_buffer_size_kb")]
    pub ring_buffer_size_kb: u32,
    /// Poll interval in milliseconds for ring buffer consumption (default: 100)
    #[serde(default = "default_poll_interval_ms")]
    pub poll_interval_ms: u64,
    /// Whether to fall back to /proc-based collection when eBPF is unavailable (default: true)
    #[serde(default = "default_fallback_to_proc")]
    pub fallback_to_proc: bool,
}

fn default_ring_buffer_size_kb() -> u32 {
    256
}

fn default_poll_interval_ms() -> u64 {
    100
}

fn default_fallback_to_proc() -> bool {
    true
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
    // The config file may contain the agent API key. On Unix, warn loudly
    // when it is readable by group or others — a world-readable key lets any
    // local user impersonate this agent to the cluster.
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        if let Ok(meta) = std::fs::metadata(config_path) {
            let mode = meta.permissions().mode();
            if mode & 0o077 != 0 {
                tracing::warn!(
                    path = config_path,
                    mode = format!("{:o}", mode & 0o777),
                    "config file is readable by group/others; it may contain the agent API key — run: chmod 600"
                );
            }
        }
    }

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

    if let Ok(tenant_id) = std::env::var("PARYTY_TENANT_ID") {
        config.agent.tenant_id = Some(tenant_id);
    }

    // Override twin ID from env var (fallback only; gRPC assignment is primary).
    if let Ok(twin_id) = std::env::var("PARYTY_TWIN_ID") {
        config.agent.twin_id = Some(twin_id);
    }

    // Override client ID from env var (fallback only; gRPC assignment is primary).
    if let Ok(client_id) = std::env::var("PARYTY_CLIENT_ID") {
        config.agent.client_id = Some(client_id);
    }

    // Override cluster agent ID from env var for auto-pairing.
    if let Ok(id) = std::env::var("PARYTY_CLUSTER_AGENT_ID") {
        config.agent.cluster_agent_id = Some(id);
    }

    // Generate agent ID if set to "auto"
    if config.agent.id == "auto" {
        config.agent.id = uuid::Uuid::new_v4().to_string();
    }

    // Validate the merged configuration
    validate(&config)?;

    Ok(config)
}

/// Persist a new API key to the agent config file.
///
/// Reads the existing YAML, updates the `agent.api_key` field, and writes it back.
/// If the file doesn't exist, creates a minimal config with just the API key.
pub fn persist_api_key(config_path: &str, new_key: &str) -> Result<()> {
    use std::path::Path;

    let path = Path::new(config_path);

    // Read existing config or create minimal one.
    let mut doc: serde_yaml::Value = if path.exists() {
        let content = std::fs::read_to_string(path)
            .context(format!("Failed to read config file: {}", config_path))?;
        serde_yaml::from_str(&content)
            .context(format!("Failed to parse config file: {}", config_path))?
    } else {
        // Create parent directories if needed.
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)
                .context(format!("Failed to create config directory: {:?}", parent))?;
        }
        serde_yaml::Value::Mapping(serde_yaml::mapping::Mapping::new())
    };

    // Navigate to agent.api_key and set it.
    let agent_key = serde_yaml::Value::String("agent".to_string());
    let api_key_key = serde_yaml::Value::String("api_key".to_string());
    let new_key_val = serde_yaml::Value::String(new_key.to_string());

    if let serde_yaml::Value::Mapping(ref mut map) = doc {
        // Get or create the agent mapping.
        let agent_entry = map
            .entry(agent_key.clone())
            .or_insert_with(|| serde_yaml::Value::Mapping(serde_yaml::mapping::Mapping::new()));
        if let serde_yaml::Value::Mapping(ref mut agent_map) = agent_entry {
            agent_map.insert(api_key_key, new_key_val);
        }
    }

    // Write back to file.
    let yaml_str = serde_yaml::to_string(&doc).context("Failed to serialize config to YAML")?;
    std::fs::write(path, yaml_str)
        .context(format!("Failed to write config file: {}", config_path))?;

    // On Unix, set file permissions to 600 (owner read/write only).
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let perms = std::fs::Permissions::from_mode(0o600);
        std::fs::set_permissions(path, perms)
            .context(format!("Failed to set file permissions: {}", config_path))?;
    }

    Ok(())
}

/// Re-read the API key from config file or environment variable.
///
/// Called during key rotation recovery. Checks environment variable first,
/// then falls back to the config file.
pub fn reread_api_key(config_path: &str) -> Result<Option<String>> {
    // Environment variable takes precedence.
    if let Ok(key) = std::env::var("PARYTY_API_KEY") {
        if !key.is_empty() {
            return Ok(Some(key));
        }
    }

    // Fall back to config file.
    if std::path::Path::new(config_path).exists() {
        let content = std::fs::read_to_string(config_path)
            .context(format!("Failed to read config file: {}", config_path))?;
        let config: Config = load_from_str(&content)?;
        if !config.agent.api_key.is_empty() {
            return Ok(Some(config.agent.api_key));
        }
    }

    Ok(None)
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
    fn test_env_override_twin_client_id() {
        // Ensure clean environment for this test
        std::env::remove_var("PARYTY_TWIN_ID");
        std::env::remove_var("PARYTY_CLIENT_ID");

        let twin_key = "PARYTY_TWIN_ID";
        let client_key = "PARYTY_CLIENT_ID";
        let test_twin = "twin-abc-123";
        let test_client = "client-xyz-789";

        std::env::set_var(twin_key, test_twin);
        std::env::set_var(client_key, test_client);

        let config = load_from_str(VALID_YAML).expect("should load with env override");

        assert_eq!(config.agent.twin_id.as_deref(), Some(test_twin));
        assert_eq!(config.agent.client_id.as_deref(), Some(test_client));

        // Cleanup
        std::env::remove_var(twin_key);
        std::env::remove_var(client_key);
    }

    #[test]
    fn test_twin_client_id_default_none() {
        let config = try_parse(VALID_YAML).expect("valid YAML should parse");
        assert!(config.agent.twin_id.is_none(), "twin_id should default to None");
        assert!(config.agent.client_id.is_none(), "client_id should default to None");
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
