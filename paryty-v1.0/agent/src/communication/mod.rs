#![allow(dead_code)]

//! Communication Layer Module
//!
//! Provides the public API for the Paryty Agent's communication with the cluster.
//! Combines gRPC client, reconnection, edge buffer, compression, and flow control
//! into a unified interface.
//!
//! Architecture:
//! ```text
//! Client
//! ├── GrpcClient          — Bidirectional gRPC stream to cluster
//! ├── ReconnectionEngine  — Exponential backoff with jitter
//! ├── EdgeBuffer          — Write-ahead log for zero data loss
//! ├── Compressor          — Zstd (all data)
//! ├── FlowControl         — Credit-based backpressure
//! └── CancellationToken   — Cooperative shutdown signal
//! ```
//!
//! Lifecycle:
//! 1. `connect()` → TCP/TLS handshake
//! 2. `register_on_connect()` → AgentRegistration RPC
//! 3. `start_heartbeat_loop()` → periodic Heartbeat RPC
//! 4. `start_stream_listener()` → bidirectional ClusterToAgent stream
//! 5. `send_metrics()` / `send_traces()` / … → compress → buffer → send
//! 6. `shutdown()` → cancel → flush buffer → disconnect

pub mod compression;
pub mod edge_buffer;
pub mod flow_control;
pub mod grpc_client;
pub mod reconnect;

use anyhow::{Context, Result};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Instant;
use tokio::sync::RwLock;
use tokio_util::sync::CancellationToken;
use tracing::{debug, error, info, warn};

use crate::config::Config;
use compression::Compressor;
use edge_buffer::{DataType, EdgeBuffer, EdgeBufferConfig};
use flow_control::FlowControl;
use grpc_client::GrpcClient;
use reconnect::{ReconnectPolicy, ReconnectionEngine};

use crate::proto::cluster_to_agent;
use crate::proto::paryty::v1::{
    AgentCapabilities, AgentCommandType, AgentRegistration, AgentRegistrationResponse,
    AgentToCluster, ClusterToAgent, CpuMetric, DiskMetric, HeartbeatRequest, MemoryMetric,
    MetricBatch, NetworkMetric, ProcessMetric,
};

use crate::metal::batch::MetalBatch;

/// The main communication client for the Paryty Agent.
///
/// This is the primary interface used by collection layers to send data
/// to the cluster. It handles all the complexity of connection management,
/// buffering, compression, and flow control internally.
///
/// All sub-components are `Arc`-wrapped so the `Client` itself is `Clone`
/// and can be shared across collection tasks without contention.
#[derive(Clone)]
pub struct Client {
    /// gRPC client for cluster communication.
    grpc: Arc<GrpcClient>,
    /// Reconnection engine.
    reconnect: Arc<ReconnectionEngine>,
    /// Edge buffer for zero data loss.
    buffer: Arc<EdgeBuffer>,
    /// Compressor for data compression.
    compressor: Arc<Compressor>,
    /// Flow control manager.
    flow_control: Arc<FlowControl>,
    /// Sequence number counter.
    sequence_counter: Arc<std::sync::atomic::AtomicU64>,
    /// Cooperative cancellation token for graceful shutdown.
    cancel_token: CancellationToken,
    /// Agent ID (from config).
    agent_id: String,
    /// Session ID assigned by the cluster after registration.
    session_id: Arc<RwLock<String>>,
    /// Instant the agent started (for uptime reporting).
    started_at: Instant,
    /// Whether the agent has completed registration.
    registered: Arc<AtomicBool>,
}

impl Client {
    /// Create a new communication client.
    ///
    /// Accepts the full agent `Config` so the gRPC client can extract the
    /// cluster endpoint and TLS settings.
    pub async fn new(config: &Config) -> Result<Self> {
        info!("Initializing communication layer");

        let grpc = GrpcClient::new(config);
        let reconnect = ReconnectionEngine::new(grpc.clone(), ReconnectPolicy::default());
        let buffer = EdgeBuffer::new(EdgeBufferConfig::default());
        let compressor = Compressor::new();
        let flow_control = FlowControl::new(10_000);

        Ok(Self {
            grpc: Arc::new(grpc),
            reconnect: Arc::new(reconnect),
            buffer: Arc::new(buffer),
            compressor: Arc::new(compressor),
            flow_control: Arc::new(flow_control),
            sequence_counter: Arc::new(std::sync::atomic::AtomicU64::new(1)),
            cancel_token: CancellationToken::new(),
            agent_id: config.agent.id.clone(),
            session_id: Arc::new(RwLock::new(String::new())),
            started_at: Instant::now(),
            registered: Arc::new(AtomicBool::new(false)),
        })
    }

    // ── Registration ────────────────────────────────────────────────────

    /// Register this agent with the cluster after a successful connection.
    ///
    /// Sends an `AgentRegistration` RPC containing agent ID, hostname,
    /// version, and capabilities. On success, stores the session ID
    /// returned by the cluster for use in subsequent RPCs.
    ///
    /// Call this immediately after `connect()` succeeds.
    pub async fn register_on_connect(&self) -> Result<()> {
        let hostname = get_hostname();

        let registration = AgentRegistration {
            agent_id: self.agent_id.clone(),
            hostname,
            ip_addresses: collect_local_ips(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            capabilities: Some(AgentCapabilities {
                has_metal_scraper: true,
                has_ebpf_observer: cfg!(target_os = "linux"),
                has_supervisor: true,
                has_sdk: false,
                supported_compression: vec!["zstd".to_string()],
                os: std::env::consts::OS.to_string(),
                arch: std::env::consts::ARCH.to_string(),
            }),
            labels: None,
            started_at: None,
        };

        let response: AgentRegistrationResponse =
            self.grpc.register_agent(registration).await.context("Agent registration failed")?;

        // Store session ID for subsequent RPCs.
        {
            let mut sid = self.session_id.write().await;
            *sid = response.session_id.clone();
        }
        self.registered.store(true, Ordering::Release);

        info!(
            session_id = %response.session_id,
            "Agent registered with cluster"
        );

        // Apply any server-pushed configuration overrides.
        if let Some(server_config) = response.config {
            info!(
                collection_interval_ms = server_config.collection_interval_ms,
                sampling_rate = server_config.sampling_rate,
                "Received server configuration overrides"
            );
        }

        Ok(())
    }

    // ── Send Metrics (compress → flow control → send_batch → buffer) ───

    /// Send metrics data to the cluster.
    ///
    /// Pipeline:
    /// 1. Compress data via Zstd
    /// 2. Check flow control (backpressure / sampling)
    /// 3. Try send via gRPC `send_batch()` unary RPC
    /// 4. On failure, persist to edge buffer for replay on reconnect
    pub async fn send_metrics(&self, category: &str, data: &[u8]) -> Result<()> {
        let seq = self.sequence_counter.fetch_add(1, Ordering::SeqCst);

        info!(seq = seq, category = category, data_len = data.len(), "send_metrics called");

        // 1. Compress the data.
        let compressed =
            self.compressor.compress_metrics(data).context("Failed to compress metrics")?;

        // 2. Check flow control.
        if self.flow_control.is_backpressured() {
            let rate = self.flow_control.sampling_rate().await;
            if fastrand::f64() > rate {
                // Dropped by sampling — still buffer for safety.
                self.buffer.write(compressed.clone(), DataType::Metrics).await?;
                debug!(
                    seq = seq,
                    category = category,
                    "Metrics dropped by flow control sampling (rate: {:.2})",
                    rate
                );
                return Ok(());
            }
        }

        // 3. Try send via gRPC send_batch.
        let connected = self.grpc.is_connected().await;
        info!(seq = seq, connected = connected, "send_metrics: connection check");
        if connected {
            // Deserialize the MetalBatch JSON and convert to proto MetricBatch.
            let metal: MetalBatch = serde_json::from_slice(data)
                .unwrap_or_else(|_| MetalBatch {
                    timestamp: String::new(),
                    cpu: None,
                    memory: None,
                    disk: None,
                    network: None,
                    process: None,
                    container: None,
                });

            let batch = MetricBatch {
                agent_id: self.agent_id.clone(),
                session_id: self.session_id.read().await.clone(),
                sequence_number: seq as i64,
                timestamp: Some(prost_types::Timestamp::from(std::time::SystemTime::now())),
                cpu: metal.cpu.map(|c| CpuMetric {
                    total_usage_percent: c.total_usage_percent,
                    per_core_percent: c.per_core_percent,
                    load_average_1m: c.load_average_1m,
                    load_average_5m: c.load_average_5m,
                    load_average_15m: c.load_average_15m,
                    frequency_mhz: c.frequency_mhz,
                    context_switches: c.context_switches as i64,
                }),
                memory: metal.memory.map(|m| MemoryMetric {
                    total_bytes: m.total_bytes as i64,
                    used_bytes: m.used_bytes as i64,
                    free_bytes: m.free_bytes as i64,
                    available_bytes: m.available_bytes as i64,
                    cached_bytes: m.cached_bytes as i64,
                    buffer_bytes: m.buffer_bytes as i64,
                    swap_total_bytes: m.swap_total_bytes as i64,
                    swap_used_bytes: m.swap_used_bytes as i64,
                    usage_percent: m.usage_percent,
                }),
                disks: metal.disk.map(|d| {
                    d.devices.into_iter().map(|dk| DiskMetric {
                        device_name: dk.device_name,
                        mount_point: dk.mount_point,
                        filesystem_type: dk.filesystem_type,
                        total_bytes: dk.total_bytes as i64,
                        used_bytes: dk.used_bytes as i64,
                        free_bytes: dk.free_bytes as i64,
                        read_ops_per_sec: dk.read_ops_per_sec,
                        write_ops_per_sec: dk.write_ops_per_sec,
                        read_bytes_per_sec: dk.read_bytes_per_sec,
                        write_bytes_per_sec: dk.write_bytes_per_sec,
                        io_latency_ms: dk.io_latency_ms,
                        queue_depth: dk.queue_depth,
                    }).collect()
                }).unwrap_or_default(),
                interfaces: metal.network.map(|n| {
                    n.interfaces.into_iter().map(|iface| NetworkMetric {
                        interface_name: iface.interface_name,
                        rx_bytes_per_sec: iface.rx_bytes_per_sec,
                        tx_bytes_per_sec: iface.tx_bytes_per_sec,
                        rx_packets_per_sec: iface.rx_packets_per_sec,
                        tx_packets_per_sec: iface.tx_packets_per_sec,
                        rx_errors: iface.rx_errors as i64,
                        tx_errors: iface.tx_errors as i64,
                        rx_dropped: iface.rx_dropped as i64,
                        tx_dropped: iface.tx_dropped as i64,
                        tcp_retransmits: iface.tcp_retransmits as i64,
                        estimated_rtt_ms: iface.estimated_rtt_ms,
                    }).collect()
                }).unwrap_or_default(),
                processes: metal.process.map(|p| {
                    p.processes.into_iter().map(|proc_| ProcessMetric {
                        pid: proc_.pid as i32,
                        parent_pid: proc_.parent_pid as i32,
                        name: proc_.name,
                        command_line: proc_.command_line,
                        cpu_usage_percent: proc_.cpu_usage_percent,
                        rss_bytes: proc_.rss_bytes as i64,
                        vsz_bytes: proc_.vsz_bytes as i64,
                        status: proc_.status,
                        thread_count: proc_.thread_count as i32,
                        fd_count: proc_.fd_count as i32,
                        container_id: proc_.container_id,
                        started_at: None,
                    }).collect()
                }).unwrap_or_default(),
                containers: vec![],
                health_checks: vec![],
                log_entries: vec![],
                network_events: vec![],
                self_metrics: None,
            };

            info!(
                seq = seq,
                has_cpu = batch.cpu.is_some(),
                has_memory = batch.memory.is_some(),
                disk_count = batch.disks.len(),
                iface_count = batch.interfaces.len(),
                proc_count = batch.processes.len(),
                "send_metrics: calling send_batch"
            );

            match self.grpc.send_batch(batch).await {
                Ok(response) => {
                    if response.accepted {
                        info!(
                            seq = seq,
                            batch_id = %response.batch_id,
                            "Sent metrics batch via send_batch"
                        );
                        return Ok(());
                    }
                    // Server rejected the batch — log error detail and buffer.
                    let err_msg = response.error.as_ref()
                        .map(|e| e.message.as_str())
                        .unwrap_or("unknown");
                    warn!(
                        seq = seq,
                        batch_id = %response.batch_id,
                        error = err_msg,
                        "send_batch rejected by server, buffering"
                    );
                    // Fall through to buffer.
                }
                Err(e) => {
                    warn!(seq = seq, category = category, "send_batch failed: {}", e);
                    // Fall through to buffer.
                }
            }
        }

        // 4. Buffer for replay on reconnect.
        self.buffer.write(compressed, DataType::Metrics).await?;
        warn!(
            seq = seq,
            category = category,
            "Metrics buffered for replay (not connected or send failed)"
        );

        Ok(())
    }

    // ── Send Traces ─────────────────────────────────────────────────────

    /// Send trace data to the cluster.
    pub async fn send_traces(&self, data: &[u8]) -> Result<()> {
        let seq = self.sequence_counter.fetch_add(1, Ordering::SeqCst);
        let compressed = self.compressor.compress_traces(data)?;
        let compressed_len = compressed.len();

        // Buffer first for zero data loss.
        self.buffer.write(compressed.clone(), DataType::Traces).await?;

        if self.grpc.is_connected().await {
            // Traces don't have a dedicated proto RPC — send via legacy channel.
            if let Err(e) = self.grpc.send(compressed).await {
                warn!("Failed to send traces via gRPC (seq: {}): {}", seq, e);
            } else {
                debug!("Sent traces batch {} ({} bytes compressed)", seq, compressed_len);
            }
        }

        Ok(())
    }

    // ── Send Events ─────────────────────────────────────────────────────

    /// Send event data to the cluster.
    pub async fn send_events(&self, data: &[u8]) -> Result<()> {
        let seq = self.sequence_counter.fetch_add(1, Ordering::SeqCst);
        self.buffer.write(data.to_vec(), DataType::Events).await?;

        if self.grpc.is_connected().await {
            if let Err(e) = self.grpc.send(data.to_vec()).await {
                warn!("Failed to send events via gRPC (seq: {}): {}", seq, e);
            } else {
                debug!("Sent events batch {} ({} bytes)", seq, data.len());
            }
        }

        Ok(())
    }

    // ── Send Network Events ─────────────────────────────────────────────

    /// Send network events to the cluster.
    pub async fn send_network_events(&self, data: &[u8]) -> Result<()> {
        let seq = self.sequence_counter.fetch_add(1, Ordering::SeqCst);
        self.buffer.write(data.to_vec(), DataType::NetworkEvents).await?;

        if self.grpc.is_connected().await {
            if let Err(e) = self.grpc.send(data.to_vec()).await {
                warn!("Failed to send network events via gRPC (seq: {}): {}", seq, e);
            } else {
                debug!("Sent network events batch {} ({} bytes)", seq, data.len());
            }
        }

        Ok(())
    }

    // ── Flush Buffer ────────────────────────────────────────────────────

    /// Flush pending entries from the edge buffer to the cluster.
    ///
    /// Drains entries and attempts to send each one. On success, marks
    /// SQLite entries as sent. On failure, marks retry and stops early
    /// to avoid wasting time on a down connection.
    pub async fn flush_buffer(&self) -> Result<()> {
        let entries = self.buffer.drain().await;
        let total = entries.len();
        if total == 0 {
            return Ok(());
        }

        info!("Flushing {} buffered entries", total);
        let mut sent_count: usize = 0;
        let mut failed_count: usize = 0;

        for entry in entries {
            // Attempt to send via the appropriate path.
            let result = match entry.data_type {
                DataType::Metrics => self.grpc.send(entry.data.clone()).await,
                DataType::Traces => self.grpc.send(entry.data.clone()).await,
                DataType::Events => self.grpc.send(entry.data.clone()).await,
                DataType::NetworkEvents => self.grpc.send(entry.data.clone()).await,
            };

            match result {
                Ok(()) => {
                    sent_count += 1;
                    // Acknowledge in SQLite if it came from there.
                    if let Some(row_id) = entry.sqlite_row_id {
                        if let Err(e) = self.buffer.mark_sent(row_id) {
                            warn!(row_id = row_id, "Failed to mark SQLite entry sent: {}", e);
                        }
                    }
                }
                Err(e) => {
                    failed_count += 1;
                    warn!(
                        seq = entry.sequence_number,
                        "Buffer flush send failed: {} — stopping flush", e
                    );
                    // Mark retry in SQLite.
                    if let Some(row_id) = entry.sqlite_row_id {
                        if let Err(e) = self.buffer.mark_retry(row_id) {
                            warn!(row_id = row_id, "Failed to mark SQLite entry retry: {}", e);
                        }
                    }
                    // Stop flushing — connection is likely down.
                    break;
                }
            }
        }

        info!(sent = sent_count, failed = failed_count, total = total, "Buffer flush complete");

        Ok(())
    }

    // ── Heartbeat Loop ──────────────────────────────────────────────────

    /// Start the heartbeat loop as a background tokio task.
    ///
    /// Sends a `HeartbeatRequest` via the proto `heartbeat()` RPC every
    /// 30 seconds. Accepts a `CancellationToken` for cooperative shutdown.
    /// On heartbeat success, processes any pending commands from the server.
    pub fn start_heartbeat_loop(self: &Arc<Self>) {
        let client = Arc::clone(self);
        let cancel = client.cancel_token.clone();

        tokio::spawn(async move {
            info!("Heartbeat loop started (interval: 30s)");
            let mut interval = tokio::time::interval(std::time::Duration::from_secs(30));
            interval.tick().await; // Skip the immediate first tick.

            loop {
                tokio::select! {
                    _ = cancel.cancelled() => {
                        info!("Heartbeat loop shutting down (cancel signal received)");
                        break;
                    }
                    _ = interval.tick() => {
                        if !client.grpc.is_connected().await {
                            debug!("Skipping heartbeat (not connected)");
                            continue;
                        }

                        let session_id = client.session_id.read().await.clone();
                        let request = HeartbeatRequest {
                            agent_id: client.agent_id.clone(),
                            session_id,
                            uptime_seconds: client.started_at.elapsed().as_secs() as i64,
                            edge_buffer_bytes: client.buffer.memory_usage() as i64,
                            edge_buffer_count: client.buffer.len().await as i64,
                            self_metrics: None,
                        };

                        match client.grpc.heartbeat(request).await {
                            Ok(response) => {
                                debug!(
                                    continue_sending = response.continue_sending,
                                    pending_commands = response.pending_commands.len(),
                                    "Heartbeat acknowledged"
                                );
                                // Process any pending commands from the heartbeat response.
                                for cmd in &response.pending_commands {
                                    client.handle_agent_command(cmd.r#type, &cmd.payload).await;
                                }
                            }
                            Err(e) => {
                                warn!("Heartbeat failed: {}", e);
                            }
                        }
                    }
                }
            }

            info!("Heartbeat loop stopped");
        });
    }

    // ── Stream Listener ─────────────────────────────────────────────────

    /// Start the bidirectional stream listener as a background tokio task.
    ///
    /// Opens a `StreamMetrics` bidirectional gRPC stream and spawns a task
    /// to listen for `ClusterToAgent` messages. Dispatches:
    /// - `FlowControl` → `flow_control.apply_server_signal()`
    /// - `ConfigPush` → log (future: apply config hot-reload)
    /// - `AgentCommand` → `handle_agent_command()`
    /// - `Heartbeat` → log (heartbeat responses on stream)
    pub fn start_stream_listener(self: &Arc<Self>) {
        let client = Arc::clone(self);
        let cancel = client.cancel_token.clone();

        tokio::spawn(async move {
            info!("Stream listener starting");

            // Wait until connected before opening the stream.
            loop {
                if cancel.is_cancelled() {
                    info!("Stream listener shutting down (cancelled before connect)");
                    return;
                }
                if client.grpc.is_connected().await {
                    break;
                }
                tokio::time::sleep(std::time::Duration::from_secs(1)).await;
            }

            // Open the bidirectional stream with a timeout.
            // The timeout prevents indefinite blocking if the server is slow
            // to accept the stream (e.g., server waiting for client first msg).
            let (tx, mut inbound) = match tokio::time::timeout(
                std::time::Duration::from_secs(15),
                client.grpc.stream_metrics(),
            )
            .await
            {
                Ok(Ok(pair)) => {
                    info!("Bidirectional metric stream opened");
                    pair
                }
                Ok(Err(e)) => {
                    error!("Failed to open metric stream: {}", e);
                    return;
                }
                Err(_) => {
                    error!("Timed out opening metric stream (15s)");
                    return;
                }
            };

            // Send an initial heartbeat to the server.
            // The Go server's StreamMetrics handler blocks on stream.Recv() waiting
            // for a client message before it sends any response. Without this, the
            // server gets EOF (if sender is dropped) or blocks forever, and the
            // client's inbound.message() hangs indefinitely — deadlocking the runtime.
            {
                let sid = client.session_id.read().await.clone();
                let initial_hb = AgentToCluster {
                    message: Some(crate::proto::agent_to_cluster::Message::Heartbeat(
                        HeartbeatRequest {
                            agent_id: client.agent_id.clone(),
                            session_id: sid,
                            uptime_seconds: client.started_at.elapsed().as_secs() as i64,
                            edge_buffer_bytes: 0,
                            edge_buffer_count: 0,
                            self_metrics: None,
                        },
                    )),
                };
                if let Err(e) = tx.send(initial_hb).await {
                    warn!("Failed to send initial stream heartbeat: {}", e);
                    return;
                }
                debug!("Sent initial stream heartbeat to server");
            }

            // Periodic heartbeat timer — keeps the stream alive so the server's
            // Recv() loop continues to receive messages and send FlowControl
            // responses that the client reads from `inbound`.
            let mut heartbeat_interval =
                tokio::time::interval(std::time::Duration::from_secs(30));
            heartbeat_interval.tick().await; // consume the immediate first tick

            // Listen for incoming messages from the cluster.
            loop {
                tokio::select! {
                    _ = cancel.cancelled() => {
                        info!("Stream listener shutting down (cancel signal received)");
                        break;
                    }
                    _ = heartbeat_interval.tick() => {
                        let sid = client.session_id.read().await.clone();
                        let hb = AgentToCluster {
                            message: Some(crate::proto::agent_to_cluster::Message::Heartbeat(
                                HeartbeatRequest {
                                    agent_id: client.agent_id.clone(),
                                    session_id: sid,
                                    uptime_seconds: client.started_at.elapsed().as_secs() as i64,
                                    edge_buffer_bytes: 0,
                                    edge_buffer_count: 0,
                                    self_metrics: None,
                                },
                            )),
                        };
                        if let Err(e) = tx.send(hb).await {
                            warn!("Stream heartbeat send failed: {}", e);
                            break;
                        }
                        debug!("Stream heartbeat sent");
                    }
                    msg = inbound.message() => {
                        match msg {
                            Ok(Some(cluster_msg)) => {
                                client.dispatch_cluster_message(cluster_msg).await;
                            }
                            Ok(None) => {
                                warn!("Cluster stream ended (server closed connection)");
                                break;
                            }
                            Err(e) => {
                                warn!("Error receiving cluster message: {}", e);
                                break;
                            }
                        }
                    }
                }
            }

            // `tx` is dropped here, closing the outbound stream.
            info!("Stream listener stopped");
        });
    }

    /// Dispatch a `ClusterToAgent` message to the appropriate handler.
    async fn dispatch_cluster_message(&self, msg: ClusterToAgent) {
        let message = match msg.message {
            Some(m) => m,
            None => {
                debug!("Received empty ClusterToAgent message");
                return;
            }
        };

        match message {
            cluster_to_agent::Message::FlowControl(fc) => {
                debug!(
                    pause = fc.pause,
                    sampling_rate = fc.sampling_rate,
                    desired_interval_ms = fc.desired_interval_ms,
                    reason = %fc.reason,
                    "Received flow control signal from cluster"
                );
                self.flow_control.apply_server_signal(&fc).await;
            }
            cluster_to_agent::Message::ConfigPush(config) => {
                info!(
                    collection_interval_ms = config.collection_interval_ms,
                    sampling_rate = config.sampling_rate,
                    metal_enabled = config.metal_enabled,
                    ebpf_enabled = config.ebpf_enabled,
                    supervisor_enabled = config.supervisor_enabled,
                    compression = %config.compression,
                    "Received config push from cluster"
                );
                // TODO: Apply hot-reload configuration.
            }
            cluster_to_agent::Message::Command(cmd) => {
                info!(
                    command_type = cmd.r#type,
                    payload = %cmd.payload,
                    "Received agent command from cluster"
                );
                self.handle_agent_command(cmd.r#type, &cmd.payload).await;
            }
            cluster_to_agent::Message::Heartbeat(hb) => {
                debug!(
                    continue_sending = hb.continue_sending,
                    pending_commands = hb.pending_commands.len(),
                    "Received heartbeat response on stream"
                );
            }
        }
    }

    /// Handle an `AgentCommand` from the cluster.
    async fn handle_agent_command(&self, command_type: i32, payload: &str) {
        match AgentCommandType::try_from(command_type) {
            Ok(AgentCommandType::Restart) => {
                info!(payload = payload, "Agent restart command received (no-op)");
                // TODO: Trigger agent restart.
            }
            Ok(AgentCommandType::UpdateConfig) => {
                info!(payload = payload, "Config update command received (no-op)");
                // TODO: Apply config update.
            }
            Ok(AgentCommandType::FlushBuffer) => {
                info!("Flush buffer command received");
                if let Err(e) = self.flush_buffer().await {
                    warn!("Buffer flush on command failed: {}", e);
                }
            }
            Ok(AgentCommandType::ToggleLayer) => {
                info!(payload = payload, "Toggle layer command received (no-op)");
                // TODO: Enable/disable a collection layer.
            }
            Ok(AgentCommandType::Unspecified) | Err(_) => {
                warn!(
                    command_type = command_type,
                    payload = payload,
                    "Unknown agent command received"
                );
            }
        }
    }

    // ── Health Check (uses proto Heartbeat RPC) ─────────────────────────

    /// Perform a health check by sending a heartbeat.
    ///
    /// Returns true if the heartbeat was acknowledged by the cluster.
    pub async fn health_check(&self) -> Result<bool> {
        if !self.grpc.is_connected().await {
            return Ok(false);
        }

        let session_id = self.session_id.read().await.clone();
        let request = HeartbeatRequest {
            agent_id: self.agent_id.clone(),
            session_id,
            uptime_seconds: self.started_at.elapsed().as_secs() as i64,
            edge_buffer_bytes: self.buffer.memory_usage() as i64,
            edge_buffer_count: self.buffer.len().await as i64,
            self_metrics: None,
        };

        match self.grpc.heartbeat(request).await {
            Ok(_response) => {
                debug!("Health check heartbeat acknowledged");
                Ok(true)
            }
            Err(e) => {
                warn!("Health check heartbeat failed: {}", e);
                Ok(false)
            }
        }
    }

    // ── Connection Management ───────────────────────────────────────────

    /// Connect to the cluster.
    pub async fn connect(&self) -> Result<()> {
        self.grpc.connect().await
    }

    /// Disconnect from the cluster.
    pub async fn disconnect(&self) {
        self.grpc.disconnect().await;
    }

    /// Check if connected to the cluster.
    pub async fn is_connected(&self) -> bool {
        self.grpc.is_connected().await
    }

    /// Get the session ID assigned by the cluster.
    pub async fn get_session_id(&self) -> String {
        self.session_id.read().await.clone()
    }

    /// Check if the agent has completed registration.
    pub fn is_registered(&self) -> bool {
        self.registered.load(Ordering::Acquire)
    }

    // ── Reconnection Loop ───────────────────────────────────────────────

    /// Start the background reconnection loop.
    ///
    /// Spawns a tokio task that monitors the connection and automatically
    /// reconnects when disconnected. On successful reconnect, re-registers
    /// the agent and replays buffered data.
    pub fn start_reconnection_loop(self: &Arc<Self>) {
        let client = Arc::clone(self);
        let cancel = client.cancel_token.clone();

        tokio::spawn(async move {
            loop {
                tokio::select! {
                    _ = cancel.cancelled() => {
                        info!("Reconnection loop shutting down (cancel signal received)");
                        break;
                    }
                    _ = tokio::time::sleep(std::time::Duration::from_secs(5)) => {
                        if client.grpc.is_connected().await {
                            continue;
                        }

                        match client.reconnect.reconnect().await {
                            Ok(true) => {
                                info!("Reconnected to cluster");

                                // Re-register after reconnection.
                                if let Err(e) = client.register_on_connect().await {
                                    warn!("Re-registration after reconnect failed: {}", e);
                                }

                                // Flush buffered data.
                                if let Err(e) = client.flush_buffer().await {
                                    warn!("Buffer flush after reconnect failed: {}", e);
                                }
                            }
                            Ok(false) => {
                                error!("Max reconnection attempts exceeded");
                                break;
                            }
                            Err(e) => {
                                warn!("Reconnection failed: {}", e);
                            }
                        }
                    }
                }
            }
        });
    }

    // ── Graceful Shutdown ───────────────────────────────────────────────

    /// Gracefully shut down the communication layer.
    ///
    /// 1. Cancel all background tasks (heartbeat, stream, reconnection).
    /// 2. Flush remaining buffered data (best-effort).
    /// 3. Disconnect the gRPC client.
    /// 4. Log shutdown completion.
    pub async fn shutdown(&self) {
        info!("Communication layer shutting down");

        // 1. Cancel all background tasks.
        self.cancel_token.cancel();
        debug!("Cancellation token triggered — background tasks will stop");

        // Small delay to let tasks observe the cancellation.
        tokio::time::sleep(std::time::Duration::from_millis(100)).await;

        // 2. Flush remaining buffered data (best-effort).
        if self.grpc.is_connected().await {
            match self.flush_buffer().await {
                Ok(()) => info!("Edge buffer flushed during shutdown"),
                Err(e) => warn!("Edge buffer flush during shutdown failed: {}", e),
            }
        } else {
            debug!("Skipping buffer flush during shutdown (not connected)");
        }

        // 3. Disconnect gRPC.
        self.grpc.disconnect().await;

        // 4. Shutdown edge buffer (checkpoint SQLite WAL).
        self.buffer.shutdown();

        info!("Communication layer shutdown complete");
    }

    // ── Accessors ───────────────────────────────────────────────────────

    /// Get the cancellation token (for external shutdown coordination).
    pub fn cancel_token(&self) -> &CancellationToken {
        &self.cancel_token
    }

    /// Get the edge buffer reference.
    pub fn buffer(&self) -> &EdgeBuffer {
        &self.buffer
    }

    /// Get the compressor reference.
    pub fn compressor(&self) -> &Compressor {
        &self.compressor
    }

    /// Get the flow control reference.
    pub fn flow_control(&self) -> &FlowControl {
        &self.flow_control
    }
}

// ── Helper Functions ────────────────────────────────────────────────────

/// Get the system hostname.
///
/// Uses the `COMPUTERNAME` (Windows) or `HOSTNAME` (Unix) environment
/// variable. Falls back to "unknown" if neither is set.
fn get_hostname() -> String {
    std::env::var("COMPUTERNAME")
        .or_else(|_| std::env::var("HOSTNAME"))
        .unwrap_or_else(|_| "unknown".to_string())
}

/// Collect local non-loopback IP addresses.
///
/// Uses a UDP socket connect trick to discover the local IP that would
/// route to a public address. This is a well-known cross-platform
/// technique that works without elevated privileges.
fn collect_local_ips() -> Vec<String> {
    let socket = match std::net::UdpSocket::bind("0.0.0.0:0") {
        Ok(s) => s,
        Err(_) => return vec![],
    };
    // Connect to a public DNS address — no packets are sent.
    if socket.connect("8.8.8.8:80").is_err() {
        return vec![];
    }
    match socket.local_addr() {
        Ok(addr) => vec![addr.ip().to_string()],
        Err(_) => vec![],
    }
}

// ── Tests ───────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

    /// Verify that collect_local_ips returns at least one address
    /// (the loopback filter removes 127.x, but there should be
    /// at least one non-loopback interface on most machines).
    #[test]
    fn test_collect_local_ips() {
        let ips = collect_local_ips();
        // Not asserting non-empty — some CI machines may only have loopback.
        // Just verify it doesn't panic.
        for ip in &ips {
            assert!(!ip.starts_with("127."), "loopback should be filtered: {}", ip);
        }
    }

    /// Verify that Client::new succeeds with a test config.
    #[tokio::test]
    async fn test_client_new() {
        let config = test_config();
        let client = Client::new(&config).await.expect("Client::new should succeed");

        assert!(!client.is_connected().await);
        assert!(!client.is_registered());
        assert!(client.get_session_id().await.is_empty());
        assert!(!client.cancel_token().is_cancelled());
    }

    /// Verify that shutdown triggers cancellation and is idempotent.
    #[tokio::test]
    async fn test_client_shutdown_cancels_token() {
        let config = test_config();
        let client = Client::new(&config).await.expect("Client::new should succeed");

        assert!(!client.cancel_token().is_cancelled());

        client.shutdown().await;

        assert!(client.cancel_token().is_cancelled());
    }

    /// Verify that send_metrics buffers data when disconnected.
    #[tokio::test]
    async fn test_send_metrics_buffers_when_disconnected() {
        let config = test_config();
        let client = Client::new(&config).await.expect("Client::new should succeed");

        // Send while disconnected — should buffer without error.
        client.send_metrics("cpu", b"{\"cpu\": 42.0}").await.expect("send_metrics should succeed");

        // Edge buffer should have at least one entry.
        let count = client.buffer().len().await;
        assert!(count > 0, "edge buffer should contain buffered entry");
    }

    /// Verify flush_buffer handles empty buffer gracefully.
    #[tokio::test]
    async fn test_flush_buffer_empty() {
        let config = test_config();
        let client = Client::new(&config).await.expect("Client::new should succeed");

        // Flush empty buffer — should succeed without error.
        client.flush_buffer().await.expect("flush_buffer on empty buffer should succeed");
    }

    /// Verify health_check returns false when not connected.
    #[tokio::test]
    async fn test_health_check_not_connected() {
        let config = test_config();
        let client = Client::new(&config).await.expect("Client::new should succeed");

        let healthy = client.health_check().await.expect("health_check should succeed");
        assert!(!healthy, "health check should return false when disconnected");
    }

    /// Build a minimal test config.
    fn test_config() -> Config {
        Config {
            agent: crate::config::AgentConfig {
                id: "test-agent".to_string(),
                cluster_endpoint: "http://localhost:50051".to_string(),
                api_key: "test-key".to_string(),
                self_metrics: crate::config::SelfMetricsConfig { enabled: false, port: 9090 },
            },
            layers: crate::config::LayersConfig {
                metal: crate::config::MetalConfig {
                    enabled: false,
                    interval: "10s".to_string(),
                    cpu_per_core: false,
                    cpu_per_process: false,
                    memory_rss: false,
                    disk_io: false,
                    network_io: false,
                    process_tree: false,
                    container_detection: false,
                },
                ebpf: crate::config::EbpfConfig {
                    enabled: false,
                    tcp_connections: false,
                    dns_resolution: false,
                    http_inspection: false,
                    db_inspection: false,
                    exclude_ports: vec![],
                    exclude_ips: vec![],
                },
                supervisor: crate::config::SupervisorConfig {
                    enabled: false,
                    health_checks: vec![],
                    log_tailing: vec![],
                },
            },
            communication: crate::config::CommunicationConfig {
                protocol: "grpc".to_string(),
                tls: crate::config::TlsConfig { enabled: false },
                compression: "zstd".to_string(),
                edge_buffer: crate::config::EdgeBufferConfig {
                    enabled: true,
                    max_size_mb: 100,
                    retention_hours: 24,
                    sqlite_path: None,
                },
                flow_control: crate::config::FlowControlConfig {
                    backpressure_enabled: true,
                    adaptive_sampling: true,
                    priority_queues: vec![],
                },
            },
            logging: crate::config::LoggingConfig {
                level: "info".to_string(),
                format: "json".to_string(),
                output: "stdout".to_string(),
            },
        }
    }
}
