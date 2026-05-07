DROP TABLE IF EXISTS reports;
DROP TRIGGER IF EXISTS trg_report_templates_updated_at ON report_templates;
DROP TABLE IF EXISTS report_templates;

DELETE FROM alarm_rules WHERE name IN (
  'UPS battery low (charge)',
  'UPS battery runtime low',
  'UPS overload',
  'UPS battery overheating',
  'UPS battery replacement needed',
  'UPS on battery (output not normal)',
  'Synology RAID degraded',
  'Synology disk failed',
  'Synology disk overheating',
  'Synology system fault',
  'Synology power fault',
  'Palo Alto session table near full',
  'Palo Alto session table critical',
  'Alletra volume nearly full',
  'Alletra volume critically full',
  'Cisco CPU 5-min sustained'
);
