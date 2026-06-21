-- 002_add_abilities.sql
-- Migration: Add the abilities JSONB column to the paryty_twins table
-- with a GIN index for efficient querying. Abilities represent what a
-- digital twin is capable of (e.g., metric collection, log aggregation,
-- alert evaluation). Frontend dashboards filter and display twins by
-- their declared abilities.
--
-- Uses ADD COLUMN IF NOT EXISTS for idempotency — safe to run on
-- databases where EnsurePhase8Tables already added this column.

ALTER TABLE paryty_twins ADD COLUMN IF NOT EXISTS abilities JSONB DEFAULT '[]';

CREATE INDEX IF NOT EXISTS idx_twins_abilities ON paryty_twins USING GIN (abilities);
