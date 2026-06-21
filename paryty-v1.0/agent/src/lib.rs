//! Paryty Agent Library
//!
//! This library provides the core functionality for the Paryty Agent.

pub mod logging;
pub mod communication;
pub mod config;
#[cfg_attr(not(target_os = "linux"), allow(dead_code))]
pub mod ebpf;
pub mod metal;
pub mod observability;
pub mod proto;
pub mod supervisor;

// Re-export main types
pub use communication::Client as CommunicationClient;
pub use config::Config;
pub use observability::Metrics;

/// Agent version
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// Agent name
pub const AGENT_NAME: &str = "paryty-agent";
