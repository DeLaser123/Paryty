#![allow(dead_code)]

//! Disk Metrics Collector
//!
//! Collects disk I/O and usage metrics from `/proc/diskstats`, `/proc/mounts`,
//! and `/sys/block/` for SSD/HDD detection, utilization, and queue depth.

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use tracing::instrument;

use crate::config::MetalConfig;

/// Disk device metrics.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct DiskDevice {
    pub device_name: String,
    pub mount_point: String,
    pub filesystem_type: String,
    pub total_bytes: u64,
    pub used_bytes: u64,
    pub free_bytes: u64,
    pub read_ops_per_sec: f64,
    pub write_ops_per_sec: f64,
    pub read_bytes_per_sec: f64,
    pub write_bytes_per_sec: f64,
    pub io_latency_ms: f64,
    pub queue_depth: f64,
    /// Whether this device is an SSD (`true`) or HDD (`false`).
    /// Determined by `/sys/block/{device}/queue/rotational`.
    pub is_ssd: bool,
    /// Device utilization percentage (0.0–100.0).
    /// Derived from the `io_ticks` delta in `/proc/diskstats` (field 12),
    /// representing the fraction of wall-clock time the device had at least
    /// one I/O request in flight.
    pub utilization_pct: f64,
}

/// Disk metrics collection result.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct DiskMetrics {
    pub timestamp: String,
    pub devices: Vec<DiskDevice>,
}

/// Disk collector.
pub struct DiskCollector {
    prev_stats: std::sync::Mutex<std::collections::HashMap<String, DiskStatSnapshot>>,
}

/// Snapshot of cumulative counters from `/proc/diskstats` for a single device.
///
/// Used for delta-based rate and utilization calculations between collection
/// cycles.
#[derive(Debug, Clone)]
struct DiskStatSnapshot {
    read_ops: u64,
    write_ops: u64,
    read_sectors: u64,
    write_sectors: u64,
    /// Cumulative time spent doing I/O in milliseconds (field 12 from
    /// `/proc/diskstats`, commonly called `io_ticks`).
    io_ticks: u64,
    timestamp: std::time::Instant,
}

/// Parsed fields from a single `/proc/diskstats` line.
#[derive(Debug, Clone, PartialEq)]
struct DiskstatsLine {
    name: String,
    read_ops: u64,
    read_sectors: u64,
    write_ops: u64,
    write_sectors: u64,
    io_ticks: u64,
}

/// Parse a single line from `/proc/diskstats`.
///
/// Returns `None` if the line has too few fields or the device should be
/// skipped (e.g., `loop*`, `ram*`).
///
/// `/proc/diskstats` field layout (0-indexed after splitting on whitespace):
/// ```text
///  0: major   1: minor   2: name
///  3: reads   4: rmerged 5: rsectors  6: rtime_ms
///  7: writes  8: wmerged 9: wsectors 10: wtime_ms
/// 11: in_flight  12: io_ticks  13: weighted_io_ticks
/// ```
fn parse_diskstats_line(line: &str) -> Option<DiskstatsLine> {
    let parts: Vec<&str> = line.split_whitespace().collect();
    if parts.len() < 14 {
        return None;
    }
    let name = parts[2].to_string();
    // Skip pseudo-devices that are not real disks
    if name.starts_with("loop") || name.starts_with("ram") {
        return None;
    }
    Some(DiskstatsLine {
        name,
        read_ops: parts[3].parse().unwrap_or(0),
        read_sectors: parts[5].parse().unwrap_or(0),
        write_ops: parts[7].parse().unwrap_or(0),
        write_sectors: parts[9].parse().unwrap_or(0),
        io_ticks: parts[12].parse().unwrap_or(0),
    })
}

/// Interpret the rotational flag value from
/// `/sys/block/{device}/queue/rotational`.
///
/// Returns `true` for SSD (rotational = `0`), `false` for HDD (rotational = `1`).
/// Any unexpected value defaults to HDD (`false`).
fn is_ssd_from_rotational(value: &str) -> bool {
    value.trim() == "0"
}

/// Calculate disk utilization percentage from `io_ticks` delta.
///
/// * `io_ticks_delta` — change in cumulative I/O time (ms) between samples.
/// * `time_delta_ms` — wall-clock elapsed time (ms) between samples.
///
/// Returns a value clamped to `[0.0, 100.0]`.
fn calculate_utilization_pct(io_ticks_delta: u64, time_delta_ms: f64) -> f64 {
    if time_delta_ms <= 0.0 {
        return 0.0;
    }
    let pct = (io_ticks_delta as f64 / time_delta_ms) * 100.0;
    pct.clamp(0.0, 100.0)
}

impl DiskCollector {
    pub fn new() -> Self {
        Self { prev_stats: std::sync::Mutex::new(std::collections::HashMap::new()) }
    }

    #[instrument(skip(self))]
    pub fn collect(&self, _config: &MetalConfig) -> Result<DiskMetrics> {
        let devices = if cfg!(target_os = "linux") {
            self.collect_linux()?
        } else {
            self.collect_cross_platform()?
        };

        Ok(DiskMetrics { timestamp: Utc::now().to_rfc3339(), devices })
    }

    #[cfg(target_os = "linux")]
    fn collect_linux(&self) -> Result<Vec<DiskDevice>> {
        use std::fs;
        use std::time::Instant;

        let now = Instant::now();

        // Read /proc/diskstats for I/O metrics
        let diskstats = fs::read_to_string("/proc/diskstats").unwrap_or_default();
        let mut current_stats = std::collections::HashMap::new();

        for line in diskstats.lines() {
            if let Some(parsed) = parse_diskstats_line(line) {
                current_stats.insert(
                    parsed.name.clone(),
                    DiskStatSnapshot {
                        read_ops: parsed.read_ops,
                        write_ops: parsed.write_ops,
                        read_sectors: parsed.read_sectors,
                        write_sectors: parsed.write_sectors,
                        io_ticks: parsed.io_ticks,
                        timestamp: now,
                    },
                );
            }
        }

        // Calculate rates from previous snapshot
        let prev = self.prev_stats.lock().unwrap();
        let mut result = Vec::with_capacity(current_stats.len());

        for (name, snapshot) in &current_stats {
            let mount_point = find_mount_point(name);
            let fs_type = find_fs_type(&mount_point);

            let (
                read_ops_rate,
                write_ops_rate,
                read_bytes_rate,
                write_bytes_rate,
                latency,
                utilization_pct,
            ) = if let Some(prev_snapshot) = prev.get(name) {
                let dt_secs = now.duration_since(prev_snapshot.timestamp).as_secs_f64();
                let dt_ms = now.duration_since(prev_snapshot.timestamp).as_millis() as f64;
                if dt_secs > 0.0 {
                    let io_ticks_delta = snapshot.io_ticks.saturating_sub(prev_snapshot.io_ticks);
                    (
                        snapshot.read_ops.saturating_sub(prev_snapshot.read_ops) as f64 / dt_secs,
                        snapshot.write_ops.saturating_sub(prev_snapshot.write_ops) as f64 / dt_secs,
                        (snapshot.read_sectors.saturating_sub(prev_snapshot.read_sectors) * 512)
                            as f64
                            / dt_secs,
                        (snapshot.write_sectors.saturating_sub(prev_snapshot.write_sectors) * 512)
                            as f64
                            / dt_secs,
                        // Average I/O latency per read op (existing metric)
                        if snapshot.read_ops > prev_snapshot.read_ops {
                            (snapshot.io_ticks.saturating_sub(prev_snapshot.io_ticks)) as f64
                                / (snapshot.read_ops.saturating_sub(prev_snapshot.read_ops)) as f64
                        } else {
                            0.0
                        },
                        // Utilization: fraction of time the device was busy
                        calculate_utilization_pct(io_ticks_delta, dt_ms),
                    )
                } else {
                    (0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
                }
            } else {
                (0.0, 0.0, 0.0, 0.0, 0.0, 0.0)
            };

            // Get filesystem usage
            let (total, used, free) = get_disk_usage(&mount_point);

            // Detect SSD/HDD and read queue depth from /sys/block/
            let base = base_device_name(name);
            let is_ssd = detect_disk_type(&base);
            let queue_depth = read_queue_depth(&base);

            result.push(DiskDevice {
                device_name: name.clone(),
                mount_point,
                filesystem_type: fs_type,
                total_bytes: total,
                used_bytes: used,
                free_bytes: free,
                read_ops_per_sec: read_ops_rate,
                write_ops_per_sec: write_ops_rate,
                read_bytes_per_sec: read_bytes_rate,
                write_bytes_per_sec: write_bytes_rate,
                io_latency_ms: latency,
                queue_depth: f64::from(queue_depth),
                is_ssd,
                utilization_pct,
            });
        }

        // Update previous stats for next cycle's delta calculation
        drop(prev);
        *self.prev_stats.lock().unwrap() = current_stats;

        Ok(result)
    }

    #[cfg(not(target_os = "linux"))]
    fn collect_linux(&self) -> Result<Vec<DiskDevice>> {
        self.collect_cross_platform()
    }

    fn collect_cross_platform(&self) -> Result<Vec<DiskDevice>> {
        use sysinfo::Disks;

        let disks = Disks::new_with_refreshed_list();
        let mut result = Vec::new();

        for disk in disks.list() {
            let mount_point = disk.mount_point().to_string_lossy().to_string();
            let filesystem_type = disk.file_system().to_string_lossy().to_string();
            let total = disk.total_space();
            let available = disk.available_space();
            let used = total.saturating_sub(available);
            let utilization_pct = if total > 0 {
                (used as f64 / total as f64) * 100.0
            } else {
                0.0
            };

            result.push(DiskDevice {
                device_name: disk.name().to_string_lossy().to_string(),
                mount_point,
                filesystem_type,
                total_bytes: total,
                used_bytes: used,
                free_bytes: available,
                // sysinfo doesn't provide per-disk IOPS/throughput on most platforms
                read_ops_per_sec: 0.0,
                write_ops_per_sec: 0.0,
                read_bytes_per_sec: 0.0,
                write_bytes_per_sec: 0.0,
                io_latency_ms: 0.0,
                queue_depth: 0.0,
                is_ssd: matches!(disk.kind(), sysinfo::DiskKind::SSD),
                utilization_pct,
            });
        }

        Ok(result)
    }
}

/// Resolve the base block device name from a `/proc/diskstats` device name.
///
/// Partition names (e.g. `sda1`, `nvme0n1p1`) are mapped back to their parent
/// device in `/sys/block/` by stripping the partition suffix.
#[cfg(target_os = "linux")]
fn base_device_name(device: &str) -> String {
    use std::path::Path;

    // If /sys/block/{device} exists, it's already a base device
    if Path::new(&format!("/sys/block/{device}")).exists() {
        return device.to_string();
    }

    // Try stripping nvme partition suffix (e.g., "nvme0n1p1" → "nvme0n1")
    if let Some(pos) = device.rfind('p') {
        let candidate = &device[..pos];
        if !candidate.is_empty()
            && candidate.chars().last().map_or(false, |c| c.is_ascii_digit())
            && Path::new(&format!("/sys/block/{candidate}")).exists()
        {
            return candidate.to_string();
        }
    }

    // Try stripping trailing digits (e.g., "sda1" → "sda")
    let base = device.trim_end_matches(|c: char| c.is_ascii_digit());
    if !base.is_empty() && Path::new(&format!("/sys/block/{base}")).exists() {
        return base.to_string();
    }

    // Fallback: return as-is
    device.to_string()
}

/// Detect whether a block device is an SSD by reading
/// `/sys/block/{device}/queue/rotational`.
///
/// Returns `true` for SSD, `false` for HDD or on read error.
#[cfg(target_os = "linux")]
fn detect_disk_type(base_device: &str) -> bool {
    let path = format!("/sys/block/{base_device}/queue/rotational");
    std::fs::read_to_string(&path).map(|content| is_ssd_from_rotational(&content)).unwrap_or(false)
}

/// Read the queue depth (`nr_requests`) for a block device from
/// `/sys/block/{device}/queue/nr_requests`.
///
/// Returns `0` on read or parse error.
#[cfg(target_os = "linux")]
fn read_queue_depth(base_device: &str) -> u32 {
    let path = format!("/sys/block/{base_device}/queue/nr_requests");
    std::fs::read_to_string(&path).ok().and_then(|content| content.trim().parse().ok()).unwrap_or(0)
}

#[cfg(target_os = "linux")]
fn find_mount_point(device: &str) -> String {
    std::fs::read_to_string("/proc/mounts")
        .ok()
        .and_then(|mounts| {
            mounts.lines().find_map(|line| {
                let parts: Vec<&str> = line.split_whitespace().collect();
                if parts.len() >= 2 && parts[0].contains(device) {
                    Some(parts[1].to_string())
                } else {
                    None
                }
            })
        })
        .unwrap_or_else(|| "/".to_string())
}

#[cfg(target_os = "linux")]
fn find_fs_type(mount_point: &str) -> String {
    std::fs::read_to_string("/proc/mounts")
        .ok()
        .and_then(|mounts| {
            mounts.lines().find_map(|line| {
                let parts: Vec<&str> = line.split_whitespace().collect();
                if parts.len() >= 3 && parts[1] == mount_point {
                    Some(parts[2].to_string())
                } else {
                    None
                }
            })
        })
        .unwrap_or_else(|| "unknown".to_string())
}

#[cfg(target_os = "linux")]
fn get_disk_usage(mount_point: &str) -> (u64, u64, u64) {
    use std::ffi::CString;

    let path = CString::new(mount_point).unwrap_or_else(|_| CString::new("/").unwrap());
    // SAFETY: `statvfs` is called with a valid, NUL-terminated C string
    // pointing to an existing mount point. The `stat` struct is zeroed
    // before the call and is only read after a successful return (rc == 0).
    unsafe {
        let mut stat: libc::statvfs = std::mem::zeroed();
        if libc::statvfs(path.as_ptr(), &mut stat) == 0 {
            let total = stat.f_blocks as u64 * stat.f_frsize as u64;
            let free = stat.f_bfree as u64 * stat.f_frsize as u64;
            let available = stat.f_bavail as u64 * stat.f_frsize as u64;
            let used = total.saturating_sub(free);
            (total, used, available)
        } else {
            (0, 0, 0)
        }
    }
}

#[cfg(not(target_os = "linux"))]
fn get_disk_usage(_mount_point: &str) -> (u64, u64, u64) {
    (0, 0, 0)
}

impl Default for DiskCollector {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_diskstats_line() {
        // Typical /proc/diskstats line (14 fields):
        // major minor name reads rmerged rsectors rtime writes wmerged wsectors wtime inflight io_ticks weighted_ticks
        let line = "   8       0 sda 12345 67 890123 4567 890 12 345678 901 3 2345 3456";
        let parsed = parse_diskstats_line(line);
        assert!(parsed.is_some(), "should parse a valid diskstats line");
        let p = parsed.unwrap();
        assert_eq!(p.name, "sda");
        assert_eq!(p.read_ops, 12345);
        assert_eq!(p.read_sectors, 890123);
        assert_eq!(p.write_ops, 890);
        assert_eq!(p.write_sectors, 345678);
        assert_eq!(p.io_ticks, 2345);
    }

    #[test]
    fn test_parse_diskstats_line_skips_pseudo_devices() {
        let loop_line = "   7       0 loop0 0 0 0 0 0 0 0 0 0 0 0 0";
        assert!(parse_diskstats_line(loop_line).is_none(), "loop devices should be skipped");

        let ram_line = "   1       0 ram0 0 0 0 0 0 0 0 0 0 0 0 0";
        assert!(parse_diskstats_line(ram_line).is_none(), "ram devices should be skipped");
    }

    #[test]
    fn test_parse_diskstats_line_too_few_fields() {
        let line = "   8       0 sda";
        assert!(parse_diskstats_line(line).is_none(), "lines with < 14 fields should return None");
    }

    #[test]
    fn test_ssd_detection() {
        // rotational = 0 → SSD
        assert!(is_ssd_from_rotational("0"), "'0' means SSD");
        assert!(is_ssd_from_rotational("0\n"), "trimmed '0\\n' means SSD");

        // rotational = 1 → HDD
        assert!(!is_ssd_from_rotational("1"), "'1' means HDD");
        assert!(!is_ssd_from_rotational("1\n"), "trimmed '1\\n' means HDD");

        // Edge cases default to HDD
        assert!(!is_ssd_from_rotational(""), "empty string defaults to HDD");
        assert!(!is_ssd_from_rotational("2"), "unexpected value defaults to HDD");
    }

    #[test]
    fn test_utilization_calculation() {
        // 500ms I/O in 1000ms wall-clock → 50%
        let pct = calculate_utilization_pct(500, 1000.0);
        assert!((pct - 50.0).abs() < f64::EPSILON, "expected 50%, got {pct}");

        // 0ms I/O → 0%
        let pct = calculate_utilization_pct(0, 1000.0);
        assert!((pct - 0.0).abs() < f64::EPSILON, "expected 0%, got {pct}");

        // Zero time delta → 0%
        let pct = calculate_utilization_pct(500, 0.0);
        assert!((pct - 0.0).abs() < f64::EPSILON, "zero delta should yield 0%");

        // Negative time delta → 0%
        let pct = calculate_utilization_pct(500, -100.0);
        assert!((pct - 0.0).abs() < f64::EPSILON, "negative delta should yield 0%");

        // Over 100% should be clamped to 100%
        let pct = calculate_utilization_pct(1500, 1000.0);
        assert!((pct - 100.0).abs() < f64::EPSILON, "should clamp to 100%, got {pct}");

        // Exact 100%
        let pct = calculate_utilization_pct(1000, 1000.0);
        assert!((pct - 100.0).abs() < f64::EPSILON, "expected 100%, got {pct}");

        // Small realistic delta
        let pct = calculate_utilization_pct(12, 5000.0);
        assert!((pct - 0.24).abs() < 1e-10, "expected 0.24%, got {pct}");
    }

    #[test]
    fn test_diskstats_nvme_line() {
        let line = "259       0 nvme0n1 50000 100 4000000 2000 30000 50 2400000 1500 0 3000 3500";
        let parsed = parse_diskstats_line(line);
        assert!(parsed.is_some());
        let p = parsed.unwrap();
        assert_eq!(p.name, "nvme0n1");
        assert_eq!(p.read_ops, 50000);
        assert_eq!(p.write_ops, 30000);
        assert_eq!(p.io_ticks, 3000);
    }

    #[test]
    fn test_disk_collector_new_is_default() {
        let a = DiskCollector::new();
        let b = DiskCollector::default();
        // Both should be valid — just verifying construction doesn't panic
        let _ = (a, b);
    }
}
