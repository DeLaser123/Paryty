-- Paryty Database Wipe Script
-- WARNING: This will delete ALL data from the database!
-- Use only in development environments.

-- Disable triggers temporarily for faster truncation
SET session_replication_role = 'replica';

-- Truncate all tables in dependency order
TRUNCATE TABLE 
  agent_commands,
  agent_backlogs,
  agent_assignments,
  agent_registrations,
  paryty_twins,
  api_keys,
  refresh_tokens,
  audit_log,
  tenant_plans,
  users,
  tenants
CASCADE;

-- Re-enable triggers
SET session_replication_role = 'origin';

-- Verify tables are empty
SELECT 'tenants' as table_name, COUNT(*) as row_count FROM tenants
UNION ALL
SELECT 'users', COUNT(*) FROM users
UNION ALL
SELECT 'api_keys', COUNT(*) FROM api_keys
UNION ALL
SELECT 'paryty_twins', COUNT(*) FROM paryty_twins
UNION ALL
SELECT 'agent_registrations', COUNT(*) FROM agent_registrations
UNION ALL
SELECT 'agent_assignments', COUNT(*) FROM agent_assignments;
