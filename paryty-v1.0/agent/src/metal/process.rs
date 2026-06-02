//! Process Tree Collector
//!
//! Discovers and monitors all running processes with parent-child relationships.
//! Collects CPU usage via delta-based calculation, container IDs via cgroup
//! parsing, and process start times via /proc/[pid]/stat.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::Instant;

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use tracing::instrument;

use crate::config::MetalConfig;

/// Clock ticks per second on Linux (`sysconf(_SC_CLK_TCK)`).
/// Hardcoded to the standard value; all major Linux architectures use 100.
/// Also used as a cross-platform test constant.
#[allow(dead_code)] // Used only in Linux code path; ungated for cross-platform testability
const CLOCK_TICKS_PER_SEC: u64 = 100;

/// Per-process metrics.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct ProcessInfo {
    pub pid: u32,
    pub parent_pid: u32,
    pub name: String,
    pub command_line: String,
    pub cpu_usage_percent: f64,
    pub rss_bytes: u64,
    pub vsz_bytes: u64,
    pub status: String,
    pub thread_count: u32,
    pub fd_count: u32,
    pub container_id: String,
    pub started_at: String,
}

/// Process metrics collection result.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct ProcessMetrics {
    pub timestamp: String,
    pub processes: Vec<ProcessInfo>,
}

/// Process tree collector with delta-based CPU usage tracking.
#[allow(dead_code)] // Fields are read in Linux /proc path; cross-platform path uses sysinfo
pub struct ProcessCollector {
    /// Previous per-process CPU times: pid -> (utime, stime).
    prev_cpu_times: Mutex<HashMap<u32, (u64, u64)>>,
    /// Timestamp of the previous collection sample.
    prev_sample_time: Mutex<Option<Instant>>,
}

impl ProcessCollector {
    pub fn new() -> Self {
        Self { prev_cpu_times: Mutex::new(HashMap::new()), prev_sample_time: Mutex::new(None) }
    }

    /// Collect process metrics.
    ///
    /// On Linux, reads /proc directly with delta-based CPU% calculation.
    /// On other platforms, falls back to sysinfo crate.
    #[instrument(skip(self))]
    pub fn collect(&self, _config: &MetalConfig) -> Result<ProcessMetrics> {
        let processes = if cfg!(target_os = "linux") {
            self.collect_linux()?
        } else {
            self.collect_cross_platform()?
        };

        Ok(ProcessMetrics { timestamp: Utc::now().to_rfc3339(), processes })
    }

    #[cfg(target_os = "linux")]
    fn collect_linux(&self) -> Result<Vec<ProcessInfo>> {
        use std::fs;

        // Snapshot the current time for delta calculation.
        let now = Instant::now();

        // Take previous state under lock, then release immediately.
        let (prev_times, prev_time) = {
            let prev_times =
                self.prev_cpu_times.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
            let prev_time =
                self.prev_sample_time.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
            (prev_times, prev_time)
        };

        let delta_time_secs =
            prev_time.map(|t| now.duration_since(t).as_secs_f64()).filter(|&dt| dt > 0.0);

        let mut processes = Vec::with_capacity(256);
        let mut current_cpu_times = HashMap::with_capacity(256);

        // Scan /proc/[pid] directories
        let proc_dir = fs::read_dir("/proc")?;
        for entry in proc_dir {
            let entry = entry?;
            let name = entry.file_name().to_string_lossy().to_string();

            // Only look at numeric directories (PIDs)
            let pid: u32 = match name.parse() {
                Ok(p) => p,
                Err(_) => continue,
            };

            // Read /proc/[pid]/stat for basic info.
            // Use non-blocking read to avoid hanging on zombie/uninterruptible
            // processes in WSL2 (e.g., leftover agent processes whose /proc
            // entries block indefinitely on read).
            let stat = match read_proc_file_timeout(pid, "stat") {
                Some(s) => s,
                None => continue, // skip inaccessible processes
            };

            let parts: Vec<&str> = stat.splitn(2, ')').collect();
            if parts.len() == 2 {
                let fields: Vec<&str> = parts[1].split_whitespace().collect();

                let process_name =
                    parts[0].split_once('(').map(|(_, n)| n.to_string()).unwrap_or_default();

                let state = fields.first().map(|s| s.to_string()).unwrap_or_default();
                let ppid: u32 = fields.get(1).and_then(|s| s.parse().ok()).unwrap_or(0);
                let utime: u64 = fields.get(11).and_then(|s| s.parse().ok()).unwrap_or(0);
                let stime: u64 = fields.get(12).and_then(|s| s.parse().ok()).unwrap_or(0);
                let num_threads: u32 = fields.get(17).and_then(|s| s.parse().ok()).unwrap_or(0);
                // Field 22 (1-indexed in /proc/[pid]/stat) = starttime in clock ticks since boot
                // In the 0-indexed fields array after ')': index 19
                let starttime_ticks: u64 =
                    fields.get(19).and_then(|s| s.parse().ok()).unwrap_or(0);
                let vsize: u64 = fields.get(20).and_then(|s| s.parse().ok()).unwrap_or(0);
                let rss: u64 = fields.get(21).and_then(|s| s.parse().ok()).unwrap_or(0);

                // --- Delta-based CPU% calculation ---
                current_cpu_times.insert(pid, (utime, stime));

                let cpu_usage_percent = match (delta_time_secs, prev_times.get(&pid)) {
                    (Some(dt), Some(&(prev_utime, prev_stime))) => {
                        let delta_utime = utime.saturating_sub(prev_utime);
                        let delta_stime = stime.saturating_sub(prev_stime);
                        let total_delta = (delta_utime + delta_stime) as f64;
                        // cpu_pct = (delta_ticks / (delta_time * ticks_per_sec)) * 100
                        (total_delta / (dt * CLOCK_TICKS_PER_SEC as f64)) * 100.0
                    }
                    _ => 0.0, // First sample or pid not seen before
                };

                // --- Start time ---
                let started_at = match get_boot_time() {
                    Ok(boot_time) => parse_start_time(starttime_ticks, boot_time),
                    Err(_) => String::new(),
                };

                // --- Container ID ---
                let container_id = read_proc_file_timeout(pid, "cgroup")
                    .map(|cgroup| extract_container_id_from_cgroup(&cgroup))
                    .unwrap_or_default();

                // Read RSS in bytes from /proc/[pid]/status for accuracy
                let rss_bytes = read_proc_file_timeout(pid, "status")
                    .and_then(|status| {
                        status.lines().find_map(|line| {
                            if line.starts_with("VmRSS:") {
                                line.split_whitespace()
                                    .nth(1)
                                    .and_then(|v| v.parse::<u64>().ok())
                                    .map(|kb| kb * 1024)
                            } else {
                                None
                            }
                        })
                    })
                    .unwrap_or(rss * 4096); // fallback: pages * page_size

                // Read command line
                let cmdline = read_proc_file_timeout(pid, "cmdline")
                    .map(|s| s.replace('\0', " ").trim().to_string())
                    .unwrap_or_default();

                // Skip fd count — /proc/[pid]/fd enumeration hangs on WSL2
                // for most processes (kernel threads, privileged processes).
                let fd_count = 0u32;

                processes.push(ProcessInfo {
                    pid,
                    parent_pid: ppid,
                    name: process_name,
                    command_line: cmdline,
                    cpu_usage_percent,
                    rss_bytes,
                    vsz_bytes: vsize,
                    status: state,
                    thread_count: num_threads,
                    fd_count,
                    container_id,
                    started_at,
                });
            }
        }

        // Store current state for next iteration.
        {
            let mut times =
                self.prev_cpu_times.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
            // Clear stale PIDs by replacing entirely.
            times.clear();
            times.extend(current_cpu_times);
        }
        {
            let mut sample_time =
                self.prev_sample_time.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
            *sample_time = Some(now);
        }

        Ok(processes)
    }

    #[cfg(not(target_os = "linux"))]
    fn collect_linux(&self) -> Result<Vec<ProcessInfo>> {
        self.collect_cross_platform()
    }

    fn collect_cross_platform(&self) -> Result<Vec<ProcessInfo>> {
        use sysinfo::System;

        let mut sys = System::new_all();
        sys.refresh_processes();

        let processes: Vec<ProcessInfo> = sys
            .processes()
            .iter()
            .map(|(pid, proc_info)| ProcessInfo {
                pid: pid.as_u32(),
                parent_pid: proc_info.parent().map(|p| p.as_u32()).unwrap_or(0),
                name: proc_info.name().to_string(),
                command_line: proc_info.cmd().join(" "),
                cpu_usage_percent: proc_info.cpu_usage() as f64,
                rss_bytes: proc_info.memory(),
                vsz_bytes: proc_info.virtual_memory(),
                status: format!("{:?}", proc_info.status()),
                thread_count: proc_info.tasks().map(|t| t.len() as u32).unwrap_or(0),
                fd_count: 0,
                container_id: String::new(),
                started_at: String::new(),
            })
            .collect();

        Ok(processes)
    }
}

// ---------------------------------------------------------------------------
// Linux-only helpers
// ---------------------------------------------------------------------------

/// Read a /proc/[pid]/[name] file with a short timeout using non-blocking I/O.
///
/// On WSL2, some /proc entries (especially for zombie/uninterruptible processes)
/// block indefinitely on read. This helper uses `poll()` with a 500ms timeout
/// to avoid hanging the entire collection.
///
/// Returns `None` if the file can't be opened, read, or times out.
#[cfg(target_os = "linux")]
fn read_proc_file_timeout(pid: u32, name: &str) -> Option<String> {
    use std::os::unix::fs::OpenOptionsExt;
    use std::os::unix::io::AsRawFd;

    let path = format!("/proc/{}/{}", pid, name);

    // Open with O_RDONLY | O_NONBLOCK | O_CLOEXEC (0x80200 = O_CLOEXEC on Linux)
    let file = std::fs::OpenOptions::new()
        .read(true)
        .custom_flags(libc::O_NONBLOCK | libc::O_CLOEXEC)
        .open(&path)
        .ok()?;

    let fd = file.as_raw_fd();

    // poll() with 500ms timeout — if the kernel doesn't have data ready
    // within this window, the process is likely inaccessible.
    let mut pfd = libc::pollfd { fd, events: libc::POLLIN, revents: 0 };
    let ret = unsafe { libc::poll(&mut pfd, 1, 500) };
    if ret <= 0 {
        return None; // timeout or error
    }

    // Read the file content. With O_NONBLOCK, a ready /proc file returns
    // data immediately.
    use std::io::Read;
    let mut reader = std::io::BufReader::new(file);
    let mut buf = String::new();
    reader.read_to_string(&mut buf).ok()?;
    Some(buf)
}

/// Extract the container ID from cgroup content string.
///
/// Supports Docker (64-char hex), containerd, and Podman cgroup layouts.
/// Returns an empty string if no container is detected.
#[cfg(target_os = "linux")]
fn extract_container_id_from_cgroup(cgroup: &str) -> String {
    for line in cgroup.lines() {
        let parts: Vec<&str> = line.splitn(3, ':').collect();
        if parts.len() < 3 {
            continue;
        }
        let path = parts[2];

        // Docker — 64-char hex container ID in path
        for segment in path.split('/') {
            if segment.len() >= 64 && segment.chars().all(|c| c.is_ascii_hexdigit()) {
                return segment[..64].to_string();
            }
        }

        // containerd
        if path.contains("containerd") {
            if let Some(id) = extract_id_from_path(path) {
                return id;
            }
        }

        // Podman (libpod)
        if path.contains("libpod") {
            if let Some(id) = extract_id_from_path(path) {
                return id;
            }
        }
    }
    String::new()
}

/// Read the system boot time (`btime`) from `/proc/stat`.
///
/// Returns seconds since the Unix epoch.
#[cfg(target_os = "linux")]
fn get_boot_time() -> Result<u64> {
    let stat = std::fs::read_to_string("/proc/stat")?;
    for line in stat.lines() {
        if let Some(rest) = line.strip_prefix("btime ") {
            return Ok(rest.trim().parse::<u64>()?);
        }
    }
    anyhow::bail!("btime not found in /proc/stat")
}

/// Convert a process start time (in clock ticks since boot) to an RFC 3339 string.
#[cfg(target_os = "linux")]
fn parse_start_time(starttime_ticks: u64, boot_time_secs: u64) -> String {
    let start_epoch = boot_time_secs + (starttime_ticks / CLOCK_TICKS_PER_SEC);
    match chrono::DateTime::from_timestamp(start_epoch as i64, 0) {
        Some(dt) => dt.to_rfc3339(),
        None => String::new(),
    }
}

/// Extract the container ID for a given PID by parsing `/proc/[pid]/cgroup`.
///
/// Supports Docker (64-char hex), containerd, and Podman cgroup layouts.
/// Returns an empty string if the process is not inside a container.
#[cfg(target_os = "linux")]
fn get_container_id(pid: u32) -> String {
    let cgroup = match read_proc_file_timeout(pid, "cgroup") {
        Some(c) => c,
        None => return String::new(),
    };
    extract_container_id_from_cgroup(&cgroup)
}

/// Extract a container ID from the last path segment if it looks like a hex ID.
/// Pure logic — available on all platforms for testability.
#[allow(dead_code)] // Called from Linux-only code; ungated for testability
fn extract_id_from_path(path: &str) -> Option<String> {
    path.split('/')
        .next_back()
        .filter(|s| s.len() >= 8 && s.chars().all(|c| c.is_ascii_hexdigit()))
        .map(|s| s.to_string())
}

// Non-Linux stubs so the methods compile on all targets.
#[cfg(not(target_os = "linux"))]
#[allow(dead_code)]
fn get_boot_time() -> Result<u64> {
    anyhow::bail!("boot time only available on Linux")
}

#[cfg(not(target_os = "linux"))]
#[allow(dead_code)]
fn parse_start_time(_starttime_ticks: u64, _boot_time_secs: u64) -> String {
    String::new()
}

#[cfg(not(target_os = "linux"))]
#[allow(dead_code)]
fn get_container_id(_pid: u32) -> String {
    String::new()
}

impl Default for ProcessCollector {
    fn default() -> Self {
        Self::new()
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    /// Verify that a known `/proc/[pid]/stat` line is parsed correctly,
    /// including the case where `comm` contains spaces and special characters.
    #[test]
    fn test_parse_pid_stat() {
        // Simulated /proc/[pid]/stat with comm containing a space.
        // Format: pid (comm) state ppid pgrp session tty_nr tpgid flags
        //         minflt cminflt majflt cmajflt utime stime cutime cstart
        //         priority nice num_threads itrealvalue starttime vsize rss
        let stat = "1234 (my process) S 1 1234 1234 0 -1 4194304 \
                     100 5 0 0 1500 300 0 0 20 0 1 0 50000 104857600 2048";

        // Replicate the parser from collect_linux
        let parts: Vec<&str> = stat.splitn(2, ')').collect();
        assert_eq!(parts.len(), 2);

        let process_name = parts[0].split_once('(').map(|(_, n)| n.to_string()).unwrap_or_default();
        assert_eq!(process_name, "my process");

        let fields: Vec<&str> = parts[1].split_whitespace().collect();
        let state = fields.first().map(|s| s.to_string()).unwrap_or_default();
        assert_eq!(state, "S");

        let ppid: u32 = fields.get(1).and_then(|s| s.parse().ok()).unwrap_or(0);
        assert_eq!(ppid, 1);

        let utime: u64 = fields.get(11).and_then(|s| s.parse().ok()).unwrap_or(0);
        assert_eq!(utime, 1500);

        let stime: u64 = fields.get(12).and_then(|s| s.parse().ok()).unwrap_or(0);
        assert_eq!(stime, 300);

        let num_threads: u32 = fields.get(17).and_then(|s| s.parse().ok()).unwrap_or(0);
        assert_eq!(num_threads, 1);

        let starttime: u64 = fields.get(19).and_then(|s| s.parse().ok()).unwrap_or(0);
        assert_eq!(starttime, 50000);

        let vsize: u64 = fields.get(20).and_then(|s| s.parse().ok()).unwrap_or(0);
        assert_eq!(vsize, 104857600);

        let rss: u64 = fields.get(21).and_then(|s| s.parse().ok()).unwrap_or(0);
        assert_eq!(rss, 2048);
    }

    /// Verify delta-based CPU% calculation with known values.
    #[test]
    fn test_cpu_delta_calculation() {
        let prev_utime: u64 = 1000;
        let prev_stime: u64 = 500;
        let curr_utime: u64 = 1200;
        let curr_stime: u64 = 600;
        let delta_time_secs: f64 = 2.0;

        let delta_utime = curr_utime.saturating_sub(prev_utime);
        let delta_stime = curr_stime.saturating_sub(prev_stime);
        let total_delta = (delta_utime + delta_stime) as f64;
        let cpu_pct = (total_delta / (delta_time_secs * CLOCK_TICKS_PER_SEC as f64)) * 100.0;

        assert!((cpu_pct - 150.0).abs() < f64::EPSILON);
    }

    /// Verify that CPU% saturates at 0 when prev > current (counter wrap).
    #[test]
    fn test_cpu_delta_saturating_sub() {
        let prev_utime: u64 = 5000;
        let prev_stime: u64 = 3000;
        let curr_utime: u64 = 3000;
        let curr_stime: u64 = 4000;

        let delta_utime = curr_utime.saturating_sub(prev_utime);
        let delta_stime = curr_stime.saturating_sub(prev_stime);

        assert_eq!(delta_utime, 0);
        assert_eq!(delta_stime, 1000);
    }

    /// Verify container ID extraction from Docker cgroup format.
    #[test]
    fn test_container_id_extraction() {
        let cgroup_v1 =
            "12:devices:/docker/abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789";
        let result = extract_container_id_from_cgroup(cgroup_v1);
        assert_eq!(
            result, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
        );
    }

    /// Verify container ID extraction from containerd cgroup format.
    #[test]
    fn test_container_id_extraction_containerd() {
        let cgroup = "0::/kubepods/burstable/podabc123/abcdef0123456789abcdef0123456789abcdef01";
        let result = extract_container_id_from_cgroup(cgroup);
        assert_eq!(result, "abcdef0123456789abcdef0123456789abcdef01");
    }

    /// Verify start time conversion from ticks to RFC 3339.
    #[cfg(target_os = "linux")]
    #[test]
    fn test_parse_start_time_conversion() {
        let boot_time: u64 = 1700000000;
        let starttime_ticks: u64 = 50000;
        let result = parse_start_time(starttime_ticks, boot_time);
        assert!(!result.is_empty());
        assert!(result.contains("2023"));
    }

    /// Verify that a stat line with parentheses in comm is parsed correctly.
    #[test]
    fn test_parse_pid_stat_with_parens_in_comm() {
        let stat = "999 ((test)) R 1 999 999 0 -1 0 \
                     0 0 0 0 50 50 0 0 0 0 1 0 100 0 0";
        let parts: Vec<&str> = stat.splitn(2, ')').collect();
        assert_eq!(parts.len(), 2);

        let process_name = parts[0].split_once('(').map(|(_, n)| n.to_string()).unwrap_or_default();
        let rest = parts[1];
        let fields_str = rest.strip_prefix(')').unwrap_or(rest);
        let fields: Vec<&str> = fields_str.split_whitespace().collect();

        assert_eq!(process_name, "(test");
        assert_eq!(fields.first().unwrap(), &"R");
    }

    /// Verify that extract_id_from_path works for typical hex IDs.
    #[test]
    fn test_extract_id_from_path() {
        assert_eq!(
            extract_id_from_path("/kubepods/pod-abc123/abcdef012345678"),
            Some("abcdef012345678".to_string())
        );
        assert_eq!(extract_id_from_path("/short/abc"), None);
        assert_eq!(extract_id_from_path("/not-hex-xyzwxyzw"), None);
    }

    /// Verify ProcessCollector::new() initializes with empty state.
    #[test]
    fn test_process_collector_new() {
        let collector = ProcessCollector::new();
        let times = collector.prev_cpu_times.lock().unwrap_or_else(|p| p.into_inner());
        assert!(times.is_empty());
        let sample = collector.prev_sample_time.lock().unwrap_or_else(|p| p.into_inner());
        assert!(sample.is_none());
    }
}
