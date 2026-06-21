-- 003_twin_name_unique_index.sql
-- Migration: Add partial unique index on twin name per tenant
-- Addresses VULN-03-003: Application-level COUNT check is not race-safe.
-- The partial unique index guarantees uniqueness at the database level
-- for non-deleted twins.

CREATE UNIQUE INDEX IF NOT EXISTS idx_twins_tenant_name
  ON paryty_twins (tenant_id, name)
  WHERE deleted_at IS NULL;
