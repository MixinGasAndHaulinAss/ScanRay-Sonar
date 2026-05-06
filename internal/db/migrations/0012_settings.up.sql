-- =============================================================================
-- 0012_settings.up.sql — GUI-managed SMTP + webhook endpoints.
-- =============================================================================

CREATE TABLE smtp_settings (
  id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  host TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 587,
  user TEXT NOT NULL DEFAULT '',
  enc_password BYTEA,
  from_addr TEXT NOT NULL DEFAULT '',
  use_tls BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO smtp_settings (id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE webhook_endpoints (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  enc_signing_secret BYTEA,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
