#![allow(dead_code)]

//! Flow Control and Backpressure Management
//!
//! Implements credit-based flow control to prevent overwhelming the cluster.
//! Uses priority queues for critical data (alerts, errors) and adaptive
//! sampling for normal data when under backpressure.
//!
//! Research basis:
//! - "gRPC: Up and Running" (Indrasiri, Kuruppu): HTTP/2 flow control
//! - "TCP/IP Illustrated, Vol. 1" (Stevens): Sliding window protocol

use std::collections::VecDeque;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::RwLock;
use tracing::{debug, info, warn};

/// Priority levels for outgoing messages.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Priority {
    /// Critical data: alerts, errors, health check failures.
    /// Never dropped, even under extreme backpressure.
    Critical = 0,
    /// High priority: topology changes, agent registration.
    High = 1,
    /// Normal priority: regular metrics and traces.
    Normal = 2,
    /// Low priority: detailed process metrics, container info.
    Low = 3,
}

/// A message in the priority queue.
#[derive(Debug, Clone)]
pub struct PrioritizedMessage {
    pub priority: Priority,
    pub data: Vec<u8>,
    pub sequence_number: u64,
}

/// Flow control state from the cluster.
#[derive(Debug, Clone)]
pub struct FlowControlState {
    /// Whether the cluster is accepting data.
    pub accepting: bool,
    /// Current credit balance (messages the agent can send).
    pub credits: u64,
    /// Window size in bytes.
    pub window_bytes: u64,
    /// Current window usage in bytes.
    pub window_used: u64,
    /// Suggested sampling rate (0.0 to 1.0).
    pub suggested_sampling_rate: f64,
    /// Server-suggested collection interval (None = keep current).
    pub desired_interval_ms: Option<u64>,
}

impl Default for FlowControlState {
    fn default() -> Self {
        Self {
            accepting: true,
            credits: 1000,
            window_bytes: 10 * 1024 * 1024, // 10MB
            window_used: 0,
            suggested_sampling_rate: 1.0,
            desired_interval_ms: None,
        }
    }
}

/// Flow control manager.
pub struct FlowControl {
    /// Current flow control state.
    state: Arc<RwLock<FlowControlState>>,
    /// Priority queue for outgoing messages.
    queue: Arc<RwLock<VecDeque<PrioritizedMessage>>>,
    /// Whether backpressure is active.
    backpressure_active: Arc<AtomicBool>,
    /// Total messages dropped due to backpressure.
    messages_dropped: Arc<AtomicU64>,
    /// Current sampling rate (adaptive).
    sampling_rate: Arc<RwLock<f64>>,
    /// Maximum queue size.
    max_queue_size: usize,
}

impl FlowControl {
    /// Create a new flow control manager.
    pub fn new(max_queue_size: usize) -> Self {
        Self {
            state: Arc::new(RwLock::new(FlowControlState::default())),
            queue: Arc::new(RwLock::new(VecDeque::new())),
            backpressure_active: Arc::new(AtomicBool::new(false)),
            messages_dropped: Arc::new(AtomicU64::new(0)),
            sampling_rate: Arc::new(RwLock::new(1.0)),
            max_queue_size,
        }
    }

    /// Enqueue a message with priority.
    ///
    /// Critical messages are always enqueued. Normal/Low messages may be
    /// dropped if the queue is full or backpressure is active.
    pub async fn enqueue(&self, data: Vec<u8>, priority: Priority, sequence_number: u64) -> bool {
        // Critical messages always get through
        if priority == Priority::Critical {
            let msg = PrioritizedMessage { priority, data, sequence_number };
            let mut queue = self.queue.write().await;
            queue.push_back(msg);
            // Sort by priority (critical first)
            make_contiguous_sorted(&mut queue);
            return true;
        }

        // Check if backpressure is active
        if self.backpressure_active.load(Ordering::Relaxed) {
            // Apply sampling: drop messages based on sampling rate
            let rate = *self.sampling_rate.read().await;
            if fastrand::f64() > rate {
                self.messages_dropped.fetch_add(1, Ordering::Relaxed);
                debug!(
                    "Dropped message {} due to backpressure (sampling rate: {:.2})",
                    sequence_number, rate
                );
                return false;
            }
        }

        // Check queue capacity
        let mut queue = self.queue.write().await;
        if queue.len() >= self.max_queue_size {
            // Try to drop the lowest priority message
            if let Some(lowest) = queue.iter().rposition(|m| m.priority >= priority) {
                queue.remove(lowest);
                self.messages_dropped.fetch_add(1, Ordering::Relaxed);
                debug!("Dropped low-priority message to make room");
            } else {
                self.messages_dropped.fetch_add(1, Ordering::Relaxed);
                return false;
            }
        }

        let msg = PrioritizedMessage { priority, data, sequence_number };
        queue.push_back(msg);
        make_contiguous_sorted(&mut queue);
        true
    }

    /// Dequeue the highest-priority messages.
    pub async fn dequeue(&self, count: usize) -> Vec<PrioritizedMessage> {
        let mut queue = self.queue.write().await;
        let drain_count = count.min(queue.len());
        queue.drain(..drain_count).collect()
    }

    /// Update flow control state from cluster signal.
    pub async fn update_state(&self, new_state: FlowControlState) {
        let was_active = self.backpressure_active.load(Ordering::Relaxed);

        // Update sampling rate
        if new_state.suggested_sampling_rate > 0.0 {
            let mut rate = self.sampling_rate.write().await;
            *rate = new_state.suggested_sampling_rate;
        }

        // Activate backpressure if credits are low or window is nearly full
        let window_usage = if new_state.window_bytes > 0 {
            new_state.window_used as f64 / new_state.window_bytes as f64
        } else {
            0.0
        };

        let should_activate = !new_state.accepting || new_state.credits == 0 || window_usage > 0.9;

        self.backpressure_active.store(should_activate, Ordering::Release);

        let mut state = self.state.write().await;
        *state = new_state;

        if should_activate && !was_active {
            warn!(
                "Backpressure activated: credits={}, window_usage={:.1}%",
                state.credits,
                window_usage * 100.0
            );
        } else if !should_activate && was_active {
            info!("Backpressure deactivated");
        }
    }

    /// Check if backpressure is currently active.
    pub fn is_backpressured(&self) -> bool {
        self.backpressure_active.load(Ordering::Relaxed)
    }

    /// Get current sampling rate.
    pub async fn sampling_rate(&self) -> f64 {
        *self.sampling_rate.read().await
    }

    /// Get the number of messages in the queue.
    pub async fn queue_len(&self) -> usize {
        self.queue.read().await.len()
    }

    /// Get the number of messages dropped.
    pub fn messages_dropped(&self) -> u64 {
        self.messages_dropped.load(Ordering::Relaxed)
    }

    /// Apply a flow control signal from the cluster.
    ///
    /// Maps the protobuf `FlowControl` message fields to internal state,
    /// updating sampling rate, pause state, and desired collection interval.
    /// Fields with "keep current" sentinel values (0, 0.0) are ignored.
    pub async fn apply_server_signal(&self, signal: &crate::proto::paryty::v1::FlowControl) {
        // Apply pause signal (atomic store — no lock needed for backpressure_active)
        if signal.pause {
            self.backpressure_active.store(true, Ordering::Release);
            info!(
                "Flow control: agent paused by server (reason: {})",
                if signal.reason.is_empty() { "unspecified" } else { &signal.reason }
            );
        }

        // Apply sampling rate (0.0 means keep current)
        if signal.sampling_rate > 0.0 {
            let rate = signal.sampling_rate as f64;
            let mut sampling = self.sampling_rate.write().await;
            *sampling = rate;
            debug!("Flow control: sampling rate set to {:.2}", rate);
        }

        // Consolidate state struct updates under a single write lock
        {
            let mut state = self.state.write().await;
            if signal.pause {
                state.accepting = false;
            }
            if signal.sampling_rate > 0.0 {
                state.suggested_sampling_rate = signal.sampling_rate as f64;
            }
            if signal.desired_interval_ms > 0 {
                state.desired_interval_ms = Some(signal.desired_interval_ms as u64);
                debug!("Flow control: desired interval set to {}ms", signal.desired_interval_ms);
            }
        }
    }

    /// Get the server-suggested collection interval.
    ///
    /// Returns `Some(Duration)` if the server has suggested a specific
    /// collection interval via a `FlowControl` signal, `None` otherwise
    /// (agent should use its configured default).
    pub async fn desired_interval(&self) -> Option<Duration> {
        let state = self.state.read().await;
        state.desired_interval_ms.map(Duration::from_millis)
    }

    /// Get the server-suggested sampling rate.
    ///
    /// Returns the current sampling rate (0.0 to 1.0). A value of 1.0
    /// means full collection (no sampling). Lower values indicate the
    /// agent should reduce collection frequency.
    pub async fn suggested_sampling_rate(&self) -> f64 {
        *self.sampling_rate.read().await
    }
}

/// Sort a VecDeque by priority (critical first).
fn make_contiguous_sorted(queue: &mut VecDeque<PrioritizedMessage>) {
    queue.make_contiguous().sort_by(|a, b| a.priority.cmp(&b.priority));
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Helper: create a proto FlowControl message.
    fn proto_flow_control(
        desired_interval_ms: i32,
        sampling_rate: f32,
        pause: bool,
        reason: &str,
    ) -> crate::proto::paryty::v1::FlowControl {
        crate::proto::paryty::v1::FlowControl {
            desired_interval_ms,
            sampling_rate,
            pause,
            reason: reason.to_string(),
        }
    }

    #[tokio::test]
    async fn test_server_signal_application() {
        let fc = FlowControl::new(1000);

        // Apply a signal with a desired interval and sampling rate
        let signal = proto_flow_control(5000, 0.75, false, "reduce load");
        fc.apply_server_signal(&signal).await;

        // Verify desired interval was stored
        let interval = fc.desired_interval().await;
        assert!(interval.is_some(), "desired_interval should be Some after signal");
        assert_eq!(interval.unwrap(), Duration::from_millis(5000));

        // Verify sampling rate was updated
        let rate = fc.suggested_sampling_rate().await;
        assert!((rate - 0.75).abs() < f64::EPSILON, "sampling rate should be 0.75, got {}", rate);

        // Verify state struct is consistent
        let state = fc.state.read().await;
        assert_eq!(state.desired_interval_ms, Some(5000));
        assert!((state.suggested_sampling_rate - 0.75).abs() < f64::EPSILON);
        assert!(state.accepting, "accepting should remain true (not paused)");
    }

    #[tokio::test]
    async fn test_pause_signal() {
        let fc = FlowControl::new(1000);

        // Initially accepting
        {
            let state = fc.state.read().await;
            assert!(state.accepting, "should start accepting");
        }
        assert!(!fc.is_backpressured(), "should not be backpressured initially");

        // Apply pause signal
        let signal = proto_flow_control(0, 0.0, true, "server overloaded");
        fc.apply_server_signal(&signal).await;

        // Verify pause took effect
        {
            let state = fc.state.read().await;
            assert!(!state.accepting, "accepting should be false after pause");
        }
        assert!(fc.is_backpressured(), "should be backpressured after pause");
    }

    #[tokio::test]
    async fn test_sampling_rate_application() {
        let fc = FlowControl::new(1000);

        // Default should be 1.0
        let rate = fc.suggested_sampling_rate().await;
        assert!((rate - 1.0).abs() < f64::EPSILON, "default rate should be 1.0");

        // Apply a reduced rate
        let signal = proto_flow_control(0, 0.25, false, "");
        fc.apply_server_signal(&signal).await;

        let rate = fc.suggested_sampling_rate().await;
        assert!(
            (rate - 0.25).abs() < f64::EPSILON,
            "sampling rate should be 0.25 after signal, got {}",
            rate
        );

        // Sentinel 0.0 should NOT change the rate
        let keep_signal = proto_flow_control(0, 0.0, false, "");
        fc.apply_server_signal(&keep_signal).await;

        let rate = fc.suggested_sampling_rate().await;
        assert!(
            (rate - 0.25).abs() < f64::EPSILON,
            "sampling rate should still be 0.25 (0.0 sentinel preserves current), got {}",
            rate
        );
    }

    #[tokio::test]
    async fn test_desired_interval_default_is_none() {
        let fc = FlowControl::new(1000);
        assert!(
            fc.desired_interval().await.is_none(),
            "no server signal means desired_interval should be None"
        );
    }

    #[tokio::test]
    async fn test_zero_interval_sentinel_preserves_current() {
        let fc = FlowControl::new(1000);

        // Set an interval
        let signal = proto_flow_control(3000, 0.0, false, "");
        fc.apply_server_signal(&signal).await;
        assert_eq!(fc.desired_interval().await, Some(Duration::from_millis(3000)));

        // Send 0 sentinel — should preserve
        let keep = proto_flow_control(0, 0.0, false, "");
        fc.apply_server_signal(&keep).await;
        assert_eq!(fc.desired_interval().await, Some(Duration::from_millis(3000)));
    }
}
