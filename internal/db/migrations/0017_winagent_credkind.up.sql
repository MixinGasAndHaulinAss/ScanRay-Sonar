-- Add 'winagent' as an allowed kind for site_credentials so collectors
-- can store the shared bearer token used to talk to a sonar-collector-
-- winagent on a Windows endpoint.
ALTER TABLE site_credentials DROP CONSTRAINT IF EXISTS site_credentials_kind_check;
ALTER TABLE site_credentials
  ADD CONSTRAINT site_credentials_kind_check
  CHECK (kind IN ('snmp','ssh','telnet','vmware','wmi','cli','generic','winagent'));
