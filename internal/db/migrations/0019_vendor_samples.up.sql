-- =============================================================================
-- 0019_vendor_samples.up.sql — generic per-appliance vendor metric hypertable.
-- =============================================================================
--
-- Vendor-specific health (UPS battery %, Synology disk temps, Palo session
-- utilization, Alletra volume capacity, Cisco mem/CPU breakdowns) is keyed
-- into one wide table rather than a table per vendor. Schema sprawl avoided;
-- query paths stay parallel to appliance_metric_samples.
--
-- metric_key examples:
--   ups.battery.charge_pct
--   ups.battery.runtime_min
--   ups.output.load_pct
--   ups.battery.temp_c
--   synology.disk.<n>.temp_c
--   synology.system.temp_c
--   paloalto.session.active
--   paloalto.session.util_pct
--   alletra.volume.<idx>.used_pct
--   alletra.volume.<idx>.used_bytes
--   cisco.cpu.5min_pct

CREATE TABLE appliance_vendor_samples (
  appliance_id  UUID NOT NULL REFERENCES appliances(id) ON DELETE CASCADE,
  time          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  metric_key    TEXT NOT NULL,
  value_double  DOUBLE PRECISION,
  value_text    TEXT,
  PRIMARY KEY (appliance_id, metric_key, time)
);

CREATE INDEX appliance_vendor_samples_time_idx
  ON appliance_vendor_samples (time DESC);

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('appliance_vendor_samples', 'time',
                              chunk_time_interval => INTERVAL '7 days',
                              if_not_exists       => TRUE);
    PERFORM add_retention_policy('appliance_vendor_samples', INTERVAL '180 days',
                                 if_not_exists => TRUE);
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'timescaledb hypertable appliance_vendor_samples skipped: %', SQLERRM;
END$$;
