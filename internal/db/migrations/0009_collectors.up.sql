-- =============================================================================
-- 0009_collectors.up.sql
-- Remote site collectors: enrollment + WS ingest to central Sonar.
-- =============================================================================

CREATE TABLE collector_enrollment_tokens (
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
CREATE INDEX collector_enrollment_tokens_site_idx ON collector_enrollment_tokens(site_id);

CREATE TABLE collectors (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id           UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name              TEXT NOT NULL,
  hostname          TEXT NOT NULL DEFAULT '',
  fingerprint       TEXT,
  collector_version TEXT NOT NULL DEFAULT '',
  last_seen_at      TIMESTAMPTZ,
  is_active         BOOLEAN NOT NULL DEFAULT TRUE,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (site_id, name)
);
CREATE INDEX collectors_site_idx ON collectors(site_id);
CREATE TRIGGER trg_collectors_updated_at BEFORE UPDATE ON collectors
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE appliances
  ADD COLUMN collector_id UUID REFERENCES collectors(id) ON DELETE SET NULL;
CREATE INDEX appliances_collector_idx ON appliances(collector_id)
  WHERE collector_id IS NOT NULL;

ALTER TABLE agents
  ADD COLUMN collector_id UUID REFERENCES collectors(id) ON DELETE SET NULL;
CREATE INDEX agents_collector_idx ON agents(collector_id)
  WHERE collector_id IS NOT NULL;
