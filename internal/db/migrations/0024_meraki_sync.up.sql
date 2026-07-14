-- Meraki Dashboard API sync: site-scoped credential kind + discovery settings.
ALTER TABLE site_credentials DROP CONSTRAINT IF EXISTS site_credentials_kind_check;
ALTER TABLE site_credentials
  ADD CONSTRAINT site_credentials_kind_check
  CHECK (kind IN ('snmp','ssh','telnet','vmware','wmi','cli','generic','winagent','meraki'));

ALTER TABLE site_discovery_settings
  ADD COLUMN IF NOT EXISTS meraki_sync_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS meraki_org_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS meraki_sync_interval_seconds INTEGER NOT NULL DEFAULT 900
    CHECK (meraki_sync_interval_seconds >= 300),
  ADD COLUMN IF NOT EXISTS meraki_last_sync_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS meraki_last_sync_error TEXT;
