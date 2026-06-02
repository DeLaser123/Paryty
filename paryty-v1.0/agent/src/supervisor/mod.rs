#![allow(dead_code)]

//! Supervisor module
//!
//! Provides service supervision capabilities:
//! - Health check polling for dependencies
//! - Log tailing for centralized log collection
//! - Dependency discovery for automatic service detection
//!
//! This module implements the supervisor pattern, monitoring
//! dependent services and providing observability into the
//! infrastructure.

pub mod discovery;
pub mod health;
pub mod log_tailer;

use std::sync::Arc;

use tokio::sync::mpsc;
use tracing::{debug, error, info, warn};

use self::discovery::{DiscoveryConfig, DiscoveryEngine};
use self::health::{HealthCheck, HealthPoller};
use self::log_tailer::{LogLine, LogTailer, LogTailerConfig};
use crate::communication::Client;

/// Supervisor configuration
#[derive(Debug, Clone, Default)]
pub struct SupervisorConfig {
    /// Health check configuration
    pub health: Vec<HealthCheck>,
    /// Log tailer configurations
    pub log_tailers: Vec<LogTailerConfig>,
    /// Discovery configuration
    pub discovery: Option<DiscoveryConfig>,
}

/// Supervisor instance
pub struct Supervisor {
    config: SupervisorConfig,
    health_poller: HealthPoller,
    log_tx: mpsc::Sender<LogLine>,
    log_rx: mpsc::Receiver<LogLine>,
}

impl Supervisor {
    /// Create a new supervisor
    pub fn new(config: SupervisorConfig) -> Self {
        let mut health_poller = HealthPoller::new();

        // Register health checks
        for check in &config.health {
            health_poller.register(check.clone());
        }

        let (log_tx, log_rx) = mpsc::channel(1024);

        Self { config, health_poller, log_tx, log_rx }
    }

    /// Get health status for a service
    pub async fn get_health_status(&self, service_name: &str) -> Option<health::HealthCheckResult> {
        self.health_poller.get_status(service_name).await
    }

    /// Get all health statuses
    pub async fn get_all_health_statuses(
        &self,
    ) -> std::collections::HashMap<String, health::HealthCheckResult> {
        self.health_poller.get_all_statuses().await
    }

    /// Get log receiver for consuming log lines
    pub fn take_log_receiver(&mut self) -> Option<mpsc::Receiver<LogLine>> {
        // This takes ownership of the receiver, so it can only be called once
        // In production, we'd use Arc<Mutex<Receiver>> or broadcast channel
        Some(std::mem::replace(&mut self.log_rx, mpsc::channel(1).1))
    }

    /// Run supervisor (blocks indefinitely)
    ///
    /// Health check results and log lines are sent to the cluster via the
    /// communication client.
    pub async fn run(&mut self, client: &Client) {
        info!("Starting supervisor");

        let mut handles = Vec::new();

        // Start health poller with periodic reporting
        let health_poller = Arc::new(tokio::sync::RwLock::new(HealthPoller::new()));
        for check in &self.config.health {
            health_poller.write().await.register(check.clone());
        }

        let poller = Arc::clone(&health_poller);
        let health_client = client.clone();
        let health_handle = tokio::spawn(async move {
            // Run health checks in background
            let poller_run = Arc::clone(&poller);
            let _run_handle = tokio::spawn(async move {
                poller_run.read().await.run().await;
            });

            // Periodically send health status to cluster
            let mut ticker = tokio::time::interval(std::time::Duration::from_secs(30));
            loop {
                ticker.tick().await;
                let statuses = poller.read().await.get_all_statuses().await;
                if !statuses.is_empty() {
                    match serde_json::to_vec(&statuses) {
                        Ok(data) => {
                            if let Err(e) = health_client.send_events(&data).await {
                                warn!("Failed to send health status: {}", e);
                            } else {
                                debug!("Sent health status for {} services", statuses.len());
                            }
                        }
                        Err(e) => warn!("Failed to serialize health status: {}", e),
                    }
                }
            }
        });
        handles.push(health_handle);

        // Start log tailers with forwarding to cluster
        for tailer_config in &self.config.log_tailers {
            let config = tailer_config.clone();
            let tx = self.log_tx.clone();

            let handle = tokio::spawn(async move {
                let mut tailer = LogTailer::new(config);
                tailer.run(tx).await;
            });
            handles.push(handle);
        }

        // Forward log lines to cluster
        let log_client = client.clone();
        let mut log_rx = std::mem::replace(&mut self.log_rx, mpsc::channel(1).1);
        let log_forward_handle = tokio::spawn(async move {
            while let Some(log_line) = log_rx.recv().await {
                match serde_json::to_vec(&log_line) {
                    Ok(data) => {
                        if let Err(e) = log_client.send_events(&data).await {
                            warn!("Failed to forward log line: {}", e);
                        }
                    }
                    Err(e) => warn!("Failed to serialize log line: {}", e),
                }
            }
        });
        handles.push(log_forward_handle);

        // Run discovery if configured
        if let Some(discovery_config) = &self.config.discovery {
            let config = discovery_config.clone();
            let discovery_client = client.clone();
            let handle = tokio::spawn(async move {
                let mut engine = DiscoveryEngine::new(config);
                let services = engine.discover().await;
                engine.build_dependency_graph();

                info!(services = services.len(), "Discovery complete");

                // Send discovery results to cluster
                if let Ok(data) = serde_json::to_vec(&services) {
                    if let Err(e) = discovery_client.send_events(&data).await {
                        warn!("Failed to send discovery results: {}", e);
                    }
                }
            });
            handles.push(handle);
        }

        // Wait for all tasks
        for handle in handles {
            if let Err(e) = handle.await {
                error!(error = %e, "Supervisor task failed");
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_supervisor_config_default() {
        let config = SupervisorConfig::default();
        assert!(config.health.is_empty());
        assert!(config.log_tailers.is_empty());
        assert!(config.discovery.is_none());
    }

    #[tokio::test]
    async fn test_supervisor_creation() {
        let config = SupervisorConfig::default();
        let _supervisor = Supervisor::new(config);
    }
}
