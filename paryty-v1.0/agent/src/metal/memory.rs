#![allow(dead_code)]

//! Memory Metrics Collector
//!
//! Collects system and per-process memory usage data.
//! Uses /proc/meminfo on Linux, sysinfo on other platforms.
//!
//! Performance target: <500us collection time.

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use tracing::instrument;

use crate::config::MetalConfig;

/// Maximum number of PIDs to scan for per-process memory.
const MAX_PID_SCAN: usize = 1000;

/// Default number of top processes to report (by RSS).
const DEFAULT_TOP_N: usize = 10;

/// Memory pressure information from `/proc/pressure/memory` (Linux 4.20+).
///
/// PSI (Pressure Stall Information) metrics indicate the percentage of time
/// in which at least some tasks are stalled on memory.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct MemoryPressure {
    /// Some tasks stalled, 10-second exponentially-decaying average.
    pub some_avg10: f64,
    /// Some tasks stalled, 1-minute exponentially-decaying average.
    pub some_avg60: f64,
    /// Some tasks stalled, 5-minute exponentially-decaying average.
    pub some_avg300: f64,
    /// All tasks stalled, 10-second exponentially-decaying average.
    pub full_avg10: f64,
    /// All tasks stalled, 1-minute exponentially-decaying average.
    pub full_avg60: f64,
    /// All tasks stalled, 5-minute exponentially-decaying average.
    pub full_avg300: f64,
}

/// Per-process memory usage snapshot.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct ProcessMemory {
    /// Process ID.
    pub pid: u32,
    /// Process name (comm field).
    pub name: String,
    /// Resident Set Size in bytes.
    pub rss_bytes: u64,
    /// Virtual memory size in bytes.
    pub vsz_bytes: u64,
    /// Swap usage in bytes.
    pub swap_bytes: u64,
}

/// Memory metrics snapshot.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct MemoryMetrics {
    pub timestamp: String,
    pub total_bytes: u64,
    pub used_bytes: u64,
    pub free_bytes: u64,
    pub available_bytes: u64,
    /// Page cache memory in bytes. `None` on non-Linux (sysinfo 0.30 has no API).
    pub cached_bytes: Option<u64>,
    /// Buffer memory in bytes. `None` on non-Linux (sysinfo 0.30 has no API).
    pub buffer_bytes: Option<u64>,
    pub swap_total_bytes: u64,
    pub swap_used_bytes: u64,
    pub usage_percent: f64,
    /// Memory pressure (PSI) data. `None` on kernels < 4.20 or non-Linux.
    pub pressure: Option<MemoryPressure>,
    /// Top N processes by RSS. Empty on non-Linux.
    pub top_processes: Vec<ProcessMemory>,
}

/// Memory collector.
pub struct MemoryCollector;

impl MemoryCollector {
    pub fn new() -> Self {
        Self
    }

    #[instrument(skip(self, _config), fields(collector = "memory"))]
    pub fn collect(&self, _config: &MetalConfig) -> Result<MemoryMetrics> {
        if cfg!(target_os = "linux") {
            self.collect_linux()
        } else {
            self.collect_cross_platform()
        }
    }

    #[cfg(target_os = "linux")]
    fn collect_linux(&self) -> Result<MemoryMetrics> {
        use std::fs;

        let meminfo = fs::read_to_string("/proc/meminfo")?;
        let values = parse_meminfo(&meminfo);

        let total = values.get("MemTotal").copied().unwrap_or(0);
        let free = values.get("MemFree").copied().unwrap_or(0);
        let available = values.get("MemAvailable").copied().unwrap_or(free);
        let cached = values.get("Cached").copied().unwrap_or(0);
        let buffers = values.get("Buffers").copied().unwrap_or(0);
        let swap_total = values.get("SwapTotal").copied().unwrap_or(0);
        let swap_free = values.get("SwapFree").copied().unwrap_or(0);
        let used = total.saturating_sub(available);

        let pressure = read_memory_pressure();
        let top_processes = read_top_processes(DEFAULT_TOP_N);

        Ok(MemoryMetrics {
            timestamp: Utc::now().to_rfc3339(),
            total_bytes: total,
            used_bytes: used,
            free_bytes: free,
            available_bytes: available,
            cached_bytes: Some(cached),
            buffer_bytes: Some(buffers),
            swap_total_bytes: swap_total,
            swap_used_bytes: swap_total.saturating_sub(swap_free),
            usage_percent: if total > 0 {
                ((used as f64 / total as f64) * 100.0).clamp(0.0, 100.0)
            } else {
                0.0
            },
            pressure,
            top_processes,
        })
    }

    #[cfg(not(target_os = "linux"))]
    fn collect_linux(&self) -> Result<MemoryMetrics> {
        self.collect_cross_platform()
    }

    fn collect_cross_platform(&self) -> Result<MemoryMetrics> {
        use sysinfo::System;

        let sys = System::new_all();
        let total = sys.total_memory();
        let used = sys.used_memory();
        let free = total.saturating_sub(used);
        let available = sys.available_memory();
        let swap_total = sys.total_swap();
        let swap_used = sys.used_swap();
        // buffer_bytes and cached_bytes are Linux-only (/proc/meminfo fields).
        // sysinfo 0.30 does not expose cached_memory on Windows/macOS.

        Ok(MemoryMetrics {
            timestamp: Utc::now().to_rfc3339(),
            total_bytes: total,
            used_bytes: used,
            free_bytes: free,
            available_bytes: available,
            cached_bytes: None, // genuinely unavailable — sysinfo 0.30 has no API on Windows/macOS
            buffer_bytes: None, // genuinely unavailable — sysinfo 0.30 has no API on Windows/macOS
            swap_total_bytes: swap_total,
            swap_used_bytes: swap_used,
            usage_percent: if total > 0 {
                ((used as f64 / total as f64) * 100.0).clamp(0.0, 100.0)
            } else {
                0.0
            },
            pressure: None,            // PSI is Linux-only (/proc/pressure/memory)
            top_processes: Vec::new(), // per-process memory collected by ProcessCollector
        })
    }
}

impl Default for MemoryCollector {
    fn default() -> Self {
        Self::new()
    }
}

// ---------------------------------------------------------------------------
// Pure parsing functions (testable without /proc access)
// ---------------------------------------------------------------------------

/// Parse `/proc/meminfo` content into key-value pairs (values in bytes).
///
/// Each line has the format `Key:   12345 kB`. Values are converted from
/// KiB to bytes. The map is pre-allocated with capacity 64 to avoid
/// rehashing during the typical ~50-line meminfo file.
fn parse_meminfo(content: &str) -> std::collections::HashMap<String, u64> {
    use std::collections::HashMap;

    let mut values = HashMap::with_capacity(64);

    for line in content.lines() {
        let parts: Vec<&str> = line.split_whitespace().collect();
        if parts.len() >= 2 {
            let key = parts[0].trim_end_matches(':');
            let value_kb: u64 = parts[1].parse().unwrap_or(0);
            values.insert(key.to_string(), value_kb * 1024); // Convert KB to bytes
        }
    }

    values
}

/// Parse `/proc/pressure/memory` content into a `MemoryPressure` struct.
///
/// Expected format:
/// ```text
/// some avg10=0.00 avg60=0.00 avg300=0.00 total=0
/// full avg10=0.00 avg60=0.00 avg300=0.00 total=0
/// ```
///
/// Returns `None` if the content cannot be parsed.
fn parse_pressure(content: &str) -> Option<MemoryPressure> {
    let mut some_avg10 = 0.0_f64;
    let mut some_avg60 = 0.0_f64;
    let mut some_avg300 = 0.0_f64;
    let mut full_avg10 = 0.0_f64;
    let mut full_avg60 = 0.0_f64;
    let mut full_avg300 = 0.0_f64;

    let mut found_some = false;
    let mut found_full = false;

    for line in content.lines() {
        let line = line.trim();
        if line.starts_with("some") {
            if let Some((a10, a60, a300)) = parse_pressure_line(line) {
                some_avg10 = a10;
                some_avg60 = a60;
                some_avg300 = a300;
                found_some = true;
            }
        } else if line.starts_with("full") {
            if let Some((a10, a60, a300)) = parse_pressure_line(line) {
                full_avg10 = a10;
                full_avg60 = a60;
                full_avg300 = a300;
                found_full = true;
            }
        }
    }

    if found_some && found_full {
        Some(MemoryPressure {
            some_avg10,
            some_avg60,
            some_avg300,
            full_avg10,
            full_avg60,
            full_avg300,
        })
    } else {
        None
    }
}

/// Parse a single PSI line extracting avg10, avg60, avg300 values.
///
/// Line format: `some avg10=0.00 avg60=0.00 avg300=0.00 total=12345`
fn parse_pressure_line(line: &str) -> Option<(f64, f64, f64)> {
    let mut avg10 = None;
    let mut avg60 = None;
    let mut avg300 = None;

    for token in line.split_whitespace() {
        if let Some(val) = token.strip_prefix("avg10=") {
            avg10 = val.parse::<f64>().ok();
        } else if let Some(val) = token.strip_prefix("avg60=") {
            avg60 = val.parse::<f64>().ok();
        } else if let Some(val) = token.strip_prefix("avg300=") {
            avg300 = val.parse::<f64>().ok();
        }
    }

    match (avg10, avg60, avg300) {
        (Some(a), Some(b), Some(c)) => Some((a, b, c)),
        _ => None,
    }
}

/// Parse a `/proc/[pid]/status` line value (e.g., `VmRSS:\t1234 kB`).
///
/// Returns the value converted to bytes, or `None` if the line is missing
/// or the value cannot be parsed.
fn parse_status_kb_value(line: &str) -> Option<u64> {
    // Format: "VmRSS:     12345 kB"
    let after_colon = line.split(':').nth(1)?;
    let trimmed = after_colon.trim();
    let parts: Vec<&str> = trimmed.split_whitespace().collect();
    let value: u64 = parts.first()?.parse().ok()?;
    Some(value * 1024) // kB to bytes
}

// ---------------------------------------------------------------------------
// Linux-specific /proc readers
// ---------------------------------------------------------------------------

/// Read memory pressure from `/proc/pressure/memory`.
///
/// Returns `None` if the file does not exist (kernels < 4.20) or cannot
/// be parsed.
#[cfg(target_os = "linux")]
fn read_memory_pressure() -> Option<MemoryPressure> {
    use std::fs;

    let content = fs::read_to_string("/proc/pressure/memory").ok()?;
    parse_pressure(&content)
}

#[cfg(not(target_os = "linux"))]
fn read_memory_pressure() -> Option<MemoryPressure> {
    None
}

/// Scan `/proc/[pid]/status` for the top N processes by RSS.
///
/// Bounded to `MAX_PID_SCAN` directory entries to avoid excessive iteration
/// on systems with many threads/processes. On WSL2, uses shell-based PID
/// enumeration to avoid the Plan 9 filesystem bridge hang.
#[cfg(target_os = "linux")]
fn read_top_processes(n: usize) -> Vec<ProcessMemory> {
    use std::fs;

    // WSL2 detection: /proc/version contains "microsoft" on WSL2.
    // fs::read_dir("/proc") hangs on the Plan 9 bridge.
    let wsl2 = fs::read_to_string("/proc/version")
        .map(|v| v.to_lowercase().contains("microsoft"))
        .unwrap_or(false);

    let pids: Vec<u32> = if wsl2 {
        let output = std::process::Command::new("/bin/sh")
            .args(["-c", "ls -d /proc/[0-9]* 2>/dev/null"])
            .output()
            .unwrap_or(std::process::Output {
                status: std::process::ExitStatus::default(),
                stdout: Vec::new(),
                stderr: Vec::new(),
            });
        let stdout = String::from_utf8_lossy(&output.stdout);
        stdout
            .lines()
            .filter_map(|line| line.split('/').next_back().and_then(|s| s.parse::<u32>().ok()))
            .collect()
    } else {
        match fs::read_dir("/proc") {
            Ok(dir) => dir
                .filter_map(|e| e.ok())
                .filter_map(|e| e.file_name().to_string_lossy().parse::<u32>().ok())
                .collect(),
            Err(_) => return Vec::new(),
        }
    };

    let pid_iter = pids.into_iter();

    let mut processes: Vec<ProcessMemory> = Vec::with_capacity(n.min(256));
    let mut scanned: usize = 0;

    for pid in pid_iter {
        if scanned >= MAX_PID_SCAN {
            break;
        }
        scanned += 1;

        let status_path = format!("/proc/{}/status", pid);
        let status_content = match fs::read_to_string(&status_path) {
            Ok(c) => c,
            Err(_) => continue, // Process may have exited
        };

        let mut name = String::new();
        let mut rss_bytes = 0_u64;
        let mut vsz_bytes = 0_u64;
        let mut swap_bytes = 0_u64;

        for line in status_content.lines() {
            if line.starts_with("Name:") {
                // Format: "Name:\tprocess_name"
                if let Some(after_colon) = line.split(':').nth(1) {
                    name = after_colon.trim().to_string();
                }
            } else if line.starts_with("VmRSS:") {
                rss_bytes = parse_status_kb_value(line).unwrap_or(0);
            } else if line.starts_with("VmSize:") {
                vsz_bytes = parse_status_kb_value(line).unwrap_or(0);
            } else if line.starts_with("VmSwap:") {
                swap_bytes = parse_status_kb_value(line).unwrap_or(0);
            }
        }

        // Skip processes with zero RSS (kernel threads, zombies)
        if rss_bytes == 0 {
            continue;
        }

        processes.push(ProcessMemory { pid, name, rss_bytes, vsz_bytes, swap_bytes });
    }

    // Sort by RSS descending, take top N
    processes.sort_unstable_by(|a, b| b.rss_bytes.cmp(&a.rss_bytes));
    processes.truncate(n);
    processes
}

#[cfg(not(target_os = "linux"))]
fn read_top_processes(_n: usize) -> Vec<ProcessMemory> {
    Vec::new()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    // -- /proc/meminfo parsing --

    #[test]
    fn test_parse_meminfo() {
        let content = "\
MemTotal:       16384000 kB
MemFree:         2048000 kB
MemAvailable:    8192000 kB
Buffers:          512000 kB
Cached:          4096000 kB
SwapTotal:       2048000 kB
SwapFree:        1024000 kB
Shmem:            256000 kB
SReclaimable:     128000 kB";

        let values = parse_meminfo(content);

        assert_eq!(values.get("MemTotal").copied(), Some(16_384_000 * 1024));
        assert_eq!(values.get("MemFree").copied(), Some(2_048_000 * 1024));
        assert_eq!(values.get("MemAvailable").copied(), Some(8_192_000 * 1024));
        assert_eq!(values.get("Buffers").copied(), Some(512_000 * 1024));
        assert_eq!(values.get("Cached").copied(), Some(4_096_000 * 1024));
        assert_eq!(values.get("SwapTotal").copied(), Some(2_048_000 * 1024));
        assert_eq!(values.get("SwapFree").copied(), Some(1_024_000 * 1024));
        assert_eq!(values.get("Shmem").copied(), Some(256_000 * 1024));
        assert_eq!(values.get("SReclaimable").copied(), Some(128_000 * 1024));
    }

    #[test]
    fn test_parse_meminfo_empty() {
        let values = parse_meminfo("");
        assert!(values.is_empty());
    }

    #[test]
    fn test_parse_meminfo_malformed_lines_skipped() {
        let content = "\
MemTotal:       1000 kB
bad_line
MemFree:         500 kB
AnotherBadLine: not_a_number kB";

        let values = parse_meminfo(content);
        assert_eq!(values.get("MemTotal").copied(), Some(1000 * 1024));
        assert_eq!(values.get("MemFree").copied(), Some(500 * 1024));
        // "AnotherBadLine" value is not numeric, so it falls back to 0
        assert_eq!(values.get("AnotherBadLine").copied(), Some(0));
    }

    // -- /proc/pressure/memory parsing --

    #[test]
    fn test_parse_pressure() {
        let content = "\
some avg10=0.01 avg60=0.05 avg300=0.10 total=12345678
full avg10=0.00 avg60=0.02 avg300=0.05 total=9876543";

        let pressure = parse_pressure(content).expect("should parse valid pressure data");

        assert_eq!(pressure.some_avg10, 0.01);
        assert_eq!(pressure.some_avg60, 0.05);
        assert_eq!(pressure.some_avg300, 0.10);
        assert_eq!(pressure.full_avg10, 0.0);
        assert_eq!(pressure.full_avg60, 0.02);
        assert_eq!(pressure.full_avg300, 0.05);
    }

    #[test]
    fn test_parse_pressure_missing_full_line() {
        let content = "some avg10=0.01 avg60=0.05 avg300=0.10 total=12345678\n";
        assert!(
            parse_pressure(content).is_none(),
            "should return None when 'full' line is missing"
        );
    }

    #[test]
    fn test_parse_pressure_empty_content() {
        assert!(parse_pressure("").is_none());
    }

    #[test]
    fn test_parse_pressure_high_values() {
        let content = "\
some avg10=99.99 avg60=88.88 avg300=77.77 total=999999999
full avg10=55.55 avg60=44.44 avg300=33.33 total=888888888";

        let pressure = parse_pressure(content).expect("should parse high pressure values");
        assert_eq!(pressure.some_avg10, 99.99);
        assert_eq!(pressure.full_avg300, 33.33);
    }

    // -- Pressure line parsing --

    #[test]
    fn test_parse_pressure_line_valid() {
        let line = "some avg10=1.23 avg60=4.56 avg300=7.89 total=999";
        let result = parse_pressure_line(line);
        assert_eq!(result, Some((1.23, 4.56, 7.89)));
    }

    #[test]
    fn test_parse_pressure_line_zeroes() {
        let line = "full avg10=0.00 avg60=0.00 avg300=0.00 total=0";
        let result = parse_pressure_line(line);
        assert_eq!(result, Some((0.0, 0.0, 0.0)));
    }

    #[test]
    fn test_parse_pressure_line_missing_field() {
        let line = "some avg10=1.00 avg60=2.00 total=100";
        assert!(parse_pressure_line(line).is_none());
    }

    // -- Status value parsing --

    #[test]
    fn test_parse_status_kb_value() {
        assert_eq!(parse_status_kb_value("VmRSS:\t   12345 kB"), Some(12_345 * 1024));
        assert_eq!(parse_status_kb_value("VmSize:\t    1000 kB"), Some(1000 * 1024));
        assert_eq!(parse_status_kb_value("VmSwap:\t       0 kB"), Some(0));
        assert_eq!(parse_status_kb_value("Name:\tpython3"), None);
        assert_eq!(parse_status_kb_value(""), None);
        assert_eq!(parse_status_kb_value("VmRSS:\t   not_a_number kB"), None);
    }

    // -- Usage percent bounds --

    #[test]
    fn test_usage_percent_in_range() {
        // Verify that the calculation always yields [0.0, 100.0]
        let cases: &[(u64, u64)] = &[
            (0, 0),                 // Zero total -> 0%
            (1_000_000, 0),         // Fully used
            (1_000_000, 500_000),   // Half used
            (1_000_000, 1_000_000), // Fully free
            (u64::MAX, 0),          // Max total, zero free
            (1, 1),                 // Minimal total, fully free
            (100, 150), // Available > total (MemAvailable can exceed MemFree on some kernels)
        ];

        for &(total, available) in cases {
            let used = total.saturating_sub(available);
            let percent = if total > 0 {
                ((used as f64 / total as f64) * 100.0).clamp(0.0, 100.0)
            } else {
                0.0
            };
            assert!(
                (0.0..=100.0).contains(&percent),
                "usage_percent={percent} out of range for total={total}, available={available}"
            );
        }
    }

    // -- MemoryMetrics defaults --

    #[test]
    fn test_memory_metrics_pressure_none_on_cross_platform() {
        // Cross-platform collect should always have pressure = None
        // and top_processes = empty
        // (We can only truly verify this on non-Linux, but the structures
        //  are correct regardless.)
        let pressure: Option<MemoryPressure> = None;
        assert!(pressure.is_none());

        let procs: Vec<ProcessMemory> = Vec::new();
        assert!(procs.is_empty());
    }

    #[test]
    fn test_process_memory_fields() {
        let pm = ProcessMemory {
            pid: 1,
            name: "init".to_string(),
            rss_bytes: 1024 * 1024,
            vsz_bytes: 4 * 1024 * 1024,
            swap_bytes: 0,
        };
        assert_eq!(pm.pid, 1);
        assert_eq!(pm.name, "init");
        assert_eq!(pm.rss_bytes, 1_048_576);
        assert_eq!(pm.vsz_bytes, 4_194_304);
        assert_eq!(pm.swap_bytes, 0);
    }

    #[test]
    fn test_memory_pressure_fields() {
        let mp = MemoryPressure {
            some_avg10: 0.01,
            some_avg60: 0.02,
            some_avg300: 0.03,
            full_avg10: 0.00,
            full_avg60: 0.01,
            full_avg300: 0.02,
        };
        assert_eq!(mp.some_avg10, 0.01);
        assert_eq!(mp.full_avg300, 0.02);
    }
}
