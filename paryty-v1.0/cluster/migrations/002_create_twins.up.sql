-- 002_create_twins.up.sql
-- Migration: Create the paryty_twins table for digital twin definitions.
-- Includes GIN index on abilities JSONB column and a partial unique index
-- on (tenant_id, name) for non-deleted twins.

CREATE TABLE IF NOT EXISTS paryty_twins (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    twin_config JSONB DEFAULT '{}',
    abilities JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_twins_tenant_name
    ON paryty_twins (tenant_id, name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_twins_abilities ON paryty_twins USING GIN (abilities);
