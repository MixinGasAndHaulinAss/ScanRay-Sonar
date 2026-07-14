-- =============================================================================
-- 0027_agent_dex_platform.down.sql
-- =============================================================================

DELETE FROM report_templates
 WHERE slug IN ('agent-fleet-summary', 'agent-compliance', 'agent-patches');

DELETE FROM alarm_rules WHERE name IN (
  'Agent high CPU',
  'Agent high memory',
  'Agent UX score low',
  'Agent missing patches',
  'Agent BSOD in last 24h',
  'Agent pending reboot'
);

ALTER TABLE alarm_rules DROP COLUMN IF EXISTS target_kind;

DROP TABLE IF EXISTS agent_vulnerabilities;
DROP TABLE IF EXISTS agent_compliance_issues;
DROP TABLE IF EXISTS agent_system_events;
DROP TABLE IF EXISTS agent_health_samples;
DROP TABLE IF EXISTS agent_patch_samples;
DROP TABLE IF EXISTS agent_app_inventory_daily;
DROP TABLE IF EXISTS agent_process_samples;
DROP TABLE IF EXISTS agent_score_samples;

ALTER TABLE agents
  DROP COLUMN IF EXISTS gpu_name,
  DROP COLUMN IF EXISTS boot_duration_ms,
  DROP COLUMN IF EXISTS battery_wear_pct,
  DROP COLUMN IF EXISTS last_compliance_at,
  DROP COLUMN IF EXISTS compliance_issues_count,
  DROP COLUMN IF EXISTS compliance_severity,
  DROP COLUMN IF EXISTS compliance_score,
  DROP COLUMN IF EXISTS group_id;

DROP TABLE IF EXISTS device_groups;
