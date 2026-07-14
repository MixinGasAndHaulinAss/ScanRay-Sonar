-- =============================================================================
-- 0027_agent_dex_platform.up.sql
-- Agent DEX platform: device groups, historical DEX indices, system events,
-- compliance surface, agent alarm seeds, agent report templates.
-- =============================================================================

-- ---------- Device groups (1:1 membership via agents.group_id) --------------

CREATE TABLE device_groups (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id     UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX device_groups_site_name_uidx
  ON device_groups (site_id, lower(name));
CREATE INDEX device_groups_site_idx ON device_groups(site_id);
CREATE TRIGGER trg_device_groups_updated_at BEFORE UPDATE ON device_groups
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES device_groups(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS agents_group_id_idx ON agents(group_id)
  WHERE group_id IS NOT NULL;

-- ---------- Compliance columns on agents -----------------------------------

ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS compliance_score       NUMERIC(5,2),
  ADD COLUMN IF NOT EXISTS compliance_severity    TEXT,
  ADD COLUMN IF NOT EXISTS compliance_issues_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_compliance_at      TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS battery_wear_pct        REAL,
  ADD COLUMN IF NOT EXISTS boot_duration_ms        BIGINT,
  ADD COLUMN IF NOT EXISTS gpu_name                TEXT;

-- ---------- Historical DEX indices (narrow hypertables) --------------------

CREATE TABLE agent_score_samples (
  time       TIMESTAMPTZ NOT NULL,
  agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  site_id    UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  score      REAL NOT NULL,
  ux_inputs  JSONB NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (agent_id, time)
);
CREATE INDEX agent_score_samples_site_time_idx
  ON agent_score_samples (site_id, time DESC);

CREATE TABLE agent_process_samples (
  time       TIMESTAMPTZ NOT NULL,
  agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  site_id    UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  pid        INTEGER NOT NULL,
  name       TEXT NOT NULL,
  user_name  TEXT,
  cpu_pct    REAL,
  rss_bytes  BIGINT,
  PRIMARY KEY (agent_id, time, pid)
);
CREATE INDEX agent_process_samples_site_time_idx
  ON agent_process_samples (site_id, time DESC);

CREATE TABLE agent_app_inventory_daily (
  day        DATE NOT NULL,
  agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  site_id    UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  version    TEXT NOT NULL DEFAULT '',
  publisher  TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (agent_id, day, name, version)
);
CREATE INDEX agent_app_inventory_daily_site_day_idx
  ON agent_app_inventory_daily (site_id, day DESC);

CREATE TABLE agent_patch_samples (
  time       TIMESTAMPTZ NOT NULL,
  agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  site_id    UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  kb         TEXT NOT NULL DEFAULT '',
  severity   TEXT NOT NULL DEFAULT '',
  size_mb    REAL,
  PRIMARY KEY (agent_id, time, title, kb)
);
CREATE INDEX agent_patch_samples_site_time_idx
  ON agent_patch_samples (site_id, time DESC);

CREATE TABLE agent_health_samples (
  time              TIMESTAMPTZ NOT NULL,
  agent_id          UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  site_id           UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  battery_pct       REAL,
  battery_wear_pct  REAL,
  bsod_24h          INTEGER,
  crash_24h         INTEGER,
  patch_count       INTEGER,
  wifi_rssi         INTEGER,
  logon_ms          REAL,
  boot_duration_ms  BIGINT,
  pending_reboot    BOOLEAN,
  PRIMARY KEY (agent_id, time)
);
CREATE INDEX agent_health_samples_site_time_idx
  ON agent_health_samples (site_id, time DESC);

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('agent_score_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists => TRUE);
    PERFORM create_hypertable('agent_process_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists => TRUE);
    PERFORM create_hypertable('agent_patch_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists => TRUE);
    PERFORM create_hypertable('agent_health_samples', 'time',
                              chunk_time_interval => INTERVAL '1 day',
                              if_not_exists => TRUE);
    -- daily inventory is date-keyed; promote if timescale accepts DATE as time
    BEGIN
      PERFORM create_hypertable('agent_app_inventory_daily', 'day',
                                chunk_time_interval => INTERVAL '30 days',
                                if_not_exists => TRUE);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'agent_app_inventory_daily hypertable skipped: %', SQLERRM;
    END;

    PERFORM add_retention_policy('agent_score_samples', INTERVAL '90 days', if_not_exists => TRUE);
    PERFORM add_retention_policy('agent_process_samples', INTERVAL '30 days', if_not_exists => TRUE);
    PERFORM add_retention_policy('agent_patch_samples', INTERVAL '90 days', if_not_exists => TRUE);
    PERFORM add_retention_policy('agent_health_samples', INTERVAL '90 days', if_not_exists => TRUE);

    BEGIN
      ALTER TABLE agent_score_samples SET (
        timescaledb.compress,
        timescaledb.compress_segmentby = 'agent_id',
        timescaledb.compress_orderby   = 'time DESC'
      );
      PERFORM add_compression_policy('agent_score_samples', INTERVAL '1 day', if_not_exists => TRUE);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'agent_score_samples compression skipped: %', SQLERRM;
    END;
    BEGIN
      ALTER TABLE agent_process_samples SET (
        timescaledb.compress,
        timescaledb.compress_segmentby = 'agent_id',
        timescaledb.compress_orderby   = 'time DESC'
      );
      PERFORM add_compression_policy('agent_process_samples', INTERVAL '1 day', if_not_exists => TRUE);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'agent_process_samples compression skipped: %', SQLERRM;
    END;
    BEGIN
      ALTER TABLE agent_patch_samples SET (
        timescaledb.compress,
        timescaledb.compress_segmentby = 'agent_id',
        timescaledb.compress_orderby   = 'time DESC'
      );
      PERFORM add_compression_policy('agent_patch_samples', INTERVAL '1 day', if_not_exists => TRUE);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'agent_patch_samples compression skipped: %', SQLERRM;
    END;
    BEGIN
      ALTER TABLE agent_health_samples SET (
        timescaledb.compress,
        timescaledb.compress_segmentby = 'agent_id',
        timescaledb.compress_orderby   = 'time DESC'
      );
      PERFORM add_compression_policy('agent_health_samples', INTERVAL '1 day', if_not_exists => TRUE);
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'agent_health_samples compression skipped: %', SQLERRM;
    END;
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'timescaledb hypertable promotion skipped: %', SQLERRM;
END$$;

-- ---------- System events stream -------------------------------------------

CREATE TABLE agent_system_events (
  id         BIGSERIAL,
  time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  site_id    UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  agent_id   UUID REFERENCES agents(id) ON DELETE SET NULL,
  kind       TEXT NOT NULL,
  severity   TEXT NOT NULL DEFAULT 'info'
             CHECK (severity IN ('emergency','critical','warning','info')),
  title      TEXT NOT NULL,
  body       TEXT NOT NULL DEFAULT '',
  metadata   JSONB NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (id, time)
);
CREATE INDEX agent_system_events_site_time_idx
  ON agent_system_events (site_id, time DESC);
CREATE INDEX agent_system_events_agent_time_idx
  ON agent_system_events (agent_id, time DESC)
  WHERE agent_id IS NOT NULL;
CREATE INDEX agent_system_events_kind_idx
  ON agent_system_events (kind, time DESC);

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
    PERFORM create_hypertable('agent_system_events', 'time',
                              chunk_time_interval => INTERVAL '7 days',
                              if_not_exists => TRUE);
    PERFORM add_retention_policy('agent_system_events', INTERVAL '90 days', if_not_exists => TRUE);
  END IF;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'agent_system_events hypertable skipped: %', SQLERRM;
END$$;

-- ---------- Compliance issue + CVE-lite tables -----------------------------

CREATE TABLE agent_compliance_issues (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  site_id     UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  category    TEXT NOT NULL CHECK (category IN ('patch','misconfig','vulnerability','policy')),
  code        TEXT NOT NULL,
  severity    TEXT NOT NULL CHECK (severity IN ('critical','high','medium','low','info')),
  title       TEXT NOT NULL,
  detail      TEXT NOT NULL DEFAULT '',
  detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  cleared_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX agent_compliance_issues_open_uidx
  ON agent_compliance_issues (agent_id, code)
  WHERE cleared_at IS NULL;
CREATE INDEX agent_compliance_issues_site_open_idx
  ON agent_compliance_issues (site_id, cleared_at)
  WHERE cleared_at IS NULL;

CREATE TABLE agent_vulnerabilities (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  cve_id      TEXT NOT NULL,
  severity    TEXT NOT NULL CHECK (severity IN ('critical','high','medium','low','info')),
  product     TEXT NOT NULL DEFAULT '',
  detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  cleared_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX agent_vulnerabilities_open_uidx
  ON agent_vulnerabilities (agent_id, cve_id)
  WHERE cleared_at IS NULL;
CREATE INDEX agent_vulnerabilities_agent_idx
  ON agent_vulnerabilities (agent_id, cleared_at);

-- ---------- Alarm rule target_kind (appliance|agent|any) -------------------

ALTER TABLE alarm_rules
  ADD COLUMN IF NOT EXISTS target_kind TEXT NOT NULL DEFAULT 'any'
    CHECK (target_kind IN ('appliance','agent','any'));

-- Seed agent DEX alarm rules
INSERT INTO alarm_rules (name, severity, expression, enabled, for_seconds, clear_for_seconds, target_kind)
VALUES
  ('Agent high CPU',              'warning',  'device.cpuPct > 90',              TRUE, 120, 300, 'agent'),
  ('Agent high memory',           'warning',  'device.memUsedRatio > 0.9',       TRUE, 120, 300, 'agent'),
  ('Agent UX score low',          'warning',  'device.score < 5',                TRUE, 300, 600, 'agent'),
  ('Agent missing patches',       'warning',  'device.missingPatchCount >= 5',   TRUE, 0,   0,   'agent'),
  ('Agent BSOD in last 24h',      'critical', 'device.bsod24h >= 1',             TRUE, 0,   0,   'agent'),
  ('Agent pending reboot',        'info',     'device.pendingReboot > 0',        TRUE, 0,   0,   'agent')
ON CONFLICT DO NOTHING;

-- ---------- Agent report templates -----------------------------------------

INSERT INTO report_templates (slug, title, vendor_scope, description, body_tmpl) VALUES
('agent-fleet-summary', 'Agent fleet summary', NULL,
 'Endpoint roster with UX score, compliance, group membership, and open agent alarms.',
$tmpl$# {{.Site.Name}} — Agent fleet summary

Generated {{.Now.Format "2006-01-02 15:04 MST"}}

## Agents ({{len .Agents}})

| Hostname | Group | Score | Compliance | Issues | Status |
|----------|-------|-------|------------|--------|--------|
{{range .Agents}}| {{.Hostname}} | {{.GroupName}} | {{.Score}} | {{.ComplianceScore}} | {{.IssuesCount}} | {{.Status}} |
{{end}}

## Open agent alarms ({{len .OpenAgentAlarms}})

{{if .OpenAgentAlarms}}| Severity | Title | Opened |
|----------|-------|--------|
{{range .OpenAgentAlarms}}| {{.Severity}} | {{.Title}} | {{.OpenedAt}} |
{{end}}{{else}}_None._{{end}}
$tmpl$),

('agent-compliance', 'Agent compliance posture', NULL,
 'Fleet compliance scores, open issues by severity, and CVE-lite hits.',
$tmpl$# {{.Site.Name}} — Agent compliance posture

Generated {{.Now.Format "2006-01-02 15:04 MST"}}

## Summary

- Agents: **{{.Compliance.AgentCount}}**
- Average compliance score: **{{.Compliance.AvgScore}}**
- Open issues: **{{.Compliance.OpenIssues}}**
- Open CVEs: **{{.Compliance.OpenCVEs}}**

## Agents by compliance

| Hostname | Score | Severity | Issues |
|----------|-------|----------|--------|
{{range .Agents}}| {{.Hostname}} | {{.ComplianceScore}} | {{.ComplianceSeverity}} | {{.IssuesCount}} |
{{end}}

## Top issues

{{if .ComplianceIssues}}| Severity | Category | Title | Host |
|----------|----------|-------|------|
{{range .ComplianceIssues}}| {{.Severity}} | {{.Category}} | {{.Title}} | {{.Hostname}} |
{{end}}{{else}}_None._{{end}}
$tmpl$),

('agent-patches', 'Missing patches by severity', NULL,
 'Outstanding patches across the endpoint fleet, grouped by severity.',
$tmpl$# {{.Site.Name}} — Missing patches

Generated {{.Now.Format "2006-01-02 15:04 MST"}}

## Patch summary

- Critical/High patch issues: **{{.Compliance.HighPatchIssues}}**
- Agents with pending reboot: **{{.Compliance.PendingRebootCount}}**

## Agents with missing patches

| Hostname | Patch count | Pending reboot | Compliance |
|----------|-------------|----------------|------------|
{{range .Agents}}{{if gt .PatchCount 0}}| {{.Hostname}} | {{.PatchCount}} | {{.PendingReboot}} | {{.ComplianceScore}} |
{{end}}{{end}}
$tmpl$)
ON CONFLICT (slug) DO NOTHING;
