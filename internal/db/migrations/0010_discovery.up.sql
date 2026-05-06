-- =============================================================================
-- 0010_discovery.up.sql — site discovery, credentials vault, health samples.
-- =============================================================================

CREATE TABLE site_credentials (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('snmp','ssh','telnet','vmware','wmi','cli','generic')),
  name TEXT NOT NULL,
  enc_secret BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (site_id, kind, name)
);
CREATE INDEX site_credentials_site_idx ON site_credentials(site_id);

CREATE TABLE site_discovery_settings (
  site_id UUID PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  subnets JSONB NOT NULL DEFAULT '[]'::jsonb,
  scan_interval_seconds INTEGER NOT NULL DEFAULT 3600 CHECK (scan_interval_seconds >= 60),
  subnets_per_period INTEGER NOT NULL DEFAULT 4 CHECK (subnets_per_period > 0),
  device_offline_delete_days INTEGER NOT NULL DEFAULT 30 CHECK (device_offline_delete_days > 0),
  unidentified_delete_days INTEGER NOT NULL DEFAULT 7 CHECK (unidentified_delete_days > 0),
  config_backup_interval_seconds INTEGER NOT NULL DEFAULT 86400 CHECK (config_backup_interval_seconds >= 300),
  icmp_timeout_ms INTEGER NOT NULL DEFAULT 2000 CHECK (icmp_timeout_ms > 0),
  cli_features JSONB NOT NULL DEFAULT '{}'::jsonb,
  filter_rules JSONB NOT NULL DEFAULT '{"include":[],"exclude":[]}'::jsonb,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_site_discovery_settings_updated_at BEFORE UPDATE ON site_discovery_settings
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE discovered_devices (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  collector_id UUID REFERENCES collectors(id) ON DELETE SET NULL,
  ip INET NOT NULL,
  mac TEXT,
  hostname TEXT,
  vendor TEXT,
  sys_object_id TEXT,
  identified BOOLEAN NOT NULL DEFAULT FALSE,
  protocols TEXT[] NOT NULL DEFAULT '{}',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at TIMESTAMPTZ,
  UNIQUE (site_id, ip)
);
CREATE INDEX discovered_devices_site_idx ON discovered_devices(site_id);

CREATE TABLE device_config_backups (
  id BIGSERIAL PRIMARY KEY,
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  discovered_device_id UUID REFERENCES discovered_devices(id) ON DELETE CASCADE,
  appliance_id UUID REFERENCES appliances(id) ON DELETE SET NULL,
  captured_at TIMESTAMPTZ NOT NULL,
  config_text TEXT NOT NULL,
  sha256 TEXT NOT NULL
);
CREATE INDEX device_config_backups_site_time_idx ON device_config_backups(site_id, captured_at DESC);

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('device_config_backups', 'captured_at',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists       => TRUE);
    PERFORM add_retention_policy('device_config_backups', INTERVAL '90 days',
                                 if_not_exists => TRUE);
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'timescaledb hypertable device_config_backups skipped: %', SQLERRM;
END$$;

CREATE TABLE health_check_samples (
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  target_kind TEXT NOT NULL,
  target_id UUID NOT NULL,
  time TIMESTAMPTZ NOT NULL,
  reachable BOOLEAN NOT NULL,
  rtt_ms REAL,
  PRIMARY KEY (site_id, target_kind, target_id, time)
);
CREATE INDEX health_check_samples_time_idx ON health_check_samples(time DESC);

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('health_check_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists       => TRUE);
    PERFORM add_retention_policy('health_check_samples', INTERVAL '90 days',
                                 if_not_exists => TRUE);
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'timescaledb hypertable health_check_samples skipped: %', SQLERRM;
END$$;
