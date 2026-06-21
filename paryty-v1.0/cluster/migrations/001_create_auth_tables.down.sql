-- 001_create_auth_tables.down.sql
-- Rollback: Drop authentication and authorization tables in reverse
-- dependency order.

DROP TABLE IF EXISTS password_reset_tokens;
DROP TABLE IF EXISTS email_verifications;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS paryty_users;
DROP TABLE IF EXISTS tenants;
