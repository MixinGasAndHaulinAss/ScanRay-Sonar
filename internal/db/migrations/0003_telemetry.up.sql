-- =============================================================================
-- 0003_telemetry.up.sql
-- Phase 3: per-agent telemetry storage.
--
-- Two storage shapes living side-by-side:
--   1. agents.last_metrics (JSONB) — the most recent full snapshot
--      from each host, mirrored verbatim to drive the detail page.
--      A few hot fields (cpu_pct, mem_used_bytes, mem_total_bytes,
--      uptime_seconds, pending_reboot, last_metrics_at) are also
--      lifted out of the JSON into typed columns so list views can
--      sort/filter without parsing JSON for every row.
--   2. agent_metric_samples — narrow, append-only time-series for
--      the four headline metrics powering 24h sparklines. Promoted
--      to a TimescaleDB hypertable when the extension is available;
--      degrades gracefully to a plain table on vanilla Postgres.
--
-- The full snapshot is intentionally NOT time-series'd — the JSONB
-- payload is large (~10–50 KB) and the UI only ever needs "latest";
-- chunking it into a hypertable would 50× our storage for no read
-- benefit. If we later want full-history snapshots for forensics,
-- they belong in a separate table or object store, not this one.
-- =============================================================================

ALTER TABLE agents
  ADD COLUMN last_metrics       JSONB,
  ADD COLUMN last_metrics_at    TIMESTAMPTZ,
  ADD COLUMN cpu_pct            REAL,
  ADD COLUMN mem_used_bytes     BIGINT,
  ADD COLUMN mem_total_bytes    BIGINT,
  ADD COLUMN root_disk_used_bytes  BIGINT,
  ADD COLUMN root_disk_total_bytes BIGINT,
  ADD COLUMN uptime_seconds     BIGINT,
  ADD COLUMN pending_reboot     BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN primary_ip         INET;

-- A flag of "this agent told us its system is unhappy in some
-- specific, machine-readable way" — currently just pending-reboot,
-- but the column lets list views filter on a single boolean.
CREATE INDEX agents_pending_reboot_idx
  ON agents(pending_reboot)
  WHERE pending_reboot = TRUE;

-- Time-series samples. Composite PK keeps the hypertable happy and
-- means we can upsert without an extra unique index.
CREATE TABLE agent_metric_samples (
  agent_id              UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  time                  TIMESTAMPTZ NOT NULL,
  cpu_pct               REAL,
  mem_used_bytes        BIGINT,
  mem_total_bytes       BIGINT,
  root_disk_used_bytes  BIGINT,
  root_disk_total_bytes BIGINT,
  PRIMARY KEY (agent_id, time)
);
CREATE INDEX agent_metric_samples_time_idx
  ON agent_metric_samples (time DESC);

-- Promote to a hypertable when timescaledb is loaded. The DO block
-- swallows the failure path so the migration still succeeds on plain
-- postgres (CI, local dev without timescale).
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('agent_metric_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists       => TRUE);
    -- Drop chunks older than 30 days. Sparklines only ever look at
    -- 24h; 30d is generous "show me what changed last week" room.
    PERFORM add_retention_policy('agent_metric_samples', INTERVAL '30 days',
                                 if_not_exists => TRUE);
  END IF;
EXCEPTION WHEN OTHERS THEN
  -- Hypertable promotion failure is not fatal — the table still
  -- works as a plain Postgres table, just without the chunk-based
  -- pruning and storage win. Log via NOTICE so it shows up in the
  -- migrate command output.
  RAISE NOTICE 'timescaledb hypertable promotion skipped: %', SQLERRM;
END$$;
