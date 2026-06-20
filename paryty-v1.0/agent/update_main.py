filepath = r'd:\__Projects\Paryty\paryty-v1.0\agent\src\main.rs'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# 1. Add cluster_agent_id to CliArgs struct
old_struct = '''struct CliArgs {
    config_path: Option<String>,
    set_key: Option<String>,
}'''

new_struct = '''struct CliArgs {
    config_path: Option<String>,
    set_key: Option<String>,
    cluster_agent_id: Option<String>,
}'''

assert old_struct in content, "Could not find CliArgs struct"
content = content.replace(old_struct, new_struct)

# 2. Add cluster_agent_id initialization in parse_cli
old_init = '''    let mut result = CliArgs { config_path: None, set_key: None };'''

new_init = '''    let mut result = CliArgs { config_path: None, set_key: None, cluster_agent_id: None };'''

assert old_init in content, "Could not find CliArgs initialization"
content = content.replace(old_init, new_init)

# 3. Add --cluster-agent-id parsing (before the --key handler)
old_key_parse = '''            "--key" if i + 1 < args.len() => {'''

new_key_parse = '''            "--cluster-agent-id" if i + 1 < args.len() => {
                result.cluster_agent_id = Some(args[i + 1].clone());
                i += 2;
            }
            "--key" if i + 1 < args.len() => {'''

assert old_key_parse in content, "Could not find --key parse block"
content = content.replace(old_key_parse, new_key_parse)

# 4. Add to help text
old_help = '''                eprintln!("  --key <API_KEY>      Set or update the API key and persist to config file");'''

new_help = '''                eprintln!("  --cluster-agent-id <ID>  Cluster agent ID for auto-pairing");
                eprintln!("  --key <API_KEY>      Set or update the API key and persist to config file");'''

assert old_help in content, "Could not find help text for --key"
content = content.replace(old_help, new_help)

# 5. Also add PARYTY_CLUSTER_AGENT_ID to env vars help
old_env_help = '''                eprintln!("  PARYTY_AGENT_CONFIG       Config file path (default: configs/agent/agent.yaml)");'''

new_env_help = '''                eprintln!("  PARYTY_AGENT_CONFIG       Config file path (default: configs/agent/agent.yaml)");
                eprintln!("  PARYTY_CLUSTER_AGENT_ID   Cluster agent ID for auto-pairing");'''

assert old_env_help in content, "Could not find env var help text"
content = content.replace(old_env_help, new_env_help)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('main.rs updated successfully')
