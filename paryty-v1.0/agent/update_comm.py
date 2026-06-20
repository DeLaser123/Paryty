filepath = r'd:\__Projects\Paryty\paryty-v1.0\agent\src\communication\mod.rs'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# ============================================================
# 1. Add cluster_agent_id field to Client struct
# ============================================================
old_identity = '''    /// Whether identity has been fully assigned and validated.
    identity_valid: Arc<AtomicBool>,

    // ── Backlog Tracking ───────────────────────────────────────────────'''

new_identity = '''    /// Whether identity has been fully assigned and validated.
    identity_valid: Arc<AtomicBool>,
    /// Cluster agent ID for auto-pairing (from config/env/CLI).
    cluster_agent_id: Option<String>,

    // ── Backlog Tracking ───────────────────────────────────────────────'''

assert old_identity in content, "Could not find identity_valid field in Client struct"
content = content.replace(old_identity, new_identity)

# ============================================================
# 2. Initialize cluster_agent_id in Client::new()
# ============================================================
old_init = '''            identity_valid: Arc::new(AtomicBool::new(identity_assigned)),
            backlog_bytes: Arc::new(AtomicU64::new(0)),'''

new_init = '''            identity_valid: Arc::new(AtomicBool::new(identity_assigned)),
            cluster_agent_id: config.agent.cluster_agent_id.clone(),
            backlog_bytes: Arc::new(AtomicU64::new(0)),'''

assert old_init in content, "Could not find identity_valid initialization"
content = content.replace(old_init, new_init)

# ============================================================
# 3. Update register_on_connect to include cluster_agent_id, os, arch
# ============================================================
old_registration = '''        let registration = AgentRegistration {
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
            twin_id: String::new(),
            client_id: String::new(),
        };'''

new_registration = '''        let registration = AgentRegistration {
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
            twin_id: String::new(),
            client_id: String::new(),
            cluster_agent_id: self.cluster_agent_id.clone().unwrap_or_default(),
            os: std::env::consts::OS.to_string(),
            arch: std::env::consts::ARCH.to_string(),
        };'''

assert old_registration in content, "Could not find AgentRegistration construction"
content = content.replace(old_registration, new_registration)

# ============================================================
# 4. Add clear_identity() method after apply_identity()
# ============================================================
old_after_apply = '''    /// Check whether identity has been fully assigned.
    pub fn is_identity_assigned(&self) -> bool {'''

new_after_apply = '''    /// Clear all identity state, reverting the agent to an unpaired/rogue state.
    ///
    /// Sets `twin_id`, `client_id`, `topic_prefix` to empty and
    /// `identity_valid` to false. Also clears the gRPC client identity
    /// metadata so subsequent RPCs do not carry stale identity.
    pub async fn clear_identity(&self) {
        {
            let mut tid = self.twin_id.write().await;
            *tid = None;
        }
        {
            let mut cid = self.client_id.write().await;
            *cid = None;
        }
        {
            let mut tp = self.topic_prefix.write().await;
            *tp = None;
        }
        self.identity_valid.store(false, Ordering::Release);
        self.grpc.update_identity(None, None);
        info!("Identity cleared — agent is now unpaired (rogue)");
    }

    /// Check whether identity has been fully assigned.
    pub fn is_identity_assigned(&self) -> bool {'''

assert old_after_apply in content, "Could not find is_identity_assigned method"
content = content.replace(old_after_apply, new_after_apply)

# ============================================================
# 5. Add command handlers for Retire, Blacklist, Unpair
# ============================================================
old_command = '''            Ok(AgentCommandType::DeleteBacklog) => {
                info!("DeleteBacklog command received");
                self.buffer.delete_all_backlog();
                info!("Backlog deleted permanently");
            }
            Ok(AgentCommandType::Unspecified) | Err(_) => {'''

new_command = '''            Ok(AgentCommandType::DeleteBacklog) => {
                info!("DeleteBacklog command received");
                self.buffer.delete_all_backlog();
                info!("Backlog deleted permanently");
            }
            Ok(AgentCommandType::Retire) => {
                warn!("Received RETIRE command from cluster — shutting down gracefully");
                self.cancel_token.cancel();
            }
            Ok(AgentCommandType::Blacklist) => {
                error!("Received BLACKLIST command from cluster — agent is blacklisted");
                if let Err(e) = std::fs::write(".paryty-blacklisted", "true") {
                    warn!(error = %e, "Failed to persist blacklist state to disk");
                }
                self.cancel_token.cancel();
            }
            Ok(AgentCommandType::Unpair) => {
                warn!("Received UNPAIR command from cluster — clearing identity");
                self.clear_identity().await;
                info!("Agent unpaired — continuing as rogue agent");
            }
            Ok(AgentCommandType::Unspecified) | Err(_) => {'''

assert old_command in content, "Could not find DeleteBacklog handler"
content = content.replace(old_command, new_command)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('communication/mod.rs updated successfully')
