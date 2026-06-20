import sys

filepath = r'd:\__Projects\Paryty\paryty-v1.0\agent\src\proto\paryty.v1.rs'

with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# 1. Add new enum variants to AgentCommandType
old_enum = '    DeleteBacklog = 7,\n}\nimpl AgentCommandType {'
new_enum = (
    '    DeleteBacklog = 7,\n'
    '    /// Agent should gracefully shut down.\n'
    '    Retire = 8,\n'
    '    /// Agent is blacklisted.\n'
    '    Blacklist = 9,\n'
    '    /// Agent should clear identity and continue as rogue.\n'
    '    Unpair = 10,\n'
    '}\nimpl AgentCommandType {'
)
assert old_enum in content, "Could not find enum block"
content = content.replace(old_enum, new_enum)

# 2. Add new variants to as_str_name
old_as_str = '            AgentCommandType::DeleteBacklog => "AGENT_COMMAND_TYPE_DELETE_BACKLOG",\n        }\n    }\n    /// Creates an enum from field names used in the ProtoBuf definition.\n    pub fn from_str_name'
new_as_str = (
    '            AgentCommandType::DeleteBacklog => "AGENT_COMMAND_TYPE_DELETE_BACKLOG",\n'
    '            AgentCommandType::Retire => "AGENT_COMMAND_TYPE_RETIRE",\n'
    '            AgentCommandType::Blacklist => "AGENT_COMMAND_TYPE_BLACKLIST",\n'
    '            AgentCommandType::Unpair => "AGENT_COMMAND_TYPE_UNPAIR",\n'
    '        }\n    }\n'
    '    /// Creates an enum from field names used in the ProtoBuf definition.\n'
    '    pub fn from_str_name'
)
assert old_as_str in content, "Could not find as_str_name block"
content = content.replace(old_as_str, new_as_str)

# 3. Add new variants to from_str_name
old_from_str = '            "AGENT_COMMAND_TYPE_DELETE_BACKLOG" => Some(Self::DeleteBacklog),\n            _ => None,'
new_from_str = (
    '            "AGENT_COMMAND_TYPE_DELETE_BACKLOG" => Some(Self::DeleteBacklog),\n'
    '            "AGENT_COMMAND_TYPE_RETIRE" => Some(Self::Retire),\n'
    '            "AGENT_COMMAND_TYPE_BLACKLIST" => Some(Self::Blacklist),\n'
    '            "AGENT_COMMAND_TYPE_UNPAIR" => Some(Self::Unpair),\n'
    '            _ => None,'
)
assert old_from_str in content, "Could not find from_str_name block"
content = content.replace(old_from_str, new_from_str)

# 4. Add cluster_agent_id, os, arch to AgentRegistration struct
old_reg = '    pub client_id: ::prost::alloc::string::String,\n}\n/// AgentRegistrationResponse is returned by the cluster after registration.'
new_reg = (
    '    pub client_id: ::prost::alloc::string::String,\n'
    '    /// Dual Reality: cluster agent ID for auto-pairing on registration.\n'
    '    #[prost(string, tag = "10")]\n'
    '    pub cluster_agent_id: ::prost::alloc::string::String,\n'
    '    /// Dual Reality: OS of the machine (e.g., "windows", "linux").\n'
    '    #[prost(string, tag = "11")]\n'
    '    pub os: ::prost::alloc::string::String,\n'
    '    /// Dual Reality: CPU architecture (e.g., "amd64", "arm64").\n'
    '    #[prost(string, tag = "12")]\n'
    '    pub arch: ::prost::alloc::string::String,\n'
    '}\n'
    '/// AgentRegistrationResponse is returned by the cluster after registration.'
)
assert old_reg in content, "Could not find AgentRegistration block"
content = content.replace(old_reg, new_reg)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)

print('Proto .rs file updated successfully')
