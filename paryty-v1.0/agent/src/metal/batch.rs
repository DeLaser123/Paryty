#![allow(dead_code)]

//! Batch Collector — Orchestrates all metal scrapers
//!
//! Coordinates CPU, Memory, Disk, Network, Process, and Container collectors
//! into a single MetricBatch for efficient transmission.
//!
//! ## Error Isolation
//!
//! A failure in one collector MUST NOT prevent other collectors from
//! reporting. Each collection call is wrapped in a match that logs
//! errors and continues.
//!
//! ## Timing
//!
//! Every collection records wall-clock duration via `Instant::now()`
//! for performance monitoring and diagnostics.

use std::sync::OnceLock;
use std::time::{Duration, Instant};

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use tracing::{debug, error, instrument, warn};

use crate::communication::Client;
use crate::config::MetalConfig;

use super::container::{ContainerDetector, ContainerMetrics};
use super::cpu::{CpuCollector, CpuMetrics};
use super::disk::{DiskCollector, DiskMetrics};
use super::memory::{MemoryCollector, MemoryMetrics};
use super::network::{NetworkCollector, NetworkMetrics};
use super::process::{ProcessCollector, ProcessMetrics};

// ── Environment Detection ─────────────────────────────────────────────

/// Cached result of WSL2 detection. Checked once at first use.
static IS_WSL2: OnceLock<bool> = OnceLock::new();

/// Detect whether the current environment is WSL2 (Windows Subsystem
/// for Linux). Checks `/proc/version` for the "microsoft" tag that
/// WSL2 kernels always include. Result is cached after first call.
///
/// Returns `false` on any error (non-Linux platforms, missing /proc, etc.).
fn is_wsl2() -> bool {
    *IS_WSL2.get_or_init(|| {
        std::fs::read_to_string("/proc/version")
            .map(|v| v.to_lowercase().contains("microsoft"))
            .unwrap_or(false)
    })
}

// ── Collection Result ──────────────────────────────────────────────────

/// Structured result from a single collector with timing and error info.
///
/// Returned by `collect_all_with_timing()` for diagnostic and monitoring
/// purposes. The `data` field holds the serialized JSON only on success;
/// `error` holds the error message only on failure.
#[derive(Debug, Clone)]
pub struct CollectionResult {
    /// Human-readable collector name (e.g., "cpu", "memory").
    pub name: String,
    /// Collected data as JSON value. `None` if collection failed.
    pub data: Option<serde_json::Value>,
    /// Wall-clock time spent in this collector's `collect()` call.
    pub duration: Duration,
    /// Error message if collection failed. `None` on success.
    pub error: Option<String>,
}

// ── Batch Collector ────────────────────────────────────────────────────

/// Batch collector that orchestrates all metal scrapers.
pub struct BatchCollector {
    cpu: CpuCollector,
    memory: MemoryCollector,
    disk: DiskCollector,
    network: NetworkCollector,
    process: ProcessCollector,
    container: ContainerDetector,
}

/// Combined metal metrics batch.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct MetalBatch {
    pub timestamp: String,
    pub cpu: Option<CpuMetrics>,
    pub memory: Option<MemoryMetrics>,
    pub disk: Option<DiskMetrics>,
    pub network: Option<NetworkMetrics>,
    pub process: Option<ProcessMetrics>,
    pub container: Option<ContainerMetrics>,
}

impl BatchCollector {
    /// Create a new batch collector.
    pub fn new() -> Self {
        Self {
            cpu: CpuCollector::new(),
            memory: MemoryCollector::new(),
            disk: DiskCollector::new(),
            network: NetworkCollector::new(),
            process: ProcessCollector::new(),
            container: ContainerDetector::new(),
        }
    }

    /// Collect all enabled metrics into a single batch with error isolation.
    ///
    /// Each collector runs independently — a failure in CPU collection
    /// MUST NOT stop memory, disk, etc. Errors are logged at `error`
    /// level and the corresponding field is set to `None`.
    ///
    /// Individual collection durations are logged at `debug` level.
    #[instrument(skip(self, config))]
    pub fn collect_all(&self, config: &MetalConfig) -> MetalBatch {
        let timestamp = Utc::now().to_rfc3339();

        let cpu = self.collect_cpu(config);
        let memory = self.collect_memory(config);
        let disk = self.collect_disk(config);
        let network = self.collect_network(config);

        // Process collection strategy depends on the environment.
        // On WSL2, /proc/[pid] reads can hang indefinitely for certain
        // kernel threads and zombie processes. We detect this at startup
        // and use a dedicated OS thread with a hard timeout to avoid
        // blocking the main collection loop.
        let process = if is_wsl2() {
            use std::cell::RefCell;
            use std::sync::mpsc;

            thread_local! {
                static PROC_COLLECTOR: RefCell<ProcessCollector> =
                    RefCell::new(ProcessCollector::new());
            }

            let (tx, rx) = mpsc::channel();
            let config_clone = config.clone();
            std::thread::spawn(move || {
                PROC_COLLECTOR.with(|c| {
                    let result = c.borrow().collect(&config_clone);
                    let _ = tx.send(result);
                });
            });
            match rx.recv_timeout(std::time::Duration::from_secs(5)) {
                Ok(Ok(pm)) => Some(pm),
                Ok(Err(e)) => {
                    tracing::warn!("Process collection failed: {}", e);
                    None
                }
                Err(_) => {
                    tracing::warn!("Process collection timed out (5s), skipping");
                    None
                }
            }
        } else {
            self.collect_process(config)
        };

        let container = self.collect_container(config);

        MetalBatch { timestamp, cpu, memory, disk, network, process, container }
    }

    /// Collect all enabled metrics with detailed timing information.
    ///
    /// Returns a `Vec<CollectionResult>` — one per collector, regardless
    /// of whether it succeeded or failed. This is useful for diagnostics,
    /// self-metrics, and performance monitoring.
    ///
    /// Disabled collectors (per config) are excluded from the results.
    #[instrument(skip(self, config))]
    pub fn collect_all_with_timing(&self, config: &MetalConfig) -> Vec<CollectionResult> {
        let mut results = Vec::with_capacity(6);

        if config.cpu_per_core || config.cpu_per_process {
            results.push(self.timed_collect("cpu", || {
                let m = self.cpu.collect(config)?;
                serde_json::to_value(m).map_err(Into::into)
            }));
        }

        if config.memory_rss {
            results.push(self.timed_collect("memory", || {
                let m = self.memory.collect(config)?;
                serde_json::to_value(m).map_err(Into::into)
            }));
        }

        if config.disk_io {
            results.push(self.timed_collect("disk", || {
                let m = self.disk.collect(config)?;
                serde_json::to_value(m).map_err(Into::into)
            }));
        }

        if config.network_io {
            results.push(self.timed_collect("network", || {
                let m = self.network.collect(config)?;
                serde_json::to_value(m).map_err(Into::into)
            }));
        }

        if config.process_tree {
            results.push(self.timed_collect("process", || {
                let m = self.process.collect(config)?;
                serde_json::to_value(m).map_err(Into::into)
            }));
        }

        if config.container_detection {
            results.push(self.timed_collect("container", || {
                let m = self.container.collect(config)?;
                serde_json::to_value(m).map_err(Into::into)
            }));
        }

        results
    }

    /// Collect all enabled metrics and send via communication layer.
    ///
    /// Uses error-isolated `collect_all()` internally — a partial
    /// collection (some collectors failed) is still serialized and sent.
    pub async fn collect_and_send(&self, config: &MetalConfig, client: &Client) -> Result<()> {
        let batch = self.collect_all(config);

        // Serialize and send — even if some fields are None due to errors.
        let data = serde_json::to_vec(&batch)?;
        client.send_metrics("metal", &data).await?;

        Ok(())
    }

    // ── Private: per-collector with error isolation ──────────────────

    /// Collect CPU metrics with error isolation.
    fn collect_cpu(&self, config: &MetalConfig) -> Option<CpuMetrics> {
        if !(config.cpu_per_core || config.cpu_per_process) {
            return None;
        }
        let start = Instant::now();
        match self.cpu.collect(config) {
            Ok(metrics) => {
                let elapsed = start.elapsed();
                debug!(
                    collector = "cpu",
                    elapsed_ms = elapsed.as_millis() as u64,
                    "Collection succeeded"
                );
                Some(metrics)
            }
            Err(e) => {
                error!(collector = "cpu", error = %e, "Collection failed, continuing with other collectors");
                None
            }
        }
    }

    /// Collect memory metrics with error isolation.
    fn collect_memory(&self, config: &MetalConfig) -> Option<MemoryMetrics> {
        if !config.memory_rss {
            return None;
        }
        let start = Instant::now();
        match self.memory.collect(config) {
            Ok(metrics) => {
                let elapsed = start.elapsed();
                debug!(
                    collector = "memory",
                    elapsed_ms = elapsed.as_millis() as u64,
                    "Collection succeeded"
                );
                Some(metrics)
            }
            Err(e) => {
                error!(collector = "memory", error = %e, "Collection failed, continuing with other collectors");
                None
            }
        }
    }

    /// Collect disk metrics with error isolation.
    fn collect_disk(&self, config: &MetalConfig) -> Option<DiskMetrics> {
        if !config.disk_io {
            return None;
        }
        let start = Instant::now();
        match self.disk.collect(config) {
            Ok(metrics) => {
                let elapsed = start.elapsed();
                debug!(
                    collector = "disk",
                    elapsed_ms = elapsed.as_millis() as u64,
                    "Collection succeeded"
                );
                Some(metrics)
            }
            Err(e) => {
                error!(collector = "disk", error = %e, "Collection failed, continuing with other collectors");
                None
            }
        }
    }

    /// Collect network metrics with error isolation.
    fn collect_network(&self, config: &MetalConfig) -> Option<NetworkMetrics> {
        if !config.network_io {
            return None;
        }
        let start = Instant::now();
        match self.network.collect(config) {
            Ok(metrics) => {
                let elapsed = start.elapsed();
                debug!(
                    collector = "network",
                    elapsed_ms = elapsed.as_millis() as u64,
                    "Collection succeeded"
                );
                Some(metrics)
            }
            Err(e) => {
                error!(collector = "network", error = %e, "Collection failed, continuing with other collectors");
                None
            }
        }
    }

    /// Collect process metrics with error isolation.
    fn collect_process(&self, config: &MetalConfig) -> Option<ProcessMetrics> {
        if !config.process_tree {
            return None;
        }
        let start = Instant::now();
        match self.process.collect(config) {
            Ok(metrics) => {
                let elapsed = start.elapsed();
                debug!(
                    collector = "process",
                    elapsed_ms = elapsed.as_millis() as u64,
                    "Collection succeeded"
                );
                Some(metrics)
            }
            Err(e) => {
                error!(collector = "process", error = %e, "Collection failed, continuing with other collectors");
                None
            }
        }
    }

    /// Collect container metrics with error isolation.
    fn collect_container(&self, config: &MetalConfig) -> Option<ContainerMetrics> {
        if !config.container_detection {
            return None;
        }
        let start = Instant::now();
        match self.container.collect(config) {
            Ok(metrics) => {
                let elapsed = start.elapsed();
                debug!(
                    collector = "container",
                    elapsed_ms = elapsed.as_millis() as u64,
                    "Collection succeeded"
                );
                Some(metrics)
            }
            Err(e) => {
                warn!(collector = "container", error = %e, "Collection failed, continuing with other collectors");
                None
            }
        }
    }

    // ── Private: timed collection helper ────────────────────────────

    /// Run a collection closure with timing, producing a `CollectionResult`.
    ///
    /// Captures the `Instant::now()` wall clock before and after the closure.
    /// On success, populates `data`; on failure, populates `error`.
    /// The collector name is set from the `name` parameter.
    fn timed_collect<F>(&self, name: &str, f: F) -> CollectionResult
    where
        F: FnOnce() -> Result<serde_json::Value>,
    {
        let start = Instant::now();
        match f() {
            Ok(value) => {
                let duration = start.elapsed();
                debug!(
                    collector = name,
                    elapsed_ms = duration.as_millis() as u64,
                    "Timed collection succeeded"
                );
                CollectionResult {
                    name: name.to_string(),
                    data: Some(value),
                    duration,
                    error: None,
                }
            }
            Err(e) => {
                let duration = start.elapsed();
                error!(
                    collector = name,
                    elapsed_ms = duration.as_millis() as u64,
                    error = %e,
                    "Timed collection failed"
                );
                CollectionResult {
                    name: name.to_string(),
                    data: None,
                    duration,
                    error: Some(e.to_string()),
                }
            }
        }
    }
}

impl Default for BatchCollector {
    fn default() -> Self {
        Self::new()
    }
}

// ── Tests ──────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    /// Build a MetalConfig with all collectors enabled.
    fn all_enabled_config() -> MetalConfig {
        MetalConfig {
            enabled: true,
            interval: "10s".to_string(),
            cpu_per_core: true,
            cpu_per_process: false,
            memory_rss: true,
            disk_io: true,
            network_io: true,
            process_tree: true,
            container_detection: true,
        }
    }

    /// Build a MetalConfig with all collectors disabled.
    fn all_disabled_config() -> MetalConfig {
        MetalConfig {
            enabled: false,
            interval: "10s".to_string(),
            cpu_per_core: false,
            cpu_per_process: false,
            memory_rss: false,
            disk_io: false,
            network_io: false,
            process_tree: false,
            container_detection: false,
        }
    }

    /// Test that a failure in one collector does NOT prevent others
    /// from succeeding. On this platform, the collectors use sysinfo
    /// and should generally succeed. We verify that `collect_all`
    /// returns `MetalBatch` even when some fields might be `None`
    /// due to platform limitations (not errors).
    ///
    /// More importantly, this verifies the return type is `MetalBatch`
    /// (not `Result<MetalBatch>`) — proving error isolation is in place.
    #[test]
    fn test_error_isolation_returns_metal_batch_not_result() {
        let collector = BatchCollector::new();
        let config = all_enabled_config();

        // This MUST return MetalBatch, not Result<MetalBatch>.
        // If any collector panics or propagates an error, the test will fail.
        let batch = collector.collect_all(&config);

        // At minimum, timestamp should be populated.
        assert!(!batch.timestamp.is_empty(), "timestamp must be populated");

        // On Windows with sysinfo, CPU and memory should succeed.
        // Disk, network, process, container may or may not succeed depending
        // on platform — but the point is the batch is still returned.
        // We just verify the call doesn't panic or return an error.
    }

    /// Test that disabled collectors are skipped (fields set to None)
    /// and the batch still has a valid timestamp.
    #[test]
    fn test_conditional_collection_disabled_collectors_skipped() {
        let collector = BatchCollector::new();
        let config = all_disabled_config();

        let batch = collector.collect_all(&config);

        // All fields should be None when all collectors are disabled.
        assert!(batch.cpu.is_none(), "CPU should be None when disabled");
        assert!(batch.memory.is_none(), "memory should be None when disabled");
        assert!(batch.disk.is_none(), "disk should be None when disabled");
        assert!(batch.network.is_none(), "network should be None when disabled");
        assert!(batch.process.is_none(), "process should be None when disabled");
        assert!(batch.container.is_none(), "container should be None when disabled");

        // Timestamp should still be populated.
        assert!(!batch.timestamp.is_empty());
    }

    /// Test that `collect_all_with_timing()` records non-zero durations
    /// for enabled collectors.
    #[test]
    fn test_collection_timing_records_duration() {
        let collector = BatchCollector::new();
        let config = all_enabled_config();

        let results = collector.collect_all_with_timing(&config);

        // Should have results for all enabled collectors.
        assert!(!results.is_empty(), "should have at least one result");

        for result in &results {
            // Duration should be non-zero (at least on modern hardware).
            // We use a loose check — 0ns would indicate timing was not recorded.
            assert!(
                result.duration.as_nanos() > 0,
                "collector '{}' should have non-zero duration",
                result.name
            );
            // Name should not be empty.
            assert!(!result.name.is_empty(), "result name must not be empty");
            // Either data or error must be present (XOR-like).
            assert!(
                result.data.is_some() || result.error.is_some(),
                "collector '{}' must have either data or error",
                result.name
            );
        }
    }

    /// Test that `collect_all_with_timing()` excludes disabled collectors.
    #[test]
    fn test_timing_excludes_disabled_collectors() {
        let collector = BatchCollector::new();

        // Only enable memory.
        let config = MetalConfig {
            enabled: true,
            interval: "10s".to_string(),
            cpu_per_core: false,
            cpu_per_process: false,
            memory_rss: true,
            disk_io: false,
            network_io: false,
            process_tree: false,
            container_detection: false,
        };

        let results = collector.collect_all_with_timing(&config);

        // Should only have one result (memory).
        assert_eq!(results.len(), 1, "only memory should be collected");
        assert_eq!(results[0].name, "memory");
    }

    /// Test that `collect_all_with_timing()` on empty config returns empty results.
    #[test]
    fn test_timing_empty_config() {
        let collector = BatchCollector::new();
        let config = all_disabled_config();

        let results = collector.collect_all_with_timing(&config);

        assert!(results.is_empty(), "disabled config should yield no results");
    }

    /// Test that CollectionResult fields are properly populated.
    #[test]
    fn test_collection_result_structure() {
        let collector = BatchCollector::new();
        let config = MetalConfig {
            enabled: true,
            interval: "10s".to_string(),
            cpu_per_core: true,
            cpu_per_process: false,
            memory_rss: true,
            disk_io: false,
            network_io: false,
            process_tree: false,
            container_detection: false,
        };

        let results = collector.collect_all_with_timing(&config);

        // At least CPU should be present.
        let cpu_result = results.iter().find(|r| r.name == "cpu");
        assert!(cpu_result.is_some(), "CPU result should be present");

        let cpu = cpu_result.expect("CPU result should exist");

        // If the CPU collector succeeded on this platform, verify data is Some.
        if cpu.data.is_some() {
            assert!(cpu.error.is_none(), "error should be None when data is present");
            // Duration should be reasonable (< 10s for a single collection).
            assert!(cpu.duration.as_secs() < 10, "CPU collection should take < 10s");
        } else {
            // If it failed, error should be present.
            assert!(cpu.error.is_some(), "error should be Some when data is None");
        }
    }

    /// Test that `collect_all` with partially-enabled config returns
    /// a batch with Some/None fields matching the config.
    #[test]
    fn test_partial_config_selective_collection() {
        let collector = BatchCollector::new();

        // Only enable CPU and memory.
        let config = MetalConfig {
            enabled: true,
            interval: "10s".to_string(),
            cpu_per_core: true,
            cpu_per_process: false,
            memory_rss: true,
            disk_io: false,
            network_io: false,
            process_tree: false,
            container_detection: false,
        };

        let batch = collector.collect_all(&config);

        // disk, network, process, container should be None.
        assert!(batch.disk.is_none(), "disk should be None when disk_io=false");
        assert!(batch.network.is_none(), "network should be None when network_io=false");
        assert!(batch.process.is_none(), "process should be None when process_tree=false");
        assert!(
            batch.container.is_none(),
            "container should be None when container_detection=false"
        );
    }
}
