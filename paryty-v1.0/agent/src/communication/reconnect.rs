#![allow(dead_code)]

//! Reconnection Engine with Exponential Backoff and Jitter
//!
//! Implements the reconnection strategy for the gRPC client.
//! Uses exponential backoff with jitter to prevent thundering herd.
//!
//! Research basis:
//! - "TCP/IP Illustrated, Vol. 1" (Stevens): TCP retransmission patterns
//! - AWS Architecture Blog: Exponential backoff and jitter

use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::RwLock;
use tracing::{info, warn};

use super::grpc_client::{ConnectionState, GrpcClient};

/// Reconnection policy configuration.
#[derive(Debug, Clone)]
pub struct ReconnectPolicy {
    /// Base delay for the first retry (default: 100ms).
    pub base_delay: Duration,
    /// Maximum delay between retries (default: 30s).
    pub max_delay: Duration,
    /// Jitter factor (0.0 to 1.0, default: 0.5).
    /// Prevents thundering herd when multiple agents reconnect simultaneously.
    pub jitter_factor: f64,
    /// Maximum number of consecutive failures before giving up (0 = unlimited).
    pub max_retries: u32,
    /// Whether to reset the backoff counter on successful connection.
    pub reset_on_success: bool,
}

impl Default for ReconnectPolicy {
    fn default() -> Self {
        Self {
            base_delay: Duration::from_millis(100),
            max_delay: Duration::from_secs(30),
            jitter_factor: 0.5,
            max_retries: 0, // unlimited
            reset_on_success: true,
        }
    }
}

/// Reconnection engine that manages automatic reconnection.
pub struct ReconnectionEngine {
    policy: ReconnectPolicy,
    attempt: AtomicU32,
    client: GrpcClient,
    state: Arc<RwLock<ReconnectState>>,
}

#[derive(Debug)]
struct ReconnectState {
    consecutive_failures: u32,
    last_attempt: Option<std::time::Instant>,
    total_reconnections: u64,
}

impl ReconnectionEngine {
    /// Create a new reconnection engine.
    pub fn new(client: GrpcClient, policy: ReconnectPolicy) -> Self {
        Self {
            policy,
            attempt: AtomicU32::new(0),
            client,
            state: Arc::new(RwLock::new(ReconnectState {
                consecutive_failures: 0,
                last_attempt: None,
                total_reconnections: 0,
            })),
        }
    }

    /// Calculate the next backoff delay with jitter.
    ///
    /// Uses the formula: min(max_delay, base_delay * 2^attempt) + jitter
    /// where jitter is a random value in [-jitter_factor * delay, +jitter_factor * delay]
    pub fn next_delay(&self) -> Duration {
        let attempt = self.attempt.load(Ordering::Relaxed);
        let base = self.policy.base_delay.as_millis() as f64;
        let max = self.policy.max_delay.as_millis() as f64;

        // Exponential backoff: base * 2^attempt
        let delay_ms = base * (2.0_f64.powi(attempt as i32));

        // Cap at max delay
        let delay_ms = delay_ms.min(max);

        // Apply jitter: random value in [delay * (1 - jitter), delay * (1 + jitter)]
        let jitter_range = delay_ms * self.policy.jitter_factor;
        let jitter = (fastrand::f64() * 2.0 - 1.0) * jitter_range;
        let delay_ms = (delay_ms + jitter).max(0.0);

        Duration::from_millis(delay_ms as u64)
    }

    /// Attempt to reconnect to the cluster.
    ///
    /// Returns Ok(true) if reconnection was successful, Ok(false) if max retries exceeded.
    pub async fn reconnect(&self) -> Result<bool, anyhow::Error> {
        let mut state = self.state.write().await;
        state.last_attempt = Some(std::time::Instant::now());
        state.consecutive_failures += 1;
        drop(state);

        let attempt = self.attempt.load(Ordering::Relaxed);

        // Check max retries
        if self.policy.max_retries > 0 && attempt >= self.policy.max_retries {
            warn!("Max reconnection attempts ({}) exceeded", self.policy.max_retries);
            return Ok(false);
        }

        // Calculate delay
        let delay = self.next_delay();
        info!("Reconnection attempt {} in {:?}", attempt + 1, delay);

        // Wait before retry
        self.client.set_state(ConnectionState::Reconnecting).await;
        tokio::time::sleep(delay).await;

        // Increment attempt counter
        self.attempt.fetch_add(1, Ordering::SeqCst);

        // Try to connect
        match self.client.connect().await {
            Ok(()) => {
                info!("Reconnection successful on attempt {}", attempt + 1);
                if self.policy.reset_on_success {
                    self.attempt.store(0, Ordering::SeqCst);
                }
                let mut state = self.state.write().await;
                state.consecutive_failures = 0;
                state.total_reconnections += 1;
                Ok(true)
            }
            Err(e) => {
                warn!("Reconnection attempt {} failed: {}", attempt + 1, e);
                Err(e)
            }
        }
    }

    /// Reset the reconnection state (e.g., after successful connection).
    pub fn reset(&self) {
        self.attempt.store(0, Ordering::SeqCst);
    }

    /// Get the number of consecutive failures.
    pub async fn consecutive_failures(&self) -> u32 {
        self.state.read().await.consecutive_failures
    }

    /// Get the total number of reconnections.
    pub async fn total_reconnections(&self) -> u64 {
        self.state.read().await.total_reconnections
    }
}
