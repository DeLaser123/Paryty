#![allow(dead_code)]

//! HTTP Inspector
//!
//! Inspects HTTP request/response pairs from network traffic.

use anyhow::Result;

/// HTTP request/response pair.
#[derive(Debug, Clone)]
pub struct HttpEvent {
    pub method: String,
    pub path: String,
    pub status_code: u16,
    pub latency_ms: f64,
    pub request_bytes: u64,
    pub response_bytes: u64,
    pub source_ip: String,
    pub destination_ip: String,
    pub destination_port: u16,
    pub host: String,
    pub pid: u32,
}

/// HTTP inspector.
pub struct HttpInspector;

impl HttpInspector {
    pub fn new() -> Self {
        Self
    }

    /// Parse HTTP data from a packet buffer.
    pub fn inspect(&self, _data: &[u8]) -> Result<Option<HttpEvent>> {
        // TODO: Parse HTTP/1.1 and HTTP/2 frames
        Ok(None)
    }
}

impl Default for HttpInspector {
    fn default() -> Self {
        Self::new()
    }
}
