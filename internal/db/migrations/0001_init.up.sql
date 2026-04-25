-- =============================================================================
-- 0001_init.up.sql
-- ScanRay Sonar — Phase 1 baseline schema.
-- Conventions:
--   * UUID primary keys (gen_random_uuid via pgcrypto)
--   * TIMESTAMPTZ everywhere; UTC enforced at the app layer
--   * Encrypted columns are bytea, prefixed `enc_`, sealed by internal/crypto
--   * created_at / updated_at maintained by `set_updated_at` trigger
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- timescaledb is loaded via shared_preload_libraries in the timescale image;
-- the CREATE EXTENSION is a no-op there but harmless on plain postgres.
CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

-- ---------------------------------------------------------------------------
-- Common updated_at trigger
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- sites — multi-tenant boundary. Every operational entity belongs to a site.
-- ---------------------------------------------------------------------------
CREATE TABLE sites (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug        TEXT NOT NULL UNIQUE,        -- url-safe short id, e.g. "hq"
  name        TEXT NOT NULL,
  timezone    TEXT NOT NULL DEFAULT 'UTC',
  description TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_sites_updated_at BEFORE UPDATE ON sites
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- users — local accounts. password_hash is an argon2id PHC string.
-- enc_totp_secret is sealed by internal/crypto; NULL until MFA is enrolled.
-- ---------------------------------------------------------------------------
CREATE TABLE users (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email           TEXT NOT NULL,
  display_name    TEXT NOT NULL,
  password_hash   TEXT NOT NULL,
  role            TEXT NOT NULL CHECK (role IN ('superadmin','siteadmin','tech','readonly')),
  enc_totp_secret BYTEA,
  totp_enrolled   BOOLEAN NOT NULL DEFAULT FALSE,
  is_active       BOOLEAN NOT NULL DEFAULT TRUE,
  last_login_at   TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Functional unique index gives us case-insensitive email lookup
-- without depending on the citext extension.
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));
CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- user_sites — explicit site membership for non-superadmins. Superadmins
-- bypass this table at the app layer.
-- ---------------------------------------------------------------------------
CREATE TABLE user_sites (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  site_id    UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, site_id)
);

-- ---------------------------------------------------------------------------
-- api_keys — long-lived credentials for OpenAPI consumers / scripts.
-- token_hash is sha256(key) so the plaintext key is shown exactly once.
-- ---------------------------------------------------------------------------
CREATE TABLE api_keys (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  token_hash  BYTEA NOT NULL UNIQUE,           -- sha256 of the issued token
  scopes      TEXT[] NOT NULL DEFAULT '{}',
  expires_at  TIMESTAMPTZ,
  last_used_at TIMESTAMPTZ,
  revoked_at  TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------------------
-- agents — endpoint hosts running the Sonar Probe.
-- enc_enroll_secret holds the sealed shared secret used during initial
-- enrollment; cleared once the agent is fully provisioned.
-- ---------------------------------------------------------------------------
CREATE TABLE agents (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id           UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  hostname          TEXT NOT NULL,
  fingerprint       TEXT,                       -- TPM/UUID/serial-derived
  os                TEXT NOT NULL DEFAULT '',   -- "windows" | "linux" | "darwin"
  os_version        TEXT NOT NULL DEFAULT '',
  agent_version     TEXT NOT NULL DEFAULT '',
  enc_enroll_secret BYTEA,
  enrolled_at       TIMESTAMPTZ,
  last_seen_at      TIMESTAMPTZ,
  is_active         BOOLEAN NOT NULL DEFAULT TRUE,
  tags              TEXT[] NOT NULL DEFAULT '{}',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (site_id, hostname)
);
CREATE INDEX agents_site_idx ON agents(site_id);
CREATE INDEX agents_last_seen_idx ON agents(last_seen_at DESC);
CREATE TRIGGER trg_agents_updated_at BEFORE UPDATE ON agents
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- appliances — switches/routers/firewalls/APs polled by sonar-poller.
-- All credential fields are sealed by internal/crypto.
-- ---------------------------------------------------------------------------
CREATE TABLE appliances (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id         UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name            TEXT NOT NULL,
  vendor          TEXT NOT NULL DEFAULT 'generic',  -- meraki | cisco | aruba | ubiquiti | mikrotik | generic
  model           TEXT,
  serial          TEXT,
  mgmt_ip         INET NOT NULL,
  snmp_version    TEXT NOT NULL DEFAULT 'v2c' CHECK (snmp_version IN ('v1','v2c','v3')),
  enc_snmp_creds  BYTEA,                            -- JSON-encoded creds, sealed
  poll_interval_s INTEGER NOT NULL DEFAULT 60,
  is_active       BOOLEAN NOT NULL DEFAULT TRUE,
  tags            TEXT[] NOT NULL DEFAULT '{}',
  last_polled_at  TIMESTAMPTZ,
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (site_id, name)
);
CREATE INDEX appliances_site_idx ON appliances(site_id);
CREATE TRIGGER trg_appliances_updated_at BEFORE UPDATE ON appliances
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- audit_log — append-only record of security-relevant events. Never updated.
-- ---------------------------------------------------------------------------
CREATE TABLE audit_log (
  id          BIGSERIAL PRIMARY KEY,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  actor_id    UUID,                  -- NULL for system actions
  actor_kind  TEXT NOT NULL,         -- "user" | "agent" | "api_key" | "system"
  action      TEXT NOT NULL,         -- "user.login.ok", "agent.enroll", ...
  target_kind TEXT,
  target_id   TEXT,
  ip          INET,
  user_agent  TEXT,
  metadata    JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_log_occurred_idx ON audit_log(occurred_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log(actor_id);
CREATE INDEX audit_log_action_idx ON audit_log(action);

-- ---------------------------------------------------------------------------
-- schema_metadata — single-row table for runtime-visible bookkeeping.
-- ---------------------------------------------------------------------------
CREATE TABLE schema_metadata (
  id            INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  initialized_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO schema_metadata (id) VALUES (1) ON CONFLICT DO NOTHING;
