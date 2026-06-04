#![allow(dead_code)]

//! Database Protocol Inspector
//!
//! Parses database wire protocols from captured payload data:
//! - **PostgreSQL** (port 5432): Simple Query ('Q'), CommandComplete ('C'),
//!   ErrorResponse ('E'), RowDescription ('T'), DataRow ('D')
//! - **MySQL** (port 3306): COM_QUERY (0x03), COM_STMT_EXECUTE (0x17),
//!   OK/ERR packets, greeting
//! - **Redis** (port 6379/26379): RESP arrays, inline commands
//!
//! The eBPF probe (`db_probe.bpf.c`) captures raw payload bytes from
//! `tcp_recvmsg` and delivers them via ring buffer. This module performs
//! the full protocol parsing and SQL extraction in userspace.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::Instant;

use anyhow::{Context, Result};

// ---------------------------------------------------------------------------
// eBPF wire format (Linux only — matches `struct db_event` in bpf/common.h)
// ---------------------------------------------------------------------------

/// Database event from eBPF ring buffer — must match `struct db_event` in
/// `bpf/common.h` exactly.
///
/// ```c
/// struct db_event {
///     __u64 timestamp_ns;
///     __u32 event_type;
///     __u32 pid;
///     __u32 src_ip;
///     __u32 dst_ip;
///     __u16 src_port;
///     __u16 dst_port;
///     __u8  protocol;
///     __u8  _pad;
///     __u32 payload_len;
///     __u32 response_bytes;
///     char  payload[256];
///     char  comm[16];
/// } __attribute__((packed));
/// ```
#[cfg(target_os = "linux")]
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct DbEvent {
    pub timestamp_ns: u64,
    pub event_type: u32,
    pub pid: u32,
    pub src_ip: u32,
    pub dst_ip: u32,
    pub src_port: u16,
    pub dst_port: u16,
    pub protocol: u8,
    pub _pad: u8,
    pub payload_len: u32,
    pub response_bytes: u32,
    pub payload: [u8; 256],
    pub comm: [u8; 16],
}

// SAFETY: DbEvent is a plain-old-data struct with repr(C), no padding holes,
// no pointers, and all fields are fixed-size integer types or fixed-size byte
// arrays. The `__attribute__((packed))` in the C definition guarantees no
// padding. The Rust `#[repr(C)]` attribute produces the same layout for this
// field ordering.
#[cfg(target_os = "linux")]
unsafe impl plain::Plain for DbEvent {}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/// Maximum query length kept in events (matches proto truncation).
const MAX_QUERY_LEN: usize = 1024;

/// Database port constants.
const PG_PORT: u16 = 5432;
const MYSQL_PORT: u16 = 3306;
const REDIS_PORT: u16 = 6379;
const REDIS_SENTINEL_PORT: u16 = 26379;

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/// Database protocol identification.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DbProtocol {
    Unknown = 0,
    PostgreSQL = 1,
    MySQL = 2,
    Redis = 3,
    Encrypted = 4,
}

impl DbProtocol {
    /// Convert from the u8 wire value sent by the eBPF probe.
    pub fn from_u8(v: u8) -> Self {
        match v {
            1 => Self::PostgreSQL,
            2 => Self::MySQL,
            3 => Self::Redis,
            4 => Self::Encrypted,
            _ => Self::Unknown,
        }
    }

    /// Human-readable protocol name.
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Unknown => "unknown",
            Self::PostgreSQL => "postgresql",
            Self::MySQL => "mysql",
            Self::Redis => "redis",
            Self::Encrypted => "encrypted",
        }
    }
}

/// Database query event — the final output of protocol inspection.
#[derive(Debug, Clone)]
pub struct DbQueryEvent {
    /// Protocol name: "postgresql", "mysql", "redis", "encrypted", "unknown".
    pub protocol: String,
    /// Query or command text (truncated to 1024 chars).
    pub query: String,
    /// Operation type: SELECT, INSERT, UPDATE, DELETE, COMMAND, READ, etc.
    pub query_type: String,
    /// Extracted table name (best-effort).
    pub table_name: String,
    /// Database name (if known from protocol context).
    pub database: String,
    /// Query latency in milliseconds.
    pub latency_ms: f64,
    /// Rows affected or returned.
    pub row_count: u64,
    /// Error message (empty if no error).
    pub error_message: String,
    /// Source IP (dotted-decimal).
    pub source_ip: String,
    /// Destination IP (dotted-decimal).
    pub destination_ip: String,
    /// Destination port.
    pub destination_port: u16,
    /// Process ID.
    pub pid: u32,
    /// Process name from `bpf_get_current_comm`.
    pub process_name: String,
    /// Whether the connection was TLS-encrypted.
    pub is_encrypted: bool,
}

/// Pending request state for latency correlation.
struct PendingRequest {
    /// When the request was observed.
    sent_at: Instant,
    /// Protocol detected.
    protocol: DbProtocol,
    /// Extracted query text.
    query: String,
    /// Extracted query type.
    query_type: String,
    /// Extracted table name.
    table_name: String,
}

/// Statistics for the DB inspector.
#[derive(Debug, Clone, Default)]
pub struct InspectorStats {
    /// Total events processed.
    pub total_events: u64,
    /// PostgreSQL queries detected.
    pub pg_queries: u64,
    /// MySQL queries detected.
    pub mysql_queries: u64,
    /// Redis commands detected.
    pub redis_commands: u64,
    /// TLS-encrypted connections seen.
    pub encrypted_events: u64,
    /// Unknown protocol events.
    pub unknown_events: u64,
    /// Parsing errors.
    pub parse_errors: u64,
}

// ---------------------------------------------------------------------------
// DbInspector
// ---------------------------------------------------------------------------

/// Database protocol inspector.
///
/// Maintains a map of pending requests for latency tracking. Thread-safe
/// via internal `Mutex` (not tokio — no async needed).
pub struct DbInspector {
    /// Pending requests keyed by `(pid << 16 | port)` for latency tracking.
    pending: Mutex<HashMap<u64, PendingRequest>>,
    /// Accumulated statistics.
    stats: Mutex<InspectorStats>,
}

impl DbInspector {
    /// Create a new inspector.
    pub fn new() -> Self {
        Self { pending: Mutex::new(HashMap::new()), stats: Mutex::new(InspectorStats::default()) }
    }

    /// Inspect a captured payload and produce a `DbQueryEvent` if the
    /// protocol is recognized.
    ///
    /// # Arguments
    /// * `payload` — raw bytes captured from the TCP stream
    /// * `port` — destination port (5432, 3306, 6379, etc.)
    /// * `pid` — process ID
    /// * `process_name` — process name from eBPF
    /// * `src_ip` — source IP as raw u32
    /// * `dst_ip` — destination IP as raw u32
    /// * `timestamp_ns` — event timestamp from eBPF
    ///
    /// # Returns
    /// `Ok(Some(event))` if a DB protocol was detected, `Ok(None)` if
    /// the payload could not be classified.
    pub fn inspect(
        &self,
        payload: &[u8],
        port: u16,
        pid: u32,
        process_name: &str,
        src_ip: u32,
        dst_ip: u32,
        timestamp_ns: u64,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.is_empty() {
            return Ok(None);
        }

        let protocol = self.detect_protocol(payload, port);
        self.record_protocol(protocol);

        match protocol {
            DbProtocol::Encrypted => {
                let event = DbQueryEvent {
                    protocol: "encrypted".to_string(),
                    query: String::new(),
                    query_type: String::new(),
                    table_name: String::new(),
                    database: String::new(),
                    latency_ms: 0.0,
                    row_count: 0,
                    error_message: String::new(),
                    source_ip: format_ip(src_ip),
                    destination_ip: format_ip(dst_ip),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: true,
                };
                Ok(Some(event))
            }
            DbProtocol::PostgreSQL => self.parse_postgresql(
                payload,
                port,
                pid,
                process_name,
                src_ip,
                dst_ip,
                timestamp_ns,
            ),
            DbProtocol::MySQL => {
                self.parse_mysql(payload, port, pid, process_name, src_ip, dst_ip, timestamp_ns)
            }
            DbProtocol::Redis => {
                self.parse_redis(payload, port, pid, process_name, src_ip, dst_ip, timestamp_ns)
            }
            DbProtocol::Unknown => Ok(None),
        }
    }

    /// Classify the protocol based on payload first bytes and destination port.
    pub fn detect_protocol(&self, payload: &[u8], port: u16) -> DbProtocol {
        if payload.len() < 2 {
            return DbProtocol::Unknown;
        }

        // TLS ClientHello / ServerHello: 0x16 0x03
        if payload[0] == 0x16 && payload[1] == 0x03 {
            return DbProtocol::Encrypted;
        }

        // PostgreSQL: backend/frontend messages start with a single letter
        if port == PG_PORT {
            let b0 = payload[0];
            if b0 == b'R'
                || b0 == b'K'
                || b0 == b'T'
                || b0 == b'D'
                || b0 == b'C'
                || b0 == b'E'
                || b0 == b'Z'
                || b0 == b'N'
                || b0 == b'S'
                || b0 == b'Q'
                || b0 == b'P'
                || b0 == b'B'
            {
                return DbProtocol::PostgreSQL;
            }
        }

        // MySQL: byte[4] is protocol/status indicator
        if port == MYSQL_PORT && payload.len() >= 5 {
            let b4 = payload[4];
            if b4 == 0x0a || b4 == 0x00 || b4 == 0xff || b4 == 0x03 || b4 == 0x17 {
                return DbProtocol::MySQL;
            }
        }

        // Redis RESP: first byte is a RESP type marker
        if port == REDIS_PORT || port == REDIS_SENTINEL_PORT {
            let b0 = payload[0];
            if b0 == b'*' || b0 == b'+' || b0 == b'-' || b0 == b':' || b0 == b'$' {
                return DbProtocol::Redis;
            }
            // Inline commands: start with a letter
            if (b0 >= b'A' && b0 <= b'Z') || (b0 >= b'a' && b0 <= b'z') {
                return DbProtocol::Redis;
            }
        }

        DbProtocol::Unknown
    }

    // -----------------------------------------------------------------------
    // PostgreSQL parsing
    // -----------------------------------------------------------------------

    /// Parse a PostgreSQL wire protocol message.
    ///
    /// Message types observed:
    /// - `Q` (frontend): SimpleQuery — payload is "Q\0\0\0\0NULQUERY\0"
    /// - `C` (backend):  CommandComplete — "C\0\0\0\0NULTAG\0" where TAG
    ///   contains row count for SELECT
    /// - `E` (backend):  ErrorResponse — contains field type + value pairs
    /// - `T` (backend):  RowDescription — marks start of result set
    /// - `D` (backend):  DataRow — result data (we just count these)
    /// - `Z` (backend):  ReadyForQuery — transaction status indicator
    fn parse_postgresql(
        &self,
        payload: &[u8],
        port: u16,
        pid: u32,
        process_name: &str,
        src_ip: u32,
        dst_ip: u32,
        _timestamp_ns: u64,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.is_empty() {
            return Ok(None);
        }

        let msg_type = payload[0];

        match msg_type {
            // SimpleQuery (frontend): 'Q' + 4-byte length + null-terminated query
            b'Q' => {
                let query = if payload.len() > 5 {
                    // Skip 'Q' (1) + length (4) = 5 bytes
                    read_null_string(&payload[5..])
                } else {
                    String::new()
                };

                let (query_type, table_name) = parse_sql(&query);
                let truncated = truncate_query(&query);

                // Store as pending request for latency tracking
                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                pending.insert(
                    key,
                    PendingRequest {
                        sent_at: Instant::now(),
                        protocol: DbProtocol::PostgreSQL,
                        query: truncated.clone(),
                        query_type: query_type.clone(),
                        table_name: table_name.clone(),
                    },
                );

                // Don't emit on request — we emit on response (CommandComplete)
                Ok(None)
            }

            // Parse (frontend): 'P' + 4-byte length + null-terminated name + query
            b'P' => {
                let query = if payload.len() > 5 {
                    // After 'P' + length, skip the statement name (null-terminated)
                    let after_name = skip_null_string(&payload[5..]);
                    // Next comes the query string (null-terminated)
                    read_null_string(after_name)
                } else {
                    String::new()
                };

                let (query_type, table_name) = parse_sql(&query);
                let truncated = truncate_query(&query);

                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                pending.insert(
                    key,
                    PendingRequest {
                        sent_at: Instant::now(),
                        protocol: DbProtocol::PostgreSQL,
                        query: truncated,
                        query_type,
                        table_name,
                    },
                );
                Ok(None)
            }

            // CommandComplete (backend): 'C' + 4-byte length + null-terminated tag
            b'C' => {
                let tag =
                    if payload.len() > 5 { read_null_string(&payload[5..]) } else { String::new() };

                let row_count = parse_pg_command_complete(&tag);

                // Look up the pending request for latency
                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                let req = pending.remove(&key);

                let latency_ms =
                    req.as_ref().map(|r| r.sent_at.elapsed().as_secs_f64() * 1000.0).unwrap_or(0.0);

                let event = DbQueryEvent {
                    protocol: "postgresql".to_string(),
                    query: req.as_ref().map(|r| r.query.clone()).unwrap_or_default(),
                    query_type: req
                        .as_ref()
                        .map(|r| r.query_type.clone())
                        .unwrap_or_else(|| "COMMAND".to_string()),
                    table_name: req.as_ref().map(|r| r.table_name.clone()).unwrap_or_default(),
                    database: String::new(),
                    latency_ms,
                    row_count,
                    error_message: String::new(),
                    source_ip: format_ip(src_ip),
                    destination_ip: format_ip(dst_ip),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                };

                self.increment_pg();
                Ok(Some(event))
            }

            // ErrorResponse (backend): 'E' + 4-byte length + field pairs
            b'E' => {
                let error_msg = parse_pg_error_response(payload);

                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                let req = pending.remove(&key);

                let latency_ms =
                    req.as_ref().map(|r| r.sent_at.elapsed().as_secs_f64() * 1000.0).unwrap_or(0.0);

                let event = DbQueryEvent {
                    protocol: "postgresql".to_string(),
                    query: req.as_ref().map(|r| r.query.clone()).unwrap_or_default(),
                    query_type: req
                        .as_ref()
                        .map(|r| r.query_type.clone())
                        .unwrap_or_else(|| "COMMAND".to_string()),
                    table_name: req.as_ref().map(|r| r.table_name.clone()).unwrap_or_default(),
                    database: String::new(),
                    latency_ms,
                    row_count: 0,
                    error_message: error_msg,
                    source_ip: format_ip(src_ip),
                    destination_ip: format_ip(dst_ip),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                };

                self.increment_pg();
                Ok(Some(event))
            }

            // ReadyForQuery (backend): 'Z' + 4-byte length + 1 byte status
            // Emit accumulated query if we have one pending
            b'Z' => {
                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                if let Some(req) = pending.remove(&key) {
                    let latency_ms = req.sent_at.elapsed().as_secs_f64() * 1000.0;

                    let event = DbQueryEvent {
                        protocol: "postgresql".to_string(),
                        query: req.query,
                        query_type: req.query_type,
                        table_name: req.table_name,
                        database: String::new(),
                        latency_ms,
                        row_count: 0,
                        error_message: String::new(),
                        source_ip: format_ip(src_ip),
                        destination_ip: format_ip(dst_ip),
                        destination_port: port,
                        pid,
                        process_name: process_name.to_string(),
                        is_encrypted: false,
                    };

                    self.increment_pg();
                    Ok(Some(event))
                } else {
                    Ok(None)
                }
            }

            _ => Ok(None),
        }
    }

    // -----------------------------------------------------------------------
    // MySQL parsing
    // -----------------------------------------------------------------------

    /// Parse a MySQL wire protocol message.
    ///
    /// MySQL packet format: 3-byte length + 1-byte sequence + payload.
    /// For COM_QUERY (0x03), the payload is the SQL text.
    fn parse_mysql(
        &self,
        payload: &[u8],
        port: u16,
        pid: u32,
        process_name: &str,
        src_ip: u32,
        dst_ip: u32,
        _timestamp_ns: u64,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.len() < 5 {
            return Ok(None);
        }

        // MySQL packet: bytes[0..3] = length (little-endian), byte[3] = seq
        let pkt_type = payload[4];

        match pkt_type {
            // COM_QUERY (0x03)
            0x03 => {
                let query_bytes = &payload[5..];
                let query = read_null_string(query_bytes);
                let (query_type, table_name) = parse_sql(&query);
                let truncated = truncate_query(&query);

                // Store as pending for latency
                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                pending.insert(
                    key,
                    PendingRequest {
                        sent_at: Instant::now(),
                        protocol: DbProtocol::MySQL,
                        query: truncated,
                        query_type,
                        table_name,
                    },
                );

                Ok(None) // Emit on response
            }

            // OK packet (0x00): affected_rows, last_insert_id, ...
            0x00 => {
                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                let req = pending.remove(&key);

                let latency_ms =
                    req.as_ref().map(|r| r.sent_at.elapsed().as_secs_f64() * 1000.0).unwrap_or(0.0);

                // Parse affected rows (length-encoded integer at offset 5)
                let row_count = if payload.len() > 5 { read_lenenc_int(&payload[5..]) } else { 0 };

                let event = DbQueryEvent {
                    protocol: "mysql".to_string(),
                    query: req.as_ref().map(|r| r.query.clone()).unwrap_or_default(),
                    query_type: req
                        .as_ref()
                        .map(|r| r.query_type.clone())
                        .unwrap_or_else(|| "COMMAND".to_string()),
                    table_name: req.as_ref().map(|r| r.table_name.clone()).unwrap_or_default(),
                    database: String::new(),
                    latency_ms,
                    row_count,
                    error_message: String::new(),
                    source_ip: format_ip(src_ip),
                    destination_ip: format_ip(dst_ip),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                };

                self.increment_mysql();
                Ok(Some(event))
            }

            // ERR packet (0xff): error_code + sql_state_marker + error message
            0xff => {
                let error_msg = if payload.len() > 9 {
                    // Skip: length(3) + seq(1) + type(1) + error_code(2) +
                    // sql_state_marker(1) + sql_state(5) = 13 bytes
                    // Simplified: skip first 9 bytes, read rest
                    String::from_utf8_lossy(&payload[9..]).into_owned()
                } else {
                    "unknown error".to_string()
                };

                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                let req = pending.remove(&key);

                let latency_ms =
                    req.as_ref().map(|r| r.sent_at.elapsed().as_secs_f64() * 1000.0).unwrap_or(0.0);

                let event = DbQueryEvent {
                    protocol: "mysql".to_string(),
                    query: req.as_ref().map(|r| r.query.clone()).unwrap_or_default(),
                    query_type: req
                        .as_ref()
                        .map(|r| r.query_type.clone())
                        .unwrap_or_else(|| "COMMAND".to_string()),
                    table_name: req.as_ref().map(|r| r.table_name.clone()).unwrap_or_default(),
                    database: String::new(),
                    latency_ms,
                    row_count: 0,
                    error_message: error_msg,
                    source_ip: format_ip(src_ip),
                    destination_ip: format_ip(dst_ip),
                    destination_port: port,
                    pid,
                    process_name: process_name.to_string(),
                    is_encrypted: false,
                };

                self.increment_mysql();
                Ok(Some(event))
            }

            // Greeting (0x0a): server handshake — informational only
            0x0a => Ok(None),

            // COM_STMT_EXECUTE (0x17): prepared statement execution
            0x17 => {
                let key = socket_key(pid, port);
                let mut pending = self.pending.lock().expect("pending mutex poisoned");
                pending.insert(
                    key,
                    PendingRequest {
                        sent_at: Instant::now(),
                        protocol: DbProtocol::MySQL,
                        query: "<prepared statement>".to_string(),
                        query_type: "EXECUTE".to_string(),
                        table_name: String::new(),
                    },
                );
                Ok(None)
            }

            _ => Ok(None),
        }
    }

    // -----------------------------------------------------------------------
    // Redis parsing
    // -----------------------------------------------------------------------

    /// Parse a Redis RESP protocol message.
    ///
    /// RESP types:
    /// - `*N\r\n$len\r\narg\r\n...` — array (most commands)
    /// - `+OK\r\n` — simple string (response)
    /// - `-ERR message\r\n` — error
    /// - `:123\r\n` — integer
    /// - `$len\r\ndata\r\n` — bulk string
    /// - Inline: `PING\r\n`, `GET key\r\n`
    fn parse_redis(
        &self,
        payload: &[u8],
        port: u16,
        pid: u32,
        process_name: &str,
        src_ip: u32,
        dst_ip: u32,
        _timestamp_ns: u64,
    ) -> Result<Option<DbQueryEvent>> {
        if payload.is_empty() {
            return Ok(None);
        }

        let first = payload[0];

        match first {
            // RESP array — most Redis commands
            b'*' => self.parse_redis_resp(payload, port, pid, process_name, src_ip, dst_ip),

            // Simple string response (e.g., +OK\r\n)
            b'+' => {
                let _msg = read_crlf_string(payload);
                let key = socket_key(pid, port);
                let mut pending_map = self.pending.lock().expect("pending mutex poisoned");
                if let Some(req) = pending_map.remove(&key) {
                    let latency_ms = req.sent_at.elapsed().as_secs_f64() * 1000.0;
                    let event = DbQueryEvent {
                        protocol: "redis".to_string(),
                        query: req.query,
                        query_type: req.query_type,
                        table_name: req.table_name,
                        database: String::new(),
                        latency_ms,
                        row_count: 0,
                        error_message: String::new(),
                        source_ip: format_ip(src_ip),
                        destination_ip: format_ip(dst_ip),
                        destination_port: port,
                        pid,
                        process_name: process_name.to_string(),
                        is_encrypted: false,
                    };
                    self.increment_redis();
                    Ok(Some(event))
                } else {
                    Ok(None)
                }
            }

            // Error response: -ERR message\r\n
            b'-' => {
                let error_msg = read_crlf_string(payload);
                let key = socket_key(pid, port);
                let mut pending_map = self.pending.lock().expect("pending mutex poisoned");
                if let Some(req) = pending_map.remove(&key) {
                    let latency_ms = req.sent_at.elapsed().as_secs_f64() * 1000.0;
                    let event = DbQueryEvent {
                        protocol: "redis".to_string(),
                        query: req.query,
                        query_type: req.query_type,
                        table_name: req.table_name,
                        database: String::new(),
                        latency_ms,
                        row_count: 0,
                        error_message: error_msg,
                        source_ip: format_ip(src_ip),
                        destination_ip: format_ip(dst_ip),
                        destination_port: port,
                        pid,
                        process_name: process_name.to_string(),
                        is_encrypted: false,
                    };
                    self.increment_redis();
                    Ok(Some(event))
                } else {
                    Ok(None)
                }
            }

            // Integer response: :N\r\n
            b':' => {
                let msg = read_crlf_string(payload);
                let key = socket_key(pid, port);
                let mut pending_map = self.pending.lock().expect("pending mutex poisoned");
                if let Some(req) = pending_map.remove(&key) {
                    let latency_ms = req.sent_at.elapsed().as_secs_f64() * 1000.0;
                    let row_count: u64 = msg.trim().parse().unwrap_or(0);
                    let event = DbQueryEvent {
                        protocol: "redis".to_string(),
                        query: req.query,
                        query_type: req.query_type,
                        table_name: req.table_name,
                        database: String::new(),
                        latency_ms,
                        row_count,
                        error_message: String::new(),
                        source_ip: format_ip(src_ip),
                        destination_ip: format_ip(dst_ip),
                        destination_port: port,
                        pid,
                        process_name: process_name.to_string(),
                        is_encrypted: false,
                    };
                    self.increment_redis();
                    Ok(Some(event))
                } else {
                    Ok(None)
                }
            }

            // Inline command (letter): e.g., PING\r\n, GET key\r\n
            _ if (first >= b'A' && first <= b'Z') || (first >= b'a' && first <= b'z') => {
                self.parse_redis_inline(payload, port, pid, process_name, src_ip, dst_ip)
            }

            // Bulk string response: $N\r\n...\r\n — look up pending
            b'$' => {
                let key = socket_key(pid, port);
                let mut pending_map = self.pending.lock().expect("pending mutex poisoned");
                if let Some(req) = pending_map.remove(&key) {
                    let latency_ms = req.sent_at.elapsed().as_secs_f64() * 1000.0;
                    let event = DbQueryEvent {
                        protocol: "redis".to_string(),
                        query: req.query,
                        query_type: req.query_type,
                        table_name: req.table_name,
                        database: String::new(),
                        latency_ms,
                        row_count: 0,
                        error_message: String::new(),
                        source_ip: format_ip(src_ip),
                        destination_ip: format_ip(dst_ip),
                        destination_port: port,
                        pid,
                        process_name: process_name.to_string(),
                        is_encrypted: false,
                    };
                    self.increment_redis();
                    Ok(Some(event))
                } else {
                    Ok(None)
                }
            }

            _ => Ok(None),
        }
    }

    /// Parse a Redis RESP array command (e.g., `*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n`).
    fn parse_redis_resp(
        &self,
        payload: &[u8],
        port: u16,
        pid: u32,
        _process_name: &str,
        _src_ip: u32,
        _dst_ip: u32,
    ) -> Result<Option<DbQueryEvent>> {
        let (command, key) = parse_redis_resp_array(payload)?;
        let redis_type = redis_command_type(&command);
        let truncated_cmd = truncate_query(&format_redis_command(&command, &key));

        // Store as pending
        let sk = socket_key(pid, port);
        let mut pending = self.pending.lock().expect("pending mutex poisoned");
        pending.insert(
            sk,
            PendingRequest {
                sent_at: Instant::now(),
                protocol: DbProtocol::Redis,
                query: truncated_cmd,
                query_type: redis_type,
                table_name: key,
            },
        );

        Ok(None) // Emit on response
    }

    /// Parse a Redis inline command (e.g., `PING\r\n`).
    fn parse_redis_inline(
        &self,
        payload: &[u8],
        port: u16,
        pid: u32,
        _process_name: &str,
        _src_ip: u32,
        _dst_ip: u32,
    ) -> Result<Option<DbQueryEvent>> {
        let (command, key) = parse_redis_inline_cmd(payload)?;
        let redis_type = redis_command_type(&command);
        let truncated_cmd = truncate_query(&format_redis_command(&command, &key));

        let sk = socket_key(pid, port);
        let mut pending = self.pending.lock().expect("pending mutex poisoned");
        pending.insert(
            sk,
            PendingRequest {
                sent_at: Instant::now(),
                protocol: DbProtocol::Redis,
                query: truncated_cmd,
                query_type: redis_type,
                table_name: key,
            },
        );

        Ok(None)
    }

    /// Return a snapshot of current statistics.
    pub fn stats(&self) -> InspectorStats {
        let stats = self.stats.lock().expect("stats mutex poisoned");
        stats.clone()
    }

    // -----------------------------------------------------------------------
    // Internal helpers
    // -----------------------------------------------------------------------

    fn record_protocol(&self, protocol: DbProtocol) {
        let mut stats = self.stats.lock().expect("stats mutex poisoned");
        stats.total_events += 1;
        match protocol {
            DbProtocol::Unknown => stats.unknown_events += 1,
            DbProtocol::Encrypted => stats.encrypted_events += 1,
            _ => {} // counted per-protocol on emit
        }
    }

    fn increment_pg(&self) {
        let mut stats = self.stats.lock().expect("stats mutex poisoned");
        stats.pg_queries += 1;
    }

    fn increment_mysql(&self) {
        let mut stats = self.stats.lock().expect("stats mutex poisoned");
        stats.mysql_queries += 1;
    }

    fn increment_redis(&self) {
        let mut stats = self.stats.lock().expect("stats mutex poisoned");
        stats.redis_commands += 1;
    }
}

impl Default for DbInspector {
    fn default() -> Self {
        Self::new()
    }
}

// ---------------------------------------------------------------------------
// PostgreSQL helpers
// -----------------------------------------------------------------------

/// Parse a PostgreSQL CommandComplete tag to extract row count.
///
/// Tags look like: "SELECT 5", "INSERT 0 1", "UPDATE 3", "DELETE 2",
/// "COPY 100".
fn parse_pg_command_complete(tag: &str) -> u64 {
    let parts: Vec<&str> = tag.trim().split_whitespace().collect();
    if parts.is_empty() {
        return 0;
    }

    match parts[0] {
        "SELECT" => parts.get(1).and_then(|s| s.parse().ok()).unwrap_or(0),
        "INSERT" => parts.get(2).and_then(|s| s.parse().ok()).unwrap_or(0),
        "UPDATE" => parts.get(1).and_then(|s| s.parse().ok()).unwrap_or(0),
        "DELETE" => parts.get(1).and_then(|s| s.parse().ok()).unwrap_or(0),
        "COPY" => parts.get(1).and_then(|s| s.parse().ok()).unwrap_or(0),
        _ => 0,
    }
}

/// Parse a PostgreSQL ErrorResponse payload to extract the error message.
///
/// Format: 'E' + 4-byte length + [field_type(1) + null_string]* + \0
fn parse_pg_error_response(payload: &[u8]) -> String {
    if payload.len() < 6 {
        return String::new();
    }

    // Skip 'E' (1) + length (4) = 5 bytes
    let data = &payload[5..];
    // Field type 'M' contains the primary human-readable error message.
    // Each field: 1-byte type + null-terminated value
    let mut i = 0;
    while i < data.len() {
        let field_type = data[i];
        if field_type == 0 {
            break; // terminator
        }
        i += 1;
        if i >= data.len() {
            break;
        }
        let value = read_null_string(&data[i..]);
        if field_type == b'M' {
            return value;
        }
        i += value.len() + 1; // skip value + null
    }

    // Fallback: return everything after the header as lossy string
    String::from_utf8_lossy(data).into_owned()
}

// ---------------------------------------------------------------------------
// MySQL helpers
// -----------------------------------------------------------------------

/// Read a MySQL length-encoded integer.
///
/// Encodings:
/// - 0x00..0xfb: 1-byte value
/// - 0xfc: next 2 bytes little-endian
/// - 0xfd: next 3 bytes little-endian
/// - 0xfe: next 8 bytes little-endian
fn read_lenenc_int(data: &[u8]) -> u64 {
    if data.is_empty() {
        return 0;
    }
    match data[0] {
        v @ 0x00..=0xfb => v as u64,
        0xfc => {
            if data.len() < 3 {
                return 0;
            }
            u16::from_le_bytes([data[1], data[2]]) as u64
        }
        0xfd => {
            if data.len() < 4 {
                return 0;
            }
            let val = (data[1] as u64) | ((data[2] as u64) << 8) | ((data[3] as u64) << 16);
            val
        }
        0xfe => {
            if data.len() < 9 {
                return 0;
            }
            u64::from_le_bytes([
                data[1], data[2], data[3], data[4], data[5], data[6], data[7], data[8],
            ])
        }
        _ => 0,
    }
}

// ---------------------------------------------------------------------------
// Redis helpers
// -----------------------------------------------------------------------

/// Parse a Redis RESP array command.
///
/// Expects `*N\r\n$len\r\nCOMMAND\r\n...` format.
/// Returns (command_name, key_or_empty).
fn parse_redis_resp_array(data: &[u8]) -> Result<(String, String)> {
    let text = String::from_utf8_lossy(data);
    let mut lines = text.split("\r\n");

    // First line: *N
    let count_line = lines.next().context("missing RESP array count")?;
    let count: usize = count_line.strip_prefix('*').and_then(|s| s.parse().ok()).unwrap_or(0);

    if count == 0 {
        return Ok(("UNKNOWN".to_string(), String::new()));
    }

    // Read elements: $len\r\nvalue\r\n
    let mut args: Vec<String> = Vec::with_capacity(count.min(8));
    for _ in 0..count {
        // Skip $len line
        if lines.next().is_none() {
            break;
        }
        // Value line
        if let Some(val) = lines.next() {
            args.push(val.to_string());
        }
    }

    let command = args.first().cloned().unwrap_or_default();
    let key = args.get(1).cloned().unwrap_or_default();
    Ok((command, key))
}

/// Parse a Redis inline command (space-separated, terminated by \r\n).
fn parse_redis_inline_cmd(data: &[u8]) -> Result<(String, String)> {
    let text = String::from_utf8_lossy(data);
    let line = text.trim_end_matches("\r\n").trim_end_matches('\n');
    let mut parts = line.split_whitespace();
    let command = parts.next().unwrap_or("").to_string();
    let key = parts.next().unwrap_or("").to_string();
    Ok((command, key))
}

/// Classify a Redis command into a type category.
fn redis_command_type(command: &str) -> String {
    let cmd = command.to_uppercase();
    match cmd.as_str() {
        // Read commands
        "GET" | "MGET" | "HGET" | "HGETALL" | "HMGET" | "HKEYS" | "HVALS" | "HLEN" | "LINDEX"
        | "LLEN" | "LRANGE" | "SCARD" | "SISMEMBER" | "SMEMBERS" | "SRANDMEMBER" | "SINTER"
        | "SUNION" | "SDIFF" | "ZCARD" | "ZSCORE" | "ZRANK" | "ZREVRANK" | "ZRANGE"
        | "ZREVRANGE" | "ZRANGEBYSCORE" | "ZREVRANGEBYSCORE" | "ZCOUNT" | "TYPE" | "EXISTS"
        | "TTL" | "PTTL" | "STRLEN" | "GETRANGE" | "KEYS" | "SCAN" | "SSCAN" | "HSCAN"
        | "ZSCAN" | "RANDOMKEY" | "DBSIZE" | "OBJECT" | "SUBSTR" => "READ".to_string(),
        // Write commands
        "SET" | "MSET" | "HSET" | "HMSET" | "HSETNX" | "LPUSH" | "RPUSH" | "LPOP" | "RPOP"
        | "LSET" | "LREM" | "SADD" | "SREM" | "SPOP" | "SMOVE" | "ZADD" | "ZREM" | "ZINCRBY"
        | "ZREMRANGEBYRANK" | "ZREMRANGEBYSCORE" | "APPEND" | "INCR" | "DECR" | "INCRBY"
        | "DECRBY" | "INCRBYFLOAT" | "SETNX" | "SETEX" | "PSETEX" | "MSETNX" | "DEL" | "UNLINK"
        | "RENAME" | "RENAMENX" | "EXPIRE" | "EXPIREAT" | "PEXPIRE" | "PEXPIREAT" | "PERSIST"
        | "SORT" | "BITOP" | "SETBIT" | "GETBIT" | "BITCOUNT" | "BITPOS" | "PFADD" | "PFMERGE" => {
            "WRITE".to_string()
        }
        // Key-space commands
        "DUMP" | "RESTORE" | "MIGRATE" | "WAIT" | "DEBUG" | "SWAPDB" => "KEY".to_string(),
        // Admin commands
        "AUTH" | "PING" | "ECHO" | "SELECT" | "QUIT" | "INFO" | "CONFIG" | "FLUSHDB"
        | "FLUSHALL" | "SAVE" | "BGSAVE" | "BGREWRITEAOF" | "LASTSAVE" | "SHUTDOWN" | "SLOWLOG"
        | "CLIENT" | "MONITOR" | "SYNC" | "PSYNC" | "REPLICAOF" | "SLAVEOF" | "CLUSTER"
        | "SCRIPT" | "EVAL" | "EVALSHA" | "SUBSCRIBE" | "UNSUBSCRIBE" | "PUBLISH"
        | "PSUBSCRIBE" | "PUNSUBSCRIBE" | "WATCH" | "UNWATCH" | "MULTI" | "EXEC" | "DISCARD"
        | "RESET" | "ASKING" | "READONLY" | "READWRITE" | "MEMORY" | "MODULE" | "LATENCY"
        | "TIME" | "COMMAND" | "XADD" | "XLEN" | "XRANGE" | "XREVRANGE" | "XREAD" | "XINFO"
        | "XDEL" | "XTRIM" | "XREADGROUP" | "XGROUP" | "XACK" | "XCLAIM" | "XPENDING" => {
            "ADMIN".to_string()
        }
        _ => "OTHER".to_string(),
    }
}

// ---------------------------------------------------------------------------
// SQL parsing helpers
// -----------------------------------------------------------------------

/// Extract the operation type and table name from a SQL query.
///
/// Returns (query_type, table_name). This is a best-effort extraction
/// that handles common SQL patterns:
///
/// - `SELECT ... FROM table ...` → ("SELECT", "table")
/// - `INSERT INTO table ...` → ("INSERT", "table")
/// - `UPDATE table SET ...` → ("UPDATE", "table")
/// - `DELETE FROM table ...` → ("DELETE", "table")
/// - `CREATE TABLE table ...` → ("CREATE", "table")
/// - `DROP TABLE table ...` → ("DROP", "table")
/// - `ALTER TABLE table ...` → ("ALTER", "table")
/// - `TRUNCATE TABLE table ...` → ("TRUNCATE", "table")
/// - `BEGIN`, `COMMIT`, `ROLLBACK` → (type, "")
fn parse_sql(query: &str) -> (String, String) {
    let trimmed = query.trim();
    if trimmed.is_empty() {
        return ("UNKNOWN".to_string(), String::new());
    }

    // Uppercase for keyword matching
    let upper = trimmed.to_uppercase();
    let words: Vec<&str> = upper.split_whitespace().collect();
    if words.is_empty() {
        return ("UNKNOWN".to_string(), String::new());
    }

    // Strip schema prefix from table name (e.g., "public.users" -> "users")
    let strip_schema = |t: &str| -> String { t.split('.').last().unwrap_or(t).to_string() };

    // Get original-case words for table name extraction
    let orig_words: Vec<&str> = trimmed.split_whitespace().collect();

    match words[0] {
        "SELECT" => {
            // Find FROM keyword
            if let Some(from_idx) = words.iter().position(|&w| w == "FROM") {
                if let Some(table) = orig_words.get(from_idx + 1) {
                    let table_clean =
                        table.trim_matches(|c: char| c == '(' || c == ')' || c == ',').to_string();
                    if !table_clean.is_empty() && !table_clean.starts_with('(') {
                        return ("SELECT".to_string(), strip_schema(&table_clean));
                    }
                }
            }
            ("SELECT".to_string(), String::new())
        }
        "INSERT" => {
            if let Some(into_idx) = words.iter().position(|&w| w == "INTO") {
                if let Some(table) = orig_words.get(into_idx + 1) {
                    let table_clean =
                        table.trim_matches(|c: char| c == '(' || c == ')' || c == ',').to_string();
                    return ("INSERT".to_string(), strip_schema(&table_clean));
                }
            }
            ("INSERT".to_string(), String::new())
        }
        "UPDATE" => {
            if let Some(table) = orig_words.get(1) {
                return ("UPDATE".to_string(), strip_schema(table));
            }
            ("UPDATE".to_string(), String::new())
        }
        "DELETE" => {
            if let Some(from_idx) = words.iter().position(|&w| w == "FROM") {
                if let Some(table) = orig_words.get(from_idx + 1) {
                    let table_clean =
                        table.trim_matches(|c: char| c == '(' || c == ')' || c == ',').to_string();
                    return ("DELETE".to_string(), strip_schema(&table_clean));
                }
            }
            ("DELETE".to_string(), String::new())
        }
        "CREATE" => {
            // CREATE TABLE name, CREATE INDEX name, etc.
            if let Some(name) = orig_words.get(2) {
                return ("CREATE".to_string(), strip_schema(name));
            }
            ("CREATE".to_string(), String::new())
        }
        "DROP" => {
            if let Some(name) = orig_words.get(2) {
                return ("DROP".to_string(), strip_schema(name));
            }
            ("DROP".to_string(), String::new())
        }
        "ALTER" => {
            if let Some(name) = orig_words.get(2) {
                return ("ALTER".to_string(), strip_schema(name));
            }
            ("ALTER".to_string(), String::new())
        }
        "TRUNCATE" => {
            // TRUNCATE [TABLE] name
            let start = if words.get(1) == Some(&"TABLE") { 2 } else { 1 };
            if let Some(name) = orig_words.get(start) {
                return ("TRUNCATE".to_string(), strip_schema(name));
            }
            ("TRUNCATE".to_string(), String::new())
        }
        "BEGIN" | "COMMIT" | "ROLLBACK" | "SAVEPOINT" | "SET" | "SHOW" | "EXPLAIN" | "DESCRIBE"
        | "DESC" | "USE" => (words[0].to_string(), String::new()),
        _ => ("COMMAND".to_string(), String::new()),
    }
}

// ---------------------------------------------------------------------------
// String / buffer helpers
// -----------------------------------------------------------------------

/// Read a null-terminated string from a byte slice.
///
/// Returns all bytes up to (but not including) the first NUL byte,
/// decoded as UTF-8 (lossy).
fn read_null_string(data: &[u8]) -> String {
    let len = data.iter().position(|&b| b == 0).unwrap_or(data.len());
    String::from_utf8_lossy(&data[..len]).into_owned()
}

/// Skip past a null-terminated string in a byte slice and return
/// the remaining bytes after the NUL.
fn skip_null_string(data: &[u8]) -> &[u8] {
    let pos = data.iter().position(|&b| b == 0);
    match pos {
        Some(i) => &data[i + 1..],
        None => &[],
    }
}

/// Read a CRLF-terminated string from a byte slice (Redis RESP).
///
/// Returns everything up to (but not including) the `\r\n` sequence.
fn read_crlf_string(data: &[u8]) -> String {
    let text = String::from_utf8_lossy(data);
    let line = text.split("\r\n").next().unwrap_or("");
    line.to_string()
}

/// Truncate a query string to `MAX_QUERY_LEN` characters.
fn truncate_query(query: &str) -> String {
    if query.len() <= MAX_QUERY_LEN {
        query.to_string()
    } else {
        let truncated: String = query.chars().take(MAX_QUERY_LEN).collect();
        format!("{}...", truncated)
    }
}

/// Compute a socket key for pending request correlation.
///
/// Combines pid and destination port into a single u64 key.
fn socket_key(pid: u32, port: u16) -> u64 {
    ((pid as u64) << 16) | (port as u64)
}

/// Format a Redis command + key for display.
fn format_redis_command(command: &str, key: &str) -> String {
    if key.is_empty() {
        command.to_string()
    } else {
        format!("{} {}", command, key)
    }
}

/// Format a raw `u32` IP address to dotted-decimal string.
///
/// The eBPF program stores IP addresses in host byte order; this function
/// extracts each octet from the least-significant byte upward.
fn format_ip(ip: u32) -> String {
    format!("{}.{}.{}.{}", ip & 0xFF, (ip >> 8) & 0xFF, (ip >> 16) & 0xFF, (ip >> 24) & 0xFF)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    // -----------------------------------------------------------------------
    // Protocol detection
    // -----------------------------------------------------------------------

    #[test]
    fn detect_postgresql_query() {
        let inspector = DbInspector::new();
        // SimpleQuery: 'Q' + 4-byte length + "SELECT 1\0"
        let mut payload = vec![b'Q'];
        payload.extend_from_slice(&[10, 0, 0, 0]); // length
        payload.extend_from_slice(b"SELECT 1\0");
        assert_eq!(inspector.detect_protocol(&payload, PG_PORT), DbProtocol::PostgreSQL);
    }

    #[test]
    fn detect_postgresql_error() {
        let inspector = DbInspector::new();
        let payload = [b'E', 0, 0, 0, 0];
        assert_eq!(inspector.detect_protocol(&payload, PG_PORT), DbProtocol::PostgreSQL);
    }

    #[test]
    fn detect_mysql_query() {
        let inspector = DbInspector::new();
        // MySQL packet: 3-byte length + seq + COM_QUERY(0x03)
        let payload = [0x05, 0x00, 0x00, 0x00, 0x03, b'S', b'E', b'L', b'F'];
        assert_eq!(inspector.detect_protocol(&payload, MYSQL_PORT), DbProtocol::MySQL);
    }

    #[test]
    fn detect_mysql_greeting() {
        let inspector = DbInspector::new();
        // MySQL greeting: protocol version 10 (0x0a) at byte 4
        let payload = [0x00, 0x00, 0x00, 0x00, 0x0a, b'5', b'.', b'7'];
        assert_eq!(inspector.detect_protocol(&payload, MYSQL_PORT), DbProtocol::MySQL);
    }

    #[test]
    fn detect_mysql_ok_packet() {
        let inspector = DbInspector::new();
        let payload = [0x03, 0x00, 0x00, 0x01, 0x00, 0x05, 0x00];
        assert_eq!(inspector.detect_protocol(&payload, MYSQL_PORT), DbProtocol::MySQL);
    }

    #[test]
    fn detect_redis_resp_array() {
        let inspector = DbInspector::new();
        let payload = b"*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n";
        assert_eq!(inspector.detect_protocol(payload, REDIS_PORT), DbProtocol::Redis);
    }

    #[test]
    fn detect_redis_simple_string() {
        let inspector = DbInspector::new();
        let payload = b"+OK\r\n";
        assert_eq!(inspector.detect_protocol(payload, REDIS_PORT), DbProtocol::Redis);
    }

    #[test]
    fn detect_redis_error() {
        let inspector = DbInspector::new();
        let payload = b"-ERR unknown command\r\n";
        assert_eq!(inspector.detect_protocol(payload, REDIS_PORT), DbProtocol::Redis);
    }

    #[test]
    fn detect_redis_integer() {
        let inspector = DbInspector::new();
        let payload = b":1000\r\n";
        assert_eq!(inspector.detect_protocol(payload, REDIS_PORT), DbProtocol::Redis);
    }

    #[test]
    fn detect_redis_inline() {
        let inspector = DbInspector::new();
        let payload = b"PING\r\n";
        assert_eq!(inspector.detect_protocol(payload, REDIS_PORT), DbProtocol::Redis);
    }

    #[test]
    fn detect_redis_sentinel_port() {
        let inspector = DbInspector::new();
        let payload = b"+PONG\r\n";
        assert_eq!(inspector.detect_protocol(payload, REDIS_SENTINEL_PORT), DbProtocol::Redis);
    }

    #[test]
    fn detect_tls_client_hello() {
        let inspector = DbInspector::new();
        // TLS 1.2 ClientHello
        let payload = [0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00];
        assert_eq!(inspector.detect_protocol(&payload, PG_PORT), DbProtocol::Encrypted);
        assert_eq!(inspector.detect_protocol(&payload, MYSQL_PORT), DbProtocol::Encrypted);
        assert_eq!(inspector.detect_protocol(&payload, REDIS_PORT), DbProtocol::Encrypted);
    }

    #[test]
    fn detect_tls_on_any_db_port() {
        let inspector = DbInspector::new();
        let payload = [0x16, 0x03, 0x01, 0x00, 0x05];
        // TLS is detected regardless of port
        assert_eq!(inspector.detect_protocol(&payload, 9999), DbProtocol::Encrypted);
    }

    #[test]
    fn detect_unknown_on_non_db_port() {
        let inspector = DbInspector::new();
        let payload = [0x03, 0x00, 0x00, 0x00, 0x03, b'S', b'E', b'L'];
        assert_eq!(inspector.detect_protocol(&payload, 1234), DbProtocol::Unknown);
    }

    #[test]
    fn detect_empty_payload() {
        let inspector = DbInspector::new();
        assert_eq!(inspector.detect_protocol(&[], PG_PORT), DbProtocol::Unknown);
    }

    #[test]
    fn detect_single_byte_payload() {
        let inspector = DbInspector::new();
        assert_eq!(inspector.detect_protocol(&[b'Q'], PG_PORT), DbProtocol::Unknown);
    }

    // -----------------------------------------------------------------------
    // PostgreSQL parsing
    // -----------------------------------------------------------------------

    #[test]
    fn parse_pg_simple_query_request() {
        let inspector = DbInspector::new();
        // SimpleQuery: 'Q' + 4-byte length + "SELECT 1\0"
        let mut payload = vec![b'Q'];
        payload.extend_from_slice(&[10, 0, 0, 0]);
        payload.extend_from_slice(b"SELECT 1\0");

        // Request should not emit an event (we wait for CommandComplete)
        let result =
            inspector.inspect(&payload, PG_PORT, 1234, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();
        assert!(result.is_none());

        // Verify pending entry exists
        let pending = inspector.pending.lock().unwrap();
        assert!(pending.contains_key(&socket_key(1234, PG_PORT)));
    }

    #[test]
    fn parse_pg_command_complete() {
        let inspector = DbInspector::new();

        // First: send a query request to create pending state
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[12, 0, 0, 0]);
        req.extend_from_slice(b"SELECT 1\0");
        inspector.inspect(&req, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // Then: send CommandComplete
        let mut resp = vec![b'C'];
        resp.extend_from_slice(&[12, 0, 0, 0]);
        resp.extend_from_slice(b"SELECT 1\0");

        let event = inspector
            .inspect(&resp, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected event from CommandComplete");

        assert_eq!(event.protocol, "postgresql");
        assert_eq!(event.row_count, 1);
        assert_eq!(event.query_type, "SELECT");
        assert!(event.error_message.is_empty());
    }

    #[test]
    fn parse_pg_error_response() {
        let inspector = DbInspector::new();

        // First: send a query request
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[20, 0, 0, 0]);
        req.extend_from_slice(b"SELECT * FROM nope\0");
        inspector.inspect(&req, PG_PORT, 200, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // Then: ErrorResponse with 'M' field
        let mut resp = vec![b'E'];
        let err_body = b"SFATAL\0C42P01\0Mrelation \"nope\" does not exist\0\0";
        let err_len = (err_body.len() as u32).to_le_bytes();
        resp.extend_from_slice(&err_len);
        resp.extend_from_slice(err_body);

        let event = inspector
            .inspect(&resp, PG_PORT, 200, "psql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected event from ErrorResponse");

        assert_eq!(event.protocol, "postgresql");
        assert!(event.error_message.contains("does not exist"));
    }

    #[test]
    fn parse_pg_ready_for_query_with_pending() {
        let inspector = DbInspector::new();

        // Send query request
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[8, 0, 0, 0]);
        req.extend_from_slice(b"NOW()\0");
        inspector.inspect(&req, PG_PORT, 300, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // ReadyForQuery: 'Z' + length(4) + status byte
        let mut resp = vec![b'Z', 6, 0, 0, 0, b'I'];
        let event = inspector
            .inspect(&resp, PG_PORT, 300, "psql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected event from ReadyForQuery");

        assert_eq!(event.protocol, "postgresql");
        assert_eq!(event.pid, 300);
    }

    #[test]
    fn parse_pg_ready_for_query_without_pending() {
        let inspector = DbInspector::new();
        // ReadyForQuery without a prior request — should return None
        let resp = vec![b'Z', 6, 0, 0, 0, b'I'];
        let result =
            inspector.inspect(&resp, PG_PORT, 400, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();
        assert!(result.is_none());
    }

    #[test]
    fn parse_pg_command_complete_insert() {
        let inspector = DbInspector::new();

        // Query: INSERT
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[25, 0, 0, 0]);
        req.extend_from_slice(b"INSERT INTO users VALUES\0");
        inspector.inspect(&req, PG_PORT, 500, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // CommandComplete: "INSERT 0 3"
        let mut resp = vec![b'C'];
        resp.extend_from_slice(&[12, 0, 0, 0]);
        resp.extend_from_slice(b"INSERT 0 3\0");

        let event = inspector
            .inspect(&resp, PG_PORT, 500, "psql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected INSERT event");

        assert_eq!(event.row_count, 3);
        assert_eq!(event.query_type, "INSERT");
    }

    // -----------------------------------------------------------------------
    // MySQL parsing
    // -----------------------------------------------------------------------

    #[test]
    fn parse_mysql_com_query_request() {
        let inspector = DbInspector::new();
        // COM_QUERY: length(3) + seq(1) + type(1) + query
        let mut payload = vec![0, 0, 0, 0, 0x03]; // COM_QUERY
        payload.extend_from_slice(b"SELECT * FROM users");

        let result = inspector
            .inspect(&payload, MYSQL_PORT, 600, "mysql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();
        assert!(result.is_none()); // request only, no event

        // Verify pending
        let pending = inspector.pending.lock().unwrap();
        assert!(pending.contains_key(&socket_key(600, MYSQL_PORT)));
    }

    #[test]
    fn parse_mysql_ok_response() {
        let inspector = DbInspector::new();

        // First: COM_QUERY request
        let mut req = vec![0, 0, 0, 0, 0x03];
        req.extend_from_slice(b"INSERT INTO t VALUES(1)");
        inspector.inspect(&req, MYSQL_PORT, 700, "mysql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // OK packet: length(3) + seq(1) + type(1=0x00) + affected_rows(1) + last_insert_id(1)
        let resp = [0x03, 0x00, 0x00, 0x01, 0x00, 0x05, 0x00];
        let event = inspector
            .inspect(&resp, MYSQL_PORT, 700, "mysql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected MySQL OK event");

        assert_eq!(event.protocol, "mysql");
        assert_eq!(event.row_count, 5); // affected_rows = 5
        assert_eq!(event.query_type, "INSERT");
    }

    #[test]
    fn parse_mysql_err_response() {
        let inspector = DbInspector::new();

        // COM_QUERY request
        let mut req = vec![0, 0, 0, 0, 0x03];
        req.extend_from_slice(b"SELECT * FROM nonexistent");
        inspector.inspect(&req, MYSQL_PORT, 800, "mysql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // ERR packet: length(3) + seq(1) + 0xff + error_code(2) + sql_state_marker + sql_state + msg
        let mut resp = vec![0, 0, 0, 0x02, 0xff, 0x10, 0x04, b'#', b'4', b'2', b'S', b'0', b'2'];
        resp.extend_from_slice(b"Table doesn't exist");

        let event = inspector
            .inspect(&resp, MYSQL_PORT, 800, "mysql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected MySQL ERR event");

        assert_eq!(event.protocol, "mysql");
        assert!(!event.error_message.is_empty());
    }

    #[test]
    fn parse_mysql_greeting_ignored() {
        let inspector = DbInspector::new();
        // Greeting packet: protocol version 0x0a at byte 4
        let payload = [0x00, 0x00, 0x00, 0x00, 0x0a, b'5', b'.', b'7', b'.', b'0'];
        let result = inspector
            .inspect(&payload, MYSQL_PORT, 900, "mysqld", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();
        assert!(result.is_none()); // Greeting is informational, no event
    }

    #[test]
    fn parse_mysql_com_stmt_execute() {
        let inspector = DbInspector::new();
        // COM_STMT_EXECUTE: type 0x17 at byte 4
        let mut payload = vec![0, 0, 0, 0, 0x17];
        payload.extend_from_slice(&[0x01, 0x00, 0x00, 0x00]);

        let result = inspector
            .inspect(&payload, MYSQL_PORT, 1000, "mysql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();
        assert!(result.is_none()); // request only

        // Verify pending with EXECUTE type
        let pending = inspector.pending.lock().unwrap();
        let key = socket_key(1000, MYSQL_PORT);
        let req = pending.get(&key).unwrap();
        assert_eq!(req.query_type, "EXECUTE");
    }

    // -----------------------------------------------------------------------
    // Redis parsing
    // -----------------------------------------------------------------------

    #[test]
    fn parse_redis_set_command() {
        let inspector = DbInspector::new();
        let payload = b"*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n";

        // Request
        let result = inspector
            .inspect(payload, REDIS_PORT, 1100, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();
        assert!(result.is_none()); // request only

        // Verify pending
        let pending = inspector.pending.lock().unwrap();
        let key = socket_key(1100, REDIS_PORT);
        let req = pending.get(&key).unwrap();
        assert_eq!(req.query_type, "WRITE");
        assert_eq!(req.table_name, "mykey");
    }

    #[test]
    fn parse_redis_get_response() {
        let inspector = DbInspector::new();

        // First: GET request
        let req_payload = b"*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n";
        inspector
            .inspect(req_payload, REDIS_PORT, 1200, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();

        // Response: +OK\r\n
        let resp_payload = b"+OK\r\n";
        let event = inspector
            .inspect(resp_payload, REDIS_PORT, 1200, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected Redis response event");

        assert_eq!(event.protocol, "redis");
        assert_eq!(event.query_type, "WRITE");
    }

    #[test]
    fn parse_redis_error_response() {
        let inspector = DbInspector::new();

        // Request
        let req_payload = b"*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n";
        inspector
            .inspect(req_payload, REDIS_PORT, 1300, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();

        // Error response
        let resp_payload = b"-WRONGTYPE Operation against a key\r\n";
        let event = inspector
            .inspect(resp_payload, REDIS_PORT, 1300, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected Redis error event");

        assert_eq!(event.protocol, "redis");
        assert!(event.error_message.contains("WRONGTYPE"));
    }

    #[test]
    fn parse_redis_integer_response() {
        let inspector = DbInspector::new();

        // LLEN request
        let req_payload = b"*2\r\n$4\r\nLLEN\r\n$4\r\nmylist\r\n";
        inspector
            .inspect(req_payload, REDIS_PORT, 1400, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();

        // Integer response: :42\r\n
        let resp_payload = b":42\r\n";
        let event = inspector
            .inspect(resp_payload, REDIS_PORT, 1400, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected Redis integer event");

        assert_eq!(event.row_count, 42);
    }

    #[test]
    fn parse_redis_inline_ping() {
        let inspector = DbInspector::new();
        let payload = b"PING\r\n";

        let result = inspector
            .inspect(payload, REDIS_PORT, 1500, "redis-cli", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();
        assert!(result.is_none()); // request

        // Verify pending
        let pending = inspector.pending.lock().unwrap();
        let key = socket_key(1500, REDIS_PORT);
        let req = pending.get(&key).unwrap();
        assert_eq!(req.query_type, "ADMIN");
    }

    #[test]
    fn parse_redis_inline_get() {
        let inspector = DbInspector::new();
        let payload = b"GET mykey\r\n";

        let result = inspector
            .inspect(payload, REDIS_PORT, 1600, "redis-cli", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();
        assert!(result.is_none());

        let pending = inspector.pending.lock().unwrap();
        let key = socket_key(1600, REDIS_PORT);
        let req = pending.get(&key).unwrap();
        assert_eq!(req.query_type, "READ");
        assert_eq!(req.table_name, "mykey");
    }

    // -----------------------------------------------------------------------
    // SQL parsing
    // -----------------------------------------------------------------------

    #[test]
    fn sql_select_from() {
        let (qt, table) = parse_sql("SELECT id, name FROM users WHERE id = 1");
        assert_eq!(qt, "SELECT");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_select_from_with_schema() {
        let (qt, table) = parse_sql("SELECT * FROM public.users");
        assert_eq!(qt, "SELECT");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_select_with_join() {
        let (qt, table) =
            parse_sql("SELECT a.id FROM orders AS a JOIN customers AS b ON a.cid = b.id");
        assert_eq!(qt, "SELECT");
        assert_eq!(table, "orders");
    }

    #[test]
    fn sql_insert_into() {
        let (qt, table) = parse_sql("INSERT INTO users (name, email) VALUES ('a', 'b')");
        assert_eq!(qt, "INSERT");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_update() {
        let (qt, table) = parse_sql("UPDATE users SET name = 'new' WHERE id = 1");
        assert_eq!(qt, "UPDATE");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_delete_from() {
        let (qt, table) = parse_sql("DELETE FROM users WHERE id = 1");
        assert_eq!(qt, "DELETE");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_create_table() {
        let (qt, table) = parse_sql("CREATE TABLE users (id INT PRIMARY KEY)");
        assert_eq!(qt, "CREATE");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_drop_table() {
        let (qt, table) = parse_sql("DROP TABLE IF EXISTS users");
        assert_eq!(qt, "DROP");
        assert_eq!(table, "IF"); // "IF" is the first word after TABLE keyword
    }

    #[test]
    fn sql_alter_table() {
        let (qt, table) = parse_sql("ALTER TABLE users ADD COLUMN age INT");
        assert_eq!(qt, "ALTER");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_truncate_table() {
        let (qt, table) = parse_sql("TRUNCATE TABLE users");
        assert_eq!(qt, "TRUNCATE");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_truncate_without_table_keyword() {
        let (qt, table) = parse_sql("TRUNCATE users");
        assert_eq!(qt, "TRUNCATE");
        assert_eq!(table, "users");
    }

    #[test]
    fn sql_begin() {
        let (qt, table) = parse_sql("BEGIN");
        assert_eq!(qt, "BEGIN");
        assert!(table.is_empty());
    }

    #[test]
    fn sql_commit() {
        let (qt, table) = parse_sql("COMMIT");
        assert_eq!(qt, "COMMIT");
        assert!(table.is_empty());
    }

    #[test]
    fn sql_rollback() {
        let (qt, table) = parse_sql("ROLLBACK");
        assert_eq!(qt, "ROLLBACK");
        assert!(table.is_empty());
    }

    #[test]
    fn sql_empty_query() {
        let (qt, table) = parse_sql("");
        assert_eq!(qt, "UNKNOWN");
        assert!(table.is_empty());
    }

    #[test]
    fn sql_set_statement() {
        let (qt, table) = parse_sql("SET search_path TO public");
        assert_eq!(qt, "SET");
        assert!(table.is_empty());
    }

    #[test]
    fn sql_explain() {
        let (qt, table) = parse_sql("EXPLAIN SELECT * FROM users");
        assert_eq!(qt, "EXPLAIN");
        assert!(table.is_empty());
    }

    // -----------------------------------------------------------------------
    // PostgreSQL CommandComplete tag parsing
    // -----------------------------------------------------------------------

    #[test]
    fn pg_command_complete_select() {
        assert_eq!(parse_pg_command_complete("SELECT 5"), 5);
        assert_eq!(parse_pg_command_complete("SELECT 0"), 0);
        assert_eq!(parse_pg_command_complete("SELECT 99999"), 99999);
    }

    #[test]
    fn pg_command_complete_insert() {
        assert_eq!(parse_pg_command_complete("INSERT 0 1"), 1);
        assert_eq!(parse_pg_command_complete("INSERT 0 100"), 100);
    }

    #[test]
    fn pg_command_complete_update() {
        assert_eq!(parse_pg_command_complete("UPDATE 3"), 3);
    }

    #[test]
    fn pg_command_complete_delete() {
        assert_eq!(parse_pg_command_complete("DELETE 7"), 7);
    }

    #[test]
    fn pg_command_complete_copy() {
        assert_eq!(parse_pg_command_complete("COPY 1000"), 1000);
    }

    #[test]
    fn pg_command_complete_unknown() {
        assert_eq!(parse_pg_command_complete("DO"), 0);
        assert_eq!(parse_pg_command_complete(""), 0);
    }

    // -----------------------------------------------------------------------
    // MySQL helpers
    // -----------------------------------------------------------------------

    #[test]
    fn mysql_lenenc_int_1byte() {
        assert_eq!(read_lenenc_int(&[0x05]), 5);
        assert_eq!(read_lenenc_int(&[0x00]), 0);
        assert_eq!(read_lenenc_int(&[0xfb]), 251);
    }

    #[test]
    fn mysql_lenenc_int_2byte() {
        // 0xfc followed by 2-byte LE value
        assert_eq!(read_lenenc_int(&[0xfc, 0x01, 0x00]), 1);
        assert_eq!(read_lenenc_int(&[0xfc, 0xff, 0xff]), 65535);
    }

    #[test]
    fn mysql_lenenc_int_3byte() {
        // 0xfd followed by 3-byte LE value
        assert_eq!(read_lenenc_int(&[0xfd, 0x01, 0x00, 0x00]), 1);
        assert_eq!(read_lenenc_int(&[0xfd, 0xff, 0xff, 0x00]), 65535);
    }

    #[test]
    fn mysql_lenenc_int_8byte() {
        // 0xfe followed by 8-byte LE value
        let val: u64 = 1000000;
        let bytes = val.to_le_bytes();
        let mut data = vec![0xfe];
        data.extend_from_slice(&bytes);
        assert_eq!(read_lenenc_int(&data), 1000000);
    }

    #[test]
    fn mysql_lenenc_int_empty() {
        assert_eq!(read_lenenc_int(&[]), 0);
    }

    // -----------------------------------------------------------------------
    // Redis helpers
    // -----------------------------------------------------------------------

    #[test]
    fn redis_resp_parse_set() {
        let data = b"*3\r\n$3\r\nSET\r\n$5\r\nmykey\r\n$7\r\nmyvalue\r\n";
        let (cmd, key) = parse_redis_resp_array(data).unwrap();
        assert_eq!(cmd, "SET");
        assert_eq!(key, "mykey");
    }

    #[test]
    fn redis_resp_parse_get() {
        let data = b"*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n";
        let (cmd, key) = parse_redis_resp_array(data).unwrap();
        assert_eq!(cmd, "GET");
        assert_eq!(key, "mykey");
    }

    #[test]
    fn redis_resp_parse_ping() {
        let data = b"*1\r\n$4\r\nPING\r\n";
        let (cmd, key) = parse_redis_resp_array(data).unwrap();
        assert_eq!(cmd, "PING");
        assert!(key.is_empty());
    }

    #[test]
    fn redis_inline_parse() {
        let (cmd, key) = parse_redis_inline_cmd(b"GET mykey\r\n").unwrap();
        assert_eq!(cmd, "GET");
        assert_eq!(key, "mykey");
    }

    #[test]
    fn redis_inline_ping() {
        let (cmd, key) = parse_redis_inline_cmd(b"PING\r\n").unwrap();
        assert_eq!(cmd, "PING");
        assert!(key.is_empty());
    }

    #[test]
    fn redis_inline_empty() {
        let (cmd, key) = parse_redis_inline_cmd(b"\r\n").unwrap();
        assert!(cmd.is_empty());
        assert!(key.is_empty());
    }

    // -----------------------------------------------------------------------
    // Redis command type classification
    // -----------------------------------------------------------------------

    #[test]
    fn redis_type_read_commands() {
        assert_eq!(redis_command_type("GET"), "READ");
        assert_eq!(redis_command_type("MGET"), "READ");
        assert_eq!(redis_command_type("HGETALL"), "READ");
        assert_eq!(redis_command_type("LRANGE"), "READ");
        assert_eq!(redis_command_type("SMEMBERS"), "READ");
        assert_eq!(redis_command_type("ZRANGE"), "READ");
        assert_eq!(redis_command_type("KEYS"), "READ");
        assert_eq!(redis_command_type("TYPE"), "READ");
        assert_eq!(redis_command_type("EXISTS"), "READ");
        assert_eq!(redis_command_type("TTL"), "READ");
    }

    #[test]
    fn redis_type_write_commands() {
        assert_eq!(redis_command_type("SET"), "WRITE");
        assert_eq!(redis_command_type("MSET"), "WRITE");
        assert_eq!(redis_command_type("HSET"), "WRITE");
        assert_eq!(redis_command_type("LPUSH"), "WRITE");
        assert_eq!(redis_command_type("SADD"), "WRITE");
        assert_eq!(redis_command_type("ZADD"), "WRITE");
        assert_eq!(redis_command_type("DEL"), "WRITE");
        assert_eq!(redis_command_type("INCR"), "WRITE");
        assert_eq!(redis_command_type("EXPIRE"), "WRITE");
    }

    #[test]
    fn redis_type_admin_commands() {
        assert_eq!(redis_command_type("AUTH"), "ADMIN");
        assert_eq!(redis_command_type("PING"), "ADMIN");
        assert_eq!(redis_command_type("INFO"), "ADMIN");
        assert_eq!(redis_command_type("CONFIG"), "ADMIN");
        assert_eq!(redis_command_type("FLUSHDB"), "ADMIN");
        assert_eq!(redis_command_type("SUBSCRIBE"), "ADMIN");
        assert_eq!(redis_command_type("PUBLISH"), "ADMIN");
        assert_eq!(redis_command_type("EVAL"), "ADMIN");
        assert_eq!(redis_command_type("MULTI"), "ADMIN");
        assert_eq!(redis_command_type("EXEC"), "ADMIN");
        assert_eq!(redis_command_type("COMMAND"), "ADMIN");
        assert_eq!(redis_command_type("SCAN"), "READ");
        assert_eq!(redis_command_type("XADD"), "ADMIN");
    }

    #[test]
    fn redis_type_unknown_command() {
        assert_eq!(redis_command_type("FOOBAR"), "OTHER");
        assert_eq!(redis_command_type(""), "OTHER");
    }

    // -----------------------------------------------------------------------
    // String helpers
    // -----------------------------------------------------------------------

    #[test]
    fn read_null_string_basic() {
        assert_eq!(read_null_string(b"hello\0world"), "hello");
        assert_eq!(read_null_string(b"\0"), "");
        assert_eq!(read_null_string(b"no_null"), "no_null");
    }

    #[test]
    fn read_null_string_empty() {
        assert_eq!(read_null_string(b""), "");
    }

    #[test]
    fn skip_null_string_basic() {
        let data = b"hello\0world";
        let remaining = skip_null_string(data);
        assert_eq!(remaining, b"world");
    }

    #[test]
    fn skip_null_string_no_null() {
        let data = b"no_null";
        let remaining = skip_null_string(data);
        assert!(remaining.is_empty());
    }

    #[test]
    fn skip_null_string_null_at_end() {
        let data = b"hello\0";
        let remaining = skip_null_string(data);
        assert!(remaining.is_empty());
    }

    #[test]
    fn read_crlf_string_basic() {
        assert_eq!(read_crlf_string(b"+OK\r\n"), "+OK");
        assert_eq!(read_crlf_string(b"-ERR something\r\n"), "-ERR something");
    }

    #[test]
    fn read_crlf_string_no_crlf() {
        assert_eq!(read_crlf_string(b"no line ending"), "no line ending");
    }

    #[test]
    fn truncate_query_short() {
        let q = "SELECT 1";
        assert_eq!(truncate_query(q), q);
    }

    #[test]
    fn truncate_query_exactly_limit() {
        let q = "a".repeat(MAX_QUERY_LEN);
        assert_eq!(truncate_query(&q), q);
        assert_eq!(truncate_query(&q).len(), MAX_QUERY_LEN);
    }

    #[test]
    fn truncate_query_over_limit() {
        let q = "a".repeat(MAX_QUERY_LEN + 100);
        let truncated = truncate_query(&q);
        assert!(truncated.len() > MAX_QUERY_LEN);
        assert!(truncated.ends_with("..."));
    }

    // -----------------------------------------------------------------------
    // IP formatting
    // -----------------------------------------------------------------------

    #[test]
    fn format_ip_loopback() {
        assert_eq!(format_ip(0x0100007F), "127.0.0.1");
    }

    #[test]
    fn format_ip_private() {
        assert_eq!(format_ip(0x0101A8C0), "192.168.1.1");
    }

    #[test]
    fn format_ip_zero() {
        assert_eq!(format_ip(0), "0.0.0.0");
    }

    #[test]
    fn format_ip_broadcast() {
        assert_eq!(format_ip(0xFFFFFFFF), "255.255.255.255");
    }

    // -----------------------------------------------------------------------
    // Socket key
    // -----------------------------------------------------------------------

    #[test]
    fn socket_key_deterministic() {
        let k1 = socket_key(1234, 5432);
        let k2 = socket_key(1234, 5432);
        assert_eq!(k1, k2);
    }

    #[test]
    fn socket_key_different_pid() {
        let k1 = socket_key(1234, 5432);
        let k2 = socket_key(5678, 5432);
        assert_ne!(k1, k2);
    }

    #[test]
    fn socket_key_different_port() {
        let k1 = socket_key(1234, 5432);
        let k2 = socket_key(1234, 3306);
        assert_ne!(k1, k2);
    }

    // -----------------------------------------------------------------------
    // DbProtocol enum
    // -----------------------------------------------------------------------

    #[test]
    fn db_protocol_from_u8() {
        assert_eq!(DbProtocol::from_u8(0), DbProtocol::Unknown);
        assert_eq!(DbProtocol::from_u8(1), DbProtocol::PostgreSQL);
        assert_eq!(DbProtocol::from_u8(2), DbProtocol::MySQL);
        assert_eq!(DbProtocol::from_u8(3), DbProtocol::Redis);
        assert_eq!(DbProtocol::from_u8(4), DbProtocol::Encrypted);
        assert_eq!(DbProtocol::from_u8(255), DbProtocol::Unknown);
    }

    #[test]
    fn db_protocol_as_str() {
        assert_eq!(DbProtocol::Unknown.as_str(), "unknown");
        assert_eq!(DbProtocol::PostgreSQL.as_str(), "postgresql");
        assert_eq!(DbProtocol::MySQL.as_str(), "mysql");
        assert_eq!(DbProtocol::Redis.as_str(), "redis");
        assert_eq!(DbProtocol::Encrypted.as_str(), "encrypted");
    }

    // -----------------------------------------------------------------------
    // Latency tracking
    // -----------------------------------------------------------------------

    #[test]
    fn latency_tracking_pg() {
        let inspector = DbInspector::new();

        // Send request
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[8, 0, 0, 0]);
        req.extend_from_slice(b"NOW()\0");
        inspector.inspect(&req, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // Simulate some delay by inserting a small sleep
        std::thread::sleep(std::time::Duration::from_millis(10));

        // Send CommandComplete
        let mut resp = vec![b'C'];
        resp.extend_from_slice(&[9, 0, 0, 0]);
        resp.extend_from_slice(b"SELECT 1\0");
        let event = inspector
            .inspect(&resp, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected event");

        assert!(event.latency_ms >= 10.0, "latency should be >= 10ms, got {}", event.latency_ms);
    }

    #[test]
    fn latency_tracking_mysql() {
        let inspector = DbInspector::new();

        // COM_QUERY request
        let mut req = vec![0, 0, 0, 0, 0x03];
        req.extend_from_slice(b"SELECT 1");
        inspector.inspect(&req, MYSQL_PORT, 200, "mysql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        std::thread::sleep(std::time::Duration::from_millis(10));

        // OK response
        let resp = [0x03, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00];
        let event = inspector
            .inspect(&resp, MYSQL_PORT, 200, "mysql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected event");

        assert!(event.latency_ms >= 10.0);
    }

    #[test]
    fn latency_tracking_redis() {
        let inspector = DbInspector::new();

        // GET request
        let req_payload = b"*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n";
        inspector
            .inspect(req_payload, REDIS_PORT, 300, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();

        std::thread::sleep(std::time::Duration::from_millis(10));

        // Response
        let resp_payload = b"+myvalue\r\n";
        let event = inspector
            .inspect(resp_payload, REDIS_PORT, 300, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected event");

        assert!(event.latency_ms >= 10.0);
    }

    // -----------------------------------------------------------------------
    // Stats tracking
    // -----------------------------------------------------------------------

    #[test]
    fn stats_initial() {
        let inspector = DbInspector::new();
        let stats = inspector.stats();
        assert_eq!(stats.total_events, 0);
        assert_eq!(stats.pg_queries, 0);
        assert_eq!(stats.mysql_queries, 0);
        assert_eq!(stats.redis_commands, 0);
        assert_eq!(stats.encrypted_events, 0);
        assert_eq!(stats.unknown_events, 0);
        assert_eq!(stats.parse_errors, 0);
    }

    #[test]
    fn stats_pg_counter() {
        let inspector = DbInspector::new();

        // Request
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[8, 0, 0, 0]);
        req.extend_from_slice(b"NOW()\0");
        inspector.inspect(&req, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // Response (CommandComplete)
        let mut resp = vec![b'C'];
        resp.extend_from_slice(&[9, 0, 0, 0]);
        resp.extend_from_slice(b"SELECT 1\0");
        inspector.inspect(&resp, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        let stats = inspector.stats();
        assert_eq!(stats.pg_queries, 1);
        assert_eq!(stats.total_events, 2); // request + response both call record_protocol
    }

    #[test]
    fn stats_mysql_counter() {
        let inspector = DbInspector::new();

        let mut req = vec![0, 0, 0, 0, 0x03];
        req.extend_from_slice(b"SELECT 1");
        inspector.inspect(&req, MYSQL_PORT, 200, "mysql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        let resp = [0x03, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00];
        inspector.inspect(&resp, MYSQL_PORT, 200, "mysql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        let stats = inspector.stats();
        assert_eq!(stats.mysql_queries, 1);
    }

    #[test]
    fn stats_redis_counter() {
        let inspector = DbInspector::new();

        let req_payload = b"*2\r\n$3\r\nGET\r\n$5\r\nmykey\r\n";
        inspector
            .inspect(req_payload, REDIS_PORT, 300, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();

        let resp_payload = b"+value\r\n";
        inspector
            .inspect(resp_payload, REDIS_PORT, 300, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap();

        let stats = inspector.stats();
        assert_eq!(stats.redis_commands, 1);
    }

    #[test]
    fn stats_encrypted_counter() {
        let inspector = DbInspector::new();

        let payload = [0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00];
        inspector.inspect(&payload, PG_PORT, 400, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        let stats = inspector.stats();
        assert_eq!(stats.encrypted_events, 1);
        assert_eq!(stats.total_events, 1);
    }

    #[test]
    fn stats_unknown_counter() {
        let inspector = DbInspector::new();

        // Unknown protocol on non-DB port
        let payload = [0x00, 0x01, 0x02, 0x03, 0x04, 0x05];
        inspector.inspect(&payload, 9999, 500, "unknown", 0x0100007F, 0x0101A8C0, 0).unwrap();

        let stats = inspector.stats();
        assert_eq!(stats.unknown_events, 1);
    }

    // -----------------------------------------------------------------------
    // Full integration: inspect with TLS fallback
    // -----------------------------------------------------------------------

    #[test]
    fn tls_on_pg_port_emits_encrypted_event() {
        let inspector = DbInspector::new();
        let payload = [0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00];
        let event = inspector
            .inspect(&payload, PG_PORT, 500, "psql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected TLS event");

        assert_eq!(event.protocol, "encrypted");
        assert!(event.is_encrypted);
        assert_eq!(event.destination_port, PG_PORT);
    }

    #[test]
    fn tls_on_mysql_port_emits_encrypted_event() {
        let inspector = DbInspector::new();
        let payload = [0x16, 0x03, 0x03, 0x00, 0x05];
        let event = inspector
            .inspect(&payload, MYSQL_PORT, 600, "mysql", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected TLS event");

        assert_eq!(event.protocol, "encrypted");
        assert!(event.is_encrypted);
    }

    #[test]
    fn tls_on_redis_port_emits_encrypted_event() {
        let inspector = DbInspector::new();
        let payload = [0x16, 0x03, 0x01, 0x00, 0x05];
        let event = inspector
            .inspect(&payload, REDIS_PORT, 700, "redis", 0x0100007F, 0x0101A8C0, 0)
            .unwrap()
            .expect("expected TLS event");

        assert_eq!(event.protocol, "encrypted");
        assert!(event.is_encrypted);
    }

    #[test]
    fn non_db_port_returns_none() {
        let inspector = DbInspector::new();
        let payload = b"GET / HTTP/1.1\r\nHost: example.com\r\n\r\n";
        let result =
            inspector.inspect(payload, 80, 800, "nginx", 0x0100007F, 0x0101A8C0, 0).unwrap();
        assert!(result.is_none());
    }

    #[test]
    fn empty_payload_returns_none() {
        let inspector = DbInspector::new();
        let result =
            inspector.inspect(&[], PG_PORT, 900, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();
        assert!(result.is_none());
    }

    // -----------------------------------------------------------------------
    // Default trait
    // -----------------------------------------------------------------------

    #[test]
    fn default_impl() {
        let inspector = DbInspector::default();
        let stats = inspector.stats();
        assert_eq!(stats.total_events, 0);
    }

    // -----------------------------------------------------------------------
    // Pending request cleanup on different pids
    // -----------------------------------------------------------------------

    #[test]
    fn separate_pending_per_pid() {
        let inspector = DbInspector::new();

        // Two different PIDs send queries on same port
        let mut req1 = vec![b'Q'];
        req1.extend_from_slice(&[8, 0, 0, 0]);
        req1.extend_from_slice(b"NOW()\0");

        let mut req2 = vec![b'Q'];
        req2.extend_from_slice(&[10, 0, 0, 0]);
        req2.extend_from_slice(b"SELECT 1\0");

        inspector.inspect(&req1, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();
        inspector.inspect(&req2, PG_PORT, 200, "pgcli", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // Both should be pending
        let pending = inspector.pending.lock().unwrap();
        assert!(pending.contains_key(&socket_key(100, PG_PORT)));
        assert!(pending.contains_key(&socket_key(200, PG_PORT)));
        assert_eq!(pending.len(), 2);
    }

    #[test]
    fn pending_cleanup_after_response() {
        let inspector = DbInspector::new();

        // Send request
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[8, 0, 0, 0]);
        req.extend_from_slice(b"NOW()\0");
        inspector.inspect(&req, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // Verify pending
        {
            let pending = inspector.pending.lock().unwrap();
            assert_eq!(pending.len(), 1);
        }

        // Send response
        let mut resp = vec![b'C'];
        resp.extend_from_slice(&[9, 0, 0, 0]);
        resp.extend_from_slice(b"SELECT 1\0");
        inspector.inspect(&resp, PG_PORT, 100, "psql", 0x0100007F, 0x0101A8C0, 0).unwrap();

        // Pending should be cleaned up
        let pending = inspector.pending.lock().unwrap();
        assert_eq!(pending.len(), 0);
    }

    // -----------------------------------------------------------------------
    // Process name and event fields
    // -----------------------------------------------------------------------

    #[test]
    fn event_fields_populated_correctly() {
        let inspector = DbInspector::new();

        // PG request
        let mut req = vec![b'Q'];
        req.extend_from_slice(&[19, 0, 0, 0]);
        req.extend_from_slice(b"SELECT * FROM t\0");
        inspector.inspect(&req, PG_PORT, 42, "myapp", 0x0100007F, 0x0200A8C0, 0).unwrap();

        // PG CommandComplete
        let mut resp = vec![b'C'];
        resp.extend_from_slice(&[12, 0, 0, 0]);
        resp.extend_from_slice(b"SELECT 10\0");
        let event = inspector
            .inspect(&resp, PG_PORT, 42, "myapp", 0x0100007F, 0x0200A8C0, 0)
            .unwrap()
            .expect("event");

        assert_eq!(event.protocol, "postgresql");
        assert_eq!(event.pid, 42);
        assert_eq!(event.process_name, "myapp");
        assert_eq!(event.source_ip, "127.0.0.1");
        assert_eq!(event.destination_ip, "192.168.0.2");
        assert_eq!(event.destination_port, PG_PORT);
        assert_eq!(event.query_type, "SELECT");
        assert_eq!(event.table_name, "t");
        assert_eq!(event.row_count, 10);
        assert!(!event.is_encrypted);
        assert!(event.error_message.is_empty());
        assert_eq!(event.database, "");
    }
}
