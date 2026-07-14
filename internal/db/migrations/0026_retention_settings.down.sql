-- =============================================================================
-- 0026_retention_settings.down.sql
-- =============================================================================

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    BEGIN PERFORM remove_retention_policy('appliance_iface_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_continuous_aggregate_policy('appliance_iface_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_retention_policy('appliance_metric_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_continuous_aggregate_policy('appliance_metric_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_retention_policy('agent_latency_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_continuous_aggregate_policy('agent_latency_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_retention_policy('agent_network_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_continuous_aggregate_policy('agent_network_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_retention_policy('agent_metric_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_continuous_aggregate_policy('agent_metric_samples_hourly', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;

    BEGIN PERFORM remove_compression_policy('flow_summaries', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
    BEGIN PERFORM remove_compression_policy('appliance_vendor_samples', if_exists => TRUE); EXCEPTION WHEN OTHERS THEN NULL; END;
  END IF;
END$$;

DROP MATERIALIZED VIEW IF EXISTS appliance_iface_samples_hourly CASCADE;
DROP MATERIALIZED VIEW IF EXISTS appliance_metric_samples_hourly CASCADE;
DROP MATERIALIZED VIEW IF EXISTS agent_latency_samples_hourly CASCADE;
DROP MATERIALIZED VIEW IF EXISTS agent_network_samples_hourly CASCADE;
DROP MATERIALIZED VIEW IF EXISTS agent_metric_samples_hourly CASCADE;

DROP TABLE IF EXISTS platform_retention_settings;
