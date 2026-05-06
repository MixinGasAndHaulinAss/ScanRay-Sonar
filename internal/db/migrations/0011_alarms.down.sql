DROP TABLE IF EXISTS alarms;
DROP TABLE IF EXISTS alarm_rules;
DROP TABLE IF EXISTS notification_channels;

ALTER TABLE discovered_devices DROP COLUMN IF EXISTS criticality;
ALTER TABLE appliances DROP COLUMN IF EXISTS criticality;
ALTER TABLE agents DROP COLUMN IF EXISTS criticality;
