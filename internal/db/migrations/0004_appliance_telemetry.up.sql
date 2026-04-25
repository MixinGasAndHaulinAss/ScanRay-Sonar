-- =============================================================================
-- 0004_appliance_telemetry.up.sql
-- Phase 3a: storage for sonar-poller's SNMP collections.
--
-- Same dual-shape strategy as 0003_telemetry for agents:
--   1. appliances.last_snapshot (JSONB) — verbatim "what did the last
--      poll find" payload, drives the detail page.
--   2. appliance_metric_samples + appliance_iface_samples — narrow
--      time-series tables for chassis CPU/mem and per-port bps,
--      promoted to TimescaleDB hypertables when available.
--
-- Hot fields are also denormalized onto the appliances row so list
-- views can show "5 of 12 ports down" without parsing JSON per row.
-- =============================================================================

ALTER TABLE appliances
  ADD COLUMN last_snapshot     JSONB,
  ADD COLUMN last_snapshot_at  TIMESTAMPTZ,
  ADD COLUMN sys_descr         TEXT,
  ADD COLUMN sys_name          TEXT,
  ADD COLUMN uptime_seconds    BIGINT,
  ADD COLUMN cpu_pct           REAL,
  ADD COLUMN mem_used_bytes    BIGINT,
  ADD COLUMN mem_total_bytes   BIGINT,
  ADD COLUMN if_up_count       INTEGER,
  ADD COLUMN if_total_count    INTEGER;

-- Chassis-level time-series. CPU/mem only — that's all the chassis
-- sparklines need.
CREATE TABLE appliance_metric_samples (
  appliance_id     UUID NOT NULL REFERENCES appliances(id) ON DELETE CASCADE,
  time             TIMESTAMPTZ NOT NULL,
  cpu_pct          REAL,
  mem_used_bytes   BIGINT,
  mem_total_bytes  BIGINT,
  PRIMARY KEY (appliance_id, time)
);
CREATE INDEX appliance_metric_samples_time_idx
  ON appliance_metric_samples (time DESC);

-- Per-port time-series. We compute bps on the poller (delta of HC
-- counters / sample interval) so the database stores ready-to-graph
-- values; raw octet counts would force every UI request to do the
-- subtraction, blowing up read amplification.
CREATE TABLE appliance_iface_samples (
  appliance_id  UUID NOT NULL REFERENCES appliances(id) ON DELETE CASCADE,
  if_index      INTEGER NOT NULL,
  time          TIMESTAMPTZ NOT NULL,
  in_bps        BIGINT,
  out_bps       BIGINT,
  in_errors     BIGINT,   -- delta over the interval, not cumulative
  out_errors    BIGINT,
  in_discards   BIGINT,
  out_discards  BIGINT,
  PRIMARY KEY (appliance_id, if_index, time)
);
CREATE INDEX appliance_iface_samples_lookup_idx
  ON appliance_iface_samples (appliance_id, if_index, time DESC);

-- Promote to hypertables when timescaledb is loaded. Same idempotent
-- pattern as 0003 — DO block makes it safe on vanilla Postgres.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('appliance_metric_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists       => TRUE);
    PERFORM add_retention_policy('appliance_metric_samples', INTERVAL '30 days',
                                  if_not_exists => TRUE);
    PERFORM create_hypertable('appliance_iface_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists       => TRUE);
    PERFORM add_retention_policy('appliance_iface_samples', INTERVAL '30 days',
                                  if_not_exists => TRUE);
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'timescaledb hypertable promotion skipped: %', SQLERRM;
END$$;
