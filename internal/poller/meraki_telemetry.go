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
	Model          string                  `json:"model,omitempty"`
	LastReportedAt string                  `json:"lastReportedAt,omitempty"`
	Name           string                  `json:"name,omitempty"`
	MAC            string                  `json:"mac,omitempty"`
	PublicIP       string                  `json:"publicIp,omitempty"`
	LANIP          string                  `json:"lanIp,omitempty"`
	Gateway        string                  `json:"gateway,omitempty"`
	IPType         string                  `json:"ipType,omitempty"`
	PrimaryDNS     string                  `json:"primaryDns,omitempty"`
	SecondaryDNS   string                  `json:"secondaryDns,omitempty"`
	Tags           []string                `json:"tags,omitempty"`
	PowerSupplies  []MerakiPowerSupplySnap `json:"powerSupplies,omitempty"`
	HA             *MerakiHASnap           `json:"highAvailability,omitempty"`
	Uplinks        []MerakiUplinkSnap      `json:"uplinks,omitempty"`
	Ports          []MerakiPortSnap        `json:"ports,omitempty"`
	ClientCount    *int                    `json:"clientCount,omitempty"`
	PhysUp         *int                    `json:"physUp,omitempty"`
	PhysTotal      *int                    `json:"physTotal,omitempty"`
	UplinkCount    *int                    `json:"uplinkCount,omitempty"`
	PortErrorCount *int                    `json:"portErrorCount,omitempty"`
	LossLatency    []MerakiLossLatencySnap `json:"lossLatency,omitempty"`
	VPN            *MerakiVPNSnap          `json:"vpn,omitempty"`
	WirelessLoss   *MerakiWirelessLossSnap `json:"wirelessLoss,omitempty"`
	PerfScore      *float64                `json:"perfScore,omitempty"`
	Firmware       *MerakiFirmwareSnap     `json:"firmware,omitempty"`
	SensorReadings []MerakiSensorReading   `json:"sensorReadings,omitempty"`
	Alerts         []MerakiAlertSnap       `json:"alerts,omitempty"`
}

type MerakiPowerSupplySnap struct {
	Slot    int    `json:"slot"`
	Serial  string `json:"serial,omitempty"`
	Model   string `json:"model,omitempty"`
	Status  string `json:"status,omitempty"`
	PoEMax  *int   `json:"poeMaximum,omitempty"`
	PoEUnit string `json:"poeUnit,omitempty"`
}

type MerakiHASnap struct {
	Enabled bool   `json:"enabled"`
	Role    string `json:"role,omitempty"`
}

type MerakiUplinkSnap struct {
	Interface      string `json:"interface"`
	Status         string `json:"status"`
	IP             string `json:"ip,omitempty"`
	Gateway        string `json:"gateway,omitempty"`
	PublicIP       string `json:"publicIp,omitempty"`
	PrimaryDNS     string `json:"primaryDns,omitempty"`
	SecondaryDNS   string `json:"secondaryDns,omitempty"`
	IPAssignedBy   string `json:"ipAssignedBy,omitempty"`
	Provider       string `json:"provider,omitempty"`
	SignalType     string `json:"signalType,omitempty"`
	ICCID          string `json:"iccid,omitempty"`
	ConnectionType string `json:"connectionType,omitempty"`
	RSRP           string `json:"rsrp,omitempty"`
	RSRQ           string `json:"rsrq,omitempty"`
}

type MerakiPortSnap struct {
	PortID       string   `json:"portId"`
	Status       string   `json:"status"`
	Speed        string   `json:"speed,omitempty"`
	Duplex       string   `json:"duplex,omitempty"`
	Enabled      bool     `json:"enabled"`
	IsUplink     bool     `json:"isUplink"`
	Errors       []string `json:"errors,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	PoEAllocated *bool    `json:"poeAllocated,omitempty"`
	SecurePort   string   `json:"securePort,omitempty"`
	STPStatuses  []string `json:"stpStatuses,omitempty"`
	RxPackets    *int64   `json:"rxPackets,omitempty"`
	TxPackets    *int64   `json:"txPackets,omitempty"`
	TotalPackets *int64   `json:"totalPackets,omitempty"`
}

type MerakiLossLatencySnap struct {
	Uplink      string   `json:"uplink"`
	LossPercent *float64 `json:"lossPercent,omitempty"`
	LatencyMs   *float64 `json:"latencyMs,omitempty"`
}

type MerakiVPNSnap struct {
	Mode               string              `json:"mode,omitempty"`
	DeviceStatus       string              `json:"deviceStatus,omitempty"`
	MerakiPeers        []MerakiVPNPeerSnap `json:"merakiPeers,omitempty"`
	ThirdPartyPeers    []MerakiVPNPeerSnap `json:"thirdPartyPeers,omitempty"`
	ReachablePeerCount int                 `json:"reachablePeerCount"`
	TotalPeerCount     int                 `json:"totalPeerCount"`
}

type MerakiVPNPeerSnap struct {
	Name         string `json:"name"`
	Reachability string `json:"reachability,omitempty"`
	PublicIP     string `json:"publicIp,omitempty"`
}

type MerakiWirelessLossSnap struct {
	DownstreamLossPct *float64 `json:"downstreamLossPct,omitempty"`
	UpstreamLossPct   *float64 `json:"upstreamLossPct,omitempty"`
}

type MerakiFirmwareSnap struct {
	Current     string `json:"current,omitempty"`
	NextUpgrade string `json:"nextUpgrade,omitempty"`
	NextAt      string `json:"nextAt,omitempty"`
	Status      string `json:"status,omitempty"`
}

type MerakiSensorReading struct {
	Metric string   `json:"metric"`
	TS     string   `json:"ts,omitempty"`
	Value  *float64 `json:"value,omitempty"`
	Bool   *bool    `json:"bool,omitempty"`
	Unit   string   `json:"unit,omitempty"`
}

type MerakiAlertSnap struct {
	ID        string `json:"id,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Title     string `json:"title,omitempty"`
	Type      string `json:"type,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
}

type merakiApplianceRow struct {
	ID     uuid.UUID
	Serial string
	Name   string
}

type merakiOrgExtras struct {
	vpnBySerial      map[string]meraki.ApplianceVPNStatus
	wlanLossBySerial map[string]meraki.WirelessPacketLossByDevice
	fwBySerial       map[string]meraki.FirmwareUpgradeByDevice
	sensorBySerial   map[string]meraki.SensorReadingLatest
	alertsBySerial   map[string][]meraki.AssuranceAlert
	devicesBySerial  map[string]meraki.Device
	perfBySerial     map[string]float64
	portPktsBySerial map[string]map[string]meraki.SwitchPortPackets
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
		fctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
		n, terr := SyncSiteMerakiTelemetry(fctx, pool, log, apiKey, siteID, orgIDs)
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
	_ = syncErr
}

// SyncSiteMerakiTelemetry pulls Dashboard health for Meraki appliances on a site.
func SyncSiteMerakiTelemetry(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, apiKey string, siteID uuid.UUID, orgFilter []string) (int, error) {
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
		if ups, uerr := cli.ListApplianceUplinkStatuses(ctx, org.ID); uerr != nil {
			log.Warn("meraki appliance uplinks failed", "org", org.Name, "err", uerr)
		} else {
			for _, u := range ups {
				uplinkBySerial[u.Serial] = u
			}
		}

		portsBySerial := map[string]meraki.SwitchPortsBySwitch{}
		if sw, serr := cli.ListSwitchPortsStatusesBySwitch(ctx, org.ID); serr != nil {
			log.Warn("meraki switch ports bySwitch failed", "org", org.Name, "err", serr)
		} else {
			for _, s := range sw {
				portsBySerial[s.Serial] = s
			}
		}

		lossBySerial := map[string][]MerakiLossLatencySnap{}
		if ll, lerr := cli.ListUplinksLossAndLatency(ctx, org.ID); lerr != nil {
			log.Warn("meraki uplinks loss/latency failed", "org", org.Name, "err", lerr)
		} else {
			for _, row := range ll {
				snap := latestLossLatency(row)
				if snap.Uplink == "" {
					continue
				}
				lossBySerial[row.Serial] = append(lossBySerial[row.Serial], snap)
			}
		}

		extras := loadMerakiOrgExtras(ctx, cli, log, org.ID, statusBySerial)

		for serial, app := range appliances {
			st, ok := statusBySerial[serial]
			if !ok {
				continue
			}
			tel := buildMerakiTelemetry(now, st, uplinkBySerial[serial], portsBySerial[serial], lossBySerial[serial], extras, serial)
			if err := PersistMerakiTelemetry(ctx, pool, app.ID, app.Name, tel); err != nil {
				continue
			}
			updated++
		}
	}
	return updated, nil
}

func loadMerakiOrgExtras(
	ctx context.Context,
	cli *meraki.Client,
	log *slog.Logger,
	orgID string,
	statuses map[string]meraki.DeviceStatus,
) merakiOrgExtras {
	ex := merakiOrgExtras{
		vpnBySerial:      map[string]meraki.ApplianceVPNStatus{},
		wlanLossBySerial: map[string]meraki.WirelessPacketLossByDevice{},
		fwBySerial:       map[string]meraki.FirmwareUpgradeByDevice{},
		sensorBySerial:   map[string]meraki.SensorReadingLatest{},
		alertsBySerial:   map[string][]meraki.AssuranceAlert{},
		devicesBySerial:  map[string]meraki.Device{},
		perfBySerial:     map[string]float64{},
		portPktsBySerial: map[string]map[string]meraki.SwitchPortPackets{},
	}
	if vpn, err := cli.ListApplianceVPNStatuses(ctx, orgID); err != nil {
		log.Warn("meraki vpn statuses failed", "org", orgID, "err", err)
	} else {
		for _, v := range vpn {
			if v.DeviceSerial != "" {
				ex.vpnBySerial[v.DeviceSerial] = v
			}
		}
	}
	if pkts, err := cli.ListSwitchPortsStatusesPacketsByDeviceByPort(ctx, orgID); err != nil {
		log.Warn("meraki switch port packets failed", "org", orgID, "err", err)
	} else {
		for _, sw := range pkts {
			if sw.Serial == "" {
				continue
			}
			byPort := map[string]meraki.SwitchPortPackets{}
			for _, p := range sw.Ports {
				byPort[p.PortID] = p
			}
			ex.portPktsBySerial[sw.Serial] = byPort
		}
	}
	if wl, err := cli.ListWirelessPacketLossByDevice(ctx, orgID); err != nil {
		log.Warn("meraki wireless packet loss failed", "org", orgID, "err", err)
	} else {
		for _, w := range wl {
			if w.Device.Serial != "" {
				ex.wlanLossBySerial[w.Device.Serial] = w
			}
		}
	}
	if fw, err := cli.ListFirmwareUpgradesByDevice(ctx, orgID); err != nil {
		log.Warn("meraki firmware by device failed", "org", orgID, "err", err)
	} else {
		for _, f := range fw {
			serial := f.Serial
			if serial == "" && f.Device != nil {
				serial = f.Device.Serial
			}
			if serial != "" {
				ex.fwBySerial[serial] = f
			}
		}
	}
	if sens, err := cli.ListSensorReadingsLatest(ctx, orgID); err != nil {
		log.Warn("meraki sensor readings failed", "org", orgID, "err", err)
	} else {
		for _, s := range sens {
			if s.Serial != "" {
				ex.sensorBySerial[s.Serial] = s
			}
		}
	}
	if alerts, err := cli.ListAssuranceAlerts(ctx, orgID); err != nil {
		log.Warn("meraki assurance alerts failed", "org", orgID, "err", err)
	} else {
		for _, a := range alerts {
			if a.DeviceSerial == "" {
				continue
			}
			ex.alertsBySerial[a.DeviceSerial] = append(ex.alertsBySerial[a.DeviceSerial], a)
		}
	}
	if devices, err := cli.ListDevices(ctx, orgID); err != nil {
		log.Warn("meraki list devices (firmware) failed", "org", orgID, "err", err)
	} else {
		for _, d := range devices {
			if d.Serial != "" {
				ex.devicesBySerial[d.Serial] = d
			}
		}
	}
	// MX performance is per-serial; only hit appliances that appear in statuses.
	for serial, st := range statuses {
		if !strings.EqualFold(st.ProductType, "appliance") {
			continue
		}
		perf, err := cli.GetAppliancePerformance(ctx, serial)
		if err != nil || perf.PerfScore == nil {
			continue
		}
		ex.perfBySerial[serial] = *perf.PerfScore
	}
	return ex
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
	ex merakiOrgExtras,
	serial string,
) MerakiTelemetrySnapshot {
	tel := MerakiTelemetrySnapshot{
		SchemaVersion:  "meraki-2",
		Source:         "meraki-dashboard",
		CapturedAt:     now,
		Status:         st.Status,
		ProductType:    st.ProductType,
		Model:          st.Model,
		LastReportedAt: st.LastReportedAt,
		Name:           st.Name,
		MAC:            st.MAC,
		PublicIP:       st.PublicIP,
		LANIP:          st.LANIP,
		Gateway:        st.Gateway,
		IPType:         st.IPType,
		PrimaryDNS:     st.PrimaryDNS,
		SecondaryDNS:   st.SecondaryDNS,
		Tags:           st.Tags,
		LossLatency:    loss,
	}
	if st.Clients != nil {
		n := st.Clients.Counts.Total
		tel.ClientCount = &n
	}
	if st.Components != nil {
		for _, ps := range st.Components.PowerSupplies {
			row := MerakiPowerSupplySnap{
				Slot:   ps.Slot,
				Serial: ps.Serial,
				Model:  ps.Model,
				Status: ps.Status,
			}
			if ps.PoE != nil {
				m := ps.PoE.Maximum
				row.PoEMax = &m
				row.PoEUnit = ps.PoE.Unit
			}
			tel.PowerSupplies = append(tel.PowerSupplies, row)
		}
	}
	if up.HighAvailability != nil {
		tel.HA = &MerakiHASnap{Enabled: up.HighAvailability.Enabled, Role: up.HighAvailability.Role}
	}
	if up.LastReportedAt != "" && tel.LastReportedAt == "" {
		tel.LastReportedAt = up.LastReportedAt
	}
	for _, u := range up.Uplinks {
		snap := MerakiUplinkSnap{
			Interface:      u.Interface,
			Status:         u.Status,
			IP:             u.IP,
			Gateway:        u.Gateway,
			PublicIP:       u.PublicIP,
			PrimaryDNS:     u.PrimaryDNS,
			SecondaryDNS:   u.SecondaryDNS,
			IPAssignedBy:   u.IPAssignedBy,
			Provider:       u.Provider,
			SignalType:     u.SignalType,
			ICCID:          u.ICCID,
			ConnectionType: u.ConnectionType,
		}
		if u.SignalStat != nil {
			snap.RSRP = u.SignalStat.RSRP
			snap.RSRQ = u.SignalStat.RSRQ
		}
		tel.Uplinks = append(tel.Uplinks, snap)
	}
	if n := countActiveUplinks(tel.Uplinks); n > 0 {
		tel.UplinkCount = &n
	}
	if len(sw.Ports) > 0 {
		physTotal, physUp, uplinkN, errN := 0, 0, 0, 0
		for _, p := range sw.Ports {
			physTotal++
			if strings.EqualFold(p.Status, "Connected") {
				physUp++
			}
			if p.IsUplink {
				uplinkN++
			}
			if len(p.Errors) > 0 {
				errN++
			}
			if len(tel.Ports) < 256 {
				ps := MerakiPortSnap{
					PortID:   p.PortID,
					Status:   p.Status,
					Speed:    p.Speed,
					Duplex:   p.Duplex,
					Enabled:  p.Enabled,
					IsUplink: p.IsUplink,
					Errors:   p.Errors,
					Warnings: p.Warnings,
				}
				if p.PoE != nil {
					v := p.PoE.IsAllocated
					ps.PoEAllocated = &v
				}
				if p.SecurePort != nil && p.SecurePort.Active {
					ps.SecurePort = p.SecurePort.AuthenticationStatus
				}
				if p.SpanningTree != nil {
					ps.STPStatuses = p.SpanningTree.Statuses
				}
				if byPort, ok := ex.portPktsBySerial[serial]; ok {
					if pkt, ok := byPort[p.PortID]; ok {
						applyPortPackets(&ps, pkt)
					}
				}
				tel.Ports = append(tel.Ports, ps)
			}
		}
		tel.PhysTotal = &physTotal
		tel.PhysUp = &physUp
		tel.PortErrorCount = &errN
		if uplinkN > 0 {
			tel.UplinkCount = &uplinkN
		}
	}

	if vpn, ok := ex.vpnBySerial[serial]; ok {
		vs := &MerakiVPNSnap{Mode: vpn.VPNMode, DeviceStatus: vpn.DeviceStatus}
		for _, p := range vpn.MerakiVPNPeers {
			vs.TotalPeerCount++
			if strings.EqualFold(p.Reachability, "reachable") {
				vs.ReachablePeerCount++
			}
			vs.MerakiPeers = append(vs.MerakiPeers, MerakiVPNPeerSnap{
				Name: p.NetworkName, Reachability: p.Reachability,
			})
		}
		for _, p := range vpn.ThirdPartyVPNPeers {
			vs.TotalPeerCount++
			if strings.EqualFold(p.Reachability, "reachable") {
				vs.ReachablePeerCount++
			}
			vs.ThirdPartyPeers = append(vs.ThirdPartyPeers, MerakiVPNPeerSnap{
				Name: p.Name, Reachability: p.Reachability, PublicIP: p.PublicIP,
			})
		}
		tel.VPN = vs
	}
	if wl, ok := ex.wlanLossBySerial[serial]; ok {
		d := wl.Downstream.LossPercentage
		u := wl.Upstream.LossPercentage
		tel.WirelessLoss = &MerakiWirelessLossSnap{DownstreamLossPct: &d, UpstreamLossPct: &u}
	}
	if score, ok := ex.perfBySerial[serial]; ok {
		tel.PerfScore = &score
	}
	if d, ok := ex.devicesBySerial[serial]; ok && d.Firmware != "" {
		tel.Firmware = &MerakiFirmwareSnap{Current: d.Firmware}
	}
	if fw, ok := ex.fwBySerial[serial]; ok {
		if tel.Firmware == nil {
			tel.Firmware = &MerakiFirmwareSnap{}
		}
		tel.Firmware.Status = fw.Status
		if fw.Device != nil && fw.Device.Firmware != nil {
			if fw.Device.Firmware.CurrentVersion != nil {
				if fw.Device.Firmware.CurrentVersion.ShortName != "" {
					tel.Firmware.Current = fw.Device.Firmware.CurrentVersion.ShortName
				} else {
					tel.Firmware.Current = fw.Device.Firmware.CurrentVersion.Firmware
				}
			}
			if fw.Device.Firmware.NextUpgrade != nil {
				tel.Firmware.NextAt = fw.Device.Firmware.NextUpgrade.Time
				if fw.Device.Firmware.NextUpgrade.ToVersion != nil {
					tel.Firmware.NextUpgrade = fw.Device.Firmware.NextUpgrade.ToVersion.Firmware
				}
			}
		}
	}
	if sens, ok := ex.sensorBySerial[serial]; ok {
		tel.SensorReadings = convertSensorReadings(sens)
	}
	if alerts := ex.alertsBySerial[serial]; len(alerts) > 0 {
		for i, a := range alerts {
			if i >= 20 {
				break
			}
			tel.Alerts = append(tel.Alerts, MerakiAlertSnap{
				ID: a.ID, Severity: a.Severity, Title: a.Title, Type: a.Type, StartedAt: a.StartedAt,
			})
		}
	}
	return tel
}

func convertSensorReadings(sens meraki.SensorReadingLatest) []MerakiSensorReading {
	var out []MerakiSensorReading
	for _, r := range sens.Readings {
		row := MerakiSensorReading{Metric: r.Metric, TS: r.TS}
		switch {
		case r.Temperature != nil:
			v := r.Temperature.Celsius
			row.Value, row.Unit = &v, "celsius"
		case r.Humidity != nil:
			v := r.Humidity.RelativePercentage
			row.Value, row.Unit = &v, "%"
		case r.Battery != nil:
			v := r.Battery.Percentage
			row.Value, row.Unit = &v, "%"
		case r.Co2 != nil:
			v := r.Co2.Concentration
			row.Value, row.Unit = &v, "ppm"
		case r.Tvoc != nil:
			v := r.Tvoc.Concentration
			row.Value, row.Unit = &v, "µg/m³"
		case r.Pm25 != nil:
			v := r.Pm25.Concentration
			row.Value, row.Unit = &v, "µg/m³"
		case r.Noise != nil && r.Noise.Ambient != nil:
			v := r.Noise.Ambient.Level
			row.Value, row.Unit = &v, "dB"
		case r.Door != nil:
			b := r.Door.Open
			row.Bool = &b
		case r.Water != nil:
			b := r.Water.Present
			row.Bool = &b
		}
		out = append(out, row)
	}
	return out
}

func applyPortPackets(ps *MerakiPortSnap, pkt meraki.SwitchPortPackets) {
	for _, row := range pkt.Packets {
		if !strings.EqualFold(row.Desc, "Total") && row.Desc != "" {
			continue
		}
		rx, tx, total := row.Recv, row.Sent, row.Total
		ps.RxPackets = &rx
		ps.TxPackets = &tx
		ps.TotalPackets = &total
		return
	}
	if len(pkt.Packets) > 0 {
		row := pkt.Packets[0]
		rx, tx, total := row.Recv, row.Sent, row.Total
		ps.RxPackets = &rx
		ps.TxPackets = &tx
		ps.TotalPackets = &total
	}
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
	if tel.PortErrorCount != nil {
		v := float64(*tel.PortErrorCount)
		add("meraki.switch.ports.error_count", &v, "")
	}
	var rxSum, txSum float64
	havePkts := false
	for _, p := range tel.Ports {
		if p.RxPackets != nil {
			rxSum += float64(*p.RxPackets)
			havePkts = true
		}
		if p.TxPackets != nil {
			txSum += float64(*p.TxPackets)
			havePkts = true
		}
	}
	if havePkts {
		add("meraki.switch.ports.rx_packets", &rxSum, "")
		add("meraki.switch.ports.tx_packets", &txSum, "")
	}
	if tel.ClientCount != nil {
		v := float64(*tel.ClientCount)
		add("meraki.wireless.clients", &v, "")
	}
	if tel.WirelessLoss != nil {
		if tel.WirelessLoss.DownstreamLossPct != nil {
			v := *tel.WirelessLoss.DownstreamLossPct
			add("meraki.wireless.loss_downstream_pct", &v, "")
		}
		if tel.WirelessLoss.UpstreamLossPct != nil {
			v := *tel.WirelessLoss.UpstreamLossPct
			add("meraki.wireless.loss_upstream_pct", &v, "")
		}
	}
	if tel.PerfScore != nil {
		v := *tel.PerfScore
		add("meraki.appliance.perf_score", &v, "")
	}
	if tel.VPN != nil {
		v := float64(tel.VPN.ReachablePeerCount)
		add("meraki.vpn.peers_reachable", &v, "")
		t := float64(tel.VPN.TotalPeerCount)
		add("meraki.vpn.peers_total", &t, "")
	}
	for _, r := range tel.SensorReadings {
		key := "meraki.sensor." + strings.ToLower(r.Metric)
		if r.Value != nil {
			v := *r.Value
			add(key, &v, r.Unit)
		} else if r.Bool != nil {
			v := 0.0
			if *r.Bool {
				v = 1
			}
			add(key, &v, "")
		}
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
