package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	scrypto "github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/vendors/meraki"
)

// MerakiTelemetrySnapshot is stored in appliances.last_snapshot for
// Dashboard-sourced health (schema distinct from SNMP snapshots).
type MerakiTelemetrySnapshot struct {
	SchemaVersion  string                  `json:"schemaVersion"`
	Source         string                  `json:"source"`
	CapturedAt     time.Time               `json:"capturedAt"`
	Status         string                  `json:"status,omitempty"`
	ProductType    string                  `json:"productType,omitempty"`
	LastReportedAt string                  `json:"lastReportedAt,omitempty"`
	Name           string                  `json:"name,omitempty"`
	Uplinks        []MerakiUplinkSnap      `json:"uplinks,omitempty"`
	Ports          []MerakiPortSnap        `json:"ports,omitempty"`
	ClientCount    *int                    `json:"clientCount,omitempty"`
	PhysUp         *int                    `json:"physUp,omitempty"`
	PhysTotal      *int                    `json:"physTotal,omitempty"`
	UplinkCount    *int                    `json:"uplinkCount,omitempty"`
	LossLatency    []MerakiLossLatencySnap `json:"lossLatency,omitempty"`
}

// MerakiUplinkSnap is one WAN/cellular uplink for UI display.
type MerakiUplinkSnap struct {
	Interface string `json:"interface"`
	Status    string `json:"status"`
	IP        string `json:"ip,omitempty"`
	PublicIP  string `json:"publicIp,omitempty"`
}

// MerakiPortSnap is a condensed switch port status.
type MerakiPortSnap struct {
	PortID   string   `json:"portId"`
	Status   string   `json:"status"`
	Speed    string   `json:"speed,omitempty"`
	Enabled  bool     `json:"enabled"`
	IsUplink bool     `json:"isUplink"`
	Errors   []string `json:"errors,omitempty"`
}

// MerakiLossLatencySnap is the latest path-quality sample for an uplink.
type MerakiLossLatencySnap struct {
	Uplink      string   `json:"uplink"`
	LossPercent *float64 `json:"lossPercent,omitempty"`
	LatencyMs   *float64 `json:"latencyMs,omitempty"`
}

type merakiApplianceRow struct {
	ID     uuid.UUID
	Serial string
	Name   string
}

// StartMerakiTelemetry starts the Dashboard health poll loop for sites
// with Meraki sync enabled (same credentials as inventory sync).
func StartMerakiTelemetry(ctx context.Context, pool *pgxpool.Pool, sealer *scrypto.Sealer, log *slog.Logger) {
	go runMerakiTelemetryLoop(ctx, pool, sealer, log)
}

func runMerakiTelemetryLoop(ctx context.Context, pool *pgxpool.Pool, sealer *scrypto.Sealer, log *slog.Logger) {
	log.Info("meraki telemetry loop starting")
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	// First pass shortly after boot so Appliances light up without waiting
	// a full inventory interval.
	select {
	case <-ctx.Done():
		return
	case <-time.After(15 * time.Second):
		syncMerakiTelemetryDue(ctx, pool, sealer, log)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			syncMerakiTelemetryDue(ctx, pool, sealer, log)
		}
	}
}

func syncMerakiTelemetryDue(ctx context.Context, pool *pgxpool.Pool, sealer *scrypto.Sealer, log *slog.Logger) {
	rows, err := pool.Query(ctx, `
		SELECT ds.site_id, ds.meraki_org_ids, ds.meraki_sync_interval_seconds,
		       sc.id, sc.enc_secret
		  FROM site_discovery_settings ds
		  JOIN site_credentials sc ON sc.site_id = ds.site_id AND sc.kind = 'meraki'
		 WHERE ds.meraki_sync_enabled = TRUE
		   AND (ds.meraki_last_telemetry_at IS NULL
		        OR ds.meraki_last_telemetry_at < NOW() - make_interval(secs => ds.meraki_sync_interval_seconds))`)
	if err != nil {
		log.Debug("meraki telemetry query failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var siteID, credID uuid.UUID
		var orgRaw []byte
		var interval int
		var sealed []byte
		if rows.Scan(&siteID, &orgRaw, &interval, &credID, &sealed) != nil {
			continue
		}
		plain, err := sealer.Open(sealed, []byte("credential:"+credID.String()))
		if err != nil {
			log.Warn("meraki telemetry: unseal failed", "site_id", siteID.String(), "err", err)
			continue
		}
		apiKey := parseMerakiAPIKey(plain)
		if apiKey == "" {
			continue
		}
		var orgIDs []string
		_ = json.Unmarshal(orgRaw, &orgIDs)
		fctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		n, terr := SyncSiteMerakiTelemetry(fctx, pool, apiKey, siteID, orgIDs)
		recordMerakiTelemetryStatus(fctx, pool, siteID, terr)
		cancel()
		if terr != nil {
			log.Warn("meraki telemetry failed", "site_id", siteID.String(), "err", terr)
			continue
		}
		log.Info("meraki telemetry complete",
			"site_id", siteID.String(),
			"updated", n,
			"interval_s", interval,
		)
	}
}

func recordMerakiTelemetryStatus(ctx context.Context, pool *pgxpool.Pool, siteID uuid.UUID, syncErr error) {
	_, _ = pool.Exec(ctx, `
		INSERT INTO site_discovery_settings (site_id, meraki_last_telemetry_at)
		VALUES ($1, NOW())
		ON CONFLICT (site_id) DO UPDATE SET
		  meraki_last_telemetry_at = NOW()`,
		siteID)
	if syncErr != nil {
		// Keep inventory last_error separate; surface on appliances only.
		_ = syncErr
	}
}

// SyncSiteMerakiTelemetry pulls Dashboard health for Meraki appliances on a site.
func SyncSiteMerakiTelemetry(ctx context.Context, pool *pgxpool.Pool, apiKey string, siteID uuid.UUID, orgFilter []string) (int, error) {
	appliances, err := loadMerakiAppliancesBySerial(ctx, pool, siteID)
	if err != nil {
		return 0, err
	}
	if len(appliances) == 0 {
		return 0, nil
	}

	cli := meraki.New(apiKey)
	orgs, err := cli.ListOrganizations(ctx)
	if err != nil {
		return 0, err
	}
	allow := map[string]bool{}
	for _, id := range orgFilter {
		id = strings.TrimSpace(id)
		if id != "" {
			allow[id] = true
		}
	}

	updated := 0
	now := time.Now().UTC()
	for _, org := range orgs {
		if len(allow) > 0 && !allow[org.ID] {
			continue
		}
		statuses, err := cli.ListDeviceStatuses(ctx, org.ID)
		if err != nil {
			return updated, fmt.Errorf("device statuses for org %s: %w", org.Name, err)
		}
		statusBySerial := map[string]meraki.DeviceStatus{}
		for _, st := range statuses {
			if st.Serial != "" {
				statusBySerial[st.Serial] = st
			}
		}

		uplinkBySerial := map[string]meraki.ApplianceUplinkStatus{}
		if ups, uerr := cli.ListApplianceUplinkStatuses(ctx, org.ID); uerr == nil {
			for _, u := range ups {
				uplinkBySerial[u.Serial] = u
			}
		}

		portsBySerial := map[string]meraki.SwitchPortsBySwitch{}
		if sw, serr := cli.ListSwitchPortsStatusesBySwitch(ctx, org.ID); serr == nil {
			for _, s := range sw {
				portsBySerial[s.Serial] = s
			}
		}

		lossBySerial := map[string][]MerakiLossLatencySnap{}
		if ll, lerr := cli.ListUplinksLossAndLatency(ctx, org.ID); lerr == nil {
			for _, row := range ll {
				snap := latestLossLatency(row)
				if snap.Uplink == "" {
					continue
				}
				lossBySerial[row.Serial] = append(lossBySerial[row.Serial], snap)
			}
		}

		for serial, app := range appliances {
			st, ok := statusBySerial[serial]
			if !ok {
				// Device may live in another org; skip until matched.
				continue
			}
			tel := buildMerakiTelemetry(now, st, uplinkBySerial[serial], portsBySerial[serial], lossBySerial[serial])
			if err := PersistMerakiTelemetry(ctx, pool, app.ID, app.Name, tel); err != nil {
				continue
			}
			updated++
		}
	}
	return updated, nil
}

func loadMerakiAppliancesBySerial(ctx context.Context, pool *pgxpool.Pool, siteID uuid.UUID) (map[string]merakiApplianceRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, serial, name FROM appliances
		 WHERE site_id = $1 AND vendor = 'meraki'
		   AND serial IS NOT NULL AND serial <> ''
		   AND is_active = TRUE`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]merakiApplianceRow{}
	for rows.Next() {
		var r merakiApplianceRow
		var serial string
		if rows.Scan(&r.ID, &serial, &r.Name) != nil {
			continue
		}
		r.Serial = serial
		out[serial] = r
	}
	return out, nil
}

func latestLossLatency(row meraki.UplinkLossLatency) MerakiLossLatencySnap {
	out := MerakiLossLatencySnap{Uplink: row.Uplink}
	if len(row.TimeSeries) == 0 {
		return out
	}
	last := row.TimeSeries[len(row.TimeSeries)-1]
	out.LossPercent = last.LossPercent
	out.LatencyMs = last.LatencyMs
	return out
}

func buildMerakiTelemetry(
	now time.Time,
	st meraki.DeviceStatus,
	up meraki.ApplianceUplinkStatus,
	sw meraki.SwitchPortsBySwitch,
	loss []MerakiLossLatencySnap,
) MerakiTelemetrySnapshot {
	tel := MerakiTelemetrySnapshot{
		SchemaVersion:  "meraki-1",
		Source:         "meraki-dashboard",
		CapturedAt:     now,
		Status:         st.Status,
		ProductType:    st.ProductType,
		LastReportedAt: st.LastReportedAt,
		Name:           st.Name,
		LossLatency:    loss,
	}
	if st.Clients != nil {
		n := st.Clients.Counts.Total
		tel.ClientCount = &n
	}
	for _, u := range up.Uplinks {
		tel.Uplinks = append(tel.Uplinks, MerakiUplinkSnap{
			Interface: u.Interface,
			Status:    u.Status,
			IP:        u.IP,
			PublicIP:  u.PublicIP,
		})
	}
	if n := countActiveUplinks(tel.Uplinks); n > 0 {
		tel.UplinkCount = &n
	}
	if len(sw.Ports) > 0 {
		physTotal, physUp, uplinkN := 0, 0, 0
		for _, p := range sw.Ports {
			physTotal++
			connected := strings.EqualFold(p.Status, "Connected")
			if connected {
				physUp++
			}
			if p.IsUplink {
				uplinkN++
			}
			// Cap stored port list for snapshot size; denorm counts cover the rest.
			if len(tel.Ports) < 128 {
				tel.Ports = append(tel.Ports, MerakiPortSnap{
					PortID:   p.PortID,
					Status:   p.Status,
					Speed:    p.Speed,
					Enabled:  p.Enabled,
					IsUplink: p.IsUplink,
					Errors:   p.Errors,
				})
			}
		}
		tel.PhysTotal = &physTotal
		tel.PhysUp = &physUp
		if uplinkN > 0 {
			tel.UplinkCount = &uplinkN
		}
	}
	return tel
}

func countActiveUplinks(uplinks []MerakiUplinkSnap) int {
	n := 0
	for _, u := range uplinks {
		if strings.EqualFold(u.Status, "active") {
			n++
		}
	}
	return n
}

// PersistMerakiTelemetry writes Dashboard health into appliances + vendor samples.
func PersistMerakiTelemetry(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, fallbackName string, tel MerakiTelemetrySnapshot) error {
	blob, err := json.Marshal(tel)
	if err != nil {
		return fmt.Errorf("marshal meraki snapshot: %w", err)
	}

	var lastErr any
	status := strings.ToLower(strings.TrimSpace(tel.Status))
	switch status {
	case "offline", "dormant", "alerting":
		msg := "meraki device status: " + tel.Status
		lastErr = msg
	default:
		lastErr = nil
	}

	sysName := tel.Name
	if sysName == "" {
		sysName = fallbackName
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		UPDATE appliances
		   SET last_snapshot     = $2::jsonb,
		       last_snapshot_at  = $3,
		       last_polled_at    = $3,
		       last_error        = $4,
		       sys_name          = COALESCE(NULLIF($5,''), sys_name),
		       if_up_count       = COALESCE($6, if_up_count),
		       if_total_count    = COALESCE($7, if_total_count),
		       phys_up_count     = COALESCE($6, phys_up_count),
		       phys_total_count  = COALESCE($7, phys_total_count),
		       uplink_count      = COALESCE($8, uplink_count),
		       updated_at        = NOW()
		 WHERE id = $1`,
		id,
		string(blob),
		tel.CapturedAt,
		lastErr,
		sysName,
		intPtrSQL(tel.PhysUp),
		intPtrSQL(tel.PhysTotal),
		intPtrSQL(tel.UplinkCount),
	)
	if err != nil {
		return fmt.Errorf("update appliances: %w", err)
	}

	if err := persistMerakiVendorSamples(ctx, tx, id, tel); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func persistMerakiVendorSamples(ctx context.Context, tx pgx.Tx, id uuid.UUID, tel MerakiTelemetrySnapshot) error {
	batch := &pgx.Batch{}
	q := `INSERT INTO appliance_vendor_samples (appliance_id, time, metric_key, value_double, value_text)
	      VALUES ($1, $2, $3, $4, NULLIF($5,''))
	      ON CONFLICT (appliance_id, metric_key, time) DO UPDATE SET
	        value_double = EXCLUDED.value_double,
	        value_text   = EXCLUDED.value_text`
	add := func(key string, num *float64, txt string) {
		var n any
		if num != nil {
			n = *num
		}
		batch.Queue(q, id, tel.CapturedAt, key, n, txt)
	}
	online := 0.0
	if strings.EqualFold(tel.Status, "online") {
		online = 1
	}
	add("meraki.status.online", &online, tel.Status)

	for _, ll := range tel.LossLatency {
		up := strings.ToLower(strings.TrimSpace(ll.Uplink))
		if up == "" {
			continue
		}
		if ll.LossPercent != nil {
			v := *ll.LossPercent
			add("meraki.uplink."+up+".loss_pct", &v, "")
		}
		if ll.LatencyMs != nil {
			v := *ll.LatencyMs
			add("meraki.uplink."+up+".latency_ms", &v, "")
		}
	}
	if tel.PhysUp != nil {
		v := float64(*tel.PhysUp)
		add("meraki.switch.ports.up", &v, "")
	}
	if tel.PhysTotal != nil {
		v := float64(*tel.PhysTotal)
		add("meraki.switch.ports.total", &v, "")
	}
	if tel.ClientCount != nil {
		v := float64(*tel.ClientCount)
		add("meraki.wireless.clients", &v, "")
	}
	if batch.Len() == 0 {
		return nil
	}
	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("meraki vendor sample %d: %w", i, err)
		}
	}
	return br.Close()
}

func intPtrSQL(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
