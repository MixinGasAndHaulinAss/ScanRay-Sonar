-- 0028_oidpack_alarms.down.sql
DELETE FROM alarm_rules WHERE name IN (
  'Printer toner not OK',
  'Printer paper not OK',
  'Printer jam',
  'APC environmental probe alarm',
  'Dell server health not OK',
  'Host load average high',
  'Disk free space low'
);
