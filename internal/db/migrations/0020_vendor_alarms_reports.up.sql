-- =============================================================================
-- 0020_vendor_alarms_reports.up.sql — seed vendor-health alarm rules, add the
-- report_templates / reports tables for centrally-generated Markdown reports.
-- =============================================================================

-- ---------- Seed default vendor-health alarm rules ---------------------------
--
-- These rules use the existing alarm_rules.expression DSL plus the
-- vendor health fields the API now flattens into the metrics.appliance
-- NATS payload (see internal/api/vendor_metrics_payload.go). Site_id is
-- NULL so they apply to every site; an operator can disable them per
-- site by either editing or copying-then-disabling.

INSERT INTO alarm_rules (name, severity, expression, enabled, for_seconds, clear_for_seconds)
VALUES
  ('UPS battery low (charge)',          'critical', 'device.battery_charge_pct < 50', TRUE, 60,  120),
  ('UPS battery runtime low',           'critical', 'device.battery_runtime_min < 10', TRUE, 30, 120),
  ('UPS overload',                      'warning',  'device.ups_load_pct > 80', TRUE, 120, 300),
  ('UPS battery overheating',           'warning',  'device.battery_temp_c > 35', TRUE, 300, 600),
  ('UPS battery replacement needed',    'warning',  'device.battery_replace_needed > 0', TRUE, 0, 0),
  ('UPS on battery (output not normal)','warning',  'device.ups_output_status != 2', TRUE, 30, 60),

  ('Synology RAID degraded',            'critical', 'device.synology_raid_worst_status > 1', TRUE, 0, 0),
  ('Synology disk failed',              'critical', 'device.synology_disk_worst_status > 1', TRUE, 0, 0),
  ('Synology disk overheating',         'warning',  'device.synology_disk_temp_max_c > 50', TRUE, 300, 600),
  ('Synology system fault',             'critical', 'device.synology_system_status != 1', TRUE, 60, 120),
  ('Synology power fault',              'critical', 'device.synology_power_status != 1', TRUE, 60, 120),

  ('Palo Alto session table near full', 'warning',  'device.session_util_pct > 80', TRUE, 300, 600),
  ('Palo Alto session table critical',  'critical', 'device.session_util_pct > 90', TRUE, 60, 300),

  ('Alletra volume nearly full',        'warning',  'device.volume_used_pct_max > 85', TRUE, 600, 1800),
  ('Alletra volume critically full',    'critical', 'device.volume_used_pct_max > 95', TRUE, 300, 600),

  ('Cisco CPU 5-min sustained',         'warning',  'device.cisco_cpu_5min_pct > 85', TRUE, 600, 1200)
ON CONFLICT DO NOTHING;

-- ---------- Reports + templates ---------------------------------------------

CREATE TABLE report_templates (
  slug         TEXT PRIMARY KEY,
  title        TEXT NOT NULL,
  vendor_scope TEXT,
  description  TEXT,
  body_tmpl    TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_report_templates_updated_at BEFORE UPDATE ON report_templates
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE reports (
  id            BIGSERIAL PRIMARY KEY,
  template_slug TEXT NOT NULL REFERENCES report_templates(slug) ON DELETE CASCADE,
  site_id       UUID REFERENCES sites(id) ON DELETE CASCADE,
  generated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  generated_by  TEXT NOT NULL,
  format        TEXT NOT NULL DEFAULT 'markdown'
                CHECK (format IN ('markdown','html','text')),
  content       TEXT NOT NULL,
  size_bytes    INTEGER NOT NULL,
  metadata      JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX reports_template_time_idx
  ON reports (template_slug, generated_at DESC);
CREATE INDEX reports_site_time_idx
  ON reports (site_id, generated_at DESC);

-- ---------- Seed report templates -------------------------------------------
--
-- body_tmpl is a Go text/template. Available variables:
--   .Site            (struct { ID, Name string })
--   .Now             (time.Time)
--   .Appliances      ([]ApplianceLine - vendor-scoped subset)
--   .OpenAlarms      ([]AlarmLine)
--   .DiscoveredCount (int)
-- Templates that need vendor-specific data (UPS, Synology) read from
-- .Appliances filtered by .Vendor and pull a few representative
-- vendor health fields off the snapshot via .VendorJSON.

INSERT INTO report_templates (slug, title, vendor_scope, description, body_tmpl) VALUES
('site-summary', 'Site summary', NULL, 'Per-site appliance roster, open alarms, last-poll status.',
$tmpl$# {{.Site.Name}} — Site summary

Generated {{.Now.Format "2006-01-02 15:04 MST"}}

## Appliances

| Name | Vendor | IP | Last poll | Status |
|------|--------|----|-----------|--------|
{{range .Appliances}}| {{.Name}} | {{.Vendor}} | {{.MgmtIP}} | {{.LastPolled}} | {{.Status}} |
{{end}}

## Open alarms ({{len .OpenAlarms}})

{{if .OpenAlarms}}| Severity | Title | Opened |
|----------|-------|--------|
{{range .OpenAlarms}}| {{.Severity}} | {{.Title}} | {{.OpenedAt}} |
{{end}}{{else}}_None._{{end}}

## Discovery

Devices currently in passive-SNMP inventory: **{{.DiscoveredCount}}**
$tmpl$),

('ups-fleet', 'UPS fleet health', 'apc',
'Battery charge, runtime, load, temperature, and replace status across every UPS in the site.',
$tmpl$# {{.Site.Name}} — UPS fleet health

Generated {{.Now.Format "2006-01-02 15:04 MST"}}

| Name | Battery % | Runtime min | Load % | Battery temp °C | Replace? | Output |
|------|-----------|-------------|--------|------------------|----------|--------|
{{range .Appliances}}{{if eq .Vendor "apc"}}| {{.Name}} | {{.UPSBatteryPct}} | {{.UPSRuntimeMin}} | {{.UPSLoadPct}} | {{.UPSBatteryTemp}} | {{.UPSReplaceStr}} | {{.UPSOutputStr}} |
{{end}}{{end}}
$tmpl$),

('synology-fleet', 'Synology NAS fleet', 'synology',
'System status, disk temps, RAID health for every Synology in the site.',
$tmpl$# {{.Site.Name}} — Synology fleet

Generated {{.Now.Format "2006-01-02 15:04 MST"}}

| Name | Model | DSM | System | Power | Disk temp max °C | RAID worst |
|------|-------|-----|--------|-------|------------------|------------|
{{range .Appliances}}{{if eq .Vendor "synology"}}| {{.Name}} | {{.SynModel}} | {{.SynDSM}} | {{.SynSystemStr}} | {{.SynPowerStr}} | {{.SynDiskTempMax}} | {{.SynRAIDWorstStr}} |
{{end}}{{end}}
$tmpl$),

('switch-fleet', 'Switch fleet', 'cisco',
'Cisco switches: model, IOS version, CPU 5-min, memory, port utilization.',
$tmpl$# {{.Site.Name}} — Switch fleet

Generated {{.Now.Format "2006-01-02 15:04 MST"}}

| Name | Model | IOS | CPU 5-min % | Mem used % | Ports up/total |
|------|-------|-----|-------------|------------|----------------|
{{range .Appliances}}{{if eq .Vendor "cisco"}}| {{.Name}} | {{.SwitchModel}} | {{.SwitchSW}} | {{.SwitchCPU5min}} | {{.SwitchMemPct}} | {{.SwitchPhysUp}}/{{.SwitchPhysTotal}} |
{{end}}{{end}}
$tmpl$)
ON CONFLICT (slug) DO NOTHING;
