DROP TABLE IF EXISTS appliance_iface_samples;
DROP TABLE IF EXISTS appliance_metric_samples;

ALTER TABLE appliances
  DROP COLUMN IF EXISTS if_total_count,
  DROP COLUMN IF EXISTS if_up_count,
  DROP COLUMN IF EXISTS mem_total_bytes,
  DROP COLUMN IF EXISTS mem_used_bytes,
  DROP COLUMN IF EXISTS cpu_pct,
  DROP COLUMN IF EXISTS uptime_seconds,
  DROP COLUMN IF EXISTS sys_name,
  DROP COLUMN IF EXISTS sys_descr,
  DROP COLUMN IF EXISTS last_snapshot_at,
  DROP COLUMN IF EXISTS last_snapshot;
