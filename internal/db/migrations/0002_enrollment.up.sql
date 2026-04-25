-- =============================================================================
-- 0002_enrollment.up.sql
-- Phase 2: probe enrollment tokens.
--
-- An enrollment token is a single-use (or N-use) bearer credential that the
-- operator pastes into a probe install one-liner. Plaintext is shown to the
-- operator exactly once and only the sha256 hash is persisted, so a database
-- compromise can't be replayed against the enrollment endpoint.
-- =============================================================================

CREATE TABLE agent_enrollment_tokens (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id     UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  label       TEXT NOT NULL,
  token_hash  BYTEA NOT NULL UNIQUE,
  max_uses    INTEGER NOT NULL DEFAULT 1 CHECK (max_uses > 0),
  used_count  INTEGER NOT NULL DEFAULT 0,
  expires_at  TIMESTAMPTZ NOT NULL,
  revoked_at  TIMESTAMPTZ,
  created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX agent_enrollment_tokens_site_idx ON agent_enrollment_tokens(site_id);
CREATE INDEX agent_enrollment_tokens_active_idx
  ON agent_enrollment_tokens(expires_at)
  WHERE revoked_at IS NULL;
