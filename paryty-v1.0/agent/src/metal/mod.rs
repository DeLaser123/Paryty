#![allow(dead_code)]

//! Metal Scraper Module
//!
//! Collects hardware-level metrics from the host system including
//! CPU, memory, disk, network, process, and container metrics.
//!
//! Architecture:
//! ```text
//! MetalScraper
//! ├── CollectorRegistry — Trait-based registry, error isolation per collector
//! ├── CpuCollector      — /proc/stat, per-core/per-process
//! ├── MemoryCollector   — /proc/meminfo, RSS/VSZ
//! ├── DiskCollector     — /proc/diskstats, IOPS/throughput/latency
//! ├── NetworkCollector  — /proc/net/dev, TCP states/RTT
//! ├── ProcessCollector  — /proc/[pid], process tree
//! ├── ContainerDetector — cgroup v1/v2, Docker/Podman/containerd
//! └── BatchCollector    — Orchestrator, serializes and sends
//! ```

pub mod batch;
pub mod container;
pub mod cpu;
pub mod disk;
pub mod memory;
pub mod network;
pub mod process;

use anyhow::Result;
use tracing::{debug, error, info};

use crate::communication::Client;
use crate::config::MetalConfig;
use batch::BatchCollector;

/// Trait abstracting a single metric collector.
///
/// Each hardware subsystem (CPU, memory, disk, etc.) implements this trait
/// to provide a uniform collection interface. The registry uses this trait
/// to iterate over collectors with per-collector error isolation.
pub trait Collector: Send + Sync {
    /// Human-readable name for logging and result maps.
    fn name(&self) -> &str;

    /// Collect metrics and return as a JSON value.
    ///
    /// Implementations should return `Err` only for genuine collection
    /// failures (e.g., /proc read errors), not for "metric disabled" states.
    fn collect_json(&self) -> anyhow::Result<serde_json::Value>;

    /// Whether this collector is enabled based on the current configuration.
    fn is_enabled(&self) -> bool;
}

/// Registry that holds all active collectors and orchestrates collection.
///
/// Collectors are registered at startup based on configuration. During
/// collection, each collector runs independently — a failure in one
/// collector must never prevent others from reporting.
pub struct CollectorRegistry {
    collectors: Vec<Box<dyn Collector>>,
}

impl CollectorRegistry {
    /// Create an empty registry.
    pub fn new() -> Self {
        Self { collectors: Vec::new() }
    }

    /// Register a collector. Disabled collectors are silently skipped.
    pub fn register(&mut self, collector: Box<dyn Collector>) {
        if collector.is_enabled() {
            info!("Registered collector: {}", collector.name());
            self.collectors.push(collector);
        } else {
            debug!("Skipping disabled collector: {}", collector.name());
        }
    }

    /// Collect from all registered collectors with per-collector error isolation.
    ///
    /// A failure in one collector MUST NOT stop others. Returns a list of
    /// `(collector_name, json_value)` for every successful collection.
    pub fn collect_all(&self) -> Vec<(String, serde_json::Value)> {
        let mut results = Vec::with_capacity(self.collectors.len());
        for collector in &self.collectors {
            let start = std::time::Instant::now();
            match collector.collect_json() {
                Ok(value) => {
                    let elapsed = start.elapsed();
                    debug!(
                        collector = collector.name(),
                        elapsed_ms = elapsed.as_millis() as u64,
                        "Collection succeeded"
                    );
                    results.push((collector.name().to_string(), value));
                }
                Err(e) => {
                    error!(
                        collector = collector.name(),
                        error = %e,
                        "Collection failed, continuing with other collectors"
                    );
                    // Do NOT propagate the error — continue with other collectors
                }
            }
        }
        results
    }

    /// Number of registered (enabled) collectors.
    pub fn len(&self) -> usize {
        self.collectors.len()
    }

    /// Whether any collectors are registered.
    pub fn is_empty(&self) -> bool {
        self.collectors.is_empty()
    }
}

impl Default for CollectorRegistry {
    fn default() -> Self {
        Self::new()
    }
}

/// Run the metal scraper collection loop.
///
/// This is the main entry point called from the agent's main function.
/// It creates a BatchCollector and runs collection at the configured interval,
/// sending results via the communication layer.
pub async fn run(config: MetalConfig, client: Client) -> Result<()> {
    info!(
        "Starting metal scraper with interval: {}, \
         cpu={}, memory={}, disk={}, network={}, process={}, container={}",
        config.interval,
        config.cpu_per_core || config.cpu_per_process,
        config.memory_rss,
        config.disk_io,
        config.network_io,
        config.process_tree,
        config.container_detection
    );

    let interval = parse_duration(&config.interval)?;
    info!("Metal: parsed interval = {:?}", interval);
    let collector = BatchCollector::new();
    info!("Metal: BatchCollector created");
    let mut ticker = tokio::time::interval(interval);
    info!("Metal: ticker created, awaiting first tick");

    // Skip the first immediate tick
    ticker.tick().await;
    info!("Metal: first tick skipped, entering collection loop");

    loop {
        ticker.tick().await;

        info!("Collecting metal metrics");

        match collector.collect_and_send(&config, &client).await {
            Ok(()) => {
                info!("Metal metrics collected and sent successfully");
            }
            Err(e) => {
                error!("Failed to collect/send metal metrics: {}", e);
                // Continue running — transient errors should not stop the collector
            }
        }
    }
}

/// Parse a duration string (e.g., "10s", "1m", "500ms") into std::time::Duration.
fn parse_duration(s: &str) -> Result<std::time::Duration> {
    let s = s.trim();
    if let Some(stripped) = s.strip_suffix("ms") {
        let ms: u64 = stripped.parse()?;
        Ok(std::time::Duration::from_millis(ms))
    } else if let Some(stripped) = s.strip_suffix('s') {
        let secs: u64 = stripped.parse()?;
        Ok(std::time::Duration::from_secs(secs))
    } else if let Some(stripped) = s.strip_suffix('m') {
        let mins: u64 = stripped.parse()?;
        Ok(std::time::Duration::from_secs(mins * 60))
    } else {
        anyhow::bail!("Invalid duration format: {}", s)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Mock collector for testing the registry in isolation.
    struct MockCollector {
        name: &'static str,
        enabled: bool,
        should_fail: bool,
    }

    impl Collector for MockCollector {
        fn name(&self) -> &str {
            self.name
        }

        fn is_enabled(&self) -> bool {
            self.enabled
        }

        fn collect_json(&self) -> anyhow::Result<serde_json::Value> {
            if self.should_fail {
                anyhow::bail!("mock failure")
            }
            Ok(serde_json::json!({"test": true}))
        }
    }

    #[test]
    fn test_empty_registry() {
        let registry = CollectorRegistry::new();
        assert_eq!(registry.len(), 0);
        assert!(registry.is_empty());
        let results = registry.collect_all();
        assert!(results.is_empty());
    }

    #[test]
    fn test_error_isolation() {
        let mut registry = CollectorRegistry::new();
        registry.register(Box::new(MockCollector {
            name: "good",
            enabled: true,
            should_fail: false,
        }));
        registry.register(Box::new(MockCollector {
            name: "bad",
            enabled: true,
            should_fail: true,
        }));
        registry.register(Box::new(MockCollector {
            name: "also_good",
            enabled: true,
            should_fail: false,
        }));

        assert_eq!(registry.len(), 3);

        let results = registry.collect_all();
        // Only the 2 successful collectors should have results
        assert_eq!(results.len(), 2);
        assert_eq!(results[0].0, "good");
        assert_eq!(results[1].0, "also_good");
    }

    #[test]
    fn test_disabled_collector_not_registered() {
        let mut registry = CollectorRegistry::new();
        registry.register(Box::new(MockCollector {
            name: "disabled",
            enabled: false,
            should_fail: false,
        }));
        assert_eq!(registry.len(), 0);
        assert!(registry.is_empty());
    }

    #[test]
    fn test_default_registry_is_empty() {
        let registry = CollectorRegistry::default();
        assert!(registry.is_empty());
    }

    #[test]
    fn test_all_collectors_fail() {
        let mut registry = CollectorRegistry::new();
        registry.register(Box::new(MockCollector {
            name: "fail_a",
            enabled: true,
            should_fail: true,
        }));
        registry.register(Box::new(MockCollector {
            name: "fail_b",
            enabled: true,
            should_fail: true,
        }));

        assert_eq!(registry.len(), 2);
        let results = registry.collect_all();
        assert_eq!(results.len(), 0);
    }
}
