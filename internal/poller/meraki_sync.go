package poller

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/vendors/meraki"
)

// RunMerakiSyncLoop periodically pulls Meraki devices into appliances.
func RunMerakiSyncLoop(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, apiKey, siteIDStr string) {
	log.Info("meraki sync starting")
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	syncMerakiOnce(ctx, pool, log, apiKey, siteIDStr)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			syncMerakiOnce(ctx, pool, log, apiKey, siteIDStr)
		}
	}
}

func syncMerakiOnce(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, apiKey, siteIDStr string) {
	siteID, err := resolveMerakiSite(ctx, pool, siteIDStr)
	if err != nil {
		log.Warn("meraki sync: no target site", "err", err)
		return
	}
	cli := meraki.New(apiKey)
	fctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	orgs, err := cli.ListOrganizations(fctx)
	if err != nil {
		log.Warn("meraki sync: list orgs failed", "err", err)
		return
	}
	for _, org := range orgs {
		devs, err := cli.ListDevices(fctx, org.ID)
		if err != nil {
			log.Warn("meraki sync: list devices failed", "org", org.Name, "err", err)
			continue
		}
		for _, d := range devs {
			name := d.Name
			if name == "" {
				name = d.Serial
			}
			if name == "" {
				continue
			}
			ip := d.LANIP
			if ip == "" {
				ip = "0.0.0.0"
			}
			tags := []string{"meraki", org.Name}
			if d.ProductType != "" {
				tags = append(tags, d.ProductType)
			}
			_, err := pool.Exec(fctx, `
				INSERT INTO appliances (site_id, name, vendor, model, serial, mgmt_ip, snmp_version, is_active, tags)
				VALUES ($1, $2, 'meraki', $3, $4, $5::inet, 'v2c', TRUE, $6)
				ON CONFLICT (site_id, name) DO UPDATE SET
				  model = EXCLUDED.model,
				  serial = EXCLUDED.serial,
				  mgmt_ip = EXCLUDED.mgmt_ip,
				  tags = EXCLUDED.tags,
				  updated_at = NOW()`,
				siteID, name, nullStr(d.Model), nullStr(d.Serial), ip, tags)
			if err != nil {
				log.Debug("meraki sync: upsert failed", "name", name, "err", err)
			}
		}
	}
	log.Info("meraki sync complete", "site_id", siteID.String())
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
