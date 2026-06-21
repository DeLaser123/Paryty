//! Paryty Agent — Structured Logging Helpers
//!
//! The agent uses `tracing-subscriber` with JSON output (`.json()` format).
//! This module provides macros and helper functions that ensure consistent
//! field names across all tracing calls, conforming to the Paryty observability
//! standard.
//!
//! Every log event produced through these helpers includes:
//!   - `component: "agent"` — identifies the source component
//!   - Standardized field names: `tenant_id`, `trace_id`, `error`
//!
//! Usage:
//!   agent_info!("connected to cluster"; endpoint => %ep, tenant_id => %tid);
//!   agent_error!(error = %e, "gRPC stream failed");

/// Emit an INFO-level event with `component: "agent"` always present.
///
/// Accepts key-value fields using `tracing`'s field syntax (trailing semicolon
/// separates fields from message; bare strings are the message).
///
/// # Examples
/// ```
/// agent_info!("Agent started successfully");
/// agent_info!("Configuration loaded"; agent_id => %id, endpoint => %ep);
/// ```
macro_rules! agent_info {
    ($($arg:tt)*) => {
        tracing::info!(component = "agent", $($arg)*)
    };
}

/// Emit a WARN-level event with `component: "agent"` always present.
macro_rules! agent_warn {
    ($($arg:tt)*) => {
        tracing::warn!(component = "agent", $($arg)*)
    };
}

/// Emit an ERROR-level event with `component: "agent"` always present.
///
/// Always include the `error` field for error events. Use the `%e` format
/// specifier for Display or `?e` for Debug when the error does not implement
/// Display meaningfully.
macro_rules! agent_error {
    ($($arg:tt)*) => {
        tracing::error!(component = "agent", $($arg)*)
    };
}

/// Emit a DEBUG-level event with `component: "agent"` always present.
/// Only active when RUST_LOG includes debug level for the agent.
macro_rules! agent_debug {
    ($($arg:tt)*) => {
        tracing::debug!(component = "agent", $($arg)*)
    };
}

// ── Standard field name helpers ───────────────────────────────────────

/// Format a tenant_id for use in tracing field values.
/// Usage: `agent_info!("processing"; tenant_id => %tenant_id_field(opt_tid));`
#[inline]
pub fn tenant_id_field(tid: &Option<String>) -> &str {
    tid.as_deref().unwrap_or("default")
}

/// Format an error value consistently for tracing.
/// Usage: `agent_error!(error = %format_error(&e), "something failed");`
#[inline]
pub fn format_error<E: std::fmt::Display>(e: &E) -> String {
    e.to_string()
}

#[cfg(test)]
mod tests {
    #[test]
    fn macros_are_hygienic() {
        // Compile-time test: ensure macros expand without errors.
        // Uses a tracing subscriber that captures to a string.
        use tracing_subscriber::fmt::format::FmtSpan;

        let _guard = tracing_subscriber::fmt()
            .json()
            .with_span_events(FmtSpan::NONE)
            .with_test_writer()
            .set_default();

        let tid: Option<String> = Some("tenant-abc".into());

        agent_info!("test info message");
        agent_info!("with fields"; tenant_id => %super::tenant_id_field(&tid));
        agent_warn!("test warning");
        agent_error!(error = %super::format_error(&std::io::Error::new(std::io::ErrorKind::Other, "test")), "test error");
        agent_debug!("test debug message");
    }

    #[test]
    fn tenant_id_field_returns_default_for_none() {
        let none_tid: Option<String> = None;
        assert_eq!(super::tenant_id_field(&none_tid), "default");
    }

    #[test]
    fn tenant_id_field_returns_value_for_some() {
        let tid = Some("tenant-xyz".to_string());
        assert_eq!(super::tenant_id_field(&tid), "tenant-xyz");
    }

    #[test]
    fn format_error_returns_display_string() {
        let err = std::io::Error::new(std::io::ErrorKind::NotFound, "file not found");
        let formatted = super::format_error(&err);
        assert!(formatted.contains("file not found"));
    }
}
