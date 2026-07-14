ALTER TABLE site_discovery_settings
  DROP COLUMN IF EXISTS meraki_sync_enabled,
  DROP COLUMN IF EXISTS meraki_org_ids,
  DROP COLUMN IF EXISTS meraki_sync_interval_seconds,
  DROP COLUMN IF EXISTS meraki_last_sync_at,
  DROP COLUMN IF EXISTS meraki_last_sync_error;

ALTER TABLE site_credentials DROP CONSTRAINT IF EXISTS site_credentials_kind_check;
ALTER TABLE site_credentials
  ADD CONSTRAINT site_credentials_kind_check
  CHECK (kind IN ('snmp','ssh','telnet','vmware','wmi','cli','generic','winagent'));
