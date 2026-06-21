//! Paryty Agent — Self-Monitoring Metrics (Dogfooding)
//!
//! Implements the golden-signal metrics defined in the observability-golden-signals
//! specification using `std::sync::atomic` counters. No external metrics crate
//! dependency — the agent's footprint constraint (<50MB, <2% CPU) prohibits
//! pulling in an entire Prometheus client library.
//!
//! The `Metrics` struct is the single source of truth for all agent self-metrics.
//! It exposes atomic operations for hot-path counter increments and a Prometheus
//! text-format renderer for the `/metrics` HTTP endpoint.
//!
//! Golden signals exported:
//!   - paryty.agent.metrics.collected.total  (counter)
//!   - paryty.agent.errors.total             (counter)
//!   - paryty.agent.grpc.connected           (gauge, 1=connected, 0=disconnected)
//!   - paryty.agent.ebpf.active              (gauge, 1=loaded, 0=fallback)
//!   - paryty.agent.memory.bytes             (gauge)
//!   - paryty.agent.edge_buffer.size         (gauge)

use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};

/// The canonical ordering for atomics in this module.
/// Relaxed is sufficient for counters (no ordering constraints with other
/// operations). Acquire/Release is used for boolean gauges where a
/// reader must see the latest write.
const RELAXED: Ordering = Ordering::Relaxed;
const ACQ_REL: Ordering = Ordering::AcqRel;

/// Self-monitoring metrics for the Paryty Agent.
///
/// All counters are monotonic (only increment). Gauges use `AtomicBool` for
/// binary state or `AtomicU64` for numeric values. The struct is `Sync + Send`
/// and can be shared across tasks via `Arc<Metrics>`.
#[derive(Debug)]
pub struct Metrics {
    /// Total metrics collected (metal + eBPF + supervisor layers).
    /// Golden signal: paryty.agent.metrics.collected.total
    metrics_collected: AtomicU64,

    /// Total errors across all collection layers.
    /// Golden signal: paryty.agent.errors.total
    errors_total: AtomicU64,

    /// gRPC connection state: true = connected, false = disconnected.
    /// Golden signal: paryty.agent.grpc.connected
    grpc_connected: AtomicBool,

    /// eBPF program state: true = loaded and active, false = fallback.
    /// Golden signal: paryty.agent.ebpf.active
    ebpf_active: AtomicBool,

    /// Current memory usage in bytes (RSS).
    /// Golden signal: paryty.agent.memory.bytes
    memory_bytes: AtomicU64,

    /// Current edge buffer queue depth (number of pending batches).
    /// Golden signal: paryty.agent.edge_buffer.size
    edge_buffer_size: AtomicU64,
}

impl Metrics {
    /// Create a new Metrics instance with all counters initialized to zero
    /// and gauges initialized to their default (disconnected / inactive) state.
    pub fn new() -> Self {
        Self {
            metrics_collected: AtomicU64::new(0),
            errors_total: AtomicU64::new(0),
            grpc_connected: AtomicBool::new(false),
            ebpf_active: AtomicBool::new(false),
            memory_bytes: AtomicU64::new(0),
            edge_buffer_size: AtomicU64::new(0),
        }
    }

    // ── Counter operations (hot-path safe, lock-free) ──────────────────

    /// Increment the metrics-collected counter by `count`.
    /// Called from metal scraper and eBPF observer after each collection cycle.
    #[inline]
    pub fn inc_metrics_collected(&self, count: u64) {
        self.metrics_collected.fetch_add(count, RELAXED);
    }

    /// Increment the errors counter by 1.
    /// Called from any layer when a collection error occurs.
    #[inline]
    pub fn inc_errors(&self) {
        self.errors_total.fetch_add(1, RELAXED);
    }

    // ── Gauge operations ───────────────────────────────────────────────

    /// Set the gRPC connection state.
    /// Called from the communication layer on connect/disconnect events.
    #[inline]
    pub fn set_grpc_connected(&self, connected: bool) {
        self.grpc_connected.store(connected, ACQ_REL);
    }

    /// Set the eBPF active state.
    /// Called from the eBPF loader — true when programs are loaded and
    /// attached, false when the fallback (/proc or stub) is active.
    #[inline]
    pub fn set_ebpf_active(&self, active: bool) {
        self.ebpf_active.store(active, ACQ_REL);
    }

    /// Update the memory usage gauge (bytes).
    /// Called periodically from the metal scraper or a background sampler.
    #[inline]
    pub fn set_memory_bytes(&self, bytes: u64) {
        self.memory_bytes.store(bytes, RELAXED);
    }

    /// Update the edge buffer queue depth.
    /// Called from the edge buffer when batches are enqueued/dequeued.
    #[inline]
    pub fn set_edge_buffer_size(&self, size: u64) {
        self.edge_buffer_size.store(size, RELAXED);
    }

    // ── Snapshot (for testing / introspection) ─────────────────────────

    /// Return a point-in-time snapshot of all metrics as a `MetricsSnapshot`.
    /// Uses `Relaxed` ordering — acceptable for a best-effort snapshot.
    pub fn snapshot(&self) -> MetricsSnapshot {
        MetricsSnapshot {
            metrics_collected: self.metrics_collected.load(RELAXED),
            errors_total: self.errors_total.load(RELAXED),
            grpc_connected: self.grpc_connected.load(RELAXED),
            ebpf_active: self.ebpf_active.load(RELAXED),
            memory_bytes: self.memory_bytes.load(RELAXED),
            edge_buffer_size: self.edge_buffer_size.load(RELAXED),
        }
    }

    // ── Prometheus text format ─────────────────────────────────────────

    /// Render all metrics in Prometheus exposition format (text/plain;
    /// version=0.0.4). This is the format consumed by Prometheus and
    /// OpenMetrics-compatible scrapers.
    ///
    /// Format reference:
    ///   https://prometheus.io/docs/instrumenting/exposition_formats/
    pub fn render_prometheus(&self) -> String {
        let snap = self.snapshot();
        let grpc_val: u8 = if snap.grpc_connected { 1 } else { 0 };
        let ebpf_val: u8 = if snap.ebpf_active { 1 } else { 0 };

        format!(
            "# HELP paryty_agent_metrics_collected_total Total metrics collected across all layers.\n\
             # TYPE paryty_agent_metrics_collected_total counter\n\
             paryty_agent_metrics_collected_total {}\n\
             # HELP paryty_agent_errors_total Total errors across all collection layers.\n\
             # TYPE paryty_agent_errors_total counter\n\
             paryty_agent_errors_total {}\n\
             # HELP paryty_agent_grpc_connected 1 if gRPC connected, 0 otherwise.\n\
             # TYPE paryty_agent_grpc_connected gauge\n\
             paryty_agent_grpc_connected {}\n\
             # HELP paryty_agent_ebpf_active 1 if eBPF loaded, 0 if fallback active.\n\
             # TYPE paryty_agent_ebpf_active gauge\n\
             paryty_agent_ebpf_active {}\n\
             # HELP paryty_agent_memory_bytes Current RSS memory usage in bytes.\n\
             # TYPE paryty_agent_memory_bytes gauge\n\
             paryty_agent_memory_bytes {}\n\
             # HELP paryty_agent_edge_buffer_size Current edge buffer queue depth.\n\
             # TYPE paryty_agent_edge_buffer_size gauge\n\
             paryty_agent_edge_buffer_size {}\n",
            snap.metrics_collected,
            snap.errors_total,
            grpc_val,
            ebpf_val,
            snap.memory_bytes,
            snap.edge_buffer_size,
        )
    }

    /// Render all metrics as a JSON object. Used for the `/health` endpoint
    /// and when Prometheus scraping is not configured.
    pub fn render_json(&self) -> String {
        let snap = self.snapshot();
        serde_json::json!({
            "status": "healthy",
            "service": "paryty-agent",
            "version": env!("CARGO_PKG_VERSION"),
            "metrics": {
                "paryty.agent.metrics.collected.total": snap.metrics_collected,
                "paryty.agent.errors.total": snap.errors_total,
                "paryty.agent.grpc.connected": if snap.grpc_connected { 1 } else { 0 },
                "paryty.agent.ebpf.active": if snap.ebpf_active { 1 } else { 0 },
                "paryty.agent.memory.bytes": snap.memory_bytes,
                "paryty.agent.edge_buffer.size": snap.edge_buffer_size,
            }
        })
        .to_string()
    }
}

impl Default for Metrics {
    fn default() -> Self {
        Self::new()
    }
}

/// Point-in-time snapshot of all agent metrics.
/// Used for introspection and the Prometheus renderer.
#[derive(Debug, Clone, Copy)]
pub struct MetricsSnapshot {
    pub metrics_collected: u64,
    pub errors_total: u64,
    pub grpc_connected: bool,
    pub ebpf_active: bool,
    pub memory_bytes: u64,
    pub edge_buffer_size: u64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn counters_start_at_zero() {
        let m = Metrics::new();
        let snap = m.snapshot();
        assert_eq!(snap.metrics_collected, 0);
        assert_eq!(snap.errors_total, 0);
    }

    #[test]
    fn gauges_start_at_default() {
        let m = Metrics::new();
        let snap = m.snapshot();
        assert!(!snap.grpc_connected);
        assert!(!snap.ebpf_active);
        assert_eq!(snap.memory_bytes, 0);
        assert_eq!(snap.edge_buffer_size, 0);
    }

    #[test]
    fn inc_metrics_collected_is_monotonic() {
        let m = Metrics::new();
        m.inc_metrics_collected(42);
        m.inc_metrics_collected(58);
        assert_eq!(m.snapshot().metrics_collected, 100);
    }

    #[test]
    fn inc_errors_is_monotonic() {
        let m = Metrics::new();
        m.inc_errors();
        m.inc_errors();
        m.inc_errors();
        assert_eq!(m.snapshot().errors_total, 3);
    }

    #[test]
    fn grpc_connected_toggle() {
        let m = Metrics::new();
        assert!(!m.snapshot().grpc_connected);
        m.set_grpc_connected(true);
        assert!(m.snapshot().grpc_connected);
        m.set_grpc_connected(false);
        assert!(!m.snapshot().grpc_connected);
    }

    #[test]
    fn ebpf_active_toggle() {
        let m = Metrics::new();
        assert!(!m.snapshot().ebpf_active);
        m.set_ebpf_active(true);
        assert!(m.snapshot().ebpf_active);
    }

    #[test]
    fn render_prometheus_contains_all_metrics() {
        let m = Metrics::new();
        m.inc_metrics_collected(1);
        m.inc_errors();
        m.set_grpc_connected(true);
        m.set_ebpf_active(true);
        m.set_memory_bytes(1024 * 1024);
        m.set_edge_buffer_size(5);

        let output = m.render_prometheus();
        assert!(output.contains("paryty_agent_metrics_collected_total 1"));
        assert!(output.contains("paryty_agent_errors_total 1"));
        assert!(output.contains("paryty_agent_grpc_connected 1"));
        assert!(output.contains("paryty_agent_ebpf_active 1"));
        assert!(output.contains("paryty_agent_memory_bytes 1048576"));
        assert!(output.contains("paryty_agent_edge_buffer_size 5"));
    }

    #[test]
    fn render_json_contains_all_metrics() {
        let m = Metrics::new();
        m.set_grpc_connected(true);
        let output = m.render_json();
        assert!(output.contains("\"paryty.agent.grpc.connected\":1"));
        assert!(output.contains("\"status\":\"healthy\""));
        assert!(output.contains("\"service\":\"paryty-agent\""));
    }

    #[test]
    fn metrics_is_send_sync() {
        fn assert_send_sync<T: Send + Sync>() {}
        assert_send_sync::<Metrics>();
    }

    #[test]
    fn snapshot_is_copy() {
        let snap = MetricsSnapshot {
            metrics_collected: 10,
            errors_total: 2,
            grpc_connected: true,
            ebpf_active: false,
            memory_bytes: 0,
            edge_buffer_size: 0,
        };
        let snap2 = snap; // Copy — compiles only if Copy is implemented
        assert_eq!(snap2.metrics_collected, 10);
    }
}
