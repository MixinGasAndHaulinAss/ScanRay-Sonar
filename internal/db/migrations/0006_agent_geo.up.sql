-- =============================================================================
-- 0006_agent_geo.up.sql
-- Phase 4 (agent feature pack) — public IP discovery + GeoIP/ASN
-- enrichment so the world map and the per-host network topology can
-- show where each agent lives and which providers it talks to.
--
-- Storage shape:
--   * public_ip / public_ip_seen_at — what the probe last reported
--     for its own NAT'd public IP (via icanhazip.com).
--   * geo_*                          — denormalized lookup result.
--   * geo_resolved_at                — when we last asked the mmdb.
--     A 7-day TTL keeps us from re-resolving every snapshot while
--     still picking up ISP renumbering inside a reasonable window.
-- =============================================================================

ALTER TABLE agents
  ADD COLUMN public_ip          INET,
  ADD COLUMN public_ip_seen_at  TIMESTAMPTZ,
  ADD COLUMN geo_country_iso    CHAR(2),
  ADD COLUMN geo_country_name   TEXT,
  ADD COLUMN geo_subdivision    TEXT,
  ADD COLUMN geo_city           TEXT,
  ADD COLUMN geo_lat            DOUBLE PRECISION,
  ADD COLUMN geo_lon            DOUBLE PRECISION,
  ADD COLUMN geo_asn            INTEGER,
  ADD COLUMN geo_org            TEXT,
  ADD COLUMN geo_resolved_at    TIMESTAMPTZ;

-- Map view filters by site/tag and renders all rows; the lat/lon
-- index supports the spatial-cluster pass on the frontend even
-- though Postgres itself doesn't run the geo math.
CREATE INDEX IF NOT EXISTS agents_geo_idx
  ON agents (geo_lat, geo_lon)
  WHERE geo_lat IS NOT NULL;
