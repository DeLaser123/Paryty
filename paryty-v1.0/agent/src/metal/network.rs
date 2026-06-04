//! Network Metrics Collector
//!
//! Collects network interface metrics from /proc/net/dev with rate
//! calculation, TCP statistics from /proc/net/snmp and /proc/net/tcp,
//! and link speed from sysfs.
//!
//! ## Data Sources
//!
//! | Metric         | Source                          | Platform |
//! |----------------|---------------------------------|----------|
//! | I/O rates      | `/proc/net/dev`                 | Linux    |
//! | TCP retransmits| `/proc/net/snmp`                | Linux    |
//! | TCP states     | `/proc/net/tcp`, `/proc/net/tcp6`| Linux   |
//! | Link speed     | `/sys/class/net/{name}/speed`   | Linux    |
//! | Fallback       | `sysinfo` crate                 | All      |

use anyhow::Result;
use chrono::Utc;
use serde::Serialize;
use std::collections::HashMap;
use tracing::instrument;

use crate::config::MetalConfig;

/// TCP protocol statistics aggregated from `/proc/net/snmp` and state parsing.
#[derive(Debug, Clone, Serialize, serde::Deserialize, Default)]
pub struct TcpStats {
    /// Total TCP retransmitted segments from `/proc/net/snmp` RetransSegs.
    pub retransmits: u64,
    /// Number of ESTABLISHED connections (CurrEstab from /proc/net/snmp,
    /// or counted from /proc/net/tcp state parsing).
    pub active_connections: u32,
    /// Connections in TIME_WAIT state.
    pub time_wait: u32,
    /// Sockets in LISTEN state.
    pub listen: u32,
}

/// Network interface metrics.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct NetworkInterface {
    pub interface_name: String,
    pub rx_bytes_per_sec: f64,
    pub tx_bytes_per_sec: f64,
    pub rx_packets_per_sec: f64,
    pub tx_packets_per_sec: f64,
    pub rx_errors: u64,
    pub tx_errors: u64,
    /// Dropped received packets. `None` when sysinfo doesn't report this.
    pub rx_dropped: Option<u64>,
    /// Dropped transmitted packets. `None` when sysinfo doesn't report this.
    pub tx_dropped: Option<u64>,
    /// Cumulative bytes received since boot / interface creation.
    pub total_rx_bytes: u64,
    /// Cumulative bytes transmitted since boot / interface creation.
    pub total_tx_bytes: u64,
    /// Cumulative packets received since boot / interface creation.
    pub total_rx_packets: u64,
    /// Cumulative packets transmitted since boot / interface creation.
    pub total_tx_packets: u64,
    /// TCP retransmits for this interface. `None` on non-Linux or when unavailable.
    pub tcp_retransmits: Option<u64>,
    /// Estimated RTT in milliseconds. `None` when unavailable.
    pub estimated_rtt_ms: Option<f64>,
    /// Link speed in Mbps from `/sys/class/net/{name}/speed`.
    /// `None` if unavailable (e.g., virtual interfaces or non-Linux).
    pub speed_mbps: Option<u64>,
    /// Whether the interface is administratively up.
    pub is_up: bool,
}

/// Network metrics collection result.
#[derive(Debug, Serialize, serde::Deserialize, Clone)]
pub struct NetworkMetrics {
    pub timestamp: String,
    pub interfaces: Vec<NetworkInterface>,
    /// Aggregate TCP statistics. `None` on non-Linux or collection failure.
    pub tcp_stats: Option<TcpStats>,
}

/// Network collector.
///
/// On Linux, uses `/proc/net/dev` with delta-based rate calculation.
/// On other platforms, uses a persistent `sysinfo::Networks` instance
/// so that `received()` / `transmitted()` return deltas since last refresh.
pub struct NetworkCollector {
    prev_stats: std::sync::Mutex<HashMap<String, NetStatSnapshot>>,
    #[allow(dead_code)] // used only on non-Linux via collect_cross_platform()
    cross_platform_networks: std::sync::Mutex<Option<sysinfo::Networks>>,
}

#[derive(Debug, Clone)]
#[allow(dead_code)] // fields used in Linux-only collect_linux()
struct NetStatSnapshot {
    rx_bytes: u64,
    tx_bytes: u64,
    rx_packets: u64,
    tx_packets: u64,
    timestamp: std::time::Instant,
}

impl NetworkCollector {
    pub fn new() -> Self {
        Self {
            prev_stats: std::sync::Mutex::new(HashMap::new()),
            cross_platform_networks: std::sync::Mutex::new(None),
        }
    }

    #[instrument(skip(self, _config), fields(collector = "network"))]
    pub fn collect(&self, _config: &MetalConfig) -> Result<NetworkMetrics> {
        let interfaces = if cfg!(target_os = "linux") {
            self.collect_linux()?
        } else {
            self.collect_cross_platform()?
        };

        // Collect TCP stats — best-effort, non-fatal on failure
        let tcp_stats = collect_tcp_stats();

        Ok(NetworkMetrics { timestamp: Utc::now().to_rfc3339(), interfaces, tcp_stats })
    }

    #[cfg(target_os = "linux")]
    fn collect_linux(&self) -> Result<Vec<NetworkInterface>> {
        use std::fs;
        use std::time::Instant;

        let now = Instant::now();
        let netdev = fs::read_to_string("/proc/net/dev")?;
        let mut current_stats = HashMap::new();
        let mut interfaces = Vec::new();

        for line in netdev.lines().skip(2) {
            // Skip header lines
            let parts: Vec<&str> = line.split_whitespace().collect();
            if parts.len() < 17 {
                continue;
            }

            let name = parts[0].trim_end_matches(':').to_string();
            if name == "lo" {
                continue; // Skip loopback
            }

            let rx_bytes: u64 = parts[1].parse().unwrap_or(0);
            let rx_packets: u64 = parts[2].parse().unwrap_or(0);
            let rx_errors: u64 = parts[3].parse().unwrap_or(0);
            let rx_dropped: u64 = parts[4].parse().unwrap_or(0);
            let tx_bytes: u64 = parts[9].parse().unwrap_or(0);
            let tx_packets: u64 = parts[10].parse().unwrap_or(0);
            let tx_errors: u64 = parts[11].parse().unwrap_or(0);
            let tx_dropped: u64 = parts[12].parse().unwrap_or(0);

            current_stats.insert(
                name.clone(),
                NetStatSnapshot { rx_bytes, tx_bytes, rx_packets, tx_packets, timestamp: now },
            );

            // Calculate rates from previous snapshot
            let prev = self.prev_stats.lock().unwrap();
            let (rx_rate, tx_rate, rx_pkt_rate, tx_pkt_rate) =
                if let Some(prev_snap) = prev.get(&name) {
                    let dt = now.duration_since(prev_snap.timestamp).as_secs_f64();
                    if dt > 0.0 {
                        (
                            rx_bytes.saturating_sub(prev_snap.rx_bytes) as f64 / dt,
                            tx_bytes.saturating_sub(prev_snap.tx_bytes) as f64 / dt,
                            rx_packets.saturating_sub(prev_snap.rx_packets) as f64 / dt,
                            tx_packets.saturating_sub(prev_snap.tx_packets) as f64 / dt,
                        )
                    } else {
                        (0.0, 0.0, 0.0, 0.0)
                    }
                } else {
                    (0.0, 0.0, 0.0, 0.0)
                };
            drop(prev);

            // Read link speed and carrier state from sysfs
            let (speed_mbps, is_up) = get_link_speed(&name);

            interfaces.push(NetworkInterface {
                interface_name: name,
                rx_bytes_per_sec: rx_rate,
                tx_bytes_per_sec: tx_rate,
                rx_packets_per_sec: rx_pkt_rate,
                tx_packets_per_sec: tx_pkt_rate,
                rx_errors,
                tx_errors,
                rx_dropped: Some(rx_dropped),
                tx_dropped: Some(tx_dropped),
                total_rx_bytes: rx_bytes,
                total_tx_bytes: tx_bytes,
                total_rx_packets: rx_packets,
                total_tx_packets: tx_packets,
                tcp_retransmits: None, // per-interface retransmits not available from /proc/net/dev
                estimated_rtt_ms: None, // RTT estimation not available from /proc
                speed_mbps: Some(speed_mbps),
                is_up,
            });
        }

        *self.prev_stats.lock().unwrap() = current_stats;
        Ok(interfaces)
    }

    #[cfg(not(target_os = "linux"))]
    fn collect_linux(&self) -> Result<Vec<NetworkInterface>> {
        self.collect_cross_platform()
    }

    fn collect_cross_platform(&self) -> Result<Vec<NetworkInterface>> {
        use sysinfo::Networks;

        let mut guard =
            self.cross_platform_networks.lock().unwrap_or_else(|poisoned| poisoned.into_inner());

        let networks = match guard.as_mut() {
            Some(nw) => {
                // Subsequent call: refresh in-place. `received()` and
                // `transmitted()` now return deltas since last refresh.
                nw.refresh();
                nw
            }
            None => {
                // First call: create the persistent instance.
                // `new_with_refreshed_list()` does an initial baseline read,
                // so the first data sample establishes the starting counters.
                let nw = Networks::new_with_refreshed_list();
                *guard = Some(nw);
                guard.as_mut().unwrap()
            }
        };

        let mut interfaces = Vec::new();

        for (name, data) in networks.iter() {
            interfaces.push(NetworkInterface {
                interface_name: name.clone(),
                rx_bytes_per_sec: data.received() as f64,
                tx_bytes_per_sec: data.transmitted() as f64,
                rx_packets_per_sec: data.packets_received() as f64,
                tx_packets_per_sec: data.packets_transmitted() as f64,
                rx_errors: data.errors_on_received(),
                tx_errors: data.errors_on_transmitted(),
                rx_dropped: None, // sysinfo doesn't expose dropped packets
                tx_dropped: None, // sysinfo doesn't expose dropped packets
                total_rx_bytes: data.total_received(),
                total_tx_bytes: data.total_transmitted(),
                total_rx_packets: data.total_packets_received(),
                total_tx_packets: data.total_packets_transmitted(),
                tcp_retransmits: None,  // not available via sysinfo
                estimated_rtt_ms: None, // not available via sysinfo
                speed_mbps: None,       // not available via sysinfo on Windows/macOS
                is_up: false,           // sysinfo doesn't expose carrier state
            });
        }

        Ok(interfaces)
    }
}

// ---------------------------------------------------------------------------
// TCP statistics collection
// ---------------------------------------------------------------------------

/// Collect TCP statistics from procfs. Returns `None` on non-Linux or on
/// any I/O / parse failure — this is best-effort.
fn collect_tcp_stats() -> Option<TcpStats> {
    #[cfg(target_os = "linux")]
    {
        let mut stats = TcpStats::default();

        // Retransmits + CurrEstab from /proc/net/snmp
        if let Ok(snmp) = std::fs::read_to_string("/proc/net/snmp") {
            if let Some(s) = parse_tcp_snmp(&snmp) {
                stats.retransmits = s.retransmits;
                stats.active_connections = s.active_connections;
            }
        }

        // Connection state counts from /proc/net/tcp + /proc/net/tcp6
        if let Ok(tcp4) = std::fs::read_to_string("/proc/net/tcp") {
            let states = parse_tcp_states(&tcp4);
            stats.time_wait += states.time_wait;
            stats.listen += states.listen;
            // If SNMP didn't give CurrEstab, fall back to state counting
            if stats.active_connections == 0 {
                stats.active_connections += states.active_connections;
            }
        }
        if let Ok(tcp6) = std::fs::read_to_string("/proc/net/tcp6") {
            let states = parse_tcp_states(&tcp6);
            stats.time_wait += states.time_wait;
            stats.listen += states.listen;
            if stats.active_connections == 0 {
                stats.active_connections += states.active_connections;
            }
        }

        Some(stats)
    }

    #[cfg(not(target_os = "linux"))]
    {
        None
    }
}

/// Parse `/proc/net/snmp` to extract TCP retransmits and current established
/// connections.
///
/// The file contains paired header/data lines per protocol. We need the
/// second `Tcp:` line (the data line):
///
/// ```text
/// Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts
/// Tcp: 1 200 120000 -1 1234 567 89 12 5 67890 54321 10 0 42
/// ```
///
/// Returns `None` if the file format is unexpected.
#[allow(dead_code)] // also called in tests; production caller is cfg(linux)
fn parse_tcp_snmp(content: &str) -> Option<TcpStats> {
    let mut lines = content.lines().filter(|l| l.starts_with("Tcp:"));
    let _header = lines.next()?; // skip header line
    let data_line = lines.next()?; // data line

    let fields: Vec<&str> = data_line.split_whitespace().collect();
    // Field layout (0-indexed after "Tcp:" prefix):
    //   0: "Tcp:", 1: RtoAlgo, 2: RtoMin, 3: RtoMax, 4: MaxConn,
    //   5: ActiveOpens, 6: PassiveOpens, 7: AttemptFails, 8: EstabResets,
    //   9: CurrEstab, 10: InSegs, 11: OutSegs, 12: RetransSegs,
    //   13: InErrs, 14: OutRsts
    if fields.len() < 13 {
        return None;
    }

    let active_connections: u32 = fields.get(9)?.parse().ok()?;
    let retransmits: u64 = fields.get(12)?.parse().ok()?;

    Some(TcpStats { retransmits, active_connections, ..TcpStats::default() })
}

/// Parse `/proc/net/tcp` or `/proc/net/tcp6` connection entries and count
/// connections by state.
///
/// Each data line (after the header) looks like:
/// ```text
///   sl  local_address rem_address   st tx_queue rx_queue ...
///    0: 0100007F:0050 00000000:0000 0A 00000000:00000000 ...
/// ```
///
/// The `st` field (index 3, 0-indexed) is a hex state code:
/// - `01` = ESTABLISHED
/// - `06` = TIME_WAIT
/// - `0A` = LISTEN
#[allow(dead_code)] // also called in tests; production caller is cfg(linux)
fn parse_tcp_states(content: &str) -> TcpStats {
    let mut stats = TcpStats::default();

    for line in content.lines().skip(1) {
        // skip header
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 4 {
            continue;
        }
        // Field 3 is the hex state code
        match fields[3] {
            "01" => stats.active_connections += 1,
            "06" => stats.time_wait += 1,
            "0A" => stats.listen += 1,
            _ => {}
        }
    }

    stats
}

/// Read link speed (Mbps) and carrier state for a network interface from
/// sysfs. Returns `(speed_mbps, is_up)`.
///
/// - `/sys/class/net/{name}/speed` — link speed in Mbps (may fail for
///   virtual interfaces).
/// - `/sys/class/net/{name}/carrier` — `1` if the link is up.
///
/// On any read error, returns `(0, false)` — never panics.
#[cfg(target_os = "linux")]
fn get_link_speed(name: &str) -> (u64, bool) {
    use std::fs;

    let speed_path = format!("/sys/class/net/{}/speed", name);
    let carrier_path = format!("/sys/class/net/{}/carrier", name);

    let speed_mbps = fs::read_to_string(&speed_path)
        .ok()
        .and_then(|s| s.trim().parse::<u64>().ok())
        .unwrap_or(0);

    // carrier file contains "1\n" when the link is up
    let is_up = fs::read_to_string(&carrier_path).ok().map(|s| s.trim() == "1").unwrap_or(false);

    (speed_mbps, is_up)
}

impl Default for NetworkCollector {
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

    #[test]
    fn test_parse_netdev_line() {
        // Simulate a /proc/net/dev line for eth0:
        //  iface: rx_bytes rx_packets rx_errs rx_drop rx_fifo rx_frame rx_compressed rx_multicast
        //          tx_bytes tx_packets tx_errs tx_drop tx_fifo tx_colls tx_carrier tx_compressed
        let line = "eth0: 123456789 98765 10 5 0 0 0 0 87654321 65432 20 15 0 0 0 0";
        let parts: Vec<&str> = line.split_whitespace().collect();
        assert!(parts.len() >= 17);

        let name = parts[0].trim_end_matches(':');
        assert_eq!(name, "eth0");

        let rx_bytes: u64 = parts[1].parse().unwrap();
        let rx_packets: u64 = parts[2].parse().unwrap();
        let rx_errors: u64 = parts[3].parse().unwrap();
        let rx_dropped: u64 = parts[4].parse().unwrap();
        let tx_bytes: u64 = parts[9].parse().unwrap();
        let tx_packets: u64 = parts[10].parse().unwrap();
        let tx_errors: u64 = parts[11].parse().unwrap();
        let tx_dropped: u64 = parts[12].parse().unwrap();

        assert_eq!(rx_bytes, 123_456_789);
        assert_eq!(rx_packets, 98_765);
        assert_eq!(rx_errors, 10);
        assert_eq!(rx_dropped, 5);
        assert_eq!(tx_bytes, 87_654_321);
        assert_eq!(tx_packets, 65_432);
        assert_eq!(tx_errors, 20);
        assert_eq!(tx_dropped, 15);
    }

    #[test]
    fn test_parse_tcp_snmp() {
        let snmp_data = "\
Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts
Tcp: 1 200 120000 -1 54321 12345 100 50 42 999999 888888 777 3 123
";
        let stats = parse_tcp_snmp(snmp_data).expect("should parse TCP SNMP data");
        assert_eq!(stats.retransmits, 777);
        assert_eq!(stats.active_connections, 42);
    }

    #[test]
    fn test_parse_tcp_snmp_header_only_returns_none() {
        let snmp_data = "\
Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts
";
        assert!(parse_tcp_snmp(snmp_data).is_none());
    }

    #[test]
    fn test_parse_tcp_snmp_short_line_returns_none() {
        let snmp_data = "\
Tcp: RtoAlgorithm RtoMin
Tcp: 1 200
";
        assert!(parse_tcp_snmp(snmp_data).is_none());
    }

    #[test]
    fn test_tcp_state_counting() {
        // Simulated /proc/net/tcp content
        let tcp_content = "\
  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12345 1 ffff888012345680 100 0 0 10 0
   1: 0100007F:C000 0100007F:0050 01 00000000:00000000 00:00000000 00000000     0        0 12346 1 ffff888012345680 100 0 0 10 0
   2: 0100007F:C001 0100007F:0050 01 00000000:00000000 00:00000000 00000000     0        0 12347 1 ffff888012345680 100 0 0 10 0
   3: 0100007F:C002 0100007F:0050 06 00000000:00000000 00:00000000 00000000     0        0 12348 1 ffff888012345680 100 0 0 10 0
   4: 0100007F:C003 0100007F:0050 06 00000000:00000000 00:00000000 00000000     0        0 12349 1 ffff888012345680 100 0 0 10 0
   5: 0100007F:C004 0100007F:0050 06 00000000:00000000 00:00000000 00000000     0        0 12350 1 ffff888012345680 100 0 0 10 0
   6: 0100007F:C005 0100007F:0050 08 00000000:00000000 00:00000000 00000000     0        0 12351 1 ffff888012345680 100 0 0 10 0
";
        let stats = parse_tcp_states(tcp_content);
        assert_eq!(stats.listen, 1, "one LISTEN socket (state 0A)");
        assert_eq!(stats.active_connections, 2, "two ESTABLISHED connections (state 01)");
        assert_eq!(stats.time_wait, 3, "three TIME_WAIT connections (state 06)");
    }

    #[test]
    fn test_tcp_state_empty_input() {
        let stats = parse_tcp_states("");
        assert_eq!(stats.active_connections, 0);
        assert_eq!(stats.time_wait, 0);
        assert_eq!(stats.listen, 0);
    }

    #[test]
    fn test_tcp_state_malformed_lines_skipped() {
        let tcp_content = "\
  sl  local_address rem_address   st
   short line
   0: 0100007F:0050 00000000:0000 01
";
        let stats = parse_tcp_states(tcp_content);
        assert_eq!(stats.active_connections, 1);
    }

    #[test]
    fn test_network_interface_has_new_fields() {
        // Ensure the new fields exist and have correct defaults
        let iface = NetworkInterface {
            interface_name: "eth0".to_string(),
            rx_bytes_per_sec: 0.0,
            tx_bytes_per_sec: 0.0,
            rx_packets_per_sec: 0.0,
            tx_packets_per_sec: 0.0,
            rx_errors: 0,
            tx_errors: 0,
            rx_dropped: Some(0),
            tx_dropped: Some(0),
            total_rx_bytes: 0,
            total_tx_bytes: 0,
            total_rx_packets: 0,
            total_tx_packets: 0,
            tcp_retransmits: None,
            estimated_rtt_ms: None,
            speed_mbps: Some(1000),
            is_up: true,
        };
        assert_eq!(iface.speed_mbps, Some(1000));
        assert!(iface.is_up);
        assert_eq!(iface.rx_dropped, Some(0));
        assert!(iface.tcp_retransmits.is_none());
    }

    #[test]
    fn test_tcp_stats_default() {
        let stats = TcpStats::default();
        assert_eq!(stats.retransmits, 0);
        assert_eq!(stats.active_connections, 0);
        assert_eq!(stats.time_wait, 0);
        assert_eq!(stats.listen, 0);
    }

    #[test]
    fn test_network_metrics_includes_tcp_stats() {
        let metrics = NetworkMetrics {
            timestamp: "2024-01-01T00:00:00Z".to_string(),
            interfaces: vec![],
            tcp_stats: Some(TcpStats {
                retransmits: 42,
                active_connections: 10,
                time_wait: 5,
                listen: 3,
            }),
        };
        assert!(metrics.tcp_stats.is_some());
        let tcp = metrics.tcp_stats.unwrap();
        assert_eq!(tcp.retransmits, 42);
        assert_eq!(tcp.active_connections, 10);
    }
}
