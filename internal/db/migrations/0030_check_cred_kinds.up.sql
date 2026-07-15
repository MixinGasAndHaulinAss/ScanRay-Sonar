-- 0030: vault kinds for phase-4 checks + credential_id FK on checks.
-- Secrets stay in site_credentials.enc_secret; checks.params only references credentialId.

ALTER TABLE site_credentials DROP CONSTRAINT IF EXISTS site_credentials_kind_check;
ALTER TABLE site_credentials
  ADD CONSTRAINT site_credentials_kind_check
  CHECK (kind IN (
    'snmp','ssh','telnet','vmware','wmi','cli','generic','winagent','meraki',
    'sql','ldap','smtp','imap'
  ));

ALTER TABLE checks
  ADD COLUMN IF NOT EXISTS credential_id UUID REFERENCES site_credentials(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS checks_credential_idx ON checks (credential_id)
  WHERE credential_id IS NOT NULL;

INSERT INTO alarm_rules (name, severity, expression, enabled, for_seconds, clear_for_seconds, target_kind)
VALUES
  ('SQL check failed',       'critical', 'device.sql_up != 1',                     TRUE, 60,  180, 'check'),
  ('SQL query latency high', 'warning',  'device.sql_response_time_ms > 5000',     TRUE, 120, 300, 'check'),
  ('SMTP check failed',      'critical', 'device.smtp_up != 1',                    TRUE, 60,  180, 'check'),
  ('IMAP check failed',      'critical', 'device.imap_up != 1',                    TRUE, 60,  180, 'check'),
  ('LDAP bind failed',       'critical', 'device.ldap_up != 1',                    TRUE, 60,  180, 'check')
ON CONFLICT DO NOTHING;
