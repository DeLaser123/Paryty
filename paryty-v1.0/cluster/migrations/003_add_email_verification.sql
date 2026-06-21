-- 003_add_email_verification.sql
-- Migration: Add email verification support to the users table and
-- create the email_verifications table for token-based email
-- confirmation. When a user registers, an unexpired verification token
-- is created. The user must present the token to verify their email.
-- Tokens expire after 24 hours.
--
-- All statements use IF NOT EXISTS for idempotency — safe to run on
-- databases where EnsurePhase8Tables already created these objects.

ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS email_verifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_verifications_hash ON email_verifications(token_hash) WHERE expires_at > NOW();
