-- =============================================================================
-- 0014_api_key_sites.up.sql — scope API keys to specific sites (optional).
-- =============================================================================

CREATE TABLE api_key_sites (
  api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  PRIMARY KEY (api_key_id, site_id)
);
CREATE INDEX api_key_sites_site_idx ON api_key_sites(site_id);
