// Package reports generates Markdown (and, optionally, HTML/text)
// reports from the data Sonar already collects: appliances, alarms,
// passive-SNMP inventory, and per-snapshot vendor health. Templates
// live in the database (table report_templates) and use Go's standard
// text/template syntax — no external template engine, no rendering
// service.
//
// The shape of data each template receives is intentionally flat: a
// Site, a slice of Appliance lines pre-decorated with vendor-specific
// fields, the open-alarm count, and a discovered-device count. That
// keeps templates readable and lets us add new vendor fields without
// breaking older templates.
package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

// SiteContext is the .Site root the templates reference.
type SiteContext struct {
	ID   uuid.UUID
	Name string
}

// ApplianceLine is one row of the report's appliance table. Every
// vendor-specific field is included as a string (already formatted —
// "—" for missing values) so templates don't have to do null guards.
type ApplianceLine struct {
	ID         uuid.UUID
	Name       string
	Vendor     string
	MgmtIP     string
	LastPolled string
	Status     string

	// UPS-specific
	UPSBatteryPct  string
	UPSRuntimeMin  string
	UPSLoadPct     string
	UPSBatteryTemp string
	UPSReplaceStr  string
	UPSOutputStr   string

	// Synology-specific
	SynModel        string
	SynDSM          string
	SynSystemStr    string
	SynPowerStr     string
	SynDiskTempMax  string
	SynRAIDWorstStr string

	// Switch (Cisco) specific
	SwitchModel     string
	SwitchSW        string
	SwitchCPU5min   string
	SwitchMemPct    string
	SwitchPhysUp    int32
	SwitchPhysTotal int32
}

// AlarmLine is the open-alarm row in the report.
type AlarmLine struct {
	Severity string
	Title    string
	OpenedAt string
}

// Context is the root template object.
type Context struct {
	Site            SiteContext
	Now             time.Time
	Appliances      []ApplianceLine
	OpenAlarms      []AlarmLine
	DiscoveredCount int
}

// Generated is what Generate returns: the rendered body plus the
// metadata blob persisted alongside it.
type Generated struct {
	Format   string
	Content  string
	Metadata map[string]any
}

// Generate renders the named template against the named site. Returns
// ErrTemplateNotFound when the slug doesn't exist.
func Generate(ctx context.Context, pool *pgxpool.Pool, templateSlug string, siteID uuid.UUID, generatedBy string) (*Generated, error) {
	var title, body string
	err := pool.QueryRow(ctx, `
		SELECT title, body_tmpl FROM report_templates WHERE slug = $1`, templateSlug).
		Scan(&title, &body)
	if err != nil {
		return nil, fmt.Errorf("template %q: %w", templateSlug, err)
	}

	dataCtx, err := buildContext(ctx, pool, siteID)
	if err != nil {
		return nil, fmt.Errorf("build context: %w", err)
	}

	tmpl, err := template.New(templateSlug).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, dataCtx); err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}

	return &Generated{
		Format:  "markdown",
		Content: buf.String(),
		Metadata: map[string]any{
			"templateSlug":    templateSlug,
			"templateTitle":   title,
			"siteId":          siteID.String(),
			"generatedBy":     generatedBy,
			"applianceCount":  len(dataCtx.Appliances),
			"openAlarmCount":  len(dataCtx.OpenAlarms),
			"discoveredCount": dataCtx.DiscoveredCount,
		},
	}, nil
}

// buildContext loads the site row plus everything templates may
// reference. Stays in one read transaction so a mid-generate write
// can't tear the report.
func buildContext(ctx context.Context, pool *pgxpool.Pool, siteID uuid.UUID) (*Context, error) {
	c := &Context{Now: time.Now().UTC()}

	if err := pool.QueryRow(ctx, `SELECT id, name FROM sites WHERE id = $1`, siteID).
		Scan(&c.Site.ID, &c.Site.Name); err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, `
		SELECT id, name, host(mgmt_ip), COALESCE(vendor,''),
		       COALESCE(last_polled_at::text,''),
		       CASE WHEN last_error IS NULL OR last_error = '' THEN 'ok' ELSE 'error' END,
		       last_snapshot,
		       COALESCE(phys_up_count,0), COALESCE(phys_total_count,0),
		       COALESCE(cpu_pct,0)::float8,
		       CASE WHEN COALESCE(mem_total_bytes,0) > 0
		            THEN (COALESCE(mem_used_bytes,0)::float8 / mem_total_bytes::float8) * 100.0
		            ELSE 0 END
		  FROM appliances
		 WHERE site_id = $1 AND is_active
		 ORDER BY name`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                uuid.UUID
			name, ip, vendor  string
			lastPolled        string
			status            string
			snapBytes         []byte
			physUp, physTotal int32
			cpuPct            float64
			memPct            float64
		)
		if err := rows.Scan(&id, &name, &ip, &vendor, &lastPolled, &status,
			&snapBytes, &physUp, &physTotal, &cpuPct, &memPct); err != nil {
			return nil, err
		}
		line := ApplianceLine{
			ID:              id,
			Name:            name,
			Vendor:          vendor,
			MgmtIP:          ip,
			LastPolled:      shortTime(lastPolled),
			Status:          status,
			SwitchPhysUp:    physUp,
			SwitchPhysTotal: physTotal,
		}
		decorateAppliance(&line, snapBytes, cpuPct, memPct)
		c.Appliances = append(c.Appliances, line)
	}

	alarmRows, err := pool.Query(ctx, `
		SELECT severity, title, opened_at::text
		  FROM alarms
		 WHERE site_id = $1 AND cleared_at IS NULL
		 ORDER BY opened_at DESC LIMIT 100`, siteID)
	if err != nil {
		return c, nil
	}
	defer alarmRows.Close()
	for alarmRows.Next() {
		var a AlarmLine
		if err := alarmRows.Scan(&a.Severity, &a.Title, &a.OpenedAt); err == nil {
			c.OpenAlarms = append(c.OpenAlarms, a)
		}
	}

	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM passive_snmp_inventory
		 WHERE site_id = $1 AND status = 'active'`, siteID).Scan(&c.DiscoveredCount)

	return c, nil
}

// decorateAppliance fills the vendor-specific *Str fields on a line
// from the appliances.last_snapshot JSONB. It tolerates an absent or
// schema-v1 snapshot by leaving the fields as "—".
func decorateAppliance(line *ApplianceLine, snapBytes []byte, cpuPct, memPct float64) {
	dash := "—"
	line.UPSBatteryPct = dash
	line.UPSRuntimeMin = dash
	line.UPSLoadPct = dash
	line.UPSBatteryTemp = dash
	line.UPSReplaceStr = dash
	line.UPSOutputStr = dash
	line.SynModel = dash
	line.SynDSM = dash
	line.SynSystemStr = dash
	line.SynPowerStr = dash
	line.SynDiskTempMax = dash
	line.SynRAIDWorstStr = dash
	line.SwitchModel = dash
	line.SwitchSW = dash
	line.SwitchCPU5min = strconv.FormatFloat(cpuPct, 'f', 1, 64)
	line.SwitchMemPct = strconv.FormatFloat(memPct, 'f', 1, 64)

	if len(snapBytes) == 0 {
		return
	}
	var snap snmp.Snapshot
	if json.Unmarshal(snapBytes, &snap) != nil {
		return
	}
	if v := snap.Vendor; v != nil {
		if u := v.UPS; u != nil {
			if u.EstChargePct != nil {
				line.UPSBatteryPct = strconv.Itoa(int(*u.EstChargePct))
			}
			if u.EstRuntimeMin != nil {
				line.UPSRuntimeMin = strconv.Itoa(int(*u.EstRuntimeMin))
			}
			if u.OutputLoadPct != nil {
				line.UPSLoadPct = strconv.Itoa(int(*u.OutputLoadPct))
			}
			if u.BatteryTempC != nil {
				line.UPSBatteryTemp = strconv.FormatFloat(*u.BatteryTempC, 'f', 1, 64)
			}
			if u.BatteryReplaceNeeded != nil {
				if *u.BatteryReplaceNeeded {
					line.UPSReplaceStr = "Yes"
				} else {
					line.UPSReplaceStr = "No"
				}
			}
			if u.OutputStatus != nil {
				line.UPSOutputStr = upsOutputStatusStr(*u.OutputStatus)
			}
		}
		if s := v.Synology; s != nil {
			line.SynModel = s.Model
			line.SynDSM = s.DSMVersion
			if s.SystemStatus != nil {
				line.SynSystemStr = synologyStatusStr(*s.SystemStatus)
			}
			if s.PowerStatus != nil {
				line.SynPowerStr = synologyStatusStr(*s.PowerStatus)
			}
			var maxTemp float64
			for _, d := range s.Disks {
				if d.TempC > maxTemp {
					maxTemp = d.TempC
				}
			}
			if maxTemp > 0 {
				line.SynDiskTempMax = strconv.FormatFloat(maxTemp, 'f', 1, 64)
			}
			var worst int32
			for _, vol := range s.Volumes {
				if vol.Status > worst {
					worst = vol.Status
				}
			}
			if worst > 0 {
				line.SynRAIDWorstStr = synologyRAIDStatusStr(worst)
			}
		}
		if c := v.Cisco; c != nil && c.CPU5min != nil {
			line.SwitchCPU5min = strconv.FormatFloat(*c.CPU5min, 'f', 1, 64)
		}
	}
	// Pull a model + software string from system info / entity rows.
	if line.SwitchModel == "—" {
		for _, e := range snap.Entities {
			if e.Class == 3 && e.ModelName != "" { // chassis
				line.SwitchModel = e.ModelName
				if line.SwitchSW == "—" {
					line.SwitchSW = e.SoftwareRev
				}
				break
			}
		}
	}
}

func shortTime(s string) string {
	// Postgres returns ISO ("2026-05-06 22:01:00.123-04"); trim to
	// minute precision for readability.
	s = strings.TrimSpace(s)
	if len(s) >= 16 {
		return s[:16]
	}
	return s
}

func upsOutputStatusStr(n int32) string {
	switch n {
	case 1:
		return "unknown"
	case 2:
		return "onLine"
	case 3:
		return "onBattery"
	case 4:
		return "onSmartBoost"
	case 5:
		return "timedSleeping"
	case 6:
		return "softwareBypass"
	case 7:
		return "off"
	case 8:
		return "rebooting"
	case 9:
		return "switchedBypass"
	case 10:
		return "hardwareFailure"
	case 11:
		return "onSmartTrim"
	case 12:
		return "ecoMode"
	default:
		return strconv.Itoa(int(n))
	}
}

func synologyStatusStr(n int32) string {
	switch n {
	case 1:
		return "Normal"
	case 2:
		return "Failed"
	default:
		return strconv.Itoa(int(n))
	}
}

func synologyRAIDStatusStr(n int32) string {
	switch n {
	case 1:
		return "Normal"
	case 2:
		return "Repairing"
	case 3:
		return "Migrating"
	case 4:
		return "Expanding"
	case 5:
		return "Deleting"
	case 6:
		return "Creating"
	case 7:
		return "RaidSyncing"
	case 8:
		return "RaidParityChecking"
	case 9:
		return "RaidAssembling"
	case 10:
		return "Canceling"
	case 11:
		return "Degrade"
	case 12:
		return "Crashed"
	default:
		return strconv.Itoa(int(n))
	}
}
