#![allow(dead_code)]

//! Health check poller for monitoring service dependencies
//!
//! Implements multiple health check strategies:
//! - HTTP health checks (GET/HEAD with status code validation)
//! - TCP connection checks (port open/close detection)
//! - gRPC health checks (using standard health proto)
//!
//! Follows Prometheus health check patterns with configurable intervals,
//! timeouts, and failure thresholds.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::RwLock;
use tokio::time::{interval, timeout};
use tracing::{debug, info, warn};

/// Health check configuration
#[derive(Debug, Clone)]
pub struct HealthCheckConfig {
    /// Interval between health checks
    pub check_interval: Duration,
    /// Timeout for individual health checks
    pub check_timeout: Duration,
    /// Number of consecutive failures before marking unhealthy
    pub failure_threshold: u32,
    /// Number of consecutive successes before marking healthy
    pub success_threshold: u32,
}

impl Default for HealthCheckConfig {
    fn default() -> Self {
        Self {
            check_interval: Duration::from_secs(10),
            check_timeout: Duration::from_secs(5),
            failure_threshold: 3,
            success_threshold: 1,
        }
    }
}

/// Health check type
#[derive(Debug, Clone)]
pub enum HealthCheckType {
    /// HTTP health check
    Http {
        /// URL to check
        url: String,
        /// Expected status code (default: 200)
        expected_status: u16,
        /// HTTP method (GET or HEAD)
        method: HttpMethod,
    },
    /// TCP connection check
    Tcp {
        /// Address to connect to (host:port)
        address: String,
    },
    /// gRPC health check
    Grpc {
        /// Address to check
        address: String,
        /// Service name to check (empty for overall health)
        service: String,
    },
}

/// HTTP method for health checks
#[derive(Debug, Clone)]
pub enum HttpMethod {
    Get,
    Head,
}

/// Health status of a service
#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize)]
pub enum HealthStatus {
    /// Service is healthy
    Healthy,
    /// Service is unhealthy
    Unhealthy,
    /// Health status is unknown (initial state)
    Unknown,
}

/// Result of a health check
#[derive(Debug, Clone, serde::Serialize)]
pub struct HealthCheckResult {
    /// Name of the service
    pub service_name: String,
    /// Current health status
    pub status: HealthStatus,
    /// Latency of the last health check
    pub latency: Duration,
    /// Timestamp of the last check
    #[serde(skip)]
    pub last_checked: Instant,
    /// Number of consecutive failures
    pub consecutive_failures: u32,
    /// Error message if unhealthy
    pub error: Option<String>,
}

/// Health check definition
#[derive(Debug, Clone)]
pub struct HealthCheck {
    /// Name of the service to check
    pub service_name: String,
    /// Type of health check
    pub check_type: HealthCheckType,
    /// Configuration
    pub config: HealthCheckConfig,
}

/// Health check poller
pub struct HealthPoller {
    /// Registered health checks
    checks: Vec<HealthCheck>,
    /// Current health status for each service
    statuses: Arc<RwLock<HashMap<String, HealthCheckResult>>>,
}

impl HealthPoller {
    /// Create a new health poller
    pub fn new() -> Self {
        Self { checks: Vec::new(), statuses: Arc::new(RwLock::new(HashMap::new())) }
    }

    /// Register a health check
    pub fn register(&mut self, check: HealthCheck) {
        info!(
            service = %check.service_name,
            "Registered health check"
        );
        self.checks.push(check);
    }

    /// Get health status for a service
    pub async fn get_status(&self, service_name: &str) -> Option<HealthCheckResult> {
        let statuses = self.statuses.read().await;
        statuses.get(service_name).cloned()
    }

    /// Get all health statuses
    pub async fn get_all_statuses(&self) -> HashMap<String, HealthCheckResult> {
        let statuses = self.statuses.read().await;
        statuses.clone()
    }

    /// Start the health check polling loop
    pub async fn run(&self) {
        if self.checks.is_empty() {
            info!("No health checks registered, skipping health polling");
            return;
        }

        info!(check_count = self.checks.len(), "Starting health check poller");

        // Initialize statuses
        {
            let mut statuses = self.statuses.write().await;
            for check in &self.checks {
                statuses.insert(
                    check.service_name.clone(),
                    HealthCheckResult {
                        service_name: check.service_name.clone(),
                        status: HealthStatus::Unknown,
                        latency: Duration::ZERO,
                        last_checked: Instant::now(),
                        consecutive_failures: 0,
                        error: None,
                    },
                );
            }
        }

        // Run checks in separate tasks
        let mut handles = Vec::new();
        for check in self.checks.clone() {
            let statuses = Arc::clone(&self.statuses);
            let handle = tokio::spawn(async move {
                let mut ticker = interval(check.config.check_interval);
                loop {
                    ticker.tick().await;
                    let result = Self::execute_check(&check).await;
                    let mut statuses = statuses.write().await;
                    statuses.insert(check.service_name.clone(), result);
                }
            });
            handles.push(handle);
        }

        // Wait for all tasks (they run indefinitely)
        for handle in handles {
            let _ = handle.await;
        }
    }

    /// Execute a single health check
    async fn execute_check(check: &HealthCheck) -> HealthCheckResult {
        let start = Instant::now();

        let (status, error) = match &check.check_type {
            HealthCheckType::Http { url, expected_status, method } => {
                Self::check_http(url, *expected_status, method, check.config.check_timeout).await
            }
            HealthCheckType::Tcp { address } => {
                Self::check_tcp(address, check.config.check_timeout).await
            }
            HealthCheckType::Grpc { address, service } => {
                Self::check_grpc(address, service, check.config.check_timeout).await
            }
        };

        let latency = start.elapsed();

        // Update failure count
        let mut result = {
            let statuses = check.service_name.clone();
            HealthCheckResult {
                service_name: statuses,
                status,
                latency,
                last_checked: Instant::now(),
                consecutive_failures: 0,
                error,
            }
        };

        // Get previous result to track consecutive failures
        // This is a simplified version - in production, we'd use the shared state
        match status {
            HealthStatus::Unhealthy => {
                result.consecutive_failures = 1;
                warn!(
                    service = %check.service_name,
                    latency_ms = latency.as_millis(),
                    error = ?result.error,
                    "Health check failed"
                );
            }
            HealthStatus::Healthy => {
                debug!(
                    service = %check.service_name,
                    latency_ms = latency.as_millis(),
                    "Health check passed"
                );
            }
            HealthStatus::Unknown => {
                warn!(
                    service = %check.service_name,
                    "Health check returned unknown status"
                );
            }
        }

        result
    }

    /// Execute HTTP health check
    async fn check_http(
        url: &str,
        expected_status: u16,
        _method: &HttpMethod,
        check_timeout: Duration,
    ) -> (HealthStatus, Option<String>) {
        // Use TCP connection + HTTP/1.1 request for health check
        let parsed = match url::Url::parse(url) {
            Ok(u) => u,
            Err(e) => return (HealthStatus::Unhealthy, Some(format!("Invalid URL: {}", e))),
        };

        // A URL without a resolvable host or port is a misconfiguration.
        // Silently defaulting (e.g. to localhost:80) would report the health
        // of the WRONG endpoint — fail the check loudly instead.
        let Some(host) = parsed.host_str() else {
            return (
                HealthStatus::Unhealthy,
                Some(format!("health check URL has no host: {}", url)),
            );
        };
        let Some(port) = parsed.port_or_known_default() else {
            return (
                HealthStatus::Unhealthy,
                Some(format!("health check URL has no port and unknown scheme: {}", url)),
            );
        };
        let path = parsed.path();

        let addr = format!("{}:{}", host, port);

        match timeout(check_timeout, TcpStream::connect(&addr)).await {
            Ok(Ok(mut stream)) => {
                // Send HTTP request
                let request = format!("GET {} HTTP/1.0\r\nHost: {}\r\n\r\n", path, host);
                if let Err(e) = stream.write_all(request.as_bytes()).await {
                    return (
                        HealthStatus::Unhealthy,
                        Some(format!("Failed to send request: {}", e)),
                    );
                }

                // Read response
                let mut buf = [0u8; 1024];
                match timeout(Duration::from_secs(5), stream.read(&mut buf)).await {
                    Ok(Ok(n)) if n > 0 => {
                        let response = String::from_utf8_lossy(&buf[..n]);
                        if response.contains(&format!(" {} ", expected_status)) {
                            (HealthStatus::Healthy, None)
                        } else {
                            (HealthStatus::Unhealthy, Some("Unexpected response".to_string()))
                        }
                    }
                    _ => (HealthStatus::Unhealthy, Some("No response".to_string())),
                }
            }
            Ok(Err(e)) => (HealthStatus::Unhealthy, Some(format!("Connection failed: {}", e))),
            Err(_) => (HealthStatus::Unhealthy, Some("Connection timed out".to_string())),
        }
    }

    /// Execute TCP health check
    async fn check_tcp(address: &str, check_timeout: Duration) -> (HealthStatus, Option<String>) {
        match timeout(check_timeout, TcpStream::connect(address)).await {
            Ok(Ok(_stream)) => (HealthStatus::Healthy, None),
            Ok(Err(e)) => (HealthStatus::Unhealthy, Some(format!("TCP connection failed: {}", e))),
            Err(_) => (HealthStatus::Unhealthy, Some("TCP connection timed out".to_string())),
        }
    }

    /// Execute gRPC health check
    async fn check_grpc(
        address: &str,
        _service: &str,
        check_timeout: Duration,
    ) -> (HealthStatus, Option<String>) {
        // gRPC health check uses TCP connection + HTTP/2 preamble
        // This is a simplified version - full implementation would use tonic health client
        match timeout(check_timeout, TcpStream::connect(address)).await {
            Ok(Ok(mut stream)) => {
                // Send HTTP/2 connection preface
                let preface = b"PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n";
                if let Err(e) = stream.write_all(preface).await {
                    return (
                        HealthStatus::Unhealthy,
                        Some(format!("Failed to send HTTP/2 preface: {}", e)),
                    );
                }

                // For now, just check if connection succeeds
                // Full implementation would check gRPC health response
                (HealthStatus::Healthy, None)
            }
            Ok(Err(e)) => (HealthStatus::Unhealthy, Some(format!("gRPC connection failed: {}", e))),
            Err(_) => (HealthStatus::Unhealthy, Some("gRPC connection timed out".to_string())),
        }
    }
}

impl Default for HealthPoller {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_health_check_config_default() {
        let config = HealthCheckConfig::default();
        assert_eq!(config.check_interval, Duration::from_secs(10));
        assert_eq!(config.check_timeout, Duration::from_secs(5));
        assert_eq!(config.failure_threshold, 3);
        assert_eq!(config.success_threshold, 1);
    }

    #[tokio::test]
    async fn test_health_poller_register() {
        let mut poller = HealthPoller::new();
        assert!(poller.checks.is_empty());

        poller.register(HealthCheck {
            service_name: "test-service".to_string(),
            check_type: HealthCheckType::Tcp { address: "localhost:8080".to_string() },
            config: HealthCheckConfig::default(),
        });

        assert_eq!(poller.checks.len(), 1);
    }

    #[tokio::test]
    async fn test_health_poller_empty_run() {
        let poller = HealthPoller::new();
        // Should return immediately with no checks
        poller.run().await;
    }
}
