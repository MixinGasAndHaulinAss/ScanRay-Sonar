-- =============================================================================
-- 0007_agent_network_telemetry.up.sql
-- Phase 4 — agent network usage + ICMP latency time-series.
--
-- Adds two TimescaleDB hypertables that mirror the dual-storage idiom
-- of 0003 (agent_metric_samples) and 0004 (appliance_*_samples):
--
--   * agent_network_samples — per-agent aggregate in_bps / out_bps
--     summed across physical NICs. Drives the "Network usage" chart
--     on AgentDetail and the WiFi-vs-Wired traffic breakdown on the
--     Network-Performance overview.
--
--   * agent_latency_samples — one row per (agent, target) per
--     snapshot. target='8.8.8.8' (the configured WAN target) and
--     target='gateway' (the host's default-route gateway, discovered
--     by the probe per OS) are the two we collect today; the column
--     is open-ended so a future operator-defined target list can
--     coexist without a schema change.
--
-- Storage policy follows 0003/0004:
--   * Promote to TimescaleDB hypertables when the extension is
--     loaded, with chunk_time_interval = 1 day so daily ranges read
--     a single chunk.
--   * 30-day retention policy. Sparklines and the "today" overview
--     pages only ever look at the last 24 hours; 30 days is "show me
--     last week" headroom.
--   * Wrapped in a DO block that NOTICEs and continues on plain
--     Postgres so the migration succeeds in CI without the
--     extension installed.
-- =============================================================================

CREATE TABLE agent_network_samples (
  agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  time     TIMESTAMPTZ NOT NULL,
  in_bps   BIGINT,
  out_bps  BIGINT,
  PRIMARY KEY (agent_id, time)
);
CREATE INDEX agent_network_samples_time_idx
  ON agent_network_samples (time DESC);

CREATE TABLE agent_latency_samples (
  agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  time     TIMESTAMPTZ NOT NULL,
  target   TEXT NOT NULL,        -- "8.8.8.8" | "gateway" | future operator-defined
  address  INET,
  avg_ms   REAL,
  min_ms   REAL,
  max_ms   REAL,
  loss_pct REAL,
  PRIMARY KEY (agent_id, time, target)
);
CREATE INDEX agent_latency_samples_time_idx
  ON agent_latency_samples (time DESC);
-- Per-(agent,target) lookup is the most common read shape — the
-- chart fetcher always pulls one target at a time.
CREATE INDEX agent_latency_samples_target_idx
  ON agent_latency_samples (agent_id, target, time DESC);

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('agent_network_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists       => TRUE);
    PERFORM add_retention_policy('agent_network_samples', INTERVAL '30 days',
                                  if_not_exists => TRUE);

    PERFORM create_hypertable('agent_latency_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists       => TRUE);
    PERFORM add_retention_policy('agent_latency_samples', INTERVAL '30 days',
                                  if_not_exists => TRUE);
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'timescaledb hypertable promotion skipped: %', SQLERRM;
END$$;
