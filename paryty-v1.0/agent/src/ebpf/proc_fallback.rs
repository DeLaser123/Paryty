#![allow(dead_code)]

//! /proc-based network observer — fallback when eBPF is unavailable.
//!
//! Polls `/proc/net/tcp` and `/proc/net/tcp6` to detect TCP connections via
//! delta comparison between snapshots. Resolves PIDs by scanning
//! `/proc/[pid]/fd` for matching socket inodes.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use anyhow::{Context, Result};
use tracing::{debug, warn};

use super::NetworkEvent;
use crate::communication::Client;
use crate::config::EbpfConfig;

/// TTL for the PID ↔ inode cache. After this duration the entry is re-resolved.
const PID_CACHE_TTL: Duration = Duration::from_secs(30);

/// Polling interval for `/proc` snapshots.
const POLL_INTERVAL: Duration = Duration::from_secs(5);

// ── Connection key / state ──────────────────────────────────────────────────

#[derive(Debug, Clone, Hash, PartialEq, Eq)]
struct ConnectionKey {
    src_ip: u32,
    src_port: u16,
    dst_ip: u32,
    dst_port: u16,
}

#[derive(Debug, Clone)]
struct ConnectionState {
    state: String,
    pid: u32,
    process_name: String,
    first_seen: Instant,
}

/// Cached PID lookup result.
#[derive(Debug, Clone)]
struct PidCacheEntry {
    pid: u32,
    inserted_at: Instant,
}

// ── TCP state code mapping ──────────────────────────────────────────────────

fn tcp_state_from_hex(hex: &str) -> Option<&'static str> {
    match hex {
        "01" => Some("ESTABLISHED"),
        "02" => Some("SYN_SENT"),
        "03" => Some("SYN_RECV"),
        "04" => Some("FIN_WAIT1"),
        "05" => Some("FIN_WAIT2"),
        "06" => Some("TIME_WAIT"),
        "07" => Some("CLOSE_WAIT"),
        "08" => Some("LAST_ACK"),
        "09" => Some("CLOSING"),
        "0A" => Some("LISTEN"),
        _ => None,
    }
}

// ── ProcObserver ────────────────────────────────────────────────────────────

/// /proc-based network observer (fallback when eBPF is unavailable).
pub struct ProcObserver {
    prev_connections: Mutex<HashMap<ConnectionKey, ConnectionState>>,
    /// inode → cached PID (avoids scanning /proc/[pid]/fd on every poll).
    pid_cache: Mutex<HashMap<u64, PidCacheEntry>>,
    config: EbpfConfig,
}

impl ProcObserver {
    pub fn new(config: EbpfConfig) -> Self {
        Self {
            prev_connections: Mutex::new(HashMap::with_capacity(256)),
            pid_cache: Mutex::new(HashMap::with_capacity(256)),
            config,
        }
    }

    /// Collect one snapshot of `/proc/net/tcp` + `/proc/net/tcp6`, diff against
    /// the previous snapshot, and return [`NetworkEvent`]s for new / closed
    /// connections.
    pub fn collect(&self) -> Result<Vec<NetworkEvent>> {
        let mut current: HashMap<ConnectionKey, ConnectionState> = HashMap::with_capacity(256);

        // Parse IPv4 and IPv6 tables.
        Self::parse_proc_tcp("/proc/net/tcp", &self.config, &self.pid_cache, &mut current)
            .context("failed to parse /proc/net/tcp")?;
        Self::parse_proc_tcp("/proc/net/tcp6", &self.config, &self.pid_cache, &mut current)
            .context("failed to parse /proc/net/tcp6")?;

        // Swap with previous and diff.
        let mut events = Vec::new();
        let mut prev = {
            let mut guard = self.prev_connections.lock().expect("prev_connections lock poisoned");
            std::mem::replace(&mut *guard, current)
        };

        // Expire stale PID cache entries.
        {
            let mut cache = self.pid_cache.lock().expect("pid_cache lock poisoned");
            cache.retain(|_, entry| entry.inserted_at.elapsed() < PID_CACHE_TTL);
        }

        // New connections: in current (now in prev_connections) but not in old prev.
        {
            let guard = self.prev_connections.lock().expect("prev_connections lock poisoned");
            for (key, state) in guard.iter() {
                if !prev.contains_key(key) {
                    if state.state == "LISTEN" {
                        continue;
                    }
                    events.push(NetworkEvent::TcpConnection {
                        source_ip: format_ip(key.src_ip),
                        source_port: key.src_port,
                        destination_ip: format_ip(key.dst_ip),
                        destination_port: key.dst_port,
                        state: state.state.clone(),
                        pid: state.pid,
                        process_name: state.process_name.clone(),
                    });
                }
            }
        }

        // Closed connections: in prev but not in current (now in prev_connections).
        {
            let guard = self.prev_connections.lock().expect("prev_connections lock poisoned");
            for (key, state) in prev.drain() {
                if !guard.contains_key(&key) {
                    if state.state == "LISTEN" {
                        continue;
                    }
                    events.push(NetworkEvent::TcpConnection {
                        source_ip: format_ip(key.src_ip),
                        source_port: key.src_port,
                        destination_ip: format_ip(key.dst_ip),
                        destination_port: key.dst_port,
                        state: "CLOSED".to_string(),
                        pid: state.pid,
                        process_name: state.process_name,
                    });
                }
            }
        }

        Ok(events)
    }

    // ── /proc/net/tcp parser ────────────────────────────────────────────────

    /// Parse a single `/proc/net/tcp` (or tcp6) file and insert entries into
    /// `out`.  LISTEN-state and excluded ports / IPs are skipped.
    fn parse_proc_tcp(
        path: &str,
        config: &EbpfConfig,
        pid_cache: &Mutex<HashMap<u64, PidCacheEntry>>,
        out: &mut HashMap<ConnectionKey, ConnectionState>,
    ) -> Result<()> {
        let content =
            std::fs::read_to_string(path).with_context(|| format!("failed to read {}", path))?;

        for (line_idx, line) in content.lines().enumerate() {
            // Skip header line.
            if line_idx == 0 {
                continue;
            }

            let fields: Vec<&str> = line.split_whitespace().collect();
            if fields.len() < 10 {
                debug!(path, line = line_idx, "skipping malformed /proc line");
                continue;
            }

            let local_addr = fields[1];
            let remote_addr = fields[2];
            let state_hex = fields[3];
            let inode_str = fields[9];

            // Parse TCP state.
            let state = match tcp_state_from_hex(state_hex) {
                Some(s) => s,
                None => {
                    debug!(path, line = line_idx, state_hex, "unknown TCP state");
                    continue;
                }
            };

            // Skip LISTEN entries — we only care about active connections.
            if state == "LISTEN" {
                continue;
            }

            // Parse addresses.
            let (src_ip, src_port) = match parse_address(local_addr) {
                Ok(v) => v,
                Err(e) => {
                    debug!(path, line = line_idx, error = %e, "bad local_address");
                    continue;
                }
            };
            let (dst_ip, dst_port) = match parse_address(remote_addr) {
                Ok(v) => v,
                Err(e) => {
                    debug!(path, line = line_idx, error = %e, "bad rem_address");
                    continue;
                }
            };

            // Apply exclude_ports filter.
            if config.exclude_ports.contains(&src_port) || config.exclude_ports.contains(&dst_port)
            {
                continue;
            }

            // Apply exclude_ips filter (compare formatted strings).
            let src_ip_str = format_ip(src_ip);
            let dst_ip_str = format_ip(dst_ip);
            if config.exclude_ips.iter().any(|ip| ip == &src_ip_str || ip == &dst_ip_str) {
                continue;
            }

            // Resolve PID from inode.
            let inode: u64 = inode_str.parse().unwrap_or(0);
            let pid = find_pid_for_inode(inode, pid_cache);
            let process_name = if pid > 0 { get_process_name(pid) } else { String::new() };

            let key = ConnectionKey { src_ip, src_port, dst_ip, dst_port };

            out.insert(
                key,
                ConnectionState {
                    state: state.to_string(),
                    pid,
                    process_name,
                    first_seen: Instant::now(),
                },
            );
        }

        Ok(())
    }
}

// ── Address parsing ─────────────────────────────────────────────────────────

/// Parse a hex `ip:port` pair as found in `/proc/net/tcp`.
///
/// The kernel prints `__be32` values via `%08X`, which reads the network-byte-
/// order memory as a **native u32**.  On little-endian x86, 127.0.0.1 is stored
/// in memory as bytes `[7F, 00, 00, 01]`; reading as LE u32 gives `0x0100007F`,
/// and `%08X` prints `0100007F`.  So the parsed u32 is in native byte order.
///
/// We later format it with [`format_ip`] using `to_ne_bytes()` to extract
/// octets in the correct order for the host.
///
/// IPv4 example: `0100007F:0050` → `(0x0100007F, 80)` → `"127.0.0.1"`
fn parse_address(s: &str) -> Result<(u32, u16)> {
    let (ip_hex, port_hex) =
        s.split_once(':').with_context(|| format!("address missing ':': {}", s))?;

    let port =
        u16::from_str_radix(port_hex, 16).with_context(|| format!("bad port hex: {}", port_hex))?;

    if ip_hex.len() == 8 {
        // IPv4 — native-byte-order u32 in hex (printed by kernel %08X).
        let ip =
            u32::from_str_radix(ip_hex, 16).with_context(|| format!("bad IPv4 hex: {}", ip_hex))?;
        Ok((ip, port))
    } else if ip_hex.len() == 32 {
        // IPv6 — 32 hex chars = four native-byte-order u32 fields.
        // Each 8-char segment represents a u32 printed via `%08X`.
        //
        // We store the last u32 segment as our u32 key (best-effort mapping
        // to a single address).  For full IPv6 fidelity the key type would
        // need to be `[u8; 16]`, but the spec requests a `u32`.
        let seg3 = u32::from_str_radix(&ip_hex[24..32], 16)
            .with_context(|| format!("bad IPv6 hex segment: {}", &ip_hex[24..32]))?;
        Ok((seg3, port))
    } else {
        anyhow::bail!("unexpected IP hex length {}: {}", ip_hex.len(), s);
    }
}

/// Format a u32 IP (in native byte order, as parsed from `/proc/net/tcp`) as
/// dotted-decimal notation.
///
/// The kernel's `%08X` prints a `__be32` as a native u32.  On little-endian
/// hosts, `to_ne_bytes()` recovers the original IP octets in the correct order.
///
/// Example (LE): `0x0100007F` → bytes `[01, 00, 00, 7F]` → `"127.0.0.1"`.
fn format_ip(ip: u32) -> String {
    let b = ip.to_ne_bytes();
    format!("{}.{}.{}.{}", b[0], b[1], b[2], b[3])
}

// ── PID resolution via /proc ────────────────────────────────────────────────

/// Scan `/proc/[pid]/fd` for a symlink target matching `socket:[<inode>]`.
///
/// Uses a cache with [`PID_CACHE_TTL`] to avoid scanning every poll cycle.
/// Returns 0 when no matching process is found.
fn find_pid_for_inode(inode: u64, cache: &Mutex<HashMap<u64, PidCacheEntry>>) -> u32 {
    if inode == 0 {
        return 0;
    }

    // Check cache first.
    {
        let guard = cache.lock().expect("pid_cache lock poisoned");
        if let Some(entry) = guard.get(&inode) {
            if entry.inserted_at.elapsed() < PID_CACHE_TTL {
                return entry.pid;
            }
            // Entry expired — fall through to re-scan.
        }
    }

    let target = format!("socket:[{}]", inode);
    let pid = scan_proc_fds_for_inode(&target);

    // Insert into cache (even if not found — avoids re-scanning for 30 s).
    {
        let mut guard = cache.lock().expect("pid_cache lock poisoned");
        guard.insert(inode, PidCacheEntry { pid, inserted_at: Instant::now() });
    }

    pid
}

/// Iterate over `/proc/[pid]/fd` for every numeric PID directory and return
/// the first PID whose fd symlinks contain `socket:[<inode>]`.
fn scan_proc_fds_for_inode(target: &str) -> u32 {
    let proc_dir = match std::fs::read_dir("/proc") {
        Ok(d) => d,
        Err(e) => {
            warn!("cannot read /proc: {}", e);
            return 0;
        }
    };

    for entry in proc_dir {
        let entry = match entry {
            Ok(e) => e,
            Err(_) => continue,
        };

        let name = entry.file_name();
        let name_str = name.to_string_lossy();

        // Only numeric directory names (PIDs).
        let pid: u32 = match name_str.parse() {
            Ok(p) => p,
            Err(_) => continue,
        };

        let fd_dir = format!("/proc/{}/fd", pid);
        let fds = match std::fs::read_dir(&fd_dir) {
            Ok(d) => d,
            Err(_) => continue, // process may have exited or permission denied
        };

        for fd_entry in fds {
            let fd_entry = match fd_entry {
                Ok(e) => e,
                Err(_) => continue,
            };

            match std::fs::read_link(fd_entry.path()) {
                Ok(link) => {
                    if link.to_string_lossy() == target {
                        return pid;
                    }
                }
                Err(_) => continue, // fd may have closed
            }
        }
    }

    0
}

/// Read `/proc/[pid]/comm` and return the process name, or an empty string
/// if the file cannot be read (process exited, permission denied, etc.).
fn get_process_name(pid: u32) -> String {
    let path = format!("/proc/{}/comm", pid);
    match std::fs::read_to_string(&path) {
        Ok(s) => s.trim().to_string(),
        Err(_) => String::new(),
    }
}

// ── Async entry point ───────────────────────────────────────────────────────

#[cfg(target_os = "linux")]
pub async fn run(config: EbpfConfig, client: Client) -> Result<()> {
    let observer = ProcObserver::new(config);

    loop {
        match observer.collect() {
            Ok(events) => {
                if events.is_empty() {
                    debug!("/proc poll: no connection changes detected");
                } else {
                    debug!("/proc poll: {} connection change(s) detected", events.len());
                    for event in &events {
                        let data = serde_json::to_vec(&event)
                            .context("failed to serialize NetworkEvent")?;
                        if let Err(e) = client.send_network_events(&data).await {
                            warn!("failed to send /proc event: {}", e);
                        }
                    }
                }
            }
            Err(e) => {
                tracing::error!("failed to collect /proc events: {:#}", e);
            }
        }

        tokio::time::sleep(POLL_INTERVAL).await;
    }
}

/// Non-Linux stub: logs a warning and sleeps indefinitely.
#[cfg(not(target_os = "linux"))]
pub async fn run(_config: EbpfConfig, _client: Client) -> Result<()> {
    warn!("/proc fallback observer is only supported on Linux. Running in stub mode.");
    loop {
        tokio::time::sleep(Duration::from_secs(60)).await;
    }
}

// ── Tests ───────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tcp_state_from_hex() {
        assert_eq!(tcp_state_from_hex("01"), Some("ESTABLISHED"));
        assert_eq!(tcp_state_from_hex("02"), Some("SYN_SENT"));
        assert_eq!(tcp_state_from_hex("03"), Some("SYN_RECV"));
        assert_eq!(tcp_state_from_hex("04"), Some("FIN_WAIT1"));
        assert_eq!(tcp_state_from_hex("05"), Some("FIN_WAIT2"));
        assert_eq!(tcp_state_from_hex("06"), Some("TIME_WAIT"));
        assert_eq!(tcp_state_from_hex("07"), Some("CLOSE_WAIT"));
        assert_eq!(tcp_state_from_hex("08"), Some("LAST_ACK"));
        assert_eq!(tcp_state_from_hex("09"), Some("CLOSING"));
        assert_eq!(tcp_state_from_hex("0A"), Some("LISTEN"));
        assert_eq!(tcp_state_from_hex("0B"), None);
        assert_eq!(tcp_state_from_hex("ZZ"), None);
    }

    #[test]
    fn test_parse_address_ipv4() {
        // /proc/net/tcp hex "0100007F" = 0x0100007F (native LE u32 for 127.0.0.1)
        let (ip, port) = parse_address("0100007F:0050").expect("should parse");
        assert_eq!(port, 0x0050);
        assert_eq!(ip, 0x0100007F);
    }

    #[test]
    fn test_parse_address_ipv4_loopback() {
        // "0000007F" = 0x0000007F (native LE u32 for 127.0.0.0)
        let (ip, port) = parse_address("0000007F:1F90").expect("should parse");
        assert_eq!(port, 8080);
        assert_eq!(ip, 0x0000007F);
    }

    #[test]
    fn test_parse_address_ipv6() {
        // 32-char hex string: last 8 chars are the segment we store.
        let addr = "00000000000000000000000001000000:1F90";
        let (ip, port) = parse_address(addr).expect("should parse IPv6");
        assert_eq!(port, 8080);
        // Last segment: "01000000" = 0x01000000
        assert_eq!(ip, 0x01000000);
    }

    #[test]
    fn test_parse_address_bad_format() {
        assert!(parse_address("no-colon").is_err());
        assert!(parse_address("ZZZZZZZZ:0050").is_err());
    }

    #[test]
    fn test_format_ip() {
        // On LE: 0x0100007F → bytes [01, 00, 00, 7F] → "127.0.0.1"
        assert_eq!(format_ip(0x0100007F), "127.0.0.1");
        // On LE: 0x00000000 → "0.0.0.0"
        assert_eq!(format_ip(0x00000000), "0.0.0.0");
    }

    #[test]
    fn test_parse_and_format_roundtrip() {
        // Parse 127.0.0.1:80 from /proc format, verify format_ip produces correct result.
        let (ip, port) = parse_address("0100007F:0050").expect("should parse");
        assert_eq!(format_ip(ip), "127.0.0.1");
        assert_eq!(port, 80);

        // Parse 10.0.0.2:443
        let (ip, port) = parse_address("0200000A:01BB").expect("should parse");
        assert_eq!(format_ip(ip), "10.0.0.2");
        assert_eq!(port, 443);

        // Parse 192.168.1.100:22
        let (ip, port) = parse_address("6401A8C0:0016").expect("should parse");
        assert_eq!(format_ip(ip), "192.168.1.100");
        assert_eq!(port, 22);
    }

    #[test]
    fn test_connection_key_equality() {
        let a = ConnectionKey { src_ip: 1, src_port: 80, dst_ip: 2, dst_port: 443 };
        let b = ConnectionKey { src_ip: 1, src_port: 80, dst_ip: 2, dst_port: 443 };
        assert_eq!(a, b);
    }

    #[test]
    fn test_connection_key_inequality() {
        let a = ConnectionKey { src_ip: 1, src_port: 80, dst_ip: 2, dst_port: 443 };
        let b = ConnectionKey { src_ip: 1, src_port: 80, dst_ip: 2, dst_port: 8443 };
        assert_ne!(a, b);
    }

    #[test]
    fn test_proc_observer_new() {
        let config = EbpfConfig {
            enabled: true,
            tcp_connections: true,
            dns_resolution: false,
            http_inspection: false,
            db_inspection: false,
            exclude_ports: vec![22],
            exclude_ips: vec!["127.0.0.1".to_string()],
            ring_buffer_size_kb: 256,
            poll_interval_ms: 100,
            fallback_to_proc: true,
        };
        let observer = ProcObserver::new(config);
        assert_eq!(observer.config.exclude_ports, vec![22]);
    }

    /// Simulate a full parse from a synthetic /proc/net/tcp string.
    #[test]
    fn test_parse_proc_tcp_synthetic() {
        // Addresses in /proc/net/tcp format (native LE u32 hex):
        //  127.0.0.1:80   → local "0100007F:0050",  state 0A (LISTEN) → skipped
        //  127.0.0.1:49152→ local "0100007F:C000",  remote 10.0.0.2:443 "0200000A:01BB", state 01
        let content = "\
  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345
   1: 0100007F:C000 0200000A:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 67890
";
        let dir = std::env::temp_dir().join("paryty_proc_test");
        let _ = std::fs::create_dir_all(&dir);
        let path = dir.join("tcp_mock");
        std::fs::write(&path, content).expect("write temp file");

        let config = EbpfConfig {
            enabled: true,
            tcp_connections: true,
            dns_resolution: false,
            http_inspection: false,
            db_inspection: false,
            exclude_ports: vec![],
            exclude_ips: vec![],
            ring_buffer_size_kb: 256,
            poll_interval_ms: 100,
            fallback_to_proc: true,
        };

        let cache = Mutex::new(HashMap::new());
        let mut out = HashMap::new();

        ProcObserver::parse_proc_tcp(path.to_str().expect("valid path"), &config, &cache, &mut out)
            .expect("should parse");

        // LISTEN (0A) should be skipped, only ESTABLISHED (01) remains.
        assert_eq!(out.len(), 1, "only ESTABLISHED connections should be kept");

        let values: Vec<_> = out.values().collect();
        assert_eq!(values[0].state, "ESTABLISHED");

        // Verify the source and destination IPs format correctly.
        let keys: Vec<_> = out.keys().collect();
        let key = &keys[0];
        assert_eq!(format_ip(key.src_ip), "127.0.0.1");
        assert_eq!(key.src_port, 0xC000);
        assert_eq!(format_ip(key.dst_ip), "10.0.0.2");
        assert_eq!(key.dst_port, 0x01BB); // 443

        let _ = std::fs::remove_file(&path);
        let _ = std::fs::remove_dir(&dir);
    }

    /// Verify that exclude_ports filters are applied during parsing.
    #[test]
    fn test_parse_proc_tcp_excluded_port() {
        let content = "\
  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 0200000A:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 67890
";
        let dir = std::env::temp_dir().join("paryty_proc_test_exclude");
        let _ = std::fs::create_dir_all(&dir);
        let path = dir.join("tcp_mock");
        std::fs::write(&path, content).expect("write temp file");

        let config = EbpfConfig {
            enabled: true,
            tcp_connections: true,
            dns_resolution: false,
            http_inspection: false,
            db_inspection: false,
            exclude_ports: vec![80], // 0x0050 = 80
            exclude_ips: vec![],
            ring_buffer_size_kb: 256,
            poll_interval_ms: 100,
            fallback_to_proc: true,
        };

        let cache = Mutex::new(HashMap::new());
        let mut out = HashMap::new();

        ProcObserver::parse_proc_tcp(path.to_str().expect("valid path"), &config, &cache, &mut out)
            .expect("should parse");

        assert!(out.is_empty(), "connection with excluded port should be filtered");

        let _ = std::fs::remove_file(&path);
        let _ = std::fs::remove_dir(&dir);
    }

    /// Verify that exclude_ips filters are applied during parsing.
    #[test]
    fn test_parse_proc_tcp_excluded_ip() {
        // Source IP "0100007F" formats to "127.0.0.1" on LE, which should be excluded.
        let content = "\
  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode
   0: 0100007F:C000 0200000A:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 67890
";
        let dir = std::env::temp_dir().join("paryty_proc_test_exclude_ip");
        let _ = std::fs::create_dir_all(&dir);
        let path = dir.join("tcp_mock");
        std::fs::write(&path, content).expect("write temp file");

        let config = EbpfConfig {
            enabled: true,
            tcp_connections: true,
            dns_resolution: false,
            http_inspection: false,
            db_inspection: false,
            exclude_ports: vec![],
            exclude_ips: vec!["127.0.0.1".to_string()],
            ring_buffer_size_kb: 256,
            poll_interval_ms: 100,
            fallback_to_proc: true,
        };

        let cache = Mutex::new(HashMap::new());
        let mut out = HashMap::new();

        ProcObserver::parse_proc_tcp(path.to_str().expect("valid path"), &config, &cache, &mut out)
            .expect("should parse");

        assert!(out.is_empty(), "connection with excluded IP should be filtered");

        let _ = std::fs::remove_file(&path);
        let _ = std::fs::remove_dir(&dir);
    }
}
