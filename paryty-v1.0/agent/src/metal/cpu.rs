//! CPU Metrics Collector
//!
//! Collects per-core and per-process CPU usage data.
//! Uses /proc/stat on Linux for zero-allocation reads,
//! falls back to sysinfo crate for cross-platform support.
//!
//! Performance target: <1ms collection time.

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use std::sync::atomic::AtomicU64;
use tracing::instrument;

use crate::config::MetalConfig;

/// CPU metrics snapshot.
///
/// Fields that are not available on the current platform use `Option<>`.
/// `None` (serialized as `null`) means the metric is genuinely unavailable.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct CpuMetrics {
    pub timestamp: String,
    pub total_usage_percent: f64,
    pub per_core_percent: Vec<f64>,
    /// 1-minute load average. Only available on Linux/Unix.
    pub load_average_1m: Option<f64>,
    /// 5-minute load average. Only available on Linux/Unix.
    pub load_average_5m: Option<f64>,
    /// 15-minute load average. Only available on Linux/Unix.
    pub load_average_15m: Option<f64>,
    pub frequency_mhz: f64,
    /// Context switches since boot. Only available on Linux via /proc/stat.
    pub context_switches: Option<u64>,
    pub physical_cores: u32,
    pub logical_cores: u32,
    pub model_name: String,
    /// CPU vendor ID (e.g., "GenuineIntel", "AuthenticAMD").
    /// Available via sysinfo on all platforms; on Linux also from /proc/cpuinfo.
    pub vendor_id: Option<String>,
}

/// CPU topology information (internal helper).
#[allow(dead_code)] // Only constructed on Linux; stub returns defaults on other platforms
struct CpuTopology {
    physical_cores: u32,
    logical_cores: u32,
    model_name: String,
}

/// CPU collector that reads from /proc/stat (Linux) or sysinfo (cross-platform).
#[allow(dead_code)] // Fields are read by Linux /proc/stat path; cross-platform path uses sysinfo
pub struct CpuCollector {
    /// Previous total CPU ticks for delta calculation.
    prev_total: AtomicU64,
    /// Previous idle CPU ticks for delta calculation.
    prev_idle: AtomicU64,
    /// Per-core previous total ticks.
    prev_per_core_total: std::sync::Mutex<Vec<u64>>,
    /// Per-core previous idle ticks.
    prev_per_core_idle: std::sync::Mutex<Vec<u64>>,
}

impl CpuCollector {
    /// Create a new CPU collector.
    pub fn new() -> Self {
        Self {
            prev_total: AtomicU64::new(0),
            prev_idle: AtomicU64::new(0),
            prev_per_core_total: std::sync::Mutex::new(Vec::new()),
            prev_per_core_idle: std::sync::Mutex::new(Vec::new()),
        }
    }

    /// Collect CPU metrics.
    ///
    /// On Linux, reads /proc/stat directly for minimal overhead.
    /// On other platforms, uses sysinfo crate.
    #[instrument(skip(self))]
    pub fn collect(&self, config: &MetalConfig) -> Result<CpuMetrics> {
        let metrics = if cfg!(target_os = "linux") {
            self.collect_linux()?
        } else {
            self.collect_cross_platform()?
        };

        Ok(metrics)
    }

    /// Linux implementation using /proc/stat.
    #[cfg(target_os = "linux")]
    fn collect_linux(&self) -> Result<CpuMetrics> {
        use std::fs;
        use std::sync::atomic::Ordering;

        let stat = fs::read_to_string("/proc/stat")?;
        let mut lines = stat.lines();

        // Parse aggregate CPU line: cpu  user nice system idle iowait irq softirq steal
        let cpu_line = lines.next().ok_or_else(|| anyhow::anyhow!("Empty /proc/stat"))?;
        let values = parse_cpu_line(cpu_line)?;
        let total: u64 = values.iter().sum();
        let idle = values.get(3).copied().unwrap_or(0);

        let prev_total = self.prev_total.swap(total, Ordering::Relaxed);
        let prev_idle = self.prev_idle.swap(idle, Ordering::Relaxed);

        let total_delta = total.saturating_sub(prev_total);
        let idle_delta = idle.saturating_sub(prev_idle);

        let total_usage = calculate_usage_percent(total_delta, idle_delta);

        // Parse per-core CPU lines: cpu0, cpu1, ...
        let mut per_core = Vec::new();
        let mut prev_totals = self.prev_per_core_total.lock().unwrap();
        let mut prev_idles = self.prev_per_core_idle.lock().unwrap();

        for (i, line) in lines.enumerate() {
            if !line.starts_with("cpu") {
                break;
            }
            let values = parse_cpu_line(line)?;
            let core_total: u64 = values.iter().sum();
            let core_idle = values.get(3).copied().unwrap_or(0);

            if i < prev_totals.len() {
                let total_delta = core_total.saturating_sub(prev_totals[i]);
                let idle_delta = core_idle.saturating_sub(prev_idles[i]);
                let usage = calculate_usage_percent(total_delta, idle_delta);
                per_core.push(usage);
            } else {
                per_core.push(0.0);
            }

            if i < prev_totals.len() {
                prev_totals[i] = core_total;
                prev_idles[i] = core_idle;
            } else {
                prev_totals.push(core_total);
                prev_idles.push(core_idle);
            }
        }

        // Read load average
        let loadavg = fs::read_to_string("/proc/loadavg").unwrap_or_default();
        let load_parts: Vec<&str> = loadavg.split_whitespace().collect();
        let load_1m = load_parts.first().and_then(|s| s.parse().ok()).unwrap_or(0.0);
        let load_5m = load_parts.get(1).and_then(|s| s.parse().ok()).unwrap_or(0.0);
        let load_15m = load_parts.get(2).and_then(|s| s.parse().ok()).unwrap_or(0.0);

        // Collect enhanced metrics using already-loaded /proc/stat content
        let frequency_mhz = self.get_cpu_frequency();
        let context_switches = self.get_context_switches(&stat);
        let topology = self.get_cpu_topology(&stat);

        Ok(CpuMetrics {
            timestamp: Utc::now().to_rfc3339(),
            total_usage_percent: total_usage,
            per_core_percent: per_core,
            load_average_1m: Some(load_1m),
            load_average_5m: Some(load_5m),
            load_average_15m: Some(load_15m),
            frequency_mhz,
            context_switches: Some(context_switches),
            physical_cores: topology.physical_cores,
            logical_cores: topology.logical_cores,
            model_name: topology.model_name,
            vendor_id: get_vendor_id_from_cpuinfo(),
        })
    }

    /// Cross-platform implementation using sysinfo.
    #[cfg(not(target_os = "linux"))]
    fn collect_linux(&self) -> Result<CpuMetrics> {
        self.collect_cross_platform()
    }

    /// Cross-platform implementation using sysinfo.
    fn collect_cross_platform(&self) -> Result<CpuMetrics> {
        use sysinfo::System;

        let mut sys = System::new_all();
        std::thread::sleep(sysinfo::MINIMUM_CPU_UPDATE_INTERVAL);
        sys.refresh_all();

        let per_core: Vec<f64> = sys.cpus().iter().map(|c| c.cpu_usage() as f64).collect();
        let total_usage = if per_core.is_empty() {
            0.0
        } else {
            per_core.iter().sum::<f64>() / per_core.len() as f64
        };

        // On Windows, load_average() returns all zeros — report as unavailable.
        let load = System::load_average();
        let load_is_zero = load.one == 0.0 && load.five == 0.0 && load.fifteen == 0.0;

        Ok(CpuMetrics {
            timestamp: Utc::now().to_rfc3339(),
            total_usage_percent: total_usage,
            per_core_percent: per_core,
            load_average_1m: if load_is_zero { None } else { Some(load.one) },
            load_average_5m: if load_is_zero { None } else { Some(load.five) },
            load_average_15m: if load_is_zero { None } else { Some(load.fifteen) },
            frequency_mhz: sys.cpus().first().map(|c| c.frequency() as f64).unwrap_or(0.0),
            context_switches: None, // not available via sysinfo on any platform
            physical_cores: sys.physical_core_count().unwrap_or(0) as u32,
            logical_cores: sys.cpus().len() as u32,
            model_name: sys.cpus().first().map(|c| c.brand().to_string()).unwrap_or_default(),
            vendor_id: sys.cpus().first().map(|c| c.vendor_id().to_string()),
        })
    }

    /// Get CPU frequency in MHz.
    ///
    /// Reads from sysfs `scaling_cur_freq` (kHz, converted to MHz).
    /// Falls back to parsing `/proc/cpuinfo` for "cpu MHz".
    /// Returns 0.0 if neither source is available.
    #[cfg(target_os = "linux")]
    fn get_cpu_frequency(&self) -> f64 {
        use std::fs;

        // Primary: sysfs cpufreq (value in kHz, convert to MHz)
        if let Ok(content) =
            fs::read_to_string("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq")
        {
            if let Some(mhz) = parse_frequency_khz(&content) {
                return mhz;
            }
        }

        // Fallback: /proc/cpuinfo "cpu MHz" line
        if let Ok(cpuinfo) = fs::read_to_string("/proc/cpuinfo") {
            for line in cpuinfo.lines() {
                if line.starts_with("cpu MHz") {
                    if let Some(val_str) = line.split(':').nth(1) {
                        if let Ok(mhz) = val_str.trim().parse::<f64>() {
                            return mhz;
                        }
                    }
                }
            }
        }

        0.0
    }

    /// Get CPU frequency stub for non-Linux platforms.
    #[cfg(not(target_os = "linux"))]
    #[allow(dead_code)]
    fn get_cpu_frequency(&self) -> f64 {
        0.0
    }

    /// Get total context switches from `/proc/stat` content.
    ///
    /// Parses the `ctxt` line which contains the total number of context
    /// switches since boot.
    #[allow(dead_code)] // Called from Linux collect path; dead on non-Linux
    fn get_context_switches(&self, stat_content: &str) -> u64 {
        for line in stat_content.lines() {
            if let Some(val) = parse_ctxt_value(line) {
                return val;
            }
        }
        0
    }

    /// Get CPU topology information.
    ///
    /// Counts logical cores from `/proc/stat` (lines matching `cpu\d+`),
    /// and parses model name and physical core count from `/proc/cpuinfo`.
    #[cfg(target_os = "linux")]
    fn get_cpu_topology(&self, stat_content: &str) -> CpuTopology {
        use std::fs;

        let logical_cores = count_logical_cores(stat_content);
        let mut physical_cores = logical_cores; // fallback: assume no HT
        let mut model_name = String::new();

        if let Ok(cpuinfo) = fs::read_to_string("/proc/cpuinfo") {
            for line in cpuinfo.lines() {
                if line.starts_with("model name") && model_name.is_empty() {
                    if let Some(val) = line.split(':').nth(1) {
                        model_name = val.trim().to_string();
                    }
                } else if line.starts_with("cpu cores") {
                    if let Some(val) = line.split(':').nth(1) {
                        if let Ok(cores) = val.trim().parse::<u32>() {
                            physical_cores = cores;
                        }
                    }
                }
            }
        }

        CpuTopology { physical_cores, logical_cores, model_name }
    }

    /// Get CPU topology stub for non-Linux platforms.
    #[cfg(not(target_os = "linux"))]
    #[allow(dead_code)]
    fn get_cpu_topology(&self, stat_content: &str) -> CpuTopology {
        CpuTopology {
            physical_cores: 0,
            logical_cores: count_logical_cores(stat_content),
            model_name: String::new(),
        }
    }
}

/// Get CPU vendor ID from `/proc/cpuinfo`.
///
/// Looks for the "vendor_id" line (e.g., "vendor_id\t: GenuineIntel").
/// Returns `None` if the file doesn't exist or the line is missing.
#[cfg(target_os = "linux")]
fn get_vendor_id_from_cpuinfo() -> Option<String> {
    use std::fs;

    let cpuinfo = fs::read_to_string("/proc/cpuinfo").ok()?;
    for line in cpuinfo.lines() {
        if line.starts_with("vendor_id") {
            if let Some(val) = line.split(':').nth(1) {
                let vendor = val.trim().to_string();
                if !vendor.is_empty() {
                    return Some(vendor);
                }
            }
        }
    }
    None
}

/// Stub for non-Linux platforms.
#[cfg(not(target_os = "linux"))]
#[allow(dead_code)]
fn get_vendor_id_from_cpuinfo() -> Option<String> {
    None
}

/// Parse a /proc/stat CPU line into tick values.
///
/// Available on Linux and in test builds so it can be unit-tested on any platform.
#[cfg(any(target_os = "linux", test))]
fn parse_cpu_line(line: &str) -> Result<Vec<u64>> {
    line.split_whitespace()
        .skip(1) // Skip "cpu" or "cpu0" label
        .map(|v| v.parse::<u64>().map_err(|e| anyhow::anyhow!("Invalid CPU value: {}", e)))
        .collect()
}

/// Parse a context switch count from a `/proc/stat` "ctxt" line.
///
/// Expected format: `ctxt 123456789`
/// Returns `None` if the line doesn't start with "ctxt" or the value isn't a valid u64.
#[allow(dead_code)] // Used by Linux collect path and tests
fn parse_ctxt_value(line: &str) -> Option<u64> {
    let rest = line.trim().strip_prefix("ctxt")?;
    rest.trim().parse::<u64>().ok()
}

/// Parse CPU frequency from a kHz string to MHz.
///
/// Input `"2400000"` returns `Some(2400.0)`.
#[allow(dead_code)] // Used by Linux collect path and tests
fn parse_frequency_khz(khz_str: &str) -> Option<f64> {
    let khz: f64 = khz_str.trim().parse().ok()?;
    Some(khz / 1000.0)
}

/// Count logical CPU cores from `/proc/stat` content.
///
/// Counts lines matching `cpu<digits>` (e.g., `cpu0`, `cpu1`, ...).
#[allow(dead_code)] // Used by Linux collect path and tests
fn count_logical_cores(stat_content: &str) -> u32 {
    stat_content
        .lines()
        .filter(|line| {
            line.starts_with("cpu") && line.as_bytes().get(3).is_some_and(|b| b.is_ascii_digit())
        })
        .count() as u32
}

/// Calculate CPU usage percentage from delta values.
///
/// Returns a value in `[0.0, 100.0]`. Returns `0.0` when `total_delta` is zero
/// (first sample or no activity).
#[allow(dead_code)] // Used by Linux collect path and tests
fn calculate_usage_percent(total_delta: u64, idle_delta: u64) -> f64 {
    if total_delta > 0 {
        ((total_delta - idle_delta) as f64 / total_delta as f64) * 100.0
    } else {
        0.0
    }
}

impl Default for CpuCollector {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_ctxt_line() {
        // Standard /proc/stat ctxt line
        assert_eq!(parse_ctxt_value("ctxt 12345678"), Some(12_345_678));
        // Zero context switches
        assert_eq!(parse_ctxt_value("ctxt 0"), Some(0));
        // Large value (realistic after boot + uptime)
        assert_eq!(parse_ctxt_value("ctxt 9876543210"), Some(9_876_543_210));
        // Leading/trailing whitespace
        assert_eq!(parse_ctxt_value("  ctxt 42  "), Some(42));
        // Not a ctxt line
        assert_eq!(parse_ctxt_value("cpu  1 2 3 4 5"), None);
        // Missing value after ctxt
        assert_eq!(parse_ctxt_value("ctxt"), None);
        // Non-numeric value
        assert_eq!(parse_ctxt_value("ctxt abc"), None);
    }

    #[test]
    fn test_cpu_frequency_from_string() {
        // 2.4 GHz in kHz
        assert_eq!(parse_frequency_khz("2400000"), Some(2400.0));
        // 3.6 GHz in kHz
        assert_eq!(parse_frequency_khz("3600000"), Some(3600.0));
        // With trailing newline (as from read_to_string)
        assert_eq!(parse_frequency_khz("2400000\n"), Some(2400.0));
        // With whitespace padding
        assert_eq!(parse_frequency_khz("  2400000  "), Some(2400.0));
        // Zero frequency
        assert_eq!(parse_frequency_khz("0"), Some(0.0));
        // Invalid input
        assert_eq!(parse_frequency_khz("not_a_number"), None);
        // Empty string
        assert_eq!(parse_frequency_khz(""), None);
    }

    #[test]
    fn test_cpu_usage_in_valid_range() {
        // Edge: fully idle
        assert_eq!(calculate_usage_percent(100, 100), 0.0);
        // Edge: fully busy
        assert_eq!(calculate_usage_percent(100, 0), 100.0);
        // Edge: zero total (first sample or no activity)
        assert_eq!(calculate_usage_percent(0, 0), 0.0);
        // Edge: zero idle delta on large total
        assert_eq!(calculate_usage_percent(1_000_000, 0), 100.0);

        // Spot checks - must always be in [0, 100]
        let cases: &[(u64, u64)] = &[
            (1000, 500),
            (100, 25),
            (1_000_000, 999_999),
            (50, 0),
            (1, 1),
            (u64::MAX, u64::MAX - 1),
            (10, 3),
            (7, 4),
        ];
        for &(total, idle) in cases {
            let clamped_idle = idle.min(total);
            let usage = calculate_usage_percent(total, clamped_idle);
            assert!(
                (0.0..=100.0).contains(&usage),
                "usage={usage} out of range for total={total}, idle={clamped_idle}"
            );
        }
    }

    #[test]
    fn test_parse_proc_stat_cpu_line() {
        // Standard aggregate cpu line from /proc/stat
        let line = "cpu  12345 678 9012 345678 901 234 567 0 0 0";
        let values = parse_cpu_line(line).unwrap();
        assert_eq!(values, vec![12345, 678, 9012, 345678, 901, 234, 567, 0, 0, 0]);

        // Per-core line (cpu0)
        let line = "cpu0 5000 100 2000 80000 50 100 50 0 0 0";
        let values = parse_cpu_line(line).unwrap();
        assert_eq!(values, vec![5000, 100, 2000, 80000, 50, 100, 50, 0, 0, 0]);

        // Minimal line (fewer fields)
        let line = "cpu  1 2 3 4";
        let values = parse_cpu_line(line).unwrap();
        assert_eq!(values, vec![1, 2, 3, 4]);

        // Label only, no values
        let line = "cpu";
        let values = parse_cpu_line(line).unwrap();
        assert!(values.is_empty());
    }

    #[test]
    fn test_count_logical_cores() {
        // Typical /proc/stat excerpt with 4 cores
        let stat = "\
cpu  12345 678 9012 345678 901 234 567 0 0 0
cpu0 5000 100 2000 80000 50 100 50 0 0 0
cpu1 4000 200 1500 82000 40 80 40 0 0 0
cpu2 3000 150 1000 85000 30 60 30 0 0 0
cpu3 2000 100 800 88000 20 40 20 0 0 0
intr 123456789
ctxt 98765432
btime 1234567890
processes 12345
procs_running 2
procs_blocked 0";
        assert_eq!(count_logical_cores(stat), 4);

        // Only aggregate cpu line, no per-core lines
        let stat_minimal = "cpu  100 200 300 400 500 600 700 0 0 0\n";
        assert_eq!(count_logical_cores(stat_minimal), 0);

        // Empty content
        assert_eq!(count_logical_cores(""), 0);

        // Single core
        let stat_single = "cpu  1 2 3 4\ncpu0 1 2 3 4\nctxt 100\n";
        assert_eq!(count_logical_cores(stat_single), 1);
    }
}
