ALTER TABLE site_discovery_settings
  DROP COLUMN IF EXISTS passive_snmp_run_interval_seconds,
  DROP COLUMN IF EXISTS passive_snmp_retire_after,
  DROP COLUMN IF EXISTS passive_snmp_capture_seconds,
  DROP COLUMN IF EXISTS passive_snmp_interface,
  DROP COLUMN IF EXISTS passive_snmp_enabled;

DROP TABLE IF EXISTS passive_snmp_changes;
DROP TABLE IF EXISTS passive_snmp_inventory;
