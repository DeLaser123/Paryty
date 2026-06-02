#![allow(dead_code)]

//! eBPF Network Observer Module
//!
//! Network-level observation using eBPF (Linux only).
//! Provides TCP connection tracking, DNS resolution mapping,
//! HTTP request/response inspection, and database query inspection.
//!
//! On non-Linux platforms, this module compiles as stubs that return empty results.

pub mod db_inspector;
pub mod dns_mapper;
pub mod http_inspector;
pub mod loader;
pub mod tcp_tracker;

use anyhow::Result;
use serde::Serialize;
use tracing::warn;

use crate::communication::Client;
use crate::config::EbpfConfig;

/// Network event types observed by eBPF.
#[derive(Debug, Serialize, Clone)]
#[serde(tag = "type")]
pub enum NetworkEvent {
    TcpConnection {
        source_ip: String,
        source_port: u16,
        destination_ip: String,
        destination_port: u16,
        state: String,
        pid: u32,
        process_name: String,
    },
    DnsQuery {
        query_name: String,
        resolved_ips: Vec<String>,
        latency_ms: f64,
        pid: u32,
    },
    HttpRequest {
        method: String,
        path: String,
        status_code: u16,
        latency_ms: f64,
        source_ip: String,
        destination_ip: String,
        destination_port: u16,
        pid: u32,
    },
    DbQuery {
        protocol: String,
        query: String,
        latency_ms: f64,
        destination_ip: String,
        destination_port: u16,
        pid: u32,
    },
}

/// Run the eBPF network observer.
///
/// On Linux, loads eBPF programs and observes network traffic.
/// On other platforms, logs a warning and returns Ok(()) immediately.
pub async fn run(_config: EbpfConfig, _client: Client) -> Result<()> {
    #[cfg(not(target_os = "linux"))]
    {
        warn!("eBPF observer is only supported on Linux. Running in stub mode.");
        // Keep the task alive but don't do anything
        loop {
            tokio::time::sleep(std::time::Duration::from_secs(60)).await;
        }
    }

    #[cfg(target_os = "linux")]
    {
        tracing::info!("Starting eBPF network observer");
        // TODO: Load eBPF programs via aya
        // TODO: Attach to kprobes/tracepoints
        // TODO: Process events and send via client
        warn!("eBPF observer not yet implemented. Running in stub mode.");
        loop {
            tokio::time::sleep(std::time::Duration::from_secs(60)).await;
        }
    }
}
