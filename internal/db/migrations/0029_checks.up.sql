-- 0029_checks: synthetic check catalog (ICMP/TCP/HTTP/DNS/TLS).
-- Additive to SNMP OID packs, Meraki, and agent DEX.

CREATE TABLE IF NOT EXISTS checks (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id              UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name                 TEXT NOT NULL,
  type_id              TEXT NOT NULL,
  params               JSONB NOT NULL DEFAULT '{}'::jsonb,
  interval_seconds     INT NOT NULL DEFAULT 60 CHECK (interval_seconds >= 15),
  enabled              BOOLEAN NOT NULL DEFAULT TRUE,
  preferred_runner     TEXT NOT NULL DEFAULT 'auto'
                         CHECK (preferred_runner IN ('auto','agent','collector','central')),
  assigned_agent_id    UUID REFERENCES agents(id) ON DELETE SET NULL,
  assigned_collector_id UUID REFERENCES collectors(id) ON DELETE SET NULL,
  appliance_id         UUID REFERENCES appliances(id) ON DELETE SET NULL,
  last_run_at          TIMESTAMPTZ,
  last_ok              BOOLEAN,
  last_error           TEXT,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS checks_site_idx ON checks (site_id);
CREATE INDEX IF NOT EXISTS checks_enabled_idx ON checks (enabled) WHERE enabled;
CREATE INDEX IF NOT EXISTS checks_agent_idx ON checks (assigned_agent_id) WHERE assigned_agent_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS check_samples (
  check_id      UUID NOT NULL REFERENCES checks(id) ON DELETE CASCADE,
  time          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  channel_key   TEXT NOT NULL,
  value_double  DOUBLE PRECISION,
  value_text    TEXT,
  runner        TEXT NOT NULL DEFAULT 'central',
  ok            BOOLEAN NOT NULL DEFAULT TRUE,
  PRIMARY KEY (check_id, channel_key, time)
);

SELECT create_hypertable('check_samples', 'time', if_not_exists => TRUE, migrate_data => TRUE);

CREATE INDEX IF NOT EXISTS check_samples_time_idx ON check_samples (time DESC);

-- Allow alarm rules to target synthetic checks.
ALTER TABLE alarm_rules DROP CONSTRAINT IF EXISTS alarm_rules_target_kind_check;
ALTER TABLE alarm_rules
  ADD CONSTRAINT alarm_rules_target_kind_check
  CHECK (target_kind IN ('appliance','agent','check','any'));

INSERT INTO alarm_rules (name, severity, expression, enabled, for_seconds, clear_for_seconds, target_kind)
VALUES
  ('ICMP packet loss',           'warning',  'device.icmp_packet_loss_pct > 0',        TRUE, 60,  300, 'check'),
  ('ICMP latency high',          'warning',  'device.icmp_response_time_ms > 500',     TRUE, 120, 300, 'check'),
  ('TCP port down',              'critical', 'device.tcp_up != 1',                     TRUE, 30,  120, 'check'),
  ('HTTP check failed',          'critical', 'device.http_up != 1',                    TRUE, 60,  180, 'check'),
  ('HTTP latency high',          'warning',  'device.http_response_time_ms > 5000',    TRUE, 120, 300, 'check'),
  ('DNS resolve failed',         'critical', 'device.dns_up != 1',                     TRUE, 60,  180, 'check'),
  ('TLS cert expiring soon',     'warning',  'device.tls_days_to_expiration < 28',     TRUE, 300, 600, 'check'),
  ('TLS handshake failed',       'critical', 'device.tls_up != 1',                     TRUE, 60,  180, 'check')
ON CONFLICT DO NOTHING;
