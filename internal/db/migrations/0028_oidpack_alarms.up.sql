-- =============================================================================
-- 0028_oidpack_alarms.up.sql — seed alarm rules for OID pack catalog metrics.
-- =============================================================================

INSERT INTO alarm_rules (name, severity, expression, enabled, for_seconds, clear_for_seconds, target_kind)
VALUES
  ('Printer toner not OK',           'warning',  'device.printer_toner_status > 0', TRUE, 60,  300, 'appliance'),
  ('Printer paper not OK',           'warning',  'device.printer_paper_status > 0', TRUE, 60,  300, 'appliance'),
  ('Printer jam',                    'critical', 'device.printer_jam_status > 0', TRUE, 0,    60,  'appliance'),
  ('APC environmental probe alarm',  'warning',  'device.apc_env_alarm_status != 1', TRUE, 60, 300, 'appliance'),
  ('Dell server health not OK',      'warning',  'device.dell_server_overall_status != 3', TRUE, 120, 600, 'appliance'),
  ('Host load average high',         'warning',  'device.linux_load_1min > 5', TRUE, 300, 600, 'appliance'),
  ('Disk free space low',            'warning',  'device.disk_free_pct < 10', TRUE, 300, 600, 'appliance')
ON CONFLICT DO NOTHING;
