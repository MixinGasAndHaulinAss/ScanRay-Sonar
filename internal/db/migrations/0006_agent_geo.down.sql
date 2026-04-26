-- 0006_agent_geo.down.sql
DROP INDEX IF EXISTS agents_geo_idx;
ALTER TABLE agents
  DROP COLUMN IF EXISTS public_ip,
  DROP COLUMN IF EXISTS public_ip_seen_at,
  DROP COLUMN IF EXISTS geo_country_iso,
  DROP COLUMN IF EXISTS geo_country_name,
  DROP COLUMN IF EXISTS geo_subdivision,
  DROP COLUMN IF EXISTS geo_city,
  DROP COLUMN IF EXISTS geo_lat,
  DROP COLUMN IF EXISTS geo_lon,
  DROP COLUMN IF EXISTS geo_asn,
  DROP COLUMN IF EXISTS geo_org,
  DROP COLUMN IF EXISTS geo_resolved_at;
