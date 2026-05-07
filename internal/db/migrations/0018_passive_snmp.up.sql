-- =============================================================================
-- 0018_passive_snmp.up.sql — passive SNMP discovery inventory + change feed.
-- =============================================================================
--
-- Passive SNMP discovery learns the set of IPs an upstream monitoring tool
-- (LibreNMS, Observium, a vendor cloud appliance, an MSP collector, etc.)
-- is already polling, then probes each IP once with a public-OID SNMP GET.
-- The inventory is additive and per-site, with a small change feed capped
-- at ~500 events per site.

CREATE TABLE passive_snmp_inventory (
  site_id        UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  ip             INET NOT NULL,
  vendor         TEXT,
  type           TEXT,
  sub_type       TEXT,
  sys_descr      TEXT,
  sys_object_id  TEXT,
  sys_name       TEXT,
  sys_location   TEXT,
  status         TEXT NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active','retired')),
  first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  miss_count     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (site_id, ip)
);
CREATE INDEX passive_snmp_inventory_site_idx
  ON passive_snmp_inventory (site_id, status, last_seen_at DESC);
CREATE INDEX passive_snmp_inventory_vendor_idx
  ON passive_snmp_inventory (site_id, vendor);

CREATE TABLE passive_snmp_changes (
  id         BIGSERIAL PRIMARY KEY,
  time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  site_id    UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  ip         INET NOT NULL,
  kind       TEXT NOT NULL
             CHECK (kind IN ('added','retired','changed','reactivated')),
  old_json   JSONB,
  new_json   JSONB
);
CREATE INDEX passive_snmp_changes_site_time_idx
  ON passive_snmp_changes (site_id, time DESC);

-- Extend site_discovery_settings with passive-capture controls. The
-- collector reads these via the existing site-credentials/jobs poll
-- and uses them to pace its own slow ticker.
ALTER TABLE site_discovery_settings
  ADD COLUMN IF NOT EXISTS passive_snmp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS passive_snmp_interface TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS passive_snmp_capture_seconds INTEGER NOT NULL DEFAULT 60
    CHECK (passive_snmp_capture_seconds BETWEEN 5 AND 600),
  ADD COLUMN IF NOT EXISTS passive_snmp_retire_after INTEGER NOT NULL DEFAULT 3
    CHECK (passive_snmp_retire_after BETWEEN 1 AND 30),
  ADD COLUMN IF NOT EXISTS passive_snmp_run_interval_seconds INTEGER NOT NULL DEFAULT 21600
    CHECK (passive_snmp_run_interval_seconds >= 600);
