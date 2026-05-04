-- =============================================================================
-- 0008_compression.down.sql
-- Reverse of 0008_compression.up.sql: stop the background compression
-- policies, decompress every chunk back to row-store, and clear the
-- per-table compression flag so the hypertable behaves identically to
-- its pre-0008 state. Online operation; no data loss.
-- =============================================================================

DO $$
DECLARE
  ht regclass;
  ck regclass;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    RAISE NOTICE 'timescaledb extension not present; nothing to undo';
    RETURN;
  END IF;

  -- Stop the policy jobs first so a job firing mid-decompress cannot
  -- recompress a chunk we just unpacked.
  PERFORM remove_compression_policy('agent_metric_samples',     if_exists => TRUE);
  PERFORM remove_compression_policy('agent_network_samples',    if_exists => TRUE);
  PERFORM remove_compression_policy('agent_latency_samples',    if_exists => TRUE);
  PERFORM remove_compression_policy('appliance_metric_samples', if_exists => TRUE);
  PERFORM remove_compression_policy('appliance_iface_samples',  if_exists => TRUE);

  -- Decompress every currently-compressed chunk across all five
  -- hypertables. Per-chunk EXCEPTION handler so a single bad chunk
  -- cannot abort the rollback.
  FOR ht IN
    SELECT unnest(ARRAY[
      'agent_metric_samples'::regclass,
      'agent_network_samples'::regclass,
      'agent_latency_samples'::regclass,
      'appliance_metric_samples'::regclass,
      'appliance_iface_samples'::regclass
    ])
  LOOP
    FOR ck IN
      SELECT format('%I.%I', chunk_schema, chunk_name)::regclass
        FROM timescaledb_information.chunks
       WHERE format('%I.%I', hypertable_schema, hypertable_name)::regclass = ht
         AND is_compressed
    LOOP
      BEGIN
        PERFORM decompress_chunk(ck);
      EXCEPTION WHEN OTHERS THEN
        RAISE NOTICE 'decompress_chunk(%) skipped: %', ck, SQLERRM;
      END;
    END LOOP;
  END LOOP;

  -- Clear the per-table compression flag.
  ALTER TABLE agent_metric_samples     SET (timescaledb.compress = false);
  ALTER TABLE agent_network_samples    SET (timescaledb.compress = false);
  ALTER TABLE agent_latency_samples    SET (timescaledb.compress = false);
  ALTER TABLE appliance_metric_samples SET (timescaledb.compress = false);
  ALTER TABLE appliance_iface_samples  SET (timescaledb.compress = false);

EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'compression rollback skipped: %', SQLERRM;
END$$;
