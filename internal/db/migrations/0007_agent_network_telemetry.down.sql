-- =============================================================================
-- 0007_agent_network_telemetry.down.sql
-- Drop the network + latency hypertables. Timescale's drop_chunks
-- retention policies disappear with the table; no manual cleanup
-- needed.
-- =============================================================================

DROP TABLE IF EXISTS agent_latency_samples;
DROP TABLE IF EXISTS agent_network_samples;
