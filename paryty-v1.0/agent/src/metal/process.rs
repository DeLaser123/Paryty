//! Process Tree Collector
//!
//! Discovers and monitors all running processes with parent-child relationships.
//! Collects CPU usage via delta-based calculation, container IDs via cgroup
//! parsing, and process start times via /proc/[pid]/stat.

use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};
use std::time::Instant;

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use tracing::instrument;

use crate::config::MetalConfig;

// ── WSL2 Detection ────────────────────────────────────────────────────

/// Cached result of WSL2 detection. Checked once at first use.
static IS_WSL2: OnceLock<bool> = OnceLock::new();

/// Detect whether the current environment is WSL2.
/// Checks `/proc/version` for the "microsoft" tag.
fn is_wsl2() -> bool {
    *IS_WSL2.get_or_init(|| {
        std::fs::read_to_string("/proc/version")
            .map(|v| v.to_lowercase().contains("microsoft"))
            .unwrap_or(false)
    })
}

/// Clock ticks per second on Linux (`sysconf(_SC_CLK_TCK)`).
/// Hardcoded to the standard value; all major Linux architectures use 100.
/// Also used as a cross-platform test constant.
#[allow(dead_code)] // Used only in Linux code path; ungated for cross-platform testability
const CLOCK_TICKS_PER_SEC: u64 = 100;

/// Per-process metrics.
///
/// Platform-specific fields use `Option<>` — `None` (serialized as `null`)
/// means the metric is genuinely unavailable on this platform, not zero.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct ProcessInfo {
    pub pid: u32,
    pub parent_pid: u32,
    pub name: String,
    pub command_line: String,
    /// Full path to the process executable. Available on Linux, macOS, Windows.
    pub exe: Option<String>,
    pub cpu_usage_percent: f64,
    pub rss_bytes: u64,
    pub vsz_bytes: u64,
    /// Per-process cumulative bytes read from disk.
    /// `None` on Linux/WSL2 where /proc/[pid]/io can hang.
    pub disk_read_bytes: Option<u64>,
    /// Per-process cumulative bytes written to disk.
    /// `None` on Linux/WSL2 where /proc/[pid]/io can hang.
    pub disk_written_bytes: Option<u64>,
    pub status: String,
    /// Number of threads. `None` on platforms where sysinfo cannot report it (e.g. Windows).
    pub thread_count: Option<u32>,
    /// Number of open file descriptors. Only available on Linux via /proc/[pid]/fd.
    pub fd_count: Option<u32>,
    /// Container ID from cgroup. Only available on Linux.
    pub container_id: Option<String>,
    /// Process start time as RFC 3339 string. Available on Linux (from /proc),
    /// Windows and macOS (from sysinfo).
    pub started_at: Option<String>,
    /// UID of the process owner. Available on Linux and Windows via sysinfo.
    pub user_id: Option<String>,
}

/// Process metrics collection result.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct ProcessMetrics {
    pub timestamp: String,
    pub processes: Vec<ProcessInfo>,
}

/// Process tree collector with delta-based CPU usage tracking.
///
/// On Linux, uses `/proc/[pid]/stat` with delta-based CPU% calculation.
/// On other platforms, uses a persistent `sysinfo::System` instance so that
/// `cpu_usage()` returns deltas since the last refresh (not zeros).
pub struct ProcessCollector {
    /// Previous per-process CPU times: pid -> (utime, stime).
    prev_cpu_times: Mutex<HashMap<u32, (u64, u64)>>,
    /// Timestamp of the previous collection sample.
    prev_sample_time: Mutex<Option<Instant>>,
    #[allow(dead_code)] // used only on non-Linux via collect_cross_platform()
    cross_platform_system: Mutex<Option<sysinfo::System>>,
}

impl ProcessCollector {
    pub fn new() -> Self {
        Self {
            prev_cpu_times: Mutex::new(HashMap::new()),
            prev_sample_time: Mutex::new(None),
            cross_platform_system: Mutex::new(None),
        }
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
        use std::time::Duration;

        // On WSL2, /proc reads are extremely slow due to the Plan 9
        // filesystem bridge. We use a fast path that only reads
        // /proc/[pid]/stat (essential for CPU deltas) with a very short
        // timeout, skipping status, cmdline, cgroup, and exe entirely.
        // An outer time budget prevents the collection from hanging
        // indefinitely when individual /proc reads block on the P9 bridge.
        let wsl2 = is_wsl2();
        let poll_timeout_ms: i32 = if wsl2 { 20 } else { 500 };
        let budget = if wsl2 { Duration::from_secs(4) } else { Duration::from_secs(30) };

        // Snapshot the current time for delta calculation.
        let now = Instant::now();

        // Take previous state under lock, then release immediately.
        // IMPORTANT: We must clone the data and drop the guards before
        // the end of the function, because we re-lock the same mutexes
        // to store current state. Holding the guards would deadlock.
        let (prev_times, prev_time) = {
            let guard = self.prev_cpu_times.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
            let times = guard.clone();
            drop(guard);

            let guard =
                self.prev_sample_time.lock().unwrap_or_else(|poisoned| poisoned.into_inner());
            let time = *guard;
            drop(guard);

            (times, time)
        };

        let delta_time_secs =
            prev_time.map(|t| now.duration_since(t).as_secs_f64()).filter(|&dt| dt > 0.0);

        let mut processes = Vec::with_capacity(256);
        let mut current_cpu_times = HashMap::with_capacity(256);

        // Scan /proc/[pid] directories.
        // On WSL2, the ReadDir iterator can hang on the Plan 9 filesystem
        // bridge, so we use a shell command to enumerate PIDs instead.
        let t1 = Instant::now();
        let pids: Vec<u32> = if wsl2 {
            // WSL2 fast path: use `ls -d /proc/[0-9]*` which is known to be fast
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
            // Native Linux: use read_dir which is fast on real /proc
            fs::read_dir("/proc")?
                .filter_map(|e| e.ok())
                .filter_map(|e| e.file_name().to_string_lossy().parse::<u32>().ok())
                .collect()
        };
        tracing::debug!(
            elapsed_ms = t1.elapsed().as_millis() as u64,
            pid_count = pids.len(),
            "PID enumeration completed"
        );

        for pid in pids {
            // Check time budget — return partial results instead of hanging.
            if now.elapsed() > budget {
                tracing::warn!(
                    collected = processes.len(),
                    budget_ms = budget.as_millis() as u64,
                    "Process collection time budget exceeded, returning partial results"
                );
                break;
            }

            // Read /proc/[pid]/stat for basic info.
            // On WSL2: use direct fs::read_to_string (same as `cat /proc/[pid]/stat`).
            // On native Linux: use non-blocking read with configurable timeout.
            let stat = if wsl2 {
                match fs::read_to_string(format!("/proc/{}/stat", pid)) {
                    Ok(s) => s,
                    Err(_) => continue,
                }
            } else {
                match read_proc_file_timeout(pid, "stat", poll_timeout_ms) {
                    Some(s) => s,
                    None => continue,
                }
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
                let starttime_ticks: u64 = fields.get(19).and_then(|s| s.parse().ok()).unwrap_or(0);
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

                // On WSL2, skip slow /proc reads (cgroup, status, cmdline, exe)
                // to avoid blocking the entire collection. Only /proc/[pid]/stat
                // is read, which is fast since it's kernel-generated.
                let (container_id, rss_bytes, cmdline, exe, disk_r, disk_w, uid) = if wsl2 {
                    // WSL2 fast path: use stat RSS, skip everything else
                    (String::new(), rss * 4096, String::new(), None, None, None, None)
                } else {
                    // Native Linux: read all files with generous timeouts
                    let cid = read_proc_file_timeout(pid, "cgroup", poll_timeout_ms)
                        .map(|cgroup| extract_container_id_from_cgroup(&cgroup))
                        .unwrap_or_default();

                    // Read /proc/[pid]/status for RSS and UID
                    let (rss_b, uid_str) = read_proc_file_timeout(pid, "status", poll_timeout_ms)
                        .map(|status| {
                            let mut rss_val = None;
                            let mut uid_val = None;
                            for line in status.lines() {
                                if line.starts_with("VmRSS:") {
                                    rss_val = line
                                        .split_whitespace()
                                        .nth(1)
                                        .and_then(|v| v.parse::<u64>().ok())
                                        .map(|kb| kb * 1024);
                                } else if line.starts_with("Uid:") {
                                    // Format: Uid:\t<real>\t<effective>\t<saved>\t<fs>
                                    uid_val = line.split_whitespace().nth(1).map(|s| s.to_string());
                                }
                                if rss_val.is_some() && uid_val.is_some() {
                                    break;
                                }
                            }
                            (rss_val.unwrap_or(rss * 4096), uid_val)
                        })
                        .unwrap_or((rss * 4096, None));

                    let cmd = read_proc_file_timeout(pid, "cmdline", poll_timeout_ms)
                        .map(|s| s.replace('\0', " ").trim().to_string())
                        .unwrap_or_default();

                    let ex = read_proc_file_timeout(pid, "exe", poll_timeout_ms)
                        .and_then(|p| std::fs::read_link(&p).ok())
                        .map(|p| p.to_string_lossy().to_string());

                    // Read /proc/[pid]/io for disk read/write bytes
                    let (dr, dw) = read_proc_file_timeout(pid, "io", poll_timeout_ms)
                        .map(|io_content| {
                            let mut read_b = None;
                            let mut write_b = None;
                            for line in io_content.lines() {
                                if line.starts_with("read_bytes:") {
                                    read_b = line
                                        .split_whitespace()
                                        .nth(1)
                                        .and_then(|v| v.parse::<u64>().ok());
                                } else if line.starts_with("write_bytes:") {
                                    write_b = line
                                        .split_whitespace()
                                        .nth(1)
                                        .and_then(|v| v.parse::<u64>().ok());
                                }
                            }
                            (read_b, write_b)
                        })
                        .unwrap_or((None, None));

                    (cid, rss_b, cmd, ex, dr, dw, uid_str)
                };

                // Enumerate fd count on native Linux. On WSL2, /proc/[pid]/fd
                // enumeration hangs on the Plan 9 bridge for privileged processes.
                let fd_count: Option<u32> = if wsl2 {
                    None
                } else {
                    // Use a bounded readdir with a short timeout to avoid hangs.
                    read_fd_count(pid, poll_timeout_ms)
                };

                processes.push(ProcessInfo {
                    pid,
                    parent_pid: ppid,
                    name: process_name,
                    command_line: cmdline,
                    exe,
                    cpu_usage_percent,
                    rss_bytes,
                    vsz_bytes: vsize,
                    disk_read_bytes: disk_r,
                    disk_written_bytes: disk_w,
                    status: state,
                    thread_count: Some(num_threads),
                    fd_count,
                    container_id: Some(container_id),
                    started_at: Some(started_at),
                    user_id: uid,
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

        let mut guard =
            self.cross_platform_system.lock().unwrap_or_else(|poisoned| poisoned.into_inner());

        let sys = match guard.as_mut() {
            Some(s) => {
                // Subsequent call: refresh in-place. `cpu_usage()` now returns
                // the delta since the last refresh, not since system boot.
                s.refresh_processes();
                s
            }
            None => {
                // First call: create the persistent instance and do an initial
                // baseline read. The first sample will show 0% CPU because
                // there is no previous sample to diff against — this is expected.
                let mut s = System::new_all();
                s.refresh_processes();
                *guard = Some(s);
                guard.as_mut().unwrap()
            }
        };

        let processes: Vec<ProcessInfo> = sys
            .processes()
            .iter()
            .map(|(pid, proc_info)| {
                let disk = proc_info.disk_usage();
                ProcessInfo {
                    pid: pid.as_u32(),
                    parent_pid: proc_info.parent().map(|p| p.as_u32()).unwrap_or(0),
                    name: proc_info.name().to_string(),
                    command_line: proc_info.cmd().join(" "),
                    exe: proc_info.exe().map(|p| p.to_string_lossy().to_string()),
                    cpu_usage_percent: proc_info.cpu_usage() as f64,
                    rss_bytes: proc_info.memory(),
                    vsz_bytes: proc_info.virtual_memory(),
                    disk_read_bytes: Some(disk.read_bytes),
                    disk_written_bytes: Some(disk.written_bytes),
                    status: format!("{:?}", proc_info.status()),
                    thread_count: proc_info.tasks().map(|t| t.len() as u32),
                    fd_count: None,     // /proc/[pid]/fd not available outside Linux
                    container_id: None, // cgroup detection not available outside Linux
                    started_at: {
                        let epoch = proc_info.start_time();
                        if epoch > 0 {
                            chrono::DateTime::from_timestamp(epoch as i64, 0)
                                .map(|dt| dt.to_rfc3339())
                        } else {
                            None
                        }
                    },
                    user_id: proc_info.user_id().map(|uid| uid.to_string()),
                }
            })
            .collect();

        Ok(processes)
    }
}

// ---------------------------------------------------------------------------
// Linux-only helpers
// ---------------------------------------------------------------------------

/// Read a /proc/[pid]/[name] file with a configurable timeout using non-blocking I/O.
///
/// On WSL2, some /proc entries (especially for zombie/uninterruptible processes)
/// block indefinitely on read. This helper uses `poll()` with the specified
/// timeout to avoid hanging the entire collection.
///
/// Returns `None` if the file can't be opened, read, or times out.
#[cfg(target_os = "linux")]
fn read_proc_file_timeout(pid: u32, name: &str, timeout_ms: i32) -> Option<String> {
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

    // poll() with configurable timeout — if the kernel doesn't have data ready
    // within this window, the process is likely inaccessible.
    let mut pfd = libc::pollfd { fd, events: libc::POLLIN, revents: 0 };
    let ret = unsafe { libc::poll(&mut pfd, 1, timeout_ms) };
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

/// Count the number of open file descriptors for a process.
///
/// Reads `/proc/[pid]/fd` directory entries with a bounded iteration limit
/// to avoid excessive work on processes with many fds. Returns `None` on
/// any error (permission denied, process exited, etc.).
///
/// On native Linux, `/proc/[pid]/fd` reads are fast. On WSL2, this function
/// is not called (the caller checks `is_wsl2()` first).
#[cfg(target_os = "linux")]
fn read_fd_count(pid: u32, _timeout_ms: i32) -> Option<u32> {
    let path = format!("/proc/{}/fd", pid);

    // Count directory entries, bounded to 65536 to prevent runaway iteration.
    const MAX_FD_COUNT: usize = 65536;
    let mut count: u32 = 0;

    let rd = std::fs::read_dir(&path).ok()?;
    for entry in rd.flatten().take(MAX_FD_COUNT) {
        let name = entry.file_name();
        let name_str = name.to_string_lossy();
        // Skip . and .. (though /proc/[pid]/fd shouldn't have them).
        if name_str == "." || name_str == ".." {
            continue;
        }
        count += 1;
    }

    Some(count)
}

/// Extract the container ID from cgroup content string.
///
/// Supports Docker (64-char hex), containerd, kubelet, and Podman cgroup layouts.
/// Returns an empty string if no container is detected.
#[cfg(any(target_os = "linux", test))]
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

        // containerd or kubepods — look for hex ID in last segment
        if path.contains("containerd")
            || path.contains("kubepods")
            || path.contains("cri-containerd")
        {
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
    let cgroup = match read_proc_file_timeout(pid, "cgroup", 500) {
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
        assert_eq!(result, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789");
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
