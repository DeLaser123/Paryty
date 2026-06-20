import glob
import os

base = r'd:\__Projects\Paryty\paryty-v1.0\agent\src'

# Pattern to fix: both grpc_client.rs and mod.rs have the same AgentConfig construction
# Missing cluster_agent_id field

files_to_fix = [
    os.path.join(base, 'communication', 'grpc_client.rs'),
    os.path.join(base, 'communication', 'mod.rs'),
]

old_pattern = '''                client_id: None,
                self_metrics: crate::config::SelfMetricsConfig { enabled: false, port: 9090 },'''

new_pattern = '''                client_id: None,
                cluster_agent_id: None,
                self_metrics: crate::config::SelfMetricsConfig { enabled: false, port: 9090 },'''

for filepath in files_to_fix:
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()

    count = content.count(old_pattern)
    if count == 0:
        print(f'WARNING: Pattern not found in {filepath}')
        continue

    content = content.replace(old_pattern, new_pattern)
    
    with open(filepath, 'w', encoding='utf-8') as f:
        f.write(content)
    
    print(f'Fixed {count} occurrence(s) in {filepath}')

print('Done')
