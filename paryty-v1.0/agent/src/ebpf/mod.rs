//! eBPF Network Observer Module
//!
//! Network-level observation using eBPF (libbpf-rs) on Linux.
//! Provides TCP connection tracking, DNS resolution mapping,
//! HTTP request/response inspection, and database query inspection.
//!
//! Three operating modes are selected automatically at startup:
//! - **eBPF** — Full kernel-level observation via libbpf (kernel >= 5.4, BTF required)
//! - **ProcFallback** — /proc-based polling when eBPF is unavailable
//! - **Stub** — No-op on non-Linux or when explicitly disabled

pub mod db_inspector;
pub mod dns_mapper;
pub mod http_inspector;
pub mod loader;
pub mod proc_fallback;
pub mod tcp_tracker;

use anyhow::Result;
use serde::Serialize;

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

/// Observer mode — determined at startup based on system capabilities.
#[derive(Debug, Clone, PartialEq)]
pub enum ObserverMode {
    /// Full eBPF observation (kernel >= 5.4, BTF available)
    Ebpf,
    /// /proc-based fallback (Linux but no eBPF support)
    ProcFallback,
    /// Stub mode (non-Linux or explicitly disabled)
    Stub,
}

/// Detect the best available observer mode based on config and system capabilities.
#[allow(unused_variables)]
fn detect_observer_mode(config: &EbpfConfig) -> ObserverMode {
    #[cfg(not(target_os = "linux"))]
    {
        ObserverMode::Stub
    }

    #[cfg(target_os = "linux")]
    {
        if !config.enabled {
            return ObserverMode::Stub;
        }

        // Check kernel version and BTF support
        let version_path = std::path::Path::new("/proc/sys/kernel/osrelease");
        let version_ok = match std::fs::read_to_string(version_path) {
            Ok(raw) => {
                let trimmed = raw.trim();
                let mut parts = trimmed.split('.');
                let major: u32 = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0);
                let minor: u32 = parts
                    .next()
                    .and_then(|s| s.split('-').next().and_then(|s| s.parse().ok()))
                    .unwrap_or(0);
                tracing::info!(major, minor, "Detected kernel version");
                major > 5 || (major == 5 && minor >= 4)
            }
            Err(e) => {
                tracing::warn!(error = %e, "Failed to read kernel version");
                false
            }
        };

        if version_ok && std::path::Path::new("/sys/kernel/btf/vmlinux").exists() {
            ObserverMode::Ebpf
        } else if config.fallback_to_proc {
            tracing::warn!("eBPF unavailable (kernel version or BTF), falling back to /proc");
            ObserverMode::ProcFallback
        } else {
            tracing::warn!("eBPF unavailable and fallback disabled, running in stub mode");
            ObserverMode::Stub
        }
    }
}

/// Run the eBPF network observer with automatic mode detection and fallback.
pub async fn run(config: EbpfConfig, client: Client) -> Result<()> {
    let mode = detect_observer_mode(&config);
    tracing::info!(?mode, "eBPF observer starting");

    match mode {
        ObserverMode::Ebpf => run_ebpf(config, client).await,
        ObserverMode::ProcFallback => proc_fallback::run(config, client).await,
        ObserverMode::Stub => {
            tracing::warn!("Network observer running in stub mode (no-op)");
            loop {
                tokio::time::sleep(std::time::Duration::from_secs(60)).await;
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Linux eBPF implementation
// ---------------------------------------------------------------------------

/// Run full eBPF observation mode (Linux only).
///
/// Loads eBPF programs via libbpf-rs, creates ring buffer consumers for
/// TCP/DNS/HTTP events, and processes events in a loop, sending them
/// to the cluster via the gRPC client.
///
/// Falls back to `/proc` polling if no programs can be loaded.
#[cfg(target_os = "linux")]
async fn run_ebpf(config: EbpfConfig, client: Client) -> Result<()> {
    // Load eBPF programs
    let ebpf_loader = loader::EbpfLoader::new(config.clone());
    let programs = ebpf_loader.load()?;

    if !programs.is_loaded() {
        tracing::warn!("No eBPF programs loaded, falling back to /proc");
        return proc_fallback::run(config, client).await;
    }

    // Create trackers — these consume events from eBPF ring buffers
    // and expose them via drain_events().
    let tcp = tcp_tracker::TcpTracker::new(config.clone());
    let dns = dns_mapper::DnsMapper::new(config.clone());
    let http = http_inspector::HttpInspector::new(config.clone());

    // Set up ring buffer consumers.
    // Each loaded BPF object exposes a ring buffer map that delivers
    // events to userspace. The trackers' setup_ring_buffer() methods
    // register callbacks that enqueue events for drain_events().
    //
    // CRITICAL: We must keep the RingBuffer objects alive AND call
    // poll() on them periodically — dropping them stops event delivery.
    let mut ring_buffers: Vec<libbpf_rs::RingBuffer<'static>> = Vec::new();

    if let Some(ref tcp_obj) = programs.tcp_object {
        match tcp.setup_ring_buffer(tcp_obj) {
            Ok(rb) => {
                tracing::info!("TCP ring buffer configured");
                ring_buffers.push(rb);
            }
            Err(e) => {
                tracing::warn!(error = %e, "Failed to set up TCP ring buffer");
            }
        }
    }

    if let Some(ref dns_obj) = programs.dns_object {
        match dns.setup_ring_buffer(dns_obj) {
            Ok(rb) => {
                tracing::info!("DNS ring buffer configured");
                ring_buffers.push(rb);
            }
            Err(e) => {
                tracing::warn!(error = %e, "Failed to set up DNS ring buffer");
            }
        }
    }

    if let Some(ref http_obj) = programs.http_object {
        match http.setup_ring_buffer(http_obj) {
            Ok(rb) => {
                tracing::info!("HTTP ring buffer configured");
                ring_buffers.push(rb);
            }
            Err(e) => {
                tracing::warn!(error = %e, "Failed to set up HTTP ring buffer");
            }
        }
    }

    let poll_count = ring_buffers.len();
    tracing::info!(count = poll_count, "Ring buffers ready for polling");

    // Event processing loop — poll ring buffers then drain accumulated events
    let poll_interval = std::time::Duration::from_millis(config.poll_interval_ms);
    tracing::info!(interval_ms = config.poll_interval_ms, "Starting eBPF event loop");

    loop {
        // Poll each ring buffer to consume events from kernel space.
        // This invokes the registered callbacks which enqueue events
        // into the trackers' internal maps/queues.
        for rb in ring_buffers.iter() {
            if let Err(e) = rb.poll(std::time::Duration::ZERO) {
                tracing::warn!(error = %e, "Ring buffer poll error");
            }
        }

        // Drain events from each tracker
        let tcp_events = tcp.drain_events();
        let dns_events = dns.drain_events();
        let http_events = http.drain_events();

        // Combine all events into a single batch
        let mut all_events = Vec::new();
        all_events.extend(tcp_events);
        all_events.extend(dns_events);
        all_events.extend(http_events);

        // Send batch to cluster via typed proto RPC (ReportNetworkEvents).
        // Falls back to edge buffer as JSON on failure.
        if !all_events.is_empty() {
            tracing::info!(count = all_events.len(), "Sending network events batch");
            if let Err(e) = client.send_network_events_proto(&all_events).await {
                tracing::warn!(error = %e, "Failed to send network events via proto");
            } else {
                tracing::info!(count = all_events.len(), "Network events sent successfully");
            }
        }

        tokio::time::sleep(poll_interval).await;
    }
}

// ---------------------------------------------------------------------------
// Non-Linux stub
// ---------------------------------------------------------------------------

/// Stub for non-Linux platforms — eBPF is not available.
///
/// This function should never be called because [`detect_observer_mode`]
/// returns [`ObserverMode::Stub`] on non-Linux, which causes [`run`] to
/// enter the stub loop instead. It exists solely to satisfy the compiler.
#[cfg(not(target_os = "linux"))]
async fn run_ebpf(_config: EbpfConfig, client: Client) -> Result<()> {
    tracing::error!("run_ebpf called on non-Linux platform — this should not happen");
    proc_fallback::run(_config, client).await
}
