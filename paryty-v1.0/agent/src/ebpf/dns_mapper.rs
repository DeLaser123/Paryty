//! DNS Resolver Mapper
//!
//! Maps domain names to IP addresses by intercepting DNS queries from the
//! eBPF ring buffer.  Maintains a bounded in-process cache that enriches
//! TCP connections with the domain names their endpoints resolve to.
//!
//! ## Mutex discipline
//!
//! The cache uses `std::sync::Mutex` (not `tokio::sync::Mutex`) because
//! the guard is never held across an `.await` point.  Methods that return
//! `Option` use `.lock().ok()?` so a poisoned mutex returns `None` instead
//! of panicking.  Methods that must succeed use `.lock().expect(reason)`
//! with a documented reason — never bare `.unwrap()`.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use tracing::debug;
#[cfg(target_os = "linux")]
use tracing::warn;

use super::NetworkEvent;
use crate::config::EbpfConfig;

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/// Entries whose `last_seen` is older than this are evicted on each drain.
const CACHE_TTL: Duration = Duration::from_secs(300);

// ---------------------------------------------------------------------------
// Raw eBPF event — must match `struct dns_event` in bpf/common.h
// ---------------------------------------------------------------------------

/// DNS event delivered by the eBPF ring buffer.
///
/// Layout is `repr(C)` and matches `struct dns_event` from `bpf/common.h`
/// byte-for-byte (288 bytes, no padding).
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct DnsEventRaw {
    pub timestamp_ns: u64,
    pub event_type: u32,
    pub pid: u32,
    pub src_ip: u32,
    pub dst_ip: u32,
    pub src_port: u16,
    pub dst_port: u16,
    pub query_id: u16,
    pub qtype: u16,
    pub domain: [u8; 256],
}

// SAFETY: DnsEventRaw is a repr(C) struct composed entirely of integer
// primitives and a fixed-size byte array — no pointers, no interior
// mutability, no padding holes.  Its in-memory layout is identical to
// the C `struct dns_event` produced by the eBPF program.
#[cfg(target_os = "linux")]
unsafe impl plain::Plain for DnsEventRaw {}

// ---------------------------------------------------------------------------
// Domain model
// ---------------------------------------------------------------------------

/// Cached DNS resolution entry.
#[derive(Debug, Clone)]
pub struct DnsEntry {
    pub domain: String,
    pub ips: Vec<String>,
    pub ttl: u32,
    pub query_type: String,
    pub last_seen: Instant,
    pub pid: u32,
    pub process_name: String,
}

// ---------------------------------------------------------------------------
// DnsMapper
// ---------------------------------------------------------------------------

/// DNS resolver mapper backed by an eBPF ring buffer.
///
/// Collects DNS query events from the kernel, maintains a bounded cache
/// of domain→IP mappings, and provides correlation lookups so that the
/// HTTP inspector can enrich raw TCP connections with domain names.
pub struct DnsMapper {
    cache: Mutex<HashMap<String, DnsEntry>>,
    /// Consumed by [`setup_ring_buffer`](Self::setup_ring_buffer) on Linux
    /// to read `exclude_ports`.
    #[allow(dead_code)]
    config: EbpfConfig,
}

impl DnsMapper {
    /// Create a new mapper with the given eBPF configuration.
    pub fn new(config: EbpfConfig) -> Self {
        Self { cache: Mutex::new(HashMap::with_capacity(256)), config }
    }

    // -- Event ingestion ---------------------------------------------------

    /// Process a raw DNS event delivered by the eBPF ring buffer.
    ///
    /// Extracts the queried domain, maps the qtype to a human-readable
    /// name, records the calling process, and upserts the cache.
    /// Domains that are empty or the sentinel `"<pending>"` are silently
    /// ignored.
    pub fn process_event(&self, raw: &DnsEventRaw) {
        // Copy fields from packed struct to avoid unaligned references.
        let domain_raw = raw.domain;
        let qtype = raw.qtype;
        let pid = raw.pid;

        let domain = extract_domain(&domain_raw);

        if domain.is_empty() || domain == "<pending>" {
            return;
        }

        let query_type = qtype_string(qtype);
        let process_name = get_process_name(pid);

        let mut cache =
            self.cache.lock().expect("DnsMapper: cache mutex poisoned — previous holder panicked");

        let entry = cache.entry(domain.clone()).or_insert_with(|| DnsEntry {
            domain: domain.clone(),
            ips: Vec::new(),
            ttl: 0,
            query_type: query_type.to_string(),
            last_seen: Instant::now(),
            pid,
            process_name: process_name.clone(),
        });

        // Always refresh staleness tracking on every event.
        entry.last_seen = Instant::now();
        entry.pid = pid;
        entry.process_name = process_name;

        debug!(
            domain = %domain,
            query_type = query_type,
            pid = pid,
            "DNS query detected"
        );
    }

    // -- Cache lookups -----------------------------------------------------

    /// Look up a cached DNS entry by domain name.
    ///
    /// Returns `None` if the domain is not cached **or** if the cache
    /// mutex has been poisoned (degraded gracefully instead of panicking).
    pub fn resolve(&self, domain: &str) -> Option<DnsEntry> {
        let cache = self.cache.lock().ok()?;
        cache.get(domain).cloned()
    }

    /// Reverse-lookup: find the domain that resolves to the given IP.
    ///
    /// This is the primary integration point for the HTTP inspector —
    /// given an IP from a TCP connection, determine which domain it
    /// belongs to.
    ///
    /// Returns `None` if no cached entry contains the IP **or** if the
    /// cache mutex has been poisoned.
    pub fn correlate_ip(&self, ip: &str) -> Option<String> {
        let cache = self.cache.lock().ok()?;
        for entry in cache.values() {
            if entry.ips.iter().any(|cached| cached == ip) {
                return Some(entry.domain.clone());
            }
        }
        None
    }

    // -- Drain / export ----------------------------------------------------

    /// Evict expired entries and return the survivors as [`NetworkEvent`]s.
    ///
    /// Called periodically by the eBPF event loop.  "Expired" means the
    /// entry's `last_seen` is older than [`CACHE_TTL`] (5 minutes).
    pub fn drain_events(&self) -> Vec<NetworkEvent> {
        let mut cache =
            self.cache.lock().expect("DnsMapper: cache mutex poisoned — previous holder panicked");

        // Evict stale entries.
        cache.retain(|_domain, entry| entry.last_seen.elapsed() < CACHE_TTL);

        cache
            .values()
            .map(|entry| NetworkEvent::DnsQuery {
                query_name: entry.domain.clone(),
                resolved_ips: entry.ips.clone(),
                latency_ms: 0.0, // Latency is measured at the packet level.
                pid: entry.pid,
            })
            .collect()
    }

    // -- Linux eBPF ring buffer setup --------------------------------------

    /// Attach to the `dns_events` ring buffer exposed by the loaded BPF
    /// object.  Each incoming event is parsed as a [`DnsEventRaw`] and
    /// fed into the process_event path.
    ///
    /// Ports listed in `config.exclude_ports` are silently dropped.
    ///
    /// Returns a [`libbpf_rs::RingBuffer`] with `'static` lifetime.  The
    /// caller **must** keep this alive and call [`poll`](libbpf_rs::RingBuffer::poll)
    /// on it periodically to receive events.
    #[cfg(target_os = "linux")]
    pub fn setup_ring_buffer(
        &self,
        obj: &libbpf_rs::Object,
    ) -> anyhow::Result<libbpf_rs::RingBuffer<'static>> {
        use anyhow::Context as _;

        let map = obj.map("dns_events").context("dns_events map not found in BPF object")?;

        let excluded = self.config.exclude_ports.clone();

        // Capture raw pointer to cache.  Safe because:
        // 1. `self` (DnsMapper) outlives the returned RingBuffer — the caller
        //    stores the RingBuffer alongside or inside the same scope.
        // 2. The callback locks the Mutex before every access.
        let cache_ptr = &self.cache as *const Mutex<HashMap<String, DnsEntry>>;
        // SAFETY: `cache_ptr` remains valid for the lifetime of the DnsMapper,
        // and all access goes through the Mutex.  The callback is `'static`
        // (captures only the raw pointer and a cloned Vec<u16>), which
        // satisfies RingBufferBuilder's bounds.
        let cache_ref: &'static Mutex<HashMap<String, DnsEntry>> =
            unsafe { &*cache_ptr };

        let mut builder = libbpf_rs::RingBufferBuilder::new();
        builder
            .add(map, move |data: &[u8]| {
                if data.len() != std::mem::size_of::<DnsEventRaw>() {
                    warn!(
                        data_len = data.len(),
                        expected = std::mem::size_of::<DnsEventRaw>(),
                        "DNS ring buffer event size mismatch, skipping"
                    );
                    return 0;
                }

                // SAFETY: `data.len()` == `size_of::<DnsEventRaw>()` (checked
                // above) and `DnsEventRaw` is `repr(C)` + `plain::Plain`.
                // libbpf ring-buffer records are 8-byte aligned, satisfying
                // the alignment requirement of `plain::from_bytes`.
                match plain::from_bytes::<DnsEventRaw>(data) {
                    Ok(raw) => {
                        // Copy fields from packed struct to avoid unaligned references.
                        let dst_port = raw.dst_port;
                        let qtype = raw.qtype;
                        let pid = raw.pid;
                        let domain_raw = raw.domain;

                        if excluded.contains(&dst_port) {
                            return 0;
                        }
                        // Process DNS event: extract domain, upsert cache.
                        let domain = extract_domain(&domain_raw);
                        if domain.is_empty() || domain == "<pending>" {
                            return 0;
                        }
                        let query_type = qtype_string(qtype);
                        let process_name = get_process_name(pid);

                        let mut c = cache_ref
                            .lock()
                            .expect("DnsMapper: cache mutex poisoned");
                        let entry = c.entry(domain.clone()).or_insert_with(|| DnsEntry {
                            domain: domain.clone(),
                            ips: Vec::new(),
                            ttl: 0,
                            query_type: query_type.to_string(),
                            last_seen: std::time::Instant::now(),
                            pid,
                            process_name: process_name.clone(),
                        });
                        entry.last_seen = std::time::Instant::now();
                        entry.pid = pid;
                        entry.process_name = process_name;

                        debug!(
                            domain = %domain,
                            query_type = query_type,
                            pid = pid,
                            "DNS query detected"
                        );
                    }
                    Err(e) => {
                        warn!(error = ?e, "Failed to parse DNS event from ring buffer");
                    }
                }
                0
            })
            .context("failed to register DNS ring buffer callback")?;

        builder.build().context("failed to build DNS ring buffer")
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Extract a null-terminated UTF-8 domain name from a 256-byte fixed array.
fn extract_domain(raw: &[u8; 256]) -> String {
    let end = raw.iter().position(|&b| b == 0).unwrap_or(raw.len());
    String::from_utf8_lossy(&raw[..end]).into_owned()
}

/// Map a DNS qtype numeric code to its human-readable name.
fn qtype_string(qtype: u16) -> &'static str {
    match qtype {
        1 => "A",
        28 => "AAAA",
        5 => "CNAME",
        15 => "MX",
        16 => "TXT",
        _ => "UNKNOWN",
    }
}

/// Read the short command name from `/proc/<pid>/comm` (Linux).
///
/// Returns an empty string on non-Linux platforms or when the pid does
/// not exist.
fn get_process_name(pid: u32) -> String {
    std::fs::read_to_string(format!("/proc/{}/comm", pid))
        .map(|s| s.trim().to_string())
        .unwrap_or_default()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    /// Minimal [`EbpfConfig`] for unit tests.
    fn test_config() -> EbpfConfig {
        EbpfConfig {
            enabled: true,
            tcp_connections: true,
            dns_resolution: true,
            http_inspection: true,
            db_inspection: true,
            exclude_ports: vec![],
            exclude_ips: vec![],
            ring_buffer_size_kb: 256,
            poll_interval_ms: 100,
            fallback_to_proc: true,
        }
    }

    /// Build a [`DnsEventRaw`] with the given domain, qtype and pid.
    /// The domain is null-terminated inside the 256-byte array.
    fn make_raw(domain: &str, qtype: u16, pid: u32) -> DnsEventRaw {
        let mut raw = DnsEventRaw {
            timestamp_ns: 1_700_000_000_000_000_000,
            event_type: 0,
            pid,
            src_ip: 0,
            dst_ip: 0,
            src_port: 0,
            dst_port: 53,
            query_id: 0x1234,
            qtype,
            domain: [0u8; 256],
        };
        let bytes = domain.as_bytes();
        let len = bytes.len().min(255); // leave room for null terminator
        raw.domain[..len].copy_from_slice(&bytes[..len]);
        raw
    }

    // -- Struct layout -----------------------------------------------------

    #[test]
    fn test_dns_event_raw_size() {
        // Must match `struct dns_event` in common.h — 288 bytes packed.
        assert_eq!(std::mem::size_of::<DnsEventRaw>(), 288);
    }

    // -- process_event -----------------------------------------------------

    #[test]
    fn test_process_event_valid() {
        let mapper = DnsMapper::new(test_config());
        let raw = make_raw("example.com", 1, 1234);

        mapper.process_event(&raw);

        let entry = mapper.resolve("example.com").expect("entry should be cached");
        assert_eq!(entry.domain, "example.com");
        assert_eq!(entry.query_type, "A");
        assert_eq!(entry.pid, 1234);
        // get_process_name returns empty on non-Linux; just check it's set.
        // (No assertion on value — platform-dependent.)
    }

    #[test]
    fn test_process_event_pending() {
        let mapper = DnsMapper::new(test_config());
        let raw = make_raw("<pending>", 1, 100);

        mapper.process_event(&raw);

        assert!(mapper.resolve("<pending>").is_none(), "<pending> domain must be silently skipped");
    }

    #[test]
    fn test_process_event_empty() {
        let mapper = DnsMapper::new(test_config());
        let raw = make_raw("", 1, 100);

        mapper.process_event(&raw);

        assert!(mapper.resolve("").is_none(), "empty domain must be silently skipped");
    }

    // -- resolve -----------------------------------------------------------

    #[test]
    fn test_resolve_found() {
        let mapper = DnsMapper::new(test_config());
        let raw = make_raw("rust-lang.org", 28, 5678);

        mapper.process_event(&raw);

        let entry = mapper.resolve("rust-lang.org").expect("should find cached entry");
        assert_eq!(entry.query_type, "AAAA");
        assert_eq!(entry.pid, 5678);
    }

    #[test]
    fn test_resolve_not_found() {
        let mapper = DnsMapper::new(test_config());

        assert!(mapper.resolve("nonexistent.example").is_none(), "unknown domain must return None");
    }

    // -- correlate_ip ------------------------------------------------------

    #[test]
    fn test_correlate_ip() {
        let mapper = DnsMapper::new(test_config());

        // Directly populate the cache with a known IP mapping.
        {
            let mut cache = mapper.cache.lock().expect("test: cache mutex poisoned");
            cache.insert(
                "example.com".to_string(),
                DnsEntry {
                    domain: "example.com".to_string(),
                    ips: vec![
                        "93.184.216.34".to_string(),
                        "2606:2800:0220:0001:0248:1893:25c8:1946".to_string(),
                    ],
                    ttl: 300,
                    query_type: "A".to_string(),
                    last_seen: Instant::now(),
                    pid: 42,
                    process_name: "curl".to_string(),
                },
            );
        }

        // IPv4 lookup
        assert_eq!(mapper.correlate_ip("93.184.216.34"), Some("example.com".to_string()),);

        // IPv6 lookup
        assert_eq!(
            mapper.correlate_ip("2606:2800:0220:0001:0248:1893:25c8:1946"),
            Some("example.com".to_string()),
        );

        // Unknown IP
        assert!(mapper.correlate_ip("10.0.0.1").is_none());
    }

    // -- drain_events ------------------------------------------------------

    #[test]
    fn test_drain_evicts_expired() {
        let mapper = DnsMapper::new(test_config());

        // Insert a stale entry (10 minutes old — well past the 5-min TTL).
        {
            let mut cache = mapper.cache.lock().expect("test: cache mutex poisoned");
            cache.insert(
                "expired.example".to_string(),
                DnsEntry {
                    domain: "expired.example".to_string(),
                    ips: vec!["1.2.3.4".to_string()],
                    ttl: 300,
                    query_type: "A".to_string(),
                    last_seen: Instant::now() - Duration::from_secs(600),
                    pid: 99,
                    process_name: "test".to_string(),
                },
            );
        }

        let events = mapper.drain_events();

        assert!(events.is_empty(), "entry older than TTL must be evicted by drain");
        assert!(
            mapper.resolve("expired.example").is_none(),
            "evicted entry must not be resolvable"
        );
    }

    #[test]
    fn test_drain_returns_fresh_entries() {
        let mapper = DnsMapper::new(test_config());
        let raw = make_raw("fresh.example", 1, 42);
        mapper.process_event(&raw);

        let events = mapper.drain_events();

        assert_eq!(events.len(), 1);
        match &events[0] {
            NetworkEvent::DnsQuery { query_name, resolved_ips, latency_ms, pid } => {
                assert_eq!(query_name, "fresh.example");
                assert!(resolved_ips.is_empty());
                assert_eq!(*latency_ms, 0.0);
                assert_eq!(*pid, 42);
            }
            other => panic!("expected DnsQuery, got {:?}", other),
        }
    }

    // -- qtype mapping -----------------------------------------------------

    #[test]
    fn test_qtype_mapping() {
        assert_eq!(qtype_string(1), "A");
        assert_eq!(qtype_string(28), "AAAA");
        assert_eq!(qtype_string(5), "CNAME");
        assert_eq!(qtype_string(15), "MX");
        assert_eq!(qtype_string(16), "TXT");

        // Unmapped types must return "UNKNOWN"
        assert_eq!(qtype_string(0), "UNKNOWN");
        assert_eq!(qtype_string(2), "UNKNOWN");
        assert_eq!(qtype_string(99), "UNKNOWN");
        assert_eq!(qtype_string(u16::MAX), "UNKNOWN");
    }
}
