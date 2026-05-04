-- =============================================================================
-- 0008_compression.up.sql
-- Phase 5 — turn on TimescaleDB native columnar compression for every
-- time-series hypertable in the database.
--
-- Why this exists
-- ---------------
-- Per-second metric data is the dominant storage cost in Sonar. A 30-day
-- window of `appliance_iface_samples` for a single chassis with ~120 ports
-- polled every 60 s is ~1.4 GB uncompressed; the same data compresses to
-- ~140 MB with no perceptible read-side latency. Native compression is
-- the single highest-leverage knob TimescaleDB offers and is the
-- industry-standard answer for this exact data shape.
--
-- How TimescaleDB compression works
-- ---------------------------------
-- Each chunk that crosses the `compress_after` threshold is rewritten
-- from a row-oriented heap into a columnar layout. Within a chunk, rows
-- are grouped by the `segmentby` columns and ordered by `orderby`; each
-- column then gets a per-codec compressed array (delta-of-delta for
-- timestamps, Gorilla for floats, dictionary for low-cardinality text).
-- Reads are transparent: the planner decompresses on the fly and
-- index-scans the segmentby columns directly without touching unrelated
-- segments.
--
-- segmentby choice (the knob that matters most)
-- ---------------------------------------------
-- Picked from the actual WHERE clauses in handlers_*.go so each query
-- decompresses only the rows it needs:
--
--   agent_metric_samples       segmentby agent_id
--   agent_network_samples      segmentby agent_id
--   agent_latency_samples      segmentby agent_id, target
--   appliance_metric_samples   segmentby appliance_id
--   appliance_iface_samples    segmentby appliance_id, if_index
--
-- compress_after = 1 day
-- ----------------------
-- The UI's hot read window is the last 24 h. Keeping the most recent
-- chunk uncompressed means inserts stay row-store-fast and the most
-- common queries never pay decompression cost. Older chunks compress
-- on a managed background policy.
--
-- One-shot backfill
-- -----------------
-- The compression policy job runs at most once a day, so without an
-- explicit backfill the savings would not appear until tomorrow. The
-- final DO block walks every hypertable's chunks older than 1 day and
-- compresses them inline, with a per-chunk EXCEPTION handler so a
-- single bad chunk cannot abort the migration.
--
-- Vanilla-Postgres safety
-- -----------------------
-- Wrapped in `IF EXISTS (timescaledb)` so CI without the extension still
-- migrates cleanly, matching the idiom in 0003 / 0004 / 0007.
-- =============================================================================

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    RAISE NOTICE 'timescaledb extension not present; skipping compression setup';
    RETURN;
  END IF;

  -- ---------------------------------------------------------------------------
  -- Section 1: declare each hypertable compressible.
  -- ---------------------------------------------------------------------------
  ALTER TABLE agent_metric_samples SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'agent_id',
    timescaledb.compress_orderby   = 'time DESC'
  );

  ALTER TABLE agent_network_samples SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'agent_id',
    timescaledb.compress_orderby   = 'time DESC'
  );

  ALTER TABLE agent_latency_samples SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'agent_id, target',
    timescaledb.compress_orderby   = 'time DESC'
  );

  ALTER TABLE appliance_metric_samples SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'appliance_id',
    timescaledb.compress_orderby   = 'time DESC'
  );

  ALTER TABLE appliance_iface_samples SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'appliance_id, if_index',
    timescaledb.compress_orderby   = 'time DESC'
  );

  -- ---------------------------------------------------------------------------
  -- Section 2: schedule the background policy. 1-day delay matches the
  -- 24 h hot window every read path uses.
  -- ---------------------------------------------------------------------------
  PERFORM add_compression_policy('agent_metric_samples',
                                 INTERVAL '1 day', if_not_exists => TRUE);
  PERFORM add_compression_policy('agent_network_samples',
                                 INTERVAL '1 day', if_not_exists => TRUE);
  PERFORM add_compression_policy('agent_latency_samples',
                                 INTERVAL '1 day', if_not_exists => TRUE);
  PERFORM add_compression_policy('appliance_metric_samples',
                                 INTERVAL '1 day', if_not_exists => TRUE);
  PERFORM add_compression_policy('appliance_iface_samples',
                                 INTERVAL '1 day', if_not_exists => TRUE);

EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'compression policy setup skipped: %', SQLERRM;
END$$;

-- -----------------------------------------------------------------------------
-- Section 3: one-shot backfill. Compress every existing chunk older
-- than 1 day so the savings are visible the moment migrate-up returns.
-- A separate DO block so the per-chunk EXCEPTION handler is scoped
-- locally and a failed chunk does not abort the whole loop.
-- -----------------------------------------------------------------------------
DO $$
DECLARE
  ht regclass;
  ck regclass;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    RETURN;
  END IF;

  FOR ht IN
    SELECT unnest(ARRAY[
      'agent_metric_samples'::regclass,
      'agent_network_samples'::regclass,
      'agent_latency_samples'::regclass,
      'appliance_metric_samples'::regclass,
      'appliance_iface_samples'::regclass
    ])
  LOOP
    FOR ck IN SELECT show_chunks(ht, older_than => INTERVAL '1 day')
    LOOP
      BEGIN
        PERFORM compress_chunk(ck, if_not_compressed => TRUE);
      EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'compress_chunk(%) skipped: %', ck, SQLERRM;
      END;
    END LOOP;
  END LOOP;
END$$;
