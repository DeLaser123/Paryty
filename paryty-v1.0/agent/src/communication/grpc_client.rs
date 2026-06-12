#![allow(dead_code)]

//! gRPC Client for Paryty Agent
//!
//! Manages the bidirectional gRPC stream to the Paryty Cluster.
//! Uses tonic for the gRPC transport layer with prost for serialization.
//!
//! The client wraps the proto-generated `IngestionServiceClient` and manages
//! its lifecycle through a connection state machine. All proto RPCs
//! (RegisterAgent, SendBatch, Heartbeat, StreamMetrics, ReportNetworkEvents)
//! are exposed as typed methods that delegate to the underlying tonic client.

use anyhow::{Context, Result};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use tokio::sync::{mpsc, Mutex, RwLock};
use tonic::metadata::MetadataValue;
use tonic::transport::{Channel, Endpoint};
use tracing::{debug, info, warn};

use crate::config::Config;
use crate::proto::paryty::v1::ingestion_service_client::IngestionServiceClient;
use crate::proto::paryty::v1::twin_service_client::TwinServiceClient;
use crate::proto::paryty::v1::{
    AgentRegistration, AgentRegistrationResponse, AgentToCluster, ClusterToAgent, HeartbeatRequest,
    HeartbeatResponse, MetricBatch, NetworkEventBatch, NetworkEventResponse,
    ResolveIdentityRequest, SendBatchResponse,
};

use super::tenant::TenantCache;

/// Connection state for the gRPC client.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ConnectionState {
    Disconnected,
    Connecting,
    Connected,
    Reconnecting,
}

/// Metrics tracked by the gRPC client.
#[derive(Debug, Default)]
pub struct ClientMetrics {
    pub messages_sent: AtomicU64,
    pub messages_received: AtomicU64,
    pub bytes_sent: AtomicU64,
    pub bytes_received: AtomicU64,
    pub reconnection_count: AtomicU64,
    pub errors: AtomicU64,
}

/// The main gRPC client for communicating with the Paryty Cluster.
///
/// Wraps the proto-generated `IngestionServiceClient` behind a connection
/// state machine. The proto client is lazily created on the first `connect()`
/// call and destroyed on `disconnect()`.
pub struct GrpcClient {
    /// Cluster endpoint URL (e.g. "http://cluster:50051").
    endpoint: String,
    /// Whether TLS is enabled for the connection.
    tls_enabled: bool,
    /// Current connection state.
    state: Arc<RwLock<ConnectionState>>,
    /// The gRPC channel (if connected).
    channel: Arc<Mutex<Option<Channel>>>,
    /// The typed proto client (if connected).
    ///
    /// Uses `tokio::sync::Mutex` because RPC methods require `&mut self`
    /// and are async. Lock is held only for the duration of each RPC call.
    client: Arc<Mutex<Option<IngestionServiceClient<Channel>>>>,
    /// Stored tonic Endpoint for creating separate streaming connections.
    ///
    /// The StreamMetrics bidirectional RPC holds its connection's tower buffer
    /// worker for the entire stream lifetime, blocking send_batch/heartbeat.
    /// By creating a separate connection for streaming, unary RPCs stay free.
    endpoint_config: Arc<Mutex<Option<Endpoint>>>,
    /// Whether the client should be running.
    running: Arc<AtomicBool>,
    /// Client metrics.
    metrics: Arc<ClientMetrics>,
    /// Channel for sending messages to the stream.
    tx: mpsc::Sender<Vec<u8>>,
    /// Channel for receiving messages from the stream.
    rx: Arc<Mutex<mpsc::Receiver<Vec<u8>>>>,
    /// Optional API key for authenticating with the cluster.
    ///
    /// When set, the API key is attached as `x-api-key` gRPC metadata
    /// on all requests. The server uses this to resolve the
    /// tenant for this agent.
    ///
    /// Wrapped in `Arc<RwLock>` for atomic updates during key rotation.
    api_key: Arc<RwLock<Option<String>>>,
    /// Optional tenant ID for multi-tenant routing.
    ///
    /// When set, the tenant ID is attached as `x-tenant-id` gRPC metadata
    /// on all requests. This overrides tenant resolution from API keys.
    tenant_id: Option<String>,
    /// Local cache for the tenant ID assigned after registration.
    tenant_cache: Option<TenantCache>,

    // ── Identity Metadata ──────────────────────────────────────────────
    /// Assigned twin ID for identity-aware routing.
    ///
    /// When set, attached as `x-twin-id` gRPC metadata on ALL requests.
    /// Set via `update_identity()` after registration/identity resolution.
    twin_id: Arc<RwLock<Option<String>>>,
    /// Assigned client ID for identity-aware routing.
    ///
    /// When set, attached as `x-client-id` gRPC metadata on ALL requests.
    /// Set via `update_identity()` after registration/identity resolution.
    client_id: Arc<RwLock<Option<String>>>,
    /// Path to the agent config file, used for runtime key refresh.
    config_path: Arc<RwLock<String>>,
}

impl GrpcClient {
    /// Create a new gRPC client from the full agent configuration.
    ///
    /// Extracts the cluster endpoint, TLS setting, API key, and tenant ID from the config.
    pub fn new(config: &Config) -> Self {
        let endpoint = config.agent.cluster_endpoint.clone();
        let tls_enabled = config.communication.tls.enabled;
        let (tx, rx) = mpsc::channel(1024);

        let api_key =
            if config.agent.api_key.is_empty() { None } else { Some(config.agent.api_key.clone()) };

        let tenant_id = config.agent.tenant_id.clone();

        Self {
            endpoint,
            tls_enabled,
            state: Arc::new(RwLock::new(ConnectionState::Disconnected)),
            channel: Arc::new(Mutex::new(None)),
            client: Arc::new(Mutex::new(None)),
            endpoint_config: Arc::new(Mutex::new(None)),
            running: Arc::new(AtomicBool::new(false)),
            metrics: Arc::new(ClientMetrics::default()),
            tx,
            rx: Arc::new(Mutex::new(rx)),
            api_key: Arc::new(RwLock::new(api_key)),
            tenant_id,
            tenant_cache: None,
            twin_id: Arc::new(RwLock::new(None)),
            client_id: Arc::new(RwLock::new(None)),
            config_path: Arc::new(RwLock::new(String::new())),
        }
    }

    /// Create a new gRPC client with a specific endpoint (TLS disabled).
    ///
    /// Useful for testing and manual configuration.
    pub fn with_endpoint(endpoint: &str) -> Self {
        let (tx, rx) = mpsc::channel(1024);

        Self {
            endpoint: endpoint.to_string(),
            tls_enabled: false,
            state: Arc::new(RwLock::new(ConnectionState::Disconnected)),
            channel: Arc::new(Mutex::new(None)),
            client: Arc::new(Mutex::new(None)),
            endpoint_config: Arc::new(Mutex::new(None)),
            running: Arc::new(AtomicBool::new(false)),
            metrics: Arc::new(ClientMetrics::default()),
            tx,
            rx: Arc::new(Mutex::new(rx)),
            api_key: Arc::new(RwLock::new(None)),
            tenant_id: None,
            tenant_cache: None,
            twin_id: Arc::new(RwLock::new(None)),
            client_id: Arc::new(RwLock::new(None)),
            config_path: Arc::new(RwLock::new(String::new())),
        }
    }

    /// Set the tenant cache for persistence of tenant ID across restarts.
    ///
    /// Must be called before `register_agent()` if tenant caching is desired.
    pub fn set_tenant_cache(&mut self, cache: TenantCache) {
        self.tenant_cache = Some(cache);
    }

    // ── Identity ───────────────────────────────────────────────────────

    /// Update the identity metadata attached to all outgoing gRPC requests.
    ///
    /// Called by the `Client` layer after identity is resolved or assigned.
    /// When set, `x-twin-id` and `x-client-id` are attached as gRPC metadata
    /// on all subsequent requests (RegisterAgent, SendBatch, Heartbeat,
    /// StreamMetrics, ReportNetworkEvents).
    pub fn update_identity(&self, twin_id: Option<String>, client_id: Option<String>) {
        // We use block_in_place + block_on because this may be called from
        // async context but RwLock::write is synchronous.
        let tid_guard = self.twin_id.try_write();
        let cid_guard = self.client_id.try_write();

        if let Ok(mut tid) = tid_guard {
            *tid = twin_id.clone();
        }
        if let Ok(mut cid) = cid_guard {
            *cid = client_id.clone();
        }

        if twin_id.is_some() && client_id.is_some() {
            info!(
                twin_id = %twin_id.as_deref().unwrap_or(""),
                client_id = %client_id.as_deref().unwrap_or(""),
                "gRPC identity metadata updated"
            );
        } else {
            info!("gRPC identity metadata cleared");
        }
    }

    /// Get the current twin ID.
    pub async fn twin_id(&self) -> Option<String> {
        self.twin_id.read().await.clone()
    }

    /// Get the current client ID.
    pub async fn client_id(&self) -> Option<String> {
        self.client_id.read().await.clone()
    }

    /// Set the config file path for runtime key refresh.
    pub async fn set_config_path(&self, path: String) {
        let mut guard = self.config_path.write().await;
        *guard = path;
    }

    /// Update the API key atomically.
    ///
    /// Called during key rotation recovery. The new key takes effect on the
    /// next gRPC request.
    pub async fn update_api_key(&self, new_key: Option<String>) {
        let mut guard = self.api_key.write().await;
        *guard = new_key;
        info!("API key updated in gRPC client");
    }

    /// Attempt to refresh the API key from config file or environment variable.
    ///
    /// Called when an `UNAUTHENTICATED` error is detected. Returns `true` if
    /// the key was successfully refreshed (new key loaded), `false` otherwise.
    pub async fn refresh_api_key(&self) -> bool {
        use crate::config;

        let config_path = self.config_path.read().await.clone();
        let new_key = match config::reread_api_key(&config_path) {
            Ok(Some(key)) => key,
            Ok(None) => {
                warn!("No API key found in config file or environment during refresh");
                return false;
            }
            Err(e) => {
                warn!(error = %e, "Failed to re-read API key from config");
                return false;
            }
        };

        // Check if the key actually changed.
        let current = self.api_key.read().await;
        if current.as_deref() == Some(new_key.as_str()) {
            info!("API key refresh: key unchanged, no update needed");
            return false;
        }
        drop(current);

        self.update_api_key(Some(new_key)).await;
        true
    }

    /// Attach identity metadata (x-twin-id, x-client-id) to a tonic request.
    ///
    /// Called internally before every RPC. If identity is not assigned,
    /// this is a no-op.
    fn attach_identity_metadata<T>(&self, request: &mut tonic::Request<T>) {
        // We read synchronously; at this point the lock should not be contended.
        // Using try_read to avoid blocking in hot path.
        if let Ok(tid_guard) = self.twin_id.try_read() {
            if let Some(ref twin_id) = *tid_guard {
                match MetadataValue::try_from(twin_id.as_str()) {
                    Ok(value) => {
                        request.metadata_mut().insert("x-twin-id", value);
                    }
                    Err(e) => {
                        warn!(
                            error = %e,
                            "Invalid twin ID format — skipping x-twin-id metadata"
                        );
                    }
                }
            }
        }

        if let Ok(cid_guard) = self.client_id.try_read() {
            if let Some(ref client_id) = *cid_guard {
                match MetadataValue::try_from(client_id.as_str()) {
                    Ok(value) => {
                        request.metadata_mut().insert("x-client-id", value);
                    }
                    Err(e) => {
                        warn!(
                            error = %e,
                            "Invalid client ID format — skipping x-client-id metadata"
                        );
                    }
                }
            }
        }
    }

    /// Attach API key metadata (x-api-key) to a tonic request.
    ///
    /// Called internally before every RPC. If no API key is configured,
    /// this is a no-op.
    fn attach_api_key_metadata<T>(&self, request: &mut tonic::Request<T>) {
        if let Ok(guard) = self.api_key.try_read() {
            if let Some(ref api_key) = *guard {
                match MetadataValue::try_from(api_key.as_str()) {
                    Ok(value) => {
                        request.metadata_mut().insert("x-api-key", value);
                    }
                    Err(e) => {
                        warn!(
                            error = %e,
                            "Invalid API key format — skipping x-api-key metadata"
                        );
                    }
                }
            }
        }
    }

    /// Resolve identity via the TwinService gRPC call.
    ///
    /// Called after registration to discover the assigned twin/client ID.
    /// Creates a `TwinServiceClient` on the same channel used by the
    /// `IngestionServiceClient`. The `Client` layer treats this as
    /// best-effort and falls back to env-var identity on failure.
    pub async fn resolve_identity(&self, agent_id: &str) -> Result<(String, String)> {
        // Acquire the channel to create a TwinServiceClient on the same connection.
        let channel_guard = self.channel.lock().await;
        let channel = match channel_guard.as_ref() {
            Some(ch) => ch.clone(),
            None => {
                anyhow::bail!("Cannot resolve identity: not connected");
            }
        };
        drop(channel_guard);

        let mut twin_client = TwinServiceClient::new(channel);

        let request =
            tonic::Request::new(ResolveIdentityRequest { agent_id: agent_id.to_string() });

        let response =
            twin_client.resolve_identity(request).await.context("ResolveIdentity RPC failed")?;

        let resp = response.into_inner();

        if !resp.assigned {
            anyhow::bail!("Agent not yet assigned to a twin");
        }

        Ok((resp.twin_id, resp.client_id))
    }

    /// Connect to the cluster and create the typed proto client.
    ///
    /// Creates a `Channel` via `tonic::transport::Endpoint`, wraps it in an
    /// `IngestionServiceClient`, and enables Gzip compression for outgoing
    /// requests. On success the state machine transitions to `Connected`.
    pub async fn connect(&self) -> Result<()> {
        info!(endpoint = %self.endpoint, "Connecting to cluster");
        *self.state.write().await = ConnectionState::Connecting;

        // Build the endpoint with transport-level settings.
        // Prepend http:// if no scheme is present (config may use bare host:port).
        let endpoint_uri = if self.endpoint.contains("://") {
            self.endpoint.clone()
        } else {
            format!("http://{}", self.endpoint)
        };
        let mut endpoint = Endpoint::from_shared(endpoint_uri)
            .context("Invalid endpoint URL")?
            .timeout(std::time::Duration::from_secs(60))
            .keep_alive_timeout(std::time::Duration::from_secs(10))
            .keep_alive_while_idle(true);

        // Configure TLS if enabled.
        if self.tls_enabled {
            let tls_config = tonic::transport::ClientTlsConfig::new();
            endpoint = endpoint.tls_config(tls_config).context("Failed to configure TLS")?;
            debug!("TLS enabled for cluster connection");
        }

        // Establish the transport channel.
        let channel = endpoint.connect().await.context("Failed to connect to cluster")?;

        // Create the typed proto client with Gzip compression for outgoing requests.
        let proto_client = IngestionServiceClient::new(channel.clone());

        // Store the client and channel, update state.
        *self.client.lock().await = Some(proto_client);
        *self.channel.lock().await = Some(channel);
        *self.endpoint_config.lock().await = Some(endpoint);
        *self.state.write().await = ConnectionState::Connected;
        self.running.store(true, Ordering::Release);

        info!("Connected to cluster successfully");
        Ok(())
    }

    /// Disconnect from the cluster and destroy the proto client.
    pub async fn disconnect(&self) {
        info!("Disconnecting from cluster");
        self.running.store(false, Ordering::Release);
        *self.client.lock().await = None;
        *self.channel.lock().await = None;
        *self.endpoint_config.lock().await = None;
        *self.state.write().await = ConnectionState::Disconnected;
        info!("Disconnected from cluster");
    }

    /// Get the current connection state.
    pub async fn state(&self) -> ConnectionState {
        *self.state.read().await
    }

    /// Check if the client is connected.
    pub async fn is_connected(&self) -> bool {
        *self.state.read().await == ConnectionState::Connected
    }

    /// Get the cluster endpoint URL.
    pub fn endpoint(&self) -> &str {
        &self.endpoint
    }

    /// Get the sender for the message channel.
    pub fn sender(&self) -> mpsc::Sender<Vec<u8>> {
        self.tx.clone()
    }

    /// Get client metrics.
    pub fn metrics(&self) -> &ClientMetrics {
        &self.metrics
    }

    // ── Proto RPC Methods ──────────────────────────────────────────────

    /// Register this agent with the cluster.
    ///
    /// Sends an `AgentRegistration` message and returns the server's
    /// `AgentRegistrationResponse` containing the session ID and any
    /// server-pushed configuration.
    ///
    /// If an API key is configured, it is attached as `x-api-key` gRPC
    /// metadata so the server can resolve the tenant. After successful
    /// registration the tenant ID is cached to disk (if a `TenantCache`
    /// was provided via `set_tenant_cache()`).
    pub async fn register_agent(
        &self,
        registration: AgentRegistration,
    ) -> Result<AgentRegistrationResponse> {
        let mut guard = self.client.lock().await;
        let client = guard.as_mut().context("Not connected: proto client unavailable")?;

        // Build the tonic Request so we can attach metadata.
        let mut request = tonic::Request::new(registration);

        // Attach API key as gRPC metadata if configured.
        self.attach_api_key_metadata(&mut request);

        // Attach tenant ID as gRPC metadata if configured.
        if let Some(ref tenant_id) = self.tenant_id {
            match MetadataValue::try_from(tenant_id.as_str()) {
                Ok(value) => {
                    request.metadata_mut().insert("x-tenant-id", value);
                    debug!(tenant_id = %tenant_id, "Attached x-tenant-id metadata to registration request");
                }
                Err(e) => {
                    warn!(
                        error = %e,
                        "Invalid tenant ID format — skipping x-tenant-id metadata"
                    );
                }
            }
        }

        // Attach identity metadata (x-twin-id, x-client-id).
        self.attach_identity_metadata(&mut request);

        let response = client
            .register_agent(request)
            .await
            .map_err(|status| anyhow::anyhow!("RegisterAgent RPC failed: {}", status))?;

        self.metrics.messages_sent.fetch_add(1, Ordering::Relaxed);

        let resp = response.into_inner();

        // Cache the tenant ID from the agent_id for offline restart support.
        // The server-side tenant is resolved from the API key; locally we
        // persist the agent_id as the tenant identifier so the agent can
        // report itself on restart without re-authenticating.
        if let Some(ref cache) = self.tenant_cache {
            let tenant_id = &resp.session_id;
            match cache.write(tenant_id).await {
                Ok(()) => {
                    debug!(tenant_id = %tenant_id, "Tenant ID cached to disk");
                }
                Err(e) => {
                    warn!(error = %e, "Failed to cache tenant ID (non-fatal)");
                }
            }
        }

        info!("Agent registered successfully");
        Ok(resp)
    }

    /// Send a single metric batch to the cluster (unary RPC).
    ///
    /// Returns the `SendBatchResponse` indicating whether the batch was
    /// accepted, the server-assigned batch ID, and a server timestamp.
    pub async fn send_batch(&self, batch: MetricBatch) -> Result<SendBatchResponse> {
        let mut guard = self.client.lock().await;
        let client = guard.as_mut().context("Not connected: proto client unavailable")?;

        // Build the tonic Request so we can attach metadata.
        let mut request = tonic::Request::new(batch);

        // Attach API key as gRPC metadata if configured.
        self.attach_api_key_metadata(&mut request);

        // Attach tenant ID as gRPC metadata if configured.
        if let Some(ref tenant_id) = self.tenant_id {
            match MetadataValue::try_from(tenant_id.as_str()) {
                Ok(value) => {
                    request.metadata_mut().insert("x-tenant-id", value);
                }
                Err(e) => {
                    warn!(
                        error = %e,
                        "Invalid tenant ID format — skipping x-tenant-id metadata for send_batch"
                    );
                }
            }
        }

        // Attach identity metadata (x-twin-id, x-client-id).
        self.attach_identity_metadata(&mut request);

        let response = client
            .send_batch(request)
            .await
            .map_err(|status| anyhow::anyhow!("SendBatch RPC failed: {}", status))?;

        self.metrics.messages_sent.fetch_add(1, Ordering::Relaxed);
        debug!("Metric batch sent successfully");

        Ok(response.into_inner())
    }

    /// Send a heartbeat to the cluster.
    ///
    /// Returns the `HeartbeatResponse` containing server time for clock
    /// synchronization, a `continue_sending` flag, and any pending commands.
    pub async fn heartbeat(&self, request: HeartbeatRequest) -> Result<HeartbeatResponse> {
        let mut guard = self.client.lock().await;
        let client = guard.as_mut().context("Not connected: proto client unavailable")?;

        let mut tonic_request = tonic::Request::new(request);

        // Attach API key as gRPC metadata if configured.
        self.attach_api_key_metadata(&mut tonic_request);

        // Attach tenant ID as gRPC metadata if configured.
        if let Some(ref tenant_id) = self.tenant_id {
            if let Ok(value) = MetadataValue::try_from(tenant_id.as_str()) {
                tonic_request.metadata_mut().insert("x-tenant-id", value);
            }
        }

        // Attach identity metadata (x-twin-id, x-client-id).
        self.attach_identity_metadata(&mut tonic_request);

        let response = client
            .heartbeat(tonic_request)
            .await
            .map_err(|status| anyhow::anyhow!("Heartbeat RPC failed: {}", status))?;

        self.metrics.messages_sent.fetch_add(1, Ordering::Relaxed);
        self.metrics.messages_received.fetch_add(1, Ordering::Relaxed);
        debug!("Heartbeat acknowledged");

        Ok(response.into_inner())
    }

    /// Report network events to the cluster (client-streaming RPC).
    ///
    /// Sends a single `NetworkEventBatch` through a client-streaming channel
    /// and returns the `NetworkEventResponse` with accepted/rejected counts.
    ///
    /// Uses a `tokio::sync::mpsc` channel wrapped in a `ReceiverStream` to
    /// satisfy tonic's `IntoStreamingRequest` trait. The channel is closed
    /// after the single batch is sent, signaling end-of-stream to the server.
    pub async fn report_network_events(
        &self,
        batch: NetworkEventBatch,
    ) -> Result<NetworkEventResponse> {
        let mut guard = self.client.lock().await;
        let client = guard.as_mut().context("Not connected: proto client unavailable")?;

        // Create a channel for the client-streaming request.
        let (tx, rx) = tokio::sync::mpsc::channel::<NetworkEventBatch>(1);

        // Send the batch into the channel, then drop the sender to close the stream.
        tx.send(batch)
            .await
            .map_err(|_| anyhow::anyhow!("ReportNetworkEvents: channel send failed"))?;
        drop(tx);

        // Convert the receiver into a stream that tonic can consume.
        let stream = tokio_stream::wrappers::ReceiverStream::new(rx);

        // Build request with API key, tenant, and identity metadata.
        let mut request = tonic::Request::new(stream);

        // Attach API key as gRPC metadata if configured.
        self.attach_api_key_metadata(&mut request);

        // Attach tenant ID as gRPC metadata if configured.
        if let Some(ref tenant_id) = self.tenant_id {
            if let Ok(value) = MetadataValue::try_from(tenant_id.as_str()) {
                request.metadata_mut().insert("x-tenant-id", value);
            }
        }

        // Attach identity metadata (x-twin-id, x-client-id).
        self.attach_identity_metadata(&mut request);

        let response = client
            .report_network_events(request)
            .await
            .map_err(|status| anyhow::anyhow!("ReportNetworkEvents RPC failed: {}", status))?;

        self.metrics.messages_sent.fetch_add(1, Ordering::Relaxed);
        debug!("Network events reported successfully via ReportNetworkEvents");

        Ok(response.into_inner())
    }

    /// Open a bidirectional streaming RPC for real-time metric exchange.
    ///
    /// Returns a sender for `AgentToCluster` messages and a receiver for
    /// `ClusterToAgent` responses. The caller should spawn a task to drive
    /// each direction.
    ///
    /// Uses a **separate** gRPC channel so the streaming RPC's tower buffer
    /// worker doesn't block unary RPCs (send_batch, heartbeat) on the main client.
    ///
    /// # Stream Protocol
    /// - Agent sends: `MetricBatch`, `NetworkEventBatch`, `HeartbeatRequest`, `ConfigUpdateRequest`
    /// - Cluster sends: `FlowControl`, `ConfigPush`, `AgentCommand`, `HeartbeatResponse`
    pub async fn stream_metrics(
        &self,
    ) -> Result<(mpsc::Sender<AgentToCluster>, tonic::Streaming<ClusterToAgent>)> {
        // Reuse the existing gRPC channel (HTTP/2 multiplexing).
        // Creating a separate connection was timing out in WSL2 environments.
        let channel = {
            let guard = self.channel.lock().await;
            guard.clone().context("Not connected: channel unavailable")?
        };
        let mut stream_client = IngestionServiceClient::new(channel);

        // Create a channel that the caller will use to send AgentToCluster messages.
        let (tx, rx) = mpsc::channel::<AgentToCluster>(256);

        // Convert the tokio mpsc receiver into a stream for tonic.
        let outbound = tokio_stream::wrappers::ReceiverStream::new(rx);

        // Build request with API key, tenant, and identity metadata.
        let mut request = tonic::Request::new(outbound);

        // Attach API key as gRPC metadata if configured.
        self.attach_api_key_metadata(&mut request);

        // Attach tenant ID as gRPC metadata if configured.
        if let Some(ref tenant_id) = self.tenant_id {
            match MetadataValue::try_from(tenant_id.as_str()) {
                Ok(value) => {
                    request.metadata_mut().insert("x-tenant-id", value);
                    debug!(tenant_id = %tenant_id, "Attached x-tenant-id metadata to stream request");
                }
                Err(e) => {
                    warn!(
                        error = %e,
                        "Invalid tenant ID format — skipping x-tenant-id metadata for stream"
                    );
                }
            }
        }

        // Attach identity metadata (x-twin-id, x-client-id).
        self.attach_identity_metadata(&mut request);

        let response = stream_client
            .stream_metrics(request)
            .await
            .map_err(|status| anyhow::anyhow!("StreamMetrics RPC failed: {}", status))?;

        let inbound = response.into_inner();
        info!("Bidirectional metric stream opened (reused channel)");

        Ok((tx, inbound))
    }

    // ── Raw Byte Channel (Legacy) ──────────────────────────────────────

    /// Send raw data through the gRPC stream.
    ///
    /// Queues data for sending via the mpsc channel. The actual gRPC
    /// streaming happens when the connection is established.
    pub async fn send(&self, data: Vec<u8>) -> Result<()> {
        if !self.is_connected().await {
            anyhow::bail!("Not connected to cluster");
        }

        self.tx.send(data).await.map_err(|_| anyhow::anyhow!("Channel closed"))?;
        self.metrics.messages_sent.fetch_add(1, Ordering::Relaxed);
        Ok(())
    }

    /// Get the channel (if connected).
    pub async fn channel(&self) -> Option<Channel> {
        self.channel.lock().await.clone()
    }

    /// Set the connection state (used by reconnection engine).
    pub async fn set_state(&self, state: ConnectionState) {
        *self.state.write().await = state;
    }
}

impl Clone for GrpcClient {
    fn clone(&self) -> Self {
        let (tx, rx) = mpsc::channel(1024);
        Self {
            endpoint: self.endpoint.clone(),
            tls_enabled: self.tls_enabled,
            state: Arc::clone(&self.state),
            channel: Arc::clone(&self.channel),
            client: Arc::clone(&self.client),
            endpoint_config: Arc::clone(&self.endpoint_config),
            running: Arc::clone(&self.running),
            metrics: Arc::clone(&self.metrics),
            tx,
            rx: Arc::new(Mutex::new(rx)),
            api_key: self.api_key.clone(),
            tenant_id: self.tenant_id.clone(),
            tenant_cache: None, // Cache is not cloned — set explicitly on the clone if needed.
            twin_id: Arc::clone(&self.twin_id),
            client_id: Arc::clone(&self.client_id),
            config_path: self.config_path.clone(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Verify the connection state machine transitions correctly
    /// through Disconnected → Connecting → Connected → Disconnected.
    #[tokio::test]
    async fn test_connection_state_transitions() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");

        // Initial state: Disconnected
        assert_eq!(client.state().await, ConnectionState::Disconnected);
        assert!(!client.is_connected().await);

        // Transition to Connecting
        client.set_state(ConnectionState::Connecting).await;
        assert_eq!(client.state().await, ConnectionState::Connecting);
        assert!(!client.is_connected().await);

        // Transition to Connected
        client.set_state(ConnectionState::Connected).await;
        assert_eq!(client.state().await, ConnectionState::Connected);
        assert!(client.is_connected().await);

        // Transition to Reconnecting
        client.set_state(ConnectionState::Reconnecting).await;
        assert_eq!(client.state().await, ConnectionState::Reconnecting);
        assert!(!client.is_connected().await);

        // Back to Disconnected
        client.set_state(ConnectionState::Disconnected).await;
        assert_eq!(client.state().await, ConnectionState::Disconnected);
        assert!(!client.is_connected().await);
    }

    /// Verify the endpoint is stored correctly and returned by the accessor.
    #[test]
    fn test_endpoint_stored() {
        let endpoint = "http://cluster.paryty.local:50051";
        let client = GrpcClient::with_endpoint(endpoint);
        assert_eq!(client.endpoint(), endpoint);
    }

    /// Verify TLS flag defaults to false in with_endpoint constructor.
    #[test]
    fn test_tls_disabled_by_default_in_with_endpoint() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");
        assert!(!client.tls_enabled);
    }

    /// Verify that clone shares the same state and client references.
    #[tokio::test]
    async fn test_clone_shares_state() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");
        client.set_state(ConnectionState::Connected).await;

        let cloned = client.clone();
        assert_eq!(cloned.state().await, ConnectionState::Connected);
        assert_eq!(cloned.endpoint(), client.endpoint());
    }

    /// Verify identity defaults to None.
    #[tokio::test]
    async fn test_identity_defaults_to_none() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");
        assert!(client.twin_id().await.is_none());
        assert!(client.client_id().await.is_none());
    }

    /// Verify update_identity sets and clears identity.
    #[tokio::test]
    async fn test_update_identity() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");

        client.update_identity(Some("twin-001".to_string()), Some("client-001".to_string()));
        assert_eq!(client.twin_id().await.as_deref(), Some("twin-001"));
        assert_eq!(client.client_id().await.as_deref(), Some("client-001"));

        client.update_identity(None, None);
        assert!(client.twin_id().await.is_none());
        assert!(client.client_id().await.is_none());
    }

    /// Verify proto methods return an error when not connected.
    #[tokio::test]
    async fn test_rpc_fails_when_disconnected() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");

        let registration = AgentRegistration {
            agent_id: "test-agent".to_string(),
            hostname: "test-host".to_string(),
            ip_addresses: vec![],
            version: "0.1.0".to_string(),
            capabilities: None,
            labels: None,
            started_at: None,
            twin_id: String::new(),
            client_id: String::new(),
        };

        let result = client.register_agent(registration).await;
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("Not connected"));
    }

    /// Verify report_network_events returns an error when not connected.
    #[tokio::test]
    async fn test_report_network_events_fails_when_disconnected() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");

        let batch = NetworkEventBatch {
            agent_id: "test-agent".to_string(),
            session_id: "test-session".to_string(),
            sequence_number: 1,
            timestamp: None,
            events: vec![],
        };

        let result = client.report_network_events(batch).await;
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("Not connected"));
    }

    /// Verify that api_key is None when constructed via with_endpoint.
    #[tokio::test]
    async fn test_api_key_none_by_default_in_with_endpoint() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");
        assert!(client.api_key.read().await.is_none());
    }

    /// Verify that the client from Config extracts the api_key.
    #[tokio::test]
    async fn test_api_key_extracted_from_config() {
        let config = crate::config::Config {
            agent: crate::config::AgentConfig {
                id: "test-agent".to_string(),
                cluster_endpoint: "http://localhost:50051".to_string(),
                api_key: "secret-api-key".to_string(),
                tenant_id: None,
                twin_id: None,
                client_id: None,
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
                    ring_buffer_size_kb: 256,
                    poll_interval_ms: 100,
                    fallback_to_proc: true,
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
        };

        let client = GrpcClient::new(&config);
        let guard = client.api_key.read().await;
        assert_eq!(guard.as_deref(), Some("secret-api-key"));
    }

    /// Verify that clone does not carry tenant_cache but shares identity.
    #[test]
    fn test_clone_shares_identity_not_cache() {
        let mut client = GrpcClient::with_endpoint("http://localhost:50051");
        let dir = tempfile::tempdir().expect("failed to create temp dir");
        client.set_tenant_cache(TenantCache::new(dir.path()));
        client.update_identity(Some("twin-001".to_string()), Some("client-001".to_string()));

        let cloned = client.clone();
        assert!(cloned.tenant_cache.is_none());
        // Identity IS shared via Arc.
        let rt = tokio::runtime::Runtime::new().unwrap();
        assert_eq!(rt.block_on(cloned.twin_id()).as_deref(), Some("twin-001"));
        assert_eq!(rt.block_on(cloned.client_id()).as_deref(), Some("client-001"));
    }

    /// Verify resolve_identity returns an error when not connected.
    #[tokio::test]
    async fn test_resolve_identity_not_connected() {
        let client = GrpcClient::with_endpoint("http://localhost:50051");
        let result = client.resolve_identity("test-agent").await;
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("not connected"));
    }
}
