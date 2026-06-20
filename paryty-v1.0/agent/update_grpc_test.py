filepath = r'd:\__Projects\Paryty\paryty-v1.0\agent\src\communication\grpc_client.rs'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

old_reg = '''        let registration = AgentRegistration {
            agent_id: "test-agent".to_string(),
            hostname: "test-host".to_string(),
            ip_addresses: vec![],
            version: "0.1.0".to_string(),
            capabilities: None,
            labels: None,
            started_at: None,
            twin_id: String::new(),
            client_id: String::new(),
        };'''

new_reg = '''        let registration = AgentRegistration {
            agent_id: "test-agent".to_string(),
            hostname: "test-host".to_string(),
            ip_addresses: vec![],
            version: "0.1.0".to_string(),
            capabilities: None,
            labels: None,
            started_at: None,
            twin_id: String::new(),
            client_id: String::new(),
            cluster_agent_id: String::new(),
            os: std::env::consts::OS.to_string(),
            arch: std::env::consts::ARCH.to_string(),
        };'''

assert old_reg in content, "Could not find test AgentRegistration construction"
content = content.replace(old_reg, new_reg)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('grpc_client.rs test updated successfully')
