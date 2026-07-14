package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	scrypto "github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/vendors/meraki"
)

// MerakiSyncResult summarizes one Dashboard inventory pull.
type MerakiSyncResult struct {
	Upserted int `json:"upserted"`
	Orgs     int `json:"orgs"`
	Devices  int `json:"devices"`
}

// SyncSiteMeraki pulls Meraki org devices into appliances for one site.
// orgFilter empty means all orgs visible to the API key.
func SyncSiteMeraki(ctx context.Context, pool *pgxpool.Pool, apiKey string, siteID uuid.UUID, orgFilter []string) (MerakiSyncResult, error) {
	var out MerakiSyncResult
	cli := meraki.New(apiKey)
	orgs, err := cli.ListOrganizations(ctx)
	if err != nil {
		return out, err
	}
	allow := map[string]bool{}
	for _, id := range orgFilter {
		id = strings.TrimSpace(id)
		if id != "" {
			allow[id] = true
		}
	}
	for _, org := range orgs {
		if len(allow) > 0 && !allow[org.ID] {
			continue
		}
		out.Orgs++
		devs, err := cli.ListDevices(ctx, org.ID)
		if err != nil {
			return out, fmt.Errorf("list devices for org %s: %w", org.Name, err)
		}
		uplinkIP := map[string]string{}
		if statuses, uerr := cli.ListApplianceUplinkStatuses(ctx, org.ID); uerr == nil {
			for _, st := range statuses {
				if ip := pickUplinkMgmtIP(st); ip != "" {
					uplinkIP[st.Serial] = ip
				}
			}
		} else {
			slog.Debug("meraki uplink statuses unavailable", "org", org.Name, "err", uerr)
		}
		for _, d := range devs {
			out.Devices++
			name := d.Name
			if name == "" {
				name = d.Serial
			}
			if name == "" {
				continue
			}
			ip := pickMerakiMgmtIP(d, uplinkIP[d.Serial])
			tags := merakiRoleTags(org.Name, d.ProductType, d.Model)
			_, err := pool.Exec(ctx, `
				INSERT INTO appliances (site_id, name, vendor, model, serial, mgmt_ip, snmp_version, is_active, tags)
				VALUES ($1, $2, 'meraki', $3, $4, $5::inet, 'v2c', TRUE, $6)
				ON CONFLICT (site_id, name) DO UPDATE SET
				  model = EXCLUDED.model,
				  serial = EXCLUDED.serial,
				  mgmt_ip = EXCLUDED.mgmt_ip,
				  tags = EXCLUDED.tags,
				  vendor = 'meraki',
				  is_active = TRUE,
				  updated_at = NOW()`,
				siteID, name, nullStr(d.Model), nullStr(d.Serial), ip, tags)
			if err != nil {
				continue
			}
			out.Upserted++
		}
	}
	return out, nil
}

// merakiRoleTags builds auto-detected inventory tags for a Meraki device.
func merakiRoleTags(orgName, productType, model string) []string {
	tags := []string{"meraki"}
	if orgName != "" {
		tags = append(tags, orgName)
	}
	role := merakiRoleFromProduct(productType, model)
	if role != "" {
		tags = append(tags, role)
	}
	return tags
}

func merakiRoleFromProduct(productType, model string) string {
	switch strings.ToLower(strings.TrimSpace(productType)) {
	case "appliance":
		return "firewall"
	case "wireless":
		return "wap"
	case "switch":
		return "switch"
	case "sensor":
		return "sensor"
	case "camera":
		return "camera"
	case "cellulargateway":
		return "cellular"
	case "wirelesscontroller":
		return "wlc"
	}
	m := strings.ToUpper(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(m, "MX"), strings.HasPrefix(m, "Z"):
		return "firewall"
	case strings.HasPrefix(m, "MR"), strings.HasPrefix(m, "CW"):
		return "wap"
	case strings.HasPrefix(m, "MS"), strings.HasPrefix(m, "C9"):
		return "switch"
	case strings.HasPrefix(m, "MT"):
		return "sensor"
	case strings.HasPrefix(m, "MV"):
		return "camera"
	case strings.HasPrefix(m, "MG"):
		return "cellular"
	}
	return ""
}

func pickMerakiMgmtIP(d meraki.Device, uplinkFallback string) string {
	for _, cand := range []string{d.LANIP, d.Wan1IP, d.Wan2IP, uplinkFallback} {
		if usableMerakiIP(cand) {
			return cand
		}
	}
	return "0.0.0.0"
}

func pickUplinkMgmtIP(st meraki.ApplianceUplinkStatus) string {
	var privateActive, privateAny, publicActive, publicAny string
	for _, u := range st.Uplinks {
		active := strings.EqualFold(u.Status, "active")
		for _, cand := range []string{u.IP, u.PublicIP} {
			if !usableMerakiIP(cand) {
				continue
			}
			priv := isPrivateIP(cand)
			switch {
			case priv && active && privateActive == "":
				privateActive = cand
			case priv && privateAny == "":
				privateAny = cand
			case !priv && active && publicActive == "":
				publicActive = cand
			case !priv && publicAny == "":
				publicAny = cand
			}
		}
	}
	for _, cand := range []string{privateActive, privateAny, publicActive, publicAny} {
		if cand != "" {
			return cand
		}
	}
	return ""
}

func usableMerakiIP(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "0.0.0.0" || s == "::" {
		return false
	}
	ip := net.ParseIP(s)
	return ip != nil && !ip.IsUnspecified() && !ip.IsLoopback()
}

func isPrivateIP(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return false
	}
	return ip.IsPrivate()
}

// RecordMerakiSyncStatus writes last-sync metadata on site_discovery_settings.
func RecordMerakiSyncStatus(ctx context.Context, pool *pgxpool.Pool, siteID uuid.UUID, syncErr error) {
	var errText *string
	if syncErr != nil {
		s := syncErr.Error()
		if len(s) > 500 {
			s = s[:500]
		}
		errText = &s
	}
	_, _ = pool.Exec(ctx, `
		INSERT INTO site_discovery_settings (site_id, meraki_last_sync_at, meraki_last_sync_error)
		VALUES ($1, NOW(), $2)
		ON CONFLICT (site_id) DO UPDATE SET
		  meraki_last_sync_at = NOW(),
		  meraki_last_sync_error = EXCLUDED.meraki_last_sync_error`,
		siteID, errText)
}

// StartMerakiSync starts DB-driven Meraki sync for sites with sync enabled,
// plus an optional env-based fallback (SONAR_MERAKI_API_KEY).
func StartMerakiSync(ctx context.Context, pool *pgxpool.Pool, sealer *scrypto.Sealer, log *slog.Logger) {
	go runMerakiDBLoop(ctx, pool, sealer, log)
	key := strings.TrimSpace(os.Getenv("SONAR_MERAKI_API_KEY"))
	if key == "" {
		return
	}
	siteID := strings.TrimSpace(os.Getenv("SONAR_MERAKI_SITE_ID"))
	go runMerakiEnvLoop(ctx, pool, log, key, siteID)
}

func runMerakiEnvLoop(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, apiKey, siteIDStr string) {
	log.Info("meraki env sync starting")
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	syncMerakiEnvOnce(ctx, pool, log, apiKey, siteIDStr)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			syncMerakiEnvOnce(ctx, pool, log, apiKey, siteIDStr)
		}
	}
}

func syncMerakiEnvOnce(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, apiKey, siteIDStr string) {
	siteID, err := resolveMerakiSite(ctx, pool, siteIDStr)
	if err != nil {
		log.Warn("meraki env sync: no target site", "err", err)
		return
	}
	fctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := SyncSiteMeraki(fctx, pool, apiKey, siteID, nil)
	RecordMerakiSyncStatus(fctx, pool, siteID, err)
	if err != nil {
		log.Warn("meraki env sync failed", "err", err)
		return
	}
	log.Info("meraki env sync complete", "site_id", siteID.String(), "upserted", res.Upserted, "devices", res.Devices)
}

func runMerakiDBLoop(ctx context.Context, pool *pgxpool.Pool, sealer *scrypto.Sealer, log *slog.Logger) {
	log.Info("meraki db sync loop starting")
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			syncMerakiDBDue(ctx, pool, sealer, log)
		}
	}
}

func syncMerakiDBDue(ctx context.Context, pool *pgxpool.Pool, sealer *scrypto.Sealer, log *slog.Logger) {
	rows, err := pool.Query(ctx, `
		SELECT ds.site_id, ds.meraki_org_ids, ds.meraki_sync_interval_seconds,
		       sc.id, sc.enc_secret
		  FROM site_discovery_settings ds
		  JOIN site_credentials sc ON sc.site_id = ds.site_id AND sc.kind = 'meraki'
		 WHERE ds.meraki_sync_enabled = TRUE
		   AND (ds.meraki_last_sync_at IS NULL
		        OR ds.meraki_last_sync_at < NOW() - make_interval(secs => ds.meraki_sync_interval_seconds))`)
	if err != nil {
		log.Debug("meraki db sync query failed", "err", err)
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
			log.Warn("meraki db sync: unseal failed", "site_id", siteID.String(), "err", err)
			continue
		}
		apiKey := parseMerakiAPIKey(plain)
		if apiKey == "" {
			continue
		}
		var orgIDs []string
		_ = json.Unmarshal(orgRaw, &orgIDs)
		fctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		res, syncErr := SyncSiteMeraki(fctx, pool, apiKey, siteID, orgIDs)
		RecordMerakiSyncStatus(fctx, pool, siteID, syncErr)
		cancel()
		if syncErr != nil {
			log.Warn("meraki db sync failed", "site_id", siteID.String(), "err", syncErr)
			continue
		}
		log.Info("meraki db sync complete",
			"site_id", siteID.String(),
			"upserted", res.Upserted,
			"devices", res.Devices,
			"interval_s", interval,
		)
	}
}

func parseMerakiAPIKey(plain []byte) string {
	s := strings.TrimSpace(string(plain))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "{") {
		var m map[string]string
		if json.Unmarshal(plain, &m) == nil {
			if k := strings.TrimSpace(m["apiKey"]); k != "" {
				return k
			}
			if k := strings.TrimSpace(m["api_key"]); k != "" {
				return k
			}
		}
	}
	return s
}

func resolveMerakiSite(ctx context.Context, pool *pgxpool.Pool, siteIDStr string) (uuid.UUID, error) {
	if siteIDStr != "" {
		return uuid.Parse(siteIDStr)
	}
	var id uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM sites ORDER BY created_at LIMIT 1`).Scan(&id)
	return id, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
