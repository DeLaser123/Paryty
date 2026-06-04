//! TCP Connection Tracker
//!
//! Tracks TCP connection lifecycle events (connect, accept, close, reset)
//! via eBPF ring buffer on Linux. Extracts source/destination IP:port pairs
//! and connection state.
//!
//! On non-Linux platforms the ring buffer integration is unavailable but
//! [`TcpTracker::drain_events`] works on all platforms (returning whatever
//! connections have been inserted).

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use tracing::debug;

use crate::config::EbpfConfig;

#[cfg(target_os = "linux")]
use anyhow::Context;
#[cfg(target_os = "linux")]
use tracing::warn;

// ---------------------------------------------------------------------------
// eBPF wire format (Linux only — requires `plain` crate)
// ---------------------------------------------------------------------------

/// TCP event from eBPF ring buffer — must match `struct tcp_event` in
/// `bpf/common.h` exactly.
///
/// ```c
/// struct tcp_event {
///     __u64 timestamp_ns;
///     __u32 event_type;
///     __u32 pid;
///     __u32 tid;
///     __u32 src_ip;
///     __u32 dst_ip;
///     __u16 src_port;
///     __u16 dst_port;
///     __u32 tcp_state;
///     __u64 bytes_sent;
///     __u64 bytes_recv;
///     char comm[16];
/// } __attribute__((packed));
/// ```
#[cfg(target_os = "linux")]
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct TcpEvent {
    pub timestamp_ns: u64,
    pub event_type: u32,
    pub pid: u32,
    pub tid: u32,
    pub src_ip: u32,
    pub dst_ip: u32,
    pub src_port: u16,
    pub dst_port: u16,
    pub tcp_state: u32,
    pub bytes_sent: u64,
    pub bytes_recv: u64,
    pub comm: [u8; 16],
}

// SAFETY: TcpEvent is a plain-old-data struct with repr(C), no padding holes,
// no pointers, and all fields are fixed-size integer types or fixed-size byte
// arrays. The `__attribute__((packed))` in the C definition guarantees no
// padding. The Rust `#[repr(C)]` attribute produces the same layout for this
// field ordering.
#[cfg(target_os = "linux")]
unsafe impl plain::Plain for TcpEvent {}

// ---------------------------------------------------------------------------
// Connection tracking types (all platforms)
// ---------------------------------------------------------------------------

/// Unique key identifying a TCP connection by its 4-tuple.
#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub struct ConnectionKey {
    pub src_ip: u32,
    pub src_port: u16,
    pub dst_ip: u32,
    pub dst_port: u16,
}

/// Tracks the current state and byte counters for a single TCP connection.
#[derive(Debug, Clone)]
pub struct TrackedConnection {
    pub pid: u32,
    pub process_name: String,
    pub state: String,
    pub bytes_sent: u64,
    pub bytes_recv: u64,
    pub first_seen: Instant,
    pub last_seen: Instant,
}

// ---------------------------------------------------------------------------
// TcpTracker
// ---------------------------------------------------------------------------

/// Tracks live TCP connections from eBPF events.
///
/// On Linux, events arrive via a ring buffer attached to the `tcp_events` BPF
/// map. The caller invokes [`poll`](Self::poll) to consume pending events,
/// then [`drain_events`](Self::drain_events) to snapshot-and-clear the
/// connection map.
///
/// On non-Linux platforms the tracker can still be constructed and drained
/// (returning empty results) but no ring buffer is available.
pub struct TcpTracker {
    connections: Mutex<HashMap<ConnectionKey, TrackedConnection>>,
    #[allow(dead_code)] // read in setup_ring_buffer (Linux only)
    config: EbpfConfig,
}

impl TcpTracker {
    /// Create a new tracker from the eBPF configuration.
    pub fn new(config: EbpfConfig) -> Self {
        Self { connections: Mutex::new(HashMap::new()), config }
    }

    /// Set up the TCP ring buffer from a loaded BPF object.
    ///
    /// Registers a callback on the `tcp_events` ring buffer map that parses
    /// incoming [`TcpEvent`]s, filters excluded ports, and updates the
    /// internal connection map.
    ///
    /// Returns a [`libbpf_rs::RingBuffer`] that the caller **must keep
    /// alive** for the duration of event processing and call
    /// [`poll`](libbpf_rs::RingBuffer::poll) on periodically.
    ///
    /// Only available on Linux where `libbpf-rs` is present.
    #[cfg(target_os = "linux")]
    pub fn setup_ring_buffer(
        &self,
        obj: &libbpf_rs::Object,
    ) -> anyhow::Result<libbpf_rs::RingBuffer<'static>> {
        let tcp_map = obj.map("tcp_events").context("tcp_events map not found in BPF object")?;

        let exclude_ports = self.config.exclude_ports.clone();

        // Capture a raw pointer to the connections map. This is safe because:
        // 1. `self` (TcpTracker) outlives the returned RingBuffer — the caller
        //    stores the RingBuffer alongside or inside the same scope as `self`.
        // 2. The callback locks the Mutex before every access.
        // 3. poll() is called from the event loop which holds &self.
        let conns_ptr =
            &self.connections as *const Mutex<HashMap<ConnectionKey, TrackedConnection>>;

        // SAFETY: see comment above — `conns_ptr` remains valid for the
        // lifetime of the TcpTracker, and all access goes through the Mutex.
        // The callback is `'static` (captures only the raw pointer and a
        // cloned Vec<u16>), which satisfies RingBufferBuilder's bounds.
        let conns_ref: &'static Mutex<HashMap<ConnectionKey, TrackedConnection>> =
            unsafe { &*conns_ptr };

        let mut builder = libbpf_rs::RingBufferBuilder::new();
        builder
            .add(tcp_map, move |data: &[u8]| handle_tcp_event(conns_ref, &exclude_ports, data))
            .context("failed to register tcp_events ring buffer callback")?;

        let ring_buffer = builder.build().context("failed to build TCP ring buffer")?;
        Ok(ring_buffer)
    }

    /// Drain all tracked connections as [`super::NetworkEvent`]s.
    ///
    /// 1. Evicts connections inactive for more than 5 minutes.
    /// 2. Converts remaining connections to `NetworkEvent::TcpConnection`.
    /// 3. Clears the internal map so the next drain starts fresh.
    ///
    /// Works on all platforms.
    pub fn drain_events(&self) -> Vec<super::NetworkEvent> {
        let mut conns = self.connections.lock().expect("connections mutex poisoned");

        // Evict stale connections (no activity for 5 minutes).
        let stale_threshold = Duration::from_secs(300);
        let now = Instant::now();
        conns.retain(|_key, conn| {
            let stale = now.duration_since(conn.last_seen) > stale_threshold;
            if stale {
                debug!(
                    pid = conn.pid,
                    process = %conn.process_name,
                    "Evicting stale TCP connection"
                );
            }
            !stale
        });

        // Convert remaining connections to network events.
        let events: Vec<super::NetworkEvent> = conns
            .iter()
            .map(|(key, conn)| super::NetworkEvent::TcpConnection {
                source_ip: format_ip(key.src_ip),
                source_port: key.src_port,
                destination_ip: format_ip(key.dst_ip),
                destination_port: key.dst_port,
                state: conn.state.clone(),
                pid: conn.pid,
                process_name: conn.process_name.clone(),
            })
            .collect();

        // Clear the map so the next drain starts fresh.
        conns.clear();

        events
    }
}

// ---------------------------------------------------------------------------
// Ring buffer event handler (Linux only)
// ---------------------------------------------------------------------------

/// Process a single raw TCP event from the eBPF ring buffer.
///
/// Returns `0` to keep consuming events, non-zero to stop.
#[cfg(target_os = "linux")]
fn handle_tcp_event(
    connections: &Mutex<HashMap<ConnectionKey, TrackedConnection>>,
    exclude_ports: &[u16],
    data: &[u8],
) -> i32 {
    // Parse raw bytes into TcpEvent via zero-copy plain crate.
    let event = match plain::from_bytes::<TcpEvent>(data) {
        Ok(e) => *e,
        Err(_) => {
            warn!(len = data.len(), "Failed to parse TcpEvent from ring buffer");
            return 0;
        }
    };

    // Copy fields to locals to avoid unaligned references from packed struct.
    let src_port = event.src_port;
    let dst_port = event.dst_port;
    let src_ip = event.src_ip;
    let dst_ip = event.dst_ip;
    let pid = event.pid;
    let tcp_state = event.tcp_state;
    let bytes_sent = event.bytes_sent;
    let bytes_recv = event.bytes_recv;

    // Filter excluded ports.
    if exclude_ports.contains(&src_port) || exclude_ports.contains(&dst_port) {
        return 0;
    }

    // Build connection key.
    let key = ConnectionKey {
        src_ip,
        src_port,
        dst_ip,
        dst_port,
    };

    // Extract null-terminated process name from comm field.
    let process_name = extract_comm(&event.comm);
    let state = tcp_state_name(tcp_state).to_string();

    debug!(
        pid = pid,
        src = %format_ip(src_ip),
        src_port = src_port,
        dst = %format_ip(dst_ip),
        dst_port = dst_port,
        state = %state,
        process = %process_name,
        "TCP event received"
    );

    // Update tracked connection.
    let now = Instant::now();
    let mut conns = connections.lock().expect("connections mutex poisoned");
    let tracked = conns.entry(key).or_insert_with(|| TrackedConnection {
        pid,
        process_name: process_name.clone(),
        state: state.clone(),
        bytes_sent: 0,
        bytes_recv: 0,
        first_seen: now,
        last_seen: now,
    });

    tracked.state = state;
    tracked.bytes_sent = bytes_sent;
    tracked.bytes_recv = bytes_recv;
    tracked.last_seen = now;
    tracked.pid = pid;
    tracked.process_name = process_name;

    0
}

// ---------------------------------------------------------------------------
// Helper functions (all platforms)
// ---------------------------------------------------------------------------

/// Extract a null-terminated UTF-8 string from a fixed-size byte array.
///
/// Returns up to `comm.len()` bytes, stopping at the first null byte.
/// Non-UTF-8 bytes are replaced with the Unicode replacement character.
#[allow(dead_code)] // used in handle_tcp_event (Linux) and tests
fn extract_comm(comm: &[u8; 16]) -> String {
    let len = comm.iter().position(|&b| b == 0).unwrap_or(comm.len());
    String::from_utf8_lossy(&comm[..len]).into_owned()
}

/// Format a raw `u32` IP address to dotted-decimal string.
///
/// The eBPF program stores IP addresses in host byte order; this function
/// extracts each octet from the least-significant byte upward.
fn format_ip(ip: u32) -> String {
    format!("{}.{}.{}.{}", ip & 0xFF, (ip >> 8) & 0xFF, (ip >> 16) & 0xFF, (ip >> 24) & 0xFF)
}

/// Map a Linux kernel TCP state code to a human-readable name.
///
/// Values from `include/net/tcp_states.h` in the Linux kernel.
#[allow(dead_code)] // used in handle_tcp_event (Linux) and tests
fn tcp_state_name(state: u32) -> &'static str {
    match state {
        1 => "ESTABLISHED",
        2 => "SYN_SENT",
        3 => "SYN_RECV",
        4 => "FIN_WAIT1",
        5 => "FIN_WAIT2",
        6 => "TIME_WAIT",
        7 => "CLOSE_WAIT",
        8 => "LAST_ACK",
        9 => "CLOSING",
        10 => "LISTEN",
        _ => "UNKNOWN",
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};

    /// Create a minimal [`EbpfConfig`] for testing.
    fn test_ebpf_config() -> EbpfConfig {
        EbpfConfig {
            enabled: true,
            tcp_connections: true,
            dns_resolution: false,
            http_inspection: false,
            db_inspection: false,
            exclude_ports: vec![22, 53],
            exclude_ips: vec!["127.0.0.1".to_string()],
            ring_buffer_size_kb: 256,
            poll_interval_ms: 100,
            fallback_to_proc: true,
        }
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn test_tcp_event_size() {
        // The C struct is packed — expected layout:
        //   u64 (8) + u32 (4) + u32 (4) + u32 (4) + u32 (4) + u32 (4)
        //   + u16 (2) + u16 (2) + u32 (4) + u64 (8) + u64 (8) + [u8;16] (16)
        // = 68 bytes
        assert_eq!(std::mem::size_of::<TcpEvent>(), 68);
    }

    #[test]
    fn test_format_ip() {
        // 127.0.0.1 stored as little-endian u32: octets 127,0,0,1 -> 0x01_00_00_7F
        assert_eq!(format_ip(0x0100_007F), "127.0.0.1");
        // 192.168.1.1 -> octets 192,168,1,1 -> 0x0101_A8C0
        assert_eq!(format_ip(0x0101_A8C0), "192.168.1.1");
        // 10.0.0.1 -> 0x0100_000A
        assert_eq!(format_ip(0x0100_000A), "10.0.0.1");
        // 255.255.255.255
        assert_eq!(format_ip(0xFFFF_FFFF), "255.255.255.255");
        // 0.0.0.0
        assert_eq!(format_ip(0x0000_0000), "0.0.0.0");
    }

    #[test]
    fn test_tcp_state_name() {
        assert_eq!(tcp_state_name(1), "ESTABLISHED");
        assert_eq!(tcp_state_name(2), "SYN_SENT");
        assert_eq!(tcp_state_name(3), "SYN_RECV");
        assert_eq!(tcp_state_name(4), "FIN_WAIT1");
        assert_eq!(tcp_state_name(5), "FIN_WAIT2");
        assert_eq!(tcp_state_name(6), "TIME_WAIT");
        assert_eq!(tcp_state_name(7), "CLOSE_WAIT");
        assert_eq!(tcp_state_name(8), "LAST_ACK");
        assert_eq!(tcp_state_name(9), "CLOSING");
        assert_eq!(tcp_state_name(10), "LISTEN");
        // Out-of-range values
        assert_eq!(tcp_state_name(0), "UNKNOWN");
        assert_eq!(tcp_state_name(99), "UNKNOWN");
        assert_eq!(tcp_state_name(u32::MAX), "UNKNOWN");
    }

    #[test]
    fn test_drain_events_empty() {
        let config = test_ebpf_config();
        let tracker = TcpTracker::new(config);
        let events = tracker.drain_events();
        assert!(events.is_empty(), "drain on fresh tracker should return empty vec");
    }

    #[test]
    fn test_connection_key_hash() {
        let key1 = ConnectionKey {
            src_ip: 0x0100_007F,
            src_port: 12345,
            dst_ip: 0x0101_A8C0,
            dst_port: 80,
        };
        let key2 = key1.clone();

        // Same key should produce the same hash.
        let mut h1 = DefaultHasher::new();
        let mut h2 = DefaultHasher::new();
        key1.hash(&mut h1);
        key2.hash(&mut h2);
        assert_eq!(h1.finish(), h2.finish());

        // Same key should be equal.
        assert_eq!(key1, key2);

        // Different key should NOT be equal.
        let key3 = ConnectionKey {
            src_ip: 0x0100_007F,
            src_port: 12346, // different port
            dst_ip: 0x0101_A8C0,
            dst_port: 80,
        };
        assert_ne!(key1, key3);

        // Verify it works as HashMap keys.
        let mut map = HashMap::new();
        map.insert(key1.clone(), "connection_a");
        map.insert(key3.clone(), "connection_b");
        assert_eq!(map.len(), 2);
        assert_eq!(map.get(&key1), Some(&"connection_a"));
        assert_eq!(map.get(&key2), Some(&"connection_a")); // key2 == key1
        assert_eq!(map.get(&key3), Some(&"connection_b"));
    }

    #[test]
    fn test_extract_comm() {
        // Normal null-terminated name.
        let mut comm = [0u8; 16];
        comm[..5].copy_from_slice(b"nginx");
        assert_eq!(extract_comm(&comm), "nginx");

        // Full 16 bytes, no null terminator — uses all bytes.
        let comm2 = [b'x'; 16];
        assert_eq!(extract_comm(&comm2), "xxxxxxxxxxxxxxxx");

        // All zeros — empty name.
        let comm3 = [0u8; 16];
        assert_eq!(extract_comm(&comm3), "");

        // Null byte in the middle.
        let mut comm4 = [0u8; 16];
        comm4[..3].copy_from_slice(b"ab\0");
        comm4[4] = b'c'; // after the null — should be ignored
        assert_eq!(extract_comm(&comm4), "ab");
    }
}
