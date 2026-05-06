ALTER TABLE site_credentials DROP CONSTRAINT IF EXISTS site_credentials_kind_check;
ALTER TABLE site_credentials
  ADD CONSTRAINT site_credentials_kind_check
  CHECK (kind IN ('snmp','ssh','telnet','vmware','wmi','cli','generic'));
