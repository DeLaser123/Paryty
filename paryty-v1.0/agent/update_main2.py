filepath = r'd:\__Projects\Paryty\paryty-v1.0\agent\src\main.rs'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Add CLI override after config loading, before logging
old_config = '''    let config = if let Some(path) = &cli.config_path {
        info!(path = %path, "Loading config from CLI argument");
        config::load_from_path(path)?
    } else {
        config::load()?
    };
    info!(
        agent_id = %config.agent.id,
        endpoint = %config.agent.cluster_endpoint,
        "Configuration loaded successfully"
    );'''

new_config = '''    let mut config = if let Some(path) = &cli.config_path {
        info!(path = %path, "Loading config from CLI argument");
        config::load_from_path(path)?
    } else {
        config::load()?
    };

    // CLI override: --cluster-agent-id takes highest precedence.
    if let Some(ref ca_id) = cli.cluster_agent_id {
        config.agent.cluster_agent_id = Some(ca_id.clone());
    }

    info!(
        agent_id = %config.agent.id,
        endpoint = %config.agent.cluster_endpoint,
        "Configuration loaded successfully"
    );'''

assert old_config in content, "Could not find config loading block"
content = content.replace(old_config, new_config)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('main.rs config override added successfully')
