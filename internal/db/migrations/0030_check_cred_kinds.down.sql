ALTER TABLE checks DROP COLUMN IF EXISTS credential_id;

ALTER TABLE site_credentials DROP CONSTRAINT IF EXISTS site_credentials_kind_check;
ALTER TABLE site_credentials
  ADD CONSTRAINT site_credentials_kind_check
  CHECK (kind IN ('snmp','ssh','telnet','vmware','wmi','cli','generic','winagent','meraki'));

DELETE FROM alarm_rules WHERE name IN (
  'SQL check failed',
  'SQL query latency high',
  'SMTP check failed',
  'IMAP check failed',
  'LDAP bind failed'
);
