DELETE FROM alarm_rules WHERE name IN (
  'ICMP packet loss',
  'ICMP latency high',
  'TCP port down',
  'HTTP check failed',
  'HTTP latency high',
  'DNS resolve failed',
  'TLS cert expiring soon',
  'TLS handshake failed'
);

ALTER TABLE alarm_rules DROP CONSTRAINT IF EXISTS alarm_rules_target_kind_check;
ALTER TABLE alarm_rules
  ADD CONSTRAINT alarm_rules_target_kind_check
  CHECK (target_kind IN ('appliance','agent','any'));

DROP TABLE IF EXISTS check_samples;
DROP TABLE IF EXISTS checks;
