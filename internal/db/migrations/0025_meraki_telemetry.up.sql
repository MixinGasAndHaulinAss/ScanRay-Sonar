-- Meraki Dashboard live telemetry scheduling (independent of inventory sync).
ALTER TABLE site_discovery_settings
  ADD COLUMN IF NOT EXISTS meraki_last_telemetry_at TIMESTAMPTZ;
