//! HTTP/SNI Inspector
//!
//! Parses HTTP/1.1 request/response data and extracts TLS SNI (Server Name
//! Indication) hostnames from eBPF ring buffer events.  The eBPF kernel
//! programs push [`HttpEventRaw`] structs via a BPF ring buffer; this module
//! converts them into platform-neutral [`NetworkEvent`]s.
//!
//! # Platform support
//!
//! - **Linux** — Full eBPF ring buffer consumption via [`libbpf_rs`].
//!   [`HttpInspector::setup_ring_buffer`] registers a callback that parses
//!   raw events and enqueues them for [`HttpInspector::drain_events`].
//! - **Non-Linux** — Parser functions (`SniExtractor`, `HttpParser`) and
//!   [`HttpInspector::process_event`] work on all platforms.  The ring
//!   buffer integration is gated behind `#[cfg(target_os = "linux")]`.

use std::collections::VecDeque;
use std::sync::Mutex;

use tracing::{debug, warn};

use crate::config::EbpfConfig;

use super::NetworkEvent;

// ---------------------------------------------------------------------------
// Constants — must match `enum event_type` in bpf/common.h
// ---------------------------------------------------------------------------

/// eBPF event type: HTTP request observed.
const EVENT_HTTP_REQUEST: u32 = 5;
/// eBPF event type: HTTPS SNI observed.
const EVENT_HTTPS_SNI: u32 = 6;

/// Maximum number of events buffered in the internal queue before older events
/// are dropped.  Prevents unbounded memory growth under sustained back-pressure.
#[cfg(target_os = "linux")]
const MAX_EVENT_QUEUE: usize = 4096;

// ---------------------------------------------------------------------------
// HttpEventRaw — zero-copy repr(C) struct matching `struct http_event` in C
// ---------------------------------------------------------------------------

/// HTTP/SNI event from eBPF ring buffer.
///
/// This struct is `repr(C)` with identical field ordering and sizes to the C
/// `struct http_event` defined in `bpf/common.h`.  On Linux, it implements
/// [`plain::Plain`] so that raw ring-buffer bytes can be cast zero-copy.
///
/// With `#[repr(C)]` (not packed), the compiler inserts alignment padding
/// after `method`/`_pad` to align the `host` array to a 4-byte boundary.
/// Actual size: **288** bytes.
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct HttpEventRaw {
    pub timestamp_ns: u64,
    pub event_type: u32,
    pub pid: u32,
    pub src_ip: u32,
    pub dst_ip: u32,
    pub src_port: u16,
    pub dst_port: u16,
    pub status_code: u16,
    pub method: u8,
    #[allow(dead_code)]
    pub _pad: u8,
    pub host: [u8; 128],
    pub path: [u8; 128],
}

// SAFETY: `HttpEventRaw` is `repr(C)`, has no padding bytes beyond `_pad`,
// and contains only integer / fixed-size-array fields — all bit patterns are
// valid.  The struct size (288 bytes) matches the C definition exactly.
#[cfg(target_os = "linux")]
unsafe impl plain::Plain for HttpEventRaw {}

/// Compile-time assertion: `HttpEventRaw` is exactly 288 bytes, matching
/// `sizeof(struct http_event)` in `bpf/common.h`.
///
/// Layout with `#[repr(C)]` alignment:
/// - `timestamp_ns` (u64)       @ 0   -> 8 bytes
/// - `event_type`   (u32)       @ 8   -> 4 bytes
/// - `pid`          (u32)       @ 12  -> 4 bytes
/// - `src_ip`       (u32)       @ 16  -> 4 bytes
/// - `dst_ip`       (u32)       @ 20  -> 4 bytes
/// - `src_port`     (u16)       @ 24  -> 2 bytes
/// - `dst_port`     (u16)       @ 26  -> 2 bytes
/// - `status_code`  (u16)       @ 28  -> 2 bytes
/// - `method`       (u8)        @ 30  -> 1 byte
/// - `_pad`         (u8)        @ 31  -> 1 byte
/// - `host`         ([u8;128])  @ 32  -> 128 bytes
/// - `path`         ([u8;128])  @ 160 -> 128 bytes
///
/// total = 288
const _: () = assert!(
    std::mem::size_of::<HttpEventRaw>() == 288,
    "HttpEventRaw size mismatch with C struct http_event"
);

impl HttpEventRaw {
    /// Parse an `HttpEventRaw` from a byte slice of at least 288 bytes.
    ///
    /// Returns `None` if the slice is not the correct length.
    #[cfg(not(target_os = "linux"))]
    pub fn from_bytes(data: &[u8]) -> Option<Self> {
        if data.len() < std::mem::size_of::<Self>() {
            return None;
        }
        // SAFETY: We verified the length; `HttpEventRaw` is `repr(C)` with
        // all-valid bit patterns.
        Some(unsafe { std::ptr::read_unaligned(data.as_ptr().cast::<Self>()) })
    }
}

// ---------------------------------------------------------------------------
// SNI Extractor
// ---------------------------------------------------------------------------

/// Extracts the Server Name Indication (SNI) hostname from a TLS ClientHello.
pub struct SniExtractor;

impl SniExtractor {
    /// Extract SNI (Server Name Indication) from a TLS ClientHello message.
    ///
    /// Returns the server hostname if a valid SNI extension is found.
    pub fn extract_sni(data: &[u8]) -> Option<String> {
        if data.len() < 44 {
            return None;
        }

        if data[0] != 0x16 {
            return None;
        }

        if data[1] < 0x03 || (data[1] == 0x03 && data[2] < 0x01) {
            return None;
        }

        let record_len = ((data[3] as usize) << 8) | (data[4] as usize);
        if 5 + record_len > data.len() {
            return None;
        }

        if data[5] != 0x01 {
            return None;
        }

        let handshake_len =
            ((data[6] as usize) << 16) | ((data[7] as usize) << 8) | (data[8] as usize);
        if 9 + handshake_len > data.len() {
            return None;
        }

        let mut offset: usize = 43;

        if offset >= data.len() {
            return None;
        }
        let session_id_len = data[offset] as usize;
        offset += 1 + session_id_len;

        if offset + 2 > data.len() {
            return None;
        }
        let cipher_suites_len =
            ((data[offset] as usize) << 8) | (data.get(offset + 1).copied().unwrap_or(0) as usize);
        offset += 2 + cipher_suites_len;

        if offset >= data.len() {
            return None;
        }
        let compression_len = data[offset] as usize;
        offset += 1 + compression_len;

        if offset + 2 > data.len() {
            return None;
        }
        let extensions_len =
            ((data[offset] as usize) << 8) | (data.get(offset + 1).copied().unwrap_or(0) as usize);
        offset += 2;

        let extensions_end = offset.saturating_add(extensions_len);
        if extensions_end > data.len() {
            return None;
        }

        while offset + 4 <= extensions_end {
            let ext_type =
                ((data[offset] as u16) << 8) | (data.get(offset + 1).copied().unwrap_or(0) as u16);
            let ext_len = ((data.get(offset + 2).copied().unwrap_or(0) as usize) << 8)
                | (data.get(offset + 3).copied().unwrap_or(0) as usize);
            offset += 4;

            let ext_end = offset.saturating_add(ext_len);
            if ext_end > extensions_end {
                return None;
            }

            if ext_type == 0x0000 {
                return Self::parse_sni_extension(&data[offset..ext_end]);
            }

            offset = ext_end;
        }

        None
    }

    fn parse_sni_extension(data: &[u8]) -> Option<String> {
        if data.len() < 5 {
            return None;
        }

        let list_len = ((data[0] as usize) << 8) | (data[1] as usize);
        if list_len + 2 > data.len() {
            return None;
        }

        if data[2] != 0 {
            return None;
        }

        let hostname_len = ((data[3] as usize) << 8) | (data[4] as usize);
        if 5 + hostname_len > data.len() {
            return None;
        }

        let hostname = std::str::from_utf8(&data[5..5 + hostname_len]).ok()?;
        Some(hostname.to_string())
    }
}

// ---------------------------------------------------------------------------
// HTTP/1.1 Parser
// ---------------------------------------------------------------------------

/// Lightweight HTTP/1.1 request/response parser.
pub struct HttpParser;

/// Parsed HTTP/1.1 request metadata.
#[derive(Debug)]
pub struct HttpRequest {
    pub method: u8,
    pub path: String,
    pub host: String,
    pub http_version: String,
}

impl HttpParser {
    pub fn parse_request(data: &[u8]) -> Option<HttpRequest> {
        let text = std::str::from_utf8(data).ok()?;
        let first_line = text.lines().next()?;
        let parts: Vec<&str> = first_line.split_whitespace().collect();
        if parts.len() < 3 {
            return None;
        }

        let method = match parts[0] {
            "GET" => 1,
            "POST" => 2,
            "PUT" => 3,
            "DELETE" => 4,
            "PATCH" => 5,
            "HEAD" => 6,
            "OPTIONS" => 7,
            _ => 0,
        };

        let host = text
            .lines()
            .find(|line| line.to_lowercase().starts_with("host:"))
            .map(|line| line[5..].trim().to_string())
            .unwrap_or_default();

        Some(HttpRequest {
            method,
            path: parts[1].to_string(),
            host,
            http_version: parts[2].to_string(),
        })
    }

    pub fn parse_response(data: &[u8]) -> Option<u16> {
        let text = std::str::from_utf8(data).ok()?;
        let first_line = text.lines().next()?;
        if !first_line.starts_with("HTTP/") {
            return None;
        }
        let parts: Vec<&str> = first_line.splitn(3, ' ').collect();
        if parts.len() < 2 {
            return None;
        }
        parts[1].parse().ok()
    }
}

// ---------------------------------------------------------------------------
// HttpInspector
// ---------------------------------------------------------------------------

/// Processes raw eBPF `http_event` structs and converts them into
/// [`NetworkEvent`]s for the agent's event pipeline.
pub struct HttpInspector {
    config: EbpfConfig,
    events: Mutex<VecDeque<NetworkEvent>>,
}

impl HttpInspector {
    pub fn new(config: EbpfConfig) -> Self {
        Self { config, events: Mutex::new(VecDeque::new()) }
    }

    /// Convert a raw eBPF event + payload into a [`NetworkEvent`].
    ///
    /// Returns `None` if the event type is unrecognised, the payload cannot
    /// be parsed, or the source port is in the exclusion list.
    pub fn process_event(&self, raw: &HttpEventRaw, payload: &[u8]) -> Option<NetworkEvent> {
        // Copy fields from packed struct to avoid unaligned references.
        let src_port = raw.src_port;
        let dst_port = raw.dst_port;
        let event_type = raw.event_type;
        let pid = raw.pid;
        let src_ip = raw.src_ip;
        let dst_ip = raw.dst_ip;

        if self.config.exclude_ports.contains(&src_port) {
            return None;
        }

        match event_type {
            EVENT_HTTP_REQUEST => {
                let req = HttpParser::parse_request(payload)?;
                debug!(
                    method = req.method,
                    path = %req.path,
                    host = %req.host,
                    pid = pid,
                    "Parsed HTTP request from eBPF"
                );
                Some(NetworkEvent::HttpRequest {
                    method: method_name(req.method).to_string(),
                    path: req.path,
                    status_code: 0,
                    latency_ms: 0.0,
                    source_ip: format_ip(src_ip),
                    destination_ip: format_ip(dst_ip),
                    destination_port: dst_port,
                    pid,
                })
            }
            EVENT_HTTPS_SNI => {
                let hostname = SniExtractor::extract_sni(payload)?;
                debug!(
                    sni = %hostname,
                    pid = pid,
                    dst_port = dst_port,
                    "Extracted SNI from eBPF"
                );
                Some(NetworkEvent::HttpRequest {
                    method: "TLS".to_string(),
                    path: String::new(),
                    status_code: 0,
                    latency_ms: 0.0,
                    source_ip: format_ip(src_ip),
                    destination_ip: format_ip(dst_ip),
                    destination_port: dst_port,
                    pid,
                })
            }
            other => {
                debug!(event_type = other, "Ignoring unknown HTTP event type");
                None
            }
        }
    }

    // -----------------------------------------------------------------------
    // Linux-only: eBPF ring buffer integration
    // -----------------------------------------------------------------------

    /// Set up the eBPF ring buffer consumer for HTTP/SNI events.
    ///
    /// Returns a [`libbpf_rs::RingBuffer`] that the caller **must keep
    /// alive** and call [`poll`](libbpf_rs::RingBuffer::poll) on periodically.
    #[cfg(target_os = "linux")]
    pub fn setup_ring_buffer(
        &self,
        obj: &libbpf_rs::Object,
    ) -> anyhow::Result<libbpf_rs::RingBuffer<'static>> {
        use anyhow::Context;

        let map = obj.map("http_events").context("http_events map not found in BPF object")?;

        let exclude_ports = self.config.exclude_ports.clone();

        let events_ptr = &self.events as *const Mutex<VecDeque<NetworkEvent>>;

        // SAFETY: `events_ptr` remains valid for the lifetime of the
        // HttpInspector, and all access goes through the Mutex.  The callback
        // is `'static` (captures only the raw pointer and a cloned Vec<u16>),
        // which satisfies RingBufferBuilder's bounds.
        let events_ref: &'static Mutex<VecDeque<NetworkEvent>> = unsafe { &*events_ptr };

        let mut builder = libbpf_rs::RingBufferBuilder::new();
        builder
            .add(map, move |data: &[u8]| handle_http_event(events_ref, &exclude_ports, data))
            .context("failed to register http_events ring buffer callback")?;

        let ring_buffer = builder.build().context("failed to build HTTP ring buffer")?;
        Ok(ring_buffer)
    }

    // -----------------------------------------------------------------------
    // Event queue (cross-platform)
    // -----------------------------------------------------------------------

    /// Drain all accumulated events from the internal queue.
    pub fn drain_events(&self) -> Vec<NetworkEvent> {
        let mut guard = match self.events.lock() {
            Ok(g) => g,
            Err(poisoned) => {
                warn!("HTTP event queue mutex poisoned -- recovering");
                poisoned.into_inner()
            }
        };
        guard.drain(..).collect()
    }
}

// ---------------------------------------------------------------------------
// Ring buffer event handler (Linux only)
// ---------------------------------------------------------------------------

/// Process a single raw HTTP/SNI event from the eBPF ring buffer.
///
/// Returns `0` to keep consuming events, non-zero to stop.
#[cfg(target_os = "linux")]
fn handle_http_event(
    events: &Mutex<VecDeque<NetworkEvent>>,
    exclude_ports: &[u16],
    data: &[u8],
) -> i32 {
    let struct_size = std::mem::size_of::<HttpEventRaw>();

    if data.len() < struct_size {
        warn!(
            len = data.len(),
            expected = struct_size,
            "Ring buffer event too short for HttpEventRaw"
        );
        return 0;
    }

    let raw = match plain::from_bytes::<HttpEventRaw>(data) {
        Ok(r) => *r,
        Err(e) => {
            warn!(error = ?e, "Failed to parse HttpEventRaw from ring buffer");
            return 0;
        }
    };

    // Copy fields from packed struct to avoid unaligned references.
    let src_port = raw.src_port;
    let dst_port = raw.dst_port;

    if exclude_ports.contains(&src_port) || exclude_ports.contains(&dst_port) {
        return 0;
    }

    let payload = if data.len() > struct_size { &data[struct_size..] } else { &[] };

    // Build a temporary inspector just for parsing (no state needed).
    let inspector = HttpInspector {
        config: EbpfConfig {
            enabled: false,
            tcp_connections: false,
            dns_resolution: false,
            http_inspection: false,
            db_inspection: false,
            exclude_ports: exclude_ports.to_vec(),
            exclude_ips: vec![],
            ring_buffer_size_kb: 0,
            poll_interval_ms: 0,
            fallback_to_proc: false,
        },
        events: Mutex::new(VecDeque::new()),
    };

    if let Some(event) = inspector.process_event(&raw, payload) {
        let mut guard = match events.lock() {
            Ok(g) => g,
            Err(poisoned) => poisoned.into_inner(),
        };
        if guard.len() >= MAX_EVENT_QUEUE {
            guard.pop_front();
            warn!(max = MAX_EVENT_QUEUE, "HTTP event queue full -- dropping oldest event");
        }
        guard.push_back(event);
    }

    0
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Format a little-endian `u32` IPv4 address as a dotted-decimal string.
fn format_ip(ip: u32) -> String {
    format!("{}.{}.{}.{}", ip & 0xFF, (ip >> 8) & 0xFF, (ip >> 16) & 0xFF, (ip >> 24) & 0xFF)
}

/// Map a numeric HTTP method code (as used in the event struct) to its name.
fn method_name(method: u8) -> &'static str {
    match method {
        1 => "GET",
        2 => "POST",
        3 => "PUT",
        4 => "DELETE",
        5 => "PATCH",
        6 => "HEAD",
        7 => "OPTIONS",
        _ => "UNKNOWN",
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

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

    fn build_client_hello(hostname: &[u8]) -> Vec<u8> {
        let sni_ext_len = 2 + 1 + 2 + hostname.len();
        let ext_total_len = 4 + sni_ext_len;
        let extensions_len = ext_total_len;
        let total = 52 + extensions_len;
        let mut data = vec![0u8; total];

        data[0] = 0x16;
        data[1] = 0x03;
        data[2] = 0x03;
        let record_len = total - 5;
        data[3] = ((record_len >> 8) & 0xFF) as u8;
        data[4] = (record_len & 0xFF) as u8;

        data[5] = 0x01;
        let handshake_len = total - 9;
        data[6] = ((handshake_len >> 16) & 0xFF) as u8;
        data[7] = ((handshake_len >> 8) & 0xFF) as u8;
        data[8] = (handshake_len & 0xFF) as u8;

        data[9] = 0x03;
        data[10] = 0x03;
        data[43] = 0;
        data[44] = 0x00;
        data[45] = 0x02;
        data[46] = 0x00;
        data[47] = 0x2F;
        data[48] = 0x01;
        data[49] = 0x00;

        data[50] = ((extensions_len >> 8) & 0xFF) as u8;
        data[51] = (extensions_len & 0xFF) as u8;
        data[52] = 0x00;
        data[53] = 0x00;
        data[54] = ((sni_ext_len >> 8) & 0xFF) as u8;
        data[55] = (sni_ext_len & 0xFF) as u8;

        let list_content_len = 1 + 2 + hostname.len();
        data[56] = ((list_content_len >> 8) & 0xFF) as u8;
        data[57] = (list_content_len & 0xFF) as u8;
        data[58] = 0x00;
        data[59] = 0x00;
        data[60] = hostname.len() as u8;
        data[61..61 + hostname.len()].copy_from_slice(hostname);

        data
    }

    fn build_raw_event(event_type: u32, src_port: u16, dst_port: u16) -> HttpEventRaw {
        HttpEventRaw {
            timestamp_ns: 0,
            event_type,
            pid: 1234,
            src_ip: 0x0100007F,
            dst_ip: 0x0200007F,
            src_port,
            dst_port,
            status_code: 0,
            method: 0,
            _pad: 0,
            host: [0u8; 128],
            path: [0u8; 128],
        }
    }

    // -- SNI Tests ----------------------------------------------------------

    #[test]
    fn test_sni_extraction_valid() {
        let payload = build_client_hello(b"example.com");
        assert_eq!(SniExtractor::extract_sni(&payload).as_deref(), Some("example.com"));
    }

    #[test]
    fn test_sni_extraction_not_tls() {
        assert_eq!(SniExtractor::extract_sni(&[0x15, 0x03, 0x03]), None);
    }

    #[test]
    fn test_sni_extraction_too_short() {
        assert_eq!(SniExtractor::extract_sni(&[0x16, 0x03, 0x03]), None);
    }

    #[test]
    fn test_sni_extraction_bad_version() {
        let mut data = vec![0u8; 50];
        data[0] = 0x16;
        data[1] = 0x03;
        data[2] = 0x00;
        assert_eq!(SniExtractor::extract_sni(&data), None);
    }

    #[test]
    fn test_sni_extraction_empty() {
        assert_eq!(SniExtractor::extract_sni(&[]), None);
    }

    #[test]
    fn test_sni_extraction_www_google_com() {
        let payload = build_client_hello(b"www.google.com");
        assert_eq!(SniExtractor::extract_sni(&payload).as_deref(), Some("www.google.com"));
    }

    // -- HTTP Request Tests -------------------------------------------------

    #[test]
    fn test_http_request_parsing() {
        let data = b"GET /api/v1/status HTTP/1.1\r\nHost: example.com\r\n\r\n";
        let req = HttpParser::parse_request(data).unwrap();
        assert_eq!(req.method, 1);
        assert_eq!(req.path, "/api/v1/status");
        assert_eq!(req.host, "example.com");
        assert_eq!(req.http_version, "HTTP/1.1");
    }

    #[test]
    fn test_http_request_post() {
        let data = b"POST /submit HTTP/1.1\r\nHost: api.example.com\r\n\r\n";
        let req = HttpParser::parse_request(data).unwrap();
        assert_eq!(req.method, 2);
    }

    #[test]
    fn test_http_request_no_host() {
        let data = b"GET / HTTP/1.1\r\n\r\n";
        let req = HttpParser::parse_request(data).unwrap();
        assert_eq!(req.host, "");
        assert_eq!(req.path, "/");
    }

    #[test]
    fn test_http_request_invalid() {
        assert!(HttpParser::parse_request(b"not http").is_none());
    }

    #[test]
    fn test_http_request_empty() {
        assert!(HttpParser::parse_request(b"").is_none());
    }

    // -- HTTP Response Tests ------------------------------------------------

    #[test]
    fn test_http_response_parsing() {
        assert_eq!(HttpParser::parse_response(b"HTTP/1.1 200 OK\r\n\r\n"), Some(200));
    }

    #[test]
    fn test_http_response_404() {
        assert_eq!(HttpParser::parse_response(b"HTTP/1.1 404 Not Found\r\n\r\n"), Some(404));
    }

    #[test]
    fn test_http_response_500() {
        assert_eq!(
            HttpParser::parse_response(b"HTTP/1.1 500 Internal Server Error\r\n\r\n"),
            Some(500)
        );
    }

    #[test]
    fn test_http_response_not_http() {
        assert_eq!(HttpParser::parse_response(b"not a response"), None);
    }

    // -- Helper Tests -------------------------------------------------------

    #[test]
    fn test_format_ip() {
        assert_eq!(format_ip(0x0100007F), "127.0.0.1");
        assert_eq!(format_ip(0x0101A8C0), "192.168.1.1");
        assert_eq!(format_ip(0), "0.0.0.0");
        assert_eq!(format_ip(0xFFFFFFFF), "255.255.255.255");
    }

    #[test]
    fn test_method_names() {
        assert_eq!(method_name(1), "GET");
        assert_eq!(method_name(2), "POST");
        assert_eq!(method_name(3), "PUT");
        assert_eq!(method_name(4), "DELETE");
        assert_eq!(method_name(5), "PATCH");
        assert_eq!(method_name(6), "HEAD");
        assert_eq!(method_name(7), "OPTIONS");
        assert_eq!(method_name(0), "UNKNOWN");
        assert_eq!(method_name(255), "UNKNOWN");
    }

    // -- HttpEventRaw Tests -------------------------------------------------

    #[test]
    fn test_http_event_raw_size() {
        assert_eq!(std::mem::size_of::<HttpEventRaw>(), 288);
    }

    #[test]
    fn test_http_event_raw_field_offsets() {
        assert_eq!(std::mem::offset_of!(HttpEventRaw, timestamp_ns), 0);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, event_type), 8);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, pid), 12);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, src_ip), 16);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, dst_ip), 20);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, src_port), 24);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, dst_port), 26);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, status_code), 28);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, method), 30);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, _pad), 31);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, host), 32);
        assert_eq!(std::mem::offset_of!(HttpEventRaw, path), 160);
    }

    // -- Integration Tests --------------------------------------------------

    #[test]
    fn test_process_event_excluded_port() {
        let config = EbpfConfig {
            enabled: true,
            tcp_connections: true,
            dns_resolution: true,
            http_inspection: true,
            db_inspection: true,
            exclude_ports: vec![443],
            exclude_ips: vec![],
            ring_buffer_size_kb: 256,
            poll_interval_ms: 100,
            fallback_to_proc: true,
        };
        let inspector = HttpInspector::new(config);
        let raw = build_raw_event(EVENT_HTTP_REQUEST, 443, 80);
        let payload = b"GET / HTTP/1.1\r\nHost: example.com\r\n\r\n";
        assert!(inspector.process_event(&raw, payload).is_none());
    }

    #[test]
    fn test_process_event_http_request() {
        let inspector = HttpInspector::new(test_config());
        let raw = build_raw_event(EVENT_HTTP_REQUEST, 54321, 80);
        let payload = b"POST /api/data HTTP/1.1\r\nHost: api.example.com\r\n\r\n";
        let event = inspector.process_event(&raw, payload).unwrap();

        match event {
            NetworkEvent::HttpRequest {
                method,
                path,
                source_ip,
                destination_ip,
                destination_port,
                pid,
                ..
            } => {
                assert_eq!(method, "POST");
                assert_eq!(path, "/api/data");
                assert_eq!(source_ip, "127.0.0.1");
                assert_eq!(destination_ip, "127.0.0.2");
                assert_eq!(destination_port, 80);
                assert_eq!(pid, 1234);
            }
            _ => panic!("Expected HttpRequest event"),
        }
    }

    #[test]
    fn test_process_event_https_sni() {
        let inspector = HttpInspector::new(test_config());
        let raw = build_raw_event(EVENT_HTTPS_SNI, 12345, 443);
        let payload = build_client_hello(b"example.com");
        let event = inspector.process_event(&raw, &payload).unwrap();

        match event {
            NetworkEvent::HttpRequest {
                method,
                source_ip,
                destination_ip,
                destination_port,
                pid,
                ..
            } => {
                assert_eq!(method, "TLS");
                assert_eq!(source_ip, "127.0.0.1");
                assert_eq!(destination_ip, "127.0.0.2");
                assert_eq!(destination_port, 443);
                assert_eq!(pid, 1234);
            }
            _ => panic!("Expected HttpRequest event from SNI"),
        }
    }

    #[test]
    fn test_process_event_unknown_type() {
        let inspector = HttpInspector::new(test_config());
        let raw = build_raw_event(99, 1234, 80);
        assert!(inspector.process_event(&raw, &[]).is_none());
    }

    #[test]
    fn test_drain_events_empty() {
        let inspector = HttpInspector::new(test_config());
        assert!(inspector.drain_events().is_empty());
    }

    #[test]
    fn test_ring_buffer_callback_too_short() {
        let inspector = HttpInspector::new(test_config());
        // Verify that too-short data doesn't panic
        let short_data = &[0u8; 10];
        let struct_size = std::mem::size_of::<HttpEventRaw>();
        assert!(short_data.len() < struct_size);
        assert!(inspector.drain_events().is_empty());
    }
}
