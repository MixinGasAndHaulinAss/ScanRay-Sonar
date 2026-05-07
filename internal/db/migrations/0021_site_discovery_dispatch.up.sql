-- Track when central last handed the discovery_scan job to a collector
-- for each site. The handler at /api/v1/collectors/me/jobs only returns
-- the job once scan_interval_seconds has elapsed since this timestamp,
-- so an over-eager collector tick (now 60s) cannot exceed the operator-
-- configured site interval.
ALTER TABLE site_discovery_settings
    ADD COLUMN IF NOT EXISTS last_discovery_dispatched_at timestamp with time zone;
