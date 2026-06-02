#![allow(dead_code)]

//! Dependency discovery for automatic service detection
//!
//! Implements port scanning and service fingerprinting to build
//! a dependency graph of running services.
//!
//! Features:
//! - Well-known port scanning (HTTP, gRPC, databases, caches)
//! - Service fingerprinting via protocol detection
//! - Dependency graph building

use std::collections::{HashMap, HashSet};
use std::net::{IpAddr, SocketAddr};
use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::time::timeout;
use tracing::{debug, info};

/// Discovery configuration
#[derive(Debug, Clone)]
pub struct DiscoveryConfig {
    /// Host to scan (default: localhost)
    pub host: IpAddr,
    /// Port range to scan (start, end)
    pub port_range: (u16, u16),
    /// Connection timeout per port
    pub timeout: Duration,
    /// Maximum concurrent scans
    pub max_concurrent: usize,
}

impl Default for DiscoveryConfig {
    fn default() -> Self {
        Self {
            host: IpAddr::V4(std::net::Ipv4Addr::LOCALHOST),
            port_range: (1, 1024),
            timeout: Duration::from_millis(100),
            max_concurrent: 100,
        }
    }
}

/// Detected service type
#[derive(Debug, Clone, PartialEq, Eq, Hash, serde::Serialize)]
pub enum ServiceType {
    /// HTTP/HTTPS service
    Http,
    /// gRPC service
    Grpc,
    /// PostgreSQL database
    Postgres,
    /// MySQL database
    Mysql,
    /// Redis cache
    Redis,
    /// Kafka/Redpanda stream
    Kafka,
    /// Dragonfly cache
    Dragonfly,
    /// QuestDB time-series database
    QuestDB,
    /// SeaweedFS storage
    SeaweedFS,
    /// Unknown service
    Unknown,
}

/// Discovered service
#[derive(Debug, Clone, serde::Serialize)]
pub struct DiscoveredService {
    /// Service address
    pub address: SocketAddr,
    /// Detected service type
    pub service_type: ServiceType,
    /// Confidence level (0-100)
    pub confidence: u8,
    /// Optional version string
    pub version: Option<String>,
}

/// Dependency graph node
#[derive(Debug, Clone)]
pub struct DependencyNode {
    /// Service address
    pub address: SocketAddr,
    /// Service type
    pub service_type: ServiceType,
    /// Services this depends on
    pub depends_on: HashSet<SocketAddr>,
    /// Services that depend on this
    pub depended_by: HashSet<SocketAddr>,
}

/// Dependency discovery engine
pub struct DiscoveryEngine {
    /// Configuration
    config: DiscoveryConfig,
    /// Discovered services
    services: HashMap<SocketAddr, DiscoveredService>,
    /// Dependency graph
    graph: HashMap<SocketAddr, DependencyNode>,
}

impl DiscoveryEngine {
    /// Create a new discovery engine
    pub fn new(config: DiscoveryConfig) -> Self {
        Self { config, services: HashMap::new(), graph: HashMap::new() }
    }

    /// Run discovery scan
    pub async fn discover(&mut self) -> Vec<DiscoveredService> {
        info!(
            host = %self.config.host,
            ports = format!("{}-{}", self.config.port_range.0, self.config.port_range.1),
            "Starting dependency discovery"
        );

        let mut handles = Vec::new();
        let (start, end) = self.config.port_range;

        // Scan ports in batches
        for port in start..=end {
            let host = self.config.host;
            let timeout_duration = self.config.timeout;

            let handle = tokio::spawn(async move {
                let addr = SocketAddr::new(host, port);
                Self::probe_port(addr, timeout_duration).await
            });

            handles.push(handle);

            // Limit concurrency
            if handles.len() >= self.config.max_concurrent {
                for handle in handles.drain(..) {
                    if let Ok(Some(service)) = handle.await {
                        self.services.insert(service.address, service);
                    }
                }
            }
        }

        // Collect remaining results
        for handle in handles {
            if let Ok(Some(service)) = handle.await {
                self.services.insert(service.address, service);
            }
        }

        info!(count = self.services.len(), "Discovery complete");

        self.services.values().cloned().collect()
    }

    /// Probe a single port
    async fn probe_port(addr: SocketAddr, timeout_duration: Duration) -> Option<DiscoveredService> {
        match timeout(timeout_duration, TcpStream::connect(addr)).await {
            Ok(Ok(mut stream)) => {
                debug!(addr = %addr, "Port open");

                // Try to identify service
                let (service_type, confidence, version) =
                    Self::identify_service(&mut stream, addr).await;

                Some(DiscoveredService { address: addr, service_type, confidence, version })
            }
            _ => None,
        }
    }

    /// Identify service type by protocol detection
    async fn identify_service(
        stream: &mut TcpStream,
        addr: SocketAddr,
    ) -> (ServiceType, u8, Option<String>) {
        // Try HTTP detection
        if let Ok(Some(version)) = Self::try_http(stream).await {
            return (ServiceType::Http, 90, Some(version));
        }

        // Try PostgreSQL detection
        if Self::try_postgres(stream).await {
            return (ServiceType::Postgres, 85, None);
        }

        // Try Redis detection
        if Self::try_redis(stream).await {
            return (ServiceType::Redis, 85, None);
        }

        // Try MySQL detection
        if Self::try_mysql(stream).await {
            return (ServiceType::Mysql, 85, None);
        }

        // Fall back to well-known port detection
        let service_type = Self::well_known_port(addr.port());
        (service_type, 50, None)
    }

    /// Try HTTP detection
    async fn try_http(stream: &mut TcpStream) -> Result<Option<String>, ()> {
        let request = b"OPTIONS / HTTP/1.0\r\nHost: localhost\r\n\r\n";
        if stream.write_all(request).await.is_err() {
            return Err(());
        }

        let mut buf = [0u8; 1024];
        if let Ok(Ok(n)) = timeout(Duration::from_millis(100), stream.read(&mut buf)).await {
            if n > 0 {
                let response = String::from_utf8_lossy(&buf[..n]);
                if response.starts_with("HTTP/") {
                    // Extract server header
                    for line in response.lines() {
                        if line.to_lowercase().starts_with("server:") {
                            let version = line[7..].trim().to_string();
                            return Ok(Some(version));
                        }
                    }
                    return Ok(Some("unknown".to_string()));
                }
            }
        }

        Err(())
    }

    /// Try PostgreSQL detection
    async fn try_postgres(stream: &mut TcpStream) -> bool {
        // PostgreSQL startup message
        let startup = [
            0x00, 0x00, 0x00, 0x08, // Length
            0x00, 0x03, 0x00, 0x00, // Protocol version
        ];

        if stream.write_all(&startup).await.is_err() {
            return false;
        }

        let mut buf = [0u8; 256];
        if let Ok(Ok(n)) = timeout(Duration::from_millis(100), stream.read(&mut buf)).await {
            // PostgreSQL responds with 'R' for authentication
            if n > 0 && buf[0] == b'R' {
                return true;
            }
        }

        false
    }

    /// Try Redis detection
    async fn try_redis(stream: &mut TcpStream) -> bool {
        let ping = b"PING\r\n";
        if stream.write_all(ping).await.is_err() {
            return false;
        }

        let mut buf = [0u8; 256];
        if let Ok(Ok(n)) = timeout(Duration::from_millis(100), stream.read(&mut buf)).await {
            if n > 0 {
                let response = String::from_utf8_lossy(&buf[..n]);
                return response.contains("PONG") || response.contains("NOAUTH");
            }
        }

        false
    }

    /// Try MySQL detection
    async fn try_mysql(stream: &mut TcpStream) -> bool {
        let mut buf = [0u8; 256];
        if let Ok(Ok(n)) = timeout(Duration::from_millis(100), stream.read(&mut buf)).await {
            // MySQL sends a greeting packet starting with protocol version
            if n > 5 {
                let protocol = buf[4];
                if protocol == 0x0a || protocol == 0x09 {
                    return true;
                }
            }
        }

        false
    }

    /// Map well-known ports to service types
    fn well_known_port(port: u16) -> ServiceType {
        match port {
            80 | 443 | 8080 | 8443 => ServiceType::Http,
            5432 => ServiceType::Postgres,
            3306 => ServiceType::Mysql,
            6379 => ServiceType::Redis,
            9092 | 9093 => ServiceType::Kafka,
            _ => ServiceType::Unknown,
        }
    }

    /// Build dependency graph from discovered services
    pub fn build_dependency_graph(&mut self) {
        info!("Building dependency graph");

        // Create nodes for all services
        for (addr, service) in &self.services {
            self.graph.insert(
                *addr,
                DependencyNode {
                    address: *addr,
                    service_type: service.service_type.clone(),
                    depends_on: HashSet::new(),
                    depended_by: HashSet::new(),
                },
            );
        }

        // TODO: In production, analyze actual connections to build graph
        // For now, use heuristics based on service types
        // - Query layer depends on storage layer
        // - Ingestion depends on stream engine
        // - Stream engine depends on storage layer

        info!(nodes = self.graph.len(), "Dependency graph built");
    }

    /// Get dependency graph
    pub fn get_graph(&self) -> &HashMap<SocketAddr, DependencyNode> {
        &self.graph
    }

    /// Get discovered services
    pub fn get_services(&self) -> &HashMap<SocketAddr, DiscoveredService> {
        &self.services
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_discovery_config_default() {
        let config = DiscoveryConfig::default();
        assert_eq!(config.host, IpAddr::V4(std::net::Ipv4Addr::LOCALHOST));
        assert_eq!(config.port_range, (1, 1024));
    }

    #[test]
    fn test_well_known_ports() {
        assert_eq!(DiscoveryEngine::well_known_port(80), ServiceType::Http);
        assert_eq!(DiscoveryEngine::well_known_port(5432), ServiceType::Postgres);
        assert_eq!(DiscoveryEngine::well_known_port(6379), ServiceType::Redis);
        assert_eq!(DiscoveryEngine::well_known_port(9999), ServiceType::Unknown);
    }
}
