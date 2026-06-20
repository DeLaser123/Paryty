filepath = r'd:\__Projects\Paryty\paryty-v1.0\agent\src\config\mod.rs'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# 1. Add cluster_agent_id field to AgentConfig struct (after client_id)
old_field = '''    pub client_id: Option<String>,
    pub self_metrics: SelfMetricsConfig,'''

new_field = '''    pub client_id: Option<String>,
    /// Assigned cluster agent ID for auto-pairing on registration.
    #[serde(default)]
    pub cluster_agent_id: Option<String>,
    pub self_metrics: SelfMetricsConfig,'''

assert old_field in content, "Could not find client_id field in AgentConfig"
content = content.replace(old_field, new_field)

# 2. Add env var override in load_from_str (after PARYTY_CLIENT_ID block)
old_env = '''    // Override client ID from env var (fallback only; gRPC assignment is primary).
    if let Ok(client_id) = std::env::var("PARYTY_CLIENT_ID") {
        config.agent.client_id = Some(client_id);
    }

    // Generate agent ID if set to "auto"'''

new_env = '''    // Override client ID from env var (fallback only; gRPC assignment is primary).
    if let Ok(client_id) = std::env::var("PARYTY_CLIENT_ID") {
        config.agent.client_id = Some(client_id);
    }

    // Override cluster agent ID from env var for auto-pairing.
    if let Ok(id) = std::env::var("PARYTY_CLUSTER_AGENT_ID") {
        config.agent.cluster_agent_id = Some(id);
    }

    // Generate agent ID if set to "auto"'''

assert old_env in content, "Could not find env var override block"
content = content.replace(old_env, new_env)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('config/mod.rs updated successfully')
