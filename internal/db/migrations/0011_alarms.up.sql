-- =============================================================================
-- 0011_alarms.up.sql — criticality + alarm rules + notifications.
-- =============================================================================

ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS criticality TEXT NOT NULL DEFAULT 'normal'
    CHECK (criticality IN ('low','normal','high','critical'));

ALTER TABLE appliances
  ADD COLUMN IF NOT EXISTS criticality TEXT NOT NULL DEFAULT 'normal'
    CHECK (criticality IN ('low','normal','high','critical'));

ALTER TABLE discovered_devices
  ADD COLUMN IF NOT EXISTS criticality TEXT NOT NULL DEFAULT 'normal'
    CHECK (criticality IN ('low','normal','high','critical'));

CREATE TABLE notification_channels (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind TEXT NOT NULL CHECK (kind IN ('email','webhook')),
  name TEXT NOT NULL,
  config JSONB NOT NULL DEFAULT '{}'::jsonb,
  enc_secret BYTEA,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE alarm_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  severity TEXT NOT NULL CHECK (severity IN ('emergency','critical','warning','info')),
  expression TEXT NOT NULL,
  channel_ids UUID[] NOT NULL DEFAULT '{}',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX alarm_rules_site_idx ON alarm_rules(site_id);
CREATE TRIGGER trg_alarm_rules_updated_at BEFORE UPDATE ON alarm_rules
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE alarms (
  id BIGSERIAL PRIMARY KEY,
  rule_id UUID REFERENCES alarm_rules(id) ON DELETE SET NULL,
  site_id UUID REFERENCES sites(id) ON DELETE CASCADE,
  target_kind TEXT NOT NULL,
  target_id UUID NOT NULL,
  severity TEXT NOT NULL CHECK (severity IN ('emergency','critical','warning','info')),
  title TEXT NOT NULL,
  body TEXT,
  opened_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  cleared_at TIMESTAMPTZ,
  dedup_key TEXT,
  last_value JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX alarms_open_idx ON alarms(site_id, cleared_at) WHERE cleared_at IS NULL;
CREATE INDEX alarms_opened_idx ON alarms(opened_at DESC);
