#![allow(dead_code)]

//! TCP Connection Tracker
//!
//! Tracks TCP connection lifecycle events (connect, accept, close, reset).
//! Extracts source/destination IP:port pairs and connection state.

use anyhow::Result;

/// TCP connection event.
#[derive(Debug, Clone)]
pub struct TcpConnectionEvent {
    pub event_type: String,
    pub source_ip: String,
    pub source_port: u16,
    pub destination_ip: String,
    pub destination_port: u16,
    pub state: String,
    pub pid: u32,
    pub process_name: String,
}

/// TCP connection tracker.
pub struct TcpTracker;

impl TcpTracker {
    pub fn new() -> Self {
        Self
    }

    /// Read current TCP connections from /proc/net/tcp (Linux) or stub.
    pub fn snapshot(&self) -> Result<Vec<TcpConnectionEvent>> {
        #[cfg(target_os = "linux")]
        {
            // TODO: Read from eBPF maps or /proc/net/tcp
            Ok(Vec::new())
        }

        #[cfg(not(target_os = "linux"))]
        {
            Ok(Vec::new())
        }
    }
}

impl Default for TcpTracker {
    fn default() -> Self {
        Self::new()
    }
}
