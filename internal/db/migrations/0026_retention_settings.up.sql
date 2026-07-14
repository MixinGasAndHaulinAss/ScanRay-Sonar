-- =============================================================================
-- 0026_retention_settings.up.sql
-- Platform-admin retention settings, compression for vendor/flow tables,
-- and hourly continuous aggregates for year-scale trend charts.
-- =============================================================================

CREATE TABLE platform_retention_settings (
  id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  hot_window_days INTEGER NOT NULL DEFAULT 30
    CHECK (hot_window_days >= 7 AND hot_window_days <= 90),
  compress_after_days INTEGER NOT NULL DEFAULT 1
    CHECK (compress_after_days >= 0 AND compress_after_days <= 7),
  rollup_retention_days INTEGER NOT NULL DEFAULT 365
    CHECK (rollup_retention_days >= 30 AND rollup_retention_days <= 1825),
  flow_hot_window_days INTEGER NOT NULL DEFAULT 14
    CHECK (flow_hot_window_days >= 3 AND flow_hot_window_days <= 90),
  vendor_samples_days INTEGER NOT NULL DEFAULT 180
    CHECK (vendor_samples_days >= 30 AND vendor_samples_days <= 730),
  alarms_cleared_days INTEGER NOT NULL DEFAULT 365
    CHECK (alarms_cleared_days >= 30 AND alarms_cleared_days <= 1825),
  audit_log_days INTEGER NOT NULL DEFAULT 365
    CHECK (audit_log_days >= 30 AND audit_log_days <= 1825),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO platform_retention_settings (id) VALUES (1) ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Compression for vendor samples + flow summaries (previously uncompressed).
-- ---------------------------------------------------------------------------
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    RAISE NOTICE 'timescaledb extension not present; skipping vendor/flow compression';
    RETURN;
  END IF;

  BEGIN
    ALTER TABLE appliance_vendor_samples SET (
      timescaledb.compress,
      timescaledb.compress_segmentby = 'appliance_id, metric_key',
      timescaledb.compress_orderby   = 'time DESC'
    );
    PERFORM add_compression_policy('appliance_vendor_samples',
                                   INTERVAL '1 day', if_not_exists => TRUE);
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'vendor_samples compression skipped: %', SQLERRM;
  END;

  BEGIN
    ALTER TABLE flow_summaries SET (
      timescaledb.compress,
      timescaledb.compress_segmentby = 'proto',
      timescaledb.compress_orderby   = 'time DESC'
    );
    PERFORM add_compression_policy('flow_summaries',
                                   INTERVAL '1 day', if_not_exists => TRUE);
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'flow_summaries compression skipped: %', SQLERRM;
  END;
END$$;

-- ---------------------------------------------------------------------------
-- Hourly continuous aggregates for long-range chart queries.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    RAISE NOTICE 'timescaledb extension not present; skipping continuous aggregates';
    RETURN;
  END IF;

  -- agent metrics
  BEGIN
    EXECUTE $v$
      CREATE MATERIALIZED VIEW IF NOT EXISTS agent_metric_samples_hourly
      WITH (timescaledb.continuous) AS
      SELECT time_bucket('1 hour', time) AS bucket,
             agent_id,
             AVG(cpu_pct)::REAL AS cpu_pct,
             AVG(mem_used_bytes)::BIGINT AS mem_used_bytes,
             AVG(mem_total_bytes)::BIGINT AS mem_total_bytes,
             AVG(root_disk_used_bytes)::BIGINT AS root_disk_used_bytes,
             AVG(root_disk_total_bytes)::BIGINT AS root_disk_total_bytes
        FROM agent_metric_samples
       GROUP BY bucket, agent_id
      WITH NO DATA
    $v$;
    PERFORM add_continuous_aggregate_policy('agent_metric_samples_hourly',
      start_offset => INTERVAL '3 days',
      end_offset   => INTERVAL '1 hour',
      schedule_interval => INTERVAL '1 hour',
      if_not_exists => TRUE);
    PERFORM add_retention_policy('agent_metric_samples_hourly', INTERVAL '365 days',
                                 if_not_exists => TRUE);
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'agent_metric_samples_hourly skipped: %', SQLERRM;
  END;

  BEGIN
    EXECUTE $v$
      CREATE MATERIALIZED VIEW IF NOT EXISTS agent_network_samples_hourly
      WITH (timescaledb.continuous) AS
      SELECT time_bucket('1 hour', time) AS bucket,
             agent_id,
             AVG(in_bps)::BIGINT AS in_bps,
             AVG(out_bps)::BIGINT AS out_bps
        FROM agent_network_samples
       GROUP BY bucket, agent_id
      WITH NO DATA
    $v$;
    PERFORM add_continuous_aggregate_policy('agent_network_samples_hourly',
      start_offset => INTERVAL '3 days',
      end_offset   => INTERVAL '1 hour',
      schedule_interval => INTERVAL '1 hour',
      if_not_exists => TRUE);
    PERFORM add_retention_policy('agent_network_samples_hourly', INTERVAL '365 days',
                                 if_not_exists => TRUE);
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'agent_network_samples_hourly skipped: %', SQLERRM;
  END;

  BEGIN
    EXECUTE $v$
      CREATE MATERIALIZED VIEW IF NOT EXISTS agent_latency_samples_hourly
      WITH (timescaledb.continuous) AS
      SELECT time_bucket('1 hour', time) AS bucket,
             agent_id,
             target,
             AVG(avg_ms)::REAL AS avg_ms,
             AVG(min_ms)::REAL AS min_ms,
             AVG(max_ms)::REAL AS max_ms,
             AVG(loss_pct)::REAL AS loss_pct
        FROM agent_latency_samples
       GROUP BY bucket, agent_id, target
      WITH NO DATA
    $v$;
    PERFORM add_continuous_aggregate_policy('agent_latency_samples_hourly',
      start_offset => INTERVAL '3 days',
      end_offset   => INTERVAL '1 hour',
      schedule_interval => INTERVAL '1 hour',
      if_not_exists => TRUE);
    PERFORM add_retention_policy('agent_latency_samples_hourly', INTERVAL '365 days',
                                 if_not_exists => TRUE);
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'agent_latency_samples_hourly skipped: %', SQLERRM;
  END;

  BEGIN
    EXECUTE $v$
      CREATE MATERIALIZED VIEW IF NOT EXISTS appliance_metric_samples_hourly
      WITH (timescaledb.continuous) AS
      SELECT time_bucket('1 hour', time) AS bucket,
             appliance_id,
             AVG(cpu_pct)::REAL AS cpu_pct,
             AVG(mem_used_bytes)::BIGINT AS mem_used_bytes,
             AVG(mem_total_bytes)::BIGINT AS mem_total_bytes
        FROM appliance_metric_samples
       GROUP BY bucket, appliance_id
      WITH NO DATA
    $v$;
    PERFORM add_continuous_aggregate_policy('appliance_metric_samples_hourly',
      start_offset => INTERVAL '3 days',
      end_offset   => INTERVAL '1 hour',
      schedule_interval => INTERVAL '1 hour',
      if_not_exists => TRUE);
    PERFORM add_retention_policy('appliance_metric_samples_hourly', INTERVAL '365 days',
                                 if_not_exists => TRUE);
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'appliance_metric_samples_hourly skipped: %', SQLERRM;
  END;

  BEGIN
    EXECUTE $v$
      CREATE MATERIALIZED VIEW IF NOT EXISTS appliance_iface_samples_hourly
      WITH (timescaledb.continuous) AS
      SELECT time_bucket('1 hour', time) AS bucket,
             appliance_id,
             if_index,
             AVG(in_bps)::BIGINT AS in_bps,
             AVG(out_bps)::BIGINT AS out_bps,
             AVG(in_errors)::BIGINT AS in_errors,
             AVG(out_errors)::BIGINT AS out_errors,
             AVG(in_discards)::BIGINT AS in_discards,
             AVG(out_discards)::BIGINT AS out_discards
        FROM appliance_iface_samples
       GROUP BY bucket, appliance_id, if_index
      WITH NO DATA
    $v$;
    PERFORM add_continuous_aggregate_policy('appliance_iface_samples_hourly',
      start_offset => INTERVAL '3 days',
      end_offset   => INTERVAL '1 hour',
      schedule_interval => INTERVAL '1 hour',
      if_not_exists => TRUE);
    PERFORM add_retention_policy('appliance_iface_samples_hourly', INTERVAL '365 days',
                                 if_not_exists => TRUE);
  EXCEPTION WHEN OTHERS THEN
    RAISE NOTICE 'appliance_iface_samples_hourly skipped: %', SQLERRM;
  END;
END$$;
