-- 0003_telemetry.down.sql — reverse the telemetry migration.
DROP TABLE IF EXISTS agent_metric_samples;

DROP INDEX IF EXISTS agents_pending_reboot_idx;
ALTER TABLE agents
  DROP COLUMN IF EXISTS last_metrics,
  DROP COLUMN IF EXISTS last_metrics_at,
  DROP COLUMN IF EXISTS cpu_pct,
  DROP COLUMN IF EXISTS mem_used_bytes,
  DROP COLUMN IF EXISTS mem_total_bytes,
  DROP COLUMN IF EXISTS root_disk_used_bytes,
  DROP COLUMN IF EXISTS root_disk_total_bytes,
  DROP COLUMN IF EXISTS uptime_seconds,
  DROP COLUMN IF EXISTS pending_reboot,
  DROP COLUMN IF EXISTS primary_ip;
