-- =============================================================================
-- 0022_flows.up.sql — NetFlow / IPFIX flow summaries hypertable.
-- =============================================================================

CREATE TABLE flow_summaries (
  time        TIMESTAMPTZ NOT NULL,
  src_addr    INET NOT NULL,
  dst_addr    INET NOT NULL,
  src_port    INTEGER,
  dst_port    INTEGER,
  proto       SMALLINT NOT NULL DEFAULT 0,
  bytes       BIGINT NOT NULL DEFAULT 0,
  packets     BIGINT NOT NULL DEFAULT 0,
  exporter_ip INET
);

SELECT create_hypertable('flow_summaries', 'time', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS flow_summaries_src_time_idx ON flow_summaries (src_addr, time DESC);
CREATE INDEX IF NOT EXISTS flow_summaries_dst_time_idx ON flow_summaries (dst_addr, time DESC);

SELECT add_retention_policy('flow_summaries', INTERVAL '14 days', if_not_exists => TRUE);
